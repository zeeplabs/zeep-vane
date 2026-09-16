# Public Status Redesign Specification

## Problem Statement

`PublicStatusPage.tsx` já implementa toda a lógica/dados reais (overall status, incidente ativo, lista de serviços com barras horárias, seletor de período, histórico paginado, SEO title/description) mas seu visual não segue o handoff `handoff-new-layout/Status Page Publica.dc.html` — usa componentes genéricos (`Card`/`Tag`) com paddings/radii/tipografia diferentes dos valores exatos do mock. Pedido do Julio (2026-09-15): reskin pixel-fiel ao handoff, mais uma adição que o mock não tem: um alternador de tema (dark/light) que afeta **somente esta página**, com padrão light, independente do tema global do resto do app (que hoje é dark por padrão para o usuário logado, controlado por `useThemeToggle`/`vane:theme`).

## Goals

- [ ] Reskin de `PublicStatusPage.tsx` pixel-fiel ao mock: header (logo/nome da empresa + "Atualizado há X"), banda de status geral, card de incidente ativo + linha do tempo expansível, seção "Serviços" com seletor de período e lista de cards (dot+nome+uptime+badge+barras+labels), rodapé "Powered by Vane" — sem alterar nenhum dado/lógica/comportamento já coberto pelos testes existentes.
- [ ] Paleta: reaproveitar os tokens já existentes em `tokens.css` (`--color-bg`, `--color-surface`, `--color-text`, `--color-text-muted`, `--color-accent`, `--color-divider`, `--color-success/warning/critical`, `--color-neutral-600`) — decisão confirmada via `AskUserQuestion` (visual quase idêntico ao mock, sem paleta hex duplicada fora do sistema de design).
- [ ] Tipografia: manter `font-heading`/`font-body` (Manrope) já globais — não carregar Space Grotesk (mesmo precedente já aplicado em `auth-pages-redesign`'s `AuthLayout`).
- [ ] Novo alternador de tema **local a esta página**: estado próprio (`usePublicStatusTheme` ou equivalente), chave de `localStorage` própria (não `vane:theme`), aplicado via atributo `data-theme` no elemento raiz desta página (não em `document.documentElement`) — os seletores `[data-theme="dark"]` de `tokens.css` já cascateiam para qualquer elemento com o atributo, então isso reaproveita a paleta dark existente sem tocar no tema do resto do app.
- [ ] Padrão **light**, sempre — nunca lê `prefers-color-scheme` do SO nem o tema global do admin logado; primeira visita de qualquer usuário (autenticado ou anônimo) sempre começa light.
- [ ] Ícone de alternância (sol/lua, reaproveitando `MdOutlineWbSunny`/`MdOutlineNightlight` do `Topbar.tsx` para consistência visual) no cabeçalho, ao lado de "Atualizado há X minutos".

## Out of Scope

| Item | Motivo |
| --- | --- |
| Alterar dados/lógica de `usePublicStatusPage`, `publicStatus.ts`, `format.ts` | Reskin visual apenas — toda a suíte de testes existente (24 casos) cobre comportamento real e não pode quebrar. |
| Persistir a preferência de tema no backend/tenant | Mock não pede isso; é um alternador puramente client-side, visitante a visitante. |
| Aplicar esse alternador local em outras páginas | Pedido explícito é só para a status page pública. |
| Logo "vane" fixo do mock | O quadrado+"vane" do mock é o exemplo do próprio Vane usando seu produto como tenant demo — no dado real, esse slot já é dinâmico (`data.logo_url` / `data.company_name` da empresa dona da status page) e continua assim. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Paleta dark: tokens existentes vs. hex literais do mock | Tokens existentes (`tokens.css`) | Decisão explícita do usuário via `AskUserQuestion` — visual quase idêntico, sem duplicar fonte de verdade de cor. | y |
| Fonte do wordmark (mock usa Space Grotesk) | Manter `font-heading` (Manrope) | Mesmo precedente de `auth-pages-redesign`: não introduzir uma fonte nova só para uma tela. | y (precedente já aplicado nesta sessão) |
| Onde aplicar `data-theme` do alternador local | No elemento raiz de `PublicStatusPage`, nunca em `document.documentElement` | Isola o efeito a esta página; `tokens.css` já usa seletor de atributo genérico `[data-theme="dark"]`, então cascateia automaticamente sem mudar `tokens.css`. | y |
| Persistência do alternador | `localStorage` com chave própria (não `vane:theme`) | Mesmo padrão UX de `useThemeToggle` (lembrar escolha entre visitas), mas sem colidir com o tema do admin logado — a página pública pode ser vista sem sessão nenhuma. | y |
| Ícone/label do alternador | Reaproveitar `MdOutlineWbSunny`/`MdOutlineNightlight` (mesmos ícones do `Topbar`) + `aria-label` genérico já existente (`topbar.toggleTheme`, i18n) | Consistência visual com o resto do app; evita criar uma chave i18n nova para o mesmo conceito ("alternar tema"). | y |

**Open questions:** none — todas resolvidas acima.

---

## User Stories

### P1: Reskin pixel-fiel ao handoff ⭐ MVP

**User Story**: Como visitante de uma status page pública, quero ver o mesmo visual refinado do handoff (espaçamento, tipografia, cores, cards), para que a página transmita a mesma qualidade de produto do restante do app redesenhado.

**Acceptance Criteria**:

1. O cabeçalho SHALL exibir a logo/nome da empresa (dado real, inalterado) à esquerda e "Atualizado {relativo}" à direita, com o alternador de tema (AC da story P2) entre esses dois blocos ou imediatamente ao lado do texto de atualização.
2. A banda de status geral SHALL usar padding/radius/tipografia do mock (~18px/22px de padding, radius 12px, dot 10px, label 16px/700) com cor de fundo/dot conforme o pior status (`operational`/`degraded`/`outage`), via tokens (`--color-success`/`--color-warning`/`--color-critical`).
3. O card de incidente ativo SHALL manter todo o comportamento existente (expandir/ocultar linha do tempo, badge de status, tags de serviço afetado) com o visual do mock: borda/fundo tingidos de crítico, título 15px/700, badge 11.5px/700, link de alternância sem sublinhado.
4. A seção "Serviços" SHALL manter o `Seg` de período (24h/7d/30d/90d) e a lista de serviços com dot+nome+uptime+badge+barras+labels, com espaçamento/radius/tamanhos de fonte do mock (cards com borda+fundo diferenciados do bg da página, barras com 2px de gap e leve arredondamento).
5. O rodapé "Powered by Vane" SHALL permanecer centralizado, tom mudo, com a margem superior do mock.
6. Nenhum teste de `PublicStatusPage.test.tsx` já existente SHALL precisar mudar sua asserção de comportamento (apenas ajustes triviais de seletor DOM, se algum, são aceitáveis; nenhuma mudança de expectativa de dado/lógica).

**Independent Test**: renderizar `/status/:id` com uma status page de exemplo e comparar visualmente com o handoff; rodar a suíte existente e confirmar 24/24 verdes sem alterar asserções de dado.

---

### P2: Alternador de tema local, padrão light ⭐ MVP

**User Story**: Como visitante anônimo de uma status page pública, quero poder alternar entre claro e escuro só nesta página, começando sempre em claro, sem que isso afete (ou seja afetado por) o tema do painel administrativo.

**Acceptance Criteria**:

1. A página SHALL renderizar em tema light por padrão na primeira visita, independentemente do `prefers-color-scheme` do sistema operacional do visitante e independentemente do valor salvo em `vane:theme` (tema do app logado).
2. Um botão com ícone sol/lua SHALL alternar entre light e dark, atualizando visualmente toda a página imediatamente (cores via os tokens de `tokens.css`, aplicadas por `data-theme` no elemento raiz desta página).
3. A escolha SHALL persistir em `localStorage` sob uma chave própria desta feature (não `vane:theme`), sobrevivendo a reloads da mesma página.
4. Alternar o tema nesta página SHALL NOT alterar `document.documentElement`'s `data-theme` nem o valor de `vane:theme` — verificável abrindo a página em uma aba onde o admin está logado com tema dark: o resto do app permanece dark, só a status page pública muda.
5. IF `localStorage` está indisponível (modo privado, quota) THEN a página SHALL degradar para light em memória, sem erro visível (mesmo Edge Case já tratado por `useThemeToggle`).

**Independent Test**: abrir a status page pública sem nenhum dado salvo — deve renderizar light. Clicar no alternador — vira dark, textos/cores mudam. Recarregar a página — permanece dark (persistido). Verificar que `document.documentElement.dataset.theme` nunca é tocado.

---

## Edge Cases

- IF o SO do visitante está em dark mode THEN a página pública SHALL mesmo assim abrir light na primeira visita (AC P2-01) — não há leitura de `prefers-color-scheme`.
- IF o admin está com o app em tema dark (`vane:theme=dark`) E abre a própria status page pública em outra aba THEN essa aba SHALL abrir light por padrão (tema local, não herda o do app) — mudar um não muda o outro em nenhuma direção.
- IF a empresa não tem `logo_url` THEN o slot do cabeçalho SHALL continuar mostrando apenas o texto do `company_name`, sem tentar desenhar um ícone/quadrado decorativo (esse quadrado do mock é parte do wordmark "vane" de exemplo, não um placeholder genérico de logo).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| PUBSTATUS-01 | P1: Reskin pixel-fiel | - | Verified |
| PUBSTATUS-02 | P1: Reskin pixel-fiel | - | Verified |
| PUBSTATUS-03 | P1: Reskin pixel-fiel | - | Verified |
| PUBSTATUS-04 | P1: Reskin pixel-fiel | - | Verified |
| PUBSTATUS-05 | P1: Reskin pixel-fiel | - | Verified |
| PUBSTATUS-06 | P1: Reskin pixel-fiel | - | Verified |
| PUBSTATUS-07 | P2: Alternador de tema local | - | Verified |
| PUBSTATUS-08 | P2: Alternador de tema local | - | Verified |
| PUBSTATUS-09 | P2: Alternador de tema local | - | Verified |
| PUBSTATUS-10 | P2: Alternador de tema local | - | Verified |
| PUBSTATUS-11 | P2: Alternador de tema local | - | Verified |

**ID format:** `PUBSTATUS-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 11 total, 11 mapeados a tarefas (implícitas, escopo Medium), 0 unmapped

---

## Success Criteria

- [ ] Reskin visualmente fiel ao handoff (espaçamento/tipografia/cores via tokens), sem regressão em nenhum dos 24 testes existentes de `PublicStatusPage.test.tsx`.
- [ ] Novo alternador de tema local, testado (default light, toggle funcional, persistência, isolamento do tema global).
- [ ] `tsc -b --noEmit` e suíte de frontend verdes.
