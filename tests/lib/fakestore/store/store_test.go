// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"text/template"

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

func getSha(fn string) (string, uint64) {
	snapDigest, size, err := asserts.SnapFileSHA3_384(fn)
	if err != nil {
		panic(err)
	}
	return hexify(snapDigest), size
}

func (s *storeTestSuite) SetUpTest(c *C) {
	topdir := c.MkDir()
	err := os.Mkdir(filepath.Join(topdir, "asserts"), 0755)
	c.Assert(err, IsNil)
	s.store = NewStore(topdir, defaultAddr, false)
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
	resp, err := s.StoreGet("/api/v1/snaps/search")
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 501)
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, "search not implemented")

}

func (s *storeTestSuite) TestDetailsEndpointWithAssertions(c *C) {
	snapFn := s.makeTestSnap(c, "name: foo\nversion: 7")
	s.makeAssertions(c, snapFn, "foo", "xidididididididididididididididid", "foo-devel", "foo-devel-id", 77)

	resp, err := s.StoreGet(`/api/v1/snaps/details/foo`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)

	var body map[string]interface{}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	sha3_384, _ := getSha(snapFn)
	c.Check(body, DeepEquals, map[string]interface{}{
		"architecture":      []interface{}{"all"},
		"snap_id":           "xidididididididididididididididid",
		"package_name":      "foo",
		"origin":            "foo-devel",
		"developer_id":      "foo-devel-id",
		"anon_download_url": s.store.URL() + "/download/foo_7_all.snap",
		"download_url":      s.store.URL() + "/download/foo_7_all.snap",
		"version":           "7",
		"revision":          float64(77),
		"download_sha3_384": sha3_384,
	})
}

func (s *storeTestSuite) TestDetailsEndpoint(c *C) {
	snapFn := s.makeTestSnap(c, "name: foo\nversion: 1")
	resp, err := s.StoreGet(`/api/v1/snaps/details/foo`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)

	var body map[string]interface{}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	sha3_384, _ := getSha(snapFn)
	c.Check(body, DeepEquals, map[string]interface{}{
		"architecture":      []interface{}{"all"},
		"snap_id":           "",
		"package_name":      "foo",
		"origin":            "canonical",
		"developer_id":      "canonical",
		"anon_download_url": s.store.URL() + "/download/foo_1_all.snap",
		"download_url":      s.store.URL() + "/download/foo_1_all.snap",
		"version":           "1",
		"revision":          float64(424242),
		"download_sha3_384": sha3_384,
	})
}

func (s *storeTestSuite) TestBulkEndpoint(c *C) {
	snapFn := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 1")

	// note that we send the test-snapd-tools snapID here
	resp, err := s.StorePostJSON("/api/v1/snaps/metadata", []byte(`{
"snaps": [{"snap_id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","channel":"stable","revision":1}]
}`))
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)

	var body struct {
		Top struct {
			Cat []map[string]interface{} `json:"clickindex:package"`
		} `json:"_embedded"`
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	sha3_384, _ := getSha(snapFn)
	c.Check(body.Top.Cat, DeepEquals, []map[string]interface{}{{
		"architecture":      []interface{}{"all"},
		"snap_id":           "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"package_name":      "test-snapd-tools",
		"origin":            "canonical",
		"developer_id":      "canonical",
		"anon_download_url": s.store.URL() + "/download/test-snapd-tools_1_all.snap",
		"download_url":      s.store.URL() + "/download/test-snapd-tools_1_all.snap",
		"version":           "1",
		"revision":          float64(424242),
		"download_sha3_384": sha3_384,
	}})
}

