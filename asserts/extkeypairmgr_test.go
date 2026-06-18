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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/testutil"
)

type extKeypairMgrSuite struct {
	pgm *testutil.MockCmd

	keyDir     string
	defaultPub *rsa.PublicKey
	modelsPub  *rsa.PublicKey
	openpgpPub *rsa.PublicKey
}

var _ = Suite(&extKeypairMgrSuite{})

type fakePublicExtKeypairMgrBackend struct {
	loaded *asserts.ExtKeypairMgrLoadedKey
}

func (b *fakePublicExtKeypairMgrBackend) CheckFeatures() (asserts.ExtKeypairMgrSigning, error) {
	return asserts.ExtKeypairMgrSigningRSAPKCS, nil
}

func (b *fakePublicExtKeypairMgrBackend) LoadByName(name string) (*asserts.ExtKeypairMgrLoadedKey, error) {
	if b.loaded == nil || b.loaded.Name != name {
		return nil, errors.New("missing key")
	}
	return b.loaded, nil
}

func (b *fakePublicExtKeypairMgrBackend) Visit(consider func(loaded *asserts.ExtKeypairMgrLoadedKey) error) error {
	if b.loaded == nil {
		return nil
	}
	return consider(b.loaded)
}

func (b *fakePublicExtKeypairMgrBackend) RSAPKCSSign(keyHandle string, prepared []byte) ([]byte, error) {
	return nil, errors.New("unexpected sign call")
}

func (b *fakePublicExtKeypairMgrBackend) Sign(keyHandle string, content []byte) ([]byte, error) {
	return nil, errors.New("unexpected sign call")
}

type fakePublicSignOnlyExtKeypairMgrBackend struct {
	loaded *asserts.ExtKeypairMgrLoadedKey
}

func (b *fakePublicSignOnlyExtKeypairMgrBackend) CheckFeatures() (asserts.ExtKeypairMgrSigning, error) {
	return asserts.ExtKeypairMgrSigningRSAPKCS, nil
}

func (b *fakePublicSignOnlyExtKeypairMgrBackend) LoadByID(keyID string) (*asserts.ExtKeypairMgrLoadedKey, error) {
	if b.loaded == nil || b.loaded.PublicKey.ID() != keyID {
		return nil, errors.New("missing key")
	}
	return b.loaded, nil
}

func (b *fakePublicSignOnlyExtKeypairMgrBackend) RSAPKCSSign(keyHandle string, prepared []byte) ([]byte, error) {
	return nil, errors.New("unexpected sign call")
}

func (b *fakePublicSignOnlyExtKeypairMgrBackend) Sign(keyHandle string, content []byte) ([]byte, error) {
	return nil, errors.New("unexpected sign call")
}

