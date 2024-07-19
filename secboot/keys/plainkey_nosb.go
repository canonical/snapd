//go:build nosecboot

package keys

import ()

const (
	PlatformKeySize = 32
)

var errBuildWithoutSecboot = errors.New("build without secboot support")

type PlatformKey []byte

func NewPlatformKey() (PlatformKey, error) {
	key := make(PlatformKey, PlatformKeySize)
	_, err := rand.Read(key[:])
	return key, err
}

type PlainKey struct {
}

func (key PlatformKey) CreateProtectedKey() (*PlainKey, []byte, error) {
	return &PlainKey{}, []byte{}, errBuildWithoutSecboot
}

type KeyDataWriter interface {
	io.Writer
	Commit() error
}

func (key *PlainKey) Write(writer KeyDataWriter) error {
	return errBuildWithoutSecboot
}
