package opteetest

// MockClient can be used to mock the implementation of the FDE TA client.
type MockClient struct {
	FDETAPresentFn func() bool
	DecryptKeyFn   func(input []byte, handle []byte) ([]byte, error)
	EncryptKeyFn   func(input []byte) (handle []byte, sealed []byte, err error)
	LockTAFn       func() error
	VersionFn      func() (string, error)
}

func (m *MockClient) FDETAPresent() bool {
	if m.FDETAPresentFn == nil {
		panic("unexpected call to FDETAPresent")
	}
	return m.FDETAPresentFn()
}

func (m *MockClient) DecryptKey(input []byte, handle []byte) ([]byte, error) {
	if m.DecryptKeyFn == nil {
		panic("unexpected call to DecryptKey")
	}
	return m.DecryptKeyFn(input, handle)
}

func (m *MockClient) EncryptKey(input []byte) (handle []byte, sealed []byte, err error) {
	if m.EncryptKeyFn == nil {
		panic("unexpected call to EncryptKey")
	}
	return m.EncryptKeyFn(input)
}

func (m *MockClient) LockTA() error {
	if m.LockTAFn == nil {
		panic("unexpected call to LockTA")
	}
	return m.LockTAFn()
}

func (m *MockClient) Version() (string, error) {
	if m.VersionFn == nil {
		panic("unexpected call to Version")
	}
	return m.VersionFn()
}
