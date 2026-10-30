package main

import (
	"cmp"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
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

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           app.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("starting server, listening on: %s (database %s)", port, dbPath)
	log.Fatal(srv.ListenAndServe())
}
