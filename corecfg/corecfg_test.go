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

package corecfg_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/corecfg"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type coreCfgSuite struct{}

var _ = Suite(&coreCfgSuite{})

func (s *coreCfgSuite) TestConfigureErrorsOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	err := corecfg.Run()
	c.Check(err, ErrorMatches, "cannot run core-configure on classic distribution")
}

func (s *coreCfgSuite) TestConfigureErrorOnMissingCoreSupport(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSystemctl := testutil.MockCommand(c, "systemctl", `
echo "simulate missing core-support"
exit 1
`)
	defer mockSystemctl.Restore()

	err := corecfg.Run()
	c.Check(err, ErrorMatches, `(?m)Cannot run systemctl - is core-support available: \[--version\] failed with exit status 1: simulate missing core-support`)
}
