package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	dataSourceURL = "https://ressources.data.sncf.com/api/explore/v2.1/catalog/datasets/tgvmax/exports/json?lang=fr&timezone=Europe%2FBerlin"
	dataMagic     = "TGVDATA001"
	dataVersion   = 1
)

// RawTGVData représente la structure brute du JSON de la SNCF
type RawTGVData struct {
	Results []map[string]interface{} `json:"results"`
}

// DataPackage encapsule toutes les données optimisées pour TGVmax
type DataPackage struct {
	Version    int
	Timestamp  int64
	Trains     []TrainData
	Stations   []StationRef
	StationMap map[string]int // id -> index dans Stations
}

// DownloadAndConvertData télécharge les données JSON de la SNCF et les convertit en binaire compressé
func DownloadAndConvertData(outputPath string) error {
	log.Printf("Téléchargement des données TGVmax depuis %s", dataSourceURL)

	resp, err := http.Get(dataSourceURL)
	if err != nil {
		return fmt.Errorf("erreur de téléchargement : %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("erreur lecture réponse : %w", err)
	}

	log.Printf("Conversion des données JSON")
	pkg, err := parseAndFilterData(body)
	if err != nil {
		return fmt.Errorf("erreur parse JSON : %w", err)
	}

	log.Printf("Encodage binaire : %d trains, %d gares", len(pkg.Trains), len(pkg.Stations))
	binary, err := encodeToGzip(pkg)
	if err != nil {
		return fmt.Errorf("erreur encodage : %w", err)
	}

	if err := os.WriteFile(outputPath, binary, 0644); err != nil {
		return fmt.Errorf("erreur écriture fichier : %w", err)
	}

	log.Printf("✓ Données sauvegardées en %s (%.1f Mo)", outputPath, float64(len(binary))/1e6)
	return nil
}

// parseAndFilterData parse le JSON et filtre les champs pertinents
func parseAndFilterData(jsonData []byte) (*DataPackage, error) {
	var raw RawTGVData
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		return nil, fmt.Errorf("parse JSON échoué : %w", err)
	}

	pkg := &DataPackage{
		Version:    dataVersion,
		Timestamp:  time.Now().Unix(),
		StationMap: make(map[string]int),
	}

	stations := make(map[string]StationRef)
	trainMap := make(map[string]bool)

	for _, item := range raw.Results {
		train, err := extractTrain(item)
		if err != nil {
			log.Printf("⚠ Erreur extraction train : %v", err)
			continue
		}

		// Déduplication
		trainKey := fmt.Sprintf("%s:%s:%s", train.TrainNumber, train.Departure, train.Arrival)
		if trainMap[trainKey] {
			continue
		}
		trainMap[trainKey] = true

		// Extraction des stations
		if train.Origin.ID != "" {
			stations[train.Origin.ID] = train.Origin
		}
		if train.Destination.ID != "" {
			stations[train.Destination.ID] = train.Destination
		}

		for _, stop := range train.Stops {
			if stop.Station.ID != "" {
				stations[stop.Station.ID] = stop.Station
			}
		}

		pkg.Trains = append(pkg.Trains, train)
	}

	// Construction de l'index des stations
	for id, station := range stations {
		pkg.StationMap[id] = len(pkg.Stations)
		pkg.Stations = append(pkg.Stations, station)
	}

	return pkg, nil
}

