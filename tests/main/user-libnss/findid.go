package main

import (
	"fmt"
	"log"
	"os"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

func main() {
	fnName := os.Args[1]
	userOrGroupName := os.Args[2]

	var fn func(string) (uint64, error)
	switch fnName {
	case "uid":
		fn = osutil.FindUid
	case "gid":
		fn = osutil.FindGid
	default:
		log.Fatalf("unknown fnName: %q", fnName)
	}
	id := mylog.Check2(fn(userOrGroupName))

	fmt.Println(id)
}
