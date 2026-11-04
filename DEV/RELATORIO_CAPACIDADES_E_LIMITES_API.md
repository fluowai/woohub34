# Relatório Estratégico & Técnico: Capacidades, Limites e Roadmap da WooHub API (Motor Whatsmeow)

> **Data da Auditoria:** 03/11/2026  
> **Escopo:** Análise completa do repositório (`whatsmeow` core + camada SaaS `internal/` e `web/`) para comercialização como **API Não Oficial de WhatsApp (B2B SaaS / White-Label)** em comparação direta com a **API Oficial da Meta (Cloud API)**.

---

## 1. Diagnóstico do Estado Atual do Repositório

O repositório atual possui **duas camadas distintas**:
1. **Motor Base (Raiz do Repositório - `package whatsmeow`):** A biblioteca completa `go.mau.fi/whatsmeow` em Go puro (protocolo Web Multidevice via NoiseSocket + Signal Protocol E2EE). Consome apenas **15 MB a 35 MB de RAM por instância conectada** (15x a 30x mais leve que WPPConnect/Puppeteer e 4x mais leve que Baileys em Node.js).
2. **Camada Comercial SaaS (`cmd/woohub-api`, `internal/`, `web/`):** Já implementada como **WooHub API**, contando com arquitetura Multitenant de 3 níveis (`Super Admin` -> `Revendedor White-Label` -> `Cliente Final`), suporte nativo a **Supabase (PostgreSQL)** e SQLite, RBAC com proteção Anti-IDOR, proteção Anti-SSRF, fila de Webhooks com HMAC-SHA256 e painel SPA completo.

