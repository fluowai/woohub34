# Arquitetura: Documentação Pública Tri-língue e Protocolo MCP para IA

Este documento descreve a disponibilização pública da documentação oficial da **WooHub API**, sua separação estrita de segurança em relação ao painel administrativo e a análise técnica completa para suporte ao **Model Context Protocol (MCP)**.

---

## 1. Documentação Pública Tri-língue (/docs e /documentacao)

### 1.1 Objetivo e Isolamento de Segurança
- **Apenas a documentação é pública**: Qualquer desenvolvedor, cliente ou parceiro pode acessar livremente a documentação pelo link direto (ex: `https://wooapi.wootech.com.br/docs` ou `http://localhost:8080/docs`) sem exigir login, token JWT ou chave de API.
- **Painel Administrativo 100% Protegido**: Todas as demais rotas da aplicação (`/`, `/instancias`, `/testador`, `/planos`, `/revendedores`, `/whitelabel`, `/logs`, etc.) continuam rigorosamente protegidas por autenticação RBAC, redirecionando usuários não autenticados para a tela de login.
- **Rotas de API Protegidas**: Endpoints operacionais continuam exigindo `x-api-key` (para instâncias WhatsApp) ou `Authorization: Bearer` (para gestão).

### 1.2 Recursos da Interface da Documentação
1. **Tri-língue Nativo em Tempo Real**:
   - 🇧🇷 **Português do Brasil (PT-BR)**: Padrão nacional com terminologia de DDD, 9º dígito e Pix.
   - 🇺🇸 **Inglês (EN)**: Padrão internacional para integrações globais.
   - 🇪🇸 **Espanhol (ES)**: Padrão para clientes e revendas na América Latina e Espanha.
   - Alternância instantânea via botões no topo, sem recarregar a página, com persistência no `localStorage` e detecção via query string (`?lang=pt`, `?lang=en`, `?lang=es`) ou idioma do navegador.
2. **Design e Usabilidade**:
   - Tema escuro moderno (*dark slate* com acentos esmeralda `#10b981`), tipografia *Plus Jakarta Sans* e código *JetBrains Mono*.
   - Sidebar com filtro de busca em tempo real por endpoints.
   - Blocos de código com botão de cópia rápida em 1 clique.
   - Botão no topo para alternar facilmente para o painel de login (`/`).
3. **Módulos Documentados**:
   - **Autenticação**: Headers `x-api-key` e `Authorization: Bearer`.
   - **Pacote 1 (Mensageria)**: Texto, Áudio PTT Microfone Verde, Imagens, Vídeos, Documentos PDF, Enquetes Interativas, Botões Native Flow (CTA URL, Pix Copia e Cola, Quick Reply), Reações Emoji, Edição de mensagens e Revogação (Delete para todos).
   - **Pacote 2 (Higienização & Chat)**: Validador de números BR com resolução do 9º dígito, Presença de Chat (digitando/gravando áudio), Confirmação de Leitura (checks azuis) e Etiquetas.
   - **Pacotes 3 e 4 (Grupos, Status & Blindagem)**: Menção Fantasma em grupos (@todos), publicação em Status 24h (Stories), Proxy Residencial SOCKS5/HTTP e Anti-Ban com rejeição de chamadas VoIP.
   - **Model Context Protocol (MCP)**: Guia completo para conexão com Claude Desktop, Cursor, Antigravity e agentes de IA.

---

## 2. Análise Técnica: Como a WooHub API pode ter um MCP (Model Context Protocol)

### 2.1 O que é o MCP e qual o valor para a WooHub API?
O **Model Context Protocol (MCP)** é o padrão aberto mantido pela Anthropic (e adotado pela comunidade de IA: Cursor, Antigravity, VS Code, Goose, Windsurf e frameworks como LangChain/LlamaIndex) que padroniza como Modelos de Linguagem (LLMs) interagem com ferramentas externas e fontes de dados.

Hoje, para um usuário automatizar o WhatsApp com IA, ele precisa configurar webhooks manuais em ferramentas como Typebot, n8n ou criar scripts personalizados. Com um **servidor MCP da WooHub API**, qualquer assistente de IA ganha "mãos" para operar o WhatsApp corporativo sob **comandos em linguagem natural**:
- *"Envie uma mensagem para o cliente (11) 98765-4321 avisando que o pedido #458 foi enviado."*
- *"Crie uma enquete no grupo 'Clientes VIP' perguntando qual dia preferem a live."*
- *"Verifique quais telefones desta lista de 100 leads possuem WhatsApp ativo."*
- *"Qual o status de conexão e bateria da instância do suporte?"*

---

### 2.2 Três Abordagens de Implementação do MCP

#### Abordagem 1: MCP Server Nativo Embutido em Go (RECOMENDADA)
- **Como funciona**: Implementar um handler HTTP/SSE nativo dentro do próprio binário Go da WooHub API (por exemplo, na rota `/api/v1/mcp/sse`), utilizando a biblioteca open-source madura `github.com/mark3labs/mcp-go`.
- **Vantagens**:
  - **Zero containers extras**: O mesmo container Docker compilado no Portainer já sobe com o MCP ativo.
  - **Performance extrema**: Sem pontes intermediárias de Node.js ou Python; latência de microssegundos.
  - **Reutilização direta do core**: As tools invocam diretamente as funções internas de `internal/core/` (como `EnviarTexto`, `EnviarAudio`, `NormalizarNumeroBrasil`), sem custo de rede local.
