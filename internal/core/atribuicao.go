package core

import (
	"net/url"
	"regexp"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
)

// DadosAtribuicao consolida a inteligência de rastreamento de origem da mensagem
type DadosAtribuicao struct {
	Canal            string            `json:"canal"`             // Ex: "Meta Ads (Instagram)", "Google Ads", "Site / Landing Page", "Orgânico"
	Campanha         string            `json:"campanha"`          // Nome da campanha ou ID
	UtmSource        string            `json:"utm_source"`        // Origem (google, meta, instagram, etc.)
	UtmMedium        string            `json:"utm_medium"`        // Mídia (cpc, stories, feed, email)
	UtmCampaign      string            `json:"utm_campaign"`      // Nome da campanha
	UtmTerm          string            `json:"utm_term"`          // Palavra-chave ou termo
	UtmContent       string            `json:"utm_content"`       // Variação do criativo/anúncio
	CtwaClid         string            `json:"ctwa_clid"`         // Click-to-WhatsApp Click ID (Meta Ads)
	AdSourceID       string            `json:"ad_source_id"`      // ID do anúncio ou post
	AdSourceURL      string            `json:"ad_source_url"`     // Link de origem do anúncio
	Referencia       string            `json:"referencia"`        // Ref tag de link wa.me
	Gclid            string            `json:"gclid,omitempty"`   // Google Ads Click ID
	Fbclid           string            `json:"fbclid,omitempty"`  // Facebook Click ID
	TituloAnuncio    string            `json:"titulo_anuncio,omitempty"`
	CorpoAnuncio     string            `json:"corpo_anuncio,omitempty"`
	PrimeiraMensagem string            `json:"primeira_mensagem,omitempty"`
	ParametrosExtras map[string]string `json:"parametros_extras,omitempty"`
}

var (
	reUtmSource   = regexp.MustCompile(`(?i)[?&]utm_source=([^\s&]+)|utm_source:\s*([^\s,;\]]+)`)
	reUtmMedium   = regexp.MustCompile(`(?i)[?&]utm_medium=([^\s&]+)|utm_medium:\s*([^\s,;\]]+)`)
	reUtmCampaign = regexp.MustCompile(`(?i)[?&]utm_campaign=([^\s&]+)|utm_campaign:\s*([^\s,;\]]+)`)
	reUtmTerm     = regexp.MustCompile(`(?i)[?&]utm_term=([^\s&]+)|utm_term:\s*([^\s,;\]]+)`)
	reUtmContent  = regexp.MustCompile(`(?i)[?&]utm_content=([^\s&]+)|utm_content:\s*([^\s,;\]]+)`)
	reGclid       = regexp.MustCompile(`(?i)[?&]gclid=([^\s&]+)|gclid:\s*([^\s,;\]]+)`)
	reFbclid      = regexp.MustCompile(`(?i)[?&]fbclid=([^\s&]+)|fbclid:\s*([^\s,;\]]+)`)
	reTagOrigem   = regexp.MustCompile(`(?i)\[(?:origem|campanha|ref|fonte|canal):\s*([^\]]+)\]`)
)

// ExtrairAtribuicaoOrigem rastreia a procedência de onde veio o lead no WhatsApp:
// 1. Metadados Nativos Meta Ads (Click to WhatsApp Ads / CTWA ContextInfo)
// 2. Parâmetros UTM e Tags embutidas na mensagem ou wa.me link
// 3. Heurística contextual (palavras-chave de campanhas de tráfego pago)
func ExtrairAtribuicaoOrigem(v *events.Message, textoMensagem string) DadosAtribuicao {
	atrib := DadosAtribuicao{
		Canal:            "Orgânico / Direto",
		PrimeiraMensagem: textoMensagem,
		ParametrosExtras: make(map[string]string),
	}

	if v == nil || v.Message == nil {
		return extrairDeTexto(textoMensagem, atrib)
	}

	// 1. Buscar ContextInfo nos tipos de mensagem suportados
	var ci *waE2E.ContextInfo
	switch {
	case v.Message.GetExtendedTextMessage() != nil:
		ci = v.Message.GetExtendedTextMessage().GetContextInfo()
	case v.Message.GetImageMessage() != nil:
		ci = v.Message.GetImageMessage().GetContextInfo()
	case v.Message.GetVideoMessage() != nil:
		ci = v.Message.GetVideoMessage().GetContextInfo()
	case v.Message.GetDocumentMessage() != nil:
		ci = v.Message.GetDocumentMessage().GetContextInfo()
	case v.Message.GetAudioMessage() != nil:
		ci = v.Message.GetAudioMessage().GetContextInfo()
	}

	// 2. Analisar Metadados Nativos Meta Ads (CTWA)
	if ci != nil {
		if ad := ci.GetExternalAdReply(); ad != nil {
			atrib.TituloAnuncio = ad.GetTitle()
			atrib.CorpoAnuncio = ad.GetBody()
			atrib.AdSourceID = ad.GetSourceID()
			atrib.AdSourceURL = ad.GetSourceURL()
			atrib.CtwaClid = ad.GetCtwaClid()
			atrib.Referencia = ad.GetRef()

			appOrigem := strings.ToLower(strings.TrimSpace(ad.GetSourceApp()))
			if appOrigem == "" {
				appOrigem = strings.ToLower(strings.TrimSpace(ad.GetSourceType()))
			}

			switch {
			case strings.Contains(appOrigem, "instagram"):
				atrib.Canal = "Meta Ads (Instagram)"
				atrib.UtmSource = "instagram"
			case strings.Contains(appOrigem, "facebook"):
				atrib.Canal = "Meta Ads (Facebook)"
				atrib.UtmSource = "facebook"
			default:
				atrib.Canal = "Meta Ads (Click-to-WhatsApp)"
				atrib.UtmSource = "meta_ads"
			}
			atrib.UtmMedium = "cpc"

			if ad.GetTitle() != "" {
				atrib.Campanha = ad.GetTitle()
				atrib.UtmCampaign = ad.GetTitle()
			} else if ad.GetSourceID() != "" {
				atrib.Campanha = "Ad ID: " + ad.GetSourceID()
				atrib.UtmCampaign = ad.GetSourceID()
			}
		}

		if utm := ci.GetUtm(); utm != nil {
			if utm.GetUtmSource() != "" {
				atrib.UtmSource = utm.GetUtmSource()
			}
			if utm.GetUtmCampaign() != "" {
				atrib.UtmCampaign = utm.GetUtmCampaign()
				if atrib.Campanha == "" {
					atrib.Campanha = utm.GetUtmCampaign()
				}
			}
		}
	}

	// 3. Analisar Texto da Mensagem (UTMs e Tags)
	return extrairDeTexto(textoMensagem, atrib)
}

