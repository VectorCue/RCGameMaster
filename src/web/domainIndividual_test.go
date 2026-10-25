package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func setAutomaticLeagueMode(t *testing.T, app *fiber.App, league domain.League, mode string) domain.League {
	t.Helper()
	league.Mode = mode
	return readLeagueResponse(t, requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/leagues/%d", league.ID), league, 200))
}
func TestPairedStagePreparationAndPopulationWorkInBothLeagueModes(t *testing.T) {
	for _, mode := range []string{"full", "scheduling"} {
		for _, kind := range []string{"round_robin", "swiss", "knockout"} {
			t.Run(mode+"/"+kind, func(t *testing.T) {
				db, app, tid, lid := automaticFixture(t)
				var league domain.League
				db.First(&league, lid)
				league = setAutomaticLeagueMode(t, app, league, mode)
				if mode == "scheduling" && league.RefereeMode {
					t.Fatal("scheduling mode retained referee control")
				}
				registerAutomaticTeams(t, app, tid, lid, 4)
				sid := automaticStageID(t, app, tid, lid, kind, kind, 4)
				want := 2
				if kind == "round_robin" {
					want = 6
				}
				empty := stageRows(t, db, sid)
				if len(empty) != want {
					t.Fatal("automatic preparation depended on league mode")
				}
				preview := generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/preview", sid), fiber.Map{}, 200))
				draw := generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", sid), fiber.Map{"preview_token": preview.PreviewToken}, 200))
				if len(draw.Matches) != want || len(stageRows(t, db, sid)) != want {
					t.Fatal("automatic population duplicated or omitted matches")
				}
				for _, match := range draw.Matches {
					if match.IsPlaceholder || match.Team1ID == 0 || match.Team2ID == nil || match.IsBye {
						t.Fatal("pairing was not assigned to prepared match")
					}
				}
			})
		}
	}
}
func TestIndividualRunsReserveOneSlotPerTeamForZeroOneAndOddRosters(t *testing.T) {
	for _, mode := range []string{"full", "scheduling"} {
		for _, n := range []int{0, 1, 5} {
			t.Run(fmt.Sprintf("%s/%d", mode, n), func(t *testing.T) {
				db, app, tid, lid := automaticFixture(t)
				var league domain.League
				db.First(&league, lid)
				setAutomaticLeagueMode(t, app, league, mode)
				registerAutomaticTeams(t, app, tid, lid, n)
				sid := automaticStageID(t, app, tid, lid, "Runs", "individual", 0)
				rows := stageRows(t, db, sid)
				if len(rows) != n {
					t.Fatalf("got %d runs for %d teams", len(rows), n)
				}
				if n == 0 {
					requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/preview", sid), fiber.Map{}, 409)
					return
				}
				preview := generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/preview", sid), fiber.Map{}, 200))
				if len(preview.Matches) != n {
					t.Fatal("wrong individual preview count")
				}
				for _, match := range preview.Matches {
					if match.ID == 0 || match.IsBye || match.Team2ID != nil || match.Round != 1 || match.Status != "draft" || match.Published {
						t.Fatal("individual preview created a bye, opponent, result or later round")
					}
				}
				draw := generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", sid), fiber.Map{"preview_token": preview.PreviewToken}, 200))
				seen := map[uint]bool{}
				for _, match := range draw.Matches {
					if match.Team1ID == 0 || match.Team2ID != nil || match.IsBye || match.Published || match.Status != "draft" || seen[match.Team1ID] {
						t.Fatal("run was auto-approved, duplicated or mislabeled as bye")
					}
					seen[match.Team1ID] = true
				}
				requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", sid), fiber.Map{}, 409)
				// Full referee-enabled leagues also let editors complete one-team runs directly.
				requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/review", draw.Matches[0].ID), fiber.Map{"approve": true}, 200)
				response := requestJSON(t, app, "GET", "/rest/v1/AUTO/matches", nil, 200)
				var published struct {
					Matches []struct {
						Team2   *domain.Team `json:"team2"`
						Points1 int          `json:"points1"`
						Points2 int          `json:"points2"`
					} `json:"matches"`
				}
				if err := json.Unmarshal(response, &published); err != nil {
					t.Fatal(err)
				}
				if len(published.Matches) != 1 || published.Matches[0].Team2 != nil || published.Matches[0].Points1 != 0 || published.Matches[0].Points2 != 0 || strings.Contains(string(response), "BYE") {
					t.Fatal("completed run published a fake opponent or match points")
				}
				w, err := loadWorkspace(db, tid, false)
				if err != nil {
					t.Fatal(err)
				}
				for _, standing := range w.Standings {
					if standing.StageID == sid && (standing.Points != 0 || standing.Wins != 0 || standing.Draws != 0 || standing.Losses != 0) {
						t.Fatal("completed individual run affected head-to-head standings")
					}
				}
			})
		}
	}
}
func TestIndividualExpectedOneQualificationCapAndRegistrationOrder(t *testing.T) {
	db, app, tid, lid := automaticFixture(t)
	sid := automaticStageID(t, app, tid, lid, "Single expected run", "individual", 1)
	if len(stageRows(t, db, sid)) != 1 {
		t.Fatal("expected one did not reserve a run before registration")
	}
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/preview", sid), fiber.Map{}, 409)
	registerAutomaticTeams(t, app, tid, lid, 5)
	for id := 1; id <= 5; id++ {
		db.Model(&domain.Team{}).Where("id = ?", id).Update("startnumber", 6-id)
	}
	preview := generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/preview", sid), fiber.Map{}, 200))
	if len(preview.Matches) != 1 || preview.Matches[0].Team1ID != 5 {
		t.Fatal("individual runs were not assigned in registration order")
	}
	capID := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/stages", tid), fiber.Map{"name": "Capped run", "type": "individual", "league_id": lid, "expected_teams": 5, "team_limit": 1})
	if len(stageRows(t, db, capID)) != 1 {
		t.Fatal("individual qualification cap one was rejected or ignored")
	}
	draw := generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", capID), fiber.Map{}, 200))
	if len(draw.Matches) != 1 || draw.Matches[0].Team1ID != 5 || draw.Matches[0].IsBye {
		t.Fatal("qualification cap did not constrain assignment")
	}
	for _, kind := range []string{"round_robin", "swiss", "knockout"} {
		requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/tournaments/%d/stages", tid), fiber.Map{"name": kind, "type": kind, "league_id": lid, "expected_teams": 1}, 400)
		requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/tournaments/%d/stages", tid), fiber.Map{"name": kind, "type": kind, "league_id": lid, "team_limit": 1}, 400)
	}
}
func TestIndividualBookingPopulationConflictsAndClearPreserveIdentity(t *testing.T) {
	db, app, tid, lid := automaticFixture(t)
	sid := automaticStageID(t, app, tid, lid, "Runs", "individual", 3)
	rows := stageRows(t, db, sid)
	reserved := rows[0]
	// Individual booking length is independent from the soccer half clock.
	reserved.DurationMinutes = 5
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/matches/%d", reserved.ID), reserved, 200)
	fieldA := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/fields", tid), fiber.Map{"league_id": lid, "name": "A"})
	fieldB := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/fields", tid), fiber.Map{"league_id": lid, "name": "B"})
	slot := func(field uint, start, end string) uint {
		return createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/slots", tid), fiber.Map{"field_id": field, "starts_at": start, "ends_at": end})
	}
	a := slot(fieldA, "2026-10-25T09:00:00Z", "2026-10-25T09:10:00Z")
	b := slot(fieldB, "2026-10-25T09:00:00Z", "2026-10-25T09:30:00Z")
	late := slot(fieldB, "2026-10-25T09:30:00Z", "2026-10-25T10:00:00Z")
	ref := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/referees", tid), fiber.Map{"name": "Ref"})
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", reserved.ID), fiber.Map{"slot_id": a, "referee_ids": []uint{ref}}, 200)
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/publish", reserved.ID), fiber.Map{"published": true}, 200)
	registerAutomaticTeams(t, app, tid, lid, 3)
	manual := automaticStageID(t, app, tid, lid, "Other game", "manual", 0)
	other := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/matches", tid), fiber.Map{"league_id": lid, "stage_id": manual, "team1_id": 1, "team2_id": 2, "duration_minutes": 25, "round": 1})
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", other), fiber.Map{"slot_id": b, "referee_ids": []uint{}}, 200)
	before := stageRows(t, db, sid)
	preview := generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/preview", sid), fiber.Map{}, 200))
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", sid), fiber.Map{"preview_token": preview.PreviewToken}, 409)
	assertJSONUnchanged(t, before, stageRows(t, db, sid))
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", other), fiber.Map{"slot_id": late, "referee_ids": []uint{}}, 200)
	preview = generationResponse(t, requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/preview", sid), fiber.Map{}, 200))
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", sid), fiber.Map{"preview_token": preview.PreviewToken}, 200)
	var populated domain.Match
	db.First(&populated, reserved.ID)
	if populated.IsPlaceholder || populated.IsBye || populated.Team2ID != nil || populated.Team1ID != 1 || populated.SlotID == nil || *populated.SlotID != a || populated.DurationMinutes != 5 || !populated.Published || len(populated.RefereeIDs) != 1 || populated.RefereeIDs[0] != ref {
		t.Fatal("individual assignment changed booking identity or invented a bye")
	}
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/control", reserved.ID), fiber.Map{"action": "start"}, 409)
	var league domain.League
	db.First(&league, lid)
	league.Name = "Renamed soccer"
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/leagues/%d", lid), league, 200)
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/clear", sid), fiber.Map{}, 200)
	var empty domain.Match
	db.First(&empty, reserved.ID)
	if !empty.IsPlaceholder || empty.IsBye || empty.SlotID == nil || *empty.SlotID != a || !empty.Published || empty.DurationMinutes != 5 {
		t.Fatal("individual clear did not retain scheduled reservation")
	}
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", sid), fiber.Map{}, 200)
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/review", reserved.ID), fiber.Map{"approve": true}, 200)
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/clear", sid), fiber.Map{}, 409)
}
func TestIndividualManualInputsCannotAssignOpponentDuplicateOrLaterRound(t *testing.T) {
	db, app, tid, lid := automaticFixture(t)
	registerAutomaticTeams(t, app, tid, lid, 2)
	sid := automaticStageID(t, app, tid, lid, "Runs", "individual", 2)
	before := stageRows(t, db, sid)
	for _, matches := range [][]fiber.Map{{{"team1_id": 1, "team2_id": 2, "round": 1}}, {{"team1_id": 1, "team2_id": nil, "round": 2}}, {{"team1_id": 1, "team2_id": nil, "round": 1}, {"team1_id": 1, "team2_id": nil, "round": 1}}} {
		requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/matches", sid), fiber.Map{"matches": matches}, 400)
		assertJSONUnchanged(t, before, stageRows(t, db, sid))
	}
	// Legacy create clients fill a saved automatic slot rather than adding a third run.
	matchID := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/matches", tid), fiber.Map{"league_id": lid, "stage_id": sid, "team1_id": 1, "round": 1})
	if matchID != before[0].ID || len(stageRows(t, db, sid)) != 2 {
		t.Fatal("generic creation duplicated automatic individual slots")
	}
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/tournaments/%d/matches", tid), fiber.Map{"league_id": lid, "stage_id": sid, "team1_id": 1, "round": 1}, 400)
	for _, role := range []string{"editor", "referee", "viewer"} {
		t.Run(role, func(t *testing.T) {
			restricted := fiber.New()
			api := API{DB: db}
			v := restricted.Group("/api/v1", func(c fiber.Ctx) error { c.Locals("user", User{Roles: []string{role}}); return c.Next() })
			api.registerDomainRoutes(v)
			for _, action := range []string{"prepare", "preview", "generate"} {
				requestJSON(t, restricted, "POST", fmt.Sprintf("/api/v1/stages/%d/%s", sid, action), fiber.Map{}, 403)
			}
		})
	}
}
func TestSchedulingAutomaticBackfillAndModeSwitchRetainReservations(t *testing.T) {
	db, app, tid, lid := automaticFixture(t)
	sid := automaticStageID(t, app, tid, lid, "RR", "round_robin", 4)
	rows := stageRows(t, db, sid)
	field := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/fields", tid), fiber.Map{"league_id": lid, "name": "A"})
	slot := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/slots", tid), fiber.Map{"field_id": field, "starts_at": "2026-10-25T09:00:00Z", "ends_at": "2026-10-25T09:30:00Z"})
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", rows[0].ID), fiber.Map{"slot_id": slot, "referee_ids": []uint{}}, 200)
	before := stageRows(t, db, sid)
	var league domain.League
	db.First(&league, lid)
	setAutomaticLeagueMode(t, app, league, "scheduling")
	assertJSONUnchanged(t, before, stageRows(t, db, sid))
	legacy := domain.Stage{TournamentID: tid, LeagueID: lid, Name: "Existing runs", Type: "individual", ExpectedTeams: 1}
	saveIntegrity(t, db, &legacy)
	backfillAutomaticStages(db)
	if len(stageRows(t, db, legacy.ID)) != 1 {
		t.Fatal("startup excluded scheduling-only automatic stage")
	}
}
func TestOtherStageEmptyReservationsDoNotBlockLegacySwissNextRound(t *testing.T) {
	db, app := integrityFixture(t)
	source := domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Legacy Swiss", Type: "swiss"}
	later := domain.Stage{ID: 2, TournamentID: 1, LeagueID: 1, Name: "Future", Type: "knockout", SortOrder: 1}
	saveIntegrity(t, db, &source, &later)
	requestJSON(t, app, "POST", "/api/v1/stages/1/generate", fiber.Map{"round": 1}, 200)
	db.Model(&domain.Match{}).Where("stage_id = ?", 1).Update("status", "approved")
	future := domain.Match{IsPlaceholder: true, TournamentID: 1, LeagueID: 1, StageID: 2, Number: 4, Round: 1, DurationMinutes: 25, Status: "draft", RefereeIDs: []uint{}, LiveState: []byte("{}")}
	saveIntegrity(t, db, &future)
	requestJSON(t, app, "POST", "/api/v1/stages/1/preview", fiber.Map{"round": 2}, 200)
}

func TestPreparedAutomaticMatchCannotMoveAcrossStageReservations(t *testing.T) {
	db, app, tid, lid := automaticFixture(t)
	registerAutomaticTeams(t, app, tid, lid, 1)
	a := automaticStageID(t, app, tid, lid, "Run A", "individual", 1)
	b := automaticStageID(t, app, tid, lid, "Run B", "individual", 1)
	field := createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/fields", tid), fiber.Map{"league_id": lid, "name": "A"})
	slots := []uint{}
	for _, window := range [][2]string{{"2026-10-25T09:00:00Z", "2026-10-25T09:30:00Z"}, {"2026-10-25T09:30:00Z", "2026-10-25T10:00:00Z"}} {
		slots = append(slots, createdID(t, app, fmt.Sprintf("/api/v1/tournaments/%d/slots", tid), fiber.Map{"field_id": field, "starts_at": window[0], "ends_at": window[1]}))
	}
	for i, sid := range []uint{a, b} {
		rows := stageRows(t, db, sid)
		requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", rows[0].ID), fiber.Map{"slot_id": slots[i], "referee_ids": []uint{}}, 200)
		requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/publish", rows[0].ID), fiber.Map{"published": true}, 200)
	}
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/%d/generate", a), fiber.Map{}, 200)
	beforeA, beforeB := stageRows(t, db, a), stageRows(t, db, b)
	moving := beforeA[0]
	moving.StageID = b
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/matches/%d", moving.ID), moving, 409)
	assertJSONUnchanged(t, beforeA, stageRows(t, db, a))
	assertJSONUnchanged(t, beforeB, stageRows(t, db, b))
}
