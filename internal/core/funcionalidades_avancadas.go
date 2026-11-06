package core

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"go.mau.fi/whatsmeow/internal/config"
	"go.mau.fi/whatsmeow/internal/database"
	"go.mau.fi/whatsmeow/internal/integracoes"
	"go.mau.fi/whatsmeow/internal/storage"
)

// OpcoesEnvio unifica opções avançadas de mensageria (Quote/Reply, Menções, Fantasma, ViewOnce, Delay Humanizado, Encaminhamento, Link Preview)
type OpcoesEnvio struct {
	ResponderMensagemID string   `json:"responder_mensagem_id,omitempty"`
	ResponderRemetente  string   `json:"responder_remetente,omitempty"`
	TextoCitado         string   `json:"texto_citado,omitempty"`
	Mencionar           []string `json:"mencionar,omitempty"`
	MencionarTodos      bool     `json:"mencionar_todos,omitempty"`
	VisualizacaoUnica   bool     `json:"visualizacao_unica,omitempty"`
	DelayDigitandoMs    int      `json:"delay_digitando_ms,omitempty"`
	Encaminhada         bool     `json:"encaminhada,omitempty"`
	EncaminhadaFrequente bool    `json:"encaminhada_frequente,omitempty"`
	ExpiracaoSegundos   uint32   `json:"expiracao_segundos,omitempty"`
	TituloLink          string   `json:"titulo_link,omitempty"`
	DescricaoLink       string   `json:"descricao_link,omitempty"`
	UrlLink             string   `json:"url_link,omitempty"`
}

// BotaoInterativo representa um botão Native Flow no padrão moderno do WhatsApp (CTA URL, Pix Copia e Cola, Quick Reply)
type BotaoInterativo struct {
	Tipo  string `json:"tipo"`            // "url", "copiar", "resposta", "chamada" ou "cta_url", "cta_copy", "quick_reply", "cta_call"
	Texto string `json:"texto"`           // Texto visível do botão
	Valor string `json:"valor"`           // URL do site, código Pix/cupom, ID da resposta ou telefone
	Id    string `json:"id,omitempty"`    // ID customizado do botão (para quick_reply)
}

// ContatoCartao representa um contato para geração automática de vCard 3.0 ou envio direto
type ContatoCartao struct {
	Nome     string `json:"nome"`
	Telefone string `json:"telefone"`
	Empresa  string `json:"empresa,omitempty"`
	Email    string `json:"email,omitempty"`
	Vcard    string `json:"vcard,omitempty"`
}

// ConfiguracaoGrupoReq representa os parâmetros para atualizar configurações de um grupo
type ConfiguracaoGrupoReq struct {
	Nome                *string `json:"nome,omitempty"`
	Descricao           *string `json:"descricao,omitempty"`
	ApenasAdminsFalam   *bool   `json:"apenas_admins_falam,omitempty"`
	DadosTravados       *bool   `json:"dados_travados,omitempty"`
	AprovacaoEntrada    *bool   `json:"aprovacao_entrada,omitempty"`
	FotoBase64          string  `json:"foto_base64,omitempty"`
	TemporizadorExpira  string  `json:"temporizador_expira,omitempty"` // "0", "24h", "7d", "90d"
}

// ConfiguracaoInstanciaReq representa as opções Anti-Ban, Proxy e Atendimento de uma instância
type ConfiguracaoInstanciaReq struct {
	IgnorarGrupos            bool   `json:"ignorar_grupos"`
	RejeitarChamadas         bool   `json:"rejeitar_chamadas"`
	MensagemChamadaRejeitada string `json:"mensagem_chamada_rejeitada"`
	ProxyURL                 string `json:"proxy_url"`
	LeituraAutomatica        bool   `json:"leitura_automatica"`
	SempreOnline             bool   `json:"sempre_online"`
}

// construirContextInfo monta o ContextInfo para respostas (Quote), menções (@), menção fantasma (@todos em grupos) e encaminhamento
func (g *GerenciadorInstancias) construirContextInfo(ctx context.Context, inst *InstanciaAtiva, destJID types.JID, opcoes *OpcoesEnvio) *waProto.ContextInfo {
	if opcoes == nil {
		return nil
	}

	temAlgo := opcoes.ResponderMensagemID != "" ||
		len(opcoes.Mencionar) > 0 ||
		opcoes.MencionarTodos ||
		opcoes.Encaminhada ||
		opcoes.EncaminhadaFrequente ||
		opcoes.ExpiracaoSegundos > 0

	if !temAlgo {
		return nil
	}

	ci := &waProto.ContextInfo{}

	// 1. Responder mensagem (Quote/Reply)
	if opcoes.ResponderMensagemID != "" {
		ci.StanzaID = proto.String(strings.TrimSpace(opcoes.ResponderMensagemID))
		participante := destJID.ToNonAD().String()
		if opcoes.ResponderRemetente != "" {
			if pJID, err := ObterJIDPadrao(opcoes.ResponderRemetente); err == nil {
				participante = pJID.ToNonAD().String()
			}
		}
		ci.Participant = proto.String(participante)
		textoQuote := opcoes.TextoCitado
		if textoQuote == "" {
			textoQuote = "..."
		}
		ci.QuotedMessage = &waProto.Message{
			Conversation: proto.String(textoQuote),
		}
	}

	// 2. Menções explícitas e Menção Fantasma (@todos em grupos)
	mencoesMap := make(map[string]struct{})
	for _, m := range opcoes.Mencionar {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if strings.EqualFold(m, "todos") || strings.EqualFold(m, "@todos") || strings.EqualFold(m, "all") || strings.EqualFold(m, "@all") {
			opcoes.MencionarTodos = true
			continue
		}
		if mJID, err := ObterJIDPadrao(m); err == nil {
			mencoesMap[mJID.ToNonAD().String()] = struct{}{}
		}
	}

	if opcoes.MencionarTodos && destJID.Server == types.GroupServer && inst != nil && inst.Cliente != nil && inst.Cliente.IsConnected() {
		if grpInfo, err := inst.Cliente.GetGroupInfo(ctx, destJID); err == nil && grpInfo != nil {
			for _, p := range grpInfo.Participants {
				mencoesMap[p.JID.ToNonAD().String()] = struct{}{}
			}
		}
	}

	if len(mencoesMap) > 0 {
		for mStr := range mencoesMap {
			ci.MentionedJID = append(ci.MentionedJID, mStr)
		}
	}

	// 3. Flag de mensagem encaminhada
	if opcoes.EncaminhadaFrequente {
		ci.IsForwarded = proto.Bool(true)
		ci.ForwardingScore = proto.Uint32(999)
	} else if opcoes.Encaminhada {
		ci.IsForwarded = proto.Bool(true)
		ci.ForwardingScore = proto.Uint32(1)
	}

	// 4. Expiração de mensagens temporárias
	if opcoes.ExpiracaoSegundos > 0 {
		ci.Expiration = proto.Uint32(opcoes.ExpiracaoSegundos)
	}

	return ci
}

// simularCadenciaHumana envia presença ("digitando..." ou "gravando áudio...") antes do disparo
func (g *GerenciadorInstancias) simularCadenciaHumana(ctx context.Context, inst *InstanciaAtiva, destJID types.JID, delayMs int, gravandoAudio bool) {
	if delayMs <= 0 || inst == nil || inst.Cliente == nil || !inst.Cliente.IsConnected() {
		return
	}
	if delayMs > 15000 {
		delayMs = 15000 // máximo de 15s para evitar timeout HTTP
	}

	midia := types.ChatPresenceMedia("")
	if gravandoAudio {
		midia = types.ChatPresenceMediaAudio
	}

	_ = inst.Cliente.SendChatPresence(ctx, destJID, types.ChatPresenceComposing, midia)
	select {
	case <-ctx.Done():
	case <-time.After(time.Duration(delayMs) * time.Millisecond):
	}
	_ = inst.Cliente.SendChatPresence(ctx, destJID, types.ChatPresencePaused, midia)
}

// enviarComFallbackBrasil envia qualquer waProto.Message com fallback inteligente para o 9º dígito brasileiro
func (g *GerenciadorInstancias) enviarComFallbackBrasil(ctx context.Context, inst *InstanciaAtiva, destinatario, tipoLog string, msg *waProto.Message) (string, error) {
	jid, err := ObterJIDPadrao(destinatario)
	if err != nil {
		return "", err
	}

	// Se for número individual BR, tenta resolver via IsOnWhatsApp para acertar o JID de primeira
	if jid.Server == types.DefaultUserServer && strings.HasPrefix(jid.User, "55") {
		altNum, temAlt := AlternarNonoDigitoBrasil(jid.User)
		consultas := []string{"+" + jid.User}
		if temAlt {
			consultas = append(consultas, "+"+altNum)
		}
		if res, checkErr := inst.Cliente.IsOnWhatsApp(ctx, consultas); checkErr == nil && len(res) > 0 {
			for _, item := range res {
				if item.IsIn && !item.JID.IsEmpty() {
					jid = item.JID.ToNonAD()
					break
				}
			}
		}
	}

	resp, err := inst.Cliente.SendMessage(ctx, jid, msg)
	if err != nil {
		altNum, alterado := AlternarNonoDigitoBrasil(destinatario)
		if alterado {
			if altJID, altErr := ObterJIDPadrao(altNum); altErr == nil {
				if respAlt, errAlt := inst.Cliente.SendMessage(ctx, altJID, msg); errAlt == nil {
					_ = g.db.GravarLogMensagem(inst.ID, respAlt.ID, tipoLog, altNum, "ENVIADA", "ENTREGUE", "")
					return respAlt.ID, nil
				}
			}
		}
		_ = g.db.GravarLogMensagem(inst.ID, "", tipoLog, destinatario, "ENVIADA", "ERRO", err.Error())
		return "", fmt.Errorf("falha ao enviar %s: %w", tipoLog, err)
	}

	_ = g.db.GravarLogMensagem(inst.ID, resp.ID, tipoLog, jid.String(), "ENVIADA", "ENTREGUE", "")

	// Salva mensagem enviada e atualiza a conversa no Web Chat
	textoEnviado := ""
	if msg.GetConversation() != "" {
		textoEnviado = msg.GetConversation()
	} else if msg.GetExtendedTextMessage() != nil {
		textoEnviado = msg.GetExtendedTextMessage().GetText()
	} else {
		textoEnviado = "[" + tipoLog + "]"
	}
	resolvido := g.ResolverInfoContato(ctx, inst.ID, jid, "")
	msgChatWeb := database.MensagemChat{
		ID:              resp.ID,
		InstanciaID:     inst.ID,
		ChatJID:         jid.String(),
		RemetenteJID:    "eu",
		RemetenteNome:   "Eu",
		RemetenteNumero: inst.NumeroConectado,
		DeMim:           true,
		Tipo:            tipoLog,
		Texto:           textoEnviado,
		Status:          "ENVIADA",
		CriadoEm:        time.Now(),
	}
	_ = g.db.SalvarMensagemChatComContato(msgChatWeb, resolvido.Nome, resolvido.Numero, resolvido.FotoURL, resolvido.EhGrupo)
	HubChatGlobal.Transmitir(EventoChatWeb{
		Tipo:        "nova_mensagem",
		InstanciaID: inst.ID,
		Mensagem:    &msgChatWeb,
	})

	return resp.ID, nil
}

