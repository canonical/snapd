//go:build !nosecboot

package main

import (
	"fmt"
	"os"

	"github.com/canonical/go-tpm2"
	"github.com/canonical/go-tpm2/linux"
	"github.com/ddkwork/golibrary/mylog"
)

func run() error {
	tcti := mylog.Check2(linux.OpenDevice("/dev/tpm0"))

	tpm := tpm2.NewTPMContext(tcti)
	defer tpm.Close()

	v := mylog.Check2(tpm.GetCapabilityTPMProperty(tpm2.PropertyLockoutCounter))

	fmt.Printf("lockout counter value: %v\n", v)

	return nil
}

func main() {
	mylog.Check(run())
}
