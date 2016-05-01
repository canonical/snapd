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

	"github.com/ubuntu-core/snappy/asserts"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
)

type remoteRepoTestSuite struct {
	store *SnapUbuntuStoreRepository

	origDownloadFunc func(string, io.Writer, *http.Request, progress.Meter) error
}

func TestStore(t *testing.T) { TestingT(t) }

var _ = Suite(&remoteRepoTestSuite{})

type fakeAuthenticator struct{}

func (fa *fakeAuthenticator) Authenticate(r *http.Request) {
	r.Header.Set("Authorization", "Authorization-details")
}

func (t *remoteRepoTestSuite) SetUpTest(c *C) {
	t.store = NewUbuntuStoreSnapRepository(nil, "")
	t.origDownloadFunc = download
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(dirs.SnapSnapsDir, 0755), IsNil)
}

func (t *remoteRepoTestSuite) TearDownTest(c *C) {
	download = t.origDownloadFunc
}

func (t *remoteRepoTestSuite) TestDownloadOK(c *C) {

	download = func(name string, w io.Writer, req *http.Request, pbar progress.Meter) error {
		c.Check(req.URL.String(), Equals, "anon-url")
		w.Write([]byte("I was downloaded"))
		return nil
	}

	snap := &snap.Info{}
	snap.OfficialName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"

	path, err := t.store.Download(snap, nil, nil)
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
		c.Check(authorization, Equals, "Authorization-details")

		c.Check(req.URL.String(), Equals, "AUTH-URL")
		w.Write([]byte("I was downloaded"))
		return nil
	}

	snap := &snap.Info{}
	snap.OfficialName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"

	authenticator := &fakeAuthenticator{}
	path, err := t.store.Download(snap, nil, authenticator)
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
	snap.OfficialName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"
	// simulate a failed download
	path, err := t.store.Download(snap, nil, nil)
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
	snap.OfficialName = "foo"
	snap.AnonDownloadURL = "anon-url"
	snap.DownloadURL = "AUTH-URL"

	// simulate a failed sync
	path, err := t.store.Download(snap, nil, nil)
	c.Assert(err, ErrorMatches, "fsync:.*")
	c.Assert(path, Equals, "")
	// ... and ensure that the tempfile is removed
	c.Assert(osutil.FileExists(tmpfile.Name()), Equals, false)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryHeaders(c *C) {
	req, err := http.NewRequest("GET", "http://example.com", nil)
	c.Assert(err, IsNil)

	t.store.applyUbuntuStoreHeaders(req, "", nil)

	c.Assert(req.Header.Get("X-Ubuntu-Release"), Equals, "16")
	c.Check(req.Header.Get("Accept"), Equals, "application/hal+json")

	t.store.applyUbuntuStoreHeaders(req, "application/json", nil)

	c.Check(req.Header.Get("Accept"), Equals, "application/json")
	c.Assert(req.Header.Get("Authorization"), Equals, "")
}

const (
	funkyAppName      = "8nzc1x4iim2xj1g2ul64"
	funkyAppDeveloper = "chipaca"
)

/* acquired via
curl -s -H "accept: application/hal+json" -H "X-Ubuntu-Release: 16" -H "X-Ubuntu-Device-Channel: edge" -H "X-Ubuntu-Wire-Protocol: 1" -H "X-Ubuntu-Architecture: amd64"  'https://search.apps.ubuntu.com/api/v1/search?fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&q=package_name%3A%22hello-world%22' | python -m json.tool | xsel -b
*/
const MockDetailsJSON = `{
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
                "prices": {},
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
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=package_name%3A%22hello-world%22&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        },
        "last": {
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=package_name%3A%22hello-world%22&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        },
        "self": {
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=package_name%3A%22hello-world%22&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        }
    }
}
`

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryDetails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// no store ID by default
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Check(storeID, Equals, "")

		c.Check(r.URL.Path, Equals, "/search")

		q := r.URL.Query()
		c.Check(q.Get("q"), Equals, "package_name:\"hello-world\"")
		c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")

		w.Header().Set("X-Suggested-Currency", "GBP")
		w.WriteHeader(http.StatusOK)

		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	searchURI, err := url.Parse(mockServer.URL + "/search")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI: searchURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "")
	c.Assert(repo, NotNil)

	// the actual test
	result, err := repo.Snap("hello-world", "edge", nil)
	c.Assert(err, IsNil)
	c.Check(result.Name(), Equals, "hello-world")
	c.Check(result.Architectures, DeepEquals, []string{"all"})
	c.Check(result.Revision, Equals, 25)
	c.Check(result.SnapID, Equals, "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ")
	c.Check(result.Developer, Equals, "canonical")
	c.Check(result.Version, Equals, "6.0")
	c.Check(result.Sha512, Equals, "4bf23ce93efa1f32f0aeae7ec92564b7b0f9f8253a0bd39b2741219c1be119bb676c21208c6845ccf995e6aabe791d3f28a733ebcbbc3171bb23f67981f4068e")
	c.Check(result.Size, Equals, int64(20480))
	c.Check(result.Channel, Equals, "edge")
	c.Check(result.Description(), Equals, "This is a simple hello world example.")
	c.Check(result.Summary(), Equals, "Hello world example")
	c.Assert(result.Prices, DeepEquals, map[string]float64{})

	c.Check(repo.SuggestedCurrency(), Equals, "GBP")
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryDetailsSetsAuth(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, "Authorization-details")

		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	searchURI, err := url.Parse(mockServer.URL + "/search")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI: searchURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "")
	c.Assert(repo, NotNil)

	authenticator := &fakeAuthenticator{}
	_, err = repo.Snap("hello-world", "edge", authenticator)
	c.Assert(err, IsNil)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryDetailsOopses(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, Equals, "/search")

		q := r.URL.Query()
		c.Check(q.Get("q"), Equals, `package_name:"hello-world"`)
		c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")

		w.Header().Set("X-Oops-Id", "OOPS-d4f46f75a5bcc10edcacc87e1fc0119f")
		w.WriteHeader(http.StatusInternalServerError)

		io.WriteString(w, `{"oops": "OOPS-d4f46f75a5bcc10edcacc87e1fc0119f"}`)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	searchURI, err := url.Parse(mockServer.URL + "/search")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI: searchURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "")
	c.Assert(repo, NotNil)

	// the actual test
	_, err = repo.Snap("hello-world", "edge", nil)
	c.Assert(err, ErrorMatches, `Ubuntu CPI service returned unexpected HTTP status code 5.. while looking for snap "hello-world" in channel "edge" \[OOPS-[a-f0-9A-F]*\]`)
}

