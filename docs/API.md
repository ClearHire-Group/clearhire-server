# API — clearhire-server

Base URL local: `http://localhost:8080/api/v1`. Toda resposta sai no mesmo
envelope (`pkg/response`):

```json
{ "success": true,  "data": { ... } }
{ "success": false, "error": "mensagem" }
```

## Autenticação

A API é multi-tenant: todo endpoint fora de `/auth` e `POST /companies` exige
um access token válido, enviado como:

```
Authorization: Bearer <accessToken>
```

O token carrega `userID`, `companyID` e `role` — é dali que todo dado
retornado é escopado, nunca de um parâmetro da URL ou do corpo da requisição.
Token expira em 15 minutos; não existe endpoint de refresh ainda (o
`refreshToken` devolvido no login é persistido, mas nada troca ele por um
access token novo por enquanto — ver "Ainda não implementado").

---

## Endpoints implementados

### `POST /companies` — cadastrar empresa

Público (não exige token — é o que cria a primeira conta). Cria a empresa e
o primeiro RH, com `role=owner`.

**Request**
```json
{
  "companyName": "Aurora Tech",
  "ownerName": "Pedro Soterio",
  "ownerEmail": "pedro@aurora.com",
  "ownerPassword": "senha-com-pelo-menos-8-caracteres"
}
```

| Campo | Obrigatório | Regra |
|---|---|---|
| `companyName` | sim | — |
| `ownerName` | sim | — |
| `ownerEmail` | sim | formato de e-mail; único no sistema (não só na empresa) |
| `ownerPassword` | sim | mínimo 8 caracteres |

**Resposta — `201 Created`**
```json
{ "success": true, "data": { "id": "uuid", "name": "Aurora Tech" } }
```

**Erros**
| Status | Quando |
|---|---|
| `400` | payload inválido/incompleto, ou `ownerEmail` já cadastrado |
| `500` | falha inesperada (hash de senha, banco) |

**Nota:** a resposta não inclui token — o cadastro não loga a pessoa
automaticamente. Pra ter uma sessão, o cliente chama `POST /auth/login` em
seguida com o mesmo e-mail/senha. Isso está documentado como um ponto aberto
em `CLAUDE.md`; se decidirmos unificar (cadastro já devolve sessão), este
documento precisa mudar junto.

---

### `POST /auth/login` — autenticar

Público. Devolve um par de tokens.

**Request**
```json
{ "email": "pedro@aurora.com", "password": "senha-com-pelo-menos-8-caracteres" }
```

**Resposta — `200 OK`**
```json
{
  "success": true,
  "data": {
    "accessToken": "eyJ...",
    "refreshToken": "a1b2c3..."
  }
}
```

**Erros**
| Status | Quando |
|---|---|
| `400` | payload inválido |
| `401` | e-mail não encontrado, senha incorreta, ou usuário desativado (`is_active=false`) — mesma mensagem pros três casos, de propósito, pra não revelar qual e-mail existe |

---

### `GET /company-profile` — ler perfil cultural da empresa

Protegido. Sempre a empresa do usuário autenticado — não existe parâmetro de
rota; não é possível ler o perfil de outra empresa.

**Resposta — `200 OK`**
```json
{
  "success": true,
  "data": {
    "name": "Aurora Tech",
    "values": ["Transparência radical", "Autonomia com responsabilidade"],
    "tone": "Direto, mas empático...",
    "importance": "Times pequenos e autônomos..."
  }
}
```
`values`/`tone`/`importance` vêm vazios (`[]`/`""`) numa empresa recém-cadastrada
— ninguém preencheu ainda, não é erro.

**Erros**
| Status | Quando |
|---|---|
| `401` | token ausente/inválido/expirado |

---

### `POST /company-profile` — atualizar perfil cultural da empresa

Protegido. Substitui `tone`/`importance` e a lista de `values` inteira (não é
um merge — mandar `values` sem um item existente remove esse item).

**Request**
```json
{
  "tone": "Direto, mas empático...",
  "importance": "Times pequenos e autônomos...",
  "values": ["Transparência radical", "Autonomia com responsabilidade"]
}
```

| Campo | Obrigatório | Regra |
|---|---|---|
| `tone` | não | máximo 2000 caracteres |
| `importance` | não | máximo 2000 caracteres |
| `values` | não | máximo 10 itens, cada um até 60 caracteres; espaços nas pontas e itens repetidos são limpos automaticamente pelo servidor |

**Resposta — `200 OK`**: mesmo formato de `GET /company-profile`, já com o
valor salvo.

**Erros**
| Status | Quando |
|---|---|
| `400` | payload inválido (item de `values` fora do limite de tamanho, mais de 10 itens, etc.) |
| `401` | token ausente/inválido/expirado |

**Nota:** não existe override por campanha — toda nova campanha herda este
perfil no momento da criação. Alterar aqui não re-processa campanhas já
criadas (hoje nada lê este perfil no momento de pontuar um candidato; quando
o scoring de Fit Cultural existir, esta nota precisa ser revisitada).

---