func (s *extKeypairMgrSuite) SetUpSuite(c *C) {
	tmpdir := c.MkDir()
	s.keyDir = tmpdir
	k1, err := rsa.GenerateKey(rand.Reader, 4096)
	c.Assert(err, IsNil)
	k2, err := rsa.GenerateKey(rand.Reader, 4096)
	c.Assert(err, IsNil)

	derPub1, err := x509.MarshalPKIXPublicKey(&k1.PublicKey)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(tmpdir, "default.pub"), derPub1, 0644)
	c.Assert(err, IsNil)
	derPub2, err := x509.MarshalPKIXPublicKey(&k2.PublicKey)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(tmpdir, "models.pub"), derPub2, 0644)
	c.Assert(err, IsNil)

	err = os.WriteFile(filepath.Join(tmpdir, "default.key"), x509.MarshalPKCS1PrivateKey(k1), 0600)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(tmpdir, "models.key"), x509.MarshalPKCS1PrivateKey(k2), 0600)
	c.Assert(err, IsNil)
	_, openpgpPriv := assertstest.ReadPrivKey(assertstest.DevKey)
	derPub3, err := x509.MarshalPKIXPublicKey(&openpgpPriv.PublicKey)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(tmpdir, "openpgp.pub"), derPub3, 0644)
	c.Assert(err, IsNil)

	s.defaultPub = &k1.PublicKey
	s.modelsPub = &k2.PublicKey
	s.openpgpPub = &openpgpPriv.PublicKey

	s.pgm = testutil.MockCommand(c, "keymgr", fmt.Sprintf(`
keydir=%q
case $1 in
  features)
	    if [ -n "${EXT_KEYMGR_FEATURES}" ]; then
	      echo "${EXT_KEYMGR_FEATURES}"
	    else
	      echo '{"signing":["RSA-PKCS","OPENPGP"] , "public-keys":["DER"]}'
	    fi
    ;;
  key-names)
	    if [ -n "${EXT_KEYMGR_KEY_NAMES}" ]; then
	      echo "${EXT_KEYMGR_KEY_NAMES}"
	    else
	      echo '{"key-names": ["default", "models"]}'
	    fi
    ;;
  get-public-key)
    if [ "$5" = missing ]; then
       echo not found
       exit 1
    fi
	    pubname="$5"
	    if [ "${EXT_KEYMGR_USE_OPENPGP_KEY}" = "1" ] && [ "$5" = default ]; then
	      pubname="openpgp"
	    fi
	    cat ${keydir}/"$pubname".pub
    ;;
  sign)
	    case "$3" in
	      RSA-PKCS)
	        openssl rsautl -sign -pkcs -keyform DER -inkey ${keydir}/"$5".key
	        ;;
	      OPENPGP)
	        gpgbin=$(command -v gpg1 || command -v gpg) || exit 1
	        [ -n "${EXT_KEYMGR_GNUPG_HOME}" ] || exit 1
	        "$gpgbin" --homedir "${EXT_KEYMGR_GNUPG_HOME}" --batch --yes --personal-digest-preferences SHA512 --default-key "0x%s" --detach-sign --output -
	        ;;
	      *)
	        exit 1
	        ;;
	    esac
    ;;
  *)
    exit 1
    ;;
esac
`, tmpdir, assertstest.DevKeyPGPFingerprint))
}

func (s *extKeypairMgrSuite) TearDownSuite(c *C) {
	s.pgm.Restore()
}

func (s *extKeypairMgrSuite) TestNewExternalKeypairManagerWithBackend(c *C) {
	priv, err := rsa.GenerateKey(rand.Reader, 4096)
	c.Assert(err, IsNil)
	rsaPub := asserts.RSAPublicKey(&priv.PublicKey)
	backend := &fakePublicExtKeypairMgrBackend{
		loaded: &asserts.ExtKeypairMgrLoadedKey{
			Name:      "default",
			KeyHandle: "backend-default",
			PublicKey: rsaPub,
		},
	}

	kmgr, err := asserts.NewExternalKeypairManagerWithBackend(backend, asserts.ExtKeypairMgrConfig{
		SigningWith: "fake backend",
		KeyStore:    "fake store",
	})
	c.Assert(err, IsNil)

	exported, err := kmgr.Export("default")
	c.Assert(err, IsNil)
	expected, err := asserts.EncodePublicKey(rsaPub)
	c.Assert(err, IsNil)
	c.Check(exported, DeepEquals, expected)
}

