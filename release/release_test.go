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

package release_test

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/release"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type ReleaseTestSuite struct {
}

var _ = Suite(&ReleaseTestSuite{})

func (s *ReleaseTestSuite) TestSetup(c *C) {
	c.Assert(release.Setup(c.MkDir()), IsNil)
	c.Check(release.String(), Equals, "16-core")
	rel := release.Get()
	c.Check(rel.Flavor, Equals, "core")
	c.Check(rel.Series, Equals, "16")
}

func (s *ReleaseTestSuite) TestOverride(c *C) {
	rel := release.Release{Flavor: "personal", Series: "10.06"}
	release.Override(rel)
	c.Check(release.String(), Equals, "10.06-personal")
	c.Check(release.Get(), DeepEquals, rel)
}

func makeMockLsbRelease(c *C) string {
	// FIXME: use AddCleanup here once available so that we
	//        can do release.SetLsbReleasePath() here directly
	mockLsbRelease := filepath.Join(c.MkDir(), "mock-lsb-release")
	s := `
DISTRIB_ID=Ubuntu
DISTRIB_RELEASE=18.09
DISTRIB_CODENAME=awsome
DISTRIB_DESCRIPTION=I'm not real!
`
	err := ioutil.WriteFile(mockLsbRelease, []byte(s), 0644)
	c.Assert(err, IsNil)

	return mockLsbRelease
}

func (a *ReleaseTestSuite) TestReadLsb(c *C) {
	reset := release.HackLsbReleasePath(makeMockLsbRelease(c))
	defer reset()

	lsb, err := release.ReadLsb()
	c.Assert(err, IsNil)
	c.Assert(lsb.ID, Equals, "Ubuntu")
	c.Assert(lsb.Release, Equals, "18.09")
	c.Assert(lsb.Codename, Equals, "awsome")
}

func (a *ReleaseTestSuite) TestReadLsbNotFound(c *C) {
	reset := release.HackLsbReleasePath("not-there")
	defer reset()

	_, err := release.ReadLsb()
	c.Assert(err, ErrorMatches, "cannot read lsb-release:.*")
}