### `PATCH /campaigns/:id` — atualizar dados da campanha

Protegido. Seção "Dados da campanha" em Configurações da Campanha — título,
descrição, cidade/estado, modalidade, tipo de contrato e senioridade. Nunca
altera status nem fases do funil (ver endpoint abaixo).

**Request**
```json
{
  "title": "Engenheiro(a) de Software Sênior — Backend",
  "description": "Vaga para o time de plataforma...",
  "city": "São Paulo",
  "state": "SP",
  "modality": "hibrido",
  "contractType": "clt",
  "seniority": "senior"
}
```

| Campo | Obrigatório | Regra |
|---|---|---|
| `title` | sim | máximo 200 caracteres |
| `description` | não | máximo 5000 caracteres |
| `city` / `state` | não | máximo 100 caracteres cada |
| `modality` | sim | `remoto` \| `hibrido` \| `presencial` |
| `contractType` | sim | `clt` \| `pj` \| `estagio` |
| `seniority` | sim | `junior` \| `pleno` \| `senior` |

**Resposta — `200 OK`**: mesmo formato de `GET /campaigns/:id`, já com os
valores salvos.

**Erros**
| Status | Quando |
|---|---|
| `400` | payload inválido |
| `404` | campanha não existe (ou não pertence à empresa do token) |

---

### `PATCH /campaigns/:id/phases` — atualizar fases do funil

Protegido. Seção "Fases do funil" em Configurações da Campanha — adiciona,
remove e reordena os módulos opcionais (`fit`/`tecnica`/`entrevista`).
"Recebidos" e "Selecionados" são sempre implícitas, nunca vão em `phaseKeys`.

**Request**
```json
{ "phaseKeys": ["tecnica", "entrevista"] }
```

| Campo | Obrigatório | Regra |
|---|---|---|
| `phaseKeys` | não (lista vazia é funil válido) | máximo 3 itens, sem repetição, cada um `fit`\|`tecnica`\|`entrevista` |

**Resposta — `200 OK`**: mesmo formato de `GET /campaigns/:id`.

**Erros**
| Status | Quando |
|---|---|
| `400` | payload inválido, **ou** a lista nova remove uma fase que ainda tem candidato nela |
| `404` | campanha não existe (ou não pertence à empresa do token) |

---

### `GET /dashboard/activity` — feed de atividade

Protegido. Aba "Atividade" do Dashboard — histórico de ações da IA e decisões
manuais da empresa inteira, mais recente primeiro.

**Query**

| Parâmetro | Obrigatório | Regra |
|---|---|---|
| `limit` | não | default `50`, teto `200`; valor fora da faixa é truncado, nunca `400` |

**Resposta — `200 OK`**
```json
{ "success": true, "data": [
  { "id": "3fac…", "actor": "recrutador", "message": "Campanha pausada.",
    "campaignId": "6726…", "campaignTitle": "Eng. Backend Sênior",
    "createdAt": "2026-09-17T14:58:57.016817-03:00" }
] }
```

`actor` é `ia` ou `recrutador`. `campaignId`/`campaignTitle` são **omitidos
juntos** em dois casos: ação de nível empresa (`activity_feed.campaign_id` é
nulável) e campanha arquivada — nos dois o front renderiza a linha sem link,
em vez de um link que abriria em `404`.

`createdAt` é ISO 8601, não rótulo pronto: o texto relativo ("há 12 minutos")
é derivado no cliente a cada render (`clearhire-app core/relative-time.ts`),
senão congelaria no instante da resposta.

**Escrita**: não há endpoint pra inserir no feed. As linhas são gravadas pelos
próprios domínios, **na mesma transação** da ação que registram — criar campanha,
pausar/retomar, avançar/reprovar candidato e candidatura pública recebida. Uma
ação que deu rollback não deixa rastro no histórico.

---

## Ainda não implementado

Rota registrada e respondendo `501 não implementado` (sem lógica por trás):

| Endpoint | Seria |
|---|---|
| `POST /auth/invitations/:token/accept` | segundo RH aceita convite e define senha |
| `GET /users/:id` · `PATCH /users/:id` · `DELETE /users/:id` | perfil de RH / desativar assento |
| `GET /candidates/:id` · `POST /candidates/:id/decisions` | candidatos e decisões manuais |
| `GET /talents` · `POST /talents` · `GET /talents/search` · `GET /talents/:id` | banco de talentos |

`GET /campaigns` · `POST /campaigns` · `GET /campaigns/:id` ·
`POST /campaigns/:id/toggle-pause` · `POST /campaigns/:id/public-application-link`
já estão implementados (fora desta lista) — não documentados nesta versão do
arquivo com o mesmo detalhe de `PATCH /campaigns/:id` acima; ver
`internal/domain/campaign/handler.go` como fonte de verdade enquanto a seção
completa de campanhas não sobe pra "Endpoints implementados".

Todos exigem token exceto onde marcado público acima. Conforme cada um saia
do esqueleto, a seção dele sobe pra "Endpoints implementados" com o mesmo
formato desta página.
