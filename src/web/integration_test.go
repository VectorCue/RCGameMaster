package web

import (
	"RCJVGameMaster/src/config"
	"RCJVGameMaster/src/domain"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.AutoMigrate(domain.Models()...); err != nil {
		t.Fatal(err)
	}
	return db
}

func localTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Auth.DisableFusionAuth = true
	cfg.Auth.FrontendURL = "http://localhost:2005/#/"
	cfg.Auth.OAuthRedirectURI = "http://localhost:2005/auth/callback"
	return cfg
}

func requestJSON(t *testing.T, app *fiber.App, method, path string, body any, want int) []byte {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TAS-CSRF", "1")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s: got %d, want %d: %s", method, path, res.StatusCode, want, got)
	}
	return got
}

func createdID(t *testing.T, app *fiber.App, path string, body any) uint {
	t.Helper()
	var row struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal(requestJSON(t, app, "POST", path, body, 201), &row); err != nil {
		t.Fatal(err)
	}
	if row.ID == 0 {
		t.Fatal("created record has no ID")
	}
	return row.ID
}

func TestTournamentSchedulingReviewAndPublicAPI(t *testing.T) {
	db := integrationDB(t)
	app, err := newApp(localTestConfig(), db)
	if err != nil {
		t.Fatal(err)
	}
	tid := createdID(t, app, "/api/v1/tournaments", fiber.Map{"name": "Test Cup", "abbrev": "TEST", "start_date": "2026-10-25", "end_date": "2026-10-26", "timezone": "Europe/Berlin"})
	base := fmt.Sprintf("/api/v1/tournaments/%d/", tid)
	lid := createdID(t, app, base+"leagues", fiber.Map{"name": "Soccer", "abbrev": "soccer", "mode": "full", "referee_mode": true, "robot_count": 1, "max_weight_g": 1500, "half_seconds": 600, "halftime_seconds": 300})
	stage := createdID(t, app, base+"stages", fiber.Map{"name": "Round robin", "league_id": lid, "type": "round_robin"})
	for i := 1; i <= 3; i++ {
		createdID(t, app, base+"teams", fiber.Map{"name": fmt.Sprintf("Team %d", i), "league_id": lid, "startnumber": i})
	}
	field := createdID(t, app, base+"fields", fiber.Map{"name": "Field A", "league_id": lid})
	fieldB := createdID(t, app, base+"fields", fiber.Map{"name": "Field B", "league_id": lid})
	ref := createdID(t, app, base+"referees", fiber.Map{"name": "Test Ref", "user_id": "linked-referee", "available_from": "2026-10-25T08:00:00Z", "available_until": "2026-10-25T12:00:00Z"})
	slot := createdID(t, app, base+"slots", fiber.Map{"field_id": field, "starts_at": "2026-10-25T09:00:00Z", "ends_at": "2026-10-25T09:30:00Z"})
	slotB := createdID(t, app, base+"slots", fiber.Map{"field_id": fieldB, "starts_at": "2026-10-25T09:00:00Z", "ends_at": "2026-10-25T09:30:00Z"})
	var generation struct {
		Matches []domain.Match `json:"matches"`
	}
	if err := json.Unmarshal(requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", stage), fiber.Map{}, 200), &generation); err != nil {
		t.Fatal(err)
	}
	if len(generation.Matches) != 3 {
		t.Fatal("expected three round robin matches")
	}
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", stage), fiber.Map{}, 409)
	m := generation.Matches[0]
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", m.ID), fiber.Map{"slot_id": slot, "referee_ids": []uint{ref}}, 200)
	// A different field cannot bypass shared team/referee overlap checks.
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", generation.Matches[1].ID), fiber.Map{"slot_id": slotB, "referee_ids": []uint{ref}}, 409)
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/publish", m.ID), fiber.Map{"published": true}, 200)
	public := requestJSON(t, app, "GET", "/api/public/TEST", nil, 200)
	var workspace domain.Workspace
	if err := json.Unmarshal(public, &workspace); err != nil {
		t.Fatal(err)
	}
	if len(workspace.Matches) != 1 || len(workspace.Slots) != 0 {
		t.Fatal("public snapshot leaked unplanned matches or private availability")
	}
	if strings.Contains(string(public), "linked-referee") {
		t.Fatal("public snapshot leaked account linkage")
	}
	// Drive the API through mandatory inspection and setup.
	control := fmt.Sprintf("/api/v1/matches/%d/control", m.ID)
	requestJSON(t, app, "POST", control, fiber.Map{"action": "start"}, 409)
	checks := []RobotCheck{}
	for _, team := range []uint{m.Team1ID, *m.Team2ID} {
		checks = append(checks, RobotCheck{TeamID: team, Robot: 1, WeightG: 1400, BallZone: true, Voltage: true, Size: true, Motors: true})
	}
	requestJSON(t, app, "POST", control, fiber.Map{"action": "checks", "checks": checks}, 200)
	requestJSON(t, app, "POST", control, fiber.Map{"action": "setup", "blue_team_id": m.Team1ID, "kickoff_team_id": *m.Team2ID}, 200)
	requestJSON(t, app, "POST", control, fiber.Map{"action": "start"}, 200)
	requestJSON(t, app, "POST", control, fiber.Map{"action": "goal", "team_id": m.Team1ID, "delta": 1}, 200)
	// Clock progression itself is tested with controlled time in TestRefereeLifecycle.
	var live LiveState
	if err := db.First(&m, m.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(m.LiveState, &live); err != nil {
		t.Fatal(err)
	}
	live.Phase = "finished"
	live.Running = false
	live.RemainingSeconds = 0
	live.StartedAt = nil
	m.LiveState, _ = json.Marshal(live)
	if err := db.Save(&m).Error; err != nil {
		t.Fatal(err)
	}
	sigs := testSignatures(t)
	sigs[0].ID = m.Team1ID
	sigs[1].ID = *m.Team2ID
	sigs[2].ID = ref
	requestJSON(t, app, "POST", control, fiber.Map{"action": "submit", "signatures": sigs, "notes": "Private referee note"}, 200)
	requestJSON(t, app, "POST", control, fiber.Map{"action": "goal", "team_id": m.Team1ID, "delta": 1}, 409)
	public = requestJSON(t, app, "GET", "/api/public/TEST", nil, 200)
	if strings.Contains(string(public), "data:image") || strings.Contains(string(public), "Private referee note") {
		t.Fatal("public snapshot leaked signatures or private notes")
	}
	if err := json.Unmarshal(public, &workspace); err != nil {
		t.Fatal(err)
	}
	if workspace.Matches[0].Goals1 != 0 {
		t.Fatal("unreviewed result published")
	}
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/review", m.ID), fiber.Map{"approve": true}, 200)
	public = requestJSON(t, app, "GET", "/api/public/TEST", nil, 200)
	if err := json.Unmarshal(public, &workspace); err != nil {
		t.Fatal(err)
	}
	if workspace.Matches[0].Status != "approved" || workspace.Matches[0].Goals1 != 1 {
		t.Fatal("approved result not automatically published")
	}
	var compat map[string]json.RawMessage
	if err := json.Unmarshal(requestJSON(t, app, "GET", "/rest/v1/TEST/matches?league=soccer&league_stage=0", nil, 200), &compat); err != nil {
		t.Fatal("existing stats client requires object wrapper:", err)
	}
	if len(compat["matches"]) == 0 {
		t.Fatal("compatibility API missing matches")
	}
	requestJSON(t, app, "GET", "/rest/v1/TEST/standings?league=soccer&league_stage=999", nil, 404)
}

func TestRolesAndCSRF(t *testing.T) {
	db := integrationDB(t)
	for _, role := range []string{"viewer", "referee", "editor", "Admin"} {
		t.Run(role, func(t *testing.T) {
			app := fiber.New()
			a := API{DB: db}
			v := app.Group("/api/v1", func(c fiber.Ctx) error {
				c.Locals("user", User{ID: "test-user", Roles: []string{role}})
				return c.Next()
			})
			a.registerDomainRoutes(v)
			a.registerMatchRoutes(v)
			requestJSON(t, app, "GET", "/api/v1/tournaments", nil, 200)
			want := 403
			if role == "Admin" {
				want = 201
			}
			requestJSON(t, app, "POST", "/api/v1/tournaments", fiber.Map{"name": "Roles", "abbrev": "ROLES", "start_date": "2026-10-25", "end_date": "2026-10-25"}, want)
			if role == "viewer" || role == "referee" {
				requestJSON(t, app, "POST", "/api/v1/matches/999/review", fiber.Map{"approve": true}, 403)
			}
		})
	}
	app, err := newApp(localTestConfig(), db)
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"", "https://untrusted.example"} {
		req := httptest.NewRequest("POST", "/api/v1/tournaments", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
			req.Header.Set("X-TAS-CSRF", "1")
		}
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatal("missing header or foreign Origin bypassed CSRF")
		}
	}
	cfg := localTestConfig()
	cfg.Auth.DisableFusionAuth = false
	cfg.Auth.FusionAuthURL = "http://localhost:9999"
	cfg.Auth.FusionAuthClientId = "client"
	cfg.Auth.FusionAuthTenantId = "tenant"
	cfg.Auth.FusionAuthSecret = "secret"
	protected, err := newApp(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON(t, protected, http.MethodGet, "/api/v1/tournaments", nil, 401)
	requestJSON(t, protected, http.MethodGet, "/api/public/tournaments", nil, 200)
}

