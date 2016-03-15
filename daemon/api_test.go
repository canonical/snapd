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

package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/asserts"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/release"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/remote"
	"github.com/ubuntu-core/snappy/snappy"
	"github.com/ubuntu-core/snappy/systemd"
	"github.com/ubuntu-core/snappy/testutil"
	"github.com/ubuntu-core/snappy/timeout"
)

type apiSuite struct {
	parts      []*snappy.RemoteSnap
	err        error
	vars       map[string]string
	searchTerm string
	channel    string
	overlord   *fakeOverlord
}

var _ = check.Suite(&apiSuite{})

func (s *apiSuite) Snap(string, string) (*snappy.RemoteSnap, error) {
	if len(s.parts) > 0 {
		return s.parts[0], s.err
	}
	return nil, s.err
}

func (s *apiSuite) FindSnaps(searchTerm, channel string) ([]*snappy.RemoteSnap, error) {
	s.searchTerm = searchTerm
	s.channel = channel

	return s.parts, s.err
}

func (s *apiSuite) muxVars(*http.Request) map[string]string {
	return s.vars
}

func (s *apiSuite) SetUpSuite(c *check.C) {
	newRemoteRepo = func() metarepo {
		return s
	}
	muxVars = s.muxVars
}

func (s *apiSuite) TearDownSuite(c *check.C) {
	newRemoteRepo = nil
	muxVars = nil
}

func (s *apiSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapLockFile), 0755), check.IsNil)
	c.Assert(os.MkdirAll(dirs.SnapSnapsDir, 0755), check.IsNil)

	s.parts = nil
	s.err = nil
	s.vars = nil
	s.overlord = &fakeOverlord{
		configs: map[string]string{},
	}
}

func (s *apiSuite) TearDownTest(c *check.C) {
	findServices = snappy.FindServices
}

func (s *apiSuite) mkInstalled(c *check.C, name, developer, version string, active bool, extraYaml string) {
	fullname := name + "." + developer
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapDataDir, fullname, version), 0755), check.IsNil)

	metadir := filepath.Join(dirs.SnapSnapsDir, fullname, version, "meta")
	c.Assert(os.MkdirAll(metadir, 0755), check.IsNil)

	guidir := filepath.Join(metadir, "gui")
	c.Assert(os.MkdirAll(guidir, 0755), check.IsNil)

	c.Check(ioutil.WriteFile(filepath.Join(guidir, "icon.svg"), []byte("yadda icon"), 0644), check.IsNil)

	content := fmt.Sprintf(`
name: %s
version: %s
%s`, name, version, extraYaml)
	c.Check(ioutil.WriteFile(filepath.Join(metadir, "snap.yaml"), []byte(content), 0644), check.IsNil)
	c.Check(ioutil.WriteFile(filepath.Join(metadir, "hashes.yaml"), []byte(nil), 0644), check.IsNil)

	if active {
		c.Assert(os.Symlink(version, filepath.Join(dirs.SnapSnapsDir, fullname, "current")), check.IsNil)
	}
}

func (s *apiSuite) mkGadget(c *check.C, store string) {
	content := []byte(fmt.Sprintf(`name: test
version: 1
type: gadget
gadget: {store: {id: %q}}
`, store))

	d := filepath.Join(dirs.SnapSnapsDir, "test")
	m := filepath.Join(d, "1", "meta")
	c.Assert(os.MkdirAll(m, 0755), check.IsNil)
	c.Assert(os.Symlink("1", filepath.Join(d, "current")), check.IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(m, "snap.yaml"), content, 0644), check.IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(m, "hashes.yaml"), []byte(nil), 0644), check.IsNil)
}

func (s *apiSuite) TestSnapInfoOneIntegration(c *check.C) {
	newTestDaemon(c)

	s.vars = map[string]string{"name": "foo", "developer": "bar"}

	// the store tells us about v2
	s.parts = []*snappy.RemoteSnap{
		&snappy.RemoteSnap{
			Pkg: remote.Snap{
				Name:    "foo",
				Version: "v2",
				// FIXME: sucks that title is descripton!
				Title:        "description",
				Developer:    "bar",
				IconURL:      "meta/gui/icon.svg",
				Type:         snap.TypeApp,
				DownloadSize: 2,
			},
		},
	}

	// we have v0 installed
	s.mkInstalled(c, "foo", "bar", "v0", false, "")
	// and v1 is current
	s.mkInstalled(c, "foo", "bar", "v1", true, "")

	rsp, ok := getSnapInfo(snapCmd, nil).(*resp)
	c.Assert(ok, check.Equals, true)

	c.Assert(rsp, check.NotNil)
	c.Assert(rsp.Result, check.FitsTypeOf, map[string]interface{}{})
	m := rsp.Result.(map[string]interface{})

	// installed_size depends on vagaries of the filesystem, just check type
	c.Check(m["installed_size"], check.FitsTypeOf, int64(0))
	delete(m, "installed_size")

	expected := &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: map[string]interface{}{
			"name":               "foo",
			"version":            "v1",
			"description":        "description",
			"developer":          "bar",
			"status":             "active",
			"icon":               "/2.0/icons/foo.bar/icon",
			"type":               string(snap.TypeApp),
			"vendor":             "",
			"download_size":      int64(2),
			"resource":           "/2.0/snaps/foo.bar",
			"update_available":   "v2",
			"rollback_available": "v0",
			"channel":            "stable",
		},
	}

	c.Check(rsp, check.DeepEquals, expected)
}

