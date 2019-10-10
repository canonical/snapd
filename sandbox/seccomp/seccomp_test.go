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

package seccomp_test

import (
	"io"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/seccomp"
)

type seccompSuite struct{}

var _ = Suite(&seccompSuite{})

func (s *seccompSuite) TestInterfaceSystemKey(c *C) {
	reset := seccomp.MockActions([]string{})
	defer reset()
	c.Check(seccomp.Actions(), DeepEquals, []string{})

	reset = seccomp.MockActions([]string{"allow", "errno", "kill", "log", "trace", "trap"})
	defer reset()
	c.Check(seccomp.Actions(), DeepEquals, []string{"allow", "errno", "kill", "log", "trace", "trap"})
}

func (s *seccompSuite) TestSecCompSupportsAction(c *C) {
	reset := seccomp.MockActions([]string{})
	defer reset()
	c.Check(seccomp.SupportsAction("log"), Equals, false)

	reset = seccomp.MockActions([]string{"allow", "errno", "kill", "log", "trace", "trap"})
	defer reset()
	c.Check(seccomp.SupportsAction("log"), Equals, true)
}

func (s *seccompSuite) TestProbe(c *C) {
	seccomp.FreshSecCompProbe()
	r1 := seccomp.MockIoutilReadfile(func(string) ([]byte, error) {
		return []byte("a b\n"), nil
	})
	defer r1()

	c.Check(seccomp.Actions(), DeepEquals, []string{"a", "b"})

	r2 := seccomp.MockIoutilReadfile(func(string) ([]byte, error) {
		return nil, io.ErrUnexpectedEOF
	})
	defer r2()

	c.Check(seccomp.Actions(), DeepEquals, []string{"a", "b"})
}
