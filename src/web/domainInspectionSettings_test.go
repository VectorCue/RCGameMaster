package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestInspectionFlagPersistsFalseAndOmittedUpdatesPreserveSettings(t *testing.T) {
	db, app := integrityFixture(t)
	child := readLeagueResponse(t, requestJSON(t, app, "POST", "/api/v1/tournaments/1/leagues", fiber.Map{"name": "Infrared", "abbrev": "ir", "parent_id": 1, "inspections_enabled": false, "robot_count": 3, "max_weight_g": 1400, "ball_zone_cm": 1.5, "voltage": 24, "size_cm": 22, "motor_limit": 4, "half_seconds": 720, "halftime_seconds": 180}, 201))
	if child.InspectionsEnabled == nil || child.InspectionsRequired() {
		t.Fatal("explicit false was replaced by default true")
	}
	var stored domain.League
	if err := db.First(&stored, child.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.InspectionsEnabled == nil || stored.InspectionsRequired() {
		t.Fatal("disabled inspections did not survive database reload")
	}
	var workspace domain.Workspace
	if err := json.Unmarshal(requestJSON(t, app, "GET", "/api/v1/tournaments/1/workspace", nil, 200), &workspace); err != nil {
		t.Fatal(err)
	}
	if len(workspace.Leagues) != 2 || workspace.Leagues[1].InspectionsRequired() {
		t.Fatal("workspace did not expose persisted false flag")
	}
	// Inactive settings can be omitted without erasing the saved limits or clock.
	body := fiber.Map{"name": "Infrared renamed", "abbrev": "ir", "parent_id": 1}
	updated := readLeagueResponse(t, requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/leagues/%d", child.ID), body, 200))
	if updated.InspectionsRequired() || updated.RobotCount != 3 || updated.MaxWeightG != 1400 || updated.BallZoneCM != 1.5 || updated.Voltage != 24 || updated.SizeCM != 22 || updated.MotorLimit != 4 || updated.HalfSeconds != 720 || updated.HalftimeSeconds != 180 {
		t.Fatalf("omitted inactive settings were reset: %+v", updated)
	}
	body["inspections_enabled"] = true
	updated = readLeagueResponse(t, requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/leagues/%d", child.ID), body, 200))
	if !updated.InspectionsRequired() || updated.MaxWeightG != 1400 || updated.HalfSeconds != 720 {
		t.Fatal("re-enabling inspections did not restore saved rules")
	}
}
func TestOmittedInspectionFlagDefaultsToEnabledAndInactiveLimitsAreOptional(t *testing.T) {
	if !(domain.League{}).InspectionsRequired() {
		t.Fatal("old in-memory fixtures must require inspections")
	}
	db, app := integrityFixture(t)
	enabled := readLeagueResponse(t, requestJSON(t, app, "POST", "/api/v1/tournaments/1/leagues", fiber.Map{"name": "Default", "abbrev": "default", "parent_id": 1}, 201))
	if enabled.InspectionsEnabled == nil || !enabled.InspectionsRequired() {
		t.Fatal("omitted create flag did not default to enabled")
	}
	var reloaded domain.League
	if err := db.First(&reloaded, enabled.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reloaded.InspectionsRequired() {
		t.Fatal("default inspection flag was not persisted")
	}
	disabled := readLeagueResponse(t, requestJSON(t, app, "POST", "/api/v1/tournaments/1/leagues", fiber.Map{"name": "No inspection", "abbrev": "no-checks", "parent_id": 1, "inspections_enabled": false}, 201))
	if disabled.InspectionsRequired() {
		t.Fatal("inactive inspection limits should be optional")
	}
}
func TestMainInspectionFlagDoesNotCascadeAndStartedChildCannotToggle(t *testing.T) {
	db, app := integrityFixture(t)
	disabled := readLeagueResponse(t, requestJSON(t, app, "POST", "/api/v1/tournaments/1/leagues", fiber.Map{"name": "No checks", "abbrev": "none", "parent_id": 1, "inspections_enabled": false}, 201))
	enabled := readLeagueResponse(t, requestJSON(t, app, "POST", "/api/v1/tournaments/1/leagues", fiber.Map{"name": "With checks", "abbrev": "checks", "parent_id": 1, "inspections_enabled": true}, 201))
	var parent domain.League
	db.First(&parent, 1)
	off := false
	parent.InspectionsEnabled = &off
	requestJSON(t, app, "PUT", "/api/v1/leagues/1", parent, 200)
	db.First(&enabled, enabled.ID)
	db.First(&disabled, disabled.ID)
	if !enabled.InspectionsRequired() || disabled.InspectionsRequired() {
		t.Fatal("main inspection flag overrode independent child settings")
	}
	match := integrityMatch(1, 1, 2, "in_progress")
	match.LeagueID = disabled.ID
	saveIntegrity(t, db, &match)
	on := true
	disabled.InspectionsEnabled = &on
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/leagues/%d", disabled.ID), disabled, 400)
	db.First(&disabled, disabled.ID)
	if disabled.InspectionsRequired() {
		t.Fatal("started match inspection requirements changed")
	}
	// A main league's independent inspection flag does not govern its children.
	parent.InspectionsEnabled = &on
	requestJSON(t, app, "PUT", "/api/v1/leagues/1", parent, 200)
	db.First(&disabled, disabled.ID)
	if disabled.InspectionsRequired() {
		t.Fatal("main toggle changed a started child's inspection setting")
	}
}
func TestInspectionMigrationEnablesExistingLeagueRows(t *testing.T) {
	db := integrationDB(t)
	// Reconstruct the previous schema, then use the same migration list as startup.
	if err := db.Migrator().DropColumn(&domain.League{}, "inspections_enabled"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO leagues (id, tournament_id, name, abbrev, mode, referee_mode) VALUES (1, 1, 'Legacy', 'legacy', 'full', true)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(domain.Models()...); err != nil {
		t.Fatal(err)
	}
	var legacy domain.League
	if err := db.First(&legacy, 1).Error; err != nil {
		t.Fatal(err)
	}
	if legacy.InspectionsEnabled == nil || !legacy.InspectionsRequired() {
		t.Fatal("migration disabled inspections for an existing league")
	}
}

func TestInspectionToggleResetsOnlyExactLeaguePreparedState(t *testing.T) {
	db, app := integrityFixture(t)
	var parent domain.League
	db.First(&parent, 1)
	parent.RefereeMode = true
	parent = readLeagueResponse(t, requestJSON(t, app, "PUT", "/api/v1/leagues/1", parent, 200))
	child := readLeagueResponse(t, requestJSON(t, app, "POST", "/api/v1/tournaments/1/leagues", fiber.Map{"name": "Infrared", "abbrev": "ir", "parent_id": 1, "inspections_enabled": false}, 201))
	start := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	fieldID, slotID := uint(1), uint(1)
	prepared := json.RawMessage(`{"phase":"ready","blue_team_id":1,"kickoff_team_id":2,"checks":[{"team_id":1,"robot":1,"weight_g":1400}],"remaining_seconds":600}`)
	scheduled := integrityMatch(1, 1, 2, "scheduled")
	scheduled.LeagueID = child.ID
	scheduled.FieldID = &fieldID
	scheduled.SlotID = &slotID
	scheduled.ScheduledAt = &start
	scheduled.RefereeIDs = []uint{12, 13}
	scheduled.Published = true
	scheduled.Goals1 = 2
	scheduled.Goals2 = 1
	scheduled.Notes = "Preserve metadata"
	scheduled.LiveState = prepared
	scheduled.UpdatedAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	draft := integrityMatch(2, 3, 4, "draft")
	draft.LeagueID = child.ID
	draft.LiveState = prepared
	draft.UpdatedAt = scheduled.UpdatedAt
	mainMatch := integrityMatch(3, 1, 2, "draft")
	mainMatch.LiveState = prepared
	saveIntegrity(t, db, &scheduled, &draft, &mainMatch)
	before := []domain.Match{scheduled, draft}
	for i := range before {
		if err := db.First(&before[i], before[i].ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	assertOnlyStateReset := func() {
		t.Helper()
		for _, old := range before {
			var got domain.Match
			if err := db.First(&got, old.ID).Error; err != nil {
				t.Fatal(err)
			}
			if string(got.LiveState) != "{}" {
				t.Fatal("prepared match state was retained after inspection toggle")
			}
			state, err := readLiveState(got.LiveState)
			if err != nil || state.Phase != "pregame" {
				t.Fatal("reset match did not return to pregame")
			}
			if !got.UpdatedAt.After(old.UpdatedAt) {
				t.Fatal("state reset did not update the match timestamp")
			}
			want := old
			want.LiveState = json.RawMessage("{}")
			want.UpdatedAt = got.UpdatedAt
			wantJSON, _ := json.Marshal(want)
			gotJSON, _ := json.Marshal(got)
			if string(wantJSON) != string(gotJSON) {
				t.Fatal("inspection toggle changed scheduling, assignments, scores or other match metadata")
			}
		}
	}
	on := true
	child.InspectionsEnabled = &on
	child = readLeagueResponse(t, requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/leagues/%d", child.ID), child, 200))
	assertOnlyStateReset()
	var untouched domain.Match
	if err := db.First(&untouched, mainMatch.ID).Error; err != nil {
		t.Fatal(err)
	}
	if string(untouched.LiveState) != string(prepared) {
		t.Fatal("child inspection toggle reset another league's state")
	}
	// Disabling inspections must also discard preparations made under old rules.
	for _, old := range before {
		if err := db.Model(&domain.Match{}).Where("id = ?", old.ID).Updates(map[string]any{"live_state": prepared, "updated_at": old.UpdatedAt}).Error; err != nil {
			t.Fatal(err)
		}
	}
	off := false
	child.InspectionsEnabled = &off
	child = readLeagueResponse(t, requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/leagues/%d", child.ID), child, 200))
	assertOnlyStateReset()
	// The main league's hidden inspection flag is independent of child settings.
	if err := db.Model(&domain.Match{}).Where("id = ?", scheduled.ID).UpdateColumn("live_state", prepared).Error; err != nil {
		t.Fatal(err)
	}
	parent.InspectionsEnabled = &off
	requestJSON(t, app, "PUT", "/api/v1/leagues/1", parent, 200)
	untouched = domain.Match{}
	if err := db.First(&untouched, scheduled.ID).Error; err != nil {
		t.Fatal(err)
	}
	if string(untouched.LiveState) != string(prepared) {
		t.Fatal("main inspection toggle reset child state")
	}
}
