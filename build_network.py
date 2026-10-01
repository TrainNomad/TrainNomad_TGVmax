"""
Étape 2/2 du pipeline TGVmax : GTFS (tgvmax_gtfs.zip) -> network.bin + network.bin.gz.

Version mono-flux de gtfs/build_network.py (réseau Europe) : même format binaire (TNNET001, version 1),
mêmes règles de correspondance, même regroupement des gares par ville. Le moteur Go (fichiers *.go
de ce dossier, copiés de Europe/) charge le fichier tel quel en RAM.

Différences avec le réseau Europe :
  - un seul flux, produit par sncf_to_gtfs.py ;
  - la fenêtre de jours s'adapte aux données (≈ 30 jours au lieu de 128) pour que /health annonce
    la vraie période couverte ;
  - les gares sont rapprochées du référentiel par leur code SNCF (stop_id = FRPLY, ...).

Usage : python build_network.py [--input tgvmax_gtfs.zip]
"""
import argparse
import gzip
import json
import logging
import math
import os
import shutil
import struct
import sys
import zipfile
from collections import defaultdict
from datetime import date, datetime, timedelta, timezone

import numpy as np
import pandas as pd

from stations import StationRef

if hasattr(sys.stderr, "reconfigure"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")  # emojis dans la console Windows
logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
IN_ZIP = os.path.join(BASE_DIR, "tgvmax_gtfs.zip")
OUT_BIN = os.path.join(BASE_DIR, "network.bin")
OUT_GZ = OUT_BIN + ".gz"
REPORT_PATH = os.path.join(BASE_DIR, "build_report.json")

FORMAT_VERSION = 1
DAYS_BEFORE = 1             # la fenêtre commence la veille (trains de nuit en cours)
DAYS_AFTER_LAST = 3         # le moteur regarde jusqu'au surlendemain de la date demandée

MIN_CHANGE = 10             # minutes de correspondance dans la même gare
HUB_CHANGE = 15             # ... dans les grandes gares
HUB_DAILY_DEPARTURES = 150  # seuil "grande gare" (départs moyens par jour)
WALK_MAX_KM = 1.0           # correspondance à pied entre deux gares proches
CITY_MAX_KM = 20.0          # correspondance en transport urbain dans la même ville
CITY_TRANSFER_MAX = 90      # plafond (minutes) d'une traversée de ville
# Changement de siège : enchaîner deux billets TGVmax dans le MÊME train (même numéro) sans les
# 10 min de correspondance. La valeur est l'attente maximale en gare (garde-fou : c'est bien le même
# passage du train) ; en pratique l'arrêt dure 2 à 5 min. 0 = désactivé.
SEAT_CHANGE_MAX = 30

NO_PICKUP = 1
NO_DROPOFF = 2

OPERATORS = [{"id": "SNCF", "name": "SNCF"}]

# route_short_name du GTFS TGVmax -> type affiché (mêmes libellés que le réseau Europe)
TYPES = {
    "TGV INOUI": "SNCF TGV INOUI",
    "Lyria": "SNCF TGV Lyria",
    "INTERCITES": "SNCF Intercités",
    "INTERCITES de nuit": "SNCF Intercités de nuit",
    "Car": "SNCF Car",
}

# garde-fous : un réseau anormalement petit ne doit pas être publié
MIN_STOPS = 100
MIN_TRIPS = 1000


# ---------------------------------------------------------------------------
# Utilitaires
# ---------------------------------------------------------------------------

def haversine_km(lat1, lon1, lat2, lon2) -> float:
    p1, p2 = math.radians(lat1), math.radians(lat2)
    dp, dl = p2 - p1, math.radians(lon2 - lon1)
    a = math.sin(dp / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(dl / 2) ** 2
    return 6371.0 * 2 * math.atan2(math.sqrt(a), math.sqrt(1 - a))


def to_float(v):
    try:
        f = float(v)
        return None if math.isnan(f) else f
    except (TypeError, ValueError):
        return None


def read_gtfs(z: zipfile.ZipFile, name: str):
    if name not in z.namelist():
        return None
    df = pd.read_csv(z.open(name), dtype=str, keep_default_na=False, encoding="utf-8-sig")
    df.columns = [c.strip().lower() for c in df.columns]
    for c in df.columns:
        df[c] = df[c].str.strip()
    return df


def parse_minutes(series: pd.Series) -> np.ndarray:
    """'8:30:00' / '25:10:00' -> minutes depuis le début du jour de service."""
    parts = series.replace("", "0:00:00").str.split(":", n=2, expand=True)
    return (parts[0].astype(int) * 60 + parts[1].astype(int)).to_numpy(np.int32)


# ---------------------------------------------------------------------------
# Construction
# ---------------------------------------------------------------------------

class NetworkBuilder:
    def __init__(self, ref: StationRef, first: date, last: date):
        self.ref = ref
        self.base_date = min(first, datetime.now(timezone.utc).date()) - timedelta(days=DAYS_BEFORE)
        self.ndays = (last - self.base_date).days + DAYS_AFTER_LAST
        self.words = (self.ndays + 63) // 64
        self.day_dates = [self.base_date + timedelta(days=i) for i in range(self.ndays)]
        self.day_index = {d.strftime("%Y%m%d"): i for i, d in enumerate(self.day_dates)}

        self.stops = []
        self.stop_key_index = {}
        self.timezones = []
        self.types = []
        self.trips = {}
        self.stats = defaultdict(int)

    def intern(self, lst, value):
        try:
            return lst.index(value)
        except ValueError:
            lst.append(value)
            return len(lst) - 1

    # -- gares -------------------------------------------------------------

    def stop_for(self, raw, agency_tz):
        rid = self.ref.match(raw["stop_id"], raw.get("stop_code", ""))
        key = "ref:" + rid if rid else "TGVMAX:" + raw["stop_id"]
        self.stats["stops_matched_ref" if rid else "stops_unmatched"] += 1
        if key in self.stop_key_index:
            return self.stop_key_index[key]
        if rid:
            row = self.ref.rows[rid]
            city_rid = self.ref.city_of(rid)
            city = self.ref.rows[city_rid]
            s = {
                "id": row["uic"] or f"TL{rid}", "name": row["name"],
                "lat": row["lat"] if row["lat"] is not None else to_float(raw.get("stop_lat")),
                "lon": row["lon"] if row["lon"] is not None else to_float(raw.get("stop_lon")),
                "tz": row["tz"] or raw.get("stop_timezone") or agency_tz, "country": row["country"],
                "city_key": "ref:" + city_rid, "city_name": city["name"], "city_country": city["country"],
                "city_lat": city["lat"], "city_lon": city["lon"],
            }
        else:
            name = raw.get("stop_name") or raw["stop_id"]
            s = {
                "id": key, "name": name,
                "lat": to_float(raw.get("stop_lat")), "lon": to_float(raw.get("stop_lon")),
                "tz": raw.get("stop_timezone") or agency_tz, "country": "FR",
                "city_key": key, "city_name": name, "city_country": "FR", "city_lat": None, "city_lon": None,
            }
        idx = len(self.stops)
        self.stops.append(s)
        self.stop_key_index[key] = idx
        return idx

    # -- calendriers ---------------------------------------------------------

    def service_bits(self, z):
        bits = defaultdict(int)
        cd = read_gtfs(z, "calendar_dates.txt")
        for sid, d, ex in zip(cd["service_id"], cd["date"], cd["exception_type"]):
            i = self.day_index.get(d)
            if i is None:
                continue
            if ex == "1":
                bits[sid] |= 1 << i
            elif ex == "2":
                bits[sid] &= ~(1 << i)
        return bits

    # -- horaires ------------------------------------------------------------

    def load_feed(self, path):
        z = zipfile.ZipFile(path)
        agency_tz = read_gtfs(z, "agency.txt")["agency_timezone"].iloc[0]
        tz_idx = self.intern(self.timezones, agency_tz)

        raw_stops = {r["stop_id"]: r for r in read_gtfs(z, "stops.txt").to_dict("records")}
        route_info = {r["route_id"]: r for r in read_gtfs(z, "routes.txt").to_dict("records")}
        trip_meta = {r["trip_id"]: r for r in read_gtfs(z, "trips.txt").to_dict("records")}
        bits = self.service_bits(z)

        st = read_gtfs(z, "stop_times.txt")
        st["seq"] = st["stop_sequence"].astype(int)
        st = st.sort_values(["trip_id", "seq"], kind="stable")
        arr_all = parse_minutes(st["arrival_time"].where(st["arrival_time"] != "", st["departure_time"]))
        dep_all = parse_minutes(st["departure_time"].where(st["departure_time"] != "", st["arrival_time"]))
        no_pick = (st["pickup_type"] == "1").to_numpy()
        no_drop = (st["drop_off_type"] == "1").to_numpy()
        stop_ids = st["stop_id"].to_numpy()
        trip_ids = st["trip_id"].to_numpy()

        canon = {sid: self.stop_for(raw_stops.get(sid, {"stop_id": sid}), agency_tz) for sid in pd.unique(stop_ids)}

        bounds = np.flatnonzero(trip_ids[1:] != trip_ids[:-1]) + 1
        starts = np.concatenate([[0], bounds])
        ends = np.concatenate([bounds, [len(trip_ids)]])
        kept = 0
        for a, b in zip(starts, ends):
            meta = trip_meta.get(trip_ids[a])
            if meta is None:
                continue
            days = bits.get(meta["service_id"], 0)
            if not days:
                self.stats["trips_out_of_window"] += 1
                continue

            seq_stops, seq_arr, seq_dep, seq_flags = [], [], [], []
            for k in range(a, b):
                s = canon[stop_ids[k]]
                f = (NO_PICKUP if no_pick[k] else 0) | (NO_DROPOFF if no_drop[k] else 0)
                if seq_stops and seq_stops[-1] == s:
                    seq_dep[-1] = int(dep_all[k])
                    seq_flags[-1] &= f
                    continue
                seq_stops.append(s)
                seq_arr.append(int(arr_all[k]))
                seq_dep.append(int(dep_all[k]))
                seq_flags.append(f)
            if len(seq_stops) < 2:
                continue
            for i in range(len(seq_stops)):
                if i and seq_arr[i] < seq_dep[i - 1]:
                    seq_arr[i] = seq_dep[i - 1]
                if seq_dep[i] < seq_arr[i]:
                    seq_dep[i] = seq_arr[i]
            seq_flags[0] |= NO_DROPOFF
            seq_flags[-1] |= NO_PICKUP
            if all(f & NO_PICKUP for f in seq_flags[:-1]):
                continue

            rs = route_info.get(meta["route_id"], {}).get("route_short_name", "")
            ttype = TYPES.get(rs, f"SNCF {rs}" if rs else "SNCF Train")
            number = meta.get("trip_short_name", "")
            type_idx = self.intern(self.types, ttype)
            key = (tuple(seq_stops), tuple(seq_flags), tuple(seq_arr), tuple(seq_dep), number, type_idx)
            t = self.trips.get(key)
            if t is None:
                self.trips[key] = {
                    "op": 0, "tz": tz_idx, "stops": seq_stops, "flags": seq_flags,
                    "arr": seq_arr, "dep": seq_dep, "number": number, "type": type_idx,
                    "checkin": 0, "days": days,
                }
            else:
                t["days"] |= days
                self.stats["trips_merged"] += 1
            kept += 1
        self.stats["trips_TGVMAX"] = kept
        logging.info(f"  [TGVMAX] {kept} trajets retenus")

    # -- routes RAPTOR -------------------------------------------------------

    def build_routes(self):
        """Regroupe les trajets par (suite d'arrêts, règles montée/descente, fuseau) puis découpe
        chaque groupe pour qu'aucun trajet n'en dépasse un autre (condition RAPTOR)."""
        groups = defaultdict(list)
        for t in self.trips.values():
            groups[(t["tz"], t["checkin"], tuple(t["stops"]), tuple(t["flags"]))].append(t)
        routes = []
        for (tz, checkin, stops, flags), trips in groups.items():
            trips.sort(key=lambda t: (t["dep"][0], t["arr"][-1]))
            subs = []
            for t in trips:
                for sub in subs:
                    last = sub[-1]
                    if all(t["dep"][i] >= last["dep"][i] and t["arr"][i] >= last["arr"][i] for i in range(len(stops))):
                        sub.append(t)
                        break
                else:
                    subs.append([t])
            for sub in subs:
                routes.append({"tz": tz, "checkin": checkin, "stops": list(stops), "flags": list(flags), "trips": sub})
        self.stats["routes"] = len(routes)
        self.stats["trips"] = len(self.trips)
        return routes

    # -- correspondances -----------------------------------------------------

    def build_footpaths(self, used):
        n = len(self.stops)
        paths = defaultdict(dict)

        def add(a, b, minutes):
            if a != b and minutes < paths[a].get(b, 10**9):
                paths[a][b] = minutes
                paths[b][a] = minutes

        grid = defaultdict(list)
        for i in range(n):
            s = self.stops[i]
            if used[i] and s["lat"] is not None:
                grid[(int(s["lat"] / 0.02), int(s["lon"] / 0.02))].append(i)
        for i in range(n):
            s = self.stops[i]
            if not used[i] or s["lat"] is None:
                continue
            gy, gx = int(s["lat"] / 0.02), int(s["lon"] / 0.02)
            for dy in (-1, 0, 1):
                for dx in (-1, 0, 1):
                    for j in grid.get((gy + dy, gx + dx), ()):
                        if j <= i:
                            continue
                        o = self.stops[j]
                        d = haversine_km(s["lat"], s["lon"], o["lat"], o["lon"])
                        if d <= WALK_MAX_KM:
                            add(i, j, math.ceil(5 + d * 1.25 / 4.5 * 60))

        by_city = defaultdict(list)
        for i in range(n):
            if used[i]:
                by_city[self.stops[i]["city_key"]].append(i)
        for members in by_city.values():
            for x in range(len(members)):
                for y in range(x + 1, len(members)):
                    a, b = self.stops[members[x]], self.stops[members[y]]
                    if a["lat"] is None or b["lat"] is None:
                        continue
                    d = haversine_km(a["lat"], a["lon"], b["lat"], b["lon"])
                    if d <= CITY_MAX_KM:
                        add(members[x], members[y], min(CITY_TRANSFER_MAX, math.ceil(25 + 5 * d)))
        return paths

    # -- export --------------------------------------------------------------

    def build(self):
        routes = self.build_routes()

        used = [False] * len(self.stops)
        weight = [0.0] * len(self.stops)
        for r in routes:
            for t in r["trips"]:
                active = bin(t["days"]).count("1") / self.ndays
                for s, f in zip(r["stops"], r["flags"]):
                    used[s] = True
                    if not f & NO_PICKUP:
                        weight[s] += active
        remap = {}
        for i, u in enumerate(used):
            if u:
                remap[i] = len(remap)
        old = list(remap.keys())
        n = len(old)

        footpaths = self.build_footpaths(used)

        city_keys, city_index = [], {}
        stop_city = np.zeros(n, np.int32)
        for new, oi in enumerate(old):
            ck = self.stops[oi]["city_key"]
            if ck not in city_index:
                city_index[ck] = len(city_keys)
                city_keys.append(oi)
            stop_city[new] = city_index[ck]
        city_members = defaultdict(list)
        for new in range(n):
            city_members[stop_city[new]].append(new)

        cities = []
        for ci in range(len(city_keys)):
            s0 = self.stops[city_keys[ci]]
            lat, lon = s0["city_lat"], s0["city_lon"]
            if lat is None or lon is None:
                pts = [(self.stops[old[m]]["lat"], self.stops[old[m]]["lon"]) for m in city_members[ci]
                       if self.stops[old[m]]["lat"] is not None]
                lat = sum(p[0] for p in pts) / len(pts) if pts else 0.0
                lon = sum(p[1] for p in pts) / len(pts) if pts else 0.0
            ck = s0["city_key"]
            cities.append({
                "id": ("TL" + ck[4:]) if ck.startswith("ref:") else ck,
                "name": s0["city_name"], "country": s0["city_country"] or s0["country"], "lat": lat, "lon": lon,
            })

        words = self.words
        day_sets, day_set_index = [], {}
        for t in self.trips.values():
            if t["days"] not in day_set_index:
                day_set_index[t["days"]] = len(day_sets)
                day_sets.append(t["days"])
        day_bits = np.zeros(len(day_sets) * words, np.uint64)
        for i, v in enumerate(day_sets):
            for w in range(words):
                day_bits[i * words + w] = (v >> (64 * w)) & 0xFFFFFFFFFFFFFFFF

        route_stop_off, route_trip_off, route_time_off = [0], [0], [0]
        route_stops, route_flags, route_tz, route_checkin = [], [], [], []
        t_arr, t_dep, trip_days, trip_number, trip_type, trip_op = [], [], [], [], [], []
        for r in routes:
            route_stops.extend(remap[s] for s in r["stops"])
            route_flags.extend(r["flags"])
            route_stop_off.append(len(route_stops))
            route_tz.append(r["tz"])
            route_checkin.append(r["checkin"])
            for t in r["trips"]:
                t_arr.extend(t["arr"])
                t_dep.extend(t["dep"])
                trip_days.append(day_set_index[t["days"]])
                trip_number.append(t["number"])
                trip_type.append(t["type"])
                trip_op.append(t["op"])
            route_trip_off.append(len(trip_days))
            route_time_off.append(len(t_arr))
        if max(t_dep + t_arr) >= 65535:
            raise ValueError("horaire hors limite uint16")

        fp_off, fp_to, fp_min = [0], [], []
        for oi in old:
            for j, m in sorted(footpaths.get(oi, {}).items()):
                fp_to.append(remap[j])
                fp_min.append(m)
            fp_off.append(len(fp_to))

        change = [HUB_CHANGE if weight[oi] >= HUB_DAILY_DEPARTURES else MIN_CHANGE for oi in old]

        stop_tz = np.array([self.intern(self.timezones, self.stops[o]["tz"]) for o in old], np.uint8)
        self.stats.update({
            "stops": n, "cities": len(cities), "footpaths": len(fp_to), "stop_times": len(t_arr),
            "day_sets": len(day_sets),
        })
        meta = {
            "version": FORMAT_VERSION,
            "built_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
            "base_date": self.base_date.isoformat(),
            "ndays": self.ndays, "words": words,
            "timezones": self.timezones, "types": self.types,
            "operators": OPERATORS,
            "seat_change_max": SEAT_CHANGE_MAX,
            "stats": {k: int(v) for k, v in self.stats.items()},
        }

        w = BinWriter()
        w.add_json("meta", meta)
        w.add_strings("stop.id", [self.stops[o]["id"] for o in old])
        w.add_strings("stop.name", [self.stops[o]["name"] for o in old])
        w.add_strings("stop.country", [self.stops[o]["country"] for o in old])
        w.add("stop.lat", np.array([self.stops[o]["lat"] or 0.0 for o in old], np.float32))
        w.add("stop.lon", np.array([self.stops[o]["lon"] or 0.0 for o in old], np.float32))
        w.add("stop.tz", stop_tz)
        w.add("stop.city", stop_city)
        w.add("stop.change", np.array(change, np.uint16))
        w.add("stop.weight", np.array([weight[o] for o in old], np.float32))

        w.add_strings("city.id", [c["id"] for c in cities])
        w.add_strings("city.name", [c["name"] for c in cities])
        w.add_strings("city.country", [c["country"] for c in cities])
        w.add("city.lat", np.array([c["lat"] for c in cities], np.float32))
        w.add("city.lon", np.array([c["lon"] for c in cities], np.float32))

        w.add("fp.off", np.array(fp_off, np.uint32))
        w.add("fp.to", np.array(fp_to, np.int32))
        w.add("fp.min", np.array(fp_min, np.uint16))

        w.add("route.stopoff", np.array(route_stop_off, np.uint32))
        w.add("route.stops", np.array(route_stops, np.int32))
        w.add("route.flags", np.array(route_flags, np.uint8))
        w.add("route.tripoff", np.array(route_trip_off, np.uint32))
        w.add("route.timeoff", np.array(route_time_off, np.uint32))
        w.add("route.tz", np.array(route_tz, np.uint8))
        w.add("route.checkin", np.array(route_checkin, np.uint16))

        w.add("time.arr", np.array(t_arr, np.uint16))
        w.add("time.dep", np.array(t_dep, np.uint16))

        w.add("trip.days", np.array(trip_days, np.uint32))
        w.add("trip.type", np.array(trip_type, np.uint16))
        w.add("trip.op", np.array(trip_op, np.uint8))
        w.add_strings("trip.number", trip_number)
        w.add("days.bits", day_bits)
        return w


# ---------------------------------------------------------------------------
# Format binaire (identique à gtfs/build_network.py)
#   en-tête : "TNNET001" | u32 nb_sections | u32 réservé
#   section : nom (24 o, UTF-8 complété par \0) | u8 type | 7 o réservés | u64 nb éléments | u64 nb octets
#             puis les données, complétées à un multiple de 8 octets. Tout est little-endian.
#   types   : 1=u8 2=u16 3=u32 4=i32 5=u64 6=f32
#   chaînes : deux sections "<nom>.off" (u32, n+1 offsets) et "<nom>.dat" (u8, UTF-8 concaténé)
# ---------------------------------------------------------------------------

DTYPES = {np.dtype("uint8"): 1, np.dtype("uint16"): 2, np.dtype("uint32"): 3,
          np.dtype("int32"): 4, np.dtype("uint64"): 5, np.dtype("float32"): 6}


class BinWriter:
    def __init__(self):
        self.sections = []

    def add(self, name, arr: np.ndarray):
        assert len(name) <= 24, name
        arr = np.ascontiguousarray(arr)
        self.sections.append((name, DTYPES[arr.dtype], arr.astype(arr.dtype.newbyteorder("<"), copy=False)))

    def add_json(self, name, obj):
        self.add(name, np.frombuffer(json.dumps(obj, ensure_ascii=False).encode("utf-8"), np.uint8))

    def add_strings(self, name, values):
        data = [v.encode("utf-8") for v in values]
        off = np.zeros(len(data) + 1, np.uint32)
        off[1:] = np.cumsum([len(d) for d in data]) if data else []
        self.add(name + ".off", off)
        self.add(name + ".dat", np.frombuffer(b"".join(data), np.uint8))

    def write(self, path):
        with open(path, "wb") as f:
            f.write(b"TNNET001")
            f.write(struct.pack("<II", len(self.sections), 0))
            for name, code, arr in self.sections:
                raw = arr.tobytes()
                f.write(name.encode("utf-8").ljust(24, b"\0"))
                f.write(struct.pack("<B7xQQ", code, arr.size, len(raw)))
                f.write(raw)
                f.write(b"\0" * (-len(raw) % 8))


def feed_dates(path):
    z = zipfile.ZipFile(path)
    dates = read_gtfs(z, "calendar_dates.txt")["date"]
    return datetime.strptime(dates.min(), "%Y%m%d").date(), datetime.strptime(dates.max(), "%Y%m%d").date()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", default=IN_ZIP, help="GTFS produit par sncf_to_gtfs.py")
    args = parser.parse_args()
    if not os.path.exists(args.input):
        sys.exit(f"❌ {args.input} introuvable : lancer d'abord python sncf_to_gtfs.py")

    first, last = feed_dates(args.input)
    builder = NetworkBuilder(StationRef(), first, last)
    logging.info(f"📅 Fenêtre : {builder.day_dates[0]} → {builder.day_dates[-1]} ({builder.ndays} jours)")
    builder.load_feed(args.input)

    writer = builder.build()
    if builder.stats["stops"] < MIN_STOPS or builder.stats["trips"] < MIN_TRIPS:
        sys.exit(f"❌ Réseau anormalement petit ({builder.stats['stops']} gares, {builder.stats['trips']} trajets), abandon")

    # écriture atomique : le fichier publié n'est jamais à moitié écrit
    writer.write(OUT_BIN + ".part")
    os.replace(OUT_BIN + ".part", OUT_BIN)
    with open(OUT_BIN, "rb") as fi, gzip.open(OUT_GZ + ".part", "wb", compresslevel=9) as fo:
        shutil.copyfileobj(fi, fo)
    os.replace(OUT_GZ + ".part", OUT_GZ)
    with open(REPORT_PATH, "w", encoding="utf-8") as f:
        json.dump({"first_date": first.isoformat(), "last_date": last.isoformat(), **builder.stats},
                  f, indent=2, ensure_ascii=False)

    logging.info(f"📊 {json.dumps(dict(builder.stats), ensure_ascii=False)}")
    logging.info(f"✅ {OUT_BIN} : {os.path.getsize(OUT_BIN) / 1e6:.2f} Mo | .gz : {os.path.getsize(OUT_GZ) / 1e6:.2f} Mo")


if __name__ == "__main__":
    main()
