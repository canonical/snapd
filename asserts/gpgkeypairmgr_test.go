// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package asserts_test

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"os/exec"
	"regexp"
	"time"

	. "gopkg.in/check.v1"

	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/packet"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/osutil"
)

type gpgKeypairMgrSuite struct {
	homedir    string
	keypairMgr asserts.KeypairManager
}

var _ = Suite(&gpgKeypairMgrSuite{})

func (gkms *gpgKeypairMgrSuite) SetUpSuite(c *C) {
	if !osutil.FileExists("/usr/bin/gpg") {
		c.Skip("gpg not installed")
	}
}

const (
	testKey = `-----BEGIN PGP PRIVATE KEY BLOCK-----
Version: GnuPG v1

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
=r01O
-----END PGP PRIVATE KEY BLOCK-----
`

	testKeyID = "b52cf438a2d078f7"

	testKeyFingerprint = "42a3050d365c10d5c093abeeb52cf438a2d078f7"
)

func (gkms *gpgKeypairMgrSuite) importKey(c *C, key string) {
	gpg := exec.Command("gpg", "--homedir", gkms.homedir, "-q", "--batch", "--import", "--armor")
	gpg.Stdin = bytes.NewBufferString(key)
	out, err := gpg.CombinedOutput()
	c.Assert(err, IsNil, Commentf("test key import failed: %v (%q)", err, out))
}

func (gkms *gpgKeypairMgrSuite) SetUpTest(c *C) {
	gkms.homedir = c.MkDir()
	gkms.keypairMgr = asserts.NewGPGKeypairManager(gkms.homedir)
	// import test key
	gkms.importKey(c, testKey)
}

func (gkms *gpgKeypairMgrSuite) TestGetPublicKeyLooksGood(c *C) {
	got, err := gkms.keypairMgr.Get("auth-id1", testKeyID)
	c.Assert(err, IsNil)
	fp := got.PublicKey().Fingerprint()
	c.Check(fp, Equals, testKeyFingerprint)
}

func (gkms *gpgKeypairMgrSuite) TestGetNotFound(c *C) {
	got, err := gkms.keypairMgr.Get("auth-id1", "ffffffffffffffff")
	c.Check(err, ErrorMatches, `cannot find key "ffffffffffffffff" in GPG keyring`)
	c.Check(got, IsNil)
}

func (gkms *gpgKeypairMgrSuite) TestUseInSigning(c *C) {
	trustedKey, err := asserts.GenerateKey()
	c.Assert(err, IsNil)

	tmgr := asserts.NewMemoryKeypairManager()
	tmgr.Put("trusted", trustedKey)

	authorityDB, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: tmgr,
	})
	c.Assert(err, IsNil)

	now := time.Now().UTC()
	headers := map[string]string{
		"authority-id":           "trusted",
		"account-id":             "trusted",
		"public-key-id":          trustedKey.PublicKey().ID(),
		"public-key-fingerprint": trustedKey.PublicKey().Fingerprint(),
		"since":                  now.Format(time.RFC3339),
		"until":                  now.AddDate(10, 0, 0).Format(time.RFC3339),
	}
	pubTrustedKeyEnc, err := asserts.EncodePublicKey(trustedKey.PublicKey())
	c.Assert(err, IsNil)
	trustedAccKey, err := authorityDB.Sign(asserts.AccountKeyType, headers, pubTrustedKeyEnc, trustedKey.PublicKey().ID())
	c.Assert(err, IsNil)

	devKey, err := gkms.keypairMgr.Get("dev1", testKeyID)
	c.Assert(err, IsNil)
	headers = map[string]string{
		"authority-id":           "trusted",
		"account-id":             "dev1-id",
		"public-key-id":          devKey.PublicKey().ID(),
		"public-key-fingerprint": devKey.PublicKey().Fingerprint(),
		"since":                  now.Format(time.RFC3339),
		"until":                  now.AddDate(10, 0, 0).Format(time.RFC3339),
	}
	pubDevKeyEnc, err := asserts.EncodePublicKey(devKey.PublicKey())
	c.Assert(err, IsNil)
	devAccKey, err := authorityDB.Sign(asserts.AccountKeyType, headers, pubDevKeyEnc, trustedKey.PublicKey().ID())
	c.Assert(err, IsNil)

	signDB, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: gkms.keypairMgr,
	})
	c.Assert(err, IsNil)

	checkDB, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: asserts.NewMemoryKeypairManager(),
		Backstore:      asserts.NewMemoryBackstore(),
		TrustedKeys:    []*asserts.AccountKey{trustedAccKey.(*asserts.AccountKey)},
	})
	c.Assert(err, IsNil)
	err = checkDB.Add(devAccKey)
	c.Assert(err, IsNil)

	headers = map[string]string{
		"authority-id": "dev1-id",
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-digest":  "sha512-...",
		"grade":        "devel",
		"snap-size":    "1025",
		"timestamp":    now.Format(time.RFC3339),
	}
	snapBuild, err := signDB.Sign(asserts.SnapBuildType, headers, nil, testKeyID)
	c.Assert(err, IsNil)

	err = checkDB.Check(snapBuild)
	c.Check(err, IsNil)
}

