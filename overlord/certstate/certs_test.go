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
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

func opensslSubjectHash(c *C, certData []byte) string {
	if _, err := exec.LookPath("openssl"); err != nil {
		c.Skip("openssl not available")
	}

	dir := c.MkDir()
	certPath := filepath.Join(dir, "cert.pem")
	c.Assert(os.WriteFile(certPath, certData, 0o644), IsNil)

	out, err := exec.Command("openssl", "x509", "-in", certPath, "-noout", "-subject_hash").CombinedOutput()
	c.Assert(err, IsNil, Commentf("openssl output: %s", out))

	return strings.TrimSpace(string(out))
}

func digestForPEM(c *C, pemBytes []byte) string {
	dir := c.MkDir()
	path := filepath.Join(dir, "one.crt")
	c.Assert(os.WriteFile(path, pemBytes, 0o644), IsNil)

	certs, err := certstate.ParseCertificates(dir)
	c.Assert(err, IsNil)
	c.Assert(certs, HasLen, 1)

	return certs[0].Sha256
}

func parsedCertificateData(c *C, pemBytes []byte) *certstate.CertificateData {
	parsed, err := certstate.ParseCertificateData(pemBytes)
	c.Assert(err, IsNil)
	return parsed
}

func setAcceptedCustomCertificate(c *C, name string, pemBytes []byte) string {
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapdPKIV1Dir, "added"), 0o755), IsNil)
	c.Assert(certstate.WriteCertificate(name, string(pemBytes)), IsNil)

	digest := digestForPEM(c, pemBytes)
	c.Assert(certstate.SetCertificateState(name, digest, certstate.CertificateStateAccepted), IsNil)
	return digest
}

func removeAcceptedCustomCertificate(c *C, name, digest string) {
	c.Assert(certstate.RemoveCertificateSymlinks(digest), IsNil)
	c.Assert(certstate.RemoveCertificate(name), IsNil)
}

func makeSymlinkLoop(c *C, path string) {
	c.Assert(os.MkdirAll(filepath.Dir(path), 0o755), IsNil)
	c.Assert(os.Symlink(path, path), IsNil)
}

func (s *certsTestSuite) TestIsBlockedReturnsBlocked(c *C) {
	c.Check(certstate.IsBlocked(certstate.Certificate{
		Sha256:   "digest-123",
		RealPath: "blocked-cert.crt",
	}, []string{"digest-123", "digest-789"}), Equals, true)
}

func (s *certsTestSuite) TestIsBlockedReturnsBlockedOnSpecialNamings(c *C) {
	c.Check(certstate.IsBlocked(certstate.Certificate{
		Name: "ca-certificates",
	}, nil), Equals, true)
}

func (s *certsTestSuite) TestIsBlockedReturnsBlockedOnSuffix(c *C) {
	// RealPath must end with a supported extension, otherwise it returns true
	c.Check(certstate.IsBlocked(certstate.Certificate{
		RealPath: "blocked-cert.crl",
	}, nil), Equals, true)
}

func (s *certsTestSuite) TestIsBlockedReturnsNotBlocked(c *C) {
	c.Check(certstate.IsBlocked(certstate.Certificate{
		Sha256:   "digest-123",
		RealPath: "not-blocked-cert.crt",
	}, []string{"digest-789"}), Equals, false)
}

func (s *certsTestSuite) TestParseCertificateDataSimpleHappy(c *C) {
	certPEM, _, err := makeTestCertPEM("Test Certificate Root CA")
	c.Assert(err, IsNil)

	parsed, err := certstate.ParseCertificateData(certPEM)
	c.Assert(err, IsNil)
	c.Check(parsed.Sha256, Equals, digestForPEM(c, certPEM))
	c.Check(parsed.SubjectNameSha1, Equals, opensslSubjectHash(c, certPEM))
}

func (s *certsTestSuite) TestParseCertificateDataSkipsNonCertificateBlocks(c *C) {
	certPEM, _, err := makeTestCertPEM("Test Certificate Root CA")
	c.Assert(err, IsNil)

	nonCert := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("junk")})
	data := append(append([]byte{}, nonCert...), certPEM...)

	parsed, err := certstate.ParseCertificateData(data)
	c.Assert(err, IsNil)
	c.Check(parsed.Sha256, Equals, digestForPEM(c, certPEM))
	c.Check(parsed.SubjectNameSha1, Equals, opensslSubjectHash(c, certPEM))
}

func (s *certsTestSuite) TestParseCertificateDataDERInput(c *C) {
	certPEM, _, err := makeTestCertPEM("Test Certificate Root CA")
	c.Assert(err, IsNil)
	block, _ := pem.Decode(certPEM)
	c.Assert(block, NotNil)

	parsed, err := certstate.ParseCertificateData(block.Bytes)
	c.Assert(err, IsNil)
	c.Check(parsed.Sha256, Equals, digestForPEM(c, certPEM))
	c.Check(parsed.SubjectNameSha1, Equals, opensslSubjectHash(c, certPEM))
}

func (s *certsTestSuite) TestParseCertificateDataSubjectHashMatchesOpenSSL(c *C) {
	certPEM, _, err := makeTestCertPEM(" Test  Name ")
	c.Assert(err, IsNil)

	parsed, err := certstate.ParseCertificateData(certPEM)
	c.Assert(err, IsNil)
	c.Check(parsed.SubjectNameSha1, Equals, opensslSubjectHash(c, certPEM))
}

