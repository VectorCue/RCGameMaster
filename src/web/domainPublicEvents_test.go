package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestPublicMatchEventsAllowlistAndPendingResultPrivacy(t *testing.T) {
	db, app := integrityFixture(t)
	match := integrityMatch(1, 1, 2, "in_progress")
	match.Published = true
	match.Goals1 = 2
	match.Goals2 = 1
	match.Notes = "private-match-note"
	saveIntegrity(t, db, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Round", Type: "manual"}, &match)
	for _, tt := range []struct {
		name, status, phase   string
		wantEvents, wantGoals int
	}{{"live goals visible", "in_progress", "first_half", 4, 2}, {"ended result awaiting signatures", "in_progress", "finished", 2, 0}, {"submitted result awaiting editor", "submitted", "finished", 2, 0}, {"approved result and goals visible", "approved", "finished", 4, 2}} {
		t.Run(tt.name, func(t *testing.T) {
			events := []fiber.Map{
				{"id": 1, "at": "2026-10-25T09:01:00Z", "type": "goal", "phase": "first_half", "remaining_seconds": 540, "team_id": 1, "side": "blue", "notes": "private-event-note"},
				{"id": 2, "at": "2026-10-25T09:02:00Z", "type": "robot_timeout", "phase": "first_half", "remaining_seconds": 480, "team_id": 1, "robot": 1, "side": "blue", "reason": "private-timeout-reason"},
				{"id": 3, "at": "2026-10-25T09:03:00Z", "type": "goal_removed", "phase": "first_half", "remaining_seconds": 420, "team_id": 2, "signature": "private-event-signature"},
				{"id": 4, "at": "2026-10-25T09:04:00Z", "type": "match_ended", "phase": "second_half", "remaining_seconds": 300, "next_phase": "finished", "forced": true, "reason": "private-ending-reason"},
			}
			raw, err := json.Marshal(fiber.Map{"phase": tt.phase, "events": events, "ended_early": true, "end_reason": "private-end-reason", "notes": "private-state-note", "checks": []string{"private-inspection"}, "signatures": []string{"private-signature"}, "signed_goals1": 2, "signed_goals2": 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&domain.Match{}).Where("id = ?", match.ID).Updates(map[string]any{"status": tt.status, "live_state": json.RawMessage(raw)}).Error; err != nil {
				t.Fatal(err)
			}
			response := requestJSON(t, app, "GET", "/api/public/C", nil, 200)
			if strings.Contains(string(response), "private-") || strings.Contains(string(response), "signed_goals") {
				t.Fatal("public event response leaked private state or nested event fields")
			}
			var workspace domain.Workspace
			if err := json.Unmarshal(response, &workspace); err != nil {
				t.Fatal(err)
			}
			if len(workspace.Matches) != 1 {
				t.Fatal("missing published match")
			}
			got := workspace.Matches[0]
			var state struct {
				Events     []domain.MatchEvent `json:"events"`
				EndedEarly bool                `json:"ended_early"`
			}
			if err := json.Unmarshal(got.LiveState, &state); err != nil {
				t.Fatal(err)
			}
			if len(state.Events) != tt.wantEvents || got.Goals1 != tt.wantGoals || !state.EndedEarly {
				t.Fatalf("unexpected event/score privacy: events=%d goals=%d early=%v", len(state.Events), got.Goals1, state.EndedEarly)
			}
			if tt.wantEvents == 2 && (state.Events[0].Type != "robot_timeout" || state.Events[1].Type != "match_ended" || !state.Events[1].Forced || state.Events[1].NextPhase != "finished") {
				t.Fatal("safe non-goal events or phase transition metadata were lost")
			}
		})
	}
}
func TestPublicMalformedEventsFailClosedWithoutLeakingLiveScore(t *testing.T) {
	db, app := integrityFixture(t)
	match := integrityMatch(1, 1, 2, "in_progress")
	match.Published = true
	match.Goals1 = 7
	match.LiveState = []byte(`{"phase":"first_half","events":"private-invalid-event-data","end_reason":"private-reason"}`)
	saveIntegrity(t, db, &domain.Stage{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Round", Type: "manual"}, &match)
	response := requestJSON(t, app, "GET", "/api/public/C", nil, 200)
	if strings.Contains(string(response), "private-") {
		t.Fatal("malformed event data leaked")
	}
	var workspace domain.Workspace
	if err := json.Unmarshal(response, &workspace); err != nil {
		t.Fatal(err)
	}
	if len(workspace.Matches) != 1 || workspace.Matches[0].Goals1 != 0 || string(workspace.Matches[0].LiveState) != "{}" {
		t.Fatal("invalid event JSON did not fail closed")
	}
}
