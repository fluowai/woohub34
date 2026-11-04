package core

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestExtrairAtribuicaoOrigem(t *testing.T) {
	// Cenário 1: Mensagem com UTMs no texto (Google Ads)
	textoGoogle := "Olá, tenho interesse na consultoria! ?utm_source=google&utm_medium=cpc&utm_campaign=Campanha_BlackFriday_2026&gclid=Cj0KCQjww5u2BhCqARIsAP"
	atrib1 := ExtrairAtribuicaoOrigem(nil, textoGoogle)
	if atrib1.Canal != "Google Ads" {
		t.Errorf("Esperava 'Google Ads', obteve '%s'", atrib1.Canal)
	}
	if atrib1.UtmCampaign != "Campanha_BlackFriday_2026" {
		t.Errorf("Esperava campanha 'Campanha_BlackFriday_2026', obteve '%s'", atrib1.UtmCampaign)
	}
	if atrib1.Gclid == "" {
		t.Errorf("Esperava gclid capturado")
	}

	// Cenário 2: Mensagem com tag manual [origem: ...]
	textoTag := "Olá! Quero saber o preço do software [origem: Campanha Meta Stories Vendas]"
	atrib2 := ExtrairAtribuicaoOrigem(nil, textoTag)
	if atrib2.Campanha != "Campanha Meta Stories Vendas" {
		t.Errorf("Esperava campanha 'Campanha Meta Stories Vendas', obteve '%s'", atrib2.Campanha)
	}

	// Cenário 3: Metadados Nativos Meta Ads (Click-to-WhatsApp CTWA)
	title := "Curso de Automação Pro 2026"
	sourceApp := "instagram"
	sourceID := "ad_123456789"
	ctwaClid := "ctwa_clid_abc_999"
	sourceURL := "https://instagram.com/p/abcdef"

	msgCTWA := &events.Message{
		Message: &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("Vi o anúncio de vocês e quero mais detalhes"),
				ContextInfo: &waE2E.ContextInfo{
					ExternalAdReply: &waE2E.ContextInfo_ExternalAdReplyInfo{
						Title:       &title,
						SourceApp:   &sourceApp,
						SourceID:    &sourceID,
						CtwaClid:    &ctwaClid,
						SourceURL:   &sourceURL,
					},
				},
			},
		},
	}

	atrib3 := ExtrairAtribuicaoOrigem(msgCTWA, "Vi o anúncio de vocês e quero mais detalhes")
	if atrib3.Canal != "Meta Ads (Instagram)" {
		t.Errorf("Esperava 'Meta Ads (Instagram)', obteve '%s'", atrib3.Canal)
	}
	if atrib3.Campanha != "Curso de Automação Pro 2026" {
		t.Errorf("Esperava 'Curso de Automação Pro 2026', obteve '%s'", atrib3.Campanha)
	}
	if atrib3.CtwaClid != "ctwa_clid_abc_999" {
		t.Errorf("Esperava ctwa_clid_abc_999, obteve '%s'", atrib3.CtwaClid)
	}
}
