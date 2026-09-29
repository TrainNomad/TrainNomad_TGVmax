#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
SNCF OpenData JSON to GTFS Converter
Convertit les donnees TGVmax/SNCF en format GTFS standard
"""

import json
import csv
import os
import sys
import requests
import zipfile
import warnings
from pathlib import Path
from datetime import datetime
from typing import Dict, List

warnings.filterwarnings('ignore', message='Unverified HTTPS request')

SNCF_API_URL = "https://ressources.data.sncf.com/api/explore/v2.1/catalog/datasets/tgvmax/exports/json"
SNCF_PARAMS = {"lang": "fr", "timezone": "Europe/Paris"}
OUTPUT_DIR = "gtfs_output"

class SNFCtoGTFS:
    """Convertisseur SNCF JSON to GTFS"""

    def __init__(self, output_dir: str = OUTPUT_DIR):
        self.output_dir = Path(output_dir)
        self.output_dir.mkdir(exist_ok=True)
        self.stops = {}
        self.routes = {}
        self.trips = []
        self.stop_times = []
        self.station_cache = {}

    def fetch_data(self) -> List[Dict]:
        """Telecharge les donnees SNCF"""
        print("[*] Telecharger donnees SNCF...")
        params = "&".join([f"{k}={v}" for k, v in SNCF_PARAMS.items()])
        url = f"{SNCF_API_URL}?{params}"

        try:
            response = requests.get(url, timeout=30, verify=False)
            response.raise_for_status()
            data = response.json()

            if isinstance(data, list):
                results = data
            else:
                results = data.get('results', [])

            print(f"[OK] {len(results)} trajets telecharges")
            return results
        except Exception as e:
            print(f"[ERROR] Telecharger: {e}")
            sys.exit(1)

    def parse_trains(self, results: List[Dict]):
        """Parse les trajets JSON en entites GTFS"""
        print("[*] Conversion trains to GTFS...")

        route_id = "ROUTE_TGVMAX"
        if route_id not in self.routes:
            self.routes[route_id] = {
                "id": route_id,
                "name": "TGVmax",
                "type": "2",
                "color": "0066CC",
                "text_color": "FFFFFF"
            }

        for idx, train in enumerate(results):
            # Extraire infos
            train_id = f"TRAIN_{train.get('date', '2026-01-01').replace('-', '')}_{train.get('train_no', idx)}"
            origin = train.get("origine", "").strip()
            dest = train.get("destination", "").strip()
            origin_iata = train.get("origine_iata", "").strip()
            dest_iata = train.get("destination_iata", "").strip()
            dep_time = train.get("heure_depart", "00:00")
            arr_time = train.get("heure_arrivee", "00:00")
            date = train.get("date", "2026-01-01")

            if not (origin and dest):
                continue

            # Creer IDs uniques
            origin_id = f"STOP_{origin_iata}_{origin.replace(' ', '_')[:30]}"
            dest_id = f"STOP_{dest_iata}_{dest.replace(' ', '_')[:30]}"

            # Ajouter stops
            if origin_id not in self.stops:
                self.stops[origin_id] = {
                    "id": origin_id,
                    "name": origin,
                    "city": origin,
                    "iata": origin_iata
                }

            if dest_id not in self.stops:
                self.stops[dest_id] = {
                    "id": dest_id,
                    "name": dest,
                    "city": dest,
                    "iata": dest_iata
                }

            # Trip
            service_id = date.replace("-", "")
            trip = {
                "trip_id": train_id,
                "route_id": route_id,
                "service_id": service_id,
                "trip_headsign": dest,
                "shape_id": "",
                "wheelchair_accessible": ""
            }
            self.trips.append(trip)

            # Stop times
            self.stop_times.append({
                "trip_id": train_id,
                "arrival_time": arr_time,
                "departure_time": dep_time,
                "stop_id": origin_id,
                "stop_sequence": "1",
                "stop_headsign": "",
                "pickup_type": "",
                "drop_off_type": ""
            })

            self.stop_times.append({
                "trip_id": train_id,
                "arrival_time": arr_time,
                "departure_time": "",
                "stop_id": dest_id,
                "stop_sequence": "2",
                "stop_headsign": "",
                "pickup_type": "",
                "drop_off_type": ""
            })

            if (idx + 1) % 10000 == 0:
                print(f"  {idx + 1}/{len(results)}")

        print(f"[OK] {len(self.trips)} trajets, {len(self.stops)} gares")

    def write_gtfs(self):
        """Ecrit les fichiers GTFS"""
        print("\n[*] Ecrire fichiers GTFS...")

        # agency.txt
        self._write_csv("agency.txt", [
            {
                "agency_id": "SNCF",
                "agency_name": "SNCF",
                "agency_url": "https://www.sncf-connect.com",
                "agency_timezone": "Europe/Paris",
                "agency_lang": "fr",
                "agency_phone": "3635"
            }
        ])

        # stops.txt
        stops_data = [
            {
                "stop_id": s["id"],
                "stop_name": s["name"],
                "stop_desc": s.get("city", ""),
                "stop_lat": "45.0",
                "stop_lon": "2.0",
                "zone_id": "",
                "stop_url": "",
                "location_type": "",
                "parent_station": ""
            }
            for s in self.stops.values()
        ]
        self._write_csv("stops.txt", stops_data)

        # routes.txt
        routes_data = [
            {
                "route_id": r["id"],
                "agency_id": "SNCF",
                "route_short_name": r["name"],
                "route_long_name": "TGVmax",
                "route_type": r["type"],
                "route_color": r["color"],
                "route_text_color": r["text_color"]
            }
            for r in self.routes.values()
        ]
        self._write_csv("routes.txt", routes_data)

        # calendar.txt
        calendar_data = [{
            "service_id": "weekday",
            "monday": "1", "tuesday": "1", "wednesday": "1", "thursday": "1",
            "friday": "1", "saturday": "1", "sunday": "1",
            "start_date": "20260101", "end_date": "20261231"
        }]
        self._write_csv("calendar.txt", calendar_data)

        # trips.txt
        trips_data = [
            {
                "route_id": t["route_id"],
                "service_id": t["service_id"],
                "trip_id": t["trip_id"],
                "trip_headsign": t["trip_headsign"],
                "direction_id": "",
                "shape_id": "",
                "wheelchair_accessible": ""
            }
            for t in self.trips
        ]
        self._write_csv("trips.txt", trips_data)

        # stop_times.txt
        self._write_csv("stop_times.txt", self.stop_times)

        print(f"[OK] GTFS ecrit dans {self.output_dir}/")

    def _write_csv(self, filename: str, rows: List[Dict]):
        """Ecrit un fichier CSV"""
        filepath = self.output_dir / filename

        if not rows:
            print(f"  [!] {filename} vide")
            return

        with open(filepath, 'w', newline='', encoding='utf-8') as f:
            writer = csv.DictWriter(f, fieldnames=rows[0].keys())
            writer.writeheader()
            writer.writerows(rows)

        size_mb = filepath.stat().st_size / 1024 / 1024
        print(f"  [+] {filename} ({len(rows)} lignes, {size_mb:.1f} MB)")

    def create_zip(self):
        """Cree une archive GTFS"""
        print("\n[*] Creer archive GTFS...")

        timestamp = datetime.now().strftime('%Y%m%d_%H%M%S')
        zip_path = f"gtfs_{timestamp}.zip"

        with zipfile.ZipFile(zip_path, 'w', zipfile.ZIP_DEFLATED) as zf:
            for file in self.output_dir.glob("*.txt"):
                zf.write(file, file.name)

        size_mb = os.path.getsize(zip_path) / 1024 / 1024
        print(f"[OK] {zip_path} ({size_mb:.1f} MB)")
        return zip_path

    def run(self):
        """Lance la conversion complete"""
        print("=" * 60)
        print("SNCF JSON to GTFS Converter")
        print("=" * 60)

        results = self.fetch_data()
        self.parse_trains(results)
        self.write_gtfs()
        zip_file = self.create_zip()

        print("\n" + "=" * 60)
        print("[SUCCESS] Conversion reussie!")
        print(f"[DIR] {self.output_dir}/")
        print(f"[ZIP] {zip_file}")
        print("=" * 60)

        return zip_file

def main():
    converter = SNFCtoGTFS()
    converter.run()

if __name__ == "__main__":
    main()
