// cmd: CLI tool to download and convert TGVmax data from SNCF JSON to binary format
//
// Usage:
//   go run ./cmd -output data.bin.gz
//   go run ./cmd -url "..." -output data/data.bin.gz
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	dataSourceURL = "https://ressources.data.sncf.com/api/explore/v2.1/catalog/datasets/tgvmax/exports/json?lang=fr&timezone=Europe%2FBerlin"
	dataMagic     = "TGVDATA001"
	dataVersion   = 1
)

type RawTGVData struct {
	Results []map[string]interface{} `json:"results"`
}

type DataPackage struct {
	Version    int
	Timestamp  int64
	Trains     []TrainData
	Stations   []StationRef
	StationMap map[string]int
}

type TrainData struct {
	ID            string
	TrainNumber   string
	Departure     string
	Arrival       string
	DepartureTime string
	ArrivalTime   string
	Origin        StationRef
	Destination   StationRef
	AvailableSeats int
	TotalSeats    int
	OperatingDay  string
	Stops         []StopInfo
}

type StationRef struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Code      string  `json:"code"`
	City      string  `json:"city"`
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type StopInfo struct {
	Station   StationRef
	Arrival   string
	Departure string
}

func main() {
	outputFlag := flag.String("output", "data.bin.gz", "Output file path for compiled data")
	urlFlag := flag.String("url", dataSourceURL, "URL of SNCF JSON API")
	flag.Parse()

	log.Printf("TGVmax Data Converter v2.0")
	log.Printf("Source: %s", *urlFlag)
	log.Printf("Output: %s", *outputFlag)

	if dir := filepath.Dir(*outputFlag); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Directory creation error: %v", err)
		}
	}

	if err := downloadAndConvertData(*urlFlag, *outputFlag); err != nil {
		log.Fatalf("❌ Error: %v", err)
	}

	log.Printf("✓ Conversion successful")
}

func downloadAndConvertData(sourceURL, outputPath string) error {
	log.Printf("Downloading data from %s", sourceURL)

	resp, err := http.Get(sourceURL)
	if err != nil {
		return fmt.Errorf("download error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response error: %w", err)
	}

	log.Printf("Converting JSON data")
	pkg, err := parseAndFilterData(body)
	if err != nil {
		return fmt.Errorf("JSON parse error: %w", err)
	}

	log.Printf("Encoding binary: %d trains, %d stations", len(pkg.Trains), len(pkg.Stations))
	bin, err := encodeToGzip(pkg)
	if err != nil {
		return fmt.Errorf("encoding error: %w", err)
	}

	if err := os.WriteFile(outputPath, bin, 0644); err != nil {
		return fmt.Errorf("file write error: %w", err)
	}

	log.Printf("✓ Data saved to %s (%.1f MB)", outputPath, float64(len(bin))/1e6)
	return nil
}

func parseAndFilterData(jsonData []byte) (*DataPackage, error) {
	var raw RawTGVData
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		return nil, fmt.Errorf("JSON unmarshal failed: %w", err)
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
			continue
		}

		trainKey := fmt.Sprintf("%s:%s:%s", train.TrainNumber, train.Departure, train.Arrival)
		if trainMap[trainKey] {
			continue
		}
		trainMap[trainKey] = true

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

	for id, station := range stations {
		pkg.StationMap[id] = len(pkg.Stations)
		pkg.Stations = append(pkg.Stations, station)
	}

	return pkg, nil
}

func extractTrain(item map[string]interface{}) (TrainData, error) {
	train := TrainData{}

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

	if v, ok := item["available_seats"].(float64); ok {
		train.AvailableSeats = int(v)
	}

	if v, ok := item["total_seats"].(float64); ok {
		train.TotalSeats = int(v)
	}

	if v, ok := item["operating_day"].(string); ok {
		train.OperatingDay = v
	}

	train.Origin = extractStation(item, "origin")
	train.Destination = extractStation(item, "destination")

	if train.ID == "" || train.TrainNumber == "" {
		return train, fmt.Errorf("missing ID or TrainNumber")
	}

	return train, nil
}

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

func encodeToGzip(pkg *DataPackage) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString(dataMagic)
	binary.Write(&buf, binary.LittleEndian, int32(dataVersion))
	binary.Write(&buf, binary.LittleEndian, pkg.Timestamp)

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

		binary.Write(&buf, binary.LittleEndian, int32(pkg.StationMap[train.Origin.ID]))
		binary.Write(&buf, binary.LittleEndian, int32(pkg.StationMap[train.Destination.ID]))

		binary.Write(&buf, binary.LittleEndian, int32(len(train.Stops)))
		for _, stop := range train.Stops {
			binary.Write(&buf, binary.LittleEndian, int32(pkg.StationMap[stop.Station.ID]))
			writeString(&buf, stop.Arrival)
			writeString(&buf, stop.Departure)
		}
	}

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

func writeString(w *bytes.Buffer, s string) {
	binary.Write(w, binary.LittleEndian, int32(len(s)))
	w.WriteString(s)
}
