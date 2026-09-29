package main

import (
	"log"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
)

const dataFileName = "data.bin.gz"

func main() {
	debug.SetMemoryLimit(350 << 20)

	srv := newServer()

	if err := srv.loadData(); err != nil {
		log.Fatalf("Erreur chargement données : %v", err)
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	log.Printf("✓ Démarrage TGVmax API (heap: %.1f Mo)", float64(m.HeapAlloc)/1e6)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	log.Printf("Écoute sur :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, srv.routes()))
}
