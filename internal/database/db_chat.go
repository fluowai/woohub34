package database

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ConversaChat representa um chat na lista do WhatsApp Web
type ConversaChat struct {
	ID             string    `json:"id"`
	InstanciaID    string    `json:"instancia_id"`
	ChatJID        string    `json:"chat_jid"`
	NomeContato    string    `json:"nome_contato"`
	FotoURL        string    `json:"foto_url"`
	UltimaMensagem string    `json:"ultima_mensagem"`
	UltimoTimestamp time.Time `json:"ultimo_timestamp"`
	TipoUltimaMsg  string    `json:"tipo_ultima_msg"`
	NaoLidas       int       `json:"nao_lidas"`
	EhGrupo        bool      `json:"eh_grupo"`
	Arquivado      bool      `json:"arquivado"`
	Fixado         bool      `json:"fixado"`
	AtualizadoEm   time.Time `json:"atualizado_em"`
}

// MensagemChat representa uma mensagem enviada ou recebida no chat
type MensagemChat struct {
	ID                string    `json:"id"`
	InstanciaID       string    `json:"instancia_id"`
	ChatJID           string    `json:"chat_jid"`
	RemetenteJID      string    `json:"remetente_jid"`
	DeMim             bool      `json:"de_mim"`
	Tipo              string    `json:"tipo"`
	Texto             string    `json:"texto"`
	MidiaURL          string    `json:"midia_url"`
	MimeType          string    `json:"mimetype"`
	NomeArquivo       string    `json:"nome_arquivo"`
	Status            string    `json:"status"` // PENDENTE, ENVIADA, ENTREGUE, LIDA, ERRO
	DeletadaRemetente bool      `json:"deletada_remetente"`
	RespostaDeID      string    `json:"resposta_de_id"`
	CriadoEm          time.Time `json:"criado_em"`
}

// RespostaRapidaChat representa um snippet (/pix, /horario) liberado por plano
type RespostaRapidaChat struct {
	ID        string    `json:"id"`
	ClienteID string    `json:"cliente_id"`
	Atalho    string    `json:"atalho"`
	Texto     string    `json:"texto"`
	Categoria string    `json:"categoria"`
	CriadoEm  time.Time `json:"criado_em"`
}

