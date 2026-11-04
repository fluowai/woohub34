package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/skip2/go-qrcode"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"go.mau.fi/whatsmeow/internal/database"
)

type InstanciaAtiva struct {
	ID              string
	Nome            string
	Cliente         *whatsmeow.Client
	UltimoQR        string
	UltimoStatus    string
	NumeroConectado string
	assinantesQR    map[chan string]struct{}
	loopQRAtivo     bool
	mu              sync.RWMutex
}

// ObterEstado retorna de forma thread-safe o status, último QR e número conectado
func (inst *InstanciaAtiva) ObterEstado() (status, ultimoQR, numero string) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return inst.UltimoStatus, inst.UltimoQR, inst.NumeroConectado
}

// AssinarQR registra um canal exclusivo para receber eventos SSE de QR Code sem condição de corrida
func (inst *InstanciaAtiva) AssinarQR() (<-chan string, func()) {
	ch := make(chan string, 8)
	inst.mu.Lock()
	if inst.assinantesQR == nil {
		inst.assinantesQR = make(map[chan string]struct{})
	}
	inst.assinantesQR[ch] = struct{}{}
	qrAtual := inst.UltimoQR
	inst.mu.Unlock()

	if qrAtual != "" {
		ch <- qrAtual
	}

	cancelar := func() {
		inst.mu.Lock()
		if _, ok := inst.assinantesQR[ch]; ok {
			delete(inst.assinantesQR, ch)
			close(ch)
		}
		inst.mu.Unlock()
	}
	return ch, cancelar
}

func (inst *InstanciaAtiva) publicarQR(qr string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for ch := range inst.assinantesQR {
		select {
		case ch <- qr:
		default:
		}
	}
}

type GerenciadorInstancias struct {
	container  *sqlstore.Container
	db         *database.GerenciadorDB
	webhook    *DespachanteWebhook
	instancias map[string]*InstanciaAtiva
	mu         sync.RWMutex
}

var Gerenciador *GerenciadorInstancias

func InicializarGerenciador(container *sqlstore.Container, db *database.GerenciadorDB, webhook *DespachanteWebhook) *GerenciadorInstancias {
	g := &GerenciadorInstancias{
		container:  container,
		db:         db,
		webhook:    webhook,
		instancias: make(map[string]*InstanciaAtiva),
	}
	Gerenciador = g
	return g
}

// ReconectarInstanciasSalvas reconecta automaticamente no boot todas as instâncias que possuem sessão salva no sqlstore
func (g *GerenciadorInstancias) ReconectarInstanciasSalvas(ctx context.Context) {
	todas, err := g.db.ListarInstancias()
	if err != nil {
		log.Warn().Err(err).Msg("Não foi possível listar instâncias para auto-reconexão inicial")
		return
	}

	for _, instDB := range todas {
		if instDB.JID == "" {
			if instDB.Status != "DESCONECTADO" {
				_ = g.db.AtualizarStatusInstancia(instDB.ID, "DESCONECTADO", "", instDB.NumeroTelefone)
			}
			continue
		}

		jid, parseErr := types.ParseJID(instDB.JID)
		if parseErr != nil {
			_ = g.db.AtualizarStatusInstancia(instDB.ID, "DESCONECTADO", "", "")
			continue
		}

		disp, devErr := g.container.GetDevice(ctx, jid)
		if devErr != nil || disp == nil {
			_ = g.db.AtualizarStatusInstancia(instDB.ID, "DESCONECTADO", "", "")
			continue
		}

		inst, err := g.ObterInstanciaAtiva(instDB.ID)
		if err != nil {
			continue
		}

		if err := inst.Cliente.Connect(); err != nil {
			log.Warn().Str("instancia", instDB.ID).Err(err).Msg("Falha ao reconectar instância salva no boot")
			_ = g.db.AtualizarStatusInstancia(instDB.ID, "DESCONECTADO", instDB.JID, instDB.NumeroTelefone)
		} else {
			log.Info().Str("instancia", instDB.ID).Str("nome", instDB.Nome).Msg("Instância reconectada automaticamente no boot")
		}
	}
}

