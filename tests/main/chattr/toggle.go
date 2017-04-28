package main

import (
	"fmt"
	"os"

	"github.com/snapcore/snapd/osutil"
)

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		die(fmt.Errorf("usage: %s file", os.Args[0]))
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		die(err)
	}

	before, err := osutil.GetAttr(f)
	if err != nil {
		die(err)
	}

	err = osutil.SetAttr(f, before^osutil.FS_IMMUTABLE_FL)
	if err != nil {
		die(err)
	}

	after, err := osutil.GetAttr(f)
	if err != nil {
		die(err)
	}

	if before&osutil.FS_IMMUTABLE_FL != 0 {
		fmt.Print("immutable")
	} else {
		fmt.Print("mutable")
	}
	fmt.Print(" -> ")
	if after&osutil.FS_IMMUTABLE_FL != 0 {
		fmt.Println("immutable")
	} else {
		fmt.Println("mutable")
	}
}
