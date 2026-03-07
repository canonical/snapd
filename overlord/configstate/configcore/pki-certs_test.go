// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

package configcore_test

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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/certstate"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
)

type pkiCertsSuite struct {
	configcoreSuite
}

var _ = Suite(&pkiCertsSuite{})

type orderedMockConf struct {
	*mockConf
	orderedChanges []string
}

func (cfg *orderedMockConf) Changes() []string {
	out := make([]string, 0, len(cfg.orderedChanges))
	for _, k := range cfg.orderedChanges {
		out = append(out, "core."+k)
	}
	return out
}

func makePKITestCertPEM(c *C, commonName string) []byte {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	c.Assert(err, IsNil)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	c.Assert(err, IsNil)

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,

		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	c.Assert(err, IsNil)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func certDigest(c *C, certPEM []byte) string {
	parsed, err := certstate.ParseCertificateData(certPEM)
	c.Assert(err, IsNil)
	return parsed.Digest
}

func certificateDatabaseContains(c *C, certPEM []byte) bool {
	bundlePath := filepath.Join(dirs.SnapdPKIV1Dir, "merged", "ca-certificates.crt")
	bundle, err := os.ReadFile(bundlePath)
	if os.IsNotExist(err) {
		return false
	}
	c.Assert(err, IsNil)
	return bytes.Contains(bundle, certPEM)
}

func assertCertificateDatabaseContains(c *C, certPEM []byte, expected bool) {
	c.Check(certificateDatabaseContains(c, certPEM), Equals, expected)
}

func assertSymlinkTarget(c *C, path, expectedTarget string) {
	target, err := os.Readlink(path)
	c.Assert(err, IsNil)
	c.Check(target, Equals, expectedTarget)
}

func assertSymlinkAbsent(c *C, path string) {
	_, err := os.Lstat(path)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *pkiCertsSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc"), 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "etc", "environment"), nil, 0o644), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc", "ssl", "certs"), 0o755), IsNil)

	for _, dir := range []string{
		dirs.SnapdPKIV1Dir,
		filepath.Join(dirs.SnapdPKIV1Dir, "added"),
		filepath.Join(dirs.SnapdPKIV1Dir, "blocked"),
		filepath.Join(dirs.SnapdPKIV1Dir, "merged"),
	} {
		c.Assert(os.MkdirAll(dir, 0o755), IsNil)
	}
}

func (s *pkiCertsSuite) TestValidateCustomCertificateRequestInvalidState(c *C) {
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]any{
			"pki.certs.custom.cert1.state": "bad-state",
		},
	})
	c.Assert(err, ErrorMatches, `invalid state value for "pki.certs.custom.cert1.state": "bad-state"`)
}

func (s *pkiCertsSuite) TestValidateCustomCertificateRequestInvalidContent(c *C) {
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]any{
			"pki.certs.custom.cert1.content": "not-a-certificate",
		},
	})
	c.Assert(err, ErrorMatches, `invalid certificate content for "pki.certs.custom.cert1.content": .*`)
}

