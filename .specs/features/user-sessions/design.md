# Per-Device User Sessions Design

**Spec:** `.specs/features/user-sessions/spec.md`
**Context:** `.specs/features/user-sessions/context.md`
**Status:** Draft

> **Status de decisões**: as 9 decisões de design-phase (híbrido `SessionsRevokedAt`, quebra de JWTs pré-deploy, captura de UA/IP via helper, assinatura de `RequireAuth`, `Logout` lê sid, `SwitchTenant` reusa sid, TTL único 24h, race trivial, sem cleanup) estão em `context.md` — este `design.md` arquiva **como** cada uma é implementada, não **o que** foi decidido.

---

## 1. Visão arquitetural

Hoje, o JWT (`internal/auth/session.go`) é stateless — assinado com `VANE_SESSION_SECRET`, claims `sub`/`iat`/`exp`/`tid`, e o único estado servidor é `users.sessions_revoked_at`, gravado por 4 caminhos de "evento do user/admin" e checado em `RequireAuth` comparando `iat` com o timestamp.

A feature introduz uma tabela `sessions` no banco, e o JWT passa a carregar o `id` da row como claim `sid`. `RequireAuth` ganha 1 lookup (PK) por request autenticado, atrás da checagem de `users.sessions_revoked_at` que já existe. O estado do servidor vai de "1 timestamp global por user" pra "N rows por user, com revogação individual e `last_seen_at` throttled".

Fluxo de alto nível:

```
Login (correto + sem 2FA)
  ↓ issueSessionForUser(userID, activeTenantID, ua, ip)
  ↓ 1. INSERT INTO sessions (..., created_at=now())
  ↓ 2. sid = row.id
  ↓ 3. IssueSessionWithTenant(userID, activeTenantID, sid, secret) → JWT
  ↓ 4. set vane_session cookie
  ↓ response

Request autenticado
  ↓ RequireAuth(secret, users, sessions)
  ↓ 1. Verify JWT (claims sub/iat/exp/tid/sid)
  ↓ 2. users.GetByID(sub) → checa iat vs SessionsRevokedAt → 401 se revoked
  ↓ 3. sessions.GetByID(sid) → checa revoked_at → 401 se revoked
  ↓ 4. throttle last_seen_at (≥5min desde último update) → UPDATE se passou
  ↓ context: {user, activeTenant, sid}
  ↓ tenant middleware abre tx, seta app.user_id/app.tenant_id

GET /api/auth/sessions
  ↓ requireAuth + anyRole (self)
  ↓ sessions.ListForUser(userID, excludeOlderThan=now-24h)
  ↓ marca current = (row.id == ctx.sid)
  ↓ response

DELETE /api/auth/sessions/{id}
  ↓ requireAuth + anyRole (self)
  ↓ if {id} == ctx.sid → 409
  ↓ sessions.GetByIDAndUser({id}, userID) → 404 se não é do user
  ↓ sessions.Revoke({id})
  ↓ 200

POST /api/auth/logout
  ↓ requireAuth
  ↓ sessions.Revoke(ctx.sid) ← NOVO
  ↓ cookie.MaxAge = -1
  ↓ 200
```

---

## 2. Data model

### 2.1 Migration `0031_sessions.up.sql`

```sql
CREATE TABLE sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_agent  TEXT,                 -- raw header; NULL se ausente
    ip          INET,                 -- RemoteAddr; NULL se não determinável
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ,         -- throttled update; NULL = nunca vista
    revoked_at  TIMESTAMPTZ           -- NULL = ativa
);

CREATE INDEX sessions_user_id_created_at_idx
    ON sessions (user_id, created_at DESC);

CREATE INDEX sessions_user_id_active_idx
    ON sessions (user_id)
    WHERE revoked_at IS NULL;
```

**Decisões mecânicas** (registradas em `context.md` §"Agent's Discretion"):

