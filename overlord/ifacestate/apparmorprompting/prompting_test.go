// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package apparmorprompting_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type apparmorPromptingSuite struct {
	testutil.BaseTest
}

var _ = Suite(&apparmorPromptingSuite{})

func (s *apparmorPromptingSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *apparmorPromptingSuite) TestSmoke(c *C) {
	restorePromptingEnabled := apparmorprompting.MockPromptingEnabled(func() bool {
		return true
	})
	defer restorePromptingEnabled()
	restoreListener := apparmorprompting.MockListener()
	defer restoreListener()
	st := state.New(nil)
	p := apparmorprompting.New(st)
	c.Assert(p, NotNil)
	c.Assert(p.Connect(), IsNil)
	c.Assert(p.Run(), IsNil)
	c.Assert(p.Stop(), IsNil)
}
