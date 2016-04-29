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

	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap/snaptest"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type storeTestSuite struct {
	client *http.Client
	store  *Store
}

var _ = Suite(&storeTestSuite{})

func (s *storeTestSuite) SetUpTest(c *C) {
	s.store = NewStore(c.MkDir())
	err := s.store.Start()
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
	c.Assert(string(body), Equals, "full search not implemented")

}

func (s *storeTestSuite) TestExactMathEndpoint(c *C) {
	s.makeTestSnap(c, "name: foo\nversion: 1")
	resp, err := s.StoreGet(`/search?q=package_name:"foo"`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, fmt.Sprintf(`{
    "_embedded": {
        "clickindex:package": [
            {
                "name": "foo.canonical",
                "package_name": "foo",
                "origin": "canonical",
                "anon_download_url": "%s/download/foo_1_all.snap",
                "download_url": "%s/download/foo_1_all.snap",
                "version": "1",
                "revision": 424242
            }
        ]
    }
}`, s.store.URL(), s.store.URL()))
}

func (s *storeTestSuite) TestBulkEndpoint(c *C) {
	c.Skip("not relevant atm, will change")
	s.makeTestSnap(c, "name: foo\nversion: 1")

	resp, err := s.StorePostJSON("/click-metadata", []byte(`{
"name": ["foo.canonical"]
}`))
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, fmt.Sprintf(`[
    {
        "status": "Published",
        "name": "foo.canonical",
        "package_name": "foo",
        "origin": "canonical",
        "anon_download_url": "%s/download/foo_1_all.snap",
        "version": "1",
        "revision": 424242
    }
]`, s.store.URL()))
}

// FIXME: extract into snappy/testutils
func (s *storeTestSuite) makeTestSnap(c *C, snapYamlContent string) string {
	tmpdir := c.MkDir()
	os.MkdirAll(filepath.Join(tmpdir, "meta"), 0755)

	snapYaml := filepath.Join(tmpdir, "meta", "snap.yaml")
	err := ioutil.WriteFile(snapYaml, []byte(snapYamlContent), 0644)
	c.Assert(err, IsNil)

	targetDir := s.store.blobDir
	snapFn, err := snaptest.BuildSquashfsSnap(tmpdir, targetDir)
	c.Assert(err, IsNil)
	return snapFn
}

func (s *storeTestSuite) TestMakeTestSnap(c *C) {
	snapFn := s.makeTestSnap(c, "name: foo\nversion: 1")
	c.Assert(osutil.FileExists(snapFn), Equals, true)
	c.Assert(snapFn, Equals, filepath.Join(s.store.blobDir, "foo_1_all.snap"))
}

func (s *storeTestSuite) TestRefreshSnaps(c *C) {
	s.makeTestSnap(c, "name: foo\nversion: 1")

	s.store.refreshSnaps()
	c.Assert(s.store.snaps, DeepEquals, map[string]string{
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
