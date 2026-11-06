package database

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ConversaChat representa um chat na lista do WhatsApp Web
type ConversaChat struct {
	ID              string    `json:"id"`
	InstanciaID     string    `json:"instancia_id"`
	ChatJID         string    `json:"chat_jid"`
	NomeContato     string    `json:"nome_contato"`
	NumeroTelefone  string    `json:"numero_telefone"`
	FotoURL         string    `json:"foto_url"`
	UltimaMensagem  string    `json:"ultima_mensagem"`
	UltimoTimestamp time.Time `json:"ultimo_timestamp"`
	TipoUltimaMsg   string    `json:"tipo_ultima_msg"`
	NaoLidas        int       `json:"nao_lidas"`
	EhGrupo         bool      `json:"eh_grupo"`
	Arquivado       bool      `json:"arquivado"`
	Fixado          bool      `json:"fixado"`
	AtualizadoEm    time.Time `json:"atualizado_em"`
}

// MensagemChat representa uma mensagem enviada ou recebida no chat
type MensagemChat struct {
	ID                string    `json:"id"`
	InstanciaID       string    `json:"instancia_id"`
	ChatJID           string    `json:"chat_jid"`
	RemetenteJID      string    `json:"remetente_jid"`
	RemetenteNome     string    `json:"remetente_nome"`
	RemetenteNumero   string    `json:"remetente_numero"`
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
			numero_telefone TEXT DEFAULT '',
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
			remetente_nome TEXT DEFAULT '',
			remetente_numero TEXT DEFAULT '',
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

	// Migrações seguras para tabelas pré-existentes (PostgreSQL e SQLite)
	_, _ = g.db.Exec(`ALTER TABLE chat_conversas ADD COLUMN numero_telefone TEXT DEFAULT ''`)
	_, _ = g.db.Exec(`ALTER TABLE chat_mensagens ADD COLUMN remetente_nome TEXT DEFAULT ''`)
	_, _ = g.db.Exec(`ALTER TABLE chat_mensagens ADD COLUMN remetente_numero TEXT DEFAULT ''`)

	// Limpeza de foto_url inválidas (ex: nomes de arquivo diretos do WhatsApp como .jpg salvos sem esquema HTTP)
	_, _ = g.db.Exec(`UPDATE chat_conversas SET foto_url = '' WHERE foto_url != '' AND foto_url NOT LIKE 'http%' AND foto_url NOT LIKE 'data:%'`)

	return nil
}

// SalvarMensagemChat persiste mensagem enviada ou recebida e atualiza a conversa correspondente
func (g *GerenciadorDB) SalvarMensagemChat(msg MensagemChat) error {
	return g.SalvarMensagemChatComContato(msg, "", "", "", strings.Contains(msg.ChatJID, "@g.us"))
}

