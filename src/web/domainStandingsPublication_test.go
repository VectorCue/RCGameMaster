package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func standingsPublicationFixture(t *testing.T) (*gorm.DB, *fiber.App) {
	t.Helper()
	db, app := integrityFixture(t)
	if err := db.Model(&domain.League{}).Where("id = ?", 1).Update("robot_count", 2).Error; err != nil {
		t.Fatal(err)
	}
	for _, stage := range []domain.Stage{{ID: 1, TournamentID: 1, LeagueID: 1, Name: "First table", Type: "manual", SortOrder: 10}, {ID: 2, TournamentID: 1, LeagueID: 1, Name: "Second table", Type: "manual", SortOrder: 20}, {ID: 3, TournamentID: 1, LeagueID: 1, Name: "Future only byes", Type: "manual", SortOrder: 30}} {
		row := stage
		saveIntegrity(t, db, &row)
	}
	a := integrityMatch(1, 1, 2, "approved")
	a.Published = true
	a.Goals1 = 2
	a.UpdatedAt = time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)
	b := integrityMatch(2, 3, 4, "draft")
	b.Published = true
	c := integrityMatch(3, 1, 3, "draft")
	c.Published = true
	c.StageID = 2
	d := integrityMatch(4, 2, 4, "draft")
	d.Published = true
	d.StageID = 2
	bye := integrityMatch(5, 5, 0, "approved")
	bye.Published = true
	bye.IsBye = true
	bye.StageID = 3
	empty := domain.Match{TournamentID: 1, LeagueID: 1, StageID: 3, Number: 6, Round: 1, IsPlaceholder: true, Status: "draft", Published: false, DurationMinutes: 25, RefereeIDs: []uint{}, LiveState: []byte("{}")}
	saveIntegrity(t, db, &a, &b, &c, &d, &bye, &empty)
	return db, app
}
func publicationWorkspace(t *testing.T, app *fiber.App, public bool) domain.Workspace {
	t.Helper()
	path := "/api/v1/tournaments/1/workspace"
	if public {
		path = "/api/public/C"
	}
	var workspace domain.Workspace
	if err := json.Unmarshal(requestJSON(t, app, "GET", path, nil, 200), &workspace); err != nil {
		t.Fatal(err)
	}
	return workspace
}
func publishedTeamPoints(publication domain.StandingPublication, teamID uint) int {
	for _, row := range publication.Rows {
		if row.TeamID == teamID {
			return row.Points
		}
	}
	return 0
}
func setStandingsAuto(t *testing.T, app *fiber.App, leagueID uint, enabled bool) {
	t.Helper()
	requestJSON(t, app, "PUT", fmt.Sprintf("/api/v1/leagues/%d/standings-settings", leagueID), fiber.Map{"auto_publish": enabled}, 200)
}
func approveStandingResult(t *testing.T, app *fiber.App, matchID uint, goals1, goals2 int) {
	t.Helper()
	requestJSON(t, app, "POST", fmt.Sprintf("/api/v1/matches/%d/review", matchID), fiber.Map{"approve": true, "goals1": goals1, "goals2": goals2}, 200)
}
func TestStandingsAutoPublishesLatestEligibleStageByOrderNotEditTime(t *testing.T) {
	db, app := standingsPublicationFixture(t)
	public := publicationWorkspace(t, app, true)
	if len(public.PublishedStandings) != 1 || public.PublishedStandings[0].StageID != 1 {
		t.Fatal("empty future games or automatic byes promoted an unpublished table")
	}
	for _, row := range public.Standings {
		if row.StageID != 1 {
			t.Fatal("public workspace flattened multiple stage tables")
		}
	}
	requestJSON(t, app, "POST", "/api/v1/stages/3/standings/publish", fiber.Map{}, 409)
	approveStandingResult(t, app, 3, 1, 0)
	db.Model(&domain.Match{}).Where("id = ?", 1).UpdateColumn("updated_at", time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC))
	public = publicationWorkspace(t, app, true)
	if len(public.PublishedStandings) != 1 || public.PublishedStandings[0].StageID != 2 {
		t.Fatal("older stage's later edit replaced the latest stage table")
	}
	approveStandingResult(t, app, 4, 4, 0)
	public = publicationWorkspace(t, app, true)
	if publishedTeamPoints(public.PublishedStandings[0], 2) != 3 {
		t.Fatal("auto publication did not reflect the next approved result")
	}
	private := publicationWorkspace(t, app, false)
	if len(private.PublishedStandings) != 2 {
		t.Fatal("private workspace did not retain effective publication history")
	}
	// A completed individual run is a real eligible result despite no head-to-head points.
	individual := domain.Stage{ID: 4, TournamentID: 1, LeagueID: 1, Name: "Individual completion", Type: "individual", SortOrder: 40}
	run := integrityMatch(7, 5, 0, "draft")
	run.StageID = 4
	saveIntegrity(t, db, &individual, &run)
	approveStandingResult(t, app, run.ID, 0, 0)
	public = publicationWorkspace(t, app, true)
	if len(public.PublishedStandings) != 1 || public.PublishedStandings[0].StageID != 4 || len(public.PublishedStandings[0].Rows) != 1 || public.PublishedStandings[0].Rows[0].Points != 0 {
		t.Fatal("completed individual stage was not eligible or gained match points")
	}
}
func TestManualStandingsFreezeRepublishUnpublishAndAutoTransitions(t *testing.T) {
	_, app := standingsPublicationFixture(t)
	setStandingsAuto(t, app, 1, false)
	frozen := publicationWorkspace(t, app, true).PublishedStandings[0]
	approveStandingResult(t, app, 2, 4, 0)
	setStandingsAuto(t, app, 1, false)
	public := publicationWorkspace(t, app, true)
	if len(public.PublishedStandings) != 1 || publishedTeamPoints(public.PublishedStandings[0], 3) != 0 || !public.PublishedStandings[0].PublishedAt.Equal(frozen.PublishedAt) {
		t.Fatal("later approval or repeated manual-mode toggle changed frozen standings")
	}
	private := publicationWorkspace(t, app, false)
	livePoints := 0
	for _, row := range private.Standings {
		if row.StageID == 1 && row.TeamID == 3 {
			livePoints = row.Points
		}
	}
	if livePoints != 3 {
		t.Fatal("private live standings were frozen along with public table")
	}
	requestJSON(t, app, "POST", "/api/v1/stages/1/standings/publish", fiber.Map{}, 200)
	public = publicationWorkspace(t, app, true)
	if publishedTeamPoints(public.PublishedStandings[0], 3) != 3 {
		t.Fatal("manual republish did not publish newly reviewed results")
	}
	approveStandingResult(t, app, 3, 1, 0)
	public = publicationWorkspace(t, app, true)
	if public.PublishedStandings[0].StageID != 1 {
		t.Fatal("manual mode promoted an unrequested later stage")
	}
	setStandingsAuto(t, app, 1, true)
	public = publicationWorkspace(t, app, true)
	if public.PublishedStandings[0].StageID != 2 {
		t.Fatal("enabling auto did not select the latest eligible stage")
	}
	requestJSON(t, app, "DELETE", "/api/v1/stages/2/standings/publication", nil, 409)
	setStandingsAuto(t, app, 1, false)
	requestJSON(t, app, "POST", "/api/v1/stages/1/standings/publish", fiber.Map{}, 200)
	if publicationWorkspace(t, app, true).PublishedStandings[0].StageID != 2 {
		t.Fatal("republishing an earlier stage replaced the latest stage table")
	}
	requestJSON(t, app, "DELETE", "/api/v1/stages/2/standings/publication", nil, 204)
	if publicationWorkspace(t, app, true).PublishedStandings[0].StageID != 1 {
		t.Fatal("unpublishing latest did not fall back to prior published table")
	}
	requestJSON(t, app, "DELETE", "/api/v1/stages/1/standings/publication", nil, 204)
	public = publicationWorkspace(t, app, true)
	if len(public.PublishedStandings) != 0 || len(public.Standings) != 0 {
		t.Fatal("unpublished manual tables remained public")
	}
}
func TestFrozenStandingsWithdrawOnlyIncludedResults(t *testing.T) {
	for _, action := range []string{"publish", "review"} {
		t.Run(action, func(t *testing.T) {
			db, app := standingsPublicationFixture(t)
			setStandingsAuto(t, app, 1, false)
			before := publicationWorkspace(t, app, true).PublishedStandings[0]
			approveStandingResult(t, app, 2, 5, 0)
			requestJSON(t, app, "POST", "/api/v1/matches/2/publish", fiber.Map{"published": false}, 200)
			public := publicationWorkspace(t, app, true)
			if len(public.PublishedStandings) != 1 {
				t.Fatal("withdrawing later result B erased frozen snapshot A")
			}
			assertJSONUnchanged(t, before, public.PublishedStandings[0])
			if action == "publish" {
				requestJSON(t, app, "POST", "/api/v1/matches/1/publish", fiber.Map{"published": false}, 200)
			} else {
				requestJSON(t, app, "POST", "/api/v1/matches/1/review", fiber.Map{"approve": false}, 200)
			}
			public = publicationWorkspace(t, app, true)
			if len(public.PublishedStandings) != 0 || len(public.Standings) != 0 {
				t.Fatal("retracted source result A remained in a public table")
			}
			var count int64
			db.Model(&domain.StandingPublication{}).Where("stage_id = ?", 1).Count(&count)
			if count != 0 {
				t.Fatal("retracted source left an active persisted publication")
			}
		})
	}
}
func TestFrozenSnapshotScoreCorrectionsRequireRepublishAndReadRetractionsFailClosed(t *testing.T) {
	db, app := standingsPublicationFixture(t)
	setStandingsAuto(t, app, 1, false)
	before := publicationWorkspace(t, app, true).PublishedStandings[0]
	approveStandingResult(t, app, 1, 0, 5)
	after := publicationWorkspace(t, app, true).PublishedStandings[0]
	assertJSONUnchanged(t, before, after)
	requestJSON(t, app, "POST", "/api/v1/stages/1/standings/publish", fiber.Map{}, 200)
	after = publicationWorkspace(t, app, true).PublishedStandings[0]
	if publishedTeamPoints(after, 2) != 3 || publishedTeamPoints(after, 1) != 0 {
		t.Fatal("republish did not apply corrected approved score")
	}
	// Read-side source references also protect against retractions made outside API hooks.
	db.Model(&domain.Match{}).Where("id = ?", 1).Update("status", "submitted")
	public := publicationWorkspace(t, app, true)
	if len(public.PublishedStandings) != 0 {
		t.Fatal("read-side validation leaked a manually retracted source score")
	}
}
func TestStandingsRESTUsesPublicationSnapshotsAndAccurateCatalogFlags(t *testing.T) {
	_, app := standingsPublicationFixture(t)
	setStandingsAuto(t, app, 1, false)
	approveStandingResult(t, app, 2, 4, 0)
	approveStandingResult(t, app, 3, 1, 0)
	type restTable struct {
		State           string  `json:"standings_publishing_state"`
		Name            string  `json:"league_stage_name"`
		Number          string  `json:"league_stage_number"`
		PublishedID     *uint   `json:"published_stage_id"`
		PublishedName   *string `json:"published_stage_name"`
		PublishedNumber *string `json:"published_stage_number"`
		Last            string  `json:"last_published"`
		Rows            []struct {
			Team   domain.Team `json:"team"`
			Points int         `json:"points"`
		} `json:"standings"`
	}
	decode := func(path string) restTable {
		t.Helper()
		raw := requestJSON(t, app, "GET", path, nil, 200)
		if strings.Contains(string(raw), "source_match_ids") {
			t.Fatal("private publication source references leaked")
		}
		var table restTable
		if err := json.Unmarshal(raw, &table); err != nil {
			t.Fatal(err)
		}
		return table
	}
	seeding := decode("/rest/v1/C/standings?league=s")
	if seeding.Name != "Seeding" || seeding.Number != "" || seeding.PublishedID == nil || *seeding.PublishedID != 1 || seeding.PublishedName == nil || *seeding.PublishedName != "First table" || seeding.PublishedNumber == nil || *seeding.PublishedNumber != "0" || seeding.State != "PUBLISHED" {
		t.Fatal("seeding endpoint did not identify its latest published snapshot without changing legacy tags")
	}
	for _, row := range seeding.Rows {
		if row.Team.ID == 3 && row.Points != 0 {
			t.Fatal("REST seeding leaked a later manual-mode approval")
		}
	}
	future := decode("/rest/v1/C/standings?league=s&league_stage=1")
	if future.State != "UNPUBLISHED" || future.Name != "Second table" || len(future.Rows) != 0 || future.Last != "" || future.PublishedID != nil {
		t.Fatal("unpublished exact stage exposed live standings")
	}
	raw := requestJSON(t, app, "GET", "/rest/v1/C/leagues", nil, 200)
	var catalog struct {
		Leagues []struct {
			Seeding string `json:"seeding_publishing_state"`
			Stages  []struct {
				State string `json:"standings_publishing_state"`
			} `json:"league_stages"`
		} `json:"leagues"`
	}
	json.Unmarshal(raw, &catalog)
	if len(catalog.Leagues) != 1 || catalog.Leagues[0].Seeding != "PUBLISHED" || catalog.Leagues[0].Stages[0].State != "PUBLISHED" || catalog.Leagues[0].Stages[1].State != "UNPUBLISHED" || catalog.Leagues[0].Stages[2].State != "UNPUBLISHED" {
		t.Fatal("catalog standings publication flags still follow match publication")
	}
	requestJSON(t, app, "POST", "/api/v1/stages/2/standings/publish", fiber.Map{}, 200)
	latest := decode("/rest/v1/C/standings?league=s")
	if latest.PublishedID == nil || *latest.PublishedID != 2 || latest.PublishedNumber == nil || *latest.PublishedNumber != "1" {
		t.Fatal("manual latest publication did not update REST snapshot context")
	}
	var legacy []restTable
	if err := json.Unmarshal(requestJSON(t, app, "GET", "/rest/v1/C/standings?league=s&league_stage=0&format=openapi", nil, 200), &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy) != 1 || legacy[0].PublishedID == nil || *legacy[0].PublishedID != 1 {
		t.Fatal("historical snapshot or OpenAPI array wrapper was lost")
	}
}
func TestStandingsGettersAreReadOnlyWithStablePublicationTimes(t *testing.T) {
	db, app := standingsPublicationFixture(t)
	for _, manual := range []bool{false, true} {
		t.Run(fmt.Sprintf("manual%v", manual), func(t *testing.T) {
			if manual {
				setStandingsAuto(t, app, 1, false)
			}
			var before, after int
			db.Raw("SELECT total_changes()").Scan(&before)
			first := publicationWorkspace(t, app, true)
			publicationWorkspace(t, app, false)
			requestJSON(t, app, "GET", "/rest/v1/C/leagues", nil, 200)
			requestJSON(t, app, "GET", "/rest/v1/C/standings?league=s", nil, 200)
			second := publicationWorkspace(t, app, true)
			db.Raw("SELECT total_changes()").Scan(&after)
			if before != after {
				t.Fatal("GET endpoint wrote standings publications")
			}
			assertJSONUnchanged(t, first.PublishedStandings, second.PublishedStandings)
		})
	}
}
func TestStandingsSettingsRolesAndPerLeagueDefaults(t *testing.T) {
	db, app := standingsPublicationFixture(t)
	// Defaults are true in both migrated rows and legacy in-memory fixtures.
	if !(domain.League{}).AutoPublishStandings() {
		t.Fatal("legacy league did not default to auto publication")
	}
	parent := readLeagueResponse(t, requestJSON(t, app, "POST", "/api/v1/tournaments/1/leagues", fiber.Map{"name": "Parent", "abbrev": "p", "mode": "full", "standings_auto_publish": false}, 201))
	child := readLeagueResponse(t, requestJSON(t, app, "POST", "/api/v1/tournaments/1/leagues", fiber.Map{"name": "Child", "abbrev": "child", "parent_id": parent.ID}, 201))
	if parent.AutoPublishStandings() || !child.AutoPublishStandings() {
		t.Fatal("standings auto setting was inherited instead of per-league")
	}
	for _, role := range []string{"editor", "referee", "viewer"} {
		t.Run(role, func(t *testing.T) {
			restricted := fiber.New()
			api := API{DB: db}
			v := restricted.Group("/api/v1", func(c fiber.Ctx) error { c.Locals("user", User{Roles: []string{role}}); return c.Next() })
			api.registerDomainRoutes(v)
			if role == "editor" {
				requestJSON(t, restricted, "PUT", "/api/v1/leagues/1/standings-settings", fiber.Map{"auto_publish": false}, 200)
				requestJSON(t, restricted, "POST", "/api/v1/stages/1/standings/publish", fiber.Map{}, 200)
				requestJSON(t, restricted, "DELETE", "/api/v1/stages/1/standings/publication", nil, 204)
				requestJSON(t, restricted, "PUT", "/api/v1/leagues/1", fiber.Map{"name": "Forbidden setup"}, 403)
			} else {
				requestJSON(t, restricted, "PUT", "/api/v1/leagues/1/standings-settings", fiber.Map{"auto_publish": false}, 403)
				requestJSON(t, restricted, "POST", "/api/v1/stages/1/standings/publish", fiber.Map{}, 403)
				requestJSON(t, restricted, "DELETE", "/api/v1/stages/1/standings/publication", nil, 403)
			}
		})
	}
	requestJSON(t, app, "PUT", "/api/v1/leagues/1/standings-settings", fiber.Map{}, 400)
}
func TestStandingsPublicationsStayIsolatedAcrossTournamentsAndAdminLeagueEdits(t *testing.T) {
	db, app := standingsPublicationFixture(t)
	foreign := domain.Tournament{ID: 2, Name: "Other", Abbrev: "OTHER", StartDate: "2026-10-25", EndDate: "2026-10-25", Timezone: "UTC"}
	league := domain.League{ID: 2, TournamentID: 2, Name: "Other league", Abbrev: "s"}
	stage := domain.Stage{ID: 11, TournamentID: 2, LeagueID: 2, Name: "Foreign table", Type: "manual"}
	saveIntegrity(t, db, &foreign, &league, &stage, &domain.Team{ID: 6, TournamentID: 2, LeagueID: 2, Name: "Foreign A"}, &domain.Team{ID: 7, TournamentID: 2, LeagueID: 2, Name: "Foreign B"})
	match := integrityMatch(1, 6, 7, "approved")
	match.TournamentID = 2
	match.LeagueID = 2
	match.StageID = 11
	match.Published = true
	match.Goals1 = 9
	saveIntegrity(t, db, &match)
	setStandingsAuto(t, app, 2, false)
	// Setup clients may also toggle the flag; the shared transaction must freeze it.
	var local domain.League
	db.First(&local, 1)
	off := false
	local.StandingsAutoPublish = &off
	requestJSON(t, app, "PUT", "/api/v1/leagues/1", local, 200)
	approveStandingResult(t, app, 2, 4, 0)
	local.Name = "Renamed local"
	requestJSON(t, app, "PUT", "/api/v1/leagues/1", local, 200)
	public := publicationWorkspace(t, app, true)
	if len(public.PublishedStandings) != 1 || public.PublishedStandings[0].LeagueID != 1 || publishedTeamPoints(public.PublishedStandings[0], 3) != 0 {
		t.Fatal("admin league edit lost frozen local table or mixed tournaments")
	}
	raw := requestJSON(t, app, "GET", "/rest/v1/C/standings?league=s", nil, 200)
	if strings.Contains(string(raw), "Foreign") {
		t.Fatal("foreign publication leaked through REST")
	}
	raw = requestJSON(t, app, "GET", "/rest/v1/OTHER/standings?league=s", nil, 200)
	if !strings.Contains(string(raw), "Foreign A") {
		t.Fatal("isolated foreign publication was removed by local settings")
	}
}
