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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/check.v1"

	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/release"
	"launchpad.net/snappy/snappy"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

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

func (s *apiSuite) muxVars(*http.Request) map[string]string {
	return s.vars
}

func (s *apiSuite) SetUpSuite(c *check.C) {
	newRepo = func() metarepo {
		return s
	}
	newLocalRepo = newRepo
	newRemoteRepo = newRepo
	muxVars = s.muxVars
}

func (s *apiSuite) TearDownSuite(c *check.C) {
	newLocalRepo = nil
	newRemoteRepo = nil
	newRepo = nil
	muxVars = nil
}

func (s *apiSuite) SetUpTest(c *check.C) {
	s.parts = nil
	s.err = nil
	s.vars = nil
}

func (s *apiSuite) TestPackageInfoOneIntegration(c *check.C) {
	d := New()
	d.addRoutes()

	s.vars = map[string]string{"name": "foo", "origin": "bar"}

	s.parts = []snappy.Part{&tP{
		name:          "foo",
		version:       "v1",
		description:   "description",
		origin:        "bar",
		vendor:        "a vendor",
		isInstalled:   true,
		isActive:      true,
		icon:          iconPath + "icon.png",
		_type:         "a type",
		installedSize: 42,
		downloadSize:  2,
	}}
	rsp, ok := getPackageInfo(packageCmd, nil).(*resp)
	c.Assert(ok, check.Equals, true)

	expected := &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Metadata: map[string]string{
			"name":           "foo",
			"version":        "v1",
			"description":    "description",
			"origin":         "bar",
			"vendor":         "a vendor",
			"status":         "active",
			"icon":           iconPrefix + "icon.png",
			"type":           "a type",
			"download_size":  "2",
			"installed_size": "42",
			"resource":       "/1.0/packages/foo.bar",
		},
	}

	c.Check(rsp, check.DeepEquals, expected)
}

func (s *apiSuite) TestPackageInfoBadReq(c *check.C) {
	// no muxVars; can't really happen afaict
	c.Check(getPackageInfo(packageCmd, nil), check.Equals, BadRequest)
}

func (s *apiSuite) TestPackageInfoNotFound(c *check.C) {
	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	s.err = snappy.ErrPackageNotFound

	c.Check(getPackageInfo(packageCmd, nil), check.Equals, NotFound)
}

func (s *apiSuite) TestPackageInfoNoneFound(c *check.C) {
	s.vars = map[string]string{"name": "foo", "origin": "bar"}

	c.Check(getPackageInfo(packageCmd, nil), check.Equals, NotFound)
}

func (s *apiSuite) TestPackageInfoWeirdDetails(c *check.C) {
	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	s.err = errors.New("weird")
	c.Check(getPackageInfo(packageCmd, nil), check.Equals, InternalError)
}

func (s *apiSuite) TestPackageInfoWeirdRoute(c *check.C) {
	// can't really happen

	d := New()
	d.addRoutes()

	// use the wrong command to force the issue
	wrongCmd := &Command{Path: "/{what}", d: d}
	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	s.parts = []snappy.Part{&tP{name: "foo"}}
	c.Check(getPackageInfo(wrongCmd, nil), check.Equals, InternalError)
}

func (s *apiSuite) TestPackageInfoBadRoute(c *check.C) {
	// can't really happen, v2

	d := New()
	d.addRoutes()

	// get the route and break it
	route := d.router.Get(packageCmd.Path)
	c.Assert(route.Name("foo").GetError(), check.NotNil)

	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	s.parts = []snappy.Part{&tP{name: "foo"}}
	c.Check(getPackageInfo(packageCmd, nil), check.Equals, InternalError)
}

func (s *apiSuite) TestParts2Map(c *check.C) {
	parts := []snappy.Part{&tP{
		name:          "foo",
		version:       "v1",
		description:   "description",
		origin:        "bar",
		vendor:        "a vendor",
		isInstalled:   true,
		isActive:      true,
		icon:          "icon.png",
		_type:         "a type",
		installedSize: 42,
		downloadSize:  2,
	}}

	resource := "/1.0/packages/foo.bar"

	expected := map[string]string{
		"name":           "foo",
		"version":        "v1",
		"description":    "description",
		"origin":         "bar",
		"vendor":         "a vendor",
		"status":         "active",
		"icon":           "icon.png",
		"type":           "a type",
		"download_size":  "2",
		"installed_size": "42",
		"resource":       resource,
	}

	c.Check(parts2map(parts, resource), check.DeepEquals, expected)
}

func (s *apiSuite) TestParts2MapState(c *check.C) {
	c.Check(parts2map([]snappy.Part{&tP{}}, "")["status"], check.Equals, "not installed")
	c.Check(parts2map([]snappy.Part{&tP{isInstalled: true}}, "")["status"], check.Equals, "installed")
	c.Check(parts2map([]snappy.Part{&tP{isInstalled: true, isActive: true}}, "")["status"], check.Equals, "active")
	// TODO: more statuses
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
		"api",
		"findServices",
		"maxReadBuflen",
		"muxVars",
		"newLocalRepo",
		"newRemoteRepo",
		"newRepo",
		"newSnap",
		"pkgActionDispatch",
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
	c.Check(rsp.Metadata, check.DeepEquals, expected)
}

