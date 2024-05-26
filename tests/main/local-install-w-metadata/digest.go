package main

import (
	"fmt"
	"os"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
)

func main() {
	sha3_384, _ := mylog.Check3(asserts.SnapFileSHA3_384(os.Args[1]))

	fmt.Fprintf(os.Stdout, "%s\n", sha3_384)
}