// CriarTabelasChat inicializa o esquema de tabelas do Web Chat
func (g *GerenciadorDB) CriarTabelasChat() error {
	tipoDataHora := "DATETIME"
	if g.driver == "postgres" {
		tipoDataHora = "TIMESTAMPTZ"
	}

	queries := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS chat_conversas (
			id TEXT PRIMARY KEY,
			instancia_id TEXT NOT NULL,
			chat_jid TEXT NOT NULL,
			nome_contato TEXT DEFAULT '',
			foto_url TEXT DEFAULT '',
			ultima_mensagem TEXT DEFAULT '',
			ultimo_timestamp %s DEFAULT CURRENT_TIMESTAMP,
			tipo_ultima_msg TEXT DEFAULT 'texto',
			nao_lidas INTEGER DEFAULT 0,
			eh_grupo INTEGER DEFAULT 0,
			arquivado INTEGER DEFAULT 0,
			fixado INTEGER DEFAULT 0,
			atualizado_em %s DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (instancia_id) REFERENCES instancias(id) ON DELETE CASCADE,
			UNIQUE (instancia_id, chat_jid)
		);`, tipoDataHora, tipoDataHora),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS chat_mensagens (
			id TEXT PRIMARY KEY,
			instancia_id TEXT NOT NULL,
			chat_jid TEXT NOT NULL,
			remetente_jid TEXT NOT NULL,
			de_mim INTEGER DEFAULT 0,
			tipo TEXT NOT NULL,
			texto TEXT DEFAULT '',
			midia_url TEXT DEFAULT '',
			mimetype TEXT DEFAULT '',
			nome_arquivo TEXT DEFAULT '',
			status TEXT DEFAULT 'PENDENTE',
			deletada_remetente INTEGER DEFAULT 0,
			resposta_de_id TEXT DEFAULT '',
			criado_em %s DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (instancia_id) REFERENCES instancias(id) ON DELETE CASCADE
		);`, tipoDataHora),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS chat_respostas_rapidas (
			id TEXT PRIMARY KEY,
			cliente_id TEXT NOT NULL,
			atalho TEXT NOT NULL,
			texto TEXT NOT NULL,
			categoria TEXT DEFAULT 'Geral',
			criado_em %s DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (cliente_id) REFERENCES clientes(id) ON DELETE CASCADE
		);`, tipoDataHora),

		`CREATE INDEX IF NOT EXISTS idx_chat_conversas_instancia ON chat_conversas(instancia_id, atualizado_em);`,
		`CREATE INDEX IF NOT EXISTS idx_chat_mensagens_busca ON chat_mensagens(instancia_id, chat_jid, criado_em);`,
		`CREATE INDEX IF NOT EXISTS idx_chat_respostas_cliente ON chat_respostas_rapidas(cliente_id);`,
	}

	for _, q := range queries {
		if _, err := g.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// SalvarMensagemChat persiste mensagem enviada ou recebida e atualiza a conversa correspondente
func (g *GerenciadorDB) SalvarMensagemChat(msg MensagemChat) error {
	if msg.ID == "" {
		msg.ID = "msg_" + uuid.New().String()[:16]
	}
	if msg.CriadoEm.IsZero() {
		msg.CriadoEm = time.Now()
	}

	deMimInt := 0
	if msg.DeMim {
		deMimInt = 1
	}
	delInt := 0
	if msg.DeletadaRemetente {
		delInt = 1
	}

	// Tenta inserção com substituição segura
	queryMsg := `INSERT INTO chat_mensagens (
		id, instancia_id, chat_jid, remetente_jid, de_mim, tipo,
		texto, midia_url, mimetype, nome_arquivo, status,
		deletada_remetente, resposta_de_id, criado_em
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	if _, err := g.exec(queryMsg,
		msg.ID, msg.InstanciaID, msg.ChatJID, msg.RemetenteJID, deMimInt, msg.Tipo,
		msg.Texto, msg.MidiaURL, msg.MimeType, msg.NomeArquivo, msg.Status,
		delInt, msg.RespostaDeID, msg.CriadoEm,
	); err != nil {
		// Se já existir, atualiza status e mídia
		_, _ = g.exec(`UPDATE chat_mensagens SET
			status = ?,
			midia_url = CASE WHEN ? != '' THEN ? ELSE midia_url END,
			deletada_remetente = ?
			WHERE id = ?`,
			msg.Status, msg.MidiaURL, msg.MidiaURL, delInt, msg.ID,
		)
	}

	// Resumo para lista de conversas
	resumoTexto := msg.Texto
	if resumoTexto == "" {
		switch msg.Tipo {
		case "imagem":
			resumoTexto = "📷 Foto"
		case "audio", "audio_ptt":
			resumoTexto = "🎵 Áudio"
		case "video":
			resumoTexto = "🎥 Vídeo"
		case "documento":
			resumoTexto = "📄 Documento"
		case "sticker":
			resumoTexto = "🏷️ Figurinha"
		case "enquete":
			resumoTexto = "📊 Enquete"
		case "localizacao":
			resumoTexto = "📍 Localização"
		case "contato":
			resumoTexto = "👤 Contato"
		default:
			resumoTexto = "Mensagem"
		}
	}

	ehGrupo := strings.Contains(msg.ChatJID, "@g.us")
	ehGrupoInt := 0
	if ehGrupo {
		ehGrupoInt = 1
	}

	convID := fmt.Sprintf("%s:%s", msg.InstanciaID, msg.ChatJID)
	incrementoNaoLida := 0
	if !msg.DeMim {
		incrementoNaoLida = 1
	}

	// Verifica se a conversa já existe
	var existe int
	row := g.queryRow(`SELECT COUNT(*) FROM chat_conversas WHERE instancia_id = ? AND chat_jid = ?`, msg.InstanciaID, msg.ChatJID)
	_ = row.Scan(&existe)

	if existe > 0 {
		_, _ = g.exec(`UPDATE chat_conversas SET
			ultima_mensagem = ?,
			ultimo_timestamp = ?,
			tipo_ultima_msg = ?,
			nao_lidas = nao_lidas + ?,
			atualizado_em = ?
			WHERE instancia_id = ? AND chat_jid = ?`,
			resumoTexto, msg.CriadoEm, msg.Tipo, incrementoNaoLida, msg.CriadoEm,
			msg.InstanciaID, msg.ChatJID,
		)
	} else {
		_, _ = g.exec(`INSERT INTO chat_conversas (
			id, instancia_id, chat_jid, nome_contato, foto_url,
			ultima_mensagem, ultimo_timestamp, tipo_ultima_msg,
			nao_lidas, eh_grupo, arquivado, fixado, atualizado_em
		) VALUES (?, ?, ?, ?, '', ?, ?, ?, ?, ?, 0, 0, ?)`,
			convID, msg.InstanciaID, msg.ChatJID, msg.ChatJID,
			resumoTexto, msg.CriadoEm, msg.Tipo,
			incrementoNaoLida, ehGrupoInt, msg.CriadoEm,
		)
	}

	return nil
}

