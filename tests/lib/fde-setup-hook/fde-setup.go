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
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

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

	Key []byte `json:"key,omitempty"`
}

type fdeSetupResultJSON struct {
	// XXX call this encrypted-key if possible?
	EncryptedKey []byte `json:"sealed-key"`
	Handle       []byte `json:"handle"`
}

// Note that in real implementations this would be something like an
// internal handle for the crypto hardware and generated in "initial-setup"
// for each key
var testKeyHandle = []byte(`{"some":"json-handle"}`)

var (
	// used in tests
	osStdin  = io.Reader(os.Stdin)
	osStdout = io.Writer(os.Stdout)
)

// Note that this can be removed when using the hook as an example for
// how to implement your own hook, the below Base64 is here so that
// we can test that strict base64 is used.
//
// This is the same as fdeSetupJSON, but is more strict in that it decodes Key
// as a string, which _must_ be a base64 encoded version of the same []byte Key
// we have above, the handler below validates this as a test
type fdeSetupJSONStrictBase64 struct {
	Key string `json:"key,omitempty"`
}

func runFdeSetup() error {
	fromInitrd := osutil.FileExists("/etc/initrd-release")

	var input []byte

	if fromInitrd {
		input = mylog.Check2(io.ReadAll(os.Stdin))
	} else {
		input = mylog.Check2(exec.Command("snapctl", "fde-setup-request").CombinedOutput())
	}

	var js fdeSetupJSON
	mylog.Check(json.Unmarshal(input, &js))

	var jsStrict fdeSetupJSONStrictBase64
	mylog.Check(json.Unmarshal(input, &jsStrict))

	// verify that the two de-coding mechanisms agree on the key, manually
	// decoding the base64 string in the stricter case
	decodedBase64Key := mylog.Check2(base64.StdEncoding.DecodeString(jsStrict.Key))

	if !bytes.Equal(decodedBase64Key, js.Key) {
		return fmt.Errorf("fde-setup-request key is not strictly the same base64 decoded as binary decoded")
	}

	var fdeSetupResult []byte
	switch js.Op {
	case "features":
		fdeSetupResult = []byte(`{"features":[]}`)
		if osutil.FileExists(filepath.Join(filepath.Dir(os.Args[0]), "enable-ice-support")) {
			fdeSetupResult = []byte(`{"features":["inline-crypto-engine"]}`)
		}
	case "initial-setup":
		// "seal" using a really bad crypto algorithm
		res := fdeSetupResultJSON{
			EncryptedKey: xor13(js.Key),
			Handle:       testKeyHandle,
		}
		fdeSetupResult = mylog.Check2(json.Marshal(res))

	default:
		return fmt.Errorf("unsupported op %q", js.Op)
	}

	if fromInitrd {
		os.Stdout.Write(fdeSetupResult)
	} else {
		cmd := exec.Command("snapctl", "fde-setup-result")
		// simulate a secboot v1 encrypted key
		cmd.Stdin = bytes.NewBuffer(fdeSetupResult)
		output := mylog.Check2(cmd.CombinedOutput())

	}

	return nil
}

type fdeRevealJSON struct {
	Op string `json:"op"`

	SealedKey []byte `json:"sealed-key"`
	Handle    []byte `json:"handle"`
}

type fdeRevealJSONStrict struct {
	SealedKey string `json:"sealed-key"`
	Handle    string `json:"handle"`
}

type fdeRevealKeyResultJSON struct {
	Key []byte `json:"key"`
}

func runFdeRevealKey() error {
	var js fdeRevealJSON
	var jsStrict fdeRevealJSONStrict

	b := mylog.Check2(io.ReadAll(osStdin))
	mylog.Check(json.Unmarshal(b, &js))
	mylog.Check(json.Unmarshal(b, &jsStrict))

	// verify that the two de-coding mechanisms agree on the key, manually
	// decoding the base64 string in the stricter case
	decodedBase64Key := mylog.Check2(base64.StdEncoding.DecodeString(jsStrict.SealedKey))

	if !bytes.Equal(decodedBase64Key, js.SealedKey) {
		return fmt.Errorf("fde-reveal-key key input is not strictly the same base64 decoded as binary decoded")
	}
	decodedBase64Handle := mylog.Check2(base64.StdEncoding.DecodeString(jsStrict.Handle))

	if !bytes.Equal(decodedBase64Handle, js.Handle) {
		return fmt.Errorf("fde-reveal-key handle input is not strictly the same base64 decoded as binary decoded")
	}

	switch js.Op {
	case "reveal":
		// check that the handle created in initial-setup is passed
		// back to reveal correctly.
		if string(js.Handle) != string(testKeyHandle) {
			return fmt.Errorf(`fde-reveal-key expected handle %q but got %q`, testKeyHandle, js.Handle)
		}
		// "decrypt" key
		var res fdeRevealKeyResultJSON
		res.Key = xor13(js.SealedKey)
		mylog.Check(json.NewEncoder(osStdout).Encode(res))

	case "lock":
		// NOTE: when using this file as an example code for
		// implementing a real world, production grade FDE
		// hook, the lock operation must be implemented here
		// to block decryption operations. This example does
		// nothing.
	default:
		return fmt.Errorf(`unsupported operation %q`, js.Op)
	}

	return nil
}

func main() {
	// XXX: workaround systemd bug
	// https://bugs.launchpad.net/ubuntu/+source/systemd/+bug/1921145
	time.Sleep(1 * time.Second)

	switch filepath.Base(os.Args[0]) {
	case "fde-setup":
		mylog.
			// run as regular hook
			Check(runFdeSetup())
	case "fde-reveal-key":
		mylog.
			// run from initrd
			Check(runFdeRevealKey())
	default:
		mylog.Check(fmt.Errorf("binary needs to be called as fde-setup or fde-reveal-key"))
	}
}
