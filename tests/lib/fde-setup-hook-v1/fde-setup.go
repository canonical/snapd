package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
)

// DO NOT USE THIS FILE as an example to write fde-hooks. It is just
// here for historic reasons and uses the obsolte v1 hook
// format. Please do not change any of the JSON structs in this file,
// it needs to be kept to ensure we keep compatbility with the
// "denver" project and fde-hooks v1.

// This is a very insecure crypto just for demonstration purposes.
// Please delete it you use this for real.
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

// this is the same as fdeSetupJSON, but is more strict in that it decodes Key
// as a string, which _must_ be a base64 encoded version of the same []byte Key
// we have above, the handler below validates this as a test
type fdeSetupJSONStrictBase64 struct {
	Key string `json:"key,omitempty"`
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

	var jsStrict fdeSetupJSONStrictBase64
	if err := json.Unmarshal(output, &jsStrict); err != nil {
		return err
	}

	// verify that the two de-coding mechanisms agree on the key, manually
	// decoding the base64 string in the stricter case
	decodedBase64Key, err := base64.StdEncoding.DecodeString(jsStrict.Key)
	if err != nil {
		return fmt.Errorf("fde-setup-request is not valid base64: %v", err)
	}
	if !bytes.Equal(decodedBase64Key, js.Key) {
		return fmt.Errorf("fde-setup-request is not strictly the same base64 decoded as binary decoded")
	}

	var fdeSetupResult []byte
	switch js.Op {
	case "features":
		// no special features supported by this hook
		fdeSetupResult = []byte(`{"features":[]}`)
	case "initial-setup":
		// "seal"
		buf := bytes.NewBufferString("USK$")
		buf.Write(xor13(js.Key))
		fdeSetupResult = buf.Bytes()
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

type fdeRevealJSONStrict struct {
	SealedKey string `json:"sealed-key"`
}

func runFdeRevealKey() error {
	var js fdeRevealJSON
	var jsStrict fdeRevealJSONStrict

	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(b, &js); err != nil {
		return err
	}

	if err := json.Unmarshal(b, &jsStrict); err != nil {
		return err
	}

	// verify that the two de-coding mechanisms agree on the key, manually
	// decoding the base64 string in the stricter case
	decodedBase64Key, err := base64.StdEncoding.DecodeString(jsStrict.SealedKey)
	if err != nil {
		return fmt.Errorf("fde-reveal-key input is not valid base64: %v", err)
	}
	if !bytes.Equal(decodedBase64Key, js.SealedKey) {
		return fmt.Errorf("fde-reveal-key input is not strictly the same base64 decoded as binary decoded")
	}

	switch js.Op {
	case "reveal":
		// strip the "USK$" prefix
		unsealedKey := xor13(js.SealedKey[len("USK$"):])
		fmt.Fprintf(os.Stdout, "%s", unsealedKey)
	case "lock":
		// nothing right now

		// NOTE: when using this file as an example code for implementing a real
		// world, production grade FDE hook, the lock operation must be
		// implemented here to block decryption operations
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
