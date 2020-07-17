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
	s.testGrubConfigContains(c, "grub.cfg", "snapd_recovery_mode")
}

func (s *grubAssetsTestSuite) TestGrubRecoveryConf(c *C) {
	s.testGrubConfigContains(c, "grub-recovery.cfg",
		"snapd_recovery_mode",
		"snapd_recovery_system",
	)
}

func (s *grubAssetsTestSuite) TestGrubCmdlineSnippet(c *C) {
	grubCfg := assets.Internal("grub.cfg")
	c.Assert(grubCfg, NotNil)
	c.Assert(bytes.HasPrefix(grubCfg, []byte("# Snapd-Boot-Config-Edition: 1")), Equals, true)
	// get a matching snippet
	snip := assets.SnippetForEdition("grub.cfg:static-cmdline", 1)
	c.Assert(snip, DeepEquals, []byte("console=ttyS0 console=tty1 panic=-1"))
	c.Assert(string(grubCfg), testutil.Contains, string(snip))

	// check recovery
	grubRecoveryCfg := assets.Internal("grub-recovery.cfg")
	c.Assert(grubRecoveryCfg, NotNil)
	c.Assert(bytes.HasPrefix(grubCfg, []byte("# Snapd-Boot-Config-Edition: 1")), Equals, true)
	recoverySnip := assets.SnippetForEdition("grub-recovery.cfg:static-cmdline", 1)
	c.Assert(snip, DeepEquals, []byte("console=ttyS0 console=tty1 panic=-1"))
	c.Assert(string(grubRecoveryCfg), testutil.Contains, string(recoverySnip))

}
