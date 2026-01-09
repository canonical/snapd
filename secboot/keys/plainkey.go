// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2024 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

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
	protectorKeySize = 32
)

var (
	sbNewProtectedKey = sb_plainkey.NewProtectedKey
)

// ProtectorKey is a key that can be used to protect "plainkey" keys.
type ProtectorKey []byte

// NewProtectorKey creates a new random ProtectorKey.
func NewProtectorKey() (ProtectorKey, error) {
	key := make(ProtectorKey, protectorKeySize)
	_, err := rand.Read(key[:])
	return key, err
}

// SaveToFile saves the ProtectorKey to a file at given path.
func (key ProtectorKey) SaveToFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(path, key[:], 0600, 0)
}

// PlainKey is a wrapper for a secboot KeyData representing a plainkey.
type PlainKey struct {
	keyData *sb.KeyData
}

// CreateProtectedKey creates a protected key for a given ProtectorKey
// and primary key. It returns a the protected key wrapped as a PlainKey
// as well the used primary key and the unlock key.
// If primaryKey is nil, the primary key will be generated.
func (key ProtectorKey) CreateProtectedKey(primaryKey []byte) (*PlainKey, []byte, []byte, error) {
	protectedKey, generatedPK, unlockKey, err := sbNewProtectedKey(rand.Reader, key[:], primaryKey)
	return &PlainKey{protectedKey}, generatedPK, unlockKey, err
}

// KeyDataWriter is a the same as KeyDataWriter from
// github.com/canonical/secboot.
type KeyDataWriter interface {
	io.Writer
	Commit() error
}

// Write writes a PlainKey to a KeyDataWriter.
func (key *PlainKey) Write(writer KeyDataWriter) error {
	return key.keyData.WriteAtomic(writer)
}