// SincronizarStatusInstancias atualiza a lista de instâncias do banco com o status real em memória do socket WhatsApp
func (g *GerenciadorInstancias) SincronizarStatusInstancias(lista []database.Instancia) []database.Instancia {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for i := range lista {
		if ativa, ok := g.instancias[lista[i].ID]; ok && ativa != nil && ativa.Cliente != nil {
			if ativa.Cliente.IsConnected() && ativa.Cliente.Store != nil && ativa.Cliente.Store.ID != nil {
				lista[i].Status = "CONECTADO"
				if lista[i].NumeroTelefone == "" {
					lista[i].NumeroTelefone = ativa.Cliente.Store.ID.User
				}
			}
		}
	}
	return lista
}

// ContarInstanciasConectadas retorna o número real de instâncias WhatsApp conectadas em memória (usado pelo Heartbeat de Licença)
func (g *GerenciadorInstancias) ContarInstanciasConectadas() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	total := 0
	for _, ativa := range g.instancias {
		if ativa != nil && ativa.Cliente != nil && ativa.Cliente.IsConnected() {
			total++
		}
	}
	return total
}

// ObterInstanciaAtiva recupera ou inicializa a instância do banco
func (g *GerenciadorInstancias) ObterInstanciaAtiva(id string) (*InstanciaAtiva, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if inst, ok := g.instancias[id]; ok {
		return inst, nil
	}

	instDB, err := g.db.BuscarInstanciaPorId(id)
	if err != nil {
		return nil, fmt.Errorf("instância %s não encontrada no banco: %w", id, err)
	}

	var dispositivo *store.Device
	ctx := context.Background()

	if instDB.JID != "" {
		jid, parseErr := types.ParseJID(instDB.JID)
		if parseErr == nil {
			dispositivo, _ = g.container.GetDevice(ctx, jid)
		}
	}

	if dispositivo == nil {
		dispositivo = g.container.NewDevice()
	}

	cliente := whatsmeow.NewClient(dispositivo, nil)
	if instDB.ProxyURL != "" {
		if proxyErr := cliente.SetProxyAddress(instDB.ProxyURL); proxyErr != nil {
			log.Warn().Str("instancia", id).Str("proxy", instDB.ProxyURL).Err(proxyErr).Msg("Proxy configurado inválido na instância")
		}
	}

	inst := &InstanciaAtiva{
		ID:              id,
		Nome:            instDB.Nome,
		Cliente:         cliente,
		assinantesQR:    make(map[chan string]struct{}),
		UltimoStatus:    instDB.Status,
		NumeroConectado: instDB.NumeroTelefone,
	}

	cliente.AddEventHandler(func(evt interface{}) {
		g.tratarEventosConexao(evt, inst)
	})

	g.instancias[id] = inst
	return inst, nil
}

