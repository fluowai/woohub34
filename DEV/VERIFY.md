# Verificação de prontidão

## Verificações executadas em 2026-11-04

- `node --check web/app.js`: aprovado.
- `git diff --check`: aprovado.
- Inspeção de configuração, autenticação, webhooks e manifests Docker: concluída.

## Verificações pendentes

- `go test ./...`
- `go vet ./...`
- `go build ./cmd/woohub-api`
- `docker build .`
- Teste de integração com PostgreSQL/Supabase, MinIO e proxy reverso HTTPS.

As verificações pendentes não puderam ser executadas porque Go e Docker não estão instalados/disponíveis no ambiente atual.
