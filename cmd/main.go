// cmd/main.go : utilitaire pour télécharger et convertir les données TGVmax
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	// À adapter selon la structure du projet
	_ "backend/tgvmax"
)

func main() {
	outputFlag := flag.String("output", "data.bin.gz", "Chemin du fichier de sortie")
	urlFlag := flag.String("url", "https://ressources.data.sncf.com/api/explore/v2.1/catalog/datasets/tgvmax/exports/json?lang=fr&timezone=Europe%2FBerlin", "URL du JSON source")
	flag.Parse()

	log.Printf("TGVmax Data Converter")
	log.Printf("URL: %s", *urlFlag)
	log.Printf("Output: %s", *outputFlag)

	// Créer le répertoire de sortie si nécessaire
	if dir := filepath.Dir(*outputFlag); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Erreur création répertoire : %v", err)
		}
	}

	// Télécharger et convertir
	if err := downloadAndConvertData(*outputFlag); err != nil {
		log.Fatalf("❌ Erreur : %v", err)
	}

	log.Printf("✓ Conversion réussie")
}

// Stub : à implémenter en appelant converter.DownloadAndConvertData
func downloadAndConvertData(output string) error {
	fmt.Printf("Téléchargement depuis %s...\n", "API SNCF")
	// Appel à converter.DownloadAndConvertData(output)
	return nil
}