// ============================================================================
// PACOTE 1: MENSAGERIA COMPLETA E INTERATIVA
// ============================================================================

// EnviarMensagemTextoComOpcoes envia texto simples ou rico (com Quote, Menções, Link Preview e Delay Digitando)
func (g *GerenciadorInstancias) EnviarMensagemTextoComOpcoes(ctx context.Context, id, destinatario, texto string, opcoes *OpcoesEnvio) (string, error) {
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

	if opcoes != nil && opcoes.DelayDigitandoMs > 0 {
		g.simularCadenciaHumana(ctx, inst, jid, opcoes.DelayDigitandoMs, false)
	}

	ci := g.construirContextInfo(ctx, inst, jid, opcoes)
	temPreview := opcoes != nil && (opcoes.TituloLink != "" || opcoes.DescricaoLink != "" || opcoes.UrlLink != "")

	var msg *waProto.Message
	if ci != nil || temPreview {
		ext := &waProto.ExtendedTextMessage{
			Text:        proto.String(texto),
			ContextInfo: ci,
		}
		if opcoes != nil {
			if opcoes.TituloLink != "" {
				ext.Title = proto.String(opcoes.TituloLink)
			}
			if opcoes.DescricaoLink != "" {
				ext.Description = proto.String(opcoes.DescricaoLink)
			}
			if opcoes.UrlLink != "" {
				ext.MatchedText = proto.String(opcoes.UrlLink)
			}
		}
		msg = &waProto.Message{
			ExtendedTextMessage: ext,
		}
	} else {
		msg = &waProto.Message{
			Conversation: proto.String(texto),
		}
	}

	return g.enviarComFallbackBrasil(ctx, inst, destinatario, "texto", msg)
}

// EnviarMensagemAudioComOpcoes envia áudio PTT ou normal com suporte a ViewOnce, Quote e Delay "Gravando áudio..."
func (g *GerenciadorInstancias) EnviarMensagemAudioComOpcoes(ctx context.Context, id, destinatario string, dadosAudio []byte, ptt bool, opcoes *OpcoesEnvio) (string, error) {
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

	if opcoes != nil && opcoes.DelayDigitandoMs > 0 {
		g.simularCadenciaHumana(ctx, inst, jid, opcoes.DelayDigitandoMs, true)
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

	viewOnce := opcoes != nil && opcoes.VisualizacaoUnica
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
		ViewOnce:      proto.Bool(viewOnce),
		ContextInfo:   g.construirContextInfo(ctx, inst, jid, opcoes),
	}

	msg := &waProto.Message{AudioMessage: msgAudio}
	if viewOnce {
		msg = &waProto.Message{
			ViewOnceMessageV2: &waProto.FutureProofMessage{
				Message: &waProto.Message{AudioMessage: msgAudio},
			},
		}
	}

	return g.enviarComFallbackBrasil(ctx, inst, destinatario, "audio_ptt", msg)
}

// EnviarMensagemImagemComOpcoes envia imagem com suporte a ViewOnce, Quote, Menções e Delay
func (g *GerenciadorInstancias) EnviarMensagemImagemComOpcoes(ctx context.Context, id, destinatario string, dadosImagem []byte, legenda string, opcoes *OpcoesEnvio) (string, error) {
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

	if opcoes != nil && opcoes.DelayDigitandoMs > 0 {
		g.simularCadenciaHumana(ctx, inst, jid, opcoes.DelayDigitandoMs, false)
	}

	mimeType := http.DetectContentType(dadosImagem)
	upload, err := inst.Cliente.Upload(ctx, dadosImagem, whatsmeow.MediaImage)
	if err != nil {
		return "", fmt.Errorf("falha no upload da imagem: %w", err)
	}

	viewOnce := opcoes != nil && opcoes.VisualizacaoUnica
	msgImagem := &waProto.ImageMessage{
		URL:           proto.String(upload.URL),
		DirectPath:    proto.String(upload.DirectPath),
		MediaKey:      upload.MediaKey,
		Mimetype:      proto.String(mimeType),
		FileEncSHA256: upload.FileEncSHA256,
		FileSHA256:    upload.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(dadosImagem))),
		Caption:       proto.String(legenda),
		ViewOnce:      proto.Bool(viewOnce),
		ContextInfo:   g.construirContextInfo(ctx, inst, jid, opcoes),
	}

	msg := &waProto.Message{ImageMessage: msgImagem}
	if viewOnce {
		msg = &waProto.Message{
			ViewOnceMessageV2: &waProto.FutureProofMessage{
				Message: &waProto.Message{ImageMessage: msgImagem},
			},
		}
	}

	return g.enviarComFallbackBrasil(ctx, inst, destinatario, "imagem", msg)
}

// EnviarMensagemVideo envia vídeo MP4 normal, GIF animado (gifPlayback) ou Nota de Vídeo Redonda (ptv)
func (g *GerenciadorInstancias) EnviarMensagemVideo(ctx context.Context, id, destinatario string, dadosVideo []byte, legenda string, comoGif, notaVideoCircular bool, opcoes *OpcoesEnvio) (string, error) {
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

	if opcoes != nil && opcoes.DelayDigitandoMs > 0 {
		g.simularCadenciaHumana(ctx, inst, jid, opcoes.DelayDigitandoMs, false)
	}

	mimeType := http.DetectContentType(dadosVideo)
	if mimeType == "application/octet-stream" {
		mimeType = "video/mp4"
	}

	upload, err := inst.Cliente.Upload(ctx, dadosVideo, whatsmeow.MediaVideo)
	if err != nil {
		return "", fmt.Errorf("falha no upload do vídeo: %w", err)
	}

	viewOnce := opcoes != nil && opcoes.VisualizacaoUnica
	msgVideo := &waProto.VideoMessage{
		URL:           proto.String(upload.URL),
		DirectPath:    proto.String(upload.DirectPath),
		MediaKey:      upload.MediaKey,
		Mimetype:      proto.String(mimeType),
		FileEncSHA256: upload.FileEncSHA256,
		FileSHA256:    upload.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(dadosVideo))),
		Caption:       proto.String(legenda),
		GifPlayback:   proto.Bool(comoGif),
		ViewOnce:      proto.Bool(viewOnce),
		ContextInfo:   g.construirContextInfo(ctx, inst, jid, opcoes),
	}

	var msg *waProto.Message
	if notaVideoCircular {
		msg = &waProto.Message{
			PtvMessage: msgVideo,
		}
	} else if viewOnce {
		msg = &waProto.Message{
			ViewOnceMessageV2: &waProto.FutureProofMessage{
				Message: &waProto.Message{VideoMessage: msgVideo},
			},
		}
	} else {
		msg = &waProto.Message{VideoMessage: msgVideo}
	}

	return g.enviarComFallbackBrasil(ctx, inst, destinatario, "video", msg)
}

// EnviarMensagemDocumento envia PDF, boletos, XML, planilhas ou qualquer arquivo com nome e legenda
func (g *GerenciadorInstancias) EnviarMensagemDocumento(ctx context.Context, id, destinatario string, dadosDoc []byte, nomeArquivo, legenda, mimeInformado string, opcoes *OpcoesEnvio) (string, error) {
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

	if opcoes != nil && opcoes.DelayDigitandoMs > 0 {
		g.simularCadenciaHumana(ctx, inst, jid, opcoes.DelayDigitandoMs, false)
	}

	if strings.TrimSpace(nomeArquivo) == "" {
		nomeArquivo = "documento.pdf"
	}

	mimeType := strings.TrimSpace(mimeInformado)
	if mimeType == "" {
		ext := strings.ToLower(filepath.Ext(nomeArquivo))
		switch ext {
		case ".pdf":
			mimeType = "application/pdf"
		case ".xlsx":
			mimeType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		case ".xls":
			mimeType = "application/vnd.ms-excel"
		case ".docx":
			mimeType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		case ".doc":
			mimeType = "application/msword"
		case ".csv":
			mimeType = "text/csv"
		case ".xml":
			mimeType = "application/xml"
		case ".zip":
			mimeType = "application/zip"
		default:
			mimeType = http.DetectContentType(dadosDoc)
		}
	}

	upload, err := inst.Cliente.Upload(ctx, dadosDoc, whatsmeow.MediaDocument)
	if err != nil {
		return "", fmt.Errorf("falha no upload do documento: %w", err)
	}

	msgDoc := &waProto.DocumentMessage{
		URL:           proto.String(upload.URL),
		DirectPath:    proto.String(upload.DirectPath),
		MediaKey:      upload.MediaKey,
		Mimetype:      proto.String(mimeType),
		Title:         proto.String(nomeArquivo),
		FileName:      proto.String(nomeArquivo),
		FileEncSHA256: upload.FileEncSHA256,
		FileSHA256:    upload.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(dadosDoc))),
		Caption:       proto.String(legenda),
		ContextInfo:   g.construirContextInfo(ctx, inst, jid, opcoes),
	}

	var msg *waProto.Message
	if legenda != "" {
		msg = &waProto.Message{
			DocumentWithCaptionMessage: &waProto.FutureProofMessage{
				Message: &waProto.Message{DocumentMessage: msgDoc},
			},
		}
	} else {
		msg = &waProto.Message{DocumentMessage: msgDoc}
	}

	return g.enviarComFallbackBrasil(ctx, inst, destinatario, "documento", msg)
}

