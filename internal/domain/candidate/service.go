package candidate

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/sync/singleflight"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/activity"
	"github.com/ClearHire-Group/clearhire-server/internal/domain/llmusage"
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
	// Assess gera (ou devolve, se já existe para esta fase e versão do prompt) a sugestão da IA
	// para um candidato. Só grava a avaliação — nunca decide, avança ou reprova ninguém.
	Assess(ctx context.Context, companyID, id string) (*AIAssessment, error)
	// RegisterManualTalent é o cadastro manual de talento — ver manual_talent.go.
	RegisterManualTalent(ctx context.Context, companyID string, in ManualTalentInput) (string, error)
}

// duplicateApplicationMessage vai no campo e-mail: é ele que identifica a candidatura (ver
// applyFormOverrides), então é ele que a pessoa precisa conferir.
const duplicateApplicationMessage = "Você já se candidatou a esta vaga com este e-mail."

// cacheProvider é o "provedor" registrado quando uma extração foi servida do cache — nenhuma
// chamada externa aconteceu, e é isso que a linha em llm_usage documenta.
const cacheProvider = "cache"

// budgetProvider marca, em llm_usage, a chamada que o teto de gasto IMPEDIU de acontecer. Sem esta
// linha, estourar o orçamento seria silencioso: o gasto simplesmente pararia de crescer e não
// haveria como distinguir "ninguém se candidatou" de "recusamos todo mundo".
const budgetProvider = "budget"

// autoAssessTimeout é o teto da avaliação automática disparada depois de uma candidatura pública.
// Roda fora do request (o candidato não espera pela triagem interna), então precisa de um teto
// próprio — o do request já terá acabado.
const autoAssessTimeout = 60 * time.Second

type service struct {
	repo      Repository
	withTx    func(ctx context.Context, fn func(db database.DB) error) error
	extractor llm.Extractor
	// assessor é nil quando o provedor configurado não avalia (deterministic, anthropic) — a
	// avaliação por IA simplesmente não existe naquele ambiente, e o service diz isso claramente.
	assessor llm.Assessor
	usage    llmusage.Repository

	// assessFlight junta avaliações simultâneas do MESMO candidato/fase (duplo clique) numa chamada
	// só, em vez de pagar duas. Entre instâncias quem garante não haver duplicata é o índice único.
	assessFlight singleflight.Group
	// spawn roda a avaliação automática; go func em produção, síncrono nos testes.
	spawn func(fn func())
	// txRepo constrói o repositório sobre a transação; é NewRepository em produção.
	txRepo func(db database.DB) Repository
}

func NewService(repo Repository, withTx func(ctx context.Context, fn func(db database.DB) error) error, extractor llm.Extractor, assessor llm.Assessor, usage llmusage.Repository) Service {
	return &service{repo: repo, withTx: withTx, extractor: extractor, assessor: assessor, usage: usage,
		spawn: func(fn func()) { go fn() }, txRepo: NewRepository}
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

	// Aprovado (chegou à fase final) com consentimento entra no banco: o perfil de quem deu certo é a
	// referência de comparação para as próximas vagas. Sem consentimento dado, não entra — a
	// aprovação não é momento de pedir autorização a ninguém.
	var talentIDPtr *string
	if next.Key == PhaseSelecionados {
		id, err := addToBank(ctx, repo, companyID, cand, OriginApproval, true)
		if err != nil {
			return nil, apperror.Internal("falha ao registrar talento")
		}
		talentIDPtr = id
	}

	message := fmt.Sprintf("%s avançou para %s.", cand.Name, phaseLabel(next.Key))
	if talentIDPtr != nil {
		message = fmt.Sprintf("%s avançou para %s — perfil adicionado ao Banco de Talentos.", cand.Name, phaseLabel(next.Key))
	}
	if err := feed.Create(ctx, &activity.Entry{
		CompanyID: companyID, CampaignID: &cand.CampaignID,
		Actor: activity.ActorRecruiter, ActorUserID: &decidedByUserID,
		Message: message,
	}); err != nil {
		return nil, apperror.Internal("falha ao registrar atividade")
	}

	return &DecideResult{Phase: next.Key, TalentID: talentIDPtr}, nil
}

// Origens de entrada no banco (enum talent_origin).
const (
	OriginRejection = "reprovacao_qualificada"
	OriginApproval  = "aprovacao"
	OriginManual    = "cadastro_manual"
)

