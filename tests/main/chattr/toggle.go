package main

import (
	"fmt"
	"os"

	"github.com/ddkwork/golibrary/mylog"
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

	f := mylog.Check2(os.Open(os.Args[1]))

	before := mylog.Check2(osutil.GetAttr(f))
	mylog.Check(osutil.SetAttr(f, before^osutil.FS_IMMUTABLE_FL))

	after := mylog.Check2(osutil.GetAttr(f))

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
