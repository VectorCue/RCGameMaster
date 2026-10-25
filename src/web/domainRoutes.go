package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var abbrevPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,39}$`)

func hasRole(u User, roles ...string) bool {
	for _, have := range u.Roles {
		for _, want := range roles {
			if strings.EqualFold(have, want) {
				return true
			}
		}
	}
	return false
}
func requireRoles(roles ...string) fiber.Handler {
	return func(c fiber.Ctx) error {
		u, _ := c.Locals("user").(User)
		if !hasRole(u, roles...) {
			return apiError(c, 403, "You do not have permission for this action")
		}
		return c.Next()
	}
}
func apiError(c fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(fiber.Map{"error": message})
}

// Keep PostgreSQL diagnostics (schema names, SQL and connection details) out of
// API errors while retaining actionable domain validation messages.
func domainError(err error) string {
	var sqlError interface{ SQLState() string }
	if errors.As(err, &sqlError) {
		if sqlError.SQLState() == "23505" {
			return "A record with that abbreviation or number already exists"
		}
		return "Database operation failed"
	}
	message := err.Error()
	for _, prefix := range []string{"UNIQUE constraint failed", "FOREIGN KEY constraint failed", "CHECK constraint failed", "NOT NULL constraint failed", "sql:", "failed to connect", "ERROR:"} {
		if strings.HasPrefix(message, prefix) {
			return "Could not save record; check required values and unique abbreviations"
		}
	}
	return message
}
func dbError(c fiber.Ctx, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apiError(c, 404, "Record not found")
	}
	if err != nil {
		return apiError(c, 400, "Could not save record; check its values and unique abbreviation")
	}
	return nil
}
func paramID(c fiber.Ctx) (uint, error) {
	id, e := strconv.ParseUint(c.Params("id"), 10, 64)
	if e != nil || id == 0 || uint64(uint(id)) != id {
		return 0, errors.New("invalid ID")
	}
	return uint(id), nil
}
func lockTournament(tx *gorm.DB, id uint) error {
	var t domain.Tournament
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&t, id).Error
}
func leagueIn(tx *gorm.DB, id, tid uint) (domain.League, error) {
	var l domain.League
	err := tx.Where("id = ? AND tournament_id = ?", id, tid).First(&l).Error
	return l, err
}
func (a *API) registerDomainRoutes(v fiber.Router) {
	v.Use(requireRoles("admin", "editor", "referee", "viewer"))
	v.Get("/tournaments", a.listTournaments)
	v.Post("/tournaments", requireRoles("admin"), a.createTournament)
	v.Put("/tournaments/:id", requireRoles("admin"), a.updateTournament)
	v.Delete("/tournaments/:id", requireRoles("admin"), a.deleteTournament)
	v.Get("/tournaments/:id/workspace", a.workspace)
	registerEntity[domain.League](a, v, "leagues", validateLeague)
	registerEntity[domain.Stage](a, v, "stages", validateStage)
	registerEntity[domain.Field](a, v, "fields", validateField)
	registerEntity[domain.Slot](a, v, "slots", validateSlot)
	registerEntity[domain.Team](a, v, "teams", validateTeam)
	registerEntity[domain.Referee](a, v, "referees", validateReferee)
	registerEntity[domain.Match](a, v, "matches", validateMatch)
	v.Post("/fields/:id/copy-slots", requireRoles("admin"), a.copySlots)
	v.Post("/stages/:id/generate", requireRoles("admin"), a.generate)
	v.Put("/matches/:id", requireRoles("admin", "editor"), a.updateMatch)
	v.Post("/matches/:id/schedule", requireRoles("admin", "editor"), a.schedule)
	v.Post("/matches/:id/review", requireRoles("admin", "editor"), a.review)
	v.Post("/matches/:id/publish", requireRoles("admin", "editor"), a.publish)
}
func entityIDs(value any) (*uint, *uint) {
	switch v := value.(type) {
	case *domain.League:
		return &v.ID, &v.TournamentID
	case *domain.Stage:
		return &v.ID, &v.TournamentID
	case *domain.Field:
		return &v.ID, &v.TournamentID
	case *domain.Slot:
		return &v.ID, &v.TournamentID
	case *domain.Team:
		return &v.ID, &v.TournamentID
	case *domain.Referee:
		return &v.ID, &v.TournamentID
	case *domain.Match:
		return &v.ID, &v.TournamentID
	}
	panic("unknown entity")
}
func registerEntity[T any](a *API, v fiber.Router, name string, validate func(*gorm.DB, *T) error) {
	create := func(c fiber.Ctx) error {
		id, e := paramID(c)
		if e != nil {
			return apiError(c, 400, domainError(e))
		}
		var row T
		if e = c.Bind().Body(&row); e != nil {
			return apiError(c, 400, "Invalid JSON request")
		}
		rid, tid := entityIDs(&row)
		*rid = 0
		*tid = id
		e = a.DB.Transaction(func(tx *gorm.DB) error {
			if e := lockTournament(tx, id); e != nil {
				return e
			}
			if e := validate(tx, &row); e != nil {
				return e
			}
			return tx.Create(&row).Error
		})
		if e != nil {
			return apiError(c, 400, domainError(e))
		}
		return c.Status(201).JSON(row)
	}
	update := func(c fiber.Ctx) error {
		id, e := paramID(c)
		if e != nil {
			return apiError(c, 400, domainError(e))
		}
		var old T
		if e = a.DB.First(&old, id).Error; e != nil {
			return dbError(c, e)
		}
		var row T
		if e = c.Bind().Body(&row); e != nil {
			return apiError(c, 400, "Invalid JSON request")
		}
		rid, tid := entityIDs(&row)
		_, oldtid := entityIDs(&old)
		*rid = id
		*tid = *oldtid
		e = a.DB.Transaction(func(tx *gorm.DB) error {
			if e := lockTournament(tx, *tid); e != nil {
				return e
			}
			if e := tx.First(&old, id).Error; e != nil {
				return e
			}
			if league, ok := any(&row).(*domain.League); ok {
				preserveLeagueSettings(league, any(&old).(*domain.League), c.Body())
			}
			if e := validate(tx, &row); e != nil {
				return e
			}
			if e := protectUpdate(tx, name, id, &row, &old); e != nil {
				return e
			}
			if e := tx.Save(&row).Error; e != nil {
				return e
			}
			if league, ok := any(&row).(*domain.League); ok {
				previous := any(&old).(*domain.League)
				if league.InspectionsRequired() != previous.InspectionsRequired() {
					// Prepared sides/checks were made against different rules.
					// Return only this league's unstarted matches to pregame;
					// scheduling, assignments and results remain untouched.
					if e := tx.Model(&domain.Match{}).Where("tournament_id = ? AND league_id = ? AND status IN ?", league.TournamentID, league.ID, []string{"draft", "scheduled"}).Updates(map[string]any{"live_state": json.RawMessage("{}"), "updated_at": time.Now().UTC()}).Error; e != nil {
						return e
					}
				}
				if league.ParentID == nil {
					return cascadeLeagueControls(tx, league)
				}
			}
			return nil
		})
		if e != nil {
			return apiError(c, 400, domainError(e))
		}
		return c.JSON(row)
	}
	remove := func(c fiber.Ctx) error {
		id, e := paramID(c)
		if e != nil {
			return apiError(c, 400, domainError(e))
		}
		var row T
		if e = a.DB.First(&row, id).Error; e != nil {
			return dbError(c, e)
		}
		_, tid := entityIDs(&row)
		e = a.DB.Transaction(func(tx *gorm.DB) error {
			if e := lockTournament(tx, *tid); e != nil {
				return e
			}
			if e := protectDelete(tx, name, id); e != nil {
				return e
			}
			return tx.Delete(&row).Error
		})
		if e != nil {
			return apiError(c, 409, domainError(e))
		}
		return c.SendStatus(204)
	}
	v.Post("/tournaments/:id/"+name, requireRoles("admin"), create)
	if name != "matches" {
		v.Put("/"+name+"/:id", requireRoles("admin"), update)
	}
	v.Delete("/"+name+"/:id", requireRoles("admin"), remove)
}
func validName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 160 {
		return errors.New("name must contain 1–160 characters")
	}
	return nil
}
func validateTournament(t *domain.Tournament) error {
	if e := validName(t.Name); e != nil {
		return e
	}
	if !abbrevPattern.MatchString(t.Abbrev) {
		return errors.New("abbreviation must use 1–40 letters, numbers, underscores or hyphens")
	}
	start, e := time.Parse("2006-01-02", t.StartDate)
	if e != nil {
		return errors.New("invalid start date")
	}
	end, e := time.Parse("2006-01-02", t.EndDate)
	if e != nil || end.Before(start) {
		return errors.New("end date must be on or after start date")
	}
	if t.Timezone == "" {
		t.Timezone = "Europe/Berlin"
	}
	if _, e = time.LoadLocation(t.Timezone); e != nil {
		return errors.New("invalid IANA timezone")
	}
	return nil
}

// Disabled form controls may be omitted by clients. Preserve their persisted
// configuration so toggling inspection or referee mode never erases limits or
// clocks. A deliberately supplied numeric zero still retains its API meaning.
func preserveLeagueSettings(next, old *domain.League, body []byte) {
	if next.InspectionsEnabled == nil {
		enabled := old.InspectionsRequired()
		next.InspectionsEnabled = &enabled
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return
	}
	for _, field := range []struct {
		name string
		copy func()
	}{
		{"robot_count", func() { next.RobotCount = old.RobotCount }},
		{"max_weight_g", func() { next.MaxWeightG = old.MaxWeightG }},
		{"ball_zone_cm", func() { next.BallZoneCM = old.BallZoneCM }},
		{"voltage", func() { next.Voltage = old.Voltage }},
		{"size_cm", func() { next.SizeCM = old.SizeCM }},
		{"motor_limit", func() { next.MotorLimit = old.MotorLimit }},
		{"half_seconds", func() { next.HalfSeconds = old.HalfSeconds }},
		{"halftime_seconds", func() { next.HalftimeSeconds = old.HalftimeSeconds }},
	} {
		if _, present := fields[field.name]; !present {
			field.copy()
		}
	}
}
func validateLeague(tx *gorm.DB, l *domain.League) error {
	if l.InspectionsEnabled == nil {
		enabled := true
		l.InspectionsEnabled = &enabled
	}
	if e := validName(l.Name); e != nil {
		return e
	}
	if !abbrevPattern.MatchString(l.Abbrev) {
		return errors.New("invalid league abbreviation")
	}
	if l.ParentID != nil {
		parent, e := leagueIn(tx, *l.ParentID, l.TournamentID)
		if e != nil || parent.ParentID != nil || parent.ID == l.ID {
			return errors.New("parent must be a main league in this tournament")
		}
		l.Mode = parent.Mode
		l.RefereeMode = parent.RefereeMode
	}
	if l.Mode == "" {
		l.Mode = "full"
	}
	if l.Mode != "full" && l.Mode != "scheduling" {
		return errors.New("invalid league mode")
	}
	if l.Mode == "scheduling" {
		l.RefereeMode = false
	}
	if l.RobotCount == 0 {
		l.RobotCount = 2
	}
	if l.HalfSeconds == 0 {
		l.HalfSeconds = 600
	}
	if l.HalftimeSeconds == 0 {
		l.HalftimeSeconds = 300
	}
	if l.RobotCount < 1 || l.RobotCount > 20 || l.HalfSeconds < 1 || l.HalfSeconds > 86400 || l.HalftimeSeconds < 1 || l.HalftimeSeconds > 86400 {
		return errors.New("invalid robot count or match timing")
	}
	if l.InspectionsRequired() && (l.MaxWeightG < 0 || l.BallZoneCM < 0 || l.Voltage < 0 || l.SizeCM < 0 || l.MotorLimit < 0) {
		return errors.New("invalid robot inspection limits")
	}

	if l.ID != 0 && l.RefereeMode {
		var n int64
		minimum := (2*l.HalfSeconds + l.HalftimeSeconds + 59) / 60
		if e := tx.Model(&domain.Match{}).Where("league_id = ? AND scheduled_at IS NOT NULL AND duration_minutes < ?", l.ID, minimum).Count(&n).Error; e != nil {
			return e
		}
		if n > 0 {
			return errors.New("existing scheduled matches are too short for the new clock settings")
		}
	}
	return nil
}

// The main league owns the operation controls, while every child retains
// its own robot inspection and match clock settings.
func cascadeLeagueControls(tx *gorm.DB, parent *domain.League) error {
	children := []domain.League{}
	if e := tx.Where("tournament_id = ? AND parent_id = ?", parent.TournamentID, parent.ID).Find(&children).Error; e != nil {
		return e
	}
	for _, old := range children {
		child := old
		child.Mode = parent.Mode
		child.RefereeMode = parent.RefereeMode
		if e := validateLeague(tx, &child); e != nil {
			return e
		}
		if e := protectUpdate(tx, "leagues", child.ID, &child, &old); e != nil {
			return e
		}
		if e := tx.Model(&domain.League{}).Where("id = ?", child.ID).Updates(map[string]any{"mode": child.Mode, "referee_mode": child.RefereeMode}).Error; e != nil {
			return e
		}
	}
	return nil
}
func validateStage(tx *gorm.DB, s *domain.Stage) error {
	if s.TeamLimit < 0 || s.TeamLimit == 1 || s.TeamLimit > 256 {
		return errors.New("team limit must be zero or between 2 and 256")
	}
	if e := validName(s.Name); e != nil {
		return e
	}
	if _, e := leagueIn(tx, s.LeagueID, s.TournamentID); e != nil {
		return errors.New("league is not in this tournament")
	}
	switch s.Type {
	case "round_robin", "swiss", "knockout", "manual":
	default:
		return errors.New("invalid stage type")
	}
	if s.SourceStageID != nil {
		var other domain.Stage
		if tx.First(&other, *s.SourceStageID).Error != nil || other.LeagueID != s.LeagueID || other.ID == s.ID || other.SortOrder >= s.SortOrder {
			return errors.New("source must be an earlier stage of this league")
		}
	}
	if s.ID != 0 {
		var n int64
		if e := tx.Model(&domain.Stage{}).Where("source_stage_id = ? AND sort_order <= ?", s.ID, s.SortOrder).Count(&n).Error; e != nil {
			return e
		}
		if n > 0 {
			return errors.New("source stage must remain earlier than its dependent stages")
		}
	}
	return nil
}
func validateField(tx *gorm.DB, f *domain.Field) error {
	if e := validName(f.Name); e != nil {
		return e
	}
	if _, e := leagueIn(tx, f.LeagueID, f.TournamentID); e != nil {
		return errors.New("invalid field league")
	}
	return nil
}
func validateSlot(tx *gorm.DB, s *domain.Slot) error {
	var f domain.Field
	if tx.Where("id = ? AND tournament_id = ?", s.FieldID, s.TournamentID).First(&f).Error != nil {
		return errors.New("invalid field")
	}
	if s.StartsAt.IsZero() || !s.EndsAt.After(s.StartsAt) {
		return errors.New("slot end must be after start")
	}
	var t domain.Tournament
	if tx.First(&t, s.TournamentID).Error != nil {
		return errors.New("invalid tournament")
	}
	loc, _ := time.LoadLocation(t.Timezone)
	if loc == nil {
		loc = time.UTC
	}
	date := s.StartsAt.In(loc).Format("2006-01-02")
	if date < t.StartDate || date > t.EndDate || s.EndsAt.Sub(s.StartsAt) > 24*time.Hour {
		return errors.New("slot must be within tournament dates and at most 24 hours")
	}
	var n int64
	tx.Model(&domain.Slot{}).Where("field_id = ? AND id <> ? AND starts_at < ? AND ends_at > ?", s.FieldID, s.ID, s.EndsAt, s.StartsAt).Count(&n)
	if n > 0 {
		return errors.New("field availability overlaps another slot")
	}
	return nil
}
func validateTeam(tx *gorm.DB, t *domain.Team) error {
	if e := validName(t.Name); e != nil {
		return e
	}
	if _, e := leagueIn(tx, t.LeagueID, t.TournamentID); e != nil {
		return errors.New("invalid team league")
	}
	if t.Startnumber < 0 {
		return errors.New("starting number cannot be negative")
	}
	return nil
}
func validateReferee(tx *gorm.DB, r *domain.Referee) error {
	if r.UserID != "" {
		var n int64
		if e := tx.Model(&domain.Referee{}).Where("tournament_id = ? AND user_id = ? AND id <> ?", r.TournamentID, r.UserID, r.ID).Count(&n).Error; e != nil {
			return e
		}
		if n > 0 {
			return errors.New("this account is already linked to a referee in the tournament")
		}
	}
	if e := validName(r.Name); e != nil {
		return e
	}
	if r.AvailableFrom != nil && r.AvailableUntil != nil && !r.AvailableUntil.After(*r.AvailableFrom) {
		return errors.New("availability end must follow start")
	}
	return nil
}
func validateMatch(tx *gorm.DB, m *domain.Match) error {
	var s domain.Stage
	if tx.Where("id = ? AND tournament_id = ?", m.StageID, m.TournamentID).First(&s).Error != nil || s.LeagueID != m.LeagueID {
		return errors.New("stage is not in this league")
	}
	var a domain.Team
	if tx.First(&a, m.Team1ID).Error != nil || a.LeagueID != m.LeagueID {
		return errors.New("first team is not in this league")
	}
	if m.Team2ID != nil {
		var b domain.Team
		if *m.Team2ID == m.Team1ID || tx.First(&b, *m.Team2ID).Error != nil || b.LeagueID != m.LeagueID {
			return errors.New("second team must be a different team of this league")
		}
	}
	l, _ := leagueIn(tx, m.LeagueID, m.TournamentID)
	if m.DurationMinutes == 0 {
		m.DurationMinutes = (2*l.HalfSeconds + l.HalftimeSeconds + 59) / 60
	}
	if l.RefereeMode && m.DurationMinutes < (2*l.HalfSeconds+l.HalftimeSeconds+59)/60 {
		return errors.New("match duration must allow both halves and halftime")
	}
	if m.DurationMinutes < 1 || m.DurationMinutes > 1440 {
		return errors.New("duration must be between 1 and 1440 minutes")
	}
	m.IsBye = false
	m.Status = "draft"
	m.Published = false
	m.Goals1 = 0
	m.Goals2 = 0
	m.LiveState = []byte("{}")
	m.RefereeIDs = []uint{}
	m.FieldID = nil
	m.SlotID = nil
	m.ScheduledAt = nil
	if m.Round < 1 {
		m.Round = 1
	}
	var n int
	tx.Model(&domain.Match{}).Where("tournament_id = ?", m.TournamentID).Select("COALESCE(MAX(number),0)").Scan(&n)
	m.Number = n + 1
	return nil
}
func (a *API) listTournaments(c fiber.Ctx) error {
	rows := []domain.Tournament{}
	if e := a.DB.Order("start_date DESC, id DESC").Find(&rows).Error; e != nil {
		return dbError(c, e)
	}
	return c.JSON(rows)
}
func (a *API) createTournament(c fiber.Ctx) error {
	var row domain.Tournament
	if c.Bind().Body(&row) != nil {
		return apiError(c, 400, "Invalid JSON")
	}
	row.ID = 0
	if e := validateTournament(&row); e != nil {
		return apiError(c, 400, domainError(e))
	}
	if e := a.DB.Create(&row).Error; e != nil {
		return dbError(c, e)
	}
	return c.Status(201).JSON(row)
}
func (a *API) updateTournament(c fiber.Ctx) error {
	id, e := paramID(c)
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	var old domain.Tournament
	if e = a.DB.First(&old, id).Error; e != nil {
		return dbError(c, e)
	}
	var row domain.Tournament
	if c.Bind().Body(&row) != nil {
		return apiError(c, 400, "Invalid JSON")
	}
	row.ID = id
	row.CreatedAt = old.CreatedAt
	if e = validateTournament(&row); e != nil {
		return apiError(c, 400, domainError(e))
	}
	e = a.DB.Transaction(func(tx *gorm.DB) error {
		if e := lockTournament(tx, id); e != nil {
			return e
		}
		loc, _ := time.LoadLocation(row.Timezone)
		var slots []domain.Slot
		if e := tx.Where("tournament_id = ?", id).Find(&slots).Error; e != nil {
			return e
		}
		for _, slot := range slots {
			date := slot.StartsAt.In(loc).Format("2006-01-02")
			if date < row.StartDate || date > row.EndDate {
				return errors.New("existing field availability falls outside the new tournament dates")
			}
		}
		return tx.Save(&row).Error
	})
	if e != nil {
		return apiError(c, 409, domainError(e))
	}
	return c.JSON(row)
}
func (a *API) deleteTournament(c fiber.Ctx) error {
	id, e := paramID(c)
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	e = a.DB.Transaction(func(tx *gorm.DB) error {
		if e := lockTournament(tx, id); e != nil {
			return e
		}
		for _, m := range domain.Models() {
			if _, ok := m.(*domain.Tournament); ok {
				continue
			}
			if e := tx.Where("tournament_id = ?", id).Delete(m).Error; e != nil {
				return e
			}
		}
		return tx.Delete(&domain.Tournament{}, id).Error
	})
	if e != nil {
		return dbError(c, e)
	}
	return c.SendStatus(204)
}
