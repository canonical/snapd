package optee

import (
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

var (
	newClient = newOPTEEClient
)

type Client interface {
	FDETAPresent() bool
	DecryptKey(input []byte, handle []byte) ([]byte, error)
	EncryptKey(input []byte) (handle []byte, sealed []byte, err error)
	LockTA() error
	Version() (string, error)
}

func NewClient() Client {
	return newClient()
}

type MockClient struct {
	FDETAPresentFn func() bool
	DecryptKeyFn   func(input []byte, handle []byte) ([]byte, error)
	EncryptKeyFn   func(input []byte) (handle []byte, sealed []byte, err error)
	LockTAFn       func() error
	VersionFn      func() (string, error)
}

func (m *MockClient) FDETAPresent() bool {
	if m.FDETAPresentFn == nil {
		panic("unexpected call")
	}
	return m.FDETAPresentFn()
}

func (m *MockClient) DecryptKey(input []byte, handle []byte) ([]byte, error) {
	if m.DecryptKeyFn == nil {
		panic("unexpected call")
	}
	return m.DecryptKeyFn(input, handle)
}

func (m *MockClient) EncryptKey(input []byte) (handle []byte, sealed []byte, err error) {
	if m.EncryptKeyFn == nil {
		panic("unexpected call")
	}
	return m.EncryptKeyFn(input)
}

func (m *MockClient) LockTA() error {
	if m.LockTAFn == nil {
		panic("unexpected call")
	}
	return m.LockTAFn()
}

func (m *MockClient) Version() (string, error) {
	if m.VersionFn == nil {
		panic("not implemented")
	}
	return m.VersionFn()
}

func MockNewClient(c Client) (restore func()) {
	osutil.MustBeTestBinary("can only mock optee client in tests")
	return testutil.Mock(&newClient, func() Client {
		return c
	})
}
