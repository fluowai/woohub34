# Webhooks WooHub

## Configuração

Cada instância recebe eventos via `POST` na URL informada no painel. O corpo sempre possui:

```json
{
  "instancia_id": "inst_123",
  "nome_instancia": "Principal",
  "evento": "mensagem_recebida",
  "data_hora": "2026-11-04T15:00:00-03:00",
  "dados": {}
}
```

Se um segredo for cadastrado, valide o cabeçalho `X-WooHub-Signature-SHA256` comparando-o com o HMAC-SHA256 do corpo bruto da requisição. O envio possui retentativa automática para falhas HTTP ou de rede.

## Eventos

Os eventos atualmente suportados são `mensagem_recebida`, `status_envio`, `conexao`, `presenca`, `qrcode`, `chamada_recebida`, `voto_enquete`, `reacao_mensagem` e `atualizacao_grupo`. O conteúdo detalhado fica no campo `dados`, porque varia conforme a operação do WhatsApp.
