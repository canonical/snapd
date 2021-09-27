package main

import (
	"fmt"
	"os"

	"github.com/snapcore/snapd/osutil/disks"
)

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		die(fmt.Errorf("usage: %s mount-point", os.Args[0]))
	}

	size, err := disks.Size(os.Args[1])
	if err != nil {
		die(err)
	}

	fmt.Printf("%v\n", size)
}
