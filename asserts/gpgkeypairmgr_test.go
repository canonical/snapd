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
	"regexp"
	"time"

	. "gopkg.in/check.v1"

	"golang.org/x/crypto/openpgp/packet"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/osutil"
)

type gpgKeypairMgrSuite struct {
	homedir    string
	keypairMgr asserts.KeypairManager
}

var _ = Suite(&gpgKeypairMgrSuite{})

func (gkms *gpgKeypairMgrSuite) SetUpSuite(c *C) {
	c.Skip("FIX LAST!")
	if !osutil.FileExists("/usr/bin/gpg") {
		c.Skip("gpg not installed")
	}
}

func (gkms *gpgKeypairMgrSuite) importKey(key string) {
	assertstest.GPGImportKey(gkms.homedir, key)
}

func (gkms *gpgKeypairMgrSuite) SetUpTest(c *C) {
	gkms.homedir = c.MkDir()
	gkms.keypairMgr = asserts.NewGPGKeypairManager(gkms.homedir)
	// import test key
	gkms.importKey(assertstest.DevKey)
}

func (gkms *gpgKeypairMgrSuite) TestGetPublicKeyLooksGood(c *C) {
	got, err := gkms.keypairMgr.Get("auth-id1", assertstest.DevKeyID)
	c.Assert(err, IsNil)
	sha3_384 := got.PublicKey().SHA3_384()
	c.Check(sha3_384, Equals, assertstest.DevKeyFingerprint)
}

func (gkms *gpgKeypairMgrSuite) TestGetNotFound(c *C) {
	got, err := gkms.keypairMgr.Get("auth-id1", "ffffffffffffffff")
	c.Check(err, ErrorMatches, `cannot find key "ffffffffffffffff" in GPG keyring`)
	c.Check(got, IsNil)
}

func (gkms *gpgKeypairMgrSuite) TestUseInSigning(c *C) {
	store := assertstest.NewStoreStack("trusted", testPrivKey0, testPrivKey1)

	devKey, err := gkms.keypairMgr.Get("dev1", assertstest.DevKeyID)
	c.Assert(err, IsNil)

	devAcct := assertstest.NewAccount(store, "devel1", map[string]interface{}{
		"account-id": "dev1-id",
	}, "")
	devAccKey := assertstest.NewAccountKey(store, devAcct, nil, devKey.PublicKey(), "")

	signDB, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: gkms.keypairMgr,
	})
	c.Assert(err, IsNil)

	checkDB, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: asserts.NewMemoryKeypairManager(),
		Backstore:      asserts.NewMemoryBackstore(),
		Trusted:        store.Trusted,
	})
	c.Assert(err, IsNil)
	// add store key
	err = checkDB.Add(store.StoreAccountKey(""))
	c.Assert(err, IsNil)
	// enable devel key
	err = checkDB.Add(devAcct)
	c.Assert(err, IsNil)
	err = checkDB.Add(devAccKey)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"authority-id":  "dev1-id",
		"snap-sha3-384": blobSHA3_384,
		"snap-id":       "snap-id-1",
		"grade":         "devel",
		"snap-size":     "1025",
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapBuild, err := signDB.Sign(asserts.SnapBuildType, headers, nil, assertstest.DevKeyID)
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
	gkms.importKey(dsaKey)
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

	_, err := gkms.keypairMgr.Get("auth-id1", assertstest.DevKeyID)
	c.Check(err, ErrorMatches, fmt.Sprintf("cannot use GPG key %q: cannot select exported public key, found many", assertstest.DevKeyID))
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

	_, err := gkms.keypairMgr.Get("auth-id1", assertstest.DevKeyID)
	c.Check(err, ErrorMatches, fmt.Sprintf("cannot use GPG key %q: need at least 4096 bits key, got 512", assertstest.DevKeyID))
}

func (gkms *gpgKeypairMgrSuite) TestUseInSigningBrokenSignature(c *C) {
	_, privk := assertstest.ReadPrivKey(assertstest.DevKey)

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
		// XXX: hack for now
		gpgPrivK := packet.NewRSAPrivateKey(time.Unix(1, 0), privk)
		sig.IssuerKeyId = &gpgPrivK.KeyId

		// poking to break the signature
		cont := breakSig(sig, input)

		h := sig.Hash.New()
		h.Write([]byte(cont))

		err := sig.Sign(h, gpgPrivK, nil)
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

	headers := map[string]interface{}{
		"authority-id":  "dev1-id",
		"snap-sha3-384": blobSHA3_384,
		"snap-id":       "snap-id-1",
		"grade":         "devel",
		"snap-size":     "1025",
		"timestamp":     time.Now().Format(time.RFC3339),
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
		}, regexp.QuoteMeta(fmt.Sprintf("cannot sign assertion: bad GPG produced signature: wrong key id (expected %q): ffffffffffffffff", assertstest.DevKeyID))},
		{func(sig *packet.Signature, cont []byte) []byte {
			return cont[:5]
		}, "cannot sign assertion: bad GPG produced signature: it does not verify:.*"},
	}

	for _, t := range tests {
		breakSig = t.breakSig

		_, err = signDB.Sign(asserts.SnapBuildType, headers, nil, assertstest.DevKeyID)
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

	headers := map[string]interface{}{
		"authority-id":  "dev1-id",
		"snap-sha3-384": blobSHA3_384,
		"snap-id":       "snap-id-1",
		"grade":         "devel",
		"snap-size":     "1025",
		"timestamp":     time.Now().Format(time.RFC3339),
	}

	_, err = signDB.Sign(asserts.SnapBuildType, headers, nil, assertstest.DevKeyID)
	c.Check(err, ErrorMatches, "cannot sign assertion: cannot sign using GPG: boom")
}
