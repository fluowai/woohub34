package core

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// InfoContatoResolvido armazena os metadados humanos e fotos de contatos, canais e grupos do WhatsApp
type InfoContatoResolvido struct {
	Nome     string
	Numero   string
	FotoURL  string
	EhGrupo  bool
	PushName string
}

// ResolverInfoContato resolve LIDs, Newsletters/Canais, Grupos e Contatos para nomes reais, números de telefone reais e fotos
func (g *GerenciadorInstancias) ResolverInfoContato(ctx context.Context, instanciaID string, jid types.JID, pushNameHint string) InfoContatoResolvido {
	info := InfoContatoResolvido{
		Nome:     pushNameHint,
		PushName: pushNameHint,
	}

	inst, err := g.ObterInstanciaAtiva(instanciaID)
	if err != nil || inst == nil || inst.Cliente == nil {
		if info.Nome == "" {
			info.Nome = jid.User
		}
		info.Numero = jid.User
		return info
	}

	// 1. Canais / Newsletters (@newsletter)
	if jid.Server == types.NewsletterServer {
		info.EhGrupo = true
		if meta, errNL := inst.Cliente.GetNewsletterInfo(ctx, jid); errNL == nil && meta != nil {
			if meta.ThreadMeta.Name.Text != "" {
				info.Nome = meta.ThreadMeta.Name.Text
			}
			if meta.ThreadMeta.Picture.URL != "" && (strings.HasPrefix(meta.ThreadMeta.Picture.URL, "http://") || strings.HasPrefix(meta.ThreadMeta.Picture.URL, "https://")) {
				info.FotoURL = meta.ThreadMeta.Picture.URL
			}
		}
		if info.Nome == "" && pushNameHint != "" {
			info.Nome = pushNameHint
		}
		if info.Nome == "" {
			info.Nome = "Canal " + jid.User
		}
		if info.FotoURL == "" {
			if pic, errPic := inst.Cliente.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true}); errPic == nil && pic != nil {
				info.FotoURL = pic.URL
			}
		}
		return info
	}

	// 2. Grupos (@g.us)
	if jid.Server == types.GroupServer {
		info.EhGrupo = true
		if grp, errGrp := inst.Cliente.GetGroupInfo(ctx, jid); errGrp == nil && grp != nil {
			if grp.Name != "" {
				info.Nome = grp.Name
			}
		}
		if info.Nome == "" && pushNameHint != "" {
			info.Nome = pushNameHint
		}
		if info.Nome == "" {
			info.Nome = "Grupo " + jid.User
		}
		// Tenta foto com Preview e fallback sem Preview
		if pic, errPic := inst.Cliente.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true}); errPic == nil && pic != nil {
			info.FotoURL = pic.URL
		} else if picFull, errPicFull := inst.Cliente.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: false}); errPicFull == nil && picFull != nil {
			info.FotoURL = picFull.URL
		}
		return info
	}

	// 3. Resolução de Número Real de Telefone
	if jid.Server == types.HiddenUserServer {
		// Contatos com LID (@lid)
		if inst.Cliente.Store != nil && inst.Cliente.Store.LIDs != nil {
			if pn, errLid := inst.Cliente.Store.LIDs.GetPNForLID(ctx, jid); errLid == nil && !pn.IsEmpty() {
				info.Numero = pn.User
			}
		}
		if info.Numero == "" && inst.Cliente.Store != nil && inst.Cliente.Store.Contacts != nil {
			if contatos, errC := inst.Cliente.Store.Contacts.GetAllContacts(ctx); errC == nil {
				for cJID, cInfo := range contatos {
					if cJID.User == jid.User || (pushNameHint != "" && cInfo.PushName == pushNameHint) {
						if cJID.Server == types.DefaultUserServer {
							info.Numero = cJID.User
							if cInfo.PushName != "" {
								info.PushName = cInfo.PushName
							}
							break
						}
					}
				}
			}
		}
	} else if jid.Server == types.DefaultUserServer {
		info.Numero = jid.User
	}

	// 4. Resolução de Nome de Contato (PRIORIDADE ABSOLUTA AO PUSHNAME REAL)
	var pushNameFinal string
	var businessNameFinal string
	var fullNameFinal string

	// A) Hint recebido diretamente da mensagem ou evento em tempo real
	if pushNameHint != "" && pushNameHint != jid.User && !strings.Contains(pushNameHint, "@") {
		pushNameFinal = pushNameHint
	}

	// B) Consulta nos contatos salvos no SQLite do Whatsmeow
	if inst.Cliente.Store != nil && inst.Cliente.Store.Contacts != nil {
		if c, errCont := inst.Cliente.Store.Contacts.GetContact(ctx, jid); errCont == nil {
			if c.PushName != "" && pushNameFinal == "" {
				pushNameFinal = c.PushName
			}
			if c.BusinessName != "" {
				businessNameFinal = c.BusinessName
			}
			if c.FullName != "" {
				fullNameFinal = c.FullName
			}
		}
	}

	// C) Se não achou PushName no Store, busca o PushName registrado na tabela de mensagens
	if pushNameFinal == "" && g.db != nil {
		if pushMsg := g.db.ObterPushNameRecente(instanciaID, jid.String()); pushMsg != "" {
			pushNameFinal = pushMsg
		}
	}

	// D) Atribuição de precedência estrita:
	// 1º PushName real (o nome que o próprio contato escolheu para si)
	// 2º BusinessName (nome da empresa verificado)
	// 3º FullName (nome salvo na agenda do telefone)
	// 4º Número de telefone real
	if pushNameFinal != "" {
		info.Nome = pushNameFinal
		info.PushName = pushNameFinal
	} else if businessNameFinal != "" {
		info.Nome = businessNameFinal
	} else if fullNameFinal != "" {
		info.Nome = fullNameFinal
	} else if info.Numero != "" {
		info.Nome = info.Numero
	} else {
		info.Nome = jid.User
	}

	// 5. Foto de perfil do contato
	if pic, errPic := inst.Cliente.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true}); errPic == nil && pic != nil {
		info.FotoURL = pic.URL
	} else if picFull, errPicFull := inst.Cliente.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: false}); errPicFull == nil && picFull != nil {
		info.FotoURL = picFull.URL
	}

	return info
}

