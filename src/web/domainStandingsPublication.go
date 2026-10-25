package web

import (
	"RCJVGameMaster/src/domain"
	"errors"
	"reflect"
	"sort"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func computeStandingPublication(teams []domain.Team, matches []domain.Match, stage domain.Stage) (domain.StandingPublication, bool) {
	visible := []domain.Match{}
	sources := []uint{}
	eligible := false
	var latest time.Time
	for _, match := range matches {
		if match.TournamentID != stage.TournamentID || match.StageID != stage.ID || !match.Published || match.IsPlaceholder || match.Team1ID == 0 {
			continue
		}
		visible = append(visible, match)
		if match.Status == "approved" {
			sources = append(sources, match.ID)
			if !match.IsBye {
				eligible = true
			}
			if match.UpdatedAt.After(latest) {
				latest = match.UpdatedAt
			}
		}
	}
	if !eligible {
		return domain.StandingPublication{}, false
	}
	if latest.IsZero() {
		latest = time.Unix(0, 0).UTC()
	}
	return domain.StandingPublication{TournamentID: stage.TournamentID, LeagueID: stage.LeagueID, StageID: stage.ID, PublishedAt: latest.UTC(), SourceMatchIDs: sources, Rows: domain.Standings(teams, visible, stage.LeagueID, stage.ID)}, true
}
func effectiveStandingPublications(db *gorm.DB, workspace domain.Workspace) ([]domain.StandingPublication, error) {
	stored := []domain.StandingPublication{}
	if err := db.Where("tournament_id = ?", workspace.Tournament.ID).Find(&stored).Error; err != nil {
		return nil, err
	}
	byStage := map[uint]domain.StandingPublication{}
	for _, publication := range stored {
		if publication.Rows == nil {
			publication.Rows = []domain.Standing{}
		}
		byStage[publication.StageID] = publication
	}
	leagues := map[uint]domain.League{}
	for _, league := range workspace.Leagues {
		leagues[league.ID] = league
	}
	approvedPublic := map[uint]bool{}
	for _, match := range workspace.Matches {
		if match.Status == "approved" && match.Published && !match.IsPlaceholder {
			approvedPublic[match.ID] = true
		}
	}
	result := []domain.StandingPublication{}
	for _, stage := range workspace.Stages {
		league, found := leagues[stage.LeagueID]
		if !found {
			continue
		}
		publication, exists := byStage[stage.ID]
		current, eligible := computeStandingPublication(workspace.Teams, workspace.Matches, stage)
		if league.AutoPublishStandings() {
			if !eligible {
				continue
			}
			// A manual publication in auto mode remains the same publication until the table changes.
			if !exists || !reflect.DeepEqual(publication.Rows, current.Rows) {
				publication = current
			} else {
				publication.SourceMatchIDs = current.SourceMatchIDs
			}
		} else {
			if !exists || !eligible {
				continue
			}
			valid := true
			for _, id := range publication.SourceMatchIDs {
				if !approvedPublic[id] {
					valid = false
					break
				}
			}
			if !valid {
				continue
			}
		}
		result = append(result, publication)
	}
	return result, nil
}
func latestStandingPublications(stages []domain.Stage, publications []domain.StandingPublication) []domain.StandingPublication {
	ordered := append([]domain.Stage{}, stages...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].SortOrder != ordered[j].SortOrder {
			return ordered[i].SortOrder < ordered[j].SortOrder
		}
		return ordered[i].ID < ordered[j].ID
	})
	byStage := map[uint]domain.StandingPublication{}
	for _, publication := range publications {
		byStage[publication.StageID] = publication
	}
	byLeague := map[uint]domain.StandingPublication{}
	for _, stage := range ordered {
		if publication, found := byStage[stage.ID]; found {
			byLeague[stage.LeagueID] = publication
		}
	}
	result := []domain.StandingPublication{}
	for _, publication := range byLeague {
		result = append(result, publication)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LeagueID < result[j].LeagueID })
	return result
}
func findStandingPublication(publications []domain.StandingPublication, leagueID, stageID uint) *domain.StandingPublication {
	for _, publication := range publications {
		if publication.LeagueID == leagueID && (stageID == 0 || publication.StageID == stageID) {
			copy := publication
			return &copy
		}
	}
	return nil
}
func latestLeagueStandingPublication(stages []domain.Stage, publications []domain.StandingPublication, leagueID uint) *domain.StandingPublication {
	return findStandingPublication(latestStandingPublications(stages, publications), leagueID, 0)
}
func publicationInputWorkspace(tx *gorm.DB, tournamentID uint) (domain.Workspace, error) {
	w := domain.Workspace{Leagues: []domain.League{}, Stages: []domain.Stage{}, Teams: []domain.Team{}, Matches: []domain.Match{}}
	if err := tx.First(&w.Tournament, tournamentID).Error; err != nil {
		return w, err
	}
	for _, part := range []struct {
		out   any
		order string
	}{{&w.Leagues, "id"}, {&w.Stages, "sort_order,id"}, {&w.Teams, "id"}, {&w.Matches, "id"}} {
		if err := tx.Where("tournament_id = ?", tournamentID).Order(part.order).Find(part.out).Error; err != nil {
			return w, err
		}
	}
	return w, nil
}
func storeStandingPublication(tx *gorm.DB, publication *domain.StandingPublication) error {
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "stage_id"}}, DoUpdates: clause.AssignmentColumns([]string{"tournament_id", "league_id", "published_at", "rows", "source_match_ids"})}).Create(publication).Error
}
func freezeAutomaticStandings(tx *gorm.DB, league domain.League) error {
	w, err := publicationInputWorkspace(tx, league.TournamentID)
	if err != nil {
		return err
	}
	publications, err := effectiveStandingPublications(tx, w)
	if err != nil {
		return err
	}
	// Replace the persisted history with the exact set currently visible in auto mode.
	if err := tx.Where("tournament_id = ? AND league_id = ?", league.TournamentID, league.ID).Delete(&domain.StandingPublication{}).Error; err != nil {
		return err
	}
	for _, publication := range publications {
		if publication.LeagueID != league.ID {
			continue
		}
		publication.ID = 0
		if err := storeStandingPublication(tx, &publication); err != nil {
			return err
		}
	}
	return nil
}
func invalidateStandingPublication(tx *gorm.DB, stageID uint) error {
	return tx.Where("stage_id = ?", stageID).Delete(&domain.StandingPublication{}).Error
}
func (a *API) publishStandings(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return apiError(c, 400, err.Error())
	}
	var publication domain.StandingPublication
	err = a.DB.Transaction(func(tx *gorm.DB) error {
		stage, err := lockedStage(tx, id)
		if err != nil {
			return err
		}
		w, err := publicationInputWorkspace(tx, stage.TournamentID)
		if err != nil {
			return err
		}
		var eligible bool
		publication, eligible = computeStandingPublication(w.Teams, w.Matches, stage)
		if !eligible {
			return errors.New("publish at least one reviewed real result before publishing standings; empty games and byes alone are not eligible")
		}
		publication.PublishedAt = time.Now().UTC()
		return storeStandingPublication(tx, &publication)
	})
	if err != nil {
		return apiError(c, 409, domainError(err))
	}
	return c.JSON(publication)
}
func (a *API) unpublishStandings(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return apiError(c, 400, err.Error())
	}
	err = a.DB.Transaction(func(tx *gorm.DB) error {
		stage, err := lockedStage(tx, id)
		if err != nil {
			return err
		}
		league, err := leagueIn(tx, stage.LeagueID, stage.TournamentID)
		if err != nil {
			return err
		}
		if league.AutoPublishStandings() {
			return errors.New("turn off automatic standings publication before unpublishing a table")
		}
		return invalidateStandingPublication(tx, stage.ID)
	})
	if err != nil {
		return apiError(c, 409, domainError(err))
	}
	return c.SendStatus(204)
}
func (a *API) standingsSettings(c fiber.Ctx) error {
	id, err := paramID(c)
	if err != nil {
		return apiError(c, 400, err.Error())
	}
	var body struct {
		AutoPublish *bool `json:"auto_publish"`
	}
	if c.Bind().Body(&body) != nil || body.AutoPublish == nil {
		return apiError(c, 400, "auto_publish must be true or false")
	}
	var league domain.League
	var latest *domain.StandingPublication
	err = a.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&league, id).Error; err != nil {
			return err
		}
		if err := lockTournament(tx, league.TournamentID); err != nil {
			return err
		}
		if err := tx.First(&league, id).Error; err != nil {
			return err
		}
		if league.AutoPublishStandings() && !*body.AutoPublish {
			if err := freezeAutomaticStandings(tx, league); err != nil {
				return err
			}
		}
		league.StandingsAutoPublish = body.AutoPublish
		if err := tx.Model(&domain.League{}).Where("id = ?", league.ID).Update("standings_auto_publish", *body.AutoPublish).Error; err != nil {
			return err
		}
		w, err := publicationInputWorkspace(tx, league.TournamentID)
		if err != nil {
			return err
		}
		publications, err := effectiveStandingPublications(tx, w)
		if err != nil {
			return err
		}
		latest = latestLeagueStandingPublication(w.Stages, publications, league.ID)
		return nil
	})
	if err != nil {
		return apiError(c, 409, domainError(err))
	}
	return c.JSON(fiber.Map{"league": league, "publication": latest})
}

// A frozen table records only results it actually included. Withdrawing a later,
// unpublished-in-this-table result must not erase that stable publication.
func invalidateStandingPublicationForMatch(tx *gorm.DB, match domain.Match) error {
	var publication domain.StandingPublication
	err := tx.Where("stage_id = ?", match.StageID).First(&publication).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(publication.SourceMatchIDs) == 0 {
		return invalidateStandingPublication(tx, match.StageID)
	}
	for _, id := range publication.SourceMatchIDs {
		if id == match.ID {
			return invalidateStandingPublication(tx, match.StageID)
		}
	}
	return nil
}
