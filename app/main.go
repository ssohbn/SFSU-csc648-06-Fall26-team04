package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
)

func main() {
	tmpl := template.Must(template.ParseFiles("./static/templates/base.html",
		"./static/templates/footer.html",
		"./static/templates/header.html",
	))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		type Page struct {
			Body string
		}

		err := tmpl.Execute(w, &Page{Body: "<h1>hi</h1>"})

		if err != nil {
			fmt.Printf("err: %v\n", err)

		}
		// http.Redirect(w, r, "/static/aboutPage.html", http.StatusSeeOther)
	})

	fs := http.FileServer(http.Dir("static/"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	port := "6767"

	fmt.Printf("starting server, listening on: %s\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}
