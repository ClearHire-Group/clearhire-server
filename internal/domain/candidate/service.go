package candidate

import (
	"context"
	"errors"
	"fmt"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/activity"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

// DecideResult é o mínimo que o frontend precisa de volta depois de uma decisão — Phase pro
// "avançar" (a tela mostra "Avançado para: <fase>") e TalentID pro "reprovar" quando a reprovação
// qualificada criou um talento (a tela linka pro perfil dele). Nunca a entidade inteira: nem
// Candidate nem Talent completos fazem sentido aqui — ver clearhire-app
// candidate-profile.component.ts (approveAndAdvance/confirmReject), que só lê esses dois campos.
type DecideResult struct {
	Phase    string
	TalentID *string
}

type Service interface {
	Get(ctx context.Context, companyID, id string) (*CandidateDetail, error)
	ListByCampaign(ctx context.Context, companyID, campaignID, phaseKey string) ([]Candidate, error)
	// Decide roda avançar/reprovar numa única transação — mover de fase (ou reprovar + linkar
	// talento) e gravar a linha de candidate_decisions são um evento só, nunca dois.
	Decide(ctx context.Context, companyID, id string, req DecideRequest, decidedByUserID string) (*DecideResult, error)
	// RegisterPublicApplication é a escrita da candidatura pública — chamada de uma rota sem
	// tenant nenhum (companyID vem da própria campanha, nunca de quem chama). Cria talent+candidate
	// juntos numa única transação, igual Decide já faz pra talent+candidate_decisions.
	RegisterPublicApplication(ctx context.Context, campaignID string, in PublicApplicationInput) error
}

type service struct {
	repo      Repository
	withTx    func(ctx context.Context, fn func(db database.DB) error) error
	extractor llm.Extractor
}

func NewService(repo Repository, withTx func(ctx context.Context, fn func(db database.DB) error) error, extractor llm.Extractor) Service {
	return &service{repo: repo, withTx: withTx, extractor: extractor}
}

func (s *service) Get(ctx context.Context, companyID, id string) (*CandidateDetail, error) {
	d, err := s.repo.FindByID(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar candidato")
	}
	if d == nil {
		return nil, apperror.NotFound("candidato não encontrado")
	}
	return d, nil
}

func (s *service) ListByCampaign(ctx context.Context, companyID, campaignID, phaseKey string) ([]Candidate, error) {
	candidates, err := s.repo.ListByCampaign(ctx, companyID, campaignID, phaseKey)
	if err != nil {
		return nil, apperror.Internal("falha ao listar candidatos")
	}
	return candidates, nil
}

func (s *service) Decide(ctx context.Context, companyID, id string, req DecideRequest, decidedByUserID string) (*DecideResult, error) {
	if req.Decision == "reprovar" && (req.RejectionReasonKey == nil || *req.RejectionReasonKey == "") {
		return nil, apperror.BadRequest("motivo de reprovação é obrigatório")
	}

	var result *DecideResult
	err := s.withTx(ctx, func(db database.DB) error {
		txRepo := NewRepository(db)

		cand, err := txRepo.FindBasicByID(ctx, companyID, id)
		if err != nil {
			return apperror.Internal("falha ao buscar candidato")
		}
		if cand == nil {
			return apperror.NotFound("candidato não encontrado")
		}
		// Reprovado/contratado são estados finais — não dá pra decidir de novo sobre quem já saiu
		// do funil (evita um "avançar" acidental reabrir um candidato já fechado).
		if cand.Status == StatusRejected || cand.Status == StatusHired {
			return apperror.BadRequest("este candidato já teve uma decisão final registrada")
		}

		// A avaliação de IA que estava na tela quando a decisão foi tomada — nil quando o
		// candidato ainda não foi avaliado (decisão manual sem sugestão da IA por trás, que
		// também é um caso válido, só não computável na métrica de "confiança na IA").
		assessmentID, err := txRepo.LatestAssessmentID(ctx, id)
		if err != nil {
			return apperror.Internal("falha ao buscar avaliação da IA")
		}
		var assessmentIDPtr *string
		if assessmentID != "" {
			assessmentIDPtr = &assessmentID
		}

		// Mesmo db da tx: a linha do feed e a decisão são o mesmo evento — uma decisão que deu
		// rollback não pode deixar rastro no histórico.
		feed := activity.NewRepository(db)

		if req.Decision == "avancar" {
			result, err = advance(ctx, txRepo, feed, companyID, cand, assessmentIDPtr, decidedByUserID)
		} else {
			result, err = reject(ctx, txRepo, feed, companyID, cand, req, assessmentIDPtr, decidedByUserID)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// nextPhase acha a fase com a MENOR posição maior que a atual — a ordem é a posição configurada
// pela campanha (campaign_phases.position), nunca a ordem do enum phase_key (a tela Nova Campanha
// deixa reordenar os módulos opcionais). nil quando já está na última fase configurada.
func nextPhase(phases []PhaseRow, currentKey string) *PhaseRow {
	currentPos := -1
	for _, p := range phases {
		if p.Key == currentKey {
			currentPos = p.Position
			break
		}
	}
	if currentPos == -1 {
		return nil
	}
	var best *PhaseRow
	for i := range phases {
		if phases[i].Position > currentPos && (best == nil || phases[i].Position < best.Position) {
			best = &phases[i]
		}
	}
	return best
}

func advance(ctx context.Context, repo Repository, feed activity.Repository, companyID string, cand *Candidate, assessmentID *string, decidedByUserID string) (*DecideResult, error) {
	phases, err := repo.FindPhasesByCampaign(ctx, companyID, cand.CampaignID)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar fases da campanha")
	}
	next := nextPhase(phases, cand.PhaseKey)
	if next == nil {
		return nil, apperror.BadRequest("candidato já está na última fase do funil")
	}

	// Chegar numa fase nova é sempre "aguardando a próxima triagem" — mesmo status que um
	// candidato recém-chegado em 'recebidos' tem, só que não existe pipeline de IA rodando de
	// verdade ainda (ver internal/domain/talent, mesmo estágio). Selecionados é diferente: é a
	// última fase fixa, então virar 'proposta' é o que o funil já assume por padrão (ver mock data
	// original — status 'Proposta em elaboração' é o caso comum ali, 'Contratada' é um estado
	// manual posterior que esta versão não cobre).
	nextStatus := StatusAwaitingAI
	if next.Key == PhaseSelecionados {
		nextStatus = StatusProposal
	}

	matched, err := repo.AdvancePhase(ctx, companyID, cand.ID, next.Key, nextStatus)
	if err != nil {
		return nil, apperror.Internal("falha ao avançar candidato")
	}
	if !matched {
		return nil, apperror.NotFound("candidato não encontrado")
	}

	toPhase := next.Key
	if err := repo.CreateDecision(ctx, &Decision{
		CompanyID: companyID, CandidateID: cand.ID, DecidedByUserID: decidedByUserID,
		AssessmentID: assessmentID, Decision: "avancar", FromPhase: cand.PhaseKey, ToPhase: &toPhase,
	}); err != nil {
		return nil, apperror.Internal("falha ao registrar decisão")
	}

	if err := feed.Create(ctx, &activity.Entry{
		CompanyID: companyID, CampaignID: &cand.CampaignID,
		Actor: activity.ActorRecruiter, ActorUserID: &decidedByUserID,
		Message: fmt.Sprintf("%s avançou para %s.", cand.Name, phaseLabel(next.Key)),
	}); err != nil {
		return nil, apperror.Internal("falha ao registrar atividade")
	}

	return &DecideResult{Phase: next.Key}, nil
}

func reject(ctx context.Context, repo Repository, feed activity.Repository, companyID string, cand *Candidate, req DecideRequest, assessmentID *string, decidedByUserID string) (*DecideResult, error) {
	reasonKey := *req.RejectionReasonKey

	var talentIDPtr *string
	if goesToBank[reasonKey] {
		// sendBankInvite reflete o checkbox "Enviar convite ao Banco de Talentos" (ver
		// candidate-profile.component.html) — é o RH decidindo mandar (ou não) o convite de
		// consentimento pra pessoa, não a pessoa já tendo respondido. Por isso consent_state vira
		// 'notificado' (convite enviado, aguardando resposta) quando marcado, nunca 'consentido'
		// direto — essa transição (resposta real da pessoa) é fluxo do Banco de Talentos, que
		// ainda não existe. Sem o convite, fica como todo cadastro qualificado sem contato:
		// legítimo interesse, não notificado.
		legalBasis, consentState := "legitimo_interesse", "nao_notificado"
		if req.SendBankInvite {
			legalBasis, consentState = "consentimento", "notificado"
		}
		talentID, err := repo.CreateTalentFromRejection(ctx, &TalentSeed{
			CompanyID: companyID, Name: cand.Name, Email: cand.Email, City: cand.City, State: cand.State,
			Origin: "reprovacao_qualificada", LegalBasis: legalBasis, ConsentState: consentState,
		})
		if err != nil {
			return nil, apperror.Internal("falha ao registrar talento")
		}
		talentIDPtr = &talentID
	}

	matched, err := repo.Reject(ctx, companyID, cand.ID, reasonKey, talentIDPtr)
	if err != nil {
		return nil, apperror.Internal("falha ao reprovar candidato")
	}
	if !matched {
		return nil, apperror.NotFound("candidato não encontrado")
	}

	if err := repo.CreateDecision(ctx, &Decision{
		CompanyID: companyID, CandidateID: cand.ID, DecidedByUserID: decidedByUserID,
		AssessmentID: assessmentID, Decision: "reprovar", FromPhase: cand.PhaseKey, RejectionReasonKey: &reasonKey,
	}); err != nil {
		return nil, apperror.Internal("falha ao registrar decisão")
	}

	message := fmt.Sprintf("Reprovação registrada para %s em %s.", cand.Name, phaseLabel(cand.PhaseKey))
	if talentIDPtr != nil {
		message = fmt.Sprintf("Reprovação registrada para %s em %s — perfil adicionado ao Banco de Talentos.", cand.Name, phaseLabel(cand.PhaseKey))
	}
	if err := feed.Create(ctx, &activity.Entry{
		CompanyID: companyID, CampaignID: &cand.CampaignID,
		Actor: activity.ActorRecruiter, ActorUserID: &decidedByUserID,
		Message: message,
	}); err != nil {
		return nil, apperror.Internal("falha ao registrar atividade")
	}

	return &DecideResult{TalentID: talentIDPtr}, nil
}

func (s *service) RegisterPublicApplication(ctx context.Context, campaignID string, in PublicApplicationInput) error {
	if !in.Consent {
		return apperror.BadRequest("é necessário autorizar o uso dos seus dados para se candidatar")
	}
	// Honeypot preenchido = bot. Resposta de sucesso falsa — não avisa que foi pego, não grava nada.
	if in.Honeypot != "" {
		return nil
	}

	// Reconfirma a elegibilidade da campanha no momento da escrita — nunca confia que o handler já
	// validou (mesmo se a rota só existe pra campanhas elegíveis, o estado pode ter mudado entre a
	// pessoa abrir o link e enviar o formulário).
	ref, err := s.repo.FindPublicCampaignRef(ctx, campaignID)
	if err != nil {
		return apperror.Internal("falha ao verificar vaga")
	}
	if ref == nil {
		return apperror.NotFound("vaga não encontrada")
	}

	if in.Name == "" {
		return apperror.BadRequest("nome é obrigatório")
	}

	profile, err := s.resolveApplicationProfile(ctx, in)
	if err != nil {
		return mapExtractionError(err)
	}
	if profile.Email == "" {
		return apperror.BadRequest("e-mail é obrigatório")
	}

	dup, err := s.repo.FindExistingApplication(ctx, campaignID, profile.Email)
	if err != nil {
		return apperror.Internal("falha ao verificar candidatura")
	}
	if dup {
		return apperror.BadRequest("você já se candidatou a esta vaga")
	}

	return s.withTx(ctx, func(db database.DB) error {
		txRepo := NewRepository(db)

		talentID, err := txRepo.CreateTalentFromPublicApplication(ctx, &TalentSeed{
			CompanyID: ref.CompanyID, Name: profile.Name, Email: profile.Email, Phone: profile.Phone,
			LinkedInURL: profile.LinkedInURL, City: profile.City, State: profile.State,
			Modality: profile.Modality, Seniority: profile.Seniority, YearsExperience: profile.YearsExperience,
			SalaryMin: profile.SalaryMin, SalaryMax: profile.SalaryMax, AvailableFrom: profile.AvailableFrom,
			AvailabilityNote: profile.AvailabilityNote, Summary: profile.Summary,
			EducationDegree: profile.EducationDegree, EducationInstitution: profile.EducationInstitution,
			EducationPeriod: profile.EducationPeriod,
			// Consentimento capturado explicitamente no formulário (checkbox obrigatório, checado
			// acima) — diferente da reprovação qualificada, aqui é a própria pessoa se candidatando
			// que autoriza, não um convite pós-reprovação. legal_basis='consentimento' de saída,
			// não 'legitimo_interesse': foi pedido e dado, não presumido.
			Origin: "candidatura_publica", LegalBasis: "consentimento", ConsentState: "consentido",
		})
		if err != nil {
			return apperror.Internal("falha ao registrar talento")
		}

		candidateID, err := txRepo.CreateCandidate(ctx, &CandidateSeed{
			CompanyID: ref.CompanyID, CampaignID: campaignID, TalentID: talentID,
			Name: profile.Name, Email: profile.Email, Phone: profile.Phone, LinkedInURL: profile.LinkedInURL,
			City: profile.City, State: profile.State, YearsExperience: profile.YearsExperience,
			Summary: profile.Summary, EducationDegree: profile.EducationDegree,
			EducationInstitution: profile.EducationInstitution, EducationPeriod: profile.EducationPeriod,
		})
		if err != nil {
			return apperror.Internal("falha ao registrar candidatura")
		}

		if len(profile.Experience) > 0 {
			entries := toCandidateExperience(profile.Experience)
			if err := txRepo.InsertCandidateExperience(ctx, candidateID, entries); err != nil {
				return apperror.Internal("falha ao registrar experiência")
			}
			if err := txRepo.InsertTalentExperience(ctx, talentID, entries); err != nil {
				return apperror.Internal("falha ao registrar experiência")
			}
		}

		// Duas formas de achar skill, dependendo de onde o perfil veio: modo manual dá termos
		// soltos digitados pela pessoa (precisa casar contra a taxonomia, termo que não bate vai
		// pra fila de revisão); modo currículo (texto/PDF) não tem lista nenhuma — é um dicionário
		// escaneando o texto bruto por qualquer termo/sinônimo já cadastrado (determinístico, sem
		// "quase-match", por isso não gera fila de revisão aqui).
		var resolved []ResolvedSkill
		if in.Manual != nil {
			var unmapped []llm.SkillMention
			resolved, unmapped, err = txRepo.ResolveSkills(ctx, profile.Skills)
			if err != nil {
				return apperror.Internal("falha ao resolver skills")
			}
			for _, u := range unmapped {
				// Melhor esforço — um termo que a fila de revisão não conseguiu gravar não pode
				// derrubar a candidatura inteira.
				_ = txRepo.QueueSkillReview(ctx, ref.CompanyID, u.Term)
			}
		} else if profile.RawText != "" {
			resolved, err = txRepo.FindSkillMentionsInText(ctx, profile.RawText)
			if err != nil {
				return apperror.Internal("falha ao identificar skills")
			}
		}
		if len(resolved) > 0 {
			if err := txRepo.InsertCandidateSkills(ctx, candidateID, resolved); err != nil {
				return apperror.Internal("falha ao registrar skills")
			}
			if err := txRepo.InsertTalentSkills(ctx, talentID, resolved); err != nil {
				return apperror.Internal("falha ao registrar skills")
			}
		}

		if len(profile.Sectors) > 0 {
			if err := txRepo.InsertTalentSectors(ctx, talentID, profile.Sectors); err != nil {
				return apperror.Internal("falha ao registrar setores")
			}
		}
		if len(profile.Languages) > 0 {
			if err := txRepo.InsertTalentLanguages(ctx, talentID, profile.Languages); err != nil {
				return apperror.Internal("falha ao registrar idiomas")
			}
		}
		if profile.RawText != "" {
			if err := txRepo.InsertTalentSourceDocument(ctx, talentID, profile.RawText); err != nil {
				return apperror.Internal("falha ao registrar currículo")
			}
		}

		// Actor 'ia' e ActorUserID nil: não há usuário logado nesta rota (candidato anônimo pelo link
		// público) e actor_type não tem valor pra "candidato". 'ia' é o mesmo enquadramento que o
		// front já usava pra chegada de candidatura ("IA recebeu e organizou N candidaturas").
		if err := activity.NewRepository(db).Create(ctx, &activity.Entry{
			CompanyID: ref.CompanyID, CampaignID: &campaignID,
			Actor: activity.ActorAI, ActorUserID: nil,
			Message: fmt.Sprintf("Nova candidatura recebida pelo link público: %s.", profile.Name),
		}); err != nil {
			return apperror.Internal("falha ao registrar atividade")
		}
		return nil
	})
}

// resolveApplicationProfile mapeia o modo manual direto (sem IA nenhuma — é o caminho mais
// confiável, ver ManualApplicationFields) ou chama o Extractor pro modo currículo (texto colado ou
// PDF). Exatamente um dos dois roda por chamada.
func (s *service) resolveApplicationProfile(ctx context.Context, in PublicApplicationInput) (*llm.ExtractedProfile, error) {
	if in.Manual != nil {
		m := in.Manual
		skills := make([]llm.SkillMention, len(m.Skills))
		for i, term := range m.Skills {
			skills[i] = llm.SkillMention{Term: term}
		}
		experience := make([]llm.ExperienceEntry, len(m.Experience))
		for i, e := range m.Experience {
			experience[i] = llm.ExperienceEntry{Role: e.Role, Company: e.Company, PeriodLabel: e.PeriodLabel, Description: e.Description}
		}
		return &llm.ExtractedProfile{
			Name: in.Name, Email: in.Email, Phone: m.Phone, LinkedInURL: m.LinkedInURL,
			City: m.City, State: m.State, YearsExperience: m.YearsExperience, Summary: m.Summary,
			EducationDegree: m.EducationDegree, EducationInstitution: m.EducationInstitution,
			EducationPeriod: m.EducationPeriod, Experience: experience, Skills: skills,
		}, nil
	}

	if s.extractor == nil {
		return nil, llm.ErrProviderUnavailable
	}
	profile, err := s.extractor.Extract(ctx, llm.Input{Text: in.ResumeText, PDFBytes: in.PDFBytes})
	if err != nil {
		return nil, err
	}
	// Nome e e-mail são sempre exigidos no formulário (ver DTOs), nos dois modos — nunca dependem
	// de o extrator ter reconhecido certo (o determinístico nem tenta reconhecer nome, ver
	// pkg/llm/deterministic; mesmo se um extrator futuro tentasse, o dado que a própria pessoa
	// digitou é sempre mais confiável).
	profile.Name = in.Name
	if profile.Email == "" {
		profile.Email = in.Email
	}
	return profile, nil
}

func toCandidateExperience(entries []llm.ExperienceEntry) []ExperienceEntry {
	out := make([]ExperienceEntry, len(entries))
	for i, e := range entries {
		out[i] = ExperienceEntry{Role: e.Role, Company: e.Company, PeriodLabel: e.PeriodLabel, Description: e.Description}
	}
	return out
}

// mapExtractionError nunca deixa o erro cru do provedor (ou o texto do currículo) vazar pro
// candidato — toda falha de IA vira a mesma sugestão acionável: tentar o formulário manual.
func mapExtractionError(err error) error {
	if errors.Is(err, llm.ErrRefused) || errors.Is(err, llm.ErrMalformedOutput) ||
		errors.Is(err, llm.ErrRateLimited) || errors.Is(err, llm.ErrProviderUnavailable) {
		return apperror.BadRequest("não foi possível processar seu currículo automaticamente. Tente o formulário manual.")
	}
	return apperror.Internal("falha ao processar candidatura")
}
