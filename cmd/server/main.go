package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"take-home-test-go/internal/app"
)

// main mirrors reference-ts/src/index.ts.
func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           app.New(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("Server is running on http://localhost:%s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
