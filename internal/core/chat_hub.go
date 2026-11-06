package core

import (
	"sync"
	"time"

	"go.mau.fi/whatsmeow/internal/database"
)

// EventoChatWeb representa qualquer evento transmitido para o WhatsApp Web
type EventoChatWeb struct {
	Tipo        string                 `json:"tipo"` // nova_mensagem, status_mensagem, mensagem_deletada, presenca, conexao
	InstanciaID string                 `json:"instancia_id"`
	Timestamp   int64                  `json:"timestamp"`
	Mensagem    *database.MensagemChat `json:"mensagem,omitempty"`
	Conversa    *database.ConversaChat `json:"conversa,omitempty"`
	Dados       map[string]interface{} `json:"dados,omitempty"`
}

// HubChatWeb faz o roteamento SSE das mensagens em tempo real para os navegadores conectados
type HubChatWeb struct {
	mu         sync.RWMutex
	assinantes map[string]map[chan EventoChatWeb]struct{}
}

// HubChatGlobal é o despachante singleton do Web Chat
var HubChatGlobal = NovoHubChatWeb()

func NovoHubChatWeb() *HubChatWeb {
	return &HubChatWeb{
		assinantes: make(map[string]map[chan EventoChatWeb]struct{}),
	}
}

// Assinar registra uma nova conexão SSE de um navegador para uma instância
func (h *HubChatWeb) Assinar(instanciaID string) chan EventoChatWeb {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan EventoChatWeb, 120)
	if _, ok := h.assinantes[instanciaID]; !ok {
		h.assinantes[instanciaID] = make(map[chan EventoChatWeb]struct{})
	}
	h.assinantes[instanciaID][ch] = struct{}{}
	return ch
}

// Desassinar encerra a assinatura de um canal quando o navegador desconecta
func (h *HubChatWeb) Desassinar(instanciaID string, ch chan EventoChatWeb) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if subs, ok := h.assinantes[instanciaID]; ok {
		delete(subs, ch)
		close(ch)
		if len(subs) == 0 {
			delete(h.assinantes, instanciaID)
		}
	}
}

// Transmitir envia um evento para todos os operadores conectados na instância
func (h *HubChatWeb) Transmitir(evento EventoChatWeb) {
	if evento.Timestamp == 0 {
		evento.Timestamp = time.Now().Unix()
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	subs, ok := h.assinantes[evento.InstanciaID]
	if !ok || len(subs) == 0 {
		return
	}

	for ch := range subs {
		select {
		case ch <- evento:
		default:
			// Canal do cliente ocupado ou com lag: descarta suavemente
		}
	}
}