const (
	dsaKey = `-----BEGIN PGP PRIVATE KEY BLOCK-----
Version: GnuPG v1

lQNTBFdLWt0RCAC3sBvyl2j13gKxvnRF7DpBfN1cxba8n/qvCu2uGvlaekCFCVol
jJt594gL0QRzWPaV+KWQQroZ4u0knYA15QCbFqJ/ziX7zRI+5xcOGJ8ZBJJnDiGM
Eu7v2NGpxJHxgz1n+fjUqDPC/fHMfnQ1bkYNbXDXht2Uw9j8LP3FPueYRH46ZYQs
G91s6x+row7RCIGJcg0gVJhVvqoojk+Z+7pQ2kiNIeBeVztjybZGLlqL6fnKfeXq
TsBjnsqUIxdu286UU/xkn6sHa4APqr5wywNjvWoRyWIXxQVTWQp81PlvPfJFyCJJ
diOb6z2+sfbQ+jdB/MUXYAT2HaOhRMaP/9UPAQCr/nhHyDBb5iq1F/YdftulV9wx
cOGdWxM2AD9LnLLHGQf+Oct7QLco7SK43NzIDNvp1J/ESK6smfsIgMz6ICyj1Z21
8Rch0do/0fAiKQpAimxvMQnSE4JtT92xPPV0PdHde/Xs8QxoaKnF2XECoIqMFmjP
VLerqhyWOv3CE+MHLbj0b0WMl5DSYAcizgF6768R8To9Oow/YdEy7GFCutPoFlNE
EHW+FA0EZVwGi3BelWMEAjJS+EtJ8knP9d7Im+GHBZ41f0yWU06CWgncfQvxxrOw
9f/uO2eoTpSb4QLqyasnp4e93iul1r1sJuGYFscQUo1gXJWvGJyh+iYj/K+bk53Y
fbbc4efJOLNJ6blBLFRY1cwFWKKEmn+GtsN7TA88lAf+MOnzlSpEDMMNHSPcU/RI
KJe2VDuf3z7nP6Isy9PbPLtuXothU0iLtR76SZuVkUMtRDf+s2B79Lb5c4LQhg8H
DAiuJqUtCUmyAwwHj2cv5rZT3YuOOb80D16rHXM4Ut05oYeGNEulHG2Qsqe6pxUp
gEL7Ar2ZempjeVpN8jNqbOW8WHsYJ49CHA6pF30hGIHk2zMvBKBORa5kGEpgSDex
kZWB66bOXveUpharOwsvnaa/9SLL+DLcdaVUydrGZMPNVTmoXQmJpvNZj+7uU8IU
RYDEoe9lalEwXUv7Z2eAbMbo23AYKN4omxuaW9cp/hldiXoHgh70KGuwlBtSd+ml
bAAA/jFnXDTFL0rDbz9ykVftBS/QooNR2xZLam/0G824RpQKDyO0BiAoZHNhKYh6
BBMRCAAiBQJXS1rdAhsjBgsJCAcDAgYVCAIJCgsEFgIDAQIeAQIXgAAKCRBWv9KR
5/7U5VSKAQCSrbnVtHaGN9ZUk/PtnJMhbBTtk2R2Y4huxCdFUYQc0QD+IxU66SP+
Iri22tgdp1HhmTuG2ZyaxUs0cDgkzRTrsAg=
=Pbfn
-----END PGP PRIVATE KEY BLOCK-----
`

	dsaKeyID = "56bfd291e7fed4e5"
)

func (gkms *gpgKeypairMgrSuite) TestGetWrongKeyType(c *C) {
	gkms.importKey(c, dsaKey)
	_, err := gkms.keypairMgr.Get("auth-id1", dsaKeyID)
	c.Check(err, ErrorMatches, fmt.Sprintf(`cannot use GPG key %q: not a RSA key`, dsaKeyID))
}

func (gkms *gpgKeypairMgrSuite) TestGetNotUnique(c *C) {
	mockGPG := func(prev asserts.GPGRunner, homedir string, input []byte, args ...string) ([]byte, error) {
		c.Assert(args[1], Equals, "--export")

		pk1, err := rsa.GenerateKey(rand.Reader, 512)
		c.Assert(err, IsNil)
		pk2, err := rsa.GenerateKey(rand.Reader, 512)
		c.Assert(err, IsNil)

		buf := new(bytes.Buffer)
		err = packet.NewRSAPublicKey(time.Now(), &pk1.PublicKey).Serialize(buf)
		c.Assert(err, IsNil)
		err = packet.NewRSAPublicKey(time.Now(), &pk2.PublicKey).Serialize(buf)
		c.Assert(err, IsNil)

		return buf.Bytes(), nil
	}
	restore := asserts.MockRunGPG(mockGPG)
	defer restore()

	_, err := gkms.keypairMgr.Get("auth-id1", testKeyID)
	c.Check(err, ErrorMatches, fmt.Sprintf("cannot use GPG key %q: cannot select exported public key, found many", testKeyID))
}