// Conectar inicia a escuta e garante que o loop de QR Code esteja ativo
func (g *GerenciadorInstancias) Conectar(ctx context.Context, id string) (*InstanciaAtiva, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return nil, err
	}

	// Se já está logado com sessão salva
	if inst.Cliente.Store.ID != nil {
		if !inst.Cliente.IsConnected() {
			if err := inst.Cliente.Connect(); err != nil {
				return nil, fmt.Errorf("erro ao reconectar sessão salva: %w", err)
			}
		}
		jidStr := inst.Cliente.Store.ID.String()
		numStr := inst.Cliente.Store.ID.User
		inst.mu.Lock()
		inst.UltimoStatus = "CONECTADO"
		inst.UltimoQR = ""
		inst.NumeroConectado = numStr
		inst.mu.Unlock()
		_ = g.db.AtualizarStatusInstancia(id, "CONECTADO", jidStr, numStr)
		return inst, nil
	}

	inst.mu.Lock()
	jaEmLoop := inst.loopQRAtivo
	inst.mu.Unlock()

	if jaEmLoop && inst.Cliente.IsConnected() {
		return inst, nil
	}

	if inst.Cliente.IsConnected() {
		inst.Cliente.Disconnect()
	}

	// Usa contexto de background para que o fechamento de uma aba SSE não derrube o canal de pareamento
	qrChan, err := inst.Cliente.GetQRChannel(context.Background())
	if err != nil {
		return nil, fmt.Errorf("erro ao obter canal de QR code: %w", err)
	}

	if err := inst.Cliente.Connect(); err != nil {
		return nil, fmt.Errorf("erro ao conectar socket WhatsApp: %w", err)
	}

	inst.mu.Lock()
	inst.loopQRAtivo = true
	inst.mu.Unlock()

	go func() {
		defer func() {
			inst.mu.Lock()
			inst.loopQRAtivo = false
			inst.mu.Unlock()
		}()

		for item := range qrChan {
			if item.Event == "code" {
				png, qrErr := qrcode.Encode(item.Code, qrcode.Medium, 256)
				var base64Img string
				if qrErr == nil {
					base64Img = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
				} else {
					base64Img = item.Code
				}

				inst.mu.Lock()
				inst.UltimoQR = base64Img
				inst.UltimoStatus = "AGUARDANDO_LEITURA"
				inst.mu.Unlock()

				_ = g.db.AtualizarStatusInstancia(id, "AGUARDANDO_LEITURA", "", "")
				inst.publicarQR(base64Img)

				instDB, _ := g.db.BuscarInstanciaPorId(id)
				if instDB != nil && instDB.WebhookURL != "" {
					g.webhook.Enviar(instDB.WebhookURL, instDB.WebhookSegredo, PayloadWebhook{
						InstanciaID: id,
						Nome:        inst.Nome,
						Evento:      EventoQRCode,
						Dados: map[string]string{
							"qrcode_base64": base64Img,
							"codigo_puro":   item.Code,
						},
					})
				}
			} else if item.Event == "success" {
				jidStr := ""
				numStr := ""
				if inst.Cliente.Store.ID != nil {
					jidStr = inst.Cliente.Store.ID.String()
					numStr = inst.Cliente.Store.ID.User
				}

				inst.mu.Lock()
				inst.UltimoStatus = "CONECTADO"
				inst.UltimoQR = ""
				inst.NumeroConectado = numStr
				inst.mu.Unlock()

				_ = g.db.AtualizarStatusInstancia(id, "CONECTADO", jidStr, numStr)
			}
		}
	}()

	return inst, nil
}

// GerarCodigoPareamento gera código de 8 dígitos para parear celular sem câmera
func (g *GerenciadorInstancias) GerarCodigoPareamento(ctx context.Context, id, numeroTelefone string) (string, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return "", err
	}

	numeroLimpo, err := NormalizarNumeroBrasil(numeroTelefone)
	if err != nil {
		return "", fmt.Errorf("número de telefone brasileiro inválido: %w", err)
	}

	if !inst.Cliente.IsConnected() {
		if _, err := g.Conectar(ctx, id); err != nil {
			return "", fmt.Errorf("erro ao conectar no WhatsApp: %w", err)
		}
	}

	codigo, err := inst.Cliente.PairPhone(ctx, numeroLimpo, true, whatsmeow.PairClientChrome, "Chrome (Windows)")
	if err != nil {
		return "", fmt.Errorf("falha ao solicitar código de pareamento: %w", err)
	}

	return codigo, nil
}

// Desconectar encerra a conexão da instância e limpa a sessão ativa se necessário
func (g *GerenciadorInstancias) Desconectar(id string) error {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return err
	}

	if inst.Cliente.IsConnected() {
		inst.Cliente.Disconnect()
	}

	inst.mu.Lock()
	inst.UltimoStatus = "DESCONECTADO"
	inst.UltimoQR = ""
	inst.mu.Unlock()

	jidStr := ""
	numStr := ""
	if inst.Cliente.Store != nil && inst.Cliente.Store.ID != nil {
		jidStr = inst.Cliente.Store.ID.String()
		numStr = inst.Cliente.Store.ID.User
	}

	return g.db.AtualizarStatusInstancia(id, "DESCONECTADO", jidStr, numStr)
}

