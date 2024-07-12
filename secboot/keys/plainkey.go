//go:build !nosecboot
package keys

import (
	"crypto/rand"
	"io"
	"os"
	"path/filepath"

	sb "github.com/snapcore/secboot"
	sb_plainkey "github.com/snapcore/secboot/plainkey"

	"github.com/snapcore/snapd/osutil"
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

func (key PlatformKey) SaveToFile(path string) (error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(path, key[:], 0600, 0)
}

type PlainKey struct {
	keyData *sb.KeyData
}

func (key PlatformKey) CreateProtectedKey() (*PlainKey, []byte, error) {
	protectedKey, /*primaryKey*/_, unlockKey, err := sb_plainkey.NewProtectedKey(rand.Reader, key[:], nil)
	return &PlainKey{protectedKey}, unlockKey, err
}

type KeyDataWriter interface {
	io.Writer
	Commit() error
}

func (key *PlainKey) Write(writer KeyDataWriter) error {
	return key.keyData.WriteAtomic(writer)
}