func (s *apiSuite) TestSnapInfoNotFound(c *check.C) {
	s.vars = map[string]string{"name": "foo", "developer": "bar"}
	s.err = snappy.ErrPackageNotFound

	c.Check(getSnapInfo(snapCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestSnapInfoNoneFound(c *check.C) {
	s.vars = map[string]string{"name": "foo", "developer": "bar"}

	c.Check(getSnapInfo(snapCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestSnapInfoIgnoresRemoteErrors(c *check.C) {
	s.vars = map[string]string{"name": "foo", "developer": "bar"}
	s.err = errors.New("weird")

	rsp := getSnapInfo(snapCmd, nil).Self(nil, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestSnapInfoWeirdRoute(c *check.C) {
	// can't really happen

	d := newTestDaemon(c)

	// use the wrong command to force the issue
	wrongCmd := &Command{Path: "/{what}", d: d}
	s.vars = map[string]string{"name": "foo", "developer": "bar"}
	s.parts = []*snappy.RemoteSnap{
		&snappy.RemoteSnap{
			Pkg: remote.Snap{
				Name: "foo",
			},
		},
	}
	c.Check(getSnapInfo(wrongCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusInternalServerError)
}

func (s *apiSuite) TestSnapInfoBadRoute(c *check.C) {
	// can't really happen, v2

	d := newTestDaemon(c)

	// get the route and break it
	route := d.router.Get(snapCmd.Path)
	c.Assert(route.Name("foo").GetError(), check.NotNil)

	s.vars = map[string]string{"name": "foo", "developer": "bar"}
	s.parts = []*snappy.RemoteSnap{
		&snappy.RemoteSnap{
			Pkg: remote.Snap{
				Name: "foo",
			},
		},
	}

	rsp := getSnapInfo(snapCmd, nil).Self(nil, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusInternalServerError)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, `route can't build URL .*`)
}

func (s *apiSuite) TestListIncludesAll(c *check.C) {
	// Very basic check to help stop us from not adding all the
	// commands to the command list.
	//
	// It could get fancier, looking deeper into the AST to see
	// exactly what's being defined, but it's probably not worth
	// it; this gives us most of the benefits of that, with a
	// fraction of the work.
	//
	// NOTE: there's probably a
	// better/easier way of doing this (patches welcome)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "api.go", nil, 0)
	if err != nil {
		panic(err)
	}

	found := 0

	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.ValueSpec:
			found += len(v.Values)
			return false
		}
		return true
	})

	exceptions := []string{ // keep sorted, for scanning ease
		"apiCompatLevel",
		"api",
		"findServices",
		"maxReadBuflen",
		"muxVars",
		"newRemoteRepo",
		"newSnap",
		"pkgActionDispatch",
		// snapInstruction vars:
		"snappyInstall",
		"getConfigurator",
	}
	c.Check(found, check.Equals, len(api)+len(exceptions),
		check.Commentf(`At a glance it looks like you've not added all the Commands defined in api to the api list. If that is not the case, please add the exception to the "exceptions" list in this test.`))
}

func (s *apiSuite) TestRootCmd(c *check.C) {
	// check it only does GET
	c.Check(rootCmd.PUT, check.IsNil)
	c.Check(rootCmd.POST, check.IsNil)
	c.Check(rootCmd.DELETE, check.IsNil)
	c.Assert(rootCmd.GET, check.NotNil)

	rec := httptest.NewRecorder()
	c.Check(rootCmd.Path, check.Equals, "/")

	rootCmd.GET(rootCmd, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/json")

	expected := []interface{}{"TBD"}
	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *apiSuite) mkrelease() {
	// set up release
	release.Override(release.Release{
		Flavor:  "flavor",
		Series:  "release",
		Channel: "channel",
	})
}

func (s *apiSuite) TestSysInfo(c *check.C) {
	// check it only does GET
	c.Check(sysInfoCmd.PUT, check.IsNil)
	c.Check(sysInfoCmd.POST, check.IsNil)
	c.Check(sysInfoCmd.DELETE, check.IsNil)
	c.Assert(sysInfoCmd.GET, check.NotNil)

	rec := httptest.NewRecorder()
	c.Check(sysInfoCmd.Path, check.Equals, "/2.0/system-info")

	s.mkrelease()

	sysInfoCmd.GET(sysInfoCmd, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/json")

	expected := map[string]interface{}{
		"flavor":          "flavor",
		"release":         "release",
		"default_channel": "channel",
		"api_compat":      apiCompatLevel,
	}
	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *apiSuite) TestSysInfoStore(c *check.C) {
	rec := httptest.NewRecorder()
	c.Check(sysInfoCmd.Path, check.Equals, "/2.0/system-info")

	s.mkrelease()
	s.mkGadget(c, "some-store")

	sysInfoCmd.GET(sysInfoCmd, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)

	expected := map[string]interface{}{
		"flavor":          "flavor",
		"release":         "release",
		"default_channel": "channel",
		"api_compat":      apiCompatLevel,
		"store":           "some-store",
	}
	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *apiSuite) TestSnapsInfoOnePerIntegration(c *check.C) {
	req, err := http.NewRequest("GET", "/2.0/snaps", nil)
	c.Assert(err, check.IsNil)

	ddirs := [][3]string{{"foo", "bar", "v1"}, {"bar", "baz", "v2"}, {"baz", "qux", "v3"}, {"qux", "mip", "v4"}}

	for _, d := range ddirs {
		s.mkInstalled(c, d[0], d[1], d[2], false, "")
	}

	rsp, ok := getSnapsInfo(snapsCmd, req).(*resp)
	c.Assert(ok, check.Equals, true)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Result, check.NotNil)

	meta, ok := rsp.Result.(map[string]interface{})
	c.Assert(ok, check.Equals, true)
	c.Assert(meta, check.NotNil)
	c.Check(meta["paging"], check.DeepEquals, map[string]interface{}{"pages": 1, "page": 1, "count": len(ddirs)})

	snaps, ok := meta["snaps"].(map[string]map[string]interface{})
	c.Assert(ok, check.Equals, true)
	c.Check(snaps, check.NotNil)
	c.Check(snaps, check.HasLen, len(ddirs))

	for i := range ddirs {
		name, developer, version := ddirs[i][0], ddirs[i][1], ddirs[i][2]
		qn := name + "." + developer
		got := snaps[qn]
		c.Assert(got, check.NotNil, check.Commentf(qn))
		c.Check(got["name"], check.Equals, name)
		c.Check(got["version"], check.Equals, version)
		c.Check(got["developer"], check.Equals, developer)
	}
}

func (s *apiSuite) TestSnapsInfoOnlyLocal(c *check.C) {
	s.parts = []*snappy.RemoteSnap{
		&snappy.RemoteSnap{
			Pkg: remote.Snap{
				Name:      "store",
				Developer: "foo",
			},
		},
	}
	s.mkInstalled(c, "local", "foo", "v1", true, "")

	req, err := http.NewRequest("GET", "/2.0/snaps?sources=local", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["sources"], check.DeepEquals, []string{"local"})

	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps["local.foo"], check.NotNil)
}

func (s *apiSuite) TestSnapsInfoOnlyStore(c *check.C) {
	s.parts = []*snappy.RemoteSnap{
		&snappy.RemoteSnap{
			Pkg: remote.Snap{
				Name:      "store",
				Developer: "foo",
			},
		},
	}
	s.mkInstalled(c, "local", "foo", "v1", true, "")

	req, err := http.NewRequest("GET", "/2.0/snaps?sources=store", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["sources"], check.DeepEquals, []string{"store"})

	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps["store.foo"], check.NotNil)
}

func (s *apiSuite) TestSnapsInfoLocalAndStore(c *check.C) {
	s.parts = []*snappy.RemoteSnap{
		&snappy.RemoteSnap{
			Pkg: remote.Snap{
				Name:      "remote",
				Developer: "foo",
			},
		},
	}
	s.mkInstalled(c, "local", "foo", "v1", true, "")

	req, err := http.NewRequest("GET", "/2.0/snaps?sources=local,store", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["sources"], check.DeepEquals, []string{"local", "store"})

	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 2)
}

func (s *apiSuite) TestSnapsInfoDefaultSources(c *check.C) {
	s.parts = []*snappy.RemoteSnap{
		&snappy.RemoteSnap{
			Pkg: remote.Snap{
				Name:      "remote",
				Developer: "foo",
			},
		},
	}
	s.mkInstalled(c, "local", "foo", "v1", true, "")

	req, err := http.NewRequest("GET", "/2.0/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["sources"], check.DeepEquals, []string{"local", "store"})
}

func (s *apiSuite) TestSnapsInfoUnknownSource(c *check.C) {
	s.parts = []*snappy.RemoteSnap{
		&snappy.RemoteSnap{
			Pkg: remote.Snap{
				Name:      "remote",
				Developer: "foo",
			},
		},
	}
	s.mkInstalled(c, "local", "foo", "v1", true, "")

	req, err := http.NewRequest("GET", "/2.0/snaps?sources=unknown", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["sources"], check.HasLen, 0)

	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 0)
}

func (s *apiSuite) TestSnapsInfoFilterLocal(c *check.C) {
	s.parts = nil
	s.mkInstalled(c, "foo", "foo", "v1", true, "")
	s.mkInstalled(c, "bar", "bar", "v1", true, "")

	req, err := http.NewRequest("GET", "/2.0/snaps?q=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["snaps"], check.NotNil)

	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps["foo.foo"], check.NotNil)
}

func (s *apiSuite) TestSnapsInfoFilterRemote(c *check.C) {
	s.parts = nil

	req, err := http.NewRequest("GET", "/2.0/snaps?q=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	c.Check(s.searchTerm, check.Equals, "foo")

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["snaps"], check.NotNil)
}

func (s *apiSuite) TestSnapsInfoAppsOnly(c *check.C) {
	s.mkInstalled(c, "app", "foo", "v1", true, "type: app")
	s.mkInstalled(c, "framework", "foo", "v1", true, "type: framework")

	req, err := http.NewRequest("GET", "/2.0/snaps?types=app", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps["app.foo"], check.NotNil)
}

func (s *apiSuite) TestSnapsInfoFrameworksOnly(c *check.C) {
	s.mkInstalled(c, "app", "foo", "v1", true, "type: app")
	s.mkInstalled(c, "framework", "foo", "v1", true, "type: framework")

	req, err := http.NewRequest("GET", "/2.0/snaps?types=framework", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps["framework.foo"], check.NotNil)
}

func (s *apiSuite) TestSnapsInfoAppsAndFrameworks(c *check.C) {
	s.mkInstalled(c, "app", "foo", "v1", true, "type: app")
	s.mkInstalled(c, "framework", "foo", "v1", true, "type: framework")

	req, err := http.NewRequest("GET", "/2.0/snaps?types=app,framework", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 2)
}

func (s *apiSuite) TestDeleteOpNotFound(c *check.C) {
	s.vars = map[string]string{"uuid": "42"}
	rsp := deleteOp(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestDeleteOpStillRunning(c *check.C) {
	d := newTestDaemon(c)

	d.tasks["42"] = &Task{}
	s.vars = map[string]string{"uuid": "42"}
	rsp := deleteOp(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
}

func (s *apiSuite) TestDeleteOp(c *check.C) {
	d := newTestDaemon(c)

	task := &Task{}
	d.tasks["42"] = task
	task.tomb.Kill(nil)
	s.vars = map[string]string{"uuid": "42"}
	rsp := deleteOp(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
}

func (s *apiSuite) TestGetOpInfoIntegration(c *check.C) {
	d := newTestDaemon(c)

	s.vars = map[string]string{"uuid": "42"}
	rsp := getOpInfo(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)

	ch := make(chan struct{})

	t := d.AddTask(func() interface{} {
		ch <- struct{}{}
		return "hello"
	})

	id := t.UUID()
	s.vars = map[string]string{"uuid": id}

	rsp = getOpInfo(operationCmd, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.DeepEquals, map[string]interface{}{
		"resource":   "/2.0/operations/" + id,
		"status":     TaskRunning,
		"may_cancel": false,
		"created_at": FormatTime(t.CreatedAt()),
		"updated_at": FormatTime(t.UpdatedAt()),
		"output":     nil,
	})
	tf1 := t.UpdatedAt().UTC().UnixNano()

	<-ch
	time.Sleep(time.Millisecond)

	rsp = getOpInfo(operationCmd, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.DeepEquals, map[string]interface{}{
		"resource":   "/2.0/operations/" + id,
		"status":     TaskSucceeded,
		"may_cancel": false,
		"created_at": FormatTime(t.CreatedAt()),
		"updated_at": FormatTime(t.UpdatedAt()),
		"output":     "hello",
	})

	tf2 := t.UpdatedAt().UTC().UnixNano()

	c.Check(tf1 < tf2, check.Equals, true)
}

func (s *apiSuite) TestPostSnapBadRequest(c *check.C) {
	s.vars = map[string]string{"uuid": "42"}
	rsp := getOpInfo(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)

	buf := bytes.NewBufferString(`hello`)
	req, err := http.NewRequest("POST", "/2.0/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp = postSnap(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostSnapBadAction(c *check.C) {
	s.vars = map[string]string{"uuid": "42"}
	c.Check(getOpInfo(operationCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)

	buf := bytes.NewBufferString(`{"action": "potato"}`)
	req, err := http.NewRequest("POST", "/2.0/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostSnap(c *check.C) {
	d := newTestDaemon(c)

	s.vars = map[string]string{"uuid": "42"}
	c.Check(getOpInfo(operationCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)

	ch := make(chan struct{})

	pkgActionDispatch = func(*snapInstruction) func() interface{} {
		return func() interface{} {
			ch <- struct{}{}
			return "hi"
		}
	}
	defer func() {
		pkgActionDispatch = pkgActionDispatchImpl
	}()

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/2.0/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)
	m := rsp.Result.(map[string]interface{})
	c.Assert(m["resource"], check.Matches, "/2.0/operations/.*")

	uuid := m["resource"].(string)[16:]

	task := d.GetTask(uuid)
	c.Assert(task, check.NotNil)

	c.Check(task.State(), check.Equals, TaskRunning)

	<-ch
	time.Sleep(time.Millisecond)

	task = d.GetTask(uuid)
	c.Assert(task, check.NotNil)
	c.Check(task.State(), check.Equals, TaskSucceeded)
	c.Check(task.Output(), check.Equals, "hi")
}

func (s *apiSuite) TestPostSnapDispatch(c *check.C) {
	inst := &snapInstruction{}

	type T struct {
		s string
		m func() interface{}
	}

	actions := []T{
		{"install", inst.install},
		{"update", inst.update},
		{"remove", inst.remove},
		{"purge", inst.purge},
		{"rollback", inst.rollback},
		{"xyzzy", nil},
	}

	for _, action := range actions {
		inst.Action = action.s
		// do you feel dirty yet?
		c.Check(fmt.Sprintf("%p", action.m), check.Equals, fmt.Sprintf("%p", inst.dispatch()))
	}
}

type fakeOverlord struct {
	configs map[string]string
}

func (o *fakeOverlord) Configure(s *snappy.Snap, c []byte) ([]byte, error) {
	if len(c) > 0 {
		o.configs[s.Name()] = string(c)
	}
	config, ok := o.configs[s.Name()]
	if !ok {
		return nil, fmt.Errorf("no config for %q", s.Name())
	}
	return []byte(config), nil
}

func (s *apiSuite) TestSnapGetConfig(c *check.C) {
	req, err := http.NewRequest("GET", "/2.0/snaps/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	getConfigurator = func() configurator {
		return s.overlord
	}

	configStr := "some: config"
	s.overlord.configs["foo"] = configStr
	s.vars = map[string]string{"name": "foo", "developer": "bar"}
	s.mkInstalled(c, "foo", "bar", "v1", true, "")

	rsp := snapConfig(snapsCmd, req).(*resp)

	c.Check(rsp, check.DeepEquals, &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: configStr,
	})
}

func (s *apiSuite) TestSnapGetConfigMissing(c *check.C) {
	s.vars = map[string]string{"name": "foo", "developer": "bar"}

	req, err := http.NewRequest("GET", "/2.0/snaps/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := snapConfig(snapsCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestSnapGetConfigInactive(c *check.C) {
	s.vars = map[string]string{"name": "foo", "developer": "bar"}

	s.mkInstalled(c, "foo", "bar", "v1", false, "")

	req, err := http.NewRequest("GET", "/2.0/snaps/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := snapConfig(snapsCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
}

func (s *apiSuite) TestSnapGetConfigNoConfig(c *check.C) {
	s.vars = map[string]string{"name": "foo", "developer": "bar"}
	getConfigurator = func() configurator {
		return s.overlord
	}

	s.mkInstalled(c, "foo", "bar", "v1", true, "")
	req, err := http.NewRequest("GET", "/2.0/snaps/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := snapConfig(snapsCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusInternalServerError)
}

func (s *apiSuite) TestSnapPutConfig(c *check.C) {
	newConfigStr := "some other config"
	req, err := http.NewRequest("PUT", "/2.0/snaps/foo.bar/config", bytes.NewBufferString(newConfigStr))
	c.Assert(err, check.IsNil)

	//configStr := "some: config"
	getConfigurator = func() configurator {
		return s.overlord
	}

	s.vars = map[string]string{"name": "foo", "developer": "bar"}
	s.mkInstalled(c, "foo", "bar", "v1", true, "")

	rsp := snapConfig(snapConfigCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp, check.DeepEquals, &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: newConfigStr,
	})
}

func (s *apiSuite) TestSnapPutConfigMissing(c *check.C) {
	s.vars = map[string]string{"name": "foo", "developer": "bar"}

	req, err := http.NewRequest("PUT", "/2.0/snaps/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := snapConfig(snapsCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestSnapPutConfigInactive(c *check.C) {
	s.vars = map[string]string{"name": "foo", "developer": "bar"}

	s.mkInstalled(c, "foo", "bar", "v1", false, "")

	req, err := http.NewRequest("PUT", "/2.0/snaps/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := snapConfig(snapsCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
}

func (s *apiSuite) TestSnapPutConfigNoConfig(c *check.C) {
	s.vars = map[string]string{"name": "foo", "developer": "bar"}

	s.mkInstalled(c, "foo", "bar", "v1", true, "")

	req, err := http.NewRequest("PUT", "/2.0/snaps/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := snapConfig(snapsCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusInternalServerError)
}

func (s *apiSuite) TestSnapServiceGet(c *check.C) {
	findServices = func(string, string, progress.Meter) (snappy.ServiceActor, error) {
		return &tSA{ssout: []*snappy.PackageServiceStatus{{AppName: "svc"}}}, nil
	}

	req, err := http.NewRequest("GET", "/2.0/snaps/foo.bar/services", nil)
	c.Assert(err, check.IsNil)

	s.mkInstalled(c, "foo", "bar", "v1", true, `apps:
 svc:
  daemon: forking
`)
	s.vars = map[string]string{"name": "foo", "developer": "bar"} // NB: no service specified

	rsp := snapService(snapSvcsCmd, req).(*resp)
	c.Assert(rsp, check.NotNil)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)

	m := rsp.Result.(map[string]*appDesc)
	c.Assert(m["svc"], check.FitsTypeOf, new(appDesc))
	c.Check(m["svc"].Op, check.Equals, "status")
	c.Check(m["svc"].Spec, check.DeepEquals, &snappy.AppYaml{Name: "svc", Daemon: "forking", StopTimeout: timeout.DefaultTimeout})
	c.Check(m["svc"].Status, check.DeepEquals, &snappy.PackageServiceStatus{AppName: "svc"})
}

func (s *apiSuite) TestSnapServicePut(c *check.C) {
	findServices = func(string, string, progress.Meter) (snappy.ServiceActor, error) {
		return &tSA{ssout: []*snappy.PackageServiceStatus{{AppName: "svc"}}}, nil
	}

	buf := bytes.NewBufferString(`{"action": "stop"}`)
	req, err := http.NewRequest("PUT", "/2.0/snaps/foo.bar/services", buf)
	c.Assert(err, check.IsNil)

	s.mkInstalled(c, "foo", "bar", "v1", true, `apps:
 svc:
  command: svc
  daemon: forking
`)
	s.vars = map[string]string{"name": "foo", "developer": "bar"} // NB: no service specified

	rsp := snapService(snapSvcsCmd, req).(*resp)
	c.Assert(rsp, check.NotNil)
	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)
	c.Check(rsp.Status, check.Equals, http.StatusAccepted)
}

func (s *apiSuite) TestSideloadSnap(c *check.C) {
	// try a direct upload, with no x-allow-unsigned header
	s.sideloadCheck(c, "xyzzy", false, nil)
	// try a direct upload *with* an x-allow-unsigned header
	s.sideloadCheck(c, "xyzzy", true, map[string]string{"X-Allow-Unsigned": "Very Yes"})
	// try a multipart/form-data upload without allow-unsigned
	s.sideloadCheck(c, "----hello--\r\nContent-Disposition: form-data; name=\"x\"; filename=\"x\"\r\n\r\nxyzzy\r\n----hello----\r\n", false, map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"})
	// and one *with* allow-unsigned
	s.sideloadCheck(c, "----hello--\r\nContent-Disposition: form-data; name=\"unsigned-ok\"\r\n\r\n----hello--\r\nContent-Disposition: form-data; name=\"x\"; filename=\"x\"\r\n\r\nxyzzy\r\n----hello----\r\n", false, map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"})
}

func (s *apiSuite) sideloadCheck(c *check.C, content string, unsignedExpected bool, head map[string]string) {
	ch := make(chan struct{})
	tmpfile, err := ioutil.TempFile("", "test-")
	c.Assert(err, check.IsNil)
	_, err = tmpfile.WriteString(content)
	c.Check(err, check.IsNil)
	_, err = tmpfile.Seek(0, 0)
	c.Check(err, check.IsNil)

	// setup done

	newSnap = func(fn string, developer string, unauthOk bool) (*snappy.SnapFile, error) {
		c.Check(developer, check.Equals, snappy.SideloadedDeveloper)
		c.Check(unauthOk, check.Equals, unsignedExpected)

		bs, err := ioutil.ReadFile(fn)
		c.Check(err, check.IsNil)
		c.Check(string(bs), check.Equals, "xyzzy")

		ch <- struct{}{}

		return &snappy.SnapFile{}, nil
	}
	defer func() { newSnap = newSnapImpl }()

	req, err := http.NewRequest("POST", "/2.0/snaps", tmpfile)
	c.Assert(err, check.IsNil)
	for k, v := range head {
		req.Header.Set(k, v)
	}

	rsp := sideloadSnap(snapsCmd, req).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)

	<-ch
}

func (s *apiSuite) TestServiceLogs(c *check.C) {
	log := systemd.Log{
		"__REALTIME_TIMESTAMP": "42",
		"MESSAGE":              "hi",
	}

	findServices = func(string, string, progress.Meter) (snappy.ServiceActor, error) {
		return &tSA{lgout: []systemd.Log{log}}, nil
	}

	req, err := http.NewRequest("GET", "/2.0/snaps/foo.bar/services/baz/logs", nil)
	c.Assert(err, check.IsNil)

	rsp := getLogs(snapSvcLogsCmd, req).(*resp)
	c.Assert(rsp, check.DeepEquals, &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: []map[string]interface{}{{"message": "hi", "timestamp": "1970-01-01T00:00:00.000042Z", "raw": log}},
	})
}

func (s *apiSuite) TestAppIconGet(c *check.C) {
	// have an active foo.bar in the system
	s.mkInstalled(c, "foo", "bar", "v1", true, "")

	// have an icon for it in the package itself
	iconfile := filepath.Join(dirs.SnapSnapsDir, "foo.bar", "v1", "meta", "gui", "icon.ick")
	c.Assert(os.MkdirAll(filepath.Dir(iconfile), 0755), check.IsNil)
	c.Check(ioutil.WriteFile(iconfile, []byte("ick"), 0644), check.IsNil)

	s.vars = map[string]string{"name": "foo", "developer": "bar"}
	req, err := http.NewRequest("GET", "/2.0/icons/foo.bar/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Body.String(), check.Equals, "ick")
}

func (s *apiSuite) TestAppIconGetInactive(c *check.C) {
	// have an *in*active foo.bar in the system
	s.mkInstalled(c, "foo", "bar", "v1", false, "")

	// have an icon for it in the package itself

	iconfile := filepath.Join(dirs.SnapSnapsDir, "foo.bar", "v1", "meta", "gui", "icon.ick")
	c.Assert(os.MkdirAll(filepath.Dir(iconfile), 0755), check.IsNil)
	c.Check(ioutil.WriteFile(iconfile, []byte("ick"), 0644), check.IsNil)

	s.vars = map[string]string{"name": "foo", "developer": "bar"}
	req, err := http.NewRequest("GET", "/2.0/icons/foo.bar/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Body.String(), check.Equals, "ick")
}

func (s *apiSuite) TestAppIconGetNoIcon(c *check.C) {
	// have an *in*active foo.bar in the system
	s.mkInstalled(c, "foo", "bar", "v1", true, "")

	// NO ICON!
	err := os.RemoveAll(filepath.Join(dirs.SnapSnapsDir, "foo.bar", "v1", "meta", "gui", "icon.svg"))
	c.Assert(err, check.IsNil)

	s.vars = map[string]string{"name": "foo", "developer": "bar"}
	req, err := http.NewRequest("GET", "/2.0/icons/foo.bar/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code/100, check.Equals, 4)
}

func (s *apiSuite) TestAppIconGetNoApp(c *check.C) {
	s.vars = map[string]string{"name": "foo", "developer": "bar"}
	req, err := http.NewRequest("GET", "/2.0/icons/foo.bar/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 404)
}

func (s *apiSuite) TestPkgInstructionAgreedOK(c *check.C) {
	lic := &licenseData{
		Intro:   "hi",
		License: "Void where empty",
		Agreed:  true,
	}

	inst := &snapInstruction{License: lic}

	c.Check(inst.Agreed(lic.Intro, lic.License), check.Equals, true)
}

func (s *apiSuite) TestPkgInstructionAgreedNOK(c *check.C) {
	lic := &licenseData{
		Intro:   "hi",
		License: "Void where empty",
		Agreed:  false,
	}

	inst := &snapInstruction{License: lic}

	c.Check(inst.Agreed(lic.Intro, lic.License), check.Equals, false)
}

func (s *apiSuite) TestPkgInstructionMismatch(c *check.C) {
	lic := &licenseData{
		Intro:   "hi",
		License: "Void where empty",
		Agreed:  true,
	}

	inst := &snapInstruction{License: lic}

	c.Check(inst.Agreed("blah", "yak yak"), check.Equals, false)
}

func (s *apiSuite) TestInstall(c *check.C) {
	orig := snappyInstall
	defer func() { snappyInstall = orig }()

	calledFlags := snappy.InstallFlags(42)

	snappyInstall = func(name, channel string, flags snappy.InstallFlags, meter progress.Meter) (string, error) {
		calledFlags = flags

		return "", nil
	}

	inst := &snapInstruction{
		Action: "install",
	}

	err := inst.dispatch()()

	c.Check(calledFlags, check.Equals, snappy.DoInstallGC)
	c.Check(err, check.IsNil)
}

func (s *apiSuite) TestInstallLeaveOld(c *check.C) {
	orig := snappyInstall
	defer func() { snappyInstall = orig }()

	calledFlags := snappy.InstallFlags(42)

	snappyInstall = func(name, channel string, flags snappy.InstallFlags, meter progress.Meter) (string, error) {
		calledFlags = flags

		return "", nil
	}

	inst := &snapInstruction{
		Action:   "install",
		LeaveOld: true,
	}

	err := inst.dispatch()()

	c.Check(calledFlags, check.Equals, snappy.InstallFlags(0))
	c.Check(err, check.IsNil)
}

func (s *apiSuite) TestInstallLicensed(c *check.C) {
	orig := snappyInstall
	defer func() { snappyInstall = orig }()

	snappyInstall = func(name, channel string, flags snappy.InstallFlags, meter progress.Meter) (string, error) {
		if meter.Agreed("hi", "yak yak") {
			return "", nil
		}

		return "", snappy.ErrLicenseNotAccepted
	}

	inst := &snapInstruction{
		Action: "install",
	}

	lic, ok := inst.dispatch()().(*licenseData)
	c.Assert(ok, check.Equals, true)
	c.Check(lic, check.ErrorMatches, "license agreement required")
	c.Check(lic.Intro, check.Equals, "hi")
	c.Check(lic.License, check.Equals, "yak yak")
	c.Check(lic.Agreed, check.Equals, false)

	// now, pass it in
	inst.License = lic
	inst.License.Agreed = true

	err := inst.dispatch()()
	c.Check(err, check.IsNil)
}

func (s *apiSuite) TestInstallLicensedIntegration(c *check.C) {
	d := newTestDaemon(c)

	orig := snappyInstall
	defer func() { snappyInstall = orig }()

	snappyInstall = func(name, channel string, flags snappy.InstallFlags, meter progress.Meter) (string, error) {
		if meter.Agreed("hi", "yak yak") {
			return "", nil
		}

		return "", snappy.ErrLicenseNotAccepted
	}

	req, err := http.NewRequest("POST", "/2.0/snaps/foo.bar", strings.NewReader(`{"action": "install"}`))
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"name": "foo", "developer": "bar"}

	res := postSnap(snapCmd, req).(*resp).Result.(map[string]interface{})
	task := d.tasks[res["resource"].(string)[16:]]
	c.Check(task, check.NotNil)

	task.tomb.Wait()
	c.Check(task.State(), check.Equals, TaskFailed)
	errRes := task.output.(errorResult)
	c.Check(errRes.Message, check.Equals, "license agreement required")
	c.Check(errRes.Kind, check.Equals, errorKindLicenseRequired)
	c.Check(errRes.Value, check.DeepEquals, &licenseData{
		Intro:   "hi",
		License: "yak yak",
	})

	req, err = http.NewRequest("POST", "/2.0/snaps/foo.bar", strings.NewReader(`{"action": "install", "license": {"intro": "hi", "license": "yak yak", "agreed": true}}`))
	c.Assert(err, check.IsNil)

	res = postSnap(snapCmd, req).(*resp).Result.(map[string]interface{})
	task = d.tasks[res["resource"].(string)[16:]]
	c.Check(task, check.NotNil)

	task.tomb.Wait()
	c.Check(task.State(), check.Equals, TaskSucceeded)
}

// Tests for GET /2.0/interfaces

func (s *apiSuite) TestGetPlugs(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(&interfaces.Plug{Snap: "producer", Name: "plug", Interface: "interface", Label: "label"})
	d.interfaces.AddSlot(&interfaces.Slot{Snap: "consumer", Name: "slot", Interface: "interface", Label: "label"})
	d.interfaces.Connect("producer", "plug", "consumer", "slot")
	req, err := http.NewRequest("GET", "/2.0/interfaces", nil)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.GET(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"plugs": []interface{}{
				map[string]interface{}{
					"snap":      "producer",
					"plug":      "plug",
					"interface": "interface",
					"label":     "label",
					"connections": []interface{}{
						map[string]interface{}{"snap": "consumer", "slot": "slot"},
					},
				},
			},
			"slots": []interface{}{
				map[string]interface{}{
					"snap":      "consumer",
					"slot":      "slot",
					"interface": "interface",
					"label":     "label",
					"connections": []interface{}{
						map[string]interface{}{"snap": "producer", "plug": "plug"},
					},
				},
			},
		},
		"status":      "OK",
		"status_code": 200.0,
		"type":        "sync",
	})
}

// Test for POST /2.0/interfaces

func (s *apiSuite) TestConnectPlugSuccess(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(&interfaces.Plug{Snap: "producer", Name: "plug", Interface: "interface"})
	d.interfaces.AddSlot(&interfaces.Slot{Snap: "consumer", Name: "slot", Interface: "interface"})
	action := &interfaceAction{
		Action: "connect",
		Plugs:  []interfaces.Plug{{Snap: "producer", Name: "plug"}},
		Slots:  []interfaces.Slot{{Snap: "consumer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result":      nil,
		"status":      "OK",
		"status_code": 200.0,
		"type":        "sync",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Plugs: []*interfaces.Plug{{
			Snap:        "producer",
			Name:        "plug",
			Interface:   "interface",
			Connections: []interfaces.SlotRef{{Snap: "consumer", Name: "slot"}},
		}},
		Slots: []*interfaces.Slot{{
			Snap:        "consumer",
			Name:        "slot",
			Interface:   "interface",
			Connections: []interfaces.PlugRef{{Snap: "producer", Name: "plug"}},
		}},
	})
}

func (s *apiSuite) TestConnectPlugFailureInterfaceMismatch(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "other-interface"})
	d.interfaces.AddPlug(&interfaces.Plug{Snap: "producer", Name: "plug", Interface: "interface"})
	d.interfaces.AddSlot(&interfaces.Slot{Snap: "consumer", Name: "slot", Interface: "other-interface"})
	action := &interfaceAction{
		Action: "connect",
		Plugs:  []interfaces.Plug{{Snap: "producer", Name: "plug"}},
		Slots:  []interfaces.Slot{{Snap: "consumer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": `cannot connect plug "producer:plug" (interface "interface") to "consumer:slot" (interface "other-interface")`,
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Plugs: []*interfaces.Plug{{
			Snap:      "producer",
			Name:      "plug",
			Interface: "interface",
		}},
		Slots: []*interfaces.Slot{{
			Snap:      "consumer",
			Name:      "slot",
			Interface: "other-interface",
		}},
	})
}

func (s *apiSuite) TestConnectPlugFailureNoSuchPlug(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddSlot(&interfaces.Slot{Snap: "consumer", Name: "slot", Interface: "interface"})
	action := &interfaceAction{
		Action: "connect",
		Plugs:  []interfaces.Plug{{Snap: "producer", Name: "plug"}},
		Slots:  []interfaces.Slot{{Snap: "consumer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": `cannot connect plug "plug" from snap "producer", no such plug`,
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Slots: []*interfaces.Slot{{
			Snap:      "consumer",
			Name:      "slot",
			Interface: "interface",
		}},
	})
}

func (s *apiSuite) TestConnectPlugFailureNoSuchSlot(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(&interfaces.Plug{Snap: "producer", Name: "plug", Interface: "interface"})
	action := &interfaceAction{
		Action: "connect",
		Plugs:  []interfaces.Plug{{Snap: "producer", Name: "plug"}},
		Slots:  []interfaces.Slot{{Snap: "consumer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": `cannot connect plug to slot "slot" from snap "consumer", no such slot`,
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Plugs: []*interfaces.Plug{{
			Snap:      "producer",
			Name:      "plug",
			Interface: "interface",
		}},
	})
}

func (s *apiSuite) TestDisconnectPlugSuccess(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(&interfaces.Plug{Snap: "producer", Name: "plug", Interface: "interface"})
	d.interfaces.AddSlot(&interfaces.Slot{Snap: "consumer", Name: "slot", Interface: "interface"})
	d.interfaces.Connect("producer", "plug", "consumer", "slot")
	action := &interfaceAction{
		Action: "disconnect",
		Plugs:  []interfaces.Plug{{Snap: "producer", Name: "plug"}},
		Slots:  []interfaces.Slot{{Snap: "consumer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result":      nil,
		"status":      "OK",
		"status_code": 200.0,
		"type":        "sync",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Plugs: []*interfaces.Plug{{
			Snap:      "producer",
			Name:      "plug",
			Interface: "interface",
		}},
		Slots: []*interfaces.Slot{{
			Snap:      "consumer",
			Name:      "slot",
			Interface: "interface",
		}},
	})
}

func (s *apiSuite) TestDisconnectPlugFailureNoSuchPlug(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddSlot(&interfaces.Slot{Snap: "consumer", Name: "slot", Interface: "interface"})
	action := &interfaceAction{
		Action: "disconnect",
		Plugs:  []interfaces.Plug{{Snap: "producer", Name: "plug"}},
		Slots:  []interfaces.Slot{{Snap: "consumer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": `cannot disconnect plug "plug" from snap "producer", no such plug`,
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Slots: []*interfaces.Slot{{
			Snap:      "consumer",
			Name:      "slot",
			Interface: "interface",
		}},
	})
}

func (s *apiSuite) TestDisconnectPlugFailureNoSuchSlot(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(&interfaces.Plug{Snap: "producer", Name: "plug", Interface: "interface"})
	action := &interfaceAction{
		Action: "disconnect",
		Plugs:  []interfaces.Plug{{Snap: "producer", Name: "plug"}},
		Slots:  []interfaces.Slot{{Snap: "consumer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": `cannot disconnect plug from slot "slot" from snap "consumer", no such slot`,
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Plugs: []*interfaces.Plug{{
			Snap:      "producer",
			Name:      "plug",
			Interface: "interface",
		}},
	})
}

func (s *apiSuite) TestDisconnectPlugFailureNotConnected(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(&interfaces.Plug{Snap: "producer", Name: "plug", Interface: "interface"})
	d.interfaces.AddSlot(&interfaces.Slot{Snap: "consumer", Name: "slot", Interface: "interface"})
	action := &interfaceAction{
		Action: "disconnect",
		Plugs:  []interfaces.Plug{{Snap: "producer", Name: "plug"}},
		Slots:  []interfaces.Slot{{Snap: "consumer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": `cannot disconnect plug "plug" from snap "producer" from slot "slot" from snap "consumer", it is not connected`,
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Plugs: []*interfaces.Plug{{
			Snap:      "producer",
			Name:      "plug",
			Interface: "interface",
		}},
		Slots: []*interfaces.Slot{{
			Snap:      "consumer",
			Name:      "slot",
			Interface: "interface",
		}},
	})
}

func (s *apiSuite) TestAddPlugSuccess(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	action := &interfaceAction{
		Action: "add-plug",
		Plugs: []interfaces.Plug{{
			Snap:      "snap",
			Name:      "plug",
			Label:     "label",
			Interface: "interface",
			Attrs:     map[string]interface{}{"key": "value"},
			Apps:      []string{"app"},
		}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 201)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result":      nil,
		"status":      "Created",
		"status_code": 201.0,
		"type":        "sync",
	})
	c.Check(d.interfaces.Plug("snap", "plug"), check.DeepEquals, &action.Plugs[0])
}

func (s *apiSuite) TestAddPlugDisabled(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{
		InterfaceName: "interface",
	})
	d.enableInternalInterfaceActions = false
	action := &interfaceAction{
		Action: "add-plug",
		Plugs: []interfaces.Plug{{
			Snap:      "producer",
			Name:      "plug",
			Label:     "label",
			Interface: "interface",
			Attrs:     map[string]interface{}{"key": "value"},
			Apps:      []string{"app"},
		}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "internal interface actions are disabled",
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Check(d.interfaces.Plug("producer", "plug"), check.IsNil)
}

func (s *apiSuite) TestAddPlugFailure(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{
		InterfaceName: "interface",
		SanitizePlugCallback: func(plug *interfaces.Plug) error {
			return fmt.Errorf("required attribute missing")
		},
	})
	action := &interfaceAction{
		Action: "add-plug",
		Plugs: []interfaces.Plug{{
			Snap:      "snap",
			Name:      "plug",
			Label:     "label",
			Interface: "interface",
			Attrs:     map[string]interface{}{"key": "value"},
			Apps:      []string{"app"},
		}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "cannot add plug: required attribute missing",
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Check(d.interfaces.Plug("snap", "name"), check.IsNil)
}

func (s *apiSuite) TestRemovePlugSuccess(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(&interfaces.Plug{Snap: "producer", Name: "plug", Interface: "interface"})
	action := &interfaceAction{
		Action: "remove-plug",
		Plugs:  []interfaces.Plug{{Snap: "producer", Name: "plug"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result":      nil,
		"status":      "OK",
		"status_code": 200.0,
		"type":        "sync",
	})
	c.Check(d.interfaces.Plug("snap", "name"), check.IsNil)
}

func (s *apiSuite) TestRemovePlugDisabled(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(&interfaces.Plug{Snap: "producer", Name: "plug", Interface: "interface"})
	d.enableInternalInterfaceActions = false
	action := &interfaceAction{
		Action: "remove-plug",
		Plugs:  []interfaces.Plug{{Snap: "producer", Name: "plug"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "internal interface actions are disabled",
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Check(d.interfaces.Plug("producer", "plug"), check.Not(check.IsNil))
}

func (s *apiSuite) TestRemovePlugFailure(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(&interfaces.Plug{Snap: "producer", Name: "plug", Interface: "interface"})
	d.interfaces.AddSlot(&interfaces.Slot{Snap: "consumer", Name: "slot", Interface: "interface"})
	d.interfaces.Connect("producer", "plug", "consumer", "slot")
	action := &interfaceAction{
		Action: "remove-plug",
		Plugs:  []interfaces.Plug{{Snap: "producer", Name: "plug"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": `cannot remove plug "plug" from snap "producer", it is still connected`,
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Check(d.interfaces.Plug("producer", "plug"), check.Not(check.IsNil))
}

func (s *apiSuite) TestAddSlotSuccess(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	action := &interfaceAction{
		Action: "add-slot",
		Slots: []interfaces.Slot{{
			Snap:      "snap",
			Name:      "slot",
			Label:     "label",
			Interface: "interface",
			Attrs:     map[string]interface{}{"key": "value"},
			Apps:      []string{"app"},
		}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 201)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result":      nil,
		"status":      "Created",
		"status_code": 201.0,
		"type":        "sync",
	})
	c.Check(d.interfaces.Slot("snap", "slot"), check.DeepEquals, &action.Slots[0])
}

func (s *apiSuite) TestAddSlotDisabled(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{
		InterfaceName: "interface",
	})
	d.enableInternalInterfaceActions = false
	action := &interfaceAction{
		Action: "add-slot",
		Slots: []interfaces.Slot{{
			Snap:      "consumer",
			Name:      "slot",
			Label:     "label",
			Interface: "interface",
			Attrs:     map[string]interface{}{"key": "value"},
			Apps:      []string{"app"},
		}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "internal interface actions are disabled",
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Check(d.interfaces.Slot("consumer", "slot"), check.IsNil)
}

func (s *apiSuite) TestAddSlotFailure(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{
		InterfaceName: "interface",
		SanitizeSlotCallback: func(slot *interfaces.Slot) error {
			return fmt.Errorf("required attribute missing")
		},
	})
	action := &interfaceAction{
		Action: "add-slot",
		Slots: []interfaces.Slot{{
			Snap:      "snap",
			Name:      "slot",
			Label:     "label",
			Interface: "interface",
			Attrs:     map[string]interface{}{"key": "value"},
			Apps:      []string{"app"},
		}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "cannot add slot: required attribute missing",
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Check(d.interfaces.Slot("snap", "name"), check.IsNil)
}

func (s *apiSuite) TestRemoveSlotSuccess(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddSlot(&interfaces.Slot{Snap: "consumer", Name: "slot", Interface: "interface"})
	action := &interfaceAction{
		Action: "remove-slot",
		Slots:  []interfaces.Slot{{Snap: "consumer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result":      nil,
		"status":      "OK",
		"status_code": 200.0,
		"type":        "sync",
	})
	c.Check(d.interfaces.Slot("snap", "name"), check.IsNil)
}

func (s *apiSuite) TestRemoveSlotDisabled(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddSlot(&interfaces.Slot{Snap: "consumer", Name: "slot", Interface: "interface"})
	d.enableInternalInterfaceActions = false
	action := &interfaceAction{
		Action: "remove-slot",
		Slots:  []interfaces.Slot{{Snap: "consumer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "internal interface actions are disabled",
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Check(d.interfaces.Slot("consumer", "slot"), check.Not(check.IsNil))
}

func (s *apiSuite) TestRemoveSlotFailure(c *check.C) {
	d := newTestDaemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(&interfaces.Plug{Snap: "producer", Name: "plug", Interface: "interface"})
	d.interfaces.AddSlot(&interfaces.Slot{Snap: "consumer", Name: "slot", Interface: "interface"})
	d.interfaces.Connect("producer", "plug", "consumer", "slot")
	action := &interfaceAction{
		Action: "remove-slot",
		Slots:  []interfaces.Slot{{Snap: "consumer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": `cannot remove slot "slot" from snap "consumer", it is still connected`,
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Check(d.interfaces.Slot("consumer", "slot"), check.Not(check.IsNil))
}

func (s *apiSuite) TestUnsupportedInterfaceRequest(c *check.C) {
	buf := bytes.NewBuffer([]byte(`garbage`))
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "cannot decode request body into an interface action: invalid character 'g' looking for beginning of value",
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
}

func (s *apiSuite) TestMissingInterfaceAction(c *check.C) {
	action := &interfaceAction{}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "interface action not specified",
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
}

func (s *apiSuite) TestUnsupportedInterfaceAction(c *check.C) {
	d := newTestDaemon(c)
	action := &interfaceAction{
		Action: "foo",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/2.0/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "unsupported interface action: \"foo\"",
		},
		"status":      "Bad Request",
		"status_code": 400.0,
		"type":        "error",
	})
	c.Check(d.interfaces.Slot("snap", "name"), check.IsNil)
}

const (
	testTrustedKey = `type: account-key
authority-id: can0nical
account-id: can0nical
public-key-id: 844efa9730eec4be
public-key-fingerprint: 716ff3cec4b9364a2bd930dc844efa9730eec4be
since: 2016-01-14T15:00:00Z
until: 2023-01-14T15:00:00Z
body-length: 376

openpgp xsBNBFaXv40BCADIlqLKFZaPaoe4TNLQv77vh4JWTlt7Z3IN2ducNqfg50q5mnkyUD2D
SckvsMy1440+a0Z83m/A7aPaO1JkLpMGfLr23VLyKCaAe0k6hg69/6aEfXhfy0yYvEOgGcBiX+fN
T6tqdRCsd+08LtisjYez7iJvmVwQ/syeduoTU4EiSVO1zlgc3eeq3TFyvcN0E1EsZ/7l2A33amTo
mtAPVyQsa1B+lTeaUgwuPBWV0oTuYcUSfYsmmsXEKx/PnzkliicnrC9QZ5CcisskVve3QwPAuLUz
2nV7/6vSRF22T4cUPF4QntjZBB6xjopdDH6wQsKyzLTTRak74moWksx8MEmVABEBAAE=

openpgp wsBcBAABCAAQBQJWl8DiCRCETvqXMO7EvgAAhjkIAEoINWjQkujtx/TFYsKh0yYcQSpT
v8O83mLRP7Ty+mH99uQ0/DbeQ1hM5st8cFgzU8SzlDCh6BUMnAl/bR/hhibFD40CBLd13kDXl1aN
APybmSYoDVRQPAPop44UF0aCrTIw4Xds3E56d2Rsn+CkNML03kRc/i0Q53uYzZwxXVnzW/gVOXDL
u/IZtjeo3KsB645MVEUxJLQmjlgMOwMvCHJgWhSvZOuf7wC0soBCN9Ufa/0M/PZFXzzn8LpjKVrX
iDXhV7cY5PceG8ZV7Duo1JadOCzpkOHmai4DcrN7ZeY8bJnuNjOwvTLkrouw9xci4IxpPDRu0T/i
K9qaJtUo4cA=`
	testAccKey = `type: account-key
authority-id: can0nical
account-id: developer1
public-key-id: adea89b00094c337
public-key-fingerprint: 5fa7b16ad5e8c8810d5a0686adea89b00094c337
since: 2016-01-14T15:00:00Z
until: 2023-01-14T15:00:00Z
body-length: 376

openpgp xsBNBFaXv5MBCACkK//qNb3UwRtDviGcCSEi8Z6d5OXok3yilQmEh0LuW6DyP9sVpm08
Vb1LGewOa5dThWGX4XKRBI/jCUnjCJQ6v15lLwHe1N7MJQ58DUxKqWFMV9yn4RcDPk6LqoFpPGdR
rbp9Ivo3PqJRMyD0wuJk9RhbaGZmILcL//BLgomE9NgQdAfZbiEnGxtkqAjeVtBtcJIj5TnCC658
ZCqwugQeO9iJuIn3GosYvvTB6tReq6GP6b4dqvoi7SqxHVhtt2zD4Y6FUZIVmvZK0qwkV0gua2az
LzPOeoVcU1AEl7HVeBk7G6GiT5jx+CjjoGa0j22LdJB9S3JXHtGYk5p9CAwhABEBAAE=

openpgp wsBcBAABCAAQBQJWl8HNCRCETvqXMO7EvgAAeuAIABn/1i8qGyaIhxOWE2cHIPYW3hq2
PWpq7qrPN5Dbp/00xrTvc6tvMQWsXlMrAsYuq3sBCxUp3JRp9XhGiQeJtb8ft10g3+3J7e8OGHjl
CfXJ3A5el8Xxp5qkFywCsLdJgNtF6+uSQ4dO8SrAwzkM7c3JzntxdiFOjDLUSyZ+rXL42jdRagTY
8bcZfb47vd68Hyz3EvSvJuHSDbcNSTd3B832cimpfq5vJ7FoDrchVn3sg+3IwekuPhG3LQn5BVtc
0ontHd+V1GaandhqBaDA01cGZN0gnqv2Haogt0P/h3nZZZJ1nTW5PLC6hs8TZdBdl3Lel8yAHD5L
ZF5jSvRDLgI=`
)

func (s *apiSuite) TestAssertOK(c *check.C) {
	// Setup
	os.MkdirAll(filepath.Dir(dirs.SnapTrustedAccountKey), 0755)
	err := ioutil.WriteFile(dirs.SnapTrustedAccountKey, []byte(testTrustedKey), 0640)
	c.Assert(err, check.IsNil)
	d := newTestDaemon(c)
	buf := bytes.NewBufferString(testAccKey)
	// Execute
	req, err := http.NewRequest("POST", "/2.0/assertions", buf)
	c.Assert(err, check.IsNil)
	rsp := doAssert(assertsCmd, req).Self(nil, nil).(*resp)
	// Verify (external)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	// Verify (internal)
	_, err = d.asserts.Find(asserts.AccountKeyType, map[string]string{
		"account-id":    "developer1",
		"public-key-id": "adea89b00094c337",
	})
	c.Check(err, check.IsNil)
}

func (s *apiSuite) TestAssertInvalid(c *check.C) {
	// Setup
	newTestDaemon(c)
	buf := bytes.NewBufferString("blargh")
	req, err := http.NewRequest("POST", "/2.0/assertions", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	// Execute
	assertsCmd.POST(assertsCmd, req).ServeHTTP(rec, req)
	// Verify (external)
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains,
		"can't decode request body into an assertion")
}

func (s *apiSuite) TestAssertError(c *check.C) {
	// Setup
	newTestDaemon(c)
	buf := bytes.NewBufferString(testAccKey)
	req, err := http.NewRequest("POST", "/2.0/assertions", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	// Execute
	assertsCmd.POST(assertsCmd, req).ServeHTTP(rec, req)
	// Verify (external)
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains, "assert failed")
}

func (s *apiSuite) TestAssertsFindManyAll(c *check.C) {
	// Setup
	os.MkdirAll(filepath.Dir(dirs.SnapTrustedAccountKey), 0755)
	err := ioutil.WriteFile(dirs.SnapTrustedAccountKey, []byte(testTrustedKey), 0640)
	c.Assert(err, check.IsNil)
	d := newTestDaemon(c)
	a, err := asserts.Decode([]byte(testAccKey))
	c.Assert(err, check.IsNil)
	err = d.asserts.Add(a)
	c.Assert(err, check.IsNil)
	// Execute
	req, err := http.NewRequest("POST", "/2.0/assertions/account-key", nil)
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"assertType": "account-key"}
	rec := httptest.NewRecorder()
	assertsFindManyCmd.GET(assertsFindManyCmd, req).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, http.StatusOK, check.Commentf("body %q", rec.Body))
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/x.ubuntu.assertion; bundle=y")
	c.Check(rec.HeaderMap.Get("X-Ubuntu-Assertions-Count"), check.Equals, "2")
	dec := asserts.NewDecoder(rec.Body)
	a1, err := dec.Decode()
	c.Assert(err, check.IsNil)
	c.Check(a1.Type(), check.Equals, asserts.AccountKeyType)

	a2, err := dec.Decode()
	c.Assert(err, check.IsNil)

	_, err = dec.Decode()
	c.Assert(err, check.Equals, io.EOF)

	ids := []string{a1.(*asserts.AccountKey).AccountID(), a2.(*asserts.AccountKey).AccountID()}
	c.Check(ids, check.DeepEquals, []string{"can0nical", "developer1"})
}

func (s *apiSuite) TestAssertsFindManyFilter(c *check.C) {
	// Setup
	os.MkdirAll(filepath.Dir(dirs.SnapTrustedAccountKey), 0755)
	err := ioutil.WriteFile(dirs.SnapTrustedAccountKey, []byte(testTrustedKey), 0640)
	c.Assert(err, check.IsNil)
	d := newTestDaemon(c)
	a, err := asserts.Decode([]byte(testAccKey))
	c.Assert(err, check.IsNil)
	err = d.asserts.Add(a)
	c.Assert(err, check.IsNil)
	// Execute
	req, err := http.NewRequest("POST", "/2.0/assertions/account-key?account-id=developer1", nil)
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"assertType": "account-key"}
	rec := httptest.NewRecorder()
	assertsFindManyCmd.GET(assertsFindManyCmd, req).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, http.StatusOK, check.Commentf("body %q", rec.Body))
	c.Check(rec.HeaderMap.Get("X-Ubuntu-Assertions-Count"), check.Equals, "1")
	dec := asserts.NewDecoder(rec.Body)
	a1, err := dec.Decode()
	c.Assert(err, check.IsNil)
	c.Check(a1.Type(), check.Equals, asserts.AccountKeyType)
	c.Check(a1.(*asserts.AccountKey).AccountID(), check.Equals, "developer1")
	_, err = dec.Decode()
	c.Check(err, check.Equals, io.EOF)
}

func (s *apiSuite) TestAssertsFindManyNoResults(c *check.C) {
	// Setup
	os.MkdirAll(filepath.Dir(dirs.SnapTrustedAccountKey), 0755)
	err := ioutil.WriteFile(dirs.SnapTrustedAccountKey, []byte(testTrustedKey), 0640)
	c.Assert(err, check.IsNil)
	d := newTestDaemon(c)
	a, err := asserts.Decode([]byte(testAccKey))
	c.Assert(err, check.IsNil)
	err = d.asserts.Add(a)
	c.Assert(err, check.IsNil)
	// Execute
	req, err := http.NewRequest("POST", "/2.0/assertions/account-key?account-id=xyzzyx", nil)
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"assertType": "account-key"}
	rec := httptest.NewRecorder()
	assertsFindManyCmd.GET(assertsFindManyCmd, req).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, http.StatusOK, check.Commentf("body %q", rec.Body))
	c.Check(rec.HeaderMap.Get("X-Ubuntu-Assertions-Count"), check.Equals, "0")
	dec := asserts.NewDecoder(rec.Body)
	_, err = dec.Decode()
	c.Check(err, check.Equals, io.EOF)
}

func (s *apiSuite) TestAssertsInvalidType(c *check.C) {
	// Setup
	newTestDaemon(c)
	// Execute
	req, err := http.NewRequest("POST", "/2.0/assertions/foo", nil)
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"assertType": "foo"}
	rec := httptest.NewRecorder()
	assertsFindManyCmd.GET(assertsFindManyCmd, req).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains, "invalid assert type")
}

func (s *apiSuite) TestGetEvents(c *check.C) {
	d := newTestDaemon(c)
	eventsCmd.d = d
	c.Assert(d.hub.SubscriberCount(), check.Equals, 0)

	ts := httptest.NewServer(http.HandlerFunc(eventsCmd.GET(eventsCmd, nil).ServeHTTP))

	req, err := http.NewRequest("GET", ts.URL, nil)
	c.Assert(err, check.IsNil)
	req.Header.Add("Upgrade", "websocket")
	req.Header.Add("Connection", "Upgrade")
	req.Header.Add("Sec-WebSocket-Key", "xxx")
	req.Header.Add("Sec-WebSocket-Version", "13")

	client := &http.Client{}
	resp, err := client.Do(req)
	c.Assert(err, check.IsNil)
	// upgrades request
	c.Assert(resp.Header["Upgrade"], check.DeepEquals, []string{"websocket"})
	// adds subscriber - this happens at the end of the response so close the
	// server first to prevent problems with sequence ordering
	ts.Close()
	c.Assert(d.hub.SubscriberCount(), check.Equals, 1)
}