func extrairDeTexto(texto string, atrib DadosAtribuicao) DadosAtribuicao {
	if strings.TrimSpace(texto) == "" {
		return atrib
	}

	// Se o texto contiver URL codificada com query string, decodifica
	textoDecodificado, err := url.QueryUnescape(texto)
	if err == nil && textoDecodificado != "" {
		texto = textoDecodificado
	}

	// Extração de UTMs via Regex
	if m := reUtmSource.FindStringSubmatch(texto); len(m) > 0 {
		valor := m[1]
		if valor == "" && len(m) > 2 {
			valor = m[2]
		}
		if valor != "" && atrib.UtmSource == "" {
			atrib.UtmSource = strings.TrimSpace(valor)
		}
	}

	if m := reUtmMedium.FindStringSubmatch(texto); len(m) > 0 {
		valor := m[1]
		if valor == "" && len(m) > 2 {
			valor = m[2]
		}
		if valor != "" && atrib.UtmMedium == "" {
			atrib.UtmMedium = strings.TrimSpace(valor)
		}
	}

	if m := reUtmCampaign.FindStringSubmatch(texto); len(m) > 0 {
		valor := m[1]
		if valor == "" && len(m) > 2 {
			valor = m[2]
		}
		if valor != "" {
			atrib.UtmCampaign = strings.TrimSpace(valor)
			if atrib.Campanha == "" || atrib.Campanha == "Orgânico / Direto" {
				atrib.Campanha = strings.TrimSpace(valor)
			}
		}
	}

	if m := reUtmTerm.FindStringSubmatch(texto); len(m) > 0 {
		valor := m[1]
		if valor == "" && len(m) > 2 {
			valor = m[2]
		}
		if valor != "" {
			atrib.UtmTerm = strings.TrimSpace(valor)
		}
	}

	if m := reUtmContent.FindStringSubmatch(texto); len(m) > 0 {
		valor := m[1]
		if valor == "" && len(m) > 2 {
			valor = m[2]
		}
		if valor != "" {
			atrib.UtmContent = strings.TrimSpace(valor)
		}
	}

	if m := reGclid.FindStringSubmatch(texto); len(m) > 0 {
		valor := m[1]
		if valor == "" && len(m) > 2 {
			valor = m[2]
		}
		if valor != "" {
			atrib.Gclid = strings.TrimSpace(valor)
			atrib.Canal = "Google Ads"
			if atrib.UtmSource == "" {
				atrib.UtmSource = "google"
			}
			if atrib.UtmMedium == "" {
				atrib.UtmMedium = "cpc"
			}
		}
	}

	if m := reFbclid.FindStringSubmatch(texto); len(m) > 0 {
		valor := m[1]
		if valor == "" && len(m) > 2 {
			valor = m[2]
		}
		if valor != "" {
			atrib.Fbclid = strings.TrimSpace(valor)
			if !strings.HasPrefix(atrib.Canal, "Meta Ads") {
				atrib.Canal = "Meta Ads (Facebook/Instagram)"
			}
			if atrib.UtmSource == "" {
				atrib.UtmSource = "meta"
			}
		}
	}

	// Extração de tags manuais como [origem: Google Ads] ou [campanha: Black Friday 2026]
	if m := reTagOrigem.FindStringSubmatch(texto); len(m) > 1 {
		tagValor := strings.TrimSpace(m[1])
		if tagValor != "" {
			if atrib.Campanha == "" {
				atrib.Campanha = tagValor
			}
			atrib.Referencia = tagValor
		}
	}

	// Consolidação de canal por UTM Source se ainda estiver genérico
	if atrib.Canal == "Orgânico / Direto" {
		srcLower := strings.ToLower(atrib.UtmSource)
		switch {
		case strings.Contains(srcLower, "google"):
			atrib.Canal = "Google Ads"
		case strings.Contains(srcLower, "insta") || strings.Contains(srcLower, "ig"):
			atrib.Canal = "Meta Ads (Instagram)"
		case strings.Contains(srcLower, "face") || strings.Contains(srcLower, "fb"):
			atrib.Canal = "Meta Ads (Facebook)"
		case strings.Contains(srcLower, "tiktok"):
			atrib.Canal = "TikTok Ads"
		case strings.Contains(srcLower, "linkedin"):
			atrib.Canal = "LinkedIn Ads"
		case strings.Contains(srcLower, "youtube"):
			atrib.Canal = "YouTube"
		case strings.Contains(srcLower, "site") || strings.Contains(srcLower, "landing"):
			atrib.Canal = "Site / Landing Page"
		case srcLower != "":
			atrib.Canal = strings.ToUpper(srcLower)
		}
	}

	return atrib
}
