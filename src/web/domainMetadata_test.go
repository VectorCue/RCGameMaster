package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"github.com/gofiber/fiber/v3"
	"testing"
	"time"
)

func TestEditorMetadataUpdatePreservesResultAndRevalidatesSchedule(t *testing.T) {
	db := integrationDB(t)
	team2 := uint(2)
	fieldID, slotID := uint(1), uint(1)
	start := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	rows := []any{&domain.Tournament{ID: 1, Name: "Cup", Abbrev: "C", StartDate: "2026-10-25", EndDate: "2026-10-25", Timezone: "UTC"}, &domain.League{ID: 1, TournamentID: 1, Name: "Soccer", Abbrev: "s", Mode: "full", HalfSeconds: 600, HalftimeSeconds: 300}, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Round", Type: "manual"}, &domain.Team{ID: 1, TournamentID: 1, LeagueID: 1, Name: "A"}, &domain.Team{ID: 2, TournamentID: 1, LeagueID: 1, Name: "B"}, &domain.Field{ID: 1, TournamentID: 1, LeagueID: 1, Name: "A"}, &domain.Slot{ID: 1, TournamentID: 1, FieldID: 1, StartsAt: start, EndsAt: start.Add(30 * time.Minute)}, &domain.Match{ID: 1, TournamentID: 1, LeagueID: 1, StageID: 1, Number: 1, Team1ID: 1, Team2ID: &team2, Status: "scheduled", Published: true, FieldID: &fieldID, SlotID: &slotID, ScheduledAt: &start, DurationMinutes: 25, RefereeIDs: []uint{}, LiveState: []byte(`{}`)}}
	for _, row := range rows {
		if e := db.Create(row).Error; e != nil {
			t.Fatal(e)
		}
	}
	app := fiber.New()
	a := API{DB: db}
	v := app.Group("/api/v1", func(c fiber.Ctx) error { c.Locals("user", User{Roles: []string{"editor"}}); return c.Next() })
	a.registerDomainRoutes(v)
	body := fiber.Map{"league_id": 1, "stage_id": 1, "team1_id": 1, "team2_id": 2, "duration_minutes": 20, "round": 1, "status": "approved", "goals1": 99, "number": 99, "published": false, "live_state": fiber.Map{"phase": "finished"}}
	var got domain.Match
	if e := json.Unmarshal(requestJSON(t, app, "PUT", "/api/v1/matches/1", body, 200), &got); e != nil {
		t.Fatal(e)
	}
	if got.DurationMinutes != 20 || got.Status != "scheduled" || got.Goals1 != 0 || got.Number != 1 || !got.Published {
		t.Fatalf("unsafe metadata update: %+v", got)
	}
	body["duration_minutes"] = 40
	requestJSON(t, app, "PUT", "/api/v1/matches/1", body, 409)
	if e := db.First(&got, 1).Error; e != nil {
		t.Fatal(e)
	}
	if got.DurationMinutes != 20 {
		t.Fatal("failed edit was not rolled back")
	}
	db.Model(&got).Update("status", "in_progress")
	body["duration_minutes"] = 20
	requestJSON(t, app, "PUT", "/api/v1/matches/1", body, 409)
}
