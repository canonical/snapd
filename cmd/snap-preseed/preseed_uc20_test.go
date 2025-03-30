// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022-2023 Canonical Ltd
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

package main_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/cmd/snap-preseed"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/image/preseed"
	"github.com/snapcore/snapd/store"
)

const accountAssertString = `type: account
authority-id: canonical
account-id: my-brand
display-name: my-brand
username: my-brand
validation: unproven
timestamp: 2020-01-01T00:00:00Z
body-length: 0
sign-key-sha3-384: -CvQKAwRQ5h3Ffn10FILJoEZUXOv6km9FwA80-Rcj-f-6jadQ89VRswHNiEB9Lxk

AXNpZw==`

var (
	defaultBrandPrivKey, _      = assertstest.GenerateKey(752)
	defaultBrandKeyAssertString = generateAccountKeyAssert("my-brand", defaultBrandPrivKey)
	altBrandPrivKey, _          = assertstest.GenerateKey(752)
	altBrandKeyAssertString     = generateAccountKeyAssert("my-brand", altBrandPrivKey)
)

type fakeKeyMgr struct {
	defaultKey asserts.PrivateKey
	altKey     asserts.PrivateKey
}

func (f *fakeKeyMgr) Put(privKey asserts.PrivateKey) error { return nil }
func (f *fakeKeyMgr) Get(keyID string) (asserts.PrivateKey, error) {
	switch keyID {
	case f.defaultKey.PublicKey().ID():
		return f.defaultKey, nil
	case f.altKey.PublicKey().ID():
		return f.altKey, nil
	default:
		return nil, fmt.Errorf("Could not find key pair with ID %q", keyID)
	}
}

func (f *fakeKeyMgr) GetByName(keyName string) (asserts.PrivateKey, error) {
	switch keyName {
	case "default":
		return f.defaultKey, nil
	case "alt":
		return f.altKey, nil
	default:
		return nil, fmt.Errorf("Could not find key pair with name %q", keyName)
	}
}

func (f *fakeKeyMgr) Delete(keyID string) error                { return nil }
func (f *fakeKeyMgr) Export(keyName string) ([]byte, error)    { return nil, nil }
func (f *fakeKeyMgr) List() ([]asserts.ExternalKeyInfo, error) { return nil, nil }
func (f *fakeKeyMgr) DeleteByName(keyName string) error        { return nil }

func generateAccountKeyAssert(accountID string, key asserts.PrivateKey) string {
	const accountKeySignerHash = "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij"
	pubKeyBody, _ := asserts.EncodePublicKey(key.PublicKey())
	return "type: account-key\n" +
		"authority-id: canonical\n" +
		"account-id: " + accountID + "\n" +
		"name: default\n" +
		"public-key-sha3-384: " + key.PublicKey().ID() + "\n" +
		"since: " + time.Now().Format(time.RFC3339) + "\n" +
		"body-length: " + fmt.Sprint(len(pubKeyBody)) + "\n" +
		"sign-key-sha3-384: " + accountKeySignerHash + "\n\n" +
		string(pubKeyBody) + "\n\n" +
		"AXNpZw=="
}

func (s *startPreseedSuite) TestRunPreseedUC20Happy(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)

	restore := main.MockOsGetuid(func() int {
		return 0
	})
	defer restore()

	// for UC20 probing
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "system-seed/systems/20220203"), 0755), IsNil)
	// we don't run tar, so create a fake artifact to make FileDigest happy
	c.Assert(os.WriteFile(filepath.Join(tmpDir, "system-seed/systems/20220203/preseed.tgz"), nil, 0644), IsNil)

	keyMgr := &fakeKeyMgr{defaultBrandPrivKey, altBrandPrivKey}
	restoreGetKeypairMgr := main.MockGetKeypairManager(func() (signtool.KeypairManager, error) {
		return keyMgr, nil
	})
	defer restoreGetKeypairMgr()

	var server *httptest.Server
	restoreStoreNew := main.MockStoreNew(func(cfg *store.Config, storeCtx store.DeviceAndAuthContext) *store.Store {
		if cfg == nil {
			cfg = store.DefaultConfig()
		}
		serverURL, _ := url.Parse(server.URL)
		cfg.AssertionsBaseURL = serverURL
		return store.New(cfg, storeCtx)
	})
	defer restoreStoreNew()

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, "GET")
		switch r.URL.Path {
		case "/v2/assertions/account-key/" + altBrandPrivKey.PublicKey().ID():
			fmt.Fprint(w, altBrandKeyAssertString)
		case "/v2/assertions/account/my-brand":
			fmt.Fprint(w, accountAssertString)
		default:
			c.Fatalf("invalid request: %q", r.URL.Path)
		}
	}))

	accountAssert, err := asserts.Decode([]byte(accountAssertString))
	c.Assert(err, IsNil)

	accountKeyAssert, err := asserts.Decode([]byte(altBrandKeyAssertString))
	c.Assert(err, IsNil)

	var called bool
	restorePreseed := main.MockPreseedCore20(func(opts *preseed.CoreOptions) error {
		c.Check(opts.PrepareImageDir, Equals, tmpDir)
		c.Check(opts.PreseedSignKey, DeepEquals, &altBrandPrivKey)
		c.Check(opts.PreseedAccountAssert, DeepEquals, accountAssert.(*asserts.Account))
		c.Check(opts.PreseedAccountKeyAssert, DeepEquals, accountKeyAssert.(*asserts.AccountKey))
		c.Check(opts.AppArmorKernelFeaturesDir, Equals, "/custom/aa/features")
		c.Check(opts.SysfsOverlay, Equals, "/sysfs-overlay")
		called = true
		return nil
	})
	defer restorePreseed()

	parser := testParser(c)
	c.Assert(main.Run(parser, []string{"--preseed-sign-key", "alt", "--apparmor-features-dir", "/custom/aa/features", "--sysfs-overlay", "/sysfs-overlay", tmpDir}), IsNil)
	c.Check(called, Equals, true)
}

