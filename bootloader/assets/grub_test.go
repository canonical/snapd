// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package assets_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type grubAssetsTestSuite struct{}

var _ = Suite(&grubAssetsTestSuite{})

func (s *grubAssetsTestSuite) testGrubConfigContains(c *C, name string, keys ...string) {
	a := assets.Internal(name)
	c.Assert(a, NotNil)
	as := string(a)
	for _, canary := range keys {
		c.Assert(as, testutil.Contains, canary)
	}
	idx := bytes.IndexRune(a, '\n')
	c.Assert(idx, Not(Equals), -1)
	c.Assert(string(a[:idx]), Equals, "# Snapd-Boot-Config-Edition: 1")
}

func (s *grubAssetsTestSuite) TestGrubConf(c *C) {
	s.testGrubConfigContains(c, "grub.cfg",
		"snapd_recovery_mode",
		"set snapd_static_cmdline_args='console=ttyS0 console=tty1 panic=-1'",
	)
}

func (s *grubAssetsTestSuite) TestGrubRecoveryConf(c *C) {
	s.testGrubConfigContains(c, "grub-recovery.cfg",
		"snapd_recovery_mode",
		"snapd_recovery_system",
		"set snapd_static_cmdline_args='console=ttyS0 console=tty1 panic=-1'",
	)
}

func (s *grubAssetsTestSuite) TestGrubCmdlineSnippetEditions(c *C) {
	for _, tc := range []struct {
		asset   string
		edition uint
		snip    []byte
	}{
		{"grub.cfg:static-cmdline", 1, []byte("console=ttyS0 console=tty1 panic=-1")},
		{"grub-recovery.cfg:static-cmdline", 1, []byte("console=ttyS0 console=tty1 panic=-1")},
	} {
		snip := assets.SnippetForEdition(tc.asset, tc.edition)
		c.Assert(snip, NotNil)
		c.Check(snip, DeepEquals, tc.snip)
	}
}

func (s *grubAssetsTestSuite) TestGrubCmdlineSnippetCrossCheck(c *C) {
	for _, tc := range []struct {
		asset   string
		snippet string
		edition uint
		content []byte
		pattern string
	}{
		{
			asset: "grub.cfg", snippet: "grub.cfg:static-cmdline", edition: 1,
			content: []byte("console=ttyS0 console=tty1 panic=-1"),
			pattern: "set snapd_static_cmdline_args='%s'\n",
		},
		{
			asset: "grub-recovery.cfg", snippet: "grub-recovery.cfg:static-cmdline", edition: 1,
			content: []byte("console=ttyS0 console=tty1 panic=-1"),
			pattern: "set snapd_static_cmdline_args='%s'\n",
		},
	} {
		grubCfg := assets.Internal(tc.asset)
		c.Assert(grubCfg, NotNil)
		prefix := fmt.Sprintf("# Snapd-Boot-Config-Edition: %d", tc.edition)
		c.Assert(bytes.HasPrefix(grubCfg, []byte(prefix)), Equals, true)
		// get a matching snippet
		snip := assets.SnippetForEdition(tc.snippet, tc.edition)
		c.Assert(snip, NotNil)
		c.Assert(snip, DeepEquals, tc.content)
		c.Assert(string(grubCfg), testutil.Contains, fmt.Sprintf(tc.pattern, string(snip)))
	}
}

func (s *grubAssetsTestSuite) TestGrubAssetsWereRegenerated(c *C) {
	for _, tc := range []struct {
		asset string
		file  string
	}{
		{"grub.cfg", "data/grub.cfg"},
		{"grub-recovery.cfg", "data/grub-recovery.cfg"},
	} {
		assetData := assets.Internal(tc.asset)
		c.Assert(assetData, NotNil)
		data, err := ioutil.ReadFile(tc.file)
		c.Assert(err, IsNil)
		c.Check(assetData, DeepEquals, data, Commentf("asset %q has not been updated", tc.asset))
	}
}
