package campaign

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
)

// CampaignView é uma Campaign com os campos derivados que o frontend espera — sempre computados,
// nunca colunas (ver PhaseCount no model.go). Get/List/Create devolvem isto, não Campaign puro.
type CampaignView struct {
	Campaign
	Phases          []PhaseCount // ordenado por posição
	TotalCandidates int
	CurrentPhaseKey string
	FunnelPercent   int
	HiredCount      int // só relevante/populado quando Status == StatusClosed
}

// PerformanceRow é uma linha de GET /reports/campaign-performance.
type PerformanceRow struct {
	Campaign
	TotalCandidates int
	SelectedCount   int
	CurrentPhaseKey string
}

type Service interface {
	Get(ctx context.Context, companyID, id string) (*CampaignView, error)
	List(ctx context.Context, companyID string) ([]CampaignView, error)
	Create(ctx context.Context, companyID, createdByUserID string, req CreateCampaignRequest) (*CampaignView, error)
	// Update altera os dados editáveis da campanha (título, descrição, cidade/estado, modalidade,
	// tipo de contrato, senioridade) — nunca status nem fases, que têm seus próprios métodos.
	Update(ctx context.Context, companyID, id string, req UpdateCampaignRequest) (*CampaignView, error)
	// UpdatePhases substitui os módulos OPCIONAIS do funil (fit/tecnica/entrevista) — recusa
	// remover uma fase que ainda tem candidato nela (ver CandidateCountsByPhase).
	UpdatePhases(ctx context.Context, companyID, id string, phaseKeys []string) (*CampaignView, error)
	TogglePause(ctx context.Context, companyID, id string) (*CampaignView, error)
	FunnelSummary(ctx context.Context, companyID string) ([]PhaseCount, error)
	CampaignPerformance(ctx context.Context, companyID string) ([]PerformanceRow, error)
	// SetPublicApplicationsEnabled liga/desliga o link de candidatura pública desta campanha.
	SetPublicApplicationsEnabled(ctx context.Context, companyID, id string, enabled bool) (*CampaignView, error)
	// GetPublicInfo é a única leitura sem tenant deste domínio — chamada de uma rota pública, sem
	// JWT nenhum. 404 idêntico pra "não existe", "pausada" e "link desligado": nunca diferenciar
	// pra um chamador anônimo.
	GetPublicInfo(ctx context.Context, id string) (*PublicInfo, error)
}

type service struct {
	repo   Repository
	withTx func(ctx context.Context, fn func(db database.DB) error) error
}

func NewService(repo Repository, withTx func(ctx context.Context, fn func(db database.DB) error) error) Service {
	return &service{repo: repo, withTx: withTx}
}

func (s *service) Get(ctx context.Context, companyID, id string) (*CampaignView, error) {
	c, err := s.repo.FindByID(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar campanha")
	}
	if c == nil {
		return nil, apperror.NotFound("campanha não encontrada")
	}

	phases, err := s.repo.PhaseCountsByCampaign(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular funil da campanha")
	}

	view := assembleView(*c, phases)
	if err := s.populateHiredCount(ctx, companyID, &view); err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *service) List(ctx context.Context, companyID string) ([]CampaignView, error) {
	campaigns, err := s.repo.ListByCompany(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao listar campanhas")
	}

	allPhases, err := s.repo.PhaseCountsByCompany(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular funil das campanhas")
	}
	phasesByCampaign := groupByCampaign(allPhases)

	views := make([]CampaignView, len(campaigns))
	for i, c := range campaigns {
		views[i] = assembleView(c, phasesByCampaign[c.ID])
		if err := s.populateHiredCount(ctx, companyID, &views[i]); err != nil {
			return nil, err
		}
	}
	return views, nil
}

func (s *service) Create(ctx context.Context, companyID, createdByUserID string, req CreateCampaignRequest) (*CampaignView, error) {
	c := &Campaign{
		CompanyID:        companyID,
		CreatedByUserID:  createdByUserID,
		Title:            req.Title,
		Description:      req.Description,
		Responsibilities: req.Responsibilities,
		Requirements:     req.Requirements,
		Benefits:         req.Benefits,
		City:             req.City,
		State:            req.State,
		Modality:         req.Modality,
		ContractType:     req.ContractType,
		Seniority:        req.Seniority,
	}
	phases := buildPhases(req.PhaseKeys)

	// Campanha + fases numa transação única: sem isso, uma falha entre os dois inserts deixaria
	// uma campanha sem nenhuma fase configurada — quebrando a FK que todo candidato dessa
	// campanha vai depender (campaigns.id, phase_key -> campaign_phases).
	err := s.withTx(ctx, func(db database.DB) error {
		return NewRepository(db).Create(ctx, c, phases)
	})
	if err != nil {
		return nil, apperror.Internal("falha ao criar campanha")
	}

	// Campanha recém-criada não pode ter candidato nenhum ainda — monta a resposta direto das
	// fases que acabamos de inserir (contagem 0) em vez de fazer outra ida ao banco.
	counts := make([]PhaseCount, len(phases))
	for i, p := range phases {
		counts[i] = PhaseCount{CampaignID: c.ID, Key: p.Key, Position: p.Position, Count: 0}
	}
	view := assembleView(*c, counts)
	return &view, nil
}

