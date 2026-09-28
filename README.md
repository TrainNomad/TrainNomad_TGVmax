# TGVmax API

API Go pour rechercher et filtrer les trajets TGVmax de la SNCF avec chargement optimisé des données en mémoire RAM.

## Architecture

### Structure
```
tgvmax/
├── main.go          # Serveur HTTP principal
├── types.go         # Structures de données
├── converter.go      # Téléchargement et conversion JSON → binaire gzippé
├── go.mod          # Module Go
├── cmd/main.go      # CLI pour conversion de données
├── data.bin.gz     # Données compilées (généré)
└── README.md
```

### Flux de données

```
1. Script de conversion (cron/manual)
   ├─ Télécharger JSON depuis SNCF API
   ├─ Parser et filtrer champs pertinents
   └─ Encoder en binaire gzippé (data.bin.gz)

2. Déploiement
   ├─ GitHub Actions crée une Release avec data.bin.gz
   └─ Serveur télécharge depuis la Release

3. Runtime
   ├─ Charger data.bin.gz en mémoire au démarrage
   ├─ Fournir endpoints API de recherche
   └─ Endpoint POST /reload pour mise à jour sans redémarrage
```

## Installation & Usage

### Démarrage local

```bash
cd tgvmax

# Télécharger et convertir les données
go run ./cmd -output data.bin.gz

# Lancer le serveur
go run main.go converter.go types.go

# Le serveur écoute sur http://localhost:8000
```

### Variables d'environnement

```bash
PORT=8000                    # Port d'écoute (défaut: 8000)
RELOAD_SECRET=xyz           # Secret pour l'endpoint /reload
GITHUB_TOKEN=ghp_...        # Token GitHub (optionnel, augmente les quotas API)
RENDER_TGVMAX_URL=https://... # URL de l'instance Render pour reload automatique
```

## API Endpoints

### 1. Health Check
```bash
GET /health
```

Réponse :
```json
{
  "status": "healthy",
  "timestamp": "2024-01-01T03:00:00Z",
  "trains_count": 5242,
  "stations_count": 312,
  "data_loaded_at": "2024-01-01T02:55:30Z",
  "uptime": "45m30.5s"
}
```

### 2. Recherche de trajets
```bash
GET /api/tgvmax/search?from=PARIS&to=LYON&date=2024-01-15&free_only=false
```

**Paramètres** :
- `from` (string): Gare de départ (nom ou ID)
- `to` (string): Gare d'arrivée (nom ou ID)
- `date` (string, optionnel): Date de circulation (YYYY-MM-DD)
- `free_only` (boolean, optionnel): Filtrer places à 0€ uniquement

**Réponse** :
```json
{
  "query": {
    "from": "PARIS",
    "to": "LYON",
    "date": "2024-01-15"
  },
  "results": [
    {
      "id": "tgv_001",
      "train_number": "9001",
      "departure": "Paris Gare de Lyon",
      "arrival": "Lyon Part-Dieu",
      "departure_time": "07:15",
      "arrival_time": "09:45",
      "origin": {
        "id": "PGLYON",
        "name": "Paris Gare de Lyon",
        "city": "Paris",
        "country": "France",
        "latitude": 48.8432,
        "longitude": 2.3734
      },
      "destination": {
        "id": "LPARDIEU",
        "name": "Lyon Part-Dieu",
        "city": "Lyon",
        "country": "France",
        "latitude": 45.7655,
        "longitude": 4.8352
      },
      "available_seats": 145,
      "total_seats": 240,
      "free_seats_only": false,
      "duration_minutes": 150
    }
  ],
  "count": 12
}
```

### 3. Lister les gares
```bash
GET /api/tgvmax/stations
```

**Réponse** :
```json
{
  "stations": [
    {
      "id": "PGLYON",
      "name": "Paris Gare de Lyon",
      "code": "FRSQL",
      "city": "Paris",
      "country": "France",
      "latitude": 48.8432,
      "longitude": 2.3734
    }
  ],
  "count": 312
}
```