// EnviarMensagemSticker envia figurinha (.webp) estática ou animada
func (g *GerenciadorInstancias) EnviarMensagemSticker(ctx context.Context, id, destinatario string, dadosSticker []byte, animado bool, opcoes *OpcoesEnvio) (string, error) {
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

	upload, err := inst.Cliente.Upload(ctx, dadosSticker, whatsmeow.MediaImage)
	if err != nil {
		return "", fmt.Errorf("falha no upload do sticker: %w", err)
	}

	msgSticker := &waProto.StickerMessage{
		URL:           proto.String(upload.URL),
		DirectPath:    proto.String(upload.DirectPath),
		MediaKey:      upload.MediaKey,
		Mimetype:      proto.String("image/webp"),
		FileEncSHA256: upload.FileEncSHA256,
		FileSHA256:    upload.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(dadosSticker))),
		IsAnimated:    proto.Bool(animado),
		ContextInfo:   g.construirContextInfo(ctx, inst, jid, opcoes),
	}

	msg := &waProto.Message{StickerMessage: msgSticker}
	return g.enviarComFallbackBrasil(ctx, inst, destinatario, "sticker", msg)
}

// EnviarMensagemEnquete envia uma enquete interativa (substituto oficial dos antigos botões que funciona 100% no iOS/Android)
func (g *GerenciadorInstancias) EnviarMensagemEnquete(ctx context.Context, id, destinatario, pergunta string, opcoes []string, maxEscolhas int) (string, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return "", err
	}
	if !inst.Cliente.IsConnected() {
		return "", fmt.Errorf("a instância %s não está conectada", id)
	}

	if len(opcoes) < 2 {
		return "", fmt.Errorf("a enquete precisa ter pelo menos 2 opções")
	}
	if maxEscolhas <= 0 || maxEscolhas > len(opcoes) {
		maxEscolhas = 1 // Padrão: escolha única (comportamento de botão interativo)
	}

	msg := inst.Cliente.BuildPollCreation(pergunta, opcoes, maxEscolhas)
	return g.enviarComFallbackBrasil(ctx, inst, destinatario, "enquete", msg)
}

// EnviarMensagemLocalizacao envia coordenadas de mapa com nome do estabelecimento e endereço

// EnviarMensagemInterativa envia mensagem com botões interativos nativos (Native Flow Buttons: CTA URL, Pix Copia e Cola, Quick Reply)
func (g *GerenciadorInstancias) EnviarMensagemInterativa(
	ctx context.Context,
	id, destinatario string,
	titulo, corpo, rodape string,
	botoes []BotaoInterativo,
	opcoes *OpcoesEnvio,
) (string, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return "", err
	}
	if !inst.Cliente.IsConnected() {
		return "", fmt.Errorf("a instância %s não está conectada", id)
	}

	if strings.TrimSpace(corpo) == "" {
		if strings.TrimSpace(titulo) != "" {
			corpo = titulo
		} else {
			return "", fmt.Errorf("o corpo da mensagem interativa é obrigatório")
		}
	}

	if len(botoes) == 0 {
		return "", fmt.Errorf("ao menos um botão interativo deve ser fornecido")
	}

	jid, err := ObterJIDPadrao(destinatario)
	if err != nil {
		return "", err
	}

	var nativeButtons []*waProto.InteractiveMessage_NativeFlowMessage_NativeFlowButton
	for i, b := range botoes {
		tipoNorm := strings.ToLower(strings.TrimSpace(b.Tipo))
		textoBotao := strings.TrimSpace(b.Texto)
		if textoBotao == "" {
			textoBotao = fmt.Sprintf("Opção %d", i+1)
		}

		var nomeTipo string
		var paramsBytes []byte
		var errJson error

		switch tipoNorm {
		case "url", "cta_url", "link":
			nomeTipo = "cta_url"
			urlAlvo := strings.TrimSpace(b.Valor)
			paramsBytes, errJson = json.Marshal(map[string]string{
				"display_text": textoBotao,
				"url":          urlAlvo,
				"merchant_url": urlAlvo,
			})

		case "copiar", "cta_copy", "pix", "cupom":
			nomeTipo = "cta_copy"
			paramsBytes, errJson = json.Marshal(map[string]string{
				"display_text": textoBotao,
				"copy_code":    strings.TrimSpace(b.Valor),
			})

		case "resposta", "quick_reply", "reply":
			nomeTipo = "quick_reply"
			idBtn := strings.TrimSpace(b.Id)
			if idBtn == "" {
				idBtn = strings.TrimSpace(b.Valor)
			}
			if idBtn == "" {
				idBtn = textoBotao
			}
			paramsBytes, errJson = json.Marshal(map[string]string{
				"display_text": textoBotao,
				"id":           idBtn,
			})

		case "chamada", "cta_call", "ligar", "telefone":
			nomeTipo = "cta_call"
			paramsBytes, errJson = json.Marshal(map[string]string{
				"display_text": textoBotao,
				"phone_number": strings.TrimSpace(b.Valor),
			})

		default:
			nomeTipo = "quick_reply"
			idBtn := strings.TrimSpace(b.Id)
			if idBtn == "" {
				idBtn = strings.TrimSpace(b.Valor)
			}
			if idBtn == "" {
				idBtn = textoBotao
			}
			paramsBytes, errJson = json.Marshal(map[string]string{
				"display_text": textoBotao,
				"id":           idBtn,
			})
		}

		if errJson != nil {
			return "", fmt.Errorf("erro ao serializar parâmetros do botão %s: %w", textoBotao, errJson)
		}

		nativeButtons = append(nativeButtons, &waProto.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
			Name:             proto.String(nomeTipo),
			ButtonParamsJSON: proto.String(string(paramsBytes)),
		})
	}

	msgVersion := int32(3)
	flowMsg := &waProto.InteractiveMessage_NativeFlowMessage{
		Buttons:        nativeButtons,
		MessageVersion: &msgVersion,
	}

	var header *waProto.InteractiveMessage_Header
	if strings.TrimSpace(titulo) != "" {
		header = &waProto.InteractiveMessage_Header{
			Title:              proto.String(strings.TrimSpace(titulo)),
			HasMediaAttachment: proto.Bool(false),
		}
	}

	body := &waProto.InteractiveMessage_Body{
		Text: proto.String(strings.TrimSpace(corpo)),
	}

	var footer *waProto.InteractiveMessage_Footer
	if strings.TrimSpace(rodape) != "" {
		footer = &waProto.InteractiveMessage_Footer{
			Text: proto.String(strings.TrimSpace(rodape)),
		}
	}

	interactiveMsg := &waProto.InteractiveMessage{
		Header: header,
		Body:   body,
		Footer: footer,
		InteractiveMessage: &waProto.InteractiveMessage_NativeFlowMessage_{
			NativeFlowMessage: flowMsg,
		},
		ContextInfo: g.construirContextInfo(ctx, inst, jid, opcoes),
	}

	msg := &waProto.Message{
		InteractiveMessage: interactiveMsg,
	}

	return g.enviarComFallbackBrasil(ctx, inst, destinatario, "interativa", msg)
}

func (g *GerenciadorInstancias) EnviarMensagemLocalizacao(ctx context.Context, id, destinatario string, latitude, longitude float64, nome, endereco, urlMapa string, opcoes *OpcoesEnvio) (string, error) {
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

	msgLoc := &waProto.LocationMessage{
		DegreesLatitude:  proto.Float64(latitude),
		DegreesLongitude: proto.Float64(longitude),
		Name:             proto.String(nome),
		Address:          proto.String(endereco),
		URL:              proto.String(urlMapa),
		ContextInfo:      g.construirContextInfo(ctx, inst, jid, opcoes),
	}

	msg := &waProto.Message{LocationMessage: msgLoc}
	return g.enviarComFallbackBrasil(ctx, inst, destinatario, "localizacao", msg)
}

// montarVcard Padroniza um cartão vCard 3.0 com waid compatível com WhatsApp Brasil
func montarVcard(c ContatoCartao) string {
	if strings.TrimSpace(c.Vcard) != "" {
		return strings.TrimSpace(c.Vcard)
	}
	nome := strings.TrimSpace(c.Nome)
	if nome == "" {
		nome = "Contato"
	}
	numLimpo, err := NormalizarNumeroBrasil(c.Telefone)
	if err != nil {
		numLimpo = apenasDigitosRegex.ReplaceAllString(c.Telefone, "")
	}
	visual := FormatarNumeroBrasilVisual(numLimpo)
	org := strings.TrimSpace(c.Empresa)
	if org == "" {
		org = "Contato Comercial"
	}

	var sb strings.Builder
	sb.WriteString("BEGIN:VCARD\nVERSION:3.0\n")
	sb.WriteString(fmt.Sprintf("FN:%s\n", nome))
	sb.WriteString(fmt.Sprintf("ORG:%s;\n", org))
	if c.Email != "" {
		sb.WriteString(fmt.Sprintf("EMAIL;type=INTERNET;type=WORK:%s\n", strings.TrimSpace(c.Email)))
	}
	if numLimpo != "" {
		sb.WriteString(fmt.Sprintf("TEL;type=CELL;type=VOICE;waid=%s:%s\n", numLimpo, visual))
	}
	sb.WriteString("END:VCARD")
	return sb.String()
}

