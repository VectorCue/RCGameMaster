package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestMatchSkipsDisabledInspectionsButRequiresSetup(t *testing.T) {
	match, league, state := matchFixture()
	enabled := false
	league.InspectionsEnabled = &enabled
	state.Checks = nil
	now := time.Now().UTC()
	if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "start"}, now); err == nil {
		t.Fatal("disabling inspection bypassed sides/kickoff setup")
	}
	if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "checks"}, now); err == nil {
		t.Fatal("accepted an inspection in a league that disables inspections")
	}
	if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "setup", BlueTeamID: 1, KickoffTeamID: 2}, now); err != nil {
		t.Fatalf("no-inspection setup rejected: %v", err)
	}
	if state.Phase != "ready" || len(state.Checks) != 0 {
		t.Fatal("disabled inspections created inspection records or failed to prepare match")
	}
	if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "start"}, now); err != nil {
		t.Fatalf("match without required inspections could not start: %v", err)
	}
	if state.Phase != "first_half" || !state.Running || match.Status != "in_progress" {
		t.Fatal("referee control did not start normally with inspections disabled")
	}
}

func TestInspectionDefaultAndExplicitEnabledStillRequireChecks(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		match, league, state := matchFixture()
		if explicit {
			enabled := true
			league.InspectionsEnabled = &enabled
		}
		state.Checks = nil
		if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "setup", BlueTeamID: 1, KickoffTeamID: 2}, time.Now()); err == nil {
			t.Fatal("enabled/default inspections accepted setup without required robot checks")
		}
	}
}

func TestReadyMatchUsesCurrentClockSettingsAtKickoff(t *testing.T) {
	match, league, state := matchFixture()
	now := time.Now().UTC()
	if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "setup", BlueTeamID: 1, KickoffTeamID: 2}, now); err != nil {
		t.Fatal(err)
	}
	league.HalfSeconds = 420
	if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "start"}, now); err != nil {
		t.Fatal(err)
	}
	if state.RemainingSeconds != 420 {
		t.Fatal("match started with a stale pre-setup clock duration")
	}
}

func TestNoInspectionMatchAPIAndRefereeModeGuard(t *testing.T) {
	db := integrationDB(t)
	app, err := newApp(localTestConfig(), db)
	if err != nil {
		t.Fatal(err)
	}
	tid := createdID(t, app, "/api/v1/tournaments", fiber.Map{"name": "Cup", "abbrev": "NOINSPECTION", "start_date": "2026-10-25", "end_date": "2026-10-25", "timezone": "UTC"})
	base := fmt.Sprintf("/api/v1/tournaments/%d/", tid)
	main := createdID(t, app, base+"leagues", fiber.Map{"name": "Soccer", "abbrev": "soccer", "mode": "full", "referee_mode": true})
	child := createdID(t, app, base+"leagues", fiber.Map{"name": "Entry", "abbrev": "entry", "parent_id": main, "inspections_enabled": false})
	stage := createdID(t, app, base+"stages", fiber.Map{"name": "Final", "league_id": child, "type": "manual"})
	team1 := createdID(t, app, base+"teams", fiber.Map{"name": "A", "league_id": child})
	team2 := createdID(t, app, base+"teams", fiber.Map{"name": "B", "league_id": child})
	rid := createdID(t, app, base+"referees", fiber.Map{"name": "Referee"})
	mid := createdID(t, app, base+"matches", fiber.Map{"stage_id": stage, "league_id": child, "team1_id": team1, "team2_id": team2})
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", mid), fiber.Map{"slot_id": nil, "referee_ids": []uint{rid}}, 200)
	control := fmt.Sprintf("/api/v1/matches/%d/control", mid)
	requestJSON(t, app, "POST", control, fiber.Map{"action": "setup", "blue_team_id": team1, "kickoff_team_id": team2}, 200)
	var match domain.Match
	if err := json.Unmarshal(requestJSON(t, app, "POST", control, fiber.Map{"action": "start"}, 200), &match); err != nil {
		t.Fatal(err)
	}
	if match.Status != "in_progress" {
		t.Fatal("inspection-disabled match failed to start through API")
	}
	// A disabled referee mode must still prevent live-clock control entirely.
	if err := db.Model(&domain.League{}).Where("id = ?", child).Update("referee_mode", false).Error; err != nil {
		t.Fatal(err)
	}
	requestJSON(t, app, "POST", control, fiber.Map{"action": "pause"}, 409)
}