func (s *service) Update(ctx context.Context, companyID, id string, req UpdateCampaignRequest) (*CampaignView, error) {
	c, err := s.repo.FindByID(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar campanha")
	}
	if c == nil {
		return nil, apperror.NotFound("campanha não encontrada")
	}

	in := UpdateDetailsInput{
		Title:            req.Title,
		Description:      req.Description,
		Responsibilities: req.Responsibilities,
		Requirements:     req.Requirements,
		Benefits:         req.Benefits,
		City:             req.City,
		State:            req.State,
		Modality:         req.Modality,
		ContractType:     req.ContractType,
		Seniority:        req.Seniority,
	}
	if err := s.repo.UpdateDetails(ctx, companyID, id, in); err != nil {
		return nil, apperror.Internal("falha ao atualizar campanha")
	}

	c.Title, c.Description, c.City, c.State = req.Title, req.Description, req.City, req.State
	c.Responsibilities, c.Requirements, c.Benefits = req.Responsibilities, req.Requirements, req.Benefits
	c.Modality, c.ContractType, c.Seniority = req.Modality, req.ContractType, req.Seniority

	phases, err := s.repo.PhaseCountsByCampaign(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular funil da campanha")
	}
	view := assembleView(*c, phases)
	if err := s.populateHiredCount(ctx, companyID, &view); err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *service) UpdatePhases(ctx context.Context, companyID, id string, phaseKeys []string) (*CampaignView, error) {
	c, err := s.repo.FindByID(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar campanha")
	}
	if c == nil {
		return nil, apperror.NotFound("campanha não encontrada")
	}

	currentPhases, err := s.repo.PhaseCountsByCampaign(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular funil da campanha")
	}
	occupied, err := s.repo.CandidateCountsByPhase(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao verificar candidatos das fases")
	}

	newPhases := buildPhases(phaseKeys)
	newKeys := make(map[string]bool, len(newPhases))
	for _, p := range newPhases {
		newKeys[p.Key] = true
	}
	for _, pc := range currentPhases {
		if !newKeys[pc.Key] && occupied[pc.Key] > 0 {
			return nil, apperror.BadRequest(fmt.Sprintf(
				"não é possível remover a fase '%s': ainda há candidato nela", phaseLabels[pc.Key],
			))
		}
	}

	err = s.withTx(ctx, func(db database.DB) error {
		return NewRepository(db).ReplacePhases(ctx, id, newPhases)
	})
	if err != nil {
		return nil, apperror.Internal("falha ao atualizar fases da campanha")
	}

	phases, err := s.repo.PhaseCountsByCampaign(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular funil da campanha")
	}
	view := assembleView(*c, phases)
	if err := s.populateHiredCount(ctx, companyID, &view); err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *service) TogglePause(ctx context.Context, companyID, id string) (*CampaignView, error) {
	c, err := s.repo.FindByID(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar campanha")
	}
	if c == nil {
		return nil, apperror.NotFound("campanha não encontrada")
	}

	// Campanha encerrada: no-op tolerado, não erro — mesmo comportamento do frontend (mock e
	// contrato documentado em data-api.ts: "não afeta campanhas já encerradas").
	if c.Status == StatusClosed {
		phases, err := s.repo.PhaseCountsByCampaign(ctx, companyID, id)
		if err != nil {
			return nil, apperror.Internal("falha ao calcular funil da campanha")
		}
		view := assembleView(*c, phases)
		if err := s.populateHiredCount(ctx, companyID, &view); err != nil {
			return nil, err
		}
		return &view, nil
	}

	var pausedAt *time.Time
	newStatus := StatusPaused
	if c.Status == StatusPaused {
		newStatus = StatusActive
		pausedAt = nil
	} else {
		now := time.Now()
		pausedAt = &now
	}

	if err := s.repo.SetStatus(ctx, companyID, id, newStatus, pausedAt); err != nil {
		return nil, apperror.Internal("falha ao atualizar campanha")
	}

	c.Status = newStatus
	c.PausedAt = pausedAt
	phases, err := s.repo.PhaseCountsByCampaign(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular funil da campanha")
	}
	view := assembleView(*c, phases)
	return &view, nil
}

func (s *service) SetPublicApplicationsEnabled(ctx context.Context, companyID, id string, enabled bool) (*CampaignView, error) {
	c, err := s.repo.FindByID(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar campanha")
	}
	if c == nil {
		return nil, apperror.NotFound("campanha não encontrada")
	}

	// Sem descrição, o link público abriria numa vaga que não diz nada ao candidato — a aba "Vaga"
	// ficaria vazia. Só barra ao LIGAR: desligar é sempre permitido, e campanha que já está com o
	// link ativo de antes desta regra continua funcionando.
	if enabled && strings.TrimSpace(c.Description) == "" {
		return nil, apperror.BadRequest("preencha a descrição da vaga antes de gerar o link de candidatura")
	}

	if err := s.repo.SetPublicApplicationsEnabled(ctx, companyID, id, enabled); err != nil {
		return nil, apperror.Internal("falha ao atualizar link de candidatura")
	}
	c.AcceptsPublicApplications = enabled

	phases, err := s.repo.PhaseCountsByCampaign(ctx, companyID, id)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular funil da campanha")
	}
	view := assembleView(*c, phases)
	if err := s.populateHiredCount(ctx, companyID, &view); err != nil {
		return nil, err
	}
	return &view, nil
}