// AtualizarStatusMensagemChat atualiza recibos (ENTREGUE, LIDA)
func (g *GerenciadorDB) AtualizarStatusMensagemChat(instanciaID, msgID, novoStatus string) error {
	_, err := g.exec(`UPDATE chat_mensagens SET status = ? WHERE instancia_id = ? AND id = ?`, novoStatus, instanciaID, msgID)
	return err
}

// MarcarMensagemDeletada ativa o GHOST MODE quando o remetente revoga a mensagem
func (g *GerenciadorDB) MarcarMensagemDeletada(instanciaID, msgID string) error {
	_, err := g.exec(`UPDATE chat_mensagens SET deletada_remetente = 1 WHERE instancia_id = ? AND id = ?`, instanciaID, msgID)
	return err
}

// ListarConversasChat retorna a lista de contatos/conversas recentes
func (g *GerenciadorDB) ListarConversasChat(instanciaID string, limite int) ([]ConversaChat, error) {
	if limite <= 0 || limite > 300 {
		limite = 100
	}
	rows, err := g.query(`
		SELECT id, instancia_id, chat_jid, COALESCE(nome_contato, ''), COALESCE(foto_url, ''),
		       COALESCE(ultima_mensagem, ''), ultimo_timestamp, COALESCE(tipo_ultima_msg, 'texto'),
		       nao_lidas, eh_grupo, arquivado, fixado, atualizado_em
		FROM chat_conversas
		WHERE instancia_id = ?
		ORDER BY fixado DESC, atualizado_em DESC
		LIMIT ?
	`, instanciaID, limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lista []ConversaChat
	for rows.Next() {
		var c ConversaChat
		var ehGrupoInt, arqInt, fixInt int
		if err := rows.Scan(
			&c.ID, &c.InstanciaID, &c.ChatJID, &c.NomeContato, &c.FotoURL,
			&c.UltimaMensagem, &c.UltimoTimestamp, &c.TipoUltimaMsg,
			&c.NaoLidas, &ehGrupoInt, &arqInt, &fixInt, &c.AtualizadoEm,
		); err == nil {
			c.EhGrupo = ehGrupoInt == 1
			c.Arquivado = arqInt == 1
			c.Fixado = fixInt == 1
			lista = append(lista, c)
		}
	}
	return lista, nil
}

// ObterOuCriarConversaChat busca ou cria o registro de uma conversa
func (g *GerenciadorDB) ObterOuCriarConversaChat(instanciaID, chatJID, nomeContato string, ehGrupo bool) (*ConversaChat, error) {
	row := g.queryRow(`
		SELECT id, instancia_id, chat_jid, COALESCE(nome_contato, ''), COALESCE(foto_url, ''),
		       COALESCE(ultima_mensagem, ''), ultimo_timestamp, COALESCE(tipo_ultima_msg, 'texto'),
		       nao_lidas, eh_grupo, arquivado, fixado, atualizado_em
		FROM chat_conversas
		WHERE instancia_id = ? AND chat_jid = ?
	`, instanciaID, chatJID)

	var c ConversaChat
	var ehGrupoInt, arqInt, fixInt int
	err := row.Scan(
		&c.ID, &c.InstanciaID, &c.ChatJID, &c.NomeContato, &c.FotoURL,
		&c.UltimaMensagem, &c.UltimoTimestamp, &c.TipoUltimaMsg,
		&c.NaoLidas, &ehGrupoInt, &arqInt, &fixInt, &c.AtualizadoEm,
	)
	if err == nil {
		c.EhGrupo = ehGrupoInt == 1
		c.Arquivado = arqInt == 1
		c.Fixado = fixInt == 1
		if nomeContato != "" && c.NomeContato != nomeContato {
			c.NomeContato = nomeContato
			_, _ = g.exec(`UPDATE chat_conversas SET nome_contato = ? WHERE id = ?`, nomeContato, c.ID)
		}
		return &c, nil
	}

	convID := fmt.Sprintf("%s:%s", instanciaID, chatJID)
	agora := time.Now()
	grpVal := 0
	if ehGrupo {
		grpVal = 1
	}
	if nomeContato == "" {
		nomeContato = chatJID
	}

	_, errInsert := g.exec(`INSERT INTO chat_conversas (
		id, instancia_id, chat_jid, nome_contato, foto_url,
		ultima_mensagem, ultimo_timestamp, tipo_ultima_msg,
		nao_lidas, eh_grupo, arquivado, fixado, atualizado_em
	) VALUES (?, ?, ?, ?, '', '', ?, 'texto', 0, ?, 0, 0, ?)`,
		convID, instanciaID, chatJID, nomeContato, agora, grpVal, agora,
	)
	if errInsert != nil {
		return nil, errInsert
	}

	return &ConversaChat{
		ID:              convID,
		InstanciaID:     instanciaID,
		ChatJID:         chatJID,
		NomeContato:     nomeContato,
		UltimoTimestamp: agora,
		TipoUltimaMsg:   "texto",
		EhGrupo:         ehGrupo,
		AtualizadoEm:    agora,
	}, nil
}

// ListarMensagensChat retorna as mensagens de uma conversa
func (g *GerenciadorDB) ListarMensagensChat(instanciaID, chatJID string, limite int) ([]MensagemChat, error) {
	if limite <= 0 || limite > 500 {
		limite = 100
	}
	rows, err := g.query(`
		SELECT id, instancia_id, chat_jid, remetente_jid, de_mim, tipo,
		       COALESCE(texto, ''), COALESCE(midia_url, ''), COALESCE(mimetype, ''),
		       COALESCE(nome_arquivo, ''), status, deletada_remetente,
		       COALESCE(resposta_de_id, ''), criado_em
		FROM chat_mensagens
		WHERE instancia_id = ? AND chat_jid = ?
		ORDER BY criado_em ASC
		LIMIT ?
	`, instanciaID, chatJID, limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lista []MensagemChat
	for rows.Next() {
		var m MensagemChat
		var deMimInt, delInt int
		if err := rows.Scan(
			&m.ID, &m.InstanciaID, &m.ChatJID, &m.RemetenteJID, &deMimInt, &m.Tipo,
			&m.Texto, &m.MidiaURL, &m.MimeType, &m.NomeArquivo, &m.Status,
			&delInt, &m.RespostaDeID, &m.CriadoEm,
		); err == nil {
			m.DeMim = deMimInt == 1
			m.DeletadaRemetente = delInt == 1
			lista = append(lista, m)
		}
	}
	return lista, nil
}

// ZerarNaoLidasConversa zera o contador de mensagens não lidas
func (g *GerenciadorDB) ZerarNaoLidasConversa(instanciaID, chatJID string) error {
	_, err := g.exec(`UPDATE chat_conversas SET nao_lidas = 0 WHERE instancia_id = ? AND chat_jid = ?`, instanciaID, chatJID)
	return err
}

// ListarRespostasRapidas lista snippets salvos da conta
func (g *GerenciadorDB) ListarRespostasRapidas(clienteID string) ([]RespostaRapidaChat, error) {
	rows, err := g.query(`
		SELECT id, cliente_id, atalho, texto, COALESCE(categoria, 'Geral'), criado_em
		FROM chat_respostas_rapidas
		WHERE cliente_id = ?
		ORDER BY atalho ASC
	`, clienteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lista []RespostaRapidaChat
	for rows.Next() {
		var r RespostaRapidaChat
		if err := rows.Scan(&r.ID, &r.ClienteID, &r.Atalho, &r.Texto, &r.Categoria, &r.CriadoEm); err == nil {
			lista = append(lista, r)
		}
	}
	return lista, nil
}

// SalvarRespostaRapida cadastra ou atualiza um snippet
func (g *GerenciadorDB) SalvarRespostaRapida(resp RespostaRapidaChat) error {
	if resp.ID == "" {
		resp.ID = "rr_" + uuid.New().String()[:12]
	}
	if resp.CriadoEm.IsZero() {
		resp.CriadoEm = time.Now()
	}
	query := `INSERT INTO chat_respostas_rapidas (id, cliente_id, atalho, texto, categoria, criado_em)
	          VALUES (?, ?, ?, ?, ?, ?)`
	_, err := g.exec(query, resp.ID, resp.ClienteID, resp.Atalho, resp.Texto, resp.Categoria, resp.CriadoEm)
	return err
}

// ExcluirRespostaRapida remove um snippet
func (g *GerenciadorDB) ExcluirRespostaRapida(clienteID, id string) error {
	_, err := g.exec(`DELETE FROM chat_respostas_rapidas WHERE cliente_id = ? AND id = ?`, clienteID, id)
	return err
}
