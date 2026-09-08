package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/canonical/go-tpm2"
	"github.com/canonical/go-tpm2/linux"
)

// driftNVIndex is a throwaway NV index used solely to generate failed
// authorizations that bump the TPM DA lockout counter out of band.
const driftNVIndex = tpm2.Handle(0x018fff00)

func openTPM() (*tpm2.TPMContext, error) {
	tcti, err := linux.OpenDevice("/dev/tpm0")
	if err != nil {
		return nil, fmt.Errorf("cannot open TPM device: %v", err)
	}
	return tpm2.NewTPMContext(tcti), nil
}

func printCounter(tpm *tpm2.TPMContext) error {
	v, err := tpm.GetCapabilityTPMProperty(tpm2.PropertyLockoutCounter)
	if err != nil {
		return fmt.Errorf("cannot obtain lockout counter value: %v", err)
	}
	fmt.Printf("lockout counter value: %v\n", v)

	return nil
}

// bumpCounter drifts the real TPM DA lockout counter by n. It defines a
// DA-protected NV index (owner hierarchy auth is empty on snapd-provisioned
// TPMs) and then issues n writes with a deliberately wrong authorization
// value. Each failed authorization increments the hardware lockout counter,
// letting the test create a divergence between snapd's cached token bucket and
// the real counter that only a genuine hardware re-read can reconcile.
func bumpCounter(tpm *tpm2.TPMContext, n int) error {
	owner := tpm.OwnerHandleContext()

	// best-effort cleanup of a leftover index from an interrupted run
	if existing, err := tpm.NewResourceContext(driftNVIndex); err == nil {
		tpm.NVUndefineSpace(owner, existing, nil)
	}

	pub := &tpm2.NVPublic{
		Index:   driftNVIndex,
		NameAlg: tpm2.HashAlgorithmSHA256,
		Attrs:   tpm2.NVTypeOrdinary.WithAttrs(tpm2.AttrNVAuthWrite | tpm2.AttrNVAuthRead),
		Size:    8,
	}
	index, err := tpm.NVDefineSpace(owner, []byte("correct-auth"), pub, nil)
	if err != nil {
		return fmt.Errorf("cannot define NV index: %v", err)
	}
	defer tpm.NVUndefineSpace(owner, index, nil)

	for i := 0; i < n; i++ {
		index.SetAuthValue([]byte("wrong-auth"))
		if err := tpm.NVWrite(index, index, make([]byte, 8), 0, nil); err == nil {
			return fmt.Errorf("expected authorization failure on attempt %d, got success", i+1)
		}
	}

	return nil
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: %s <get|bump <count>>", os.Args[0])
	}

	tpm, err := openTPM()
	if err != nil {
		return err
	}
	defer tpm.Close()

	switch os.Args[1] {
	case "get":
		if len(os.Args) != 2 {
			return fmt.Errorf("usage: %s get", os.Args[0])
		}
		return printCounter(tpm)
	case "bump":
		if len(os.Args) != 3 {
			return fmt.Errorf("usage: %s bump <count>", os.Args[0])
		}
		n, err := strconv.Atoi(os.Args[2])
		if err != nil || n < 1 {
			return fmt.Errorf("invalid bump count: %q", os.Args[2])
		}
		return bumpCounter(tpm, n)
	default:
		return fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
