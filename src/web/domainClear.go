package web

import (
	"RCJVGameMaster/src/domain"
	"encoding/json"
	"errors"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

var errPartialRoundRobinClear = errors.New("round-robin games must be cleared as a whole draw; omit round")

func clearRequestedRound(c fiber.Ctx) (int, error) {
	if len(c.Body()) == 0 {
		return 0, nil
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(c.Body(), &body); err != nil || body == nil {
		return 0, errors.New("provide a JSON object with an optional positive round")
	}
	for key := range body {
		if key != "round" {
			return 0, errors.New("only the optional round field is supported")
		}
	}
	raw, present := body["round"]
	if !present {
		return 0, nil
	}
	var round *int
	if err := json.Unmarshal(raw, &round); err != nil || round == nil || *round <= 0 {
		return 0, errors.New("round must be a positive whole number when provided")
	}
	return *round, nil
}

func (a *API) clearStage(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return apiError(c, 400, domainError(err))
	}
	round, err := clearRequestedRound(c)
	if err != nil {
		return apiError(c, 400, err.Error())
	}
	var stage domain.Stage
	var cleared int64
	err = a.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		stage, err = lockedStage(tx, id)
		if err != nil {
			return err
		}
		if round > 0 && stage.Type == "round_robin" {
			return errPartialRoundRobinClear
		}
		targetsQuery := func() *gorm.DB {
			query := tx.Where("stage_id = ? AND tournament_id = ?", stage.ID, stage.TournamentID)
			if round > 0 {
				query = query.Where("round = ?", round)
			}
			return query
		}
		targets := []domain.Match{}
		if err := targetsQuery().Find(&targets).Error; err != nil {
			return err
		}
		populated := []domain.Match{}
		for _, match := range targets {
			if !match.IsPlaceholder {
				populated = append(populated, match)
			}
		}
		if len(populated) > 0 {
			for _, match := range populated {
				autoBye := match.IsBye && match.Team2ID == nil && match.Status == "approved"
				if match.Status != "draft" && match.Status != "scheduled" && !autoBye {
					return errors.New("started, submitted or reviewed matches cannot be cleared; the entire draw was left unchanged")
				}
			}
			if round > 0 {
				var later int64
				if err := tx.Model(&domain.Match{}).Where("stage_id = ? AND tournament_id = ? AND round > ? AND is_placeholder = ?", stage.ID, stage.TournamentID, round, false).Count(&later).Error; err != nil {
					return err
				}
				if later > 0 {
					return errors.New("clear later rounds before clearing this round")
				}
			}
			if err := protectSeededStageDependents(tx, stage); err != nil {
				return err
			}
			league, err := leagueIn(tx, stage.LeagueID, stage.TournamentID)
			if err != nil {
				return err
			}
			if automaticStage(stage, league) {
				for _, match := range populated {
					if match.IsBye {
						if err := tx.Delete(&domain.Match{}, match.ID).Error; err != nil {
							return err
						}
					} else {
						match.IsPlaceholder = true
						match.IsBye = false
						match.Team1ID = 0
						match.Team2ID = nil
						match.Goals1 = 0
						match.Goals2 = 0
						match.Notes = ""
						match.LiveState = []byte("{}")
						match.Status = "draft"
						if match.SlotID != nil {
							match.Status = "scheduled"
						}
						if err := tx.Save(&match).Error; err != nil {
							return err
						}
					}
					cleared++
				}
			} else {
				result := targetsQuery().Delete(&domain.Match{})
				if result.Error != nil {
					return result.Error
				}
				cleared = result.RowsAffected
			}
		}
		if cleared > 0 {
			if err := invalidateStandingPublication(tx, stage.ID); err != nil {
				return err
			}
		}
		var latest int
		if err := tx.Model(&domain.Match{}).Where("stage_id = ? AND tournament_id = ? AND is_placeholder = ?", stage.ID, stage.TournamentID, false).Select("COALESCE(MAX(round),0)").Scan(&latest).Error; err != nil {
			return err
		}
		stage.Round = latest
		if err := tx.Model(&domain.Stage{}).Where("id = ?", stage.ID).Update("round", latest).Error; err != nil {
			return err
		}
		return syncStagePlaceholders(tx, &stage)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apiError(c, 404, "Stage not found")
	}
	if errors.Is(err, errPartialRoundRobinClear) {
		return apiError(c, 400, err.Error())
	}
	if err != nil {
		return apiError(c, 409, domainError(err))
	}
	return c.JSON(fiber.Map{"cleared": cleared, "round": round, "stage": stage})
}

// Both result corrections and draw clearing must preserve downstream seeded
// decisions. Same-stage later rounds are checked separately by each operation.
func protectSeededStageDependents(tx *gorm.DB, stage domain.Stage) error {
	var stages []domain.Stage
	if err := tx.Where("tournament_id = ? AND league_id = ? AND id <> ?", stage.TournamentID, stage.LeagueID, stage.ID).Find(&stages).Error; err != nil {
		return err
	}
	for _, next := range stages {
		explicit := next.SourceStageID != nil && *next.SourceStageID == stage.ID
		inferred := next.SourceStageID == nil && (next.Type == "swiss" || next.Type == "knockout") && (next.SortOrder > stage.SortOrder || next.SortOrder == stage.SortOrder && next.ID > stage.ID)
		if !explicit && !inferred {
			continue
		}
		var count int64
		if err := tx.Model(&domain.Match{}).Where("stage_id = ? AND tournament_id = ? AND is_placeholder = ?", next.ID, stage.TournamentID, false).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("clear dependent seeded stage matches before changing their source stage")
		}
	}
	return nil
}