// EnviarMensagemContato envia 1 cartão de contato (ContactMessage) ou vários contatos (ContactsArrayMessage)
func (g *GerenciadorInstancias) EnviarMensagemContato(ctx context.Context, id, destinatario string, contatos []ContatoCartao, opcoes *OpcoesEnvio) (string, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return "", err
	}
	if !inst.Cliente.IsConnected() {
		return "", fmt.Errorf("a instância %s não está conectada", id)
	}
	if len(contatos) == 0 {
		return "", fmt.Errorf("informe ao menos 1 contato para envio")
	}

	jid, err := ObterJIDPadrao(destinatario)
	if err != nil {
		return "", err
	}

	ci := g.construirContextInfo(ctx, inst, jid, opcoes)

	var msg *waProto.Message
	if len(contatos) == 1 {
		c := contatos[0]
		vcard := montarVcard(c)
		msg = &waProto.Message{
			ContactMessage: &waProto.ContactMessage{
				DisplayName: proto.String(c.Nome),
				Vcard:       proto.String(vcard),
				ContextInfo: ci,
			},
		}
	} else {
		var lista []*waProto.ContactMessage
		for _, c := range contatos {
			lista = append(lista, &waProto.ContactMessage{
				DisplayName: proto.String(c.Nome),
				Vcard:       proto.String(montarVcard(c)),
			})
		}
		msg = &waProto.Message{
			ContactsArrayMessage: &waProto.ContactsArrayMessage{
				DisplayName: proto.String(fmt.Sprintf("%s e outros %d contatos", contatos[0].Nome, len(contatos)-1)),
				Contacts:    lista,
				ContextInfo: ci,
			},
		}
	}

	return g.enviarComFallbackBrasil(ctx, inst, destinatario, "contato", msg)
}

// EnviarReacao envia um emoji de reação para uma mensagem específica (ou string vazia para remover a reação)
func (g *GerenciadorInstancias) EnviarReacao(ctx context.Context, id, destinatario, remetenteOriginal, idMensagemAlvo, emoji string) (string, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return "", err
	}
	if !inst.Cliente.IsConnected() {
		return "", fmt.Errorf("a instância %s não está conectada", id)
	}

	chatJID, err := ObterJIDPadrao(destinatario)
	if err != nil {
		return "", err
	}

	var senderJID types.JID
	if strings.TrimSpace(remetenteOriginal) != "" {
		senderJID, _ = ObterJIDPadrao(remetenteOriginal)
	}

	msg := inst.Cliente.BuildReaction(chatJID, senderJID, types.MessageID(idMensagemAlvo), emoji)
	resp, err := inst.Cliente.SendMessage(ctx, chatJID, msg)
	if err != nil {
		return "", fmt.Errorf("falha ao enviar reação: %w", err)
	}
	_ = g.db.GravarLogMensagem(id, resp.ID, "reacao", chatJID.String(), "ENVIADA", "ENTREGUE", "")
	return resp.ID, nil
}

// EditarMensagem edita o texto de uma mensagem enviada anteriormente (janela oficial de 20 minutos)
func (g *GerenciadorInstancias) EditarMensagem(ctx context.Context, id, destinatario, idMensagemOriginal, novoTexto string) (string, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return "", err
	}
	if !inst.Cliente.IsConnected() {
		return "", fmt.Errorf("a instância %s não está conectada", id)
	}

	chatJID, err := ObterJIDPadrao(destinatario)
	if err != nil {
		return "", err
	}

	msgEdit := inst.Cliente.BuildEdit(chatJID, types.MessageID(idMensagemOriginal), &waProto.Message{
		Conversation: proto.String(novoTexto),
	})

	resp, err := inst.Cliente.SendMessage(ctx, chatJID, msgEdit)
	if err != nil {
		return "", fmt.Errorf("falha ao editar mensagem: %w", err)
	}
	_ = g.db.GravarLogMensagem(id, resp.ID, "edicao", chatJID.String(), "ENVIADA", "ENTREGUE", "")
	return resp.ID, nil
}

// RevogarMensagem apaga uma mensagem para todos (própria ou de terceiros se for admin do grupo)
func (g *GerenciadorInstancias) RevogarMensagem(ctx context.Context, id, destinatario, remetenteOriginal, idMensagemAlvo string) (string, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return "", err
	}
	if !inst.Cliente.IsConnected() {
		return "", fmt.Errorf("a instância %s não está conectada", id)
	}

	chatJID, err := ObterJIDPadrao(destinatario)
	if err != nil {
		return "", err
	}

	var senderJID types.JID
	if strings.TrimSpace(remetenteOriginal) != "" {
		senderJID, _ = ObterJIDPadrao(remetenteOriginal)
	}

	msgRevoke := inst.Cliente.BuildRevoke(chatJID, senderJID, types.MessageID(idMensagemAlvo))
	resp, err := inst.Cliente.SendMessage(ctx, chatJID, msgRevoke)
	if err != nil {
		return "", fmt.Errorf("falha ao apagar mensagem para todos: %w", err)
	}
	_ = g.db.GravarLogMensagem(id, resp.ID, "revogacao", chatJID.String(), "ENVIADA", "ENTREGUE", "")
	return resp.ID, nil
}

// ============================================================================
// PACOTE 2: ATENDIMENTO, MÍDIA RECEBIDA, ANTI-CHAMADAS, VALIDADOR & PERFIL
// ============================================================================

// tratarEventoChamada processa ofertas de ligação de voz/vídeo, envia webhook e rejeita automaticamente se configurado
func (g *GerenciadorInstancias) tratarEventoChamada(v *events.CallOffer, inst *InstanciaAtiva) {
	instDB, err := g.db.BuscarInstanciaPorId(inst.ID)
	if err != nil || instDB == nil {
		return
	}

	remetente := v.CallCreator.ToNonAD()
	if remetente.Server == types.HiddenUserServer && !v.CallCreatorAlt.IsEmpty() {
		remetente = v.CallCreatorAlt.ToNonAD()
	}

	rejeitadaAuto := false
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if instDB.RejeitarChamadas && inst.Cliente != nil && inst.Cliente.IsConnected() {
		if rejErr := inst.Cliente.RejectCall(ctx, v.From, v.CallID); rejErr == nil {
			rejeitadaAuto = true
			_ = g.db.GravarLogMensagem(inst.ID, v.CallID, "chamada_rejeitada", remetente.String(), "RECEBIDA", "REJEITADA", "")

			if strings.TrimSpace(instDB.MensagemChamadaRejeitar) != "" {
				msgResp := &waProto.Message{
					Conversation: proto.String(instDB.MensagemChamadaRejeitar),
				}
				_, _ = inst.Cliente.SendMessage(ctx, remetente, msgResp)
			}
		}
	}

	if instDB.WebhookURL != "" {
		g.webhook.Enviar(instDB.WebhookURL, instDB.WebhookSegredo, PayloadWebhook{
			InstanciaID: inst.ID,
			Nome:        inst.Nome,
			Evento:      EventoChamada,
			Dados: map[string]interface{}{
				"id_chamada":             v.CallID,
				"remetente":              remetente.String(),
				"remetente_br":           FormatarNumeroBrasilVisual(remetente.User),
				"plataforma":             v.RemotePlatform,
				"rejeitada_automatico":   rejeitadaAuto,
				"mensagem_auto_enviada":  rejeitadaAuto && strings.TrimSpace(instDB.MensagemChamadaRejeitar) != "",
				"timestamp":              v.Timestamp.Unix(),
			},
		})
	}
}