### 4. Recharger les données (Hot Reload)
```bash
POST /api/tgvmax/reload
X-Reload-Secret: xyz
```

**Réponse** :
```json
{
  "status": "success",
  "message": "Data reloaded",
  "duration": "245ms",
  "timestamp": "2024-01-01T03:15:42Z"
}
```

## Configuration GitHub Actions

### Secrets à ajouter au repo

| Secret | Description | Exemple |
|--------|-------------|---------|
| `RENDER_TGVMAX_URL` | URL complète de l'API sur Render | `https://my-app.onrender.com` |
| `RELOAD_SECRET` | Secret pour l'endpoint `/reload` | `super_secret_xyz` |
| `GITHUB_TOKEN` | Token GitHub (auto-générés par Actions) | (auto) |

### Workflow - Mise à jour automatique

Le workflow `.github/workflows/update-tgvmax-data.yml` :

1. **Déclenché** : Chaque jour à 3h UTC ou manuellement via `workflow_dispatch`
2. **Étapes** :
   - Télécharge le JSON SNCF
   - Compile en `data.bin.gz`
   - Crée/met à jour une Release GitHub (`latest-tgvmax`)
   - Appelle l'endpoint de reload sur Render

**Pour tester** :
```bash
# Via Actions UI ou :
gh workflow run update-tgvmax-data.yml --ref main
```

## Format de données binaire

Le fichier `data.bin.gz` est compressé et encodé ainsi :

```
Magic: "TGVDATA001" (10 bytes)
Version: int32 (4 bytes)
Timestamp: int64 (8 bytes)

[Trains] (count: int32)
  ├─ ID: string
  ├─ TrainNumber: string
  ├─ Departure: string
  ├─ Arrival: string
  ├─ DepartureTime: string (HH:MM)
  ├─ ArrivalTime: string (HH:MM)
  ├─ AvailableSeats: int32
  ├─ TotalSeats: int32
  ├─ OperatingDay: string
  ├─ OriginStationIndex: int32
  ├─ DestinationStationIndex: int32
  └─ [Stops]
       ├─ StationIndex: int32
       ├─ Arrival: string
       └─ Departure: string

[Stations] (count: int32)
  ├─ ID: string
  ├─ Name: string
  ├─ Code: string
  ├─ City: string
  ├─ Country: string
  ├─ Latitude: float64
  └─ Longitude: float64
```

## Performance

- **Chargement initial** : ~200-500ms (selon taille données)
- **Mémoire** : ~30-80 Mo (512 Mo limit sur Render gratuit)
- **Recherche** : < 10ms (indexée en mémoire)
- **Reload à chaud** : ~250ms (sans redémarrage)

## CORS

L'API supporte CORS avec headers :
- `Access-Control-Allow-Origin: *`
- `Access-Control-Allow-Methods: GET, POST, OPTIONS`
- `Access-Control-Allow-Headers: Content-Type, Authorization`

## Troubleshooting

### Erreur "data.bin.gz not found"
```
[INFO] Fichier data.bin.gz non trouvé localement, tentative de téléchargement...
```

**Solution** : Assurer que `data.bin.gz` est présent ou que `GITHUB_TOKEN` est configuré.

### Mémoire insuffisante
```
memory limit exceeded
```

**Solution** : Limiter la taille des données ou augmenter la limite dans `main.go`.

### Reload échoue
```
POST /api/tgvmax/reload → 401 Unauthorized
```

**Solution** : Vérifier le header `X-Reload-Secret` et la variable `RELOAD_SECRET`.

## Développement

```bash
# Formatter le code
go fmt ./...

# Linter
golangci-lint run ./...

# Tests (À ajouter)
go test -v ./...
```

## Licence

Données : [SNCF Open Data](https://ressources.data.sncf.com)
Code : TrainNomad Backend
