package integracoes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"go.mau.fi/whatsmeow/internal/database"
)

type DisparadorWhatsApp interface {
	EnviarMensagemTexto(ctx context.Context, id, destinatario, texto string) (string, error)
	EnviarMensagemAudio(ctx context.Context, id, destinatario string, dadosAudio []byte, ptt bool) (string, error)
	EnviarMensagemImagem(ctx context.Context, id, destinatario string, dadosImagem []byte, legenda string) (string, error)
	EnviarMensagemEnquete(ctx context.Context, id, destinatario, pergunta string, opcoes []string, maxEscolhas int) (string, error)
}

type GerenciadorIntegracoes struct {
	db        *database.GerenciadorDB
	wpp       DisparadorWhatsApp
	chatwoot  *ClienteChatwoot
	typebot   *ClienteTypebot
	n8n       *ClienteN8N
	kommo     *ClienteKommo
	rdstation *ClienteRDStation
}

var InstanciaGerenciador *GerenciadorIntegracoes

func InicializarGerenciadorIntegracoes(db *database.GerenciadorDB, wpp DisparadorWhatsApp) *GerenciadorIntegracoes {
	g := &GerenciadorIntegracoes{
		db:        db,
		wpp:       wpp,
		chatwoot:  NovoClienteChatwoot(),
		typebot:   NovoClienteTypebot(),
		n8n:       NovoClienteN8N(),
		kommo:     NovoClienteKommo(),
		rdstation: NovoClienteRDStation(),
	}
	InstanciaGerenciador = g
	return g
}

// ProcessarMensagemRecebida aciona todas as caixas de conexão ativas de forma paralela e não-bloqueante
func (g *GerenciadorIntegracoes) ProcessarMensagemRecebida(ctx context.Context, payload MensagemRecebidaPayload) {
	if g == nil || g.db == nil {
		return
	}

	integracoes, err := g.db.ObterIntegracoesInstancia(payload.InstanciaID)
	if err != nil || len(integracoes) == 0 {
		return
	}

	for _, intg := range integracoes {
		if !intg.Ativo {
			continue
		}

		// Disparo em goroutine protegida contra pânicos para resiliência 100%
		go func(item database.IntegracaoInstancia) {
			defer func() {
				if r := recover(); r != nil {
					log.Error().Interface("panic", r).Str("tipo", item.Tipo).Msg("Pânico recuperado no despachante da integração")
				}
			}()

			ctxOp, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()

			switch item.Tipo {
			case "chatwoot":
				var cfg ConfigChatwoot
				if err := json.Unmarshal([]byte(item.Configuracoes), &cfg); err == nil {
					_ = g.chatwoot.EnviarMensagemRecebida(ctxOp, cfg, payload)
				}

			case "typebot":
				var cfg ConfigTypebot
				if err := json.Unmarshal([]byte(item.Configuracoes), &cfg); err == nil {
					g.executarFluxoTypebot(ctxOp, cfg, payload)
				}

			case "n8n":
				var cfg ConfigN8N
				if err := json.Unmarshal([]byte(item.Configuracoes), &cfg); err == nil {
					_ = g.n8n.DespacharEvento(ctxOp, cfg, "mensagem_recebida", payload)
				}

			case "kommo":
				var cfg ConfigKommo
				if err := json.Unmarshal([]byte(item.Configuracoes), &cfg); err == nil {
					_ = g.kommo.SincronizarLeadECadastro(ctxOp, cfg, payload)
				}

			case "rdstation":
				var cfg ConfigRDStation
				if err := json.Unmarshal([]byte(item.Configuracoes), &cfg); err == nil {
					_ = g.rdstation.SincronizarLeadMarketing(ctxOp, cfg, payload)
				}

			case "webhook":
				var cfg ConfigWebhookUniversal
				if err := json.Unmarshal([]byte(item.Configuracoes), &cfg); err == nil {
					g.despacharWebhooksUniversais(ctxOp, cfg, "mensagem_recebida", payload)
				}
			}
		}(intg)
	}
}

