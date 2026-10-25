package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRobotTimeoutCanRestartClearAndRestartAfterExpiry(t *testing.T) {
	match, league, state := matchFixture()
	now := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	apply := func(command matchCommand) {
		t.Helper()
		if err := applyMatchCommand(&match, &league, &state, command, now); err != nil {
			t.Fatal(err)
		}
	}
	apply(matchCommand{Action: "setup", BlueTeamID: 1, KickoffTeamID: 2})
	apply(matchCommand{Action: "start"})
	apply(matchCommand{Action: "timeout", TeamID: 1, Robot: 1})
	apply(matchCommand{Action: "timeout", TeamID: 2, Robot: 2})
	now = now.Add(25 * time.Second)
	apply(matchCommand{Action: "timeout", TeamID: 1, Robot: 1})
	if len(state.Timeouts) != 2 || !state.Timeouts[0].EndsAt.Equal(now.Add(time.Minute)) {
		t.Fatal("reset did not restart exactly one timeout at 60 seconds")
	}
	if state.Events[len(state.Events)-1].Type != "robot_timeout_reset" {
		t.Fatal("reset action was not logged")
	}
	apply(matchCommand{Action: "timeout_clear", TeamID: 1, Robot: 1})
	if len(state.Timeouts) != 1 || state.Timeouts[0].TeamID != 2 {
		t.Fatal("returning a robot removed the wrong timeout")
	}
	apply(matchCommand{Action: "timeout", TeamID: 1, Robot: 1})
	now = now.Add(70 * time.Second)
	apply(matchCommand{Action: "timeout", TeamID: 1, Robot: 1})
	if !state.Timeouts[1].EndsAt.Equal(now.Add(time.Minute)) {
		t.Fatal("expired robot could not restart its timeout")
	}
	apply(matchCommand{Action: "timeout_clear", TeamID: 1, Robot: 1})
	count := len(state.Events)
	if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "timeout_clear", TeamID: 1, Robot: 1}, now); err == nil {
		t.Fatal("clearing a missing timeout succeeded")
	}
	if len(state.Events) != count {
		t.Fatal("rejected timeout command generated a public event")
	}
}

func TestForcedAdvancePreservesHalftimeAndSwapsSidesOnce(t *testing.T) {
	match, league, state := matchFixture()
	now := time.Now().UTC()
	apply := func(command matchCommand) {
		t.Helper()
		if err := applyMatchCommand(&match, &league, &state, command, now); err != nil {
			t.Fatal(err)
		}
	}
	apply(matchCommand{Action: "setup", BlueTeamID: 1, KickoffTeamID: 2})
	apply(matchCommand{Action: "start"})
	apply(matchCommand{Action: "goal", TeamID: 2, Delta: 1})
	goal := state.Events[len(state.Events)-1]
	if goal.Type != "goal" || goal.Side != "yellow" || goal.TeamID != 2 {
		t.Fatal("goal event did not preserve the team's current side")
	}
	if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "advance"}, now); err == nil {
		t.Fatal("ordinary advance skipped time without confirmation")
	}
	apply(matchCommand{Action: "advance", Force: true})
	if state.Phase != "halftime" || !state.Running || *state.BlueTeamID != 1 {
		t.Fatal("first half skip did not start break or changed sides early")
	}
	apply(matchCommand{Action: "advance", Force: true})
	if state.Phase != "second_half" || state.Running || *state.BlueTeamID != 2 || *state.KickoffTeamID != 1 {
		t.Fatal("break skip must switch sides/kickoff and wait for start")
	}
	apply(matchCommand{Action: "start"})
	apply(matchCommand{Action: "goal", TeamID: 2, Delta: 1})
	if state.Events[len(state.Events)-1].Side != "blue" || goal.Side != "yellow" {
		t.Fatal("event side was reinterpreted after halftime")
	}
	apply(matchCommand{Action: "advance", Force: true})
	if state.Phase != "finished" || state.Running || !state.EndedEarly || match.Goals2 != 2 || match.Status != "in_progress" {
		t.Fatal("second-half skip must finish early while retaining the score for normal review")
	}
	last := state.Events[len(state.Events)-1]
	if last.Type != "match_ended_early" || !last.Forced {
		t.Fatal("forced end was not recorded")
	}
}

func TestEarlyFinishRequiresReasonAndRetainsSignatureReviewFlow(t *testing.T) {
	for _, phase := range []string{"first_half", "halftime", "second_half"} {
		t.Run(phase, func(t *testing.T) {
			match, league, state := matchFixture()
			state.Phase, state.Running, state.RemainingSeconds = phase, true, 500
			match.Status, match.Goals1, match.Goals2 = "in_progress", 3, 2
			now := time.Now().UTC()
			state.StartedAt = &now
			if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "finish_early"}, now); err == nil {
				t.Fatal("early finish accepted no reason")
			}
			if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "finish_early", Reason: strings.Repeat("x", 1001)}, now); err == nil {
				t.Fatal("early finish accepted oversized reason")
			}
			if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "finish_early", Reason: "  Technical problem  "}, now); err != nil {
				t.Fatal(err)
			}
			if !state.EndedEarly || state.EndReason != "Technical problem" || state.Phase != "finished" || state.Running || state.StartedAt != nil || state.RemainingSeconds != 0 {
				t.Fatal("early finish did not stop/save the match")
			}
			if match.Goals1 != 3 || match.Goals2 != 2 || match.Status != "in_progress" || match.Published {
				t.Fatal("early finish changed score or bypassed review")
			}
			if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "submit"}, now); err == nil {
				t.Fatal("early-ended match submitted without signatures")
			}
			if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "submit", Signatures: testSignatures(t)}, now); err != nil {
				t.Fatal(err)
			}
			if match.Status != "submitted" {
				t.Fatal("early-ended match failed normal signature submission")
			}
		})
	}
	for _, phase := range []string{"pregame", "ready", "finished"} {
		match, league, state := matchFixture()
		state.Phase = phase
		if err := applyMatchCommand(&match, &league, &state, matchCommand{Action: "finish_early", Reason: "test"}, time.Now()); err == nil {
			t.Fatalf("early finish accepted phase %s", phase)
		}
	}
}

func TestMatchEventsPersistAndLegacyStatesRemainReadable(t *testing.T) {
	match, league, state := matchFixture()
	now := time.Now().UTC()
	for _, command := range []matchCommand{{Action: "setup", BlueTeamID: 1, KickoffTeamID: 2}, {Action: "start"}, {Action: "goal", TeamID: 1, Delta: 1}, {Action: "pause"}} {
		if err := applyMatchCommand(&match, &league, &state, command, now); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := readLiveState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Events) != 3 {
		t.Fatalf("events lost on persistence: %d", len(reloaded.Events))
	}
	for index, event := range reloaded.Events {
		if event.ID != uint(index+1) || event.At.IsZero() {
			t.Fatal("event sequence or timestamp invalid")
		}
	}
	legacy, err := readLiveState(json.RawMessage(`{"phase":"first_half","remaining_seconds":60}`))
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Events == nil || len(legacy.Events) != 0 {
		t.Fatal("legacy match state invented events")
	}
}
