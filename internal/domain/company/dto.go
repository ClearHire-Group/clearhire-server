package company

// RegisterCompanyRequest é o payload de POST /companies — o cadastro inicial
// da empresa junto com o primeiro RH (owner).
type RegisterCompanyRequest struct {
	CompanyName   string `json:"companyName" validate:"required,max=200"`
	OwnerName     string `json:"ownerName" validate:"required,max=200"`
	OwnerEmail    string `json:"ownerEmail" validate:"required,email,max=254"`
	OwnerPassword string `json:"ownerPassword" validate:"required,min=8,max=72"`
}

// UpdateCultureProfileRequest é o payload de POST /company-profile — tela de
// Configurações (perfil cultural da empresa). JSON tag de ImportanceNote é
// "importance" de propósito: bate com CompanyProfile.importance do frontend
// (nome do campo Go segue a coluna SQL culture_importance_note, só o contrato
// HTTP precisa bater com o outro lado).
type UpdateCultureProfileRequest struct {
	Tone           string   `json:"tone" validate:"max=2000"`
	ImportanceNote string   `json:"importance" validate:"max=2000"`
	Values         []string `json:"values" validate:"max=10,dive,min=1,max=60"`
}

// Response é o que POST /companies devolve — nunca o model direto (ele não
// tem tags json e não deveria: model é forma interna, não contrato de API).
type Response struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func toResponse(c *Company) *Response {
	return &Response{ID: c.ID, Name: c.Name}
}

// ProfileResponse é o que GET/POST /company-profile devolvem — espelha
// CompanyProfile do frontend (clearhire-app/src/app/core/models.ts) campo a campo.
type ProfileResponse struct {
	Name       string   `json:"name"`
	Values     []string `json:"values"`
	Tone       string   `json:"tone"`
	Importance string   `json:"importance"`
}

func toProfileResponse(p *Profile) *ProfileResponse {
	values := p.Values
	if values == nil {
		values = []string{}
	}
	return &ProfileResponse{
		Name:       p.Name,
		Values:     values,
		Tone:       p.Tone,
		Importance: p.ImportanceNote,
	}
}