// SalvarMensagemChatComContato persiste mensagem e enriquece os metadados do contato ou grupo
func (g *GerenciadorDB) SalvarMensagemChatComContato(msg MensagemChat, nomeContato, numeroTelefone, fotoURL string, ehGrupo bool) error {
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
		id, instancia_id, chat_jid, remetente_jid, remetente_nome, remetente_numero, de_mim, tipo,
		texto, midia_url, mimetype, nome_arquivo, status,
		deletada_remetente, resposta_de_id, criado_em
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	if _, err := g.exec(queryMsg,
		msg.ID, msg.InstanciaID, msg.ChatJID, msg.RemetenteJID, msg.RemetenteNome, msg.RemetenteNumero, deMimInt, msg.Tipo,
		msg.Texto, msg.MidiaURL, msg.MimeType, msg.NomeArquivo, msg.Status,
		delInt, msg.RespostaDeID, msg.CriadoEm,
	); err != nil {
		// Se já existir, atualiza status e mídia
		_, _ = g.exec(`UPDATE chat_mensagens SET
			status = ?,
			midia_url = CASE WHEN ? != '' THEN ? ELSE midia_url END,
			remetente_nome = CASE WHEN ? != '' THEN ? ELSE remetente_nome END,
			remetente_numero = CASE WHEN ? != '' THEN ? ELSE remetente_numero END,
			deletada_remetente = ?
			WHERE id = ?`,
			msg.Status, msg.MidiaURL, msg.MidiaURL, msg.RemetenteNome, msg.RemetenteNome, msg.RemetenteNumero, msg.RemetenteNumero, delInt, msg.ID,
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

	ehGrupoInt := 0
	if ehGrupo || strings.Contains(msg.ChatJID, "@g.us") {
		ehGrupoInt = 1
	}

	convID := fmt.Sprintf("%s:%s", msg.InstanciaID, msg.ChatJID)
	incrementoNaoLida := 0
	if !msg.DeMim {
		incrementoNaoLida = 1
	}

	if nomeContato == "" {
		nomeContato = msg.ChatJID
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
			nome_contato = CASE WHEN ? != '' AND ? != chat_jid THEN ? ELSE nome_contato END,
			numero_telefone = CASE WHEN ? != '' THEN ? ELSE numero_telefone END,
			foto_url = CASE WHEN ? != '' THEN ? ELSE foto_url END,
			eh_grupo = CASE WHEN ? = 1 THEN 1 ELSE eh_grupo END,
			atualizado_em = ?
			WHERE instancia_id = ? AND chat_jid = ?`,
			resumoTexto, msg.CriadoEm, msg.Tipo, incrementoNaoLida,
			nomeContato, nomeContato, nomeContato,
			numeroTelefone, numeroTelefone,
			fotoURL, fotoURL,
			ehGrupoInt,
			msg.CriadoEm,
			msg.InstanciaID, msg.ChatJID,
		)
	} else {
		_, _ = g.exec(`INSERT INTO chat_conversas (
			id, instancia_id, chat_jid, nome_contato, numero_telefone, foto_url,
			ultima_mensagem, ultimo_timestamp, tipo_ultima_msg,
			nao_lidas, eh_grupo, arquivado, fixado, atualizado_em
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, ?)`,
			convID, msg.InstanciaID, msg.ChatJID, nomeContato, numeroTelefone, fotoURL,
			resumoTexto, msg.CriadoEm, msg.Tipo,
			incrementoNaoLida, ehGrupoInt, msg.CriadoEm,
		)
	}

	return nil
}

// AtualizarDadosConversa atualiza o nome, número ou foto de uma conversa
func (g *GerenciadorDB) AtualizarDadosConversa(instanciaID, chatJID, nomeContato, numeroTelefone, fotoURL string, ehGrupo bool) error {
	ehGrupoInt := 0
	if ehGrupo || strings.Contains(chatJID, "@g.us") {
		ehGrupoInt = 1
	}
	convID := fmt.Sprintf("%s:%s", instanciaID, chatJID)
	agora := time.Now()

	var existe int
	row := g.queryRow(`SELECT COUNT(*) FROM chat_conversas WHERE instancia_id = ? AND chat_jid = ?`, instanciaID, chatJID)
	_ = row.Scan(&existe)

	if existe > 0 {
		_, err := g.exec(`UPDATE chat_conversas SET
			nome_contato = CASE WHEN ? != '' THEN ? ELSE nome_contato END,
			numero_telefone = CASE WHEN ? != '' THEN ? ELSE numero_telefone END,
			foto_url = CASE WHEN ? != '' THEN ? ELSE foto_url END,
			eh_grupo = CASE WHEN ? = 1 THEN 1 ELSE eh_grupo END,
			atualizado_em = ?
			WHERE instancia_id = ? AND chat_jid = ?`,
			nomeContato, nomeContato,
			numeroTelefone, numeroTelefone,
			fotoURL, fotoURL,
			ehGrupoInt,
			agora,
			instanciaID, chatJID,
		)
		return err
	}

	_, err := g.exec(`INSERT INTO chat_conversas (
		id, instancia_id, chat_jid, nome_contato, numero_telefone, foto_url,
		ultima_mensagem, ultimo_timestamp, tipo_ultima_msg,
		nao_lidas, eh_grupo, arquivado, fixado, atualizado_em
	) VALUES (?, ?, ?, ?, ?, ?, '', ?, 'texto', 0, ?, 0, 0, ?)`,
		convID, instanciaID, chatJID, nomeContato, numeroTelefone, fotoURL,
		agora, ehGrupoInt, agora,
	)
	return err
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

// parseDataFlexivel converte qualquer retorno de data do SQLite ou PostgreSQL para time.Time com segurança
func parseDataFlexivel(val interface{}) time.Time {
	if val == nil {
		return time.Now()
	}
	switch v := val.(type) {
	case time.Time:
		return v
	case *time.Time:
		if v != nil {
			return *v
		}
		return time.Now()
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return time.Now()
		}
		for _, layout := range []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02 15:04:05-07:00",
			"2006-01-02 15:04:05.999999999",
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			"2006-01-02",
		} {
			if t, err := time.Parse(layout, v); err == nil {
				return t
			}
		}
	case []byte:
		return parseDataFlexivel(string(v))
	}
	return time.Now()
}

// ListarConversasChat retorna a lista de contatos/conversas recentes
func (g *GerenciadorDB) ListarConversasChat(instanciaID string, limite int) ([]ConversaChat, error) {
	if limite <= 0 || limite > 500 {
		limite = 150
	}
	rows, err := g.query(`
		SELECT id, instancia_id, chat_jid, COALESCE(nome_contato, ''), COALESCE(numero_telefone, ''), COALESCE(foto_url, ''),
		       COALESCE(ultima_mensagem, ''), COALESCE(ultimo_timestamp, CURRENT_TIMESTAMP), COALESCE(tipo_ultima_msg, 'texto'),
		       COALESCE(nao_lidas, 0), COALESCE(eh_grupo, 0), COALESCE(arquivado, 0), COALESCE(fixado, 0), COALESCE(atualizado_em, CURRENT_TIMESTAMP)
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
		var rawUltimoTimestamp, rawAtualizadoEm interface{}
		if err := rows.Scan(
			&c.ID, &c.InstanciaID, &c.ChatJID, &c.NomeContato, &c.NumeroTelefone, &c.FotoURL,
			&c.UltimaMensagem, &rawUltimoTimestamp, &c.TipoUltimaMsg,
			&c.NaoLidas, &ehGrupoInt, &arqInt, &fixInt, &rawAtualizadoEm,
		); err == nil {
			c.UltimoTimestamp = parseDataFlexivel(rawUltimoTimestamp)
			c.AtualizadoEm = parseDataFlexivel(rawAtualizadoEm)
			c.EhGrupo = ehGrupoInt == 1
			c.Arquivado = arqInt == 1
			c.Fixado = fixInt == 1
			lista = append(lista, c)
		}
	}
	return lista, nil
}

// ObterOuCriarConversaChat busca ou cria o registro de uma conversa
func (g *GerenciadorDB) ObterOuCriarConversaChat(instanciaID, chatJID, nomeContato, numeroTelefone, fotoURL string, ehGrupo bool) (*ConversaChat, error) {
	row := g.queryRow(`
		SELECT id, instancia_id, chat_jid, COALESCE(nome_contato, ''), COALESCE(numero_telefone, ''), COALESCE(foto_url, ''),
		       COALESCE(ultima_mensagem, ''), COALESCE(ultimo_timestamp, CURRENT_TIMESTAMP), COALESCE(tipo_ultima_msg, 'texto'),
		       COALESCE(nao_lidas, 0), COALESCE(eh_grupo, 0), COALESCE(arquivado, 0), COALESCE(fixado, 0), COALESCE(atualizado_em, CURRENT_TIMESTAMP)
		FROM chat_conversas
		WHERE instancia_id = ? AND chat_jid = ?
	`, instanciaID, chatJID)

	var c ConversaChat
	var ehGrupoInt, arqInt, fixInt int
	var rawUltimoTimestamp, rawAtualizadoEm interface{}
	err := row.Scan(
		&c.ID, &c.InstanciaID, &c.ChatJID, &c.NomeContato, &c.NumeroTelefone, &c.FotoURL,
		&c.UltimaMensagem, &rawUltimoTimestamp, &c.TipoUltimaMsg,
		&c.NaoLidas, &ehGrupoInt, &arqInt, &fixInt, &rawAtualizadoEm,
	)
	if err == nil {
		c.UltimoTimestamp = parseDataFlexivel(rawUltimoTimestamp)
		c.AtualizadoEm = parseDataFlexivel(rawAtualizadoEm)
		c.EhGrupo = ehGrupoInt == 1
		c.Arquivado = arqInt == 1
		c.Fixado = fixInt == 1
		modificado := false
		if nomeContato != "" && c.NomeContato != nomeContato && nomeContato != chatJID {
			c.NomeContato = nomeContato
			modificado = true
		}
		if numeroTelefone != "" && c.NumeroTelefone != numeroTelefone {
			c.NumeroTelefone = numeroTelefone
			modificado = true
		}
		if fotoURL != "" && c.FotoURL != fotoURL {
			c.FotoURL = fotoURL
			modificado = true
		}
		if modificado {
			_, _ = g.exec(`UPDATE chat_conversas SET nome_contato = ?, numero_telefone = ?, foto_url = ? WHERE id = ?`, c.NomeContato, c.NumeroTelefone, c.FotoURL, c.ID)
		}
		return &c, nil
	}

	convID := fmt.Sprintf("%s:%s", instanciaID, chatJID)
	agora := time.Now()
	grpVal := 0
	if ehGrupo || strings.Contains(chatJID, "@g.us") {
		grpVal = 1
	}
	if nomeContato == "" {
		nomeContato = chatJID
	}

	_, errInsert := g.exec(`INSERT INTO chat_conversas (
		id, instancia_id, chat_jid, nome_contato, numero_telefone, foto_url,
		ultima_mensagem, ultimo_timestamp, tipo_ultima_msg,
		nao_lidas, eh_grupo, arquivado, fixado, atualizado_em
	) VALUES (?, ?, ?, ?, ?, ?, '', ?, 'texto', 0, ?, 0, 0, ?)`,
		convID, instanciaID, chatJID, nomeContato, numeroTelefone, fotoURL,
		agora, grpVal, agora,
	)
	if errInsert != nil {
		return nil, errInsert
	}

	return &ConversaChat{
		ID:              convID,
		InstanciaID:     instanciaID,
		ChatJID:         chatJID,
		NomeContato:     nomeContato,
		NumeroTelefone:  numeroTelefone,
		FotoURL:         fotoURL,
		UltimoTimestamp: agora,
		TipoUltimaMsg:   "texto",
		EhGrupo:         ehGrupo || grpVal == 1,
		AtualizadoEm:    agora,
	}, nil
}

// ListarMensagensChat retorna as mensagens de uma conversa
func (g *GerenciadorDB) ListarMensagensChat(instanciaID, chatJID string, limite int) ([]MensagemChat, error) {
	if limite <= 0 || limite > 500 {
		limite = 150
	}
	rows, err := g.query(`
		SELECT id, instancia_id, chat_jid, remetente_jid, COALESCE(remetente_nome, ''), COALESCE(remetente_numero, ''),
		       COALESCE(de_mim, 0), tipo, COALESCE(texto, ''), COALESCE(midia_url, ''), COALESCE(mimetype, ''),
		       COALESCE(nome_arquivo, ''), COALESCE(status, 'PENDENTE'), COALESCE(deletada_remetente, 0),
		       COALESCE(resposta_de_id, ''), COALESCE(criado_em, CURRENT_TIMESTAMP)
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
		var rawCriadoEm interface{}
		if err := rows.Scan(
			&m.ID, &m.InstanciaID, &m.ChatJID, &m.RemetenteJID, &m.RemetenteNome, &m.RemetenteNumero,
			&deMimInt, &m.Tipo, &m.Texto, &m.MidiaURL, &m.MimeType,
			&m.NomeArquivo, &m.Status, &delInt, &m.RespostaDeID, &rawCriadoEm,
		); err == nil {
			m.CriadoEm = parseDataFlexivel(rawCriadoEm)
			m.DeMim = deMimInt == 1
			m.DeletadaRemetente = delInt == 1
			lista = append(lista, m)
		}
	}
	return lista, nil
}

// ZerarNaoLidasConversa zera o contador de não lidas ao abrir a conversa
func (g *GerenciadorDB) ZerarNaoLidasConversa(instanciaID, chatJID string) error {
	_, err := g.exec(`UPDATE chat_conversas SET nao_lidas = 0 WHERE instancia_id = ? AND chat_jid = ?`, instanciaID, chatJID)
	return err
}

// ListarRespostasRapidas lista snippets cadastrados pelo cliente
func (g *GerenciadorDB) ListarRespostasRapidas(clienteID string) ([]RespostaRapidaChat, error) {
	rows, err := g.query(`
		SELECT id, cliente_id, atalho, texto, categoria, criado_em
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

// SalvarRespostaRapida insere ou atualiza um snippet
func (g *GerenciadorDB) SalvarRespostaRapida(r RespostaRapidaChat) error {
	if r.ID == "" {
		r.ID = "snip_" + uuid.New().String()[:12]
	}
	if r.CriadoEm.IsZero() {
		r.CriadoEm = time.Now()
	}

	_, err := g.exec(`
		INSERT INTO chat_respostas_rapidas (id, cliente_id, atalho, texto, categoria, criado_em)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			atalho = EXCLUDED.atalho,
			texto = EXCLUDED.texto,
			categoria = EXCLUDED.categoria
	`, r.ID, r.ClienteID, r.Atalho, r.Texto, r.Categoria, r.CriadoEm)
	if err != nil {
		// Fallback para SQLite simples
		_, err = g.exec(`
			INSERT OR REPLACE INTO chat_respostas_rapidas (id, cliente_id, atalho, texto, categoria, criado_em)
			VALUES (?, ?, ?, ?, ?, ?)
		`, r.ID, r.ClienteID, r.Atalho, r.Texto, r.Categoria, r.CriadoEm)
	}
	return err
}

// ExcluirRespostaRapida deleta snippet
func (g *GerenciadorDB) ExcluirRespostaRapida(clienteID, id string) error {
	_, err := g.exec(`DELETE FROM chat_respostas_rapidas WHERE cliente_id = ? AND id = ?`, clienteID, id)
	return err
}

// ObterPushNameRecente busca o último PushName registrado para um JID em mensagens recebidas
func (g *GerenciadorDB) ObterPushNameRecente(instanciaID, jid string) string {
	if g == nil || instanciaID == "" || jid == "" {
		return ""
	}
	var nome string
	row := g.queryRow(`
		SELECT remetente_nome 
		FROM chat_mensagens 
		WHERE instancia_id = ? AND (chat_jid = ? OR remetente_jid = ?) 
		  AND remetente_nome != '' AND remetente_nome != chat_jid AND remetente_nome NOT LIKE '%@%'
		ORDER BY criado_em DESC LIMIT 1
	`, instanciaID, jid, jid)
	if err := row.Scan(&nome); err == nil {
		return strings.TrimSpace(nome)
	}
	return ""
}

// ItemLogDestinatario representa um contato extraído do histórico de disparos
type ItemLogDestinatario struct {
	Destinatario string
	Tipo         string
	CriadoEm     time.Time
}

// ObterDestinatariosRecentesLogs busca destinatários únicos com interações recentes para popular conversas
func (g *GerenciadorDB) ObterDestinatariosRecentesLogs(instanciaID string, limite int) ([]ItemLogDestinatario, error) {
	if limite <= 0 || limite > 300 {
		limite = 100
	}
	rows, err := g.query(`
		SELECT destinatario, tipo, COALESCE(criado_em, CURRENT_TIMESTAMP)
		FROM logs_mensagens
		WHERE instancia_id = ? AND destinatario != ''
		ORDER BY criado_em DESC
		LIMIT ?
	`, instanciaID, limite*3)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	vistos := make(map[string]bool)
	var resultado []ItemLogDestinatario

	for rows.Next() {
		var dest, tipo string
		var rawCriado interface{}
		if err := rows.Scan(&dest, &tipo, &rawCriado); err == nil {
			dest = strings.TrimSpace(dest)
			if dest == "" || vistos[dest] {
				continue
			}
			vistos[dest] = true
			resultado = append(resultado, ItemLogDestinatario{
				Destinatario: dest,
				Tipo:         tipo,
				CriadoEm:     parseDataFlexivel(rawCriado),
			})
			if len(resultado) >= limite {
				break
			}
		}
	}
	return resultado, nil
}
