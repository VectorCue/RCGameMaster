package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"io"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

type hierarchyStage struct {
	Name  string `json:"name"`
	State string `json:"standings_publishing_state"`
}
type hierarchyLeague struct {
	ID         uint             `json:"id"`
	ParentID   *uint            `json:"parent_id"`
	Abbrev     string           `json:"abbrev"`
	Name       string           `json:"name"`
	MainAbbrev string           `json:"main_league_abbrev"`
	MainName   string           `json:"main_league_name"`
	SubAbbrev  *string          `json:"sub_league_abbrev"`
	SubName    *string          `json:"sub_league_name"`
	Stages     []hierarchyStage `json:"league_stages"`
}
type hierarchyMain struct {
	ID       uint              `json:"id"`
	Abbrev   string            `json:"abbrev"`
	Name     string            `json:"name"`
	Stages   []hierarchyStage  `json:"league_stages"`
	Children []hierarchyLeague `json:"sub_leagues"`
}
type hierarchyCatalog struct {
	TournamentName string            `json:"tournament_name"`
	Leagues        []hierarchyLeague `json:"leagues"`
	Mains          []hierarchyMain   `json:"main_leagues"`
	LeagueAbbrev   string            `json:"league_abbrev"`
	MainAbbrev     string            `json:"main_league_abbrev"`
	SubAbbrev      *string           `json:"sub_league_abbrev"`
}
type hierarchyMatch struct {
	Number       int          `json:"number"`
	Team1        *domain.Team `json:"team1"`
	Team2        *domain.Team `json:"team2"`
	League       string       `json:"league"`
	LeagueAbbrev string       `json:"league_abbrev"`
	MainAbbrev   string       `json:"main_league_abbrev"`
	SubAbbrev    *string      `json:"sub_league_abbrev"`
	StageType    string       `json:"stage_type"`
	Placeholder  bool         `json:"is_placeholder"`
	Bye          bool         `json:"is_bye"`
	Status       string       `json:"status"`
	Goals1       int          `json:"goals1"`
	Points1      int          `json:"points1"`
	Referees     []struct {
		FirstName string `json:"first_name"`
	} `json:"referees"`
}
type hierarchyMatches struct {
	LeagueAbbrev string           `json:"league_abbrev"`
	MainAbbrev   string           `json:"main_league_abbrev"`
	SubAbbrev    *string          `json:"sub_league_abbrev"`
	StageNumber  string           `json:"league_stage_number"`
	Matches      []hierarchyMatch `json:"matches"`
}

