// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/macaroon.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
)

type remoteRepoTestSuite struct {
	store  *SnapUbuntuStoreRepository
	logbuf *bytes.Buffer
	user   *auth.UserState

	origDownloadFunc func(string, io.Writer, *http.Request, progress.Meter) error
}

func TestStore(t *testing.T) { TestingT(t) }

var _ = Suite(&remoteRepoTestSuite{})

func makeTestMacaroon() (*macaroon.Macaroon, error) {
	m, err := macaroon.New([]byte("secret"), "some-id", "location")
	if err != nil {
		return nil, err
	}
	err = m.AddThirdPartyCaveat([]byte("shared-key"), "third-party-caveat", UbuntuoneLocation)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func makeTestDischarge() (*macaroon.Macaroon, error) {
	m, err := macaroon.New([]byte("shared-key"), "third-party-caveat", UbuntuoneLocation)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func createTestUser(userID int, root, discharge *macaroon.Macaroon) (*auth.UserState, error) {
	serializedMacaroon, err := MacaroonSerialize(root)
	if err != nil {
		return nil, err
	}
	serializedDischarge, err := MacaroonSerialize(discharge)
	if err != nil {
		return nil, err
	}

	return &auth.UserState{
		ID:              userID,
		Username:        "test-user",
		Macaroon:        serializedMacaroon,
		Discharges:      []string{serializedDischarge},
		StoreMacaroon:   serializedMacaroon,
		StoreDischarges: []string{serializedDischarge},
	}, nil
}

func (t *remoteRepoTestSuite) SetUpTest(c *C) {
	t.store = NewUbuntuStoreSnapRepository(nil, "", nil)
	t.origDownloadFunc = download
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(dirs.SnapSnapsDir, 0755), IsNil)

	t.logbuf = bytes.NewBuffer(nil)
	l, err := logger.NewConsoleLog(t.logbuf, logger.DefaultFlags)
	c.Assert(err, IsNil)
	logger.SetLogger(l)

	root, err := makeTestMacaroon()
	c.Assert(err, IsNil)
	discharge, err := makeTestDischarge()
	c.Assert(err, IsNil)
	t.user, err = createTestUser(1, root, discharge)
	c.Assert(err, IsNil)
}

func (t *remoteRepoTestSuite) TearDownTest(c *C) {
	download = t.origDownloadFunc
}

func (t *remoteRepoTestSuite) TearDownSuite(c *C) {
	logger.SimpleSetup()
}

func (t *remoteRepoTestSuite) expectedAuthorization(c *C, user *auth.UserState) string {
	var buf bytes.Buffer

	root, err := MacaroonDeserialize(user.StoreMacaroon)
	c.Assert(err, IsNil)
	discharge, err := MacaroonDeserialize(user.StoreDischarges[0])
	c.Assert(err, IsNil)
	discharge.Bind(root.Signature())

	serializedMacaroon, err := MacaroonSerialize(root)
	c.Assert(err, IsNil)
	serializedDischarge, err := MacaroonSerialize(discharge)
	c.Assert(err, IsNil)

	fmt.Fprintf(&buf, `Macaroon root="%s", discharge="%s"`, serializedMacaroon, serializedDischarge)
	return buf.String()
}

func (t *remoteRepoTestSuite) TestDownloadOK(c *C) {

	download = func(name string, w io.Writer, req *http.Request, pbar progress.Meter) error {
		c.Check(req.URL.String(), Equals, "anon-url")
		w.Write([]byte("I was downloaded"))
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"

	path, err := t.store.Download("foo", &snap.DownloadInfo, nil, nil)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	content, err := ioutil.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "I was downloaded")
}

func (t *remoteRepoTestSuite) TestAuthenticatedDownloadDoesNotUseAnonURL(c *C) {
	download = func(name string, w io.Writer, req *http.Request, pbar progress.Meter) error {
		// check authorization is set
		authorization := req.Header.Get("Authorization")
		c.Check(authorization, Equals, t.expectedAuthorization(c, t.user))
		c.Check(req.UserAgent(), Equals, userAgent)

		c.Check(req.URL.String(), Equals, "AUTH-URL")
		w.Write([]byte("I was downloaded"))
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"

	path, err := t.store.Download("foo", &snap.DownloadInfo, nil, t.user)
	c.Assert(err, IsNil)
	defer os.Remove(path)

	content, err := ioutil.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "I was downloaded")
}

func (t *remoteRepoTestSuite) TestDownloadFails(c *C) {
	var tmpfile *os.File
	download = func(name string, w io.Writer, req *http.Request, pbar progress.Meter) error {
		tmpfile = w.(*os.File)
		return fmt.Errorf("uh, it failed")
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	// simulate a failed download
	path, err := t.store.Download("foo", &snap.DownloadInfo, nil, nil)
	c.Assert(err, ErrorMatches, "uh, it failed")
	c.Assert(path, Equals, "")
	// ... and ensure that the tempfile is removed
	c.Assert(osutil.FileExists(tmpfile.Name()), Equals, false)
}

func (t *remoteRepoTestSuite) TestDownloadSyncFails(c *C) {
	var tmpfile *os.File
	download = func(name string, w io.Writer, req *http.Request, pbar progress.Meter) error {
		tmpfile = w.(*os.File)
		w.Write([]byte("sync will fail"))
		err := tmpfile.Close()
		c.Assert(err, IsNil)
		return nil
	}

	snap := &snap.Info{}
	snap.RealName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"

	// simulate a failed sync
	path, err := t.store.Download("foo", &snap.DownloadInfo, nil, nil)
	c.Assert(err, ErrorMatches, "fsync:.*")
	c.Assert(path, Equals, "")
	// ... and ensure that the tempfile is removed
	c.Assert(osutil.FileExists(tmpfile.Name()), Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryHeaders(c *C) {
	req, err := http.NewRequest("GET", "http://example.com", nil)
	c.Assert(err, IsNil)

	t.store.setUbuntuStoreHeaders(req, "", false, nil)

	c.Check(req.UserAgent(), Equals, userAgent)

	c.Check(req.Header.Get("X-Ubuntu-Release"), Equals, "16")
	c.Check(req.Header.Get("X-Ubuntu-Device-Channel"), Equals, "")
	c.Check(req.Header.Get("X-Ubuntu-Confinement"), Equals, "")

	t.store.setUbuntuStoreHeaders(req, "chan", true, nil)

	c.Check(req.Header.Get("Authorization"), Equals, "")
	c.Check(req.Header.Get("X-Ubuntu-Device-Channel"), Equals, "chan")
	c.Check(req.Header.Get("X-Ubuntu-Confinement"), Equals, "devmode")
}

const (
	funkyAppName      = "8nzc1x4iim2xj1g2ul64"
	funkyAppDeveloper = "chipaca"
	funkyAppSnapID    = "1e21e12ex4iim2xj1g2ul6f12f1"

	helloWorldSnapID      = "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ"
	helloWorldDeveloperID = "canonical"
)

/* acquired via

http --pretty=format --print b https://search.apps.ubuntu.com/api/v1/snaps/details/hello-world X-Ubuntu-Series:16 fields==anon_download_url,architecture,channel,download_sha512,summary,description,binary_filesize,download_url,icon_url,last_updated,package_name,prices,publisher,ratings_average,revision,snap_id,support_url,title,content,version,origin,developer_id,private,confinement channel==edge | xsel -b

on 2016-07-03. Then, by hand:
 * set prices to {"EUR": 0.99, "USD": 1.23}.

On Ubuntu, apt install httpie xsel (although you could get http from
the http snap instead).

*/
const MockDetailsJSON = `{
    "_links": {
        "curies": [
            {
                "href": "https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex#reltype_{rel}",
                "name": "clickindex",
                "templated": true
            }
        ],
        "self": {
            "href": "https://search.apps.ubuntu.com/api/v1/snaps/details/hello-world?fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin%2Cdeveloper_id%2Cprivate%2Cconfinement&channel=edge"
        }
    },
    "anon_download_url": "https://public.apps.ubuntu.com/anon/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_26.snap",
    "architecture": [
        "all"
    ],
    "binary_filesize": 20480,
    "channel": "edge",
    "confinement": "strict",
    "content": "application",
    "description": "This is a simple hello world example.",
    "developer_id": "canonical",
    "download_sha512": "345f33c06373f799b64c497a778ef58931810dd7ae85279d6917d8b4f43d38abaf37e68239cb85914db276cb566a0ef83ea02b6f2fd064b54f9f2508fa4ca1f1",
    "download_url": "https://public.apps.ubuntu.com/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_26.snap",
    "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
    "last_updated": "2016-05-31T07:02:32.586839Z",
    "origin": "canonical",
    "package_name": "hello-world",
    "prices": {"EUR": 0.99, "USD": 1.23},
    "publisher": "Canonical",
    "ratings_average": 0.0,
    "revision": 26,
    "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
    "summary": "Hello world example",
    "support_url": "mailto:snappy-devel@lists.ubuntu.com",
    "title": "hello-world",
    "version": "6.1"
}`

const mockPurchasesJSON = `[
  {
    "open_id": "https://login.staging.ubuntu.com/+id/open_id",
    "package_name": "hello-world.canonical",
    "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
    "refundable_until": "2015-07-15 18:46:21",
    "state": "Complete"
  },
  {
    "open_id": "https://login.staging.ubuntu.com/+id/open_id",
    "package_name": "hello-world.canonical",
    "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
    "item_sku": "item-1-sku",
    "purchase_id": "1",
    "refundable_until": null,
    "state": "Complete"
  },
  {
    "open_id": "https://login.staging.ubuntu.com/+id/open_id",
    "package_name": "8nzc1x4iim2xj1g2ul64.chipaca",
    "snap_id": "1e21e12ex4iim2xj1g2ul6f12f1",
    "refundable_until": "2015-07-17 11:33:29",
    "state": "Complete"
  }
]
`

const mockPurchaseJSON = `[
  {
    "open_id": "https://login.staging.ubuntu.com/+id/open_id",
    "package_name": "hello-world.canonical",
    "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
    "refundable_until": "2015-07-15 18:46:21",
    "state": "Complete"
  },
  {
    "open_id": "https://login.staging.ubuntu.com/+id/open_id",
    "package_name": "hello-world.canonical",
    "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
    "item_sku": "item-1-sku",
    "purchase_id": "1",
    "refundable_until": null,
    "state": "Complete"
  }
]
`

const mockSinglePurchaseJSON = `
{
  "open_id": "https://login.staging.ubuntu.com/+id/open_id",
  "package_name": "hello-world.canonical",
  "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
  "refundable_until": "2015-07-15 18:46:21",
  "state": "Complete"
}
`

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryDetails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)

		// no store ID by default
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.URL.Path, Equals, "/details/hello-world")

		c.Check(r.URL.Query().Get("channel"), Equals, "edge")

		c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "")
		c.Check(r.Header.Get("X-Ubuntu-Confinement"), Equals, "")

		w.Header().Set("X-Suggested-Currency", "GBP")
		w.WriteHeader(http.StatusOK)

		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		DetailsURI: detailsURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	// the actual test
	result, err := repo.Snap("hello-world", "edge", true, nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
	c.Check(result.Architectures, DeepEquals, []string{"all"})
	c.Check(result.Revision, Equals, snap.R(26))
	c.Check(result.SnapID, Equals, helloWorldSnapID)
	c.Check(result.Developer, Equals, "canonical")
	c.Check(result.Version, Equals, "6.1")
	c.Check(result.Sha512, Matches, `[[:xdigit:]]{128}`)
	c.Check(result.Size, Equals, int64(20480))
	c.Check(result.Channel, Equals, "edge")
	c.Check(result.Description(), Equals, "This is a simple hello world example.")
	c.Check(result.Summary(), Equals, "Hello world example")
	c.Assert(result.Prices, DeepEquals, map[string]float64{"EUR": 0.99, "USD": 1.23})
	c.Check(result.MustBuy, Equals, true)

	// Make sure the epoch (currently not sent by the store) defaults to "0"
	c.Check(result.Epoch, Equals, "0")

	c.Check(repo.SuggestedCurrency(), Equals, "GBP")

	// skip this one until the store supports it
	// c.Check(result.Private, Equals, true)

	c.Check(snap.Validate(result), IsNil)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryDetailsDevmode(c *C) {
	mockDevmodeJSON := strings.Replace(MockDetailsJSON, `"strict"`, `"devmode"`, -1)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)

		c.Check(r.URL.Path, Equals, "/details/hello-world")

		query := r.URL.Query()
		c.Check(query.Get("channel"), Equals, "edge")

		if query.Get("confinement") == "strict" {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, "{}")
		} else {
			w.Header().Set("X-Suggested-Currency", "GBP")
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, mockDevmodeJSON)
		}
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		DetailsURI: detailsURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	// the actual test
	result, err := repo.Snap("hello-world", "edge", false, nil)
	c.Check(err, Equals, ErrSnapNotFound)
	c.Check(result, IsNil)
	result, err = repo.Snap("hello-world", "edge", true, nil)
	c.Assert(err, IsNil)
	c.Check(result, NotNil)

	c.Check(snap.Validate(result), IsNil)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryDetailsSetsAuth(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.UserAgent(), Equals, userAgent)

		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)
		// check authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, t.expectedAuthorization(c, t.user))

		c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/"+helloWorldSnapID+"/")
		c.Check(r.URL.Query().Get("include_item_purchases"), Equals, "true")
		io.WriteString(w, mockPurchaseJSON)
	}))
	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	purchasesURI, err := url.Parse(mockPurchasesServer.URL + "/dev/api/snap-purchases/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		DetailsURI:   detailsURI,
		PurchasesURI: purchasesURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	snap, err := repo.Snap("hello-world", "edge", false, t.user)
	c.Assert(snap, NotNil)
	c.Assert(err, IsNil)
	c.Check(snap.MustBuy, Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryDetailsOopses(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, Equals, "/details/hello-world")
		c.Check(r.URL.Query().Get("channel"), Equals, "edge")

		w.Header().Set("X-Oops-Id", "OOPS-d4f46f75a5bcc10edcacc87e1fc0119f")
		w.WriteHeader(http.StatusInternalServerError)

		io.WriteString(w, `{"oops": "OOPS-d4f46f75a5bcc10edcacc87e1fc0119f"}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		DetailsURI: detailsURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	// the actual test
	_, err = repo.Snap("hello-world", "edge", false, nil)
	c.Assert(err, ErrorMatches, `Ubuntu CPI service returned unexpected HTTP status code 5.. while getting details for snap "hello-world" in channel "edge" \[OOPS-[a-f0-9A-F]*\]`)
}

/*
acquired via

http --pretty=format --print b https://search.apps.ubuntu.com/api/v1/snaps/details/no:such:package X-Ubuntu-Series:16 fields==anon_download_url,architecture,channel,download_sha512,summary,description,binary_filesize,download_url,icon_url,last_updated,package_name,prices,publisher,ratings_average,revision,snap_id,support_url,title,content,version,origin,developer_id,private,confinement channel==edge | xsel -b

on 2016-07-03

On Ubuntu, apt install httpie xsel (although you could get http from
the http snap instead).

*/
const MockNoDetailsJSON = `{
    "errors": [
        "No such package"
    ],
    "result": "error"
}`

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryNoDetails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, Equals, "/details/no-such-pkg")

		q := r.URL.Query()
		c.Check(q.Get("channel"), Equals, "edge")
		w.WriteHeader(404)
		io.WriteString(w, MockNoDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		DetailsURI: detailsURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	// the actual test
	result, err := repo.Snap("no-such-pkg", "edge", false, nil)
	c.Assert(err, NotNil)
	c.Assert(result, IsNil)
}

func (t *remoteRepoTestSuite) TestStructFields(c *C) {
	type s struct {
		Foo int `json:"hello"`
		Bar int `json:"potato,stuff"`
	}
	c.Assert(getStructFields(s{}), DeepEquals, []string{"hello", "potato"})
}

/* acquired via:
curl -s -H "accept: application/hal+json" -H "X-Ubuntu-Release: 16" -H "X-Ubuntu-Device-Channel: edge" -H "X-Ubuntu-Wire-Protocol: 1" -H "X-Ubuntu-Architecture: amd64"  'https://search.apps.ubuntu.com/api/v1/search?fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&q=hello' | python -m json.tool | xsel -b
*/
const MockSearchJSON = `{
    "_embedded": {
        "clickindex:package": [
            {
                "_links": {
                    "self": {
                        "href": "https://search.apps.ubuntu.com/api/v1/package/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ"
                    }
                },
                "anon_download_url": "https://public.apps.ubuntu.com/anon/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_25.snap",
                "architecture": [
                    "all"
                ],
                "binary_filesize": 20480,
                "channel": "edge",
                "content": "application",
                "description": "This is a simple hello world example.",
                "download_sha512": "4bf23ce93efa1f32f0aeae7ec92564b7b0f9f8253a0bd39b2741219c1be119bb676c21208c6845ccf995e6aabe791d3f28a733ebcbbc3171bb23f67981f4068e",
                "download_url": "https://public.apps.ubuntu.com/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_25.snap",
                "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
                "last_updated": "2016-04-19T19:50:50.435291Z",
                "origin": "canonical",
                "package_name": "hello-world",
                "prices": {"EUR": 2.99, "USD": 3.49},
                "publisher": "Canonical",
                "ratings_average": 0.0,
                "revision": 25,
                "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
                "summary": "Hello world example",
                "support_url": "mailto:snappy-devel@lists.ubuntu.com",
                "title": "hello-world",
                "version": "6.0"
            }
        ]
    },
    "_links": {
        "curies": [
            {
                "href": "https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex#reltype_{rel}",
                "name": "clickindex",
                "templated": true
            }
        ],
        "first": {
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=hello&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        },
        "last": {
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=hello&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        },
        "self": {
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=hello&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        }
    }
}
`

func (t *remoteRepoTestSuite) TestUbuntuStoreFindQueries(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		name := query.Get("name")
		q := query.Get("q")

		switch n {
		case 0, 1:
			c.Check(r.URL.Path, Equals, "/search")
			c.Check(name, Equals, "hello")
			c.Check(q, Equals, "")
		case 2:
			c.Check(r.URL.Path, Equals, "/details/hello")
			c.Check(name, Equals, "")
			c.Check(q, Equals, "")
		case 3:
			c.Check(r.URL.Path, Equals, "/search")
			c.Check(name, Equals, "")
			c.Check(q, Equals, "hello")
		default:
			c.Fatalf("what? %d", n)
		}

		n++
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	serverURL, _ := url.Parse(mockServer.URL)
	searchURI, _ := serverURL.Parse("/search")
	detailsURI, _ := serverURL.Parse("/details/")
	cfg := SnapUbuntuStoreConfig{
		DetailsURI: detailsURI,
		SearchURI:  searchURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	for _, query := range []string{
		"hello",
		"name:hello*",
		"name:hello",
		"text:hello",
	} {
		repo.Find(query, "", nil)
	}
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindFailures(c *C) {
	repo := NewUbuntuStoreSnapRepository(&SnapUbuntuStoreConfig{SearchURI: new(url.URL)}, "", nil)
	_, err := repo.Find("", "", nil)
	c.Check(err, Equals, ErrEmptyQuery)
	_, err = repo.Find("foo:bar", "", nil)
	c.Check(err, Equals, ErrBadPrefix)

	for _, prefix := range []string{"text:", "name:"} {
		_, err = repo.Find(prefix, "", nil)
		c.Check(err, Equals, ErrEmptyQuery, Commentf(prefix))
		_, err = repo.Find(prefix+":", "", nil)
		c.Check(err, Equals, ErrBadQuery, Commentf(prefix))
		_, err = repo.Find(prefix+"foo*bar", "", nil)
		c.Check(err, Equals, ErrBadQuery, Commentf(prefix))
	}
	_, err = repo.Find("text:foo*", "", nil)
	c.Check(err, Equals, ErrBadQuery)

	_, err = repo.Find("name:foo*bar", "", nil)
	c.Check(err, Equals, ErrBadQuery)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindFails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Query().Get("name"), Equals, "hello")
		http.Error(w, http.StatusText(http.StatusTeapot), http.StatusTeapot)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI:    searchURI,
		DetailFields: []string{}, // make the error less noisy
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	snaps, err := repo.Find("hello", "", nil)
	c.Check(err, ErrorMatches, `received an unexpected http response code \(418 I'm a teapot\) when trying to search via "http://\S+[?&]name=hello.*"`)
	c.Check(snaps, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindBadContentType(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Query().Get("name"), Equals, "hello")
		io.WriteString(w, MockSearchJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI:    searchURI,
		DetailFields: []string{}, // make the error less noisy
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	snaps, err := repo.Find("hello", "", nil)
	c.Check(err, ErrorMatches, `received an unexpected content type \("text/plain[^"]+"\) when trying to search via "http://\S+[?&]name=hello.*"`)
	c.Check(snaps, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindBadBody(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		c.Check(query.Get("name"), Equals, "hello")
		c.Check(query.Get("confinement"), Equals, "strict")
		w.Header().Set("Content-Type", "application/hal+json")
		io.WriteString(w, "<hello>")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI:    searchURI,
		DetailFields: []string{}, // make the error less noisy
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	snaps, err := repo.Find("hello", "", nil)
	c.Check(err, ErrorMatches, `cannot decode reply \(got invalid character.*\) when trying to search via "http://\S+[?&]name=hello.*"`)
	c.Check(snaps, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindSetsAuth(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)
		// check authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, t.expectedAuthorization(c, t.user))

		c.Check(r.URL.Query().Get("name"), Equals, "foo")
		w.Header().Set("Content-Type", "application/hal+json")
		io.WriteString(w, MockSearchJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.UserAgent(), Equals, userAgent)
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/"+helloWorldSnapID+"/")
		c.Check(r.URL.Query().Get("include_item_purchases"), Equals, "true")
		io.WriteString(w, mockPurchaseJSON)
	}))
	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	purchasesURI, err := url.Parse(mockPurchasesServer.URL + "/dev/api/snap-purchases/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI:    searchURI,
		PurchasesURI: purchasesURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	snaps, err := repo.Find("foo", "", t.user)
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 1)
	c.Check(snaps[0].SnapID, Equals, helloWorldSnapID)
	c.Check(snaps[0].Prices, DeepEquals, map[string]float64{"EUR": 2.99, "USD": 3.49})
	c.Check(snaps[0].MustBuy, Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindAuthFailed(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, t.expectedAuthorization(c, t.user))

		query := r.URL.Query()
		c.Check(query.Get("name"), Equals, "foo")
		c.Check(query.Get("confinement"), Equals, "strict")
		w.Header().Set("Content-Type", "application/hal+json")
		io.WriteString(w, MockSearchJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/"+helloWorldSnapID+"/")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "{}")
	}))
	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	purchasesURI, err := url.Parse(mockPurchasesServer.URL + "/dev/api/snap-purchases/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI:    searchURI,
		PurchasesURI: purchasesURI,
		DetailFields: []string{}, // make the error less noisy
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	snaps, err := repo.Find("foo", "", t.user)
	c.Assert(err, IsNil)

	// Check that we log an error.
	c.Check(t.logbuf.String(), Matches, "(?ms).* cannot get user purchases: invalid credentials")

	// But still successfully return snap information.
	c.Assert(snaps, HasLen, 1)
	c.Check(snaps[0].SnapID, Equals, helloWorldSnapID)
	c.Check(snaps[0].Prices, DeepEquals, map[string]float64{"EUR": 2.99, "USD": 3.49})
	c.Check(snaps[0].MustBuy, Equals, true)
}

/* acquired via:
(against production "hello-world")
$ curl -s --data-binary '{"snaps":[{"snap_id":"buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ","channel":"stable","revision":25,"epoch":"0","confinement":"strict"}],"fields":["anon_download_url","architecture","channel","download_sha512","summary","description","binary_filesize","download_url","icon_url","last_updated","package_name","prices","publisher","ratings_average","revision","snap_id","support_url","title","content","version","origin","developer_id","private","confinement"]}'  -H 'content-type: application/json' -H 'X-Ubuntu-Release: 16' -H 'X-Ubuntu-Wire-Protocol: 1' -H "accept: application/hal+json" https://search.apps.ubuntu.com/api/v1/snaps/metadata | python3 -m json.tool --sort-keys | xsel -b
*/
var MockUpdatesJSON = `
{
    "_embedded": {
        "clickindex:package": [
            {
                "_links": {
                    "self": {
                        "href": "https://search.apps.ubuntu.com/api/v1/package/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ"
                    }
                },
                "anon_download_url": "https://public.apps.ubuntu.com/anon/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_26.snap",
                "architecture": [
                    "all"
                ],
                "binary_filesize": 20480,
                "channel": "stable",
                "confinement": "strict",
                "content": "application",
                "description": "This is a simple hello world example.",
                "developer_id": "canonical",
                "download_sha512": "345f33c06373f799b64c497a778ef58931810dd7ae85279d6917d8b4f43d38abaf37e68239cb85914db276cb566a0ef83ea02b6f2fd064b54f9f2508fa4ca1f1",
                "download_url": "https://public.apps.ubuntu.com/download-snap/buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ_26.snap",
                "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
                "last_updated": "2016-05-31T07:02:32.586839Z",
                "origin": "canonical",
                "package_name": "hello-world",
                "prices": {},
                "publisher": "Canonical",
                "ratings_average": 0.0,
                "revision": 26,
                "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
                "summary": "Hello world example",
                "support_url": "mailto:snappy-devel@lists.ubuntu.com",
                "title": "hello-world",
                "version": "6.1"
            }
        ]
    },
    "_links": {
        "curies": [
            {
                "href": "https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex#reltype_{rel}",
                "name": "clickindex",
                "templated": true
            }
        ]
    }
}
`

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryListRefresh(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var resp struct {
			Snaps  []map[string]interface{} `json:"snaps"`
			Fields []string                 `json:"fields"`
		}

		err = json.Unmarshal(jsonReq, &resp)
		c.Assert(err, IsNil)

		c.Assert(resp.Snaps, HasLen, 1)
		c.Assert(resp.Snaps[0], DeepEquals, map[string]interface{}{
			"snap_id":     helloWorldSnapID,
			"channel":     "stable",
			"revision":    float64(1),
			"epoch":       "0",
			"confinement": "strict",
		})
		c.Assert(resp.Fields, DeepEquals, detailFields)

		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		BulkURI: bulkURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	results, err := repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(1),
			Epoch:    "0",
			DevMode:  false,
		},
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name(), Equals, "hello-world")
	c.Assert(results[0].Revision, Equals, snap.R(26))
	c.Assert(results[0].Version, Equals, "6.1")
	c.Assert(results[0].SnapID, Equals, helloWorldSnapID)
	c.Assert(results[0].DeveloperID, Equals, helloWorldDeveloperID)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryListRefreshSkipCurrent(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var resp struct {
			Snaps []map[string]interface{} `json:"snaps"`
		}

		err = json.Unmarshal(jsonReq, &resp)
		c.Assert(err, IsNil)

		c.Assert(resp.Snaps, HasLen, 1)
		c.Assert(resp.Snaps[0], DeepEquals, map[string]interface{}{
			"snap_id":     helloWorldSnapID,
			"channel":     "stable",
			"revision":    float64(26),
			"epoch":       "0",
			"confinement": "strict",
		})

		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		BulkURI: bulkURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	results, err := repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(26),
			Epoch:    "0",
			DevMode:  false,
		},
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryListRefreshSkipBlocked(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)

		var resp struct {
			Snaps []map[string]interface{} `json:"snaps"`
		}

		err = json.Unmarshal(jsonReq, &resp)
		c.Assert(err, IsNil)

		c.Assert(resp.Snaps, HasLen, 1)
		c.Assert(resp.Snaps[0], DeepEquals, map[string]interface{}{
			"snap_id":     helloWorldSnapID,
			"channel":     "stable",
			"revision":    float64(25),
			"epoch":       "0",
			"confinement": "strict",
		})

		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		BulkURI: bulkURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	results, err := repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(25),
			Epoch:    "0",
			DevMode:  false,
			Block:    []snap.Revision{snap.R(26)},
		},
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryUpdateNotSendLocalRevs(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		var resp struct {
			Snaps []map[string]interface{} `json:"snaps"`
		}

		err = json.Unmarshal(jsonReq, &resp)
		c.Assert(err, IsNil)

		c.Assert(resp.Snaps, HasLen, 1)
		c.Assert(resp.Snaps[0], DeepEquals, map[string]interface{}{
			"snap_id":     helloWorldSnapID,
			"channel":     "stable",
			"epoch":       "0",
			"confinement": "devmode",
		})

		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		BulkURI: bulkURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	_, err = repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(-2),
			Epoch:    "0",
			DevMode:  true,
		},
	}, nil)
	c.Assert(err, IsNil)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryUpdatesSetsAuth(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, t.expectedAuthorization(c, t.user))

		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	bulkURI, err := url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		BulkURI: bulkURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	_, err = repo.ListRefresh([]*RefreshCandidate{
		{
			SnapID:   helloWorldSnapID,
			Channel:  "stable",
			Revision: snap.R(1),
			Epoch:    "0",
			DevMode:  false,
		},
	}, t.user)
	c.Assert(err, IsNil)
}

