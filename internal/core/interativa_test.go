package core

import (
	"encoding/json"
	"testing"

	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestConstrucaoBotoesInterativos(t *testing.T) {
	botoes := []BotaoInterativo{
		{
			Tipo:  "url",
			Texto: "🔗 Acessar Site",
			Valor: "https://woohub.com.br",
		},
		{
			Tipo:  "copiar",
			Texto: "📋 Copiar Chave Pix",
			Valor: "00020126580014br.gov.bcb.pix0136teste",
		},
		{
			Tipo:  "resposta",
			Texto: "💬 Falar com Suporte",
			Id:    "btn_suporte_1",
		},
		{
			Tipo:  "chamada",
			Texto: "📞 Ligar Agora",
			Valor: "+5511999998888",
		},
	}

	var nativeButtons []*waProto.InteractiveMessage_NativeFlowMessage_NativeFlowButton
	for _, b := range botoes {
		var nomeTipo string
		var paramsBytes []byte

		switch b.Tipo {
		case "url":
			nomeTipo = "cta_url"
			paramsBytes, _ = json.Marshal(map[string]string{
				"display_text": b.Texto,
				"url":          b.Valor,
				"merchant_url": b.Valor,
			})
		case "copiar":
			nomeTipo = "cta_copy"
			paramsBytes, _ = json.Marshal(map[string]string{
				"display_text": b.Texto,
				"copy_code":    b.Valor,
			})
		case "resposta":
			nomeTipo = "quick_reply"
			paramsBytes, _ = json.Marshal(map[string]string{
				"display_text": b.Texto,
				"id":           b.Id,
			})
		case "chamada":
			nomeTipo = "cta_call"
			paramsBytes, _ = json.Marshal(map[string]string{
				"display_text": b.Texto,
				"phone_number": b.Valor,
			})
		}

		nativeButtons = append(nativeButtons, &waProto.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
			Name:             proto.String(nomeTipo),
			ButtonParamsJSON: proto.String(string(paramsBytes)),
		})
	}

	if len(nativeButtons) != 4 {
		t.Fatalf("Esperava 4 botões nativos, obteve %d", len(nativeButtons))
	}

	// Validar botão URL
	if nativeButtons[0].GetName() != "cta_url" {
		t.Errorf("Esperava cta_url, obteve %s", nativeButtons[0].GetName())
	}
	var paramsURL map[string]string
	_ = json.Unmarshal([]byte(nativeButtons[0].GetButtonParamsJSON()), &paramsURL)
	if paramsURL["url"] != "https://woohub.com.br" {
		t.Errorf("URL incorreta no botão: %s", paramsURL["url"])
	}

	// Validar botão Copiar Pix
	if nativeButtons[1].GetName() != "cta_copy" {
		t.Errorf("Esperava cta_copy, obteve %s", nativeButtons[1].GetName())
	}
	var paramsPix map[string]string
	_ = json.Unmarshal([]byte(nativeButtons[1].GetButtonParamsJSON()), &paramsPix)
	if paramsPix["copy_code"] != "00020126580014br.gov.bcb.pix0136teste" {
		t.Errorf("Código Pix incorreto: %s", paramsPix["copy_code"])
	}

	// Validar botão Resposta Rápida
	if nativeButtons[2].GetName() != "quick_reply" {
		t.Errorf("Esperava quick_reply, obteve %s", nativeButtons[2].GetName())
	}
	var paramsReply map[string]string
	_ = json.Unmarshal([]byte(nativeButtons[2].GetButtonParamsJSON()), &paramsReply)
	if paramsReply["id"] != "btn_suporte_1" {
		t.Errorf("ID incorreto: %s", paramsReply["id"])
	}

	// Validar botão Chamada
	if nativeButtons[3].GetName() != "cta_call" {
		t.Errorf("Esperava cta_call, obteve %s", nativeButtons[3].GetName())
	}

	// Validar estrutura protobuf completa
	msgVersion := int32(3)
	msg := &waProto.Message{
		InteractiveMessage: &waProto.InteractiveMessage{
			Header: &waProto.InteractiveMessage_Header{
				Title:              proto.String("Título Demonstração"),
				HasMediaAttachment: proto.Bool(false),
			},
			Body: &waProto.InteractiveMessage_Body{
				Text: proto.String("Texto explicativo com botões"),
			},
			Footer: &waProto.InteractiveMessage_Footer{
				Text: proto.String("Rodapé WooHub"),
			},
			InteractiveMessage: &waProto.InteractiveMessage_NativeFlowMessage_{
				NativeFlowMessage: &waProto.InteractiveMessage_NativeFlowMessage{
					Buttons:        nativeButtons,
					MessageVersion: &msgVersion,
				},
			},
		},
	}

	bytes, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("Erro ao serializar protobuf do InteractiveMessage: %v", err)
	}
	if len(bytes) == 0 {
		t.Fatal("Protobuf serializado vazio")
	}

	// Deserializar e conferir
	var msgDecodificada waProto.Message
	if err := proto.Unmarshal(bytes, &msgDecodificada); err != nil {
		t.Fatalf("Erro ao deserializar protobuf: %v", err)
	}
	if msgDecodificada.GetInteractiveMessage().GetBody().GetText() != "Texto explicativo com botões" {
		t.Errorf("Texto do corpo inconsistente: %s", msgDecodificada.GetInteractiveMessage().GetBody().GetText())
	}
}
