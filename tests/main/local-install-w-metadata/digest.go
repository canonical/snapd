package main

import (
	"fmt"
	"os"

	"github.com/snapcore/snapd/asserts"
)

func main() {
	sha3_384, _, err := asserts.SnapFileSHA3_384(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot compute digest: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "%s\n", sha3_384)
}
