# Arquitetura Comercial Estilo Uazapi: Licenciamento, Portainer Stack & Isolamento BYO (Supabase + MinIO)

> **Objetivo:** Transformar a **WooHub API** em um produto comercial blindado (estilo Uazapi / Z-API / Evolution Pro), protegendo 100% do código-fonte, exigindo licença ativa para subir e operar containers, e transferindo **100% do custo de banco de dados (Supabase) e armazenamento de mídia (MinIO/S3)** para cada revendedor.

---

## 1. Visão Geral: Como Funciona o Modelo Comercial

Você tem uma vantagem enorme por estar em **Go puro (`whatsmeow`)** em vez de Node.js/TypeScript:
1. **Em Node.js (Evolution/Baileys):** Quem baixa a imagem Docker consegue entrar no container (`docker exec -it ... sh`) e copiar todo o código-fonte JS.
2. **Em Go (WooHub API):** O código vira um **único binário compilado em código de máquina** (`CGO_ENABLED=0 go build -ldflags="-w -s"`), sem tabela de símbolos, sem código-fonte legível, e com os arquivos web (`index.html`, `app.js`, `style.css`) embutidos dentro do próprio binário via `//go:embed`. Se usarmos uma imagem `distroless` ou `scratch`, o container **não tem nem shell (`sh`/`bash`)** para alguém tentar inspecionar.

---

## 2. Comparativo das 3 Arquiteturas de Distribuição

| Critério | Opção A: Stack Portainer no VPS do Revendedor (Self-Hosted Licenciado) | Opção B: Servidor Central Seu + Banco/MinIO do Revendedor (Cloud Híbrida BYODB) | Opção C: Ambos (Planos Start vs. Enterprise) |
| :--- | :--- | :--- | :--- |
| **Onde roda a CPU/RAM da API?** | Na VPS do próprio Revendedor (Hetzner, Contabo, DigitalOcean dele) | No seu cluster central | Plano Pro = Seu Servidor / Plano White-Label Dedicado = Portainer do Cliente |
| **Onde ficam o Supabase e o MinIO?** | No Supabase e MinIO do Revendedor (via `.env` da Stack no Portainer) | No Supabase e MinIO do Revendedor (cadastrados na tabela `revendedores`) | Sempre no Supabase e MinIO do Revendedor (**Custo Zero de Storage/DB para você**) |
| **Risco de Bloqueio de IP pela Meta** | **Zero risco para você** (cada revenda usa o IP da própria VPS) | Exige Proxy Residencial por instância/revenda | Isolado por revenda |
| **Controle de Licença (Kill-Switch)** | Container valida `LICENCA_CHAVE` no boot + Heartbeat a cada 15 min | Bloqueio instantâneo direto no seu painel | Bloqueio no boot + Heartbeat + assinatura criptográfica Ed25519 |
| **Atualizações Automáticas** | Via `watchtower` ou Portainer Webhook puxando do seu Registry Privado (`ghcr.io`) | Você atualiza 1 único servidor e todos recebem | Unificado via imagem Docker oficial privada |

> **Recomendação Estratégica (Modelo Uazapi):**
> A **Opção A (Stack no Portainer do Revendedor)** combinada com a **Opção B (Multi-Connection Pool por Revenda)** é o modelo mais lucrativo e escalável do mercado brasileiro porque **zera seu custo de VPS, zera seu custo de Supabase, zera seu custo de MinIO/S3 e isola o IP de cada revendedor perante a Meta**.

---

## 3. Como Blindar o Repositório, a Imagem Docker e a Licença

### 3.1. Proteção do Código-Fonte e Imagem Docker
1. **Repositório Git 100% Privado:** Nenhum cliente tem acesso ao GitHub do código.
2. **Frontend Embutido no Binário (`embed.FS`):** Embutimos a pasta `web/` dentro do executável Go e compilamos com `-trimpath -ldflags="-w -s"`.
3. **Container Blindado:** A imagem final contém apenas o binário `/woohub-api`, `ffmpeg` estático e certificados TLS.
4. **Distribuição via Registry Privado (GitHub Container Registry `ghcr.io` ou Docker Hub Privado):**
   - Quando você vende uma licença para o Revendedor, seu Servidor de Licenças gera um **Token de Leitura (Pull Token)** exclusivo ou fornece um binário Bootstrap que baixa a atualização apenas se a licença estiver `ATIVA`.