func (s *certsTestSuite) TestLoadCertificatesPropagatesAddedDirLookupError(c *C) {
	c.Assert(os.MkdirAll(dirs.SystemCertsDir, 0o755), IsNil)
	makeSymlinkLoop(c, filepath.Join(dirs.SnapdPKIV1Dir, "added"))

	_, err := certstate.LoadCertificates()
	c.Assert(err, NotNil)
	c.Check(errors.Is(err, syscall.ELOOP), Equals, true)
}

func (s *certsTestSuite) TestLoadCertificatesPropagatesBlockedDirLookupError(c *C) {
	c.Assert(os.MkdirAll(dirs.SystemCertsDir, 0o755), IsNil)
	makeSymlinkLoop(c, filepath.Join(dirs.SnapdPKIV1Dir, "blocked"))

	_, err := certstate.LoadCertificates()
	c.Assert(err, NotNil)
	c.Check(errors.Is(err, syscall.ELOOP), Equals, true)
}

func (s *certsTestSuite) TestParseCertificateDataMultipleCertificatesSkipsSubjectHash(c *C) {
	cert1, _, err := makeTestCertPEM("Test Certificate Root CA 1")
	c.Assert(err, IsNil)
	cert2, _, err := makeTestCertPEM("Test Certificate Root CA 2")
	c.Assert(err, IsNil)

	parsed, err := certstate.ParseCertificateData(append(cert1, cert2...))
	c.Assert(err, IsNil)
	c.Check(parsed.SubjectNameSha1, Equals, "")
}

func (s *certsTestSuite) TestParseCertificateDataNoCertificateBlock(c *C) {
	data := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("junk")})
	cert, err := certstate.ParseCertificateData(data)
	c.Check(cert, IsNil)
	c.Check(err, ErrorMatches, ".*no certificate PEM block found.*")
}

func (s *certsTestSuite) TestParseCertificateDataInvalidCertificatePEMBlock(c *C) {
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("junk")})

	cert, err := certstate.ParseCertificateData(data)
	c.Check(cert, IsNil)
	c.Check(err, ErrorMatches, `cannot parse certificate PEM block: .*`)
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
	c.Assert(os.WriteFile(""+certsDir+"/cert2.cer", cert2, 0o644), IsNil)
	c.Assert(os.WriteFile(""+certsDir+"/cert3.pem", cert3, 0o644), IsNil)

	// this one should be ignored as it does not have a supported suffix
	c.Assert(os.WriteFile(""+certsDir+"/cert4.crl", cert3, 0o644), IsNil)

	certs, err := certstate.ParseCertificates(certsDir)
	c.Assert(err, IsNil)
	c.Assert(len(certs), Equals, 3)
	c.Assert(certs[0].Name, Equals, "cert1")
	c.Assert(certs[1].Name, Equals, "cert2")
	c.Assert(certs[2].Name, Equals, "cert3")
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

