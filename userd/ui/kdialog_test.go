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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/userd/ui"
)

type kdialogSuite struct {
	mock *testutil.MockCmd
}

var _ = Suite(&kdialogSuite{})

func (s *kdialogSuite) TestYesNoSimpleYes(c *C) {
	mock := testutil.MockCommand(c, "kdialog", "")
	defer mock.Restore()

	z := &ui.KDialog{}
	answeredYes := z.YesNo("primary", "secondary", nil)
	c.Check(answeredYes, Equals, true)
	c.Check(mock.Calls(), DeepEquals, [][]string{
		{"kdialog", "--yesno=<p><big><b>primary</b></big></p><p>secondary</p>"},
	})
}

func (s *kdialogSuite) TestYesNoSimpleNo(c *C) {
	mock := testutil.MockCommand(c, "kdialog", "false")
	defer mock.Restore()

	z := &ui.KDialog{}
	answeredYes := z.YesNo("primary", "secondary", nil)
	c.Check(answeredYes, Equals, false)
	c.Check(mock.Calls(), DeepEquals, [][]string{
		{"kdialog", "--yesno=<p><big><b>primary</b></big></p><p>secondary</p>"},
	})
}

func (s *kdialogSuite) TestYesNoSimpleFooter(c *C) {
	mock := testutil.MockCommand(c, "kdialog", "")
	defer mock.Restore()

	z := &ui.KDialog{}
	answeredYes := z.YesNo("primary", "secondary", &ui.DialogOptions{Footer: "footer"})
	c.Check(answeredYes, Equals, true)
	c.Check(mock.Calls(), DeepEquals, [][]string{
		{"kdialog", "--yesno=<p><big><b>primary</b></big></p><p>secondary</p><p><small>footer</small></p>"},
	})
}

func (s *kdialogSuite) TestYesNoSimpleTimeout(c *C) {
	mock := testutil.MockCommand(c, "kdialog", "sleep 9999999")
	defer mock.Restore()

	z := &ui.KDialog{}
	answeredYes := z.YesNo("primary", "secondary", &ui.DialogOptions{Timeout: 1 * time.Second})
	// this got auto-canceled after 1sec
	c.Check(answeredYes, Equals, false)
	c.Check(mock.Calls(), DeepEquals, [][]string{
		{"kdialog", "--yesno=<p><big><b>primary</b></big></p><p>secondary</p>"},
	})
}
