package integracoes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type ClienteKommo struct {
	httpCliente *http.Client
}

func NovoClienteKommo() *ClienteKommo {
	return &ClienteKommo{
		httpCliente: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// SincronizarLeadECadastro gerencia a criação de contato e lead no Kommo CRM
func (c *ClienteKommo) SincronizarLeadECadastro(ctx context.Context, cfg ConfigKommo, payload MensagemRecebidaPayload) error {
	if cfg.Subdominio == "" || cfg.Token == "" {
		return fmt.Errorf("configuração do Kommo CRM incompleta")
	}

	subdominio := strings.TrimSuffix(cfg.Subdominio, "/")
	if !strings.Contains(subdominio, ".") {
		subdominio = subdominio + ".kommo.com"
	}
	baseURL := "https://" + strings.TrimPrefix(subdominio, "https://")

	// 1. Obter ou Criar Contato
	contatoID, err := c.obterOuCriarContato(ctx, baseURL, cfg.Token, payload)
	if err != nil {
		return fmt.Errorf("erro ao obter/criar contato no Kommo: %w", err)
	}

	// 2. Se configurado para criar lead automático
	if cfg.CriarLeadAutomatico {
		leadID, err := c.criarLead(ctx, baseURL, cfg, contatoID, payload)
		if err != nil {
			log.Warn().Err(err).Msg("Aviso ao criar lead no Kommo CRM")
		} else if cfg.SincronizarNotas && leadID > 0 {
			_ = c.adicionarNota(ctx, baseURL, cfg.Token, leadID, payload)
		}
	}

	return nil
}

func (c *ClienteKommo) obterOuCriarContato(ctx context.Context, baseURL, token string, payload MensagemRecebidaPayload) (int, error) {
	telefoneLimpo := strings.TrimPrefix(payload.RemetenteBR, "+")
	if strings.Contains(payload.RemetenteJID, "@") {
		partes := strings.Split(payload.RemetenteJID, "@")
		telefoneLimpo = partes[0]
	}

	// Busca por telefone
	urlBusca := fmt.Sprintf("%s/api/v4/contacts?query=%s", baseURL, url.QueryEscape(telefoneLimpo))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlBusca, nil)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+token)
		if resp, err := c.httpCliente.Do(req); err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var resBusca struct {
				Embedded struct {
					Contacts []struct {
						ID int `json:"id"`
					} `json:"contacts"`
				} `json:"_embedded"`
			}
			if json.NewDecoder(resp.Body).Decode(&resBusca) == nil && len(resBusca.Embedded.Contacts) > 0 {
				return resBusca.Embedded.Contacts[0].ID, nil
			}
		} else if resp != nil {
			resp.Body.Close()
		}
	}

	// Criar novo contato
	nome := payload.NomeContato
	if strings.TrimSpace(nome) == "" {
		nome = payload.RemetenteBR
	}

	urlCriar := fmt.Sprintf("%s/api/v4/contacts", baseURL)
	corpoContato := []map[string]interface{}{
		{
			"name": nome,
			"custom_fields_values": []map[string]interface{}{
				{
					"field_code": "PHONE",
					"values": []map[string]interface{}{
						{
							"value":     "+" + strings.TrimPrefix(telefoneLimpo, "+"),
							"enum_code": "MOB",
						},
					},
				},
			},
		},
	}

	b, _ := json.Marshal(corpoContato)
	reqCriar, err := http.NewRequestWithContext(ctx, http.MethodPost, urlCriar, bytes.NewBuffer(b))
	if err != nil {
		return 0, err
	}
	reqCriar.Header.Set("Authorization", "Bearer "+token)
	reqCriar.Header.Set("Content-Type", "application/json; charset=utf-8")

	respCriar, err := c.httpCliente.Do(reqCriar)
	if err != nil {
		return 0, err
	}
	defer respCriar.Body.Close()

	if respCriar.StatusCode >= 400 {
		corpoErr, _ := io.ReadAll(respCriar.Body)
		return 0, fmt.Errorf("kommo erro criar contato: %s", string(corpoErr))
	}

	var resCriar struct {
		Embedded struct {
			Contacts []struct {
				ID int `json:"id"`
			} `json:"contacts"`
		} `json:"_embedded"`
	}
	if err := json.NewDecoder(respCriar.Body).Decode(&resCriar); err != nil || len(resCriar.Embedded.Contacts) == 0 {
		return 0, fmt.Errorf("resposta invalida ao criar contato no Kommo")
	}

	return resCriar.Embedded.Contacts[0].ID, nil
}

func (c *ClienteKommo) criarLead(ctx context.Context, baseURL string, cfg ConfigKommo, contatoID int, payload MensagemRecebidaPayload) (int, error) {
	urlLeads := fmt.Sprintf("%s/api/v4/leads", baseURL)

	nomeLead := fmt.Sprintf("WhatsApp - %s", payload.RemetenteBR)
	if payload.NomeContato != "" {
		nomeLead = fmt.Sprintf("WhatsApp - %s (%s)", payload.NomeContato, payload.RemetenteBR)
	}

	tags := []map[string]string{
		{"name": "WhatsApp"},
	}

	// Adiciona tags de origem
	if payload.Origem != nil {
		if mapa, ok := payload.Origem.(map[string]interface{}); ok {
			if canal, ok := mapa["canal"].(string); ok && canal != "" {
				tags = append(tags, map[string]string{"name": "Origem: " + canal})
			}
			if camp, ok := mapa["campanha"].(string); ok && camp != "" {
				tags = append(tags, map[string]string{"name": "Campanha: " + camp})
			}
		}
	}

	itemLead := map[string]interface{}{
		"name": nomeLead,
		"_embedded": map[string]interface{}{
			"contacts": []map[string]interface{}{
				{"id": contatoID},
			},
			"tags": tags,
		},
	}

	if cfg.PipelineID > 0 {
		itemLead["pipeline_id"] = cfg.PipelineID
	}
	if cfg.StatusID > 0 {
		itemLead["status_id"] = cfg.StatusID
	}

	b, _ := json.Marshal([]map[string]interface{}{itemLead})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlLeads, bytes.NewBuffer(b))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpCliente.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		corpoErr, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("kommo erro criar lead: %s", string(corpoErr))
	}

	var resLeads struct {
		Embedded struct {
			Leads []struct {
				ID int `json:"id"`
			} `json:"leads"`
		} `json:"_embedded"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&resLeads); err == nil && len(resLeads.Embedded.Leads) > 0 {
		return resLeads.Embedded.Leads[0].ID, nil
	}

	return 0, nil
}

func (c *ClienteKommo) adicionarNota(ctx context.Context, baseURL, token string, leadID int, payload MensagemRecebidaPayload) error {
	urlNotas := fmt.Sprintf("%s/api/v4/leads/%d/notes", baseURL, leadID)

	textoNota := fmt.Sprintf("Mensagem WhatsApp Recebida:\n%s", payload.Texto)
	if payload.URLMidia != "" {
		textoNota += fmt.Sprintf("\nMídia: %s", payload.URLMidia)
	}

	corpo := []map[string]interface{}{
		{
			"note_type": "common",
			"params": map[string]string{
				"text": textoNota,
			},
		},
	}

	b, _ := json.Marshal(corpo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlNotas, bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpCliente.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
