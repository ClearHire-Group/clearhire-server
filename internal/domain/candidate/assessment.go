package candidate

import (
	"context"
	"errors"
	"fmt"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/llmusage"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// stageFocus é o que se pede à IA em CADA fase, além da avaliação geral de sempre — a variável que
// torna a mesma chamada "ciente da fase" sem multiplicar prompts distintos (ver docs/API.md e
// clearhire-server/CLAUDE.md). Vazio em recebidos/selecionados: ali a avaliação geral já É o
// insight da fase, uma segunda camada de texto só repetiria matchNote/justification.
//
// Um mapa por phase_key, não um switch espalhado pelo código — se um dia a empresa puder
// personalizar as próprias fases (campaign_phases hoje só reordena um subconjunto fixo de
// fit/tecnica/entrevista, não cria fases novas), este é o único lugar que precisaria virar dado em
// vez de constante.
var stageFocus = map[string]string{
	PhaseFit: "Avalie especificamente indícios de compatibilidade com o AMBIENTE E A CULTURA de " +
		"trabalho descritos na vaga — trajetória em times com estilo de trabalho semelhante, sinais " +
		"de autonomia, comunicação, colaboração. Não é sobre habilidade técnica.",
	PhaseTecnica: "Avalie especificamente a ADERÊNCIA TÉCNICA aos requisitos da vaga — skills, anos " +
		"de experiência com cada tecnologia pedida, profundidade demonstrada nas descrições de " +
		"experiência.",
	// Fase Entrevista: hoje o perfil do candidato não tem nenhum campo de nota/feedback de
	// entrevista (isso é lacuna de produto, não deste prompt — ver análise da tela de Funil). O
	// foco pede exatamente isso: se não há nada no perfil além do que as fases anteriores já
	// viram, dizer "insuficiente" é a resposta CORRETA, não uma falha.
	PhaseEntrevista: "Avalie se HÁ ALGO NO PERFIL que vá além do que já foi avaliado nas fases " +
		"anteriores (ver <fase_anterior>, se houver). Se o perfil disponível é o mesmo currículo já " +
		"visto antes, sem nenhuma nota ou registro novo de entrevista, isso não é evidência nova — " +
		"devolva confidence \"insuficiente\" em vez de reafirmar a mesma conclusão como se fosse novidade.",
}

// priorStageSummary monta o resumo curto que vai pro modelo em <fase_anterior> — não o JSON
// inteiro da avaliação anterior, só o essencial pra comparar (ver llm.AssessInput.PriorStageSummary
// pro porquê disso custar uma chamada só, não duas).
func priorStageSummary(prior *AIAssessment, priorPhaseKey string) string {
	if prior == nil {
		return ""
	}
	note := prior.StageInsight
	if note == "" {
		note = prior.MatchNote
	}
	return fmt.Sprintf("Fase %s, confiança %s: %s", phaseLabel(priorPhaseKey), prior.Confidence, note)
}

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

	// Perfil sem nada pra avaliar: pular a chamada, nunca chega a valer a pena gastar nela (ver
	// isProfileTooThinToAssess).
	if isProfileTooThinToAssess(d) {
		return s.assessInsufficientProfile(ctx, d)
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

	prior, priorPhaseKey, err := s.repo.PriorStageAssessment(ctx, d.ID, d.PhaseKey)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar avaliação de fase anterior")
	}

	assessment, usage, err := s.assessor.Assess(ctx, buildAssessInput(d, job, prior, priorPhaseKey))
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

// buildAssessInput monta o que o avaliador vê: a vaga, o perfil ESTRUTURADO do candidato (sem
// nome, e-mail, telefone nem LinkedIn — ver llm.CandidateContext), o foco desta fase (stageFocus,
// vazio se a fase não tem um) e o resumo da fase avaliada anterior, se houver.
func buildAssessInput(d *CandidateDetail, job *llm.JobContext, prior *AIAssessment, priorPhaseKey string) llm.AssessInput {
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
		StageFocus:        stageFocus[d.PhaseKey],
		PriorStageSummary: priorStageSummary(prior, priorPhaseKey),
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

// insufficientProfilePromptVersion identifica avaliações que nunca chegaram a chamar o provedor de
// IA — versão própria, nunca a do assessor real (s.assessor.PromptVersion()), por dois motivos: (a)
// nunca colide com uma avaliação de verdade no unique de (candidato, fase, prompt_version); (b) se o
// perfil for enriquecido depois (reextração, edição manual), uma chamada nova a Assess vê que o
// prompt_version salvo não é mais o que isProfileTooThinToAssess produziria e tenta de novo — dessa
// vez com o assessor de verdade.
const insufficientProfilePromptVersion = "assessment-insufficient-profile-v1"

// isProfileTooThinToAssess é o guard-rail de custo mais barato que existe: chamar a IA para um
// perfil sem resumo, formação, experiência, skills NEM anos de experiência só paga por uma resposta
// que já se sabe de graça ("sem dado pra avaliar"). Comum em candidatura manual com só nome/e-mail
// preenchidos, ou currículo do qual a extração não aproveitou nada. Não é o filtro de estágio 1
// cogitado no plano de IA (campaign_match_criteria, que reprovaria por não bater com a VAGA) — este
// nunca reprova ninguém, só evita uma chamada cujo resultado já é certo antes de perguntar.
func isProfileTooThinToAssess(d *CandidateDetail) bool {
	return d.Summary == "" &&
		d.EducationDegree == "" &&
		d.EducationInstitution == "" &&
		len(d.Experience) == 0 &&
		len(d.Skills) == 0 &&
		(d.YearsExperience == nil || *d.YearsExperience == 0)
}

// assessInsufficientProfile grava, sem chamar o provedor, o mesmo estado 'insuficiente' que o
// assessor real usa quando não há evidência pra concluir (ver AIAssessment.Confidence) — aqui a
// decisão é determinística porque não há NADA no perfil, então nem vale gastar a chamada pra
// descobrir isso. Idempotente do mesmo jeito que runAssessment (unique de SaveAssessment cobre a
// corrida entre duas requisições simultâneas).
func (s *service) assessInsufficientProfile(ctx context.Context, d *CandidateDetail) (*AIAssessment, error) {
	if existing, err := s.repo.FindAssessmentVersion(ctx, d.ID, d.PhaseKey, insufficientProfilePromptVersion); err != nil {
		return nil, apperror.Internal("falha ao consultar avaliação")
	} else if existing != nil {
		return existing, nil
	}

	assessment := &llm.Assessment{
		Confidence:         "insuficiente",
		MissingInformation: []string{"Nenhuma informação de currículo, formação, experiência ou skills foi preenchida ou extraída."},
	}
	origin := AssessmentOrigin{Provider: "deterministic", PromptVersion: insufficientProfilePromptVersion}
	err := s.withTx(ctx, func(db database.DB) error {
		_, saveErr := s.newTxRepo(db).SaveAssessment(ctx, d.ID, d.PhaseKey, assessment, origin)
		return saveErr
	})
	if err != nil {
		return nil, apperror.Internal("falha ao registrar avaliação")
	}

	stored, err := s.repo.FindAssessmentVersion(ctx, d.ID, d.PhaseKey, insufficientProfilePromptVersion)
	if err != nil || stored == nil {
		return nil, apperror.Internal("falha ao consultar avaliação")
	}
	return stored, nil
}

// AssessTalentForCampaign é a etapa 2 do match reverso (documentos/banco-de-talentos-recomendacao-
// plano.md): leitura qualitativa da IA sobre UM talento do Banco de Talentos contra a vaga de uma
// campanha real — sempre uma chamada explícita do recrutador sobre um recorte pequeno (ver
// talent.Service.AssessForCampaign, que impõe o teto), nunca automática sobre o banco inteiro.
//
// Mesmo Assessor, mesmo orçamento mensal, mesma sanitização de saída e mesma idempotência de
// Assess (acima) — a diferença é só a origem do perfil (llm.CandidateContext já pronto, não
// construído de um CandidateDetail) e onde a avaliação é lida/gravada
// (campaign_talent_recommendations, não candidate_ai_assessments). Não há StageFocus nem
// PriorStageSummary: recomendação de banco não tem "fase anterior" a comparar.
func (s *service) AssessTalentForCampaign(ctx context.Context, companyID, campaignID, talentID string, profile llm.CandidateContext) (*llm.Assessment, error) {
	if s.assessor == nil {
		return nil, apperror.Unavailable("a análise por IA não está habilitada neste ambiente")
	}

	job, err := s.repo.FindJobContext(ctx, companyID, campaignID)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar vaga")
	}
	if job == nil {
		return nil, apperror.NotFound("vaga não encontrada")
	}

	version := s.assessor.PromptVersion()
	existing, err := s.repo.FindTalentRecommendation(ctx, companyID, campaignID, talentID, version)
	if err != nil {
		return nil, apperror.Internal("falha ao consultar avaliação")
	}
	if existing != nil {
		return existing, nil
	}

	// Mesmo teto mensal de Assess/extração — um caminho de gasto só, nunca um segundo que o
	// orçamento não enxerga.
	if err := s.checkBudget(ctx, companyID); err != nil {
		s.recordTalentMatchUsage(ctx, companyID, campaignID, llm.Free(budgetProvider), llmusage.StatusFailed, err.Error())
		return nil, mapAssessmentError(err)
	}

	assessment, usage, err := s.assessor.Assess(ctx, llm.AssessInput{Job: *job, Candidate: profile})
	if err != nil {
		status := llmusage.StatusFailed
		if usage.InputTokens > 0 || usage.OutputTokens > 0 {
			status = llmusage.StatusBilledError
		}
		s.recordTalentMatchUsage(ctx, companyID, campaignID, usage, status, err.Error())
		return nil, mapAssessmentError(err)
	}
	s.recordTalentMatchUsage(ctx, companyID, campaignID, usage, llmusage.StatusSuccess, "")

	// Mesma entrada não confiável de sempre: o texto do perfil do talento pode ter sido manipulado
	// (currículo colado com instrução embutida) tanto quanto o de um candidato de verdade.
	llm.SanitizeAssessment(assessment)

	origin := AssessmentOrigin{Provider: usage.Provider, Model: usage.Model, PromptVersion: version}
	saved, err := s.repo.SaveTalentRecommendation(ctx, companyID, campaignID, talentID, assessment, origin)
	if err != nil {
		return nil, apperror.Internal("falha ao registrar avaliação")
	}
	if saved {
		return assessment, nil
	}

	// Perdeu a corrida: outra requisição gravou primeiro (duplo clique) — relê o que ficou, é o
	// mesmo que qualquer leitura seguinte vai mostrar.
	stored, err := s.repo.FindTalentRecommendation(ctx, companyID, campaignID, talentID, version)
	if err != nil || stored == nil {
		return nil, apperror.Internal("falha ao consultar avaliação")
	}
	return stored, nil
}

// recordTalentMatchUsage é o recordUsage/recordAssessmentUsage de OperationTalentMatch — trilha
// separada de propósito (ver llmusage.OperationTalentMatch) pra não misturar, no relatório de
// custo, avaliar candidato no funil com avaliar talento do banco pra vaga nova.
func (s *service) recordTalentMatchUsage(ctx context.Context, companyID, campaignID string, usage llm.Usage, status, errCode string) {
	if s.usage == nil {
		return
	}
	_ = s.usage.Record(ctx, &llmusage.Record{
		CompanyID:  companyID,
		CampaignID: &campaignID,
		Operation:  llmusage.OperationTalentMatch,
		Usage:      usage,
		Status:     status,
		ErrorCode:  errCode,
	})
}

func (s *service) newTxRepo(db database.DB) Repository {
	if s.txRepo != nil {
		return s.txRepo(db)
	}
	return NewRepository(db)
}
