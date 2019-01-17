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
		features, err := release.AppArmorKernelFeatures()
		c.Check(err, IsNil)
		c.Check(features, DeepEquals, []string{"mocked-kernel-feature"})
		features, err = release.AppArmorParserFeatures()
		c.Check(err, IsNil)
		c.Check(features, DeepEquals, []string{"mocked-parser-feature"})
		restore()
	}
}

// Using MockAppArmorFeatures yields in apparmor assessment
func (*apparmorSuite) TestMockAppArmorFeatures(c *C) {
	// No apparmor in the kernel, apparmor is disabled.
	restore := release.MockAppArmorFeatures([]string{}, os.ErrNotExist, []string{}, nil)
	c.Check(release.AppArmorLevel(), Equals, release.NoAppArmor)
	c.Check(release.AppArmorSummary(), Equals, "apparmor not enabled")
	features, err := release.AppArmorKernelFeatures()
	c.Assert(err, Equals, os.ErrNotExist)
	c.Check(features, DeepEquals, []string{})
	features, err = release.AppArmorParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{})
	restore()

	// No apparmor_parser, apparmor is disabled.
	restore = release.MockAppArmorFeatures([]string{}, nil, []string{}, os.ErrNotExist)
	c.Check(release.AppArmorLevel(), Equals, release.NoAppArmor)
	c.Check(release.AppArmorSummary(), Equals, "apparmor_parser not found")
	features, err = release.AppArmorKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{})
	features, err = release.AppArmorParserFeatures()
	c.Assert(err, Equals, os.ErrNotExist)
	c.Check(features, DeepEquals, []string{})
	restore()

	// Complete kernel features but apparmor is unusable because of missing required parser features.
	restore = release.MockAppArmorFeatures(release.RequiredAppArmorKernelFeatures, nil, []string{}, nil)
	c.Check(release.AppArmorLevel(), Equals, release.UnusableAppArmor)
	c.Check(release.AppArmorSummary(), Equals, "apparmor_parser is available but required parser features are missing: unsafe")
	features, err = release.AppArmorKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, release.RequiredAppArmorKernelFeatures)
	features, err = release.AppArmorParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{})
	restore()

	// Complete parser features but apparmor is unusable because of missing required kernel features.
	// The dummy feature is there to pretend that apparmor in the kernel is not entirely disabled.
	restore = release.MockAppArmorFeatures([]string{"dummy-feature"}, nil, release.RequiredAppArmorParserFeatures, nil)
	c.Check(release.AppArmorLevel(), Equals, release.UnusableAppArmor)
	c.Check(release.AppArmorSummary(), Equals, "apparmor is enabled but required kernel features are missing: file")
	features, err = release.AppArmorKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"dummy-feature"})
	features, err = release.AppArmorParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, release.RequiredAppArmorParserFeatures)
	restore()

	// Required kernel and parser features available, some optional features are missing though.
	restore = release.MockAppArmorFeatures(release.RequiredAppArmorKernelFeatures, nil, release.RequiredAppArmorParserFeatures, nil)
	c.Check(release.AppArmorLevel(), Equals, release.PartialAppArmor)
	c.Check(release.AppArmorSummary(), Equals, "apparmor is enabled but some kernel features are missing: caps, dbus, domain, mount, namespaces, network, ptrace, signal")
	features, err = release.AppArmorKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, release.RequiredAppArmorKernelFeatures)
	features, err = release.AppArmorParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, release.RequiredAppArmorParserFeatures)
	restore()

	// Preferred kernel and parser features available.
	restore = release.MockAppArmorFeatures(release.PreferredAppArmorKernelFeatures, nil, release.PreferredAppArmorParserFeatures, nil)
	c.Check(release.AppArmorLevel(), Equals, release.FullAppArmor)
	c.Check(release.AppArmorSummary(), Equals, "apparmor is enabled and all features are available")
	features, err = release.AppArmorKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, release.PreferredAppArmorKernelFeatures)
	features, err = release.AppArmorParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, release.PreferredAppArmorParserFeatures)
	restore()
}

