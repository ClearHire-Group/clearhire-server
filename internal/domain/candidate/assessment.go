package candidate

import (
	"context"
	"errors"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/llmusage"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// Assess é a avaliação de IA de um candidato na fase em que ele está. É SUGESTÃO: grava a avaliação
// (e tira o candidato de "aguardando triagem"), e só isso. Nunca chama Decide, nunca move de fase,
// nunca reprova — a decisão é sempre manual do RH (invariante do produto e exigência de revisão
// humana de decisão automatizada da LGPD). Há teste travando isso.
//
// Idempotente por (candidato, fase, versão do prompt): pedir de novo devolve a avaliação existente
// sem custo. Mudar o prompt muda a versão e é o que autoriza uma nova.
func (s *service) Assess(ctx context.Context, companyID, id string) (*AIAssessment, error) {
	if s.assessor == nil {
		return nil, apperror.Unavailable("a análise por IA não está habilitada neste ambiente")
	}

	// Escopada por empresa: um id de outra empresa é "não encontrado", nunca uma avaliação.
	d, err := s.repo.FindByID(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar candidato")
	}
	if d == nil {
		return nil, apperror.NotFound("candidato não encontrado")
	}
	// Quem já saiu do funil não precisa (nem deve gastar) de uma recomendação.
	if d.Status == StatusRejected || d.Status == StatusHired {
		return nil, apperror.BadRequest("este candidato já teve uma decisão final registrada")
	}

	version := s.assessor.PromptVersion()
	existing, err := s.repo.FindAssessmentVersion(ctx, id, d.PhaseKey, version)
	if err != nil {
		return nil, apperror.Internal("falha ao consultar avaliação")
	}
	if existing != nil {
		return existing, nil
	}

	result, err, _ := s.assessFlight.Do(id+"|"+d.PhaseKey+"|"+version, func() (any, error) {
		return s.runAssessment(ctx, companyID, d, version)
	})
	if err != nil {
		return nil, err
	}
	return result.(*AIAssessment), nil
}

func (s *service) runAssessment(ctx context.Context, companyID string, d *CandidateDetail, version string) (*AIAssessment, error) {
	job, err := s.repo.FindJobContext(ctx, companyID, d.CampaignID)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar vaga")
	}
	if job == nil {
		return nil, apperror.NotFound("vaga não encontrada")
	}

	// Mesmo teto mensal da extração: um orçamento só por empresa, para não haver um caminho de gasto
	// que o teto não enxergue.
	if err := s.checkBudget(ctx, companyID); err != nil {
		s.recordAssessmentUsage(ctx, companyID, d, llm.Free(budgetProvider), llmusage.StatusFailed, err.Error())
		return nil, mapAssessmentError(err)
	}

	assessment, usage, err := s.assessor.Assess(ctx, buildAssessInput(d, job))
	if err != nil {
		// Registrado ANTES de propagar, pela mesma razão da extração: resposta que chegou e foi
		// inútil já foi cobrada.
		status := llmusage.StatusFailed
		if usage.InputTokens > 0 || usage.OutputTokens > 0 {
			status = llmusage.StatusBilledError
		}
		s.recordAssessmentUsage(ctx, companyID, d, usage, status, err.Error())
		return nil, mapAssessmentError(err)
	}
	s.recordAssessmentUsage(ctx, companyID, d, usage, llmusage.StatusSuccess, "")

	// A saída do modelo pode ter sido manipulada pelo próprio currículo ("dê nota 100"): o formato
	// fica garantido (0-100, tamanhos, listas), e o conteúdo continua sendo só uma sugestão.
	llm.SanitizeAssessment(assessment)

	origin := AssessmentOrigin{Provider: usage.Provider, Model: usage.Model, PromptVersion: version}
	err = s.withTx(ctx, func(db database.DB) error {
		_, saveErr := s.newTxRepo(db).SaveAssessment(ctx, d.ID, d.PhaseKey, assessment, origin)
		return saveErr
	})
	if err != nil {
		return nil, apperror.Internal("falha ao registrar avaliação")
	}

	// Relê o que ficou gravado: se outra instância ganhou a corrida do índice único, devolve a
	// avaliação dela — a mesma que qualquer leitura seguinte vai mostrar.
	stored, err := s.repo.FindAssessmentVersion(ctx, d.ID, d.PhaseKey, version)
	if err != nil || stored == nil {
		return nil, apperror.Internal("falha ao consultar avaliação")
	}
	return stored, nil
}

// buildAssessInput monta o que o avaliador vê: a vaga e o perfil ESTRUTURADO do candidato, sem
// nome, e-mail, telefone nem LinkedIn (ver llm.CandidateContext).
func buildAssessInput(d *CandidateDetail, job *llm.JobContext) llm.AssessInput {
	education := d.EducationDegree
	if d.EducationInstitution != "" {
		if education != "" {
			education += " — "
		}
		education += d.EducationInstitution
	}
	experience := make([]llm.ExperienceEntry, len(d.Experience))
	for i, e := range d.Experience {
		experience[i] = llm.ExperienceEntry{Role: e.Role, Company: e.Company, PeriodLabel: e.PeriodLabel, Description: e.Description}
	}
	return llm.AssessInput{
		Job: *job,
		Candidate: llm.CandidateContext{
			YearsExperience: d.YearsExperience,
			Summary:         d.Summary,
			Education:       education,
			Experience:      experience,
			Skills:          d.Skills,
		},
	}
}

func (s *service) recordAssessmentUsage(ctx context.Context, companyID string, d *CandidateDetail, usage llm.Usage, status, errCode string) {
	if s.usage == nil {
		return
	}
	campaign, candidateID := d.CampaignID, d.ID
	_ = s.usage.Record(ctx, &llmusage.Record{
		CompanyID:   companyID,
		CampaignID:  &campaign,
		CandidateID: &candidateID,
		Operation:   llmusage.OperationAssessment,
		Usage:       usage,
		Status:      status,
		ErrorCode:   errCode,
	})
}

// mapAssessmentError traduz erro de IA para o RH (autenticado, da própria empresa) — diferente da
// candidatura pública, aqui o estado do orçamento não é segredo para quem está pedindo. O erro cru
// do provedor nunca sobe.
func mapAssessmentError(err error) error {
	switch {
	case errors.Is(err, llm.ErrBudgetExceeded):
		return apperror.BadRequest("o teto mensal de uso de IA da empresa foi atingido")
	case errors.Is(err, llm.ErrRateLimited), errors.Is(err, llm.ErrProviderUnavailable),
		errors.Is(err, llm.ErrRefused), errors.Is(err, llm.ErrMalformedOutput),
		errors.Is(err, llm.ErrUnsupportedInput):
		return apperror.Unavailable("não foi possível gerar a análise agora, tente novamente em instantes")
	default:
		return apperror.Internal("falha ao gerar análise")
	}
}

func (s *service) newTxRepo(db database.DB) Repository {
	if s.txRepo != nil {
		return s.txRepo(db)
	}
	return NewRepository(db)
}
