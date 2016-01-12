// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

package client_test

import (
	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
)

func (cs *clientSuite) TestClientPackagesCallsEndpoint(c *check.C) {
	_, _ = cs.cli.Packages()
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/1.0/packages")
}

func (cs *clientSuite) TestClientPackagesResultJSONHasNoPackages(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {}
	}`
	_, err := cs.cli.Packages()
	c.Check(err, check.ErrorMatches, `.*no packages`)
}

func (cs *clientSuite) TestClientPackagesInvalidPackagesJSON(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"packages": "not a list of packages"
		}
	}`
	_, err := cs.cli.Packages()
	c.Check(err, check.ErrorMatches, `.*failed to unmarshal packages.*`)
}

func (cs *clientSuite) TestClientPackages(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"packages": {
				"hello-world.canonical": {
					"description": "hello-world",
					"download_size": "22212",
					"icon": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
					"installed_size": "-1",
					"name": "hello-world",
					"origin": "canonical",
					"resource": "/1.0/packages/hello-world.canonical",
					"status": "not installed",
					"type": "app",
					"version": "1.0.18"
				}
			}
		}
	}`
	applications, err := cs.cli.Packages()
	c.Check(err, check.IsNil)
	c.Check(applications, check.DeepEquals, client.Packages{
		client.Package{
			Description:   "hello-world",
			DownloadSize:  22212,
			Icon:          "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
			InstalledSize: -1,
			Name:          "hello-world",
			Origin:        "canonical",
			Status:        client.StatusNotInstalled,
			Type:          client.TypeApp,
			Version:       "1.0.18",
		},
	})
}

func (cs *clientSuite) TestPackageIsInstalled(c *check.C) {
	isInstalledTests := map[string]bool{
		"": false,
		client.StatusNotInstalled: false,
		client.StatusInstalled:    true,
		client.StatusActive:       true,
	}

	for status, result := range isInstalledTests {
		pkg := client.Package{Status: status}
		c.Assert(pkg.IsInstalled(), check.Equals, result)
	}
}

func (cs *clientSuite) TestPackagesInstalled(c *check.C) {
	packages := client.Packages{
		client.Package{Name: "not installed", Status: client.StatusNotInstalled},
		client.Package{Name: "installed", Status: client.StatusInstalled},
		client.Package{Name: "removed", Status: client.StatusRemoved},
	}

	installed := packages.Installed()
	c.Assert(installed, check.HasLen, 1)
	c.Assert(installed[0].Name, check.Equals, "installed")
}

func (cs *clientSuite) TestPackageHasNameContaining(c *check.C) {
	pkg := client.Package{Name: "my package"}

	hasNameContainingTests := map[string]bool{
		".*":   false,
		"":     true,
		"pack": true,
		"MY":   true,
		"myp":  false,
	}

	for query, result := range hasNameContainingTests {
		c.Assert(pkg.HasNameContaining(query), check.Equals, result)
	}
}

func (cs *clientSuite) TestPackagesNamesContaining(c *check.C) {
	packages := client.Packages{
		client.Package{Name: "first app"},
		client.Package{Name: "second app"},
		client.Package{Name: "third app"},
	}

	matching := packages.NamesContaining("second")
	c.Assert(matching, check.HasLen, 1)
	c.Assert(matching[0].Name, check.Equals, "second app")
}

func (cs *clientSuite) TestPackageHasTypeInSet(c *check.C) {
	pkg := client.Package{Type: client.TypeFramework}

	hasTypeInSetTest := []struct {
		types  []string
		result bool
	}{
		{[]string{}, false},
		{[]string{client.TypeFramework}, true},
		{[]string{client.TypeApp, client.TypeFramework}, true},
		{[]string{client.TypeKernel}, false},
	}

	for _, tt := range hasTypeInSetTest {
		c.Assert(pkg.HasTypeInSet(tt.types), check.Equals, tt.result)
	}
}

func (cs *clientSuite) TestPackagesTypesInSet(c *check.C) {
	packages := client.Packages{
		client.Package{Name: "app", Type: client.TypeApp},
		client.Package{Name: "framework", Type: client.TypeFramework},
		client.Package{Name: "kernel", Type: client.TypeKernel},
	}

	matching := packages.TypesInSet([]string{client.TypeFramework})
	c.Assert(matching, check.HasLen, 1)
	c.Assert(matching[0].Name, check.Equals, "framework")
}

func (cs *clientSuite) TestPackagesSortByName(c *check.C) {
	packages := client.Packages{
		client.Package{Name: "c"},
		client.Package{Name: "b"},
		client.Package{Name: "a"},
	}

	sorted := packages.SortByName()

	c.Assert(sorted, check.DeepEquals, client.Packages{{Name: "a"}, {Name: "b"}, {Name: "c"}})
}

func (cs *clientSuite) TestPackagesChainedFilters(c *check.C) {
	packages := client.Packages{
		client.Package{Name: "app 1", Type: client.TypeApp, Status: client.StatusNotInstalled},
		client.Package{Name: "app 2", Type: client.TypeApp, Status: client.StatusInstalled},
		client.Package{Name: "framework 1", Type: client.TypeFramework, Status: client.StatusInstalled},
	}

	chainedResult := packages.Installed().
		TypesInSet([]string{client.TypeApp}).
		NamesContaining("app").
		SortByName()

	c.Assert(chainedResult, check.HasLen, 1)
	c.Assert(chainedResult[0].Name, check.Equals, "app 2")
}
