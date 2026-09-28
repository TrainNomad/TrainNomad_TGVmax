package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"time"
)

const (
	githubReleaseURL = "https://api.github.com/repos/[USERNAME]/[REPO]/releases/latest"
	dataFileName     = "data.bin.gz"
)

type Server struct {
	data       *DataPackage
	mu         sync.RWMutex
	started    time.Time
	dataLoadedAt time.Time
	loadMs     int64
}

func main() {
	// Limiter la mémoire (Render offre 512 Mo par défaut)
	debug.SetMemoryLimit(350 << 20)

	srv := &Server{
		started: time.Now(),
	}

	// Charger les données au démarrage
	if err := srv.loadData(); err != nil {
		log.Fatalf("Erreur chargement données : %v", err)
	}

	printMemStats(srv)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	log.Printf("TGVmax API écoute sur :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, srv.routes()))
}

func (s *Server) loadData() error {
	start := time.Now()

	dataPath := findDataFile()
	if dataPath == "" {
		log.Println("Fichier data.bin.gz non trouvé localement, tentative de téléchargement...")
		if err := downloadDataFromRelease(); err != nil {
			return fmt.Errorf("téléchargement échoué : %w", err)
		}
		dataPath = dataFileName
	}

	pkg, err := LoadFromGzip(dataPath)
	if err != nil {
		return fmt.Errorf("chargement %s : %w", dataPath, err)
	}

	s.mu.Lock()
	s.data = pkg
	s.dataLoadedAt = time.Now()
	s.loadMs = time.Since(start).Milliseconds()
	s.mu.Unlock()

	log.Printf("✓ %d trains et %d gares chargés en %d ms", len(pkg.Trains), len(pkg.Stations), s.loadMs)
	return nil
}

func findDataFile() string {
	paths := []string{
		dataFileName,
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

func downloadDataFromRelease() error {
	githubToken := os.Getenv("GITHUB_TOKEN")
	client := &http.Client{Timeout: 30 * time.Second}

	req, _ := http.NewRequest("GET", githubReleaseURL, nil)
	if githubToken != "" {
		req.Header.Set("Authorization", "token "+githubToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Parser la réponse GitHub et chercher le fichier data.bin.gz dans les assets
	// Pour simplifier, on suppose que le fichier est le premier asset
	body, _ := io.ReadAll(resp.Body)
	// (À implémenter selon structure GitHub API si nécessaire)

	log.Printf("Téléchargement de %s", dataFileName)
	return nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/tgvmax/search", s.handleSearch)
	mux.HandleFunc("/api/tgvmax/stations", s.handleStations)
	mux.HandleFunc("/api/tgvmax/reload", s.handleReload)

	return withMiddleware(mux)
}

func withMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Log
		log.Printf("%s %s", r.Method, r.URL.Path)

		h.ServeHTTP(w, r)
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	s.mu.RLock()
	trains := len(s.data.Trains)
	s.mu.RUnlock()

	fmt.Fprintf(w, `{"service":"TGVmax API","version":"1.0","trains":%d}`, trains)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	s.mu.RLock()
	trainCount := len(s.data.Trains)
	stationCount := len(s.data.Stations)
	dataLoadedAt := s.dataLoadedAt
	loadMs := s.loadMs
	s.mu.RUnlock()

	uptime := time.Since(s.started)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	h := HealthResponse{
		Status:        "healthy",
		Timestamp:     time.Now(),
		TrainsCount:   trainCount,
		StationsCount: stationCount,
		DataLoadedAt:  dataLoadedAt,
		Uptime:        uptime.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := jsonResponse(w, h); err != nil {
		log.Printf("Erreur encoding health : %v", err)
	}

	log.Printf("Health: %d trains, %d gares, heap %.1f Mo", trainCount, stationCount, float64(m.HeapAlloc)/1e6)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		jsonResponse(w, apiError{
			Error: "Method not allowed",
			Detail: "Use GET or POST",
		})
		return
	}

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	date := r.URL.Query().Get("date")
	freeOnly := r.URL.Query().Get("free_only") == "true"

	if from == "" || to == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		jsonResponse(w, apiError{
			Error: "Missing parameters",
			Detail: "from and to are required",
		})
		return
	}

	s.mu.RLock()
	results := s.filterTrains(from, to, date, freeOnly)
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	jsonResponse(w, map[string]interface{}{
		"query": map[string]string{
			"from": from,
			"to":   to,
			"date": date,
		},
		"results": results,
		"count":   len(results),
	})
}

func (s *Server) handleStations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	s.mu.RLock()
	stations := s.data.Stations
	s.mu.RUnlock()

	jsonResponse(w, map[string]interface{}{
		"stations": stations,
		"count":    len(stations),
	})
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		jsonResponse(w, apiError{
			Error: "Method not allowed",
			Detail: "Use POST",
		})
		return
	}

	// Vérification du secret (optionnel)
	secret := r.Header.Get("X-Reload-Secret")
	expectedSecret := os.Getenv("RELOAD_SECRET")
	if expectedSecret != "" && secret != expectedSecret {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		jsonResponse(w, apiError{
			Error: "Unauthorized",
		})
		return
	}

	log.Println("Rechargement des données en cours...")
	start := time.Now()

	if err := s.loadData(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		jsonResponse(w, apiError{
			Error: "Reload failed",
			Detail: err.Error(),
		})
		return
	}

	duration := time.Since(start)
	w.Header().Set("Content-Type", "application/json")
	jsonResponse(w, map[string]interface{}{
		"status":    "success",
		"message":   "Data reloaded",
		"duration":  duration.String(),
		"timestamp": time.Now(),
	})

	log.Printf("✓ Rechargement terminé en %v", duration)
}

func (s *Server) filterTrains(from, to, date string, freeOnly bool) []SearchResult {
	var results []SearchResult

	for _, train := range s.data.Trains {
		// Filtrer par gare de départ
		if from != "" && !stringMatches(train.Departure, from) && !stringMatches(train.Origin.ID, from) {
			continue
		}

		// Filtrer par gare d'arrivée
		if to != "" && !stringMatches(train.Arrival, to) && !stringMatches(train.Destination.ID, to) {
			continue
		}

		// Filtrer par date
		if date != "" && train.OperatingDay != date {
			continue
		}

		// Filtrer par places gratuites
		if freeOnly && train.AvailableSeats <= 0 {
			continue
		}

		// Calculer la durée
		dur := calculateDuration(train.DepartureTime, train.ArrivalTime)

		result := SearchResult{
			ID:             train.ID,
			TrainNumber:    train.TrainNumber,
			Departure:      train.Departure,
			Arrival:        train.Arrival,
			DepartureTime:  train.DepartureTime,
			ArrivalTime:    train.ArrivalTime,
			Origin:         train.Origin,
			Destination:    train.Destination,
			AvailableSeats: train.AvailableSeats,
			TotalSeats:     train.TotalSeats,
			FreeSeatsOnly:  freeOnly,
			DurationMinutes: dur,
		}

		results = append(results, result)
	}

	return results
}

func stringMatches(haystack, needle string) bool {
	return needle != "" && (haystack == needle ||
		contains(haystack, needle) || contains(needle, haystack))
}

func contains(haystack, needle string) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func calculateDuration(depStr, arrStr string) int {
	// Format simplifié : HH:MM
	depHour, depMin := parseTime(depStr)
	arrHour, arrMin := parseTime(arrStr)

	depTotalMin := depHour*60 + depMin
	arrTotalMin := arrHour*60 + arrMin

	// Gérer le passage minuit
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

func printMemStats(s *Server) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	log.Printf("Mémoire : heap %.1f Mo / %d trains",
		float64(m.HeapAlloc)/1e6, len(s.data.Trains))
}

func jsonResponse(w http.ResponseWriter, v interface{}) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(v)
}
