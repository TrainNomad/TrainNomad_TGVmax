package main

import "time"

// TrainData représente une rame TGVmax
type TrainData struct {
	ID           string         `json:"id"`
	TrainNumber  string         `json:"train_number"`
	Departure    string         `json:"departure"`
	Arrival      string         `json:"arrival"`
	DepartureTime string        `json:"departure_time"`
	ArrivalTime  string         `json:"arrival_time"`
	Origin       StationRef     `json:"origin"`
	Destination  StationRef     `json:"destination"`
	AvailableSeats int          `json:"available_seats"`
	TotalSeats   int            `json:"total_seats"`
	OperatingDay string         `json:"operating_day"`
	Stops        []StopInfo     `json:"stops,omitempty"`
}

type StationRef struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Code     string  `json:"code"`
	City     string  `json:"city"`
	Country  string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type StopInfo struct {
	Station    StationRef `json:"station"`
	Arrival    string     `json:"arrival,omitempty"`
	Departure  string     `json:"departure,omitempty"`
}

// SearchResult représente le résultat d'une recherche
type SearchResult struct {
	ID              string       `json:"id"`
	TrainNumber     string       `json:"train_number"`
	Departure       string       `json:"departure"`
	Arrival         string       `json:"arrival"`
	DepartureTime   string       `json:"departure_time"`
	ArrivalTime     string       `json:"arrival_time"`
	Origin          StationRef   `json:"origin"`
	Destination     StationRef   `json:"destination"`
	AvailableSeats  int          `json:"available_seats"`
	TotalSeats      int          `json:"total_seats"`
	FreeSeatsOnly   bool         `json:"free_seats_only"`
	DurationMinutes int          `json:"duration_minutes"`
}

type apiError struct {
	Error   string `json:"error"`
	Detail  string `json:"detail,omitempty"`
	Message string `json:"message,omitempty"`
}

type HealthResponse struct {
	Status        string    `json:"status"`
	Timestamp     time.Time `json:"timestamp"`
	TrainsCount   int       `json:"trains_count"`
	StationsCount int       `json:"stations_count"`
	DataLoadedAt  time.Time `json:"data_loaded_at"`
	Uptime        string    `json:"uptime"`
}