### 3.2. Motor de Licenciamento (`internal/licenca`) — "Sem Licença, Não Sobe"
O binário `woohub-api` passa a ter um **Gatekeeper obrigatório** antes da etapa 1 do `main.go`:

```mermaid
sequenceDiagram
    participant Container as Container WooHub (Portainer do Cliente)
    participant Master as Seu Servidor Central de Licenças (Master API)
    participant SupabaseRev as Supabase do Revendedor
    participant MinIORev as MinIO do Revendedor

    Note over Container: 1. Boot do Container (main.go)
    Container->>Master: POST /v1/licenca/validar (LICENCA_CHAVE + Fingerprint + Dominio)
    alt Licença Inválida / Vencida / Inadimplente
        Master-->>Container: HTTP 403 (Licença Bloqueada / Expirada)
        Note over Container: os.Exit(1) -> Container morre e não sobe!
    else Licença Ativa
        Master-->>Container: JWT Ed25519 Assinado (MaxInstancias, Plano, Validade, VersaoMinima)
        Container->>SupabaseRev: Conecta e aplica Migrations no Supabase do Revendedor
        Container->>MinIORev: Conecta no Bucket S3/MinIO do Revendedor
        Note over Container: 🟢 Sobe API e Painel White-Label
    end

    loop Heartbeat a cada 30 minutos
        Container->>Master: POST /v1/licenca/heartbeat (Instâncias Ativas, Versão, Uptime)
        alt Licença Cancelada ou Limite Excedido
            Master-->>Container: Comando KILL / SUSPEND
            Note over Container: Desconecta sockets WhatsApp e encerra processo
        end
    end
```

#### O que a Licença Criptográfica (Assinada com Chave Pública `Ed25519` embutida no binário) controla:
- **`status`**: `ATIVA`, `SUSPENSA`, `EXPIRADA` (se não estiver ativa no boot, o processo executa `log.Fatal` e o container não sobe).
- **`limite_instancias`**: Ex: Licença de 50 conexões. Mesmo que o revendedor tenha acesso total ao próprio banco Supabase dele e tente mudar o número na tabela `revendedores` para `99999`, **o código Go ignora o banco e respeita o limite assinado na licença criptográfica**.
- **`dominio_autorizado` / `ip_autorizado`**: Impede que o revendedor pegue a mesma `LICENCA_CHAVE` e suba em 10 servidores diferentes no Portainer. Se subir em outro IP/Domínio, o servidor de licenças invalida o anterior ou bloqueia o novo.
- **`versao_minima`**: Permite que você force todos os revendedores a atualizarem a imagem Docker quando houver mudança crítica no protocolo da Meta.

---

## 4. Isolamento Total de Banco (Supabase) e Mídia (MinIO/S3) por Revenda

Hoje, o que mais gera custo oculto em APIs de WhatsApp são **mídias (áudios PTT, vídeos, PDFs, fotos)** e **armazenamento de logs/sessões no banco**.

### Como funciona o isolamento em cada modelo:

### Cenário 1: Stack no Portainer do Revendedor (1 Stack = 1 Revenda)
Você entrega para o revendedor um template pronto de **Stack do Portainer (`docker-compose.stack.yml`)**. O revendedor preenche apenas as variáveis dele no Portainer:

```yaml
version: "3.8"
services:
  woohub-api:
    image: ghcr.io/sua-empresa/woohub-api:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      # 1. LICENÇA OBRIGATÓRIA (Fornecida por você)
      - WOOHUB_LICENCA_CHAVE=woo_lic_9f8a7b6c5d4e3f2a1b
      - WOOHUB_DOMINIO_OFICIAL=api.revendacliente.com.br

      # 2. SUPABASE PRÓPRIO DO REVENDEDOR (Custo Zero para você)
      - TIPO_BANCO=postgres
      - URI_BANCO=postgresql://postgres.xxxx:senha@aws-0-sa-east-1.pooler.supabase.com:5432/postgres?sslmode=require

      # 3. MINIO / S3 PRÓPRIO DO REVENDEDOR (Custo Zero de Mídia para você)
      - STORAGE_DRIVER=s3
      - MINIO_ENDPOINT=https://s3.revendacliente.com.br
      - MINIO_ACCESS_KEY=minio_user_revenda
      - MINIO_SECRET_KEY=minio_senha_revenda
      - MINIO_BUCKET=whatsapp-midias
      - MINIO_USE_SSL=true

      # 4. ADMIN INICIAL DA REVENDA
      - ADMIN_EMAIL=contato@revendacliente.com.br
      - ADMIN_SENHA=SenhaForteDaRevenda123!
```