// extractTrain extrait les données pertinentes d'un élément JSON
func extractTrain(item map[string]interface{}) (TrainData, error) {
	train := TrainData{}

	// Champs simples (strings)
	if v, ok := item["id"].(string); ok {
		train.ID = v
	} else if v, ok := item["Id"].(string); ok {
		train.ID = v
	}

	if v, ok := item["train_number"].(string); ok {
		train.TrainNumber = v
	} else if v, ok := item["Numéro du train"].(string); ok {
		train.TrainNumber = v
	}

	if v, ok := item["departure"].(string); ok {
		train.Departure = v
	} else if v, ok := item["Gare de départ"].(string); ok {
		train.Departure = v
	}

	if v, ok := item["arrival"].(string); ok {
		train.Arrival = v
	} else if v, ok := item["Gare d'arrivée"].(string); ok {
		train.Arrival = v
	}

	if v, ok := item["departure_time"].(string); ok {
		train.DepartureTime = v
	}

	if v, ok := item["arrival_time"].(string); ok {
		train.ArrivalTime = v
	}

	// Nombres entiers
	if v, ok := item["available_seats"].(float64); ok {
		train.AvailableSeats = int(v)
	}

	if v, ok := item["total_seats"].(float64); ok {
		train.TotalSeats = int(v)
	}

	if v, ok := item["operating_day"].(string); ok {
		train.OperatingDay = v
	}

	// Stations (géo)
	train.Origin = extractStation(item, "origin")
	train.Destination = extractStation(item, "destination")

	if train.ID == "" || train.TrainNumber == "" {
		return train, fmt.Errorf("ID ou TrainNumber manquant")
	}

	return train, nil
}

// extractStation extrait une station d'un objet JSON imbriqué
func extractStation(item map[string]interface{}, prefix string) StationRef {
	station := StationRef{}

	if stationObj, ok := item[prefix].(map[string]interface{}); ok {
		if v, ok := stationObj["id"].(string); ok {
			station.ID = v
		}
		if v, ok := stationObj["name"].(string); ok {
			station.Name = v
		}
		if v, ok := stationObj["code"].(string); ok {
			station.Code = v
		}
		if v, ok := stationObj["city"].(string); ok {
			station.City = v
		}
		if v, ok := stationObj["country"].(string); ok {
			station.Country = v
		}
		if v, ok := stationObj["latitude"].(float64); ok {
			station.Latitude = v
		}
		if v, ok := stationObj["longitude"].(float64); ok {
			station.Longitude = v
		}
	}

	return station
}

// encodeToGzip encode les données en binaire gzippé
func encodeToGzip(pkg *DataPackage) ([]byte, error) {
	var buf bytes.Buffer

	// Header
	buf.WriteString(dataMagic)
	binary.Write(&buf, binary.LittleEndian, int32(dataVersion))
	binary.Write(&buf, binary.LittleEndian, pkg.Timestamp)

	// Trains
	binary.Write(&buf, binary.LittleEndian, int32(len(pkg.Trains)))
	for _, train := range pkg.Trains {
		writeString(&buf, train.ID)
		writeString(&buf, train.TrainNumber)
		writeString(&buf, train.Departure)
		writeString(&buf, train.Arrival)
		writeString(&buf, train.DepartureTime)
		writeString(&buf, train.ArrivalTime)
		binary.Write(&buf, binary.LittleEndian, int32(train.AvailableSeats))
		binary.Write(&buf, binary.LittleEndian, int32(train.TotalSeats))
		writeString(&buf, train.OperatingDay)

		// Origin
		binary.Write(&buf, binary.LittleEndian, int32(pkg.StationMap[train.Origin.ID]))
		// Destination
		binary.Write(&buf, binary.LittleEndian, int32(pkg.StationMap[train.Destination.ID]))

		// Stops
		binary.Write(&buf, binary.LittleEndian, int32(len(train.Stops)))
		for _, stop := range train.Stops {
			binary.Write(&buf, binary.LittleEndian, int32(pkg.StationMap[stop.Station.ID]))
			writeString(&buf, stop.Arrival)
			writeString(&buf, stop.Departure)
		}
	}

	// Stations
	binary.Write(&buf, binary.LittleEndian, int32(len(pkg.Stations)))
	for _, station := range pkg.Stations {
		writeString(&buf, station.ID)
		writeString(&buf, station.Name)
		writeString(&buf, station.Code)
		writeString(&buf, station.City)
		writeString(&buf, station.Country)
		binary.Write(&buf, binary.LittleEndian, station.Latitude)
		binary.Write(&buf, binary.LittleEndian, station.Longitude)
	}

	// Compression gzip
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	if _, err := gz.Write(buf.Bytes()); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}

	return gzBuf.Bytes(), nil
}

