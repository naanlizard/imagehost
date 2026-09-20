package main

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	baseURL := strings.TrimRight(os.Getenv("BASE_URL"), "/")
	if u, err := url.Parse(baseURL); err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		log.Fatal("BASE_URL must be set to the public origin, for example https://images.example.com")
	}
	store, err := OpenStore("/data")
	if err != nil {
		log.Fatal(err)
	}
	app := &App{store: store, baseURL: baseURL}
	posts, err := app.RebuildAll()
	if err != nil {
		log.Fatal(err)
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	log.Printf("listening on %s with %d posts", addr, posts)
	log.Fatal(srv.ListenAndServe())
}