/*
acquired via
curl -s -H "accept: application/hal+json" -H "X-Ubuntu-Release: 16" -H "X-Ubuntu-Device-Channel: edge" -H "X-Ubuntu-Wire-Protocol: 1" -H "X-Ubuntu-Architecture: amd64"  'https://search.apps.ubuntu.com/api/v1/search?fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&q=package_name%3A%22%22' | python -m json.tool | xsel -b
*/
const MockNoDetailsJSON = `{
    "_links": {
        "curies": [
            {
                "href": "https://wiki.ubuntu.com/AppStore/Interfaces/ClickPackageIndex#reltype_{rel}",
                "name": "clickindex",
                "templated": true
            }
        ],
        "first": {
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=package_name%3A%22%22&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        },
        "last": {
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=package_name%3A%22%22&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        },
        "self": {
            "href": "https://search.apps.ubuntu.com/api/v1/search?q=package_name%3A%22%22&fields=anon_download_url%2Carchitecture%2Cchannel%2Cdownload_sha512%2Csummary%2Cdescription%2Cbinary_filesize%2Cdownload_url%2Cicon_url%2Clast_updated%2Cpackage_name%2Cprices%2Cpublisher%2Cratings_average%2Crevision%2Csnap_id%2Csupport_url%2Ctitle%2Ccontent%2Cversion%2Corigin&page=1"
        }
    }
}
`

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryNoDetails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, Equals, "/search")

		q := r.URL.Query()
		c.Check(q.Get("q"), Equals, "package_name:\"no-such-pkg\"")
		c.Check(r.Header.Get("X-Ubuntu-Device-Channel"), Equals, "edge")
		w.WriteHeader(404)
		io.WriteString(w, MockNoDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	searchURI, err := url.Parse(mockServer.URL + "/search")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI: searchURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "")
	c.Assert(repo, NotNil)

	// the actual test
	result, err := repo.Snap("no-such-pkg", "edge", nil)
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
                "prices": {},
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

func (t *remoteRepoTestSuite) TestUbuntuStoreFind(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.RawQuery, Equals, "q=hello")
		w.Header().Set("Content-Type", "application/hal+json")
		io.WriteString(w, MockSearchJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI: searchURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "")
	c.Assert(repo, NotNil)

	snaps, err := repo.FindSnaps("hello", "", nil)
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 1)
	c.Check(snaps[0].Name(), Equals, "hello-world")
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindFails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.RawQuery, Equals, "q=hello")
		http.Error(w, http.StatusText(http.StatusTeapot), http.StatusTeapot)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI: searchURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "")
	c.Assert(repo, NotNil)

	snaps, err := repo.FindSnaps("hello", "", nil)
	c.Check(err, ErrorMatches, `received an unexpected http response code \(418 I'm a teapot\) when trying to search via "http://[^?]+\?q=hello"`)
	c.Check(snaps, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindBadContentType(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.RawQuery, Equals, "q=hello")
		io.WriteString(w, MockSearchJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI: searchURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "")
	c.Assert(repo, NotNil)

	snaps, err := repo.FindSnaps("hello", "", nil)
	c.Check(err, ErrorMatches, `received an unexpected content type \("text/plain[^"]+"\) when trying to search via "http://[^?]+\?q=hello"`)
	c.Check(snaps, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindBadBoody(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.RawQuery, Equals, "q=hello")
		w.Header().Set("Content-Type", "application/hal+json")
		io.WriteString(w, "<hello>")
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI: searchURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "")
	c.Assert(repo, NotNil)

	snaps, err := repo.FindSnaps("hello", "", nil)
	c.Check(err, ErrorMatches, `cannot decode reply \(got invalid character.*\) when trying to search via "http://[^?]+\?q=hello"`)
	c.Check(snaps, HasLen, 0)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreFindSetsAuth(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, "Authorization-details")

		c.Check(r.URL.RawQuery, Equals, "q=foo")
		w.Header().Set("Content-Type", "application/hal+json")
		io.WriteString(w, MockSearchJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	searchURI, err := url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI: searchURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "")
	c.Assert(repo, NotNil)

	authenticator := &fakeAuthenticator{}
	_, err = repo.FindSnaps("foo", "", authenticator)
	c.Assert(err, IsNil)
}

