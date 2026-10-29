package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"

	_ "github.com/mattn/go-sqlite3"
)

type StudyGroup struct {
	Name             string
	ShortDescription string
	LongDescription  string
}

type Interest struct {
	ID   int
	Name string
}

type InterestSearchResults struct {
	Interests []Interest
}

func isHTMXRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func main() {
	db, err := sql.Open("sqlite3", "./database.sqlite3")

	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	basetemplate := template.Must(template.ParseFiles("./templates/base.html"))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(basetemplate.Clone())
		tmpl = template.Must(tmpl.ParseFiles("./templates/home.html"))

		tmpl.ExecuteTemplate(w, "base", nil)
	})

	http.HandleFunc("/about/", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(basetemplate.Clone())
		tmpl = template.Must(tmpl.ParseFiles("./templates/about.html"))

		tmpl.ExecuteTemplate(w, "base", nil)
	})

	http.HandleFunc("/studygroups/", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(basetemplate.Clone())
		tmpl = template.Must(tmpl.ParseFiles("./templates/search.html"))

		// this template has a search bar
		// grab its query!
		query := r.FormValue("query")

		var matches []StudyGroup

		// search everything ever using query
		//
		// theyre calling it the worst optimized query of all time
		//
		// theyre also asking about when maeve has ever cared about optimization
		// rows, err := db.Query("SELECT Name, ShortDescription, LongDescription FROM StudyGroups WHERE LOWER(Name) LIKE LOWER('%?%') OR LOWER(ShortDescription) LIKE LOWER('%?%') OR LOWER(LongDescription) LIKE LOWER('%?%')", query, query, query)
		rows, err := db.Query("SELECT Name, ShortDescription, LongDescription FROM StudyGroups "+
			"WHERE LOWER(Name) LIKE LOWER('%' || ? || '%')"+
			"OR LOWER(ShortDescription) LIKE LOWER('%' || ? || '%')"+
			"OR LOWER(LongDescription) LIKE LOWER('%' || ? || '%')", query, query, query)
		if err != nil {
			log.Fatal(err)

		}
		defer rows.Close()

		for rows.Next() {

			var group StudyGroup

			// driver cant bind NULL to string so we use this special type to error check it and do what we want
			var longdesc sql.NullString

			err = rows.Scan(&group.Name, &group.ShortDescription, &longdesc)
			if err != nil {
				log.Fatal(err)
			}

			if longdesc.Valid {
				// valid long description should just be the description
				group.LongDescription = longdesc.String
			} else {
				// null description should just be empty
				group.LongDescription = ""

			}

			matches = append(matches, group)
		}

		// this is checked last in the example I read
		// (https://github.com/mattn/go-sqlite3/blob/v1.14.52/_example/simple/simple.go)
		// im not sure if theres a reason for this going after rows.Next
		err = rows.Err()
		if err != nil {
			log.Fatal(err)
		}

		type SearchStudyGroups struct {
			Placeholder string
			Groups      []StudyGroup
		}

		// send the whole page by default
		template := "base"

		if isHTMXRequest(r) {
			// send only the partial if request sent by htmx
			template = "rows"
		}

		// do not cache anything with HX-Request present
		w.Header().Add("Vary", "HX-Request")

		tmpl.ExecuteTemplate(w, template, &SearchStudyGroups{
			Placeholder: "search groups here",
			Groups:      matches})
	})

	http.HandleFunc("/interests/", func(w http.ResponseWriter, r *http.Request) {
    // build this page on top of the normal site layout
    tmpl := template.Must(basetemplate.Clone())
    tmpl = template.Must(tmpl.ParseFiles("./templates/interests.html"))

    // grab whatever was typed into the search box
    query := r.FormValue("query")

    // find interest names that contain the search text
    rows, err := db.Query(
        "SELECT Id, Name FROM Interests "+
            "WHERE LOWER(Name) LIKE LOWER('%' || ? || '%') "+
            "ORDER BY Name",
        query,
    )
    if err != nil {
        log.Printf("interest search query failed: %v", err)
        http.Error(w, "Could not search interests", http.StatusInternalServerError)
        return
    }
    defer rows.Close()

    // make a list for the template to display
    results := InterestSearchResults{}
    for rows.Next() {
        var interest Interest
        // copy this database row into an Interest
        if err := rows.Scan(&interest.ID, &interest.Name); err != nil {
            log.Printf("could not read interest row: %v", err)
            http.Error(w, "Could not read interests", http.StatusInternalServerError)
            return
        }

        results.Interests = append(results.Interests, interest)
    }

    // check whether an error happened while reading the rows
    if err := rows.Err(); err != nil {
        log.Printf("interest search rows failed: %v", err)
        http.Error(w, "Could not read interests", http.StatusInternalServerError)
        return
    }
    page := "base"
    if isHTMXRequest(r) {
        page = "interest-rows"
    }

    // tell caches that the response changes based on the htmx header
    w.Header().Add("Vary", "HX-Request")

    // render the page or just the matching rows
    if err := tmpl.ExecuteTemplate(w, page, results); err != nil {
        log.Printf("could not render interests: %v", err)
    }
})

	fs := http.FileServer(http.Dir("static/"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	port := "6767"

	fmt.Printf("starting server, listening on: %s\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}