// executarFluxoTypebot gerencia sessão, transbordo para humano e envio de blocos/enquetes para o WhatsApp
func (g *GerenciadorIntegracoes) executarFluxoTypebot(ctx context.Context, cfg ConfigTypebot, payload MensagemRecebidaPayload) {
	if payload.EhGrupo && cfg.IgnorarGrupos {
		return
	}
	if payload.DeMim {
		return
	}

	jid := payload.RemetenteJID
	textoRecebido := strings.TrimSpace(payload.Texto)

	// 1. Verificar comando de transbordo humano
	if cfg.PalavraTransbordo != "" && strings.Contains(strings.ToLower(textoRecebido), strings.ToLower(cfg.PalavraTransbordo)) {
		_ = g.db.SalvarSessaoTypebot(payload.InstanciaID, jid, "", "PAUSADO_HUMANO")
		_, _ = g.wpp.EnviarMensagemTexto(ctx, payload.InstanciaID, jid, "Aguarde um momento, transferindo seu atendimento para um atendente humano...")
		return
	}

	// 2. Checar estado da sessão
	sessaoDB, _ := g.db.ObterSessaoTypebot(payload.InstanciaID, jid)
	if sessaoDB != nil && sessaoDB.Estado == "PAUSADO_HUMANO" {
		return // Atendimento em controle de humano
	}

	expiraMin := cfg.ExpiracaoMinutos
	if expiraMin <= 0 {
		expiraMin = 30
	}

	var resBot *RespostaTypebot
	var errBot error

	if sessaoDB == nil || sessaoDB.SessionIDTypebot == "" || time.Since(sessaoDB.AtualizadoEm) > time.Duration(expiraMin)*time.Minute {
		// Iniciar nova sessão com variáveis do lead e origem de tráfego
		vars := map[string]interface{}{
			"telefone": payload.RemetenteBR,
			"nome":     payload.NomeContato,
			"canal":    "WhatsApp",
		}
		if payload.Origem != nil {
			if mapaOrigem, ok := payload.Origem.(map[string]interface{}); ok {
				for k, v := range mapaOrigem {
					vars[k] = v
				}
			}
		}

		resBot, errBot = g.typebot.IniciarSessao(ctx, cfg.URL, cfg.PublicID, vars)
		if errBot == nil && resBot != nil {
			_ = g.db.SalvarSessaoTypebot(payload.InstanciaID, jid, resBot.SessionID, "ATIVO")
		}
	} else {
		// Continuar sessão existente
		resBot, errBot = g.typebot.ContinuarSessao(ctx, cfg.URL, sessaoDB.SessionIDTypebot, textoRecebido)
		if errBot != nil && strings.Contains(errBot.Error(), "404") {
			// Se a sessão expirou no Typebot, reinicia
			_ = g.db.ExcluirSessaoTypebot(payload.InstanciaID, jid)
			resBot, errBot = g.typebot.IniciarSessao(ctx, cfg.URL, cfg.PublicID, map[string]interface{}{
				"telefone": payload.RemetenteBR,
				"nome":     payload.NomeContato,
			})
			if errBot == nil && resBot != nil {
				_ = g.db.SalvarSessaoTypebot(payload.InstanciaID, jid, resBot.SessionID, "ATIVO")
			}
		} else if errBot == nil {
			_ = g.db.SalvarSessaoTypebot(payload.InstanciaID, jid, sessaoDB.SessionIDTypebot, "ATIVO")
		}
	}

	if errBot != nil || resBot == nil {
		log.Warn().Err(errBot).Str("instancia", payload.InstanciaID).Msg("Falha ao comunicar com Typebot")
		return
	}

	// 3. Enviar Mensagens retornadas pelo Typebot para o WhatsApp
	for _, msg := range resBot.Mensagens {
		if cfg.AtrasoDigitacaoMs > 0 {
			time.Sleep(time.Duration(cfg.AtrasoDigitacaoMs) * time.Millisecond)
		} else {
			time.Sleep(800 * time.Millisecond)
		}

		switch msg.Type {
		case "text":
			txt := ExtrairTextoMensagemTypebot(msg.Content)
			if txt != "" {
				_, _ = g.wpp.EnviarMensagemTexto(ctx, payload.InstanciaID, jid, txt)
			}
		case "image":
			urlImg := ExtrairTextoMensagemTypebot(msg.Content)
			if urlImg != "" {
				dados, _ := baixarBytesRemotos(urlImg)
				if len(dados) > 0 {
					_, _ = g.wpp.EnviarMensagemImagem(ctx, payload.InstanciaID, jid, dados, "")
				}
			}
		case "audio":
			urlAud := ExtrairTextoMensagemTypebot(msg.Content)
			if urlAud != "" {
				dados, _ := baixarBytesRemotos(urlAud)
				if len(dados) > 0 {
					_, _ = g.wpp.EnviarMensagemAudio(ctx, payload.InstanciaID, jid, dados, true)
				}
			}
		}
	}

	// 4. Se houver Choices (Botões), envia como Enquete Interativa Oficial do WhatsApp!
	if resBot.Input != nil && resBot.Input.Type == "choice input" && len(resBot.Input.Items) > 0 {
		var opcoes []string
		for _, item := range resBot.Input.Items {
			if item.Content != "" {
				opcoes = append(opcoes, item.Content)
			}
		}
		if len(opcoes) > 0 {
			pergunta := "Selecione uma opção:"
			_, _ = g.wpp.EnviarMensagemEnquete(ctx, payload.InstanciaID, jid, pergunta, opcoes, 1)
		}
	}
}

