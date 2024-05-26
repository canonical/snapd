// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2021 Canonical Ltd
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
	"os"
	"time"

	"golang.org/x/crypto/openpgp/packet"
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
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
	if !osutil.FileExists("/usr/bin/gpg1") && !osutil.FileExists("/usr/bin/gpg") {
		c.Skip("gpg not installed")
	}
}

func (gkms *gpgKeypairMgrSuite) importKey(key string) {
	assertstest.GPGImportKey(gkms.homedir, key)
}

func (gkms *gpgKeypairMgrSuite) SetUpTest(c *C) {
	gkms.homedir = c.MkDir()
	os.Setenv("SNAP_GNUPG_HOME", gkms.homedir)
	gkms.keypairMgr = asserts.NewGPGKeypairManager()
	// import test key
	gkms.importKey(assertstest.DevKey)
}

func (gkms *gpgKeypairMgrSuite) TearDownTest(c *C) {
	os.Unsetenv("SNAP_GNUPG_HOME")
}

func (gkms *gpgKeypairMgrSuite) TestGetPublicKeyLooksGood(c *C) {
	got := mylog.Check2(gkms.keypairMgr.Get(assertstest.DevKeyID))

	keyID := got.PublicKey().ID()
	c.Check(keyID, Equals, assertstest.DevKeyID)
}

func (gkms *gpgKeypairMgrSuite) TestGetNotFound(c *C) {
	got := mylog.Check2(gkms.keypairMgr.Get("ffffffffffffffff"))
	c.Check(err, ErrorMatches, `cannot find key pair in GPG keyring`)
	c.Check(asserts.IsKeyNotFound(err), Equals, true)
	c.Check(got, IsNil)
}

func (gkms *gpgKeypairMgrSuite) TestGetByNameNotFound(c *C) {
	gpgKeypairMgr := gkms.keypairMgr.(*asserts.GPGKeypairManager)
	got := mylog.Check2(gpgKeypairMgr.GetByName("missing"))
	c.Check(err, ErrorMatches, `cannot find key pair in GPG keyring`)
	c.Check(asserts.IsKeyNotFound(err), Equals, true)
	c.Check(got, IsNil)
}

func (gkms *gpgKeypairMgrSuite) TestUseInSigning(c *C) {
	store := assertstest.NewStoreStack("trusted", nil)

	devKey := mylog.Check2(gkms.keypairMgr.Get(assertstest.DevKeyID))


	devAcct := assertstest.NewAccount(store, "devel1", map[string]interface{}{
		"account-id": "dev1-id",
	}, "")
	devAccKey := assertstest.NewAccountKey(store, devAcct, nil, devKey.PublicKey(), "")

	signDB := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: gkms.keypairMgr,
	}))


	checkDB := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   store.Trusted,
	}))

	mylog.
		// add store key
		Check(checkDB.Add(store.StoreAccountKey("")))

	mylog.
		// enable devel key
		Check(checkDB.Add(devAcct))

	mylog.Check(checkDB.Add(devAccKey))


	headers := map[string]interface{}{
		"authority-id":  "dev1-id",
		"snap-sha3-384": blobSHA3_384,
		"snap-id":       "snap-id-1",
		"grade":         "devel",
		"snap-size":     "1025",
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapBuild := mylog.Check2(signDB.Sign(asserts.SnapBuildType, headers, nil, assertstest.DevKeyID))

	mylog.Check(checkDB.Check(snapBuild))
	c.Check(err, IsNil)
}

func (gkms *gpgKeypairMgrSuite) TestGetNotUnique(c *C) {
	mockGPG := func(prev asserts.GPGRunner, input []byte, args ...string) ([]byte, error) {
		if args[1] == "--list-secret-keys" {
			return prev(input, args...)
		}
		c.Assert(args[1], Equals, "--export")

		pk1 := mylog.Check2(rsa.GenerateKey(rand.Reader, 512))

		pk2 := mylog.Check2(rsa.GenerateKey(rand.Reader, 512))


		buf := new(bytes.Buffer)
		mylog.Check(packet.NewRSAPublicKey(time.Now(), &pk1.PublicKey).Serialize(buf))

		mylog.Check(packet.NewRSAPublicKey(time.Now(), &pk2.PublicKey).Serialize(buf))


		return buf.Bytes(), nil
	}
	restore := asserts.MockRunGPG(mockGPG)
	defer restore()

	_ := mylog.Check2(gkms.keypairMgr.Get(assertstest.DevKeyID))
	c.Check(err, ErrorMatches, `cannot load GPG public key with fingerprint "[A-F0-9]+": cannot select exported public key, found many`)
}

