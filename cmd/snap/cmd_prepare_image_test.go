// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2023 Canonical Ltd
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
	cmdsnap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

type SnapPrepareImageSuite struct {
	BaseSnapSuite
}

var _ = Suite(&SnapPrepareImageSuite{})

var (
	defaultBrandPrivKey, _ = assertstest.GenerateKey(752)
	altBrandPrivKey, _     = assertstest.GenerateKey(752)
)

type fakeKeyMgr struct {
	defaultBrandKey asserts.PrivateKey
	altBrandKey     asserts.PrivateKey
}

func (f *fakeKeyMgr) Put(privKey asserts.PrivateKey) error { return nil }
func (f *fakeKeyMgr) Get(keyID string) (asserts.PrivateKey, error) {
	switch keyID {
	case f.defaultBrandKey.PublicKey().ID():
		return f.defaultBrandKey, nil
	case f.altBrandKey.PublicKey().ID():
		return f.altBrandKey, nil
	default:
		return nil, fmt.Errorf("Could not find key pair with ID %q", keyID)
	}
}

func (f *fakeKeyMgr) GetByName(keyName string) (asserts.PrivateKey, error) {
	switch keyName {
	case "default":
		return f.defaultBrandKey, nil
	case "alt":
		return f.altBrandKey, nil
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

func (s *SnapPrepareImageSuite) TestPrepareImageCore(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:  "model",
		PrepareDir: "prepare-dir",
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageClassic(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--classic", "model", "prepare-dir"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		Classic:    true,
		ModelFile:  "model",
		PrepareDir: "prepare-dir",
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageClassicArch(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--classic", "--arch", "i386", "model", "prepare-dir"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		Classic:      true,
		Architecture: "i386",
		ModelFile:    "model",
		PrepareDir:   "prepare-dir",
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageClassicWideCohort(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	os.Setenv("UBUNTU_STORE_COHORT_KEY", "is-six-centuries")

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--classic", "model", "prepare-dir"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		Classic:       true,
		WideCohortKey: "is-six-centuries",
		ModelFile:     "model",
		PrepareDir:    "prepare-dir",
	})

	os.Unsetenv("UBUNTU_STORE_COHORT_KEY")
}

func (s *SnapPrepareImageSuite) TestPrepareImageExtraSnaps(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir", "--channel", "candidate", "--snap", "foo", "--snap", "bar=t/edge", "--snap", "local.snap", "--extra-snaps", "local2.snap", "--extra-snaps", "store-snap"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:    "model",
		Channel:      "candidate",
		PrepareDir:   "prepare-dir",
		Snaps:        []string{"foo", "bar", "local.snap", "local2.snap", "store-snap"},
		SnapChannels: map[string]string{"bar": "t/edge"},
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageCustomize(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	tmpdir := c.MkDir()
	customizeFile := filepath.Join(tmpdir, "custo.json")
	err := os.WriteFile(customizeFile, []byte(`{
  "console-conf": "disabled",
  "cloud-init-user-data": "cloud-init-user-data"
}`), 0644)
	c.Assert(err, IsNil)

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir", "--customize", customizeFile})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:  "model",
		PrepareDir: "prepare-dir",
		Customizations: image.Customizations{
			ConsoleConf:       "disabled",
			CloudInitUserData: "cloud-init-user-data",
		},
	})
}

func (s *SnapPrepareImageSuite) TestReadSeedManifest(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	var readManifestCalls int
	r = cmdsnap.MockSeedWriterReadManifest(func(manifestFile string) (*seedwriter.Manifest, error) {
		readManifestCalls++
		c.Check(manifestFile, Equals, "seed.manifest")
		return seedwriter.MockManifest(map[string]*seedwriter.ManifestSnapRevision{"snapd": {SnapName: "snapd", Revision: snap.R(100)}}, nil, nil, nil), nil
	})
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir", "--revisions", "seed.manifest"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(readManifestCalls, Equals, 1)
	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:    "model",
		PrepareDir:   "prepare-dir",
		SeedManifest: seedwriter.MockManifest(map[string]*seedwriter.ManifestSnapRevision{"snapd": {SnapName: "snapd", Revision: snap.R(100)}}, nil, nil, nil),
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImagePreseedArgError(c *C) {
	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--preseed-sign-key", "alt", "model", "prepare-dir"})
	c.Assert(err, ErrorMatches, `--preseed-sign-key cannot be used without --preseed`)
}

func (s *SnapPrepareImageSuite) TestPrepareImagePreseed(c *C) {
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

	var accountAssertKeyString = generateAccountKeyAssert("my-brand", altBrandPrivKey)

	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	keyMgr := &fakeKeyMgr{defaultBrandPrivKey, altBrandPrivKey}
	restoreGetKeypairMgr := cmdsnap.MockGetKeypairManager(func() (signtool.KeypairManager, error) {
		return keyMgr, nil
	})
	defer restoreGetKeypairMgr()

	var server *httptest.Server
	restoreStoreNew := cmdsnap.MockStoreNew(func(cfg *store.Config, storeCtx store.DeviceAndAuthContext) *store.Store {
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
			fmt.Fprint(w, accountAssertKeyString)
		case "/v2/assertions/account/my-brand":
			fmt.Fprint(w, accountAssertString)
		default:
			c.Fatalf("invalid request: %q", r.URL.Path)
		}
	}))

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--preseed", "--preseed-sign-key", "alt", "--apparmor-features-dir", "aafeatures-dir", "--sysfs-overlay", "sys-overlay", "model", "prepare-dir"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	accountAssertDecode, err := asserts.Decode([]byte(accountAssertString))
	c.Assert(err, IsNil)

	accountAssert, ok := accountAssertDecode.(*asserts.Account)
	c.Assert(ok, Equals, true)

	accountAssertKeyDecode, err := asserts.Decode([]byte(accountAssertKeyString))
	c.Assert(err, IsNil)

	accountKeyAssert, ok := accountAssertKeyDecode.(*asserts.AccountKey)
	c.Assert(ok, Equals, true)

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:                 "model",
		PrepareDir:                "prepare-dir",
		Preseed:                   true,
		PreseedSignKey:            &altBrandPrivKey,
		PreseedAccountAssert:      accountAssert,
		PreseedAccountKeyAssert:   accountKeyAssert,
		SysfsOverlay:              "sys-overlay",
		AppArmorKernelFeaturesDir: "aafeatures-dir",
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageWriteRevisions(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir", "--write-revisions"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:        "model",
		PrepareDir:       "prepare-dir",
		SeedManifestPath: "./seed.manifest",
	})

	rest, err = cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir", "--write-revisions=/tmp/seed.manifest"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:        "model",
		PrepareDir:       "prepare-dir",
		SeedManifestPath: "/tmp/seed.manifest",
	})
}