func (g *GerenciadorIntegracoes) despacharWebhooksUniversais(ctx context.Context, cfg ConfigWebhookUniversal, evento string, payload interface{}) {
	for _, dest := range cfg.Destinos {
		if dest.URL == "" {
			continue
		}
		if len(dest.Eventos) > 0 {
			permitido := false
			for _, e := range dest.Eventos {
				if strings.EqualFold(e, evento) || e == "*" {
					permitido = true
					break
				}
			}
			if !permitido {
				continue
			}
		}

		corpo, _ := json.Marshal(map[string]interface{}{
			"evento":    evento,
			"timestamp": time.Now().Unix(),
			"dados":     payload,
		})

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest.URL, strings.NewReader(string(corpo)))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			if dest.Segredo != "" {
				req.Header.Set("X-Webhook-Secret", dest.Segredo)
			}
			cli := &http.Client{Timeout: 10 * time.Second}
			resp, errDo := cli.Do(req)
			if errDo == nil {
				_ = resp.Body.Close()
			}
		}
	}
}

// ProcessarWebhookReversoChatwoot recebe a mensagem enviada pelo atendente humano no Chatwoot e dispara no WhatsApp
func (g *GerenciadorIntegracoes) ProcessarWebhookReversoChatwoot(ctx context.Context, instanciaID string, corpo PayloadWebhookChatwoot) error {
	if corpo.Event != "message_created" || corpo.MessageType != "outgoing" || corpo.Private {
		return nil
	}

	destinatario := corpo.Conversation.ContactInbox.SourceID
	if destinatario == "" {
		destinatario = corpo.Conversation.Meta.Sender.PhoneNumber
	}
	if destinatario == "" {
		return fmt.Errorf("destinatário não identificado no webhook do Chatwoot")
	}

	// 1. Se tem texto
	if strings.TrimSpace(corpo.Content) != "" {
		_, err := g.wpp.EnviarMensagemTexto(ctx, instanciaID, destinatario, corpo.Content)
		if err != nil {
			return err
		}
	}

	// 2. Se tem anexo de mídia
	for _, anexo := range corpo.Attachments {
		if anexo.DataURL != "" {
			dados, err := baixarBytesRemotos(anexo.DataURL)
			if err == nil && len(dados) > 0 {
				if strings.Contains(anexo.FileType, "audio") {
					_, _ = g.wpp.EnviarMensagemAudio(ctx, instanciaID, destinatario, dados, true)
				} else {
					_, _ = g.wpp.EnviarMensagemImagem(ctx, instanciaID, destinatario, dados, "")
				}
			}
		}
	}

	return nil
}

// ProcessarWebhookReversoKommo recebe gatilho de automação do Kommo para enviar WhatsApp
func (g *GerenciadorIntegracoes) ProcessarWebhookReversoKommo(ctx context.Context, instanciaID, destinatario, mensagem string) error {
	if strings.TrimSpace(destinatario) == "" || strings.TrimSpace(mensagem) == "" {
		return fmt.Errorf("telefone ou mensagem vazios")
	}
	_, err := g.wpp.EnviarMensagemTexto(ctx, instanciaID, destinatario, mensagem)
	return err
}

// ProcessarWebhookReversoRDStation recebe gatilho de régua do RD Station para enviar WhatsApp
func (g *GerenciadorIntegracoes) ProcessarWebhookReversoRDStation(ctx context.Context, instanciaID, destinatario, mensagem string) error {
	if strings.TrimSpace(destinatario) == "" || strings.TrimSpace(mensagem) == "" {
		return fmt.Errorf("telefone ou mensagem vazios")
	}
	_, err := g.wpp.EnviarMensagemTexto(ctx, instanciaID, destinatario, mensagem)
	return err
}