func (s *apiSuite) TestV1(c *check.C) {
	// check it only does GET
	c.Check(v1Cmd.PUT, check.IsNil)
	c.Check(v1Cmd.POST, check.IsNil)
	c.Check(v1Cmd.DELETE, check.IsNil)
	c.Assert(v1Cmd.GET, check.NotNil)

	rec := httptest.NewRecorder()
	c.Check(v1Cmd.Path, check.Equals, "/1.0")

	// set up release
	root := c.MkDir()
	d := filepath.Join(root, "etc", "system-image")
	c.Assert(os.MkdirAll(d, 0755), check.IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d, "channel.ini"), []byte("[service]\nchannel: ubuntu-flavor/release/channel"), 0644), check.IsNil)
	c.Assert(release.Setup(root), check.IsNil)

	v1Cmd.GET(v1Cmd, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/json")

	expected := map[string]interface{}{
		"flavor":          "flavor",
		"release":         "release",
		"default_channel": "channel",
		"api_compat":      "0",
	}
	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Metadata, check.DeepEquals, expected)
}

func (s *apiSuite) TestPackagesInfoOnePerIntegration(c *check.C) {
	d := New()
	d.addRoutes()

	req, err := http.NewRequest("GET", "/1.0/packages", nil)
	c.Assert(err, check.IsNil)

	s.parts = []snappy.Part{
		&tP{name: "foo", version: "v1", origin: "bar"},
		&tP{name: "bar", version: "v2", origin: "baz"},
		&tP{name: "baz", version: "v3", origin: "qux"},
		&tP{name: "qux", version: "v4", origin: "mip"},
	}
	rsp, ok := getPackagesInfo(packagesCmd, req).(*resp)
	c.Assert(ok, check.Equals, true)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Metadata, check.NotNil)

	meta, ok := rsp.Metadata.(map[string]interface{})
	c.Assert(ok, check.Equals, true)
	c.Assert(meta, check.NotNil)
	c.Check(meta["paging"], check.DeepEquals, map[string]interface{}{"pages": 1, "page": 1, "count": len(s.parts)})

	packages, ok := meta["packages"].(map[string]map[string]string)
	c.Assert(ok, check.Equals, true)
	c.Check(packages, check.NotNil)
	c.Check(packages, check.HasLen, len(s.parts))

	for _, part := range s.parts {
		part := part.(*tP)
		qn := part.name + "." + part.origin
		got := packages[qn]
		c.Assert(got, check.NotNil, check.Commentf(qn))
		c.Check(got["name"], check.Equals, part.name)
		c.Check(got["version"], check.Equals, part.version)
		c.Check(got["origin"], check.Equals, part.origin)
	}
}

func (s *apiSuite) TestGetOpInfoIntegration(c *check.C) {
	d := New()
	d.addRoutes()

	s.vars = map[string]string{"uuid": "42"}
	c.Check(getOpInfo(operationCmd, nil), check.Equals, NotFound)

	ch := make(chan struct{})

	t := d.AddTask(func() interface{} {
		ch <- struct{}{}
		return "hello"
	})

	id := t.UUID()
	s.vars = map[string]string{"uuid": id}

	rsp := getOpInfo(operationCmd, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Metadata, check.DeepEquals, map[string]interface{}{
		"resource":   "/1.0/operations/" + id,
		"status":     TaskRunning,
		"may_cancel": false,
		"created_at": FormatTime(t.CreatedAt()),
		"updated_at": FormatTime(t.UpdatedAt()),
		"metadata":   nil,
	})
	tf1 := t.UpdatedAt().UTC().UnixNano()

	<-ch
	time.Sleep(time.Millisecond)

	rsp = getOpInfo(operationCmd, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Metadata, check.DeepEquals, map[string]interface{}{
		"resource":   "/1.0/operations/" + id,
		"status":     TaskSucceeded,
		"may_cancel": false,
		"created_at": FormatTime(t.CreatedAt()),
		"updated_at": FormatTime(t.UpdatedAt()),
		"metadata":   "hello",
	})

	tf2 := t.UpdatedAt().UTC().UnixNano()

	c.Check(tf1 < tf2, check.Equals, true)
}