func (gkms *gpgKeypairMgrSuite) TestUseInSigningBrokenSignature(c *C) {
	_, rsaPrivKey := assertstest.ReadPrivKey(assertstest.DevKey)
	pgpPrivKey := packet.NewRSAPrivateKey(time.Unix(1, 0), rsaPrivKey)

	var breakSig func(sig *packet.Signature, cont []byte) []byte

	mockGPG := func(prev asserts.GPGRunner, input []byte, args ...string) ([]byte, error) {
		if args[1] == "--list-secret-keys" || args[1] == "--export" {
			return prev(input, args...)
		}
		n := len(args)
		c.Assert(args[n-1], Equals, "--detach-sign")

		sig := new(packet.Signature)
		sig.PubKeyAlgo = packet.PubKeyAlgoRSA
		sig.Hash = crypto.SHA512
		sig.CreationTime = time.Now()

		// poking to break the signature
		cont := breakSig(sig, input)

		h := sig.Hash.New()
		h.Write([]byte(cont))
		mylog.Check(sig.Sign(h, pgpPrivKey, nil))


		buf := new(bytes.Buffer)
		sig.Serialize(buf)
		return buf.Bytes(), nil
	}
	restore := asserts.MockRunGPG(mockGPG)
	defer restore()

	signDB := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: gkms.keypairMgr,
	}))


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
			return cont[:5]
		}, "cannot sign assertion: bad GPG produced signature: it does not verify:.*"},
	}

	for _, t := range tests {
		breakSig = t.breakSig

		_ = mylog.Check2(signDB.Sign(asserts.SnapBuildType, headers, nil, assertstest.DevKeyID))
		c.Check(err, ErrorMatches, t.expectedErr)
	}
}

func (gkms *gpgKeypairMgrSuite) TestUseInSigningFailure(c *C) {
	mockGPG := func(prev asserts.GPGRunner, input []byte, args ...string) ([]byte, error) {
		if args[1] == "--list-secret-keys" || args[1] == "--export" {
			return prev(input, args...)
		}
		n := len(args)
		c.Assert(args[n-1], Equals, "--detach-sign")
		return nil, fmt.Errorf("boom")
	}
	restore := asserts.MockRunGPG(mockGPG)
	defer restore()

	signDB := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: gkms.keypairMgr,
	}))


	headers := map[string]interface{}{
		"authority-id":  "dev1-id",
		"snap-sha3-384": blobSHA3_384,
		"snap-id":       "snap-id-1",
		"grade":         "devel",
		"snap-size":     "1025",
		"timestamp":     time.Now().Format(time.RFC3339),
	}

	_ = mylog.Check2(signDB.Sign(asserts.SnapBuildType, headers, nil, assertstest.DevKeyID))
	c.Check(err, ErrorMatches, "cannot sign assertion: cannot sign using GPG: boom")
}

