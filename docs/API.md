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

## Ainda não implementado

Rota registrada e respondendo `501 não implementado` (sem lógica por trás):

| Endpoint | Seria |
|---|---|
| `POST /auth/invitations/:token/accept` | segundo RH aceita convite e define senha |
| `GET /users/:id` · `PATCH /users/:id` · `DELETE /users/:id` | perfil de RH / desativar assento |
| `GET /campaigns` · `POST /campaigns` · `GET /campaigns/:id` · `POST /campaigns/:id/toggle-pause` | vagas |
| `GET /candidates/:id` · `POST /candidates/:id/decisions` | candidatos e decisões manuais |
| `GET /talents` · `POST /talents` · `GET /talents/search` · `GET /talents/:id` | banco de talentos |

Todos exigem token exceto onde marcado público acima. Conforme cada um saia
do esqueleto, a seção dele sobe pra "Endpoints implementados" com o mesmo
formato desta página.
