package main

import (
	"fmt"
	"os"

	"github.com/snapcore/snapd/osutil/sys"
)

func main() {
	expected := sys.UserID((1 << 32) - 2)
	if uid := sys.Getuid(); uid != expected {
		fmt.Printf("*** getuid() returned %d (expecting %d)\n", uid, expected)
		os.Exit(1)
	}
}
