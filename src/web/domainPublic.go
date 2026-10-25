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
	rest.Get("/openapi.yaml", getRESTOpenAPI)
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
	c.Set("X-Server-Time", time.Now().UTC().Format(time.RFC3339Nano))
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
	var publication *domain.StandingPublication
	if sid == 0 {
		publication = latestLeagueStandingPublication(w.Stages, w.StandingHistory, lid)
	} else {
		publication = findStandingPublication(w.StandingHistory, lid, sid)
	}
	if publication == nil {
		return nil
	}
	stamp := publication.PublishedAt
	return &stamp
}
func (a *API) compatLeagues(c fiber.Ctx) error {
	w, err := a.publicSnapshot(c)
	if err != nil {
		return apiError(c, 404, "Tournament not found")
	}
	// Stage indexes come from these full arrays. Catalog league_stage is intentionally ignored.
	selection, err := selectCompatLeagues(c, w, false, false)
	if err != nil {
		return apiError(c, 404, err.Error())
	}
	openapi := c.Query("format") == "openapi"
	leagues := []fiber.Map{}
	for _, league := range selection.Leagues {
		if len(publicStages(w, league.ID)) == 0 {
			continue
		}
		leagues = append(leagues, compatLeagueEntry(w, league, openapi))
	}
	if openapi {
		return c.JSON(leagues)
	}
	result := compatEnvelope(w)
	result["leagues"] = leagues
	result["main_leagues"] = compatMainCatalog(w, selection)
	applyCompatSelection(c, w, selection, result)
	return c.JSON(result)
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
	team1 := externalTeam(w, m.Team1ID)
	if m.IsPlaceholder {
		team1 = fiber.Map{"id": 0, "name": "Awaiting teams", "affiliation": "", "startnumber": 0, "external_key": ""}
		team2 = fiber.Map{"id": 0, "name": "Awaiting teams", "affiliation": "", "startnumber": 0, "external_key": ""}
	}
	if !m.IsPlaceholder && m.Team2ID == nil && !m.IsBye {
		team2 = nil
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
		if m.Team2ID == nil {
			if m.IsBye {
				p1 = 3
			}
		} else {
			switch {
			case g1 > g2:
				p1 = 3
			case g2 > g1:
				p2 = 3
			default:
				p1 = 1
				p2 = 1
			}
		}

	}
	duration := m.DurationMinutes * 60
	result := fiber.Map{"number": m.Number, "team1": team1, "team2": team2, "league": l.Name, "league_abbrev": l.Abbrev, "league_stage": s.Name, "stage_type": s.Type, "is_placeholder": m.IsPlaceholder, "is_bye": m.IsBye, "status": m.Status, "group_name": "Group 1", "start": compatTime(m.ScheduledAt, w.Tournament.Timezone), "duration": fmt.Sprintf("%02d:%02d:%02d", duration/3600, duration/60%60, duration%60), "pitch": pitch, "goals1": g1, "goals2": g2, "points1": p1, "points2": p2, "referees": refs, "last_published": compatTime(&m.UpdatedAt, w.Tournament.Timezone)}
	addCompatFields(result, compatHierarchyMetadata(w, l))
	return result
}
func (a *API) compatMatches(c fiber.Ctx) error {
	w, e := a.publicSnapshot(c)
	if e != nil {
		return apiError(c, 404, "Tournament not found")
	}
	selection, e := selectCompatLeagues(c, w, false, true)
	if e != nil {
		return apiError(c, 404, e.Error())
	}
	out := []fiber.Map{}
	flat := []fiber.Map{}
	for _, l := range selection.Leagues {
		for _, s := range publicStages(w, l.ID) {
			if selection.Stage != nil && s.ID != selection.Stage.ID {
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
				group := fiber.Map{"league_name": l.Name, "league_abbrev": l.Abbrev, "league_stage_name": s.Name, "stage_type": s.Type, "matches": matches}
				addCompatFields(group, compatHierarchyMetadata(w, l))
				out = append(out, group)
			}
		}
	}
	if c.Query("format") == "openapi" {
		return c.JSON(out)
	}
	result := compatEnvelope(w)
	result["matches"] = flat
	applyCompatSelection(c, w, selection, result)
	return c.JSON(result)
}
func (a *API) compatStandings(c fiber.Ctx) error {
	w, e := a.publicSnapshot(c)
	if e != nil {
		return apiError(c, 404, "Tournament not found")
	}
	selection, e := selectCompatLeagues(c, w, true, true)
	if e != nil {
		return apiError(c, 404, e.Error())
	}
	l := *selection.Exact
	sid := uint(0)
	name := "Seeding"
	if selection.Stage != nil {
		sid = selection.Stage.ID
		name = selection.Stage.Name
	}
	var publication *domain.StandingPublication
	if selection.Stage != nil {
		publication = findStandingPublication(w.StandingHistory, l.ID, sid)
	} else {
		publication = latestLeagueStandingPublication(w.Stages, w.StandingHistory, l.ID)
	}
	entries := []fiber.Map{}
	var stamp *time.Time
	state := "UNPUBLISHED"
	if publication != nil {
		state = "PUBLISHED"
		stamp = &publication.PublishedAt
		for _, row := range publication.Rows {
			entries = append(entries, fiber.Map{"team": externalTeam(w, row.TeamID), "group": 1, "wins": row.Wins, "draws": row.Draws, "losses": row.Losses, "points": row.Points, "scored": row.Scored, "conceded": row.Conceded, "difference": row.Difference, "rank": row.Rank, "qualification": "UNDEFINED", "last_published": compatTime(stamp, w.Tournament.Timezone)})
		}
	}
	result := compatEnvelope(w)
	result["league_abbrev"] = l.Abbrev
	result["league_name"] = l.Name
	result["league_stage_number"] = c.Query("league_stage")
	result["league_stage_name"] = name
	result["last_published"] = compatTime(stamp, w.Tournament.Timezone)
	result["standings_publishing_state"] = state
	result["standings"] = entries
	result["published_stage_id"] = nil
	result["published_stage_name"] = nil
	result["published_stage_number"] = nil
	if publication != nil {
		result["published_stage_id"] = publication.StageID
		for index, stage := range publicStages(w, l.ID) {
			if stage.ID == publication.StageID {
				result["published_stage_name"] = stage.Name
				result["published_stage_number"] = strconv.Itoa(index)
				break
			}
		}
	}
	applyCompatSelection(c, w, selection, result)
	if c.Query("format") == "openapi" {
		return c.JSON([]fiber.Map{result})
	}
	return c.JSON(result)
}