func hierarchyFixture(t *testing.T) *fiber.App {
	t.Helper()
	db := integrationDB(t)
	app, err := newApp(localTestConfig(), db)
	if err != nil {
		t.Fatal(err)
	}
	one, four, eleven := uint(1), uint(4), uint(11)
	saveIntegrity(t, db, &domain.Tournament{ID: 1, Name: "Hierarchy cup", Abbrev: "HIER", StartDate: "2026-10-25", EndDate: "2026-10-25", Timezone: "UTC"}, &domain.Tournament{ID: 2, Name: "Other cup", Abbrev: "OTHER", StartDate: "2026-10-25", EndDate: "2026-10-25", Timezone: "UTC"})
	for _, league := range []domain.League{{ID: 1, TournamentID: 1, Name: "Soccer", Abbrev: "soccer"}, {ID: 2, TournamentID: 1, ParentID: &one, Name: "Infrared", Abbrev: "ir"}, {ID: 3, TournamentID: 1, ParentID: &one, Name: "Vision", Abbrev: "vision"}, {ID: 4, TournamentID: 1, Name: "Rescue", Abbrev: "rescue"}, {ID: 5, TournamentID: 1, ParentID: &four, Name: "Line", Abbrev: "line"}, {ID: 6, TournamentID: 1, Name: "Empty main", Abbrev: "empty"}, {ID: 7, TournamentID: 1, ParentID: &four, Name: "Unused child", Abbrev: "unused"}, {ID: 11, TournamentID: 2, Name: "Other main", Abbrev: "away"}, {ID: 12, TournamentID: 2, ParentID: &eleven, Name: "Other Infrared", Abbrev: "other_ir"}} {
		row := league
		saveIntegrity(t, db, &row)
	}
	for _, stage := range []domain.Stage{{ID: 1, TournamentID: 1, LeagueID: 2, Name: "Final", Type: "knockout", SortOrder: 20}, {ID: 2, TournamentID: 1, LeagueID: 2, Name: "Qualification", Type: "round_robin", SortOrder: 10}, {ID: 3, TournamentID: 1, LeagueID: 5, Name: "Run", Type: "individual"}, {ID: 4, TournamentID: 1, LeagueID: 4, Name: "Root stage", Type: "individual"}, {ID: 11, TournamentID: 2, LeagueID: 12, Name: "Other stage", Type: "individual"}} {
		row := stage
		saveIntegrity(t, db, &row)
	}
	for _, team := range []domain.Team{{ID: 1, TournamentID: 1, LeagueID: 2, Name: "Alpha", Startnumber: 1}, {ID: 2, TournamentID: 1, LeagueID: 2, Name: "Beta", Startnumber: 2}, {ID: 3, TournamentID: 1, LeagueID: 5, Name: "Runner", Startnumber: 3}, {ID: 4, TournamentID: 1, LeagueID: 4, Name: "Root runner", Startnumber: 4}, {ID: 11, TournamentID: 2, LeagueID: 12, Name: "Foreign private team"}} {
		row := team
		saveIntegrity(t, db, &row)
	}
	saveIntegrity(t, db, &domain.Referee{ID: 1, TournamentID: 1, Name: "Ref Name", UserID: "private-user-id"}, &domain.Field{ID: 1, TournamentID: 1, LeagueID: 1, Name: "Soccer A"}, &domain.Field{ID: 2, TournamentID: 1, LeagueID: 4, Name: "Rescue A"})
	two := uint(2)
	start := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	privateState := json.RawMessage(`{"phase":"finished","checks":["private-check"],"signatures":["private-signature"],"end_reason":"private-reason"}`)
	matches := []domain.Match{
		{ID: 1, TournamentID: 1, LeagueID: 2, StageID: 2, Number: 1, Round: 1, Team1ID: 1, Team2ID: &two, Status: "approved", Goals1: 2, Goals2: 1, Published: true, RefereeIDs: []uint{1}},
		{ID: 2, TournamentID: 1, LeagueID: 2, StageID: 1, Number: 2, Round: 1, Team1ID: 1, Team2ID: &two, Status: "submitted", Goals1: 9, Goals2: 8, Published: true, RefereeIDs: []uint{1}},
		{ID: 3, TournamentID: 1, LeagueID: 5, StageID: 3, Number: 3, Round: 1, Team1ID: 3, Status: "scheduled", Published: true, RefereeIDs: []uint{}},
		{ID: 4, TournamentID: 1, LeagueID: 4, StageID: 4, Number: 4, Round: 1, Team1ID: 4, Status: "approved", Published: true, RefereeIDs: []uint{}},
		{ID: 5, TournamentID: 1, LeagueID: 2, StageID: 2, Number: 5, Round: 1, Team1ID: 1, Team2ID: &two, Status: "draft", Published: false, RefereeIDs: []uint{}},
		{ID: 6, TournamentID: 1, LeagueID: 2, StageID: 2, Number: 6, Round: 1, IsPlaceholder: true, Status: "scheduled", Published: true, RefereeIDs: []uint{1}},
		{ID: 7, TournamentID: 1, LeagueID: 2, StageID: 2, Number: 7, Round: 1, Team1ID: 1, IsBye: true, Status: "approved", Published: true, RefereeIDs: []uint{}},
		{ID: 8, TournamentID: 2, LeagueID: 12, StageID: 11, Number: 1, Round: 1, Team1ID: 11, Status: "approved", Published: true, RefereeIDs: []uint{}},
	}
	for i := range matches {
		match := &matches[i]
		match.DurationMinutes = 25
		match.ScheduledAt = &start
		match.LiveState = privateState
		match.Notes = "private-match-note"
		saveIntegrity(t, db, match)
	}
	return app
}
func decodeHierarchyCatalog(t *testing.T, raw []byte) hierarchyCatalog {
	t.Helper()
	var value hierarchyCatalog
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func decodeHierarchyMatches(t *testing.T, raw []byte) hierarchyMatches {
	t.Helper()
	var value hierarchyMatches
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func leagueAbbreviations(leagues []hierarchyLeague) string {
	values := []string{}
	for _, league := range leagues {
		values = append(values, league.Abbrev)
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}
func TestRESTHierarchyCatalogPreservesFlatClientsAndIncludesEmptyParents(t *testing.T) {
	app := hierarchyFixture(t)
	raw := requestJSON(t, app, "GET", "/rest/v1/HIER/leagues", nil, 200)
	catalog := decodeHierarchyCatalog(t, raw)
	if catalog.TournamentName != "Hierarchy cup" || leagueAbbreviations(catalog.Leagues) != "ir,line,rescue" || len(catalog.Mains) != 3 {
		t.Fatal("flat legacy leagues or complete main catalog incorrect")
	}
	for _, league := range catalog.Leagues {
		if len(league.Stages) == 0 {
			t.Fatal("empty league leaked into legacy array")
		}
		if league.Abbrev == "ir" && (league.ParentID == nil || *league.ParentID != 1 || league.MainAbbrev != "soccer" || league.SubAbbrev == nil || *league.SubAbbrev != "ir" || league.Stages[0].Name != "Qualification" || league.Stages[1].Name != "Final") {
			t.Fatal("sub-league metadata or stable stage sort order incorrect")
		}
		if league.Abbrev == "rescue" && (league.ParentID != nil || league.SubAbbrev != nil || league.MainAbbrev != "rescue") {
			t.Fatal("direct main-league competition has incorrect hierarchy metadata")
		}
	}
	for _, main := range catalog.Mains {
		switch main.Abbrev {
		case "soccer":
			if len(main.Stages) != 0 || leagueAbbreviations(main.Children) != "ir,vision" {
				t.Fatal("stage-less soccer parent omitted children")
			}
		case "rescue":
			if len(main.Stages) != 1 || leagueAbbreviations(main.Children) != "line,unused" {
				t.Fatal("root stages or stage-less rescue child omitted")
			}
		case "empty":
			if len(main.Stages) != 0 || len(main.Children) != 0 {
				t.Fatal("empty main has fabricated catalog contents")
			}
		default:
			t.Fatal("foreign main league leaked")
		}
	}
	// This matches the adjacent RCSoccerStats decoder's old fields; additions are ignored.
	var legacy struct {
		Leagues []struct {
			Name   string `json:"name"`
			Abbrev string `json:"abbrev"`
			Stages []struct {
				Name string `json:"name"`
			} `json:"league_stages"`
		} `json:"leagues"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy.Leagues) != 3 || len(legacy.Leagues[0].Stages) != 2 {
		t.Fatal("legacy client shape changed")
	}
}
func TestRESTHierarchyCatalogSelectionAndAliases(t *testing.T) {
	app := hierarchyFixture(t)
	for _, tt := range []struct {
		query, flat, main, children string
		rootStages                  int
	}{{"main_league=soccer", "ir", "soccer", "ir,vision", 0}, {"main_league=rescue", "line,rescue", "rescue", "line,unused", 1}, {"sub_league=ir", "ir", "soccer", "ir", 0}, {"league=ir&sub_league=ir", "ir", "soccer", "ir", 0}, {"main_league=soccer&league=ir&sub_league=ir", "ir", "soccer", "ir", 0}, {"league=rescue", "rescue", "rescue", "", 1}, {"main_league=rescue&league=rescue", "rescue", "rescue", "", 1}, {"sub_league=vision", "", "soccer", "vision", 0}, {"main_league=empty", "", "empty", "", 0}} {
		t.Run(tt.query, func(t *testing.T) {
			catalog := decodeHierarchyCatalog(t, requestJSON(t, app, "GET", "/rest/v1/HIER/leagues?"+tt.query, nil, 200))
			if leagueAbbreviations(catalog.Leagues) != tt.flat || len(catalog.Mains) != 1 || catalog.Mains[0].Abbrev != tt.main || leagueAbbreviations(catalog.Mains[0].Children) != tt.children || len(catalog.Mains[0].Stages) != tt.rootStages {
				t.Fatal("catalog filter lost main context or exposed unselected leagues")
			}
		})
	}
	// Catalog stage arrays retain complete indexes even when an unsupported stage query is present.
	catalog := decodeHierarchyCatalog(t, requestJSON(t, app, "GET", "/rest/v1/HIER/leagues?sub_league=ir&league_stage=99", nil, 200))
	if len(catalog.Leagues) != 1 || len(catalog.Leagues[0].Stages) != 2 {
		t.Fatal("catalog stage query renumbered stage indexes")
	}
}
func TestRESTHierarchyMatchScopesMetadataAndPublicRedaction(t *testing.T) {
	app := hierarchyFixture(t)
	for _, tt := range []struct {
		query string
		count int
		main  string
		exact string
		sub   *string
	}{{"", 6, "", "", nil}, {"main_league=soccer", 4, "soccer", "", nil}, {"main_league=rescue", 2, "rescue", "", nil}, {"sub_league=ir", 4, "soccer", "ir", stringPtr("ir")}, {"league=ir&sub_league=ir", 4, "soccer", "ir", stringPtr("ir")}, {"main_league=soccer&sub_league=ir&league_stage=0", 3, "soccer", "ir", stringPtr("ir")}, {"sub_league=ir&league_stage=1", 1, "soccer", "ir", stringPtr("ir")}, {"league=rescue&league_stage=0", 1, "rescue", "rescue", nil}, {"sub_league=vision", 0, "soccer", "vision", stringPtr("vision")}} {
		t.Run(tt.query, func(t *testing.T) {
			raw := requestJSON(t, app, "GET", "/rest/v1/HIER/matches?"+tt.query, nil, 200)
			if strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "Foreign private") {
				t.Fatal("public match scope leaked private or other-tournament data")
			}
			got := decodeHierarchyMatches(t, raw)
			if len(got.Matches) != tt.count || got.MainAbbrev != tt.main || got.LeagueAbbrev != tt.exact {
				t.Fatal("match scope/envelope differs from requested hierarchy")
			}
			if (tt.sub == nil) != (got.SubAbbrev == nil) || tt.sub != nil && *tt.sub != *got.SubAbbrev {
				t.Fatal("selected sub-league metadata incorrect")
			}
			for _, match := range got.Matches {
				if match.LeagueAbbrev == "" || match.MainAbbrev == "" || match.StageType == "" || match.Status == "" {
					t.Fatal("match omitted hierarchy/state extensions")
				}
				switch match.Number {
				case 2:
					if match.Status != "submitted" || match.Goals1 != 0 || match.Points1 != 0 {
						t.Fatal("pending result was published")
					}
				case 3, 4:
					if match.Team2 != nil || match.Bye || match.StageType != "individual" || match.Points1 != 0 {
						t.Fatal("single run became a bye or head-to-head result")
					}
				case 6:
					if !match.Placeholder || match.Bye || match.Team1 == nil || match.Team1.Name != "Awaiting teams" {
						t.Fatal("empty scheduled match is not identifiable")
					}
				case 7:
					if !match.Bye || match.Placeholder || match.Points1 != 3 {
						t.Fatal("real bye lost its explicit marker")
					}
				case 5, 8:
					t.Fatal("unpublished or foreign match leaked")
				}
			}
		})
	}
}
func stringPtr(value string) *string { return &value }
func TestRESTHierarchyStandingsRequireExactLeagueAndKeepSortedStageIndexes(t *testing.T) {
	app := hierarchyFixture(t)
	for _, query := range []string{"league=ir&league_stage=0", "sub_league=ir&league_stage=0", "main_league=soccer&sub_league=ir&league_stage=0", "league=ir&sub_league=ir&league_stage=0"} {
		t.Run(query, func(t *testing.T) {
			raw := requestJSON(t, app, "GET", "/rest/v1/HIER/standings?"+query, nil, 200)
			var result struct {
				Main      string `json:"main_league_abbrev"`
				Sub       string `json:"sub_league_abbrev"`
				Stage     string `json:"league_stage_name"`
				Standings []struct {
					Points int `json:"points"`
				} `json:"standings"`
			}
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			if result.Main != "soccer" || result.Sub != "ir" || result.Stage != "Qualification" || len(result.Standings) != 2 || result.Standings[0].Points != 6 {
				t.Fatal("alias or sorted per-league stage standings incorrect")
			}
		})
	}
	raw := requestJSON(t, app, "GET", "/rest/v1/HIER/standings?sub_league=line", nil, 200)
	var emptyResult struct {
		Last string `json:"last_published"`
		Rows []struct {
			Last string `json:"last_published"`
		} `json:"standings"`
	}
	json.Unmarshal(raw, &emptyResult)
	if emptyResult.Last != "" || len(emptyResult.Rows) != 0 {
		t.Fatal("no-result publication timestamp must be empty in envelope and rows")
	}
	requestJSON(t, app, "GET", "/rest/v1/HIER/standings", nil, 404)
	requestJSON(t, app, "GET", "/rest/v1/HIER/standings?main_league=soccer", nil, 404)
}
func TestRESTHierarchyRejectsInvalidSelectionAndCrossTournamentAliases(t *testing.T) {
	app := hierarchyFixture(t)
	for _, query := range []string{"main_league=ir", "main_league=away", "sub_league=soccer", "sub_league=other_ir", "league=other_ir", "main_league=rescue&sub_league=ir", "main_league=soccer&league=rescue", "league=ir&sub_league=line", "main_league=soccer&league=ir&sub_league=line"} {
		for _, endpoint := range []string{"leagues", "matches", "standings"} {
			requestJSON(t, app, "GET", "/rest/v1/HIER/"+endpoint+"?"+query, nil, 404)
		}
	}
	for _, query := range []string{"league_stage=0", "main_league=soccer&league_stage=0", "sub_league=ir&league_stage=-1", "sub_league=ir&league_stage=nope", "sub_league=ir&league_stage=2", "sub_league=ir&league_stage=1.5", "sub_league=vision&league_stage=0"} {
		for _, endpoint := range []string{"matches", "standings"} {
			requestJSON(t, app, "GET", "/rest/v1/HIER/"+endpoint+"?"+query, nil, 404)
		}
	}
	requestJSON(t, app, "GET", "/rest/v1/UNKNOWN/leagues", nil, 404)
}
func TestRESTHierarchyOpenAPIFormatKeepsArrayWrappersAndMetadata(t *testing.T) {
	app := hierarchyFixture(t)
	raw := requestJSON(t, app, "GET", "/rest/v1/HIER/leagues?format=openapi&main_league=soccer", nil, 200)
	var leagues []struct {
		Abbrev string   `json:"abbrev"`
		Main   string   `json:"main_league_abbrev"`
		Parent *uint    `json:"parent_id"`
		Stages []string `json:"league_stages"`
	}
	if err := json.Unmarshal(raw, &leagues); err != nil {
		t.Fatal(err)
	}
	if len(leagues) != 1 || leagues[0].Abbrev != "ir" || leagues[0].Main != "soccer" || leagues[0].Parent == nil || len(leagues[0].Stages) != 2 || leagues[0].Stages[0] != "Qualification" || strings.Contains(string(raw), "main_leagues") {
		t.Fatal("OpenAPI league array or stage-name shape changed")
	}
	raw = requestJSON(t, app, "GET", "/rest/v1/HIER/matches?format=openapi&main_league=rescue", nil, 200)
	var groups []struct {
		League  string           `json:"league_abbrev"`
		Main    string           `json:"main_league_abbrev"`
		Sub     *string          `json:"sub_league_abbrev"`
		Type    string           `json:"stage_type"`
		Matches []hierarchyMatch `json:"matches"`
	}
	if err := json.Unmarshal(raw, &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatal("OpenAPI grouped match array changed")
	}
	for _, group := range groups {
		if group.League == "" || group.Main != "rescue" || group.Type != "individual" || len(group.Matches) != 1 {
			t.Fatal("OpenAPI grouped matches omitted hierarchy metadata")
		}
	}
	raw = requestJSON(t, app, "GET", "/rest/v1/HIER/standings?format=openapi&sub_league=line", nil, 200)
	var standings []struct {
		Main string `json:"main_league_abbrev"`
		Sub  string `json:"sub_league_abbrev"`
		Rows []any  `json:"standings"`
	}
	if err := json.Unmarshal(raw, &standings); err != nil {
		t.Fatal(err)
	}
	if len(standings) != 1 || standings[0].Main != "rescue" || standings[0].Sub != "line" || len(standings[0].Rows) != 0 {
		t.Fatal("OpenAPI standings array changed")
	}
}
func TestPublicOpenAPISpecificationIsEmbeddedAndUsesRESTHeaders(t *testing.T) {
	app := hierarchyFixture(t)
	response, err := app.Test(httptest.NewRequest("GET", "/rest/v1/openapi.yaml", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "application/yaml" || response.Header.Get("Access-Control-Allow-Origin") != "*" || response.Header.Get("Cache-Control") != "no-cache" {
		t.Fatal("public API specification route or headers incorrect")
	}
	if string(body) != string(restOpenAPISpecification) || !strings.Contains(string(body), "openapi:") {
		t.Fatal("route did not serve the embedded OpenAPI document")
	}
}
