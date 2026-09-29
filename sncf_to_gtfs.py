"""
Étape 1/2 du pipeline TGVmax : données SNCF Open Data -> GTFS (tgvmax_gtfs.zip).

Le jeu de données « tgvmax » liste, pour chaque train et chaque date (≈ 30 jours glissants), tous les
couples origine -> destination vendus, avec `od_happy_card` = OUI si une place TGVmax est disponible.

Modélisation GTFS :
  - Un trajet GTFS par couple origine -> destination disponible (OUI). La disponibilité TGVmax
    dépend du couple, pas du train : Paris -> Marseille peut être OUI et Paris -> Lyon NON.
    Seule l'origine autorise la montée et seule la destination la descente (pickup/drop_off_type),
    le moteur ne peut donc proposer que des couples réellement réservables.
  - Les arrêts intermédiaires sont reconstitués à partir de tous les couples du train (OUI et NON)
    pour l'affichage (liste des arrêts, tracé sur la carte) : on y passe sans monter ni descendre.
  - `date` est le jour de départ du train : les heures après minuit sont écrites 24:xx:xx, comme
    le veut GTFS (ex. Intercités de nuit Rodez -> Paris, Gramat à 00:12).
  - Les trajets identiques sur plusieurs dates partagent un service (calendar_dates.txt).

Usage : python sncf_to_gtfs.py [--refresh] [--input fichier.csv] [--output tgvmax_gtfs.zip]
"""
import argparse
import csv
import io
import logging
import os
import subprocess
import sys
import time
import zipfile
from collections import defaultdict
from datetime import datetime, timezone

import requests

from stations import StationRef

