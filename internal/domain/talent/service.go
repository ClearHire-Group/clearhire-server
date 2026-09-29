package talent

import (
	"context"
	"fmt"

	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// RegisterManualFunc grava um cadastro manual e devolve o id do talento. Implementado pelo domínio
// candidate (candidate.Service.RegisterManualTalent), que é onde mora toda escrita de perfil —
// ligado na factory, sem import entre os dois domínios.
type RegisterManualFunc func(ctx context.Context, companyID string, in ManualInput) (string, error)

// AssessFunc pede à IA uma leitura qualitativa de UM perfil do banco contra a vaga de uma campanha
// real — implementado por candidate.Service.AssessTalentForCampaign (mesmo llm.Assessor, mesmo
// orçamento mensal, mesma sanitização e idempotência já usados pra avaliar candidato no funil),
// ligado na factory. A assinatura usa só tipos de pkg/llm (pacote neutro, sem dono de domínio —
// nem talent nem candidate "são donos" dele) — por isso, diferente de RegisterManualFunc, não
// precisa de conversão nenhuma na factory: os dois domínios já falam a mesma língua aqui.
type AssessFunc func(ctx context.Context, companyID, campaignID, talentID string, profile llm.CandidateContext) (*llm.Assessment, error)

// maxAssessedTalentsPerRequest é o teto de talentos avaliados por chamada — o ponto exato onde
// "poderoso" (documentos/banco-de-talentos-recomendacao-plano.md) e "barato" se encontram: a IA
// nunca roda sobre a lista inteira do match reverso, só sobre o recorte pequeno que o recrutador já
// escolheu olhar de perto.
const maxAssessedTalentsPerRequest = 5

// TalentRecommendation é o talento mais a leitura da IA sobre ele para ESTA vaga — nunca
// persistido como atributo do talento (ver invariante em migrations/0001 e no documento de
// especificação, seção 6.3/11); o que fica gravado é o evento em campaign_talent_recommendations,
// escopado à campanha, não uma propriedade do Talent.
type TalentRecommendation struct {
	Talent     Talent
	Assessment llm.Assessment
}

type Service interface {
	List(ctx context.Context, companyID string) ([]Talent, error)
	Get(ctx context.Context, companyID, id string) (*Talent, error)
	RegisterManual(ctx context.Context, companyID string, in ManualInput) (*Talent, error)
	MarkFirstContact(ctx context.Context, companyID, id string) (*Talent, error)
	Coverage(ctx context.Context, companyID string) ([]CoverageEntry, error)
	// ReverseMatch é o "match reverso" (documento de especificação, seção 8.2) — ver reversematch.go.
	ReverseMatch(ctx context.Context, companyID string, criteria MatchCriteria) ([]TalentMatch, error)
	// AssessForCampaign é a etapa 2 do match reverso — leitura de IA sob demanda, capada a
	// maxAssessedTalentsPerRequest, sempre contra uma campanha JÁ CRIADA (nunca um rascunho: a
	// idempotência por (campaignID, talentID) só existe depois que a campanha tem id de verdade).
	AssessForCampaign(ctx context.Context, companyID, campaignID string, talentIDs []string) ([]TalentRecommendation, error)
}

type service struct {
	repo           Repository
	registerManual RegisterManualFunc
	assess         AssessFunc
}

func NewService(repo Repository, registerManual RegisterManualFunc, assess AssessFunc) Service {
	return &service{repo: repo, registerManual: registerManual, assess: assess}
}

func (s *service) List(ctx context.Context, companyID string) ([]Talent, error) {
	talents, err := s.repo.ListInBank(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao listar talentos")
	}
	return talents, nil
}

func (s *service) Get(ctx context.Context, companyID, id string) (*Talent, error) {
	t, err := s.repo.FindInBank(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar talento")
	}
	if t == nil {
		return nil, apperror.NotFound("talento não encontrado")
	}
	return t, nil
}

func (s *service) RegisterManual(ctx context.Context, companyID string, in ManualInput) (*Talent, error) {
	id, err := s.registerManual(ctx, companyID, in)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, companyID, id)
}