func (s *extKeypairMgrSuite) TestNewExternalKeypairManagerWithSignOnlyBackend(c *C) {
	priv, err := rsa.GenerateKey(rand.Reader, 4096)
	c.Assert(err, IsNil)
	rsaPub := asserts.RSAPublicKey(&priv.PublicKey)
	backend := &fakePublicSignOnlyExtKeypairMgrBackend{
		loaded: &asserts.ExtKeypairMgrLoadedKey{
			Name:      "default",
			KeyHandle: "backend-default",
			PublicKey: rsaPub,
		},
	}

	kmgr, err := asserts.NewExternalKeypairManagerWithBackend(backend, asserts.ExtKeypairMgrConfig{
		SigningWith: "fake backend",
		KeyStore:    "fake store",
	})
	c.Assert(err, IsNil)

	privKey, err := kmgr.Get(rsaPub.ID())
	c.Assert(err, IsNil)
	c.Check(privKey.PublicKey().ID(), Equals, rsaPub.ID())

	_, err = kmgr.GetByName("default")
	c.Assert(err, ErrorMatches, `cannot get key by name from sign-only external keypair manager`)
	c.Check(err, FitsTypeOf, &asserts.ExternalUnsupportedOpError{})

	_, err = kmgr.Export("default")
	c.Assert(err, ErrorMatches, `cannot get key by name from sign-only external keypair manager`)
	c.Check(err, FitsTypeOf, &asserts.ExternalUnsupportedOpError{})

	_, err = kmgr.List()
	c.Assert(err, ErrorMatches, `cannot list keys in sign-only external keypair manager`)
	c.Check(err, FitsTypeOf, &asserts.ExternalUnsupportedOpError{})
}

func (s *extKeypairMgrSuite) TestFeatures(c *C) {
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
		{`{"signing":["OPENPGP"],"public-keys":["DER"]}`, ""},
		{`{"signing":["RSA-PKCS","OPENPGP"],"public-keys":["DER"]}`, ""},
		{"{}", `external keypair manager \"keymgr\" missing support for RSA-PKCS or OPENPGP signing`},
		{"{", `cannot decode external keypair manager "keymgr" \[features\] output.*`},
		{"", `cannot decode external keypair manager "keymgr" \[features\] output.*`},
	}

	defer os.Unsetenv("EXT_KEYMGR_FAIL")
	for _, t := range tests {
		os.Setenv("EXT_KEYMGR_FAIL", t.outcome)

		_, err := asserts.NewExternalKeypairManager("keymgr")
		if t.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
		c.Check(pgm.Calls(), DeepEquals, [][]string{
			{"keymgr", "features"},
		})
		pgm.ForgetCalls()
	}
}

func (s *extKeypairMgrSuite) TestGetByName(c *C) {
	kmgr, err := asserts.NewExternalKeypairManager("keymgr")
	c.Assert(err, IsNil)
	s.pgm.ForgetCalls()

	pk, err := kmgr.GetByName("default")
	c.Assert(err, IsNil)

	expPK := asserts.RSAPublicKey(s.defaultPub)

	c.Check(pk.PublicKey().ID(), DeepEquals, expPK.ID())

	c.Check(s.pgm.Calls(), DeepEquals, [][]string{
		{"keymgr", "get-public-key", "-f", "DER", "-k", "default"},
	})
}

func (s *extKeypairMgrSuite) TestGetByNameNotFound(c *C) {
	kmgr, err := asserts.NewExternalKeypairManager("keymgr")
	c.Assert(err, IsNil)

	_, err = kmgr.GetByName("missing")
	c.Check(err, ErrorMatches, `cannot find external key pair: external keypair manager "keymgr" .* failed: .*`)
	c.Check(asserts.IsKeyNotFound(err), Equals, true)
}

func (s *extKeypairMgrSuite) TestGet(c *C) {
	kmgr, err := asserts.NewExternalKeypairManager("keymgr")
	c.Assert(err, IsNil)
	s.pgm.ForgetCalls()

	defaultID := asserts.RSAPublicKey(s.defaultPub).ID()
	modelsID := asserts.RSAPublicKey(s.modelsPub).ID()

	pk1, err := kmgr.Get(defaultID)
	c.Assert(err, IsNil)
	c.Check(pk1.PublicKey().ID(), Equals, defaultID)

	pk2, err := kmgr.Get(modelsID)
	c.Assert(err, IsNil)
	c.Check(pk2.PublicKey().ID(), Equals, modelsID)

	c.Check(s.pgm.Calls(), DeepEquals, [][]string{
		{"keymgr", "key-names"},
		{"keymgr", "get-public-key", "-f", "DER", "-k", "default"},
		{"keymgr", "key-names"},
		{"keymgr", "get-public-key", "-f", "DER", "-k", "default"},
		{"keymgr", "get-public-key", "-f", "DER", "-k", "models"},
	})

	_, err = kmgr.Get("unknown-id")
	c.Check(err, ErrorMatches, `cannot find key pair in external keypair manager`)
	c.Check(asserts.IsKeyNotFound(err), Equals, true)
}

