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

type generationTestResponse struct {
	Matches      []domain.Match `json:"matches"`
	PreviewToken string         `json:"preview_token"`
	Round        int            `json:"round"`
}

func generationResponse(t *testing.T, raw []byte) generationTestResponse {
	t.Helper()
	var response generationTestResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	return response
}
func generationSequence(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var sequence int
	if err := db.Raw("SELECT COALESCE(MAX(seq),0) FROM sqlite_sequence WHERE name = 'matches'").Scan(&sequence).Error; err != nil {
		t.Fatal(err)
	}
	return sequence
}
func TestStagePreviewDoesNotWriteOrConsumeIDsAndCommitsExactlyItsPlan(t *testing.T) {
	db, app := integrityFixture(t)
	stage := domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Round robin", Type: "round_robin", Round: 7}
	saveIntegrity(t, db, &stage)
	var changesBefore, changesAfter int
	db.Raw("SELECT total_changes()").Scan(&changesBefore)
	sequence := generationSequence(t, db)
	preview := generationResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/preview", fiber.Map{}, 200))
	again := generationResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/preview", fiber.Map{}, 200))
	if len(preview.Matches) != 10 || preview.Round != 1 || len(preview.PreviewToken) != 64 || preview.PreviewToken != again.PreviewToken {
		t.Fatal("preview plan or stable fingerprint is incorrect")
	}
	for i, match := range preview.Matches {
		if match.ID != 0 || match.Number != i+1 || !match.CreatedAt.IsZero() || !match.UpdatedAt.IsZero() {
			t.Fatal("preview created IDs, timestamps or inconsistent numbers")
		}
	}
	db.Raw("SELECT total_changes()").Scan(&changesAfter)
	if changesBefore != changesAfter || generationSequence(t, db) != sequence {
		t.Fatal("preview performed writes or consumed match IDs")
	}
	var stored domain.Stage
	db.First(&stored, stage.ID)
	if stored.Round != 7 {
		t.Fatal("preview advanced the stage round")
	}
	var count int64
	db.Model(&domain.Match{}).Count(&count)
	if count != 0 {
		t.Fatal("preview persisted matches")
	}
	created := generationResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/generate", fiber.Map{"round": preview.Round, "preview_token": preview.PreviewToken}, 200))
	if len(created.Matches) != len(preview.Matches) {
		t.Fatal("commit produced a different number of matches")
	}
	for i, match := range created.Matches {
		if match.ID != uint(i+1) {
			t.Fatal("preview consumed IDs before commit")
		}
		match.ID = 0
		match.CreatedAt = time.Time{}
		match.UpdatedAt = time.Time{}
		want, _ := json.Marshal(preview.Matches[i])
		got, _ := json.Marshal(match)
		if string(want) != string(got) {
			t.Fatalf("committed match %d differs from preview", i)
		}
	}
	db.First(&stored, stage.ID)
	if stored.Round != 5 {
		t.Fatal("commit did not update the stage round")
	}
	requestJSON(t, app, "POST", "/api/v1/stages/1/generate", fiber.Map{"round": 1, "preview_token": preview.PreviewToken}, 409)
}
func TestGenerationRejectsStalePreviewAfterStandingsTeamsOrNumberCounterChange(t *testing.T) {
	for _, kind := range []string{"standings", "team details", "team registration", "match number counter"} {
		t.Run(kind, func(t *testing.T) {
			db, app := integrityFixture(t)
			source := uint(1)
			result := integrityMatch(1, 1, 2, "approved")
			result.Goals1 = 1
			saveIntegrity(t, db, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Qualification", Type: "round_robin"}, &domain.Stage{ID: 2, TournamentID: 1, LeagueID: 1, Name: "Swiss", Type: "swiss", SourceStageID: &source, SortOrder: 1}, &result)
			preview := generationResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/2/preview", fiber.Map{}, 200))
			switch kind {
			case "standings":
				if err := db.Model(&result).Update("goals1", 2).Error; err != nil {
					t.Fatal(err)
				}
			case "team details":
				if err := db.Model(&domain.Team{}).Where("id = ?", 1).Update("name", "Renamed team").Error; err != nil {
					t.Fatal(err)
				}
			case "team registration":
				saveIntegrity(t, db, &domain.Team{TournamentID: 1, LeagueID: 1, Name: "New registration", Startnumber: 6})
			case "match number counter":
				other := integrityMatch(2, 1, 2, "draft")
				other.LeagueID = 2
				other.StageID = 3
				saveIntegrity(t, db, &domain.League{ID: 2, TournamentID: 1, Name: "Other", Abbrev: "other", Mode: "full"}, &domain.Stage{ID: 3, TournamentID: 1, LeagueID: 2, Name: "Other stage", Type: "manual"}, &other)
			}
			sequence := generationSequence(t, db)
			requestJSON(t, app, "POST", "/api/v1/stages/2/generate", fiber.Map{"round": preview.Round, "preview_token": preview.PreviewToken}, 409)
			var count int64
			db.Model(&domain.Match{}).Where("stage_id = ?", 2).Count(&count)
			if count != 0 || generationSequence(t, db) != sequence {
				t.Fatal("stale preview committed matches or consumed IDs")
			}
			var stage domain.Stage
			db.First(&stage, 2)
			if stage.Round != 0 {
				t.Fatal("stale preview advanced stage")
			}
		})
	}
}
func TestStageManualBatchIsAtomicAndKeepsSingleTeamRunsUnscored(t *testing.T) {
	db, app := integrityFixture(t)
	db.Model(&domain.League{}).Where("id = ?", 1).Update("mode", "scheduling")
	saveIntegrity(t, db, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Runs", Type: "manual"})
	bad := fiber.Map{"matches": []fiber.Map{{"team1_id": 1, "team2_id": nil, "duration_minutes": 5, "round": 1}, {"team1_id": 999, "team2_id": nil, "duration_minutes": 5, "round": 1}}}
	sequence := generationSequence(t, db)
	requestJSON(t, app, "POST", "/api/v1/stages/1/matches", bad, 400)
	var count int64
	db.Model(&domain.Match{}).Count(&count)
	if count != 0 || generationSequence(t, db) != sequence {
		t.Fatal("invalid batch partially persisted or consumed IDs")
	}
	body := fiber.Map{"matches": []fiber.Map{{"team1_id": 1, "team2_id": nil, "duration_minutes": 5, "round": 1, "notes": "First run", "league_id": 999, "tournament_id": 999, "status": "approved"}, {"team1_id": 2, "team2_id": nil, "duration_minutes": 10, "round": 2}}}
	created := generationResponse(t, requestJSON(t, app, "POST", "/api/v1/stages/1/matches", body, 201))
	if len(created.Matches) != 2 {
		t.Fatal("wrong batch size")
	}
	for i, match := range created.Matches {
		if match.ID != uint(i+1) || match.Number != i+1 || match.LeagueID != 1 || match.TournamentID != 1 || match.StageID != 1 || match.IsBye || match.Status != "draft" || match.Published || match.Team2ID != nil {
			t.Fatalf("manual run has incorrect derived ownership or bye result: %+v", match)
		}
	}
	if created.Matches[0].Notes != "First run" {
		t.Fatal("manual notes were lost")
	}
	oversized := make([]fiber.Map, 257)
	for i := range oversized {
		oversized[i] = fiber.Map{"team1_id": 1}
	}
	requestJSON(t, app, "POST", "/api/v1/stages/1/matches", fiber.Map{"matches": oversized}, 400)
	requestJSON(t, app, "POST", "/api/v1/stages/1/matches", fiber.Map{"matches": []fiber.Map{}}, 400)
}
func TestStagePlanningAndBatchRoutesRequireAdministrator(t *testing.T) {
	db, _ := integrityFixture(t)
	saveIntegrity(t, db, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Round", Type: "round_robin"})
	for _, role := range []string{"editor", "referee", "viewer"} {
		t.Run(role, func(t *testing.T) {
			app := fiber.New()
			a := API{DB: db}
			v := app.Group("/api/v1", func(c fiber.Ctx) error { c.Locals("user", User{Roles: []string{role}}); return c.Next() })
			a.registerDomainRoutes(v)
			for _, path := range []string{"preview", "generate", "matches"} {
				body := fiber.Map{}
				if path == "matches" {
					body = fiber.Map{"matches": []fiber.Map{{"team1_id": 1, "team2_id": 2}}}
				}
				requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/stages/1/%s", path), body, 403)
			}
		})
	}
}
