// -*- Mode: Go; indent-tabs-mode: t -*-

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

	"github.com/ubuntu-core/snappy/caps"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/pkg"
	"github.com/ubuntu-core/snappy/pkg/lightweight"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/release"
	"github.com/ubuntu-core/snappy/snappy"
	"github.com/ubuntu-core/snappy/systemd"
	"github.com/ubuntu-core/snappy/testutil"
	"github.com/ubuntu-core/snappy/timeout"
)

type apiSuite struct {
	parts []snappy.Part
	err   error
	vars  map[string]string
}

var _ = check.Suite(&apiSuite{})

func (s *apiSuite) Details(string, string) ([]snappy.Part, error) {
	return s.parts, s.err
}

func (s *apiSuite) All() ([]snappy.Part, error) {
	return s.parts, s.err
}

func (s *apiSuite) Updates() ([]snappy.Part, error) {
	return s.parts, s.err
}

func (s *apiSuite) muxVars(*http.Request) map[string]string {
	return s.vars
}

func (s *apiSuite) SetUpSuite(c *check.C) {
	newRemoteRepo = func() metarepo {
		return s
	}
	newSystemRepo = newRemoteRepo
	muxVars = s.muxVars
}

func (s *apiSuite) TearDownSuite(c *check.C) {
	newRemoteRepo = nil
	newSystemRepo = nil
	muxVars = nil
}

func (s *apiSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapLockFile), 0755), check.IsNil)

	s.parts = nil
	s.err = nil
	s.vars = nil
}

func (s *apiSuite) TearDownTest(c *check.C) {
	findServices = snappy.FindServices
}

func (s *apiSuite) mkInstalled(c *check.C, name, origin, version string, active bool, extraYaml string) {
	fullname := name + "." + origin
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapDataDir, fullname, version), 0755), check.IsNil)

	metadir := filepath.Join(dirs.SnapAppsDir, fullname, version, "meta")
	c.Assert(os.MkdirAll(metadir, 0755), check.IsNil)

	c.Check(ioutil.WriteFile(filepath.Join(metadir, "icon.svg"), []byte("yadda icon"), 0644), check.IsNil)

	content := fmt.Sprintf(`
name: %s
version: %s
%s`, name, version, extraYaml)
	c.Check(ioutil.WriteFile(filepath.Join(metadir, "package.yaml"), []byte(content), 0644), check.IsNil)
	c.Check(ioutil.WriteFile(filepath.Join(metadir, "hashes.yaml"), []byte(nil), 0644), check.IsNil)

	if active {
		c.Assert(os.Symlink(version, filepath.Join(dirs.SnapAppsDir, fullname, "current")), check.IsNil)
	}
}

func (s *apiSuite) mkOem(c *check.C, store string) {
	content := []byte(fmt.Sprintf(`name: test
version: 1
type: oem
oem: {store: {id: %q}}
`, store))

	d := filepath.Join(dirs.SnapOemDir, "test")
	m := filepath.Join(d, "1", "meta")
	c.Assert(os.MkdirAll(m, 0755), check.IsNil)
	c.Assert(os.Symlink("1", filepath.Join(d, "current")), check.IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(m, "package.yaml"), content, 0644), check.IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(m, "hashes.yaml"), []byte(nil), 0644), check.IsNil)
}

func (s *apiSuite) TestPackageInfoOneIntegration(c *check.C) {
	newTestDaemon()

	s.vars = map[string]string{"name": "foo", "origin": "bar"}

	// the store tells us about v2
	s.parts = []snappy.Part{&tP{
		name:         "foo",
		version:      "v2",
		description:  "description",
		origin:       "bar",
		isInstalled:  true,
		isActive:     true,
		icon:         "meta/icon.svg",
		_type:        pkg.TypeApp,
		downloadSize: 2,
	}}

	// we have v0 installed
	s.mkInstalled(c, "foo", "bar", "v0", false, "")
	// and v1 is current
	s.mkInstalled(c, "foo", "bar", "v1", true, "")

	rsp, ok := getPackageInfo(packageCmd, nil).(*resp)
	c.Assert(ok, check.Equals, true)

	// installed_size depends on vagaries of the filesystem, just check regexp
	c.Assert(rsp, check.NotNil)
	c.Assert(rsp.Result, check.FitsTypeOf, map[string]string{})
	m := rsp.Result.(map[string]string)
	c.Check(m["installed_size"], check.Matches, "[0-9]+")
	delete(m, "installed_size")

	expected := &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: map[string]string{
			"name":               "foo",
			"version":            "v1",
			"description":        "description",
			"origin":             "bar",
			"status":             "active",
			"icon":               "/1.0/icons/foo.bar/icon",
			"type":               string(pkg.TypeApp),
			"vendor":             "",
			"download_size":      "2",
			"resource":           "/1.0/packages/foo.bar",
			"update_available":   "v2",
			"rollback_available": "v0",
		},
	}

	c.Check(rsp, check.DeepEquals, expected)
}

