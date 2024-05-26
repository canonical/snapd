// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/testutil"
)

type extKeypairMgrSuite struct {
	pgm *testutil.MockCmd

	defaultPub *rsa.PublicKey
	modelsPub  *rsa.PublicKey
}

var _ = Suite(&extKeypairMgrSuite{})

func (s *extKeypairMgrSuite) SetUpSuite(c *C) {
	tmpdir := c.MkDir()
	k1 := mylog.Check2(rsa.GenerateKey(rand.Reader, 4096))

	k2 := mylog.Check2(rsa.GenerateKey(rand.Reader, 4096))


	derPub1 := mylog.Check2(x509.MarshalPKIXPublicKey(&k1.PublicKey))

	mylog.Check(os.WriteFile(filepath.Join(tmpdir, "default.pub"), derPub1, 0644))

	derPub2 := mylog.Check2(x509.MarshalPKIXPublicKey(&k2.PublicKey))

	mylog.Check(os.WriteFile(filepath.Join(tmpdir, "models.pub"), derPub2, 0644))

	mylog.Check(os.WriteFile(filepath.Join(tmpdir, "default.key"), x509.MarshalPKCS1PrivateKey(k1), 0600))

	mylog.Check(os.WriteFile(filepath.Join(tmpdir, "models.key"), x509.MarshalPKCS1PrivateKey(k2), 0600))


	s.defaultPub = &k1.PublicKey
	s.modelsPub = &k2.PublicKey

	s.pgm = testutil.MockCommand(c, "keymgr", fmt.Sprintf(`
keydir=%q
case $1 in
  features)
    echo '{"signing":["RSA-PKCS"] , "public-keys":["DER"]}'
    ;;
  key-names)
    echo '{"key-names": ["default", "models"]}'
    ;;
  get-public-key)
    if [ "$5" = missing ]; then
       echo not found
       exit 1
    fi
    cat ${keydir}/"$5".pub
    ;;
  sign)
    openssl rsautl -sign -pkcs -keyform DER -inkey ${keydir}/"$5".key
    ;;
  *)
    exit 1
    ;;
esac
`, tmpdir))
}

func (s *extKeypairMgrSuite) TearDownSuite(c *C) {
	s.pgm.Restore()
}

func (s *extKeypairMgrSuite) TestFeaturesErrors(c *C) {
	pgm := testutil.MockCommand(c, "keymgr", `
if [ "$1" != "features" ]; then
  exit 2
fi
if [ "${EXT_KEYMGR_FAIL}" = "exit-1" ]; then
  exit 1
fi
echo "${EXT_KEYMGR_FAIL}"
`)
	defer pgm.Restore()
	defer os.Unsetenv("EXT_KEYMGR_FAIL")

	tests := []struct {
		outcome string
		err     string
	}{
		{"exit-1", `.*exit status 1.*`},
		{`{"signing":["RSA-PKCS"]}`, `external keypair manager "keymgr" missing support for public key DER output format`},
		{"{}", `external keypair manager \"keymgr\" missing support for RSA-PKCS signing`},
		{"{", `cannot decode external keypair manager "keymgr" \[features\] output.*`},
		{"", `cannot decode external keypair manager "keymgr" \[features\] output.*`},
	}

	defer os.Unsetenv("EXT_KEYMGR_FAIL")
	for _, t := range tests {
		os.Setenv("EXT_KEYMGR_FAIL", t.outcome)

		_ := mylog.Check2(asserts.NewExternalKeypairManager("keymgr"))
		c.Check(err, ErrorMatches, t.err)
		c.Check(pgm.Calls(), DeepEquals, [][]string{
			{"keymgr", "features"},
		})
		pgm.ForgetCalls()
	}
}

func (s *extKeypairMgrSuite) TestGetByName(c *C) {
	kmgr := mylog.Check2(asserts.NewExternalKeypairManager("keymgr"))

	s.pgm.ForgetCalls()

	pk := mylog.Check2(kmgr.GetByName("default"))


	expPK := asserts.RSAPublicKey(s.defaultPub)

	c.Check(pk.PublicKey().ID(), DeepEquals, expPK.ID())

	c.Check(s.pgm.Calls(), DeepEquals, [][]string{
		{"keymgr", "get-public-key", "-f", "DER", "-k", "default"},
	})
}

func (s *extKeypairMgrSuite) TestGetByNameNotFound(c *C) {
	kmgr := mylog.Check2(asserts.NewExternalKeypairManager("keymgr"))


	_ = mylog.Check2(kmgr.GetByName("missing"))
	c.Check(err, ErrorMatches, `cannot find external key pair: external keypair manager "keymgr" .* failed: .*`)
	c.Check(asserts.IsKeyNotFound(err), Equals, true)
}

