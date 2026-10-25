package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"fmt"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func integrityFixture(t *testing.T) (*gorm.DB, *fiber.App) {
	t.Helper()
	db := integrationDB(t)
	for _, row := range []any{&domain.Tournament{ID: 1, Name: "Cup", Abbrev: "C", StartDate: "2026-10-25", EndDate: "2026-10-25", Timezone: "UTC"}, &domain.League{ID: 1, TournamentID: 1, Name: "Soccer", Abbrev: "s", Mode: "full", HalfSeconds: 600, HalftimeSeconds: 300}} {
		if e := db.Create(row).Error; e != nil {
			t.Fatal(e)
		}
	}
	for i := uint(1); i <= 5; i++ {
		if e := db.Create(&domain.Team{ID: i, TournamentID: 1, LeagueID: 1, Name: fmt.Sprintf("Team %d", i)}).Error; e != nil {
			t.Fatal(e)
		}
	}
	app, e := newApp(localTestConfig(), db)
	if e != nil {
		t.Fatal(e)
	}
	return db, app
}
func saveIntegrity(t *testing.T, db *gorm.DB, rows ...any) {
	t.Helper()
	for _, row := range rows {
		if e := db.Create(row).Error; e != nil {
			t.Fatal(e)
		}
	}
}
func integrityMatch(number int, a, b uint, status string) domain.Match {
	var opponent *uint
	if b != 0 {
		opponent = &b
	}
	return domain.Match{TournamentID: 1, LeagueID: 1, StageID: 1, Number: number, Round: 1, Team1ID: a, Team2ID: opponent, Status: status, DurationMinutes: 25, RefereeIDs: []uint{}, LiveState: []byte(`{}`)}
}
func TestUnschedulingUnpublishesAndPublicStateFailsClosed(t *testing.T) {
	db, app := integrityFixture(t)
	start := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	fieldID, slotID := uint(1), uint(1)
	m := integrityMatch(1, 1, 2, "scheduled")
	m.Published = true
	m.ScheduledAt = &start
	m.FieldID = &fieldID
	m.SlotID = &slotID
	m.Notes = "private-note"
	m.LiveState = []byte(`["private-signature"]`)
	saveIntegrity(t, db, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Manual", Type: "manual"}, &domain.Field{ID: 1, TournamentID: 1, LeagueID: 1, Name: "A"}, &domain.Slot{ID: 1, TournamentID: 1, FieldID: 1, StartsAt: start, EndsAt: start.Add(30 * time.Minute)}, &m)
	public := requestJSON(t, app, "GET", "/api/public/C", nil, 200)
	if strings.Contains(string(public), "private-") {
		t.Fatal("private malformed state or notes leaked")
	}
	m.LiveState = []byte(`{"phase":{"private-signature":true},"checks":["private-check"]}`)
	db.Save(&m)
	public = requestJSON(t, app, "GET", "/api/public/C", nil, 200)
	if strings.Contains(string(public), "private-") {
		t.Fatal("wrong JSON field type leaked private nested state")
	}
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/schedule", m.ID), fiber.Map{"slot_id": nil, "referee_ids": []uint{}}, 200)
	var w domain.Workspace
	if e := json.Unmarshal(requestJSON(t, app, "GET", "/api/public/C", nil, 200), &w); e != nil {
		t.Fatal(e)
	}
	if len(w.Matches) != 0 {
		t.Fatal("unscheduled match remained public")
	}
}
func TestResultReturnsClearSignaturesAndSeededCorrectionsAreGuarded(t *testing.T) {
	db, app := integrityFixture(t)
	m := integrityMatch(1, 1, 2, "submitted")
	m.Goals1 = 2
	m.Goals2 = 1
	m.LiveState = []byte(`{"phase":"finished","signatures":[{"kind":"team","id":1,"name":"A","signature":"old-signature"}],"signed_goals1":2,"signed_goals2":1}`)
	saveIntegrity(t, db, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Round", Type: "round_robin"}, &m)
	db.Model(&domain.League{}).Where("id = ?", 1).Update("referee_mode", true)
	path := fmt.Sprintf("/api/v1/matches/%d/review", m.ID)
	returned := requestJSON(t, app, "POST", path, fiber.Map{"approve": false}, 200)
	if strings.Contains(string(returned), "old-signature") || strings.Contains(string(returned), "signed_goals") {
		t.Fatal("returned report retained signatures or signed score")
	}
	// A correction preserves the signed original score as separate audit evidence.
	m.Status = "submitted"
	m.LiveState = []byte(`{"phase":"finished","signatures":[{"signature":"old-signature"}],"signed_goals1":2,"signed_goals2":1}`)
	db.Save(&m)
	amended := requestJSON(t, app, "POST", path, fiber.Map{"approve": true, "goals1": 3}, 200)
	var got domain.Match
	if e := json.Unmarshal(amended, &got); e != nil {
		t.Fatal(e)
	}
	var state struct {
		SignedGoals1 int `json:"signed_goals1"`
	}
	json.Unmarshal(got.LiveState, &state)
	if got.Goals1 != 3 || state.SignedGoals1 != 2 || !strings.Contains(string(got.LiveState), "old-signature") {
		t.Fatal("amendment erased the signed original")
	}
	source := uint(1)
	child := domain.Stage{ID: 2, TournamentID: 1, LeagueID: 1, Name: "Final", Type: "knockout", SortOrder: 1, SourceStageID: &source}
	downstream := integrityMatch(2, 1, 3, "draft")
	downstream.StageID = 2
	saveIntegrity(t, db, &child, &downstream)
	requestJSON(t, app, "POST", path, fiber.Map{"approve": true, "goals1": 4}, 409)
	requestJSON(t, app, "POST", path, fiber.Map{"approve": false}, 409)
	requestJSON(t, app, "POST", path, fiber.Map{"approve": true, "notes": "Editor note"}, 200)
}
func TestKnockoutSourcesAdvanceWinnersAndLimitQualifiers(t *testing.T) {
	db, app := integrityFixture(t)
	a := integrityMatch(1, 1, 4, "approved")
	a.Goals1 = 2
	b := integrityMatch(2, 2, 3, "approved")
	b.Goals1 = 3
	source := uint(1)
	saveIntegrity(t, db, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Semifinals", Type: "knockout"}, &domain.Stage{ID: 2, TournamentID: 1, LeagueID: 1, Name: "Final", Type: "knockout", SortOrder: 1, SourceStageID: &source, TeamLimit: 2}, &a, &b)
	var out struct {
		Matches []domain.Match `json:"matches"`
	}
	if e := json.Unmarshal(requestJSON(t, app, "POST", "/api/v1/stages/2/generate", fiber.Map{}, 200), &out); e != nil {
		t.Fatal(e)
	}
	if len(out.Matches) != 1 || out.Matches[0].Team1ID != 1 || out.Matches[0].Team2ID == nil || *out.Matches[0].Team2ID != 2 {
		t.Fatalf("eliminated team entered final: %+v", out.Matches)
	}
}
func TestSwissWaitsForInferredSourceAndRoundRobinOpponentsCanBeReseeded(t *testing.T) {
	db, app := integrityFixture(t)
	m := integrityMatch(1, 1, 2, "submitted")
	saveIntegrity(t, db, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Qualification", Type: "round_robin"}, &domain.Stage{ID: 2, TournamentID: 1, LeagueID: 1, Name: "Swiss", Type: "swiss", SortOrder: 1}, &m)
	requestJSON(t, app, "POST", "/api/v1/stages/2/generate", fiber.Map{}, 409)
	m.Status = "approved"
	m.Goals1 = 1
	db.Save(&m)
	var out struct {
		Matches []domain.Match `json:"matches"`
	}
	json.Unmarshal(requestJSON(t, app, "POST", "/api/v1/stages/2/generate", fiber.Map{}, 200), &out)
	if len(out.Matches) != 3 {
		t.Fatal("Swiss must pair five teams including one bye")
	}
	byes := 0
	for _, match := range out.Matches {
		if match.IsBye {
			byes++
			if match.Status != "approved" {
				t.Fatal("bye was not awarded automatically")
			}
			requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/review", match.ID), fiber.Map{"approve": false}, 409)
			requestJSON(t, app, "DELETE", fmt.Sprintf("/api/v1/matches/%d", match.ID), nil, 204)
		}
	}
	if byes != 1 {
		t.Fatal("expected one Swiss bye")
	}
}