// tratarEventoMensagemCompleta extrai todos os tipos de mensagem, votos de enquete, reações e baixa mídias automaticamente
func (g *GerenciadorInstancias) tratarEventoMensagemCompleta(v *events.Message, inst *InstanciaAtiva) {
	instDB, err := g.db.BuscarInstanciaPorId(inst.ID)
	if err != nil || instDB == nil {
		return
	}
	if instDB.IgnorarGrupos && v.Info.IsGroup {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// Leitura automática (2 tracinhos azuis) se configurada na instância
	if instDB.LeituraAutomatica && !v.Info.IsFromMe && inst.Cliente != nil && inst.Cliente.IsConnected() {
		sender := v.Info.Sender
		if !v.Info.IsGroup {
			sender = types.EmptyJID
		}
		_ = inst.Cliente.MarkRead(ctx, []types.MessageID{v.Info.ID}, time.Now(), v.Info.Chat, sender)
	}

	// 0. Verificar se é Revogação de Mensagem (ProtocolMessage_REVOKE - Ghost Mode)
	if protoMsg := v.Message.GetProtocolMessage(); protoMsg != nil {
		if protoMsg.GetType() == waProto.ProtocolMessage_REVOKE {
			targetID := protoMsg.GetKey().GetID()
			_ = g.db.MarcarMensagemDeletada(inst.ID, targetID)
			HubChatGlobal.Transmitir(EventoChatWeb{
				Tipo:        "mensagem_deletada",
				InstanciaID: inst.ID,
				Dados: map[string]interface{}{
					"id_mensagem": targetID,
					"chat_jid":    v.Info.Chat.String(),
				},
			})
		}
	}

	// 1. Verificar se é Voto de Enquete (PollUpdateMessage)
	if v.Message.GetPollUpdateMessage() != nil && inst.Cliente != nil {
		voto, errPoll := inst.Cliente.DecryptPollVote(ctx, v)
		var hashesHex []string
		if errPoll == nil && voto != nil {
			for _, optHash := range voto.GetSelectedOptions() {
				hashesHex = append(hashesHex, strings.ToUpper(hex.EncodeToString(optHash)))
			}
		}
		idEnqueteOrig := v.Message.GetPollUpdateMessage().GetPollCreationMessageKey().GetID()
		_ = g.db.GravarLogMensagem(inst.ID, v.Info.ID, "voto_enquete", v.Info.Sender.String(), "RECEBIDA", "LIDA", "")
		if instDB.WebhookURL != "" {
			g.webhook.Enviar(instDB.WebhookURL, instDB.WebhookSegredo, PayloadWebhook{
				InstanciaID: inst.ID,
				Nome:        inst.Nome,
				Evento:      EventoVotoEnquete,
				Dados: map[string]interface{}{
					"id_mensagem":         v.Info.ID,
					"id_enquete_original": idEnqueteOrig,
					"remetente":           v.Info.Sender.String(),
					"remetente_br":        FormatarNumeroBrasilVisual(v.Info.Sender.User),
					"chat":                v.Info.Chat.String(),
					"opcoes_hashes_sha256": hashesHex,
					"timestamp":           v.Info.Timestamp.Unix(),
				},
			})
		}
		return
	}

	// 2. Verificar se é Reação com Emoji
	if rMsg := v.Message.GetReactionMessage(); rMsg != nil {
		_ = g.db.GravarLogMensagem(inst.ID, v.Info.ID, "reacao", v.Info.Sender.String(), "RECEBIDA", "LIDA", "")
		if instDB.WebhookURL != "" {
			g.webhook.Enviar(instDB.WebhookURL, instDB.WebhookSegredo, PayloadWebhook{
				InstanciaID: inst.ID,
				Nome:        inst.Nome,
				Evento:      EventoReacao,
				Dados: map[string]interface{}{
					"id_mensagem":      v.Info.ID,
					"id_mensagem_alvo": rMsg.GetKey().GetID(),
					"emoji":            rMsg.GetText(),
					"remetente":        v.Info.Sender.String(),
					"remetente_br":     FormatarNumeroBrasilVisual(v.Info.Sender.User),
					"chat":             v.Info.Chat.String(),
					"timestamp":        v.Info.Timestamp.Unix(),
				},
			})
		}
		return
	}

	// 3. Extrair Texto, Legenda, Tipo de Mídia e Metadados
	var texto string
	tipoMensagem := "texto"
	nomeArquivo := ""
	mimeType := ""

	switch {
	case v.Message.GetConversation() != "":
		texto = v.Message.GetConversation()
		tipoMensagem = "texto"
	case v.Message.GetExtendedTextMessage() != nil:
		texto = v.Message.GetExtendedTextMessage().GetText()
		tipoMensagem = "texto"
	case v.Message.GetImageMessage() != nil:
		img := v.Message.GetImageMessage()
		texto = img.GetCaption()
		mimeType = img.GetMimetype()
		tipoMensagem = "imagem"
	case v.Message.GetAudioMessage() != nil:
		aud := v.Message.GetAudioMessage()
		mimeType = aud.GetMimetype()
		if aud.GetPTT() {
			tipoMensagem = "audio_ptt"
		} else {
			tipoMensagem = "audio"
		}
	case v.Message.GetVideoMessage() != nil:
		vid := v.Message.GetVideoMessage()
		texto = vid.GetCaption()
		mimeType = vid.GetMimetype()
		tipoMensagem = "video"
	case v.Message.GetDocumentMessage() != nil:
		doc := v.Message.GetDocumentMessage()
		texto = doc.GetCaption()
		nomeArquivo = doc.GetFileName()
		mimeType = doc.GetMimetype()
		tipoMensagem = "documento"
	case v.Message.GetDocumentWithCaptionMessage().GetMessage().GetDocumentMessage() != nil:
		doc := v.Message.GetDocumentWithCaptionMessage().GetMessage().GetDocumentMessage()
		texto = doc.GetCaption()
		nomeArquivo = doc.GetFileName()
		mimeType = doc.GetMimetype()
		tipoMensagem = "documento"
	case v.Message.GetStickerMessage() != nil:
		stk := v.Message.GetStickerMessage()
		mimeType = stk.GetMimetype()
		tipoMensagem = "sticker"
	case v.Message.GetLocationMessage() != nil:
		loc := v.Message.GetLocationMessage()
		texto = fmt.Sprintf("%s (%f, %f) - %s", loc.GetName(), loc.GetDegreesLatitude(), loc.GetDegreesLongitude(), loc.GetAddress())
		tipoMensagem = "localizacao"
	case v.Message.GetContactMessage() != nil:
		cnt := v.Message.GetContactMessage()
		texto = cnt.GetDisplayName()
		tipoMensagem = "contato"
	case v.Message.GetButtonsResponseMessage() != nil:
		texto = v.Message.GetButtonsResponseMessage().GetSelectedDisplayText()
		tipoMensagem = "resposta_botao"
	case v.Message.GetListResponseMessage() != nil:
		texto = v.Message.GetListResponseMessage().GetTitle()
		tipoMensagem = "resposta_lista"
	case v.Message.GetInteractiveResponseMessage() != nil:
		resp := v.Message.GetInteractiveResponseMessage()
		tipoMensagem = "resposta_interativa"
		if resp.GetBody() != nil {
			texto = resp.GetBody().GetText()
		}
		if flowResp := resp.GetNativeFlowResponseMessage(); flowResp != nil {
			if texto == "" {
				texto = flowResp.GetName()
			}
		}

	}

	_ = g.db.GravarLogMensagem(inst.ID, v.Info.ID, tipoMensagem, v.Info.Sender.String(), "RECEBIDA", "LIDA", "")

	temMidia := tipoMensagem == "imagem" || tipoMensagem == "audio_ptt" || tipoMensagem == "audio" || tipoMensagem == "video" || tipoMensagem == "documento" || tipoMensagem == "sticker"
	midiaBase64 := ""
	caminhoSalvo := ""
	urlMidiaRemota := ""

	// Descriptografia, Download Automático e Upload para MinIO Isolado da Revenda (até 15 MB)
	if temMidia && inst.Cliente != nil {
		if rawBytes, dlErr := inst.Cliente.DownloadAny(ctx, v.Message); dlErr == nil && len(rawBytes) > 0 && len(rawBytes) <= (15<<20) {
			if mimeType == "" {
				mimeType = http.DetectContentType(rawBytes)
			}
			midiaBase64 = base64.StdEncoding.EncodeToString(rawBytes)

			ext := ".bin"
			switch {
			case strings.Contains(mimeType, "jpeg"), strings.Contains(mimeType, "jpg"):
				ext = ".jpg"
			case strings.Contains(mimeType, "png"):
				ext = ".png"
			case strings.Contains(mimeType, "webp"):
				ext = ".webp"
			case strings.Contains(mimeType, "ogg"), strings.Contains(mimeType, "opus"):
				ext = ".ogg"
			case strings.Contains(mimeType, "mp4"):
				ext = ".mp4"
			case strings.Contains(mimeType, "pdf"):
				ext = ".pdf"
			}

			// Resolve MinIO isolado do Revendedor dono da instância (se configurado)
			var cfgMinioRevenda *storage.ConfigMinIO
			if rev, revErr := g.db.ObterRevendedorDaInstancia(inst.ID); revErr == nil && rev != nil && rev.MinioEndpoint != "" {
				cfgMinioRevenda = &storage.ConfigMinIO{
					Endpoint:  rev.MinioEndpoint,
					AccessKey: rev.MinioAccessKey,
					SecretKey: rev.MinioSecretKey,
					Bucket:    rev.MinioBucket,
					UseSSL:    rev.MinioUseSSL,
					PublicURL: rev.MinioPublicURL,
				}
			}

			if urlSalva, upErr := storage.SalvarMidiaHibrida(ctx, cfgMinioRevenda, inst.ID, v.Info.ID, ext, mimeType, rawBytes); upErr == nil {
				urlMidiaRemota = urlSalva
				caminhoSalvo = urlSalva
			} else {
				dirMidia := "./armazenamento_midia"
				if config.AppConfig != nil && config.AppConfig.DiretorioMidia != "" {
					dirMidia = config.AppConfig.DiretorioMidia
				}
				nomeSeguro := fmt.Sprintf("%s_%s%s", inst.ID, v.Info.ID, ext)
				caminhoCompleto := filepath.Join(dirMidia, nomeSeguro)
				if writeErr := os.WriteFile(caminhoCompleto, rawBytes, 0644); writeErr == nil {
					caminhoSalvo = caminhoCompleto
				}
			}
		} else if dlErr != nil {
			log.Debug().Err(dlErr).Str("id_msg", v.Info.ID).Msg("Não foi possível baixar mídia recebida automaticamente")
		}
	}

	dadosWebhook := map[string]interface{}{
		"id_mensagem":   v.Info.ID,
		"tipo":          tipoMensagem,
		"remetente":     v.Info.Sender.String(),
		"remetente_br":  FormatarNumeroBrasilVisual(v.Info.Sender.User),
		"nome_push":     v.Info.PushName,
		"chat":          v.Info.Chat.String(),
		"de_mim":        v.Info.IsFromMe,
		"eh_grupo":      v.Info.IsGroup,
		"timestamp":     v.Info.Timestamp.Unix(),
		"texto":         texto,
		"tem_midia":     temMidia,
		"mimetype":      mimeType,
		"nome_arquivo":  nomeArquivo,
		"url_midia":     urlMidiaRemota,
		"midia_base64":  midiaBase64,
		"arquivo_local": caminhoSalvo,
	}
	// Persistência no Web Chat (WhatsApp Web Clone) e Transmissão Realtime
	statusInicial := "RECEBIDA"
	if v.Info.IsFromMe {
		statusInicial = "ENVIADA"
	}
	urlParaSalvar := urlMidiaRemota
	if urlParaSalvar == "" && caminhoSalvo != "" {
		urlParaSalvar = "/api/v1/midia/local?path=" + filepath.Base(caminhoSalvo)
	}
	resolvido := g.ResolverInfoContato(context.Background(), inst.ID, v.Info.Chat, v.Info.PushName)
	remetenteResolvido := g.ResolverInfoContato(context.Background(), inst.ID, v.Info.Sender, v.Info.PushName)
	remetenteNomeFinal := v.Info.PushName
	if remetenteNomeFinal == "" {
		remetenteNomeFinal = remetenteResolvido.Nome
	}
	if remetenteNomeFinal == "" {
		remetenteNomeFinal = remetenteResolvido.Numero
	}

	msgChatWeb := database.MensagemChat{
		ID:           v.Info.ID,
		InstanciaID:  inst.ID,
		ChatJID:      v.Info.Chat.String(),
		RemetenteJID:    v.Info.Sender.String(),
		RemetenteNome:   remetenteNomeFinal,
		RemetenteNumero: remetenteResolvido.Numero,
		DeMim:        v.Info.IsFromMe,
		Tipo:         tipoMensagem,
		Texto:        texto,
		MidiaURL:     urlParaSalvar,
		MimeType:     mimeType,
		NomeArquivo:  nomeArquivo,
		Status:       statusInicial,
		CriadoEm:     v.Info.Timestamp,
	}
	_ = g.db.SalvarMensagemChatComContato(msgChatWeb, resolvido.Nome, resolvido.Numero, resolvido.FotoURL, resolvido.EhGrupo)
	HubChatGlobal.Transmitir(EventoChatWeb{
		Tipo:        "nova_mensagem",
		InstanciaID: inst.ID,
		Mensagem:    &msgChatWeb,
	})

		if resp := v.Message.GetInteractiveResponseMessage(); resp != nil {
		dadosResp := map[string]interface{}{}
		if resp.GetBody() != nil {
			dadosResp["texto"] = resp.GetBody().GetText()
		}
		if flowResp := resp.GetNativeFlowResponseMessage(); flowResp != nil {
			dadosResp["nome"] = flowResp.GetName()
			dadosResp["params_json"] = flowResp.GetParamsJSON()
			var paramsParsed map[string]interface{}
			if errJson := json.Unmarshal([]byte(flowResp.GetParamsJSON()), &paramsParsed); errJson == nil {
				dadosResp["params"] = paramsParsed
			}
		}
		dadosWebhook["resposta_interativa"] = dadosResp
	}

	if v.Message.GetLocationMessage() != nil {
		loc := v.Message.GetLocationMessage()
		dadosWebhook["localizacao"] = map[string]interface{}{
			"latitude":  loc.GetDegreesLatitude(),
			"longitude": loc.GetDegreesLongitude(),
			"nome":      loc.GetName(),
			"endereco":  loc.GetAddress(),
		}
	}

	// 4. Rastreamento e Atribuição de Origem da Mensagem (Meta Ads, Google Ads, UTMs e wa.me tags)
	atrib := ExtrairAtribuicaoOrigem(v, texto)
	remetenteBR := FormatarNumeroBrasilVisual(v.Info.Sender.User)
	dadosWebhook["origem"] = atrib

	_ = g.db.SalvarOuAtualizarAtribuicaoContato(database.AtribuicaoContato{
		InstanciaID:      inst.ID,
		JID:              v.Info.Sender.String(),
		NumeroFormatado:  remetenteBR,
		OrigemCanal:      atrib.Canal,
		Campanha:         atrib.Campanha,
		UtmSource:        atrib.UtmSource,
		UtmMedium:        atrib.UtmMedium,
		UtmCampaign:      atrib.UtmCampaign,
		UtmTerm:          atrib.UtmTerm,
		UtmContent:       atrib.UtmContent,
		CtwaClid:         atrib.CtwaClid,
		AdSourceID:       atrib.AdSourceID,
		AdSourceURL:      atrib.AdSourceURL,
		Referencia:       atrib.Referencia,
		PrimeiraMensagem: texto,
	})

	// 5. Enviar para Webhook Legado da Instância (se configurado)
	if instDB.WebhookURL != "" {
		g.webhook.Enviar(instDB.WebhookURL, instDB.WebhookSegredo, PayloadWebhook{
			InstanciaID: inst.ID,
			Nome:        inst.Nome,
			Evento:      EventoMensagemRecebida,
			Dados:       dadosWebhook,
		})
	}

	// 6. Despachar para Caixas Nativas de Integração (Chatwoot, Typebot, n8n, Kommo CRM, RD Station e Webhooks Universais)
	if integracoes.InstanciaGerenciador != nil {
		integracoes.InstanciaGerenciador.ProcessarMensagemRecebida(ctx, integracoes.MensagemRecebidaPayload{
			InstanciaID:   inst.ID,
			NomeInstancia: inst.Nome,
			IDMensagem:    v.Info.ID,
			Tipo:          tipoMensagem,
			RemetenteJID:  v.Info.Sender.String(),
			RemetenteBR:   remetenteBR,
			NomeContato:   v.Info.PushName,
			ChatJID:       v.Info.Chat.String(),
			DeMim:         v.Info.IsFromMe,
			EhGrupo:       v.Info.IsGroup,
			Texto:         texto,
			TemMidia:      temMidia,
			MimeType:      mimeType,
			NomeArquivo:   nomeArquivo,
			URLMidia:      urlMidiaRemota,
			MidiaBase64:   midiaBase64,
			Timestamp:     v.Info.Timestamp.Unix(),
			Origem:        atrib,
		})
	}
}

// VerificarNumerosWhatsApp higieniza uma lista de telefones, validando quem tem WhatsApp ativo e resolvendo o 9º dígito BR
func (g *GerenciadorInstancias) VerificarNumerosWhatsApp(ctx context.Context, id string, numeros []string) ([]map[string]interface{}, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return nil, err
	}
	if !inst.Cliente.IsConnected() {
		return nil, fmt.Errorf("a instância %s não está conectada", id)
	}

	var consultas []string
	mapaOriginal := make(map[string]string)

	for _, n := range numeros {
		limpo, normErr := NormalizarNumeroBrasil(n)
		if normErr != nil {
			continue
		}
		comMais := "+" + limpo
		consultas = append(consultas, comMais)
		mapaOriginal[comMais] = n

		if alt, ok := AlternarNonoDigitoBrasil(limpo); ok {
			altMais := "+" + alt
			consultas = append(consultas, altMais)
			if _, ja := mapaOriginal[altMais]; !ja {
				mapaOriginal[altMais] = n
			}
		}
	}

	if len(consultas) == 0 {
		return nil, fmt.Errorf("nenhum número válido informado para verificação")
	}

	respostas, err := inst.Cliente.IsOnWhatsApp(ctx, consultas)
	if err != nil {
		return nil, fmt.Errorf("erro ao consultar servidores do WhatsApp: %w", err)
	}

	var resultado []map[string]interface{}
	vistos := make(map[string]bool)

	for _, r := range respostas {
		chave := r.JID.User
		if chave == "" {
			chave = r.Query
		}
		if vistos[chave] && !r.IsIn {
			continue
		}
		vistos[chave] = true

		nomeVerificado := ""
		ehBusiness := false
		if r.VerifiedName != nil && r.VerifiedName.Details != nil {
			nomeVerificado = r.VerifiedName.Details.GetVerifiedName()
			ehBusiness = true
		}

		resultado = append(resultado, map[string]interface{}{
			"consulta":          r.Query,
			"possui_whatsapp":   r.IsIn,
			"jid":               r.JID.ToNonAD().String(),
			"numero_confirmado": r.JID.User,
			"numero_visual":     FormatarNumeroBrasilVisual(r.JID.User),
			"conta_business":    ehBusiness,
			"nome_verificado":   nomeVerificado,
		})
	}

	return resultado, nil
}

