package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/snapcore/snapd/boot/fdehook"
)

func main() {
	stdin, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		panic(err)
	}
	params := &fdehook.UnlockParams{
		SealedKey:        stdin,
		VolumeName:       "some-volume-name",
		SourceDevicePath: "some-source-device-path",
		LockKeysOnFinish: true,
	}
	unsealedKey, err := fdehook.Unlock(os.Args[1], params)
	if err != nil {
		fmt.Printf("unlock error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("unsealed key: %q\n", unsealedKey)
}
