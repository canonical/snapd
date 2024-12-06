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

package keys_test

import (
	"bytes"
	"crypto"
	"io"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/secboot/keys"
)

type plainkeySuite struct {
}

var _ = Suite(&plainkeySuite{})

func (s *plainkeySuite) SetUpTest(c *C) {
}

type MyKeyDataWriter struct {
	*bytes.Buffer
}

func NewMyKeyDataWriter() *MyKeyDataWriter {
	return &MyKeyDataWriter{
		Buffer: bytes.NewBuffer([]byte{}),
	}
}

func (kdw *MyKeyDataWriter) Commit() error {
	return nil
}

func (s *plainkeySuite) TestPlainKey(c *C) {
	restore := keys.MockSbNewProtectedKey(func(rand io.Reader, protectorKey []byte, primaryKey sb.PrimaryKey) (protectedKey *sb.KeyData, primaryKeyOut sb.PrimaryKey, unlockKey sb.DiskUnlockKey, err error) {
		kd, err := sb.NewKeyData(&sb.KeyParams{
			Handle:       nil,
			Role:         "run",
			PlatformName: "fakePlatform",
			KDFAlg:       crypto.SHA256,
		})
		c.Assert(err, IsNil)
		return kd, primaryKey, []byte("unlock-key"), nil
	})
	defer restore()

	protectorKey, err := keys.NewProtectorKey()
	c.Assert(err, IsNil)
	protectedKey, primaryKeyOut, unlockKey, err := protectorKey.CreateProtectedKey([]byte("primary-in"))
	c.Assert(err, IsNil)
	c.Check(primaryKeyOut, DeepEquals, []byte("primary-in"))
	c.Check(unlockKey, DeepEquals, []byte("unlock-key"))

	kdw := NewMyKeyDataWriter()
	protectedKey.Write(kdw)

	c.Check(string(kdw.Bytes()), Equals, `{"generation":2,"platform_name":"fakePlatform","platform_handle":null,"role":"run","kdf_alg":"sha256","encrypted_payload":null}`+"\n")

	root := c.MkDir()

	path := filepath.Join(root, "somedir", "somefile")
	err = protectorKey.SaveToFile(path)
	c.Assert(err, IsNil)
	savedKey, err := os.ReadFile(path)
	c.Assert(err, IsNil)
	c.Check(savedKey, DeepEquals, []byte(protectorKey))
}
