package main

import (
	"bytes"
	"testing"
)

func TestWriteReadString(t *testing.T) {
	const testStr = "Hello, TGVmax!"

	var buf bytes.Buffer
	writeString(&buf, testStr)

	r := bytes.NewReader(buf.Bytes())
	result, err := readString(r)

	if err != nil {
		t.Fatalf("readString failed: %v", err)
	}
	if result != testStr {
		t.Errorf("Expected %q, got %q", testStr, result)
	}
}

func TestEncodeDecode(t *testing.T) {
	// Créer un package de test
	pkg := &DataPackage{
		Version:    dataVersion,
		Timestamp:  1234567890,
		StationMap: make(map[string]int),
	}

	// Ajouter une station
	station := StationRef{
		ID:        "TEST001",
		Name:      "Paris Gare de Lyon",
		City:      "Paris",
		Country:   "France",
		Latitude:  48.8432,
		Longitude: 2.3734,
	}
	pkg.Stations = append(pkg.Stations, station)
	pkg.StationMap["TEST001"] = 0

	// Ajouter un train
	train := TrainData{
		ID:           "TGV001",
		TrainNumber:  "9001",
		Departure:    "Paris Gare de Lyon",
		Arrival:      "Lyon Part-Dieu",
		DepartureTime: "07:15",
		ArrivalTime:  "09:45",
		Origin:       station,
		Destination:  station,
		AvailableSeats: 145,
		TotalSeats:   240,
		OperatingDay: "2024-01-15",
	}
	pkg.Trains = append(pkg.Trains, train)

	// Encoder
	encoded, err := encodeToGzip(pkg)
	if err != nil {
		t.Fatalf("encodeToGzip failed: %v", err)
	}

	if len(encoded) == 0 {
		t.Fatal("Encoded data is empty")
	}

	// Décoder
	decoded, err := decodeFromBinary(decompress(encoded))
	if err != nil {
		t.Fatalf("decodeFromBinary failed: %v", err)
	}

	// Vérifier
	if len(decoded.Trains) != 1 {
		t.Errorf("Expected 1 train, got %d", len(decoded.Trains))
	}

	if decoded.Trains[0].ID != "TGV001" {
		t.Errorf("Expected train ID 'TGV001', got %q", decoded.Trains[0].ID)
	}

	if len(decoded.Stations) != 1 {
		t.Errorf("Expected 1 station, got %d", len(decoded.Stations))
	}

	if decoded.Stations[0].ID != "TEST001" {
		t.Errorf("Expected station ID 'TEST001', got %q", decoded.Stations[0].ID)
	}
}

func decompress(data []byte) []byte {
	// Simplifié pour les tests - à implémenter proprement
	return data
}

func TestExtractTrain(t *testing.T) {
	item := map[string]interface{}{
		"id":           "TGV001",
		"train_number": "9001",
		"departure":    "Paris",
		"arrival":      "Lyon",
	}

	train, err := extractTrain(item)
	if err != nil {
		t.Fatalf("extractTrain failed: %v", err)
	}

	if train.ID != "TGV001" {
		t.Errorf("Expected ID 'TGV001', got %q", train.ID)
	}

	if train.TrainNumber != "9001" {
		t.Errorf("Expected TrainNumber '9001', got %q", train.TrainNumber)
	}
}

func TestExtractStationAlternateFields(t *testing.T) {
	// Tester les noms de champs alternatifs (français)
	item := map[string]interface{}{
		"id":           "TGV001",
		"Numéro du train": "9001",
		"Gare de départ":  "Paris",
		"Gare d'arrivée":  "Lyon",
	}

	train, err := extractTrain(item)
	if err != nil {
		t.Fatalf("extractTrain with French fields failed: %v", err)
	}

	if train.TrainNumber != "9001" {
		t.Errorf("Expected TrainNumber '9001', got %q", train.TrainNumber)
	}

	if train.Departure != "Paris" {
		t.Errorf("Expected Departure 'Paris', got %q", train.Departure)
	}
}
