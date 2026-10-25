package web

import (
	"RCJVGameMaster/src/domain"
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
	"time"
)

func matchFixture() (domain.Match, domain.League, LiveState) {
	team2 := uint(2)
	m := domain.Match{Team1ID: 1, Team2ID: &team2, RefereeIDs: []uint{3}, Status: "scheduled"}
	l := domain.League{RobotCount: 2, MaxWeightG: 1500, HalfSeconds: 600, HalftimeSeconds: 300}
	s, _ := readLiveState(nil)
	for _, id := range []uint{1, 2} {
		for robot := 1; robot <= 2; robot++ {
			s.Checks = append(s.Checks, RobotCheck{TeamID: id, Robot: robot, WeightG: 1400, BallZone: true, Voltage: true, Size: true, Motors: true})
		}
	}
	return m, l, s
}

func TestRefereeLifecycle(t *testing.T) {
	m, l, s := matchFixture()
	now := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	apply := func(cmd matchCommand) {
		t.Helper()
		if err := applyMatchCommand(&m, &l, &s, cmd, now); err != nil {
			t.Fatal(err)
		}
	}
	apply(matchCommand{Action: "setup", BlueTeamID: 1, KickoffTeamID: 2})
	apply(matchCommand{Action: "start"})
	if s.Phase != "first_half" || m.Status != "in_progress" {
		t.Fatalf("unexpected kickoff: %+v", s)
	}
	now = now.Add(45 * time.Second)
	apply(matchCommand{Action: "pause"})
	if s.RemainingSeconds != 555 || s.Running {
		t.Fatal("paused clock did not preserve time")
	}
	now = now.Add(time.Hour)
	if clockRemaining(&s, now) != 555 {
		t.Fatal("paused clock lost time")
	}
	apply(matchCommand{Action: "goal", TeamID: 1, Delta: 1})
	apply(matchCommand{Action: "timeout", TeamID: 1, Robot: 2})
	firstTimeout := s.Timeouts[0].EndsAt
	now = now.Add(20 * time.Second)
	apply(matchCommand{Action: "timeout", TeamID: 1, Robot: 2})
	if len(s.Timeouts) != 1 || !s.Timeouts[0].EndsAt.Equal(firstTimeout.Add(20*time.Second)) {
		t.Fatal("timeout did not reset for same robot")
	}
	apply(matchCommand{Action: "start"})
	if err := applyMatchCommand(&m, &l, &s, matchCommand{Action: "advance"}, now); err == nil {
		t.Fatal("advanced a running period early")
	}
	now = now.Add(555 * time.Second)
	apply(matchCommand{Action: "advance"})
	if s.Phase != "halftime" || !s.Running || s.RemainingSeconds != 300 {
		t.Fatal("acknowledging first half must start halftime")
	}
	now = now.Add(300 * time.Second)
	apply(matchCommand{Action: "advance"})
	if s.Phase != "second_half" || s.Running || *s.BlueTeamID != 2 || *s.KickoffTeamID != 1 {
		t.Fatal("second half must switch ends/kickoff and wait for start")
	}
	apply(matchCommand{Action: "start"})
	now = now.Add(600 * time.Second)
	apply(matchCommand{Action: "advance"})
	if s.Phase != "finished" || s.Running {
		t.Fatal("match did not finish")
	}
	if err := applyMatchCommand(&m, &l, &s, matchCommand{Action: "submit"}, now); err == nil {
		t.Fatal("submitted without signatures")
	}
	apply(matchCommand{Action: "submit", Signatures: testSignatures(t), Notes: "Both captains accepted."})
	if m.Status != "submitted" || m.Goals1 != 1 || len(s.Signatures) != 3 {
		t.Fatal("signed result not submitted")
	}
}

func testSignatures(t *testing.T) []MatchSignature {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	return []MatchSignature{{Kind: "team", ID: 1, Name: "Team one", Signature: data}, {Kind: "team", ID: 2, Name: "Team two", Signature: data}, {Kind: "referee", ID: 3, Name: "Referee", Signature: data}}
}

func TestInspectionValidation(t *testing.T) {
	for _, name := range []string{"missing robot", "overweight", "duplicate robot", "unchecked voltage", "foreign team", "zero weight"} {
		t.Run(name, func(t *testing.T) {
			m, l, s := matchFixture()
			switch name {
			case "missing robot":
				s.Checks = s.Checks[:3]
			case "overweight":
				s.Checks[0].WeightG = 1501
			case "duplicate robot":
				s.Checks[1] = s.Checks[0]
			case "unchecked voltage":
				s.Checks[0].Voltage = false
			case "foreign team":
				s.Checks[0].TeamID = 99
			case "zero weight":
				s.Checks[0].WeightG = 0
			}
			if err := validChecks(&m, &l, s.Checks); err == nil {
				t.Fatal("accepted invalid inspections")
			}
		})
	}
	m, l, s := matchFixture()
	l.MaxWeightG = 0
	s.Checks[0].WeightG = 10000
	if err := validChecks(&m, &l, s.Checks); err != nil {
		t.Fatal("unlimited weight should accept positive weight", err)
	}
}

func TestSignatureValidation(t *testing.T) {
	m, _, _ := matchFixture()
	for _, name := range []string{"missing", "duplicate", "foreign referee", "invalid image"} {
		t.Run(name, func(t *testing.T) {
			sigs := testSignatures(t)
			switch name {
			case "missing":
				sigs = sigs[:2]
			case "duplicate":
				sigs[2] = sigs[0]
			case "foreign referee":
				sigs[2].ID = 99
			case "invalid image":
				sigs[2].Signature = "data:image/png;base64,bm90LXBuZw=="
			}
			if err := validateSignatures(&m, sigs); err == nil {
				t.Fatal("accepted invalid signature set")
			}
		})
	}
}

func TestMatchControlRejectsInvalidActions(t *testing.T) {
	for _, cmd := range []matchCommand{{Action: "start"}, {Action: "advance"}, {Action: "goal", TeamID: 1, Delta: 1}, {Action: "timeout", TeamID: 1, Robot: 1}, {Action: "submit"}, {Action: "unknown"}} {
		m, l, s := matchFixture()
		if err := applyMatchCommand(&m, &l, &s, cmd, time.Now()); err == nil {
			t.Errorf("pregame accepted %s", cmd.Action)
		}
	}
}
