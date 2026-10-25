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
	return a.Team1ID == b.Team1ID || (a.Team2ID != nil && *a.Team2ID == b.Team1ID) || (b.Team2ID != nil && a.Team1ID == *b.Team2ID) || (a.Team2ID != nil && b.Team2ID != nil && *a.Team2ID == *b.Team2ID)
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
	if m.Team2ID == nil && league.Mode != "scheduling" {
		return errors.New("a bye does not need a playing slot")
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
		l, e := leagueIn(tx, m.LeagueID, m.TournamentID)
		if e != nil {
			return e
		}
		if m.Status != "submitted" && m.Status != "approved" && !(!l.RefereeMode && (m.Status == "scheduled" || m.Status == "draft")) {
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
			if !l.RefereeMode {
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
		return tx.Save(&m).Error
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
		m.Published = body.Published
		return tx.Save(&m).Error
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
		candidate := domain.Match{ID: m.ID, TournamentID: m.TournamentID, LeagueID: body.LeagueID, StageID: body.StageID, Team1ID: body.Team1ID, Team2ID: body.Team2ID, DurationMinutes: body.DurationMinutes, Round: body.Round}
		if e := validateMatch(tx, &candidate); e != nil {
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
		refs := append([]uint{}, m.RefereeIDs...)
		if e := validateSchedule(tx, &m, m.SlotID, refs); e != nil {
			return e
		}
		return tx.Save(&m).Error
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
		if e := tx.Model(&domain.Match{}).Where("stage_id = ? AND round > ?", stage.ID, m.Round).Count(&n).Error; e != nil {
			return e
		}
		if n > 0 {
			return errors.New("remove later generated rounds before changing this approved result")
		}
	}
	var stages []domain.Stage
	if e := tx.Where("league_id = ? AND id <> ?", m.LeagueID, stage.ID).Find(&stages).Error; e != nil {
		return e
	}
	for _, next := range stages {
		explicit := next.SourceStageID != nil && *next.SourceStageID == stage.ID
		inferred := next.SourceStageID == nil && (next.Type == "swiss" || next.Type == "knockout") && (next.SortOrder > stage.SortOrder || next.SortOrder == stage.SortOrder && next.ID > stage.ID)
		if !explicit && !inferred {
			continue
		}
		var n int64
		if e := tx.Model(&domain.Match{}).Where("stage_id = ?", next.ID).Count(&n).Error; e != nil {
			return e
		}
		if n > 0 {
			return errors.New("remove dependent generated stages before changing this approved result")
		}
	}
	return nil
}
