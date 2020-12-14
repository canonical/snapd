package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
)

// super secure crypto
func xor13(bs []byte) []byte {
	out := make([]byte, len(bs))
	for i := range bs {
		out[i] = bs[i] ^ 0x13
	}
	return out
}

// Note that this does not import the snapd structs to ensure we don't
// accidentally break something in the contract and miss that we broke
// it because we use the internal thing "externally" here
type fdeSetupJSON struct {
	Op string `json:"op"`

	Key     []byte `json:"key,omitempty"`
	KeyName string `json:"key-name,omitempty"`

	Models []map[string]string `json:"models,omitempty"`
}

func runFdeSetup() error {
	output, err := exec.Command("snapctl", "fde-setup-request").CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot run snapctl fde-setup-request: %v", osutil.OutputErr(output, err))
	}
	var js fdeSetupJSON
	if err := json.Unmarshal(output, &js); err != nil {
		return err
	}

	var fdeSetupResult []byte
	switch js.Op {
	case "features":
		// no special features supported by this hook
		fdeSetupResult = []byte(`{"features":[]}`)
	case "initial-setup":
		// "seal"
		fdeSetupResult = xor13(js.Key)
	default:
		return fmt.Errorf("unsupported op %q", js.Op)
	}
	cmd := exec.Command("snapctl", "fde-setup-result")
	cmd.Stdin = bytes.NewBuffer(fdeSetupResult)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot run snapctl fde-setup-result for op %q: %v", js.Op, osutil.OutputErr(output, err))
	}
	return nil
}

type fdeRevealJSON struct {
	Op string `json:"op"`

	SealedKey []byte `json:"sealed-key"`
}

func runFdeRevealKey() error {
	var js fdeRevealJSON

	if err := json.NewDecoder(os.Stdin).Decode(&js); err != nil {
		return err
	}

	switch js.Op {
	case "reveal":
		// "unseal"
		unsealedKey := xor13(js.SealedKey)
		fmt.Fprintf(os.Stdout, "%s", unsealedKey)
	case "lock":
		// nothing right now
	case "features":
		// XXX: Not used right now but might in the future?
		fmt.Fprintf(os.Stdout, `{"features":[]}`)
	default:
		return fmt.Errorf(`unsupported operations %q`, js.Op)
	}

	return nil
}

func main() {
	var err error

	switch filepath.Base(os.Args[0]) {
	case "fde-setup":
		// run as regular hook
		err = runFdeSetup()
	case "fde-reveal-key":
		// run from initrd
		err = runFdeRevealKey()
	default:
		err = fmt.Errorf("binary needs to be called as fde-setup or fde-reveal-key")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
