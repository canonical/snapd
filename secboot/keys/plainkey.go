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
	ProtectorKeySize = 32
)

type ProtectorKey []byte

func NewProtectorKey() (ProtectorKey, error) {
	key := make(ProtectorKey, ProtectorKeySize)
	_, err := rand.Read(key[:])
	return key, err
}

func (key ProtectorKey) SaveToFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(path, key[:], 0600, 0)
}

type PlainKey struct {
	keyData *sb.KeyData
}

func (key ProtectorKey) CreateProtectedKey(primaryKey []byte) (*PlainKey, []byte, []byte, error) {
	protectedKey, generatedPK, unlockKey, err := sb_plainkey.NewProtectedKey(rand.Reader, key[:], primaryKey)
	return &PlainKey{protectedKey}, generatedPK, unlockKey, err
}

type KeyDataWriter interface {
	io.Writer
	Commit() error
}

func (key *PlainKey) Write(writer KeyDataWriter) error {
	return key.keyData.WriteAtomic(writer)
}