func (s *extKeypairMgrSuite) TestSignFlowPrefersRSAPKCS(c *C) {
	// the signing uses openssl
	_, err := exec.LookPath("openssl")
	if err != nil {
		c.Skip("cannot locate openssl on this system to test signing")
	}
	os.Setenv("EXT_KEYMGR_FEATURES", `{"signing":["RSA-PKCS","OPENPGP"],"public-keys":["DER"]}`)
	defer os.Unsetenv("EXT_KEYMGR_FEATURES")
	kmgr, err := asserts.NewExternalKeypairManager("keymgr")
	c.Assert(err, IsNil)
	s.pgm.ForgetCalls()

	pk, err := kmgr.GetByName("default")
	c.Assert(err, IsNil)

	store := assertstest.NewStoreStack("trusted", nil)

	brandAcct := assertstest.NewAccount(store, "brand", map[string]any{
		"account-id": "brand-id",
	}, "")
	brandAccKey := assertstest.NewAccountKey(store, brandAcct, nil, pk.PublicKey(), "")

	signDB, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: kmgr,
	})
	c.Assert(err, IsNil)

	checkDB, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   store.Trusted,
	})
	c.Assert(err, IsNil)
	// add store key
	err = checkDB.Add(store.StoreAccountKey(""))
	c.Assert(err, IsNil)
	// enable brand key
	err = checkDB.Add(brandAcct)
	c.Assert(err, IsNil)
	err = checkDB.Add(brandAccKey)
	c.Assert(err, IsNil)

	modelHdsrs := map[string]any{
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
	a, err := signDB.Sign(asserts.ModelType, modelHdsrs, nil, pk.PublicKey().ID())
	c.Assert(err, IsNil)

	// valid
	err = checkDB.Check(a)
	c.Assert(err, IsNil)

	c.Check(s.pgm.Calls(), DeepEquals, [][]string{
		{"keymgr", "get-public-key", "-f", "DER", "-k", "default"},
		{"keymgr", "sign", "-m", "RSA-PKCS", "-k", "default"},
	})
}

