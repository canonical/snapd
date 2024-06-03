package main

import (
	"fmt"
	"log"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/osutil"
)

var possibleAssets = []string{
	"/boot/efi/EFI/ubuntu/shimx64.efi",
	"/boot/efi/EFI/BOOT/BOOTX64.efi",
	"/boot/efi/EFI/ubuntu/shimaa64.efi",
	"/boot/efi/EFI/BOOT/BOOTAA64.efi",
}

func uefiLoadOptionParameters() (description string, assetPath string, optionalData []byte, err error) {
	for _, assetPath = range possibleAssets {
		if osutil.FileExists(assetPath) {
			return "spread-test-var", assetPath, make([]byte, 0), nil
		}
	}
	return "", "", nil, fmt.Errorf("cannot find boot or shim EFI binary")
}

func main() {
	description, assetPath, optionalData, err := uefiLoadOptionParameters()
	if err != nil {
		log.Fatalf("%v", err)
	}

	err = boot.SetEfiBootVariables(description, assetPath, optionalData)
	if err != nil {
		log.Fatalf("cannot set EFI boot variables for asset path '%s': %q", assetPath, err)
	}
}