func (t *remoteRepoTestSuite) TestStructFieldsSurvivesNoTag(c *C) {
	type s struct {
		Foo int `json:"hello"`
		Bar int
	}
	c.Assert(getStructFields(s{}), DeepEquals, []string{"hello"})
}

func (t *remoteRepoTestSuite) TestCpiURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_CPI", ""), IsNil)
	before := cpiURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_CPI", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_CPI", "")
	after := cpiURL()

	c.Check(before, Not(Equals), after)
}

func (t *remoteRepoTestSuite) TestAuthLocationDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_CPI", ""), IsNil)
	before := authLocation()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_CPI", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_CPI", "")
	after := authLocation()

	c.Check(before, Not(Equals), after)
}

func (t *remoteRepoTestSuite) TestAuthURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_CPI", ""), IsNil)
	before := authURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_CPI", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_CPI", "")
	after := authURL()

	c.Check(before, Not(Equals), after)
}

func (t *remoteRepoTestSuite) TestAssertsURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_SAS", ""), IsNil)
	before := assertsURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_SAS", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_SAS", "")
	after := assertsURL()

	c.Check(before, Not(Equals), after)
}

func (t *remoteRepoTestSuite) TestMyAppsURLDependsOnEnviron(c *C) {
	c.Assert(os.Setenv("SNAPPY_USE_STAGING_MYAPPS", ""), IsNil)
	before := myappsURL()

	c.Assert(os.Setenv("SNAPPY_USE_STAGING_MYAPPS", "1"), IsNil)
	defer os.Setenv("SNAPPY_USE_STAGING_MYAPPS", "")
	after := myappsURL()

	c.Check(before, Not(Equals), after)
}