func (s *extKeypairMgrSuite) TestGet(c *C) {
	kmgr := mylog.Check2(asserts.NewExternalKeypairManager("keymgr"))

	s.pgm.ForgetCalls()

	defaultID := asserts.RSAPublicKey(s.defaultPub).ID()
	modelsID := asserts.RSAPublicKey(s.modelsPub).ID()

	pk1 := mylog.Check2(kmgr.Get(defaultID))

	c.Check(pk1.PublicKey().ID(), Equals, defaultID)

	pk2 := mylog.Check2(kmgr.Get(modelsID))

	c.Check(pk2.PublicKey().ID(), Equals, modelsID)

	c.Check(s.pgm.Calls(), DeepEquals, [][]string{
		{"keymgr", "key-names"},
		{"keymgr", "get-public-key", "-f", "DER", "-k", "default"},
		{"keymgr", "get-public-key", "-f", "DER", "-k", "models"},
	})

	_ = mylog.Check2(kmgr.Get("unknown-id"))
	c.Check(err, ErrorMatches, `cannot find external key pair`)
	c.Check(asserts.IsKeyNotFound(err), Equals, true)
}

func (s *extKeypairMgrSuite) TestSignFlow(c *C) {
	// the signing uses openssl
	_ := mylog.Check2(exec.LookPath("openssl"))

	kmgr := mylog.Check2(asserts.NewExternalKeypairManager("keymgr"))

	s.pgm.ForgetCalls()

	pk := mylog.Check2(kmgr.GetByName("default"))


	store := assertstest.NewStoreStack("trusted", nil)

	brandAcct := assertstest.NewAccount(store, "brand", map[string]interface{}{
		"account-id": "brand-id",
	}, "")
	brandAccKey := assertstest.NewAccountKey(store, brandAcct, nil, pk.PublicKey(), "")

	signDB := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: kmgr,
	}))


	checkDB := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   store.Trusted,
	}))

	mylog.
		// add store key
		Check(checkDB.Add(store.StoreAccountKey("")))

	mylog.
		// enable brand key
		Check(checkDB.Add(brandAcct))

	mylog.Check(checkDB.Add(brandAccKey))


	modelHdsrs := map[string]interface{}{
		"authority-id": "brand-id",
		"brand-id":     "brand-id",
		"model":        "model",
		"series":       "16",
		"architecture": "amd64",
		"base":         "core18",
		"gadget":       "gadget",
		"kernel":       "pc-kernel",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	a := mylog.Check2(signDB.Sign(asserts.ModelType, modelHdsrs, nil, pk.PublicKey().ID()))

	mylog.

		// valid
		Check(checkDB.Check(a))


	c.Check(s.pgm.Calls(), DeepEquals, [][]string{
		{"keymgr", "get-public-key", "-f", "DER", "-k", "default"},
		{"keymgr", "sign", "-m", "RSA-PKCS", "-k", "default"},
	})
}

func (s *extKeypairMgrSuite) TestExport(c *C) {
	kmgr := mylog.Check2(asserts.NewExternalKeypairManager("keymgr"))


	keys := []struct {
		name string
		pk   *rsa.PublicKey
	}{
		{name: "default", pk: s.defaultPub},
		{name: "models", pk: s.modelsPub},
	}

	for _, tk := range keys {
		exported := mylog.Check2(kmgr.Export(tk.name))


		expected := mylog.Check2(asserts.EncodePublicKey(asserts.RSAPublicKey(tk.pk)))

		c.Check(exported, DeepEquals, expected)
	}
}

func (s *extKeypairMgrSuite) TestList(c *C) {
	kmgr := mylog.Check2(asserts.NewExternalKeypairManager("keymgr"))


	keys := mylog.Check2(kmgr.List())


	defaultID := asserts.RSAPublicKey(s.defaultPub).ID()
	modelsID := asserts.RSAPublicKey(s.modelsPub).ID()

	c.Check(keys, DeepEquals, []asserts.ExternalKeyInfo{
		{Name: "default", ID: defaultID},
		{Name: "models", ID: modelsID},
	})
}

func (s *extKeypairMgrSuite) TestListError(c *C) {
	kmgr := mylog.Check2(asserts.NewExternalKeypairManager("keymgr"))


	pgm := testutil.MockCommand(c, "keymgr", `exit 1`)
	defer pgm.Restore()

	_ = mylog.Check2(kmgr.List())
	c.Check(err, ErrorMatches, `cannot get all external keypair manager key names:.*exit status 1.*`)
}

func (s *extKeypairMgrSuite) TestDeleteByNameUnsupported(c *C) {
	kmgr := mylog.Check2(asserts.NewExternalKeypairManager("keymgr"))

	mylog.Check(kmgr.DeleteByName("key"))
	c.Check(err, ErrorMatches, `no support to delete external keypair manager keys`)
	c.Check(err, FitsTypeOf, &asserts.ExternalUnsupportedOpError{})
}

func (s *extKeypairMgrSuite) TestDelete(c *C) {
	kmgr := mylog.Check2(asserts.NewExternalKeypairManager("keymgr"))

	mylog.Check(kmgr.Delete("key-id"))
	c.Check(err, ErrorMatches, `no support to delete external keypair manager keys`)
	c.Check(err, FitsTypeOf, &asserts.ExternalUnsupportedOpError{})
}

func (s *extKeypairMgrSuite) TestGenerateUnsupported(c *C) {
	kmgr := mylog.Check2(asserts.NewExternalKeypairManager("keymgr"))

	mylog.Check(kmgr.Generate("key"))
	c.Check(err, ErrorMatches, `no support to mediate generating an external keypair manager key`)
	c.Check(err, FitsTypeOf, &asserts.ExternalUnsupportedOpError{})
}
