package main

import (
	"cmp"
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	seedDemo := flag.Bool("seed", false, "add demo accounts and groups if the database has no accounts yet")
	flag.Parse()
	dbPath := cmp.Or(os.Getenv("DB_PATH"), "studysync.sqlite3")
	port := cmp.Or(os.Getenv("PORT"), "6767")

	db, err := openDB(dbPath)
	if err != nil {
		log.Fatalf("open database %s: %v", dbPath, err)
	}
	defer db.Close()

	app, err := newApp(db)
	if err != nil {
		log.Fatalf("load templates: %v", err)
	}
	if *seedDemo {
		if err := seed(context.Background(), app.store); err != nil {
			log.Fatalf("seed: %v", err)
		}
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           app.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	log.Printf("starting server, listening on: %s (database %s)", port, dbPath)
	log.Fatal(srv.ListenAndServe())
}
