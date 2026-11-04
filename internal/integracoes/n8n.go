package integracoes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type ClienteN8N struct {
	httpCliente *http.Client
}

func NovoClienteN8N() *ClienteN8N {
	return &ClienteN8N{
		httpCliente: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// DespacharEvento envia evento de mensagem, status ou conexão para o fluxo do n8n
func (c *ClienteN8N) DespacharEvento(ctx context.Context, cfg ConfigN8N, evento string, payload interface{}) error {
	if cfg.WebhookURL == "" {
		return nil
	}

	// Filtra se a caixa tiver lista restrita de eventos
	if len(cfg.Eventos) > 0 {
		permitido := false
		for _, e := range cfg.Eventos {
			if strings.EqualFold(e, evento) || e == "*" {
				permitido = true
				break
			}
		}
		if !permitido {
			return nil
		}
	}

	envelope := map[string]interface{}{
		"evento":     evento,
		"timestamp":  time.Now().Unix(),
		"data_hora":  time.Now().Format(time.RFC3339),
		"dados":      payload,
	}

	corpo, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("falha ao serializar payload n8n: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewBuffer(corpo))
	if err != nil {
		return fmt.Errorf("falha ao criar requisição para n8n: %w", err)
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "WooHub-API-n8n-Connector/1.0")

	if cfg.TokenSecreto != "" {
		req.Header.Set("X-N8N-Token", cfg.TokenSecreto)
		req.Header.Set("Authorization", "Bearer "+cfg.TokenSecreto)
	}

	resp, err := c.httpCliente.Do(req)
	if err != nil {
		log.Warn().Err(err).Str("url", cfg.WebhookURL).Msg("Erro ao disparar webhook para n8n")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Warn().Int("status", resp.StatusCode).Str("url", cfg.WebhookURL).Msg("n8n retornou status de erro")
		return fmt.Errorf("n8n retornou status %d", resp.StatusCode)
	}

	return nil
}