func (s *extKeypairMgrSuite) TestSignFlowOpenPGP(c *C) {
	if _, err := exec.LookPath("gpg1"); err != nil {
		if _, err := exec.LookPath("gpg"); err != nil {
			c.Skip("gpg not installed")
		}
	}
	gnupgHome := c.MkDir()
	assertstest.GPGImportKey(gnupgHome, assertstest.DevKey)
	os.Setenv("EXT_KEYMGR_FEATURES", `{"signing":["OPENPGP"],"public-keys":["DER"]}`)
	os.Setenv("EXT_KEYMGR_KEY_NAMES", `{"key-names": ["default"]}`)
	os.Setenv("EXT_KEYMGR_USE_OPENPGP_KEY", "1")
	os.Setenv("EXT_KEYMGR_GNUPG_HOME", gnupgHome)
	defer os.Unsetenv("EXT_KEYMGR_FEATURES")
	defer os.Unsetenv("EXT_KEYMGR_KEY_NAMES")
	defer os.Unsetenv("EXT_KEYMGR_USE_OPENPGP_KEY")
	defer os.Unsetenv("EXT_KEYMGR_GNUPG_HOME")

	kmgr, err := asserts.NewExternalKeypairManager("keymgr")
	c.Assert(err, IsNil)
	s.pgm.ForgetCalls()

	pk, err := kmgr.GetByName("default")
	c.Assert(err, IsNil)
	c.Check(pk.PublicKey().ID(), Equals, asserts.RSAPublicKey(s.openpgpPub).ID())

	store := assertstest.NewStoreStack("trusted", nil)

	brandAcct := assertstest.NewAccount(store, "brand", map[string]any{
		"account-id": "brand-id",
	}, "")
	brandAccKey := assertstest.NewAccountKey(store, brandAcct, nil, pk.PublicKey(), "")

	signDB, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: kmgr,
	})
	c.Assert(err, IsNil)

	checkDB, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   store.Trusted,
	})
	c.Assert(err, IsNil)
	err = checkDB.Add(store.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = checkDB.Add(brandAcct)
	c.Assert(err, IsNil)
	err = checkDB.Add(brandAccKey)
	c.Assert(err, IsNil)

	modelHdsrs := map[string]any{
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
	a, err := signDB.Sign(asserts.ModelType, modelHdsrs, nil, pk.PublicKey().ID())
	c.Assert(err, IsNil)

	err = checkDB.Check(a)
	c.Assert(err, IsNil)

	c.Check(s.pgm.Calls(), DeepEquals, [][]string{
		{"keymgr", "get-public-key", "-f", "DER", "-k", "default"},
		{"keymgr", "sign", "-m", "OPENPGP", "-k", "default"},
	})
}

func (s *extKeypairMgrSuite) TestExport(c *C) {
	kmgr, err := asserts.NewExternalKeypairManager("keymgr")
	c.Assert(err, IsNil)

	keys := []struct {
		name string
		pk   *rsa.PublicKey
	}{
		{name: "default", pk: s.defaultPub},
		{name: "models", pk: s.modelsPub},
	}

	for _, tk := range keys {
		exported, err := kmgr.Export(tk.name)
		c.Assert(err, IsNil)

		expected, err := asserts.EncodePublicKey(asserts.RSAPublicKey(tk.pk))
		c.Assert(err, IsNil)
		c.Check(exported, DeepEquals, expected)
	}
}

func (s *extKeypairMgrSuite) TestList(c *C) {
	kmgr, err := asserts.NewExternalKeypairManager("keymgr")
	c.Assert(err, IsNil)

	keys, err := kmgr.List()
	c.Assert(err, IsNil)

	defaultID := asserts.RSAPublicKey(s.defaultPub).ID()
	modelsID := asserts.RSAPublicKey(s.modelsPub).ID()

	c.Check(keys, DeepEquals, []asserts.ExternalKeyInfo{
		{Name: "default", ID: defaultID},
		{Name: "models", ID: modelsID},
	})
}

func (s *extKeypairMgrSuite) TestListError(c *C) {
	kmgr, err := asserts.NewExternalKeypairManager("keymgr")
	c.Assert(err, IsNil)

	pgm := testutil.MockCommand(c, "keymgr", `exit 1`)
	defer pgm.Restore()

	_, err = kmgr.List()
	c.Check(err, ErrorMatches, `cannot get all external keypair manager key names:.*exit status 1.*`)
}

func (s *extKeypairMgrSuite) TestDeleteByNameUnsupported(c *C) {
	kmgr, err := asserts.NewExternalKeypairManager("keymgr")
	c.Assert(err, IsNil)

	err = kmgr.DeleteByName("key")
	c.Check(err, ErrorMatches, `no support to delete external keypair manager keys`)
	c.Check(err, FitsTypeOf, &asserts.ExternalUnsupportedOpError{})

}

func (s *extKeypairMgrSuite) TestDelete(c *C) {
	kmgr, err := asserts.NewExternalKeypairManager("keymgr")
	c.Assert(err, IsNil)

	err = kmgr.Delete("key-id")
	c.Check(err, ErrorMatches, `no support to delete external keypair manager keys`)
	c.Check(err, FitsTypeOf, &asserts.ExternalUnsupportedOpError{})

}

func (s *extKeypairMgrSuite) TestGenerateUnsupported(c *C) {
	kmgr, err := asserts.NewExternalKeypairManager("keymgr")
	c.Assert(err, IsNil)

	err = kmgr.Generate("key")
	c.Check(err, ErrorMatches, `no support to mediate generating an external keypair manager key`)
	c.Check(err, FitsTypeOf, &asserts.ExternalUnsupportedOpError{})
}
