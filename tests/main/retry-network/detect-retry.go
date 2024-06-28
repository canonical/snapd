package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
)

func main() {
	logger.SimpleSetup(nil)
	if len(os.Args) < 2 {
		fmt.Println("need url as first argument")
		os.Exit(1)
	}

	_, err := http.Get(os.Args[1])
	fmt.Printf("ShouldRetryError: %v\n", httputil.ShouldRetryError(err))
	fmt.Printf("NoNetwork: %v\n", httputil.NoNetwork(err))
}
