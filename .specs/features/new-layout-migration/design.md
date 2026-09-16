# New Layout Migration — Fundação (Design System + App Shell) Design

**Spec**: `.specs/features/new-layout-migration/spec.md`
**Status**: Draft

---

## Architecture Overview

Duas camadas independentes, ambas puramente client-side + 1 gap pequeno de backend:

1. **Tokens**: `web/src/styles/tokens.css` reescrito — mesmos nomes de token que os componentes `ui/` já consomem (`bg`, `surface`, `text`, `divider`, `accent`, `critical`, `success`, `warning`, `neutral-*`), mas com os valores do handoff, definidos duas vezes (`:root` = light, `[data-theme="dark"]` = dark overrides). Tokens novos, exclusivos do shell (`sidebar-bg`, `sidebar-hover-bg`, `card-header-bg`, `text-muted`, `topbar-icon`), somem à parte. Um script inline em `index.html` aplica `data-theme` antes do React montar (evita flash).
2. **Shell**: `AuthenticatedLayout` (`App.tsx`) troca `<Sidebar/>` fixa por `<AppShell>` novo, que compõe `Sidebar` (redesenhada) + `Topbar` + área de conteúdo, mantendo `PollerBanner`/`Outlet` como hoje.
3. **Backend**: `AuthHandler.Me`/`Login`/`SwitchTenant`/`AcceptInvite` (todo handler que monta `meResponse`) passa a enriquecer cada `meMembership` com `name`/`plan_tier`, via uma nova query com `JOIN tenants` em `TenantMembershipRepository`.

```mermaid
graph TD
    A[index.html inline script] -->|seta data-theme| B[document.documentElement]
    B --> C[tokens.css: :root / [data-theme=dark]]
    C --> D[Tailwind @theme vars]
    D --> E[Componentes ui/ existentes - repintados]
    D --> F[AppShell novo]
    F --> G[Sidebar]
    F --> H[Topbar]
    G --> I[useThemeToggle hook - localStorage]
    G --> J[TenantSwitcher popover]
    J --> K[GET /api/auth/me - memberships com name/plan_tier]
    H --> L[AvatarMenu]
    H --> I
```

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
| --- | --- | --- |
| `Dialog` | `web/src/components/ui/Dialog.tsx` | Reusado tal como está pelo modal de confirmação de logout (já usado por `Sidebar.tsx` atual) — nenhuma mudança de API. |
| `Button` | `web/src/components/ui/Button.tsx` | Reusado nos botões do modal de logout e do pin toggle. |
| `useAuth` (`admin`, `logout`, `switchTenant`, `hasRole`) | `web/src/auth/AuthProvider.tsx` | Fonte de identidade/memberships/role para `Sidebar`/`Topbar`/`TenantSwitcher` — nenhuma mudança de contrato, só consumo do campo novo `name`/`plan_tier` em `memberships`. |
| `useBrandLogoUrl` | `web/src/lib/branding.ts` | Reusado como está para o logo no topo da sidebar. |
| `PollerBanner` | `web/src/features/poller/PollerBanner.tsx` | Mantido como slot fixo entre o shell e o `<Outlet/>`, sem mudança. |
| `apiClient`/`ApiError` | `web/src/lib/apiClient.ts` | Nenhuma mudança — `meResponse` cresce de forma aditiva (novos campos opcionais), o parsing existente continua válido. |
| i18n (`react-i18next`, `web/src/lib/i18n.ts`) | Chaves novas em `sidebar.*`/adicionar `topbar.*` seguindo o padrão `pt`/`en` já existente. | |

### Integration Points

| System | Integration Method |
| --- | --- |
| `GET /api/auth/me`, `Login`, `SwitchTenant`, `AcceptInvite`, `BootstrapHandler.Create` (todo lugar que monta `meResponse`/`loginResponse` com `Memberships`) | Todos os handlers que hoje chamam `memberships.ListForUser` e mapeiam pra `meMembership`/`loginResponse`'s membership list passam a receber `name`/`plan` já resolvidos pela query nova — mudança centralizada no repositório, não em cada handler. |
| `tenants` (Postgres) | `TenantMembershipRepository.ListForUser` ganha `JOIN tenants t ON t.id = tm.tenant_id`, sob a mesma transação/RLS já em vigor (nenhuma policy nova — a linha do tenant já é legível pela sessão, já que é o próprio tenant da membership). |
| `localStorage` | Duas chaves novas: `vane:theme` (`"light" \| "dark"`), `vane:sidebar-pinned` (`"true" \| "false"`). Lidas de forma síncrona no script inline de `index.html` e no `useThemeToggle`/`useSidebarPin` hooks. |