- **Transportes Suportados**:
  - **SSE (Server-Sent Events)**: Ideal para conexões remotas seguras via HTTPS (`https://wooapi.wootech.com.br/api/v1/mcp/sse`).
  - **Stdio**: Modo CLI local (`woohub-api mcp --key=...`) para uso local no Claude Desktop.

#### Abordagem 2: MCP Server Standalone (Node.js / TypeScript)
- **Como funciona**: Um pacote npm (`@woohub/mcp-server`) construído com o SDK oficial `@modelcontextprotocol/sdk` da Anthropic, que se comunica com a WooHub API via chamadas HTTP REST padrão.
- **Vantagens**:
  - Usuários do Claude Desktop ou Cursor podem rodar instantaneamente via `npx -y @woohub/mcp-server`.
  - Desacoplado do ciclo de release do binário principal.
- **Desvantagens**:
  - Exige manter um repositório secundário e publicar pacotes npm.

#### Abordagem 3: Conversor OpenAPI 3.0 para MCP
- **Como funciona**: Gerar o arquivo `openapi.json` da WooHub API e usar ferramentas de ponte automática (como `swagger-to-mcp` ou Cloudflare AI Gateway).
- **Vantagens**: Fácil de expor rapidamente todos os endpoints REST existentes.
- **Desvantagens**: Prompts e schemas menos amigáveis para as LLMs em comparação com ferramentas semânticas dedicadas.

---

### 2.3 Especificação das Tools (Ferramentas) MCP Propostas

| Nome da Tool | Parâmetros de Entrada (JSON Schema) | Ação Executada |
| :--- | :--- | :--- |
| `whatsapp_send_text` | `destinatario` (string), `mensagem` (string), `atraso_digitacao_ms` (int, opcional) | Envia mensagem de texto formatada com simulação de digitação humana. |
| `whatsapp_send_media` | `destinatario` (string), `tipo` ("audio"\|"imagem"\|"video"\|"documento"), `url` (string), `legenda` (string, opcional), `nome_arquivo` (string, opcional), `ptt` (bool, opcional) | Envia mídias, áudios PTT gravados na hora ou boletos/faturas em PDF. |
| `whatsapp_send_poll` | `destinatario` (string), `pergunta` (string), `opcoes` (array de strings), `max_selecao` (int) | Cria uma enquete oficial interativa para tomada de decisões ou pesquisas. |
| `whatsapp_verify_number` | `numeros` (array de strings) | Higieniza listas telefônicas, valida se possuem WhatsApp e resolve o 9º dígito BR. |
| `whatsapp_get_status` | `instancia_id` (string, opcional se configurada no contexto) | Retorna estado de conexão (conectado/desconectado), bateria e push name. |
| `whatsapp_group_ghost_mention` | `grupo_jid` (string), `mensagem` (string) | Notifica 100% dos integrantes do grupo (@todos) de forma limpa. |
| `whatsapp_set_presence` | `destinatario` (string), `estado` ("digitando"\|"gravando_audio"\|"pausado") | Define presença visual humana no chat antes de responder. |

---

### 2.4 Segurança, Governança e Multi-Tenancy no MCP
1. **Autenticação por Instância**:
   - O MCP Server deve receber como variável de ambiente ou header HTTP a `WOOHUB_API_KEY` da instância do usuário.
   - O agente só tem acesso estrito às operações permitidas por aquela chave.
2. **Proteção Anti-Spam e Guardrails**:
   - Limite de taxa (rate limiting) por tool para evitar que loops de agentes de IA causem disparos em massa acidentais.
   - Validação prévia de números via `NormalizarNumeroBrasil` antes de qualquer envio.
3. **Auditoria e Logs**:
   - Todas as execuções de tools via MCP são registradas na tabela `logs_mensagens` com a tag de origem `origem: "mcp_agent"`.

---

## 3. Roteiro de Implementação Sugerido

1. **Fase 1 (Concluída)**:
   - Portal de documentação pública acessível em `/docs` e `/documentacao`.
   - Suporte a 3 línguas (PT-BR, EN, ES) com persistência de escolha.
   - Isolamento total garantindo que apenas a documentação seja aberta sem login.
   - Seção e especificação de MCP documentadas para desenvolvedores.
2. **Fase 2 (Próximo Passo Recomendado)**:
   - Adicionar o pacote `github.com/mark3labs/mcp-go` no módulo Go.
   - Criar `internal/mcp/servidor.go` mapeando as 6 ferramentas prioritárias (`send_text`, `send_media`, `send_poll`, `verify_number`, `get_status`, `group_ghost_mention`).
   - Expor o endpoint SSE `/api/v1/mcp/sse` protegido por `x-api-key`.
   - Adicionar o modo CLI `woohub-api mcp` para execução via Stdio local.
