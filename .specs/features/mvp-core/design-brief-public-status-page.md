# Status Page Pública — Design Brief (para Claude Design / Figma)

**Fonte**: `.specs/features/mvp-core/spec.md` (SP-06 a SP-20) + `design.md`
**Objetivo deste doc**: contexto de produto suficiente pra gerar layout de alta fidelidade da página pública — não repete detalhe de implementação (Go/renderer/Host header), só o que afeta decisão visual/UX.

**Premissa assumida** (não travada em spec/design, decidir ou ajustar): mesma direção visual do `admin-frontend` (painel operacional, sem branding Starbem, produto standalone tipo Statuspage.io/Instatus) — mas esta tela é pública, sem sidebar/nav autenticada, sem RBAC.

---

## 1. Produto em uma frase

Página pública, sem login, servida no subdomínio próprio da empresa cliente (`status.empresa.com`), mostrando estado atual de cada serviço monitorado via SLO do Datadog + incidentes manuais com timeline — visitante não autenticado, sem interação de escrita.

## 2. Quem vê essa tela

Só um "papel": visitante público, sem autenticação (SP-06 item 5). Não existe modo leitura/edição aqui — a página inteira é somente leitura.

## 3. Telas a desenhar (2)

1. **Status page principal** — lista de serviços com estado atual + incidentes ativos em destaque no topo + histórico de incidentes resolvidos
2. **Detalhe de incidente** (se decidido como página separada, ou expansível inline na principal — a decidir no Figma) — timeline completa de updates

## 4. Estados de serviço a cobrir (badge/cor por serviço)

- `operational` — dentro do error budget do SLO
- `degraded` — error budget violado, nível parcial
- `outage` — error budget violado, nível crítico
- Estado "usando último dado em cache" — quando busca ao Datadog falhou (SP-06 item 4): mostrar timestamp da última atualização bem-sucedida, sem alarmar o visitante com erro técnico (isso é problema do admin, não do visitante)

## 5. Incidentes — regras que afetam layout

- Incidente não-resolvido (`investigating`/`identified`/`monitoring` etc.) fica **em destaque no topo da página**, acima da lista de serviços (SP-16 item 3)
- Timeline de updates: mais recente primeiro (SP-16 item 2)
- Incidente pode vincular múltiplos serviços — indicar quais na própria linha do incidente
- Reabertura existe (`resolved` → `investigating` de volta): timeline registra a mudança com timestamp, não esconder o histórico anterior
- Histórico de incidentes resolvidos fica visível por 90 dias (retenção) — precisa de seção "histórico" separada da seção "ativo agora"

## 6. Estados gerais da página

- **Carregando** (primeiro load)
- **Todos operacionais, sem incidente** — caso comum, deve parecer "tudo bem" sem precisar procurar
- **Com incidente ativo** — muda hierarquia visual, incidente vira o elemento principal da tela
- **Serviço degradado/outage sem incidente manual aberto** — cobrir esse caso (status automático pode mudar antes de o admin criar o post do incidente)
- Timestamp de "última atualização" sempre visível (polling é a cada 2min, backend nunca chama Datadog na requisição do visitante — SP-06 item 3)

## 7. Fora do escopo deste brief (não desenhar)

- Qualquer tela autenticada/admin (já coberta em `admin-frontend/design-brief.md`)
- Gráfico de histórico de uptime (P3, `SP-26`, dado não existe ainda no backend)
- Multi-idioma (não mencionado em spec)
- Assinatura/subscribe por email para notificação de incidente (não está na spec do MVP — se o usuário quiser, validar antes de desenhar, é escopo novo)

## 8. Tom / personalidade visual (a decidir no Figma — sem trava)

- Deve ser legível rápido — visitante quer resposta em segundos ("está tudo bem?"), não explorar a página
- Paleta de estado igual ao brief do admin-frontend, pra manter consistência entre as duas telas do mesmo produto: sucesso/operacional, degradado/aviso, falha/crítico, neutro/não configurado
- Sem exigir dark mode (não requisitado em spec)
- Sem branding Starbem — produto standalone

---

**Próximo passo**: usar este brief como prompt de contexto pro Claude Design (Figma) gerar layout da página pública + detalhe de incidente.