func (s *certsTestSuite) TestParseCertificatesSkipsBrokenEntries(c *C) {
	certPEM, _, err := makeTestCertPEM("Test Certificate Root CA")
	c.Assert(err, IsNil)

	dir := c.MkDir()
	goodPath := filepath.Join(dir, "good.crt")
	brokenLink := filepath.Join(dir, "broken-link.crt")
	brokenCert := filepath.Join(dir, "broken-cert.crt")

	c.Assert(os.WriteFile(goodPath, certPEM, 0o644), IsNil)
	c.Assert(os.Symlink("missing.crt", brokenLink), IsNil)
	c.Assert(os.WriteFile(brokenCert, []byte("not-a-certificate"), 0o644), IsNil)

	certs, err := certstate.ParseCertificates(dir)
	c.Assert(err, IsNil)
	c.Assert(certs, HasLen, 1)
	c.Check(certs[0].Name, Equals, "good")
	c.Check(certs[0].Path, Equals, goodPath)
	c.Check(certs[0].RealPath, Equals, goodPath)
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
	c.Check(certs[0].Sha256, Not(Equals), certs[1].Sha256)
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

func (s *certsTestSuite) TestGenerateCACertificatesMirrorsCertsDir(c *C) {
	aPEM, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	bPEM, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)
	cPEM, _, err := makeTestCertPEM("C")
	c.Assert(err, IsNil)

	baseDir := c.MkDir()
	outDir := filepath.Join(c.MkDir(), "merged")

	c.Assert(os.WriteFile(filepath.Join(baseDir, "a.crt"), aPEM, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "b.crt"), bPEM, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "c.pem"), cPEM, 0o644), IsNil)

	base, err := certstate.ParseCertificates(baseDir)
	c.Assert(err, IsNil)

	err = certstate.GenerateCACertificates(&certstate.Certificates{
		SystemCertificates: base,
	}, outDir)
	c.Assert(err, IsNil)

	// Verify individual certificate links exist and match content.
	for _, name := range []string{"a.crt", "b.crt", "c.pem"} {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		c.Assert(err, IsNil, Commentf("cert link %q", name))

		orig, err := os.ReadFile(filepath.Join(baseDir, name))
		c.Assert(err, IsNil)
		c.Check(got, DeepEquals, orig, Commentf("cert link %q content mismatch", name))
	}

	// Verify SHA-1 hash links exist (first 8 hex chars of SHA-1 + ".0").
	for _, cert := range base {
		linkName := cert.SubjectNameSha1[:8] + ".0"
		_, err := os.Stat(filepath.Join(outDir, linkName))
		c.Check(err, IsNil, Commentf("sha1 hash link %q missing for cert %q", linkName, cert.Name))
	}

	// Verify the bundle is present and contains both certificates.
	bundle, err := os.ReadFile(filepath.Join(outDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(bytes.Contains(bundle, aPEM), Equals, true)
	c.Check(bytes.Contains(bundle, bPEM), Equals, true)
	c.Check(bytes.Contains(bundle, cPEM), Equals, true)
}

func (s *certsTestSuite) TestGenerateCACertificatesDoesNotOverwriteDistinctCertificatesWithSameBasename(c *C) {
	systemPEM, _, err := makeTestCertPEM("system-same-name")
	c.Assert(err, IsNil)
	addedPEM, _, err := makeTestCertPEM("added-same-name")
	c.Assert(err, IsNil)

	baseDir := c.MkDir()
	extraDir := c.MkDir()
	outDir := filepath.Join(c.MkDir(), "merged")

	systemPath := filepath.Join(baseDir, "same.crt")
	addedPath := filepath.Join(extraDir, "same.crt")
	c.Assert(os.WriteFile(systemPath, systemPEM, 0o644), IsNil)
	c.Assert(os.WriteFile(addedPath, addedPEM, 0o644), IsNil)

	base, err := certstate.ParseCertificates(baseDir)
	c.Assert(err, IsNil)
	extras, err := certstate.ParseCertificates(extraDir)
	c.Assert(err, IsNil)
	c.Assert(base, HasLen, 1)
	c.Assert(extras, HasLen, 1)
	c.Check(base[0].SubjectNameSha1[:8], Not(Equals), extras[0].SubjectNameSha1[:8])

	err = certstate.GenerateCACertificates(&certstate.Certificates{
		SystemCertificates: base,
		AddedCertificates:  extras,
	}, outDir)
	c.Assert(err, IsNil)

	bundle, err := os.ReadFile(filepath.Join(outDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(bytes.Contains(bundle, systemPEM), Equals, true)
	c.Check(bytes.Contains(bundle, addedPEM), Equals, true)

	entries, err := os.ReadDir(outDir)
	c.Assert(err, IsNil)
	var sawSystemCopy bool
	var sawAddedCopy bool
	var copiedFiles int
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || entry.Name() == "ca-certificates.crt" {
			continue
		}
		copiedFiles++
		data, err := os.ReadFile(filepath.Join(outDir, entry.Name()))
		c.Assert(err, IsNil)
		if bytes.Equal(data, systemPEM) {
			sawSystemCopy = true
		}
		if bytes.Equal(data, addedPEM) {
			sawAddedCopy = true
		}
	}
	c.Check(copiedFiles, Equals, 2)
	c.Check(sawSystemCopy, Equals, true)
	c.Check(sawAddedCopy, Equals, true)

	systemHashLink := filepath.Join(outDir, base[0].SubjectNameSha1[:8]+".0")
	addedHashLink := filepath.Join(outDir, extras[0].SubjectNameSha1[:8]+".0")
	gotSystem, err := os.ReadFile(systemHashLink)
	c.Assert(err, IsNil)
	gotAdded, err := os.ReadFile(addedHashLink)
	c.Assert(err, IsNil)
	c.Check(gotSystem, DeepEquals, systemPEM)
	c.Check(gotAdded, DeepEquals, addedPEM)
}

func (s *certsTestSuite) TestGenerateCACertificatesUsesNumberedFallbackWhenDigestNameTaken(c *C) {
	basePEM, _, err := makeTestCertPEM("base-shared-name")
	c.Assert(err, IsNil)
	conflictingPEM, _, err := makeTestCertPEM("conflicting-digest-name")
	c.Assert(err, IsNil)
	addedPEM, _, err := makeTestCertPEM("added-shared-name")
	c.Assert(err, IsNil)

	addedDigest := digestForPEM(c, addedPEM)

	baseDir := c.MkDir()
	conflictingDir := c.MkDir()
	addedDir := c.MkDir()
	outDir := filepath.Join(c.MkDir(), "merged")

	basePath := filepath.Join(baseDir, "shared.crt")
	conflictingPath := filepath.Join(conflictingDir, "shared-"+addedDigest+".crt")
	addedPath := filepath.Join(addedDir, "shared.crt")

	c.Assert(os.WriteFile(basePath, basePEM, 0o644), IsNil)
	c.Assert(os.WriteFile(conflictingPath, conflictingPEM, 0o644), IsNil)
	c.Assert(os.WriteFile(addedPath, addedPEM, 0o644), IsNil)

	base, err := certstate.ParseCertificates(baseDir)
	c.Assert(err, IsNil)
	conflicting, err := certstate.ParseCertificates(conflictingDir)
	c.Assert(err, IsNil)
	added, err := certstate.ParseCertificates(addedDir)
	c.Assert(err, IsNil)
	c.Assert(base, HasLen, 1)
	c.Assert(conflicting, HasLen, 1)
	c.Assert(added, HasLen, 1)

	err = certstate.GenerateCACertificates(&certstate.Certificates{
		SystemCertificates: []certstate.Certificate{base[0], conflicting[0]},
		AddedCertificates:  added,
	}, outDir)
	c.Assert(err, IsNil)

	baseCopy, err := os.ReadFile(filepath.Join(outDir, "shared.crt"))
	c.Assert(err, IsNil)
	c.Check(baseCopy, DeepEquals, basePEM)

	conflictingCopyName := "shared-" + addedDigest + ".crt"
	conflictingCopy, err := os.ReadFile(filepath.Join(outDir, conflictingCopyName))
	c.Assert(err, IsNil)
	c.Check(conflictingCopy, DeepEquals, conflictingPEM)

	addedCopyName := "shared-" + addedDigest + "-1.crt"
	addedCopy, err := os.ReadFile(filepath.Join(outDir, addedCopyName))
	c.Assert(err, IsNil)
	c.Check(addedCopy, DeepEquals, addedPEM)

	bundle, err := os.ReadFile(filepath.Join(outDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(bytes.Contains(bundle, basePEM), Equals, true)
	c.Check(bytes.Contains(bundle, conflictingPEM), Equals, true)
	c.Check(bytes.Contains(bundle, addedPEM), Equals, true)
}

func (s *certsTestSuite) TestGenerateCACertificatesSkipsSourceBundleFile(c *C) {
	aPEM, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	bPEM, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)

	baseDir := c.MkDir()
	outDir := filepath.Join(c.MkDir(), "merged")
	sourceBundle := append(append([]byte(nil), aPEM...), bPEM...)

	c.Assert(os.WriteFile(filepath.Join(baseDir, "a.crt"), aPEM, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "b.crt"), bPEM, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "ca-certificates.crt"), sourceBundle, 0o644), IsNil)

	base, err := certstate.ParseCertificates(baseDir)
	c.Assert(err, IsNil)
	c.Assert(base, HasLen, 3)

	err = certstate.GenerateCACertificates(&certstate.Certificates{
		SystemCertificates: base,
	}, outDir)
	c.Assert(err, IsNil)

	bundle, err := os.ReadFile(filepath.Join(outDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(bytes.Count(bundle, []byte("BEGIN CERTIFICATE")), Equals, 2)
	c.Check(bytes.Contains(bundle, aPEM), Equals, true)
	c.Check(bytes.Contains(bundle, bPEM), Equals, true)

	_, err = os.Stat(filepath.Join(outDir, "a.crt"))
	c.Check(err, IsNil)
	_, err = os.Stat(filepath.Join(outDir, "b.crt"))
	c.Check(err, IsNil)

	entries, err := os.ReadDir(outDir)
	c.Assert(err, IsNil)
	var sourceBundleCopies int
	for _, entry := range entries {
		if entry.Name() == "ca-certificates.crt" {
			sourceBundleCopies++
		}
	}
	c.Check(sourceBundleCopies, Equals, 1)
}

func (s *certsTestSuite) TestGenerateCACertificatesSha1LinksAreUnique(c *C) {
	aPEM, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	bPEM, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)

	baseDir := c.MkDir()
	outDir := filepath.Join(c.MkDir(), "merged")

	c.Assert(os.WriteFile(filepath.Join(baseDir, "a.crt"), aPEM, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "b.crt"), bPEM, 0o644), IsNil)

	base, err := certstate.ParseCertificates(baseDir)
	c.Assert(err, IsNil)

	// Ensure the two certificates produce different SHA-1 hash links.
	c.Assert(base, HasLen, 2)
	c.Check(base[0].SubjectNameSha1[:8], Not(Equals), base[1].SubjectNameSha1[:8])

	err = certstate.GenerateCACertificates(&certstate.Certificates{
		SystemCertificates: base,
	}, outDir)
	c.Assert(err, IsNil)

	// Both hash links must be present.
	link0 := base[0].SubjectNameSha1[:8] + ".0"
	link1 := base[1].SubjectNameSha1[:8] + ".0"
	_, err = os.Stat(filepath.Join(outDir, link0))
	c.Check(err, IsNil)
	_, err = os.Stat(filepath.Join(outDir, link1))
	c.Check(err, IsNil)
}

func (s *certsTestSuite) TestGenerateCACertificatesSha1CollisionsUseNextSuffix(c *C) {
	aPEM, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	bPEM, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)

	baseDir := c.MkDir()
	outDir := filepath.Join(c.MkDir(), "merged")
	aPath := filepath.Join(baseDir, "a.crt")
	bPath := filepath.Join(baseDir, "b.crt")

	c.Assert(os.WriteFile(aPath, aPEM, 0o644), IsNil)
	c.Assert(os.WriteFile(bPath, bPEM, 0o644), IsNil)

	err = certstate.GenerateCACertificates(&certstate.Certificates{
		SystemCertificates: []certstate.Certificate{
			{
				Name:            "a",
				Path:            aPath,
				RealPath:        aPath,
				Sha256:          "sha256-a",
				SubjectNameSha1: "deadbeef00000000000000000000000000000000",
			},
			{
				Name:            "b",
				Path:            bPath,
				RealPath:        bPath,
				Sha256:          "sha256-b",
				SubjectNameSha1: "deadbeef11111111111111111111111111111111",
			},
		},
	}, outDir)
	c.Assert(err, IsNil)

	got0, err := os.ReadFile(filepath.Join(outDir, "deadbeef.0"))
	c.Assert(err, IsNil)
	got1, err := os.ReadFile(filepath.Join(outDir, "deadbeef.1"))
	c.Assert(err, IsNil)
	c.Check(got0, DeepEquals, aPEM)
	c.Check(got1, DeepEquals, bPEM)
}