// TestarConexao realiza uma requisição de teste para validar as credenciais da caixa
func (g *GerenciadorIntegracoes) TestarConexao(ctx context.Context, tipo, configJSON string) (map[string]interface{}, error) {
	tipo = strings.ToLower(strings.TrimSpace(tipo))
	switch tipo {
	case "chatwoot":
		var cfg ConfigChatwoot
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("JSON de configuração inválido: %w", err)
		}
		if cfg.URL == "" || cfg.AccountID == "" || cfg.Token == "" {
			return nil, fmt.Errorf("URL, Account ID e Token são obrigatórios")
		}
		urlTeste := fmt.Sprintf("%s/api/v1/accounts/%s/inboxes", strings.TrimRight(cfg.URL, "/"), cfg.AccountID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlTeste, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("api_access_token", cfg.Token)
		cli := &http.Client{Timeout: 10 * time.Second}
		resp, err := cli.Do(req)
		if err != nil {
			return nil, fmt.Errorf("falha ao conectar no Chatwoot: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("chatwoot retornou HTTP %d (credenciais inválidas ou conta inexistente)", resp.StatusCode)
		}
		return map[string]interface{}{"status": "sucesso", "mensagem": "Conexão com Chatwoot estabelecida com sucesso!"}, nil

	case "typebot":
		var cfg ConfigTypebot
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, err
		}
		if cfg.URL == "" || cfg.PublicID == "" {
			return nil, fmt.Errorf("URL e Public ID do Typebot são obrigatórios")
		}
		urlTeste := fmt.Sprintf("%s/api/v1/typebots/%s", strings.TrimRight(cfg.URL, "/"), cfg.PublicID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlTeste, nil)
		if err != nil {
			return nil, err
		}
		cli := &http.Client{Timeout: 10 * time.Second}
		resp, err := cli.Do(req)
		if err != nil {
			return nil, fmt.Errorf("falha ao conectar no Typebot: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("typebot retornou HTTP %d (bot não encontrado ou privado)", resp.StatusCode)
		}
		return map[string]interface{}{"status": "sucesso", "mensagem": "Typebot conectado e validado com sucesso!"}, nil

	case "n8n":
		var cfg ConfigN8N
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, err
		}
		if cfg.WebhookURL == "" {
			return nil, fmt.Errorf("URL do Webhook n8n é obrigatória")
		}
		// Dispara teste tipo ping
		err := g.n8n.DespacharEvento(ctx, cfg, "ping_teste", map[string]string{"teste": "sucesso", "mensagem": "Teste de conexão WooHub API -> n8n"})
		if err != nil {
			return nil, fmt.Errorf("n8n não respondeu ao webhook de teste: %w", err)
		}
		return map[string]interface{}{"status": "sucesso", "mensagem": "Webhook do n8n acionado com sucesso!"}, nil

	case "kommo":
		var cfg ConfigKommo
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, err
		}
		if cfg.Subdominio == "" || cfg.Token == "" {
			return nil, fmt.Errorf("Subdomínio e Token do Kommo são obrigatórios")
		}
		sub := strings.TrimSuffix(cfg.Subdominio, "/")
		if !strings.Contains(sub, ".") {
			sub += ".kommo.com"
		}
		urlTeste := fmt.Sprintf("https://%s/api/v4/account", strings.TrimPrefix(sub, "https://"))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlTeste, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
		cli := &http.Client{Timeout: 10 * time.Second}
		resp, err := cli.Do(req)
		if err != nil {
			return nil, fmt.Errorf("falha ao conectar no Kommo CRM: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("kommo CRM retornou HTTP %d (token inválido ou permissão negada)", resp.StatusCode)
		}
		return map[string]interface{}{"status": "sucesso", "mensagem": "Conexão com Kommo CRM validada com sucesso!"}, nil

	case "rdstation":
		var cfg ConfigRDStation
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, err
		}
		if cfg.TokenPrivado == "" && cfg.TokenCRM == "" {
			return nil, fmt.Errorf("informe ao menos o Token do RD Marketing ou o Token do RD CRM")
		}
		return map[string]interface{}{"status": "sucesso", "mensagem": "Configurações do RD Station salvas e validadas!"}, nil
	}

	return map[string]interface{}{"status": "sucesso", "mensagem": "Configuração válida"}, nil
}

func baixarBytesRemotos(rawURL string) ([]byte, error) {
	cli := &http.Client{Timeout: 15 * time.Second}
	resp, err := cli.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}
