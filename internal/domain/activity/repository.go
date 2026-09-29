package activity

import (
	"context"

	"github.com/ClearHire-Group/clearhire-server/internal/database"
)

type Repository interface {
	// ListByCompany devolve o feed mais recente primeiro, limitado a limit linhas.
	ListByCompany(ctx context.Context, companyID string, limit int) ([]Item, error)
	// Create registra uma ação no feed. Recebe o database.DB do chamador de propósito: quando o
	// repository é construído com a tx de quem está decidindo, a linha do feed entra na MESMA
	// transação da decisão — ou as duas acontecem, ou nenhuma (ver candidate/service.go Decide).
	Create(ctx context.Context, e *Entry) error
}

type postgresRepository struct {
	db database.DB
}

func NewRepository(db database.DB) Repository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) ListByCompany(ctx context.Context, companyID string, limit int) ([]Item, error) {
	// O join carrega `deleted_at is null` de propósito: campanha arquivada faz c.id e c.title
	// virarem NULL juntos, então a mensagem continua no histórico mas sem link — melhor que um
	// routerLink que abre num 404. Ordena por (created_at, id) porque o default now() dá o mesmo
	// timestamp pra duas linhas escritas na mesma transação; sem o desempate a ordem entre elas
	// fica indefinida e o feed embaralha a cada request.
	rows, err := r.db.Query(ctx, `
		select a.id, a.actor, a.message, c.id, c.title, a.created_at
		from activity_feed a
		left join campaigns c
		  on c.id = a.campaign_id
		 and c.company_id = a.company_id
		 and c.deleted_at is null
		where a.company_id = $1
		order by a.created_at desc, a.id desc
		limit $2
	`, companyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Item, 0)
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Actor, &it.Message, &it.CampaignID, &it.CampaignTitle, &it.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (r *postgresRepository) Create(ctx context.Context, e *Entry) error {
	_, err := r.db.Exec(ctx, `
		insert into activity_feed (company_id, campaign_id, actor, actor_user_id, message)
		values ($1, $2, $3, $4, $5)
	`, e.CompanyID, e.CampaignID, e.Actor, e.ActorUserID, e.Message)
	return err
}
