package web

import "embed"

// ArquivosEmbutidos embute o Painel Web e o WhatsApp Web (HTML, CSS e JS) diretamente dentro do binário Go compilado,
// impedindo inspeção ou cópia dos arquivos estáticos nos containers Portainer dos clientes.
//
//go:embed index.html style.css app.js docs.html chat.html chat.css chat.js
var ArquivosEmbutidos embed.FS
