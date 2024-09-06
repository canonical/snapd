//go:build nosecboot

package keys

import (
	"crypto/rand"
	"io"
)

const (
	PlatformKeySize = 32
)

type PlatformKey []byte

func NewPlatformKey() (PlatformKey, error) {
	key := make(PlatformKey, PlatformKeySize)
	_, err := rand.Read(key[:])
	return key, err
}

type PlainKey struct {
}

func (key PlatformKey) SaveToFile(path string) error {
	return nil
}

func (key PlatformKey) CreateProtectedKey() (*PlainKey, []byte, error) {
	return &PlainKey{}, []byte{}, nil
}

type KeyDataWriter interface {
	io.Writer
	Commit() error
}

func (key *PlainKey) Write(writer KeyDataWriter) error {
	return nil
}
