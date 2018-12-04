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

func (*apparmorSuite) TestAppArmorLevelTypeStringer(c *C) {
	c.Check(release.UnknownAppArmor.String(), Equals, "unknown")
	c.Check(release.NoAppArmor.String(), Equals, "none")
	c.Check(release.UnusableAppArmor.String(), Equals, "unusable")
	c.Check(release.PartialAppArmor.String(), Equals, "partial")
	c.Check(release.FullAppArmor.String(), Equals, "full")
	c.Check(release.AppArmorLevelType(42).String(), Equals, "AppArmorLevelType:42")
}

func (*apparmorSuite) TestMockAppArmorLevel(c *C) {
	for _, lvl := range []release.AppArmorLevelType{release.NoAppArmor, release.UnusableAppArmor, release.PartialAppArmor, release.FullAppArmor} {
		restore := release.MockAppArmorLevel(lvl)
		c.Check(release.AppArmorLevel(), Equals, lvl)
		c.Check(release.AppArmorSummary(), testutil.Contains, "mocked apparmor level: ")
		c.Check(release.AppArmorKernelFeatures(), DeepEquals, []string{"mocked-kernel-feature"})
		c.Check(release.AppArmorParserFeatures(), DeepEquals, []string{"mocked-parser-feature"})
		restore()
	}
}

func (*apparmorSuite) TestMockAppArmorFeatures(c *C) {
	restore := release.MockAppArmorFeatures([]string{}, []string{})
	c.Check(release.AppArmorLevel(), Equals, release.NoAppArmor)
	c.Check(release.AppArmorSummary(), Equals, "apparmor not enabled")
	c.Check(release.AppArmorKernelFeatures(), HasLen, 0)
	c.Check(release.AppArmorParserFeatures(), HasLen, 0)
	restore()

	restore = release.MockAppArmorFeatures([]string{"kernel-feature"}, []string{"parser-feature"})
	c.Check(release.AppArmorLevel(), Equals, release.UnusableAppArmor)
	c.Check(release.AppArmorSummary(), testutil.Contains, "apparmor_parser lacks essential features: unsafe")
	c.Check(release.AppArmorKernelFeatures(), DeepEquals, []string{"kernel-feature"})
	c.Check(release.AppArmorParserFeatures(), DeepEquals, []string{"parser-feature"})
	restore()

	// Unsafe is sufficient to get partial apparmor.
	restore = release.MockAppArmorFeatures([]string{"kernel-feature"}, []string{"unsafe"})
	c.Check(release.AppArmorLevel(), Equals, release.PartialAppArmor)
	c.Check(release.AppArmorSummary(), testutil.Contains, "apparmor is enabled but some kernel features are missing: ")
	c.Check(release.AppArmorKernelFeatures(), DeepEquals, []string{"kernel-feature"})
	c.Check(release.AppArmorParserFeatures(), DeepEquals, []string{"unsafe"})
	restore()

	// Unsafe is sufficient to get partial apparmor.
	restore = release.MockAppArmorFeatures(release.RequiredAppArmorKernelFeatures, release.RequiredAppArmorParserFeatures)
	c.Check(release.AppArmorLevel(), Equals, release.FullAppArmor)
	c.Check(release.AppArmorSummary(), Equals, "apparmor is enabled and all features are available")
	c.Check(release.AppArmorKernelFeatures(), DeepEquals, release.RequiredAppArmorKernelFeatures)
	c.Check(release.AppArmorParserFeatures(), DeepEquals, release.RequiredAppArmorParserFeatures)
	restore()
}

func (s *apparmorSuite) TestProbeAppArmorKernelFeatures(c *C) {
	restore := release.MockAppArmorFeaturesSysPath("/does/not/exists")
	c.Check(release.ProbeAppArmorKernelFeatures(), HasLen, 0)
	restore()

	d := c.MkDir()

	restore = release.MockAppArmorFeaturesSysPath(d)
	defer restore()
	c.Check(release.ProbeAppArmorKernelFeatures(), HasLen, 0)

	c.Assert(os.Mkdir(filepath.Join(d, "foo"), 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, "bar"), 0755), IsNil)
	c.Check(release.ProbeAppArmorKernelFeatures(), DeepEquals, []string{"bar", "foo"})
}

func (s *apparmorSuite) TestProbeAppArmorParserFeatures(c *C) {
	d := c.MkDir()

	var testcases = []struct {
		exit     string
		features []string
	}{
		{"exit 1", []string{}},
		{"exit 0", []string{"unsafe"}},
	}

	for _, t := range testcases {
		mockParserCmd := testutil.MockCommand(c, "apparmor_parser", fmt.Sprintf("cat > %s/stdin; %s", d, t.exit))
		defer mockParserCmd.Restore()
		restore := release.MockAppArmorParserSearchPath(mockParserCmd.BinDir())
		defer restore()

		features := release.ProbeAppArmorParserFeatures()
		c.Check(features, DeepEquals, t.features)
		c.Check(mockParserCmd.Calls(), DeepEquals, [][]string{{"apparmor_parser", "--preprocess"}})
		data, err := ioutil.ReadFile(filepath.Join(d, "stdin"))
		c.Assert(err, IsNil)
		c.Check(string(data), Equals, "profile snap-test {\n change_profile unsafe /**,\n}")
	}
}

func (s *apparmorSuite) TestInterfaceSystemKey(c *C) {
	d := c.MkDir()
	restore := release.MockAppArmorFeaturesSysPath(d)
	defer restore()
	c.Assert(os.MkdirAll(filepath.Join(d, "policy"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, "network"), 0755), IsNil)

	mockParserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer mockParserCmd.Restore()
	restore = release.MockAppArmorParserSearchPath(mockParserCmd.BinDir())
	defer restore()

	release.AssessAppArmor()

	c.Check(release.AppArmorKernelFeatures(), DeepEquals, []string{"network", "policy"})
	c.Check(release.AppArmorParserFeatures(), DeepEquals, []string{"unsafe"})

	// Deprecated API
	c.Check(release.AppArmorFeatures(), DeepEquals, []string{"network", "policy"})
}
