package core

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// InfoContatoResolvido armazena os metadados humanos e fotos de contatos e grupos do WhatsApp
type InfoContatoResolvido struct {
	Nome     string
	Numero   string
	FotoURL  string
	EhGrupo  bool
	PushName string
}

// ResolverInfoContato resolve LIDs, Grupos e Contatos para nomes reais, números de telefone reais e fotos
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

	// 1. Grupos (@g.us)
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
		if pic, errPic := inst.Cliente.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true}); errPic == nil && pic != nil {
			info.FotoURL = pic.URL
		}
		return info
	}

	// 2. Contatos com LID (@lid)
	if jid.Server == types.HiddenUserServer {
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
							if cInfo.FullName != "" {
								info.Nome = cInfo.FullName
							} else if cInfo.PushName != "" {
								info.Nome = cInfo.PushName
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

	// 3. Tenta obter o nome nos contatos salvos no SQLite do Whatsmeow
	if inst.Cliente.Store != nil && inst.Cliente.Store.Contacts != nil {
		if c, errCont := inst.Cliente.Store.Contacts.GetContact(ctx, jid); errCont == nil {
			if c.FullName != "" {
				info.Nome = c.FullName
			} else if c.PushName != "" && info.Nome == "" {
				info.Nome = c.PushName
			} else if c.BusinessName != "" && info.Nome == "" {
				info.Nome = c.BusinessName
			}
		}
	}

	if info.Nome == "" {
		if info.PushName != "" {
			info.Nome = info.PushName
		} else if info.Numero != "" {
			info.Nome = info.Numero
		} else {
			info.Nome = jid.User
		}
	}

	// 4. Foto de perfil
	if pic, errPic := inst.Cliente.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true}); errPic == nil && pic != nil {
		info.FotoURL = pic.URL
	}

	return info
}

// SincronizarContatosEGrupos importa grupos, contatos e histórico de conversas para a lista do Web Chat
func (g *GerenciadorInstancias) SincronizarContatosEGrupos(ctx context.Context, instanciaID string) (int, error) {
	inst, err := g.ObterInstanciaAtiva(instanciaID)
	if err != nil || inst == nil || inst.Cliente == nil {
		return 0, fmt.Errorf("instância não está conectada")
	}

	total := 0

	// 1. Sincronizar Grupos do WhatsApp
	if grupos, errGrupos := inst.Cliente.GetJoinedGroups(ctx); errGrupos == nil && len(grupos) > 0 {
		for _, grp := range grupos {
			nome := grp.Name
			if nome == "" {
				nome = "Grupo " + grp.JID.User
			}
			fotoURL := ""
			if pic, errPic := inst.Cliente.GetProfilePictureInfo(ctx, grp.JID, &whatsmeow.GetProfilePictureParams{Preview: true}); errPic == nil && pic != nil {
				fotoURL = pic.URL
			}
			_ = g.db.AtualizarDadosConversa(instanciaID, grp.JID.String(), nome, "", fotoURL, true)
			total++
		}
	}

	// 2. Sincronizar Contatos salvos na agenda do WhatsApp
	if inst.Cliente.Store != nil && inst.Cliente.Store.Contacts != nil {
		if contatos, errCont := inst.Cliente.Store.Contacts.GetAllContacts(ctx); errCont == nil {
			for jid, info := range contatos {
				if jid.Server == types.DefaultUserServer || jid.Server == types.HiddenUserServer {
					nome := info.FullName
					if nome == "" {
						nome = info.PushName
					}
					if nome == "" {
						nome = info.BusinessName
					}
					numero := jid.User
					if jid.Server == types.HiddenUserServer && inst.Cliente.Store.LIDs != nil {
						if pn, errLid := inst.Cliente.Store.LIDs.GetPNForLID(ctx, jid); errLid == nil && !pn.IsEmpty() {
							numero = pn.User
						}
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

	// 3. Importar conversas e destinatários recentes dos logs de mensagens (garante histórico das instâncias em produção)
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

	// 4. Atualizar conversas já existentes no chat_conversas que estejam com JID bruto
	if convs, errConvs := g.db.ListarConversasChat(instanciaID, 300); errConvs == nil {
		for _, c := range convs {
			if strings.Contains(c.NomeContato, "@lid") || strings.Contains(c.NomeContato, "@newsletter") || c.NomeContato == c.ChatJID || c.NumeroTelefone == "" {
				jidParsed, errJID := types.ParseJID(c.ChatJID)
				if errJID == nil {
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