func TestRefereeAssignmentRequired(t *testing.T) {
	db := integrationDB(t)
	team2 := uint(2)
	league := domain.League{TournamentID: 1, Name: "Soccer", Abbrev: "s", Mode: "full", RefereeMode: true, RobotCount: 1, HalfSeconds: 600, HalftimeSeconds: 300}
	for _, row := range []any{&domain.Tournament{ID: 1, Name: "Cup", Abbrev: "C", StartDate: "2026-10-25", EndDate: "2026-10-25", Timezone: "UTC"}, &league, &domain.Referee{ID: 3, TournamentID: 1, Name: "Ref", UserID: "assigned"}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	m := domain.Match{TournamentID: 1, LeagueID: league.ID, Team1ID: 1, Team2ID: &team2, Status: "scheduled", RefereeIDs: []uint{3}, LiveState: []byte(`{}`), CreatedAt: time.Now()}
	if err := db.Create(&m).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"unassigned", "assigned"} {
		app := fiber.New()
		a := API{DB: db}
		v := app.Group("/api/v1", func(c fiber.Ctx) error { c.Locals("user", User{ID: id, Roles: []string{"referee"}}); return c.Next() })
		a.registerMatchRoutes(v)
		want := 403
		if id == "assigned" {
			want = 200
		}
		requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/control", m.ID), fiber.Map{"action": "checks", "checks": []RobotCheck{}}, want)
	}
}
