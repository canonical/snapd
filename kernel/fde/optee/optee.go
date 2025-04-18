package optee

import (
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

var (
	newClient = newOPTEEClient
)

// Client represents an interface to our FDE trusted application.
type Client interface {
	// FDETAPresent returns true if the FDE TA is present.
	FDETAPresent() bool

	// DecryptKey requests that the FDE TA decrypt the given key, using the
	// handle as supplimentary information. The decrypted key is returned.
	DecryptKey(input []byte, handle []byte) ([]byte, error)

	// EncryptKey requests that the FDE TA encrypt the given key. A handle and
	// the encrypted key are returned.
	EncryptKey(input []byte) (handle []byte, sealed []byte, err error)

	// LockTA requests that the FDE TA be locked. This will prevent it from
	// being used further.
	LockTA() error

	// Version returns the version of the FDE TA.
	Version() (string, error)
}

// NewClient returns a new [Client].
func NewClient() Client {
	return newClient()
}

// MockClient mocks the function called by [NewClient]. Should only be used in
// tests.
func MockNewClient(c Client) (restore func()) {
	osutil.MustBeTestBinary("can only mock optee client in tests")
	return testutil.Mock(&newClient, func() Client {
		return c
	})
}