func (s *certsTestSuite) TestGenerateCACertificatesSkipsHashLinkForMultiCertificateFile(c *C) {
	cert1, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	cert2, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)

	baseDir := c.MkDir()
	outDir := filepath.Join(c.MkDir(), "merged")
	bundlePEM := append(append([]byte(nil), cert1...), cert2...)

	bundlePath := filepath.Join(baseDir, "bundle.crt")
	c.Assert(os.WriteFile(bundlePath, bundlePEM, 0o644), IsNil)

	base, err := certstate.ParseCertificates(baseDir)
	c.Assert(err, IsNil)
	c.Assert(base, HasLen, 1)
	c.Check(base[0].SubjectNameSha1, Equals, "")

	err = certstate.GenerateCACertificates(&certstate.Certificates{
		SystemCertificates: base,
	}, outDir)
	c.Assert(err, IsNil)

	entries, err := os.ReadDir(outDir)
	c.Assert(err, IsNil)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	c.Check(names, DeepEquals, []string{"bundle.crt", "ca-certificates.crt"})
}

func (s *certsTestSuite) TestGenerateCACertificatesBlockedCertHasNoLinks(c *C) {
	aPEM, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	bPEM, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)

	baseDir := c.MkDir()
	outDir := filepath.Join(c.MkDir(), "merged")

	c.Assert(os.WriteFile(filepath.Join(baseDir, "a.crt"), aPEM, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "b.crt"), bPEM, 0o644), IsNil)

	base, err := certstate.ParseCertificates(baseDir)
	c.Assert(err, IsNil)

	blockedDigest := digestForPEM(c, aPEM)

	// Find the sha1 of the blocked cert so we can check its link is absent.
	var blockedSha1Prefix string
	for _, cert := range base {
		if cert.Sha256 == blockedDigest {
			blockedSha1Prefix = cert.SubjectNameSha1[:8]
			break
		}
	}
	c.Assert(blockedSha1Prefix, Not(Equals), "")

	err = certstate.GenerateCACertificates(&certstate.Certificates{
		SystemCertificates: base,
		BlockedDigests:     []string{blockedDigest},
	}, outDir)
	c.Assert(err, IsNil)

	// The blocked cert's file link and SHA-1 hash link must not exist.
	_, err = os.Stat(filepath.Join(outDir, "a.crt"))
	c.Check(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(filepath.Join(outDir, blockedSha1Prefix+".0"))
	c.Check(os.IsNotExist(err), Equals, true)

	// The non-blocked cert's links must be present.
	_, err = os.Stat(filepath.Join(outDir, "b.crt"))
	c.Check(err, IsNil)
}

