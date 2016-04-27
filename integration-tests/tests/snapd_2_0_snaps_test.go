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

var _ = check.Suite(&snapd20SnapsTestSuite{})

type pkgsResponse struct {
	Result pkgItems
	response
	Paging            map[string]interface{}
	Sources           interface{}
	SuggestedCurrency string `json:"suggested-currency"`
}

type pkgItems []pkgItem

type pkgItem struct {
	Description   string
	DownloadSize  int64 `json:"download-size"`
	Icon          string
	InstalledSize int64 `json:"installed-size"`
	Name          string
	Developer     string
	Resource      string
	Status        string
	Type          string
	Vendor        string
	Version       string
}

type snapd20SnapsTestSuite struct {
	snapdTestSuite
	snapPath string
}

func (s *snapd20SnapsTestSuite) SetUpTest(c *check.C) {
	s.snapdTestSuite.SetUpTest(c)
	var err error
	s.snapPath, err = build.LocalSnap(c, data.BasicConfigSnapName)
	c.Assert(err, check.IsNil)
}

func (s *snapd20SnapsTestSuite) TearDownTest(c *check.C) {
	s.snapdTestSuite.TearDownTest(c)
	os.Remove(s.snapPath)
	common.RemoveSnap(c, data.BasicConfigSnapName)
}

func (s *snapd20SnapsTestSuite) resource() string {
	return baseURL + "/v2/snaps"
}

func (s *snapd20SnapsTestSuite) TestResource(c *check.C) {
	exerciseAPI(c, s)
}

func (s *snapd20SnapsTestSuite) getInteractions() apiInteractions {
	return []apiInteraction{{
		responseObject: &pkgsResponse{}}}
}

func (s *snapd20SnapsTestSuite) postInteractions() apiInteractions {
	return []apiInteraction{{
		payload:     s.snapPath,
		waitPattern: `(?U){"type":"sync","status-code":200,"status":"OK","result":{.*,"status":"active",.*}`,
		waitFunction: func() (string, error) {
			output, err := makeRequest(&requestOptions{
				resource: s.resource() + "/" + data.BasicConfigSnapName,
				verb:     "GET",
			})
			return string(output), err
		}}}
}
