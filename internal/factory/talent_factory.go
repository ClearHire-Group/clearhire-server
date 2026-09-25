package factory

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClearHire-Group/clearhire-server/internal/domain/candidate"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/talent"
)

// InitTalentFactory recebe o service de candidate porque o cadastro manual de talento é uma escrita de
// perfil (extração, skills, experiência), e toda escrita de perfil mora lá. O domínio talent só vê uma
// função — nenhum import entre os dois.
func InitTalentFactory(db *pgxpool.Pool, candidates candidate.Service) *talent.Handler {
	registerManual := func(ctx context.Context, companyID string, in talent.ManualInput) (string, error) {
		return candidates.RegisterManualTalent(ctx, companyID, candidate.ManualTalentInput{
			Name: in.Name, RawProfileText: in.RawProfileText, ContextNote: in.ContextNote,
		})
	}
	return talent.NewHandler(talent.NewService(talent.NewRepository(db), registerManual))
}