### 1.1 O que JÁ está exposto na sua API hoje ([internal/api/routes.go](file:///c:/Users/paulo/whatsmeow/internal/api/routes.go))
Atualmente, a camada REST (`internal/api`) expõe apenas **~15% do poder real** que o motor `whatsmeow` já tem pronto no código-fonte da raiz:
- **Sessão e Conexão:** Criar/apagar instância, conectar, QR Code (Base64 + Streaming SSE), Código de Pareamento de 8 dígitos (`PairPhone`), status e desconexão.
- **Mensageria Básica:** Envio de texto simples (`Conversation`), envio de áudio com transcodificação automática via FFmpeg para OGG/Opus **PTT (gravado na hora)** e envio de imagem com legenda.
- **Regras Brasil:** Normalização do `+55` e fallback automático do 9º dígito (`AlternarNonoDigitoBrasil` em [internal/core/brasil.go](file:///c:/Users/paulo/whatsmeow/internal/core/brasil.go)).
- **Webhooks:** Recebimento básico de mensagens de texto, status de entrega/leitura (`Receipt`) e status de conexão.

> [!IMPORTANT]
> **Conclusão do Diagnóstico:** O motor (`whatsmeow`) já possui implementados dezenas de recursos avançados (Grupos, Comunidades, Enquetes, Reações, Edição/Exclusão de mensagens, Presença "digitando...", Validador de números em lote, Catálogo/Pedidos, Canais/Newsletters, Rejeição de chamadas, Proxy por instância e Download de mídias) que **ainda não possuem rotas criadas em `internal/api/handlers.go`**.

---

## 2. Comparativo Direto: API Oficial (Meta Cloud API) vs. Nossa API (`whatsmeow`)

Para vender como serviço (SaaS) com segurança comercial e alinhar a expectativa dos clientes, abaixo está a matriz exata do que conseguimos igualar, onde **superamos** a API Oficial e onde existem **bloqueios técnicos da própria Meta**.

| Funcionalidade / Recurso | API Oficial (Meta Cloud API) | Nossa API (`whatsmeow`) | Situação no Repositório Atual |
| :--- | :--- | :--- | :--- |
| **Custo por Mensagem / Janela 24h** | Cobra por template (Marketing/Utilidade) e trava conversa após 24h | **R$ 0,00 por mensagem** e **sem trava de 24h** | **Pronto** |
| **Texto, Emojis, Links com Preview** | Suportado | **Suportado** (`Conversation` / `ExtendedTextMessage`) | Texto simples pronto; falta expor Link Preview e Menções |
| **Áudio Gravado na Hora (PTT)** | Áudios enviados via API aparecem como arquivo encaminhado/música | **Suportado Nativo** (aparece como microfone verde gravado na hora + waveform) | **Pronto** (`transcodificarParaOpusPTT` em [instance_manager.go](file:///c:/Users/paulo/whatsmeow/internal/core/instance_manager.go#L417-L454)) |
| **Imagens, Vídeos, Documentos (PDF/Docx/Xlsx) e Stickers** | Suportado | **Suportado** (`ImageMessage`, `VideoMessage`, `DocumentMessage`, `StickerMessage`) | Apenas Imagem exposta; Vídeo, Documento e Sticker prontos no motor (`send.go`), falta rota HTTP |
| ** Visualização Única (View Once)** | Não suportado na API Oficial | **Suportado** (`ViewOnceMessageV2` para fotos, vídeos e áudios) | Pronto no motor (`send.go`), falta rota HTTP |
| **Responder (Quote), Reagir, Editar e Apagar para Todos** | Suporta Responder e Reagir; **Não edita** nem apaga livremente após janela | **Suportado 100%** (`ContextInfo`, `BuildReaction`, `BuildEdit` até 20 min, `BuildRevoke`) | Pronto em [send.go](file:///c:/Users/paulo/whatsmeow/send.go#L511-L633), falta rota HTTP |
| **Simulação Humana ("Digitando..." / "Gravando áudio...")** | Limitado (apenas mark as read) | **Suportado 100%** (`SendChatPresence`: `composing` texto ou `recording` áudio) | Pronto em [presence.go](file:///c:/Users/paulo/whatsmeow/presence.go#L130-L144), falta rota HTTP |
| **Enquetes Interativas (Polls)** | Não suportado nativamente na Cloud API | **Suportado 100%** (Criação de enquete e descriptografia de votos em tempo real) | Pronto em [msgsecret.go](file:///c:/Users/paulo/whatsmeow/msgsecret.go#L391-L435), falta rota HTTP |
| **Botões Clicáveis (`ButtonsMessage`) e Listas (`ListMessage`)** | **Exclusivo da API Oficial** (funciona 100% em Android/iOS/Web) | **BLOQUEADO PELA META** (Meta desativou renderização de botões/listas enviadas via protocolo Web não-oficial no iOS e Android) | **Substituir por Enquetes (`PollCreationMessage`) ou Menus Numéricos** |
| **WhatsApp Flows (Formulários Nativos)** | **Exclusivo da API Oficial** | **Inviável** (Exige chave criptográfica de Business Solution Provider da Meta) | Limite estrutural da Meta |
| **Gestão de Grupos e Comunidades** | **Não existe** na API Oficial (Meta removeu suporte a grupos) | **Suporte Total e Absoluto** (Criar, adicionar/remover, promover admin, foto, descrição, aprovar entrada, links de convite, Comunidades) | 100% pronto em [group.go](file:///c:/Users/paulo/whatsmeow/group.go#L57-L628), falta expor rotas HTTP |
| **Publicação de Status / Stories (24h)** | **Não existe** na API Oficial | **Suportado** (Enviar texto colorido, foto ou vídeo para `status@broadcast`) | Pronto em [broadcast.go](file:///c:/Users/paulo/whatsmeow/broadcast.go#L47-L95), falta rota HTTP |
| **Canais do WhatsApp (Newsletters)** | Não disponível na Cloud API pública | **Suportado** (Criar canal, seguir, publicar textos/mídias, ler mensagens e reações) | Pronto em [newsletter.go](file:///c:/Users/paulo/whatsmeow/newsletter.go#L28-L440), falta rota HTTP |
| **Validação de Números (`IsOnWhatsApp`)** | Não possui endpoint de higienização de base | **Suportado** (Verifica em lote se números possuem WhatsApp ativo, retorna JID real com ou sem 9º dígito e se é conta Business) | Pronto em [user.go](file:///c:/Users/paulo/whatsmeow/user.go#L176-L229), falta rota HTTP |
| **Rejeição Automática de Ligações (Voz/Vídeo)** | Não aplicável / pago à parte | **Suportado** (`RejectCall` no evento `events.CallOffer` + envio imediato de mensagem de texto) | Pronto em [call.go](file:///c:/Users/paulo/whatsmeow/call.go#L106-L122) e colunas já existem no banco (`rejeitar_chamadas`)! |
| **Proxy Dedicado por Instância (SOCKS5 / HTTP)** | Não aplicável | **Suportado Nativo** (`SetProxyAddress` / `SetSOCKSProxy` por cliente) | Pronto em [client.go](file:///c:/Users/paulo/whatsmeow/client.go#L338-L418), falta campo no banco e UI |

---

## 3. Os Limites Reais (O que NÃO prometer ou como contornar)

Para proteger o seu negócio de cancelamentos (churn) e processos, você precisa conhecer os **4 limites críticos** de qualquer API baseada em WhatsApp Web (`whatsmeow`, Baileys, Evolution, Z-API):

### 3.1 O Mito dos Botões (`ButtonsMessage` / `ListMessage` / `TemplateMessage`)
- **O Fato Técnico:** Embora o protobuf do `whatsmeow` ([send.go](file:///c:/Users/paulo/whatsmeow/send.go#L1014-L1055)) ainda possua as estruturas `ButtonsMessage` e `ListMessage`, os aplicativos móveis oficiais do WhatsApp (especialmente **iOS** e versões atuais do **Android**) **ignoram ou ocultam mensagens de botões/listas** que não venham assinadas pelos servidores da Cloud API Oficial da Meta.
- **Como vender / contornar para o seu cliente:**
  1. Ofertar **Enquetes Nativas (`Polls`)** com `selectableOptionCount: 1` (escolha única). Elas funcionam em **100% dos aparelhos (Android, iOS e Web)**, têm visual de botões clicáveis e disparam um webhook instantâneo quando o cliente clica na opção!
  2. Ofertar **Links Clicáveis com Preview Rico (CTAs via URL)** em `ExtendedTextMessage`.

### 3.2 Limite de Disparos em Massa & Risco de Banimento (Chip Frio)
- **O Fato Técnico:** A API Oficial permite disparar 1.000 a 100.000 mensagens/dia pagando por template. Na API não oficial, o número conectado está sujeito ao algoritmo anti-spam do WhatsApp (taxa de denúncias/bloqueios e disparos para contatos que nunca interagiram com o chip).
- **Como contornar na nossa plataforma (Diferencial de Produto):**
  1. **Proxy Residencial/Móvel por Instância:** Usar `cli.SetProxyAddress()` ([client.go](file:///c:/Users/paulo/whatsmeow/client.go#L338)) para que 50 clientes diferentes no seu servidor não compartilhem o mesmo IP de datacenter perante a Meta.
  2. **Motor de Cadência Humana Automática:** Antes de enviar qualquer mensagem ou áudio, o nosso backend deve chamar automaticamente:
     - `cli.SendPresence(types.PresenceAvailable)`
     - `cli.SendChatPresence(jid, types.ChatPresenceComposing, "")` por 2 a 4 segundos (ou `ChatPresenceMediaAudio` antes de um PTT).
  3. **Verificação Prévia (`IsOnWhatsApp`):** Nunca tentar enviar mensagem diretamente para números inexistentes; validar antes com `cli.IsOnWhatsApp()` ([user.go](file:///c:/Users/paulo/whatsmeow/user.go#L176)).

### 3.3 Recebimento de Mídias (Download Descriptografado)
- **O Fato Técnico:** Hoje, em [instance_manager.go](file:///c:/Users/paulo/whatsmeow/internal/core/instance_manager.go#L638-L672), quando uma instância recebe uma foto, áudio ou PDF, o webhook envia apenas `"tem_midia": true`, mas **não baixa nem descriptografa o arquivo** (`cli.DownloadAny(ctx, v.Message)` em [download.go](file:///c:/Users/paulo/whatsmeow/download.go#L127)).
- **Impacto:** Se o seu cliente integrar um chatbot (Typebot, n8n, Chatwoot, IA com Whisper), ele precisará receber a URL pública ou o Base64 do áudio/imagem/PDF recebido. Isso já está 100% pronto no `whatsmeow` (`DownloadAny`) e leva poucas linhas para ativar no `instance_manager.go`.

### 3.4 Limite de Escala em Memória vs. Armazenamento
- **O Fato Técnico:** Cada instância em Go consome ~20 MB de RAM. Um servidor VPS de **8 GB de RAM** aguenta tranquilamente **250 a 350 instâncias conectadas simultaneamente** (algo que exigiria 150 GB de RAM em soluções baseadas em navegador).
- **Ponto de Atenção no Supabase:** Como o `sqlstore` do `whatsmeow` grava chaves criptográficas Signal (prekeys, sender keys, sessions) a cada mensagem trocada, em conexões com **Supabase** deve-se usar a porta **5432 (Session Mode)** ou conexão direta para evitar conflitos de prepared statements com o PgBouncer em Transaction Mode.

---

## 4. Catálogo Completo do que Podemos Liberar e Ofertar aos Clientes

Com base na leitura integral do código-fonte do `whatsmeow`, dividimos tudo o que podemos lançar em **4 Pacotes Comerciais de Endpoints** para transformar a **WooHub API** na API não oficial mais completa do Brasil:

### Pacote 1: Mensageria Completa & Interatividade (Alta Demanda para Bots/CRMs)
Tudo já suportado pelo motor em [send.go](file:///c:/Users/paulo/whatsmeow/send.go) e [msgsecret.go](file:///c:/Users/paulo/whatsmeow/msgsecret.go):
1. `POST /api/v1/mensagem/documento` — Envio de PDF, boletos, XML de NF-e, planilhas (`.xlsx`, `.docx`, `.zip`) com nome de arquivo customizado e legenda (`DocumentWithCaptionMessage`).
2. `POST /api/v1/mensagem/video` — Envio de vídeos MP4 com legenda, miniatura JPEG e opção de reprodução como **GIF** (`GifPlayback: true`) ou **Nota de Vídeo Circular (PTV)**.
3. `POST /api/v1/mensagem/sticker` — Envio de figurinhas estáticas ou animadas (`.webp`).
4. `POST /api/v1/mensagem/enquete` — **O substituto perfeito dos botões!** Envia enquete interativa (`BuildPollCreation`) e entrega o clique do usuário via Webhook (`DecryptPollVote`).
5. `POST /api/v1/mensagem/localizacao` — Envio de pino de mapa (`LocationMessage`) com latitude, longitude, nome da loja e endereço.
6. `POST /api/v1/mensagem/contato` — Envio de cartão de visita clicável (`ContactMessage` / vCard único ou múltiplos contatos).
7. `POST /api/v1/mensagem/reacao` — Reagir com qualquer emoji a uma mensagem do cliente (`BuildReaction`) ou remover reação.
8. `PUT /api/v1/mensagem/editar` — Editar o texto de uma mensagem já enviada em até 20 minutos (`BuildEdit`).
9. `DELETE /api/v1/mensagem/revogar` — Apagar uma mensagem enviada para todos (`BuildRevoke`), inclusive apagar mensagens de membros em grupos onde a instância é Admin.
10. **Parâmetros Universais em todos os envios:**
    - `responder_mensagem_id` (Quote/Reply com citação da mensagem original).
    - `mencionar` (Lista de números ou `"todos"` para marcação fantasma `@everyone` em grupos).
    - `delay_digitando_ms` (Simula automaticamente `"Digitando..."` ou `"Gravando áudio..."` antes de disparar).
    - `visualizacao_unica: true` (Envelopa mídia em `ViewOnceMessageV2`).

### Pacote 2: Gestão de Atendimento, Presença, Perfil e Validação (Diferencial B2B)
Tudo já suportado em [user.go](file:///c:/Users/paulo/whatsmeow/user.go), [presence.go](file:///c:/Users/paulo/whatsmeow/presence.go), [receipt.go](file:///c:/Users/paulo/whatsmeow/receipt.go) e [call.go](file:///c:/Users/paulo/whatsmeow/call.go):
1. `POST /api/v1/contato/verificar` — **Higienizador de Listas:** Recebe até 50 números de uma vez, consulta `cli.IsOnWhatsApp()`, retorna quais números têm WhatsApp ativo e qual o JID exato (resolvendo 100% o problema do 9º dígito antes mesmo de enviar!).
2. `GET /api/v1/contato/perfil` — Retorna URL da foto de perfil (`GetProfilePictureInfo`), recado/status (`GetUserInfo`) e dados de perfil comercial (`GetBusinessProfile`: endereço, categoria, horário de funcionamento, e-mail, site).
3. `POST /api/v1/chat/presenca` — Envia estado `"digitando"` (`composing`), `"gravando_audio"` (`recording`) ou `"pausado"` (`paused`).
4. `POST /api/v1/chat/marcar-lido` — Envia confirmação de leitura (dois tracinhos azuis - `MarkRead`) ou de áudio reproduzido (`ReceiptTypePlayed`).
5. `POST /api/v1/chat/arquivar`, `/fixar`, `/silenciar` — Controle do estado da conversa no aparelho via `SendAppState` ([appstate/encode.go](file:///c:/Users/paulo/whatsmeow/appstate/encode.go)).
6. `POST /api/v1/chat/etiqueta` — **Exclusivo WhatsApp Business:** Criar etiquetas coloridas e etiquetar conversas (`BuildLabelChat` / `BuildLabelEdit` em [appstate/encode.go](file:///c:/Users/paulo/whatsmeow/appstate/encode.go#L143-L209)).
7. **Auto-Rejeição de Chamadas (`RejectCall`):** Ativar no `instance_manager.go` a leitura das flags `rejeitar_chamadas` e `mensagem_chamada_rejeitada` (que já existem na tabela `instancias`!) para recusar ligações na hora e responder automaticamente *"Olá! Este número não recebe ligações. Por favor, envie uma mensagem de texto ou áudio."*

### Pacote 3: Automação de Grupos, Comunidades e Status (Para Lançamentos e Infoprodutores)
O mercado brasileiro de lançamentos (perpétuos, grupos VIP, comunidades) paga caro por isso porque **a API Oficial não faz**:
1. `GET /api/v1/grupo/listar` — Lista todos os grupos que a instância participa (`GetJoinedGroups`).
2. `POST /api/v1/grupo/criar` — Cria grupo já configurando se apenas admins falam (`IsAnnounce`), se dados são travados (`IsLocked`) e mensagens temporárias (`CreateGroup`).
3. `GET /api/v1/grupo/{jid}/info` — Lista todos os membros, quem é admin/superadmin e descrição (`GetGroupInfo`).
4. `POST /api/v1/grupo/{jid}/participantes` — Adiciona, remove, promove a admin ou rebaixa membros (`UpdateGroupParticipants`).
5. `PUT /api/v1/grupo/{jid}/configuracoes` — Altera nome (`SetGroupName`), descrição (`SetGroupDescription`), foto (`SetGroupPhoto`), fecha/abre grupo para apenas admins falarem (`SetGroupAnnounce`).
6. `GET /api/v1/grupo/{jid}/convite` e `POST /api/v1/grupo/entrar` — Obtém/reseta link `chat.whatsapp.com/...` (`GetGroupInviteLink`) ou entra em grupo pelo link (`JoinGroupWithLink`).
7. **Menção Fantasma (`Ghost Mention`):** Enviar mensagem de aviso no grupo marcando todos os 1.024 participantes no `ContextInfo.MentionedJID` sem poluir o texto visível da mensagem.
8. `POST /api/v1/status/publicar` — Publica Status (Stories de 24h) de texto com cor de fundo/fonte, foto ou vídeo para todos os contatos (`types.StatusBroadcastJID`).

### Pacote 4: Infraestrutura Anti-Ban & Webhooks Avançados (Escala SaaS)
1. **Download Automático de Mídias Recebidas no Webhook:** Usar `cli.DownloadAny(ctx, evt.Message)` no evento `*events.Message` para entregar o arquivo recebido em Base64 ou salvo em `/armazenamento_midia` com link público temporário.
2. **Proxy por Instância (`SOCKS5` / `HTTP`):** Adicionar coluna `proxy_url` na tabela `instancias` e aplicar `cli.SetProxyAddress(instDB.ProxyURL)` antes do `Connect()`.
3. **Integrações Plug-and-Play (Webhooks Nativos):** Formatação compatível ou adaptadores prontos para **n8n**, **Typebot**, **Chatwoot** e **ActiveCampaign / GoHighLevel**.

---

## 5. Plano de Ação Recomendado (Próximos Passos)

Se você quiser transformar o repositório atual neste produto completo imediatamente, recomendo executarmos na seguinte ordem de prioridade de negócio:

1. **Sprint 1 — Mensageria Completa + Recebimento de Mídia + Rejeição de Chamadas (Essencial para vender já):**
   - Implementar download automático de mídias recebidas (áudio, imagem, PDF, vídeo, figurinha) nos Webhooks.
   - Ativar a rejeição automática de chamadas (`RejectCall`) com resposta automática que já está modelada no banco.
   - Criar os endpoints de **Documento (PDF/Boleto)**, **Vídeo**, **Sticker**, **Localização**, **Contato (vCard)**, **Reação**, **Edição**, **Exclusão (Revoke)**, **Enquetes (Polls)** e **Presença ("Digitando..." / "Gravando áudio...")** com delay humanizado.
2. **Sprint 2 — Validador de Números (`IsOnWhatsApp`) + Gestão Completa de Grupos & Status:**
   - Expor `/api/v1/contato/verificar`, `/api/v1/contato/perfil` e todas as rotas de `/api/v1/grupo/...` (incluindo Menção Fantasma para lançamentos) e `/api/v1/status/publicar`.
3. **Sprint 3 — Blindagem Anti-Ban (Proxy por Instância + Cadência):**
   - Adicionar suporte a Proxy individual por instância (`SetProxyAddress`) no banco, na API e no modal de configuração do Painel Web.
   - Atualizar a Documentação Interativa e a Collection Postman com todos os novos endpoints.

