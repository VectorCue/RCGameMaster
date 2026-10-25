package web

import (
	"RCJVGameMaster/src/domain"
	"errors"
	"log/slog"
	"sort"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func automaticStage(stage domain.Stage, _ domain.League) bool { return stage.Type != "manual" }
func reservedMatch(match domain.Match) bool {
	return match.SlotID != nil || match.ScheduledAt != nil || match.FieldID != nil || len(match.RefereeIDs) > 0 || match.Published
}
func stageParticipantCount(tx *gorm.DB, stage domain.Stage) (int, error) {
	n := stage.ExpectedTeams
	if n == 0 {
		var count int64
		if err := tx.Model(&domain.Team{}).Where("league_id = ? AND tournament_id = ?", stage.LeagueID, stage.TournamentID).Count(&count).Error; err != nil {
			return 0, err
		}
		n = int(count)
	}
	if n > 256 {
		return 0, errors.New("automatic stages support up to 256 teams")
	}
	if stage.TeamLimit > 0 && n > stage.TeamLimit {
		n = stage.TeamLimit
	}
	return n, nil
}
func placeholderCounts(stage domain.Stage, n int) map[int]int {
	counts := map[int]int{}
	if stage.Type == "individual" {
		if n > 0 {
			counts[1] = n
		}
		return counts
	}
	if n < 2 {
		return counts
	}
	ids := make([]uint, n)
	for i := range ids {
		ids[i] = uint(i + 1)
	}
	switch stage.Type {
	case "round_robin":
		for _, pair := range domain.RoundRobin(ids) {
			counts[pair.Round]++
		}
	case "swiss":
		counts[1] = n / 2
	case "knockout":
		for _, pair := range domain.KnockoutPairs(ids, 1) {
			if pair.B != nil {
				counts[1]++
			}
		}
	}
	return counts
}
func defaultMatchDuration(league domain.League) int {
	duration := (league.HalfSeconds*2 + league.HalftimeSeconds + 59) / 60
	if duration < 1 {
		duration = 25
	}
	return duration
}
func syncStagePlaceholders(tx *gorm.DB, stage *domain.Stage) error {
	league, err := leagueIn(tx, stage.LeagueID, stage.TournamentID)
	if err != nil {
		return err
	}
	var existing []domain.Match
	if err := tx.Where("stage_id = ?", stage.ID).Order("number,id").Find(&existing).Error; err != nil {
		return err
	}
	latest := 0
	populated := false
	for _, match := range existing {
		if !match.IsPlaceholder {
			populated = true
			if match.Round > latest {
				latest = match.Round
			}
		}
	}
	if stage.Round != latest {
		stage.Round = latest
		if err := tx.Model(&domain.Stage{}).Where("id = ?", stage.ID).Update("round", latest).Error; err != nil {
			return err
		}
	}
	counts := map[int]int{}
	if automaticStage(*stage, league) {
		n := stage.PreparedTeams
		if populated && n == 0 {
			return nil
		} // Legacy populated draws have no preparation metadata.
		if !populated {
			var err error
			n, err = stageParticipantCount(tx, *stage)
			if err != nil {
				return err
			}
			if stage.PreparedTeams != n {
				stage.PreparedTeams = n
				if err := tx.Model(&domain.Stage{}).Where("id = ?", stage.ID).Update("prepared_teams", n).Error; err != nil {
					return err
				}
			}
		}
		counts = placeholderCounts(*stage, n)
	} else if populated {
		for _, match := range existing {
			if !match.IsPlaceholder {
				continue
			}
			if reservedMatch(match) {
				return errors.New("unassign empty reservations before switching this league to manual scheduling")
			}
			if err := tx.Delete(&domain.Match{}, match.ID).Error; err != nil {
				return err
			}
		}
		return nil
	}
	grouped := map[int][]domain.Match{}
	for _, match := range existing {
		if match.IsBye {
			continue
		}
		if populated && stage.Type != "round_robin" && match.Round > 1 {
			continue
		}
		grouped[match.Round] = append(grouped[match.Round], match)
	}
	for round, rows := range grouped {
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].IsPlaceholder != rows[j].IsPlaceholder {
				return !rows[i].IsPlaceholder
			}
			a, b := reservedMatch(rows[i]), reservedMatch(rows[j])
			if a != b {
				return a
			}
			return rows[i].Number < rows[j].Number
		})
		keep := counts[round]
		if keep > len(rows) {
			keep = len(rows)
		}
		for _, excess := range rows[keep:] {
			if !excess.IsPlaceholder {
				return errors.New("clear assigned teams before resizing this stage")
			}
			if reservedMatch(excess) {
				return errors.New("the new stage size would remove reserved empty matches; unassign those slots and referees first")
			}
			if err := tx.Delete(&domain.Match{}, excess.ID).Error; err != nil {
				return err
			}
		}
		counts[round] -= keep
	}
	counter, err := matchNumberCounter(tx, stage.TournamentID)
	if err != nil {
		return err
	}
	rounds := []int{}
	for round, count := range counts {
		if count > 0 {
			rounds = append(rounds, round)
		}
	}
	sort.Ints(rounds)
	additions := []domain.Match{}
	for _, round := range rounds {
		for i := 0; i < counts[round]; i++ {
			counter++
			additions = append(additions, domain.Match{IsPlaceholder: true, TournamentID: stage.TournamentID, LeagueID: stage.LeagueID, StageID: stage.ID, Number: counter, Round: round, DurationMinutes: defaultMatchDuration(league), Status: "draft", RefereeIDs: []uint{}, LiveState: []byte("{}")})
		}
	}
	if len(additions) > 0 {
		return tx.CreateInBatches(&additions, 256).Error
	}
	return nil
}
func syncLeaguePlaceholders(tx *gorm.DB, leagueID uint) error {
	var stages []domain.Stage
	if err := tx.Where("league_id = ?", leagueID).Order("sort_order,id").Find(&stages).Error; err != nil {
		return err
	}
	for i := range stages {
		if err := syncStagePlaceholders(tx, &stages[i]); err != nil {
			return err
		}
	}
	return nil
}
func syncEntityPlaceholders(tx *gorm.DB, row any) error {
	switch entity := row.(type) {
	case *domain.Stage:
		return syncStagePlaceholders(tx, entity)
	case *domain.Team:
		return syncLeaguePlaceholders(tx, entity.LeagueID)
	case *domain.League:
		return syncLeaguePlaceholders(tx, entity.ID)
	case *domain.Match:
		return updatePopulatedRound(tx, entity.StageID)
	}
	return nil
}
func updatePopulatedRound(tx *gorm.DB, stageID uint) error {
	var latest int
	if err := tx.Model(&domain.Match{}).Where("stage_id = ? AND is_placeholder = ?", stageID, false).Select("COALESCE(MAX(round),0)").Scan(&latest).Error; err != nil {
		return err
	}
	return tx.Model(&domain.Stage{}).Where("id = ?", stageID).Update("round", latest).Error
}
func (a *API) prepareStage(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return apiError(c, 400, err.Error())
	}
	var stage domain.Stage
	matches := []domain.Match{}
	err = a.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		stage, err = lockedStage(tx, id)
		if err != nil {
			return err
		}
		if err := syncStagePlaceholders(tx, &stage); err != nil {
			return err
		}
		return tx.Where("stage_id = ?", stage.ID).Order("number,id").Find(&matches).Error
	})
	if err != nil {
		return apiError(c, 409, domainError(err))
	}
	return c.JSON(fiber.Map{"matches": matches, "stage": stage})
}

