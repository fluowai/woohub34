# ==============================================================================
# ESTÁGIO 1: COMPILAÇÃO DO BINÁRIO BLINDADO (FRONTEND EMBUTIDO VIA go:embed)
# ==============================================================================
FROM golang:alpine AS builder

WORKDIR /app

ENV GOTOOLCHAIN=auto
RUN apk add --no-cache git gcc musl-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Compila binário único sem tabela de símbolos (-w -s), sem caminhos do host (-trimpath)
# e com todo o painel web (HTML/CSS/JS) embutido dentro do executável
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /app/woohub-api ./cmd/woohub-api

# ==============================================================================
# ESTÁGIO 2: CONTAINER DE PRODUÇÃO ENXUTO (PROTEGIDO POR LICENÇA NO BOOT)
# ==============================================================================
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata ffmpeg wget
ENV TZ=America/Sao_Paulo
ENV AMBIENTE=production
# Por padrão nos containers distribuídos para Portainer, exige licença ativa para subir
ENV EXIGIR_LICENCA=true
ENV TIPO_BANCO=postgres

# Copia apenas o binário compilado (código-fonte e arquivos web ficam ocultos dentro do binário)
COPY --from=builder /app/woohub-api /app/woohub-api

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/v1/status || exit 1

CMD ["/app/woohub-api"]