// ObterPerfilContato busca Foto de Perfil, Recado/Status, Dispositivos e Perfil Comercial (BusinessProfile)
func (g *GerenciadorInstancias) ObterPerfilContato(ctx context.Context, id, numero string) (map[string]interface{}, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return nil, err
	}
	if !inst.Cliente.IsConnected() {
		return nil, fmt.Errorf("a instância %s não está conectada", id)
	}

	jid, err := ObterJIDPadrao(numero)
	if err != nil {
		return nil, err
	}

	dados := map[string]interface{}{
		"jid":           jid.ToNonAD().String(),
		"numero_visual": FormatarNumeroBrasilVisual(jid.User),
	}

	// 1. Foto de perfil
	if pic, picErr := inst.Cliente.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: false}); picErr == nil && pic != nil {
		dados["foto_perfil_url"] = pic.URL
		dados["foto_id"] = pic.ID
	}

	// 2. Informações de usuário (Recado/Status, Business Name, LID)
	if infoMap, infoErr := inst.Cliente.GetUserInfo(ctx, []types.JID{jid}); infoErr == nil {
		if uInfo, ok := infoMap[jid]; ok {
			dados["recado_status"] = uInfo.Status
			dados["total_dispositivos"] = len(uInfo.Devices)
			if uInfo.VerifiedName != nil && uInfo.VerifiedName.Details != nil {
				dados["nome_verificado"] = uInfo.VerifiedName.Details.GetVerifiedName()
			}
		}
	}

	// 3. Perfil Comercial (Horários, Endereço, E-mail, Categoria)
	if biz, bizErr := inst.Cliente.GetBusinessProfile(ctx, jid); bizErr == nil && biz != nil {
		dados["perfil_comercial"] = map[string]interface{}{
			"endereco":               biz.Address,
			"opcoes_perfil":          biz.ProfileOptions,
			"email":                  biz.Email,
			"categorias":             biz.Categories,
			"fuso_horario":           biz.BusinessHoursTimeZone,
			"horarios_funcionamento": biz.BusinessHours,
		}
	}

	return dados, nil
}

// EnviarPresencaChat envia indicador "digitando...", "gravando_audio", "pausado", "online" ou "offline"
func (g *GerenciadorInstancias) EnviarPresencaChat(ctx context.Context, id, destinatario, estado string) error {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return err
	}
	if !inst.Cliente.IsConnected() {
		return fmt.Errorf("a instância %s não está conectada", id)
	}

	estado = strings.ToLower(strings.TrimSpace(estado))
	if estado == "online" || estado == "available" {
		return inst.Cliente.SendPresence(ctx, types.PresenceAvailable)
	}
	if estado == "offline" || estado == "unavailable" {
		return inst.Cliente.SendPresence(ctx, types.PresenceUnavailable)
	}

	jid, err := ObterJIDPadrao(destinatario)
	if err != nil {
		return err
	}

	switch estado {
	case "digitando", "composing":
		return inst.Cliente.SendChatPresence(ctx, jid, types.ChatPresenceComposing, types.ChatPresenceMediaText)
	case "gravando", "gravando_audio", "recording", "audio":
		return inst.Cliente.SendChatPresence(ctx, jid, types.ChatPresenceComposing, types.ChatPresenceMediaAudio)
	case "pausado", "paused", "parar":
		return inst.Cliente.SendChatPresence(ctx, jid, types.ChatPresencePaused, types.ChatPresenceMediaText)
	default:
		return inst.Cliente.SendChatPresence(ctx, jid, types.ChatPresenceComposing, types.ChatPresenceMediaText)
	}
}