// LoadFromGzip décode les données depuis binaire gzippé
func LoadFromGzip(path string) (*DataPackage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	decompressed, err := io.ReadAll(gz)
	if err != nil {
		return nil, err
	}

	return decodeFromBinary(decompressed)
}

// decodeFromBinary décode le format binaire
func decodeFromBinary(data []byte) (*DataPackage, error) {
	r := bytes.NewReader(data)
	pkg := &DataPackage{
		StationMap: make(map[string]int),
	}

	// Header
	magic := make([]byte, len(dataMagic))
	if _, err := r.Read(magic); err != nil {
		return nil, err
	}
	if !bytes.Equal(magic, []byte(dataMagic)) {
		return nil, fmt.Errorf("magic invalide")
	}

	var version int32
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return nil, err
	}
	pkg.Version = int(version)

	if err := binary.Read(r, binary.LittleEndian, &pkg.Timestamp); err != nil {
		return nil, err
	}

	// Trains
	var trainCount int32
	if err := binary.Read(r, binary.LittleEndian, &trainCount); err != nil {
		return nil, err
	}
	pkg.Trains = make([]TrainData, trainCount)

	for i := 0; i < int(trainCount); i++ {
		train := TrainData{}
		train.ID, _ = readString(r)
		train.TrainNumber, _ = readString(r)
		train.Departure, _ = readString(r)
		train.Arrival, _ = readString(r)
		train.DepartureTime, _ = readString(r)
		train.ArrivalTime, _ = readString(r)

		var availSeats, totalSeats int32
		binary.Read(r, binary.LittleEndian, &availSeats)
		binary.Read(r, binary.LittleEndian, &totalSeats)
		train.AvailableSeats = int(availSeats)
		train.TotalSeats = int(totalSeats)

		train.OperatingDay, _ = readString(r)

		var originIdx, destIdx int32
		binary.Read(r, binary.LittleEndian, &originIdx)
		binary.Read(r, binary.LittleEndian, &destIdx)

		var stopCount int32
		binary.Read(r, binary.LittleEndian, &stopCount)
		train.Stops = make([]StopInfo, stopCount)
		for j := 0; j < int(stopCount); j++ {
			var stationIdx int32
			binary.Read(r, binary.LittleEndian, &stationIdx)
			train.Stops[j].Arrival, _ = readString(r)
			train.Stops[j].Departure, _ = readString(r)
		}

		pkg.Trains[i] = train
	}

	// Stations
	var stationCount int32
	if err := binary.Read(r, binary.LittleEndian, &stationCount); err != nil {
		return nil, err
	}
	pkg.Stations = make([]StationRef, stationCount)

	for i := 0; i < int(stationCount); i++ {
		station := StationRef{}
		station.ID, _ = readString(r)
		station.Name, _ = readString(r)
		station.Code, _ = readString(r)
		station.City, _ = readString(r)
		station.Country, _ = readString(r)
		binary.Read(r, binary.LittleEndian, &station.Latitude)
		binary.Read(r, binary.LittleEndian, &station.Longitude)

		pkg.Stations[i] = station
		pkg.StationMap[station.ID] = i
	}

	return pkg, nil
}

func writeString(w *bytes.Buffer, s string) {
	binary.Write(w, binary.LittleEndian, int32(len(s)))
	w.WriteString(s)
}

func readString(r *bytes.Reader) (string, error) {
	var length int32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return "", err
	}
	buf := make([]byte, length)
	if _, err := r.Read(buf); err != nil {
		return "", err
	}
	return string(buf), nil
}