---

## Components

### `useThemeToggle` (hook)

- **Purpose**: Estado do tema atual + função de troca, sincronizado com `data-theme` no `<html>` e `localStorage`.
- **Location**: `web/src/lib/useThemeToggle.ts`
- **Interfaces**:
  - `useThemeToggle(): { theme: "light" | "dark"; toggleTheme(): void }`
- **Dependencies**: `document.documentElement`, `window.localStorage` (com fallback silencioso se indisponível — Edge Case da spec).
- **Reuses**: nenhum código existente — hook novo e pequeno.

### Inline theme-boot script (`web/index.html`)

- **Purpose**: Ler `localStorage["vane:theme"]` e setar `document.documentElement.dataset.theme` antes do React montar, evitando flash do tema errado (SHELL-07).
- **Location**: `web/index.html` (`<script>` inline no `<head>`, antes do `<div id="root">`/bundle).
- **Interfaces**: nenhuma (script imperativo, sem export).
- **Dependencies**: `localStorage`. Falha silenciosa (try/catch) → tema claro por padrão (SHELL-05, Edge Case de `localStorage` indisponível).
- **Reuses**: nenhum.

### `AppShell`

- **Purpose**: Compõe `Sidebar` + `Topbar` + área de conteúdo com scroll/padding/max-width do handoff; substitui o corpo de `AuthenticatedLayout`.
- **Location**: `web/src/layout/AppShell.tsx`
- **Interfaces**:
  - `AppShell({ children }: { children: ReactNode }): JSX.Element`
- **Dependencies**: `Sidebar`, `Topbar`, `useLocation` (pra derivar o título de página do topbar a partir da rota atual — reaproveita o mesmo mapeamento de rota→label que `Sidebar.tsx` atual já mantém implicitamente via `NavLink`/`t()`).
- **Reuses**: `PollerBanner`/`Outlet` continuam exatamente onde `AuthenticatedLayout` já os posiciona hoje.

### `Sidebar` (reescrita)

- **Purpose**: Sidebar colapsável (72/240px), 3 grupos de nav, pin, tenant switcher, mesma gate de papel de hoje.
- **Location**: `web/src/layout/Sidebar.tsx` (reescrito no lugar — mesmo arquivo, para não deixar 2 sidebars no repo)
- **Interfaces**:
  - `Sidebar(): JSX.Element`
- **Dependencies**: `useThemeToggle`'s irmão de pin (`useSidebarPin`, mesmo padrão de hook), `useAuth`, `TenantSwitcher`.
- **Reuses**: `Dialog`/`Button` (modal de logout, já existente), `useBrandLogoUrl`, `hasRole(["owner"])` (gate hoje aplicado a Admins/Settings — mesma condição, aplicada aos itens equivalentes do novo agrupamento).

### `useSidebarPin` (hook)

- **Purpose**: Estado do pin ("Fixar menu") + persistência.
- **Location**: `web/src/lib/useSidebarPin.ts`
- **Interfaces**:
  - `useSidebarPin(): { pinned: boolean; togglePinned(): void }`
- **Dependencies**: `localStorage` (mesma chave/fallback do `useThemeToggle`).
- **Reuses**: nenhum.

### `Topbar`

- **Purpose**: Título da página + toggle de tema + sino estático + `AvatarMenu`.
- **Location**: `web/src/layout/Topbar.tsx`
- **Interfaces**:
  - `Topbar({ title }: { title: string }): JSX.Element`
- **Dependencies**: `useThemeToggle`, `AvatarMenu`.
- **Reuses**: nenhum componente existente diretamente (elemento novo no shell), mas usa os mesmos tokens/ícone-pattern (`stroke="currentColor"` SVGs inline) já usados em `Sidebar.tsx` atual.

### `AvatarMenu`

- **Purpose**: Popover com nome/email, "Configurações" (gate owner), "Sair" (abre o mesmo modal de confirmação).
- **Location**: `web/src/layout/AvatarMenu.tsx`
- **Interfaces**:
  - `AvatarMenu(): JSX.Element`
