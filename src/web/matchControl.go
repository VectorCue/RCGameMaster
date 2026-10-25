package web

import (
	"RCJVGameMaster/src/domain"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RobotCheck struct {
	TeamID   uint    `json:"team_id"`
	Robot    int     `json:"robot"`
	WeightG  float64 `json:"weight_g"`
	BallZone bool    `json:"ball_zone"`
	Voltage  bool    `json:"voltage"`
	Size     bool    `json:"size"`
	Motors   bool    `json:"motors"`
}

type MatchSignature struct {
	Kind      string `json:"kind"`
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Signature string `json:"signature"`
}

type RobotTimeout struct {
	TeamID uint      `json:"team_id"`
	Robot  int       `json:"robot"`
	EndsAt time.Time `json:"ends_at"`
}

type LiveState struct {
	Checks           []RobotCheck     `json:"checks"`
	Phase            string           `json:"phase"`
	RemainingSeconds int              `json:"remaining_seconds"`
	Running          bool             `json:"running"`
	StartedAt        *time.Time       `json:"started_at"`
	BlueTeamID       *uint            `json:"blue_team_id"`
	KickoffTeamID    *uint            `json:"kickoff_team_id"`
	Timeouts         []RobotTimeout   `json:"timeouts"`
	Signatures       []MatchSignature `json:"signatures"`
	SignedGoals1     *int             `json:"signed_goals1,omitempty"`
	SignedGoals2     *int             `json:"signed_goals2,omitempty"`
}

type matchCommand struct {
	Action        string           `json:"action"`
	Checks        []RobotCheck     `json:"checks"`
	BlueTeamID    uint             `json:"blue_team_id"`
	KickoffTeamID uint             `json:"kickoff_team_id"`
	TeamID        uint             `json:"team_id"`
	Robot         int              `json:"robot"`
	Delta         int              `json:"delta"`
	Signatures    []MatchSignature `json:"signatures"`
	Notes         string           `json:"notes"`
}

func (a *API) registerMatchRoutes(v1 fiber.Router) {
	v1.Get("/matches/:id", a.getMatch)
	v1.Post("/matches/:id/control", requireRoles("admin", "editor", "referee"), a.controlMatch)
}

func (a *API) getMatch(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid match ID."})
	}
	var match domain.Match
	if err := a.DB.First(&match, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"error": "Match not found."})
		}
		return c.Status(500).JSON(fiber.Map{"error": "Could not load match."})
	}
	state, err := readLiveState(match.LiveState)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not load match state."})
	}
	match.LiveState, _ = json.Marshal(state)
	return c.JSON(match)
}

func (a *API) controlMatch(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid match ID."})
	}
	var command matchCommand
	if err := c.Bind().Body(&command); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid match command."})
	}
	user, _ := c.Locals("user").(User)
	var match domain.Match
	err = a.DB.Transaction(func(tx *gorm.DB) error {
		// Use the same tournament lock as scheduling/review to serialize changes
		// across those operations, then reload the authoritative match state.
		if err := tx.First(&match, id).Error; err != nil {
			return err
		}
		if err := lockTournament(tx, match.TournamentID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&match, id).Error; err != nil {
			return err
		}
		if !hasRole(user, "admin", "editor") {
			var refs []domain.Referee
			if err := tx.Where("tournament_id = ? AND user_id = ?", match.TournamentID, user.ID).Find(&refs).Error; err != nil {
				return err
			}
			assigned := false
			for _, ref := range refs {
				if slices.Contains(match.RefereeIDs, ref.ID) {
					assigned = true
					break
				}
			}
			if !assigned {
				return fiber.NewError(403, "Only a referee assigned to this match can control it. Ask an admin to link your account and assignment.")
			}
		}
		var league domain.League
		if err := tx.First(&league, match.LeagueID).Error; err != nil {
			return err
		}
		if league.Mode != "full" || !league.RefereeMode {
			return fiber.NewError(409, "Referee mode is disabled for this league.")
		}
		if match.Team2ID == nil {
			return fiber.NewError(409, "A bye cannot be refereed.")
		}
		if match.Status == "approved" || match.Status == "submitted" {
			return fiber.NewError(409, "This result is locked for review.")
		}
		state, err := readLiveState(match.LiveState)
		if err != nil {
			return err
		}
		if err := applyMatchCommand(&match, &league, &state, command, time.Now().UTC()); err != nil {
			return fiber.NewError(409, err.Error())
		}
		match.LiveState, err = json.Marshal(state)
		if err != nil {
			return err
		}
		return tx.Save(&match).Error
	})
	if err != nil {
		var ferr *fiber.Error
		if errors.As(err, &ferr) {
			return c.Status(ferr.Code).JSON(fiber.Map{"error": ferr.Message})
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"error": "Match or league not found."})
		}
		requestLogger(c.Context()).Error("match.control_failed", "match_id", id, "error", err)
		return c.Status(500).JSON(fiber.Map{"error": "Could not save match. Please try again."})
	}
	return c.JSON(match)
}