func (s *apiSuite) TestPackageInfoNotFound(c *check.C) {
	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	s.err = snappy.ErrPackageNotFound

	c.Check(getPackageInfo(packageCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestPackageInfoNoneFound(c *check.C) {
	s.vars = map[string]string{"name": "foo", "origin": "bar"}

	c.Check(getPackageInfo(packageCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestPackageInfoIgnoresRemoteErrors(c *check.C) {
	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	s.err = errors.New("weird")

	rsp := getPackageInfo(packageCmd, nil).Self(nil, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPackageInfoWeirdRoute(c *check.C) {
	// can't really happen

	d := newTestDaemon()

	// use the wrong command to force the issue
	wrongCmd := &Command{Path: "/{what}", d: d}
	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	s.parts = []snappy.Part{&tP{name: "foo"}}
	c.Check(getPackageInfo(wrongCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusInternalServerError)
}

func (s *apiSuite) TestPackageInfoBadRoute(c *check.C) {
	// can't really happen, v2

	d := newTestDaemon()

	// get the route and break it
	route := d.router.Get(packageCmd.Path)
	c.Assert(route.Name("foo").GetError(), check.NotNil)

	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	s.parts = []snappy.Part{&tP{name: "foo"}}

	rsp := getPackageInfo(packageCmd, nil).Self(nil, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusInternalServerError)
	c.Check(rsp.Result.(*errorResult).Msg, check.Matches, `route can't build URL .*`)
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
		"newSystemRepo",
		"newSnap",
		"pkgActionDispatch",
		// packageInstruction vars:
		"snappyInstall",
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

	expected := []interface{}{"/1.0"}
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

func (s *apiSuite) TestV1(c *check.C) {
	// check it only does GET
	c.Check(v1Cmd.PUT, check.IsNil)
	c.Check(v1Cmd.POST, check.IsNil)
	c.Check(v1Cmd.DELETE, check.IsNil)
	c.Assert(v1Cmd.GET, check.NotNil)

	rec := httptest.NewRecorder()
	c.Check(v1Cmd.Path, check.Equals, "/1.0")

	s.mkrelease()

	v1Cmd.GET(v1Cmd, nil).ServeHTTP(rec, nil)
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

func (s *apiSuite) TestV1Store(c *check.C) {
	rec := httptest.NewRecorder()
	c.Check(v1Cmd.Path, check.Equals, "/1.0")

	s.mkrelease()
	s.mkOem(c, "some-store")

	v1Cmd.GET(v1Cmd, nil).ServeHTTP(rec, nil)
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

func (s *apiSuite) TestPackagesInfoOnePerIntegration(c *check.C) {
	req, err := http.NewRequest("GET", "/1.0/packages", nil)
	c.Assert(err, check.IsNil)

	ddirs := [][2]string{{"foo.bar", "v1"}, {"bar.baz", "v2"}, {"baz.qux", "v3"}, {"qux.mip", "v4"}}

	for i := range ddirs {
		c.Assert(os.MkdirAll(filepath.Join(dirs.SnapDataDir, ddirs[i][0], ddirs[i][1]), 0755), check.IsNil)
	}

	rsp, ok := getPackagesInfo(packagesCmd, req).(*resp)
	c.Assert(ok, check.Equals, true)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Result, check.NotNil)

	meta, ok := rsp.Result.(map[string]interface{})
	c.Assert(ok, check.Equals, true)
	c.Assert(meta, check.NotNil)
	c.Check(meta["paging"], check.DeepEquals, map[string]interface{}{"pages": 1, "page": 1, "count": len(ddirs)})

	packages, ok := meta["packages"].(map[string]map[string]string)
	c.Assert(ok, check.Equals, true)
	c.Check(packages, check.NotNil)
	c.Check(packages, check.HasLen, len(ddirs))

	for i := range ddirs {
		qn, version := ddirs[i][0], ddirs[i][1]
		idx := strings.LastIndex(qn, ".")
		name, origin := qn[:idx], qn[idx+1:]
		got := packages[qn]
		c.Assert(got, check.NotNil, check.Commentf(qn))
		c.Check(got["name"], check.Equals, name)
		c.Check(got["version"], check.Equals, version)
		c.Check(got["origin"], check.Equals, origin)
	}
}

func (s *apiSuite) TestDeleteOpNotFound(c *check.C) {
	s.vars = map[string]string{"uuid": "42"}
	rsp := deleteOp(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestDeleteOpStillRunning(c *check.C) {
	d := newTestDaemon()

	d.tasks["42"] = &Task{}
	s.vars = map[string]string{"uuid": "42"}
	rsp := deleteOp(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
}

func (s *apiSuite) TestDeleteOp(c *check.C) {
	d := newTestDaemon()

	task := &Task{}
	d.tasks["42"] = task
	task.tomb.Kill(nil)
	s.vars = map[string]string{"uuid": "42"}
	rsp := deleteOp(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
}

func (s *apiSuite) TestGetOpInfoIntegration(c *check.C) {
	d := newTestDaemon()

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
		"resource":   "/1.0/operations/" + id,
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
		"resource":   "/1.0/operations/" + id,
		"status":     TaskSucceeded,
		"may_cancel": false,
		"created_at": FormatTime(t.CreatedAt()),
		"updated_at": FormatTime(t.UpdatedAt()),
		"output":     "hello",
	})

	tf2 := t.UpdatedAt().UTC().UnixNano()

	c.Check(tf1 < tf2, check.Equals, true)
}

func (s *apiSuite) TestPostPackageBadRequest(c *check.C) {
	s.vars = map[string]string{"uuid": "42"}
	rsp := getOpInfo(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)

	buf := bytes.NewBufferString(`hello`)
	req, err := http.NewRequest("POST", "/1.0/packages/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp = postPackage(packageCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostPackageBadAction(c *check.C) {
	s.vars = map[string]string{"uuid": "42"}
	c.Check(getOpInfo(operationCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)

	buf := bytes.NewBufferString(`{"action": "potato"}`)
	req, err := http.NewRequest("POST", "/1.0/packages/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postPackage(packageCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostPackage(c *check.C) {
	d := newTestDaemon()

	s.vars = map[string]string{"uuid": "42"}
	c.Check(getOpInfo(operationCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)

	ch := make(chan struct{})

	pkgActionDispatch = func(*packageInstruction) func() interface{} {
		return func() interface{} {
			ch <- struct{}{}
			return "hi"
		}
	}
	defer func() {
		pkgActionDispatch = pkgActionDispatchImpl
	}()

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/1.0/packages/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postPackage(packageCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)
	m := rsp.Result.(map[string]interface{})
	c.Assert(m["resource"], check.Matches, "/1.0/operations/.*")

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

func (s *apiSuite) TestPostPackageDispatch(c *check.C) {
	inst := &packageInstruction{}

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

type cfgc struct {
	cfg string
	err error
	idx int
}

func (cfgc) IsInstalled(string) bool { return true }
func (c cfgc) ActiveIndex() int      { return c.idx }
func (c cfgc) Load(string) (snappy.Part, error) {
	return &tP{name: "foo", version: "v1", origin: "bar", isActive: true, config: c.cfg, configErr: c.err}, nil
}

func (s *apiSuite) TestPackageGetConfig(c *check.C) {
	req, err := http.NewRequest("GET", "/1.0/packages/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	configStr := "some: config"
	oldConcrete := lightweight.NewConcrete
	defer func() {
		lightweight.NewConcrete = oldConcrete
	}()
	lightweight.NewConcrete = func(*lightweight.PartBag, string) lightweight.Concreter {
		return &cfgc{cfg: configStr}
	}

	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	s.mkInstalled(c, "foo", "bar", "v1", true, "")

	rsp := packageConfig(packagesCmd, req).(*resp)

	c.Check(rsp, check.DeepEquals, &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: configStr,
	})
}

func (s *apiSuite) TestPackageGetConfigMissing(c *check.C) {
	s.vars = map[string]string{"name": "foo", "origin": "bar"}

	req, err := http.NewRequest("GET", "/1.0/packages/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := packageConfig(packagesCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestPackageGetConfigInactive(c *check.C) {
	s.vars = map[string]string{"name": "foo", "origin": "bar"}

	s.mkInstalled(c, "foo", "bar", "v1", false, "")

	req, err := http.NewRequest("GET", "/1.0/packages/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := packageConfig(packagesCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
}

func (s *apiSuite) TestPackageGetConfigNoConfig(c *check.C) {
	s.vars = map[string]string{"name": "foo", "origin": "bar"}

	s.mkInstalled(c, "foo", "bar", "v1", true, "")

	req, err := http.NewRequest("GET", "/1.0/packages/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := packageConfig(packagesCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusInternalServerError)
}

func (s *apiSuite) TestPackagePutConfig(c *check.C) {
	newConfigStr := "some other config"
	req, err := http.NewRequest("PUT", "/1.0/packages/foo.bar/config", bytes.NewBufferString(newConfigStr))
	c.Assert(err, check.IsNil)

	configStr := "some: config"
	oldConcrete := lightweight.NewConcrete
	defer func() {
		lightweight.NewConcrete = oldConcrete
	}()
	lightweight.NewConcrete = func(*lightweight.PartBag, string) lightweight.Concreter {
		return &cfgc{cfg: configStr}
	}

	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	s.mkInstalled(c, "foo", "bar", "v1", true, "")

	rsp := packageConfig(packageConfigCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp, check.DeepEquals, &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: newConfigStr,
	})
}

func (s *apiSuite) TestPackagePutConfigMissing(c *check.C) {
	s.vars = map[string]string{"name": "foo", "origin": "bar"}

	req, err := http.NewRequest("PUT", "/1.0/packages/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := packageConfig(packagesCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestPackagePutConfigInactive(c *check.C) {
	s.vars = map[string]string{"name": "foo", "origin": "bar"}

	s.mkInstalled(c, "foo", "bar", "v1", false, "")

	req, err := http.NewRequest("PUT", "/1.0/packages/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := packageConfig(packagesCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
}

func (s *apiSuite) TestPackagePutConfigNoConfig(c *check.C) {
	s.vars = map[string]string{"name": "foo", "origin": "bar"}

	s.mkInstalled(c, "foo", "bar", "v1", true, "")

	req, err := http.NewRequest("PUT", "/1.0/packages/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := packageConfig(packagesCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusInternalServerError)
}

func (s *apiSuite) TestConfigMultiBadBody(c *check.C) {
	newTestDaemon()

	req, err := http.NewRequest("PUT", "/1.0/packages", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)
	rsp := configMulti(packagesCmd, req).Self(nil, nil).(*resp)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
}

func (s *apiSuite) TestPackagesPutStr(c *check.C) {
	newConfigs := map[string]string{"foo.bar": "some other config", "baz.qux": "stuff", "missing.pkg": "blah blah"}
	bs, err := json.Marshal(newConfigs)
	c.Assert(err, check.IsNil)
	s.genericTestPackagePut(c, bytes.NewBuffer(bs), map[string]*configSubtask{
		"foo.bar":     &configSubtask{Status: TaskSucceeded, Output: "some other config"},
		"baz.qux":     &configSubtask{Status: TaskFailed, Output: &errorResult{Str: snappy.ErrConfigNotFound.Error(), Obj: snappy.ErrConfigNotFound, Msg: "Config failed"}},
		"missing.pkg": &configSubtask{Status: TaskFailed, Output: &errorResult{Str: snappy.ErrPackageNotFound.Error(), Obj: snappy.ErrPackageNotFound}},
	})
}

func (s *apiSuite) TestPackagesPutNil(c *check.C) {
	newConfigs := map[string][]byte{"foo.bar": nil, "mip.brp": nil}
	bs, err := json.Marshal(newConfigs)
	c.Assert(err, check.IsNil)
	s.genericTestPackagePut(c, bytes.NewBuffer(bs), map[string]*configSubtask{
		"foo.bar": &configSubtask{Status: TaskSucceeded, Output: "some: config"},
		"mip.brp": &configSubtask{Status: TaskFailed, Output: &errorResult{Str: snappy.ErrSnapNotActive.Error(), Obj: snappy.ErrSnapNotActive}},
	})
}

func (s *apiSuite) genericTestPackagePut(c *check.C, body io.Reader, expected map[string]*configSubtask) {
	d := newTestDaemon()

	req, err := http.NewRequest("PUT", "/1.0/packages", body)
	c.Assert(err, check.IsNil)

	configStr := "some: config"
	oldConcrete := lightweight.NewConcrete
	defer func() {
		lightweight.NewConcrete = oldConcrete
	}()
	lightweight.NewConcrete = func(bag *lightweight.PartBag, _ string) lightweight.Concreter {
		switch bag.Name {
		case "foo":
			return &cfgc{cfg: configStr}
		case "mip":
			return &cfgc{idx: -1}
		default:
			return &cfgc{err: snappy.ErrConfigNotFound}
		}
	}

	s.mkInstalled(c, "foo", "bar", "v1", true, "")
	s.mkInstalled(c, "baz", "qux", "v1", true, "")
	s.mkInstalled(c, "mip", "brp", "v1", false, "")

	rsp := configMulti(packagesCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)
	c.Check(rsp.Status, check.Equals, http.StatusAccepted)
	m := rsp.Result.(map[string]interface{})
	c.Check(m["resource"], check.Matches, "/1.0/operations/.*")

	uuid := m["resource"].(string)[16:]

	task := d.GetTask(uuid)
	c.Assert(task, check.NotNil)

	// wait up to another ten seconds (!) for the task to finish properly
	for i := 0; i < 1000; i++ {
		if d.GetTask(uuid).State() != TaskRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	c.Assert(task.State(), check.Equals, TaskSucceeded)
	out := task.Output().(map[string]*configSubtask)
	c.Check(out, check.HasLen, len(expected))

	var missing []string
	for k, v := range out {
		exp, ok := expected[k]
		if !ok {
			missing = append(missing, k)
			continue
		}

		c.Check(v.Status, check.Equals, exp.Status, check.Commentf(k))
		c.Check(v.Output, check.DeepEquals, exp.Output, check.Commentf(k))
	}
	c.Check(missing, check.HasLen, 0, check.Commentf("missing from expected"))
	missing = nil
	for k := range expected {
		if _, ok := out[k]; !ok {
			missing = append(missing, k)
		}
	}
	c.Check(missing, check.HasLen, 0, check.Commentf("missing from obtained"))
}

func (s *apiSuite) TestPackageServiceGet(c *check.C) {
	findServices = func(string, string, progress.Meter) (snappy.ServiceActor, error) {
		return &tSA{ssout: []*snappy.PackageServiceStatus{{ServiceName: "svc"}}}, nil
	}

	req, err := http.NewRequest("GET", "/1.0/packages/foo.bar/services", nil)
	c.Assert(err, check.IsNil)

	s.mkInstalled(c, "foo", "bar", "v1", true, "services: [{name: svc}]")
	s.vars = map[string]string{"name": "foo", "origin": "bar"} // NB: no service specified

	rsp := packageService(packageSvcsCmd, req).(*resp)
	c.Assert(rsp, check.NotNil)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)

	m := rsp.Result.(map[string]*svcDesc)
	c.Assert(m["svc"], check.FitsTypeOf, new(svcDesc))
	c.Check(m["svc"].Op, check.Equals, "status")
	c.Check(m["svc"].Spec, check.DeepEquals, &snappy.ServiceYaml{Name: "svc", StopTimeout: timeout.DefaultTimeout})
	c.Check(m["svc"].Status, check.DeepEquals, &snappy.PackageServiceStatus{ServiceName: "svc"})
}

func (s *apiSuite) TestPackageServicePut(c *check.C) {
	findServices = func(string, string, progress.Meter) (snappy.ServiceActor, error) {
		return &tSA{ssout: []*snappy.PackageServiceStatus{{ServiceName: "svc"}}}, nil
	}

	buf := bytes.NewBufferString(`{"action": "stop"}`)
	req, err := http.NewRequest("PUT", "/1.0/packages/foo.bar/services", buf)
	c.Assert(err, check.IsNil)

	s.mkInstalled(c, "foo", "bar", "v1", true, "services: [{name: svc}]")
	s.vars = map[string]string{"name": "foo", "origin": "bar"} // NB: no service specified

	rsp := packageService(packageSvcsCmd, req).(*resp)
	c.Assert(rsp, check.NotNil)
	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)
	c.Check(rsp.Status, check.Equals, http.StatusAccepted)
}

func (s *apiSuite) TestSideloadPackage(c *check.C) {
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

	newSnap = func(fn string, origin string, unauthOk bool) (snappy.Part, error) {
		c.Check(origin, check.Equals, snappy.SideloadedOrigin)
		c.Check(unauthOk, check.Equals, unsignedExpected)

		bs, err := ioutil.ReadFile(fn)
		c.Check(err, check.IsNil)
		c.Check(string(bs), check.Equals, "xyzzy")

		ch <- struct{}{}

		return &tP{}, nil
	}
	defer func() { newSnap = newSnapImpl }()

	req, err := http.NewRequest("POST", "/1.0/packages", tmpfile)
	c.Assert(err, check.IsNil)
	for k, v := range head {
		req.Header.Set(k, v)
	}

	rsp := sideloadPackage(packagesCmd, req).(*resp)
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

	req, err := http.NewRequest("GET", "/1.0/packages/foo.bar/services/baz/logs", nil)
	c.Assert(err, check.IsNil)

	rsp := getLogs(packageSvcLogsCmd, req).(*resp)
	c.Assert(rsp, check.DeepEquals, &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: []map[string]interface{}{{"message": "hi", "timestamp": "42", "raw": log}},
	})
}

func (s *apiSuite) TestAppIconGet(c *check.C) {
	// have an active foo.bar in the system
	s.mkInstalled(c, "foo", "bar", "v1", true, "icon: icon.ick")

	// have an icon for it in the package itself
	iconfile := filepath.Join(dirs.SnapAppsDir, "foo.bar", "v1", "icon.ick")
	c.Check(ioutil.WriteFile(iconfile, []byte("ick"), 0644), check.IsNil)

	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	req, err := http.NewRequest("GET", "/1.0/icons/foo.bar/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Body.String(), check.Equals, "ick")
}

func (s *apiSuite) TestAppIconGetInactive(c *check.C) {
	// have an *in*active foo.bar in the system
	s.mkInstalled(c, "foo", "bar", "v1", false, "icon: icon.ick")

	// have an icon for it in the package itself
	iconfile := filepath.Join(dirs.SnapAppsDir, "foo.bar", "v1", "icon.ick")
	c.Check(ioutil.WriteFile(iconfile, []byte("ick"), 0644), check.IsNil)

	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	req, err := http.NewRequest("GET", "/1.0/icons/foo.bar/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Body.String(), check.Equals, "ick")
}

func (s *apiSuite) TestAppIconGetNoIcon(c *check.C) {
	// have an *in*active foo.bar in the system
	s.mkInstalled(c, "foo", "bar", "v1", true, "") // NOTE: no icon in the yaml

	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	req, err := http.NewRequest("GET", "/1.0/icons/foo.bar/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code/100, check.Equals, 4)
}

func (s *apiSuite) TestAppIconGetNoApp(c *check.C) {
	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	req, err := http.NewRequest("GET", "/1.0/icons/foo.bar/icon", nil)
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

	inst := &packageInstruction{License: lic}

	c.Check(inst.Agreed(lic.Intro, lic.License), check.Equals, true)
}

func (s *apiSuite) TestPkgInstructionAgreedNOK(c *check.C) {
	lic := &licenseData{
		Intro:   "hi",
		License: "Void where empty",
		Agreed:  false,
	}

	inst := &packageInstruction{License: lic}

	c.Check(inst.Agreed(lic.Intro, lic.License), check.Equals, false)
}

func (s *apiSuite) TestPkgInstructionMismatch(c *check.C) {
	lic := &licenseData{
		Intro:   "hi",
		License: "Void where empty",
		Agreed:  true,
	}

	inst := &packageInstruction{License: lic}

	c.Check(inst.Agreed("blah", "yak yak"), check.Equals, false)
}

func (s *apiSuite) TestInstall(c *check.C) {
	orig := snappyInstall
	defer func() { snappyInstall = orig }()

	calledFlags := snappy.InstallFlags(42)

	snappyInstall = func(name string, flags snappy.InstallFlags, meter progress.Meter) (string, error) {
		calledFlags = flags

		return "", nil
	}

	inst := &packageInstruction{
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

	snappyInstall = func(name string, flags snappy.InstallFlags, meter progress.Meter) (string, error) {
		calledFlags = flags

		return "", nil
	}

	inst := &packageInstruction{
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

	snappyInstall = func(name string, flags snappy.InstallFlags, meter progress.Meter) (string, error) {
		if meter.Agreed("hi", "yak yak") {
			return "", nil
		}

		return "", snappy.ErrLicenseNotAccepted
	}

	inst := &packageInstruction{
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
	d := newTestDaemon()

	orig := snappyInstall
	defer func() { snappyInstall = orig }()

	snappyInstall = func(name string, flags snappy.InstallFlags, meter progress.Meter) (string, error) {
		if meter.Agreed("hi", "yak yak") {
			return "", nil
		}

		return "", snappy.ErrLicenseNotAccepted
	}

	req, err := http.NewRequest("POST", "/1.0/packages/foo.bar", strings.NewReader(`{"action": "install"}`))
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"name": "foo", "origin": "bar"}

	res := postPackage(packageCmd, req).(*resp).Result.(map[string]interface{})
	task := d.tasks[res["resource"].(string)[16:]]
	c.Check(task, check.NotNil)

	task.tomb.Wait()
	c.Check(task.State(), check.Equals, TaskFailed)
	errRes := task.output.(errorResult)
	c.Check(errRes.Str, check.Equals, "license agreement required")
	c.Check(errRes.Obj, check.DeepEquals, &licenseData{
		Intro:   "hi",
		License: "yak yak",
	})

	req, err = http.NewRequest("POST", "/1.0/packages/foo.bar", strings.NewReader(`{"action": "install", "license": {"intro": "hi", "license": "yak yak", "agreed": true}}`))
	c.Assert(err, check.IsNil)

	res = postPackage(packageCmd, req).(*resp).Result.(map[string]interface{})
	task = d.tasks[res["resource"].(string)[16:]]
	c.Check(task, check.NotNil)

	task.tomb.Wait()
	c.Check(task.State(), check.Equals, TaskSucceeded)
}

func (s *apiSuite) TestGetCapabilities(c *check.C) {
	d := newTestDaemon()
	d.capRepo.Add(&caps.Capability{
		Name:  "caps-lock-led",
		Label: "Caps Lock LED",
		Type:  caps.BoolFileType,
		Attrs: map[string]string{
			"path": "/sys/class/leds/input::capslock/brightness",
		},
	})
	req, err := http.NewRequest("GET", "/1.0/capabilities", nil)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	capabilitiesCmd.GET(capabilitiesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"capabilities": map[string]interface{}{
				"caps-lock-led": map[string]interface{}{
					"name":  "caps-lock-led",
					"label": "Caps Lock LED",
					"type":  "bool-file",
					"attrs": map[string]interface{}{
						"path": "/sys/class/leds/input::capslock/brightness",
					},
				},
			},
		},
		"status":      "OK",
		"status_code": 200.0, // A float because $reasons
		"type":        "sync",
	})
}

func (s *apiSuite) TestAddCapabilitiesGood(c *check.C) {
	// Setup
	d := newTestDaemon()
	cap := &caps.Capability{
		Name:  "name",
		Label: "label",
		Type:  caps.BoolFileType,
		Attrs: map[string]string{"path": "/nonexistent"},
	}
	text, err := json.Marshal(cap)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	// Execute
	req, err := http.NewRequest("POST", "/1.0/capabilities", buf)
	c.Assert(err, check.IsNil)
	rsp := addCapability(capabilitiesCmd, req).Self(nil, nil).(*resp)
	// Verify (external)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusCreated)
	c.Check(rsp.Result, check.DeepEquals, map[string]string{"resource": "/1.0/capabilities/name"})
	// Verify (internal)
	c.Check(d.capRepo.All(), testutil.DeepContains, *cap)
}

func (s *apiSuite) TestAddCapabilitiesNameClash(c *check.C) {
	// Setup
	// Start with one capability named 'name' in the repository
	d := newTestDaemon()
	cap := &caps.Capability{
		Name:  "name",
		Label: "label",
		Type:  caps.BoolFileType,
		Attrs: map[string]string{"path": "/nonexistent"},
	}
	err := d.capRepo.Add(cap)
	c.Assert(err, check.IsNil)
	// Prepare for adding a second capability with the same name
	capClashing := &caps.Capability{
		Name:  "name",
		Label: "second label",
		Type:  caps.BoolFileType,
		Attrs: map[string]string{"path": "/nonexistent"},
	}
	text, err := json.Marshal(capClashing)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	// Execute
	req, err := http.NewRequest("GET", "/1.0/capabilities", buf)
	c.Assert(err, check.IsNil)
	rsp := addCapability(capabilitiesCmd, req).Self(nil, nil).(*resp)
	// Verify (external)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*errorResult).Msg, check.Matches, `can't add capability`)
	// Verify (internal)
	c.Check(d.capRepo.All(), testutil.DeepContains, *cap)
	c.Check(d.capRepo.All(), check.Not(testutil.DeepContains), *capClashing)
}

func (s *apiSuite) TestAddCapabilitiesUnintelligible(c *check.C) {
	// Setup
	d := newTestDaemon()
	buf := bytes.NewBufferString("blargh")
	req, err := http.NewRequest("POST", "/1.0/capabilities", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	// Execute
	capabilitiesCmd.POST(capabilitiesCmd, req).ServeHTTP(rec, req)
	// Verify (external)
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains,
		"can't decode request body into a capability")
	// Verify (internal)
	c.Check(d.capRepo.All(), check.HasLen, 0)
}

func (s *apiSuite) TestAddCapabilitiesNotACapability(c *check.C) {
	// Setup
	d := newTestDaemon()
	buf := bytes.NewBufferString(`{"NotACapability": 1}`)
	req, err := http.NewRequest("POST", "/1.0/capabilities", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	// Execute
	capabilitiesCmd.POST(capabilitiesCmd, req).ServeHTTP(rec, req)
	// Verify (external)
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains,
		`can't decode request body into a capability`)
	// Verify (internal)
	c.Check(d.capRepo.All(), check.HasLen, 0)
}

func (s *apiSuite) TestDeleteCapabilityGood(c *check.C) {
	// Setup
	d := newTestDaemon()
	t := &caps.Type{Name: "test"}
	err := d.capRepo.AddType(t)
	c.Assert(err, check.IsNil)
	cap := &caps.Capability{Name: "name", Type: t}
	err = d.capRepo.Add(cap)
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"name": "name"}
	// Execute
	rsp := deleteCapability(capabilityCmd, nil).Self(nil, nil).(*resp)
	// Verify (external)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	// Verify (internal)
	c.Check(d.capRepo.Capability(cap.Name), check.IsNil)
}

func (s *apiSuite) TestDeleteCapabilityNotFound(c *check.C) {
	// Setup
	d := newTestDaemon()
	before := d.capRepo.All()
	s.vars = map[string]string{"name": "name"}
	// Execute
	rsp := deleteCapability(capabilityCmd, nil).Self(nil, nil).(*resp)
	// Verify (external)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
	// Verify (internal)
	after := d.capRepo.All()
	c.Check(before, check.DeepEquals, after)
}
