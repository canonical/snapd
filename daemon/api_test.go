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
	"encoding/json"
	"errors"
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

func (s *apiSuite) Details(string) ([]snappy.Part, error) {
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
	muxVars = s.muxVars
}

func (s *apiSuite) SetUpTest(c *check.C) {
	s.parts = nil
	s.err = nil
	s.vars = nil
}

func (s *apiSuite) TestPackageInfoOneIntegration(c *check.C) {
	d := New()
	d.addRoutes()

	s.vars = map[string]string{"package": "foo"}

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
	s.vars = map[string]string{"package": "foo"}
	s.err = snappy.ErrPackageNotFound

	c.Check(getPackageInfo(packageCmd, nil), check.Equals, NotFound)
}

func (s *apiSuite) TestPackageInfoNoneFound(c *check.C) {
	s.vars = map[string]string{"package": "foo"}

	c.Check(getPackageInfo(packageCmd, nil), check.Equals, NotFound)
}

func (s *apiSuite) TestPackageInfoWeirdDetails(c *check.C) {
	s.vars = map[string]string{"package": "foo"}
	s.err = errors.New("weird")
	c.Check(getPackageInfo(packageCmd, nil), check.Equals, InternalError)
}

func (s *apiSuite) TestPackageInfoMixedResults(c *check.C) {
	s.vars = map[string]string{"package": "foo"}
	s.parts = []snappy.Part{&tP{name: "foo"}, &tP{name: "bar"}}
	c.Check(getPackageInfo(packageCmd, nil), check.Equals, InternalError)
}

func (s *apiSuite) TestPackageInfoWeirdRoute(c *check.C) {
	// can't really happen

	d := New()
	d.addRoutes()

	// use the wrong command to force the issue
	wrongCmd := &Command{Path: "/{what}", d: d}
	s.vars = map[string]string{"package": "foo"}
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

	s.vars = map[string]string{"package": "foo"}
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

	exceptions := []string{"api", "newRepo", "newLocalRepo", "newRemoteRepo", "muxVars"}
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
		"resource": "/1.0/operations/" + id,
		"status":   TaskRunning,
		"metadata": nil,
	})

	<-ch
	time.Sleep(time.Millisecond)

	rsp = getOpInfo(operationCmd, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Metadata, check.DeepEquals, map[string]interface{}{
		"resource": "/1.0/operations/" + id,
		"status":   TaskSucceeded,
		"metadata": "hello",
	})

}
