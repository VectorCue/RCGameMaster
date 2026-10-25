package web

import (
	"RCJVGameMaster/src/domain"
	"fmt"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestHierarchyAcceptsMinimalMainAndConfiguredChild(t *testing.T) {
	db := integrationDB(t)
	app, err := newApp(localTestConfig(), db)
	if err != nil {
		t.Fatal(err)
	}
	tournament := createdID(t, app, "/api/v1/tournaments", fiber.Map{"name": "Cup", "abbrev": "CUP", "start_date": "2026-10-25", "end_date": "2026-10-25", "timezone": "UTC"})
	path := fmt.Sprintf("/api/v1/tournaments/%d/leagues", tournament)
	// Main-league creation has no robot inspection or clock inputs in the UI.
	main := readLeagueResponse(t, requestJSON(t, app, "POST", path, fiber.Map{"name": "Soccer", "abbrev": "soccer", "mode": "full", "referee_mode": true, "parent_id": nil}, 201))
	if main.ParentID != nil || main.Mode != "full" || !main.RefereeMode {
		t.Fatalf("minimal main-league creation failed: %+v", main)
	}
	child := readLeagueResponse(t, requestJSON(t, app, "POST", path, fiber.Map{"name": "Infrared", "abbrev": "ir", "parent_id": main.ID, "robot_count": 1, "max_weight_g": 1500, "ball_zone_cm": 1.5, "voltage": 48, "size_cm": 22, "motor_limit": 4, "half_seconds": 420, "halftime_seconds": 180}, 201))
	if child.ParentID == nil || *child.ParentID != main.ID || child.Mode != "full" || !child.RefereeMode {
		t.Fatal("child failed to inherit main-league operation controls")
	}
	if child.RobotCount != 1 || child.MaxWeightG != 1500 || child.BallZoneCM != 1.5 || child.Voltage != 48 || child.SizeCM != 22 || child.MotorLimit != 4 || child.HalfSeconds != 420 || child.HalftimeSeconds != 180 {
		t.Fatal("child inspection and timer settings were not retained")
	}
}

func TestHierarchyRejectsMissingCrossTournamentAndNestedParents(t *testing.T) {
	db := integrationDB(t)
	app, err := newApp(localTestConfig(), db)
	if err != nil {
		t.Fatal(err)
	}
	tournament := createdID(t, app, "/api/v1/tournaments", fiber.Map{"name": "Cup", "abbrev": "CUP", "start_date": "2026-10-25", "end_date": "2026-10-25", "timezone": "UTC"})
	path := fmt.Sprintf("/api/v1/tournaments/%d/leagues", tournament)
	// A child cannot be persisted before its intended main league exists.
	requestJSON(t, app, "POST", path, fiber.Map{"name": "Infrared", "abbrev": "ir", "parent_id": 999}, 400)
	var count int64
	if err := db.Model(&domain.League{}).Where("tournament_id = ?", tournament).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("invalid child creation left a record behind")
	}
	otherTournament := createdID(t, app, "/api/v1/tournaments", fiber.Map{"name": "Other cup", "abbrev": "OTHER", "start_date": "2026-10-25", "end_date": "2026-10-25", "timezone": "UTC"})
	otherMain := readLeagueResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/tournaments/%d/leagues", otherTournament), fiber.Map{"name": "Other soccer", "abbrev": "soccer", "mode": "full", "parent_id": nil}, 201))
	requestJSON(t, app, "POST", path, fiber.Map{"name": "Cross-event child", "abbrev": "cross", "parent_id": otherMain.ID}, 400)
	main := readLeagueResponse(t, requestJSON(t, app, "POST", path, fiber.Map{"name": "Soccer", "abbrev": "soccer", "mode": "full", "parent_id": nil}, 201))
	child := readLeagueResponse(t, requestJSON(t, app, "POST", path, fiber.Map{"name": "Infrared", "abbrev": "ir", "parent_id": main.ID}, 201))
	requestJSON(t, app, "POST", path, fiber.Map{"name": "Nested child", "abbrev": "nested", "parent_id": child.ID}, 400)
	if err := db.Model(&domain.League{}).Where("tournament_id = ?", tournament).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal("rejected cross-event or nested creation left extra records behind")
	}
}
