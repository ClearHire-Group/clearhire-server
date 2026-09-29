package candidate

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/ClearHire-Group/clearhire-server/internal/middleware"
	"github.com/ClearHire-Group/clearhire-server/pkg/apperror"
	"github.com/ClearHire-Group/clearhire-server/pkg/idparam"
	"github.com/ClearHire-Group/clearhire-server/pkg/llm"
	"github.com/ClearHire-Group/clearhire-server/pkg/response"
	"github.com/ClearHire-Group/clearhire-server/pkg/validator"
)

// maxResumeFileBytes é o teto explícito do handler de upload — bem abaixo do BodyLimit global
// (6MB, ver internal/server/server.go) e do teto de 32MB da API da Claude, mas confortável sobre
// um currículo real (poucas centenas de KB). Rejeitar aqui com mensagem clara é melhor que deixar
// cair num corte de conexão sem explicação.
const maxResumeFileBytes = 5 * 1024 * 1024

// Tetos globais das rotas anônimas, somando todas as origens. Dimensionados como disjuntor: uma
// campanha real recebendo 60 currículos em PDF num único minuto já seria excepcional, então este
// limite não estorva uso legítimo — mas corta um bot distribuído em ordens de grandeza.
const (
	publicApplicationsPerMinute = 120
	resumeFilesPerMinute        = 60
	// Por campanha: uma vaga real recebendo 40 candidaturas em um único minuto já é excepcional,
	// então isto não estorva ninguém — serve para a inundação de UMA vaga ser cortada antes de
	// consumir o balde global, que é compartilhado por todas as empresas (ver RouteParamRateLimit).
	applicationsPerCampaignPerMinute = 40
	resumeFilesPerCampaignPerMinute  = 20
)

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
	// Análise por IA sob demanda. Dispara chamada paga, então o limite é por usuário — o teto de
	// gasto da empresa é a barreira financeira; este é o que impede um clique repetido de ocupar o
	// provedor.
	group.Post("/:id/assessment", middleware.UserRateLimit(20, time.Minute), h.Assess)
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
	// Dois limites por rota, de propósito, porque protegem de coisas diferentes: o por IP contém
	// uma origem insistente, e o global contém o ataque distribuído — que é a forma de abuso que
	// realmente importa aqui, já que estas são as únicas rotas anônimas capazes de disparar uma
	// chamada paga. Sem o global, N origens passam N vezes pelo limite por IP sem encostar nele.
	// O teto por minuto está bem acima do tráfego legítimo de uma campanha; é disjuntor, não cota.
	group.Post("/", middleware.GlobalRateLimit(publicApplicationsPerMinute, time.Minute),
		middleware.RouteParamRateLimit("id", applicationsPerCampaignPerMinute, time.Minute),
		middleware.RateLimit(5, time.Minute), h.SubmitApplication)
	// Upload de PDF: limite mais apertado por processar um arquivo (parsing tem custo de CPU
	// mesmo sendo determinístico, sem chamada de IA nenhuma — ver pkg/llm/deterministic); rota
	// própria já facilita isolar o limite sem afetar o modo texto/manual.
	group.Post("/resume-file", middleware.GlobalRateLimit(resumeFilesPerMinute, time.Minute),
		middleware.RouteParamRateLimit("id", resumeFilesPerCampaignPerMinute, time.Minute),
		middleware.RateLimit(3, time.Minute), h.SubmitResumeFile)
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

// Assess é POST /candidates/:id/assessment — gera (ou devolve, se já existe) a sugestão da IA. A
// resposta é o mesmo objeto `ai` que GET /candidates/:id devolve.
func (h *Handler) Assess(c *fiber.Ctx) error {
	id, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}
	assessment, err := h.service.Assess(c.Context(), middleware.CompanyID(c), id)
	if err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, toAIResponse(assessment))
}

// formErrorMessage é a mensagem geral de um formulário com campos inválidos; o detalhe de cada campo
// vai em Envelope.Fields.
const formErrorMessage = "Revise os campos destacados."

