package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/snapcore/snapd/boot/fdehelper"
)

func main() {
	stdin, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		panic(err)
	}
	params := &fdehelper.RevealParams{
		SealedKey:        stdin,
		VolumeName:       "some-volume-name",
		SourceDevicePath: "some-source-device-path",
	}
	revealedKey, err := fdehelper.Reveal(os.Args[1], params)
	if err != nil {
		fmt.Printf("reveal error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("revealed key: %q\n", revealedKey)
}
