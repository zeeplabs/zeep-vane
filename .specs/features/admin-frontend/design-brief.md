# Admin Frontend — Design Brief (para Claude Design / Figma)

**Fonte**: `.specs/features/admin-frontend/spec.md` + `design.md`
**Objetivo deste doc**: dar contexto de produto suficiente para gerar layouts de alta fidelidade — não repete detalhe técnico de implementação (React/API), só o que afeta decisão visual/UX.

**Premissa assumida** (não travada em spec/design, decidir ou ajustar): estilo visual novo, sem herdar identidade do `zeep-orbit`. Sem branding Starbem — este é produto standalone (status page / uptime monitoring), não produto B2B de saúde.

---

## 1. Produto em uma frase

SPA administrativa de um produto de status page + monitoramento (tipo Statuspage.io/Instatus): admin conecta Datadog, cadastra domínio, publica status page pública com TLS automático, e gerencia incidentes — tudo sem chamar API manualmente.

## 2. Usuários e papéis (RBAC visual)

| Papel | Pode fazer |
| --- | --- |
| `owner` | Tudo, incluindo convidar/remover/mudar papel de admins |
| `operator` | Tudo, exceto gestão de admins |
| `viewer` | Só leitura — nenhum botão de ação de escrita deve aparecer pra esse papel, em nenhuma tela |

Isso importa pro design: cada tela precisa de uma variante visual "modo leitura" (sem CTAs de escrita) vs "modo edição" (owner/operator).

## 3. Telas a desenhar (7 + 1 modal global)

Ordem de prioridade (P1 primeiro):

1. **Login** — email/senha, link "esqueci senha", erro genérico de credencial inválida
2. **Integrações (Datadog)** — form de API key (nunca reexibida em texto plano após salvar), lista de serviços com estado "not configured"/"operational"/"degraded", busca+seleção de SLO pra vincular a um serviço
3. **Domínios & Status Pages** — cadastro de domínio raiz, criação de status page em subdomínio, card/linha com estado "emitindo certificado" → "publicada" (com URL pública) → "falha" (com motivo)
4. **Incidentes** — lista separada "ativos" vs "resolvidos", criação de incidente vinculando serviços, timeline de updates (mais recente primeiro), ação de reabrir um incidente resolvido
5. **Admins (RBAC)** — só `owner` vê essa tela; lista de admins ativos + convites pendentes (badge "Pendente"), convidar por email+papel, reenviar/cancelar convite, mudar papel, remover
6. **Status do Poller** — por integração conectada: timestamp da última execução, sucesso/falha, mensagem de erro
7. **Banner global de falha de poller** — não é uma tela, é um banner fixo no topo, visível em qualquer rota autenticada, quando há falha ativa de polling
8. **Modal "Sessão expirada"** — bloqueante, sobre qualquer tela, com um único CTA que leva ao login

## 4. Estados que cada tela precisa cobrir

Todo layout precisa contemplar, no mínimo:

- **Vazio** (instalação nova) — CTA direto pra primeira ação, nunca tabela vazia genérica
- **Carregando**
- **Erro de rede/API** — toast, sem tela em branco, mantém última visualização válida
- **Populado** — o caso comum
- Onde aplicável: **estado intermediário assíncrono** (ex: "emitindo certificado" com polling, badge "Pendente" de convite)

## 5. Padrões de layout / navegação

- Layout autenticado: sidebar ou topbar de navegação + banner global de poller (quando ativo) + área de conteúdo
- Navegação oculta rotas que o papel atual não acessa (não é só "botão escondido", a própria entrada de menu não aparece pra `viewer` na tela de Admins)
- Formulários de erro 409 (conflito) e 422 (validação): mantêm os dados digitados, erro inline no campo quando a API aponta o campo específico
- Toda ação destrutiva (remover admin, cancelar convite) precisa de confirmação antes de executar

## 6. Tom / personalidade visual (a decidir no Figma — sem trava)

Não há decisão de marca registrada em spec/design. Sugestão de direção, mas o Claude Design pode propor alternativa:

- Painel operacional B2B, denso em dados, não uma landing page — prioriza escaneabilidade (tabelas, badges de estado, timestamps) sobre ilustração
- Paleta de estado precisa de pelo menos: sucesso/operacional, degradado/aviso, falha/crítico, neutro/não configurado, pendente
- Dark mode: não requisitado em spec — não assumir, perguntar se for decisão de produto

## 7. Fora do escopo deste brief (não desenhar)

- Renderização da status page pública (produto separado, fora desta SPA)
- Telas de auditoria/log
- Gráfico de histórico de uptime (dado não existe ainda no backend)

---

**Próximo passo**: usar este brief como prompt de contexto para o Claude Design (Figma) gerar os layouts das 7 telas + modal, na ordem de prioridade da seção 3.
