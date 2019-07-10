package fdeutils

import (
	"fmt"
)

func ProvisionTPM(lockoutAuth []byte) error {
	return fmt.Errorf("ProvisionTPM not implemented on arm64")
}

func RequestTPMClearUsingPPI() error {
	return fmt.Errorf("RequestTPMClearUsingPPI not implemented on arm64")
}
