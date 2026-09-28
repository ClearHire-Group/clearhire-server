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

### `GET /dashboard/ai-suggestions` — card "Pendências de revisão"

Protegido. **Sem LLM**: consulta + regra determinística sobre avaliações que a IA já gravou
(`candidate_ai_assessments`), nunca uma chamada nova ao provedor. Duas fontes, nesta ordem:

1. Candidatos com `status = 'aguardando_decisao'` cuja avaliação mais recente **na fase atual**
   tem `match_pct >= 80` (`internal/domain/dashboard/service.go`, `readyMatchThreshold`) — maior
   match primeiro, até 3. O primeiro da lista vem com `highlighted: true`.
2. Grupos de candidatos `aguardando_decisao` por (campanha, fase) com 2 ou mais candidatos — maior
   grupo primeiro, até 2.

Um candidato pode aparecer nos dois: no individual (pelo match) e contado no grupo da fase (pela
espera) — respondem perguntas diferentes ("quem ver primeiro" × "quanto está parado"). Sem
candidata nenhuma, `data` é `[]`, nunca `null`.

**Resposta — `200 OK`**
```json
{ "success": true, "data": [
  { "id": "ready-3fac…", "message": "Marina Albuquerque (94% de match) pode avançar de Triagem Técnica para Entrevista Estruturada em Eng. Backend Sênior.",
    "primaryActionLabel": "Revisar candidatura", "primaryActionRoute": ["/campanhas", "6726…", "candidatos", "3fac…"],
    "highlighted": true },
  { "id": "waiting-6726…-fit", "message": "7 candidatos analisados em Fit Cultural para Customer Success Pleno aguardam sua decisão.",
    "primaryActionLabel": "Ver candidatos", "primaryActionRoute": ["/campanhas", "6726…", "candidatos"],
    "highlighted": false }
] }
```

`primaryActionRoute` é a lista de segmentos que o `routerLink` do Angular espera, não uma URL
pronta. `id` não é persistido — é montado a partir do id do candidato ou de (campanha, fase), então
muda se o candidato avançar/sair da lista, o que é o comportamento esperado (a lista sempre reflete
o estado atual, nunca um histórico).

**Escrita**: não existe. Ao contrário de `ai_suggestions` (tabela do schema, hoje sem nenhum
código lendo ou escrevendo nela), este endpoint não persiste nada — cada chamada recalcula na
hora, então não há "sugestão dispensada" nem card obsoleto.

---

## Ainda não implementado

Rota registrada e respondendo `501 não implementado` (sem lógica por trás):

| Endpoint | Seria |
|---|---|
| `POST /auth/invitations/:token/accept` | segundo RH aceita convite e define senha |
| `GET /users/:id` · `PATCH /users/:id` · `DELETE /users/:id` | perfil de RH / desativar assento |
| `GET /candidates/:id` · `POST /candidates/:id/decisions` | candidatos e decisões manuais |
| `POST /candidates/:id/assessment` | análise por IA (ver abaixo) |

`GET /campaigns` · `POST /campaigns` · `GET /campaigns/:id` ·
`POST /campaigns/:id/toggle-pause` · `POST /campaigns/:id/public-application-link`
já estão implementados (fora desta lista) — não documentados nesta versão do
arquivo com o mesmo detalhe de `PATCH /campaigns/:id` acima; ver
`internal/domain/campaign/handler.go` como fonte de verdade enquanto a seção
completa de campanhas não sobe pra "Endpoints implementados".

### Listagens: tamanho, compressão e ordenação

- Toda resposta fora de `/auth` sai comprimida (gzip/brotli, conforme `Accept-Encoding`). `/auth` fica de fora
  de propósito (BREACH: o login devolve o token no corpo).
- `GET /talents` devolve cada talento **resumido**: experiências sem descrição e `history: []`. O perfil
  completo é `GET /talents/:id`. Medido com 3 mil talentos: 8,5 MB → 231 KB.
- `GET /campaigns/:id/candidates` traz `yearsExperience` (número) e `appliedAt` (ISO) além dos rótulos de texto,
  para a tela ordenar por experiência e data.
- **Ordenação, filtro e busca rápida dessas listas rodam no navegador**, sobre a lista carregada
  (`clearhire-app/src/app/core/table-sort.ts`): ~2 ms para 3 mil linhas. A API não aceita parâmetros de
  ordenação. Se as listas passarem de dezenas de milhares de linhas, o caminho é paginar no servidor, com a
  ordenação indo junto por uma lista fechada de colunas.

### Banco de Talentos — `/talents`

Protegido, escopado pela empresa do token. **Quem está no banco** (`talents.bank_entered_at` preenchido,
ver `migrations/0012`) entra só por um destes caminhos — nenhum automático:

| Caminho | Quando | Estado de consentimento |
|---|---|---|
| Reprovação qualificada (`origin: reprovacao_qualificada`) | motivo com `goesToBank` **e** `sendBankInvite: true` em `POST /candidates/:id/decisions` | mantém `consentido` de quem autorizou no formulário público; senão `notificado` (legítimo interesse) |
| Aprovação (`origin: aprovacao`) | candidato avança até **Selecionados** e já tinha consentido | `consentido` |
| Cadastro manual (`origin: cadastro_manual`) | `POST /talents` | `nao_notificado` até o primeiro contato |

Candidatura pública cria o registro da pessoa (dados + consentimento) mas **não** a põe no banco. A mesma
pessoa (mesmo e-mail na empresa) é sempre um registro só; quem pediu exclusão (`oposicao_exclusao`) nunca
volta ao banco. `POST /candidates/:id/decisions` devolve `talentId` quando a decisão levou a pessoa ao banco.

| Rota | Resposta |
|---|---|
| `GET /talents` | `Talent[]` do banco (formato de `Talent` em `clearhire-app/src/app/core/models.ts`) |
| `GET /talents/:id` | `Talent`; `404` se não está no banco desta empresa |
| `POST /talents` | `201 Talent`. Body `{ name, rawProfileText?, contextNote? }`; o perfil colado passa pela extração de IA (cache, teto de gasto). Erros por campo em `fields`; pessoa já registrada = `fields.rawProfileText`. Telefone e pretensão salarial do perfil colado não são guardados (perfil manual guarda só dado profissional). |
| `POST /talents/:id/first-contact` | `Talent`; `nao_notificado` → `notificado` |
| `GET /talents/coverage` | `[{ skillTerm, count }]` — quem pode ser chamado, por skill |
| `GET /talents/search`, `GET /talents/:id/similar` | `501` — ainda não implementados (o front mostra vazio) |

### Erros de formulário da candidatura pública (`POST /public/campaigns/:id/applications[/resume-file]`)

Formulário inválido responde `400` com o erro **por campo** em `fields`, para o front mostrar cada
mensagem no próprio campo. As regras estão em `internal/domain/candidate/application_validation.go`,
espelhadas em `clearhire-app/src/app/core/application-validation.ts`.

```json
{ "success": false, "error": "Revise os campos destacados.",
  "fields": { "email": "Confira o e-mail: você quis dizer pedro@gmail.com?", "phone": "Use apenas números no telefone, ex.: (61) 99999-9999." } }
```

Chaves possíveis: `name`, `email`, `phone`, `linkedinUrl`, `city`, `state`, `yearsExperience`, `summary`,
`educationDegree`, `educationInstitution`, `educationPeriod`, `skills`, `resumeText`, `file`, `consent`.
Candidatura duplicada também vem como `fields.email`. Valores válidos são gravados normalizados:
telefone `(61) 99902-3060`, LinkedIn `https://www.linkedin.com/in/<perfil>`, estado como UF, e-mail em
minúsculas, skills sem repetição. O texto de currículo colado vai de 100 a 14.000 caracteres.

### `POST /candidates/:id/assessment` — análise por IA (sugestão)

Protegido, escopado pela empresa do token (candidato de outra empresa = `404`). Gera a sugestão da IA
para o candidato **na fase em que ele está** e a grava; também é disparada automaticamente, em
background, depois de toda candidatura pública. **Só sugere**: nunca move de fase, decide ou reprova
ninguém (`POST /candidates/:id/decisions` continua sendo o único caminho de decisão, manual).

Idempotente por (candidato, fase, versão do prompt): pedir de novo devolve a análise existente sem
nova chamada ao provedor nem novo custo. Ao avançar de fase, uma nova análise pode ser pedida.

**Resposta — `200 OK`** (mesmo objeto de `ai` em `GET /candidates/:id`)
```json
{ "success": true, "data": {
  "matchPct": 82, "matchLabel": "Bom match", "matchNote": "Forte em backend.",
  "strengths": ["Python avançado, 6 anos"], "concerns": ["Sem experiência declarada com Kubernetes"],
  "justification": "Seis anos de Python e pipelines de dados batem com os requisitos."
} }
```

| Status | Quando |
|---|---|
| `400` | candidato já com decisão final (reprovado/contratado), ou teto mensal de IA da empresa atingido |
| `404` | candidato não existe ou é de outra empresa |
| `429` | mais de 20 pedidos/min do mesmo usuário |
| `503` | IA não habilitada neste ambiente (`LLM_PROVIDER` ≠ `groq`) ou provedor indisponível/limitado — tente de novo |

**Mudança de contrato:** `GET /candidates/:id` agora devolve `"ai": null` enquanto o candidato não foi
avaliado (antes era um objeto zerado, exibido como "0%"). O que a IA enxerga: dados da vaga e o perfil
estruturado do candidato — **sem** nome, e-mail, telefone ou LinkedIn.

Todos exigem token exceto onde marcado público acima. Conforme cada um saia
do esqueleto, a seção dele sobe pra "Endpoints implementados" com o mesmo
formato desta página.
