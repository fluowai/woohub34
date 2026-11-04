# Resend e onboarding por perfil

## Resend

Cada revenda pode cadastrar uma API Key do Resend, domínio, remetente e nome do remetente em:

- `GET /api/v1/revenda/email`
- `POST /api/v1/revenda/email`
- `POST /api/v1/revenda/email/testar`

O escopo é limitado ao `revendedor_id` do usuário autenticado. A API Key nunca é devolvida no `GET`. O remetente precisa usar um domínio verificado no Resend. O envio efetivo usa a API HTTPS oficial do Resend.

Para o envio padrão da plataforma, configure `RESEND_API_KEY`, `RESEND_FROM` e `RESEND_FROM_NAME` no ambiente. O cadastro por revenda está preparado para ser usado por recuperação de acesso, convites e notificações transacionais.

## Onboarding

`GET` e `POST /api/v1/onboarding` persistem a etapa, o papel e os dados auxiliares da conta. O painel mostra uma introdução específica para `revendedor` ou `cliente` após o primeiro login.

O onboarding de revendedor orienta marca, infraestrutura, Resend e primeiro cliente. O onboarding de cliente final orienta dados e primeira instância.

## Verificação

O ambiente desta máquina não expõe os binários `go`/`gofmt`, portanto a validação compilada não pôde ser executada aqui. Antes do deploy, executar `gofmt -w` nos arquivos Go alterados e `go test ./...`.