func (s *apiSuite) TestPostPackageBadRequest(c *check.C) {
	d := New()
	d.addRoutes()

	s.vars = map[string]string{"uuid": "42"}
	c.Check(getOpInfo(operationCmd, nil), check.Equals, NotFound)

	buf := bytes.NewBufferString(`hello`)
	req, err := http.NewRequest("POST", "/1.0/packages/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postPackage(packageCmd, req).(*resp)

	c.Check(rsp, check.DeepEquals, &resp{
		Type:   ResponseTypeError,
		Status: http.StatusBadRequest,
	})

}

func (s *apiSuite) TestPostPackageBadAction(c *check.C) {
	d := New()
	d.addRoutes()

	s.vars = map[string]string{"uuid": "42"}
	c.Check(getOpInfo(operationCmd, nil), check.Equals, NotFound)

	buf := bytes.NewBufferString(`{"action": "potato"}`)
	req, err := http.NewRequest("POST", "/1.0/packages/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postPackage(packageCmd, req).(*resp)

	c.Check(rsp, check.DeepEquals, &resp{
		Type:   ResponseTypeError,
		Status: http.StatusBadRequest,
	})

}

func (s *apiSuite) TestPostPackage(c *check.C) {
	d := New()
	d.addRoutes()

	s.vars = map[string]string{"uuid": "42"}
	c.Check(getOpInfo(operationCmd, nil), check.Equals, NotFound)

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
	m := rsp.Metadata.(map[string]interface{})
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
	c.Check(task.Metadata(), check.Equals, "hi")
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

func (s *apiSuite) TestPackageGetConfig(c *check.C) {
	d := New()
	d.addRoutes()

	req, err := http.NewRequest("GET", "/1.0/packages/foo.bar/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	configStr := "some: config"
	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	s.parts = []snappy.Part{
		&tP{name: "foo", version: "v1", origin: "bar", isActive: true, config: configStr},
		&tP{name: "bar", version: "v2", origin: "baz", isActive: true},
		&tP{name: "baz", version: "v3", origin: "qux", isActive: true},
		&tP{name: "qux", version: "v4", origin: "mip", isActive: true},
	}

	rsp := packageConfig(packagesCmd, req).(*resp)

	c.Check(rsp, check.DeepEquals, &resp{
		Type:     ResponseTypeSync,
		Status:   http.StatusOK,
		Metadata: configStr,
	})
}

func (s *apiSuite) TestPackagePutConfig(c *check.C) {
	d := New()
	d.addRoutes()

	newConfigStr := "some other config"
	req, err := http.NewRequest("PUT", "/1.0/packages/foo.bar/config", bytes.NewBufferString(newConfigStr))
	c.Assert(err, check.IsNil)

	configStr := "some: config"
	s.vars = map[string]string{"name": "foo", "origin": "bar"}
	s.parts = []snappy.Part{
		&tP{name: "foo", version: "v1", origin: "bar", isActive: true, config: configStr},
		&tP{name: "bar", version: "v2", origin: "baz", isActive: true},
		&tP{name: "baz", version: "v3", origin: "qux", isActive: true},
		&tP{name: "qux", version: "v4", origin: "mip", isActive: true},
	}

	rsp := packageConfig(packagesCmd, req).(*resp)

	c.Check(rsp, check.DeepEquals, &resp{
		Type:     ResponseTypeSync,
		Status:   http.StatusOK,
		Metadata: newConfigStr,
	})
}

func (s *apiSuite) TestPackageServiceGet(c *check.C) {
	d := New()
	d.addRoutes()

	findServices = func(string, string, progress.Meter) (snappy.ServiceActor, error) {
		return &tSA{ssout: []*snappy.PackageServiceStatus{{ServiceName: "svc"}}}, nil
	}

	req, err := http.NewRequest("GET", "/1.0/packages/foo.bar/services", nil)
	c.Assert(err, check.IsNil)

	s.parts = []snappy.Part{
		&tP{name: "foo", version: "v1", origin: "bar", isActive: true,
			svcYamls: []snappy.ServiceYaml{{Name: "svc"}},
		},
	}
	s.vars = map[string]string{"name": "foo", "origin": "bar"} // NB: no service specified

	rsp := packageService(packageSvcsCmd, req).(*resp)
	c.Assert(rsp, check.NotNil)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)

	m := rsp.Metadata.(map[string]*svcDesc)
	c.Assert(m["svc"], check.DeepEquals, &svcDesc{
		Op:     "status",
		Spec:   &snappy.ServiceYaml{Name: "svc"},
		Status: &snappy.PackageServiceStatus{ServiceName: "svc"},
	})
}

func (s *apiSuite) TestPackageServicePut(c *check.C) {
	d := New()
	d.addRoutes()

	findServices = func(string, string, progress.Meter) (snappy.ServiceActor, error) {
		return &tSA{ssout: []*snappy.PackageServiceStatus{{ServiceName: "svc"}}}, nil
	}

	buf := bytes.NewBufferString(`{"action": "stop"}`)
	req, err := http.NewRequest("PUT", "/1.0/packages/foo.bar/services", buf)
	c.Assert(err, check.IsNil)

	s.parts = []snappy.Part{
		&tP{name: "foo", version: "v1", origin: "bar", isActive: true,
			svcYamls: []snappy.ServiceYaml{{Name: "svc"}},
		},
	}
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
