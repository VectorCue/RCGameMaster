package web

import (
	"RCJVGameMaster/src/domain"
	"errors"
	"sort"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func (a *API) generate(c fiber.Ctx) error {
	id, e := paramID(c)
	if e != nil {
		return apiError(c, 400, domainError(e))
	}
	var body struct {
		Round int `json:"round"`
	}
	if len(c.Body()) > 0 && c.Bind().Body(&body) != nil {
		return apiError(c, 400, "Invalid JSON")
	}
	out := []domain.Match{}
	e = a.DB.Transaction(func(tx *gorm.DB) error {
		var stage domain.Stage
		if e := tx.First(&stage, id).Error; e != nil {
			return e
		}
		if e := lockTournament(tx, stage.TournamentID); e != nil {
			return e
		}
		if e := tx.First(&stage, id).Error; e != nil {
			return e
		}
		league, e := leagueIn(tx, stage.LeagueID, stage.TournamentID)
		if e != nil {
			return e
		}
		if league.Mode != "full" {
			return errors.New("automatic generation requires full match management")
		}
		if stage.Type == "manual" {
			return errors.New("add individual games for a manual stage")
		}
		var teams []domain.Team
		if e := tx.Where("league_id = ?", stage.LeagueID).Order("startnumber,id").Find(&teams).Error; e != nil {
			return e
		}
		if len(teams) < 2 {
			return errors.New("at least two teams are needed")
		}
		if len(teams) > 256 {
			return errors.New("automatic generation supports up to 256 teams per league")
		}
		var all []domain.Match
		if e := tx.Where("league_id = ?", stage.LeagueID).Order("number,id").Find(&all).Error; e != nil {
			return e
		}
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
		round := maxRound + 1
		if body.Round > 0 {
			if body.Round != round {
				return errors.New("generate the next sequential round")
			}
			round = body.Round
		}
		if len(current) > 0 && stage.Type == "round_robin" {
			return errors.New("round-robin matches have already been generated")
		}
		previous := []domain.Match{}
		for _, m := range all {
			if m.StageID == stage.ID {
				if m.Status != "approved" {
					return errors.New("approve all previous round results before generating another round")
				}
				previous = append(previous, m)
			}
		}
		rankStage := uint(0)
		if stage.SourceStageID != nil {
			rankStage = *stage.SourceStageID
			hasSource := false
			for _, m := range all {
				if m.StageID == rankStage {
					hasSource = true
					if m.Status != "approved" {
						return errors.New("approve all source stage results first")
					}
				}
			}
			if !hasSource {
				return errors.New("source stage has no results")
			}
		}
		seedMatches := all
		var leagueStages []domain.Stage
		if e := tx.Where("league_id = ?", stage.LeagueID).Find(&leagueStages).Error; e != nil {
			return e
		}
		earlier := map[uint]bool{}
		for _, other := range leagueStages {
			if other.SortOrder < stage.SortOrder || other.SortOrder == stage.SortOrder && other.ID < stage.ID {
				earlier[other.ID] = true
			}
		}
		if len(current) == 0 && stage.SourceStageID == nil && (stage.Type == "swiss" || stage.Type == "knockout") {
			seedMatches = []domain.Match{}
			for _, m := range all {
				if !earlier[m.StageID] {
					continue
				}
				if m.Status != "approved" {
					return errors.New("approve all previous stage results before seeding this stage")
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
				return e
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
						return errors.New("source knockout matches need decisive results")
					}
				}
			}
		}
		if len(current) == 0 && stage.TeamLimit > 0 && len(ids) > stage.TeamLimit {
			ids = ids[:stage.TeamLimit]
		}
		if len(ids) < 2 {
			return errors.New("at least two qualifying teams are needed")
		}
		if stage.Type == "knockout" && len(current) > 0 {
			var latest []domain.Match
			for _, m := range current {
				if m.Round == maxRound {
					latest = append(latest, m)
				}
			}
			if len(latest) == 1 {
				return errors.New("the knockout final is already complete")
			}
			sort.Slice(latest, func(i, j int) bool { return latest[i].Number < latest[j].Number })
			ids = []uint{}
			for _, m := range latest {
				if m.Team2ID == nil || m.Goals1 > m.Goals2 {
					ids = append(ids, m.Team1ID)
				} else if m.Goals2 > m.Goals1 {
					ids = append(ids, *m.Team2ID)
				} else {
					return errors.New("knockout matches need a decisive result before advancing")
				}
			}
		}
		var pairs []domain.Pair
		switch stage.Type {
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
				return e
			}
		case "knockout":
			pairs = domain.KnockoutPairs(ids, round)
		}
		for _, p := range pairs {
			m := domain.Match{TournamentID: stage.TournamentID, LeagueID: stage.LeagueID, StageID: stage.ID, Team1ID: p.A, Team2ID: p.B, Round: p.Round}
			if e := validateMatch(tx, &m); e != nil {
				return e
			}
			if m.Team2ID == nil {
				m.IsBye = true
				m.Status = "approved"
				m.Published = true
			}
			if e := tx.Create(&m).Error; e != nil {
				return e
			}
			out = append(out, m)
		}
		stage.Round = round
		return tx.Save(&stage).Error
	})
	if e != nil {
		return apiError(c, 409, domainError(e))
	}
	return c.JSON(fiber.Map{"matches": out})
}