func (s *apparmorSuite) TestProbeAppArmorKernelFeatures(c *C) {
	d := c.MkDir()

	// Pretend that apparmor kernel features directory doesn't exist.
	restore := release.MockAppArmorFeaturesSysPath(filepath.Join(d, "non-existent"))
	defer restore()
	features, err := release.ProbeAppArmorKernelFeatures()
	c.Assert(os.IsNotExist(err), Equals, true)
	c.Check(features, DeepEquals, []string{})

	// Pretend that apparmor kernel features directory exists but is empty.
	restore = release.MockAppArmorFeaturesSysPath(d)
	defer restore()
	features, err = release.ProbeAppArmorKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{})

	// Pretend that apparmor kernel features directory contains some entries.
	c.Assert(os.Mkdir(filepath.Join(d, "foo"), 0755), IsNil)
	c.Assert(os.Mkdir(filepath.Join(d, "bar"), 0755), IsNil)
	features, err = release.ProbeAppArmorKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"bar", "foo"})
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

		features, err := release.ProbeAppArmorParserFeatures()
		c.Assert(err, IsNil)
		c.Check(features, DeepEquals, t.features)
		c.Check(mockParserCmd.Calls(), DeepEquals, [][]string{{"apparmor_parser", "--preprocess"}})
		data, err := ioutil.ReadFile(filepath.Join(d, "stdin"))
		c.Assert(err, IsNil)
		c.Check(string(data), Equals, "profile snap-test {\n change_profile unsafe /**,\n}")
	}

	// Pretend that we just don't have apparmor_parser at all.
	restore := release.MockAppArmorParserSearchPath(c.MkDir())
	defer restore()
	features, err := release.ProbeAppArmorParserFeatures()
	c.Check(err, Equals, os.ErrNotExist)
	c.Check(features, DeepEquals, []string{})
}

func (s *apparmorSuite) TestInterfaceSystemKey(c *C) {
	release.FreshAppArmorAssessment()

	d := c.MkDir()
	restore := release.MockAppArmorFeaturesSysPath(d)
	defer restore()
	c.Assert(os.MkdirAll(filepath.Join(d, "policy"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, "network"), 0755), IsNil)

	mockParserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer mockParserCmd.Restore()
	restore = release.MockAppArmorParserSearchPath(mockParserCmd.BinDir())
	defer restore()

	release.AppArmorLevel()

	features, err := release.AppArmorKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"network", "policy"})
	features, err = release.AppArmorParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"unsafe"})
}

func (s *apparmorSuite) TestAppArmorParserMtime(c *C) {
	// Pretend that we have apparmor_parser.
	mockParserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer mockParserCmd.Restore()
	restore := release.MockAppArmorParserSearchPath(mockParserCmd.BinDir())
	defer restore()
	mtime := release.AppArmorParserMtime()
	fi, err := os.Stat(filepath.Join(mockParserCmd.BinDir(), "apparmor_parser"))
	c.Assert(err, IsNil)
	c.Check(mtime, Equals, fi.ModTime().Unix())

	// Pretend that we don't have apparmor_parser.
	restore = release.MockAppArmorParserSearchPath(c.MkDir())
	defer restore()
	mtime = release.AppArmorParserMtime()
	c.Check(mtime, Equals, int64(0))
}

func (s *apparmorSuite) TestFeaturesProbedOnce(c *C) {
	release.FreshAppArmorAssessment()

	d := c.MkDir()
	restore := release.MockAppArmorFeaturesSysPath(d)
	defer restore()
	c.Assert(os.MkdirAll(filepath.Join(d, "policy"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, "network"), 0755), IsNil)

	mockParserCmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer mockParserCmd.Restore()
	restore = release.MockAppArmorParserSearchPath(mockParserCmd.BinDir())
	defer restore()

	features, err := release.AppArmorKernelFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"network", "policy"})
	features, err = release.AppArmorParserFeatures()
	c.Assert(err, IsNil)
	c.Check(features, DeepEquals, []string{"unsafe"})

	// this makes probing fails but is not done again
	err = os.RemoveAll(d)
	c.Assert(err, IsNil)

	_, err = release.AppArmorKernelFeatures()
	c.Assert(err, IsNil)

	// this makes probing fails but is not done again
	err = os.RemoveAll(mockParserCmd.BinDir())
	c.Assert(err, IsNil)

	_, err = release.AppArmorParserFeatures()
	c.Assert(err, IsNil)
}