* Quando o container sobe no Portainer dele:
  1. Valida a `WOOHUB_LICENCA_CHAVE` no seu servidor mestre.
  2. Conecta no **Supabase dele** e cria automaticamente todas as tabelas (`revendedores`, `clientes`, `instancias`, `logs_mensagens`, `whatsmeow_device`, etc.).
  3. Conecta no **MinIO dele** e salva todos os áudios, fotos, vídeos e documentos recebidos/enviados diretamente no MinIO dele, enviando apenas a URL pública/assinada no Webhook!

---

### Cenário 2: Servidor Único Seu, mas com Supabase + MinIO Isolado por Revenda (Multi-Tenant BYODB + BYOS3)
Se você quiser oferecer também um plano onde **você hospeda o servidor Go**, mas **não quer pagar banco nem storage de mídia das revendas**:

1. **No Seu Banco Mestre (Control Plane):**
   - Fica apenas a tabela de `licencas_revendas` e o roteamento (`revendedor_id`, `dominio`, `supabase_uri_criptografada`, `minio_endpoint`, `minio_access_key`, `minio_secret_key`, `minio_bucket`).
2. **Pool Dinâmico de Conexões no Go (`TenantDBManager` & `TenantStorageManager`):**
   - Quando o Revendedor A cadastra a URI do Supabase dele e as chaves do MinIO dele no painel White-Label:
   - O motor Go abre a conexão `sql.DB` + `sqlstore.Container` direto no Supabase do Revendedor A e envia todas as mídias do Revendedor A direto para o MinIO do Revendedor A.
   - Se o Revendedor A parar de pagar, você apenas desativa a chave dele no seu painel Mestre — você não tem nenhum dado pesado dele ocupando seu disco ou seu Supabase!

---

## 5. Plano de Implementação Prático (O que podemos construir agora)

Podemos dividir a implementação em **4 módulos rápidos** dentro do projeto:

1. **Módulo 1 — Storage S3 / MinIO Nativo (`internal/storage/minio.go`):**
   - Integrar cliente S3/MinIO compatível com MinIO, AWS S3, Cloudflare R2 e Supabase Storage S3.
   - Suporte tanto a configuração global via `.env` (para Stack Portainer) quanto credenciais MinIO individuais por Revendedor (salvas na tabela `revendedores`).
   - Upload automático de mídias recebidas/enviadas retornando o link direto do MinIO do revendedor nos Webhooks.

2. **Módulo 2 — Sistema de Licenciamento & Kill-Switch (`internal/licenca/` + Servidor de Licenças):**
   - Criar o validador de licença no boot (`ValidarLicencaObrigatoria`) + Heartbeat periódico em background + trava criptográfica de `limite_instancias` e `fingerprint/domínio`.
   - Criar o **Módulo Gerador/Validador de Licenças (Admin Master)** para você emitir, suspender, renovar e monitorar todas as stacks ativas nos Portainers dos seus clientes.

3. **Módulo 3 — Isolamento Multi-Banco por Revenda (`TenantPool`):**
   - Permitir que cada revendedor configure sua própria `supabase_uri` e seu próprio `minio_endpoint` (ou rode 100% isolado via Stack Portainer).

4. **Módulo 4 — Empacotamento Blindado para Portainer (`embed.FS` + `docker-compose.portainer.yml` + Auto-Update):**
   - Embutir a pasta `web/` dentro do binário Go (`//go:embed`) para que o container não exponha nenhum arquivo avulso.
   - Gerar o template oficial de **Stack Portainer** com `watchtower` configurado para atualizar os containers automaticamente quando você lançar uma nova versão.
