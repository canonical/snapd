package main

import (
	"log"
	"os"

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
	id, err := fn(userOrGroupName)
	if err != nil {
		log.Fatalf("fn failed: %q", err)
	}
	println(id)
}