/* acquired via:
curl -s --data-binary '{"name":["8nzc1x4iim2xj1g2ul64.chipaca"]}'  -H 'content-type: application/json' https://search.apps.ubuntu.com/api/v1/click-metadata
*/
const MockUpdatesJSON = `[
    {
        "status": "Published",
        "name": "8nzc1x4iim2xj1g2ul64.chipaca",
        "package_name": "8nzc1x4iim2xj1g2ul64",
        "origin": "chipaca",
        "changelog": "",
        "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/04/hello.svg_Dlrd3L4.png",
        "title": "Returns for store credit only.",
        "binary_filesize": 65375,
        "anon_download_url": "https://public.apps.ubuntu.com/anon/download/chipaca/8nzc1x4iim2xj1g2ul64.chipaca/8nzc1x4iim2xj1g2ul64.chipaca_42_all.snap",
        "allow_unauthenticated": true,
        "revision": 3,
        "version": "42",
        "download_url": "https://public.apps.ubuntu.com/download/chipaca/8nzc1x4iim2xj1g2ul64.chipaca/8nzc1x4iim2xj1g2ul64.chipaca_42_all.snap",
        "download_sha512": "5364253e4a988f4f5c04380086d542f410455b97d48cc6c69ca2a5877d8aef2a6b2b2f83ec4f688cae61ebc8a6bf2cdbd4dbd8f743f0522fc76540429b79df42"
    }
]`

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryUpdates(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		c.Assert(string(jsonReq), Equals, `{"name":["`+funkyAppName+`"]}`)
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
	repo := NewUbuntuStoreSnapRepository(&cfg, "")
	c.Assert(repo, NotNil)

	results, err := repo.Updates([]string{funkyAppName}, nil)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name(), Equals, funkyAppName)
	c.Assert(results[0].Revision, Equals, 3)
	c.Assert(results[0].Version, Equals, "42")
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryUpdatesSetsAuth(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, "Authorization-details")

		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		c.Assert(string(jsonReq), Equals, `{"name":["`+funkyAppName+`"]}`)
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
	repo := NewUbuntuStoreSnapRepository(&cfg, "")
	c.Assert(repo, NotNil)

	authenticator := &fakeAuthenticator{}
	_, err = repo.Updates([]string{funkyAppName}, authenticator)
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
	c.Check(strings.HasPrefix(defaultConfig.SearchURI.String(), "https://search.apps.ubuntu.com/api/v1/search?"), Equals, true)
	c.Check(strings.HasPrefix(defaultConfig.BulkURI.String(), "https://search.apps.ubuntu.com/api/v1/click-metadata?"), Equals, true)
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
	repo := NewUbuntuStoreSnapRepository(&cfg, "")

	a, err := repo.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, nil)
	c.Assert(err, IsNil)
	c.Check(a, NotNil)
	c.Check(a.Type(), Equals, asserts.SnapDeclarationType)
}

func (t *remoteRepoTestSuite) TestUbuntuStoreRepositoryAssertionSetsAuth(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check authorization is set
		authorization := r.Header.Get("Authorization")
		c.Check(authorization, Equals, "Authorization-details")

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
	repo := NewUbuntuStoreSnapRepository(&cfg, "")

	authenticator := &fakeAuthenticator{}
	_, err = repo.Assertion(asserts.SnapDeclarationType, []string{"16", "snapidfoo"}, authenticator)
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
	repo := NewUbuntuStoreSnapRepository(&cfg, "")

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

	searchURI, err := url.Parse(mockServer.URL + "/search")
	c.Assert(err, IsNil)
	cfg := SnapUbuntuStoreConfig{
		SearchURI: searchURI,
	}
	repo := NewUbuntuStoreSnapRepository(&cfg, "")
	c.Assert(repo, NotNil)

	// the store doesn't know the currency until after the first search, so fall back to dollars
	c.Check(repo.SuggestedCurrency(), Equals, "USD")

	// we should soon have a suggested currency
	result, err := repo.Snap("hello-world", "edge", nil)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Check(repo.SuggestedCurrency(), Equals, "GBP")

	suggestedCurrency = "EUR"

	// checking the currency updates
	result, err = repo.Snap("hello-world", "edge", nil)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Check(repo.SuggestedCurrency(), Equals, "EUR")
}