func (s *pkiCertsSuite) TestValidateCustomCertificateRequestInvalidKey(c *C) {
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]any{
			"pki.certs.custom.cert1.too.many.parts": "value",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse custom certificate option "pki.certs.custom.cert1.too.many.parts"`)
}

func (s *pkiCertsSuite) TestValidateCustomCertificateRequestInvalidName(c *C) {
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]any{
			"pki.certs.custom.cert1.name": "///",
		},
	})
	c.Assert(err, ErrorMatches, `invalid certificate name for "pki.certs.custom.cert1.name": "///"`)
}

func (s *pkiCertsSuite) TestValidateCustomCertificateRequestUnexpectedField(c *C) {
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]any{
			"pki.certs.custom.cert1.foo": "bar",
		},
	})
	c.Assert(err, ErrorMatches, `unexpected field "foo" in custom certificate change`)
}

func (s *pkiCertsSuite) TestHandleCustomCertificateAcceptedWritesFileAndSymlink(c *C) {
	certPEM := makePKITestCertPEM(c, "accepted-cert")

	cfg := &mockConf{
		state: s.state,
		changes: map[string]any{
			"pki.certs.custom.cert1.content": string(certPEM),
			"pki.certs.custom.cert1.state":   "accepted",
		},
	}

	err := configcore.Run(coreDev, cfg)
	c.Assert(err, IsNil)

	c.Assert(filepath.Join(dirs.SnapdPKIV1Dir, "cert1.crt"), testutil.FileEquals, string(certPEM))

	fingerprint, ok := cfg.conf["pki.certs.custom.cert1.fingerprint"].(string)
	c.Check(ok, Equals, true)
	c.Check(fingerprint, Not(Equals), "")

	addedLink := filepath.Join(dirs.SnapdPKIV1Dir, "added", fingerprint+".crt")
	assertSymlinkTarget(c, addedLink, "../cert1.crt")
	assertSymlinkAbsent(c, filepath.Join(dirs.SnapdPKIV1Dir, "blocked", fingerprint+".crt"))

	assertCertificateDatabaseContains(c, certPEM, true)
}

func (s *pkiCertsSuite) TestHandleCustomCertificateChangeRegeneratesDatabase(c *C) {
	certPEM := makePKITestCertPEM(c, "regenerate-db-cert")

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	oldBundle := []byte("old-bundle-content")
	mergedPath := filepath.Join(mergedDir, "ca-certificates.crt")
	c.Assert(os.WriteFile(mergedPath, oldBundle, 0o644), IsNil)

	cfg := &mockConf{
		state: s.state,
		changes: map[string]any{
			"pki.certs.custom.cert-regen.content": string(certPEM),
			"pki.certs.custom.cert-regen.state":   "accepted",
		},
	}

	err := configcore.Run(coreDev, cfg)
	c.Assert(err, IsNil)

	newBundle, err := os.ReadFile(mergedPath)
	c.Assert(err, IsNil)
	c.Check(bytes.Equal(newBundle, oldBundle), Equals, false)
	assertCertificateDatabaseContains(c, certPEM, true)

	bak, err := os.ReadFile(filepath.Join(mergedDir, "ca-certificates.crt.old"))
	c.Assert(err, IsNil)
	c.Check(bak, DeepEquals, oldBundle)
}

func (s *pkiCertsSuite) TestHandleCustomCertificateBlockedWritesBlockedSymlink(c *C) {
	certPEM := makePKITestCertPEM(c, "blocked-cert")

	cfg := &mockConf{
		state: s.state,
		changes: map[string]any{
			"pki.certs.custom.cert2.content": string(certPEM),
			"pki.certs.custom.cert2.state":   "blocked",
		},
	}

	err := configcore.Run(coreDev, cfg)
	c.Assert(err, IsNil)

	fingerprint, ok := cfg.conf["pki.certs.custom.cert2.fingerprint"].(string)
	c.Assert(ok, Equals, true)
	c.Assert(fingerprint, Not(Equals), "")

	blockedLink := filepath.Join(dirs.SnapdPKIV1Dir, "blocked", fingerprint+".crt")
	assertSymlinkTarget(c, blockedLink, "../cert2.crt")
	assertSymlinkAbsent(c, filepath.Join(dirs.SnapdPKIV1Dir, "added", fingerprint+".crt"))

	assertCertificateDatabaseContains(c, certPEM, false)
}

func (s *pkiCertsSuite) TestHandleCustomCertificateUnsetRemovesFilesAndClearsFingerprint(c *C) {
	certPEM := makePKITestCertPEM(c, "unset-cert")
	fingerprint := certDigest(c, certPEM)

	pkiDir := dirs.SnapdPKIV1Dir
	addedDir := filepath.Join(pkiDir, "added")
	blockedDir := filepath.Join(pkiDir, "blocked")
	c.Assert(os.WriteFile(filepath.Join(pkiDir, "cert3.crt"), certPEM, 0o644), IsNil)
	c.Assert(os.Symlink("../cert3.crt", filepath.Join(addedDir, fingerprint+".crt")), IsNil)
	c.Assert(os.Symlink("../cert3.crt", filepath.Join(blockedDir, fingerprint+".crt")), IsNil)

	cfg := &mockConf{
		state: s.state,
		conf: map[string]any{
			"pki.certs.custom.cert3.fingerprint": fingerprint,
		},
		changes: map[string]any{
			"pki.certs.custom.cert3.content": "",
			"pki.certs.custom.cert3.state":   "unset",
		},
	}

	err := configcore.Run(coreDev, cfg)
	c.Assert(err, IsNil)

	_, err = os.Stat(filepath.Join(pkiDir, "cert3.crt"))
	c.Check(os.IsNotExist(err), Equals, true)

	assertSymlinkAbsent(c, filepath.Join(addedDir, fingerprint+".crt"))
	assertSymlinkAbsent(c, filepath.Join(blockedDir, fingerprint+".crt"))

	newFingerprint, ok := cfg.conf["pki.certs.custom.cert3.fingerprint"].(string)
	c.Assert(ok, Equals, true)
	c.Check(newFingerprint, Equals, "")

	assertCertificateDatabaseContains(c, certPEM, false)
}

func (s *pkiCertsSuite) TestHandleCustomCertificateUnsetWithoutPreviousFingerprintFails(c *C) {
	cfg := &mockConf{
		state: s.state,
		changes: map[string]any{
			"pki.certs.custom.certUnsetNoFp.content": "",
			"pki.certs.custom.certUnsetNoFp.state":   "unset",
		},
	}

	err := configcore.Run(coreDev, cfg)
	c.Assert(err, ErrorMatches, `cannot parse certificate content for "certUnsetNoFp": .*`)
}

func (s *pkiCertsSuite) TestHandleCustomCertificateUnsetWithContentForNewCert(c *C) {
	certPEM := makePKITestCertPEM(c, "unset-with-content")

	cfg := &mockConf{
		state: s.state,
		changes: map[string]any{
			"pki.certs.custom.certUnsetNew.content": string(certPEM),
			"pki.certs.custom.certUnsetNew.state":   "unset",
		},
	}

	err := configcore.Run(coreDev, cfg)
	c.Assert(err, IsNil)

	certPath := filepath.Join(dirs.SnapdPKIV1Dir, "certUnsetNew.crt")
	c.Assert(certPath, testutil.FileEquals, string(certPEM))

	fingerprint, ok := cfg.conf["pki.certs.custom.certUnsetNew.fingerprint"].(string)
	c.Assert(ok, Equals, true)
	c.Check(fingerprint, Not(Equals), "")

	assertSymlinkAbsent(c, filepath.Join(dirs.SnapdPKIV1Dir, "added", fingerprint+".crt"))
	assertSymlinkAbsent(c, filepath.Join(dirs.SnapdPKIV1Dir, "blocked", fingerprint+".crt"))
	assertCertificateDatabaseContains(c, certPEM, false)
}

func (s *pkiCertsSuite) TestHandleCustomCertificateStateOnlyUpdateUsesStoredContent(c *C) {
	certPEM := makePKITestCertPEM(c, "state-only-update")
	fingerprint := certDigest(c, certPEM)

	pkiDir := dirs.SnapdPKIV1Dir
	addedDir := filepath.Join(pkiDir, "added")
	c.Assert(os.WriteFile(filepath.Join(pkiDir, "cert-stateonly.crt"), certPEM, 0o644), IsNil)
	c.Assert(os.Symlink("../cert-stateonly.crt", filepath.Join(addedDir, fingerprint+".crt")), IsNil)

	cfg := &mockConf{
		state: s.state,
		conf: map[string]any{
			"pki.certs.custom.cert-stateonly.fingerprint": fingerprint,
		},
		changes: map[string]any{
			"pki.certs.custom.cert-stateonly.state": "blocked",
		},
	}

	err := configcore.Run(coreDev, cfg)
	c.Assert(err, IsNil)

	assertSymlinkAbsent(c, filepath.Join(addedDir, fingerprint+".crt"))
	assertSymlinkTarget(c, filepath.Join(pkiDir, "blocked", fingerprint+".crt"), "../cert-stateonly.crt")
	c.Assert(filepath.Join(pkiDir, "cert-stateonly.crt"), testutil.FileEquals, string(certPEM))
	assertCertificateDatabaseContains(c, certPEM, false)
}

func (s *pkiCertsSuite) TestValidateCustomCertificateRequestNewCertContentFirstPasses(c *C) {
	certPEM := makePKITestCertPEM(c, "order-check-pass")

	base := &mockConf{
		state: s.state,
		changes: map[string]any{
			"pki.certs.custom.cert-order-pass.content": string(certPEM),
			"pki.certs.custom.cert-order-pass.state":   "accepted",
		},
	}

	cfg := &orderedMockConf{
		mockConf: base,
		orderedChanges: []string{
			"pki.certs.custom.cert-order-pass.content",
			"pki.certs.custom.cert-order-pass.state",
		},
	}

	err := configcore.Run(coreDev, cfg)
	c.Assert(err, IsNil)
}

func (s *pkiCertsSuite) TestHandleCustomCertificateAcceptedSymlinkConflictIsReplaced(c *C) {
	certPEM := makePKITestCertPEM(c, "accepted-symlink-conflict")
	fingerprint := certDigest(c, certPEM)

	addedDir := filepath.Join(dirs.SnapdPKIV1Dir, "added")
	conflictPath := filepath.Join(addedDir, fingerprint+".crt")
	c.Assert(os.Symlink("../some-other.crt", conflictPath), IsNil)

	cfg := &mockConf{
		state: s.state,
		changes: map[string]any{
			"pki.certs.custom.certConflict.content": string(certPEM),
			"pki.certs.custom.certConflict.state":   "accepted",
		},
	}

	err := configcore.Run(coreDev, cfg)
	c.Assert(err, IsNil)
	assertSymlinkTarget(c, conflictPath, "../certConflict.crt")
}

func (s *pkiCertsSuite) TestHandleCustomCertificateReplacesOldFingerprintSymlinks(c *C) {
	oldCertPEM := makePKITestCertPEM(c, "old-cert")
	newCertPEM := makePKITestCertPEM(c, "new-cert")
	oldFingerprint := certDigest(c, oldCertPEM)
	newFingerprint := certDigest(c, newCertPEM)

	pkiDir := dirs.SnapdPKIV1Dir
	addedDir := filepath.Join(pkiDir, "added")
	blockedDir := filepath.Join(pkiDir, "blocked")
	c.Assert(os.WriteFile(filepath.Join(pkiDir, "cert1.crt"), oldCertPEM, 0o644), IsNil)
	c.Assert(os.Symlink("../cert1.crt", filepath.Join(addedDir, oldFingerprint+".crt")), IsNil)
	c.Assert(os.Symlink("../cert1.crt", filepath.Join(blockedDir, oldFingerprint+".crt")), IsNil)

	cfg := &mockConf{
		state: s.state,
		conf: map[string]any{
			"pki.certs.custom.cert1.fingerprint": oldFingerprint,
		},
		changes: map[string]any{
			"pki.certs.custom.cert1.content": string(newCertPEM),
			"pki.certs.custom.cert1.state":   "accepted",
		},
	}

	err := configcore.Run(coreDev, cfg)
	c.Assert(err, IsNil)

	assertSymlinkAbsent(c, filepath.Join(addedDir, oldFingerprint+".crt"))
	assertSymlinkAbsent(c, filepath.Join(blockedDir, oldFingerprint+".crt"))

	newLink := filepath.Join(addedDir, newFingerprint+".crt")
	assertSymlinkTarget(c, newLink, "../cert1.crt")

	newCfgFingerprint, ok := cfg.conf["pki.certs.custom.cert1.fingerprint"].(string)
	c.Assert(ok, Equals, true)
	c.Check(newCfgFingerprint, Equals, newFingerprint)

	assertCertificateDatabaseContains(c, oldCertPEM, false)
	assertCertificateDatabaseContains(c, newCertPEM, true)
}

func (s *pkiCertsSuite) TestHandleCustomCertificateRenameMovesCertAndFingerprint(c *C) {
	oldCertPEM := makePKITestCertPEM(c, "rename-old")
	newCertPEM := makePKITestCertPEM(c, "rename-new")
	oldFingerprint := certDigest(c, oldCertPEM)
	newFingerprint := certDigest(c, newCertPEM)

	pkiDir := dirs.SnapdPKIV1Dir
	addedDir := filepath.Join(pkiDir, "added")
	c.Assert(os.WriteFile(filepath.Join(pkiDir, "oldcert.crt"), oldCertPEM, 0o644), IsNil)
	c.Assert(os.Symlink("../oldcert.crt", filepath.Join(addedDir, oldFingerprint+".crt")), IsNil)

	base := &mockConf{
		state: s.state,
		conf: map[string]any{
			"pki.certs.custom.oldcert.fingerprint": oldFingerprint,
		},
		changes: map[string]any{
			"pki.certs.custom.oldcert.content": string(newCertPEM),
			"pki.certs.custom.oldcert.state":   "accepted",
			"pki.certs.custom.oldcert.name":    "newcert",
		},
	}

	cfg := &orderedMockConf{
		mockConf: base,
		orderedChanges: []string{
			"pki.certs.custom.oldcert.content",
			"pki.certs.custom.oldcert.state",
			"pki.certs.custom.oldcert.name",
		},
	}

	err := configcore.Run(coreDev, cfg)
	c.Assert(err, IsNil)

	_, err = os.Stat(filepath.Join(pkiDir, "oldcert.crt"))
	c.Check(os.IsNotExist(err), Equals, true)
	c.Assert(filepath.Join(pkiDir, "newcert.crt"), testutil.FileEquals, string(newCertPEM))

	assertSymlinkAbsent(c, filepath.Join(addedDir, oldFingerprint+".crt"))
	assertSymlinkTarget(c, filepath.Join(addedDir, newFingerprint+".crt"), "../newcert.crt")

	oldCfgFingerprint, ok := base.conf["pki.certs.custom.oldcert.fingerprint"].(string)
	c.Assert(ok, Equals, true)
	c.Check(oldCfgFingerprint, Equals, "")

	newCfgFingerprint, ok := base.conf["pki.certs.custom.newcert.fingerprint"].(string)
	c.Assert(ok, Equals, true)
	c.Check(newCfgFingerprint, Equals, newFingerprint)

	assertCertificateDatabaseContains(c, oldCertPEM, false)
	assertCertificateDatabaseContains(c, newCertPEM, true)
}

func (s *pkiCertsSuite) TestHandleGetCustomCertificatesMissingDirReturnsEmpty(c *C) {
	res, err := configcore.HandleGetCustomCertificates("pki.certs.custom")
	c.Assert(err, IsNil)

	certs, ok := res.([]*certstate.CertificateInfo)
	c.Assert(ok, Equals, true)
	c.Check(certs, HasLen, 0)
}

func (s *pkiCertsSuite) TestHandleGetCustomCertificatesIgnoresNonCrtFiles(c *C) {
	certPEM := makePKITestCertPEM(c, "query-ignore-noncrt")
	pkiDir := dirs.SnapdPKIV1Dir
	c.Assert(os.WriteFile(filepath.Join(pkiDir, "real.crt"), certPEM, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(pkiDir, "ignored.pem"), certPEM, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(pkiDir, "note.txt"), []byte("x"), 0o644), IsNil)

	res, err := configcore.HandleGetCustomCertificates("pki.certs.custom")
	c.Assert(err, IsNil)

	certs, ok := res.([]*certstate.CertificateInfo)
	c.Assert(ok, Equals, true)
	c.Check(certs, HasLen, 1)
	c.Check(certs[0].Name, Equals, "real")
}

func (s *pkiCertsSuite) TestHandleGetCustomCertificatesBlockedPrecedesAccepted(c *C) {
	certPEM := makePKITestCertPEM(c, "query-blocked-precedence")
	fingerprint := certDigest(c, certPEM)

	pkiDir := dirs.SnapdPKIV1Dir
	addedDir := filepath.Join(pkiDir, "added")
	blockedDir := filepath.Join(pkiDir, "blocked")
	c.Assert(os.WriteFile(filepath.Join(pkiDir, "cert-state.crt"), certPEM, 0o644), IsNil)
	c.Assert(os.Symlink("../cert-state.crt", filepath.Join(addedDir, fingerprint+".crt")), IsNil)
	c.Assert(os.Symlink("../cert-state.crt", filepath.Join(blockedDir, fingerprint+".crt")), IsNil)

	res, err := configcore.HandleGetCustomCertificates("pki.certs.custom")
	c.Assert(err, IsNil)

	certs, ok := res.([]*certstate.CertificateInfo)
	c.Assert(ok, Equals, true)
	c.Assert(certs, HasLen, 1)
	c.Check(certs[0].Name, Equals, "cert-state")
	c.Check(certs[0].State, Equals, certstate.CertificateStateBlocked)
}

func (s *pkiCertsSuite) TestHandleGetCustomCertificatesInvalidCertReturnsError(c *C) {
	pkiDir := dirs.SnapdPKIV1Dir
	c.Assert(os.WriteFile(filepath.Join(pkiDir, "broken.crt"), []byte("not-a-certificate"), 0o644), IsNil)

	res, err := configcore.HandleGetCustomCertificates("pki.certs.custom")
	c.Assert(err, IsNil)
	certs, ok := res.([]*certstate.CertificateInfo)
	c.Assert(ok, Equals, true)
	c.Check(certs, HasLen, 0)
}

func (s *pkiCertsSuite) TestHandleGetCustomCertificatesUnreadableDirectoryReturnsError(c *C) {
	pkiDir := dirs.SnapdPKIV1Dir
	c.Assert(os.RemoveAll(pkiDir), IsNil)
	c.Assert(os.WriteFile(pkiDir, []byte("not-a-directory"), 0o644), IsNil)

	_, err := configcore.HandleGetCustomCertificates("pki.certs.custom")
	c.Assert(err, ErrorMatches, `.*: not a directory`)
}

func (s *pkiCertsSuite) TestHandleGetCustomCertificatesIncludesStateAndFingerprint(c *C) {
	certPEM := makePKITestCertPEM(c, "query-cert")
	fingerprint := certDigest(c, certPEM)

	pkiDir := dirs.SnapdPKIV1Dir
	addedDir := filepath.Join(pkiDir, "added")
	c.Assert(os.WriteFile(filepath.Join(pkiDir, "cert4.crt"), certPEM, 0o644), IsNil)
	c.Assert(os.Symlink("../cert4.crt", filepath.Join(addedDir, fingerprint+".crt")), IsNil)

	res, err := configcore.HandleGetCustomCertificates("pki.certs.custom")
	c.Assert(err, IsNil)

	certs, ok := res.([]*certstate.CertificateInfo)
	c.Assert(ok, Equals, true)
	c.Assert(certs, HasLen, 1)
	c.Check(certs[0].Name, Equals, "cert4")
	c.Check(certs[0].Fingerprint, Equals, fingerprint)
	c.Check(certs[0].State, Equals, certstate.CertificateStateAccepted)
}

func (s *pkiCertsSuite) TestParseCustomCertKey(c *C) {
	name, field, err := configcore.ParseCustomCertKey("pki.certs.custom.mycert.content")
	c.Assert(err, IsNil)
	c.Check(name, Equals, "mycert")
	c.Check(field, Equals, "content")
}

func (s *pkiCertsSuite) TestParseCustomCertKeyCases(c *C) {
	tests := []struct {
		key      string
		expName  string
		expField string
		errMatch string
	}{
		{key: "pki.certs.custom", expName: "", expField: "", errMatch: ""},
		{key: "network.proxy.http", expName: "", expField: "", errMatch: ""},
		{key: "pki.certs.custom.certx", expName: "certx", expField: "", errMatch: ""},
		{key: "pki.certs.custom.certx.state", expName: "certx", expField: "state", errMatch: ""},
		{key: "pki.certs.custom.certx.state.extra", errMatch: `cannot parse custom certificate option "pki.certs.custom.certx.state.extra"`},
	}

	for _, tc := range tests {
		name, field, err := configcore.ParseCustomCertKey(tc.key)
		if tc.errMatch != "" {
			c.Assert(err, ErrorMatches, tc.errMatch)
			continue
		}
		c.Assert(err, IsNil)
		c.Check(name, Equals, tc.expName)
		c.Check(field, Equals, tc.expField)
	}
}
