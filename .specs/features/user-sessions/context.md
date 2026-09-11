# Per-Device User Sessions Context

**Gathered:** 2026-09-11
**Spec:** `.specs/features/user-sessions/spec.md`
**Status:** Ready for design

---

## Feature Boundary

Substituir a revogação global de sessões (`User.SessionsRevokedAt`, herdada de `admins`/0009 e mantida em `users`/0024) por sessões reais por device. Cada sessão emitida passa a ser uma row na tabela `sessions`, referenciada por um novo claim `sid` no JWT, listável e revogável individualmente pelo próprio dono da conta. Fecha o gap da tela "Meu Perfil → Sessões ativas" do redesenho de layout (handoff-new-layout/Meu Perfil.dc.html) e o gap correlato de "Logout só limpa cookie, não invalida o token no servidor".

---

## Implementation Decisions

Decisões registradas **além** das que já estão em `spec.md` (Assumptions & Open Questions — todas confirmadas y). Estas foram levantadas durante o recon do código atual e fixadas no Design phase; nenhuma reabre a spec.

### Revogação global: híbrido, não substituição total

`User.SessionsRevokedAt` continua existindo e continua sendo gravado por dois caminhos: `AuthHandler.ChangePassword` (`auth_handler.go:573`) e `PasswordResetHandler.Confirm` (`password_reset_handler.go:253`). Esses dois são **eventos do próprio usuário** (mudança/reset de credencial) — sentido de revogar tudo continua válido.

`AdminsHandler.UpdateRole` (`admins.go:571`) e `AdminsHandler.Delete` (`admins.go:619`) migram de `RevokeSessions` (global) para `SessionRepository.RevokeAllForUser(userID)` (per-session, equivalente em blast radius mas granular no log/middleware). Razão: hoje rebaixar um `operator` num único tenant invalida sessões em **todos** os outros tenants do mesmo usuário (over-broad, capturado durante o recon). Com per-session, o admin-event continua derrubando tudo (mesmo efeito observável hoje) mas a tabela `sessions` registra quem/quando; o JWT emitido depois continua válido até `exp`/`revoked_at`, e o middleware novo (RequireAuth checa `revoked_at`) garante o 401 imediato.

Consequência mecânica: `user_repository.RevokeSessions` (`user_repository.go:156-158`) **fica** — só perde 2 dos 4 callers. Tabela `users` mantém `sessions_revoked_at`. Nada de migration reversa.

### JWTs emitidos antes do deploy são invalidados

`sessionClaims` ganha `sid` (UUID da row `sessions`) como claim obrigatório. `VerifySessionClaims` (`internal/auth/session.go:98-123`) passa a rejeitar tokens sem `sid` parseável com 401, da mesma forma que já rejeita tokens com `audience` não-vazia. Deploy quebra sessões em curso — todos os usuários logados são forçados a re-logar.

Razão: alternativa (aceitar tokens sem `sid` durante janela de migração) exigiria dois caminhos de validação por request no hot path do `RequireAuth` por um período indefinido, com risco de ficar pra sempre. Mesmo approach já adotado por `auth-2fa-totp` (mudou o formato do JWT pós-2FA e quebrou sessões existentes sem fallback). Custo aceito: 1 re-login por usuário no upgrade. Mitigação: a janela é controlável (cutover coordenado com release), e o login é idempotente.

### Captura de User-Agent e IP: helper único, lido no `authHandler`

`AuthHandler` ganha um helper privado `captureSessionContext(r *http.Request) (userAgent, ip string)` que:
- Lê `User-Agent` via `r.UserAgent()` (stdlib), retorna string vazia se ausente (tratado como `NULL` no banco, conforme edge case da spec)
- Lê IP via a mesma `clientIP()` que `internal/ratelimit/ip_limiter.go:126-136` já usa — só `r.RemoteAddr`, nunca `X-Forwarded-For`/`X-Real-IP` (regra do AGENTS.md §4 já estabelecida)

Esse helper é invocado uma vez no início dos 4 call sites de emissão (`Login`, `SwitchTenant`, `AcceptInvite`, `Bootstrap.Create`) e os valores passados pra `issueSessionForUser(...)` que cria a row e emite o token num único lugar. Evita drift entre call sites.

### `RequireAuth` ganha `SessionRepository` na assinatura

Hoje: `RequireAuth(secret string, users UserGetter)` em `internal/api/middleware.go:87`. Chamada em `internal/cli/routes.go:95`.

Nova: `RequireAuth(secret string, users UserGetter, sessions SessionGetter)` (interface mínima — só `GetByID(ctx, sid) (Session, error)`). Toda chamada de wiring em `routes.go` atualiza (uma só). Nenhum outro middleware consome `RequireAuth`.

Warm path ganha 1 lookup (PK do `sessions`) por request — mesma ordem de grandeza do `GetByID(user)` que já roda. Throttle de `last_seen_at` (5min) mantido como decidido na spec.

### `Logout` lê o `sid` do context e revoga a row

Hoje: `internal/api/auth_handler.go:777-780` faz só `cookie.MaxAge = -1` e responde 200. Token em si continua válido até `exp` (gap que SESS-11 fecha).

Novo: o handler lê `sid` que `RequireAuth` deixou no `context.Context` (mesmo padrão de `user`/`activeTenant` hoje em `middleware.go:128-130`), chama `SessionRepository.Revoke(ctx, sid)`, depois expira o cookie. Custo: 1 write por logout. Já está atrás de `RequireAuth`, então `sid` está validado.

### `SwitchTenant` mantém o mesmo `sid`, só troca `tid` no token

Hoje: `auth_handler.go:801-844` reemite token novo (`IssueSessionWithTenant` chamado de novo). Com session row, o **mesmo `sid`** é reaproveitado — não cria row nova. O JWT muda (`tid` novo, `iat` novo, mesmo `sid`, mesmo TTL).