func (t *remoteRepoTestSuite) TestDefaultConfig(c *C) {
	c.Check(strings.HasPrefix(defaultConfig.SearchURI.String(), "https://search.apps.ubuntu.com/api/v1/snaps/search"), Equals, true)
	c.Check(strings.HasPrefix(defaultConfig.BulkURI.String(), "https://search.apps.ubuntu.com/api/v1/snaps/metadata"), Equals, true)
	c.Check(defaultConfig.AssertionsURI.String(), Equals, "https://assertions.ubuntu.com/v1/assertions/")
}

var testAssertion = `type: snap-declaration
authority-id: super
series: 16
snap-id: snapidfoo
gates: 
publisher-id: devidbaz
snap-name: mysnap
timestamp: 2016-03-30T12:22:16Z

openpgp wsBcBAABCAAQBQJW+8VBCRDWhXkqAWcrfgAAQ9gIABZFgMPByJZeUE835FkX3/y2hORn
AzE3R1ktDkQEVe/nfVDMACAuaw1fKmUS4zQ7LIrx/AZYw5i0vKVmJszL42LBWVsqR0+p9Cxebzv9
U2VUSIajEsUUKkBwzD8wxFzagepFlScif1NvCGZx0vcGUOu0Ent0v+gqgAv21of4efKqEW7crlI1
T/A8LqZYmIzKRHGwCVucCyAUD8xnwt9nyWLgLB+LLPOVFNK8SR6YyNsX05Yz1BUSndBfaTN8j/k8
8isKGZE6P0O9ozBbNIAE8v8NMWQegJ4uWuil7D3psLkzQIrxSypk9TrQ2GlIG2hJdUovc5zBuroe
xS4u9rVT6UY=`

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryAssertion(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Equals, "/assertions/snap-declaration/16/snapidfoo")
		io.WriteString(w, testAssertion)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	assertionsURI, err := url.Parse(mockServer.URL + "/assertions/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		AssertionsURI: assertionsURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)

	a, err := repo.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.SnapDeclarationType)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryAssertionSetsAuth(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, t.expectedAuthorization(c, t.user))

		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Equals, "/assertions/snap-declaration/16/snapidfoo")
		io.WriteString(w, testAssertion)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	assertionsURI, err := url.Parse(mockServer.URL + "/assertions/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		AssertionsURI: assertionsURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)

	_, err = repo.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, t.user)
	c.Assert(err, IsNil)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryNotFound(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Equals, "/assertions/snap-declaration/16/snapidfoo")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		io.WriteString(w, `{"status": 404,"title": "not found"}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	assertionsURI, err := url.Parse(mockServer.URL + "/assertions/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		AssertionsURI: assertionsURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)

	_, err = repo.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Check(err, Equals, ErrAssertionNotFound)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositorySuggestedCurrency(c *C) {
	suggestedCurrency := "GBP"

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Suggested-Currency", suggestedCurrency)
		w.WriteHeader(http.StatusOK)

		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		DetailsURI: detailsURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	// the store doesn't know the currency until after the first search, so fall back to dollars
	c.Check(repo.SuggestedCurrency(), Equals, "USD")

	// we should soon have a suggested currency
	result, err := repo.Snap("hello-world", "edge", false, nil)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Check(repo.SuggestedCurrency(), Equals, "GBP")

	suggestedCurrency = "EUR"

	// checking the currency updates
	result, err = repo.Snap("hello-world", "edge", false, nil)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Check(repo.SuggestedCurrency(), Equals, "EUR")
}

func (t *remoteRepoTestSuite) TestUbuntuStoreDecoratePurchases(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")
		c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/")
		io.WriteString(w, mockPurchasesJSON)
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	var err error
	purchasesURI, err := url.Parse(mockPurchasesServer.URL + "/dev/api/snap-purchases/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		PurchasesURI: purchasesURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}

	funkyApp := &snap.Info{}
	funkyApp.SnapID = funkyAppSnapID
	funkyApp.Prices = map[string]float64{"USD": 2.34}

	otherApp := &snap.Info{}
	otherApp.SnapID = "other"
	otherApp.Prices = map[string]float64{"USD": 3.45}

	otherApp2 := &snap.Info{}
	otherApp2.SnapID = "other2"

	snaps := []*snap.Info{helloWorld, funkyApp, otherApp, otherApp2}

	err = repo.decoratePurchases(snaps, "edge", t.user)
	c.Assert(err, IsNil)

	c.Check(helloWorld.MustBuy, Equals, false)
	c.Check(funkyApp.MustBuy, Equals, false)
	c.Check(otherApp.MustBuy, Equals, true)
	c.Check(otherApp2.MustBuy, Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreDecoratePurchasesFailedAccess(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")
		c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "{}")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	var err error
	purchasesURI, err := url.Parse(mockPurchasesServer.URL + "/dev/api/snap-purchases/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		PurchasesURI: purchasesURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}

	funkyApp := &snap.Info{}
	funkyApp.SnapID = funkyAppSnapID
	funkyApp.Prices = map[string]float64{"USD": 2.34}

	otherApp := &snap.Info{}
	otherApp.SnapID = "other"
	otherApp.Prices = map[string]float64{"USD": 3.45}

	otherApp2 := &snap.Info{}
	otherApp2.SnapID = "other2"

	snaps := []*snap.Info{helloWorld, funkyApp, otherApp, otherApp2}

	err = repo.decoratePurchases(snaps, "edge", t.user)
	c.Assert(err, NotNil)

	c.Check(helloWorld.MustBuy, Equals, true)
	c.Check(funkyApp.MustBuy, Equals, true)
	c.Check(otherApp.MustBuy, Equals, true)
	c.Check(otherApp2.MustBuy, Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreDecoratePurchasesNoAuth(c *C) {
	cfg := SnapUbuntuStoreConfig{}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}

	funkyApp := &snap.Info{}
	funkyApp.SnapID = funkyAppSnapID
	funkyApp.Prices = map[string]float64{"USD": 2.34}

	otherApp := &snap.Info{}
	otherApp.SnapID = "other"
	otherApp.Prices = map[string]float64{"USD": 3.45}

	otherApp2 := &snap.Info{}
	otherApp2.SnapID = "other2"

	snaps := []*snap.Info{helloWorld, funkyApp, otherApp, otherApp2}

	err := repo.decoratePurchases(snaps, "edge", nil)
	c.Assert(err, IsNil)

	c.Check(helloWorld.MustBuy, Equals, true)
	c.Check(funkyApp.MustBuy, Equals, true)
	c.Check(otherApp.MustBuy, Equals, true)
	c.Check(otherApp2.MustBuy, Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreGetPurchasesAllFree(c *C) {
	requestRecieved := false

	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestRecieved = true
		io.WriteString(w, "[]")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	purchasesURI, err := url.Parse(mockPurchasesServer.URL + "/click/purchases/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		PurchasesURI: purchasesURI,
	}

	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	// This snap is free
	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID

	// This snap is also free
	funkyApp := &snap.Info{}
	funkyApp.SnapID = funkyAppSnapID

	snaps := []*snap.Info{helloWorld, funkyApp}

	// There should be no request to the purchases server.
	err = repo.decoratePurchases(snaps, "edge", t.user)
	c.Assert(err, IsNil)
	c.Check(requestRecieved, Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreGetPurchasesSingle(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")
		c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/"+helloWorldSnapID+"/")
		c.Check(r.URL.Query().Get("include_item_purchases"), Equals, "true")
		io.WriteString(w, mockPurchaseJSON)
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	var err error
	purchasesURI, err := url.Parse(mockPurchasesServer.URL + "/dev/api/snap-purchases/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		PurchasesURI: purchasesURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}

	snaps := []*snap.Info{helloWorld}

	err = repo.decoratePurchases(snaps, "edge", t.user)
	c.Assert(err, IsNil)
	c.Check(helloWorld.MustBuy, Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreGetPurchasesSingleFreeSnap(c *C) {
	cfg := SnapUbuntuStoreConfig{}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID

	snaps := []*snap.Info{helloWorld}

	err := repo.decoratePurchases(snaps, "edge", t.user)
	c.Assert(err, IsNil)
	c.Check(helloWorld.MustBuy, Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreGetPurchasesSingleNotFound(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")
		c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/"+helloWorldSnapID+"/")
		c.Check(r.URL.Query().Get("include_item_purchases"), Equals, "true")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "{}")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	var err error
	purchasesURI, err := url.Parse(mockPurchasesServer.URL + "/dev/api/snap-purchases/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		PurchasesURI: purchasesURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}

	snaps := []*snap.Info{helloWorld}

	err = repo.decoratePurchases(snaps, "edge", t.user)
	c.Assert(err, NotNil)
	c.Check(helloWorld.MustBuy, Equals, true)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreGetPurchasesTokenExpired(c *C) {
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")
		c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
		c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/"+helloWorldSnapID+"/")
		c.Check(r.URL.Query().Get("include_item_purchases"), Equals, "true")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "")
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	var err error
	purchasesURI, err := url.Parse(mockPurchasesServer.URL + "/dev/api/snap-purchases/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		PurchasesURI: purchasesURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	helloWorld := &snap.Info{}
	helloWorld.SnapID = helloWorldSnapID
	helloWorld.Prices = map[string]float64{"USD": 1.23}

	snaps := []*snap.Info{helloWorld}

	err = repo.decoratePurchases(snaps, "edge", t.user)
	c.Assert(err, NotNil)
	c.Check(helloWorld.MustBuy, Equals, true)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreMustBuy(c *C) {
	free := map[string]float64{}
	priced := map[string]float64{"USD": 2.99}

	appPurchase := purchase{}
	inAppPurchase := purchase{ItemSKU: "1"}

	hasNoPurchases := []*purchase{}
	hasPurchase := []*purchase{&appPurchase}
	hasInAppPurchase := []*purchase{&inAppPurchase}
	hasPurchaseAndInAppPurchase := []*purchase{&appPurchase, &inAppPurchase}

	// Never need to buy a free snap.
	c.Check(mustBuy(free, hasNoPurchases), Equals, false)
	c.Check(mustBuy(free, hasPurchase), Equals, false)
	c.Check(mustBuy(free, hasInAppPurchase), Equals, false)
	c.Check(mustBuy(free, hasPurchaseAndInAppPurchase), Equals, false)

	// Don't need to buy snaps that have a purchase.
	c.Check(mustBuy(priced, hasNoPurchases), Equals, true)
	c.Check(mustBuy(priced, hasPurchase), Equals, false)
	c.Check(mustBuy(priced, hasInAppPurchase), Equals, true)
	c.Check(mustBuy(priced, hasPurchaseAndInAppPurchase), Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreBuySuccess(c *C) {
	searchServerCalled := false
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/hello-world")
		w.Header().Set("Content-Type", "application/hal+json")
		w.Header().Set("X-Suggested-Currency", "EUR")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, MockDetailsJSON)
		searchServerCalled = true
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	purchaseServerGetCalled := false
	purchaseServerPostCalled := false
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")
			c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
			c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/"+helloWorldSnapID+"/")
			c.Check(r.URL.Query().Get("include_item_purchases"), Equals, "true")
			io.WriteString(w, "{}")
			purchaseServerGetCalled = true
		case "POST":
			c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")
			c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
			c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/")
			jsonReq, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(string(jsonReq), Equals, "{\"snap_id\":\""+helloWorldSnapID+"\",\"amount\":0.99,\"currency\":\"EUR\",\"backend_id\":\"123\",\"method_id\":234}")
			io.WriteString(w, mockSinglePurchaseJSON)
			purchaseServerPostCalled = true
		default:
			c.Error("Unexpected request method: ", r.Method)
		}
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	detailsURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	purchasesURI, err := url.Parse(mockPurchasesServer.URL + "/dev/api/snap-purchases/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		DetailsURI:   detailsURI,
		PurchasesURI: purchasesURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	// Find the snap first
	snap, err := repo.Snap("hello-world", "edge", false, t.user)
	c.Assert(err, IsNil)

	// Now buy the snap using the suggested currency
	result, err := repo.Buy(&BuyOptions{
		SnapID:   snap.SnapID,
		SnapName: snap.Name(),
		Channel:  snap.Channel,
		Currency: repo.SuggestedCurrency(),
		Price:    snap.Prices[repo.SuggestedCurrency()],
		User:     t.user,

		BackendID: "123",
		MethodID:  234,
	})

	c.Assert(result, NotNil)
	c.Check(result.State, Equals, "Complete")
	c.Check(result.PartnerID, Equals, "")
	c.Check(result.RedirectTo, Equals, "")
	c.Assert(err, IsNil)

	c.Check(searchServerCalled, Equals, true)
	c.Check(purchaseServerGetCalled, Equals, true)
	c.Check(purchaseServerPostCalled, Equals, true)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreBuyFailWrongPrice(c *C) {
	searchServerCalled := false
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/hello-world")
		w.Header().Set("Content-Type", "application/hal+json")
		w.Header().Set("X-Suggested-Currency", "EUR")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, MockDetailsJSON)
		searchServerCalled = true
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	purchaseServerGetCalled := false
	purchaseServerPostCalled := false
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")
			c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
			c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/"+helloWorldSnapID+"/")
			c.Check(r.URL.Query().Get("include_item_purchases"), Equals, "true")
			io.WriteString(w, "{}")
			purchaseServerGetCalled = true
		case "POST":
			c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")
			c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
			c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/")
			jsonReq, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(string(jsonReq), Equals, "{\"snap_id\":\""+helloWorldSnapID+"\",\"amount\":0.99,\"currency\":\"USD\"}")
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, "{\"error_message\":\"invalid price specified\"}")
			purchaseServerPostCalled = true
		default:
			c.Error("Unexpected request method: ", r.Method)
		}
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	detailsURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	purchasesURI, err := url.Parse(mockPurchasesServer.URL + "/dev/api/snap-purchases/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		DetailsURI:   detailsURI,
		PurchasesURI: purchasesURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	// Find the snap first
	snap, err := repo.Snap("hello-world", "edge", false, t.user)
	c.Assert(err, IsNil)

	// Attempt to buy the snap using the wrong price in USD
	result, err := repo.Buy(&BuyOptions{
		SnapID:   snap.SnapID,
		SnapName: snap.Name(),
		Channel:  snap.Channel,
		Price:    0.99,
		Currency: "USD",
		User:     t.user,
	})

	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap \"hello-world\": bad request: invalid price specified")

	c.Check(searchServerCalled, Equals, true)
	c.Check(purchaseServerGetCalled, Equals, true)
	c.Check(purchaseServerPostCalled, Equals, true)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreBuyFailNotFound(c *C) {
	searchServerCalled := false
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "GET")
		c.Check(r.URL.Path, Equals, "/details/hello-world")
		q := r.URL.Query()
		c.Check(q.Get("channel"), Equals, "edge")
		w.Header().Set("Content-Type", "application/hal+json")
		w.Header().Set("X-Suggested-Currency", "EUR")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, MockDetailsJSON)
		searchServerCalled = true
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	purchaseServerGetCalled := false
	purchaseServerPostCalled := false
	mockPurchasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")
			c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
			c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/"+helloWorldSnapID+"/")
			c.Check(r.URL.Query().Get("include_item_purchases"), Equals, "true")
			io.WriteString(w, "{}")
			purchaseServerGetCalled = true
		case "POST":
			c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")
			c.Check(r.Header.Get("Authorization"), Equals, t.expectedAuthorization(c, t.user))
			c.Check(r.URL.Path, Equals, "/dev/api/snap-purchases/")
			jsonReq, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)
			c.Check(string(jsonReq), Equals, "{\"snap_id\":\"invalid snap ID\",\"amount\":0.99,\"currency\":\"EUR\"}")
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, "{\"error_message\":\"Not found\"}")
			purchaseServerPostCalled = true
		default:
			c.Error("Unexpected request method: ", r.Method)
		}
	}))

	c.Assert(mockPurchasesServer, NotNil)
	defer mockPurchasesServer.Close()

	detailsURI, err := url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)
	purchasesURI, err := url.Parse(mockPurchasesServer.URL + "/dev/api/snap-purchases/")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		DetailsURI:   detailsURI,
		PurchasesURI: purchasesURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "", nil)
	c.Assert(repo, NotNil)

	// Find the snap first
	snap, err := repo.Snap("hello-world", "edge", false, t.user)
	c.Assert(err, IsNil)

	// Now try and buy the snap, but with an invalid ID
	result, err := repo.Buy(&BuyOptions{
		SnapID:   "invalid snap ID",
		SnapName: snap.Name(),
		Channel:  snap.Channel,
		Price:    snap.Prices[repo.SuggestedCurrency()],
		Currency: repo.SuggestedCurrency(),
		User:     t.user,
	})
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap \"hello-world\": server says not found (snap got removed?)")

	c.Check(searchServerCalled, Equals, true)
	c.Check(purchaseServerGetCalled, Equals, true)
	c.Check(purchaseServerPostCalled, Equals, true)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreBuyFailArgumentChecking(c *C) {
	repo := NewUbuntuStoreSnapRepository(&SnapUbuntuStoreConfig{}, "", nil)
	c.Assert(repo, NotNil)

	// no snap ID
	result, err := repo.Buy(&BuyOptions{
		SnapName: "snap name",
		Channel:  "channel",
		Price:    1.0,
		Currency: "USD",
		User:     t.user,
	})
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap \"snap name\": snap ID missing")

	// no name
	result, err = repo.Buy(&BuyOptions{
		SnapID:   "snap ID",
		Channel:  "channel",
		Price:    1.0,
		Currency: "USD",
		User:     t.user,
	})
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap \"snap ID\": snap name missing")

	// no channel
	result, err = repo.Buy(&BuyOptions{
		SnapID:   "snap ID",
		SnapName: "snap name",
		Price:    1.0,
		Currency: "USD",
		User:     t.user,
	})
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap \"snap name\": channel missing")

	// no price
	result, err = repo.Buy(&BuyOptions{
		SnapID:   "snap ID",
		SnapName: "snap name",
		Channel:  "channel",
		Currency: "USD",
		User:     t.user,
	})
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap \"snap name\": invalid expected price")

	// no currency
	result, err = repo.Buy(&BuyOptions{
		SnapID:   "snap ID",
		SnapName: "snap name",
		Channel:  "channel",
		Price:    1.0,
		User:     t.user,
	})
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap \"snap name\": currency missing")

	// no user
	result, err = repo.Buy(&BuyOptions{
		SnapID:   "snap ID",
		SnapName: "snap name",
		Channel:  "channel",
		Price:    1.0,
		Currency: "USD",
	})
	c.Assert(result, IsNil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot buy snap \"snap name\": authentication credentials missing")
}
