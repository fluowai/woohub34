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

type ClienteTypebot struct {
	httpCliente *http.Client
}

func NovoClienteTypebot() *ClienteTypebot {
	return &ClienteTypebot{
		httpCliente: &http.Client{
			Timeout: 25 * time.Second,
		},
	}
}

// RespostaTypebot encapsula as mensagens e opções retornadas pelo bot
type RespostaTypebot struct {
	SessionID string           `json:"sessionId"`
	Mensagens []MensagemBot    `json:"messages"`
	Input     *InputBot        `json:"input,omitempty"`
}

type MensagemBot struct {
	ID      string      `json:"id"`
	Type    string      `json:"type"` // "text", "image", "audio", "video"
	Content interface{} `json:"content"`
}

type InputBot struct {
	ID    string     `json:"id"`
	Type  string     `json:"type"` // "text input", "choice input", "number input", "email input"
	Items []ItemChoice `json:"items,omitempty"`
}

type ItemChoice struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// IniciarSessao inicia um novo fluxo no Typebot passando variáveis de lead e campanha
func (c *ClienteTypebot) IniciarSessao(ctx context.Context, baseURL, typebotID string, variaveis map[string]interface{}) (*RespostaTypebot, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	urlInicio := fmt.Sprintf("%s/api/v1/typebots/%s/startChat", baseURL, typebotID)

	corpo := map[string]interface{}{
		"isStream":           false,
		"prefilledVariables": variaveis,
	}

	b, _ := json.Marshal(corpo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlInicio, bytes.NewBuffer(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpCliente.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		corpoErr, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("typebot startChat falhou (HTTP %d): %s", resp.StatusCode, string(corpoErr))
	}

	var res RespostaTypebot
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	return &res, nil
}

// ContinuarSessao envia a resposta do usuário para avançar no fluxo do Typebot
func (c *ClienteTypebot) ContinuarSessao(ctx context.Context, baseURL, sessionID, mensagem string) (*RespostaTypebot, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	urlContinue := fmt.Sprintf("%s/api/v1/sessions/%s/continueChat", baseURL, sessionID)

	corpo := map[string]interface{}{
		"message": mensagem,
	}

	b, _ := json.Marshal(corpo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlContinue, bytes.NewBuffer(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpCliente.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		corpoErr, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("typebot continueChat falhou (HTTP %d): %s", resp.StatusCode, string(corpoErr))
	}

	var res RespostaTypebot
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	res.SessionID = sessionID

	return &res, nil
}

// ExtrairTextoMensagem extrai a string legível do conteúdo (seja texto puro, richText ou HTML)
func ExtrairTextoMensagemTypebot(content interface{}) string {
	if content == nil {
		return ""
	}
	if str, ok := content.(string); ok {
		return str
	}

	if mapa, ok := content.(map[string]interface{}); ok {
		if txt, ok := mapa["text"].(string); ok && txt != "" {
			return txt
		}
		if url, ok := mapa["url"].(string); ok && url != "" {
			return url
		}
		if rich, ok := mapa["richText"].([]interface{}); ok {
			var sb strings.Builder
			extrairNosRichText(rich, &sb)
			return strings.TrimSpace(sb.String())
		}
	}

	return ""
}

func extrairNosRichText(nos []interface{}, sb *strings.Builder) {
	for _, n := range nos {
		if noMapa, ok := n.(map[string]interface{}); ok {
			if txt, ok := noMapa["text"].(string); ok {
				sb.WriteString(txt)
			}
			if filhos, ok := noMapa["children"].([]interface{}); ok {
				extrairNosRichText(filhos, sb)
			}
		}
	}
}
