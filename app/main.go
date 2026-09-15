package main

import (
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/up", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/aboutPage.html", http.StatusSeeOther)
	})

	fs := http.FileServer(http.Dir("static/"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	const port = "6767"
	log.Printf("starting server, listening on: %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
