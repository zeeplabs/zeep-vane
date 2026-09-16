# Briefing de Redesign — Vane Admin Dashboard

## Produto

Vane é status-page/monitoramento self-hosted da Zeep — SPA administrativa (React 18 + Vite + TypeScript + Tailwind CSS v4), embarcada em binário Go. Público: gestor de infra/operações que configura integrações (Datadog), serviços monitorados, domínios/status-pages públicas, incidentes, administradores, poller.

## Problema atual

Layout funcional mas genérico — legível como "gerado por IA": componentes flat sem hierarquia visual forte, densidade uniforme sem ritmo, pouca profundidade (quase nenhuma elevação/camada), tipografia sem escala expressiva, ícones outline finos e soltos sem sistema de peso consistente, sidebar item-list simples sem agrupamento. Objetivo: elevar para algo moderno, elegante, com identidade — sem mudar paleta de cor.

## O que NÃO muda

- **Paleta de cor é fixa** — tema escuro "Nocturne", tokens abaixo. Não propor nova cor de marca, não propor light mode. Uso e proporção das cores pode mudar (ênfase, contraste, camadas), os valores em si não.
- **Stack de implementação**: Tailwind v4 (token-driven via `@theme`), sem shadcn/radix/headlessui — componentes próprios em `src/components/ui/`. O redesign deve ser implementável nesse modelo (não assumir biblioteca de componente que não existe hoje).
- Strings passam por `react-i18next` (pt-BR + inglês) — nenhum texto pode virar imagem/hardcode.

## Tokens de cor atuais (não alterar valores)

```
--color-bg: #161826        (fundo base)
--color-surface: #232532   (cards/painéis)
--color-text: #e9e9ed
--color-accent: #9184d9    (roxo primário)
--color-accent-2: #a7a1db  (roxo secundário)
--color-divider: e9e9ed a 16%

--color-success: oklch(0.72 0.135 152)
--color-warning: oklch(0.78 0.15 80)
--color-critical: oklch(0.685 0.19 25)

Neutral ramp: 100→900 (claro→escuro)
Accent ramp: 100→900
Accent-2 ramp: 100→900
```

Fonte atual: Inter (heading e body, únicas). Pode manter Inter ou propor troca — abertura para justificar troca de fonte se elevar elegância (ex.: par de fontes display + text), mas manter zero custo de licença (Google Fonts / open source).

## Diagnóstico do estado atual (referência de leitura para o design tool)

- **Sidebar** (`Sidebar.tsx`): 236px fixa, logo pequeno no topo, lista de nav-items flat (ícone 17px + label, altura 36px, hover simples de cor), sem seções/grupos, sem badges de contagem, footer com seletor de "viewing as" (dev only) + card de usuário + logout. Sem colapso/compactação.
- **Tokens de espaçamento**: escala muito fina (2.8px→22.4px) — sugere que hoje quase tudo é compacto/denso sem respiro generoso em áreas de destaque (headers, dashboards).
- **Radius**: sm 4px / md 8px / lg 14px — conservador, pode ganhar mais personalidade em superfícies grandes (cards, modais).
- **Shadow**: 3 níveis, todos baseados em borda de 1px + sombra sutil — pouca profundidade real, quase todo componente parece "no mesmo plano".
- **Componentes existentes**: `Button`, `Card`, `Dialog`, `Drawer`, `Field`, `IconRoleSelector`, `Input`, `Pager`, `PhoneField`, `Seg` (segmented control), `Table`, `Tag`, `Tooltip`, `EmptyState`. Redesign deve cobrir esse inventário completo — qualquer proposta que ignore um desses componentes deixa gap de implementação.
- **Ícones**: SVG inline outline, stroke 1.6, sem biblioteca de ícone consistente — poderia virar sistema (ex.: peso único de stroke, grid consistente, talvez ícones duotone leves usando accent).

## Contexto de produto — modelo dual self-hosted + SaaS (em construção, AD-022)

Vane está migrando de single-tenant para multi-tenant (feature `multi-tenancy-core`, spec aprovada, em execução) para suportar dois modelos no mesmo codebase:

- **Self-hosted** (existente): 1 instalação = 1 tenant único, auto-provisionado no bootstrap. Sem tela de billing/plano. LLM configurado direto pelo gestor de infra. Licenciamento futuro (fora do escopo desta feature) pode bloquear features por licença paga, mas isso ainda não tem UI definida — não inventar tela de licença agora.
- **SaaS** (novo): signup público self-service, múltiplos tenants por conta de usuário (1 usuário pode pertencer a N tenants — modelo tipo Slack/GitHub org), verificação de email obrigatória antes de logar, plano `free` + planos pagos (billing/subscription é subsistema futuro, fora do escopo desta feature — não desenhar checkout/billing ainda), LLM só habilitado no plano pago com limite de uso (gating de feature por plano ainda sem spec de billing — desenhar o *espaço* para isso, não o fluxo de pagamento em si).

