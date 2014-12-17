package main

import (
	"fmt"
	"os"

	// my own stuff
	"snappy"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <command>\n", os.Args[0])
		os.Exit(1)
	}
	cmd := os.Args[1]

	err := snappy.CommandDispatch(cmd, os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr,
		"ERROR: command %s failed: %s\n",
		cmd, err)
		os.Exit(1)
	}

	os.Exit(0)
}
