//go:build nosecboot

package keys

import (
	"crypto/rand"
	"io"
)

const (
	ProtectorKeySize = 32
)

type ProtectorKey []byte

func NewProtectorKey() (ProtectorKey, error) {
	key := make(ProtectorKey, ProtectorKeySize)
	_, err := rand.Read(key[:])
	return key, err
}

type PlainKey struct {
}

func (key ProtectorKey) SaveToFile(path string) error {
	return nil
}

func (key ProtectorKey) CreateProtectedKey() (*PlainKey, []byte, error) {
	return &PlainKey{}, []byte{}, nil
}

type KeyDataWriter interface {
	io.Writer
	Commit() error
}

func (key *PlainKey) Write(writer KeyDataWriter) error {
	return nil
}
