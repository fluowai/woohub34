package integracoes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ClienteRDStation struct {
	httpCliente *http.Client
}

func NovoClienteRDStation() *ClienteRDStation {
	return &ClienteRDStation{
		httpCliente: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// SincronizarLeadMarketing envia evento de conversão para o RD Station Marketing
func (c *ClienteRDStation) SincronizarLeadMarketing(ctx context.Context, cfg ConfigRDStation, payload MensagemRecebidaPayload) error {
	if cfg.TokenPrivado == "" {
		return nil
	}

	urlConversao := "https://api.rd.services/platform/conversions"

	identificador := cfg.IdentificadorConversao
	if identificador == "" {
		identificador = "whatsapp-inbound"
	}

	nomeLead := payload.NomeContato
	if strings.TrimSpace(nomeLead) == "" {
		nomeLead = payload.RemetenteBR
	}

	telefone := payload.RemetenteBR
	if !strings.HasPrefix(telefone, "+") {
		telefone = "+" + strings.TrimPrefix(telefone, "+")
	}

	tags := append([]string{"whatsapp"}, cfg.Tags...)

	dadosPayload := map[string]interface{}{
		"conversion_identifier": identificador,
		"name":                  nomeLead,
		"mobile_phone":          telefone,
		"tags":                  tags,
	}

	// Adiciona Atribuição de Origem e Parâmetros de Campanha
	if payload.Origem != nil {
		if mapa, ok := payload.Origem.(map[string]interface{}); ok {
			if canal, ok := mapa["canal"].(string); ok && canal != "" {
				dadosPayload["traffic_source"] = canal
			}
			if camp, ok := mapa["campanha"].(string); ok && camp != "" {
				dadosPayload["traffic_campaign"] = camp
			}
			if med, ok := mapa["utm_medium"].(string); ok && med != "" {
				dadosPayload["traffic_medium"] = med
			}
			if term, ok := mapa["utm_term"].(string); ok && term != "" {
				dadosPayload["traffic_value"] = term
			}
		}
	}

	corpo := map[string]interface{}{
		"event_type":   "CONVERSION",
		"event_family": "CDP",
		"payload":      dadosPayload,
	}

	b, _ := json.Marshal(corpo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlConversao, bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.TokenPrivado)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpCliente.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		corpoErr, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("rd station marketing retornou erro %d: %s", resp.StatusCode, string(corpoErr))
	}

	// 2. Se configurado RD Station CRM
	if cfg.CriarOportunidadeCRM && cfg.TokenCRM != "" {
		_ = c.criarOportunidadeCRM(ctx, cfg.TokenCRM, nomeLead, telefone, dadosPayload)
	}

	return nil
}

func (c *ClienteRDStation) criarOportunidadeCRM(ctx context.Context, tokenCRM, nome, telefone string, meta map[string]interface{}) error {
	urlCRM := fmt.Sprintf("https://crm.rdstation.com/api/v1/deals?token=%s", tokenCRM)

	corpo := map[string]interface{}{
		"deal": map[string]interface{}{
			"name": fmt.Sprintf("Oportunidade WhatsApp - %s", nome),
		},
		"contacts": []map[string]interface{}{
			{
				"name":   nome,
				"phones": []map[string]string{{"phone": telefone, "type": "cellphone"}},
			},
		},
	}

	b, _ := json.Marshal(corpo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlCRM, bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpCliente.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
