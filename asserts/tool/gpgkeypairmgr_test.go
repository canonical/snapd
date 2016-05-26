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

package tool_test

import (
	"bytes"
	"os/exec"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/tool"
	"github.com/snapcore/snapd/osutil"
)

type gpgKeypairMgrSuite struct {
	keypairMgr asserts.KeypairManager
}

func TestTool(t *testing.T) { TestingT(t) }

var _ = Suite(&gpgKeypairMgrSuite{})

func (gkms *gpgKeypairMgrSuite) SetUpSuite(c *C) {
	if !osutil.FileExists("/usr/bin/gpg") {
		c.Skip("gpg not installed")
	}
}

const (
	testKey = `-----BEGIN PGP PRIVATE KEY BLOCK-----
Version: GnuPG v1

lQOYBFdG+0IBCADB5UOthcr3nDIIykBPdbiSX/vfw0580p779GxoyiGSBf+BF7je
wi15JRGvi8ysIXCPMTxrFaYk0Pdc75XNu6WdXrCYlHGtfRc29wg6y8ZCV0Vpgayy
lCsICTY9KvHCvNXc1jkZ8RnO1/6hPQPl5vYC4ZQncNm1hjYO8d0I+JGY7XtfTaNl
tktAehxInzXTiUsbbXPhza10L0vFBcc3jvNK8Ap7eQx5QmexJEkK87GH/1KxtWcg
X0ODaPHmWyP9mi5+DSwXKK1kH41eQTfhBnZNhY3f14AIZzxpvXZdyyNm6/IHFgn+
OeFlkC8qKIlvSGUFA5GrXDWDBwyQ6Sb0OVqDABEBAAEAB/wPFRAvzL2jh3UFkFny
nwka6w1yyUSZ2rZUNJ3u2XOat2MPEx3QchFLIEeLvKz7N2SujBlo3le8pbvqxkDZ
rn7XoBh43enTzBcEsbA5sUUm6Zaez4qen34+E0gH97GzlgXFYQKIZ+PmiOWn0Xuz
+URp/UsNda6cUKge1HsplKWasWeQ1FajewSHGUI+1x3dTCpU6Tj74ffCalM3K7ek
zDDoxzvG5TT0KUkpVp3BIt5k0QS1wqFEh3TtcICvRK1GhN/pTa3bEt4nAu9hePoB
gvh/b44/8P68XIwZjhNlHPr0IgB3kcVkESfgmDQT0TLKJDpdgANH7W6e0sVpGsbu
V091BADKqF4GzY5ry+Jm/L1viYK/Na/87liwHkK4QuIyw/a92HTfPgJCkXZ4u9mT
E3ecldYF8PoNUjlTcdQcsifYDqdnMnwwJmRwnSESx8/d59nKl9a3dp6K4mk1nTz3
SBQ8d7jvS9TrJA6S2LLXR7SrrZ5J/nnXi1ST7Vl/rDhLTy6rjQQA9O57M3K8Zhlm
b51iIwDQwdO8A3zN1XO/wCbfSP/Gwh/boj+3W++aDSQWwZicBMIdtOXtBdVxjPYh
+JOrrqrwJUSj4gyRQWDWzdxz+OMODexEQAj+Xr7/uBpfOLZdwSPc4f6Xo4Sc8556
f5jeyDilN6ed0Q+MPHnQDgIvJQXskk8EAKzS7JzXCpZZYc4br3+ZKppi3eXjsdIJ
SxQUvUQM9fcfZp3N1vmWc/29raE6nq21XQsUmuU4Cn4DT34+71yKxrSIg5fykMKE
Af2nUzgui/BBzK5kuu7zSptIeKZVgvdWBqA5tPgTfxS3iYFsgI6is5g939zO1XlO
69zm/A82NeSVUKu0BHRlc3SJATgEEwECACIFAldG+0ICGy8GCwkIBwMCBhUIAgkK
CwQWAgMBAh4BAheAAAoJEH9EWPIIUfVJbo0H/iPszDqcF17vhEQg33ntRyUCjL13
GjpMON08x2Da1atVHphZSg5ipXkRpAxezgd0MUusGvUjaDXFeKNgiKFjCQa2RzU9
sqk2Dcq50c2C95yn5y1+EdD4jDhwjhoCOhFUTBnm154jlICTjiWvvpAnfACIbday
NKPZvaR9RMyx8IDYOUYmeF9ZK9JyoZI5DIBEveQQ5I9I0fJZUQn1JDfBTQCMBwmf
7kv5prR/eYhqtQW5l3U7DGhc7VSpVrrH5YPlwiOReowIktMYXaD+n+G1Sd8zLi3I
e2iuR6+6eT6HvJWZSx04YOy8EZi83gtfrkCL3mASwB2hon+Sxxa7CkHa4ZQ=
=iLO8
-----END PGP PRIVATE KEY BLOCK-----`

	testKeyID = "7f4458f20851f549"

	testKeyFingerprint = "b2937670e9ceff8080b7cc5a7f4458f20851f549"
)

func (gkms *gpgKeypairMgrSuite) SetUpTest(c *C) {
	homedir := c.MkDir()
	gkms.keypairMgr = tool.NewGPGKeypairManager(homedir)
	// import test key
	gpg := exec.Command("gpg", "--homedir", homedir, "-q", "--batch", "--import", "--armor")
	gpg.Stdin = bytes.NewBufferString(testKey)
	out, err := gpg.CombinedOutput()
	c.Assert(err, IsNil, Commentf("test key import failed: %v (%q)", err, out))
}

func (gkms *gpgKeypairMgrSuite) TestGetPublicKeyLooksGood(c *C) {
	got, err := gkms.keypairMgr.Get("auth-id1", testKeyID)
	c.Assert(err, IsNil)
	fp := got.PublicKey().Fingerprint()
	c.Check(fp, Equals, testKeyFingerprint)
}

func (gkms *gpgKeypairMgrSuite) TestGetNotFound(c *C) {
	got, err := gkms.keypairMgr.Get("auth-id1", "ffffffffffffffff")
	c.Check(err, ErrorMatches, "no matching key pair found")
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
