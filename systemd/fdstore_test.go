// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package systemd_test

import (
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type fdstoreTestSuite struct {
	testutil.BaseTest

	fakeEnv map[string]string
}

var _ = Suite(&fdstoreTestSuite{})

func (s *fdstoreTestSuite) SetUpTest(c *C) {
	s.fakeEnv = make(map[string]string)
	s.AddCleanup(systemd.MockOsGetenv(func(k string) string {
		return s.fakeEnv[k]
	}))
}

func (s *fdstoreTestSuite) TestGetFds(c *C) {
	s.fakeEnv["LISTEN_FDS"] = "4"
	s.fakeEnv["LISTEN_FDNAMES"] = "snapd.socket:snapd.socket:rkey-fd:snapd.socket"
	// fds starts from 3
	c.Check(systemd.GetFds("snapd.socket"), DeepEquals, []int{3, 4, 6})
	c.Check(systemd.GetFds("rkey-fd"), DeepEquals, []int{5})
	c.Check(systemd.GetFds("no-fd"), IsNil)

	delete(s.fakeEnv, "LISTEN_FDS")
	c.Check(systemd.GetFds("snapd.socket"), IsNil)
	c.Check(systemd.GetFds("rkey-fd"), IsNil)
	c.Check(systemd.GetFds("no-fd"), IsNil)
}

func (s *fdstoreTestSuite) TestAddFds(c *C) {
	called := 0
	restore := systemd.MockSdNotifyWithFds(func(notifyState string, fds ...int) error {
		called++
		c.Check(notifyState, Equals, "FDSTORE=1\nFDNAME=rkey-fd")
		c.Check(fds, DeepEquals, []int{7, 8, 9})
		return nil
	})
	defer restore()

	c.Assert(systemd.AddFds(systemd.FdNameRecoveryKeyStore, 7, 8, 9), IsNil)
	c.Check(called, Equals, 1)
}

func (s *fdstoreTestSuite) TestAddFdsInvalidFdName(c *C) {
	err := systemd.AddFds("unknown-fd", 7, 8, 9)
	c.Assert(err, ErrorMatches, `cannot add file descriptor: unknown file descriptor name "unknown-fd"`)
}

func (s *fdstoreTestSuite) TestPruneFds(c *C) {
	s.fakeEnv["LISTEN_FDS"] = "6"
	s.fakeEnv["LISTEN_FDNAMES"] = "err-on-remove:snapd.socket:err-on-close:snapd.session-agent.socket:unknown-fd:rkey-fd:snapd.socket"

	var states []string
	restore := systemd.MockSdNotify(func(notifyState string) error {
		states = append(states, notifyState)
		if notifyState == "FDSTOREREMOVE=1\nFDNAME=err-on-remove" {
			// test best-effort cleanup
			return errors.New("boom!")
		}
		return nil
	})
	defer restore()

	restore = systemd.MockSyscallClose(func(fd int) (err error) {
		if fd == 5 { // fd named err-on-close
			return errors.New("boom!")
		}
		return nil
	})
	defer restore()

	systemd.PruneFds()

	// *.socket and rkey-fd are skipped
	c.Check(states, DeepEquals, []string{
		"FDSTOREREMOVE=1\nFDNAME=err-on-remove",
		"FDSTOREREMOVE=1\nFDNAME=err-on-close",
		"FDSTOREREMOVE=1\nFDNAME=unknown-fd",
	})
}

func (s *fdstoreTestSuite) TestActivationSocketFiles(c *C) {
	s.fakeEnv["LISTEN_FDS"] = "4"
	s.fakeEnv["LISTEN_FDNAMES"] = "snapd.socket:snapd.session-agent.socket:rkey-fd:snapd.socket"
	// fds starts from 3
	socketFds := systemd.ActivationSocketFds()
	c.Check(socketFds, DeepEquals, map[string][]int{
		"snapd.socket":               {3, 6},
		"snapd.session-agent.socket": {4},
	})

	delete(s.fakeEnv, "LISTEN_FDS")
	socketFds = systemd.ActivationSocketFds()
	c.Check(socketFds, HasLen, 0)
}