const shortPrivKey = `-----BEGIN PGP PRIVATE KEY BLOCK-----
Version: GnuPG v1

lQOYBFdGO7MBCADltsXglnDQdfBw0yOVpKZdkuvSnJKKn1H72PapgAr7ucLqNBCA
js0kltDTa2LQP4vljiTyoMzOMnex4kXwRPlF+poZIEBHDLT0i/6sJ6mDukss1HBR
GgNpU3y49WTXc8qxFY4clhbuqgQmy6bUmaVoo3Z4z7cqbsCepWfx5y+vJwMYqlo3
Nb4q2+hTKS/o3yLiYB7/hkEhMZrFrOPR5SM7Tz5y7cpF6ObY+JZIp/MK+LsLWLji
fEX/pcOtSjFdQqbcnhJJscXRERlFQDbc+gNmZYZ2RqdH5o46OliHkGhVDVTiW25A
SqhGfnodypbZ9QAPSRvhLrN64AqEsvRb3I13ABEBAAEAB/9cQKg8Nz6sQUkkDm9C
iCK1/qyNYwro9+3VXj9FOCJxEJuqMemUr4TMVnMcDQrchkC5GnpVJGXLw3HVcwFS
amjPhUKAp7aYsg40DcrjuXP27oiFQvWuZGuNT5WNtCNg8WQr9POjIFWqWIYdTHk9
9Ux79vW7s/Oj62GY9OWHPSilxpq1MjDKo9CSMbLeWxW+gbDxaD7cK7H/ONcz8bZ7
pRfEhNIx3mEbWaZpWRrf+dSUx2OJbPGRkeFFMbCNapqftse173BZCwUKsW7RTp2S
w8Vpo2Ky63Jlpz1DpoMDBz2vSH7pzaqAdnziI2r0IKiidajXFfpXJpJ3ICo/QhWj
x1eRBADrI4I99zHeyy+12QMpkDrOu+ahF6/emdsm1FIy88TqeBmLkeXCXKZIpU3c
USnxzm0nPNbOl7Nvf2VdAyeAftyag7t38Cud5MXldv/iY0e6oTKzxgha37yr6oRv
PZ6VGwbkBvWti1HL4yx1QnkHFS6ailR9WiiHr3HaWAklZAsC0QQA+hgOi0V9fMZZ
Y4/iFVRI9k1NK3pl0mP7pVTzbcjVYspLdIPQxPDsHJW0z48g23KOt0vL3yZvxdBx
cfYGqIonAX19aMD5D4bNLx616pZs78DKGlOz6iXDcaib+n/uCNWxd5R/0m/zugrB
qklpyIC/uxx+SmkJqqq378ytfvBMzccD/3Y6m3PM0ZnrIkr4Q7cKi9ao9rvM+J7o
ziMgfnKWedNDxNa4tIVYYGPiXsjxY/ASUyxVjUPbkyCy3ubZrew0zQ9+kQbO/6vB
WAg9ffT9M92QbSDjuxgUiC5GfvlCoDgJtuLRHd0YLDgUCS5nwb+teEsOpiNWEGXc
Tr+5HZO+g6wxT6W0BiAoeHh4KYkBOAQTAQIAIgUCV0Y7swIbLwYLCQgHAwIGFQgC
CQoLBBYCAwECHgECF4AACgkQEYacUJMr9p/i5wf/XbEiAe1+Y/ZNMO8PYnq1Nktk
CbZEfQo+QH/9gJpt4p78YseWeUp14gsULLks3xRojlKNzYkqBpJcP7Ex+hQ3LEp7
9IVbept5md4uuZcU0GFF42WAYXExd2cuxPv3lmWHOPuN63a/xpp0M2vYDfpt63qi
Tly5/P4+NgpD6vAh8zwRHuBV/0mno/QX6cUCLVxq2v1aOqC9zq9B5sdYKQKjsQBP
NOXCt1wPaINkqiW/8w2KhUl6mL6vhO0Onqu/F7M/YNXitv6Z2NFdFUVBh58UZW3C
2jrc8JeRQ4Qlr1oeHh2loYOdZfxFPxRjhsRTnNKY8UHWLfbeI6lMqxR5G3DS+g==
=kQRo
-----END PGP PRIVATE KEY BLOCK-----
`

func (gkms *gpgKeypairMgrSuite) TestUseInSigningKeyTooShort(c *C) {
	gkms.importKey(shortPrivKey)
	privk, _ := assertstest.ReadPrivKey(shortPrivKey)

	signDB := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: gkms.keypairMgr,
	}))


	headers := map[string]interface{}{
		"authority-id":  "dev1-id",
		"snap-sha3-384": blobSHA3_384,
		"snap-id":       "snap-id-1",
		"grade":         "devel",
		"snap-size":     "1025",
		"timestamp":     time.Now().Format(time.RFC3339),
	}

	_ = mylog.Check2(signDB.Sign(asserts.SnapBuildType, headers, nil, privk.PublicKey().ID()))
	c.Check(err, ErrorMatches, `cannot sign assertion: signing needs at least a 4096 bits key, got 2048`)
}

func (gkms *gpgKeypairMgrSuite) TestParametersForGenerate(c *C) {
	gpgKeypairMgr := gkms.keypairMgr.(*asserts.GPGKeypairManager)
	baseParameters := `
Key-Type: RSA
Key-Length: 4096
Name-Real: test-key
Creation-Date: seconds=1451606400
Preferences: SHA512
`

	tests := []struct {
		passphrase      string
		extraParameters string
	}{
		{"", ""},
		{"secret", "Passphrase: secret\n"},
	}

	for _, test := range tests {
		parameters := gpgKeypairMgr.ParametersForGenerate(test.passphrase, "test-key")
		c.Check(parameters, Equals, baseParameters+test.extraParameters)
	}
}

func (gkms *gpgKeypairMgrSuite) TestList(c *C) {
	gpgKeypairMgr := gkms.keypairMgr.(*asserts.GPGKeypairManager)

	keys := mylog.Check2(gpgKeypairMgr.List())

	c.Check(keys, HasLen, 1)
	c.Check(keys[0].ID, Equals, assertstest.DevKeyID)
	c.Check(keys[0].Name, Not(Equals), "")
}

func (gkms *gpgKeypairMgrSuite) TestDelete(c *C) {
	defer asserts.GPGBatchYes()()

	keyID := assertstest.DevKeyID
	_ := mylog.Check2(gkms.keypairMgr.Get(keyID))

	mylog.Check(gkms.keypairMgr.Delete(keyID))

	mylog.Check(gkms.keypairMgr.Delete(keyID))
	c.Check(err, ErrorMatches, `cannot find key.*`)

	_ = mylog.Check2(gkms.keypairMgr.Get(keyID))
	c.Check(err, ErrorMatches, `cannot find key.*`)
}