- **Dependencies**: `useAuth` (`admin`, `logout`, `hasRole`), `Dialog`, `Button`.
- **Reuses**: mesmo modal de confirmação de logout que `Sidebar.tsx` atual já implementa (extraído/reaproveitado, não duplicado — ver Risks & Concerns).

### `TenantSwitcher`

- **Purpose**: Popover do seletor de tenant na sidebar (distinto do `TenantSelector.tsx` de tela cheia, que continua existindo para o fluxo "sem tenant ativo ainda").
- **Location**: `web/src/layout/TenantSwitcher.tsx`
- **Interfaces**:
  - `TenantSwitcher(): JSX.Element | null` (retorna `null` quando `memberships.length <= 1`, SHELL-10)
- **Dependencies**: `useAuth` (`admin.memberships`, `switchTenant`).
- **Reuses**: `switchTenant` (já existe em `AuthProvider`), mesmo tipo `TenantMembership` que `TenantSelector.tsx` já consome — ambos os componentes passam a exibir `name`/`plan_tier` reais assim que o backend devolver.

---

## Data Models

### `TenantMembership` (frontend, `web/src/types/api.ts`) — campos adicionados

```typescript
export interface TenantMembership {
  tenant_id: string;
  role: Role;
  name: string;        // novo — nome do tenant, para exibição (SHELL-20)
  plan_tier: string;   // novo — plano do tenant, "" se não definido (SHELL-21)
}
```

**Relationships**: usado por `AuthenticatedAdmin.memberships` (`AuthProvider.tsx`), consumido por `TenantSelector.tsx` (existente) e `TenantSwitcher.tsx` (novo).

### `meMembership` (backend, `internal/api/auth_handler.go`) — campos adicionados

```go
type meMembership struct {
	TenantID string `json:"tenant_id"`
	Role     string `json:"role"`
	Name     string `json:"name"`
	PlanTier string `json:"plan_tier"`
}
```

**Relationships**: montado a partir do retorno novo de `TenantMembershipRepository.ListForUser` (ver abaixo), usado por todo handler que hoje monta `meResponse`/`loginResponse` a partir de `memberships.ListForUser`.

### `TenantMembership` (backend, `internal/db/tenant_membership_repository.go`) — campos adicionados

```go
type TenantMembership struct {
	UserID    string
	TenantID  string
	Role      string
	CreatedAt time.Time
	Name      string // novo - tenants.name, via JOIN
	Plan      string // novo - tenants.plan, via JOIN
}
```

**Relationships**: `ListForUser`'s query passa a ser `SELECT tm.user_id, tm.tenant_id, tm.role, tm.created_at, t.name, t.plan FROM tenant_memberships tm JOIN tenants t ON t.id = tm.tenant_id WHERE tm.user_id = $1 ORDER BY tm.created_at ASC` — mudança isolada nesse método; `ListForTenant` (outro método do mesmo repositório, usado por `notification-preferences`/admins) não é tocado.

---

## Error Handling Strategy

| Error Scenario | Handling | User Impact |
| --- | --- | --- |
| `localStorage` indisponível (modo privado restritivo, quota excedida) | `useThemeToggle`/`useSidebarPin` envolvem leitura/escrita em `try/catch`, caem no default (`light`/não-fixado) sem lançar | Nenhum — app funciona normalmente, só sem persistência entre reloads |
| Tenant sem `name` (linha legada, edge case) | `TenantSwitcher`/`TenantSelector` usam `membership.name || membership.tenant_id` como fallback de exibição | Usuário vê o UUID em vez do nome bonito, mas nada quebra |
| `plan_tier` vazio (`""`, tenant sem plano definido) | Frontend trata `""` como "Free" (pill cinza), igual ao handoff — decisão de apresentação, não de backend (spec SHELL-21) | Badge "Free" aparece mesmo sem plano setado — comportamento intencional |
| `GET /api/auth/me` falha (rede, 5xx) | Comportamento já existente de `AuthProvider` (não alterado por esta feature) — shell não introduz novo tratamento de erro aqui | Inalterado |

---

## Risks & Concerns

