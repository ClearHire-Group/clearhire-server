// Package llmusage registra o que cada chamada a um provedor de LLM consumiu e custou.
//
// É deliberadamente só escrita por enquanto: o valor imediato é existir a trilha desde a primeira
// chamada paga. Ler isso em painel é fase posterior — mas dado não coletado não se recupera
// retroativamente, e por isso a coleta vem primeiro.
package llmusage

import (
	"context"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
)

const (
	StatusSuccess     = "success"
	StatusBilledError = "billed_error"
	StatusFailed      = "failed"
	StatusCacheHit    = "cache_hit"

	OperationExtraction = "extraction"
	OperationAssessment = "assessment"
	// OperationTalentMatch é a leitura de IA sob demanda do match reverso (etapa 2 do documento
	// documentos/banco-de-talentos-recomendacao-plano.md) — deliberadamente separada de
	// OperationAssessment: são gastos de propósito diferente (avaliar candidato já no funil vs.
	// avaliar talento do banco pra vaga nova), e misturar os dois no relatório de custo por
	// operação esconderia de onde o gasto realmente vem.
	OperationTalentMatch = "talent_match"
)

// Record é uma chamada (ou uma economia, no caso de cache_hit). CampaignID e CandidateID são
// ponteiros porque o gasto acontece antes de o candidato existir — ver migrations/0008.
type Record struct {
	CompanyID   string
	CampaignID  *string
	CandidateID *string
	Operation   string
	Usage       llm.Usage
	Status      string
	ErrorCode   string
	Fingerprint string
}

type Repository interface {
	Record(ctx context.Context, r *Record) error
	// BudgetStatus devolve quanto a empresa já gastou no mês civil corrente e qual é o teto dela.
	// Uma consulta só porque as duas respostas são sempre usadas juntas, e separá-las abriria
	// espaço para decidir com um valor lido num instante e outro em instante diferente.
	BudgetStatus(ctx context.Context, companyID string) (spent, limit float64, err error)
}

type postgresRepository struct {
	db database.DB
}

func NewRepository(db database.DB) Repository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) BudgetStatus(ctx context.Context, companyID string) (float64, float64, error) {
	var spent, limit float64
	// date_trunc('month', now()) é o início do mês civil — mesma fronteira que o provedor usa para
	// fechar fatura. Linhas com estimated_cost_usd NULL (modelo fora da tabela de preços) somam
	// zero aqui; é por isso que o boot recusa subir com modelo pago sem preço conhecido, senão
	// haveria um caminho de gasto invisível para este teto.
	err := r.db.QueryRow(ctx, `
		select
			coalesce((
				select sum(estimated_cost_usd) from llm_usage
				where company_id = $1 and created_at >= date_trunc('month', now())
			), 0),
			(select llm_monthly_budget_usd from companies where id = $1)
	`, companyID).Scan(&spent, &limit)
	return spent, limit, err
}

func (r *postgresRepository) Record(ctx context.Context, rec *Record) error {
	// O custo é congelado agora, com a tabela de preços vigente — não recalculado na leitura, que
	// daria um número diferente assim que qualquer preço mudasse. nil vira NULL ("desconhecido").
	cost := llm.EstimatedCostUSD(rec.Usage)

	_, err := r.db.Exec(ctx, `
		insert into llm_usage (
			company_id, campaign_id, candidate_id, operation,
			provider, model, prompt_version,
			input_tokens, output_tokens, cached_input_tokens,
			estimated_cost_usd, status, error_code, duration_ms, fingerprint
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,nullif($13,''),$14,nullif($15,''))
	`,
		rec.CompanyID, rec.CampaignID, rec.CandidateID, rec.Operation,
		rec.Usage.Provider, rec.Usage.Model, rec.Usage.PromptVersion,
		rec.Usage.InputTokens, rec.Usage.OutputTokens, rec.Usage.CachedInputTokens,
		cost, rec.Status, rec.ErrorCode, rec.Usage.DurationMs, rec.Fingerprint,
	)
	return err
}