// addToBank põe no Banco de Talentos a pessoa por trás de um candidato e devolve o id do talento, ou
// nil se ela não entrou.
//
// Sempre reaproveita o registro que a pessoa já tem — o da própria candidatura ou, na falta dele, o de
// mesmo e-mail na empresa — em vez de criar um segundo (era assim que a mesma pessoa aparecia duas vezes
// no banco). Só cria registro novo quando não há nenhum, e então com o perfil completo do candidato.
//
// requireConsent=true (aprovação): só entra quem já consentiu; nada é criado. false (reprovação com
// envio confirmado): entra, e quem ainda não tinha consentido fica "notificado", porque é o convite que
// está sendo enviado. Quem pediu exclusão nunca volta ao banco.
func addToBank(ctx context.Context, repo Repository, companyID string, cand *Candidate, origin string, requireConsent bool) (*string, error) {
	talentID := ""
	if cand.TalentID != nil {
		talentID = *cand.TalentID
	} else {
		existing, err := repo.FindTalentIDByEmail(ctx, companyID, cand.Email)
		if err != nil {
			return nil, err
		}
		talentID = existing
	}
	if talentID == "" {
		if requireConsent {
			return nil, nil
		}
		created, err := repo.CreateTalentFromCandidate(ctx, companyID, cand.ID, origin)
		if err != nil {
			return nil, err
		}
		talentID = created
	}
	inBank, err := repo.PromoteTalentToBank(ctx, companyID, talentID, origin, requireConsent)
	if err != nil || !inBank {
		return nil, err
	}
	if cand.TalentID == nil {
		if err := repo.LinkCandidateTalent(ctx, companyID, cand.ID, talentID); err != nil {
			return nil, err
		}
	}
	return &talentID, nil
}