// RemoverInstancia desconecta, apaga chaves criptográficas do sqlstore e remove da memória
func (g *GerenciadorInstancias) RemoverInstancia(ctx context.Context, id string) {
	g.mu.Lock()
	inst, ok := g.instancias[id]
	if ok {
		delete(g.instancias, id)
	}
	g.mu.Unlock()

	if ok && inst != nil && inst.Cliente != nil {
		if inst.Cliente.IsConnected() {
			inst.Cliente.Disconnect()
		}
		if inst.Cliente.Store != nil && inst.Cliente.Store.ID != nil {
			_ = inst.Cliente.Store.Delete(ctx)
		}
	}
}

// EnviarMensagemTexto envia texto simples com suporte ao nono dígito brasileiro e fallback
func (g *GerenciadorInstancias) EnviarMensagemTexto(ctx context.Context, id, destinatario, texto string) (string, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return "", err
	}

	if !inst.Cliente.IsConnected() {
		return "", fmt.Errorf("a instância %s não está conectada ao WhatsApp", id)
	}

	jid, err := ObterJIDPadrao(destinatario)
	if err != nil {
		return "", err
	}

	msg := &waProto.Message{
		Conversation: proto.String(texto),
	}

	resp, err := inst.Cliente.SendMessage(ctx, jid, msg)
	if err != nil {
		altNum, alterado := AlternarNonoDigitoBrasil(destinatario)
		if alterado {
			altJID, altErr := ObterJIDPadrao(altNum)
			if altErr == nil {
				resp, err = inst.Cliente.SendMessage(ctx, altJID, msg)
				if err == nil {
					_ = g.db.GravarLogMensagem(id, resp.ID, "texto", altNum, "ENVIADA", "ENTREGUE", "")
					return resp.ID, nil
				}
			}
		}

		_ = g.db.GravarLogMensagem(id, "", "texto", destinatario, "ENVIADA", "ERRO", err.Error())
		return "", fmt.Errorf("falha ao enviar mensagem: %w", err)
	}

	_ = g.db.GravarLogMensagem(id, resp.ID, "texto", destinatario, "ENVIADA", "ENTREGUE", "")
	return resp.ID, nil
}

// transcodificarParaOpusPTT converte qualquer áudio de entrada para OGG Opus (VOIP PTT) via FFmpeg se disponível
func transcodificarParaOpusPTT(ctx context.Context, entrada []byte) ([]byte, string) {
	if len(entrada) == 0 {
		return entrada, "audio/ogg; codecs=opus"
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil || ffmpegPath == "" {
		return entrada, "audio/ogg; codecs=opus"
	}

	ctxCmd, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctxCmd, ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-c:a", "libopus",
		"-b:a", "32k",
		"-vbr", "on",
		"-compression_level", "10",
		"-frame_duration", "60",
		"-application", "voip",
		"-ar", "48000",
		"-ac", "1",
		"-f", "ogg",
		"pipe:1",
	)

	cmd.Stdin = bytes.NewReader(entrada)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf

	if err := cmd.Run(); err == nil && outBuf.Len() > 0 {
		return outBuf.Bytes(), "audio/ogg; codecs=opus"
	}

	return entrada, "audio/ogg; codecs=opus"
}

