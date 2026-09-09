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
