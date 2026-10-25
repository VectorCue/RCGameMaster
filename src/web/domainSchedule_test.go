package web

import (
	"RCJVGameMaster/src/domain"
	"testing"
	"time"
)

func TestScheduleIntervalsAreHalfOpen(t *testing.T) {
	start := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		offset int
		want   bool
	}{{-30, false}, {-29, true}, {0, true}, {29, true}, {30, false}}
	for _, tt := range tests {
		if got := intervalsOverlap(start, start.Add(time.Duration(tt.offset)*time.Minute), 30, 30); got != tt.want {
			t.Errorf("offset %d overlap %v want %v", tt.offset, got, tt.want)
		}
	}
}
func TestScheduleRecognizesEitherTeamAndSharedMainFields(t *testing.T) {
	a, b, c := uint(1), uint(2), uint(3)
	match := domain.Match{Team1ID: a, Team2ID: &b}
	for _, other := range []domain.Match{{Team1ID: a, Team2ID: &c}, {Team1ID: c, Team2ID: &a}, {Team1ID: b, Team2ID: &c}, {Team1ID: c, Team2ID: &b}} {
		if !sharesTeam(match, other) {
			t.Fatal("missed team conflict")
		}
	}
	if sharesTeam(match, domain.Match{Team1ID: 4, Team2ID: &c}) {
		t.Fatal("false team conflict")
	}
	league := domain.League{ID: 2, ParentID: &a}
	if !fieldAllowsLeague(domain.Field{LeagueID: 1}, league) || !fieldAllowsLeague(domain.Field{LeagueID: 2}, league) || fieldAllowsLeague(domain.Field{LeagueID: 3}, league) {
		t.Fatal("field league inheritance is incorrect")
	}
}
func TestDomainRolesAreCaseInsensitive(t *testing.T) {
	user := User{Roles: []string{"Viewer", "Editor"}}
	if !hasRole(user, "editor") || hasRole(user, "admin") {
		t.Fatal("role comparison failed")
	}
}
