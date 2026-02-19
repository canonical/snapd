// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023-2024 Canonical Ltd
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

package certstate_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/certstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type certsTestSuite struct {
	testutil.BaseTest

	state *state.State
}

func (s *certsTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.state = state.New(nil)
}

func (s *certsTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

var _ = Suite(&certsTestSuite{})

func makeTestCertPEM(commonName string) ([]byte, *x509.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),

		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return pemBytes, cert, nil
}

func digestForPEM(c *C, pemBytes []byte) string {
	dir := c.MkDir()
	path := filepath.Join(dir, "one.crt")
	c.Assert(os.WriteFile(path, pemBytes, 0o644), IsNil)

	certs, err := certstate.ParseCertificates(dir)
	c.Assert(err, IsNil)
	c.Assert(certs, HasLen, 1)

	return certs[0].Digest
}

func (s *certsTestSuite) TestIsBlockedReturnsBlocked(c *C) {
	c.Check(certstate.IsBlocked(certstate.Certificate{
		Digest:   "digest-123",
		RealPath: "blocked-cert.crt",
	}, []string{"digest-123", "digest-789"}), Equals, true)
}

func (s *certsTestSuite) TestIsBlockedReturnsBlockedOnSpecialNamings(c *C) {
	c.Check(certstate.IsBlocked(certstate.Certificate{
		Name: "ca-certificates.crt",
	}, nil), Equals, true)
}

func (s *certsTestSuite) TestIsBlockedReturnsBlockedOnSuffix(c *C) {
	// RealPath must end with .crt, otherwise it returns true
	c.Check(certstate.IsBlocked(certstate.Certificate{
		RealPath: "blocked-cert.pem",
	}, nil), Equals, true)
}

func (s *certsTestSuite) TestIsBlockedReturnsNotBlocked(c *C) {
	c.Check(certstate.IsBlocked(certstate.Certificate{
		Digest:   "digest-123",
		RealPath: "not-blocked-cert.crt",
	}, []string{"digest-789"}), Equals, false)
}

func (s *certsTestSuite) TestParseCertificateDataSimpleHappy(c *C) {
	bytes, _, err := makeTestCertPEM("Test Certificate Root CA")
	c.Assert(err, IsNil)

	cert, err := certstate.ParseCertificateData(bytes)
	c.Assert(err, IsNil)
	c.Check(cert.Raw.Subject.CommonName, Equals, "Test Certificate Root CA")
}

func (s *certsTestSuite) TestParseCertificateDataSkipsNonCertificateBlocks(c *C) {
	certPEM, _, err := makeTestCertPEM("Test Certificate Root CA")
	c.Assert(err, IsNil)

	nonCert := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("junk")})
	data := append(append([]byte{}, nonCert...), certPEM...)

	cert, err := certstate.ParseCertificateData(data)
	c.Assert(err, IsNil)
	c.Check(cert.Raw.Subject.CommonName, Equals, "Test Certificate Root CA")
}

func (s *certsTestSuite) TestParseCertificateDataDERInput(c *C) {
	_, cert, err := makeTestCertPEM("Test Certificate Root CA")
	c.Assert(err, IsNil)

	parsed, err := certstate.ParseCertificateData(cert.Raw)
	c.Assert(err, IsNil)
	c.Check(parsed.Raw.Subject.CommonName, Equals, "Test Certificate Root CA")
}

func (s *certsTestSuite) TestParseCertificateDataNoCertificateBlock(c *C) {
	data := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("junk")})
	cert, err := certstate.ParseCertificateData(data)
	c.Check(cert, IsNil)
	c.Check(err, ErrorMatches, ".*no certificate PEM block found.*")
}

func (s *certsTestSuite) TestParseCertificatesSimpleHappy(c *C) {
	cert1, _, err := makeTestCertPEM("Test Certificate Root CA 1")
	c.Assert(err, IsNil)
	cert2, _, err := makeTestCertPEM("Test Certificate Root CA 2")
	c.Assert(err, IsNil)
	cert3, _, err := makeTestCertPEM("Test Certificate Root CA 3")
	c.Assert(err, IsNil)

	// write certs into a directory
	certsDir := c.MkDir()
	c.Assert(os.WriteFile(""+certsDir+"/cert1.crt", cert1, 0o644), IsNil)
	c.Assert(os.WriteFile(""+certsDir+"/cert2.crt", cert2, 0o644), IsNil)

	// this one should be ignored as it does not have .crt suffix
	c.Assert(os.WriteFile(""+certsDir+"/cert3.pem", cert3, 0o644), IsNil)

	certs, err := certstate.ParseCertificates(certsDir)
	c.Assert(err, IsNil)
	c.Assert(len(certs), Equals, 2)
	c.Assert(certs[0].Name, Equals, "cert1")
	c.Assert(certs[1].Name, Equals, "cert2")
}

