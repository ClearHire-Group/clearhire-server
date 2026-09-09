package company

// RegisterCompanyRequest é o payload de POST /companies — o cadastro inicial
// da empresa junto com o primeiro RH (owner).
type RegisterCompanyRequest struct {
	CompanyName  string `json:"companyName" validate:"required"`
	OwnerName    string `json:"ownerName" validate:"required"`
	OwnerEmail   string `json:"ownerEmail" validate:"required,email"`
	OwnerPassword string `json:"ownerPassword" validate:"required,min=8"`
}

// UpdateCultureProfileRequest é o payload de PATCH /companies/:id/culture —
// tela de Configurações (perfil cultural da empresa).
type UpdateCultureProfileRequest struct {
	Tone           string   `json:"tone"`
	ImportanceNote string   `json:"importanceNote"`
	Values         []string `json:"values"`
}
