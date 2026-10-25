package domain

import "time"

// MatchEvent records the state at the time an accepted referee action occurred.
// It deliberately excludes free-text referee notes and signer information.
type MatchEvent struct {
	ID               uint      `json:"id"`
	At               time.Time `json:"at"`
	Type             string    `json:"type"`
	Phase            string    `json:"phase"`
	RemainingSeconds int       `json:"remaining_seconds"`
	TeamID           uint      `json:"team_id,omitempty"`
	Robot            int       `json:"robot,omitempty"`
	Side             string    `json:"side,omitempty"`
	NextPhase        string    `json:"next_phase,omitempty"`
	Forced           bool      `json:"forced,omitempty"`
}