func (s *service) GetPublicInfo(ctx context.Context, id string) (*PublicInfo, error) {
	info, err := s.repo.FindPublicByID(ctx, id)
	if err != nil {
		return nil, apperror.Internal("falha ao buscar vaga")
	}
	if info == nil {
		return nil, apperror.NotFound("vaga não encontrada")
	}
	return info, nil
}

func (s *service) FunnelSummary(ctx context.Context, companyID string) ([]PhaseCount, error) {
	counts, err := s.repo.PhaseCountsByCompany(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular funil agregado")
	}

	sums := make(map[string]int)
	for _, pc := range counts {
		sums[pc.Key] += pc.Count
	}

	// Sempre as 5 chaves fixas, mesmo quando nenhuma campanha usa uma delas — mesmo
	// comportamento do getFunnelSummary do mock, que nunca omite uma fase.
	keys := []string{PhaseRecebidos, PhaseFit, PhaseTecnica, PhaseEntrevista, PhaseSelecionados}
	summary := make([]PhaseCount, len(keys))
	for i, key := range keys {
		summary[i] = PhaseCount{Key: key, Position: i + 1, Count: sums[key]}
	}
	return summary, nil
}

func (s *service) CampaignPerformance(ctx context.Context, companyID string) ([]PerformanceRow, error) {
	campaigns, err := s.repo.ListByCompany(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao listar campanhas")
	}

	allPhases, err := s.repo.PhaseCountsByCompany(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal("falha ao calcular funil das campanhas")
	}
	phasesByCampaign := groupByCampaign(allPhases)

	rows := make([]PerformanceRow, len(campaigns))
	for i, c := range campaigns {
		view := assembleView(c, phasesByCampaign[c.ID])
		rows[i] = PerformanceRow{
			Campaign:        c,
			TotalCandidates: view.TotalCandidates,
			SelectedCount:   countForKey(view.Phases, PhaseSelecionados),
			CurrentPhaseKey: view.CurrentPhaseKey,
		}
	}
	return rows, nil
}

// populateHiredCount só consulta o banco quando a campanha está encerrada — é o único status em
// que a métrica aparece (texto de `meta`), e nada ainda produz esse estado nesta versão.
func (s *service) populateHiredCount(ctx context.Context, companyID string, view *CampaignView) error {
	if view.Status != StatusClosed {
		return nil
	}
	count, err := s.repo.HiredCount(ctx, companyID, view.ID)
	if err != nil {
		return apperror.Internal("falha ao calcular contratações da campanha")
	}
	view.HiredCount = count
	return nil
}

// assembleView monta os campos derivados (totalCandidates, fase atual, % do funil) a partir da
// contagem cumulativa por fase — nunca lidos de coluna nenhuma.
func assembleView(c Campaign, phases []PhaseCount) CampaignView {
	view := CampaignView{Campaign: c, Phases: phases, CurrentPhaseKey: PhaseRecebidos}
	if len(phases) == 0 {
		return view
	}

	view.TotalCandidates = phases[0].Count // recebidos é sempre posição 1
	currentPosition := phases[0].Position
	for _, pc := range phases {
		if pc.Count > 0 {
			view.CurrentPhaseKey = pc.Key
			currentPosition = pc.Position
		}
	}
	view.FunnelPercent = int(round(float64(currentPosition) / float64(len(phases)) * 100))
	return view
}

// buildPhases monta a lista completa de fases a partir dos módulos opcionais escolhidos:
// Recebidos sempre na posição 1, os módulos escolhidos na ordem dada em 2..N, Selecionados
// sempre por último. Validação de "são só fit/tecnica/entrevista, sem duplicata" já aconteceu
// via tag do validator no handler — aqui é só montagem.
func buildPhases(optionalKeys []string) []Phase {
	phases := make([]Phase, 0, len(optionalKeys)+2)
	phases = append(phases, Phase{Key: PhaseRecebidos, Position: 1})
	for i, key := range optionalKeys {
		phases = append(phases, Phase{Key: key, Position: i + 2})
	}
	phases = append(phases, Phase{Key: PhaseSelecionados, Position: len(optionalKeys) + 2})
	return phases
}

func groupByCampaign(counts []PhaseCount) map[string][]PhaseCount {
	grouped := make(map[string][]PhaseCount)
	for _, pc := range counts {
		grouped[pc.CampaignID] = append(grouped[pc.CampaignID], pc)
	}
	return grouped
}

func countForKey(phases []PhaseCount, key string) int {
	for _, pc := range phases {
		if pc.Key == key {
			return pc.Count
		}
	}
	return 0
}

func round(v float64) float64 {
	if v < 0 {
		return -round(-v)
	}
	return float64(int(v + 0.5))
}
