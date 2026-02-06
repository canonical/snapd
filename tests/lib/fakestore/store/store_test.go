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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"text/template"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/systestkeys"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type storeTestSuite struct {
	client *http.Client
	store  *Store
}

var _ = Suite(&storeTestSuite{})

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
	err = os.Mkdir(filepath.Join(topdir, "channels"), 0755)
	c.Assert(err, IsNil)
	s.store = NewStore(topdir, "localhost:0", false)
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
	u, err := url.Parse(s.store.URL())
	c.Assert(err, IsNil)
	c.Check(u.Hostname(), Equals, "127.0.0.1")
	c.Check(u.Port(), Not(Equals), "")
}

func (s *storeTestSuite) TestStoreListenAddr(c *C) {
	u, err := url.Parse(s.store.URL())
	c.Assert(err, IsNil)
	// use the same address as the fake store started by the tests so that
	// this will fail with addr-in-use error
	topdir := c.MkDir()
	newstore := NewStore(topdir, u.Host, false)
	err = newstore.Start()
	c.Assert(err, NotNil)
	c.Check(errors.Is(err, syscall.EADDRINUSE), Equals, true)
}

func (s *storeTestSuite) TestTrivialGetWorks(c *C) {
	resp, err := s.StoreGet("/")
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 418)
	body, err := io.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, "I'm a teapot")

}

func (s *storeTestSuite) TestSearchEndpoint(c *C) {
	resp, err := s.StoreGet("/api/v1/snaps/search")
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 501)
	body, err := io.ReadAll(resp.Body)
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

	var body map[string]any
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	sha3_384, _ := getSha(snapFn)
	c.Check(body, DeepEquals, map[string]any{
		"architecture":      []any{"all"},
		"snap_id":           "xidididididididididididididididid",
		"package_name":      "foo",
		"origin":            "foo-devel",
		"developer_id":      "foo-devel-id",
		"anon_download_url": s.store.URL() + "/download/foo_7_all.snap",
		"download_url":      s.store.URL() + "/download/foo_7_all.snap",
		"version":           "7",
		"revision":          float64(77),
		"download_sha3_384": sha3_384,
		"confinement":       "strict",
		"type":              "app",
	})
}