- `id UUID` — mesmo padrão de PK do projeto; `gen_random_uuid()` nativo do Postgres 13+, sem dependência nova.
- `INET` para IP — tipo nativo; `RemoteAddr` (formato `host:port`) é parseado no repo.
- `ON DELETE CASCADE` — apagar `users` apaga as sessões (consistente com a decisão de `admins.go:631-637` que deleta `users` quando é a última membership; sessões do user deletado não fazem sentido ficar).
- Dois índices: `(user_id, created_at DESC)` cobre a listagem ordenada por mais recente; `(user_id) WHERE revoked_at IS NULL` cobre a checagem "tem sessão ativa?" se vier a ser usada em queries futuras (não bloqueante — pode ser adicionado sob demanda via `EXPLAIN`).
- **Sem RLS** — escopo sempre vem do `RequireAuth`/`ctx.sid`, e o handler checa ownership antes de qualquer SELECT/DELETE. Mesmo padrão de `tenant_memberships` (sem policy, acesso por `app.user_id` no `Pool.BeginUserTx`).

### 2.2 Sem mudança em `users`

Coluna `sessions_revoked_at` **fica** (decision #1 do `context.md`). Eventos de "user trocou de credencial" continuam derrubando tudo de uma vez.

---

## 3. Mudanças por arquivo

### 3.1 `internal/auth/session.go`

**Estado atual** (L28-31, L98-123): `sessionClaims` carrega `RegisteredClaims + TenantID("tid")`; `VerifySessionClaims` rejeita tokens com `Audience` não-vazia.

**Mudança**:

```go
// sessionClaims — adiciona sid (SessionID)
type sessionClaims struct {
    jwt.RegisteredClaims
    TenantID   string `json:"tid"`
    SessionID  string `json:"sid"`
}

// IssueSessionWithTenant — assinatura nova
func IssueSessionWithTenant(userID, tenantID, sessionID uuid.UUID, secret string) (string, error)

// VerifySessionClaims — rejeita token sem sid parseável como UUID
// 1. Verifica assinatura + exp (igual hoje)
// 2. Verifica audience vazia (igual hoje)
// 3. NOVO: claims.SessionID não pode ser string vazia E deve parsear como uuid.UUID
//    → ErrInvalidClaims se falhar (cobre JWTs emitidos antes do deploy)
```

Tipo `SessionID` é `uuid.UUID` direto (não string). A serialização JSON fica `"sid": "<uuid>"`; a desserialização exige UUID válido.

**NÃO mexe em** `IssueTwoFactorChallenge` / `VerifyTwoFactorChallenge` (`internal/auth/two_factor.go`) — challenge tokens continuam com `Audience=["2fa_challenge"]`, e `VerifySessionClaims` continua rejeitando-os (audience não-vazia) sem nunca chegar a checar `sid`. Defesa em profundidade preservada.

### 3.2 `internal/db/session_repository.go` (novo)

Interface mínima exposta via struct `SessionRepository{ pool *Pool }`:

```go
type Session struct {
    ID          uuid.UUID
    UserID      uuid.UUID
    UserAgent   sql.NullString
    IP          sql.NullString  // pgtype.INET na prática
    CreatedAt   time.Time
    LastSeenAt  sql.NullTime
    RevokedAt   sql.NullTime
}

type SessionRepository struct{ pool *Pool }

func NewSessionRepository(pool *Pool) *SessionRepository

// Create — INSERT returning id (cobre SESS-01)
func (r *SessionRepository) Create(ctx, userID uuid.UUID, userAgent, ip string) (uuid.UUID, error)

// GetByID — PK lookup; usado por RequireAuth no warm path (SESS-03)
func (r *SessionRepository) GetByID(ctx, id uuid.UUID) (*Session, error)

// ListForUser — para GET /api/auth/sessions (SESS-05/06)
// Filtra revoked_at IS NULL AND created_at >= now() - SessionTTL
// Ordena created_at DESC
func (r *SessionRepository) ListForUser(ctx, userID uuid.UUID) ([]Session, error)

// GetByIDAndUser — para DELETE /api/auth/sessions/{id} (SESS-10)
// Retorna ErrNotFound se não pertence ao user (não revela existência)
func (r *SessionRepository) GetByIDAndUser(ctx, id, userID uuid.UUID) (*Session, error)

// Revoke — UPDATE SET revoked_at = now() WHERE id = ? AND revoked_at IS NULL (SESS-07/11)
// Idempotente (replay em sessão já revogada = 0 rows affected, mas sem erro)
func (r *SessionRepository) Revoke(ctx, id uuid.UUID) error

// TouchLastSeen — UPDATE SET last_seen_at = now() WHERE id = ? AND
//   (last_seen_at IS NULL OR last_seen_at < now() - 5min) (SESS-04)
// 0 rows affected = "throttled, skip"
func (r *SessionRepository) TouchLastSeen(ctx, id uuid.UUID) error

// RevokeAllForUser — UPDATE SET revoked_at = now() WHERE user_id = ? AND revoked_at IS NULL
// Substitui UserRepository.RevokeSessions nos callers UpdateRole/Delete (decision #1)
func (r *SessionRepository) RevokeAllForUser(ctx, userID uuid.UUID) error
```

`GetByID` é o método no **warm path** (1 query por request autenticado). Os outros só rodam em endpoints específicos (login, list, revoke) e têm frequência muito menor.

### 3.3 `internal/api/middleware.go`

**Hoje** (`RequireAuth(secret string, users UserGetter)` em L87): valida JWT → `users.GetByID(sub)` → checa `iat` vs `SessionsRevokedAt`.

**Nova assinatura**:

```go
type sessionGetter interface {
    GetByID(ctx context.Context, id uuid.UUID) (*db.Session, error)
    TouchLastSeen(ctx context.Context, id uuid.UUID) error
}

func RequireAuth(secret string, users UserGetter, sessions sessionGetter, logger *zap.Logger) func(http.Handler) http.Handler
```

Adiciona `*zap.Logger` para logging do throttle (caso o `TouchLastSeen` falhe — fail-open, mesmo padrão de `ratelimit`).

**Comportamento novo** (após `users.GetByID` + check `SessionsRevokedAt`):

1. `sid, err := claims.SessionID` — se vazio ou não-UUID → 401
2. `sess, err := sessions.GetByID(ctx, sid)` — se `ErrNotFound` ou qualquer erro → 401
3. `if sess.RevokedAt.Valid` → 401
4. `sessions.TouchLastSeen(ctx, sid)` — fire-and-forget, loga erro mas não bloqueia
5. `ctx.Set("sid", sid)` (mesmo padrão de `user`/`activeTenant` em L128-130)

Wiring em `internal/cli/routes.go:95`:

```go
protected := router.With(RequireAuth(cfg.SessionSecret, users, sessions, h.logger))
```

`protected` é o grupo já usado por todas as rotas autenticadas — única chamada, único ponto de wiring.

### 3.4 `internal/api/auth_handler.go`

**`captureSessionContext(r)`** (helper privado novo, definido perto de `sessionCookie` em L386):

```go
func captureSessionContext(r *http.Request) (userAgent, ip string) {
    userAgent = r.UserAgent()  // "" se ausente
    // ip: mesma lógica de internal/ratelimit/ip_limiter.go:126-136
    // (RemoteAddr → host:port → host)
    if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
        ip = host
    }
    return
}
```

**`issueSessionForUser`** (helper existente em L336) — assinatura nova:

```go
func (h *AuthHandler) issueSessionForUser(
    w http.ResponseWriter, r *http.Request,
    user *db.User, activeTenantID string,
) error {
    sessID, err := h.sessions.Create(r.Context(), user.ID,
        captureSessionContext(r))  // devolve (ua, ip)
    if err != nil { return err }

    token, err := auth.IssueSessionWithTenant(
        user.ID, uuid.MustParse(activeTenantID), sessID, h.sessionSecret)
    if err != nil { return err }

    setSessionCookie(w, token)
    h.loginResponse{Token: token, User: ...}.WriteJSON(w)  // shape preservado
    return nil
}
```

Os 4 call sites (`Login` em L171, `VerifyTwoFactor` em L324, `SwitchTenant` em L833, `AcceptInvite` em `admins.go:382`, `Bootstrap.Create` em `bootstrap_handler.go:190`) **não mudam de assinatura** — só o que está dentro do helper muda.

**`SwitchTenant`** (L801-844) — sem criar nova row:

```go
func (h *AuthHandler) SwitchTenant(w, r) {
    sid := sidFromContext(r.Context())  // exige RequireAuth antes
    // ... valida membership no novo tenant (igual hoje)
    token, err := auth.IssueSessionWithTenant(
        user.ID, newTenantID, sid, h.sessionSecret)  // mesmo sid, novo tid
    setSessionCookie(w, token)
    // response igual hoje
}
```

`SessionsRevokedAt` continua sendo checado em `RequireAuth` antes do `SwitchTenant` rodar — mudanças de role/credencial entre o `iat` do token atual e a reemissão derrubam como hoje.

**`Logout`** (L777-780) — agora revoga:

```go
func (h *AuthHandler) Logout(w, r) {
    sid := sidFromContext(r.Context())
    if err := h.sessions.Revoke(r.Context(), sid); err != nil {
        h.logger.Warn("logout: revoke failed (cookie still cleared)", zap.Error(err))
        // fail-open: cookie é o que o usuário vê, revogação é best-effort
    }
    clearSessionCookie(w)  // cookie.MaxAge = -1, igual hoje
    w.WriteHeader(200)
}
```

`sidFromContext` é um helper de uma linha que extrai o `uuid.UUID` que `RequireAuth` deixou no context (mesmo padrão de `userFromContext`/`tenantFromContext`).

### 3.5 `internal/api/admins.go`

**`UpdateRole`** (L571) e **`Delete`** (L619) — substituem `users.RevokeSessions(targetID)` por `sessions.RevokeAllForUser(targetID)`.

```go
// UpdateRole, depois do role change aplicado:
if err := h.sessions.RevokeAllForUser(r.Context(), target.ID); err != nil {
    h.logger.Error("admins: revoke after role change failed", zap.Error(err))
    return  // ou apenas log? ver nota abaixo
}
```

**Nota sobre erro**: hoje `users.RevokeSessions` é chamado e o erro é logado mas não tratado como falha do request (a role já mudou, sessão fica ativa até o próximo check). Mantemos o mesmo padrão em `sessions.RevokeAllForUser` — log + continua.

### 3.6 `internal/api/sessions_handler.go` (novo)

```go
type SessionsHandler struct {
    sessions *db.SessionRepository
    logger   *zap.Logger
}

func NewSessionsHandler(sessions *db.SessionRepository, logger *zap.Logger) *SessionsHandler

// GET /api/auth/sessions
// Auth: requireAuth + anyRole
// Lista sessões ativas do user (exclui revoked_at não-nulo e created_at > 24h)
// Marca current = (row.id == ctx.sid)
// Response: []SessionView
type SessionView struct {
    ID         uuid.UUID `json:"id"`
    UserAgent  *string   `json:"user_agent,omitempty"`
    IP         *string   `json:"ip,omitempty"`
    CreatedAt  time.Time `json:"created_at"`
    LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
    Current    bool      `json:"current"`
}

func (h *SessionsHandler) List(w, r) { ... }

// DELETE /api/auth/sessions/{id}
// Auth: requireAuth + anyRole
// 409 se {id} == ctx.sid
// 404 se {id} não existe OU não pertence ao user (anti-enumeration)
// 200 + revoke caso contrário
func (h *SessionsHandler) Revoke(w, r) { ... }
```

`{id}` no path é validado como UUID antes de qualquer query — UUID malformado ⇒ 404 (mesmo path de "não existe" pra não vazar formato).

### 3.7 `internal/cli/routes.go`

Mudanças:

1. L88 (junto com `NewPollerStatusHandler`): `sessions := db.NewSessionRepository(pool)` (declarado no mesmo lugar).
2. L95: `RequireAuth(cfg.SessionSecret, users, sessions, h.logger)` (assinatura nova).
3. `NewAuthHandler(...)` em L78 — adiciona `sessions` na dependência.
4. `NewSessionsHandler(...)` — declaração nova.
5. Rotas novas (depois de `me GET/PATCH`, antes de `logout`):
   - `protected.With(anyRole).Get("/api/auth/sessions", sessionsHandler.List)`
   - `protected.With(anyRole).Delete("/api/auth/sessions/{id}", sessionsHandler.Revoke)`

Nenhuma outra rota muda.

### 3.8 Frontend

**Tipos** (`web/src/types/api.ts:101+`): adicionar `SessionView` mirroring o `SessionView` Go (snake_case via fetch normal).

**MSW handler** (`web/src/test/msw/handlers.ts`): `GET /api/auth/sessions` retorna `SessionView[]`; `DELETE /api/auth/sessions/{id}` responde 200. Dados em `web/src/lib/mockData.ts` — seed com 2-3 sessões fake pra cobrir o caso "lista não-vazia + current marcada".

**Hook** (`web/src/features/sessions/hooks.ts`, novo): `useSessions()` retornando `{ data, isLoading, revoke }`. Hook de revoke usa `useMutation` + invalida `queryKey: ['sessions']` no success.

**Página** (`web/src/features/sessions/SessionsSection.tsx`, novo): reescrita da seção "Sessões ativas" dentro de `Meu Perfil`, renderizando a `SessionView[]`:
- Lista vertical com 1 row por sessão
- Row com `current: true` marcada visualmente distinta (badge "Sessão atual"), sem botão "Encerrar"
- Outras rows com botão "Encerrar" → `revoke.mutate(row.id)` → confirma → toast "Sessão encerrada" → invalida lista

**i18n**: 2 strings novas em `pt-BR.json` e `en.json` ("Sessão atual", "Encerrar").

**Testes** (Vitest + Testing Library, mesma profundidade do resto do `web/`):
- Hook testa 200/401/404 por papel (matches `admin-frontend` I13 padrão)
- Página testa empty state (1 sessão, a current), lista cheia (current + outras), revoke flow, optimistic update

---

## 4. Contrato de API — diff

| Endpoint | Antes | Depois |
|---|---|---|
| `POST /api/auth/login` | 200 + `{token}` + cookie | inalterado (corpo igual, agora `token` carrega `sid`) |
| `POST /api/auth/login/verify-2fa` | 200 + `{token}` + cookie | inalterado |
| `POST /api/auth/invite/{token}/accept` | 200 + `{token}` + cookie | inalterado |
| `POST /api/instance/bootstrap` | 200 + `{token}` + cookie | inalterado |
| `POST /api/auth/switch-tenant` | 200 + `{token}` + cookie | inalterado (mesmo sid, novo tid) |
| `POST /api/auth/logout` | 200, só limpa cookie | 200, **também revoga row** |
| `GET /api/auth/sessions` | (404) | **200** com `SessionView[]` |
| `DELETE /api/auth/sessions/{id}` | (404) | **200/404/409** |
| Qualquer rota autenticada | 401 se `iat < SessionsRevokedAt` | **+ 401 se `sid` faltando / revogado** |

Corpo de `GET /api/auth/sessions` (200):

```json
[
  {
    "id": "8f1c...-...-...",
    "user_agent": "Mozilla/5.0 ...",
    "ip": "10.0.0.7",
    "created_at": "2026-09-11T13:22:01Z",
    "last_seen_at": "2026-09-11T13:45:18Z",
    "current": true
  },
  {
    "id": "7a92...-...-...",
    "user_agent": "curl/8.4.0",
    "ip": "192.168.1.42",
    "created_at": "2026-09-10T09:01:55Z",
    "last_seen_at": null,
    "current": false
  }
]
```

`{id}` no path é UUID. UUID malformado → 404.

---

## 5. Estratégia de teste

### Backend (Go)

| Camada | Tipo | ACs | Comando |
|---|---|---|---|
| Migration `0031_sessions` | integration | schema + down reverso limpo + RLS não-aplicada | `go test -tags=integration ./internal/db` |
| `SessionRepository` (8 métodos) | integration | Create/GetByID/ListForUser/GetByIDAndUser/Revoke/TouchLastSeen(throttle)/RevokeAllForUser | mesmo |
| `RequireAuth` (warm path com sid) | integration | 401 sem sid, 401 com sid revogado, 401 com sid inexistente, 200 com sid válido + TouchLastSeen throttled | mesmo |
| `issueSessionForUser` (helper) | integration | cria row, retorna sid, sid parseado do JWT bate com row.id | mesmo |
| `SwitchTenant` (mesmo sid) | integration | 2 sessões no user depois de 1 SwitchTenant (não 2); mesmo sid no JWT novo | mesmo |
| `Logout` | integration | chama Revoke; cópia do token pré-logout retorna 401 | mesmo |
| `SessionsHandler.List` | integration | 200 com lista; exclui revoked_at não-nulo; exclui > 24h; marca exatamente 1 como current | mesmo |
| `SessionsHandler.Revoke` | integration | 200/404/409 conforme matriz; revogação torna próxima request 401 | mesmo |
| `UpdateRole`/`Delete` (per-session) | integration | `RevokeAllForUser` chamado no lugar de `RevokeSessions` global; mesmo efeito observável | mesmo |
| JWT sem `sid` pré-deploy | integration | rejeitado por `VerifySessionClaims` | mesmo |

**Discrimination sensor**: para cada método do repo + cada handler novo, injetar mutação (ex.: `Revoke` deixa de checar `revoked_at IS NULL` na WHERE ⇒ "replay" do revoke testa se o método é idempotente) e confirmar que algum teste mata.

**Cookie/browser**: zero — todos os testes batem em `httptest.NewRecorder()` direto, lendo `Set-Cookie` pelo header.

### Frontend (Vitest + Testing Library + MSW)

| Camada | Tipo | ACs | Comando |
|---|---|---|---|
| Hook `useSessions` (200) | unit (MSW) | shape bate, `current: true` na row do próprio sid | `cd web && npm run test` |
| Hook `useSessions` (401) | unit (MSW) | erro tratado, sem crash | mesmo |
| Hook `revoke.mutate` | unit (MSW) | DELETE 200 → invalida query → lista refetched; DELETE 409 → toast "não pode revogar sessão atual" | mesmo |
| Página `SessionsSection` | component | empty state, current marcada, outras revogáveis, optimistic update | mesmo |
| i18n | smoke | 2 strings em pt-BR + en | mesmo |

---

## 6. Rollout

1. Migration `0031` roda no `db.MigrateUp` antes do deploy do binário (segue convenção de "todas migrations antes do serve" já em uso).
2. Deploy do binário quebra sessões existentes (decisão #4 do context) — usuários re-logam, e o primeiro login de cada um cria row na `sessions`.
3. Sem migração de dado (não há "sessões pré-existentes" pra migrar — JWTs antigos somem sozinhos depois de `exp = 24h`).
4. Frontend deploya junto — página `Sessões ativas` em "Meu Perfil" aparece imediatamente, sem flag.

---

## 7. Riscos e mitigações

| Risco | Mitigação |
|---|---|
| Quebra de sessão em deploy incomoda usuário | Aceito (decision #4); mesmo padrão de `auth-2fa-totp`; release notes avisam |
| `RequireAuth` ganha 1 query por request | 1 PK lookup por sessão autenticada = ~ms; benchmark aceitável |
| `Logout` falha de DB mas cookie já foi limpo | Fail-open com log warning; documentado |
| Tabela `sessions` cresce sem bound | Sem cleanup job (decision #9); aceito pela ordem de grandeza esperada |
| Cookie pré-deploy vs token sem `sid` | Defense in depth: `VerifySessionClaims` rejeita qualquer JWT sem `sid` parseável, mesmo se alguém tentar injetar |
| `SwitchTenant` falha no meio (cria row mas JWT não emitido) | `sessID, _ := Create(...)` antes do token — se `IssueSessionWithTenant` falhar, log + 500; row órfã fica na tabela, vira invisível depois de 24h |
| `ON DELETE CASCADE` de `users` apagar sessões do user removido | Consistente com remoção de user (sem sessões ativas faz sentido) |

---

## 8. Ordem de execução (preview; será formalizada em `tasks.md`)

```
T1   migration 0031 + repo vazio (Create/GetByID)
T2   SessionRepository: 8 métodos + integration tests
T3   auth/session.go: sid claim + VerifySessionClaims rejeita sem sid
T4   middleware.RequireAuth: nova assinatura + lookup + touch
T5   authHandler.captureSessionContext + issueSessionForUser (cria row + emite JWT)
T6   SwitchTenant reusa sid (mesmo sid, novo tid)
T7   Logout revoga row + ainda limpa cookie
T8   admins.UpdateRole/Delete migram pra RevokeAllForUser
T9   sessionsHandler.List + Revoke + 2 endpoints novos
T10  routes.go: wiring completo
T11  frontend types + MSW handlers + mockData
T12  frontend hook useSessions + revoke mutation
T13  frontend página SessionsSection + i18n
T14  frontend tests (hook, página, i18n)
T15  Verifier independente (especificação + sensor)
```

Tasks T1–T10 são backend, T11–T14 são frontend, T15 fecha com discrimination sensor. Tudo sequencial por dependência de wiring (T5 depende de T3+T4, T10 depende de T9, etc.).

---
