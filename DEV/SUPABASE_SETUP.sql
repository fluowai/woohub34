-- ============================================================================
-- WOOHUB API - SCHEMA POSTGRESQL PARA SUPABASE (100% COMPATÍVEL)
-- Este script é executado automaticamente pelo backend Go no boot (migrar),
-- mas também pode ser executado manualmente no SQL Editor do Supabase.
-- ============================================================================

-- 1. Tabela de Revendedores (Tenants White-Label)
CREATE TABLE IF NOT EXISTS revendedores (
    id TEXT PRIMARY KEY,
    nome_empresa TEXT NOT NULL,
    slug TEXT UNIQUE NOT NULL,
    dominio_personalizado TEXT UNIQUE,
    logo_url TEXT DEFAULT '',
    favicon_url TEXT DEFAULT '',
    cor_primaria TEXT DEFAULT '#00C853',
    cor_secundaria TEXT DEFAULT '#1E293B',
    limite_instancias INTEGER DEFAULT 15,
    ativo INTEGER DEFAULT 1,
    criado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- 2. Tabela de Clientes / Usuários com Controle de Papel (RBAC: superadmin, revendedor, cliente)
CREATE TABLE IF NOT EXISTS clientes (
    id TEXT PRIMARY KEY,
    revendedor_id TEXT NOT NULL REFERENCES revendedores(id) ON DELETE CASCADE,
    nome TEXT NOT NULL,
    email TEXT UNIQUE NOT NULL,
    senha_hash TEXT NOT NULL,
    documento TEXT DEFAULT '',
    papel TEXT NOT NULL DEFAULT 'cliente', -- 'superadmin', 'revendedor', 'cliente'
    limite_instancias INTEGER DEFAULT 5,
    ativo INTEGER DEFAULT 1,
    criado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_clientes_revendedor_id ON clientes(revendedor_id);
CREATE INDEX IF NOT EXISTS idx_clientes_email_lower ON clientes(LOWER(email));

-- 3. Tabela de Instâncias de WhatsApp
CREATE TABLE IF NOT EXISTS instancias (
    id TEXT PRIMARY KEY,
    cliente_id TEXT NOT NULL REFERENCES clientes(id) ON DELETE CASCADE,
    nome TEXT NOT NULL,
    chave_api TEXT UNIQUE NOT NULL,
    jid TEXT DEFAULT '',
    numero_telefone TEXT DEFAULT '',
    status TEXT DEFAULT 'DESCONECTADO',
    webhook_url TEXT DEFAULT '',
    webhook_eventos TEXT DEFAULT 'mensagem_recebida,status_envio,conexao',
    webhook_segredo TEXT DEFAULT '',
    ignorar_grupos INTEGER DEFAULT 0,
    rejeitar_chamadas INTEGER DEFAULT 0,
    mensagem_chamada_rejeitada TEXT DEFAULT '',
    proxy_url TEXT DEFAULT '',
    leitura_automatica INTEGER DEFAULT 0,
    sempre_online INTEGER DEFAULT 0,
    criado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    atualizado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_instancias_cliente_id ON instancias(cliente_id);
CREATE INDEX IF NOT EXISTS idx_instancias_chave_api ON instancias(chave_api);

-- 4. Tabela de Logs e Auditoria de Mensagens
CREATE TABLE IF NOT EXISTS logs_mensagens (
    id TEXT PRIMARY KEY,
    instancia_id TEXT NOT NULL REFERENCES instancias(id) ON DELETE CASCADE,
    mensagem_id TEXT DEFAULT '',
    tipo TEXT NOT NULL,
    destinatario TEXT NOT NULL,
    direcao TEXT NOT NULL,
    status TEXT DEFAULT 'ENVIANDO',
    detalhes_erro TEXT DEFAULT '',
    criado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_logs_instancia_criado ON logs_mensagens(instancia_id, criado_em DESC);
CREATE INDEX IF NOT EXISTS idx_logs_criado_em ON logs_mensagens(criado_em DESC);

-- 5. Tabela de Sessões de Autenticação
CREATE TABLE IF NOT EXISTS sessoes_auth (
    token TEXT PRIMARY KEY,
    cliente_id TEXT NOT NULL REFERENCES clientes(id) ON DELETE CASCADE,
    expira_em TIMESTAMPTZ NOT NULL,
    criado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sessoes_auth_expira_em ON sessoes_auth(expira_em);
CREATE INDEX IF NOT EXISTS idx_sessoes_auth_cliente_id ON sessoes_auth(cliente_id);


-- 6. Colunas de Infraestrutura Isolada por Revenda (BYO Supabase + BYO MinIO S3)
ALTER TABLE revendedores ADD COLUMN IF NOT EXISTS supabase_uri TEXT DEFAULT '';
ALTER TABLE revendedores ADD COLUMN IF NOT EXISTS minio_endpoint TEXT DEFAULT '';
ALTER TABLE revendedores ADD COLUMN IF NOT EXISTS minio_access_key TEXT DEFAULT '';
ALTER TABLE revendedores ADD COLUMN IF NOT EXISTS minio_secret_key TEXT DEFAULT '';
ALTER TABLE revendedores ADD COLUMN IF NOT EXISTS minio_bucket TEXT DEFAULT 'woohub-midias';
ALTER TABLE revendedores ADD COLUMN IF NOT EXISTS minio_use_ssl INTEGER DEFAULT 1;
ALTER TABLE revendedores ADD COLUMN IF NOT EXISTS minio_public_url TEXT DEFAULT '';

-- 7. Tabela de Licenciamento Criptográfico & Controle de Stacks Portainer (Kill-Switch)
CREATE TABLE IF NOT EXISTS licencas_instalacoes (
    id TEXT PRIMARY KEY,
    chave_licenca TEXT UNIQUE NOT NULL,
    revendedor_id TEXT DEFAULT '',
    cliente_nome TEXT NOT NULL,
    email_contato TEXT DEFAULT '',
    dominio_autorizado TEXT DEFAULT '',
    ip_autorizado TEXT DEFAULT '',
    fingerprint_container TEXT DEFAULT '',
    plano TEXT DEFAULT 'Enterprise Stack',
    limite_instancias INTEGER DEFAULT 50,
    status TEXT DEFAULT 'ATIVA',
    expira_em TIMESTAMPTZ NOT NULL,
    ultimo_heartbeat TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    ultimo_ip TEXT DEFAULT '',
    versao_instalada TEXT DEFAULT '1.1.0-Brasil-Prod',
    instancias_ativas INTEGER DEFAULT 0,
    token_assinado_ed25519 TEXT DEFAULT '',
    criado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_licencas_chave ON licencas_instalacoes(chave_licenca);

-- 8. Tabelas de Caixas Nativas de Integração (Chatwoot, Typebot, n8n, Kommo CRM, RD Station e Webhooks)
CREATE TABLE IF NOT EXISTS integracoes_instancia (
    id TEXT PRIMARY KEY,
    instancia_id TEXT NOT NULL REFERENCES instancias(id) ON DELETE CASCADE,
    tipo TEXT NOT NULL, -- 'chatwoot', 'typebot', 'n8n', 'kommo', 'rdstation', 'webhook'
    ativo INTEGER DEFAULT 0,
    configuracoes TEXT NOT NULL,
    criado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    atualizado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (instancia_id, tipo)
);

CREATE INDEX IF NOT EXISTS idx_integracoes_inst_tipo ON integracoes_instancia(instancia_id, tipo);

-- 9. Tabela de Controle de Sessões Typebot por Contato (com Transbordo Humano)
CREATE TABLE IF NOT EXISTS sessoes_typebot (
    id TEXT PRIMARY KEY,
    instancia_id TEXT NOT NULL REFERENCES instancias(id) ON DELETE CASCADE,
    remetente_jid TEXT NOT NULL,
    session_id_typebot TEXT NOT NULL,
    estado TEXT DEFAULT 'ATIVO',
    atualizado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (instancia_id, remetente_jid)
);

CREATE INDEX IF NOT EXISTS idx_sessoes_typebot_busca ON sessoes_typebot(instancia_id, remetente_jid);

-- 10. Tabela de Rastreamento de Origem de Mensagens e Atribuição de Campanhas (Meta Ads / Google Ads / UTMs)
CREATE TABLE IF NOT EXISTS atribuicoes_contato (
    id TEXT PRIMARY KEY,
    instancia_id TEXT NOT NULL REFERENCES instancias(id) ON DELETE CASCADE,
    jid TEXT NOT NULL,
    numero_formatado TEXT DEFAULT '',
    origem_canal TEXT DEFAULT '',
    campanha TEXT DEFAULT '',
    utm_source TEXT DEFAULT '',
    utm_medium TEXT DEFAULT '',
    utm_campaign TEXT DEFAULT '',
    utm_term TEXT DEFAULT '',
    utm_content TEXT DEFAULT '',
    ctwa_clid TEXT DEFAULT '',
    ad_source_id TEXT DEFAULT '',
    ad_source_url TEXT DEFAULT '',
    referencia TEXT DEFAULT '',
    primeira_mensagem TEXT DEFAULT '',
    criado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    atualizado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (instancia_id, jid)
);

CREATE INDEX IF NOT EXISTS idx_atribuicoes_contato_busca ON atribuicoes_contato(instancia_id, jid);


-- ============================================================================
-- 11. TABELAS DO WHATSAPP WEB CLONE (web.wooapi.com.br) & SUPER-PODERES
-- ============================================================================

CREATE TABLE IF NOT EXISTS chat_conversas (
    id TEXT PRIMARY KEY,
    instancia_id TEXT NOT NULL REFERENCES instancias(id) ON DELETE CASCADE,
    chat_jid TEXT NOT NULL,
    nome_contato TEXT DEFAULT '',
    foto_url TEXT DEFAULT '',
    ultima_mensagem TEXT DEFAULT '',
    ultimo_timestamp TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    tipo_ultima_msg TEXT DEFAULT 'texto',
    nao_lidas INTEGER DEFAULT 0,
    eh_grupo INTEGER DEFAULT 0,
    arquivado INTEGER DEFAULT 0,
    fixado INTEGER DEFAULT 0,
    atualizado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (instancia_id, chat_jid)
);

CREATE INDEX IF NOT EXISTS idx_chat_conversas_instancia ON chat_conversas(instancia_id, atualizado_em);

CREATE TABLE IF NOT EXISTS chat_mensagens (
    id TEXT PRIMARY KEY,
    instancia_id TEXT NOT NULL REFERENCES instancias(id) ON DELETE CASCADE,
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
    criado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_chat_mensagens_busca ON chat_mensagens(instancia_id, chat_jid, criado_em);

CREATE TABLE IF NOT EXISTS chat_respostas_rapidas (
    id TEXT PRIMARY KEY,
    cliente_id TEXT NOT NULL REFERENCES clientes(id) ON DELETE CASCADE,
    atalho TEXT NOT NULL,
    texto TEXT NOT NULL,
    categoria TEXT DEFAULT 'Geral',
    criado_em TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_chat_respostas_cliente ON chat_respostas_rapidas(cliente_id);

-- Migrações de enriquecimento para WhatsApp Web (Número Real, Capa de Grupo e Remetente)
ALTER TABLE chat_conversas ADD COLUMN IF NOT EXISTS numero_telefone TEXT DEFAULT '';
ALTER TABLE chat_mensagens ADD COLUMN IF NOT EXISTS remetente_nome TEXT DEFAULT '';
ALTER TABLE chat_mensagens ADD COLUMN IF NOT EXISTS remetente_numero TEXT DEFAULT '';
