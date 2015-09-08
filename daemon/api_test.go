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
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/check.v1"

	"launchpad.net/snappy/release"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type apiSuite struct{}

var _ = check.Suite(&apiSuite{})

func (s *apiSuite) TestListIncludesAll(c *check.C) {
	// NOTE: there's probably a better/easier way of doing this
	// (patches welcome)

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

	exceptions := []string{"api"}
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

	rootCmd.GET(rootCmd, nil).Handler(rec, nil)
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

	v1Cmd.GET(v1Cmd, nil).Handler(rec, nil)
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
	c.Check(rsp.Metadata, check.DeepEquals, expected)
}
