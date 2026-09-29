"""
Référentiel des gares (stations.csv, format Trainline enrichi par TrainNomad), partagé par
sncf_to_gtfs.py et build_network.py.

Les données TGVmax identifient les gares par leur code SNCF à 5 lettres (colonne `sncf_id`,
ex. FRPLY = Paris Gare de Lyon). Le référentiel donne le nom propre, l'UIC, les coordonnées, le fuseau
et le rattachement à une ville (Paris = toutes ses gares), exactement comme pour le réseau Europe :
les identifiants `station:<UIC>` et `city:TL<id>` sont donc les mêmes dans les deux API.
"""
import csv
import logging
import os
import time

import requests

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
CACHE_DIR = os.path.join(BASE_DIR, "cache")

# Ordre de recherche : variable d'environnement, dépôt GTFS voisin (local), cache, téléchargement.
LOCAL_CANDIDATES = [
    os.environ.get("STATIONS_CSV", ""),
    os.path.join(BASE_DIR, "..", "gtfs", "stations.csv"),
]
STATIONS_URLS = [
    "https://raw.githubusercontent.com/TrainNomad/TrainNomad_GTFS/main/stations.csv",
    "https://raw.githubusercontent.com/trainline-eu/stations/master/stations.csv",
]
CACHE_MAX_AGE_H = 24 * 7

# Arrêts d'autocar absents du référentiel (coordonnées approximatives, centre de la commune).
EXTRA_STOPS = {
    "FRLPZ": {"name": "Souilly", "lat": 49.0275, "lon": 5.2842},
    "FRSHZ": {"name": "Fresnes-au-Mont", "lat": 48.8853, "lon": 5.4383},
    "FRSIA": {"name": "Sampigny Centre", "lat": 48.7686, "lon": 5.5092},
    "FRSHY": {"name": "Saint-Mihiel Détention", "lat": 48.8903, "lon": 5.5431},
    "FRKGD": {"name": "Commercy Zone du Seugnon", "lat": 48.7547, "lon": 5.5731},
}


def to_float(v):
    try:
        return float(v)
    except (TypeError, ValueError):
        return None


def stations_csv_path() -> str:
    for p in LOCAL_CANDIDATES:
        if p and os.path.exists(p):
            logging.info(f"📖 Référentiel des gares : {os.path.normpath(p)}")
            return p
    os.makedirs(CACHE_DIR, exist_ok=True)
    path = os.path.join(CACHE_DIR, "stations.csv")
    if os.path.exists(path) and time.time() - os.path.getmtime(path) < CACHE_MAX_AGE_H * 3600:
        logging.info(f"📖 Référentiel des gares en cache : {path}")
        return path
    for url in STATIONS_URLS:
        try:
            logging.info(f"⬇️  Téléchargement du référentiel des gares : {url}")
            r = requests.get(url, timeout=120)
            r.raise_for_status()
            with open(path + ".part", "wb") as f:
                f.write(r.content)
            os.replace(path + ".part", path)
            return path
        except requests.RequestException as e:
            logging.warning(f"   échec : {e}")
    if os.path.exists(path):
        logging.warning("   référentiel en cache périmé utilisé")
        return path
    raise RuntimeError("stations.csv introuvable")


class StationRef:
    """Index des gares par code SNCF, avec la logique de dédoublonnage de gtfs/build_network.py."""

    def __init__(self, path: str | None = None):
        path = path or stations_csv_path()
        self.rows, self.by_sncf, self.by_uic = {}, {}, {}
        with open(path, encoding="utf-8", newline="") as f:
            for r in csv.DictReader(f, delimiter=";"):
                rid = r["id"]
                self.rows[rid] = {
                    "id": rid, "name": r["name"], "uic": r.get("uic", ""),
                    "lat": to_float(r.get("latitude")), "lon": to_float(r.get("longitude")),
                    "parent": (r.get("parent_station_id") or "").removesuffix(".0"),
                    "country": (r.get("country") or "")[:2].upper(), "tz": r.get("time_zone", ""),
                    "is_city": r.get("is_city") == "t", "same_as": (r.get("same_as") or "").removesuffix(".0"),
                }
                if r.get("sncf_id") and r["sncf_id"] not in self.by_sncf:
                    self.by_sncf[r["sncf_id"]] = rid
                if r.get("uic") and r["uic"] not in self.by_uic:
                    self.by_uic[r["uic"]] = rid
        logging.info(f"   {len(self.rows)} gares, {len(self.by_sncf)} codes SNCF")

    def canonical(self, rid: str) -> str:
        """Suit same_as puis remonte les parents non-ville : un même complexe = une gare."""
        for _ in range(6):
            row = self.rows[rid]
            if row["same_as"] and row["same_as"] in self.rows and row["same_as"] != rid:
                rid = row["same_as"]
                continue
            p = row["parent"]
            if p and p in self.rows and not self.rows[p]["is_city"] and p != rid:
                rid = p
                continue
            break
        return rid

    def city_of(self, rid: str) -> str:
        """Ancêtre le plus haut ; si ce n'est pas une ville, la gare est sa propre ville."""
        seen = set()
        while rid not in seen:
            seen.add(rid)
            p = self.rows[rid]["parent"]
            if not p or p not in self.rows:
                break
            rid = p
        return rid

    def match(self, sncf_code: str, uic: str = "") -> str | None:
        rid = self.by_sncf.get(sncf_code) or self.by_uic.get(uic)
        return self.canonical(rid) if rid else None

    def describe(self, sncf_code: str, fallback_name: str) -> dict:
        """Nom, UIC, coordonnées et fuseau d'un code SNCF (pour stops.txt)."""
        rid = self.match(sncf_code)
        if rid:
            row = self.rows[rid]
            return {"name": row["name"], "uic": row["uic"], "lat": row["lat"], "lon": row["lon"],
                    "tz": row["tz"] or "Europe/Paris", "matched": True}
        extra = EXTRA_STOPS.get(sncf_code, {})
        name = extra.get("name") or fallback_name.title()
        return {"name": name, "uic": "", "lat": extra.get("lat"), "lon": extra.get("lon"),
                "tz": "Europe/Paris", "matched": False}