func readLiveState(raw json.RawMessage) (LiveState, error) {
	state := LiveState{Phase: "pregame", Checks: []RobotCheck{}, Timeouts: []RobotTimeout{}, Signatures: []MatchSignature{}}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &state); err != nil {
			return state, err
		}
	}
	if state.Phase == "" {
		state.Phase = "pregame"
	}
	return state, nil
}

func matchTeam(match *domain.Match, id uint) bool {
	return id != 0 && (id == match.Team1ID || (match.Team2ID != nil && id == *match.Team2ID))
}

func validChecks(match *domain.Match, league *domain.League, checks []RobotCheck) error {
	if !league.InspectionsRequired() {
		return nil
	}
	if league.RobotCount < 1 || league.RobotCount > 20 {
		return errors.New("Configure between 1 and 20 robots per team first.")
	}
	if len(checks) != league.RobotCount*2 {
		return errors.New("Complete the inspection for every robot on both teams.")
	}
	seen := map[[2]uint]bool{}
	for _, check := range checks {
		key := [2]uint{check.TeamID, uint(check.Robot)}
		if !matchTeam(match, check.TeamID) || check.Robot < 1 || check.Robot > league.RobotCount || seen[key] {
			return errors.New("Each team robot must have exactly one inspection.")
		}
		seen[key] = true
		if math.IsNaN(check.WeightG) || math.IsInf(check.WeightG, 0) || check.WeightG <= 0 || (league.MaxWeightG > 0 && check.WeightG > league.MaxWeightG) {
			return fmt.Errorf("Robot %d must have a valid weight within its league limit.", check.Robot)
		}
		if !check.BallZone || !check.Voltage || !check.Size || !check.Motors {
			return errors.New("All inspection checkboxes must pass before setup.")
		}
	}
	return nil
}

func clockRemaining(state *LiveState, now time.Time) int {
	remaining := state.RemainingSeconds
	if state.Running && state.StartedAt != nil {
		elapsed := int(now.Sub(*state.StartedAt).Seconds())
		if elapsed > 0 {
			remaining -= elapsed
		}
	}
	return max(0, remaining)
}

