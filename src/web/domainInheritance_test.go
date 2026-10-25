package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"fmt"
	"github.com/gofiber/fiber/v3"
	"testing"
	"time"
)

func readLeagueResponse(t *testing.T, raw []byte) domain.League {
	t.Helper()
	var league domain.League
	if e := json.Unmarshal(raw, &league); e != nil {
		t.Fatal(e)
	}
	return league
}
func TestMainLeagueControlsAreInheritedAndGuardStartedChildren(t *testing.T) {
	db, app := integrityFixture(t)
	child := readLeagueResponse(t, requestJSON(t, app, "POST", "/api/v1/tournaments/1/leagues", fiber.Map{"name": "Infrared", "abbrev": "ir", "parent_id": 1, "mode": "scheduling", "referee_mode": true, "robot_count": 3, "max_weight_g": 1400, "half_seconds": 300, "halftime_seconds": 120}, 201))
	if child.Mode != "full" || child.RefereeMode {
		t.Fatal("child overrode main league controls")
	}
	var parent domain.League
	db.First(&parent, 1)
	parent.RefereeMode = true
	parent = readLeagueResponse(t, requestJSON(t, app, "PUT", "/api/v1/leagues/1", parent, 200))
	db.First(&child, child.ID)
	if child.Mode != "full" || !child.RefereeMode || child.RobotCount != 3 || child.MaxWeightG != 1400 || child.HalfSeconds != 300 || child.HalftimeSeconds != 120 {
		t.Fatalf("cascade altered child-specific rules: %+v", child)
	}
	var workspace domain.Workspace
	json.Unmarshal(requestJSON(t, app, "GET", "/api/v1/tournaments/1/workspace", nil, 200), &workspace)
	if len(workspace.Leagues) != 2 || !workspace.Leagues[1].RefereeMode {
		t.Fatal("workspace did not reflect persisted inheritance")
	}
	match := integrityMatch(1, 1, 2, "in_progress")
	match.LeagueID = child.ID
	saveIntegrity(t, db, &match)
	parent.Mode = "scheduling"
	requestJSON(t, app, "PUT", "/api/v1/leagues/1", parent, 400)
	db.First(&parent, 1)
	db.First(&child, child.ID)
	if parent.Mode != "full" || !parent.RefereeMode || child.Mode != "full" || !child.RefereeMode {
		t.Fatal("rejected parent edit was not atomic")
	}
	// Submitted child flags are ignored in favor of the unchanged parent controls.
	child.Mode = "scheduling"
	child.RefereeMode = false
	child = readLeagueResponse(t, requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/leagues/%d", child.ID), child, 200))
	if child.Mode != "full" || !child.RefereeMode {
		t.Fatal("direct child update escaped inherited controls")
	}
	// Main inspection settings do not overwrite or depend on child inspection settings.
	parent.RobotCount = 4
	parent.HalfSeconds = 900
	requestJSON(t, app, "PUT", "/api/v1/leagues/1", parent, 200)
	db.First(&child, child.ID)
	if child.RobotCount != 3 || child.HalfSeconds != 300 {
		t.Fatal("parent update overwrote child robot or clock settings")
	}
	child.RobotCount = 4
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/leagues/%d", child.ID), child, 400)
}
func TestSchedulingModeDisablesRefereeForMainAndChildren(t *testing.T) {
	_, app := integrityFixture(t)
	main := readLeagueResponse(t, requestJSON(t, app, "POST", "/api/v1/tournaments/1/leagues", fiber.Map{"name": "Rescue", "abbrev": "rescue", "mode": "scheduling", "referee_mode": true}, 201))
	if main.RefereeMode {
		t.Fatal("scheduling-only league enabled referee mode")
	}
	child := readLeagueResponse(t, requestJSON(t, app, "POST", "/api/v1/tournaments/1/leagues", fiber.Map{"name": "Line", "abbrev": "line", "parent_id": main.ID, "mode": "full", "referee_mode": true}, 201))
	if child.Mode != "scheduling" || child.RefereeMode {
		t.Fatal("scheduling child failed to inherit disabled referee mode")
	}
}
func TestEnablingParentRefereeRollsBackWhenChildScheduleIsTooShort(t *testing.T) {
	db, app := integrityFixture(t)
	child := readLeagueResponse(t, requestJSON(t, app, "POST", "/api/v1/tournaments/1/leagues", fiber.Map{"name": "Infrared", "abbrev": "ir", "parent_id": 1, "half_seconds": 600, "halftime_seconds": 300}, 201))
	start := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	m := integrityMatch(1, 1, 2, "scheduled")
	m.LeagueID = child.ID
	m.DurationMinutes = 10
	m.ScheduledAt = &start
	saveIntegrity(t, db, &m)
	var parent domain.League
	db.First(&parent, 1)
	parent.RefereeMode = true
	requestJSON(t, app, "PUT", "/api/v1/leagues/1", parent, 400)
	db.First(&parent, 1)
	db.First(&child, child.ID)
	if parent.RefereeMode || child.RefereeMode {
		t.Fatal("partially applied inherited referee controls despite invalid child schedule")
	}
}
