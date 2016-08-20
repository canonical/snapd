// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/systestkeys"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap/snaptest"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type storeTestSuite struct {
	client *http.Client
	store  *Store
}

var _ = Suite(&storeTestSuite{})

var defaultAddr = "localhost:23321"

func (s *storeTestSuite) SetUpTest(c *C) {
	tempdir := c.MkDir()
	blobs := filepath.Join(tempdir, "snaps")
	err := os.Mkdir(blobs, 0755)
	c.Assert(err, IsNil)
	assertions := filepath.Join(tempdir, "asserts")
	err = os.Mkdir(assertions, 0755)
	c.Assert(err, IsNil)
	s.store = NewStore(blobs, assertions, defaultAddr)
	err = s.store.Start()
	c.Assert(err, IsNil)

	transport := &http.Transport{}
	s.client = &http.Client{
		Transport: transport,
	}
}

func (s *storeTestSuite) TearDownTest(c *C) {
	s.client.Transport.(*http.Transport).CloseIdleConnections()
	err := s.store.Stop()
	c.Assert(err, IsNil)
}

// StoreGet gets the given from the store
func (s *storeTestSuite) StoreGet(path string) (*http.Response, error) {
	return s.client.Get(s.store.URL() + path)
}

func (s *storeTestSuite) StorePostJSON(path string, content []byte) (*http.Response, error) {
	r := bytes.NewReader(content)
	return s.client.Post(s.store.URL()+path, "application/json", r)
}

func (s *storeTestSuite) TestStoreURL(c *C) {
	c.Assert(s.store.URL(), Equals, "http://"+defaultAddr)
}

func (s *storeTestSuite) TestTrivialGetWorks(c *C) {
	resp, err := s.StoreGet("/")
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 418)
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, "I'm a teapot")

}

func (s *storeTestSuite) TestSearchEndpoint(c *C) {
	resp, err := s.StoreGet("/search")
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 501)
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, "search not implemented")

}

func (s *storeTestSuite) TestDetailsEndpoint(c *C) {
	s.makeTestSnap(c, "name: foo\nversion: 1")
	resp, err := s.StoreGet(`/snaps/details/foo`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, fmt.Sprintf(`{
    "snap_id": "",
    "package_name": "foo",
    "origin": "canonical",
    "developer_id": "canonical",
    "anon_download_url": "%s/download/foo_1_all.snap",
    "download_url": "%s/download/foo_1_all.snap",
    "version": "1",
    "revision": 424242
}`, s.store.URL(), s.store.URL()))
}

func (s *storeTestSuite) TestBulkEndpoint(c *C) {
	s.makeTestSnap(c, "name: hello-world\nversion: 1")

	// note that we send the hello-world snapID here
	resp, err := s.StorePostJSON("/snaps/metadata", []byte(`{
"snaps": [{"snap_id":"buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ","channel":"stable","revision":1}]
}`))
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, fmt.Sprintf(`{
    "_embedded": {
        "clickindex:package": [
            {
                "snap_id": "buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ",
                "package_name": "hello-world",
                "origin": "canonical",
                "developer_id": "canonical",
                "anon_download_url": "%[1]s/download/hello-world_1_all.snap",
                "download_url": "%[1]s/download/hello-world_1_all.snap",
                "version": "1",
                "revision": 424242
            }
        ]
    }
}`, s.store.URL()))
}

func (s *storeTestSuite) makeTestSnap(c *C, snapYamlContent string) string {
	fn := snaptest.MakeTestSnapWithFiles(c, snapYamlContent, nil)
	dst := filepath.Join(s.store.blobDir, filepath.Base(fn))
	err := osutil.CopyFile(fn, dst, 0)
	c.Assert(err, IsNil)
	return dst
}

func (s *storeTestSuite) TestMakeTestSnap(c *C) {
	snapFn := s.makeTestSnap(c, "name: foo\nversion: 1")
	c.Assert(osutil.FileExists(snapFn), Equals, true)
	c.Assert(snapFn, Equals, filepath.Join(s.store.blobDir, "foo_1_all.snap"))
}

func (s *storeTestSuite) TestCollectSnaps(c *C) {
	s.makeTestSnap(c, "name: foo\nversion: 1")

	snaps, err := s.store.collectSnaps()
	c.Assert(err, IsNil)
	c.Assert(snaps, DeepEquals, map[string]string{
		"foo": filepath.Join(s.store.blobDir, "foo_1_all.snap"),
	})
}

func (s *storeTestSuite) TestSnapDownloadByFullname(c *C) {
	s.makeTestSnap(c, "name: foo\nversion: 1")

	resp, err := s.StoreGet("/download/foo_1_all.snap")
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
}

const (
	exampleSnapRev = `type: snap-revision
authority-id: canonical
snap-sha3-384: QlqR0uAWEAWF5Nwnzj5kqmmwFslYPu1IL16MKtLKhwhv0kpBv5wKZ_axf_nf_2cL
snap-id: snap-id-1
snap-size: 999
snap-revision: 36
developer-id: developer1
timestamp: 2016-08-19T19:19:19Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw=`
)

func (s *storeTestSuite) TestAssertionsEndpoint(c *C) {
	// something preloaded
	resp, err := s.StoreGet(`/assertions/account/testrootorg`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	c.Check(resp.Header.Get("Content-Type"), Equals, "application/x.ubuntu.assertion")

	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, string(asserts.Encode(systestkeys.TestRootAccount)))

	// something put in the assertion directory
	a, err := asserts.Decode([]byte(exampleSnapRev))
	c.Assert(err, IsNil)
	rev := a.(*asserts.SnapRevision)

	err = ioutil.WriteFile(filepath.Join(s.store.assertDir, "foo_36.snap-revision"), []byte(exampleSnapRev), 0655)
	c.Assert(err, IsNil)

	resp, err = s.StoreGet(`/assertions/snap-revision/` + rev.SnapSHA3_384())
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	body, err = ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, exampleSnapRev)

}