func (s *startPreseedSuite) TestRunPreseedUC20HappyNoArgs(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)

	restore := main.MockOsGetuid(func() int {
		return 0
	})
	defer restore()

	// for UC20 probing
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "system-seed/systems/20220203"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(tmpDir, "system-seed/systems/20220203/preseed.tgz"), nil, 0644), IsNil)

	keyMgr := &fakeKeyMgr{defaultBrandPrivKey, altBrandPrivKey}
	restoreGetKeypairMgr := main.MockGetKeypairManager(func() (signtool.KeypairManager, error) {
		return keyMgr, nil
	})
	defer restoreGetKeypairMgr()

	var server *httptest.Server
	restoreStoreNew := main.MockStoreNew(func(cfg *store.Config, storeCtx store.DeviceAndAuthContext) *store.Store {
		if cfg == nil {
			cfg = store.DefaultConfig()
		}
		serverURL, _ := url.Parse(server.URL)
		cfg.AssertionsBaseURL = serverURL
		return store.New(cfg, storeCtx)
	})
	defer restoreStoreNew()

	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, "GET")
		switch r.URL.Path {
		case "/v2/assertions/account-key/" + defaultBrandPrivKey.PublicKey().ID():
			fmt.Fprint(w, defaultBrandKeyAssertString)
		case "/v2/assertions/account/my-brand":
			fmt.Fprint(w, accountAssertString)
		default:
			c.Fatalf("invalid request: %q", r.URL.Path)
		}
	}))

	accountAssert, err := asserts.Decode([]byte(accountAssertString))
	c.Assert(err, IsNil)

	accountKeyAssert, err := asserts.Decode([]byte(defaultBrandKeyAssertString))
	c.Assert(err, IsNil)

	var called bool
	restorePreseed := main.MockPreseedCore20(func(opts *preseed.CoreOptions) error {
		c.Check(opts.PrepareImageDir, Equals, tmpDir)
		c.Check(opts.PreseedSignKey, DeepEquals, &defaultBrandPrivKey)
		c.Check(opts.PreseedAccountAssert, DeepEquals, accountAssert.(*asserts.Account))
		c.Check(opts.PreseedAccountKeyAssert, DeepEquals, accountKeyAssert.(*asserts.AccountKey))
		c.Check(opts.AppArmorKernelFeaturesDir, Equals, "")
		c.Check(opts.SysfsOverlay, Equals, "")
		called = true
		return nil
	})
	defer restorePreseed()

	parser := testParser(c)
	c.Assert(main.Run(parser, []string{tmpDir}), IsNil)
	c.Check(called, Equals, true)
}

func (s *startPreseedSuite) TestResetUC20(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)

	restore := main.MockOsGetuid(func() int {
		return 0
	})
	defer restore()

	// for UC20 probing
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "system-seed/systems/20220203"), 0755), IsNil)
	// we don't run tar, so create a fake artifact to make FileDigest happy
	c.Assert(os.WriteFile(filepath.Join(tmpDir, "system-seed/systems/20220203/preseed.tgz"), nil, 0644), IsNil)

	var called bool
	restorePreseed := main.MockPreseedCore20(func(opts *preseed.CoreOptions) error {
		called = true
		return nil
	})
	defer restorePreseed()

	parser := testParser(c)
	res := main.Run(parser, []string{"--reset", tmpDir})
	c.Assert(res, Not(IsNil))
	c.Check(res, ErrorMatches, "cannot snap-preseed --reset for Ubuntu Core")
	c.Check(called, Equals, false)
}
