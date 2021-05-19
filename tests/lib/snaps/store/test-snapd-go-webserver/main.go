package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", handleMainPage)

	log.Println("Starting webserver on :8081")
	if err := http.ListenAndServe(":8081", nil); err != nil {
		log.Fatalf("http.ListendAndServer() failed with %s\n", err)
	}
}

func handleMainPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	fmt.Fprintf(w, "Hello World\n")
}
