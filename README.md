# API TGVmax (TrainNomad)

Réseau séparé ne contenant **que les trajets avec une place TGVmax disponible**, servi par le même
moteur de routage Go (RAPTOR) que l'API Europe. Même format de réponses, mêmes identifiants de gares
(`station:<UIC>`, `city:TL<id>`) : le front peut interroger l'une ou l'autre API avec le même code.

## Pipeline

```
SNCF Open Data (dataset « tgvmax », CSV)          python sncf_to_gtfs.py
        │  ≈ 350 000 couples origine→destination, OUI/NON, 30 jours
        ▼
tgvmax_gtfs.zip  (GTFS standard)                    python build_network.py
        │  un trajet par couple disponible, arrêts intermédiaires pour l'affichage
        ▼
network.bin.gz   (format binaire TNNET001, identique à Europe, ~0,2 Mo)
        │  go test (le moteur doit le charger et trouver des trajets)
        ▼
commit sur GitHub ──► Render redéploie l'image Docker (binaire Go + network.bin.gz)
```

La GitHub Action `.github/workflows/update_tgvmax.yml` exécute ce pipeline chaque jour à 5h15 UTC
(ou à la demande via « Run workflow »). Des garde-fous empêchent de publier un réseau vide si l'export
SNCF est incomplet.

### Choix de modélisation

- **La disponibilité TGVmax dépend du couple origine→destination**, pas du train. Chaque couple « OUI »
  devient un trajet où l'on ne peut monter qu'à l'origine et descendre qu'à la destination : le moteur ne
  propose donc que des billets réellement réservables (et les correspondances entre eux).
- Les arrêts intermédiaires sont reconstitués à partir de tous les couples du train (OUI et NON), pour
  la liste des arrêts et le tracé sur la carte.
- Le champ `date` est le jour de départ du train : les heures après minuit (trains de nuit) sont
  écrites `24:xx` comme le veut GTFS.
- Gares : rapprochées du référentiel `stations.csv` par leur code SNCF (`FRPLY` = Paris Gare de Lyon).
  Le fichier est pris dans `../gtfs/stations.csv` en local, sinon téléchargé depuis le dépôt
  TrainNomad_GTFS (variable `STATIONS_CSV` pour forcer un chemin).

## Fichiers

| fichier | rôle |
|---|---|
| `sncf_to_gtfs.py` | téléchargement de l'export SNCF + conversion GTFS |
| `build_network.py` | compilation GTFS → `network.bin` / `network.bin.gz` (+ `build_report.json`) |
| `stations.py` | référentiel des gares partagé par les deux scripts |
| `*.go` | API (copie du moteur `Europe/`, seuls les libellés changent) |
| `tgvmax_test.go` | tests exécutés avant chaque publication |
| `Dockerfile`, `render.yaml` | déploiement Render (Docker, offre gratuite) |

Les fichiers `*.go` (hors test) sont des copies de `Europe/` : une amélioration du moteur Europe
peut être recopiée telle quelle.

## En local

```bash
pip install -r requirements.txt
python sncf_to_gtfs.py        # --refresh pour forcer le téléchargement
python build_network.py
go test ./...
go run .                      # http://localhost:8000
```

## Endpoints

Identiques à l'API Europe (voir `Europe/API.md`) :

- `GET /health` : période couverte (`valid_from` / `valid_to` ≈ aujourd'hui → J+30), date de compilation
- `GET /stations?q=par` : autocomplétion (seules les gares desservies en TGVmax)
- `GET /search?from=Paris&to=Marseille&date=2026-10-01&time=08:00` : trajets TGVmax, avec correspondances
- `GET /explorer?from=Paris&date=2026-10-01` : toutes les destinations atteignables en TGVmax dans la journée

`train_type` vaut `SNCF TGV INOUI`, `SNCF TGV Lyria`, `SNCF Intercités`, `SNCF Intercités de nuit` ou `SNCF Car`.

## Déploiement Render

Créer un « Web Service » depuis ce dépôt (Render lit `render.yaml` : runtime Docker, `healthCheckPath: /health`,
auto-deploy). Chaque commit de la GitHub Action déclenche un redéploiement (~1 min).
