package web

import (
	"RCJVGameMaster/src/domain"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"
)

type compatSelection struct {
	Leagues []domain.League
	Main    *domain.League
	Exact   *domain.League
	Stage   *domain.Stage
}

func compatLeagueByAbbrev(w domain.Workspace, abbrev string) (domain.League, bool) {
	for _, league := range w.Leagues {
		if league.Abbrev == abbrev {
			return league, true
		}
	}
	return domain.League{}, false
}
func compatMainLeague(w domain.Workspace, league domain.League) (domain.League, bool) {
	if league.ParentID == nil {
		return league, true
	}
	for _, main := range w.Leagues {
		if main.ID == *league.ParentID && main.ParentID == nil {
			return main, true
		}
	}
	return domain.League{}, false
}
func selectCompatLeagues(c fiber.Ctx, w domain.Workspace, requireExact, checkStage bool) (compatSelection, error) {
	selection := compatSelection{Leagues: []domain.League{}}
	if abbreviation := c.Query("league"); abbreviation != "" {
		league, found := compatLeagueByAbbrev(w, abbreviation)
		if !found {
			return selection, errors.New("league not found in this tournament")
		}
		selection.Exact = &league
	}
	if abbreviation := c.Query("sub_league"); abbreviation != "" {
		child, found := compatLeagueByAbbrev(w, abbreviation)
		if !found || child.ParentID == nil {
			return selection, errors.New("sub_league must identify a sub-league in this tournament")
		}
		if selection.Exact != nil && selection.Exact.ID != child.ID {
			return selection, errors.New("league and sub_league must identify the same sub-league")
		}
		selection.Exact = &child
	}
	if abbreviation := c.Query("main_league"); abbreviation != "" {
		main, found := compatLeagueByAbbrev(w, abbreviation)
		if !found || main.ParentID != nil {
			return selection, errors.New("main_league must identify a main league in this tournament")
		}
		selection.Main = &main
	}
	if selection.Exact != nil {
		main, found := compatMainLeague(w, *selection.Exact)
		if !found {
			return selection, errors.New("league main-league context not found in this tournament")
		}
		if selection.Main != nil && selection.Main.ID != main.ID {
			return selection, errors.New("selected league does not belong to main_league")
		}
		selection.Main = &main
		selection.Leagues = append(selection.Leagues, *selection.Exact)
	} else if selection.Main != nil {
		for _, league := range w.Leagues {
			if league.ID == selection.Main.ID || league.ParentID != nil && *league.ParentID == selection.Main.ID {
				selection.Leagues = append(selection.Leagues, league)
			}
		}
	} else {
		selection.Leagues = append(selection.Leagues, w.Leagues...)
	}
	if requireExact && selection.Exact == nil {
		return selection, errors.New("standings require an exact league or sub_league; main_league alone cannot combine rankings")
	}
	if checkStage {
		if raw := c.Query("league_stage"); raw != "" {
			if selection.Exact == nil {
				return selection, errors.New("league_stage requires an exact league or sub_league; it is not a main-league stage index")
			}
			index, err := strconv.Atoi(raw)
			if err != nil || index < 0 {
				return selection, errors.New("invalid league_stage index")
			}
			stages := publicStages(w, selection.Exact.ID)
			if index >= len(stages) {
				return selection, errors.New("league stage not found")
			}
			stage := stages[index]
			selection.Stage = &stage
		}
	}
	return selection, nil
}
func compatHierarchyMetadata(w domain.Workspace, league domain.League) fiber.Map {
	main, found := compatMainLeague(w, league)
	if !found {
		main = league
	}
	metadata := fiber.Map{"main_league_abbrev": main.Abbrev, "main_league_name": main.Name, "sub_league_abbrev": nil, "sub_league_name": nil}
	if league.ParentID != nil {
		metadata["sub_league_abbrev"] = league.Abbrev
		metadata["sub_league_name"] = league.Name
	}
	return metadata
}
func addCompatFields(result, extra fiber.Map) {
	for key, value := range extra {
		result[key] = value
	}
}
func applyCompatSelection(c fiber.Ctx, w domain.Workspace, selection compatSelection, result fiber.Map) {
	if selection.Exact != nil {
		result["league_abbrev"] = selection.Exact.Abbrev
		result["league_name"] = selection.Exact.Name
		addCompatFields(result, compatHierarchyMetadata(w, *selection.Exact))
	} else if selection.Main != nil {
		addCompatFields(result, compatHierarchyMetadata(w, *selection.Main))
	}
	if selection.Stage != nil {
		result["league_stage_name"] = selection.Stage.Name
		result["league_stage_number"] = c.Query("league_stage")
	}
}
func compatStageCatalog(w domain.Workspace, league domain.League) []fiber.Map {
	entries := []fiber.Map{}
	for _, stage := range publicStages(w, league.ID) {
		latest := latestPublished(w, league.ID, stage.ID)
		state := "UNPUBLISHED"
		if latest != nil {
			state = "PUBLISHED"
		}
		entries = append(entries, fiber.Map{"name": stage.Name, "standings_publishing_state": state, "standings_last_published": compatTime(latest, w.Tournament.Timezone)})
	}
	return entries
}
func compatLeagueEntry(w domain.Workspace, league domain.League, openapi bool) fiber.Map {
	result := fiber.Map{"id": league.ID, "parent_id": league.ParentID, "abbrev": league.Abbrev, "name": league.Name}
	addCompatFields(result, compatHierarchyMetadata(w, league))
	if openapi {
		names := []string{}
		for _, stage := range publicStages(w, league.ID) {
			names = append(names, stage.Name)
		}
		result["league_stages"] = names
	} else {
		result["league_stages"] = compatStageCatalog(w, league)
		latest := latestPublished(w, league.ID, 0)
		state := "UNPUBLISHED"
		if latest != nil {
			state = "PUBLISHED"
		}
		result["seeding_publishing_state"] = state
		result["seeding_last_published"] = compatTime(latest, w.Tournament.Timezone)
	}
	return result
}
func compatMainCatalog(w domain.Workspace, selection compatSelection) []fiber.Map {
	roots := []fiber.Map{}
	for _, main := range w.Leagues {
		if main.ParentID != nil || selection.Main != nil && main.ID != selection.Main.ID {
			continue
		}
		stages := []fiber.Map{}
		if selection.Exact == nil || selection.Exact.ID == main.ID {
			stages = compatStageCatalog(w, main)
		}
		children := []fiber.Map{}
		for _, child := range w.Leagues {
			if child.ParentID == nil || *child.ParentID != main.ID {
				continue
			}
			if selection.Exact != nil && selection.Exact.ID != child.ID {
				continue
			}
			children = append(children, compatLeagueEntry(w, child, false))
		}
		roots = append(roots, fiber.Map{"id": main.ID, "abbrev": main.Abbrev, "name": main.Name, "league_stages": stages, "sub_leagues": children})
	}
	return roots
}
