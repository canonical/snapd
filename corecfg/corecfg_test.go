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
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/corecfg"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/systemd"
)

func Test(t *testing.T) { TestingT(t) }

// coreCfgSuite is the base for all the corecfg tests
type coreCfgSuite struct {
	systemctlArgs [][]string
}

var _ = Suite(&coreCfgSuite{})

func (s *coreCfgSuite) SetUpSuite(c *C) {
	systemd.SystemctlCmd = func(args ...string) ([]byte, error) {
		s.systemctlArgs = append(s.systemctlArgs, args[:])
		output := []byte("ActiveState=inactive")
		return output, nil
	}
}

// runCfgSuite tests corecfg.Run()
type runCfgSuite struct {
	coreCfgSuite
}

var _ = Suite(&runCfgSuite{})

func (s *runCfgSuite) TestConfigureErrorsOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	err := corecfg.Run()
	c.Check(err, ErrorMatches, "cannot run core-configure on classic distribution")
}

func (s *runCfgSuite) TestConfigureErrorOnMissingCoreSupport(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	oldSystemdSystemctlCmd := systemd.SystemctlCmd
	systemd.SystemctlCmd = func(args ...string) ([]byte, error) {
		return nil, fmt.Errorf("simulate missing core-support")
	}
	defer func() {
		systemd.SystemctlCmd = oldSystemdSystemctlCmd
	}()

	err := corecfg.Run()
	c.Check(err, ErrorMatches, `(?m)cannot run systemctl - core-support interface seems disconnected: simulate missing core-support`)
}
