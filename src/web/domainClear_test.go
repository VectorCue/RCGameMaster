package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type clearTestResponse struct {
	Cleared int          `json:"cleared"`
	Round   int          `json:"round"`
	Stage   domain.Stage `json:"stage"`
}

func clearedResponse(t *testing.T, raw []byte) clearTestResponse {
	t.Helper()
	var response clearTestResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	return response
}
func assertJSONUnchanged(t *testing.T, before, after any) {
	t.Helper()
	a, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("clear modified records outside its target matches and stage round")
	}
}
func clearMatchCount(t *testing.T, db *gorm.DB, stageID uint) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&domain.Match{}).Where("stage_id = ?", stageID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}
func TestClearDrawFreesPublishedScheduleAndPreservesSetupForRedraw(t *testing.T) {
	db, app := integrityFixture(t)
	stage := domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Round robin", Type: "round_robin", TeamLimit: 4, SortOrder: 1}
	saveIntegrity(t, db, &stage)
	draw := generationResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/generate", fiber.Map{}, 200))
	if len(draw.Matches) != 6 {
		t.Fatal("expected four-team round robin")
	}
	start := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	field := domain.Field{ID: 1, TournamentID: 1, LeagueID: 1, Name: "A"}
	slot := domain.Slot{ID: 1, TournamentID: 1, FieldID: 1, StartsAt: start, EndsAt: start.Add(30 * time.Minute)}
	ref := domain.Referee{ID: 1, TournamentID: 1, Name: "Test referee", UserID: "ref-user"}
	saveIntegrity(t, db, &field, &slot, &ref)
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", draw.Matches[0].ID), fiber.Map{"slot_id": slot.ID, "referee_ids": []uint{ref.ID}}, 200)
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/publish", draw.Matches[0].ID), fiber.Map{"published": true}, 200)
	otherStage := domain.Stage{ID: 2, TournamentID: 1, LeagueID: 1, Name: "Other manual stage", Type: "manual", SortOrder: 2, Round: 8}
	other := integrityMatch(7, 1, 2, "draft")
	other.StageID = 2
	saveIntegrity(t, db, &otherStage, &other)
	otherTournament := domain.Tournament{ID: 2, Name: "Other cup", Abbrev: "OTHER", StartDate: "2026-10-25", EndDate: "2026-10-25", Timezone: "UTC"}
	otherLeague := domain.League{ID: 2, TournamentID: 2, Name: "Other league", Abbrev: "other", Mode: "scheduling"}
	otherTournamentStage := domain.Stage{ID: 3, TournamentID: 2, LeagueID: 2, Name: "Other runs", Type: "manual", Round: 1}
	otherTeam := domain.Team{ID: 6, TournamentID: 2, LeagueID: 2, Name: "Other team"}
	otherTournamentMatch := integrityMatch(1, 6, 0, "draft")
	otherTournamentMatch.TournamentID = 2
	otherTournamentMatch.LeagueID = 2
	otherTournamentMatch.StageID = 3
	saveIntegrity(t, db, &otherTournament, &otherLeague, &otherTournamentStage, &otherTeam, &otherTournamentMatch)
	before, err := loadWorkspace(db, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	otherBefore, err := loadWorkspace(db, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	publicBefore := requestJSON(t, app, "GET", "/api/public/C", nil, 200)
	var published domain.Workspace
	json.Unmarshal(publicBefore, &published)
	if len(published.Matches) != 1 {
		t.Fatal("scheduled match was not public before clearing")
	}
	cleared := clearedResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/clear", fiber.Map{}, 200))
	if cleared.Cleared != 6 || cleared.Round != 0 || cleared.Stage.Round != 0 || cleared.Stage.Name != stage.Name || cleared.Stage.TeamLimit != 4 || clearMatchCount(t, db, 1) != 6 {
		t.Fatal("whole draw was not cleared while preserving its stage settings")
	}
	after, err := loadWorkspace(db, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONUnchanged(t, before.Leagues, after.Leagues)
	assertJSONUnchanged(t, before.Teams, after.Teams)
	assertJSONUnchanged(t, before.Fields, after.Fields)
	assertJSONUnchanged(t, before.Slots, after.Slots)
	assertJSONUnchanged(t, before.Referees, after.Referees)
	var unchangedOther domain.Match
	if err := db.First(&unchangedOther, other.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertJSONUnchanged(t, other, unchangedOther)
	var unchangedStage domain.Stage
	if err := db.First(&unchangedStage, otherStage.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertJSONUnchanged(t, otherStage, unchangedStage)
	otherAfter, err := loadWorkspace(db, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONUnchanged(t, otherBefore, otherAfter)
	json.Unmarshal(requestJSON(t, app, "GET", "/api/public/C", nil, 200), &published)
	if len(published.Matches) != 1 || !published.Matches[0].IsPlaceholder {
		t.Fatal("cleared published booking was not retained as an empty slot")
	}
	empty := clearedResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/clear", fiber.Map{}, 200))
	if empty.Cleared != 0 || empty.Stage.Round != 0 {
		t.Fatal("empty clear is not idempotent")
	}
	preview := generationResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/preview", fiber.Map{}, 200))
	redraw := generationResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/generate", fiber.Map{"round": preview.Round, "preview_token": preview.PreviewToken}, 200))
	if len(redraw.Matches) != 6 || redraw.Matches[0].ID != draw.Matches[0].ID {
		t.Fatal("stage could not be redrawn")
	}
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", redraw.Matches[0].ID), fiber.Map{"slot_id": slot.ID, "referee_ids": []uint{ref.ID}}, 200)
}
func TestClearAutomaticByesAndRejectPlayedDrawWithoutPartialDeletion(t *testing.T) {
	t.Run("automatic byes", func(t *testing.T) {
		db, app := integrityFixture(t)
		saveIntegrity(t, db, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Knockout", Type: "knockout"})
		draw := generationResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/generate", fiber.Map{}, 200))
		byes := 0
		for _, match := range draw.Matches {
			if match.IsBye {
				byes++
			}
		}
		if byes != 3 {
			t.Fatal("expected three automatically approved byes")
		}
		cleared := clearedResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/clear", fiber.Map{}, 200))
		if cleared.Cleared != 4 || clearMatchCount(t, db, 1) != 1 {
			t.Fatal("automatic byes could not be cleared")
		}
		requestJSON(t, app, "POST", "/api/v1/stages/1/preview", fiber.Map{}, 200)
	})
	for _, status := range []string{"in_progress", "submitted", "approved"} {
		t.Run(status, func(t *testing.T) {
			db, app := integrityFixture(t)
			draft := integrityMatch(1, 1, 2, "draft")
			played := integrityMatch(2, 3, 4, status)
			bye := integrityMatch(3, 5, 0, "approved")
			bye.IsBye = true
			stage := domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Manual", Type: "manual", Round: 9}
			saveIntegrity(t, db, &stage, &draft, &played, &bye)
			requestJSON(t, app, "POST", "/api/v1/stages/1/clear", fiber.Map{}, 409)
			if clearMatchCount(t, db, 1) != 3 {
				t.Fatal("blocked clear partially deleted draw")
			}
			var got domain.Stage
			db.First(&got, 1)
			assertJSONUnchanged(t, stage, got)
		})
	}
}
func TestClearProtectsExplicitAndInferredSeededDraws(t *testing.T) {
	for _, kind := range []string{"explicit", "inferred swiss", "inferred knockout", "inferred same sort"} {
		t.Run(kind, func(t *testing.T) {
			db, app := integrityFixture(t)
			source := domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Source", Type: "manual", Round: 1}
			child := domain.Stage{ID: 2, TournamentID: 1, LeagueID: 1, Name: "Seeded", Type: "swiss", SortOrder: 1, Round: 1}
			switch kind {
			case "explicit":
				sid := uint(1)
				child.SourceStageID = &sid
				child.Type = "manual"
			case "inferred knockout":
				child.Type = "knockout"
			case "inferred same sort":
				child.SortOrder = 0
			}
			a := integrityMatch(1, 1, 2, "draft")
			b := integrityMatch(2, 3, 4, "draft")
			b.StageID = 2
			saveIntegrity(t, db, &source, &child, &a, &b)
			requestJSON(t, app, "POST", "/api/v1/stages/1/clear", fiber.Map{}, 409)
			if clearMatchCount(t, db, 1) != 1 || clearMatchCount(t, db, 2) != 1 {
				t.Fatal("source clear changed a seeded draw")
			}
			requestJSON(t, app, "POST", "/api/v1/stages/2/clear", fiber.Map{}, 200)
			requestJSON(t, app, "POST", "/api/v1/stages/1/clear", fiber.Map{}, 200)
		})
	}
}
func TestClearRoundProtectsLaterRoundsButWholeDrawIncludesThem(t *testing.T) {
	db, app := integrityFixture(t)
	a := integrityMatch(1, 1, 2, "draft")
	b := integrityMatch(2, 3, 4, "scheduled")
	b.Round = 2
	saveIntegrity(t, db, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Manual", Type: "manual", Round: 2}, &a, &b)
	requestJSON(t, app, "POST", "/api/v1/stages/1/clear", fiber.Map{"round": 1}, 409)
	if clearMatchCount(t, db, 1) != 2 {
		t.Fatal("earlier round was cleared while later games remained")
	}
	cleared := clearedResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/clear", fiber.Map{}, 200))
	if cleared.Cleared != 2 || cleared.Stage.Round != 0 {
		t.Fatal("whole-draw clear incorrectly treated its own later rounds as dependents")
	}
}
func TestClearLatestSwissRoundKeepsReviewedPreviousRoundAndCanPreviewAgain(t *testing.T) {
	db, app := integrityFixture(t)
	saveIntegrity(t, db, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Swiss", Type: "swiss"})
	requestJSON(t, app, "POST", "/api/v1/stages/1/generate", fiber.Map{"round": 1}, 200)
	if err := db.Model(&domain.Match{}).Where("stage_id = ?", 1).Updates(map[string]any{"status": "approved", "published": true}).Error; err != nil {
		t.Fatal(err)
	}
	second := generationResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/generate", fiber.Map{"round": 2}, 200))
	if len(second.Matches) != 3 {
		t.Fatal("expected three second-round fixtures")
	}
	cleared := clearedResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/clear", fiber.Map{"round": 2}, 200))
	if cleared.Cleared != 3 || cleared.Round != 2 || cleared.Stage.Round != 1 || clearMatchCount(t, db, 1) != 5 {
		t.Fatal("latest Swiss round clear changed reviewed previous round or round counter")
	}
	preview := generationResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/preview", fiber.Map{"round": 2}, 200))
	if preview.Round != 2 || len(preview.Matches) != 3 {
		t.Fatal("next Swiss round could not be previewed after clearing")
	}
	redrawn := generationResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/generate", fiber.Map{"round": 2, "preview_token": preview.PreviewToken}, 200))
	if len(redrawn.Matches) != 3 {
		t.Fatal("next Swiss round could not be redrawn")
	}
}
func TestClearValidatesOptionalRoundAndRequiresAdministrator(t *testing.T) {
	db, app := integrityFixture(t)
	match := integrityMatch(1, 1, 2, "draft")
	saveIntegrity(t, db, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Manual", Type: "manual", Round: 1}, &domain.Stage{ID: 2, TournamentID: 1, LeagueID: 1, Name: "RR", Type: "round_robin"}, &match)
	for _, body := range []any{fiber.Map{"round": 0}, fiber.Map{"round": -1}, fiber.Map{"round": nil}, fiber.Map{"round": 1.5}, fiber.Map{"round": "1"}, fiber.Map{"rounds": 1}, json.RawMessage("null")} {
		requestJSON(t, app, "POST", "/api/v1/stages/1/clear", body, 400)
	}
	requestJSON(t, app, "POST", "/api/v1/stages/2/clear", fiber.Map{"round": 1}, 400)
	requestJSON(t, app, "POST", "/api/v1/stages/999/clear", fiber.Map{}, 404)
	if clearMatchCount(t, db, 1) != 1 {
		t.Fatal("invalid clear request modified games")
	}
	for _, role := range []string{"editor", "referee", "viewer"} {
		t.Run(role, func(t *testing.T) {
			restricted := fiber.New()
			a := API{DB: db}
			v := restricted.Group("/api/v1", func(c fiber.Ctx) error { c.Locals("user", User{Roles: []string{role}}); return c.Next() })
			a.registerDomainRoutes(v)
			requestJSON(t, restricted, "POST", "/api/v1/stages/1/clear", fiber.Map{}, 403)
		})
	}
}