func (s *certsTestSuite) TestRefreshCertificateDatabaseKeepsHashLinksValidAfterSwap(c *C) {
	aPEM, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)

	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)
	aPath := filepath.Join(baseCertsDir, "a.crt")
	c.Assert(os.WriteFile(aPath, aPEM, 0o644), IsNil)

	parsed, err := certstate.ParseCertificates(baseCertsDir)
	c.Assert(err, IsNil)
	c.Assert(parsed, HasLen, 1)

	err = certstate.RefreshCertificateDatabase()
	c.Assert(err, IsNil)

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	mergedInfo, err := os.Lstat(mergedDir)
	c.Assert(err, IsNil)
	c.Check(mergedInfo.Mode()&os.ModeSymlink != 0, Equals, true)
	hashLink := filepath.Join(mergedDir, parsed[0].SubjectNameSha1[:8]+".0")

	info, err := os.Lstat(hashLink)
	c.Assert(err, IsNil)
	c.Check(info.Mode()&os.ModeSymlink != 0, Equals, true)

	target, err := os.Readlink(hashLink)
	c.Assert(err, IsNil)
	c.Check(target, Equals, "a.crt")

	linkedContents, err := os.ReadFile(hashLink)
	c.Assert(err, IsNil)
	c.Check(linkedContents, DeepEquals, aPEM)
}