func (s *storeTestSuite) TestBulkEndpointWithAssertions(c *C) {
	snapFn := s.makeTestSnap(c, "name: foo\nversion: 10")
	s.makeAssertions(c, snapFn, "foo", "xidididididididididididididididid", "foo-devel", "foo-devel-id", 99)

	resp, err := s.StorePostJSON("/api/v1/snaps/metadata", []byte(`{
"snaps": [{"snap_id":"xidididididididididididididididid","channel":"stable","revision":1}]
}`))
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	var body struct {
		Top struct {
			Cat []map[string]interface{} `json:"clickindex:package"`
		} `json:"_embedded"`
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	sha3_384, _ := getSha(snapFn)
	c.Check(body.Top.Cat, DeepEquals, []map[string]interface{}{{
		"architecture":      []interface{}{"all"},
		"snap_id":           "xidididididididididididididididid",
		"package_name":      "foo",
		"origin":            "foo-devel",
		"developer_id":      "foo-devel-id",
		"anon_download_url": s.store.URL() + "/download/foo_10_all.snap",
		"download_url":      s.store.URL() + "/download/foo_10_all.snap",
		"version":           "10",
		"revision":          float64(99),
		"download_sha3_384": sha3_384,
	}})
}

func (s *storeTestSuite) makeTestSnap(c *C, snapYamlContent string) string {
	fn := snaptest.MakeTestSnapWithFiles(c, snapYamlContent, nil)
	dst := filepath.Join(s.store.blobDir, filepath.Base(fn))
	err := osutil.CopyFile(fn, dst, 0)
	c.Assert(err, IsNil)
	return dst
}

var (
	tSnapDecl = template.Must(template.New("snap-decl").Parse(`type: snap-declaration
authority-id: testrootorg
series: 16
snap-id: {{.SnapID}}
publisher-id: {{.DeveloperID}}
snap-name: {{.Name}}
timestamp: 2016-08-19T19:19:19Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw=
`))
	tSnapRev = template.Must(template.New("snap-rev").Parse(`type: snap-revision
authority-id: testrootorg
snap-sha3-384: {{.Digest}}
developer-id: {{.DeveloperID}}
snap-id: {{.SnapID}}
snap-revision: {{.Revision}}
snap-size: {{.Size}}
timestamp: 2016-08-19T19:19:19Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw=
`))
	tAccount = template.Must(template.New("acct").Parse(`type: account
authority-id: testrootorg
account-id: {{.DeveloperID}}
display-name: {{.DevelName}} Dev
username: {{.DevelName}}
validation: unproven
timestamp: 2016-08-19T19:19:19Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw=
`))
)

func (s *storeTestSuite) makeAssertions(c *C, snapFn, name, snapID, develName, develID string, revision int) {
	dgst, size, err := asserts.SnapFileSHA3_384(snapFn)
	c.Assert(err, IsNil)

	info := essentialInfo{
		Name:        name,
		SnapID:      snapID,
		DeveloperID: develID,
		DevelName:   develName,
		Revision:    revision,
		Size:        size,
		Digest:      dgst,
	}

	f, err := os.OpenFile(filepath.Join(s.store.assertDir, snapID+".fake.snap-declaration"), os.O_CREATE|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	err = tSnapDecl.Execute(f, info)
	c.Assert(err, IsNil)

	f, err = os.OpenFile(filepath.Join(s.store.assertDir, dgst+".fake.snap-revision"), os.O_CREATE|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	err = tSnapRev.Execute(f, info)
	c.Assert(err, IsNil)

	f, err = os.OpenFile(filepath.Join(s.store.assertDir, develID+".fake.account"), os.O_CREATE|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	err = tAccount.Execute(f, info)
	c.Assert(err, IsNil)
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

func (s *storeTestSuite) TestAssertionsEndpointPreloaded(c *C) {
	// something preloaded
	resp, err := s.StoreGet(`/api/v1/snaps/assertions/account/testrootorg`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	c.Check(resp.Header.Get("Content-Type"), Equals, "application/x.ubuntu.assertion")

	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, string(asserts.Encode(systestkeys.TestRootAccount)))
}

func (s *storeTestSuite) TestAssertionsEndpointFromAssertsDir(c *C) {
	// something put in the assertion directory
	a, err := asserts.Decode([]byte(exampleSnapRev))
	c.Assert(err, IsNil)
	rev := a.(*asserts.SnapRevision)

	err = ioutil.WriteFile(filepath.Join(s.store.assertDir, "foo_36.snap-revision"), []byte(exampleSnapRev), 0655)
	c.Assert(err, IsNil)

	resp, err := s.StoreGet(`/api/v1/snaps/assertions/snap-revision/` + rev.SnapSHA3_384())
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, exampleSnapRev)
}

func (s *storeTestSuite) TestAssertionsEndpointNotFound(c *C) {
	// something not found
	resp, err := s.StoreGet(`/api/v1/snaps/assertions/account/not-an-account-id`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 404)

	dec := json.NewDecoder(resp.Body)
	var respObj map[string]interface{}
	err = dec.Decode(&respObj)
	c.Assert(err, IsNil)
	c.Check(respObj["status"], Equals, float64(404))
}

func (s *storeTestSuite) TestSnapActionEndpoint(c *C) {
	snapFn := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 1")

	resp, err := s.StorePostJSON("/v2/snaps/refresh", []byte(`{
"context": [{"instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","tracking-channel":"stable","revision":1}],
"actions": [{"action":"refresh","instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw"}]
}`))
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	var body struct {
		Results []map[string]interface{}
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	c.Check(body.Results, HasLen, 1)
	sha3_384, size := getSha(snapFn)
	c.Check(body.Results[0], DeepEquals, map[string]interface{}{
		"result":       "refresh",
		"instance-key": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"snap-id":      "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"name":         "test-snapd-tools",
		"snap": map[string]interface{}{
			"architectures": []interface{}{"all"},
			"snap-id":       "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
			"name":          "test-snapd-tools",
			"publisher": map[string]interface{}{
				"username": "canonical",
				"id":       "canonical",
			},
			"download": map[string]interface{}{
				"url":      s.store.URL() + "/download/test-snapd-tools_1_all.snap",
				"sha3-384": sha3_384,
				"size":     float64(size),
			},
			"version":  "1",
			"revision": float64(424242),
		},
	})
}

func (s *storeTestSuite) TestSnapActionEndpointWithAssertions(c *C) {
	snapFn := s.makeTestSnap(c, "name: foo\nversion: 10")
	s.makeAssertions(c, snapFn, "foo", "xidididididididididididididididid", "foo-devel", "foo-devel-id", 99)

	resp, err := s.StorePostJSON("/v2/snaps/refresh", []byte(`{
"context": [{"instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"xidididididididididididididididid","tracking-channel":"stable","revision":1}],
"actions": [{"action":"refresh","instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"xidididididididididididididididid"}]
}`))
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	var body struct {
		Results []map[string]interface{}
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	c.Check(body.Results, HasLen, 1)
	sha3_384, size := getSha(snapFn)
	c.Check(body.Results[0], DeepEquals, map[string]interface{}{
		"result":       "refresh",
		"instance-key": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"snap-id":      "xidididididididididididididididid",
		"name":         "foo",
		"snap": map[string]interface{}{
			"architectures": []interface{}{"all"},
			"snap-id":       "xidididididididididididididididid",
			"name":          "foo",
			"publisher": map[string]interface{}{
				"username": "foo-devel",
				"id":       "foo-devel-id",
			},
			"download": map[string]interface{}{
				"url":      s.store.URL() + "/download/foo_10_all.snap",
				"sha3-384": sha3_384,
				"size":     float64(size),
			},
			"version":  "10",
			"revision": float64(99),
		},
	})
}

func (s *storeTestSuite) TestSnapActionEndpointRefreshAll(c *C) {
	snapFn := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 1")

	resp, err := s.StorePostJSON("/v2/snaps/refresh", []byte(`{
"context": [{"instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","tracking-channel":"stable","revision":1}],
"actions": [{"action":"refresh-all"}]
}`))
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	var body struct {
		Results []map[string]interface{}
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	c.Check(body.Results, HasLen, 1)
	sha3_384, size := getSha(snapFn)
	c.Check(body.Results[0], DeepEquals, map[string]interface{}{
		"result":       "refresh",
		"instance-key": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"snap-id":      "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"name":         "test-snapd-tools",
		"snap": map[string]interface{}{
			"architectures": []interface{}{"all"},
			"snap-id":       "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
			"name":          "test-snapd-tools",
			"publisher": map[string]interface{}{
				"username": "canonical",
				"id":       "canonical",
			},
			"download": map[string]interface{}{
				"url":      s.store.URL() + "/download/test-snapd-tools_1_all.snap",
				"sha3-384": sha3_384,
				"size":     float64(size),
			},
			"version":  "1",
			"revision": float64(424242),
		},
	})
}

func (s *storeTestSuite) TestSnapActionEndpointWithAssertionsInstall(c *C) {
	snapFn := s.makeTestSnap(c, "name: foo\nversion: 10")
	s.makeAssertions(c, snapFn, "foo", "xidididididididididididididididid", "foo-devel", "foo-devel-id", 99)

	resp, err := s.StorePostJSON("/v2/snaps/refresh", []byte(`{
"context": [],
"actions": [{"action":"install","instance-key":"foo","name":"foo"}]
}`))
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	var body struct {
		Results []map[string]interface{}
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	c.Check(body.Results, HasLen, 1)
	sha3_384, size := getSha(snapFn)
	c.Check(body.Results[0], DeepEquals, map[string]interface{}{
		"result":       "install",
		"instance-key": "foo",
		"snap-id":      "xidididididididididididididididid",
		"name":         "foo",
		"snap": map[string]interface{}{
			"architectures": []interface{}{"all"},
			"snap-id":       "xidididididididididididididididid",
			"name":          "foo",
			"publisher": map[string]interface{}{
				"username": "foo-devel",
				"id":       "foo-devel-id",
			},
			"download": map[string]interface{}{
				"url":      s.store.URL() + "/download/foo_10_all.snap",
				"sha3-384": sha3_384,
				"size":     float64(size),
			},
			"version":  "10",
			"revision": float64(99),
		},
	})
}
