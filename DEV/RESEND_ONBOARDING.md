# Resend e Onboarding por Perfil

## Resend (Conexão 1-Clique OAuth 2.1 & Sincronização Cloudflare)

Cada revenda pode se conectar ao Resend tanto via **OAuth 2.1 em 1 clique** (sem necessidade de cadastrar ou copiar chaves manuais caso já tenha conta no Resend) quanto via API Key manual:

- `GET /api/v1/revenda/email`: Retorna status, modo de autenticação (`oauth` ou `api_key`), domínio em uso, remetente e domínios verificados da conta. A chave/token nunca é vazada no GET.
- `POST /api/v1/revenda/email`: Salva ou atualiza a configuração do remetente.
- `GET /api/v1/revenda/email/oauth/iniciar`: Gera o desafio PKCE (RFC 7636) e o link oficial do Resend para consentimento.
- `GET /api/v1/revenda/email/oauth/callback`: Recebe o retorno do Resend, troca o código pelo Bearer token, busca os domínios via `GET https://api.resend.com/domains` e persiste automaticamente.
- `GET /api/v1/revenda/email/dominios`: Sincroniza em tempo real a lista de domínios da conta e o status de verificação DNS.
- `POST /api/v1/revenda/email/testar`: Dispara e-mail de validação transacional.

### Integração com Cloudflare (Domain Connect)
Caso o domínio do cliente ou revenda ainda não esteja verificado no DNS, a interface disponibiliza atalho direto para a ferramenta nativa de **Domain Connect** do Resend com a Cloudflare (`https://resend.com/domains`). Com um clique no Resend, os registros TXT (SPF), MX e CNAME (DKIM) são injetados na Cloudflare (lembrando de manter o proxy desativado / nuvem cinza no Cloudflare para evitar interceptação).

### Variáveis de Ambiente (.env)
```env
# E-mail padrão da plataforma
RESEND_API_KEY=
RESEND_FROM=noreply@seudominio.com.br
RESEND_FROM_NAME=WooHub

# Conexão 1-Clique via Resend OAuth (OAuth 2.1 com PKCE)
RESEND_OAUTH_CLIENT_ID=
RESEND_OAUTH_CLIENT_SECRET=
RESEND_OAUTH_REDIRECT_URI=
```

## Onboarding
`GET` e `POST /api/v1/onboarding` persistem a etapa, o papel e os dados auxiliares da conta. O painel mostra uma introdução específica para `revendedor` ou `cliente` após o primeiro login.
O onboarding de revendedor orienta marca, infraestrutura (Supabase + MinIO), Resend e primeiro cliente.

## Verificação
Todos os testes automatizados foram executados e aprovados:
- `go test -v ./internal/email` (PKCE RFC 7636, URL de autorização, envio)
- `go test -v ./internal/api -run TestResendOAuthControleAcessoEDominios` (controle de acesso por papel, inicialização e listagem de domínios)
- `go build ./...` (compilação completa da aplicação)
