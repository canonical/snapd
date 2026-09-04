//go:build !nosecboot

package main

import (
	"fmt"
	"os"

	"github.com/canonical/go-tpm2"
	"github.com/canonical/go-tpm2/linux"
)

func run() error {
	tcti, err := linux.OpenDevice("/dev/tpm0")
	if err != nil {
		return fmt.Errorf("cannot open TPM device: %v", err)
	}
	tpm := tpm2.NewTPMContext(tcti)
	defer tpm.Close()

	v, err := tpm.GetCapabilityTPMProperty(tpm2.PropertyLockoutCounter)
	if err != nil {
		return fmt.Errorf("cannot obtain lockout counter value: %v", err)
	}
	fmt.Printf("lockout counter value: %v\n", v)

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