func reject(ctx context.Context, repo Repository, feed activity.Repository, companyID string, cand *Candidate, req DecideRequest, assessmentID *string, decidedByUserID string) (*DecideResult, error) {
	reasonKey := *req.RejectionReasonKey

	// Só entra no banco quem o motivo qualifica E o RH confirmou o envio (checkbox "Enviar ao Banco
	// de Talentos"). Desmarcado = não entra — o banco tem só quem o RH escolheu (ver addToBank).
	var talentIDPtr *string
	if goesToBank[reasonKey] && req.SendBankInvite {
		talentID, err := addToBank(ctx, repo, companyID, cand, OriginRejection, false)
		if err != nil {
			return nil, apperror.Internal("falha ao registrar talento")
		}
		talentIDPtr = talentID
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
		return apperror.BadRequestField("consent", consentRequiredMessage)
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

	// Dedup ANTES da extração. O formulário sempre exige e-mail nos dois modos (ver
	// SubmitApplicationRequest), então o reenvio mais comum — a mesma pessoa mandando o mesmo
	// formulário de novo — é barrado sem pagar inferência nenhuma. Até esta checagem existir, cada
	// reenvio pagava a extração inteira só pra receber o mesmo 400 no final.
	dup, err := s.repo.FindExistingApplication(ctx, campaignID, in.Email)
	if err != nil {
		return apperror.Internal("falha ao verificar candidatura")
	}
	if dup {
		return apperror.BadRequestField("email", duplicateApplicationMessage)
	}

	profile, err := s.resolveApplicationProfile(ctx, ref.CompanyID, campaignID, in)
	if err != nil {
		return mapExtractionError(err)
	}

	// Nome e e-mail são SEMPRE os do formulário (ver applyFormOverrides), então o e-mail que chega
	// aqui é o mesmo que a checagem de duplicidade acima já conferiu — não há segunda checagem.

	var candidateID string
	err = s.withTx(ctx, func(db database.DB) error {
		txRepo := NewRepository(db)

		talentID, err := txRepo.UpsertTalentFromPublicApplication(ctx, &TalentSeed{
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

		createdID, err := txRepo.CreateCandidate(ctx, &CandidateSeed{
			CompanyID: ref.CompanyID, CampaignID: campaignID, TalentID: talentID,
			Name: profile.Name, Email: profile.Email, Phone: profile.Phone, LinkedInURL: profile.LinkedInURL,
			City: profile.City, State: profile.State, YearsExperience: profile.YearsExperience,
			Summary: profile.Summary, EducationDegree: profile.EducationDegree,
			EducationInstitution: profile.EducationInstitution, EducationPeriod: profile.EducationPeriod,
		})
		if err != nil {
			// A corrida entre dois envios simultâneos que passaram pela checagem de duplicidade é
			// fechada pelo índice único (migrations/0010): quem perde recebe a mesma resposta de
			// quem foi barrado pela checagem, não um erro interno.
			if isUniqueViolation(err) {
				return apperror.BadRequestField("email", duplicateApplicationMessage)
			}
			return apperror.Internal("falha ao registrar candidatura")
		}
		candidateID = createdID

		if len(profile.Experience) > 0 {
			entries := toCandidateExperience(profile.Experience)
			if err := txRepo.InsertCandidateExperience(ctx, candidateID, entries); err != nil {
				return apperror.Internal("falha ao registrar experiência")
			}
			if err := txRepo.ReplaceTalentExperience(ctx, talentID, entries); err != nil {
				return apperror.Internal("falha ao registrar experiência")
			}
		}

		// Skills: lista do perfil (manual ou extraída por IA) + varredura do texto pelo dicionário da
		// taxonomia — ver resolveProfileSkills.
		resolved, err := resolveProfileSkills(ctx, txRepo, ref.CompanyID, profile)
		if err != nil {
			return apperror.Internal("falha ao resolver skills")
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
	if err != nil {
		return err
	}

	s.autoAssess(ref.CompanyID, candidateID)
	return nil
}

// autoAssess dispara a avaliação de IA de uma candidatura recém-registrada, FORA do request: o
// candidato não espera pela triagem interna. Melhor esforço — falhar (provedor fora, teto de gasto,
// limite de taxa) nunca desfaz a candidatura; o candidato só fica em "aguardando análise da IA" e o
// recrutador pode pedir a análise depois (POST /candidates/:id/assessment).
func (s *service) autoAssess(companyID, candidateID string) {
	if s.assessor == nil || candidateID == "" {
		return
	}
	s.spawn(func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("avaliação automática: pânico ao avaliar candidato %s: %v", candidateID, r)
			}
		}()
		// Contexto próprio: o do request já terá terminado (e o do Fiber é reciclado após o handler).
		ctx, cancel := context.WithTimeout(context.Background(), autoAssessTimeout)
		defer cancel()
		if _, err := s.Assess(ctx, companyID, candidateID); err != nil {
			log.Printf("avaliação automática do candidato %s não concluída: %v", candidateID, err)
		}
	})
}

// isUniqueViolation reconhece a violação de unicidade do Postgres (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// resolveApplicationProfile mapeia o modo manual direto (sem IA nenhuma — é o caminho mais
// confiável, ver ManualApplicationFields) ou chama o Extractor pro modo currículo (texto colado ou
// PDF). Exatamente um dos dois roda por chamada.
func (s *service) resolveApplicationProfile(ctx context.Context, companyID, campaignID string, in PublicApplicationInput) (*llm.ExtractedProfile, error) {
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

	profile, err := s.extractResume(ctx, companyID, campaignID, llm.Input{Text: in.ResumeText, PDFBytes: in.PDFBytes})
	if err != nil {
		return nil, err
	}
	return s.applyFormOverrides(profile, in), nil
}

// extractResume lê um currículo (texto ou PDF) e devolve o perfil estruturado, com tudo o que protege
// custo: cache por conteúdo, teto de gasto mensal e registro de uso. É o único caminho de extração do
// sistema — candidatura pública e cadastro manual de talento passam por aqui. campaignID vazio = fora de
// campanha (cadastro manual).
func (s *service) extractResume(ctx context.Context, companyID, campaignID string, raw llm.Input) (*llm.ExtractedProfile, error) {
	if s.extractor == nil {
		return nil, llm.ErrProviderUnavailable
	}
	// PreprocessInput converte PDF em texto localmente antes de qualquer provedor pago ver o
	// arquivo — página de PDF é cobrada como imagem, o que custa múltiplos do mesmo conteúdo em
	// texto. Só PDF escaneado (sem camada de texto) segue como bytes, e aí o modelo é o OCR.
	input := llm.PreprocessInput(raw)

	// O mesmo documento nunca é extraído duas vezes: o que determina o resultado é o conteúdo, não
	// quem enviou nem para qual vaga. É isto que faz o custo crescer com o número de currículos
	// distintos, e não com o número de envios (ver migrations/0007).
	fingerprint := llm.Fingerprint(input)
	profile, err := s.repo.FindCachedExtraction(ctx, companyID, fingerprint)
	if err != nil {
		return nil, apperror.Internal("falha ao consultar extração")
	}

	if profile != nil {
		// Chamada que não aconteceu também é informação: é assim que dá pra medir quanto o cache
		// está economizando, em vez de só supor.
		s.recordUsage(ctx, companyID, campaignID, fingerprint, llm.Free(cacheProvider), llmusage.StatusCacheHit, "")
		// Entradas gravadas antes da sanitização existir podem ter dado fora do formato.
		llm.SanitizeProfile(profile)
		return profile, nil
	}

	// Teto de gasto: checado DEPOIS do cache de propósito. Uma extração já paga anteriormente não
	// gera chamada nova, então continuar servindo do cache mesmo com o orçamento estourado é
	// gratuito — bloquear ali puniria a empresa sem economizar um centavo.
	if err := s.checkBudget(ctx, companyID); err != nil {
		s.recordUsage(ctx, companyID, campaignID, fingerprint, llm.Free(budgetProvider), llmusage.StatusFailed, err.Error())
		return nil, err
	}

	profile, usage, err := s.extractor.Extract(ctx, input)
	if err != nil {
		// Registrado ANTES de propagar o erro. Se a resposta chegou e era inútil (truncada,
		// recusada), os tokens já foram cobrados — é exatamente o gasto que mais fácil some da
		// contabilidade, porque termina em erro para o usuário.
		status := llmusage.StatusFailed
		if usage.InputTokens > 0 || usage.OutputTokens > 0 {
			status = llmusage.StatusBilledError
		}
		s.recordUsage(ctx, companyID, campaignID, fingerprint, usage, status, err.Error())
		return nil, err
	}
	s.recordUsage(ctx, companyID, campaignID, fingerprint, usage, llmusage.StatusSuccess, "")

	// A saída do modelo é entrada não confiável: valida formato e tamanho ANTES de qualquer uso e
	// antes de ir para o cache — uma data malformada, por exemplo, faria o INSERT da candidatura
	// falhar depois de a extração já ter sido paga.
	llm.SanitizeProfile(profile)

	// Melhor esforço: já pagamos pela extração e o perfil está em mãos — falhar a candidatura
	// porque o cache não gravou seria perder o dado E o dinheiro.
	_ = s.repo.SaveExtraction(ctx, companyID, fingerprint, profile)

	return profile, nil
}

// applyFormOverrides: nome e e-mail são sempre os do formulário, nos dois modos — nunca os que o
// extrator achou. O dado que a própria pessoa digitou é mais confiável, e principalmente é a
// IDENTIDADE da candidatura: um currículo que contenha o e-mail de outra pessoa não pode registrar a
// candidatura em nome dela nem, via checagem de duplicidade, bloquear a candidatura verdadeira dela
// a esta vaga. O e-mail que o currículo traz é descartado. Extraído para função porque o perfil pode
// vir do extrator OU do cache, e esquecer de aplicar num dos caminhos devolveria a identidade errada.
func (s *service) applyFormOverrides(profile *llm.ExtractedProfile, in PublicApplicationInput) *llm.ExtractedProfile {
	profile.Name = in.Name
	profile.Email = in.Email
	return profile
}

// checkBudget é o único ponto que decide se uma chamada paga pode acontecer.
//
// Falha ao CONSULTAR o orçamento libera a chamada (fail-open), decisão deliberada: o custo de deixar
// passar algumas extrações durante uma instabilidade de banco é de centavos, enquanto recusar toda
// candidatura da plataforma pelo mesmo motivo é perder candidato real. O teto protege contra abuso
// sustentado, não contra o minuto em que o Postgres piscou.
func (s *service) checkBudget(ctx context.Context, companyID string) error {
	if s.usage == nil {
		return nil
	}
	spent, limit, err := s.usage.BudgetStatus(ctx, companyID)
	if err != nil {
		return nil
	}
	if spent >= limit {
		return llm.ErrBudgetExceeded
	}
	return nil
}

// recordUsage nunca derruba a candidatura: perder a linha de contabilidade é ruim, mas recusar uma
// pessoa porque a tabela de uso falhou seria pior. O erro sobe como log, não como resposta.
func (s *service) recordUsage(ctx context.Context, companyID, campaignID, fingerprint string, usage llm.Usage, status, errCode string) {
	if s.usage == nil {
		return
	}
	var campaign *string
	if campaignID != "" {
		campaign = &campaignID
	}
	_ = s.usage.Record(ctx, &llmusage.Record{
		CompanyID:   companyID,
		CampaignID:  campaign,
		Operation:   llmusage.OperationExtraction,
		Usage:       usage,
		Status:      status,
		ErrorCode:   errCode,
		Fingerprint: fingerprint,
	})
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
		errors.Is(err, llm.ErrRateLimited) || errors.Is(err, llm.ErrProviderUnavailable) ||
		errors.Is(err, llm.ErrBudgetExceeded) || errors.Is(err, llm.ErrUnsupportedInput) {
		return apperror.BadRequest("não foi possível processar seu currículo automaticamente. Tente o formulário manual.")
	}
	return apperror.Internal("falha ao processar candidatura")
}