func (s *certsTestSuite) TestRefreshCertificateDatabasePublishesImmutableGenerations(c *C) {
	aPEM, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	bPEM, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)

	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)
	aPath := filepath.Join(baseCertsDir, "a.crt")
	bPath := filepath.Join(baseCertsDir, "b.crt")
	c.Assert(os.WriteFile(aPath, aPEM, 0o644), IsNil)

	err = certstate.RefreshCertificateDatabase()
	c.Assert(err, IsNil)

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	firstTarget, err := os.Readlink(mergedDir)
	c.Assert(err, IsNil)

	firstBundle, err := os.ReadFile(filepath.Join(dirs.SnapdPKIV1Dir, firstTarget, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(bytes.Contains(firstBundle, aPEM), Equals, true)
	c.Check(bytes.Contains(firstBundle, bPEM), Equals, false)

	c.Assert(os.Remove(aPath), IsNil)
	c.Assert(os.WriteFile(bPath, bPEM, 0o644), IsNil)

	err = certstate.RefreshCertificateDatabase()
	c.Assert(err, IsNil)

	secondTarget, err := os.Readlink(mergedDir)
	c.Assert(err, IsNil)
	c.Check(secondTarget, Not(Equals), firstTarget)

	secondBundle, err := os.ReadFile(filepath.Join(dirs.SnapdPKIV1Dir, secondTarget, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(bytes.Contains(secondBundle, aPEM), Equals, false)
	c.Check(bytes.Contains(secondBundle, bPEM), Equals, true)

	firstBundleAfterRefresh, err := os.ReadFile(filepath.Join(dirs.SnapdPKIV1Dir, firstTarget, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(firstBundleAfterRefresh, DeepEquals, firstBundle)

	mergedBundle, err := os.ReadFile(filepath.Join(mergedDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(mergedBundle, DeepEquals, secondBundle)
}

func (s *certsTestSuite) TestRefreshCertificateDatabaseRenamedAcceptedCertificateGetsNewGeneration(c *C) {
	customPEM, _, err := makeTestCertPEM("rename-me")
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(dirs.SystemCertsDir, 0o755), IsNil)

	firstDigest := setAcceptedCustomCertificate(c, "oldcert", customPEM)
	err = certstate.RefreshCertificateDatabase()
	c.Assert(err, IsNil)

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	firstTarget, err := os.Readlink(mergedDir)
	c.Assert(err, IsNil)
	firstBundle, err := os.ReadFile(filepath.Join(dirs.SnapdPKIV1Dir, firstTarget, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(dirs.SnapdPKIV1Dir, firstTarget, "oldcert.crt"))
	c.Assert(err, IsNil)

	removeAcceptedCustomCertificate(c, "oldcert", firstDigest)
	secondDigest := setAcceptedCustomCertificate(c, "newcert", customPEM)
	c.Check(secondDigest, Equals, firstDigest)

	err = certstate.RefreshCertificateDatabase()
	c.Assert(err, IsNil)

	secondTarget, err := os.Readlink(mergedDir)
	c.Assert(err, IsNil)
	c.Check(secondTarget, Not(Equals), firstTarget)

	secondBundle, err := os.ReadFile(filepath.Join(dirs.SnapdPKIV1Dir, secondTarget, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(secondBundle, DeepEquals, firstBundle)

	_, err = os.Stat(filepath.Join(dirs.SnapdPKIV1Dir, secondTarget, "newcert.crt"))
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(dirs.SnapdPKIV1Dir, secondTarget, "oldcert.crt"))
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *certsTestSuite) TestRefreshCertificateDatabaseCollisionRenameGetsNewGeneration(c *C) {
	systemPEM, _, err := makeTestCertPEM("system")
	c.Assert(err, IsNil)
	customPEM, _, err := makeTestCertPEM("custom")
	c.Assert(err, IsNil)

	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseCertsDir, "shared.crt"), systemPEM, 0o644), IsNil)

	customDigest := setAcceptedCustomCertificate(c, "extra", customPEM)
	err = certstate.RefreshCertificateDatabase()
	c.Assert(err, IsNil)

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	firstTarget, err := os.Readlink(mergedDir)
	c.Assert(err, IsNil)
	firstBundle, err := os.ReadFile(filepath.Join(dirs.SnapdPKIV1Dir, firstTarget, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(dirs.SnapdPKIV1Dir, firstTarget, "extra.crt"))
	c.Assert(err, IsNil)

	removeAcceptedCustomCertificate(c, "extra", customDigest)
	secondDigest := setAcceptedCustomCertificate(c, "shared", customPEM)
	c.Check(secondDigest, Equals, customDigest)

	err = certstate.RefreshCertificateDatabase()
	c.Assert(err, IsNil)

	secondTarget, err := os.Readlink(mergedDir)
	c.Assert(err, IsNil)
	c.Check(secondTarget, Not(Equals), firstTarget)

	secondBundle, err := os.ReadFile(filepath.Join(dirs.SnapdPKIV1Dir, secondTarget, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(secondBundle, DeepEquals, firstBundle)

	expectedCustomName := "shared-" + customDigest + ".crt"
	_, err = os.Stat(filepath.Join(dirs.SnapdPKIV1Dir, secondTarget, expectedCustomName))
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(dirs.SnapdPKIV1Dir, secondTarget, "extra.crt"))
	c.Check(os.IsNotExist(err), Equals, true)

	customData := parsedCertificateData(c, customPEM)
	customHashLink := filepath.Join(dirs.SnapdPKIV1Dir, secondTarget, customData.SubjectNameSha1[:8]+".0")
	target, err := os.Readlink(customHashLink)
	c.Assert(err, IsNil)
	c.Check(target, Equals, expectedCustomName)
}

func (s *certsTestSuite) TestRefreshCertificateDatabaseFormattingOnlyChangeReusesGeneration(c *C) {
	customPEM, _, err := makeTestCertPEM("format-invariant")
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(dirs.SystemCertsDir, 0o755), IsNil)

	digest := setAcceptedCustomCertificate(c, "samecert", customPEM)
	err = certstate.RefreshCertificateDatabase()
	c.Assert(err, IsNil)

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	firstTarget, err := os.Readlink(mergedDir)
	c.Assert(err, IsNil)
	firstBundle, err := os.ReadFile(filepath.Join(dirs.SnapdPKIV1Dir, firstTarget, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	firstCopiedCert, err := os.ReadFile(filepath.Join(dirs.SnapdPKIV1Dir, firstTarget, "samecert.crt"))
	c.Assert(err, IsNil)

	// Formatting-only source changes are intentionally outside the generation
	// contract, so re-rendering should converge on the existing published tree.
	reformattedPEM := append([]byte("# ignored PEM comment\n\n"), customPEM...)
	c.Check(reformattedPEM, Not(DeepEquals), customPEM)
	c.Check(digestForPEM(c, reformattedPEM), Equals, digest)
	c.Assert(certstate.WriteCertificate("samecert", string(reformattedPEM)), IsNil)

	err = certstate.RefreshCertificateDatabase()
	c.Assert(err, IsNil)

	secondTarget, err := os.Readlink(mergedDir)
	c.Assert(err, IsNil)
	c.Check(secondTarget, Equals, firstTarget)

	secondBundle, err := os.ReadFile(filepath.Join(mergedDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(secondBundle, DeepEquals, firstBundle)

	secondCopiedCert, err := os.ReadFile(filepath.Join(dirs.SnapdPKIV1Dir, secondTarget, "samecert.crt"))
	c.Assert(err, IsNil)
	c.Check(secondCopiedCert, DeepEquals, firstCopiedCert)
}

func (s *certsTestSuite) TestGarbageCollectCertificateGenerationsProtectsCurrentTarget(c *C) {
	currentDir := filepath.Join(dirs.SnapdPKIV1Dir, "published", "current")
	staleDir := filepath.Join(dirs.SnapdPKIV1Dir, "published", "stale")
	c.Assert(os.MkdirAll(currentDir, 0o755), IsNil)
	c.Assert(os.MkdirAll(staleDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(currentDir, ".snapd-inactive"), []byte("old-boot"), 0o644), IsNil)
	c.Assert(os.Symlink(filepath.Join("published", "current"), filepath.Join(dirs.SnapdPKIV1Dir, "merged")), IsNil)

	err := certstate.GarbageCollectCertificateGenerations("boot-1")
	c.Assert(err, IsNil)

	_, err = os.Stat(filepath.Join(currentDir, ".snapd-inactive"))
	c.Check(os.IsNotExist(err), Equals, true)
	c.Assert(filepath.Join(staleDir, ".snapd-inactive"), testutil.FileEquals, "boot-1")

	err = certstate.GarbageCollectCertificateGenerations("boot-2")
	c.Assert(err, IsNil)
	_, err = os.Stat(staleDir)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *certsTestSuite) TestGarbageCollectCertificateGenerationsRemovesStaleStagingDirectories(c *C) {
	currentDir := filepath.Join(dirs.SnapdPKIV1Dir, "published", "current")
	stagingDir := filepath.Join(dirs.SnapdPKIV1Dir, "published", ".generation-crash-leftover")
	c.Assert(os.MkdirAll(currentDir, 0o755), IsNil)
	c.Assert(os.MkdirAll(stagingDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(stagingDir, "partial"), []byte("leftover"), 0o644), IsNil)
	c.Assert(os.Symlink(filepath.Join("published", "current"), filepath.Join(dirs.SnapdPKIV1Dir, "merged")), IsNil)

	err := certstate.GarbageCollectCertificateGenerations("boot-1")
	c.Assert(err, IsNil)

	_, err = os.Stat(stagingDir)
	c.Check(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(currentDir)
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(currentDir, ".snapd-inactive"))
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *certsTestSuite) TestCertificatePathAddsCrtExtension(c *C) {
	path := certstate.CertificatePath("my-cert")
	c.Check(path, Equals, filepath.Join(dirs.SnapdPKIV1Dir, "my-cert.crt"))
}

func (s *certsTestSuite) TestWriteCertificateAndRemoveCertificate(c *C) {
	certPEM, _, err := makeTestCertPEM("write-remove")
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(dirs.SnapdPKIV1Dir, 0o755), IsNil)

	err = certstate.WriteCertificate("cert-write", string(certPEM))
	c.Assert(err, IsNil)

	certPath := filepath.Join(dirs.SnapdPKIV1Dir, "cert-write.crt")
	c.Assert(certPath, testutil.FileEquals, string(certPEM))

	err = certstate.RemoveCertificate("cert-write")
	c.Assert(err, IsNil)

	_, err = os.Stat(certPath)
	c.Check(os.IsNotExist(err), Equals, true)

	// Removing again should be idempotent.
	err = certstate.RemoveCertificate("cert-write")
	c.Assert(err, IsNil)
}

func (s *certsTestSuite) TestSetCertificateStateAndRemoveCertificateSymlinks(c *C) {
	certPEM, _, err := makeTestCertPEM("state-links")
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(dirs.SnapdPKIV1Dir, 0o755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapdPKIV1Dir, "added"), 0o755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapdPKIV1Dir, "blocked"), 0o755), IsNil)

	err = certstate.WriteCertificate("cert-state", string(certPEM))
	c.Assert(err, IsNil)

	digest := digestForPEM(c, certPEM)

	err = certstate.SetCertificateState("cert-state", digest, certstate.CertificateStateAccepted)
	c.Assert(err, IsNil)
	addedPath := filepath.Join(dirs.SnapdPKIV1Dir, "added", digest+".crt")
	target, err := os.Readlink(addedPath)
	c.Assert(err, IsNil)
	c.Check(target, Equals, "../cert-state.crt")

	err = certstate.RemoveCertificateSymlinks(digest)
	c.Assert(err, IsNil)

	_, err = os.Lstat(addedPath)
	c.Check(os.IsNotExist(err), Equals, true)

	// Idempotent removal.
	err = certstate.RemoveCertificateSymlinks(digest)
	c.Assert(err, IsNil)

	err = certstate.SetCertificateState("cert-state", digest, certstate.CertificateStateBlocked)
	c.Assert(err, IsNil)
	blockedPath := filepath.Join(dirs.SnapdPKIV1Dir, "blocked", digest+".crt")
	target, err = os.Readlink(blockedPath)
	c.Assert(err, IsNil)
	c.Check(target, Equals, "../cert-state.crt")
}

func (s *certsTestSuite) TestSetCertificateStateRepeatedSameStateIsNoop(c *C) {
	certPEM, _, err := makeTestCertPEM("state-repeat")
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(dirs.SnapdPKIV1Dir, 0o755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapdPKIV1Dir, "added"), 0o755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapdPKIV1Dir, "blocked"), 0o755), IsNil)

	err = certstate.WriteCertificate("cert-repeat", string(certPEM))
	c.Assert(err, IsNil)

	digest := digestForPEM(c, certPEM)
	addedPath := filepath.Join(dirs.SnapdPKIV1Dir, "added", digest+".crt")
	blockedPath := filepath.Join(dirs.SnapdPKIV1Dir, "blocked", digest+".crt")

	err = certstate.SetCertificateState("cert-repeat", digest, certstate.CertificateStateAccepted)
	c.Assert(err, IsNil)
	err = certstate.SetCertificateState("cert-repeat", digest, certstate.CertificateStateAccepted)
	c.Assert(err, IsNil)

	target, err := os.Readlink(addedPath)
	c.Assert(err, IsNil)
	c.Check(target, Equals, "../cert-repeat.crt")

	err = certstate.RemoveCertificateSymlinks(digest)
	c.Assert(err, IsNil)

	err = certstate.SetCertificateState("cert-repeat", digest, certstate.CertificateStateBlocked)
	c.Assert(err, IsNil)
	err = certstate.SetCertificateState("cert-repeat", digest, certstate.CertificateStateBlocked)
	c.Assert(err, IsNil)

	target, err = os.Readlink(blockedPath)
	c.Assert(err, IsNil)
	c.Check(target, Equals, "../cert-repeat.crt")
}

func (s *certsTestSuite) TestCustomCertificatesReturnsInfoAndSkipsBroken(c *C) {
	certAccepted, _, err := makeTestCertPEM("accepted")
	c.Assert(err, IsNil)
	certBlocked, _, err := makeTestCertPEM("blocked")
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(dirs.SnapdPKIV1Dir, 0o755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapdPKIV1Dir, "added"), 0o755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapdPKIV1Dir, "blocked"), 0o755), IsNil)

	err = certstate.WriteCertificate("cert-accepted", string(certAccepted))
	c.Assert(err, IsNil)
	err = certstate.WriteCertificate("cert-blocked", string(certBlocked))
	c.Assert(err, IsNil)

	acceptedDigest := digestForPEM(c, certAccepted)
	blockedDigest := digestForPEM(c, certBlocked)

	err = certstate.SetCertificateState("cert-accepted", acceptedDigest, certstate.CertificateStateAccepted)
	c.Assert(err, IsNil)
	err = certstate.SetCertificateState("cert-blocked", blockedDigest, certstate.CertificateStateBlocked)
	c.Assert(err, IsNil)

	// Broken cert should be ignored by CustomCertificates and not fail the call.
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapdPKIV1Dir, "broken.crt"), []byte("not-a-certificate"), 0o644), IsNil)

	infos, err := certstate.CustomCertificates()
	c.Assert(err, IsNil)

	byName := make(map[string]*certstate.CertificateInfo)
	for _, info := range infos {
		byName[info.Name] = info
	}

	c.Assert(byName["cert-accepted"], NotNil)
	c.Check(byName["cert-accepted"].Fingerprint, Equals, acceptedDigest)
	c.Check(byName["cert-accepted"].State, Equals, certstate.CertificateStateAccepted)

	c.Assert(byName["cert-blocked"], NotNil)
	c.Check(byName["cert-blocked"].Fingerprint, Equals, blockedDigest)
	c.Check(byName["cert-blocked"].State, Equals, certstate.CertificateStateBlocked)

	_, exists := byName["broken"]
	c.Check(exists, Equals, false)
}