if hasattr(sys.stderr, "reconfigure"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")  # emojis dans la console Windows
logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
CACHE_DIR = os.path.join(BASE_DIR, "cache")
RAW_CSV = os.path.join(CACHE_DIR, "tgvmax.csv")
OUT_ZIP = os.path.join(BASE_DIR, "tgvmax_gtfs.zip")

# Export CSV plutôt que JSON : 3 fois plus léger (≈ 35 Mo pour 350 000 lignes) et lu en flux.
SNCF_EXPORT_URL = "https://ressources.data.sncf.com/api/explore/v2.1/catalog/datasets/tgvmax/exports/csv"
SNCF_PARAMS = {"delimiter": ";", "timezone": "Europe/Paris", "lang": "fr"}
EXPECTED_COLUMNS = {"date", "train_no", "entity", "axe", "origine_iata", "destination_iata",
                    "origine", "destination", "heure_depart", "heure_arrivee", "od_happy_card"}
FEED_MAX_AGE_H = 3      # réutilise un export téléchargé depuis moins de 3 h
MIN_ROWS = 10_000       # garde-fou : un export anormalement petit ne doit pas écraser le réseau publié
MIN_AVAILABLE = 1_000

# Type de train (route GTFS) déduit de l'axe commercial et de l'entité.
ROUTES = {
    "TGV_INOUI": {"short": "TGV INOUI", "type": "2", "color": "9B2743"},
    "LYRIA": {"short": "Lyria", "type": "2", "color": "C8102E"},
    "INTERCITES": {"short": "INTERCITES", "type": "2", "color": "0C2340"},
    "INTERCITES_NUIT": {"short": "INTERCITES de nuit", "type": "2", "color": "1B1F3B"},
    "CAR": {"short": "Car", "type": "3", "color": "6E6E6E"},
}
LYRIA_ENTITIES = {"GENEVEPA", "PAGENEVE", "GENEVEMED", "MEDGENEVE", "PASUIS", "SUISPA", "PABALZUR", "ZURBALPA"}


def route_for(axe: str, entity: str) -> str:
    axe, entity = axe.upper(), entity.upper()
    if axe == "AUTOCAR SNCF" or entity.startswith("AUTOCAR"):
        return "CAR"
    if axe == "IC NUIT":
        return "INTERCITES_NUIT"
    if axe.startswith("IC "):
        return "INTERCITES"
    if entity in LYRIA_ENTITIES:
        return "LYRIA"
    return "TGV_INOUI"


def hhmm(v: str) -> int:
    h, m = v.strip().split(":")[:2]
    return int(h) * 60 + int(m)


def gtfs_time(minutes: int) -> str:
    return f"{minutes // 60:02d}:{minutes % 60:02d}:00"


# ---------------------------------------------------------------------------
# Téléchargement
# ---------------------------------------------------------------------------

def fetch_export(refresh: bool) -> str:
    os.makedirs(CACHE_DIR, exist_ok=True)
    if not refresh and os.path.exists(RAW_CSV) and time.time() - os.path.getmtime(RAW_CSV) < FEED_MAX_AGE_H * 3600:
        logging.info(f"📦 Export TGVmax en cache ({os.path.getsize(RAW_CSV) / 1e6:.1f} Mo)")
        return RAW_CSV
    tmp = RAW_CSV + ".part"
    for attempt in range(1, 4):
        try:
            logging.info(f"⬇️  Téléchargement des données TGVmax (essai {attempt}/3)")
            with requests.get(SNCF_EXPORT_URL, params=SNCF_PARAMS, stream=True, timeout=(30, 300)) as r:
                r.raise_for_status()
                with open(tmp, "wb") as f:
                    for chunk in r.iter_content(1 << 20):
                        f.write(chunk)
            os.replace(tmp, RAW_CSV)
            logging.info(f"   {os.path.getsize(RAW_CSV) / 1e6:.1f} Mo")
            return RAW_CSV
        except requests.exceptions.SSLError:
            # Certains postes (Windows) n'ont pas l'autorité de certification attendue par requests :
            # curl utilise le magasin de certificats du système.
            logging.warning("   erreur SSL avec requests, nouvel essai avec curl")
            url = requests.Request("GET", SNCF_EXPORT_URL, params=SNCF_PARAMS).prepare().url
            try:
                subprocess.run(["curl", "-sSfL", "--retry", "2", "-o", tmp, url], check=True, timeout=600)
                os.replace(tmp, RAW_CSV)
                logging.info(f"   {os.path.getsize(RAW_CSV) / 1e6:.1f} Mo")
                return RAW_CSV
            except (subprocess.SubprocessError, OSError) as e:
                logging.warning(f"   échec curl : {e}")
        except requests.RequestException as e:
            logging.warning(f"   échec : {e}")
            time.sleep(10 * attempt)
    sys.exit("❌ Impossible de télécharger les données TGVmax")


def read_rows(path: str) -> list[dict]:
    with open(path, encoding="utf-8-sig", newline="") as f:
        reader = csv.DictReader(f, delimiter=";")
        missing = EXPECTED_COLUMNS - set(reader.fieldnames or [])
        if missing:
            sys.exit(f"❌ Colonnes absentes de l'export SNCF : {sorted(missing)}")
        rows = [r for r in reader if r["origine_iata"] and r["destination_iata"] and r["heure_depart"] and r["heure_arrivee"]]
    if len(rows) < MIN_ROWS:
        sys.exit(f"❌ Export anormalement petit ({len(rows)} lignes < {MIN_ROWS}), abandon")
    return rows


# ---------------------------------------------------------------------------
# Conversion
# ---------------------------------------------------------------------------

class Converter:
    def __init__(self, ref: StationRef):
        self.ref = ref
        self.stop_names = {}                # code SNCF -> nom brut de l'export (secours)
        self.trips = {}                     # clé -> {"route", "number", "stops", "times", "dates"}
        self.stats = defaultdict(int)

    def convert(self, rows: list[dict]):
        trains = defaultdict(list)
        for r in rows:
            trains[(r["date"], r["train_no"].strip())].append(r)
            self.stop_names.setdefault(r["origine_iata"], r["origine"])
            self.stop_names.setdefault(r["destination_iata"], r["destination"])
        self.stats["rows"] = len(rows)
        self.stats["trains"] = len(trains)
        for (d, number), rs in trains.items():
            self.convert_train(d.replace("-", ""), number, rs)
        self.stats["gtfs_trips"] = len(self.trips)

    def convert_train(self, service_date: str, number: str, rs: list[dict]):
        # Heures de départ relatives au jour du train : si elles s'étalent sur plus de 12 h, le train
        # a franchi minuit et les départs du matin appartiennent au lendemain.
        deps = [hhmm(r["heure_depart"]) for r in rs]
        wrap = max(deps) - min(deps) > 12 * 60

        def dep_of(r):
            m = hhmm(r["heure_depart"])
            return m + 1440 if wrap and m < 720 else m

        dep_at, arr_at = {}, {}
        ods = {}  # (origine, destination) -> [dép, arr, disponible]
        for r in rs:
            o, dst = r["origine_iata"], r["destination_iata"]
            if o == dst:
                continue
            dep = dep_of(r)
            arr = hhmm(r["heure_arrivee"])
            while arr < dep:
                arr += 1440
            dep_at.setdefault(o, dep)
            arr_at.setdefault(dst, arr)
            od = ods.setdefault((o, dst), [dep, arr, False])
            # une même relation peut apparaître en double (tranches du train) : OUI si l'une l'est
            od[2] = od[2] or r["od_happy_card"].strip().upper() == "OUI"
        route = route_for(rs[0]["axe"], rs[0]["entity"])

        for (o, dst), (dep, arr, available) in ods.items():
            self.stats["od_available" if available else "od_full"] += 1
            if not available:
                continue
            # arrêts intermédiaires : gares du même train desservies strictement entre les deux
            mids = []
            for s in set(dep_at) | set(arr_at):
                if s in (o, dst):
                    continue
                a = arr_at.get(s, dep_at.get(s))
                b = dep_at.get(s, a)
                if dep < a and b < arr:
                    mids.append((a, max(a, b), s))
            mids.sort()
            stops = [o] + [m[2] for m in mids] + [dst]
            times = [(dep, dep)] + [(m[0], m[1]) for m in mids] + [(arr, arr)]
            key = (route, number, tuple(stops), tuple(times))
            t = self.trips.get(key)
            if t is None:
                self.trips[key] = {"route": route, "number": number, "stops": stops, "times": times,
                                   "dates": {service_date}}
            else:
                t["dates"].add(service_date)

    # -- écriture ------------------------------------------------------------

    def write_zip(self, path: str):
        used = sorted({s for t in self.trips.values() for s in t["stops"]})
        stops_rows, unmatched = [], []
        for code in used:
            info = self.ref.describe(code, self.stop_names.get(code, code))
            if not info["matched"]:
                unmatched.append(code)
            stops_rows.append({
                "stop_id": code, "stop_code": info["uic"], "stop_name": info["name"],
                "stop_lat": f"{info['lat']:.6f}" if info["lat"] is not None else "",
                "stop_lon": f"{info['lon']:.6f}" if info["lon"] is not None else "",
                "location_type": "0", "stop_timezone": info["tz"],
            })
        self.stats["stops"] = len(used)
        self.stats["stops_unmatched"] = len(unmatched)
        if unmatched:
            logging.warning(f"   gares absentes du référentiel : {', '.join(unmatched)}")

        # services : un par ensemble de dates
        services, trips_rows, st_rows = {}, [], []
        for i, t in enumerate(sorted(self.trips.values(), key=lambda t: (t["number"], t["times"][0][1], min(t["dates"])))):
            dates = tuple(sorted(t["dates"]))
            sid = services.setdefault(dates, f"S{len(services) + 1}")
            trip_id = f"{t['number']}-{t['stops'][0]}-{t['stops'][-1]}-{i}"
            trips_rows.append({
                "route_id": t["route"], "service_id": sid, "trip_id": trip_id,
                "trip_short_name": t["number"], "trip_headsign": self.ref.describe(t["stops"][-1], "")["name"],
            })
            last = len(t["stops"]) - 1
            for seq, (s, (a, d)) in enumerate(zip(t["stops"], t["times"])):
                st_rows.append({
                    "trip_id": trip_id, "arrival_time": gtfs_time(a), "departure_time": gtfs_time(d),
                    "stop_id": s, "stop_sequence": str(seq),
                    # 1 = interdit : montée uniquement à l'origine, descente uniquement à destination
                    "pickup_type": "0" if seq == 0 else "1",
                    "drop_off_type": "0" if seq == last else "1",
                })
        cal_rows = [{"service_id": sid, "date": d, "exception_type": "1"}
                    for dates, sid in services.items() for d in dates]
        self.stats["services"] = len(services)
        self.stats["stop_times"] = len(st_rows)

        all_dates = sorted({d for dates in services for d in dates})
        tables = {
            "agency.txt": [{"agency_id": "SNCF", "agency_name": "SNCF", "agency_url": "https://www.sncf-connect.com",
                            "agency_timezone": "Europe/Paris", "agency_lang": "fr"}],
            "stops.txt": stops_rows,
            "routes.txt": [{"route_id": rid, "agency_id": "SNCF", "route_short_name": r["short"],
                            "route_long_name": f"{r['short']} (TGVmax)", "route_type": r["type"],
                            "route_color": r["color"], "route_text_color": "FFFFFF"}
                           for rid, r in ROUTES.items() if any(t["route"] == rid for t in self.trips.values())],
            "trips.txt": trips_rows,
            "stop_times.txt": st_rows,
            "calendar_dates.txt": cal_rows,
            "feed_info.txt": [{"feed_publisher_name": "TrainNomad", "feed_publisher_url": "https://ressources.data.sncf.com/explore/dataset/tgvmax/",
                               "feed_lang": "fr", "feed_start_date": all_dates[0], "feed_end_date": all_dates[-1],
                               "feed_version": datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")}],
        }
        tmp = path + ".part"
        with zipfile.ZipFile(tmp, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as z:
            for name, rows in tables.items():
                buf = io.StringIO()
                w = csv.DictWriter(buf, fieldnames=list(rows[0].keys()), lineterminator="\n")
                w.writeheader()
                w.writerows(rows)
                z.writestr(name, buf.getvalue())
                logging.info(f"   {name:<20} {len(rows):>8} lignes")
        os.replace(tmp, path)
        logging.info(f"✅ {path} ({os.path.getsize(path) / 1e6:.2f} Mo), dates {all_dates[0]} → {all_dates[-1]}")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--refresh", action="store_true", help="force le re-téléchargement de l'export SNCF")
    parser.add_argument("--input", help="export CSV local (au lieu du téléchargement)")
    parser.add_argument("--output", default=OUT_ZIP, help="archive GTFS produite")
    args = parser.parse_args()

    rows = read_rows(args.input or fetch_export(args.refresh))
    conv = Converter(StationRef())
    conv.convert(rows)
    if conv.stats["od_available"] < MIN_AVAILABLE:
        sys.exit(f"❌ Seulement {conv.stats['od_available']} relations disponibles (< {MIN_AVAILABLE}), abandon")
    conv.write_zip(args.output)
    logging.info("📊 " + ", ".join(f"{k}={v}" for k, v in conv.stats.items()))


if __name__ == "__main__":
    main()
