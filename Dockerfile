# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Dépendances
RUN apk add --no-cache git make

# Copier les sources
COPY . .

# Télécharger les dépendances
RUN go mod download

# Compiler
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags="-w -s" \
    -o tgvmax-api .

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata curl

WORKDIR /app

# Copier le binaire depuis le builder
COPY --from=builder /build/tgvmax-api .

# Créer un répertoire pour les données
RUN mkdir -p data

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:${PORT:-8000}/health || exit 1

# Port
EXPOSE 8000

# Lancer le serveur
CMD ["./tgvmax-api"]