// MarcarComoLido envia recibo de leitura (dois tracinhos azuis) ou de áudio reproduzido
func (g *GerenciadorInstancias) MarcarComoLido(ctx context.Context, id, destinatario, remetente string, idsMensagens []string, audioReproduzido bool) error {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return err
	}
	if !inst.Cliente.IsConnected() {
		return fmt.Errorf("a instância %s não está conectada", id)
	}

	chatJID, err := ObterJIDPadrao(destinatario)
	if err != nil {
		return err
	}

	var senderJID types.JID
	if strings.TrimSpace(remetente) != "" {
		senderJID, _ = ObterJIDPadrao(remetente)
	}

	var msgIDs []types.MessageID
	for _, mid := range idsMensagens {
		if strings.TrimSpace(mid) != "" {
			msgIDs = append(msgIDs, types.MessageID(strings.TrimSpace(mid)))
		}
	}
	if len(msgIDs) == 0 {
		return fmt.Errorf("informe ao menos um ID de mensagem em 'ids_mensagens'")
	}

	tipoRecibo := types.ReceiptTypeRead
	if audioReproduzido {
		tipoRecibo = types.ReceiptTypePlayed
	}

	return inst.Cliente.MarkRead(ctx, msgIDs, time.Now(), chatJID, senderJID, tipoRecibo)
}

// GerenciarConversaAppState permite arquivar, fixar (pin), silenciar (mute) ou marcar conversa inteira como lida/não lida
func (g *GerenciadorInstancias) GerenciarConversaAppState(ctx context.Context, id, destinatario, acao string, ativar bool, duracaoSegundos int) error {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return err
	}
	if !inst.Cliente.IsConnected() {
		return fmt.Errorf("a instância %s não está conectada", id)
	}

	jid, err := ObterJIDPadrao(destinatario)
	if err != nil {
		return err
	}

	var patch appstate.PatchInfo
	switch strings.ToLower(strings.TrimSpace(acao)) {
	case "fixar", "pin":
		patch = appstate.BuildPin(jid, ativar)
	case "arquivar", "archive":
		patch = appstate.BuildArchive(jid, ativar, time.Now(), nil)
	case "silenciar", "mute":
		dur := time.Duration(duracaoSegundos) * time.Second
		if dur <= 0 && ativar {
			dur = 8 * time.Hour
		}
		patch = appstate.BuildMute(jid, ativar, dur)
	case "marcar_lido", "read":
		patch = appstate.BuildMarkChatAsRead(jid, ativar, time.Now(), nil)
	default:
		return fmt.Errorf("ação de conversa inválida (use: fixar, arquivar, silenciar, marcar_lido)")
	}

	return inst.Cliente.SendAppState(ctx, patch)
}

// GerenciarEtiquetaBusiness cria/edita etiquetas do WhatsApp Business ou aplica etiqueta a um chat
func (g *GerenciadorInstancias) GerenciarEtiquetaBusiness(ctx context.Context, id, acao, destinatario, etiquetaID, nomeEtiqueta string, cor int32, ativo bool) error {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return err
	}
	if !inst.Cliente.IsConnected() {
		return fmt.Errorf("a instância %s não está conectada", id)
	}

	if etiquetaID == "" {
		etiquetaID = "1"
	}

	var patch appstate.PatchInfo
	switch strings.ToLower(strings.TrimSpace(acao)) {
	case "criar", "editar":
		if strings.TrimSpace(nomeEtiqueta) == "" {
			return fmt.Errorf("nome_etiqueta é obrigatório para criar ou editar etiqueta")
		}
		patch = appstate.BuildLabelEdit(etiquetaID, nomeEtiqueta, cor, !ativo)
	case "aplicar", "associar", "chat":
		jid, err := ObterJIDPadrao(destinatario)
		if err != nil {
			return err
		}
		patch = appstate.BuildLabelChat(jid, etiquetaID, ativo)
	default:
		return fmt.Errorf("ação de etiqueta inválida (use 'criar' ou 'aplicar')")
	}

	return inst.Cliente.SendAppState(ctx, patch)
}

// ============================================================================
// PACOTE 3: GRUPOS, COMUNIDADES, STATUS 24H E CANAIS (NEWSLETTERS)
// ============================================================================

// ListarGrupos retorna todos os grupos em que a instância participa
func (g *GerenciadorInstancias) ListarGrupos(ctx context.Context, id string) ([]map[string]interface{}, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return nil, err
	}
	if !inst.Cliente.IsConnected() {
		return nil, fmt.Errorf("a instância %s não está conectada", id)
	}

	grupos, err := inst.Cliente.GetJoinedGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("erro ao listar grupos: %w", err)
	}

	var lista []map[string]interface{}
	for _, grp := range grupos {
		lista = append(lista, map[string]interface{}{
			"jid":                 grp.JID.String(),
			"nome":                grp.Name,
			"descricao":           grp.Topic,
			"total_participantes": len(grp.Participants),
			"apenas_admins_falam": grp.IsAnnounce,
			"dados_travados":      grp.IsLocked,
			"eh_comunidade":       grp.IsParent,
			"criado_em":           grp.GroupCreated,
		})
	}
	return lista, nil
}

// ObterInfoGrupo retorna detalhes completos e lista de participantes/admins de um grupo
func (g *GerenciadorInstancias) ObterInfoGrupo(ctx context.Context, id, grupoJIDStr string) (map[string]interface{}, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return nil, err
	}
	if !inst.Cliente.IsConnected() {
		return nil, fmt.Errorf("a instância %s não está conectada", id)
	}

	if !strings.Contains(grupoJIDStr, "@") {
		grupoJIDStr += "@g.us"
	}
	jid, err := types.ParseJID(grupoJIDStr)
	if err != nil {
		return nil, fmt.Errorf("JID de grupo inválido: %w", err)
	}

	grp, err := inst.Cliente.GetGroupInfo(ctx, jid)
	if err != nil {
		return nil, fmt.Errorf("erro ao obter informações do grupo: %w", err)
	}

	var membros []map[string]interface{}
	for _, p := range grp.Participants {
		membros = append(membros, map[string]interface{}{
			"jid":          p.JID.ToNonAD().String(),
			"numero":       FormatarNumeroBrasilVisual(p.PhoneNumber.User),
			"admin":        p.IsAdmin,
			"super_admin":  p.IsSuperAdmin,
			"nome_exibido": p.DisplayName,
		})
	}

	return map[string]interface{}{
		"jid":                 grp.JID.String(),
		"nome":                grp.Name,
		"descricao":           grp.Topic,
		"apenas_admins_falam": grp.IsAnnounce,
		"dados_travados":      grp.IsLocked,
		"aprovacao_entrada":   grp.IsJoinApprovalRequired,
		"total_participantes": len(membros),
		"participantes":       membros,
		"criado_em":           grp.GroupCreated,
	}, nil
}

// CriarGrupo cria um novo grupo ou comunidade no WhatsApp
func (g *GerenciadorInstancias) CriarGrupo(ctx context.Context, id, nome, descricao string, participantes []string, apenasAdmins, travado, comunidade bool) (map[string]interface{}, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return nil, err
	}
	if !inst.Cliente.IsConnected() {
		return nil, fmt.Errorf("a instância %s não está conectada", id)
	}

	nome = strings.TrimSpace(nome)
	if len([]rune(nome)) > 25 {
		runes := []rune(nome)
		nome = string(runes[:25])
	}

	var jids []types.JID
	for _, p := range participantes {
		if pJID, pErr := ObterJIDPadrao(p); pErr == nil {
			jids = append(jids, pJID.ToNonAD())
		}
	}

	req := whatsmeow.ReqCreateGroup{
		Name:         nome,
		Participants: jids,
		GroupAnnounce: types.GroupAnnounce{
			IsAnnounce: apenasAdmins,
		},
		GroupLocked: types.GroupLocked{
			IsLocked: travado,
		},
		GroupParent: types.GroupParent{
			IsParent: comunidade,
		},
	}

	grp, err := inst.Cliente.CreateGroup(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("falha ao criar grupo: %w", err)
	}

	if strings.TrimSpace(descricao) != "" {
		_ = inst.Cliente.SetGroupDescription(ctx, grp.JID, strings.TrimSpace(descricao))
	}

	linkConvite, _ := inst.Cliente.GetGroupInviteLink(ctx, grp.JID, false)

	return map[string]interface{}{
		"jid":          grp.JID.String(),
		"nome":         grp.Name,
		"descricao":    descricao,
		"link_convite": linkConvite,
	}, nil
}

// GerenciarParticipantesGrupo adiciona, remove, promove a admin ou rebaixa membros de um grupo
func (g *GerenciadorInstancias) GerenciarParticipantesGrupo(ctx context.Context, id, grupoJIDStr, acao string, numeros []string) ([]map[string]interface{}, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return nil, err
	}
	if !inst.Cliente.IsConnected() {
		return nil, fmt.Errorf("a instância %s não está conectada", id)
	}

	if !strings.Contains(grupoJIDStr, "@") {
		grupoJIDStr += "@g.us"
	}
	grupoJID, err := types.ParseJID(grupoJIDStr)
	if err != nil {
		return nil, fmt.Errorf("JID de grupo inválido: %w", err)
	}

	var jids []types.JID
	for _, n := range numeros {
		if pJID, pErr := ObterJIDPadrao(n); pErr == nil {
			jids = append(jids, pJID.ToNonAD())
		}
	}
	if len(jids) == 0 {
		return nil, fmt.Errorf("nenhum participante válido informado")
	}

	var mudanca whatsmeow.ParticipantChange
	switch strings.ToLower(strings.TrimSpace(acao)) {
	case "adicionar", "add":
		mudanca = whatsmeow.ParticipantChangeAdd
	case "remover", "remove", "kick":
		mudanca = whatsmeow.ParticipantChangeRemove
	case "promover", "promote", "admin":
		mudanca = whatsmeow.ParticipantChangePromote
	case "rebaixar", "demote":
		mudanca = whatsmeow.ParticipantChangeDemote
	default:
		return nil, fmt.Errorf("ação inválida (use: adicionar, remover, promover, rebaixar)")
	}

	res, err := inst.Cliente.UpdateGroupParticipants(ctx, grupoJID, jids, mudanca)
	if err != nil {
		return nil, fmt.Errorf("erro ao atualizar participantes do grupo: %w", err)
	}

	var saida []map[string]interface{}
	for _, p := range res {
		saida = append(saida, map[string]interface{}{
			"jid":  p.JID.String(),
			"erro": p.Error,
		})
	}
	return saida, nil
}

