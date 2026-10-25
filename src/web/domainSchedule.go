package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func intervalsOverlap(a, b time.Time, minutesA, minutesB int) bool {
	return a.Before(b.Add(time.Duration(minutesB)*time.Minute)) && b.Before(a.Add(time.Duration(minutesA)*time.Minute))
}
func sharesTeam(a, b domain.Match) bool {
	if a.IsPlaceholder || b.IsPlaceholder {
		return false
	}
	for _, x := range []uint{a.Team1ID, teamOrZero(a.Team2ID)} {
		for _, y := range []uint{b.Team1ID, teamOrZero(b.Team2ID)} {
			if x != 0 && x == y {
				return true
			}
		}
	}
	return false
}
func teamOrZero(id *uint) uint {
	if id == nil {
		return 0
	}
	return *id
}
func fieldAllowsLeague(field domain.Field, league domain.League) bool {
	return field.LeagueID == league.ID || (league.ParentID != nil && field.LeagueID == *league.ParentID)
}
func validateSchedule(tx *gorm.DB, m *domain.Match, slotID *uint, refereeIDs []uint) error {
	seen := map[uint]bool{}
	var referees []domain.Referee
	for _, id := range refereeIDs {
		if seen[id] {
			return errors.New("referee cannot be assigned twice")
		}
		seen[id] = true
		var r domain.Referee
		if e := tx.Where("id = ? AND tournament_id = ?", id, m.TournamentID).First(&r).Error; e != nil {
			return errors.New("referee is not in this tournament")
		}
		referees = append(referees, r)
	}
	if slotID == nil {
		m.SlotID = nil
		m.FieldID = nil
		m.ScheduledAt = nil
		m.RefereeIDs = refereeIDs
		m.Status = "draft"
		m.Published = false
		return nil
	}
	var slot domain.Slot
	if e := tx.Where("id = ? AND tournament_id = ?", *slotID, m.TournamentID).First(&slot).Error; e != nil {
		return errors.New("slot is not in this tournament")
	}
	var field domain.Field
	if e := tx.First(&field, slot.FieldID).Error; e != nil {
		return e
	}
	league, e := leagueIn(tx, m.LeagueID, m.TournamentID)
	if e != nil {
		return e
	}
	if !fieldAllowsLeague(field, league) {
		return errors.New("field is not available for this league")
	}
	end := slot.StartsAt.Add(time.Duration(m.DurationMinutes) * time.Minute)
	if end.After(slot.EndsAt) {
		return errors.New("match duration exceeds slot availability")
	}
	if m.IsBye {
		return errors.New("a bye does not need a playing slot")
	}
	if !m.IsPlaceholder && m.Team2ID == nil && league.Mode != "scheduling" {
		var stage domain.Stage
		if err := tx.First(&stage, m.StageID).Error; err != nil {
			return err
		}
		if stage.Type != "individual" {
			return errors.New("a bye does not need a playing slot")
		}
	}

	for _, r := range referees {
		if (r.AvailableFrom != nil && slot.StartsAt.Before(*r.AvailableFrom)) || (r.AvailableUntil != nil && end.After(*r.AvailableUntil)) {
			return errors.New("referee " + r.Name + " is unavailable during this match")
		}
	}
	var others []domain.Match
	if e := tx.Where("tournament_id = ? AND id <> ? AND scheduled_at IS NOT NULL", m.TournamentID, m.ID).Find(&others).Error; e != nil {
		return e
	}
	for _, other := range others {
		if other.ScheduledAt == nil || !intervalsOverlap(slot.StartsAt, *other.ScheduledAt, m.DurationMinutes, other.DurationMinutes) {
			continue
		}
		if other.FieldID != nil && *other.FieldID == field.ID {
			return errors.New("field already has an overlapping match")
		}
		if sharesTeam(*m, other) {
			return errors.New("a team already has an overlapping match")
		}
		for _, rid := range other.RefereeIDs {
			if seen[rid] {
				return errors.New("a referee already has an overlapping match")
			}
		}
	}
	m.SlotID = slotID
	m.FieldID = &field.ID
	m.ScheduledAt = &slot.StartsAt
	m.RefereeIDs = refereeIDs
	m.Status = "scheduled"
	return nil
}
func (a *API) schedule(c fiber.Ctx) error {
	id, e := paramID(c)
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	var body struct {
		SlotID     *uint  `json:"slot_id"`
		RefereeIDs []uint `json:"referee_ids"`
	}
	if c.Bind().Body(&body) != nil {
		return apiError(c, 400, "Invalid JSON")
	}
	var m domain.Match
	e = a.DB.Transaction(func(tx *gorm.DB) error {
		if e := tx.First(&m, id).Error; e != nil {
			return e
		}
		if e := lockTournament(tx, m.TournamentID); e != nil {
			return e
		}
		if e := tx.First(&m, id).Error; e != nil {
			return e
		}
		if m.Status != "draft" && m.Status != "scheduled" {
			return errors.New("only unstarted matches can be rescheduled")
		}
		if e := validateSchedule(tx, &m, body.SlotID, body.RefereeIDs); e != nil {
			return e
		}
		return tx.Save(&m).Error
	})
	if e != nil {
		return apiError(c, 409, domainError(e))
	}
	return c.JSON(m)
}
func (a *API) review(c fiber.Ctx) error {
	id, e := paramID(c)
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	var body struct {
		Approve bool    `json:"approve"`
		Notes   *string `json:"notes"`
		Goals1  *int    `json:"goals1"`
		Goals2  *int    `json:"goals2"`
	}
	if c.Bind().Body(&body) != nil {
		return apiError(c, 400, "Invalid JSON")
	}
	var m domain.Match
	e = a.DB.Transaction(func(tx *gorm.DB) error {
		if e := tx.First(&m, id).Error; e != nil {
			return e
		}
		if e := lockTournament(tx, m.TournamentID); e != nil {
			return e
		}
		if e := tx.First(&m, id).Error; e != nil {
			return e
		}
		wasPublishedResult := m.Status == "approved" && m.Published
		if m.IsPlaceholder || m.Team1ID == 0 {
			return errors.New("assign teams before reviewing this empty match")
		}
		l, e := leagueIn(tx, m.LeagueID, m.TournamentID)
		if e != nil {
			return e
		}
		var stage domain.Stage
		if err := tx.First(&stage, m.StageID).Error; err != nil {
			return err
		}
		individual := stage.Type == "individual"
		if m.Status != "submitted" && m.Status != "approved" && !((!l.RefereeMode || individual) && (m.Status == "scheduled" || m.Status == "draft")) {
			return errors.New("result must be submitted before review")
		}
		proposed1, proposed2 := m.Goals1, m.Goals2
		if body.Goals1 != nil {
			proposed1 = *body.Goals1
		}
		if body.Goals2 != nil {
			proposed2 = *body.Goals2
		}
		if m.IsBye && (!body.Approve || proposed1 != 0 || proposed2 != 0) {
			return errors.New("a generated bye is awarded automatically and cannot be scored or rejected")
		}
		if m.Status == "approved" && (!body.Approve || proposed1 != m.Goals1 || proposed2 != m.Goals2) {
			if e := protectResultChange(tx, m); e != nil {
				return e
			}
		}
		if body.Goals1 != nil {
			m.Goals1 = *body.Goals1
		}
		if body.Goals2 != nil {
			m.Goals2 = *body.Goals2
		}
		if m.Goals1 < 0 || m.Goals2 < 0 {
			return errors.New("scores cannot be negative")
		}
		if body.Approve {
			m.Status = "approved"
			m.Published = true
		} else {
			m.Status = "in_progress"
			m.Published = false
			if !l.RefereeMode || individual {
				m.Status = "draft"
				if m.SlotID != nil {
					m.Status = "scheduled"
				}
			}
			state := map[string]json.RawMessage{}
			if json.Unmarshal(m.LiveState, &state) != nil || state == nil {
				state = map[string]json.RawMessage{}
			}
			state["signatures"] = json.RawMessage("[]")
			delete(state, "signed_goals1")
			delete(state, "signed_goals2")
			m.LiveState, _ = json.Marshal(state)
		}
		if body.Notes != nil {
			m.Notes = *body.Notes
		}
		if err := tx.Save(&m).Error; err != nil {
			return err
		}
		if wasPublishedResult && (m.Status != "approved" || !m.Published) {
			return invalidateStandingPublicationForMatch(tx, m)
		}
		return nil
	})
	if e != nil {
		return apiError(c, 409, domainError(e))
	}
	return c.JSON(m)
}
func (a *API) publish(c fiber.Ctx) error {
	id, e := paramID(c)
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	var body struct {
		Published bool `json:"published"`
	}
	if c.Bind().Body(&body) != nil {
		return apiError(c, 400, "Invalid JSON")
	}
	var m domain.Match
	e = a.DB.Transaction(func(tx *gorm.DB) error {
		if e := tx.First(&m, id).Error; e != nil {
			return e
		}
		if e := lockTournament(tx, m.TournamentID); e != nil {
			return e
		}
		if e := tx.First(&m, id).Error; e != nil {
			return e
		}
		if body.Published && m.ScheduledAt == nil && m.Status != "approved" {
			return errors.New("schedule the match before publishing")
		}
		wasPublishedResult := m.Status == "approved" && m.Published
		m.Published = body.Published
		if err := tx.Save(&m).Error; err != nil {
			return err
		}
		if wasPublishedResult && !m.Published {
			return invalidateStandingPublicationForMatch(tx, m)
		}
		return nil
	})
	if e != nil {
		return apiError(c, 409, domainError(e))
	}
	return c.JSON(m)
}