// EnviarMensagemAudio envia áudio no formato PTT gravado nativo (microfone verde) com transcodificação FFmpeg
func (g *GerenciadorInstancias) EnviarMensagemAudio(ctx context.Context, id, destinatario string, dadosAudio []byte, ptt bool) (string, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return "", err
	}

	if !inst.Cliente.IsConnected() {
		return "", fmt.Errorf("a instância %s não está conectada ao WhatsApp", id)
	}

	jid, err := ObterJIDPadrao(destinatario)
	if err != nil {
		return "", err
	}

	dadosProcessados, mimeType := transcodificarParaOpusPTT(ctx, dadosAudio)

	upload, err := inst.Cliente.Upload(ctx, dadosProcessados, whatsmeow.MediaAudio)
	if err != nil {
		return "", fmt.Errorf("falha no upload do áudio para o WhatsApp: %w", err)
	}

	segundosEstimados := uint32(len(dadosProcessados) / 4000)
	if segundosEstimados < 1 {
		segundosEstimados = 2
	}

	msgAudio := &waProto.AudioMessage{
		URL:           proto.String(upload.URL),
		DirectPath:    proto.String(upload.DirectPath),
		MediaKey:      upload.MediaKey,
		Mimetype:      proto.String(mimeType),
		FileEncSHA256: upload.FileEncSHA256,
		FileSHA256:    upload.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(dadosProcessados))),
		Seconds:       proto.Uint32(segundosEstimados),
		PTT:           proto.Bool(ptt),
	}

	msg := &waProto.Message{
		AudioMessage: msgAudio,
	}

	resp, err := inst.Cliente.SendMessage(ctx, jid, msg)
	if err != nil {
		altNum, alterado := AlternarNonoDigitoBrasil(destinatario)
		if alterado {
			if altJID, altErr := ObterJIDPadrao(altNum); altErr == nil {
				if respAlt, errAlt := inst.Cliente.SendMessage(ctx, altJID, msg); errAlt == nil {
					_ = g.db.GravarLogMensagem(id, respAlt.ID, "audio", altNum, "ENVIADA", "ENTREGUE", "")
					return respAlt.ID, nil
				}
			}
		}
		_ = g.db.GravarLogMensagem(id, "", "audio", destinatario, "ENVIADA", "ERRO", err.Error())
		return "", fmt.Errorf("falha ao enviar áudio: %w", err)
	}

	_ = g.db.GravarLogMensagem(id, resp.ID, "audio", destinatario, "ENVIADA", "ENTREGUE", "")
	return resp.ID, nil
}

// EnviarMensagemImagem envia foto com legenda opcional e fallback do 9º dígito
func (g *GerenciadorInstancias) EnviarMensagemImagem(ctx context.Context, id, destinatario string, dadosImagem []byte, legenda string) (string, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return "", err
	}

	if !inst.Cliente.IsConnected() {
		return "", fmt.Errorf("a instância %s não está conectada", id)
	}

	jid, err := ObterJIDPadrao(destinatario)
	if err != nil {
		return "", err
	}

	mimeType := http.DetectContentType(dadosImagem)
	upload, err := inst.Cliente.Upload(ctx, dadosImagem, whatsmeow.MediaImage)
	if err != nil {
		return "", fmt.Errorf("falha no upload da imagem: %w", err)
	}

	msgImagem := &waProto.ImageMessage{
		URL:           proto.String(upload.URL),
		DirectPath:    proto.String(upload.DirectPath),
		MediaKey:      upload.MediaKey,
		Mimetype:      proto.String(mimeType),
		FileEncSHA256: upload.FileEncSHA256,
		FileSHA256:    upload.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(dadosImagem))),
		Caption:       proto.String(legenda),
	}

	msg := &waProto.Message{
		ImageMessage: msgImagem,
	}

	resp, err := inst.Cliente.SendMessage(ctx, jid, msg)
	if err != nil {
		altNum, alterado := AlternarNonoDigitoBrasil(destinatario)
		if alterado {
			if altJID, altErr := ObterJIDPadrao(altNum); altErr == nil {
				if respAlt, errAlt := inst.Cliente.SendMessage(ctx, altJID, msg); errAlt == nil {
					_ = g.db.GravarLogMensagem(id, respAlt.ID, "imagem", altNum, "ENVIADA", "ENTREGUE", "")
					return respAlt.ID, nil
				}
			}
		}
		_ = g.db.GravarLogMensagem(id, "", "imagem", destinatario, "ENVIADA", "ERRO", err.Error())
		return "", fmt.Errorf("falha ao disparar imagem: %w", err)
	}

	_ = g.db.GravarLogMensagem(id, resp.ID, "imagem", destinatario, "ENVIADA", "ENTREGUE", "")
	return resp.ID, nil
}