| Concern | Location (file:line) | Impact | Mitigation |
| --- | --- | --- | --- |
| Repintar os tokens globais (`bg`, `surface`, `text`, `divider`, `accent`, `neutral-*`) muda a aparência de toda tela existente imediatamente, não só do shell | `web/src/styles/tokens.css` (arquivo inteiro) | Telas ainda não migradas (todas exceto o shell) ficam com paleta/tema novos mas layout/estrutura antigos — visual "remendado" até cada tela ser portada | Aceito explicitamente pelo usuário (sem rollout incremental em produção) — nenhuma mitigação de código necessária, só reforçar no `Success Criteria` que isso é esperado, não regressão |
| Ramp `accent-2` (roxo secundário usado em gráficos/sparklines) não tem equivalente definido no handoff | `web/src/styles/tokens.css:41-50` | Cor pode destoar da nova paleta em qualquer tela que a use (ex. sparklines) até essa tela ser migrada | Fora de escopo desta fundação — mantido com o valor antigo (mesma lógica do ponto acima); revisar quando a tela que o usa for portada |
| `--radius-sm/md/lg` e `--shadow-sm/md/lg` mudam de significado (valores) sem mudar de nome — todo componente `ui/` que usa `rounded-sm`/`shadow-md` etc. herda o novo valor sem revisão individual | `web/src/components/ui/*.tsx` (uso via classes Tailwind) | Cantos/sombras de componentes existentes mudam de tamanho visualmente | Mesma aceitação do primeiro item — efeito colateral esperado da decisão de não fazer rollout incremental |
| `Sidebar.tsx` atual embute o modal de confirmação de logout inline; `AvatarMenu` novo precisa do mesmo modal | `web/src/layout/Sidebar.tsx:199-223` (implementação atual) | Duplicar o JSX do modal em 2 lugares (Sidebar + AvatarMenu) é o caminho de menor risco, mas gera repetição | Extrair um componente pequeno `LogoutConfirmDialog` (novo, `web/src/layout/LogoutConfirmDialog.tsx`) reusado por ambos — evita duplicação sem introduzir uma abstração maior que o necessário |
| Nenhuma tela hoje calcula "título da página atual" (a `Sidebar.tsx` atual não precisa disso) | — | `Topbar` precisa de um título por rota; inventar esse mapeamento é responsabilidade nova | Mapa simples `pathname → i18n key` colocado em `AppShell.tsx` (mesmo padrão de `domainsActive` que `Sidebar.tsx` atual já usa para destacar nav) — não introduz roteamento novo, só um `switch`/lookup |

> Nenhum risco de segurança, RLS ou N+1 identificado: o `JOIN tenants` em `ListForUser` é 1:1 por membership (sem N+1), sob a mesma transação/RLS já vigente (`AD-022`); a tabela `tenants` já é legível para o próprio tenant da membership, nenhuma policy nova necessária.

---

## Tech Decisions (only non-obvious ones)

| Decision | Choice | Rationale |
| --- | --- | --- |
| Mecanismo de tema | CSS custom properties + atributo `[data-theme]` no `<html>`, tokens Tailwind (`@theme`) apontando pra `var(--color-x)` | Aprovado com o usuário (Approach A) — repinta todo componente existente sem editá-los, e o toggle é O(1) (troca de atributo) |
| Composição do shell | Substituir `AuthenticatedLayout` em `App.tsx` no lugar, sem módulo/provider novo | Aprovado com o usuário (Approach A) — único ponto de composição já existe, uma segunda camada de abstração não tem uso hoje |
| Persistência de tema/pin | `localStorage`, 2 chaves simples (`vane:theme`, `vane:sidebar-pinned`) | Client-only por decisão explícita da spec (Out of Scope: persistência server-side fica pra depois) |
| Enriquecimento de `memberships` | Mudar a query de `ListForUser` (`JOIN tenants`), não criar método novo | Único chamador de `ListForUser` hoje é o conjunto de handlers que monta `meResponse`/`loginResponse` — nenhum consumidor depende do formato antigo sem os campos novos (aditivo, backwards-compatible no JSON) |
| Ícone do sino de notificação | Estático, sem `<Link>`, sem contagem — só o SVG do handoff | Decisão do usuário (ver Assumptions do spec) — evita rota/API inexistente |

> **Nota de convenção de projeto:** a escolha de tema via CSS custom properties + `[data-theme]` (em vez de `dark:` do Tailwind por classe) é registrada como `AD-027` em `.specs/STATE.md`, já que qualquer tela futura que reintroduza dark-mode-por-classe estaria contradizendo essa decisão sem uma razão nova.

---

## Tips

(seção de referência do template — não aplicável ao conteúdo final)
