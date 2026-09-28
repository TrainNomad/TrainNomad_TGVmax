# Quick Start - TGVmax API

## ⚡ Démarrage rapide (5 min)

### 1. Installer Go (si pas fait)
```bash
# macOS
brew install go

# Linux
sudo apt-get install golang-go

# Windows
# Télécharger depuis https://go.dev/dl/
```

### 2. Cloner & Configurer
```bash
cd Backend/tgvmax

# Créer les variables d'environnement
cp .env.example .env
# Éditer .env si besoin
```

### 3. Télécharger les données
```bash
# Option A : Utiliser make (recommandé)
make data

# Option B : Directement
go run ./cmd -output data.bin.gz
```

### 4. Lancer le serveur
```bash
# Option A : Utiliser make
make run

# Option B : Directement  
go run main.go converter.go types.go

# Option C : Build + run
go build -o tgvmax-api .
./tgvmax-api
```

Serveur lancé sur `http://localhost:8000` ✓

---

## 🧪 Tester l'API

### Health Check
```bash
curl http://localhost:8000/health
```

### Rechercher des trajets
```bash
# Tous les trajets Paris → Lyon
curl "http://localhost:8000/api/tgvmax/search?from=PARIS&to=LYON"

# Avec date spécifique
curl "http://localhost:8000/api/tgvmax/search?from=PARIS&to=LYON&date=2024-01-15"

# Places gratuites uniquement
curl "http://localhost:8000/api/tgvmax/search?from=PARIS&to=LYON&free_only=true"
```

### Lister les gares
```bash
curl http://localhost:8000/api/tgvmax/stations | head -50
```

### Recharger les données
```bash
export RELOAD_SECRET="your-secret"
curl -X POST \
  -H "X-Reload-Secret: $RELOAD_SECRET" \
  http://localhost:8000/api/tgvmax/reload
```

---

## 📦 Structure des réponses

### ✓ Recherche réussie
```json
{
  "query": {
    "from": "PARIS",
    "to": "LYON"
  },
  "results": [
    {
      "id": "tgv_001",
      "train_number": "9001",
      "departure": "Paris Gare de Lyon",
      "arrival": "Lyon Part-Dieu",
      "departure_time": "07:15",
      "arrival_time": "09:45",
      "available_seats": 145,
      "total_seats": 240,
      "duration_minutes": 150
    }
  ],
  "count": 12
}
```

### ✗ Erreur (param manquant)
```json
{
  "error": "Missing parameters",
  "detail": "from and to are required"
}
```

---

## 🔧 Commandes utiles

```bash
# Formater le code
make fmt

# Vérifier les erreurs
make lint

# Nettoyer les fichiers générés
make clean

# Voir toutes les commandes disponibles
make help
```

---

## 🐳 Avec Docker

```bash
# Build l'image
docker build -t tgvmax-api .

# Lancer le conteneur
docker run -p 8000:8000 \
  -e RELOAD_SECRET="secret" \
  tgvmax-api

# Tester
curl http://localhost:8000/health
```

---

## 📊 Structure des données

```
tgvmax/
├── main.go           ← Serveur HTTP + endpoints
├── types.go          ← Structures (Train, Station, etc.)
├── converter.go      ← Téléchargement & conversion JSON→binaire
├── cmd/
│   └── main.go       ← CLI de conversion
├── data.bin.gz       ← Données compilées (généré)
├── go.mod            ← Dépendances Go
└── Makefile          ← Commandes utiles
```

---

## 🚀 Déployer sur Render

1. **Créer un service** sur [render.com](https://render.com)
   - Connecter ce repo GitHub
   - Build command: `cd tgvmax && go run ./cmd -output data.bin.gz && go build -o tgvmax-api .`
   - Start command: `cd tgvmax && ./tgvmax-api`

2. **Ajouter les secrets**
   ```
   RELOAD_SECRET=your-secret
   GOMEMORYALIMIT=350MiB
   ```

3. **Configurer GitHub Actions**
   ```bash
   gh secret set RENDER_TGVMAX_URL -b "https://your-app.onrender.com"
   gh secret set RELOAD_SECRET -b "your-secret"
   ```

4. **Profit !** 🎉
   - Mise à jour automatique chaque jour à 3h UTC
   - Reload sans redémarrage
   - Monitoring via `/health`

---

## 📝 Fichiers à adapter

Avant le déploiement, éditer :

1. **`.github/workflows/update-tgvmax-data.yml`**
   - Remplacer `[USERNAME]/[REPO]` par votre repo

2. **`.env`** (dev)
   ```bash
   RELOAD_SECRET=your-super-secret-key
   GITHUB_TOKEN=ghp_... (optionnel)
   RENDER_TGVMAX_URL=https://your-app.onrender.com
   ```

3. **`render.yaml`** ou **`Procfile`** (pour Render)
   - Vérifier les chemins relatifs

---

## 🆘 Troubleshooting

| Problème | Solution |
|----------|----------|
| `command not found: go` | Installer Go depuis golang.org |
| `data.bin.gz not found` | Exécuter `make data` |
| `port already in use` | `PORT=3000 make run` |
| `401 Unauthorized` | Vérifier `RELOAD_SECRET` |
| `memory limit exceeded` | Réduire données ou upgrade RAM |

---

## 🔗 Liens utiles

- 📖 [Documentation complète](./README.md)
- 🚀 [Guide de déploiement](./DEPLOYMENT.md)
- 🐳 [Dockerfile](./Dockerfile)
- 📦 [SNCF Open Data](https://ressources.data.sncf.com)

---

**Prêt ? Commencez avec** :
```bash
cd Backend/tgvmax
make data && make run
# Puis visitez http://localhost:8000/health
```
