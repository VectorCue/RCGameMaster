package web

import (
	"RCJVGameMaster/src/domain"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

func (a *API) registerPublicRoutes(app *fiber.App) {
	public := app.Group("/api/public")
	public.Get("/tournaments", a.listTournaments)
	public.Get("/:abbrev", a.publicWorkspace)
	rest := app.Group("/rest/v1")
	rest.Use(func(c fiber.Ctx) error {
		c.Set("Access-Control-Allow-Origin", "*")
		c.Set("Cache-Control", "no-cache")
		return c.Next()
	})
	rest.Get("/:abbrev/leagues", a.compatLeagues)
	rest.Get("/:abbrev/matches", a.compatMatches)
	rest.Get("/:abbrev/standings", a.compatStandings)
}
func (a *API) publicSnapshot(c fiber.Ctx) (domain.Workspace, error) {
	var t domain.Tournament
	if e := a.DB.Where("abbrev = ?", c.Params("abbrev")).First(&t).Error; e != nil {
		return domain.Workspace{}, e
	}
	return loadWorkspace(a.DB, t.ID, true)
}
func (a *API) publicWorkspace(c fiber.Ctx) error {
	w, e := a.publicSnapshot(c)
	if e != nil {
		return apiError(c, 404, "Tournament not found")
	}
	c.Set("Cache-Control", "no-cache")
	return c.JSON(w)
}
func publicStages(w domain.Workspace, lid uint) []domain.Stage {
	out := []domain.Stage{}
	for _, s := range w.Stages {
		if s.LeagueID == lid {
			out = append(out, s)
		}
	}
	return out
}
func compatEnvelope(w domain.Workspace) fiber.Map {
	return fiber.Map{"tournament_abbrev": w.Tournament.Abbrev, "tournament_name": w.Tournament.Name, "tournament_publishing_timezone": w.Tournament.Timezone, "tournament_header_type": "TITLE"}
}
func latestPublished(w domain.Workspace, lid, sid uint) *time.Time {
	var latest time.Time
	for _, m := range w.Matches {
		if m.LeagueID == lid && (sid == 0 || m.StageID == sid) && m.Status == "approved" && m.UpdatedAt.After(latest) {
			latest = m.UpdatedAt
		}
	}
	if latest.IsZero() {
		return nil
	}
	return &latest
}
func (a *API) compatLeagues(c fiber.Ctx) error {
	w, e := a.publicSnapshot(c)
	if e != nil {
		return apiError(c, 404, "Tournament not found")
	}
	out := []fiber.Map{}
	for _, l := range w.Leagues {
		stages := publicStages(w, l.ID)
		if len(stages) == 0 {
			continue
		}
		if c.Query("format") == "openapi" {
			names := []string{}
			for _, s := range stages {
				names = append(names, s.Name)
			}
			out = append(out, fiber.Map{"abbrev": l.Abbrev, "name": l.Name, "league_stages": names})
			continue
		}
		entries := []fiber.Map{}
		for _, s := range stages {
			latest := latestPublished(w, l.ID, s.ID)
			state := "UNPUBLISHED"
			if latest != nil {
				state = "PUBLISHED"
			}
			entries = append(entries, fiber.Map{"name": s.Name, "standings_publishing_state": state, "standings_last_published": compatTime(latest, w.Tournament.Timezone)})
		}
		latest := latestPublished(w, l.ID, 0)
		state := "UNPUBLISHED"
		if latest != nil {
			state = "PUBLISHED"
		}
		out = append(out, fiber.Map{"abbrev": l.Abbrev, "name": l.Name, "league_stages": entries, "seeding_publishing_state": state, "seeding_last_published": compatTime(latest, w.Tournament.Timezone)})
	}
	if c.Query("format") == "openapi" {
		return c.JSON(out)
	}
	result := compatEnvelope(w)
	result["leagues"] = out
	return c.JSON(result)
}
func compatFilter(c fiber.Ctx, w domain.Workspace, requireLeague bool) ([]domain.League, *domain.Stage, error) {
	var leagues []domain.League
	for _, l := range w.Leagues {
		if c.Query("league") == "" || l.Abbrev == c.Query("league") {
			leagues = append(leagues, l)
		}
	}
	if (requireLeague && c.Query("league") == "") || len(leagues) == 0 {
		return nil, nil, fmt.Errorf("league not found")
	}
	var stage *domain.Stage
	if raw := c.Query("league_stage"); raw != "" {
		if c.Query("league") == "" {
			return nil, nil, fmt.Errorf("league is required when selecting a stage")
		}
		index, e := strconv.Atoi(raw)
		if e != nil || index < 0 {
			return nil, nil, fmt.Errorf("invalid stage index")
		}
		stages := publicStages(w, leagues[0].ID)
		if index >= len(stages) {
			return nil, nil, fmt.Errorf("stage not found")
		}
		stage = &stages[index]
	}
	return leagues, stage, nil
}
func externalTeam(w domain.Workspace, id uint) fiber.Map {
	for _, t := range w.Teams {
		if t.ID == id {
			return fiber.Map{"id": t.ID, "name": t.Name, "affiliation": t.Affiliation, "startnumber": t.Startnumber, "external_key": t.ExternalKey}
		}
	}
	return fiber.Map{"name": "BYE", "affiliation": "", "startnumber": 0, "external_key": ""}
}
func compatTime(t *time.Time, zone string) string {
	if t == nil {
		return ""
	}
	loc, e := time.LoadLocation(zone)
	if e != nil {
		loc = time.UTC
	}
	return t.In(loc).Format("2006-01-02 15:04:05")
}
func externalMatch(w domain.Workspace, m domain.Match, l domain.League, s domain.Stage) fiber.Map {
	team2 := fiber.Map{"name": "BYE", "affiliation": "", "startnumber": 0, "external_key": ""}
	if m.Team2ID != nil {
		team2 = externalTeam(w, *m.Team2ID)
	}
	refs := []fiber.Map{}
	for _, id := range m.RefereeIDs {
		for _, r := range w.Referees {
			if r.ID == id {
				parts := strings.Fields(r.Name)
				first, last := "", ""
				if len(parts) > 0 {
					first = parts[0]
					last = strings.Join(parts[1:], " ")
				}
				refs = append(refs, fiber.Map{"first_name": first, "last_name": last})
			}
		}
	}
	pitch := ""
	if m.FieldID != nil {
		for _, f := range w.Fields {
			if f.ID == *m.FieldID {
				pitch = f.Name
			}
		}
	}
	g1, g2, p1, p2 := 0, 0, 0, 0
	if m.Status == "approved" {
		g1, g2 = m.Goals1, m.Goals2
		switch {
		case m.Team2ID == nil && m.IsBye || g1 > g2:
			p1 = 3
		case g2 > g1:
			p2 = 3
		default:
			if m.Team2ID == nil {
				break
			}
			p1 = 1
			p2 = 1
		}
	}
	duration := m.DurationMinutes * 60
	return fiber.Map{"number": m.Number, "team1": externalTeam(w, m.Team1ID), "team2": team2, "league": l.Name, "league_stage": s.Name, "group_name": "Group 1", "start": compatTime(m.ScheduledAt, w.Tournament.Timezone), "duration": fmt.Sprintf("%02d:%02d:%02d", duration/3600, duration/60%60, duration%60), "pitch": pitch, "goals1": g1, "goals2": g2, "points1": p1, "points2": p2, "referees": refs, "last_published": compatTime(&m.UpdatedAt, w.Tournament.Timezone)}
}
func (a *API) compatMatches(c fiber.Ctx) error {
	w, e := a.publicSnapshot(c)
	if e != nil {
		return apiError(c, 404, "Tournament not found")
	}
	leagues, filter, e := compatFilter(c, w, false)
	if e != nil {
		return apiError(c, 404, e.Error())
	}
	out := []fiber.Map{}
	flat := []fiber.Map{}
	for _, l := range leagues {
		for _, s := range publicStages(w, l.ID) {
			if filter != nil && s.ID != filter.ID {
				continue
			}
			matches := []fiber.Map{}
			for _, m := range w.Matches {
				if m.LeagueID == l.ID && m.StageID == s.ID {
					entry := externalMatch(w, m, l, s)
					matches = append(matches, entry)
					flat = append(flat, entry)
				}
			}
			if len(matches) > 0 {
				out = append(out, fiber.Map{"league_name": l.Name, "league_stage_name": s.Name, "matches": matches})
			}
		}
	}
	if c.Query("format") == "openapi" {
		return c.JSON(out)
	}
	result := compatEnvelope(w)
	result["matches"] = flat
	if len(leagues) == 1 && c.Query("league") != "" {
		result["league_abbrev"] = leagues[0].Abbrev
		result["league_name"] = leagues[0].Name
	}
	if filter != nil {
		result["league_stage_name"] = filter.Name
		result["league_stage_number"] = c.Query("league_stage")
	}
	return c.JSON(result)
}
func (a *API) compatStandings(c fiber.Ctx) error {
	w, e := a.publicSnapshot(c)
	if e != nil {
		return apiError(c, 404, "Tournament not found")
	}
	leagues, stage, e := compatFilter(c, w, true)
	if e != nil {
		return apiError(c, 404, e.Error())
	}
	l := leagues[0]
	sid := uint(0)
	name := "Seeding"
	if stage != nil {
		sid = stage.ID
		name = stage.Name
	}
	rows := domain.Standings(w.Teams, w.Matches, l.ID, sid)
	entries := []fiber.Map{}
	var latest time.Time
	for _, m := range w.Matches {
		if m.LeagueID == l.ID && (sid == 0 || m.StageID == sid) && m.Status == "approved" && m.UpdatedAt.After(latest) {
			latest = m.UpdatedAt
		}
	}
	for _, r := range rows {
		entries = append(entries, fiber.Map{"team": externalTeam(w, r.TeamID), "group": 1, "wins": r.Wins, "draws": r.Draws, "losses": r.Losses, "points": r.Points, "scored": r.Scored, "conceded": r.Conceded, "difference": r.Difference, "rank": r.Rank, "qualification": "UNDEFINED", "last_published": compatTime(&latest, w.Tournament.Timezone)})
	}
	result := compatEnvelope(w)
	result["league_abbrev"] = l.Abbrev
	result["league_name"] = l.Name
	result["league_stage_number"] = c.Query("league_stage")
	result["league_stage_name"] = name
	result["last_published"] = compatTime(latestPublished(w, l.ID, sid), w.Tournament.Timezone)
	result["standings"] = entries
	if c.Query("format") == "openapi" {
		return c.JSON([]fiber.Map{result})
	}
	return c.JSON(result)
}
