package core

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type EventoWebhook string

const (
	EventoMensagemRecebida EventoWebhook = "mensagem_recebida"
	EventoStatusEnvio      EventoWebhook = "status_envio"
	EventoConexao          EventoWebhook = "conexao"
	EventoPresenca         EventoWebhook = "presenca"
	EventoQRCode           EventoWebhook = "qrcode"
	EventoChamada          EventoWebhook = "chamada_recebida"
	EventoVotoEnquete      EventoWebhook = "voto_enquete"
	EventoReacao           EventoWebhook = "reacao_mensagem"
	EventoGrupo            EventoWebhook = "atualizacao_grupo"
)

type PayloadWebhook struct {
	InstanciaID string        `json:"instancia_id"`
	Nome        string        `json:"nome_instancia"`
	Evento      EventoWebhook `json:"evento"`
	DataHora    string        `json:"data_hora"`
	Dados       interface{}   `json:"dados"`
}

type DespachanteWebhook struct {
	clienteHTTP *http.Client
	fila        chan itemFila
}

type itemFila struct {
	url       string
	segredo   string
	payload   PayloadWebhook
	tentativa int
}

var Despachante *DespachanteWebhook

// ValidarURLWebhook protege contra SSRF validando esquema e bloqueando IPs internos/metadata
func ValidarURLWebhook(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("URL de webhook inválida")
	}

	esquema := strings.ToLower(u.Scheme)
	if esquema != "http" && esquema != "https" {
		return fmt.Errorf("URL de webhook deve utilizar protocolo http:// ou https://")
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("hostname da URL de webhook é inválido")
	}

	// Permite localhost apenas se PERMITIR_WEBHOOK_LOCAL=true
	permitirLocal := strings.EqualFold(os.Getenv("PERMITIR_WEBHOOK_LOCAL"), "true")
	if !permitirLocal {
		hostsBloqueados := []string{"localhost", "postgres", "redis", "metadata.google.internal", "169.254.169.254"}
		for _, hb := range hostsBloqueados {
			if host == hb || strings.HasSuffix(host, "."+hb) {
				return fmt.Errorf("endereço de destino interno não permitido por política anti-SSRF: %s", host)
			}
		}

		if ip := net.ParseIP(host); ip != nil {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
				return fmt.Errorf("endereço IP privado ou loopback não permitido para webhook: %s", host)
			}
		}
	}

	return nil
}

func InicializarDespachanteWebhooks(tamanhoFila, numWorkers int) *DespachanteWebhook {
	d := &DespachanteWebhook{
		clienteHTTP: &http.Client{
			Timeout: 10 * time.Second,
		},
		fila: make(chan itemFila, tamanhoFila),
	}

	for i := 0; i < numWorkers; i++ {
		go d.trabalhador()
	}

	Despachante = d
	return d
}

func (d *DespachanteWebhook) Enviar(rawURL, segredo string, payload PayloadWebhook) {
	if d == nil || rawURL == "" {
		return
	}
	if err := ValidarURLWebhook(rawURL); err != nil {
		log.Warn().Str("url", rawURL).Err(err).Msg("Webhook bloqueado pela validação de segurança")
		return
	}
	payload.DataHora = time.Now().Format(time.RFC3339)

	select {
	case d.fila <- itemFila{url: rawURL, segredo: segredo, payload: payload, tentativa: 1}:
	default:
		log.Warn().Str("instancia", payload.InstanciaID).Msg("Fila de webhook cheia, descartando evento")
	}
}

func (d *DespachanteWebhook) trabalhador() {
	for item := range d.fila {
		d.processarItem(item)
	}
}

func (d *DespachanteWebhook) processarItem(item itemFila) {
	corpo, err := json.Marshal(item.payload)
	if err != nil {
		log.Error().Err(err).Msg("Erro ao serializar payload de webhook")
		return
	}

	req, err := http.NewRequest(http.MethodPost, item.url, bytes.NewBuffer(corpo))
	if err != nil {
		log.Error().Err(err).Str("url", item.url).Msg("Erro ao criar requisição de webhook")
		return
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "WooHub-API-Webhook/1.0 (Brasil)")

	if item.segredo != "" {
		mac := hmac.New(sha256.New, []byte(item.segredo))
		mac.Write(corpo)
		assinatura := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-WooHub-Signature-SHA256", assinatura)
	}

	resp, err := d.clienteHTTP.Do(req)
	status := 0
	if resp != nil {
		status = resp.StatusCode
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}

	if err != nil || status >= 400 {
		if item.tentativa < 3 {
			tempoEspera := time.Duration(item.tentativa*2) * time.Second
			go func(it itemFila) {
				time.Sleep(tempoEspera)
				it.tentativa++
				select {
				case d.fila <- it:
				default:
					log.Warn().Str("url", it.url).Msg("Fila cheia ao reenfileirar retentativa de webhook")
				}
			}(item)
		} else {
			log.Warn().
				Str("url", item.url).
				Int("status", status).
				Int("tentativa", item.tentativa).
				Msg("Falha ao entregar webhook após 3 tentativas")
		}
		return
	}
}
