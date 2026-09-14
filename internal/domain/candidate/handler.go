package candidate

import (
	"errors"
	"io"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/ClearHire-Group/clearhire-server/internal/middleware"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/idparam"
	"github.com/ClearHire-Group/clearhire-server/pkg/response"
	"github.com/ClearHire-Group/clearhire-server/pkg/validator"
)

// maxResumeFileBytes é o teto explícito do handler de upload — bem abaixo do BodyLimit global
// (6MB, ver internal/server/server.go) e do teto de 32MB da API da Claude, mas confortável sobre
// um currículo real (poucas centenas de KB). Rejeitar aqui com mensagem clara é melhor que deixar
// cair num corte de conexão sem explicação.
const maxResumeFileBytes = 5 * 1024 * 1024

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(router fiber.Router) {
	group := router.Group("/candidates")
	group.Get("/:id", h.Get)
	group.Post("/:id/decisions", h.Decide)
}

// RegisterCampaignRoutes pluga GET /campaigns/:id/candidates — mora aqui (não em
// campaign.Handler) porque é candidate.Service quem sabe listar, mas o path é aninhado sob
// campaigns por ser "os candidatos DESTA campanha" (mesmo padrão de campaign.Handler ter
// RegisterRoutes e RegisterReportsRoutes separados pra grupos de rota diferentes).
func (h *Handler) RegisterCampaignRoutes(router fiber.Router) {
	router.Get("/campaigns/:id/candidates", h.ListByCampaign)
}

// RegisterPublicRoutes pluga a candidatura pública — sem tenant nenhum, o candidato nunca tem
// token de autenticação. companyID é sempre derivado da própria campanha dentro do service, nunca
// aceito de quem chama.
func (h *Handler) RegisterPublicRoutes(router fiber.Router) {
	group := router.Group("/public/campaigns/:id/applications")
	// Manual + texto colado: mesmo patamar de POST /companies.
	group.Post("/", middleware.RateLimit(5, time.Minute), h.SubmitApplication)
	// Upload de PDF: limite mais apertado por processar um arquivo (parsing tem custo de CPU
	// mesmo sendo determinístico, sem chamada de IA nenhuma — ver pkg/llm/deterministic); rota
	// própria já facilita isolar o limite sem afetar o modo texto/manual.
	group.Post("/resume-file", middleware.RateLimit(3, time.Minute), h.SubmitResumeFile)
}

func (h *Handler) Get(c *fiber.Ctx) error {
	id, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}
	d, err := h.service.Get(c.Context(), middleware.CompanyID(c), id)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toCandidateProfileResponse(d))
}

func (h *Handler) ListByCampaign(c *fiber.Ctx) error {
	campaignID, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}
	candidates, err := h.service.ListByCampaign(c.Context(), middleware.CompanyID(c), campaignID, c.Query("phase"))
	if err != nil {
		return h.respondError(c, err)
	}
	result := make([]*CandidateResponse, len(candidates))
	for i := range candidates {
		result[i] = toCandidateResponse(&candidates[i])
	}
	return response.OK(c, result)
}

func (h *Handler) Decide(c *fiber.Ctx) error {
	id, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}

	var req DecideRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "dados obrigatórios faltando ou inválidos")
	}

	result, err := h.service.Decide(c.Context(), middleware.CompanyID(c), id, req, middleware.UserID(c))
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toDecideResponse(result))
}

func (h *Handler) SubmitApplication(c *fiber.Ctx) error {
	campaignID, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}

	var req SubmitApplicationRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	if err := validator.Validate(req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "dados obrigatórios faltando ou inválidos")
	}

	input, err := req.toInput()
	if err != nil {
		return h.respondError(c, err)
	}

	if err := h.service.RegisterPublicApplication(c.Context(), campaignID, input); err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, nil)
}

func (h *Handler) SubmitResumeFile(c *fiber.Ctx) error {
	campaignID, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}

	name := c.FormValue("name")
	email := c.FormValue("email")
	consent := c.FormValue("consent") == "true"
	honeypot := c.FormValue("website")

	fileHeader, err := c.FormFile("file")
	if err != nil {
		return response.Err(c, fiber.StatusBadRequest, "arquivo de currículo é obrigatório")
	}
	if fileHeader.Size > maxResumeFileBytes {
		return response.Err(c, fiber.StatusBadRequest, "arquivo muito grande (máximo 5MB)")
	}

	file, err := fileHeader.Open()
	if err != nil {
		return response.Err(c, fiber.StatusBadRequest, "não foi possível ler o arquivo")
	}
	defer file.Close()

	// Lido inteiro pra memória e nunca gravado em disco — o extrator (ver pkg/llm) processa os
	// bytes direto e eles são descartados no fim desta requisição; só o texto/dado que ele extrai
	// é persistido.
	pdfBytes, err := io.ReadAll(io.LimitReader(file, maxResumeFileBytes+1))
	if err != nil {
		return response.Err(c, fiber.StatusBadRequest, "não foi possível ler o arquivo")
	}
	if int64(len(pdfBytes)) > maxResumeFileBytes {
		return response.Err(c, fiber.StatusBadRequest, "arquivo muito grande (máximo 5MB)")
	}

	input := PublicApplicationInput{
		Consent: consent, Honeypot: honeypot, Name: name, Email: email, PDFBytes: pdfBytes,
	}
	if err := h.service.RegisterPublicApplication(c.Context(), campaignID, input); err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, nil)
}

func (h *Handler) respondError(c *fiber.Ctx, err error) error {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		return response.Err(c, appErr.Code, appErr.Message)
	}
	return response.Err(c, fiber.StatusInternalServerError, "erro interno")
}
