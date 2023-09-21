package main

import (
	"fmt"
	"log"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/osutil"
)

const shimPath string = "/boot/efi/EFI/ubuntu/shimx64.efi"
const bootPath string = "/boot/efi/EFI/BOOT/BOOTX64.efi"

func uefiLoadOptionParameters() (description string, assetPath string, optionalData []byte, err error) {
	if osutil.FileExists(shimPath) {
		assetPath = shimPath
	} else if osutil.FileExists(bootPath) {
		assetPath = bootPath
	} else {
		return "", "", nil, fmt.Errorf("cannot find boot or shim EFI binary")
	}
	return "spread-test-var", assetPath, make([]byte, 0), nil
}

func main() {
	description, assetPath, optionalData, err := uefiLoadOptionParameters()
	if err != nil {
		log.Fatalf("%v", err)
	}

	err = boot.SetEfiBootVariables(description, assetPath, optionalData)
	if err != nil {
		log.Fatalf("cannot set EFI boot variables: %q", err)
	}
}