func (h *Handler) SubmitApplication(c *fiber.Ctx) error {
	campaignID, ok := idparam.Valid(c, "id")
	if !ok {
		return nil
	}

	var req SubmitApplicationRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Err(c, fiber.StatusBadRequest, "payload inválido")
	}
	// Primeiro as regras por campo, que dizem ao formulário O QUE está errado; a validação por tags
	// depois só pega o que o formulário nunca manda (modo inválido, honeypot, experiência via API).
	if errs := req.normalizeAndValidate(); len(errs) > 0 {
		return response.ErrFields(c, fiber.StatusBadRequest, formErrorMessage, errs)
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

	form := ResumeFileForm{Name: c.FormValue("name"), Email: c.FormValue("email"), Consent: c.FormValue("consent") == "true"}
	honeypot := c.FormValue("website")

	// Todos os erros de uma vez — os de texto e o do arquivo —, para a pessoa não corrigir um campo,
	// reenviar o PDF e só então descobrir o próximo.
	errs := form.normalizeAndValidate()
	pdfBytes, fileMsg := readResumePDF(c)
	errs.add("file", fileMsg)
	if len(errs) > 0 {
		return response.ErrFields(c, fiber.StatusBadRequest, formErrorMessage, errs)
	}

	input := PublicApplicationInput{
		Consent: form.Consent, Honeypot: honeypot, Name: form.Name, Email: form.Email, PDFBytes: pdfBytes,
	}
	if err := h.service.RegisterPublicApplication(c.Context(), campaignID, input); err != nil {
		return h.respondError(c, err)
	}
	return response.OK(c, nil)
}

// readResumePDF lê o upload para a memória e o valida pelo CONTEÚDO. Devolve a mensagem de erro do
// campo "file" (vazia quando o arquivo é aceito).
//
// Lido inteiro pra memória e nunca gravado em disco — o extrator (ver pkg/llm) processa os bytes
// direto e eles são descartados no fim desta requisição; só o texto/dado que ele extrai é persistido.
func readResumePDF(c *fiber.Ctx) ([]byte, string) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		return nil, "Selecione o arquivo do currículo em PDF."
	}
	if fileHeader.Size > maxResumeFileBytes {
		return nil, "O arquivo passa de 5 MB. Envie um PDF menor."
	}
	file, err := fileHeader.Open()
	if err != nil {
		return nil, "Não foi possível ler o arquivo. Tente enviá-lo de novo."
	}
	defer file.Close()

	pdfBytes, err := io.ReadAll(io.LimitReader(file, maxResumeFileBytes+1))
	if err != nil {
		return nil, "Não foi possível ler o arquivo. Tente enviá-lo de novo."
	}
	if int64(len(pdfBytes)) > maxResumeFileBytes {
		return nil, "O arquivo passa de 5 MB. Envie um PDF menor."
	}
	if len(pdfBytes) == 0 {
		return nil, "O arquivo está vazio."
	}
	// Conteúdo, não nome nem Content-Type: um arquivo qualquer renomeado para .pdf, ou um PDF com
	// centenas de páginas, é recusado aqui — antes de gastar CPU de parsing e, principalmente, antes
	// de PreprocessInput poder mandar bytes ilegíveis para o caminho de OCR pago.
	if err := llm.ValidatePDF(pdfBytes); err != nil {
		if errors.Is(err, llm.ErrPDFTooLong) {
			return nil, fmt.Sprintf("O PDF tem páginas demais para um currículo (máximo %d).", llm.MaxPDFPages)
		}
		return nil, "O arquivo não é um PDF válido."
	}
	return pdfBytes, ""
}

func (h *Handler) respondError(c *fiber.Ctx, err error) error {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		if len(appErr.Fields) > 0 {
			return response.ErrFields(c, appErr.Code, appErr.Message, appErr.Fields)
		}
		if appErr.Field != "" {
			return response.ErrFields(c, appErr.Code, appErr.Message, map[string]string{appErr.Field: appErr.Message})
		}
		return response.Err(c, appErr.Code, appErr.Message)
	}
	return response.Err(c, fiber.StatusInternalServerError, "erro interno")
}