func (s *certsTestSuite) TestParseCertificatesResolvesSymlinks(c *C) {
	certPEM, _, err := makeTestCertPEM("Test Certificate Root CA")
	c.Assert(err, IsNil)

	dir := c.MkDir()
	real := filepath.Join(dir, "real.crt")
	link := filepath.Join(dir, "link.crt")

	c.Assert(os.WriteFile(real, certPEM, 0o644), IsNil)
	c.Assert(os.Symlink("real.crt", link), IsNil)

	certs, err := certstate.ParseCertificates(dir)
	c.Assert(err, IsNil)
	c.Assert(len(certs), Equals, 2)

	c.Check(certs[0].Name, Equals, "link")
	c.Check(certs[0].Path, Equals, link)
	c.Check(certs[0].RealPath, Equals, real)
	c.Check(certs[1].Name, Equals, "real")
	c.Check(certs[1].Path, Equals, real)
	c.Check(certs[1].RealPath, Equals, real)
}

func (s *certsTestSuite) TestParseCertificatesDigestIncludesFullChain(c *C) {
	aPEM, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	bPEM, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)
	cPEM, _, err := makeTestCertPEM("C")
	c.Assert(err, IsNil)

	chain1 := append(append([]byte{}, aPEM...), bPEM...)
	chain2 := append(append([]byte{}, aPEM...), cPEM...)

	dir := c.MkDir()
	c.Assert(os.WriteFile(filepath.Join(dir, "chain1.crt"), chain1, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dir, "chain2.crt"), chain2, 0o644), IsNil)

	certs, err := certstate.ParseCertificates(dir)
	c.Assert(err, IsNil)
	c.Assert(certs, HasLen, 2)

	// Both chains share the same first certificate, but differ after it.
	// The digest must include the full chain so the resulting digests differ.
	c.Check(certs[0].Digest, Not(Equals), certs[1].Digest)
}

func (s *certsTestSuite) TestReadDigestsMissingDir(c *C) {
	digests, err := certstate.ReadDigests(filepath.Join(c.MkDir(), "does-not-exist"))
	c.Assert(err, IsNil)
	c.Check(digests, HasLen, 0)
}

func (s *certsTestSuite) TestReadDigestsTrimsCrtExtension(c *C) {
	dir := c.MkDir()
	c.Assert(os.WriteFile(filepath.Join(dir, "abc.crt"), []byte("x"), 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dir, "def"), []byte("x"), 0o644), IsNil)
	c.Assert(os.Mkdir(filepath.Join(dir, "subdir"), 0o755), IsNil)

	digests, err := certstate.ReadDigests(dir)
	c.Assert(err, IsNil)
	c.Check(digests, DeepEquals, []string{"abc", "def"})
}

func (s *certsTestSuite) TestGenerateCACertificatesDeduplicatesAndBlocks(c *C) {
	cert1, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	cert2, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)
	cert3, _, err := makeTestCertPEM("D")
	c.Assert(err, IsNil)

	baseDir := c.MkDir()
	extraDir := c.MkDir()
	outDir := c.MkDir()

	c.Assert(os.WriteFile(filepath.Join(baseDir, "a.crt"), cert1, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "b.crt"), cert2, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(extraDir, "d.crt"), cert3, 0o644), IsNil)

	// duplicate certificate content under a different name
	c.Assert(os.WriteFile(filepath.Join(extraDir, "dup.crt"), cert1, 0o644), IsNil)

	base, err := certstate.ParseCertificates(baseDir)
	c.Assert(err, IsNil)
	extras, err := certstate.ParseCertificates(extraDir)
	c.Assert(err, IsNil)

	blockedDigest := digestForPEM(c, cert2)
	err = certstate.GenerateCACertificates(&certstate.Certificates{
		SystemCertificates: base,
		AddedCertificates:  extras,
		BlockedDigests:     []string{blockedDigest},
	}, outDir)
	c.Assert(err, IsNil)

	out, err := os.ReadFile(filepath.Join(outDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)

	c.Check(bytes.Contains(out, cert1), Equals, true)
	c.Check(bytes.Contains(out, cert2), Equals, false)
	c.Check(bytes.Contains(out, cert3), Equals, true)

	// ensure the duplicate (same digest) is only included once
	c.Check(bytes.Count(out, []byte("BEGIN CERTIFICATE")), Equals, 2)
}

