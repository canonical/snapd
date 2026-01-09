// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build nosecboot

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

	"github.com/snapcore/snapd/osutil"
)

const (
	protectorKeySize = 32
)

type ProtectorKey []byte

func NewProtectorKey() (ProtectorKey, error) {
	key := make(ProtectorKey, protectorKeySize)
	_, err := rand.Read(key[:])
	return key, err
}

type PlainKey struct {
}

func (key ProtectorKey) SaveToFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(path, key[:], 0600, 0)
}

func (key ProtectorKey) CreateProtectedKey(primaryKey []byte) (*PlainKey, []byte, []byte, error) {
	return &PlainKey{}, primaryKey, []byte{}, nil
}

type KeyDataWriter interface {
	io.Writer
	Commit() error
}

func (key *PlainKey) Write(writer KeyDataWriter) error {
	return writer.Commit()
}