func (s *storeTestSuite) TestDetailsEndpoint(c *C) {
	snapFn := s.makeTestSnap(c, "name: foo\nversion: 1")
	resp, err := s.StoreGet(`/api/v1/snaps/details/foo`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)

	var body map[string]any
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	sha3_384, _ := getSha(snapFn)
	c.Check(body, DeepEquals, map[string]any{
		"architecture":      []any{"all"},
		"snap_id":           "",
		"package_name":      "foo",
		"origin":            "canonical",
		"developer_id":      "canonical",
		"anon_download_url": s.store.URL() + "/download/foo_1_all.snap",
		"download_url":      s.store.URL() + "/download/foo_1_all.snap",
		"version":           "1",
		"revision":          float64(424242),
		"download_sha3_384": sha3_384,
		"confinement":       "strict",
		"type":              "app",
	})

	snapFn = s.makeTestSnap(c, "name: foo-classic\nversion: 1\nconfinement: classic")
	resp, err = s.StoreGet(`/api/v1/snaps/details/foo-classic`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	sha3_384, _ = getSha(snapFn)
	c.Check(body, DeepEquals, map[string]any{
		"architecture":      []any{"all"},
		"snap_id":           "",
		"package_name":      "foo-classic",
		"origin":            "canonical",
		"developer_id":      "canonical",
		"anon_download_url": s.store.URL() + "/download/foo-classic_1_all.snap",
		"download_url":      s.store.URL() + "/download/foo-classic_1_all.snap",
		"version":           "1",
		"revision":          float64(424242),
		"download_sha3_384": sha3_384,
		"confinement":       "classic",
		"type":              "app",
	})

	snapFn = s.makeTestSnap(c, "name: foo-base\nversion: 1\ntype: base")
	resp, err = s.StoreGet(`/api/v1/snaps/details/foo-base`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	sha3_384, _ = getSha(snapFn)
	c.Check(body, DeepEquals, map[string]any{
		"architecture":      []any{"all"},
		"snap_id":           "",
		"package_name":      "foo-base",
		"origin":            "canonical",
		"developer_id":      "canonical",
		"anon_download_url": s.store.URL() + "/download/foo-base_1_all.snap",
		"download_url":      s.store.URL() + "/download/foo-base_1_all.snap",
		"version":           "1",
		"revision":          float64(424242),
		"download_sha3_384": sha3_384,
		"confinement":       "strict",
		"type":              "base",
	})

	snapFn = s.makeTestSnap(c, "name: foo-core20\nversion: 1\ntype: app\nbase: core20")
	resp, err = s.StoreGet(`/api/v1/snaps/details/foo-core20`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	sha3_384, _ = getSha(snapFn)
	c.Check(body, DeepEquals, map[string]any{
		"architecture":      []any{"all"},
		"snap_id":           "",
		"package_name":      "foo-core20",
		"origin":            "canonical",
		"developer_id":      "canonical",
		"anon_download_url": s.store.URL() + "/download/foo-core20_1_all.snap",
		"download_url":      s.store.URL() + "/download/foo-core20_1_all.snap",
		"version":           "1",
		"revision":          float64(424242),
		"download_sha3_384": sha3_384,
		"confinement":       "strict",
		"type":              "app",
		"base":              "core20",
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
			Cat []map[string]any `json:"clickindex:package"`
		} `json:"_embedded"`
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	sha3_384, _ := getSha(snapFn)
	c.Check(body.Top.Cat, DeepEquals, []map[string]any{{
		"architecture":      []any{"all"},
		"snap_id":           "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"package_name":      "test-snapd-tools",
		"origin":            "canonical",
		"developer_id":      "canonical",
		"anon_download_url": s.store.URL() + "/download/test-snapd-tools_1_all.snap",
		"download_url":      s.store.URL() + "/download/test-snapd-tools_1_all.snap",
		"version":           "1",
		"revision":          float64(424242),
		"download_sha3_384": sha3_384,
		"confinement":       "strict",
		"type":              "app",
	}})
}

func (s *storeTestSuite) TestBulkEndpointWithAssertions(c *C) {
	snapFn1 := s.makeTestSnap(c, "name: foo\nversion: 10")
	s.makeAssertions(c, snapFn1, "foo", "xidididididididididididididididid", "foo-devel", "foo-devel-id", 99)
	snapFn2 := s.makeTestSnap(c, "name: foo-core20\nversion: 10\nbase: core20")
	s.makeAssertions(c, snapFn2, "foo-core20", "xidididididididididididididcore20", "foo-core20-devel", "foo-core20-devel-id", 98)

	resp, err := s.StorePostJSON("/api/v1/snaps/metadata", []byte(`{
"snaps": [
    {"snap_id":"xidididididididididididididididid","channel":"stable","revision":1},
    {"snap_id":"xidididididididididididididcore20","channel":"stable","revision":1}
]
}`))
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	var body struct {
		Top struct {
			Cat []map[string]any `json:"clickindex:package"`
		} `json:"_embedded"`
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	sha3_384_1, _ := getSha(snapFn1)
	sha3_384_2, _ := getSha(snapFn2)
	c.Check(body.Top.Cat, DeepEquals, []map[string]any{{
		"architecture":      []any{"all"},
		"snap_id":           "xidididididididididididididididid",
		"package_name":      "foo",
		"origin":            "foo-devel",
		"developer_id":      "foo-devel-id",
		"anon_download_url": s.store.URL() + "/download/foo_10_all.snap",
		"download_url":      s.store.URL() + "/download/foo_10_all.snap",
		"version":           "10",
		"revision":          float64(99),
		"download_sha3_384": sha3_384_1,
		"confinement":       "strict",
		"type":              "app",
	}, {
		"architecture":      []any{"all"},
		"snap_id":           "xidididididididididididididcore20",
		"package_name":      "foo-core20",
		"origin":            "foo-core20-devel",
		"developer_id":      "foo-core20-devel-id",
		"anon_download_url": s.store.URL() + "/download/foo-core20_10_all.snap",
		"download_url":      s.store.URL() + "/download/foo-core20_10_all.snap",
		"version":           "10",
		"revision":          float64(98),
		"download_sha3_384": sha3_384_2,
		"confinement":       "strict",
		"type":              "app",
		"base":              "core20",
	}})
}

func (s *storeTestSuite) makeTestSnap(c *C, snapYamlContent string) string {
	fn := snaptest.MakeTestSnapWithFiles(c, snapYamlContent, nil)
	dst := filepath.Join(s.store.blobDir, filepath.Base(fn))
	err := osutil.CopyFile(fn, dst, 0)
	c.Assert(err, IsNil)
	return dst
}

func (s *storeTestSuite) makeTestComponent(c *C, yaml string) string {
	fn := snaptest.MakeTestComponent(c, yaml)
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
	tResourceRevision = template.Must(template.New("resource-revision").Parse(`type: snap-resource-revision
authority-id: testrootorg
snap-id: {{.SnapID}}
resource-name: {{.Name}}
resource-size: {{.Size}}
resource-sha3-384: {{.Digest}}
resource-revision: {{.Revision}}
developer-id: {{.DeveloperID}}
snap-name: {{.Name}}
timestamp: 2016-08-19T19:19:19Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw=
`))
	tResourcePair = template.Must(template.New("resource-pair").Parse(`type: snap-resource-pair
authority-id: testrootorg
snap-id: {{.SnapID}}
resource-name: {{.Name}}
resource-revision: {{.Revision}}
snap-revision: {{.SnapRevision}}
developer-id: {{.DeveloperID}}
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

func (s *storeTestSuite) addToChannel(c *C, snapFn, channel string) {
	dgst, _, err := asserts.SnapFileSHA3_384(snapFn)
	c.Assert(err, IsNil)

	f, err := os.OpenFile(filepath.Join(s.store.blobDir, "channels", dgst), os.O_CREATE|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	defer f.Close()

	fmt.Fprintf(f, "%s\n", channel)
}

func (s *storeTestSuite) makeComponentAssertions(c *C, fn, name, snapID, develID string, compRev, snapRev int) {
	type essentialComponentInfo struct {
		Name         string
		SnapID       string
		DeveloperID  string
		Revision     int
		SnapRevision int
		Digest       string
		Size         uint64
	}

	digest, size, err := asserts.SnapFileSHA3_384(fn)
	c.Assert(err, IsNil)

	info := essentialComponentInfo{
		Name:         name,
		SnapID:       snapID,
		DeveloperID:  develID,
		Revision:     compRev,
		SnapRevision: snapRev,
		Digest:       digest,
		Size:         size,
	}

	f, err := os.OpenFile(filepath.Join(s.store.assertDir, fmt.Sprintf("%s+%s.fake.snap-resource-revison", snapID, name)), os.O_CREATE|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	err = tResourceRevision.Execute(f, info)
	c.Assert(err, IsNil)

	f, err = os.OpenFile(filepath.Join(s.store.assertDir, fmt.Sprintf("%s+%s+%d.fake.snap-resource-pair", snapID, name, snapRev)), os.O_CREATE|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	err = tResourcePair.Execute(f, info)
	c.Assert(err, IsNil)
}

func (s *storeTestSuite) TestMakeTestSnap(c *C) {
	snapFn := s.makeTestSnap(c, "name: foo\nversion: 1")
	c.Assert(osutil.FileExists(snapFn), Equals, true)
	c.Assert(snapFn, Equals, filepath.Join(s.store.blobDir, "foo_1_all.snap"))
}

func (s *storeTestSuite) TestCollectSnaps(c *C) {
	fn := s.makeTestSnap(c, "name: foo\nversion: 1")
	s.makeAssertions(c, fn, "foo", snaptest.AssertedSnapID("foo"), "devel", "devel-id", 5)

	fn = s.makeTestSnap(c, "name: foo\nversion: 2")
	s.makeAssertions(c, fn, "foo", snaptest.AssertedSnapID("foo"), "devel", "devel-id", 6)

	fn = s.makeTestSnap(c, "name: bar\nversion: 3")
	s.makeAssertions(c, fn, "bar", snaptest.AssertedSnapID("bar"), "devel", "devel-id", 7)

	fn = s.makeTestComponent(c, "component: foo+comp1\nversion: 4\ntype: standard")

	// same component is shared across two snap revisions
	s.makeComponentAssertions(c, fn, "comp1", snaptest.AssertedSnapID("foo"), "devel-id", 8, 5)
	s.makeComponentAssertions(c, fn, "comp1", snaptest.AssertedSnapID("foo"), "devel-id", 8, 6)

	bs, err := s.store.collectAssertions()
	c.Assert(err, IsNil)

	snaps, err := s.store.collectSnaps(bs)
	c.Assert(err, IsNil)
	c.Assert(snaps, DeepEquals, map[string]*revisionSet{
		"foo": {
			latest: snap.R(6),
			revisions: map[snap.Revision]availableSnap{
				snap.R(5): {
					path: filepath.Join(s.store.blobDir, "foo_1_all.snap"),
					components: map[string]availableComponent{
						"comp1": {
							path:     filepath.Join(s.store.blobDir, "foo+comp1.comp"),
							revision: snap.R(8),
						},
					},
				},
				snap.R(6): {
					path: filepath.Join(s.store.blobDir, "foo_2_all.snap"),
					components: map[string]availableComponent{
						"comp1": {
							path:     filepath.Join(s.store.blobDir, "foo+comp1.comp"),
							revision: snap.R(8),
						},
					},
				},
			},
		},
		"bar": {
			latest: snap.R(7),
			revisions: map[snap.Revision]availableSnap{
				snap.R(7): {
					path:       filepath.Join(s.store.blobDir, "bar_3_all.snap"),
					components: make(map[string]availableComponent),
				},
			},
		},
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
	exampleValidationSet = `type: validation-set
authority-id: canonical
account-id: canonical
name: base-set
sequence: 2
revision: 1
series: 16
snaps:
  -
    id: yOqKhntON3vR7kwEbVPsILm7bUViPDzz
    name: snap-b
    presence: required
    revision: 1
timestamp: 2020-11-06T09:16:26Z
sign-key-sha3-384: 7bbncP0c4RcufwReeiylCe0S7IMCn-tHLNSCgeOVmV3K-7_MzpAHgJDYeOjldefE

AXNpZw==`
)

func (s *storeTestSuite) TestAssertionsEndpointPreloaded(c *C) {
	// something preloaded
	resp, err := s.StoreGet(`/v2/assertions/account/testrootorg`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	c.Check(resp.Header.Get("Content-Type"), Equals, "application/x.ubuntu.assertion")

	body, err := io.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, string(asserts.Encode(systestkeys.TestRootAccount)))
}

func (s *storeTestSuite) TestAssertionsEndpointFromAssertsDir(c *C) {
	// something put in the assertion directory
	a, err := asserts.Decode([]byte(exampleSnapRev))
	c.Assert(err, IsNil)
	rev := a.(*asserts.SnapRevision)

	err = os.WriteFile(filepath.Join(s.store.assertDir, "foo_36.snap-revision"), []byte(exampleSnapRev), 0655)
	c.Assert(err, IsNil)

	resp, err := s.StoreGet(`/v2/assertions/snap-revision/` + rev.SnapSHA3_384())
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	body, err := io.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, exampleSnapRev)
}

func (s *storeTestSuite) TestAssertionsEndpointSequenceAssertion(c *C) {
	err := os.WriteFile(filepath.Join(s.store.assertDir, "base-set.validation-set"), []byte(exampleValidationSet), 0655)
	c.Assert(err, IsNil)

	resp, err := s.StoreGet(`/v2/assertions/validation-set/16/canonical/base-set?sequence=2`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Check(resp.StatusCode, Equals, 200)
	body, err := io.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, exampleValidationSet)
}

func (s *storeTestSuite) TestAssertionsEndpointSequenceAssertionLatest(c *C) {
	err := os.WriteFile(filepath.Join(s.store.assertDir, "base-set.validation-set"), []byte(exampleValidationSet), 0655)
	c.Assert(err, IsNil)

	resp, err := s.StoreGet(`/v2/assertions/validation-set/16/canonical/base-set?sequence=latest`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Check(resp.StatusCode, Equals, 200)
	body, err := io.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, exampleValidationSet)
}

func (s *storeTestSuite) TestAssertionsEndpointSequenceAssertionInvalidSequence(c *C) {
	err := os.WriteFile(filepath.Join(s.store.assertDir, "base-set.validation-set"), []byte(exampleValidationSet), 0655)
	c.Assert(err, IsNil)

	resp, err := s.StoreGet(`/v2/assertions/validation-set/16/canonical/base-set?sequence=0`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 400)
	body, err := io.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, "cannot retrieve assertion [16 canonical base-set]: the requested sequence must be above 0\n")
}

func (s *storeTestSuite) TestAssertionsEndpointSequenceInvalid(c *C) {
	resp, err := s.StoreGet(`/v2/assertions/validation-set/16/canonical/base-set?sequence=foo`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 400)
	body, err := io.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, "cannot retrieve assertion [16 canonical base-set]: cannot parse sequence foo: strconv.Atoi: parsing \"foo\": invalid syntax\n")
}

func (s *storeTestSuite) TestAssertionsEndpointNotFound(c *C) {
	// something not found
	resp, err := s.StoreGet(`/v2/assertions/account/not-an-account-id`)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 404)

	dec := json.NewDecoder(resp.Body)
	var respObj map[string]any
	err = dec.Decode(&respObj)
	c.Assert(err, IsNil)
	c.Check(respObj["error-list"], DeepEquals, []any{map[string]any{"code": "not-found", "message": "not found"}})
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
		Results []map[string]any
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	c.Check(body.Results, HasLen, 1)
	sha3_384, size := getSha(snapFn)
	c.Check(body.Results[0], DeepEquals, map[string]any{
		"result":       "refresh",
		"instance-key": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"snap-id":      "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"name":         "test-snapd-tools",
		"snap": map[string]any{
			"architectures": []any{"all"},
			"snap-id":       "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
			"name":          "test-snapd-tools",
			"publisher": map[string]any{
				"username": "canonical",
				"id":       "canonical",
			},
			"download": map[string]any{
				"url":      s.store.URL() + "/download/test-snapd-tools_1_all.snap",
				"sha3-384": sha3_384,
				"size":     float64(size),
			},
			"version":     "1",
			"revision":    float64(424242),
			"confinement": "strict",
			"type":        "app",
			"snap-yaml":   "name: test-snapd-tools\nversion: 1",
		},
	})
}

func (s *storeTestSuite) TestSnapActionEndpointUsesLatest(c *C) {
	snapFn := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 1")
	s.makeAssertions(c, snapFn, "test-snapd-tools", "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw", "canonical", "canonical", 1)

	snapFn = s.makeTestSnap(c, "name: test-snapd-tools\nversion: 2")
	s.makeAssertions(c, snapFn, "test-snapd-tools", "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw", "canonical", "canonical", 2)

	resp, err := s.StorePostJSON("/v2/snaps/refresh", []byte(`{
"context": [{"instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","tracking-channel":"stable","revision":1}],
"actions": [{"action":"refresh","instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw"}]
}`))
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	var body struct {
		Results []map[string]any
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	c.Check(body.Results, HasLen, 1)
	sha3_384, size := getSha(snapFn)
	c.Check(body.Results[0], DeepEquals, map[string]any{
		"result":       "refresh",
		"instance-key": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"snap-id":      "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"name":         "test-snapd-tools",
		"snap": map[string]any{
			"architectures": []any{"all"},
			"snap-id":       "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
			"name":          "test-snapd-tools",
			"publisher": map[string]any{
				"username": "canonical",
				"id":       "canonical",
			},
			"download": map[string]any{
				"url":      s.store.URL() + "/download/test-snapd-tools_2_all.snap",
				"sha3-384": sha3_384,
				"size":     float64(size),
			},
			"version":     "2",
			"revision":    float64(2),
			"confinement": "strict",
			"type":        "app",
			"snap-yaml":   "name: test-snapd-tools\nversion: 2",
		},
	})
}

func (s *storeTestSuite) TestSnapActionEndpointChannel(c *C) {
	snapFn := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 1")
	s.makeAssertions(c, snapFn, "test-snapd-tools", "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw", "canonical", "canonical", 1)
	s.addToChannel(c, snapFn, "latest/stable")

	snapFnEdge := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 2")
	s.makeAssertions(c, snapFnEdge, "test-snapd-tools", "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw", "canonical", "canonical", 2)
	s.addToChannel(c, snapFnEdge, "latest/edge")

	resp, err := s.StorePostJSON("/v2/snaps/refresh", []byte(`{
"context": [{"instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","tracking-channel":"latest/stable","revision":1}],
"actions": [{"action":"refresh","instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","channel":"latest/stable"}]
}`))
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	var body struct {
		Results []map[string]any
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	c.Check(body.Results, HasLen, 1)
	sha3_384, size := getSha(snapFn)
	c.Check(body.Results[0], DeepEquals, map[string]any{
		"result":       "refresh",
		"instance-key": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"snap-id":      "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"name":         "test-snapd-tools",
		"snap": map[string]any{
			"architectures": []any{"all"},
			"snap-id":       "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
			"name":          "test-snapd-tools",
			"publisher": map[string]any{
				"username": "canonical",
				"id":       "canonical",
			},
			"download": map[string]any{
				"url":      s.store.URL() + "/download/test-snapd-tools_1_all.snap",
				"sha3-384": sha3_384,
				"size":     float64(size),
			},
			"version":     "1",
			"revision":    float64(1),
			"confinement": "strict",
			"type":        "app",
			"snap-yaml":   "name: test-snapd-tools\nversion: 1",
		},
	})
}

func (s *storeTestSuite) TestSnapActionEndpointChannelRefreshAll(c *C) {
	snapFn := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 1")
	s.makeAssertions(c, snapFn, "test-snapd-tools", "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw", "canonical", "canonical", 1)
	s.addToChannel(c, snapFn, "latest/stable")

	snapFnEdge := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 2")
	s.makeAssertions(c, snapFnEdge, "test-snapd-tools", "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw", "canonical", "canonical", 2)
	s.addToChannel(c, snapFnEdge, "latest/edge")

	resp, err := s.StorePostJSON("/v2/snaps/refresh", []byte(`{
"context": [{"instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","tracking-channel":"latest/stable","revision":1}],
"actions": [{"action":"refresh-all"}]
}`))
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	var body struct {
		Results []map[string]any
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	c.Check(body.Results, HasLen, 1)
	sha3_384, size := getSha(snapFn)
	c.Check(body.Results[0], DeepEquals, map[string]any{
		"result":       "refresh",
		"instance-key": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"snap-id":      "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"name":         "test-snapd-tools",
		"snap": map[string]any{
			"architectures": []any{"all"},
			"snap-id":       "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
			"name":          "test-snapd-tools",
			"publisher": map[string]any{
				"username": "canonical",
				"id":       "canonical",
			},
			"download": map[string]any{
				"url":      s.store.URL() + "/download/test-snapd-tools_1_all.snap",
				"sha3-384": sha3_384,
				"size":     float64(size),
			},
			"version":     "1",
			"revision":    float64(1),
			"confinement": "strict",
			"type":        "app",
			"snap-yaml":   "name: test-snapd-tools\nversion: 1",
		},
	})
}

func (s *storeTestSuite) TestSnapActionEndpointAssertedWithRevision(c *C) {
	oldFn := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 1")
	s.makeAssertions(c, oldFn, "test-snapd-tools", "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw", "canonical", "canonical", 5)

	latestFn := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 2")
	s.makeAssertions(c, latestFn, "test-snapd-tools", "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw", "canonical", "canonical", 6)

	request := func(rev snap.Revision, version string, path string) {
		post := fmt.Sprintf(`{
		"context": [{"instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","tracking-channel":"stable","revision":1}],
		"actions": [{"action":"refresh","instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw", "revision":%d}]
		}`, rev.N)

		resp, err := s.StorePostJSON("/v2/snaps/refresh", []byte(post))
		c.Assert(err, IsNil)
		defer resp.Body.Close()

		c.Assert(resp.StatusCode, Equals, 200)
		var body struct {
			Results []map[string]any
		}
		c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
		c.Check(body.Results, HasLen, 1)
		sha3_384, size := getSha(path)
		c.Check(body.Results[0], DeepEquals, map[string]any{
			"result":       "refresh",
			"instance-key": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
			"snap-id":      "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
			"name":         "test-snapd-tools",
			"snap": map[string]any{
				"architectures": []any{"all"},
				"snap-id":       "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
				"name":          "test-snapd-tools",
				"publisher": map[string]any{
					"username": "canonical",
					"id":       "canonical",
				},
				"download": map[string]any{
					"url":      s.store.URL() + "/download/" + filepath.Base(path),
					"sha3-384": sha3_384,
					"size":     float64(size),
				},
				"version":     version,
				"revision":    float64(rev.N),
				"confinement": "strict",
				"type":        "app",
				"snap-yaml":   "name: test-snapd-tools\nversion: " + version,
			},
		})
	}

	request(snap.R(5), "1", oldFn)
	request(snap.R(6), "2", latestFn)
}

func (s *storeTestSuite) TestSnapActionEndpointAssertedWithComponents(c *C) {
	snapWithoutComp := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 1")
	s.makeAssertions(c, snapWithoutComp, "test-snapd-tools", "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw", "canonical", "canonical", 5)

	snapWithcomp := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 2")
	s.makeAssertions(c, snapWithcomp, "test-snapd-tools", "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw", "canonical", "canonical", 6)

	componentFn := s.makeTestComponent(c, "component: test-snapd-tools+comp1\nversion: 4\ntype: standard")
	s.makeComponentAssertions(c, componentFn, "comp1", "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw", "canonical", 8, 6)

	compDigest, compSize, err := asserts.SnapFileSHA3_384(componentFn)
	c.Assert(err, IsNil)

	type availableComponent struct {
		path     string
		digest   string
		size     uint64
		revision snap.Revision
		version  string
	}

	request := func(rev snap.Revision, version string, path string, comps map[string]availableComponent) {
		post := fmt.Sprintf(`{
		"context": [{"instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","tracking-channel":"stable","revision":1}],
		"actions": [{"action":"refresh","instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw", "revision":%d}]
		}`, rev.N)

		resp, err := s.StorePostJSON("/v2/snaps/refresh", []byte(post))
		c.Assert(err, IsNil)
		defer resp.Body.Close()

		c.Assert(resp.StatusCode, Equals, 200)
		var body struct {
			Results []map[string]any
		}
		c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
		c.Check(body.Results, HasLen, 1)
		sha3_384, size := getSha(path)

		payload := map[string]any{
			"result":       "refresh",
			"instance-key": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
			"snap-id":      "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
			"name":         "test-snapd-tools",
			"snap": map[string]any{
				"architectures": []any{"all"},
				"snap-id":       "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
				"name":          "test-snapd-tools",
				"publisher": map[string]any{
					"username": "canonical",
					"id":       "canonical",
				},
				"download": map[string]any{
					"url":      s.store.URL() + "/download/" + filepath.Base(path),
					"sha3-384": sha3_384,
					"size":     float64(size),
				},
				"version":     version,
				"revision":    float64(rev.N),
				"confinement": "strict",
				"type":        "app",
				"snap-yaml":   "name: test-snapd-tools\nversion: " + version,
			},
		}

		var resources []any
		for name, comp := range comps {
			resources = append(resources, map[string]any{
				"download": map[string]any{
					"url":      s.store.URL() + "/download/" + filepath.Base(comp.path),
					"sha3-384": comp.digest,
					"size":     float64(comp.size),
				},
				"type":     "component/standard",
				"name":     name,
				"revision": float64(comp.revision.N),
				"version":  comp.version,
			})
		}

		if len(resources) > 0 {
			payload["snap"].(map[string]any)["resources"] = resources
		}

		c.Check(body.Results[0], DeepEquals, payload)
	}

	request(snap.R(5), "1", snapWithoutComp, map[string]availableComponent{})
	request(snap.R(6), "2", snapWithcomp, map[string]availableComponent{
		"comp1": {
			path:     componentFn,
			digest:   hexify(compDigest),
			size:     compSize,
			revision: snap.R(8),
			version:  "4",
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
		Results []map[string]any
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	c.Check(body.Results, HasLen, 1)
	sha3_384, size := getSha(snapFn)
	c.Check(body.Results[0], DeepEquals, map[string]any{
		"result":       "refresh",
		"instance-key": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"snap-id":      "xidididididididididididididididid",
		"name":         "foo",
		"snap": map[string]any{
			"architectures": []any{"all"},
			"snap-id":       "xidididididididididididididididid",
			"name":          "foo",
			"publisher": map[string]any{
				"username": "foo-devel",
				"id":       "foo-devel-id",
			},
			"download": map[string]any{
				"url":      s.store.URL() + "/download/foo_10_all.snap",
				"sha3-384": sha3_384,
				"size":     float64(size),
			},
			"version":     "10",
			"revision":    float64(99),
			"confinement": "strict",
			"type":        "app",
			"snap-yaml":   "name: foo\nversion: 10",
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
		Results []map[string]any
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	c.Check(body.Results, HasLen, 1)
	sha3_384, size := getSha(snapFn)
	c.Check(body.Results[0], DeepEquals, map[string]any{
		"result":       "refresh",
		"instance-key": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"snap-id":      "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"name":         "test-snapd-tools",
		"snap": map[string]any{
			"architectures": []any{"all"},
			"snap-id":       "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
			"name":          "test-snapd-tools",
			"publisher": map[string]any{
				"username": "canonical",
				"id":       "canonical",
			},
			"download": map[string]any{
				"url":      s.store.URL() + "/download/test-snapd-tools_1_all.snap",
				"sha3-384": sha3_384,
				"size":     float64(size),
			},
			"version":     "1",
			"revision":    float64(424242),
			"confinement": "strict",
			"type":        "app",
			"snap-yaml":   "name: test-snapd-tools\nversion: 1",
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
		Results []map[string]any
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	c.Check(body.Results, HasLen, 1)
	sha3_384, size := getSha(snapFn)
	c.Check(body.Results[0], DeepEquals, map[string]any{
		"result":       "install",
		"instance-key": "foo",
		"snap-id":      "xidididididididididididididididid",
		"name":         "foo",
		"snap": map[string]any{
			"architectures": []any{"all"},
			"snap-id":       "xidididididididididididididididid",
			"name":          "foo",
			"publisher": map[string]any{
				"username": "foo-devel",
				"id":       "foo-devel-id",
			},
			"download": map[string]any{
				"url":      s.store.URL() + "/download/foo_10_all.snap",
				"sha3-384": sha3_384,
				"size":     float64(size),
			},
			"version":     "10",
			"revision":    float64(99),
			"confinement": "strict",
			"type":        "app",
			"snap-yaml":   "name: foo\nversion: 10",
		},
	})
}

func (s *storeTestSuite) TestSnapActionEndpointSnapWithBase(c *C) {
	snapFn := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 1\nbase: core20")

	resp, err := s.StorePostJSON("/v2/snaps/refresh", []byte(`{
"context": [{"instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","tracking-channel":"stable","revision":1}],
"actions": [{"action":"refresh","instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw"}]
}`))
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	var body struct {
		Results []map[string]any
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	c.Check(body.Results, HasLen, 1)
	sha3_384, size := getSha(snapFn)
	c.Check(body.Results[0], DeepEquals, map[string]any{
		"result":       "refresh",
		"instance-key": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"snap-id":      "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"name":         "test-snapd-tools",
		"snap": map[string]any{
			"architectures": []any{"all"},
			"snap-id":       "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
			"name":          "test-snapd-tools",
			"publisher": map[string]any{
				"username": "canonical",
				"id":       "canonical",
			},
			"download": map[string]any{
				"url":      s.store.URL() + "/download/test-snapd-tools_1_all.snap",
				"sha3-384": sha3_384,
				"size":     float64(size),
			},
			"version":     "1",
			"revision":    float64(424242),
			"confinement": "strict",
			"type":        "app",
			"base":        "core20",
			"snap-yaml":   "name: test-snapd-tools\nversion: 1\nbase: core20",
		},
	})
}

func (s *storeTestSuite) TestSnapActionEndpointUnknownSnap(c *C) {
	s.makeTestSnap(c, "name: test-snapd-tools\nversion: 1\nbase: core20")

	req, err := http.NewRequest("POST", s.store.URL()+"/v2/snaps/refresh", bytes.NewReader([]byte(`{
		"context": [{"instance-key":"foo","snap-id":"foo-id","tracking-channel":"stable","revision":1},{"instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","tracking-channel":"stable","revision":1}],
		"actions": [{"action":"refresh","instance-key":"foo","snap-id":"foo-id"},{"action":"refresh","instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw"}]
	}`)))
	req.Header["Snap-Refresh-Reason"] = nil
	c.Assert(err, IsNil)
	resp, err := s.client.Do(req)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 400)
	body, err := io.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, "unknown snap-id: \"foo-id\"\n")
}

func (s *storeTestSuite) TestSnapActionEndpointUnknownSnapAutoRefresh(c *C) {
	snapFn := s.makeTestSnap(c, "name: test-snapd-tools\nversion: 1\nbase: core20")

	req, err := http.NewRequest("POST", s.store.URL()+"/v2/snaps/refresh", bytes.NewReader([]byte(`{
		"context": [{"instance-key":"foo","snap-id":"foo-id","tracking-channel":"stable","revision":1},{"instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","tracking-channel":"stable","revision":1}],
		"actions": [{"action":"refresh","instance-key":"foo","snap-id":"foo-id"},{"action":"refresh","instance-key":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw","snap-id":"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw"}]
	}`)))
	// Mark as auto-refresh
	req.Header["Snap-Refresh-Reason"] = []string{"scheduled"}
	c.Assert(err, IsNil)
	resp, err := s.client.Do(req)
	c.Assert(err, IsNil)
	defer resp.Body.Close()

	c.Assert(resp.StatusCode, Equals, 200)
	var body struct {
		Results []map[string]any
	}
	c.Assert(json.NewDecoder(resp.Body).Decode(&body), IsNil)
	c.Check(body.Results, HasLen, 1)
	sha3_384, size := getSha(snapFn)
	// Ignore unknown snaps during auto-refresh
	c.Check(body.Results[0], DeepEquals, map[string]any{
		"result":       "refresh",
		"instance-key": "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"snap-id":      "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
		"name":         "test-snapd-tools",
		"snap": map[string]any{
			"architectures": []any{"all"},
			"snap-id":       "eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw",
			"name":          "test-snapd-tools",
			"publisher": map[string]any{
				"username": "canonical",
				"id":       "canonical",
			},
			"download": map[string]any{
				"url":      s.store.URL() + "/download/test-snapd-tools_1_all.snap",
				"sha3-384": sha3_384,
				"size":     float64(size),
			},
			"version":     "1",
			"revision":    float64(424242),
			"confinement": "strict",
			"type":        "app",
			"base":        "core20",
			"snap-yaml":   "name: test-snapd-tools\nversion: 1\nbase: core20",
		},
	})
}
