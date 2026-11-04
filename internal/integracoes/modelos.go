package integracoes

// ConfigChatwoot define os parâmetros da Caixa Chatwoot
type ConfigChatwoot struct {
	URL                          string `json:"url"`                            // Ex: "https://app.chatwoot.com" ou servidor próprio
	AccountID                    string `json:"account_id"`                     // ID da conta no Chatwoot (ex: "1")
	Token                        string `json:"token"`                          // User Access Token ou Bot Token
	InboxID                      string `json:"inbox_id,omitempty"`             // ID da Caixa de Entrada tipo API Channel
	NomeInbox                    string `json:"nome_inbox,omitempty"`          // Nome para auto-criação da caixa se não existir
	ReabrirConversa              bool   `json:"reabrir_conversa"`               // Reabrir conversa resolvida ao receber nova mensagem
	SincronizarMensagensEnviadas bool   `json:"sincronizar_mensagens_enviadas"` // Sincronizar mensagens enviadas direto pelo celular
	ImportarFotoPerfil           bool   `json:"importar_foto_perfil"`           // Importar foto de perfil do WhatsApp para o Chatwoot
}

// ConfigTypebot define os parâmetros da Caixa Typebot
type ConfigTypebot struct {
	URL              string `json:"url"`                          // Ex: "https://typebot.io" ou self-hosted
	PublicID         string `json:"public_id"`                    // Slug / ID público do Typebot
	ExpiracaoMinutos int    `json:"expiracao_minutos,omitempty"`  // Padrão: 30 minutos
	PalavraTransbordo string `json:"palavra_transbordo,omitempty"` // Ex: "#humano", "atendente", "sair"
	IgnorarGrupos    bool   `json:"ignorar_grupos"`               // Não acionar o bot em mensagens de grupos
	AtrasoDigitacaoMs int   `json:"atraso_digitacao_ms,omitempty"`// Simular digitação humana antes de enviar resposta
}

// ConfigN8N define os parâmetros da Caixa n8n
type ConfigN8N struct {
	WebhookURL   string   `json:"webhook_url"`             // URL do Webhook Trigger no n8n
	Eventos      []string `json:"eventos,omitempty"`       // Lista de eventos (mensagens, status, conexao)
	TokenSecreto string   `json:"token_secreto,omitempty"` // Header X-N8N-Token para autenticação
}

// ConfigKommo define os parâmetros da Caixa Kommo CRM (antigo amoCRM)
type ConfigKommo struct {
	Subdominio           string `json:"subdominio"`                      // Ex: "minhaempresa.kommo.com"
	Token                string `json:"token"`                            // Long-Lived API Token
	PipelineID           int    `json:"pipeline_id,omitempty"`           // ID do Funil de Vendas padrão
	StatusID             int    `json:"status_id,omitempty"`             // ID da Etapa/Fase inicial
	CriarLeadAutomatico  bool   `json:"criar_lead_automatico"`           // Criar lead ao receber nova mensagem
	SincronizarNotas     bool   `json:"sincronizar_notas"`               // Gravar histórico de mensagens nas notas do Lead
}

// ConfigRDStation define os parâmetros da Caixa RD Station (Marketing & CRM)
type ConfigRDStation struct {
	TokenPrivado          string   `json:"token_privado,omitempty"`         // API Token do RD Station Marketing
	TokenCRM              string   `json:"token_crm,omitempty"`             // Token do RD Station CRM
	IdentificadorConversao string  `json:"identificador_conversao,omitempty"`// Ex: "whatsapp-inbound"
	CriarOportunidadeCRM  bool     `json:"criar_oportunidade_crm"`          // Criar oportunidade no RD CRM
	Tags                  []string `json:"tags,omitempty"`                  // Tags automáticas (ex: ["whatsapp", "organico"])
}

// ConfigWebhookUniversal define disparador de múltiplos webhooks flexíveis
type ConfigWebhookUniversal struct {
	Destinos []DestinoWebhook `json:"destinos"`
}

type DestinoWebhook struct {
	URL     string   `json:"url"`
	Segredo string   `json:"segredo,omitempty"`
	Eventos []string `json:"eventos,omitempty"` // Vazio = todos
}

// MensagemRecebidaPayload representa o evento consolidado entregue às integrações
type MensagemRecebidaPayload struct {
	InstanciaID      string                 `json:"instancia_id"`
	NomeInstancia    string                 `json:"nome_instancia"`
	IDMensagem       string                 `json:"id_mensagem"`
	Tipo             string                 `json:"tipo"`
	RemetenteJID     string                 `json:"remetente_jid"`
	RemetenteBR      string                 `json:"remetente_br"`
	NomeContato      string                 `json:"nome_contato"`
	ChatJID          string                 `json:"chat_jid"`
	DeMim            bool                   `json:"de_mim"`
	EhGrupo          bool                   `json:"eh_grupo"`
	Texto            string                 `json:"texto"`
	TemMidia         bool                   `json:"tem_midia"`
	MimeType         string                 `json:"mimetype,omitempty"`
	NomeArquivo      string                 `json:"nome_arquivo,omitempty"`
	URLMidia         string                 `json:"url_midia,omitempty"`
	MidiaBase64      string                 `json:"midia_base64,omitempty"`
	Timestamp        int64                  `json:"timestamp"`
	Origem           interface{}            `json:"origem,omitempty"` // DadosAtribuicao
	MetadadosExtras  map[string]interface{} `json:"metadados_extras,omitempty"`
}
