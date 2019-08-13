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

package ui_test

import (
	"time"

	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/userd/ui"
)

func Test(t *testing.T) { TestingT(t) }

type zenitySuite struct {
	mock *testutil.MockCmd
}

var _ = Suite(&zenitySuite{})

func (s *zenitySuite) TestYesNoSimpleYes(c *C) {
	mock := testutil.MockCommand(c, "zenity", "")
	defer mock.Restore()

	z := &ui.Zenity{}
	answeredYes := z.YesNo("primary", "secondary", nil)
	c.Check(answeredYes, Equals, true)
	c.Check(mock.Calls(), DeepEquals, [][]string{
		{"zenity", "--question", "--modal", "--text=<big><b>primary</b></big>\n\nsecondary"},
	})
}

func (s *zenitySuite) TestYesNoSimpleNo(c *C) {
	mock := testutil.MockCommand(c, "zenity", "false")
	defer mock.Restore()

	z := &ui.Zenity{}
	answeredYes := z.YesNo("primary", "secondary", nil)
	c.Check(answeredYes, Equals, false)
	c.Check(mock.Calls(), DeepEquals, [][]string{
		{"zenity", "--question", "--modal", "--text=<big><b>primary</b></big>\n\nsecondary"},
	})
}

func (s *zenitySuite) TestYesNoLong(c *C) {
	mock := testutil.MockCommand(c, "zenity", "true")
	defer mock.Restore()

	z := &ui.Zenity{}
	_ = z.YesNo("01234567890", "01234567890", nil)
	c.Check(mock.Calls(), DeepEquals, [][]string{
		{"zenity", "--question", "--modal", "--text=<big><b>01234567890</b></big>\n\n01234567890", "--width=500"},
	})
}

func (s *zenitySuite) TestYesNoSimpleFooter(c *C) {
	mock := testutil.MockCommand(c, "zenity", "")
	defer mock.Restore()

	z := &ui.Zenity{}
	answeredYes := z.YesNo("primary", "secondary", &ui.DialogOptions{Footer: "footer"})
	c.Check(answeredYes, Equals, true)
	c.Check(mock.Calls(), DeepEquals, [][]string{
		{"zenity", "--question", "--modal", `--text=<big><b>primary</b></big>

secondary

<span size="x-small">footer</span>`},
	})
}

func (s *zenitySuite) TestYesNoSimpleTimeout(c *C) {
	mock := testutil.MockCommand(c, "zenity", "true")
	defer mock.Restore()

	z := &ui.Zenity{}
	answeredYes := z.YesNo("primary", "secondary", &ui.DialogOptions{Timeout: 60 * time.Second})
	c.Check(answeredYes, Equals, true)
	c.Check(mock.Calls(), DeepEquals, [][]string{
		{"zenity", "--question", "--modal", "--text=<big><b>primary</b></big>\n\nsecondary", "--timeout=60"},
	})
}