func (s *certsTestSuite) TestCustomCertificatesMissingDirReturnsNil(c *C) {
	c.Assert(os.RemoveAll(dirs.SnapdPKIV1Dir), IsNil)

	infos, err := certstate.CustomCertificates()
	c.Assert(err, IsNil)
	c.Check(infos, IsNil)
}

func (s *certsTestSuite) TestCustomCertificateInfoAccepted(c *C) {
	certPEM, _, err := makeTestCertPEM("custom-info-accepted")
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(dirs.SnapdPKIV1Dir, 0o755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapdPKIV1Dir, "added"), 0o755), IsNil)

	err = certstate.WriteCertificate("cert-info", string(certPEM))
	c.Assert(err, IsNil)
	digest := digestForPEM(c, certPEM)
	err = certstate.SetCertificateState("cert-info", digest, certstate.CertificateStateAccepted)
	c.Assert(err, IsNil)

	info, err := certstate.CustomCertificateInfo("cert-info")
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)
	c.Check(info.Name, Equals, "cert-info")
	c.Check(info.Fingerprint, Equals, digest)
	c.Check(info.State, Equals, certstate.CertificateStateAccepted)
	c.Check(info.Content, Equals, string(certPEM))
}

func (s *certsTestSuite) TestCustomCertificateInfoUnsetWhenNoSymlink(c *C) {
	certPEM, _, err := makeTestCertPEM("custom-info-unset")
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(dirs.SnapdPKIV1Dir, 0o755), IsNil)

	err = certstate.WriteCertificate("cert-info-unset", string(certPEM))
	c.Assert(err, IsNil)
	digest := digestForPEM(c, certPEM)

	info, err := certstate.CustomCertificateInfo("cert-info-unset")
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)
	c.Check(info.Name, Equals, "cert-info-unset")
	c.Check(info.Fingerprint, Equals, digest)
	c.Check(info.State, Equals, certstate.CertificateStateUnset)
	c.Check(info.Content, Equals, string(certPEM))
}

func (s *certsTestSuite) TestCustomCertificateInfoMissingCertificate(c *C) {
	c.Assert(os.MkdirAll(dirs.SnapdPKIV1Dir, 0o755), IsNil)

	_, err := certstate.CustomCertificateInfo("does-not-exist")
	c.Assert(err, NotNil)
	c.Check(errors.Is(err, os.ErrNotExist), Equals, true)
	c.Check(err, ErrorMatches, `cannot read certificate "does-not-exist": .*`)
}
