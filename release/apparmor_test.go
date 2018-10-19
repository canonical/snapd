// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type apparmorSuite struct{}

var _ = Suite(&apparmorSuite{})

func (s *apparmorSuite) TestMockAppArmorLevel(c *C) {
	for _, lvl := range []release.AppArmorLevelType{release.NoAppArmor, release.PartialAppArmor, release.FullAppArmor} {
		restore := release.MockAppArmorLevel(lvl)
		defer restore()
		c.Check(release.AppArmorLevel(), Equals, lvl)
	}
}

func (s *apparmorSuite) TestProbeAppArmorNoAppArmor(c *C) {
	restore := release.MockAppArmorFeaturesSysPath("/does/not/exists")
	defer restore()

	level, summary := release.ProbeAppArmor()
	c.Check(level, Equals, release.NoAppArmor)
	c.Check(summary, Equals, "apparmor not enabled")
}

func (s *apparmorSuite) TestProbeAppArmorPartialAppArmor(c *C) {
	fakeSysPath := c.MkDir()
	restore := release.MockAppArmorFeaturesSysPath(fakeSysPath)
	defer restore()

	level, summary := release.ProbeAppArmor()
	c.Check(level, Equals, release.PartialAppArmor)
	c.Check(summary, Equals, "apparmor is enabled but some features are missing: caps, dbus, domain, file, mount, namespaces, network, ptrace, signal")
}

func (s *apparmorSuite) TestProbeAppArmorFullAppArmor(c *C) {
	fakeSysPath := c.MkDir()
	restore := release.MockAppArmorFeaturesSysPath(fakeSysPath)
	defer restore()

	for _, feature := range release.RequiredAppArmorFeatures {
		err := os.Mkdir(filepath.Join(fakeSysPath, feature), 0755)
		c.Assert(err, IsNil)
	}

	level, summary := release.ProbeAppArmor()
	c.Check(level, Equals, release.FullAppArmor)
	c.Check(summary, Equals, "apparmor is enabled and all features are available")
}

func (s *apparmorSuite) TestInterfaceSystemKey(c *C) {
	fakeSysPath := c.MkDir()
	restore := release.MockAppArmorFeaturesSysPath(fakeSysPath)
	defer restore()
	err := os.MkdirAll(filepath.Join(fakeSysPath, "policy"), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(fakeSysPath, "network"), 0755)
	c.Assert(err, IsNil)

	features := release.AppArmorFeatures()
	c.Check(features, DeepEquals, []string{"network", "policy"})
}

func (s *apparmorSuite) TestAppArmorParserFeatures(c *C) {
	tmpdir := c.MkDir()

	var testcases = []struct {
		exit     string
		features []string
	}{
		{"exit 1", []string{}},
		{"exit 0", []string{"unsafe"}},
	}

	for _, t := range testcases {
		mockApparmorParser := testutil.MockCommand(c, "apparmor_parser", fmt.Sprintf("cat > %s/stdin; %s", tmpdir, t.exit))
		defer mockApparmorParser.Restore()
		restore := release.MockAppArmorParserSearchPath(os.Getenv("PATH"))
		defer restore()

		features := release.AppArmorParserFeatures()
		c.Check(features, DeepEquals, t.features)
		c.Check(mockApparmorParser.Calls(), DeepEquals, [][]string{{"apparmor_parser", "--preprocess"}})
		inp, err := ioutil.ReadFile(filepath.Join(tmpdir, "stdin"))
		c.Assert(err, IsNil)
		c.Check(string(inp), Equals, "profile snap-test {\n change_profile unsafe /**,\n}")
	}
}
