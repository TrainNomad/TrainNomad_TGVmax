package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	data         *DataPackage
	mu           sync.RWMutex
	started      time.Time
	dataLoadedAt time.Time
	loadMs       int64
	stationIndex map[string]*StationRef
}

func newServer() *Server {
	return &Server{
		started:      time.Now(),
		stationIndex: make(map[string]*StationRef),
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/stations", s.handleStationsSearch)
	mux.HandleFunc("/search", s.handleSearch)

	// Legacy endpoints (backward compatibility)
	mux.HandleFunc("/api/tgvmax/search", s.handleSearch)
	mux.HandleFunc("/api/tgvmax/stations", s.handleStationsSearch)
	mux.HandleFunc("/api/tgvmax/reload", s.handleReload)

	return withMiddleware(mux)
}

func withMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		w.WriteHeader(status)
		gz := gzip.NewWriter(w)
		defer gz.Close()
		json.NewEncoder(gz).Encode(v)
		return
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func param(r *http.Request, names ...string) string {
	q := r.URL.Query()
	for _, n := range names {
		if v := strings.TrimSpace(q.Get(n)); v != "" {
			return v
		}
	}
	return ""
}

func intParam(r *http.Request, name string, def, min, max int) int {
	v, err := strconv.Atoi(param(r, name))
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeJSON(w, r, http.StatusNotFound, apiError{Error: "not_found"})
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"name":      "TGVmax routing API",
		"version":   "2.0",
		"endpoints": []string{"/health", "/stations?q=", "/search?from=&to=&date="},
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	trainCount := 0
	stationCount := 0
	if s.data != nil {
		trainCount = len(s.data.Trains)
		stationCount = len(s.data.Stations)
	}
	dataLoadedAt := s.dataLoadedAt
	loadMs := s.loadMs
	s.mu.RUnlock()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	writeJSON(w, r, http.StatusOK, map[string]any{
		"status":          "ok",
		"timestamp":       time.Now(),
		"trains_count":    trainCount,
		"stations_count":  stationCount,
		"data_loaded_at":  dataLoadedAt,
		"load_ms":         loadMs,
		"uptime_s":        int(time.Since(s.started).Seconds()),
		"memory_mb": map[string]float64{
			"heap": float64(m.HeapAlloc) / 1e6,
			"sys":  float64(m.Sys) / 1e6,
		},
	})
}

func (s *Server) handleStationsSearch(w http.ResponseWriter, r *http.Request) {
	q := param(r, "q", "query")
	limit := intParam(r, "limit", 10, 1, 50)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.data == nil {
		writeJSON(w, r, http.StatusServiceUnavailable, apiError{
			Error:  "data_not_loaded",
			Detail: "Les données ne sont pas disponibles",
		})
		return
	}

	var results []StationRef
	if q == "" {
		for i := 0; i < len(s.data.Stations) && i < limit; i++ {
			results = append(results, s.data.Stations[i])
		}
	} else {
		q = strings.ToLower(q)
		for _, st := range s.data.Stations {
			if len(results) >= limit {
				break
			}
			if strings.Contains(strings.ToLower(st.Name), q) ||
				strings.Contains(strings.ToLower(st.City), q) ||
				strings.Contains(strings.ToLower(st.ID), q) {
				results = append(results, st)
			}
		}
	}

	writeJSON(w, r, http.StatusOK, map[string]any{
		"results": results,
		"count":   len(results),
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeJSON(w, r, http.StatusMethodNotAllowed, apiError{
			Error:  "method_not_allowed",
			Detail: "Use GET or POST",
		})
		return
	}

	from := param(r, "from", "origin")
	to := param(r, "to", "destination")
	date := param(r, "date")
	freeOnly := r.URL.Query().Get("free_only") == "true"
	limit := intParam(r, "limit", 50, 1, 200)

	if from == "" || to == "" {
		writeJSON(w, r, http.StatusBadRequest, apiError{
			Error:  "missing_parameters",
			Detail: "from and to are required",
		})
		return
	}

	s.mu.RLock()
	results := s.filterTrains(from, to, date, freeOnly, limit)
	s.mu.RUnlock()

	writeJSON(w, r, http.StatusOK, map[string]any{
		"query": map[string]string{
			"from": from,
			"to":   to,
			"date": date,
		},
		"results": results,
		"count":   len(results),
	})
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, r, http.StatusMethodNotAllowed, apiError{
			Error:  "method_not_allowed",
			Detail: "Use POST",
		})
		return
	}

	secret := r.Header.Get("X-Reload-Secret")
	expectedSecret := os.Getenv("RELOAD_SECRET")
	if expectedSecret != "" && secret != expectedSecret {
		writeJSON(w, r, http.StatusUnauthorized, apiError{
			Error: "unauthorized",
		})
		return
	}

	log.Println("Rechargement des données en cours...")
	start := time.Now()

	if err := s.loadData(); err != nil {
		writeJSON(w, r, http.StatusInternalServerError, apiError{
			Error:  "reload_failed",
			Detail: err.Error(),
		})
		return
	}

	duration := time.Since(start)
	writeJSON(w, r, http.StatusOK, map[string]any{
		"status":       "success",
		"message":      "Data reloaded",
		"duration_ms":  duration.Milliseconds(),
		"timestamp":    time.Now(),
	})

	log.Printf("✓ Rechargement terminé en %v", duration)
}

