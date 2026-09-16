# Auth Pages Redesign Specification

## Problem Statement

As telas públicas de autenticação (`LoginPage`, `BootstrapPage`, `SignupPage`, `VerifyEmailPage`, `PasswordResetRequestPage`, `PasswordResetConfirmPage`) hoje têm 6 implementações independentes do mesmo layout base (painel de marca à esquerda + card de formulário à direita), cada uma com seu próprio JSX duplicado do gradiente/blur decorativo. O mock (`handoff-new-layout/Login Bootstrap.dc.html`) define um visual novo e consistente para esse conjunto: painel esquerdo escuro com ilustração de rede de nós SVG animada + headline "Clareza total sobre a saúde da sua infraestrutura" + card de uptime flutuante, e card de formulário à direita com estilo consistente de inputs/botões. `gap-analysis.md` item 1 já confirmou que não há gap de backend aqui (login, bootstrap, signup, reset de senha, self-hosted vs SaaS já existem e funcionam) — o gap é inteiramente visual.

## Goals

- [ ] Extrair um componente de layout compartilhado (`AuthLayout`) com o painel esquerdo do mock (fundo escuro, SVG de rede animada, headline, card de uptime, logo), reutilizado pelas 6 páginas — elimina a duplicação atual do gradiente decorativo.
- [ ] Cada página bate visualmente com o estado correspondente do mock: bootstrap→`isBootstrap`, login→`isLogin`, signup→`isSignup`, verify-email→`isSignupVerify`, reset-password (pedido)→`isForgotPassword`, reset-password (enviado)→`isForgotPasswordSent`.
- [ ] Botões "Google"/"Microsoft" aparecem em Login e Signup (únicos estados do mock que os mostram), com estilo do mock, mas sem OAuth real — decorativos.
- [ ] Nenhuma mudança de contrato de API, validação ou fluxo de submit em nenhuma das 6 páginas — só o container visual e o texto/rótulos passam a bater com o mock.

## Out of Scope

| Feature | Reason |
| --- | --- |
| OAuth real (Google/Microsoft) | Backend não tem client_id/secret, rota ou handler de OAuth em lugar nenhum. Decisão do usuário (`AskUserQuestion`): manter os botões como decorativos (sem ação de login real), não implementar OAuth. |
| Badge/toggle visual "Self-hosted" no canto do formulário | O mock alterna esse badge por uma prop de instalação (`installMode`) que não existe como sinal de runtime no frontend real — nenhum endpoint expõe se a instância é self-hosted vs SaaS. Backend já trata os dois modos internamente (bootstrap/signup coexistem), mas não há flag para a UI decidir se mostra o badge. Fabricar essa distinção introduziria estado falso. Badge omitido. |
| `PasswordResetConfirmPage` (definir nova senha via token) | Não é um estado do mock (`view` só cobre bootstrap/login/signup/signup_verify/forgot_password/forgot_password_sent) — mas herda o mesmo `AuthLayout` compartilhado para consistência visual, sem novo desenho de conteúdo específico do mock. |
| Novo texto/copy do painel esquerdo por página | Mock usa o mesmo headline/uptime-card em todos os estados (é um painel de marca estático, não contextual) — todas as 6 páginas reusam o mesmo `AuthLayout` sem variar o conteúdo do lado esquerdo. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Botões OAuth decorativos | Renderizam com estilo do mock; `onClick` mostra toast "Em breve" (sonner, já usado no resto do app) em vez de navegar/chamar API. | Decisão explícita do usuário via `AskUserQuestion` — nunca renderizar ação real sobre backend inexistente, mas também não fabricar silêncio ao clique. | y |
| Badge "Self-hosted" | Omitido — nenhuma página exibe esse indicador. | Não existe sinal de runtime real para alimentá-lo (ver Out of Scope). | y |
| `PasswordResetConfirmPage` | Recebe o `AuthLayout` compartilhado (consistência visual), mas mantém seu conteúdo de formulário atual (token da URL, nova senha, confirmar) — não é um estado do mock. | Mock não modela essa etapa; melhor reaproveitar o layout novo do que deixar essa página com o visual antigo dessincronizado. | y |
| Ilustração SVG animada do painel esquerdo | Reimplementada como componente React (`AuthBrandPanel` ou dentro do próprio `AuthLayout`), com as mesmas animações CSS/SMIL do mock (círculos pulsantes) traduzidas para Tailwind/CSS-in-JS do padrão do projeto. | Mock é HTML/SVG estático de referência (`.dc.html`), não componente importável — precisa ser portado, não copiado literalmente. | y |
| Copy de cada estado (headline, subtítulo, labels) | Segue o mock literalmente, traduzido para as chaves i18n já existentes em cada página (`login.*`, `bootstrap.*`, etc.) — sem novas strings hardcoded. | Regra do projeto (AGENTS.md §5): toda string visível passa por `react-i18next`. | y |

