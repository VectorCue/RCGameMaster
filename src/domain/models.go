package domain

import (
	"encoding/json"
	"time"
)

type Tournament struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name"`
	Abbrev    string    `json:"abbrev" gorm:"uniqueIndex"`
	StartDate string    `json:"start_date"`
	EndDate   string    `json:"end_date"`
	Timezone  string    `json:"timezone"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type League struct {
	ID                 uint    `json:"id" gorm:"primaryKey"`
	TournamentID       uint    `json:"tournament_id" gorm:"index;uniqueIndex:league_abbrev"`
	ParentID           *uint   `json:"parent_id" gorm:"index"`
	Name               string  `json:"name"`
	Abbrev             string  `json:"abbrev" gorm:"uniqueIndex:league_abbrev"`
	Mode               string  `json:"mode"`
	RefereeMode        bool    `json:"referee_mode"`
	InspectionsEnabled *bool   `json:"inspections_enabled" gorm:"not null;default:true"`
	RobotCount         int     `json:"robot_count"`
	MaxWeightG         float64 `json:"max_weight_g"`
	BallZoneCM         float64 `json:"ball_zone_cm"`
	Voltage            float64 `json:"voltage"`
	SizeCM             float64 `json:"size_cm"`
	MotorLimit         int     `json:"motor_limit"`
	HalfSeconds        int     `json:"half_seconds"`
	HalftimeSeconds    int     `json:"halftime_seconds"`
}

// InspectionsRequired treats old fixtures and omitted configuration as enabled.
func (l League) InspectionsRequired() bool {
	return l.InspectionsEnabled == nil || *l.InspectionsEnabled
}

type Stage struct {
	TeamLimit     int    `json:"team_limit"`
	ID            uint   `json:"id" gorm:"primaryKey"`
	TournamentID  uint   `json:"tournament_id" gorm:"index"`
	LeagueID      uint   `json:"league_id" gorm:"index"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	SortOrder     int    `json:"sort_order"`
	SourceStageID *uint  `json:"source_stage_id"`
	Round         int    `json:"round"`
}
type Field struct {
	ID           uint   `json:"id" gorm:"primaryKey"`
	TournamentID uint   `json:"tournament_id" gorm:"index"`
	LeagueID     uint   `json:"league_id" gorm:"index"`
	Name         string `json:"name"`
}
type Slot struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	TournamentID uint      `json:"tournament_id" gorm:"index"`
	FieldID      uint      `json:"field_id" gorm:"index"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
}
type Team struct {
	ID           uint   `json:"id" gorm:"primaryKey"`
	TournamentID uint   `json:"tournament_id" gorm:"index"`
	LeagueID     uint   `json:"league_id" gorm:"index"`
	Name         string `json:"name"`
	Affiliation  string `json:"affiliation"`
	Startnumber  int    `json:"startnumber"`
	ExternalKey  string `json:"external_key"`
}
type Referee struct {
	ID             uint       `json:"id" gorm:"primaryKey"`
	TournamentID   uint       `json:"tournament_id" gorm:"index"`
	Name           string     `json:"name"`
	UserID         string     `json:"user_id,omitempty"`
	AvailableFrom  *time.Time `json:"available_from"`
	AvailableUntil *time.Time `json:"available_until"`
}
type Match struct {
	IsBye           bool            `json:"is_bye"`
	ID              uint            `json:"id" gorm:"primaryKey"`
	TournamentID    uint            `json:"tournament_id" gorm:"index;uniqueIndex:match_number"`
	LeagueID        uint            `json:"league_id" gorm:"index"`
	StageID         uint            `json:"stage_id" gorm:"index"`
	Number          int             `json:"number" gorm:"uniqueIndex:match_number"`
	Round           int             `json:"round"`
	Team1ID         uint            `json:"team1_id" gorm:"index"`
	Team2ID         *uint           `json:"team2_id" gorm:"index"`
	FieldID         *uint           `json:"field_id" gorm:"index"`
	SlotID          *uint           `json:"slot_id" gorm:"index"`
	ScheduledAt     *time.Time      `json:"scheduled_at" gorm:"index"`
	DurationMinutes int             `json:"duration_minutes"`
	RefereeIDs      []uint          `json:"referee_ids" gorm:"serializer:json;type:text"`
	Goals1          int             `json:"goals1"`
	Goals2          int             `json:"goals2"`
	Status          string          `json:"status" gorm:"index"`
	Published       bool            `json:"published"`
	Notes           string          `json:"notes"`
	LiveState       json.RawMessage `json:"live_state" gorm:"type:jsonb"`
	UpdatedAt       time.Time       `json:"updated_at"`
	CreatedAt       time.Time       `json:"created_at"`
}
type Standing struct {
	LeagueID   uint `json:"league_id"`
	StageID    uint `json:"stage_id"`
	TeamID     uint `json:"team_id"`
	Played     int  `json:"played"`
	Wins       int  `json:"wins"`
	Draws      int  `json:"draws"`
	Losses     int  `json:"losses"`
	Scored     int  `json:"scored"`
	Conceded   int  `json:"conceded"`
	Difference int  `json:"difference"`
	Points     int  `json:"points"`
	Rank       int  `json:"rank"`
}
type Workspace struct {
	Tournament Tournament `json:"tournament"`
	Leagues    []League   `json:"leagues"`
	Stages     []Stage    `json:"stages"`
	Fields     []Field    `json:"fields"`
	Slots      []Slot     `json:"slots"`
	Teams      []Team     `json:"teams"`
	Referees   []Referee  `json:"referees"`
	Matches    []Match    `json:"matches"`
	Standings  []Standing `json:"standings"`
}

func Models() []any {
	return []any{&Tournament{}, &League{}, &Stage{}, &Field{}, &Slot{}, &Team{}, &Referee{}, &Match{}}
}