func (s *certsTestSuite) TestGenerateCACertificatesDoesNotDeduplicateDifferentChains(c *C) {
	aPEM, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	bPEM, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)
	cPEM, _, err := makeTestCertPEM("C")
	c.Assert(err, IsNil)

	chain1 := append(append([]byte{}, aPEM...), bPEM...)
	chain2 := append(append([]byte{}, aPEM...), cPEM...)

	baseDir := c.MkDir()
	extraDir := c.MkDir()
	outDir := c.MkDir()

	c.Assert(os.WriteFile(filepath.Join(baseDir, "chain1.crt"), chain1, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(extraDir, "chain2.crt"), chain2, 0o644), IsNil)

	base, err := certstate.ParseCertificates(baseDir)
	c.Assert(err, IsNil)
	extras, err := certstate.ParseCertificates(extraDir)
	c.Assert(err, IsNil)

	err = certstate.GenerateCACertificates(&certstate.Certificates{
		SystemCertificates: base,
		AddedCertificates:  extras,
	}, outDir)
	c.Assert(err, IsNil)

	out, err := os.ReadFile(filepath.Join(outDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)

	// Both chains must be present (B and C certs appear).
	c.Check(bytes.Contains(out, bPEM), Equals, true)
	c.Check(bytes.Contains(out, cPEM), Equals, true)

	// Two chains with 2 certificates each.
	c.Check(bytes.Count(out, []byte("BEGIN CERTIFICATE")), Equals, 4)
}

func (s *certsTestSuite) TestGenerateCertificateDatabaseBacksUpAndWritesMerged(c *C) {
	aPEM, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	bPEM, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)

	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseCertsDir, "a.crt"), aPEM, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseCertsDir, "b.crt"), bPEM, 0o644), IsNil)

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	c.Assert(os.MkdirAll(mergedDir, 0o755), IsNil)
	old := []byte("old-ca-bundle")
	c.Assert(os.WriteFile(filepath.Join(mergedDir, "ca-certificates.crt"), old, 0o644), IsNil)

	err = certstate.GenerateCertificateDatabase()
	c.Assert(err, IsNil)

	out, err := os.ReadFile(filepath.Join(mergedDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(bytes.Contains(out, aPEM), Equals, true)
	c.Check(bytes.Contains(out, bPEM), Equals, true)

	bak, err := os.ReadFile(filepath.Join(mergedDir, "ca-certificates.crt.old"))
	c.Assert(err, IsNil)
	c.Check(bak, DeepEquals, old)
}

func (s *certsTestSuite) TestGenerateCertificateDatabaseBlocksBaseCertByDigest(c *C) {
	aPEM, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	bPEM, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)

	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseCertsDir, "a.crt"), aPEM, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseCertsDir, "b.crt"), bPEM, 0o644), IsNil)

	blockedDir := filepath.Join(dirs.SnapdPKIV1Dir, "blocked")
	c.Assert(os.MkdirAll(blockedDir, 0o755), IsNil)
	blockedDigest := digestForPEM(c, aPEM)
	c.Assert(os.WriteFile(filepath.Join(blockedDir, blockedDigest+".crt"), []byte("x"), 0o644), IsNil)

	err = certstate.GenerateCertificateDatabase()
	c.Assert(err, IsNil)

	mergedPath := filepath.Join(dirs.SnapdPKIV1Dir, "merged", "ca-certificates.crt")
	out, err := os.ReadFile(mergedPath)
	c.Assert(err, IsNil)
	c.Check(bytes.Contains(out, aPEM), Equals, false)
	c.Check(bytes.Contains(out, bPEM), Equals, true)
}