// SincronizarContatosEGrupos importa grupos, canais, contatos e histórico de conversas para a lista do Web Chat
func (g *GerenciadorInstancias) SincronizarContatosEGrupos(ctx context.Context, instanciaID string) (int, error) {
	inst, err := g.ObterInstanciaAtiva(instanciaID)
	if err != nil || inst == nil || inst.Cliente == nil {
		return 0, fmt.Errorf("instância não está conectada")
	}

	total := 0

	// 1. Sincronizar Canais / Newsletters (@newsletter)
	if newsletters, errNL := inst.Cliente.GetSubscribedNewsletters(ctx); errNL == nil && len(newsletters) > 0 {
		for _, nl := range newsletters {
			nome := nl.ThreadMeta.Name.Text
			if nome == "" {
				nome = "Canal " + nl.ID.User
			}
			fotoURL := ""
			if nl.ThreadMeta.Picture.URL != "" && (strings.HasPrefix(nl.ThreadMeta.Picture.URL, "http://") || strings.HasPrefix(nl.ThreadMeta.Picture.URL, "https://")) {
				fotoURL = nl.ThreadMeta.Picture.URL
			}
			if fotoURL == "" {
				if pic, errPic := inst.Cliente.GetProfilePictureInfo(ctx, nl.ID, &whatsmeow.GetProfilePictureParams{Preview: true}); errPic == nil && pic != nil {
					fotoURL = pic.URL
				}
			}
			_ = g.db.AtualizarDadosConversa(instanciaID, nl.ID.String(), nome, "", fotoURL, true)
			total++
		}
	}

	// 2. Sincronizar Grupos do WhatsApp (@g.us)
	if grupos, errGrupos := inst.Cliente.GetJoinedGroups(ctx); errGrupos == nil && len(grupos) > 0 {
		for _, grp := range grupos {
			nome := grp.Name
			if nome == "" {
				nome = "Grupo " + grp.JID.User
			}
			fotoURL := ""
			if pic, errPic := inst.Cliente.GetProfilePictureInfo(ctx, grp.JID, &whatsmeow.GetProfilePictureParams{Preview: true}); errPic == nil && pic != nil {
				fotoURL = pic.URL
			} else if picFull, errPicFull := inst.Cliente.GetProfilePictureInfo(ctx, grp.JID, &whatsmeow.GetProfilePictureParams{Preview: false}); errPicFull == nil && picFull != nil {
				fotoURL = picFull.URL
			}
			_ = g.db.AtualizarDadosConversa(instanciaID, grp.JID.String(), nome, "", fotoURL, true)
			total++
		}
	}

	// 3. Sincronizar Contatos salvos (PRIORIZANDO O PUSHNAME REAL)
	if inst.Cliente.Store != nil && inst.Cliente.Store.Contacts != nil {
		if contatos, errCont := inst.Cliente.Store.Contacts.GetAllContacts(ctx); errCont == nil {
			for jid, info := range contatos {
				if jid.Server == types.DefaultUserServer || jid.Server == types.HiddenUserServer {
					numero := jid.User
					if jid.Server == types.HiddenUserServer && inst.Cliente.Store.LIDs != nil {
						if pn, errLid := inst.Cliente.Store.LIDs.GetPNForLID(ctx, jid); errLid == nil && !pn.IsEmpty() {
							numero = pn.User
						}
					}

					// Prioriza PushName real sobre apelido local da agenda
					nome := info.PushName
					if nome == "" {
						nome = info.BusinessName
					}
					if nome == "" {
						nome = info.FullName
					}
					if nome == "" {
						nome = numero
					}

					fotoURL := ""
					if pic, errPic := inst.Cliente.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true}); errPic == nil && pic != nil {
						fotoURL = pic.URL
					}
					_ = g.db.AtualizarDadosConversa(instanciaID, jid.String(), nome, numero, fotoURL, false)
					total++
				}
			}
		}
	}

	// 4. Importar conversas e destinatários recentes dos logs de mensagens
	if logsRecentes, errLogs := g.db.ObterDestinatariosRecentesLogs(instanciaID, 150); errLogs == nil && len(logsRecentes) > 0 {
		for _, item := range logsRecentes {
			jidPadrao, errJID := ObterJIDPadrao(item.Destinatario)
			if errJID == nil {
				resolvido := g.ResolverInfoContato(ctx, instanciaID, jidPadrao, "")
				nome := resolvido.Nome
				if nome == "" || nome == jidPadrao.String() {
					nome = resolvido.Numero
				}
				if nome == "" {
					nome = item.Destinatario
				}
				_ = g.db.AtualizarDadosConversa(instanciaID, jidPadrao.String(), nome, resolvido.Numero, resolvido.FotoURL, resolvido.EhGrupo)
				total++
			}
		}
	}

	// 5. Atualizar conversas já existentes no chat_conversas para corrigir nomes e puxar fotos faltantes
	if convs, errConvs := g.db.ListarConversasChat(instanciaID, 300); errConvs == nil {
		for _, c := range convs {
			jidParsed, errJID := types.ParseJID(c.ChatJID)
			if errJID == nil {
				precisaAtualizar := strings.Contains(c.NomeContato, "@") || c.NomeContato == c.ChatJID || c.NumeroTelefone == "" || c.FotoURL == ""
				if !precisaAtualizar && len(c.NomeContato) > 14 && jidParsed.Server == types.NewsletterServer {
					precisaAtualizar = true
				}
				if precisaAtualizar {
					resolvido := g.ResolverInfoContato(ctx, instanciaID, jidParsed, "")
					if resolvido.Nome != "" && resolvido.Nome != c.ChatJID {
						_ = g.db.AtualizarDadosConversa(instanciaID, c.ChatJID, resolvido.Nome, resolvido.Numero, resolvido.FotoURL, resolvido.EhGrupo)
					}
				}
			}
		}
	}

	return total, nil
}