// updateMatch keeps result and referee state server-owned. Metadata can be
// changed only before the game starts, and a scheduled game must still fit its
// field and avoid every team/referee conflict after the edit.
func (a *API) updateMatch(c fiber.Ctx) error {
	id, e := paramID(c)
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	var body struct {
		LeagueID        uint   `json:"league_id"`
		StageID         uint   `json:"stage_id"`
		Team1ID         uint   `json:"team1_id"`
		Team2ID         *uint  `json:"team2_id"`
		DurationMinutes int    `json:"duration_minutes"`
		Round           int    `json:"round"`
		Notes           string `json:"notes"`
	}
	if c.Bind().Body(&body) != nil {
		return apiError(c, 400, "Invalid JSON")
	}
	var m domain.Match
	e = a.DB.Transaction(func(tx *gorm.DB) error {
		if e := tx.First(&m, id).Error; e != nil {
			return e
		}
		if e := lockTournament(tx, m.TournamentID); e != nil {
			return e
		}
		if e := tx.First(&m, id).Error; e != nil {
			return e
		}
		if m.Status != "draft" && m.Status != "scheduled" {
			return errors.New("match metadata can only change before play begins")
		}
		previousStageID := m.StageID
		if body.StageID != m.StageID {
			var source, target domain.Stage
			if err := tx.First(&source, m.StageID).Error; err != nil {
				return err
			}
			if err := tx.First(&target, body.StageID).Error; err != nil {
				return err
			}
			if source.PreparedTeams > 0 && source.Type != "manual" || target.PreparedTeams > 0 && target.Type != "manual" {
				return errors.New("prepared automatic matches must stay in their stage; use Stage games to populate or clear assignments")
			}
		}
		candidate := domain.Match{ID: m.ID, TournamentID: m.TournamentID, LeagueID: body.LeagueID, StageID: body.StageID, Team1ID: body.Team1ID, Team2ID: body.Team2ID, DurationMinutes: body.DurationMinutes, Round: body.Round}
		if m.IsPlaceholder && candidate.Team1ID == 0 && candidate.Team2ID == nil {
			if candidate.StageID != m.StageID || candidate.LeagueID != m.LeagueID || candidate.Round != m.Round {
				return errors.New("empty match reservations must remain in their prepared stage and round")
			}
			league, err := leagueIn(tx, m.LeagueID, m.TournamentID)
			if err != nil {
				return err
			}
			var stage domain.Stage
			if err := tx.First(&stage, m.StageID).Error; err != nil {
				return err
			}
			if candidate.DurationMinutes == 0 {
				candidate.DurationMinutes = defaultMatchDuration(league)
			}
			if candidate.DurationMinutes < 1 || candidate.DurationMinutes > 1440 || league.RefereeMode && stage.Type != "individual" && candidate.DurationMinutes < defaultMatchDuration(league) {
				return errors.New("invalid reserved match duration")
			}
			candidate.IsPlaceholder = true
		} else if e := validateMatch(tx, &candidate); e != nil {
			return e
		}
		m.LeagueID = candidate.LeagueID
		m.StageID = candidate.StageID
		m.Team1ID = candidate.Team1ID
		m.Team2ID = candidate.Team2ID
		m.DurationMinutes = candidate.DurationMinutes
		m.Round = candidate.Round
		m.Notes = body.Notes
		m.LiveState = []byte("{}")
		m.IsBye = false
		m.IsPlaceholder = candidate.IsPlaceholder
		refs := append([]uint{}, m.RefereeIDs...)
		if e := validateSchedule(tx, &m, m.SlotID, refs); e != nil {
			return e
		}
		if err := tx.Save(&m).Error; err != nil {
			return err
		}
		if previousStageID != m.StageID {
			if err := updatePopulatedRound(tx, previousStageID); err != nil {
				return err
			}
		}
		return updatePopulatedRound(tx, m.StageID)
	})
	if e != nil {
		return apiError(c, 409, domainError(e))
	}
	return c.JSON(m)
}

// Existing draw decisions remain stable once a later round or qualifying stage
// has been generated. Administrators can remove unstarted dependents first.
func protectResultChange(tx *gorm.DB, m domain.Match) error {
	var stage domain.Stage
	if e := tx.First(&stage, m.StageID).Error; e != nil {
		return e
	}
	if stage.Type == "swiss" || stage.Type == "knockout" {
		var n int64
		if e := tx.Model(&domain.Match{}).Where("stage_id = ? AND round > ? AND is_placeholder = ?", stage.ID, m.Round, false).Count(&n).Error; e != nil {
			return e
		}
		if n > 0 {
			return errors.New("remove later generated rounds before changing this approved result")
		}
	}
	return protectSeededStageDependents(tx, stage)
}
