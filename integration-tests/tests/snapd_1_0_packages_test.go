// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015 Canonical Ltd
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

package tests

import (
	"os"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&snapd10PackagesTestSuite{})

type pkgsResponse struct {
	Result pkgContainer
	response
}

type pkgContainer struct {
	Packages pkgItems
	Paging   map[string]interface{}
}

type pkgItems map[string]pkgItem

type pkgItem struct {
	Description   string
	DownloadSize  string `json:"download_size"`
	Icon          string
	InstalledSize string `json:"installed_size"`
	Name          string
	Origin        string
	Resource      string
	Status        string
	Type          string
	Vendor        string
	Version       string
}

type snapd10PackagesTestSuite struct {
	snapdTestSuite
	snapPath string
}

func (s *snapd10PackagesTestSuite) SetUpTest(c *check.C) {
	s.snapdTestSuite.SetUpTest(c)
	var err error
	s.snapPath, err = build.LocalSnap(c, data.BasicConfigSnapName)
	c.Assert(err, check.IsNil)
}

func (s *snapd10PackagesTestSuite) TearDownTest(c *check.C) {
	s.snapdTestSuite.TearDownTest(c)
	os.Remove(s.snapPath)
	common.RemoveSnap(c, data.BasicConfigSnapName)
}

func (s *snapd10PackagesTestSuite) resource() string {
	return baseURL + "/1.0/packages"
}

func (s *snapd10PackagesTestSuite) TestResource(c *check.C) {
	exerciseAPI(c, s)
}

func (s *snapd10PackagesTestSuite) getInteractions() apiInteractions {
	return []apiInteraction{{
		responseObject: &pkgsResponse{}}}
}

func (s *snapd10PackagesTestSuite) postInteractions() apiInteractions {
	return []apiInteraction{{
		payload:     s.snapPath,
		waitPattern: `(?U){.*,"status":"active".*"status":"OK","status_code":200,"type":"sync"}`,
		waitFunction: func() (string, error) {
			output, err := makeRequest(&requestOptions{
				resource: s.resource() + "/" + data.BasicConfigSnapName + ".sideload",
				verb:     "GET",
			})
			return string(output), err
		}}}
}

func (s *snapd10PackagesTestSuite) putInteractions() apiInteractions {
	return []apiInteraction{{
		// this payload is adapted to the httpie client
		payload:     data.BasicConfigSnapName + `.sideload:="{key: value}"`,
		waitPattern: `(?Us){"result":.*` + data.BasicConfigSnapName + `.*key: value.*","status":"OK","status_code":200,"type":"sync"}`,
		waitFunction: func() (string, error) {
			output, err := makeRequest(&requestOptions{
				resource: s.resource() + "/" + data.BasicConfigSnapName + ".sideload/config",
				verb:     "GET",
			})
			return string(output), err
		}}}
}
