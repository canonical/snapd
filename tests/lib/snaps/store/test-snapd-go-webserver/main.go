package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/ddkwork/golibrary/mylog"
)

func main() {
	http.HandleFunc("/", handleMainPage)

	log.Println("Starting webserver on :8081")
	mylog.Check(http.ListenAndServe(":8081", nil))
}

func handleMainPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	fmt.Fprintf(w, "Hello World\n")
}