// ConfigurarGrupo altera nome, descrição, foto, permissões (apenas admins falam) e mensagens temporárias
func (g *GerenciadorInstancias) ConfigurarGrupo(ctx context.Context, id, grupoJIDStr string, cfg ConfiguracaoGrupoReq) error {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return err
	}
	if !inst.Cliente.IsConnected() {
		return fmt.Errorf("a instância %s não está conectada", id)
	}

	if !strings.Contains(grupoJIDStr, "@") {
		grupoJIDStr += "@g.us"
	}
	grupoJID, err := types.ParseJID(grupoJIDStr)
	if err != nil {
		return fmt.Errorf("JID de grupo inválido: %w", err)
	}

	if cfg.Nome != nil && strings.TrimSpace(*cfg.Nome) != "" {
		if err := inst.Cliente.SetGroupName(ctx, grupoJID, strings.TrimSpace(*cfg.Nome)); err != nil {
			return fmt.Errorf("falha ao alterar nome do grupo: %w", err)
		}
	}
	if cfg.Descricao != nil {
		if err := inst.Cliente.SetGroupDescription(ctx, grupoJID, strings.TrimSpace(*cfg.Descricao)); err != nil {
			return fmt.Errorf("falha ao alterar descrição do grupo: %w", err)
		}
	}
	if cfg.ApenasAdminsFalam != nil {
		if err := inst.Cliente.SetGroupAnnounce(ctx, grupoJID, *cfg.ApenasAdminsFalam); err != nil {
			return fmt.Errorf("falha ao configurar permissão de envio: %w", err)
		}
	}
	if cfg.DadosTravados != nil {
		if err := inst.Cliente.SetGroupLocked(ctx, grupoJID, *cfg.DadosTravados); err != nil {
			return fmt.Errorf("falha ao configurar bloqueio de dados do grupo: %w", err)
		}
	}
	if cfg.AprovacaoEntrada != nil {
		if err := inst.Cliente.SetGroupJoinApprovalMode(ctx, grupoJID, *cfg.AprovacaoEntrada); err != nil {
			return fmt.Errorf("falha ao configurar aprovação de entrada: %w", err)
		}
	}
	if cfg.FotoBase64 != "" {
		if idx := strings.Index(cfg.FotoBase64, ","); idx != -1 {
			cfg.FotoBase64 = cfg.FotoBase64[idx+1:]
		}
		if rawImg, decErr := base64.StdEncoding.DecodeString(cfg.FotoBase64); decErr == nil && len(rawImg) > 0 {
			if _, photoErr := inst.Cliente.SetGroupPhoto(ctx, grupoJID, rawImg); photoErr != nil {
				return fmt.Errorf("falha ao atualizar foto do grupo: %w", photoErr)
			}
		}
	}
	if cfg.TemporizadorExpira != "" {
		if dur, ok := whatsmeow.ParseDisappearingTimerString(cfg.TemporizadorExpira); ok {
			_ = inst.Cliente.SetDisappearingTimer(ctx, grupoJID, dur, time.Now())
		}
	}

	return nil
}

// ObterLinkConviteGrupo obtém ou redefine o link de convite (https://chat.whatsapp.com/...)
func (g *GerenciadorInstancias) ObterLinkConviteGrupo(ctx context.Context, id, grupoJIDStr string, resetar bool) (string, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return "", err
	}
	if !inst.Cliente.IsConnected() {
		return "", fmt.Errorf("a instância %s não está conectada", id)
	}

	if !strings.Contains(grupoJIDStr, "@") {
		grupoJIDStr += "@g.us"
	}
	grupoJID, err := types.ParseJID(grupoJIDStr)
	if err != nil {
		return "", fmt.Errorf("JID de grupo inválido: %w", err)
	}

	return inst.Cliente.GetGroupInviteLink(ctx, grupoJID, resetar)
}

// EntrarGrupoPorLink entra em um grupo usando o link ou código de convite
func (g *GerenciadorInstancias) EntrarGrupoPorLink(ctx context.Context, id, linkOuCodigo string) (string, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return "", err
	}
	if !inst.Cliente.IsConnected() {
		return "", fmt.Errorf("a instância %s não está conectada", id)
	}

	codigo := strings.TrimSpace(linkOuCodigo)
	codigo = strings.TrimPrefix(codigo, "https://chat.whatsapp.com/")
	codigo = strings.TrimPrefix(codigo, "http://chat.whatsapp.com/")

	jid, err := inst.Cliente.JoinGroupWithLink(ctx, codigo)
	if err != nil {
		return "", fmt.Errorf("falha ao entrar no grupo por link: %w", err)
	}
	return jid.String(), nil
}

// PublicarStatus publica um Status / Story de 24h (texto colorido, imagem ou vídeo) para os contatos da instância
func (g *GerenciadorInstancias) PublicarStatus(ctx context.Context, id, tipo, texto, corFundoHex string, dadosMidia []byte, legenda string) (string, error) {
	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return "", err
	}
	if !inst.Cliente.IsConnected() {
		return "", fmt.Errorf("a instância %s não está conectada", id)
	}

	var msg *waProto.Message
	tipo = strings.ToLower(strings.TrimSpace(tipo))

	switch tipo {
	case "imagem", "image", "foto":
		if len(dadosMidia) == 0 {
			return "", fmt.Errorf("dados de imagem são obrigatórios para Status de foto")
		}
		mimeType := http.DetectContentType(dadosMidia)
		upload, upErr := inst.Cliente.Upload(ctx, dadosMidia, whatsmeow.MediaImage)
		if upErr != nil {
			return "", fmt.Errorf("falha no upload da foto para Status: %w", upErr)
		}
		msg = &waProto.Message{
			ImageMessage: &waProto.ImageMessage{
				URL:           proto.String(upload.URL),
				DirectPath:    proto.String(upload.DirectPath),
				MediaKey:      upload.MediaKey,
				Mimetype:      proto.String(mimeType),
				FileEncSHA256: upload.FileEncSHA256,
				FileSHA256:    upload.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(dadosMidia))),
				Caption:       proto.String(legenda),
			},
		}
	case "video":
		if len(dadosMidia) == 0 {
			return "", fmt.Errorf("dados de vídeo são obrigatórios para Status de vídeo")
		}
		upload, upErr := inst.Cliente.Upload(ctx, dadosMidia, whatsmeow.MediaVideo)
		if upErr != nil {
			return "", fmt.Errorf("falha no upload do vídeo para Status: %w", upErr)
		}
		msg = &waProto.Message{
			VideoMessage: &waProto.VideoMessage{
				URL:           proto.String(upload.URL),
				DirectPath:    proto.String(upload.DirectPath),
				MediaKey:      upload.MediaKey,
				Mimetype:      proto.String("video/mp4"),
				FileEncSHA256: upload.FileEncSHA256,
				FileSHA256:    upload.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(dadosMidia))),
				Caption:       proto.String(legenda),
			},
		}
	default:
		if strings.TrimSpace(texto) == "" {
			return "", fmt.Errorf("texto é obrigatório para Status de texto")
		}
		bgArgb := uint32(0xFF059669) // Verde Esmeralda padrão
		corLimpa := strings.TrimPrefix(strings.TrimSpace(corFundoHex), "#")
		if len(corLimpa) == 6 {
			if parsed, pErr := strconv.ParseUint("FF"+corLimpa, 16, 32); pErr == nil {
				bgArgb = uint32(parsed)
			}
		}
		fontType := waProto.ExtendedTextMessage_FontType(0)
		msg = &waProto.Message{
			ExtendedTextMessage: &waProto.ExtendedTextMessage{
				Text:           proto.String(texto),
				BackgroundArgb: proto.Uint32(bgArgb),
				TextArgb:       proto.Uint32(0xFFFFFFFF),
				Font:           &fontType,
			},
		}
	}

	resp, err := inst.Cliente.SendMessage(ctx, types.StatusBroadcastJID, msg)
	if err != nil {
		return "", fmt.Errorf("falha ao publicar Status 24h: %w", err)
	}
	_ = g.db.GravarLogMensagem(id, resp.ID, "status_24h", "status@broadcast", "ENVIADA", "ENTREGUE", "")
	return resp.ID, nil
}

// ============================================================================
// PACOTE 4: CONFIGURAÇÃO ANTI-BAN E PROXY POR INSTÂNCIA
// ============================================================================

// ConfigurarInstanciaAvancada salva e aplica imediatamente Proxy (SOCKS5/HTTP), Rejeição de Chamadas, Ignorar Grupos e Leitura Automática
func (g *GerenciadorInstancias) ConfigurarInstanciaAvancada(ctx context.Context, id string, cfg ConfiguracaoInstanciaReq) error {
	cfg.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	cfg.MensagemChamadaRejeitada = strings.TrimSpace(cfg.MensagemChamadaRejeitada)

	inst, err := g.ObterInstanciaAtiva(id)
	if err != nil {
		return err
	}

	if inst.Cliente != nil {
		if cfg.ProxyURL != "" {
			if proxyErr := inst.Cliente.SetProxyAddress(cfg.ProxyURL); proxyErr != nil {
				return fmt.Errorf("URL de proxy inválida (use http://usuario:senha@ip:porta ou socks5://...): %w", proxyErr)
			}
		} else {
			_ = inst.Cliente.SetProxyAddress("")
		}

		if cfg.SempreOnline && inst.Cliente.IsConnected() {
			_ = inst.Cliente.SendPresence(ctx, types.PresenceAvailable)
		}
	}

	return g.db.AtualizarConfiguracoesInstancia(
		id,
		cfg.IgnorarGrupos,
		cfg.RejeitarChamadas,
		cfg.LeituraAutomatica,
		cfg.SempreOnline,
		cfg.MensagemChamadaRejeitada,
		cfg.ProxyURL,
	)
}

