package web

import (
	"RCJVGameMaster/src/domain"
	"fmt"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func automaticFixture(t *testing.T) (*gorm.DB, *fiber.App, uint, uint) {
	t.Helper()
	db := integrationDB(t)
	app, err := newApp(localTestConfig(), db)
	if err != nil {
		t.Fatal(err)
	}
	tid := createdID(t, app, "/api/v1/tournaments", fiber.Map{"name": "Automatic cup", "abbrev": "AUTO", "start_date": "2026-10-25", "end_date": "2026-10-25", "timezone": "UTC"})
	lid := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/leagues", tid), fiber.Map{"name": "Soccer", "abbrev": "soccer", "mode": "full", "referee_mode": true, "inspections_enabled": false, "robot_count": 1})
	return db, app, tid, lid
}
func automaticStageID(t *testing.T, app *fiber.App, tid, lid uint, name, kind string, n int) uint {
	t.Helper()
	return createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/stages", tid), fiber.Map{"league_id": lid, "name": name, "type": kind, "expected_teams": n})
}
func stageRows(t *testing.T, db *gorm.DB, id uint) []domain.Match {
	t.Helper()
	rows := []domain.Match{}
	if err := db.Where("stage_id = ?", id).Order("round,number,id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}
func registerAutomaticTeams(t *testing.T, app *fiber.App, tid, lid uint, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/teams", tid), fiber.Map{"name": fmt.Sprintf("Team %d", i), "league_id": lid, "startnumber": i})
	}
}
func TestAutomaticStagesImmediatelyReserveOnlyPlayableEmptyGames(t *testing.T) {
	db, app, tid, lid := automaticFixture(t)
	for _, tt := range []struct {
		kind    string
		n, want int
	}{{"round_robin", 4, 6}, {"round_robin", 5, 10}, {"swiss", 5, 2}, {"knockout", 5, 1}, {"manual", 4, 0}} {
		t.Run(fmt.Sprintf("%s%d", tt.kind, tt.n), func(t *testing.T) {
			id := automaticStageID(t, app, tid, lid, tt.kind, tt.kind, tt.n)
			rows := stageRows(t, db, id)
			if len(rows) != tt.want {
				t.Fatalf("got %d reserved games, want %d", len(rows), tt.want)
			}
			for _, match := range rows {
				if !match.IsPlaceholder || match.IsBye || match.Team1ID != 0 || match.Team2ID != nil || match.Status != "draft" || match.Published {
					t.Fatal("reserved game already contains a pairing or result")
				}
				if tt.kind != "round_robin" && match.Round != 1 {
					t.Fatal("Swiss/KO reserved more than one round")
				}
			}
			var stage domain.Stage
			db.First(&stage, id)
			if stage.Round != 0 {
				t.Fatal("empty reservations advanced populated round")
			}
		})
	}
	scheduling := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/leagues", tid), fiber.Map{"name": "Rescue", "abbrev": "rescue", "mode": "scheduling"})
	id := automaticStageID(t, app, tid, scheduling, "Rescue runs", "round_robin", 4)
	if len(stageRows(t, db, id)) != 6 {
		t.Fatal("scheduling-only league did not create automatic matches")
	}
	rr := automaticStageID(t, app, tid, lid, "Prepared RR", "round_robin", 4)
	before := stageRows(t, db, rr)
	for i := 0; i < 2; i++ {
		requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/prepare", rr), fiber.Map{}, 200)
	}
	assertJSONUnchanged(t, before, stageRows(t, db, rr))
	requestJSON(t, app, "DELETE", fmt.Sprintf("/api/v1/matches/%d", before[0].ID), nil, 409)
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/control", before[0].ID), fiber.Map{"action": "start"}, 409)
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/review", before[0].ID), fiber.Map{"approve": true}, 409)
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/tournaments/%d/matches", tid), fiber.Map{"league_id": lid, "stage_id": rr, "team1_id": 0, "is_placeholder": true}, 400)
}
func TestPlaceholderBookingsPopulateAtomicallyAndClearRetainsReservations(t *testing.T) {
	db, app, tid, lid := automaticFixture(t)
	sid := automaticStageID(t, app, tid, lid, "RR", "round_robin", 4)
	rows := stageRows(t, db, sid)
	first, second := rows[0], rows[2]
	fieldA := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/fields", tid), fiber.Map{"league_id": lid, "name": "A"})
	fieldB := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/fields", tid), fiber.Map{"league_id": lid, "name": "B"})
	slot := func(field uint, start, end string) uint {
		return createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/slots", tid), fiber.Map{"field_id": field, "starts_at": start, "ends_at": end})
	}
	a := slot(fieldA, "2026-10-25T09:00:00Z", "2026-10-25T09:30:00Z")
	b := slot(fieldB, "2026-10-25T09:00:00Z", "2026-10-25T09:30:00Z")
	late := slot(fieldB, "2026-10-25T09:30:00Z", "2026-10-25T10:00:00Z")
	refA := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/referees", tid), fiber.Map{"name": "Ref A"})
	refB := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/referees", tid), fiber.Map{"name": "Ref B"})
	for _, booking := range []struct{ id, slot, ref uint }{{first.ID, a, refA}, {second.ID, b, refB}} {
		requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", booking.id), fiber.Map{"slot_id": booking.slot, "referee_ids": []uint{booking.ref}}, 200)
		requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/publish", booking.id), fiber.Map{"published": true}, 200)
	}
	// Both unknown-team reservations can coexist. Compatibility labels identify empty slots.
	public := requestJSON(t, app, "GET", "/rest/v1/AUTO/matches", nil, 200)
	if !strings.Contains(string(public), "Awaiting teams") || strings.Contains(string(public), "BYE") {
		t.Fatal("published unknown teams were labeled as a bye")
	}
	registerAutomaticTeams(t, app, tid, lid, 4)
	before := stageRows(t, db, sid)
	preview := generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/preview", sid), fiber.Map{}, 200))
	for _, match := range preview.Matches {
		if match.ID == 0 {
			t.Fatal("pairing preview ignored saved empty match IDs")
		}
	}
	// The same team occurs across these two booked RR rounds; committing is atomic.
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", sid), fiber.Map{"preview_token": preview.PreviewToken}, 409)
	assertJSONUnchanged(t, before, stageRows(t, db, sid))
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", second.ID), fiber.Map{"slot_id": late, "referee_ids": []uint{refB}}, 200)
	preview = generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/preview", sid), fiber.Map{}, 200))
	populated := generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", sid), fiber.Map{"preview_token": preview.PreviewToken}, 200))
	if len(populated.Matches) != 6 || len(stageRows(t, db, sid)) != 6 {
		t.Fatal("populating created duplicate matches")
	}
	var saved domain.Match
	db.First(&saved, first.ID)
	if saved.IsPlaceholder || saved.Team1ID == 0 || !saved.Published || saved.SlotID == nil || *saved.SlotID != a || len(saved.RefereeIDs) != 1 || saved.RefereeIDs[0] != refA {
		t.Fatal("populating discarded a reservation")
	}
	bookings := stageRows(t, db, sid)
	cleared := clearedResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/clear", sid), fiber.Map{}, 200))
	if cleared.Cleared != 6 || cleared.Stage.Round != 0 {
		t.Fatal("clear did not reset populated round")
	}
	empty := stageRows(t, db, sid)
	for i, match := range empty {
		if !match.IsPlaceholder || match.Team1ID != 0 || match.Team2ID != nil || match.ID != bookings[i].ID || !equalID(match.SlotID, bookings[i].SlotID) || match.Published != bookings[i].Published {
			t.Fatal("clear deleted a reservation or retained teams")
		}
		assertJSONUnchanged(t, bookings[i].RefereeIDs, match.RefereeIDs)
	}
	preview = generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/preview", sid), fiber.Map{}, 200))
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", sid), fiber.Map{"preview_token": preview.PreviewToken}, 200)
}
func TestAutomaticRosterResizeAndStageConfigGuards(t *testing.T) {
	db, app, tid, lid := automaticFixture(t)
	sid := automaticStageID(t, app, tid, lid, "RR", "round_robin", 0)
	registerAutomaticTeams(t, app, tid, lid, 3)
	if len(stageRows(t, db, sid)) != 3 {
		t.Fatal("roster changes did not prepare automatic slots")
	}
	var stage domain.Stage
	db.First(&stage, sid)
	stage.ExpectedTeams = 4
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/stages/%d", sid), stage, 200)
	original := stageRows(t, db, sid)
	if len(original) != 6 {
		t.Fatal("expected count did not resize empty stage")
	}
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/preview", sid), fiber.Map{}, 409)
	createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/teams", tid), fiber.Map{"name": "Fourth", "league_id": lid, "startnumber": 4})
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", sid), fiber.Map{}, 200)
	createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/teams", tid), fiber.Map{"name": "Fifth", "league_id": lid, "startnumber": 5})
	if len(stageRows(t, db, sid)) != 6 {
		t.Fatal("registration changed an established populated draw")
	}
	stage.ExpectedTeams = 5
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/stages/%d", sid), stage, 400)
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/clear", sid), fiber.Map{}, 200)
	stage.ExpectedTeams = 2
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/stages/%d", sid), stage, 200)
	if len(stageRows(t, db, sid)) != 1 {
		t.Fatal("cleared stage could not be resized")
	}
	stage.Type = "manual"
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/stages/%d", sid), stage, 200)
	if len(stageRows(t, db, sid)) != 0 {
		t.Fatal("manual type change orphaned empty matches")
	}
	stage.Type = "knockout"
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/stages/%d", sid), stage, 200)
	if len(stageRows(t, db, sid)) != 1 {
		t.Fatal("automatic type change did not rebuild empty matches")
	}
	requestJSON(t, app, "DELETE", fmt.Sprintf("/api/v1/stages/%d", sid), nil, 204)
	if len(stageRows(t, db, sid)) != 0 {
		t.Fatal("stage deletion orphaned placeholders")
	}
}
func TestOneRoundPreparedStagesAndOddSwissBye(t *testing.T) {
	for _, kind := range []string{"swiss", "knockout"} {
		t.Run(kind, func(t *testing.T) {
			db, app, tid, lid := automaticFixture(t)
			sid := automaticStageID(t, app, tid, lid, kind, kind, 5)
			registerAutomaticTeams(t, app, tid, lid, 5)
			before := stageRows(t, db, sid)
			draw := generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", sid), fiber.Map{}, 200))
			playable, byes := 0, 0
			for _, match := range draw.Matches {
				if match.IsBye {
					byes++
					if match.SlotID != nil || match.Status != "approved" {
						t.Fatal("bye occupied a reservation")
					}
				} else {
					playable++
				}
			}
			if playable != len(before) || (kind == "swiss" && byes != 1) || (kind == "knockout" && byes != 3) {
				t.Fatal("odd-team draw used wrong number of reserved playable games or byes")
			}
			db.Model(&domain.Match{}).Where("stage_id = ?", sid).Updates(map[string]any{"status": "approved", "published": true})
			requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", sid), fiber.Map{}, 409)
		})
	}
}
func TestPartialSourceReservationsBlockSeedingButDownstreamEmptySlotsDoNot(t *testing.T) {
	for _, explicit := range []bool{true, false} {
		t.Run(fmt.Sprintf("explicit%v", explicit), func(t *testing.T) {
			db, app, tid, lid := automaticFixture(t)
			registerAutomaticTeams(t, app, tid, lid, 4)
			source := automaticStageID(t, app, tid, lid, "Source", "round_robin", 4)
			body := fiber.Map{"league_id": lid, "name": "Final", "type": "knockout", "sort_order": 1, "expected_teams": 2}
			if explicit {
				body["source_stage_id"] = source
			}
			next := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/stages", tid), body)
			// Empty downstream slots do not prevent population of their source.
			requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/matches", source), fiber.Map{"matches": []fiber.Map{{"team1_id": 1, "team2_id": 2, "duration_minutes": 25, "round": 1}}}, 201)
			rows := stageRows(t, db, source)
			for _, m := range rows {
				if !m.IsPlaceholder {
					db.Model(&domain.Match{}).Where("id = ?", m.ID).Updates(map[string]any{"status": "approved", "goals1": 1})
				}
			}
			requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/preview", next), fiber.Map{}, 409)
			// A result correction remains allowed while downstream slots have no teams.
			for _, m := range rows {
				if !m.IsPlaceholder {
					requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/review", m.ID), fiber.Map{"approve": true, "goals1": 2}, 200)
				}
			}
		})
	}
}
func TestPrepareRestoresMissingEmptySlotsAndStartupBackfillIsIdempotent(t *testing.T) {
	db, app, tid, lid := automaticFixture(t)
	sid := automaticStageID(t, app, tid, lid, "Prepared", "round_robin", 4)
	rows := stageRows(t, db, sid)
	db.Delete(&domain.Match{}, rows[0].ID)
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/prepare", sid), fiber.Map{}, 200)
	if len(stageRows(t, db, sid)) != 6 {
		t.Fatal("prepare did not restore missing game")
	}
	legacy := domain.Stage{TournamentID: tid, LeagueID: lid, Name: "Existing empty Swiss", Type: "swiss", ExpectedTeams: 4}
	saveIntegrity(t, db, &legacy)
	if len(stageRows(t, db, legacy.ID)) != 0 {
		t.Fatal("fixture unexpectedly wrote placeholders")
	}
	backfillAutomaticStages(db)
	if len(stageRows(t, db, legacy.ID)) != 2 {
		t.Fatal("startup did not backfill existing empty stage")
	}
	before := stageRows(t, db, legacy.ID)
	backfillAutomaticStages(db)
	assertJSONUnchanged(t, before, stageRows(t, db, legacy.ID))
}

func TestReservedEmptyResizeRollsBackAndExpectedCountHonorsQualificationCap(t *testing.T) {
	db, app, tid, lid := automaticFixture(t)
	sid := automaticStageID(t, app, tid, lid, "RR", "round_robin", 4)
	rows := stageRows(t, db, sid)
	booked := rows[len(rows)-1]
	field := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/fields", tid), fiber.Map{"league_id": lid, "name": "A"})
	slot := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/slots", tid), fiber.Map{"field_id": field, "starts_at": "2026-10-25T09:00:00Z", "ends_at": "2026-10-25T09:30:00Z"})
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", booked.ID), fiber.Map{"slot_id": slot, "referee_ids": []uint{}}, 200)
	before := stageRows(t, db, sid)
	var stage domain.Stage
	if err := db.First(&stage, sid).Error; err != nil {
		t.Fatal(err)
	}
	stage.ExpectedTeams = 2
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/stages/%d", sid), stage, 400)
	assertJSONUnchanged(t, before, stageRows(t, db, sid))
	var unchanged domain.Stage
	db.First(&unchanged, sid)
	if unchanged.ExpectedTeams != 4 || unchanged.PreparedTeams != 4 {
		t.Fatal("failed resize changed stage sizing metadata")
	}
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", booked.ID), fiber.Map{"slot_id": nil, "referee_ids": []uint{}}, 200)
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/stages/%d", sid), stage, 200)
	if len(stageRows(t, db, sid)) != 1 {
		t.Fatal("unassigned excess slots could not be removed")
	}
	stage.ExpectedTeams = 8
	stage.TeamLimit = 4
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/stages/%d", sid), stage, 200)
	registerAutomaticTeams(t, app, tid, lid, 4)
	if len(stageRows(t, db, sid)) != 6 {
		t.Fatal("team limit did not cap the reserved participant count")
	}
	draw := generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", sid), fiber.Map{}, 200))
	if len(draw.Matches) != 6 {
		t.Fatal("team limit did not cap the populated participant pool")
	}
}
func TestPrepareRepairsPartialDrawWithoutExpandingFrozenRoster(t *testing.T) {
	db, app, tid, lid := automaticFixture(t)
	sid := automaticStageID(t, app, tid, lid, "RR", "round_robin", 0)
	registerAutomaticTeams(t, app, tid, lid, 4)
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/matches", sid), fiber.Map{"matches": []fiber.Map{{"team1_id": 1, "team2_id": 2, "duration_minutes": 25, "round": 1}}}, 201)
	rows := stageRows(t, db, sid)
	var lost uint
	for _, m := range rows {
		if m.IsPlaceholder {
			lost = m.ID
			break
		}
	}
	if lost == 0 {
		t.Fatal("fixture has no remaining empty rows")
	}
	if err := db.Delete(&domain.Match{}, lost).Error; err != nil {
		t.Fatal(err)
	}
	createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/teams", tid), fiber.Map{"name": "Late registration", "league_id": lid, "startnumber": 5})
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/prepare", sid), fiber.Map{}, 200)
	if len(stageRows(t, db, sid)) != 6 {
		t.Fatal("repair changed frozen draw size instead of restoring its missing empty match")
	}
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/preview", sid), fiber.Map{}, 409)
}
