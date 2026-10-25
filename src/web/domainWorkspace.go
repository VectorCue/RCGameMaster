package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func protectDelete(tx *gorm.DB, name string, id uint) error {
	checks := map[string][]struct {
		model any
		where string
	}{
		"leagues": {{&domain.League{}, "parent_id = ?"}, {&domain.Team{}, "league_id = ?"}, {&domain.Stage{}, "league_id = ?"}, {&domain.Field{}, "league_id = ?"}},
		"stages":  {{&domain.Match{}, "stage_id = ?"}, {&domain.Stage{}, "source_stage_id = ?"}},
		"fields":  {{&domain.Match{}, "field_id = ?"}, {&domain.Slot{}, "field_id = ?"}},
		"slots":   {{&domain.Match{}, "slot_id = ?"}},
		"teams":   {{&domain.Match{}, "team1_id = ? OR team2_id = ?"}},
	}
	for _, check := range checks[name] {
		var count int64
		args := []any{id}
		if name == "teams" {
			args = append(args, id)
		}
		if e := tx.Model(check.model).Where(check.where, args...).Count(&count).Error; e != nil {
			return e
		}
		if count > 0 {
			return errors.New("record is in use; remove its dependent records first")
		}
	}
	if name == "matches" {
		var m domain.Match
		if e := tx.First(&m, id).Error; e != nil {
			return e
		}
		if m.IsBye {
			if e := protectResultChange(tx, m); e != nil {
				return e
			}
		} else if m.Status != "draft" && m.Status != "scheduled" {
			return errors.New("a started match cannot be deleted")
		}
	}
	if name == "referees" {
		var matches []domain.Match
		if e := tx.Find(&matches).Error; e != nil {
			return e
		}
		for _, m := range matches {
			for _, r := range m.RefereeIDs {
				if r == id {
					return errors.New("referee is assigned to a match")
				}
			}
		}
	}
	return nil
}
func protectUpdate(tx *gorm.DB, name string, id uint, row, old any) error {
	switch name {
	case "leagues":
		r, o := row.(*domain.League), old.(*domain.League)
		if (r.ParentID == nil) != (o.ParentID == nil) || (r.ParentID != nil && o.ParentID != nil && *r.ParentID != *o.ParentID) {
			if e := protectDelete(tx, name, id); e != nil {
				return e
			}
		}
		controlsChanged := r.Mode != o.Mode || r.RefereeMode != o.RefereeMode
		var n int64
		if e := tx.Model(&domain.Match{}).Where("league_id = ? AND status NOT IN ?", id, []string{"draft", "scheduled"}).Count(&n).Error; e != nil {
			return e
		}
		if n > 0 && (controlsChanged || r.InspectionsRequired() != o.InspectionsRequired() || r.RobotCount != o.RobotCount || r.HalfSeconds != o.HalfSeconds || r.HalftimeSeconds != o.HalftimeSeconds || r.MaxWeightG != o.MaxWeightG || r.BallZoneCM != o.BallZoneCM || r.Voltage != o.Voltage || r.SizeCM != o.SizeCM || r.MotorLimit != o.MotorLimit) {
			return errors.New("league mode, inspection and timing rules cannot change after matches have started")
		}
		if controlsChanged && r.ParentID == nil {
			var children []uint
			if e := tx.Model(&domain.League{}).Where("tournament_id = ? AND parent_id = ?", r.TournamentID, r.ID).Pluck("id", &children).Error; e != nil {
				return e
			}
			if len(children) > 0 {
				if e := tx.Model(&domain.Match{}).Where("league_id IN ? AND status NOT IN ?", children, []string{"draft", "scheduled"}).Count(&n).Error; e != nil {
					return e
				}
				if n > 0 {
					return errors.New("main league controls cannot change after a sub-league match has started")
				}
			}
		}
	case "stages":
		r, o := row.(*domain.Stage), old.(*domain.Stage)
		if r.LeagueID != o.LeagueID || r.Type != o.Type || !equalID(r.SourceStageID, o.SourceStageID) {
			if e := protectDelete(tx, name, id); e != nil {
				return e
			}
		}
	case "teams":
		r, o := row.(*domain.Team), old.(*domain.Team)
		if r.LeagueID != o.LeagueID {
			if e := protectDelete(tx, name, id); e != nil {
				return e
			}
		}
	case "fields":
		r, o := row.(*domain.Field), old.(*domain.Field)
		if r.LeagueID != o.LeagueID {
			if e := protectDelete(tx, name, id); e != nil {
				return e
			}
		}
	case "slots":
		if e := protectDelete(tx, name, id); e != nil {
			return errors.New("unschedule the match before changing its slot")
		}
	case "referees":
		r := row.(*domain.Referee)
		var matches []domain.Match
		if e := tx.Find(&matches).Error; e != nil {
			return e
		}
		for _, m := range matches {
			for _, rid := range m.RefereeIDs {
				if rid == id && m.ScheduledAt != nil {
					end := m.ScheduledAt.Add(time.Duration(m.DurationMinutes) * time.Minute)
					if (r.AvailableFrom != nil && m.ScheduledAt.Before(*r.AvailableFrom)) || (r.AvailableUntil != nil && end.After(*r.AvailableUntil)) {
						return errors.New("new availability conflicts with an assigned match")
					}
				}
			}
		}
	}
	return nil
}
func equalID(a, b *uint) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
func loadWorkspace(db *gorm.DB, id uint, public bool) (domain.Workspace, error) {
	w := domain.Workspace{Leagues: []domain.League{}, Stages: []domain.Stage{}, Fields: []domain.Field{}, Slots: []domain.Slot{}, Teams: []domain.Team{}, Referees: []domain.Referee{}, Matches: []domain.Match{}, Standings: []domain.Standing{}}
	if e := db.First(&w.Tournament, id).Error; e != nil {
		return w, e
	}
	for _, part := range []struct {
		out   any
		order string
	}{{&w.Leagues, "id"}, {&w.Stages, "sort_order, id"}, {&w.Fields, "id"}, {&w.Slots, "starts_at, id"}, {&w.Teams, "startnumber, id"}, {&w.Referees, "name, id"}} {
		if e := db.Where("tournament_id = ?", id).Order(part.order).Find(part.out).Error; e != nil {
			return w, e
		}
	}
	query := db.Where("tournament_id = ?", id)
	if public {
		query = query.Where("published = ?", true)
	}
	if e := query.Order("number, id").Find(&w.Matches).Error; e != nil {
		return w, e
	}
	for _, s := range w.Stages {
		w.Standings = append(w.Standings, domain.Standings(w.Teams, w.Matches, s.LeagueID, s.ID)...)
	}
	if public {
		w.Slots = []domain.Slot{}
		for i := range w.Referees {
			w.Referees[i].UserID = ""
			w.Referees[i].AvailableFrom = nil
			w.Referees[i].AvailableUntil = nil
		}
		for i := range w.Matches {
			m := &w.Matches[i]
			m.Notes = ""
			var state struct {
				Phase            string     `json:"phase"`
				RemainingSeconds int        `json:"remaining_seconds"`
				Running          bool       `json:"running"`
				StartedAt        *time.Time `json:"started_at"`
				BlueTeamID       *uint      `json:"blue_team_id"`
				KickoffTeamID    *uint      `json:"kickoff_team_id"`
			}
			if json.Unmarshal(m.LiveState, &state) == nil {
				m.LiveState, _ = json.Marshal(state)
			} else {
				m.LiveState = []byte("{}")
				state.Phase = ""
			}
			activePhase := state.Phase == "first_half" || state.Phase == "halftime" || state.Phase == "second_half"
			if m.Status != "approved" && (m.Status != "in_progress" || !activePhase) {
				m.Goals1 = 0
				m.Goals2 = 0
			}
		}
	}
	return w, nil
}
func (a *API) workspace(c fiber.Ctx) error {
	id, e := paramID(c)
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	w, e := loadWorkspace(a.DB, id, false)
	if e != nil {
		return dbError(c, e)
	}
	return c.JSON(w)
}
func (a *API) copySlots(c fiber.Ctx) error {
	id, e := paramID(c)
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	var body struct {
		SourceFieldID uint `json:"source_field_id"`
	}
	if c.Bind().Body(&body) != nil {
		return apiError(c, 400, "Invalid JSON")
	}
	out := []domain.Slot{}
	e = a.DB.Transaction(func(tx *gorm.DB) error {
		var target, source domain.Field
		if e := tx.First(&target, id).Error; e != nil {
			return e
		}
		if e := lockTournament(tx, target.TournamentID); e != nil {
			return e
		}
		if e := tx.First(&source, body.SourceFieldID).Error; e != nil {
			return e
		}
		if source.TournamentID != target.TournamentID || source.LeagueID != target.LeagueID || source.ID == target.ID {
			return errors.New("copy requires two different fields in the same league")
		}
		var slots []domain.Slot
		if e := tx.Where("field_id = ?", source.ID).Find(&slots).Error; e != nil {
			return e
		}
		for _, s := range slots {
			s.ID = 0
			s.FieldID = target.ID
			if e := validateSlot(tx, &s); e != nil {
				return e
			}
			if e := tx.Create(&s).Error; e != nil {
				return e
			}
			out = append(out, s)
		}
		return nil
	})
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	return c.JSON(fiber.Map{"slots": out})
}
