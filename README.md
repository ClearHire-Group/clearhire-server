# clearhire-server

API do Clearhire, em Go + [Fiber](https://gofiber.io). Backend do protótipo
frontend em `clearhire-app`; a modelagem de dados que este serviço implementa
está documentada e validada em `migrations/0001_init.sql`.

Contrato de cada endpoint (request/response, status codes, o que já existe
de verdade vs. o que ainda é esqueleto) está em [`docs/API.md`](docs/API.md).

## Stack

- Go 1.25, [Fiber v2](https://gofiber.io) (HTTP)
- PostgreSQL via [pgx/v5](https://github.com/jackc/pgx) (sem ORM — SQL explícito nos repositories)
- [go-playground/validator](https://github.com/go-playground/validator) pra validação de DTO
- `log/slog` pra log estruturado

## Arquitetura em camadas

Cada domínio (`internal/domain/<nome>`) segue a mesma cadeia, sempre na
mesma direção — uma camada só conhece a de baixo, nunca a de cima:

```
handler.go      → camada HTTP (Fiber): parse de request, chamada ao service, formatação de resposta
service.go      → regra de negócio — o único lugar que decide "o que fazer"
repository.go   → acesso a dado (Postgres) — o único lugar que fala SQL
model.go        → entidade de domínio
dto.go          → payloads de request/response, separados da entidade
```

**Limites que cada camada respeita:**

- **`handler.go`** nunca monta SQL, nunca decide regra de negócio (ex.: "esse
  status permite essa transição?"). Só traduz HTTP ↔ chamada de método.
  Nunca serializa o `model` direto como resposta — sempre por um DTO, pra
  campo interno (`PasswordHash`, por exemplo) nunca escapar pra JSON por
  esquecimento de tag.
- **`service.go`** nunca importa `fiber` nem `pgx` diretamente. Recebe e
  devolve tipos de domínio (`model.go`/`dto.go`), nunca `*fiber.Ctx` nem
  `pgx.Row`. É o que permite testar regra de negócio sem subir servidor HTTP
  nem banco.
- **`repository.go`** nunca decide regra de negócio — só executa a query e
  traduz o erro do driver pra um erro de domínio (`pkg/apperror`) quando faz
  sentido (ex.: "no rows" → `apperror.NotFound`).

`internal/factory` é o composition root: monta a cadeia repository → service
→ handler de cada domínio e expõe só os handlers, que o `internal/server`
pluga nas rotas. **Nenhum outro pacote instancia essas dependências à mão** —
se `main.go` ou um teste precisar de um `service` sem passar pela factory,
é sinal de que a dependência devia estar explícita no construtor.

```
cmd/api/main.go        → bootstrap: config, conexão com o banco, factory, sobe o Fiber
internal/config        → leitura de variáveis de ambiente
internal/database      → conexão com o Postgres
internal/domain/*      → um pacote por domínio (auth, company, user, campaign, candidate, talent)
internal/factory        → composition root
internal/middleware     → auth, escopo de tenant, log, recover
internal/server          → instância do Fiber + registro de rotas
pkg/apperror             → erros de domínio com código HTTP associado
pkg/response              → envelope padrão de resposta da API
pkg/logger                → logging estruturado
pkg/validator              → validação de DTOs
migrations/               → schema.sql versionado
```

Esta é a estrutura base — os handlers/services/repositories estão como
esqueleto (assinaturas e contratos, sem lógica de negócio) até a implementação
de cada domínio começar de fato.

## Multi-tenancy — a regra que não pode furar

Toda empresa cliente compartilha o mesmo banco (ver `migrations/0001_init.sql`).
O isolamento entre empresas é garantido por **chave estrangeira composta**
(`(id, company_id)`) no schema — mas isso só protege contra dado inconsistente
*dentro* de uma escrita; não impede uma query sem `WHERE company_id = ...` de
ler outra empresa.

Regra de ouro, sem exceção: **`company_id` nunca vem do corpo da requisição
nem de um parâmetro de rota.** Ele vem exclusivamente do usuário autenticado,
resolvido pelo `middleware.Auth()` + `middleware.Tenant()` e lido do contexto
da requisição (`c.Locals(...)`). Todo método de `service`/`repository` que
toca uma tabela com `company_id` recebe esse valor como parâmetro explícito
vindo do handler — nunca aceita um `company_id` que o cliente possa forjar.

Ao implementar um repository, toda query numa tabela escopada por tenant leva
`company_id` no `WHERE`, sempre. Não existe consulta "depois eu filtro na
aplicação" — filtra no SQL.

## Convenção de erros

`pkg/apperror` carrega o código HTTP junto com o erro de domínio — o service
devolve `apperror.NotFound(...)`, `apperror.Forbidden(...)` etc., e o handler
nunca decide "isso é 404 ou 400" na ponta. Padrão de handler:

```go
func (h *Handler) Get(c *fiber.Ctx) error {
    result, err := h.service.Get(c.Context(), tenantID, c.Params("id"))
    if err != nil {
        var appErr *apperror.AppError
        if errors.As(err, &appErr) {
            return response.Err(c, appErr.Code, appErr.Message)
        }
        return response.Err(c, fiber.StatusInternalServerError, "erro interno")
    }
    return response.OK(c, toDTO(result))
}
```

Toda resposta — sucesso ou erro — sai pelo envelope de `pkg/response`
(`{"success": bool, "data": ..., "error": ...}"`), nunca um JSON solto.

## Como adicionar um domínio novo

1. `internal/domain/<nome>/{model,dto,repository,service,handler}.go`, seguindo
   o padrão dos domínios existentes (interface + implementação Postgres no
   repository; interface + implementação no service).
2. `internal/factory/<nome>_factory.go` com um `Init<Nome>Factory(db) *<nome>.Handler`.
3. Adicionar o handler no struct `Factory` e na função `New` em
   `internal/factory/factory.go`.
4. Registrar as rotas em `internal/server/router.go` (público ou dentro do
   grupo `protected`, dependendo se precisa de autenticação).
5. Se o domínio introduzir tabela nova, a migration entra em `migrations/`
   como próximo número sequencial (`0002_*.sql`), nunca editando `0001_init.sql`
   depois que ele já rodou em algum ambiente.

## Testes

Ainda não existem, mas a convenção é `_test.go` ao lado do arquivo testado
(`service_test.go`, `repository_test.go`). Como `service.go` só depende da
*interface* `Repository`, testar regra de negócio não exige banco — um mock
da interface basta. Testes de `repository.go` que exigem Postgres de verdade
devem ficar marcados com build tag (`//go:build integration`) pra não travar
`go test ./...` no dia a dia.

## Rodando localmente

```
cp .env.example .env    # ajustar DATABASE_URL
make migrate            # aplica migrations/0001_init.sql
make run
```

`GET /health` confirma que o servidor está no ar.

## Variáveis de ambiente

| Variável | Obrigatória | Descrição |
|---|---|---|
| `APP_ENV` | não (default `development`) | `development` \| `production` |
| `APP_PORT` | não (default `8080`) | porta HTTP |
| `DATABASE_URL` | sim | string de conexão do Postgres |
| `JWT_SECRET` | sim, fora de desenvolvimento | segredo de assinatura dos access tokens |
| `CORS_ORIGIN` | não (default `http://localhost:4200`) | origem(ns) do frontend autenticado, separadas por vírgula se mais de uma; também usada para montar os links de e-mail de convite/redefinição de senha (ver `internal/factory/factory.go`) |
