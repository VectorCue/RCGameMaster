package domain

import "testing"

func ptr(n uint) *uint { return &n }
func TestRoundRobinEveryPairExactlyOnce(t *testing.T) {
	for _, ids := range [][]uint{{1, 2}, {1, 2, 3}, {1, 2, 3, 4}, {1, 2, 3, 4, 5}} {
		pairs := RoundRobin(ids)
		if len(pairs) != len(ids)*(len(ids)-1)/2 {
			t.Fatalf("%d teams: got %d games", len(ids), len(pairs))
		}
		seen := map[[2]uint]bool{}
		roundTeams := map[[2]uint]bool{}
		for _, p := range pairs {
			a, b := p.A, *p.B
			if a > b {
				a, b = b, a
			}
			key := [2]uint{a, b}
			if a == 0 || a == b || seen[key] {
				t.Fatalf("invalid or repeated pair: %+v", p)
			}
			seen[key] = true
			for _, team := range []uint{a, b} {
				k := [2]uint{uint(p.Round), team}
				if roundTeams[k] {
					t.Fatalf("team plays twice in round %d", p.Round)
				}
				roundTeams[k] = true
			}
		}
	}
}
func TestSwissAvoidsRematchesAndRotatesBye(t *testing.T) {
	previous := []Match{{Team1ID: 1, Team2ID: ptr(2)}, {Team1ID: 3, Team2ID: ptr(4)}, {Team1ID: 5, Team2ID: nil}}
	pairs, e := SwissPairs([]uint{1, 2, 3, 4, 5}, previous, 2)
	if e != nil {
		t.Fatal(e)
	}
	seen := map[uint]bool{}
	for _, p := range pairs {
		if seen[p.A] {
			t.Fatal("duplicate team")
		}
		seen[p.A] = true
		if p.B == nil {
			if p.A == 5 {
				t.Fatal("repeated bye")
			}
			continue
		}
		if seen[*p.B] {
			t.Fatal("duplicate opponent")
		}
		seen[*p.B] = true
		if (p.A == 1 && *p.B == 2) || (p.A == 3 && *p.B == 4) {
			t.Fatal("rematch")
		}
	}
	if len(seen) != 5 {
		t.Fatal("missing team")
	}
}
func TestSwissImpossiblePairingFails(t *testing.T) {
	_, e := SwissPairs([]uint{1, 2}, []Match{{Team1ID: 1, Team2ID: ptr(2)}}, 2)
	if e == nil {
		t.Fatal("expected exhausted pairings to fail")
	}
}
func TestKnockoutSeedsByesAndStrongVsWeak(t *testing.T) {
	pairs := KnockoutPairs([]uint{1, 2, 3, 4, 5}, 1)
	if len(pairs) != 4 {
		t.Fatalf("got %d pairs", len(pairs))
	}
	for i := 0; i < 3; i++ {
		if pairs[i].A != uint(i+1) || pairs[i].B != nil {
			t.Fatal("top seed must receive bye")
		}
	}
	if pairs[3].A != 4 || *pairs[3].B != 5 {
		t.Fatal("wrong remaining pairing")
	}
}
func TestStandingsIgnoreUnreviewedAndCountBye(t *testing.T) {
	teams := []Team{{ID: 1, LeagueID: 1}, {ID: 2, LeagueID: 1}, {ID: 3, LeagueID: 1}}
	matches := []Match{{LeagueID: 1, StageID: 1, Team1ID: 1, Team2ID: ptr(2), Goals1: 2, Goals2: 1, Status: "approved"}, {LeagueID: 1, StageID: 1, Team1ID: 2, Team2ID: ptr(3), Goals1: 99, Status: "submitted"}, {LeagueID: 1, StageID: 1, Team1ID: 3, IsBye: true, Status: "approved"}}
	rows := Standings(teams, matches, 1, 1)
	if len(rows) != 3 || rows[0].TeamID != 1 || rows[0].Points != 3 || rows[1].TeamID != 3 || rows[1].Points != 3 || rows[2].Scored != 1 || rows[2].Played != 1 {
		t.Fatalf("unexpected standings: %+v", rows)
	}
}