O briefing de redesign deve cobrir as telas abaixo, que já fazem parte do form/fluxo aprovado, além das 8 originais:

9. **Seletor de tenant** (`TenantSelector`) — tela pós-login exibida só quando a conta tem >1 tenant/membership; lista os tenants do usuário, ação leva pro dashboard do tenant escolhido. Precisa comunicar "troca de contexto de empresa", não é um menu comum.
10. **Signup SaaS** (`SignupPage`) — formulário público (nome/email/senha, sem convite), tela de "confirme seu email" pós-cadastro com ação de reenvio, tratamento de erro de email duplicado.
11. **Configurações → Perfil da empresa** (extensão da tela 8) — ganha campos fiscais opcionais (razão social, CPF/CNPJ com seletor de tipo), sem campo de endereço de cobrança (isso pertence ao futuro módulo de billing, não expor aqui).
12. **Header/topbar com indicador de tenant ativo** — hoje não existe um seletor visível de "qual empresa estou vendo"; quando o usuário tem múltiplos tenants, precisa aparecer algum indicador/switcher rápido (dropdown no topo ou no rodapé da sidebar), não só a tela cheia do item 9.

**Desenhar com espaço para, mas SEM implementar ainda** (fora de escopo desta feature, não inventar fluxo completo):
- Badge de plano (free/pago) em algum canto visível do shell do dashboard.
- Estado "recurso bloqueado por plano" para a área de LLM (ex.: card de upsell ao invés do formulário de configuração, quando plano é free) — só o *padrão visual* de "feature gated by plan", não o texto/lógica de billing.
- Licenciamento self-hosted (campo de código de licença) — não desenhar agora, feature ainda não especificada.

## Telas principais (por ordem de uso)

1. Login / Bootstrap (primeiro setup)
2. Dashboard de Integrações (Datadog config, LLM provider, email provider)
3. Serviços monitorados (lista + detalhe)
4. Domínios & Status Pages (lista + editor + preview)
5. Incidentes (lista + timeline + fechamento com IA)
6. Administradores (lista + convite + papéis, agora por tenant)
7. Poller Status (health/observabilidade do próprio sistema)
8. Configurações (empresa, branding, perfil fiscal)
9. Seletor de tenant (multi-tenant, pós-login)
10. Signup SaaS + verificação de email
11. Topbar/shell com indicador de tenant ativo

## Direção de design pedida

- **Elegante e moderno**, não minimalista genérico — quer sensação de produto premium B2B, não template de admin gratuito.
- Mais **hierarquia visual**: tipografia com escala mais expressiva entre H1/H2 e corpo, uso deliberado de peso de fonte para guiar leitura.
- Mais **profundidade/camadas**: elevação real entre sidebar/header/conteúdo/modais — glow sutil de accent em elementos ativos é bem-vindo (a paleta já tem roxo vibrante para isso), superfícies com leve gradiente ou textura sutil ao invés de flat uniforme.
- **Sidebar repensada**: agrupar itens por contexto (ex.: "Monitoramento" vs "Administração" vs "Sistema"), dar mais respiro entre grupos, considerar estado colapsado, indicador ativo mais forte que apenas cor de texto+fundo (ex.: barra lateral de destaque, ícone preenchido quando ativo).
- **Ritmo de espaçamento**: áreas de destaque (topo de página, cards de métrica) podem respirar mais; áreas densas (tabelas) mantêm compactação.
- **Empty states e dados vazios**: hoje existe `EmptyState` — usar como oportunidade de ilustração/elegância, não só texto cinza.
- Motion sutil é bem-vinda (hover, transição de estado ativo, abertura de modal/drawer) mas nada que pese performance ou vire distração.

## Entregáveis esperados do Claude Design

- Screens de alta fidelidade para as 8 telas listadas, no tema escuro Nocturne.
- Componente system atualizado cobrindo o inventário de `src/components/ui/` (não inventar componente que não existirá na implementação, a menos que seja explicitamente marcado como "novo componente a construir").
- Especificação de tokens atualizados (espaçamento, radius, shadow, tipografia) — pode propor novos valores para esses quatro grupos, mantendo os tokens de cor intocados.
- Anotação de estados (default/hover/active/disabled/loading/error) para os componentes interativos principais (Button, Input, Table row, nav item).
