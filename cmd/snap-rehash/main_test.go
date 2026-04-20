// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type rehashSuite struct{}

var _ = Suite(&rehashSuite{})

func makeTestCert(cn string) ([]byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

func expectedHash(pemData []byte) string {
	h := sha1.New()
	rest := pemData
	for {
		block, next := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = next
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, _ := x509.ParseCertificate(block.Bytes)
		h.Write(cert.Raw)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (s *rehashSuite) TestRehashCreatesHashLinks(c *C) {
	dir := c.MkDir()

	certA, err := makeTestCert("A")
	c.Assert(err, IsNil)
	certB, err := makeTestCert("B")
	c.Assert(err, IsNil)

	c.Assert(os.WriteFile(filepath.Join(dir, "a.crt"), certA, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dir, "b.crt"), certB, 0o644), IsNil)

	err = rehashDirectory(dir)
	c.Assert(err, IsNil)

	hashA := expectedHash(certA)[:8]
	hashB := expectedHash(certB)[:8]

	_, err = os.Stat(filepath.Join(dir, hashA+".0"))
	c.Check(err, IsNil)
	_, err = os.Stat(filepath.Join(dir, hashB+".0"))
	c.Check(err, IsNil)
}

func (s *rehashSuite) TestRehashSkipsCACertificatesBundle(c *C) {
	dir := c.MkDir()

	certA, err := makeTestCert("A")
	c.Assert(err, IsNil)

	// Write something named ca-certificates.crt — it must be skipped.
	c.Assert(os.WriteFile(filepath.Join(dir, "ca-certificates.crt"), certA, 0o644), IsNil)

	err = rehashDirectory(dir)
	c.Assert(err, IsNil)

	// No hash links should exist.
	entries, err := os.ReadDir(dir)
	c.Assert(err, IsNil)
	for _, e := range entries {
		c.Check(e.Name(), Equals, "ca-certificates.crt")
	}
}

func (s *rehashSuite) TestRehashCollisionUsesSuffix(c *C) {
	dir := c.MkDir()

	certA, err := makeTestCert("A")
	c.Assert(err, IsNil)
	certB, err := makeTestCert("B")
	c.Assert(err, IsNil)

	hashA := expectedHash(certA)[:8]

	// Write a.crt
	c.Assert(os.WriteFile(filepath.Join(dir, "a.crt"), certA, 0o644), IsNil)
	// Write b.crt but also pre-create the hash link for b that collides with a's prefix
	c.Assert(os.WriteFile(filepath.Join(dir, "b.crt"), certB, 0o644), IsNil)

	// Create a pre-existing file that occupies the .0 slot for a's hash
	c.Assert(os.WriteFile(filepath.Join(dir, hashA+".0"), []byte("occupied"), 0o644), IsNil)

	err = rehashDirectory(dir)
	c.Assert(err, IsNil)

	// a.crt should get suffix .1 since .0 is already taken
	_, err = os.Stat(filepath.Join(dir, hashA+".1"))
	c.Check(err, IsNil)
}

func (s *rehashSuite) TestRehashSkipsNonCrtFiles(c *C) {
	dir := c.MkDir()

	certA, err := makeTestCert("A")
	c.Assert(err, IsNil)

	// Write with .pem extension — should be skipped.
	c.Assert(os.WriteFile(filepath.Join(dir, "a.pem"), certA, 0o644), IsNil)

	err = rehashDirectory(dir)
	c.Assert(err, IsNil)

	entries, err := os.ReadDir(dir)
	c.Assert(err, IsNil)
	// Only the original file should exist.
	c.Check(entries, HasLen, 1)
}

func (s *rehashSuite) TestRehashSkipsDirectories(c *C) {
	dir := c.MkDir()

	// Create a directory with .crt suffix — must be skipped.
	c.Assert(os.MkdirAll(filepath.Join(dir, "subdir.crt"), 0o755), IsNil)

	err := rehashDirectory(dir)
	c.Assert(err, IsNil)

	entries, err := os.ReadDir(dir)
	c.Assert(err, IsNil)
	c.Check(entries, HasLen, 1)
	c.Check(entries[0].Name(), Equals, "subdir.crt")
}
