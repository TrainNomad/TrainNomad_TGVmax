# TGVmax API - SNCF OpenData Router

Routeur de trajets TGVmax utilisant l'API Europe (RAPTOR) avec données SNCF.

## Setup

```bash
# 1. Générer données GTFS depuis SNCF API
python3 sncf_to_gtfs.py

# 2. Compiler en format network.bin (comme Europe)
# TODO: Implémenter GTFS -> network.bin converter

# 3. Démarrer le serveur
go run main.go api.go network.go engine.go places.go timetable.go raptor.go bounds.go

# 4. Tester
curl http://localhost:8000/health
curl "http://localhost:8000/search?from=Paris&to=Lyon"
```

## Architecture

- **sncf_to_gtfs.py** - Convertit données SNCF JSON → GTFS standard (stops.txt, routes.txt, trips.txt, stop_times.txt)
- **API Europe (Go)** - Routeur RAPTOR optimisé (357K trajets, 316 gares)

## Data Flow

```
SNCF OpenData JSON
  ↓ (sncf_to_gtfs.py)
GTFS files (stops.txt, trips.txt, etc.)
  ↓ (network compiler - TODO)
network.bin.gz
  ↓ (Go API)
HTTP Server (port 8000)
  ├── GET /health
  ├── GET /search?from=...&to=...
  ├── GET /stations?q=...
  └── GET /explorer?from=...
```

## Status

- ✅ Python converter (SNCF → GTFS)
- ✅ Go API (Europe RAPTOR engine)
- ⏳ TODO: GTFS → network.bin compiler

## License

Data: SNCF OpenData
Code: TrainNomad