// Attach team pairings to saved empty rows, retaining each reservation's identity.
func attachPlaceholderRows(tx *gorm.DB, stage domain.Stage, matches []domain.Match, strict bool) ([]domain.Match, error) {
	rows := []domain.Match{}
	if err := tx.Where("stage_id = ? AND is_placeholder = ?", stage.ID, true).Order("number,id").Find(&rows).Error; err != nil {
		return nil, err
	}
	grouped := map[int][]domain.Match{}
	for _, row := range rows {
		grouped[row.Round] = append(grouped[row.Round], row)
	}
	needed := map[int]int{}
	for _, m := range matches {
		if !m.IsBye {
			needed[m.Round]++
		}
	}
	if strict {
		for round, count := range needed {
			if available := len(grouped[round]); available > 0 && available != count {
				return nil, errors.New("prepared match count differs from qualifying teams; clear assignments and adjust the stage expected team count first")
			}
		}
	}
	counter, err := matchNumberCounter(tx, stage.TournamentID)
	if err != nil {
		return nil, err
	}
	for i := range matches {
		match := &matches[i]
		if !match.IsBye && len(grouped[match.Round]) > 0 {
			saved := grouped[match.Round][0]
			grouped[match.Round] = grouped[match.Round][1:]
			match.ID = saved.ID
			match.Number = saved.Number
			match.FieldID = saved.FieldID
			match.SlotID = saved.SlotID
			match.ScheduledAt = saved.ScheduledAt
			match.RefereeIDs = append([]uint{}, saved.RefereeIDs...)
			match.Published = saved.Published
			match.DurationMinutes = saved.DurationMinutes
			match.CreatedAt = saved.CreatedAt
			match.UpdatedAt = saved.UpdatedAt
			if match.Notes == "" {
				match.Notes = saved.Notes
			}
			match.Status = "draft"
			if saved.SlotID != nil {
				match.Status = "scheduled"
			}
		} else {
			if !strict && !match.IsBye && stage.PreparedTeams > 0 {
				return nil, errors.New("no empty reserved matches remain in this stage round; clear assignments before replacing its draw")
			}
			counter++
			match.Number = counter
		}
	}
	return matches, nil
}
func savePopulatedMatches(tx *gorm.DB, stage domain.Stage, matches []domain.Match) error {
	for i := range matches {
		match := &matches[i]
		if stage.Type == "individual" {
			if err := validateIndividualAssignment(tx, stage.ID, match); err != nil {
				return err
			}
		}
		match.IsPlaceholder = false
		if match.ID != 0 {
			var existing domain.Match
			if err := tx.First(&existing, match.ID).Error; err != nil {
				return err
			}
			if !existing.IsPlaceholder || existing.StageID != stage.ID {
				return errors.New("empty match changed; preview the stage again")
			}
			published := match.Published
			if err := validateSchedule(tx, match, match.SlotID, match.RefereeIDs); err != nil {
				return err
			}
			match.Published = published
			if err := tx.Save(match).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Create(match).Error; err != nil {
				return err
			}
		}
	}
	return updatePopulatedRound(tx, stage.ID)
}

