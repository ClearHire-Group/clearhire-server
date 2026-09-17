package activity

import "time"

const (
	ActorAI        = "ia"
	ActorRecruiter = "recrutador"
)

// Item é uma linha do feed já resolvida pra leitura — CampaignTitle vem de join com campaigns,
// não está em activity_feed.
//
// CampaignID/CampaignTitle são ponteiros porque activity_feed.campaign_id é nulável de propósito:
// existe atividade de nível empresa (convidar um RH, editar o perfil cultural) que não pertence a
// campanha nenhuma. O front trata esse caso não renderizando o link (ver activity.component.html).
type Item struct {
	ID            string
	Actor         string
	Message       string
	CampaignID    *string
	CampaignTitle *string
	CreatedAt     time.Time
}

// Entry é o que um domínio registra ao executar uma ação. Sem CreatedAt: quem decide o instante é
// o banco (default now()), pra a ordem do feed nunca depender do relógio de quem escreveu.
//
// ActorUserID é nil quando Actor = ActorAI (não há usuário por trás) e obrigatório quando a ação
// partiu de uma pessoa — é o que permite um dia mostrar "quem fez" agora que a empresa tem mais de
// um RH (ver migrations/0001_init.sql, seção 7).
type Entry struct {
	CompanyID   string
	CampaignID  *string
	Actor       string
	ActorUserID *string
	Message     string
}