**Open questions:** none — todas resolvidas ou registradas acima.

---

## User Stories

### P1: `AuthLayout` compartilhado (painel de marca) ⭐ MVP

**User Story**: Como usuário anônimo acessando qualquer tela de autenticação, quero ver o mesmo painel de marca consistente (rede de nós animada, headline, card de uptime), para que a experiência pareça parte do mesmo produto em todas as etapas (login, cadastro, recuperação de senha).

**Why P1**: É a peça reutilizada por todas as outras histórias — sem ela cada página continuaria duplicando o layout antigo.

**Acceptance Criteria**:

1. The system SHALL prover um componente `AuthLayout` com painel esquerdo (visível em `lg:` e acima, oculto em mobile) reproduzindo o visual do mock: fundo escuro com gradiente radial, SVG de rede de nós com animação de pulso, headline "Clareza total sobre a saúde da sua infraestrutura.", card flutuante de uptime ("Todos os sistemas operacionais" + "99.98% uptime · últimos 30 dias") e logo da marca (`useBrandLogoUrl`, com fallback para o logo padrão do Vane).
2. WHEN a viewport é mobile (abaixo do breakpoint `lg`) THEN o sistema SHALL ocultar o painel esquerdo e exibir apenas o logo compacto acima do formulário, igual ao comportamento atual de `LoginPage`.
3. Cada uma das 6 páginas (`LoginPage`, `BootstrapPage`, `SignupPage`, `VerifyEmailPage`, `PasswordResetRequestPage`, `PasswordResetConfirmPage`) SHALL usar `AuthLayout` como container, passando apenas seu conteúdo de formulário como children — nenhuma duplica o JSX do painel esquerdo.

**Independent Test**: abrir `/login`, `/bootstrap`, `/signup` lado a lado (viewport desktop) e confirmar visualmente que o painel esquerdo é idêntico nas três.

---

### P1: Login e Signup batem com o mock (incluindo OAuth decorativo) ⭐ MVP

**User Story**: Como usuário, quero que as telas de entrar e criar conta sigam exatamente o visual do mock (inputs com ícone, botões Google/Microsoft, divisor "ou continue com email"), para uma experiência visualmente polida.

**Why P1**: São as duas telas de maior tráfego (login diário, signup de novos tenants SaaS).

**Acceptance Criteria**:

1. `LoginPage` SHALL exibir, nesta ordem: título "Entrar" + subtítulo, botões "Google"/"Microsoft" lado a lado, divisor "ou continue com email", campos Email/Senha com ícone, link "Esqueceu a senha?", botão "Entrar", e (quando aplicável) o link "Criar conta" — mantendo o fluxo de submit/2FA/erro já existente inalterado.
2. `SignupPage` SHALL seguir a mesma estrutura do mock (OAuth + divisor + Nome/Email/Senha + termos + link "Já tem uma conta?"), mantendo seu fluxo de submit/validação/erro atual inalterado.
3. WHEN o usuário clica em "Google" ou "Microsoft" (em qualquer uma das duas telas) THEN o sistema SHALL exibir um toast informativo ("Em breve") e NÃO SHALL disparar nenhuma chamada de rede nem navegação.
4. The system SHALL manter todo texto visível nessas duas telas através de `react-i18next` (nenhuma string nova hardcoded).