func (s *Server) filterTrains(from, to, date string, freeOnly bool, limit int) []SearchResult {
	if s.data == nil {
		return []SearchResult{}
	}

	var results []SearchResult
	from = strings.ToLower(from)
	to = strings.ToLower(to)

	for _, train := range s.data.Trains {
		if len(results) >= limit {
			break
		}

		if !stringMatches(train.Departure, from) && !stringMatches(train.Origin.ID, from) &&
			!stringMatches(train.Origin.City, from) && !stringMatches(train.Origin.Name, from) {
			continue
		}

		if !stringMatches(train.Arrival, to) && !stringMatches(train.Destination.ID, to) &&
			!stringMatches(train.Destination.City, to) && !stringMatches(train.Destination.Name, to) {
			continue
		}

		if date != "" && train.OperatingDay != date {
			continue
		}

		if freeOnly && train.AvailableSeats <= 0 {
			continue
		}

		dur := calculateDuration(train.DepartureTime, train.ArrivalTime)

		result := SearchResult{
			ID:              train.ID,
			TrainNumber:     train.TrainNumber,
			Departure:       train.Departure,
			Arrival:         train.Arrival,
			DepartureTime:   train.DepartureTime,
			ArrivalTime:     train.ArrivalTime,
			Origin:          train.Origin,
			Destination:     train.Destination,
			AvailableSeats:  train.AvailableSeats,
			TotalSeats:      train.TotalSeats,
			FreeSeatsOnly:   freeOnly,
			DurationMinutes: dur,
		}

		results = append(results, result)
	}

	return results
}

func stringMatches(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	haystack = strings.ToLower(haystack)
	needle = strings.ToLower(needle)
	return haystack == needle || strings.Contains(haystack, needle)
}

func calculateDuration(depStr, arrStr string) int {
	depHour, depMin := parseTime(depStr)
	arrHour, arrMin := parseTime(arrStr)

	depTotalMin := depHour*60 + depMin
	arrTotalMin := arrHour*60 + arrMin

	if arrTotalMin < depTotalMin {
		arrTotalMin += 24 * 60
	}

	return arrTotalMin - depTotalMin
}

func parseTime(timeStr string) (int, int) {
	hour, min := 0, 0
	fmt.Sscanf(timeStr, "%d:%d", &hour, &min)
	return hour, min
}

func (s *Server) loadData() error {
	start := time.Now()

	dataPath := findDataFile()
	if dataPath == "" {
		log.Println("Fichier data.bin.gz non trouvé localement")
		return fmt.Errorf("data.bin.gz not found in any expected location")
	}

	pkg, err := LoadFromGzip(dataPath)
	if err != nil {
		return fmt.Errorf("chargement %s : %w", dataPath, err)
	}

	stationIndex := make(map[string]*StationRef)
	for i := range pkg.Stations {
		stationIndex[pkg.Stations[i].ID] = &pkg.Stations[i]
	}

	s.mu.Lock()
	s.data = pkg
	s.stationIndex = stationIndex
	s.dataLoadedAt = time.Now()
	s.loadMs = time.Since(start).Milliseconds()
	s.mu.Unlock()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	log.Printf("✓ %d trains et %d gares chargés en %d ms (heap: %.1f Mo)",
		len(pkg.Trains), len(pkg.Stations), s.loadMs, float64(m.HeapAlloc)/1e6)
	return nil
}

func findDataFile() string {
	paths := []string{
		dataFileName,
		"data/" + dataFileName,
		"../data/" + dataFileName,
		"/app/" + dataFileName,
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
