package company

import "time"

// Company é a empresa cliente — o tenant raiz de toda a plataforma
// (ver db/schema.sql, tabela companies).
type Company struct {
	ID                    string
	Name                  string
	CultureTone           string
	CultureImportanceNote string
	SeatLimit             int
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// Profile é a visão de "perfil cultural" que a tela Configurações consome —
// Company (a linha de companies) + a lista ordenada de company_culture_values,
// que é tabela filha, não coluna. Camada de serviço, nunca persistido como tal.
type Profile struct {
	ID             string
	Name           string
	Tone           string
	ImportanceNote string
	Values         []string
}
