// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package fdestate_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/state"
)

func TestFDE(t *testing.T) { TestingT(t) }

type fdeMgrSuite struct{}

var _ = Suite(&fdeMgrSuite{})

func (s *fdeMgrSuite) TestGetManagerFromState(c *C) {
	st := state.New(nil)

	runner := state.NewTaskRunner(st)
	manager := fdestate.Manager(st, runner)
	defer manager.Stop()
	st.Lock()
	defer st.Unlock()
	foundManager := fdestate.FdeMgr(st)
	c.Check(foundManager, Equals, manager)
}