func (s *service) MarkFirstContact(ctx context.Context, companyID, id string) (*Talent, error) {
	found, err := s.repo.MarkFirstContact(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao registrar contato")
	}
	if !found {
		return nil, apperror.NotFound("talento não encontrado")
	}
	return s.Get(ctx, companyID, id)
}

func (s *service) Coverage(ctx context.Context, companyID string) ([]CoverageEntry, error) {
	entries, err := s.repo.Coverage(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular cobertura")
	}
	return entries, nil
}

// AssessForCampaign busca cada talento pedido, no PERFIL COMPLETO (FindInBank, não o resumo de
// ListInBank que ReverseMatch usa — aqui a experiência descrita importa pra qualidade da leitura),
// monta o llm.CandidateContext e delega a avaliação em si a AssessFunc (orçamento, sanitização e
// idempotência inteiramente por conta de quem implementa — ver candidate.Service.AssessTalentForCampaign).
//
// Id que não existe ou não é desta empresa é ignorado, não aborta o lote — mesma filosofia de
// campaign.Repository.CreateCandidatesFromTalents: a seleção veio de uma tela (o ranking do match
// reverso) que pode estar um passo desatualizada.
func (s *service) AssessForCampaign(ctx context.Context, companyID, campaignID string, talentIDs []string) ([]TalentRecommendation, error) {
	if s.assess == nil {
		return nil, apperror.Unavailable("a análise por IA não está habilitada neste ambiente")
	}
	if len(talentIDs) == 0 {
		return nil, apperror.BadRequest("selecione ao menos um talento")
	}
	if len(talentIDs) > maxAssessedTalentsPerRequest {
		return nil, apperror.BadRequest(fmt.Sprintf("no máximo %d talentos por pedido", maxAssessedTalentsPerRequest))
	}

	out := make([]TalentRecommendation, 0, len(talentIDs))
	for _, id := range talentIDs {
		t, err := s.repo.FindInBank(ctx, companyID, id)
		if err != nil {
			return nil, apperror.Internal("falha ao buscar talento")
		}
		if t == nil {
			continue
		}
		assessment, err := s.assess(ctx, companyID, campaignID, id, candidateContextFromTalent(*t))
		if err != nil {
			// Orçamento estourado, provedor fora do ar etc.: propaga na hora — o recrutador vê a
			// mensagem certa (mapAssessmentError já traduz) em vez de um lote parcial confuso.
			return nil, err
		}
		out = append(out, TalentRecommendation{Talent: *t, Assessment: *assessment})
	}
	return out, nil
}

// candidateContextFromTalent espelha buildAssessInput em candidate/assessment.go: mesma
// concatenação de educação, mesmo formato de experiência — os dois perfis (Candidate e Talent)
// alimentam o MESMO contrato de IA, porque é o mesmo tipo de julgamento (aderência de um perfil a
// uma vaga), só a origem do dado muda.
func candidateContextFromTalent(t Talent) llm.CandidateContext {
	education := t.EducationDegree
	if t.EducationInstitution != "" {
		if education != "" {
			education += " — "
		}
		education += t.EducationInstitution
	}
	experience := make([]llm.ExperienceEntry, len(t.Experience))
	for i, e := range t.Experience {
		experience[i] = llm.ExperienceEntry{Role: e.Role, Company: e.Company, PeriodLabel: e.PeriodLabel, Description: e.Description}
	}
	skills := make([]string, len(t.Skills))
	for i, sk := range t.Skills {
		skills[i] = sk.Term
	}
	return llm.CandidateContext{
		YearsExperience: t.YearsExperience,
		Summary:         t.Summary,
		Education:       education,
		Experience:      experience,
		Skills:          skills,
	}
}
