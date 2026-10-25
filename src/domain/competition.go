package domain

import (
	"errors"
	"sort"
)

// Standings uses approved results only, with points, goal difference, goals scored
// and team ID providing a deterministic ordering. A bye awards three points.
func Standings(teams []Team, matches []Match, leagueID, stageID uint) []Standing {
	participants := map[uint]bool{}
	if stageID != 0 {
		for _, m := range matches {
			if !m.IsPlaceholder && m.Team1ID != 0 && m.LeagueID == leagueID && m.StageID == stageID {
				participants[m.Team1ID] = true
				if m.Team2ID != nil {
					participants[*m.Team2ID] = true
				}
			}
		}
	}
	rows := make(map[uint]*Standing)
	for _, t := range teams {
		if t.LeagueID == leagueID && (len(participants) == 0 || participants[t.ID]) {
			rows[t.ID] = &Standing{TeamID: t.ID, LeagueID: leagueID, StageID: stageID}
		}
	}
	for _, m := range matches {
		if m.IsPlaceholder || m.Team1ID == 0 || m.LeagueID != leagueID || (stageID != 0 && m.StageID != stageID) || m.Status != "approved" {
			continue
		}
		a := rows[m.Team1ID]
		if a == nil {
			continue
		}
		if m.Team2ID == nil {
			if !m.IsBye {
				continue
			}
			a.Points += 3
			a.Wins++
			a.Played++
			continue
		}
		b := rows[*m.Team2ID]
		if b == nil {
			continue
		}
		a.Played++
		b.Played++
		a.Scored += m.Goals1
		a.Conceded += m.Goals2
		b.Scored += m.Goals2
		b.Conceded += m.Goals1
		switch {
		case m.Goals1 > m.Goals2:
			a.Wins++
			a.Points += 3
			b.Losses++
		case m.Goals2 > m.Goals1:
			b.Wins++
			b.Points += 3
			a.Losses++
		default:
			a.Draws++
			b.Draws++
			a.Points++
			b.Points++
		}
	}
	out := make([]Standing, 0, len(rows))
	for _, row := range rows {
		row.Difference = row.Scored - row.Conceded
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Points != b.Points {
			return a.Points > b.Points
		}
		if a.Difference != b.Difference {
			return a.Difference > b.Difference
		}
		if a.Scored != b.Scored {
			return a.Scored > b.Scored
		}
		return a.TeamID < b.TeamID
	})
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}

type Pair struct {
	A     uint
	B     *uint
	Round int
}

func RoundRobin(ids []uint) []Pair {
	n := len(ids)
	if n < 2 {
		return nil
	}
	ring := append([]uint(nil), ids...)
	if n%2 == 1 {
		ring = append(ring, 0)
	}
	n = len(ring)
	out := []Pair{}
	for round := 1; round < n; round++ {
		for i := 0; i < n/2; i++ {
			a, b := ring[i], ring[n-1-i]
			if a != 0 && b != 0 {
				if round%2 == 0 {
					a, b = b, a
				}
				bb := b
				out = append(out, Pair{a, &bb, round})
			}
		}
		last := ring[n-1]
		copy(ring[2:], ring[1:n-1])
		ring[1] = last
	}
	return out
}

// SwissPairs uses rank order and backtracking to avoid rematches. The lowest
// ranked team without a previous bye receives the bye. Exhausted pairings fail
// explicitly rather than silently creating a repeat opponent.
func SwissPairs(ranked []uint, previous []Match, round int) ([]Pair, error) {
	ids := append([]uint(nil), ranked...)
	met := map[[2]uint]bool{}
	byes := map[uint]bool{}
	for _, m := range previous {
		if m.IsPlaceholder || m.Team1ID == 0 {
			continue
		}
		if m.Team2ID == nil {
			byes[m.Team1ID] = true
			continue
		}
		a, b := m.Team1ID, *m.Team2ID
		if a > b {
			a, b = b, a
		}
		met[[2]uint{a, b}] = true
	}
	out := []Pair{}
	if len(ids)%2 == 1 {
		idx := -1
		for i := len(ids) - 1; i >= 0; i-- {
			if !byes[ids[i]] {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, errors.New("all teams have already received a bye")
		}
		out = append(out, Pair{ids[idx], nil, round})
		ids = append(ids[:idx], ids[idx+1:]...)
	}
	nodes := 0
	var pair func([]uint) ([]Pair, bool)
	pair = func(left []uint) ([]Pair, bool) {
		nodes++
		if nodes > 200000 {
			return nil, false
		}
		if len(left) == 0 {
			return []Pair{}, true
		}
		a := left[0]
		for i := 1; i < len(left); i++ {
			b := left[i]
			x, y := a, b
			if x > y {
				x, y = y, x
			}
			if met[[2]uint{x, y}] {
				continue
			}
			rest := append([]uint{}, left[1:i]...)
			rest = append(rest, left[i+1:]...)
			tail, ok := pair(rest)
			if ok {
				bb := b
				return append([]Pair{{a, &bb, round}}, tail...), true
			}
		}
		return nil, false
	}
	pairs, ok := pair(ids)
	if !ok {
		return nil, errors.New("no Swiss pairing without rematches is available")
	}
	return append(out, pairs...), nil
}
func KnockoutPairs(ranked []uint, round int) []Pair {
	out := []Pair{}
	if len(ranked) < 2 {
		return out
	}
	size := 1
	for size < len(ranked) {
		size *= 2
	}
	byes := size - len(ranked)
	for i := 0; i < byes; i++ {
		out = append(out, Pair{ranked[i], nil, round})
	}
	rest := ranked[byes:]
	for i := 0; i < len(rest)/2; i++ {
		b := rest[len(rest)-1-i]
		out = append(out, Pair{rest[i], &b, round})
	}
	return out
}
