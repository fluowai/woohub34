package integracoes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ClienteChatwoot struct {
	httpCliente *http.Client
}

func NovoClienteChatwoot() *ClienteChatwoot {
	return &ClienteChatwoot{
		httpCliente: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// EnviarMensagemRecebida sincroniza a mensagem do WhatsApp para dentro do Chatwoot
func (c *ClienteChatwoot) EnviarMensagemRecebida(ctx context.Context, cfg ConfigChatwoot, payload MensagemRecebidaPayload) error {
	if cfg.URL == "" || cfg.AccountID == "" || cfg.Token == "" {
		return fmt.Errorf("configuração do Chatwoot incompleta")
	}

	baseURL := strings.TrimRight(cfg.URL, "/")

	// 1. Resolver ou Obter Contato no Chatwoot
	contatoID, err := c.obterOuCriarContato(ctx, baseURL, cfg, payload)
	if err != nil {
		return fmt.Errorf("erro ao obter/criar contato no Chatwoot: %w", err)
	}

	// 2. Resolver ou Obter Conversa Aberta
	conversaID, err := c.obterOuCriarConversa(ctx, baseURL, cfg, contatoID, payload)
	if err != nil {
		return fmt.Errorf("erro ao obter/criar conversa no Chatwoot: %w", err)
	}

	// 3. Montar Mensagem
	textoFinal := payload.Texto
	if textoFinal == "" && payload.TemMidia {
		textoFinal = fmt.Sprintf("[%s]", strings.ToUpper(payload.Tipo))
	}
	if payload.URLMidia != "" {
		textoFinal += fmt.Sprintf("\n📎 Mídia: %s", payload.URLMidia)
	}

	urlMensagem := fmt.Sprintf("%s/api/v1/accounts/%s/conversations/%d/messages", baseURL, cfg.AccountID, conversaID)
	corpoMsg := map[string]interface{}{
		"content":      textoFinal,
		"message_type": "incoming",
		"private":      false,
	}

	dadosJSON, _ := json.Marshal(corpoMsg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlMensagem, bytes.NewBuffer(dadosJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("api_access_token", cfg.Token)

	resp, err := c.httpCliente.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respCorpo, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chatwoot retornou status %d: %s", resp.StatusCode, string(respCorpo))
	}

	return nil
}

func (c *ClienteChatwoot) obterOuCriarContato(ctx context.Context, baseURL string, cfg ConfigChatwoot, payload MensagemRecebidaPayload) (int, error) {
	telefoneBusca := strings.TrimPrefix(payload.RemetenteBR, "+")
	if strings.Contains(payload.RemetenteJID, "@") {
		partes := strings.Split(payload.RemetenteJID, "@")
		telefoneBusca = partes[0]
	}
	telefoneE164 := "+" + strings.TrimPrefix(telefoneBusca, "+")

	// Busca se o contato já existe
	urlBusca := fmt.Sprintf("%s/api/v1/accounts/%s/contacts/search?q=%s", baseURL, cfg.AccountID, telefoneBusca)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlBusca, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("api_access_token", cfg.Token)

	resp, err := c.httpCliente.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var resBusca struct {
			Payload []struct {
				ID          int    `json:"id"`
				PhoneNumber string `json:"phone_number"`
			} `json:"payload"`
		}
		if json.NewDecoder(resp.Body).Decode(&resBusca) == nil && len(resBusca.Payload) > 0 {
			return resBusca.Payload[0].ID, nil
		}
	} else if resp != nil {
		resp.Body.Close()
	}

	// Criar novo contato
	urlCriar := fmt.Sprintf("%s/api/v1/accounts/%s/contacts", baseURL, cfg.AccountID)
	nome := payload.NomeContato
	if strings.TrimSpace(nome) == "" {
		nome = payload.RemetenteBR
	}

	customAttrs := make(map[string]interface{})
	if payload.Origem != nil {
		if mapaOrigem, ok := payload.Origem.(map[string]interface{}); ok {
			for k, v := range mapaOrigem {
				customAttrs[k] = v
			}
		}
	}

	inboxIDInt, _ := strconv.Atoi(cfg.InboxID)

	corpoCriar := map[string]interface{}{
		"name":              nome,
		"phone_number":      telefoneE164,
		"custom_attributes": customAttrs,
	}
	if inboxIDInt > 0 {
		corpoCriar["inbox_id"] = inboxIDInt
	}

	dadosJSON, _ := json.Marshal(corpoCriar)
	reqCriar, err := http.NewRequestWithContext(ctx, http.MethodPost, urlCriar, bytes.NewBuffer(dadosJSON))
	if err != nil {
		return 0, err
	}
	reqCriar.Header.Set("Content-Type", "application/json; charset=utf-8")
	reqCriar.Header.Set("api_access_token", cfg.Token)

	respCriar, err := c.httpCliente.Do(reqCriar)
	if err != nil {
		return 0, err
	}
	defer respCriar.Body.Close()

	var resCriado struct {
		Payload struct {
			Contact struct {
				ID int `json:"id"`
			} `json:"contact"`
			ID int `json:"id"`
		} `json:"payload"`
	}

	if err := json.NewDecoder(respCriar.Body).Decode(&resCriado); err != nil {
		return 0, err
	}

	cid := resCriado.Payload.Contact.ID
	if cid == 0 {
		cid = resCriado.Payload.ID
	}
	if cid == 0 {
		return 0, fmt.Errorf("não foi possível obter ID do contato criado no Chatwoot")
	}

	return cid, nil
}

func (c *ClienteChatwoot) obterOuCriarConversa(ctx context.Context, baseURL string, cfg ConfigChatwoot, contatoID int, payload MensagemRecebidaPayload) (int, error) {
	inboxIDInt, _ := strconv.Atoi(cfg.InboxID)

	// Listar conversas existentes do contato
	urlConversas := fmt.Sprintf("%s/api/v1/accounts/%s/contacts/%d/conversations", baseURL, cfg.AccountID, contatoID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlConversas, nil)
	if err == nil {
		req.Header.Set("api_access_token", cfg.Token)
		if resp, err := c.httpCliente.Do(req); err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var resConv struct {
				Payload []struct {
					ID      int    `json:"id"`
					Status  string `json:"status"`
					InboxID int    `json:"inbox_id"`
				} `json:"payload"`
			}
			if json.NewDecoder(resp.Body).Decode(&resConv) == nil && len(resConv.Payload) > 0 {
				for _, conv := range resConv.Payload {
					if inboxIDInt > 0 && conv.InboxID != inboxIDInt {
						continue
					}
					if conv.Status == "open" || conv.Status == "pending" {
						return conv.ID, nil
					}
					// Se permitir reabertura
					if cfg.ReabrirConversa && conv.Status == "resolved" {
						_ = c.reabrirConversa(ctx, baseURL, cfg, conv.ID)
						return conv.ID, nil
					}
				}
			}
		} else if resp != nil {
			resp.Body.Close()
		}
	}

	// Cria nova conversa
	urlNova := fmt.Sprintf("%s/api/v1/accounts/%s/conversations", baseURL, cfg.AccountID)
	corpoNova := map[string]interface{}{
		"contact_id": contatoID,
		"source_id":  payload.ChatJID,
		"status":     "open",
	}
	if inboxIDInt > 0 {
		corpoNova["inbox_id"] = inboxIDInt
	}

	dadosJSON, _ := json.Marshal(corpoNova)
	reqNova, err := http.NewRequestWithContext(ctx, http.MethodPost, urlNova, bytes.NewBuffer(dadosJSON))
	if err != nil {
		return 0, err
	}
	reqNova.Header.Set("Content-Type", "application/json; charset=utf-8")
	reqNova.Header.Set("api_access_token", cfg.Token)

	respNova, err := c.httpCliente.Do(reqNova)
	if err != nil {
		return 0, err
	}
	defer respNova.Body.Close()

	var resNova struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(respNova.Body).Decode(&resNova); err != nil {
		return 0, err
	}

	if resNova.ID == 0 {
		return 0, fmt.Errorf("conversa criada sem ID retornado pelo Chatwoot")
	}

	return resNova.ID, nil
}

func (c *ClienteChatwoot) reabrirConversa(ctx context.Context, baseURL string, cfg ConfigChatwoot, conversaID int) error {
	urlToggle := fmt.Sprintf("%s/api/v1/accounts/%s/conversations/%d/toggle_status", baseURL, cfg.AccountID, conversaID)
	corpo := map[string]string{"status": "open"}
	b, _ := json.Marshal(corpo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlToggle, bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("api_access_token", cfg.Token)
	resp, err := c.httpCliente.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// PayloadWebhookChatwoot representa o evento emitido pelo Chatwoot quando o agente responde
type PayloadWebhookChatwoot struct {
	Event       string `json:"event"`        // "message_created"
	MessageType string `json:"message_type"` // "outgoing"
	Private     bool   `json:"private"`
	Content     string `json:"content"`
	Conversation struct {
		ID int `json:"id"`
		Meta struct {
			Sender struct {
				PhoneNumber string `json:"phone_number"`
				Name        string `json:"name"`
			} `json:"sender"`
		} `json:"meta"`
		ContactInbox struct {
			SourceID string `json:"source_id"` // WhatsApp JID
		} `json:"contact_inbox"`
	} `json:"conversation"`
	Attachments []struct {
		DataURL  string `json:"data_url"`
		FileType string `json:"file_type"`
	} `json:"attachments"`
}
