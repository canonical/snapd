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

package ifacestate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/state"
)

type handlersSuite struct{}

var _ = Suite(&handlersSuite{})

func (s *handlersSuite) TestInSameChangeWaitChain(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// no wait chain (yet)
	startT := st.NewTask("start", "...start")
	intermediateT := st.NewTask("intermediateT", "...intermediateT")
	searchT := st.NewTask("searchT", "...searchT")
	c.Check(ifacestate.InSameChangeWaitChain(startT, searchT), Equals, false)

	// add (indirect) wait chain
	searchT.WaitFor(intermediateT)
	intermediateT.WaitFor(startT)
	c.Check(ifacestate.InSameChangeWaitChain(startT, searchT), Equals, true)
}

func (s *handlersSuite) TestInSameChangeWaitChainDifferentChanges(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t1 := st.NewTask("t1", "...")
	chg1 := st.NewChange("chg1", "...")
	chg1.AddTask(t1)

	t2 := st.NewTask("t2", "...")
	chg2 := st.NewChange("chg2", "...")
	chg2.AddTask(t2)

	// add a cross change wait chain
	t2.WaitFor(t1)
	c.Check(ifacestate.InSameChangeWaitChain(t1, t2), Equals, false)
}