// Backfill existing empty stages on real server startup, never during app/test
// construction or workspace reads. One bad stage does not prevent startup.
func backfillAutomaticStages(db *gorm.DB) {
	var stages []domain.Stage
	if err := db.Order("tournament_id,id").Find(&stages).Error; err != nil {
		slog.Error("stages.backfill_failed", "error", err)
		return
	}
	for _, stage := range stages {
		err := db.Transaction(func(tx *gorm.DB) error {
			locked, err := lockedStage(tx, stage.ID)
			if err != nil {
				return err
			}
			var populated int64
			if err := tx.Model(&domain.Match{}).Where("stage_id = ? AND is_placeholder = ?", locked.ID, false).Count(&populated).Error; err != nil {
				return err
			}
			if populated > 0 {
				return nil
			}
			return syncStagePlaceholders(tx, &locked)
		})
		if err != nil {
			slog.Warn("stage.backfill_failed", "stage_id", stage.ID, "error", domainError(err))
		}
	}
}

func validateIndividualAssignment(tx *gorm.DB, stageID uint, match *domain.Match) error {
	if match.Team2ID != nil || match.IsBye {
		return errors.New("individual runs have one team and cannot be byes")
	}
	if match.Round > 1 {
		return errors.New("individual runs use exactly one round per stage")
	}
	var duplicates int64
	if err := tx.Model(&domain.Match{}).Where("stage_id = ? AND team1_id = ? AND id <> ? AND is_placeholder = ?", stageID, match.Team1ID, match.ID, false).Count(&duplicates).Error; err != nil {
		return err
	}
	if duplicates > 0 {
		return errors.New("this team already has an individual run in the stage")
	}
	return nil
}
