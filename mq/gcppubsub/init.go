package gcppubsub

import (
	"log"
	"os"
)

func GetGCPProjectID() string {
	// Get your GCP Project ID from an environment variable or hardcode it
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		log.Fatal("GCP_PROJECT_ID environment variable must be set.")
	}
	return projectID
}