// tratarEventosConexao despacha notificações recebidas do WhatsApp e mantém o estado sincronizado no banco
func (g *GerenciadorInstancias) tratarEventosConexao(evt interface{}, inst *InstanciaAtiva) {
	switch v := evt.(type) {
	case *events.PairSuccess:
		jidStr := v.ID.String()
		numStr := v.ID.User
		inst.mu.Lock()
		inst.UltimoStatus = "CONECTADO"
		inst.UltimoQR = ""
		inst.NumeroConectado = numStr
		inst.mu.Unlock()
		_ = g.db.AtualizarStatusInstancia(inst.ID, "CONECTADO", jidStr, numStr)

	case *events.Connected:
		jidStr := ""
		numStr := ""
		if inst.Cliente != nil && inst.Cliente.Store != nil && inst.Cliente.Store.ID != nil {
			jidStr = inst.Cliente.Store.ID.String()
			numStr = inst.Cliente.Store.ID.User
		}
		inst.mu.Lock()
		inst.UltimoStatus = "CONECTADO"
		inst.UltimoQR = ""
		if numStr != "" {
			inst.NumeroConectado = numStr
		}
		inst.mu.Unlock()
		_ = g.db.AtualizarStatusInstancia(inst.ID, "CONECTADO", jidStr, numStr)

		instDB, err := g.db.BuscarInstanciaPorId(inst.ID)
		if err == nil && instDB != nil {
			if instDB.SempreOnline && inst.Cliente != nil {
				_ = inst.Cliente.SendPresence(context.Background(), types.PresenceAvailable)
			}
			if instDB.WebhookURL != "" {
				g.webhook.Enviar(instDB.WebhookURL, instDB.WebhookSegredo, PayloadWebhook{
					InstanciaID: inst.ID,
					Nome:        inst.Nome,
					Evento:      EventoConexao,
					Dados: map[string]string{
						"status": "CONECTADO",
						"numero": numStr,
					},
				})
			}
		}

	case *events.LoggedOut:
		inst.mu.Lock()
		inst.UltimoStatus = "DESCONECTADO"
		inst.UltimoQR = ""
		inst.NumeroConectado = ""
		inst.mu.Unlock()
		_ = g.db.AtualizarStatusInstancia(inst.ID, "DESCONECTADO", "", "")

		instDB, err := g.db.BuscarInstanciaPorId(inst.ID)
		if err == nil && instDB.WebhookURL != "" {
			g.webhook.Enviar(instDB.WebhookURL, instDB.WebhookSegredo, PayloadWebhook{
				InstanciaID: inst.ID,
				Nome:        inst.Nome,
				Evento:      EventoConexao,
				Dados: map[string]string{
					"status": "DESCONECTADO",
					"motivo": v.Reason.String(),
				},
			})
		}

	case *events.CallOffer:
		g.tratarEventoChamada(v, inst)

	case *events.ChatPresence:
		instDB, err := g.db.BuscarInstanciaPorId(inst.ID)
		if err == nil && instDB != nil && instDB.WebhookURL != "" {
			g.webhook.Enviar(instDB.WebhookURL, instDB.WebhookSegredo, PayloadWebhook{
				InstanciaID: inst.ID,
				Nome:        inst.Nome,
				Evento:      EventoPresenca,
				Dados: map[string]interface{}{
					"chat":      v.Chat.String(),
					"remetente": v.Sender.String(),
					"estado":    string(v.State),
					"midia":     string(v.Media),
				},
			})
		}

	case *events.Message:
		g.tratarEventoMensagemCompleta(v, inst)

	case *events.Receipt:
		instDB, err := g.db.BuscarInstanciaPorId(inst.ID)
		if err != nil || instDB.WebhookURL == "" {
			return
		}
		tipoStatus := "ENTREGUE"
		if v.Type == events.ReceiptTypeRead {
			tipoStatus = "LIDA"
		} else if v.Type == events.ReceiptTypePlayed {
			tipoStatus = "REPRODUZIDA"
		}
		g.webhook.Enviar(instDB.WebhookURL, instDB.WebhookSegredo, PayloadWebhook{
			InstanciaID: inst.ID,
			Nome:        inst.Nome,
			Evento:      EventoStatusEnvio,
			Dados: map[string]interface{}{
				"ids_mensagens": v.MessageIDs,
				"destinatario":  v.Chat.String(),
				"status":        tipoStatus,
				"timestamp":     v.Timestamp.Unix(),
			},
		})
	}
}
