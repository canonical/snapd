// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package settings_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/settings"
	"github.com/snapcore/snapd/overlord/state"
)

func TestT(t *testing.T) { TestingT(t) }

type settingsSuite struct {
	state *state.State
}

var _ = Suite(&settingsSuite{})

func (s *settingsSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
}

func (s *settingsSuite) TestSettingProblemReportsDisableDefault(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	c.Check(settings.ProblemReportsDisabled(s.state), Equals, false)
}

func (s *settingsSuite) TestSettingProblemReportsDisable(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "problem-reports.disabled", true)
	tr.Commit()

	c.Check(settings.ProblemReportsDisabled(s.state), Equals, true)
}