func (gkms *gpgKeypairMgrSuite) TestGetWrongKeyLength(c *C) {
	mockGPG := func(prev asserts.GPGRunner, homedir string, input []byte, args ...string) ([]byte, error) {
		c.Assert(args[1], Equals, "--export")

		pk, err := rsa.GenerateKey(rand.Reader, 512)
		c.Assert(err, IsNil)
		pubPkt := packet.NewRSAPublicKey(time.Now(), &pk.PublicKey)
		buf := new(bytes.Buffer)
		err = pubPkt.Serialize(buf)
		c.Assert(err, IsNil)
		return buf.Bytes(), nil
	}
	restore := asserts.MockRunGPG(mockGPG)
	defer restore()

	_, err := gkms.keypairMgr.Get("auth-id1", testKeyID)
	c.Check(err, ErrorMatches, fmt.Sprintf("cannot use GPG key %q: need at least 4096 bits key, got 512", testKeyID))
}

func (gkms *gpgKeypairMgrSuite) TestUseInSigningBrokenSignature(c *C) {
	blk, err := armor.Decode(bytes.NewBuffer([]byte(testKey)))
	c.Assert(err, IsNil)
	pkPkt, err := packet.Read(blk.Body)
	c.Assert(err, IsNil)
	privk, ok := pkPkt.(*packet.PrivateKey)
	c.Assert(ok, Equals, true)

	var breakSig func(sig *packet.Signature, cont []byte) []byte

	mockGPG := func(prev asserts.GPGRunner, homedir string, input []byte, args ...string) ([]byte, error) {
		if args[1] == "--export" {
			return prev(homedir, input, args...)
		}
		n := len(args)
		c.Assert(args[n-1], Equals, "--detach-sign")

		sig := new(packet.Signature)
		sig.PubKeyAlgo = packet.PubKeyAlgoRSA
		sig.Hash = crypto.SHA512
		sig.CreationTime = time.Now()
		sig.IssuerKeyId = &privk.KeyId

		// poking to break the signature
		cont := breakSig(sig, input)

		h := sig.Hash.New()
		h.Write([]byte(cont))

		err := sig.Sign(h, privk, nil)
		c.Assert(err, IsNil)

		buf := new(bytes.Buffer)
		sig.Serialize(buf)
		return buf.Bytes(), nil
	}
	restore := asserts.MockRunGPG(mockGPG)
	defer restore()

	signDB, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: gkms.keypairMgr,
	})
	c.Assert(err, IsNil)

	headers := map[string]string{
		"authority-id": "dev1-id",
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-digest":  "sha512-...",
		"grade":        "devel",
		"snap-size":    "1025",
		"timestamp":    time.Now().Format(time.RFC3339),
	}

	tests := []struct {
		breakSig    func(*packet.Signature, []byte) []byte
		expectedErr string
	}{
		{func(sig *packet.Signature, cont []byte) []byte {
			sig.Hash = crypto.SHA1
			return cont
		}, "cannot sign assertion: bad GPG produced signature: expected SHA512 digest"},
		{func(sig *packet.Signature, cont []byte) []byte {
			sig.IssuerKeyId = nil
			return cont
		}, "cannot sign assertion: bad GPG produced signature: no key id in the signature"},
		{func(sig *packet.Signature, cont []byte) []byte {
			sig.IssuerKeyId = new(uint64)
			*sig.IssuerKeyId = 0xffffffffffffffff
			return cont
		}, regexp.QuoteMeta(fmt.Sprintf("cannot sign assertion: bad GPG produced signature: wrong key id (expected %q): ffffffffffffffff", testKeyID))},
		{func(sig *packet.Signature, cont []byte) []byte {
			return cont[:5]
		}, "cannot sign assertion: bad GPG produced signature: it does not verify:.*"},
	}

	for _, t := range tests {
		breakSig = t.breakSig

		_, err = signDB.Sign(asserts.SnapBuildType, headers, nil, testKeyID)
		c.Check(err, ErrorMatches, t.expectedErr)
	}

}

func (gkms *gpgKeypairMgrSuite) TestUseInSigningFailure(c *C) {
	mockGPG := func(prev asserts.GPGRunner, homedir string, input []byte, args ...string) ([]byte, error) {
		if args[1] == "--export" {
			return prev(homedir, input, args...)
		}
		n := len(args)
		c.Assert(args[n-1], Equals, "--detach-sign")
		return nil, fmt.Errorf("boom")
	}
	restore := asserts.MockRunGPG(mockGPG)
	defer restore()

	signDB, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: gkms.keypairMgr,
	})
	c.Assert(err, IsNil)

	headers := map[string]string{
		"authority-id": "dev1-id",
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-digest":  "sha512-...",
		"grade":        "devel",
		"snap-size":    "1025",
		"timestamp":    time.Now().Format(time.RFC3339),
	}

	_, err = signDB.Sign(asserts.SnapBuildType, headers, nil, testKeyID)
	c.Check(err, ErrorMatches, "cannot sign assertion: cannot sign using GPG: boom")
}
