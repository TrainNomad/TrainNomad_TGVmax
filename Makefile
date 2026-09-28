.PHONY: build run fmt lint test clean help data

# Variables
BINARY=tgvmax-api
GO=go
GOFLAGS=-v
PORT ?= 8000

help:
	@echo "TGVmax API - Targets disponibles:"
	@echo ""
	@echo "  make build          Compiler le serveur"
	@echo "  make run            Démarrer le serveur (PORT=8000 par défaut)"
	@echo "  make data           Télécharger et convertir les données"
	@echo "  make fmt            Formater le code"
	@echo "  make lint           Vérifier le code"
	@echo "  make test           Exécuter les tests"
	@echo "  make clean          Nettoyer les fichiers générés"
	@echo ""
	@echo "Exemples:"
	@echo "  make run PORT=3000"
	@echo "  make data"

build:
	@echo "📦 Compilation du serveur..."
	$(GO) build $(GOFLAGS) -o $(BINARY) .

run: build
	@echo "🚀 Démarrage du serveur sur le port $(PORT)..."
	PORT=$(PORT) ./$(BINARY)

data:
	@echo "📥 Téléchargement des données TGVmax..."
	$(GO) run ./cmd -output data.bin.gz
	@echo "✓ Données converties en data.bin.gz"

fmt:
	@echo "🎨 Formatage du code..."
	$(GO) fmt ./...
	@echo "✓ Code formaté"

lint:
	@echo "🔍 Vérification du code..."
	$(GO) vet ./...
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run ./...; \
	fi
	@echo "✓ Pas d'erreurs détectées"

test:
	@echo "🧪 Exécution des tests..."
	$(GO) test -v -cover ./...

clean:
	@echo "🧹 Nettoyage..."
	$(GO) clean
	rm -f $(BINARY)
	rm -f *.prof
	@echo "✓ Nettoyé"

deps:
	@echo "📚 Installation des dépendances..."
	$(GO) mod tidy
	$(GO) mod download

# Docker
docker-build:
	@echo "🐳 Construction de l'image Docker..."
	docker build -t $(BINARY):latest .

docker-run:
	@echo "🚀 Démarrage du conteneur..."
	docker run -p $(PORT):8000 $(BINARY):latest

# Release
release: clean lint test build data
	@echo "✓ Build de release prêt"
	@ls -lh $(BINARY) data.bin.gz

.DEFAULT_GOAL := help
