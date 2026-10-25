package web

import (
	"RCJVGameMaster/src/domain"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type generationRequest struct {
	Round        int    `json:"round"`
	PreviewToken string `json:"preview_token"`
}
type generationPlan struct {
	Stage        domain.Stage
	Matches      []domain.Match
	Round        int
	PreviewToken string
}

func readGenerationRequest(c fiber.Ctx) (generationRequest, error) {
	var request generationRequest
	if len(c.Body()) > 0 && c.Bind().Body(&request) != nil {
		return request, errors.New("Invalid JSON")
	}
	if request.Round < 0 {
		return request, errors.New("round cannot be negative")
	}
	return request, nil
}
func lockedStage(tx *gorm.DB, id uint) (domain.Stage, error) {
	var stage domain.Stage
	if e := tx.First(&stage, id).Error; e != nil {
		return stage, e
	}
	if e := lockTournament(tx, stage.TournamentID); e != nil {
		return stage, e
	}
	if e := tx.First(&stage, id).Error; e != nil {
		return stage, e
	}
	return stage, nil
}
func matchNumberCounter(tx *gorm.DB, tournamentID uint) (int, error) {
	var number int
	err := tx.Model(&domain.Match{}).Where("tournament_id = ?", tournamentID).Select("COALESCE(MAX(number),0)").Scan(&number).Error
	return number, err
}
func (a *API) previewGeneration(c fiber.Ctx) error {
	id, e := paramID(c)
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	request, e := readGenerationRequest(c)
	if e != nil {
		return apiError(c, 400, e.Error())
	}
	var plan *generationPlan
	e = a.DB.Transaction(func(tx *gorm.DB) error {
		stage, e := lockedStage(tx, id)
		if e != nil {
			return e
		}
		plan, e = buildGenerationPlan(tx, stage, request.Round)
		return e
	})
	if e != nil {
		return apiError(c, 409, domainError(e))
	}
	return c.JSON(fiber.Map{"matches": plan.Matches, "preview_token": plan.PreviewToken, "round": plan.Round})
}
func (a *API) generate(c fiber.Ctx) error {
	id, e := paramID(c)
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	request, e := readGenerationRequest(c)
	if e != nil {
		return apiError(c, 400, e.Error())
	}
	var plan *generationPlan
	e = a.DB.Transaction(func(tx *gorm.DB) error {
		stage, e := lockedStage(tx, id)
		if e != nil {
			return e
		}
		plan, e = buildGenerationPlan(tx, stage, request.Round)
		if e != nil {
			return e
		}
		if request.PreviewToken != "" && request.PreviewToken != plan.PreviewToken {
			return errors.New("preview is stale; preview the stage again before creating matches")
		}
		return savePopulatedMatches(tx, stage, plan.Matches)
	})
	if e != nil {
		return apiError(c, 409, domainError(e))
	}
	return c.JSON(fiber.Map{"matches": plan.Matches, "round": plan.Round})
}

// buildGenerationPlan performs SELECTs and pure calculations only. Neither
// previewing nor validation creates match IDs or advances the stage round.
func buildGenerationPlan(tx *gorm.DB, stage domain.Stage, requestedRound int) (*generationPlan, error) {
	league, e := leagueIn(tx, stage.LeagueID, stage.TournamentID)
	if e != nil {
		return nil, e
	}
	if stage.Type == "manual" {
		return nil, errors.New("add individual games for a manual stage")
	}
	var teams []domain.Team
	if e := tx.Where("league_id = ?", stage.LeagueID).Order("startnumber,id").Find(&teams).Error; e != nil {
		return nil, e
	}
	minimum := 2
	if stage.Type == "individual" {
		minimum = 1
	}
	if len(teams) < minimum {
		return nil, errors.New("not enough registered teams for this stage type")
	}

	if len(teams) > 256 {
		return nil, errors.New("automatic generation supports up to 256 teams per league")
	}
	var all []domain.Match
	if e := tx.Where("league_id = ?", stage.LeagueID).Order("number,id").Find(&all).Error; e != nil {
		return nil, e
	}
	saved := append([]domain.Match{}, all...)
	populated := []domain.Match{}
	for _, m := range all {
		if !m.IsPlaceholder {
			populated = append(populated, m)
		}
	}
	all = populated
	current := []domain.Match{}
	maxRound := 0
	for _, m := range all {
		if m.StageID == stage.ID {
			current = append(current, m)
			if m.Round > maxRound {
				maxRound = m.Round
			}
		}
	}
	if len(current) > 0 {
		for _, m := range saved {
			if m.StageID == stage.ID && m.IsPlaceholder && m.Round <= maxRound {
				return nil, errors.New("some teams are already assigned; clear assignments before automatic pairing")
			}
		}
	}
	if len(current) > 0 && (stage.Type == "individual" || stage.PreparedTeams > 0 && (stage.Type == "swiss" || stage.Type == "knockout")) {
		return nil, errors.New("one round per stage; create a linked stage for the next round or clear this stage to redraw")
	}
	round := maxRound + 1
	if requestedRound > 0 {
		if requestedRound != round {
			return nil, errors.New("generate the next sequential round")
		}
		round = requestedRound
	}
	if len(current) > 0 && stage.Type == "round_robin" {
		return nil, errors.New("round-robin matches have already been generated")
	}
	previous := []domain.Match{}
	for _, m := range all {
		if m.StageID == stage.ID {
			if m.Status != "approved" {
				return nil, errors.New("approve all previous round results before generating another round")
			}
			previous = append(previous, m)
		}
	}
	rankStage := uint(0)
	if stage.SourceStageID != nil {
		for _, match := range saved {
			if match.StageID == *stage.SourceStageID && match.IsPlaceholder {
				return nil, errors.New("populate and approve every source stage match before seeding this stage")
			}
		}
		rankStage = *stage.SourceStageID
		hasSource := false
		for _, m := range all {
			if m.StageID == rankStage {
				hasSource = true
				if m.Status != "approved" {
					return nil, errors.New("approve all source stage results first")
				}
			}
		}
		if !hasSource {
			return nil, errors.New("source stage has no results")
		}
	}
	seedMatches := all
	var leagueStages []domain.Stage
	if e := tx.Where("league_id = ?", stage.LeagueID).Order("id").Find(&leagueStages).Error; e != nil {
		return nil, e
	}
	earlier := map[uint]bool{}
	for _, other := range leagueStages {
		if other.SortOrder < stage.SortOrder || other.SortOrder == stage.SortOrder && other.ID < stage.ID {
			earlier[other.ID] = true
		}
	}
	if len(current) == 0 && stage.SourceStageID == nil && (stage.Type == "swiss" || stage.Type == "knockout") {
		for _, match := range saved {
			if earlier[match.StageID] && match.IsPlaceholder {
				return nil, errors.New("populate and approve every earlier stage match before seeding this stage")
			}
		}
		seedMatches = []domain.Match{}
		for _, m := range all {
			if !earlier[m.StageID] {
				continue
			}
			if m.Status != "approved" {
				return nil, errors.New("approve all previous stage results before seeding this stage")
			}
			seedMatches = append(seedMatches, m)
		}
	}
	ids := []uint{}
	if len(current) > 0 {
		rankStage = stage.ID
	}
	ranked := domain.Standings(teams, seedMatches, stage.LeagueID, rankStage)
	active := map[uint]bool{}
	for _, m := range current {
		active[m.Team1ID] = true
		if m.Team2ID != nil {
			active[*m.Team2ID] = true
		}
	}
	for _, r := range ranked {
		if len(current) == 0 || active[r.TeamID] {
			ids = append(ids, r.TeamID)
		}
	}
	if len(current) == 0 && stage.SourceStageID != nil {
		var source domain.Stage
		if e := tx.First(&source, *stage.SourceStageID).Error; e != nil {
			return nil, e
		}
		if source.Type == "knockout" {
			sourceRound := 0
			for _, m := range all {
				if m.StageID == source.ID && m.Round > sourceRound {
					sourceRound = m.Round
				}
			}
			ids = []uint{}
			for _, m := range all {
				if m.StageID != source.ID || m.Round != sourceRound {
					continue
				}
				if m.Team2ID == nil || m.Goals1 > m.Goals2 {
					ids = append(ids, m.Team1ID)
				} else if m.Goals2 > m.Goals1 {
					ids = append(ids, *m.Team2ID)
				} else {
					return nil, errors.New("source knockout matches need decisive results")
				}
			}
		}
	}
	if stage.Type == "individual" && stage.SourceStageID == nil && len(current) == 0 {
		ids = []uint{}
		for _, team := range teams {
			ids = append(ids, team.ID)
		}
	}
	if len(current) == 0 && stage.TeamLimit > 0 && len(ids) > stage.TeamLimit {
		ids = ids[:stage.TeamLimit]
	}
	if len(current) == 0 && stage.ExpectedTeams > 0 {
		wanted := stage.ExpectedTeams
		if stage.TeamLimit > 0 && wanted > stage.TeamLimit {
			wanted = stage.TeamLimit
		}
		if len(ids) < wanted {
			return nil, errors.New("not enough qualifying teams for the stage expected team count")
		}
		ids = ids[:wanted]
	}
	if len(ids) < minimum {
		return nil, errors.New("not enough qualifying teams for this stage type")
	}

	if stage.Type == "knockout" && len(current) > 0 {
		var latest []domain.Match
		for _, m := range current {
			if m.Round == maxRound {
				latest = append(latest, m)
			}
		}
		if len(latest) == 1 {
			return nil, errors.New("the knockout final is already complete")
		}
		sort.Slice(latest, func(i, j int) bool { return latest[i].Number < latest[j].Number })
		ids = []uint{}
		for _, m := range latest {
			if m.Team2ID == nil || m.Goals1 > m.Goals2 {
				ids = append(ids, m.Team1ID)
			} else if m.Goals2 > m.Goals1 {
				ids = append(ids, *m.Team2ID)
			} else {
				return nil, errors.New("knockout matches need a decisive result before advancing")
			}
		}
	}
	var pairs []domain.Pair
	switch stage.Type {
	case "individual":
		for _, id := range ids {
			pairs = append(pairs, domain.Pair{A: id, Round: 1})
		}
	case "round_robin":
		pairs = domain.RoundRobin(ids)
	case "swiss":
		// A new competition stage may repeat round-robin opponents. Swiss
		// rematch and bye history spans its own stage and linked Swiss
		// source stages, rather than every unrelated game in the league.
		swissHistory := map[uint]bool{}
		if stage.SourceStageID != nil {
			next := stage.SourceStageID
			for next != nil {
				var source domain.Stage
				found := false
				for _, other := range leagueStages {
					if other.ID == *next {
						source = other
						found = true
						break
					}
				}
				if !found || source.Type != "swiss" || swissHistory[source.ID] {
					break
				}
				swissHistory[source.ID] = true
				next = source.SourceStageID
			}
		} else {
			for _, other := range leagueStages {
				if earlier[other.ID] && other.Type == "swiss" {
					swissHistory[other.ID] = true
				}
			}
		}
		for _, m := range all {
			if swissHistory[m.StageID] && m.Status == "approved" {
				previous = append(previous, m)
			}
		}
		pairs, e = domain.SwissPairs(ids, previous, round)
		if e != nil {
			return nil, e
		}
	case "knockout":
		pairs = domain.KnockoutPairs(ids, round)
	}

	counter, e := matchNumberCounter(tx, stage.TournamentID)
	if e != nil {
		return nil, e
	}
	out := make([]domain.Match, 0, len(pairs))
	for _, pair := range pairs {
		match := domain.Match{TournamentID: stage.TournamentID, LeagueID: stage.LeagueID, StageID: stage.ID, Team1ID: pair.A, Team2ID: pair.B, Round: pair.Round}
		if e := validateMatch(tx, &match); e != nil {
			return nil, e
		}
		match.Number = counter + len(out) + 1
		if match.Team2ID == nil && stage.Type != "individual" {
			match.IsBye = true
			match.Status = "approved"
			match.Published = true
		}
		out = append(out, match)
	}
	out, e = attachPlaceholderRows(tx, stage, out, true)
	if e != nil {
		return nil, e
	}
	snapshot := struct {
		Stage         domain.Stage
		League        domain.League
		Teams         []domain.Team
		Stages        []domain.Stage
		Matches       []domain.Match
		NumberCounter int
		Planned       []domain.Match
		Round         int
	}{stage, league, teams, leagueStages, saved, counter, out, round}
	encoded, e := json.Marshal(snapshot)
	if e != nil {
		return nil, errors.New("could not fingerprint stage state")
	}
	hash := sha256.Sum256(encoded)
	return &generationPlan{Stage: stage, Matches: out, Round: round, PreviewToken: hex.EncodeToString(hash[:])}, nil
}

type stagedMatchInput struct {
	Team1ID         uint   `json:"team1_id"`
	Team2ID         *uint  `json:"team2_id"`
	DurationMinutes int    `json:"duration_minutes"`
	Round           int    `json:"round"`
	Notes           string `json:"notes"`
}

func (a *API) createStageMatches(c fiber.Ctx) error {
	id, e := paramID(c)
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	var body struct {
		Matches []stagedMatchInput `json:"matches"`
	}
	if c.Bind().Body(&body) != nil {
		return apiError(c, 400, "Invalid JSON")
	}
	if len(body.Matches) < 1 || len(body.Matches) > 256 {
		return apiError(c, 400, "provide between 1 and 256 matches")
	}
	out := make([]domain.Match, 0, len(body.Matches))
	e = a.DB.Transaction(func(tx *gorm.DB) error {
		stage, e := lockedStage(tx, id)
		if e != nil {
			return e
		}
		counter, e := matchNumberCounter(tx, stage.TournamentID)
		if e != nil {
			return e
		}
		for _, input := range body.Matches {
			if input.Round < 0 {
				return errors.New("round cannot be negative")
			}
			match := domain.Match{TournamentID: stage.TournamentID, LeagueID: stage.LeagueID, StageID: stage.ID, Team1ID: input.Team1ID, Team2ID: input.Team2ID, DurationMinutes: input.DurationMinutes, Round: input.Round, Notes: input.Notes}
			if e := validateMatch(tx, &match); e != nil {
				return e
			}
			match.Number = counter + len(out) + 1
			out = append(out, match)
		}
		var err error
		out, err = attachPlaceholderRows(tx, stage, out, false)
		if err != nil {
			return err
		}
		return savePopulatedMatches(tx, stage, out)
	})
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	return c.Status(201).JSON(fiber.Map{"matches": out})
}
