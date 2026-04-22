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
	return makeTestCertWithPEMBlockType(cn, "CERTIFICATE")
}

func makeTestCertWithPEMBlockType(cn, blockType string) ([]byte, error) {
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
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), nil
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
		if !isCertificatePEMBlockType(block.Type) {
			continue
		}
		cert, _ := x509.ParseCertificate(block.Bytes)
		h.Write(cert.Raw)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (s *rehashSuite) TestRehashAcceptsTrustedCertificatePEMBlock(c *C) {
	dir := c.MkDir()

	certData, err := makeTestCertWithPEMBlockType("trusted", "TRUSTED CERTIFICATE")
	c.Assert(err, IsNil)
	c.Assert(os.WriteFile(filepath.Join(dir, "trusted.crt"), certData, 0o644), IsNil)

	err = rehashDirectory(dir)
	c.Assert(err, IsNil)

	_, err = os.Stat(filepath.Join(dir, expectedHash(certData)[:8]+".0"))
	c.Check(err, IsNil)
}

func (s *rehashSuite) TestRehashAcceptsX509CertificatePEMBlock(c *C) {
	dir := c.MkDir()

	certData, err := makeTestCertWithPEMBlockType("x509", "X509 CERTIFICATE")
	c.Assert(err, IsNil)
	c.Assert(os.WriteFile(filepath.Join(dir, "x509.crt"), certData, 0o644), IsNil)

	err = rehashDirectory(dir)
	c.Assert(err, IsNil)

	_, err = os.Stat(filepath.Join(dir, expectedHash(certData)[:8]+".0"))
	c.Check(err, IsNil)
}

func (s *rehashSuite) TestRehashInvalidPEMNoCertificateBlock(c *C) {
	dir := c.MkDir()

	// A PEM file that contains only a PRIVATE KEY block, no CERTIFICATE.
	keyBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not a real key")})
	c.Assert(os.WriteFile(filepath.Join(dir, "nocert.crt"), keyBlock, 0o644), IsNil)

	err := rehashDirectory(dir)
	c.Check(err, ErrorMatches, `cannot hash certificate "nocert.crt": no CERTIFICATE PEM block found`)
}

func (s *rehashSuite) TestRehashInvalidPEMCertificateData(c *C) {
	dir := c.MkDir()

	// A PEM file with a CERTIFICATE block containing garbage bytes.
	badCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("garbage")})
	c.Assert(os.WriteFile(filepath.Join(dir, "corrupt.crt"), badCert, 0o644), IsNil)

	err := rehashDirectory(dir)
	c.Check(err, ErrorMatches, `cannot hash certificate "corrupt.crt": cannot parse PEM certificate block: .*`)
}

func (s *rehashSuite) TestRehashInvalidDERData(c *C) {
	dir := c.MkDir()

	// Raw bytes that are not valid DER or PEM.
	c.Assert(os.WriteFile(filepath.Join(dir, "junk.crt"), []byte{0x30, 0x00, 0x01}, 0o644), IsNil)

	err := rehashDirectory(dir)
	c.Check(err, ErrorMatches, `cannot hash certificate "junk.crt": cannot parse DER certificate: .*`)
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

func (s *rehashSuite) TestRehashLinkCreationError(c *C) {
	dir := c.MkDir()

	certA, err := makeTestCert("A")
	c.Assert(err, IsNil)
	c.Assert(os.WriteFile(filepath.Join(dir, "a.crt"), certA, 0o644), IsNil)

	// Make the directory read-only so os.Link fails with permission denied
	// (not EEXIST), which exercises the non-collision error return.
	c.Assert(os.Chmod(dir, 0o555), IsNil)
	defer os.Chmod(dir, 0o755)

	err = rehashDirectory(dir)
	c.Check(err, ErrorMatches, `cannot create hash link for "a.crt": .*permission denied`)
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

func (s *rehashSuite) TestRehashSkipsUnsupportedSuffixes(c *C) {
	dir := c.MkDir()

	certA, err := makeTestCert("A")
	c.Assert(err, IsNil)

	// Write with an unsupported extension — it should be skipped.
	c.Assert(os.WriteFile(filepath.Join(dir, "a.txt"), certA, 0o644), IsNil)

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