Confirma com o spec (SESS-02 / AC2). `SessionsRevokedAt` continua sendo checado pelo `RequireAuth` em qualquer token reemitido — rebaixamento de role / mudança de senha / reset entre o token antigo e a reemissão continua derrubando.

### TTL único 24h

`SessionTTL = 24h` (`internal/auth/session.go:16`) rege: JWT `exp`, cookie `MaxAge`, e janela de exclusão da listagem (`created_at < now - 24h` ⇒ não aparece em `GET /api/auth/sessions`). Nenhuma mudança nesses 3 usos — alinhamento mecânico no design.

### Race condition revoke × in-flight

`RequireAuth` faz lookup + check em duas operações separadas (JWT validation → `GetByID(user)` → `GetByID(sessions)` → checa `revoked_at`). DELETE chega entre o check e a próxima request ⇒ trivialmente 401 na próxima request (mesmo modelo do `SessionsRevokedAt` atual). Sem locking adicional. Documentar, não tratar.

### Cleanup de rows expiradas

Decidido: **sem job**. Spec SESS-Edge case 4 já estabelece "exclude rows > 24h from list". A tabela cresce ~24h × N_users × N_devices — em instalação self-hosted típica (1 admin, 1-3 devices) é ruído; em SaaS multi-tenant vale reavaliar como hotfix separado quando o volume justificar, mas adicionar job preventivo agora é complexidade sem problema correspondente (não há vazamento de dados, só linhas que somem da listagem sozinhas depois de 24h).

Se vier a ser problema, o padrão existente é `StatusIntervalRepository.DeleteClosedBefore` (já em uso pelo poller) — cópia direta.

---

## Agent's Discretion

Decisões de implementação triviais que não precisam de confirmação (registradas pra evitar perguntas no reviewer):

- Tipo exato da coluna `id` em `sessions`: `UUID PRIMARY KEY DEFAULT gen_random_uuid()` (Postgres nativo, sem dependência nova).
- Indexes: PK em `id` (já cobre lookup do `RequireAuth`), index composto `(user_id, created_at DESC)` pra `GET /api/auth/sessions`, e index parcial `(user_id) WHERE revoked_at IS NULL` se o planner preferir (validar com EXPLAIN na primeira integration test, não pré-otimizar).
- RLS: `sessions` **não** precisa de RLS — o escopo é sempre `current_user_id` (vinda do `RequireAuth`/JWT, não do request body), e o handler checa ownership antes de qualquer SELECT/DELETE. Mesmo padrão de `tenant_memberships` (sem policy RLS, acesso por `app.user_id`).
- Posição da migração: `0031_sessions.up.sql` (depois de `0030_two_factor_auth`).

---

## Declined / Undiscussed Gray Areas → Assumptions

- **Geolocalização de IP** — assumido fora desta spec (out-of-scope explícito no spec.md, sem serviço contratado).
- **Parsing de User-Agent** — assumido fora (out-of-scope explícito no spec.md).
- **Limite de devices concorrentes** — assumido fora (out-of-scope explícito; nenhum product signal).
- **Admin vê sessões de outros usuários** — assumido fora (out-of-scope explícito; tela de admin sobre outro user não existe no handoff).
- **Renovação automática de TTL em cada request** — assumido **não**. Cada request mantém o TTL original do `iat`. Renovação implicaria tracking de "rolling session" e quebraria a invariante "um row por sessão enquanto válida".

---

## Specific References

- `handoff-new-layout/Meu Perfil.dc.html` — tela "Sessões ativas" no redesenho de layout.
- `internal/auth/session.go` — JWT atual, claim `tid`, `VerifySessionClaims`.
- `internal/api/middleware.go:87` — assinatura atual de `RequireAuth`.
- `internal/api/auth_handler.go:336-382` — `issueSessionForUser` (helper compartilhado por Login e VerifyTwoFactor).
- `internal/api/auth_handler.go:801-844` — `SwitchTenant` (sem row de sessão hoje).
- `internal/api/admins.go:571,619` — UpdateRole/Delete, `RevokeSessions` global (migram pra per-session).
- `internal/api/auth_handler.go:573` — ChangePassword, `RevokeSessions` global (fica).
- `internal/api/password_reset_handler.go:253` — PasswordReset.Confirm, `RevokeSessions` global (fica).
- `internal/auth/two_factor.go:15-21,58-60,90-109` — challenge token (mesmo signing primitive, audience distinta; preservado).
- `internal/db/migrations/0024_multi_tenancy_core.up.sql:34` — coluna `sessions_revoked_at` em `users` (fica).
- `internal/db/migrations/0030_two_factor_auth.up.sql` — última migration; nova será `0031_sessions`.
- `internal/ratelimit/ip_limiter.go:126-136` — `clientIP()` reusado.
- `.specs/features/auth-2fa-totp/spec.md` — precedente pra mudança de formato JWT quebrando sessões existentes.
- `.specs/features/profile-self-service/spec.md` — precedente pra "self-only, anyRole" em endpoint de conta pessoal.

---

## Deferred Ideas

- **Job de cleanup de rows expiradas** — se volume justificar; padrão `DeleteClosedBefore` já existe em `StatusIntervalRepository`.
- **TTL configurável por deployment** — hoje hardcoded em `SessionTTL`; mover pra config fica pra outra spec (decisão de infra, não de feature).
- **TTL rolling** (renovar `exp` em cada request ativa) — quebraria o modelo "um row por sessão enquanto válida"; rejeitado.
- **Notificação por email quando nova sessão é aberta em device novo** — out-of-scope do mock; vira feature de `notification-preferences` quando o disparo real de email entrar.