**Independent Test**: abrir `/login`, clicar "Google", ver toast "Em breve" sem navegação; preencher email/senha reais e confirmar que o login continua funcionando normalmente.

---

### P2: Bootstrap, verificação de e-mail e recuperação de senha batem com o mock

**User Story**: Como owner configurando uma instância nova (ou recuperando acesso), quero que as telas de bootstrap, verificação de e-mail e reset de senha sigam o mesmo visual consistente das demais.

**Why P2**: Telas de menor frequência de uso, mas que não podem ficar visualmente destoantes do restante do fluxo de autenticação recém-redesenhado.

**Acceptance Criteria**:

1. `BootstrapPage` SHALL seguir a estrutura do mock (rótulo "Configuração inicial", campos de organização + administrador, botão "Criar administrador e continuar"), mantendo seu fluxo de submit/erro atual inalterado.
2. `VerifyEmailPage` SHALL seguir a estrutura do mock (ícone, "Verifique seu email", e-mail em destaque, botão "Reenviar email"), mantendo seu comportamento atual inalterado.
3. `PasswordResetRequestPage` SHALL seguir a estrutura do mock para os dois estados (formulário de e-mail → tela de confirmação de envio), mantendo seu fluxo atual inalterado.
4. `PasswordResetConfirmPage` SHALL usar o `AuthLayout` compartilhado, preservando seu conteúdo de formulário (token, nova senha, confirmar senha) sem alteração de comportamento.

**Independent Test**: percorrer bootstrap→sucesso, disparar reset de senha→ver tela de confirmação, abrir link de verificação de e-mail — todas com o novo painel esquerdo consistente.

---

## Edge Cases

- IF a marca do tenant tem logo customizado (`useBrandLogoUrl`) THEN o painel esquerdo SHALL exibir esse logo em vez do logo padrão do Vane, igual ao comportamento atual.
- IF o clique em "Google"/"Microsoft" ocorrer enquanto o formulário está em `submitting` THEN o sistema SHALL ainda assim mostrar o toast (botões OAuth não compartilham o estado de loading do submit do formulário, pois não disparam requisição alguma).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| AUTHPG-01 | P1: AuthLayout compartilhado | - | Implementing |
| AUTHPG-02 | P1: AuthLayout compartilhado | - | Implementing |
| AUTHPG-03 | P1: AuthLayout compartilhado | - | Implementing |
| AUTHPG-04 | P1: Login e Signup | - | Implementing |
| AUTHPG-05 | P1: Login e Signup | - | Implementing |
| AUTHPG-06 | P1: Login e Signup | - | Implementing |
| AUTHPG-07 | P1: Login e Signup | - | Implementing |
| AUTHPG-08 | P2: Bootstrap/verify/reset | - | Implementing |
| AUTHPG-09 | P2: Bootstrap/verify/reset | - | Implementing |
| AUTHPG-10 | P2: Bootstrap/verify/reset | - | Implementing |
| AUTHPG-11 | P2: Bootstrap/verify/reset | - | Implementing |

**ID format:** `AUTHPG-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 11 total, 11 mapped a tarefas (implícitas, escopo Medium), 0 unmapped

---

## Success Criteria

- [ ] As 6 páginas de autenticação batem visualmente com `handoff-new-layout/Login Bootstrap.dc.html` (menos badge self-hosted e OAuth real, explicitamente fora de escopo).
- [ ] Nenhuma mudança de contrato de API/validação/comportamento de submit em nenhuma das 6 páginas.
- [ ] `go build`/`go vet`/`gofmt` limpos (sem mudança de backend esperada); `tsc -b --noEmit` e suíte de frontend verdes.