func applyMatchCommand(match *domain.Match, league *domain.League, state *LiveState, cmd matchCommand, now time.Time) error {
	switch cmd.Action {
	case "checks":
		if !league.InspectionsRequired() {
			return errors.New("Robot inspections are disabled for this sub-league.")
		}
		if state.Phase != "pregame" && state.Phase != "ready" {
			return errors.New("Inspections are locked after kickoff.")
		}
		// Saving partial inspections is allowed; setup validates every robot.
		if len(cmd.Checks) > league.RobotCount*2 {
			return errors.New("Too many robot inspections.")
		}
		seen := map[[2]uint]bool{}
		for _, check := range cmd.Checks {
			key := [2]uint{check.TeamID, uint(check.Robot)}
			if !matchTeam(match, check.TeamID) || check.Robot < 1 || check.Robot > league.RobotCount || seen[key] || check.WeightG < 0 || math.IsNaN(check.WeightG) || math.IsInf(check.WeightG, 0) {
				return errors.New("Invalid robot inspection.")
			}
			seen[key] = true
		}
		state.Checks = cmd.Checks
		state.Phase = "pregame"
		state.BlueTeamID, state.KickoffTeamID = nil, nil
	case "setup":
		if state.Phase != "pregame" && state.Phase != "ready" {
			return errors.New("Sides and kickoff are locked after the match starts.")
		}
		if err := validChecks(match, league, state.Checks); err != nil {
			return err
		}
		if !league.InspectionsRequired() {
			state.Checks = []RobotCheck{}
		}
		if !matchTeam(match, cmd.BlueTeamID) || !matchTeam(match, cmd.KickoffTeamID) {
			return errors.New("Select a team for the blue side and kickoff.")
		}
		if league.HalfSeconds <= 0 || league.HalftimeSeconds < 0 {
			return errors.New("Configure valid half and halftime durations first.")
		}
		state.BlueTeamID, state.KickoffTeamID = &cmd.BlueTeamID, &cmd.KickoffTeamID
		state.Phase, state.RemainingSeconds = "ready", league.HalfSeconds
		state.Running, state.StartedAt = false, nil
	case "start":
		if state.Running {
			return errors.New("The clock is already running.")
		}
		if state.Phase == "ready" {
			if len(match.RefereeIDs) == 0 {
				return errors.New("Assign at least one referee in the schedule before starting the match.")
			}
			if err := validChecks(match, league, state.Checks); err != nil {
				return err
			}
			// A pre-start settings edit must not retain an older clock duration.
			state.RemainingSeconds = league.HalfSeconds
			state.Phase = "first_half"
			match.Status = "in_progress"
		} else if state.Phase != "first_half" && state.Phase != "halftime" && state.Phase != "second_half" {
			if !league.InspectionsRequired() {
				return errors.New("Choose sides and kickoff before starting.")
			}
			return errors.New("Complete the inspection and setup before starting.")
		}
		if state.RemainingSeconds <= 0 {
			return errors.New("This period is finished. Advance to the next period.")
		}
		state.Running, state.StartedAt = true, &now
	case "pause":
		if !state.Running {
			return errors.New("The clock is already paused.")
		}
		state.RemainingSeconds = clockRemaining(state, now)
		state.Running, state.StartedAt = false, nil
	case "advance":
		if clockRemaining(state, now) > 0 {
			return errors.New("Wait until this period ends before advancing.")
		}
		state.Running, state.StartedAt = false, nil
		switch state.Phase {
		case "first_half":
			state.Phase, state.RemainingSeconds = "halftime", league.HalftimeSeconds
			// The tap acknowledging the half starts the break immediately.
			if league.HalftimeSeconds > 0 {
				state.Running, state.StartedAt = true, &now
			}
		case "halftime":
			state.Phase, state.RemainingSeconds = "second_half", league.HalfSeconds
			state.BlueTeamID = otherTeam(match, state.BlueTeamID)
			state.KickoffTeamID = otherTeam(match, state.KickoffTeamID)
		case "second_half":
			state.Phase, state.RemainingSeconds = "finished", 0
		default:
			return errors.New("There is no completed period to advance.")
		}
	case "goal":
		if state.Phase != "first_half" && state.Phase != "second_half" && state.Phase != "finished" {
			return errors.New("Goals can only be changed during or after play.")
		}
		if !matchTeam(match, cmd.TeamID) || (cmd.Delta != 1 && cmd.Delta != -1) {
			return errors.New("Choose a team and a goal change of +1 or -1.")
		}
		if cmd.TeamID == match.Team1ID {
			if match.Goals1+cmd.Delta < 0 {
				return errors.New("A score cannot be negative.")
			}
			match.Goals1 += cmd.Delta
		} else {
			if match.Goals2+cmd.Delta < 0 {
				return errors.New("A score cannot be negative.")
			}
			match.Goals2 += cmd.Delta
		}
		state.Signatures = []MatchSignature{}
		state.SignedGoals1, state.SignedGoals2 = nil, nil
	case "timeout":
		if state.Phase != "first_half" && state.Phase != "second_half" {
			return errors.New("Robot timeouts are available during play.")
		}
		if !matchTeam(match, cmd.TeamID) || cmd.Robot < 1 || cmd.Robot > league.RobotCount {
			return errors.New("Choose a robot belonging to a team in this match.")
		}
		timeout := RobotTimeout{TeamID: cmd.TeamID, Robot: cmd.Robot, EndsAt: now.Add(time.Minute)}
		found := false
		for i, item := range state.Timeouts {
			if item.TeamID == cmd.TeamID && item.Robot == cmd.Robot {
				state.Timeouts[i] = timeout
				found = true
				break
			}
		}
		if !found {
			state.Timeouts = append(state.Timeouts, timeout)
		}
	case "submit":
		if state.Phase != "finished" {
			return errors.New("Finish both halves before submitting the result.")
		}
		if len(match.RefereeIDs) == 0 {
			return errors.New("Assign at least one referee before submitting for review.")
		}
		if err := validateSignatures(match, cmd.Signatures); err != nil {
			return err
		}
		if len(cmd.Notes) > 10000 {
			return errors.New("Comments must be fewer than 10,000 characters.")
		}
		state.Signatures, match.Notes = cmd.Signatures, strings.TrimSpace(cmd.Notes)
		signedGoals1, signedGoals2 := match.Goals1, match.Goals2
		state.SignedGoals1, state.SignedGoals2 = &signedGoals1, &signedGoals2
		match.Status = "submitted"
	default:
		return errors.New("Unknown match command.")
	}
	return nil
}

func otherTeam(match *domain.Match, current *uint) *uint {
	if current == nil || match.Team2ID == nil {
		return nil
	}
	if *current == match.Team1ID {
		id := *match.Team2ID
		return &id
	}
	id := match.Team1ID
	return &id
}

func validateSignatures(match *domain.Match, signatures []MatchSignature) error {
	if len(signatures) != 2+len(match.RefereeIDs) {
		return errors.New("Both teams and every assigned referee must sign.")
	}
	seen := map[string]bool{}
	for _, sig := range signatures {
		key := fmt.Sprintf("%s:%d", sig.Kind, sig.ID)
		if seen[key] || strings.TrimSpace(sig.Name) == "" || len(sig.Name) > 200 {
			return errors.New("Each required signer must provide their name and signature once.")
		}
		seen[key] = true
		if (sig.Kind == "team" && !matchTeam(match, sig.ID)) || (sig.Kind == "referee" && !slices.Contains(match.RefereeIDs, sig.ID)) || (sig.Kind != "team" && sig.Kind != "referee") {
			return errors.New("A signature belongs to an unknown team or referee.")
		}
		if !strings.HasPrefix(sig.Signature, "data:image/png;base64,") || len(sig.Signature) > 400000 {
			return errors.New("Provide a drawn PNG signature for each signer.")
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sig.Signature, "data:image/png;base64,"))
		if err != nil {
			return errors.New("Invalid signature image.")
		}
		imageCfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil || imageCfg.Width < 1 || imageCfg.Height < 1 || imageCfg.Width > 4096 || imageCfg.Height > 2048 {
			return errors.New("Invalid signature image dimensions.")
		}
	}
	return nil
}
