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
Release GitHub « network-latest » ──► l'API sur Render le télécharge et le recharge à chaud (sans build)
```

La GitHub Action `.github/workflows/update_tgvmax.yml` exécute ce pipeline chaque jour à 5h15 UTC
(ou à la demande via « Run workflow »). Des garde-fous empêchent de publier un réseau vide si l'export
SNCF est incomplet.

### Mise à jour sans redéploiement (Render gratuit)

Les données ne passent plus par un commit : l'Action publie `network.bin.gz` dans la Release `network-latest`
(URL fixe), et l'API (`reload.go`) le récupère elle-même. Render ne rebuild donc que si le code change
(`buildFilter` de `render.yaml`), ce qui évite d'épuiser les minutes de build gratuites.

- **Au démarrage** (et donc à chaque réveil d'une instance endormie) : téléchargement de `NETWORK_URL`,
  repli sur le `network.bin.gz` de l'image si GitHub ne répond pas.
- **`POST /reload`** (`Authorization: Bearer $RELOAD_TOKEN`) : appelé par l'Action juste après la publication
  (secrets GitHub `TGVMAX_API_URL` et `RELOAD_TOKEN`). Le nouveau réseau est compilé et vérifié à côté de l'ancien,
  puis échangé atomiquement ; un fichier invalide ou plus ancien est ignoré et l'API garde le réseau actuel.
  Route désactivée si `RELOAD_TOKEN` est vide.
- Pas de vérification périodique par défaut (`NETWORK_REFRESH=30m` par exemple pour en activer une).
- `GET /health` → `network` : source (`url` ou `file:…`), sha256, `loaded_at`, `last_check`, `last_error`.

En local, sans `NETWORK_URL`, l'API lit simplement le fichier comme avant.

### Choix de modélisation

- **La disponibilité TGVmax dépend du couple origine→destination**, pas du train. Chaque couple « OUI »
  devient un trajet où l'on ne peut monter qu'à l'origine et descendre qu'à la destination : le moteur ne
  propose donc que des billets réellement réservables (et les correspondances entre eux).
- Les arrêts intermédiaires sont reconstitués à partir de tous les couples du train (OUI et NON), pour
  la liste des arrêts et le tracé sur la carte.
- **Changement de siège** : deux billets TGVmax successifs dans le **même train** (ex. Besançon → Lyon puis
  Lyon → Montpellier dans le 5521, quand Besançon → Montpellier est complet). Les correspondances classiques
  gardent leurs 10 min (15 dans les grandes gares) ; rester dans le même train n'en demande aucune (l'arrêt dure
  2 à 5 min). Réglage : `SEAT_CHANGE_MAX` dans `build_network.py` (attente maximale, garde-fou « même passage
  du train »), écrit dans `seat_change_max` du réseau ; 0 = désactivé, valeur absente = réseau Europe. L'API renvoie
  alors une correspondance `transfer_kind: "seat_change"` et `seat_changes` sur le trajet.
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

Les fichiers `*.go` (hors test) viennent de `Europe/`. Seuls ajouts : le changement de siège (désactivé tant que
le réseau ne contient pas `seat_change_max`, donc recopiable tel quel dans `Europe/`) et le port local par défaut 8002.

Pour tout lancer en local (Europe + TGVmax + front) : `python ../run_local.py`.

## En local

```bash
pip install -r requirements.txt
python sncf_to_gtfs.py        # --refresh pour forcer le téléchargement
python build_network.py
go test ./...
go run .                      # http://localhost:8002 (Europe : 8000)
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
