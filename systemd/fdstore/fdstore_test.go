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

package fdstore_test

import (
	"errors"
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd/fdstore"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type fdstoreTestSuite struct {
	testutil.BaseTest

	fakeEnv        map[string]string
	sdNotifyCalls  []string
	errOn          []string
	closedFds      []int
	closeOnExecFds []int
}

var _ = Suite(&fdstoreTestSuite{})

func (s *fdstoreTestSuite) SetUpTest(c *C) {
	s.fakeEnv = map[string]string{"LISTEN_PID": "1984"}
	s.sdNotifyCalls = nil
	s.errOn = nil
	s.closedFds = nil
	s.closeOnExecFds = nil

	s.AddCleanup(fdstore.MockOsGetenv(func(key string) string {
		return s.fakeEnv[key]
	}))
	s.AddCleanup(fdstore.MockOsUnsetenv(func(key string) error {
		delete(s.fakeEnv, key)
		return nil
	}))
	s.AddCleanup(fdstore.MockOsGetpid(func() int {
		return 1984
	}))
	s.AddCleanup(fdstore.MockSdNotify(func(notifyState string) error {
		call := fmt.Sprintf("sd-notify: %s", notifyState)
		if strutil.ListContains(s.errOn, call) {
			return errors.New("boom!")
		}
		s.sdNotifyCalls = append(s.sdNotifyCalls, call)
		return nil
	}))
	s.AddCleanup(fdstore.MockSdNotifyWithFds(func(notifyState string, fds ...int) error {
		call := fmt.Sprintf("sd-notify-with-fds: %s %v", notifyState, fds)
		if strutil.ListContains(s.errOn, call) {
			return errors.New("boom!")
		}
		s.sdNotifyCalls = append(s.sdNotifyCalls, call)
		return nil
	}))
	s.AddCleanup(fdstore.MockUnixClose(func(fd int) (err error) {
		if strutil.ListContains(s.errOn, fmt.Sprintf("close-fd: %d", fd)) {
			return errors.New("boom!")
		}
		s.closedFds = append(s.closedFds, fd)
		return nil
	}))
	s.AddCleanup(fdstore.MockUnixCloseOnExec(func(fd int) {
		s.closeOnExecFds = append(s.closeOnExecFds, fd)
	}))
	s.AddCleanup(fdstore.Clear)
}

func (s *fdstoreTestSuite) TestGet(c *C) {
	s.fakeEnv["LISTEN_FDS"] = "5"
	s.fakeEnv["LISTEN_FDNAMES"] = "snapd.socket:invalid:snapd.socket:memfd-secret-state:snapd.socket"
	// fds starts from 3

	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, 6)

	// fdstore is lazily initialized once, and clears passed environment
	c.Assert(s.fakeEnv, HasLen, 0)

	// more checks
	c.Check(fdstore.Get("no-fd"), Equals, -1)        // doesn't exist
	c.Check(fdstore.Get("invalid"), Equals, -1)      // should have been pruned by initialization
	c.Check(fdstore.Get("snapd.socket"), Equals, -1) // sockets are not returned

	// check remove call for "invalid" fd
	c.Check(s.sdNotifyCalls, DeepEquals, []string{
		"sd-notify: FDSTOREREMOVE=1\nFDNAME=invalid",
	})
	c.Check(s.closedFds, DeepEquals, []int{4})
	c.Check(s.closeOnExecFds, DeepEquals, []int{3, 4, 5, 6, 7})
}

func (s *fdstoreTestSuite) TestInitBadPIDError(c *C) {
	s.fakeEnv["LISTEN_PID"] = "1999" // not 1984
	s.fakeEnv["LISTEN_FDS"] = "3"
	s.fakeEnv["LISTEN_FDNAMES"] = "snapd.socket:memfd-secret-state:snapd.socket"

	// PID mismatch ignores passed fds
	c.Check(fdstore.ActivationSocketFds(), DeepEquals, map[string][]int{})
	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, -1)

	// passed environment variables are cleared
	c.Assert(s.fakeEnv, HasLen, 0)
}

func (s *fdstoreTestSuite) TestInitNoFds(c *C) {
	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, -1)
	c.Check(fdstore.ActivationSocketFds(), DeepEquals, map[string][]int{})
}

func (s *fdstoreTestSuite) TestInitEnvMismatchError(c *C) {
	// two fds, three fd-names
	s.fakeEnv["LISTEN_FDS"] = "2"
	s.fakeEnv["LISTEN_FDNAMES"] = "snapd.socket:other.socket:memfd-secret-state"

	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, -1)
	c.Check(fdstore.ActivationSocketFds(), DeepEquals, map[string][]int{})
}

func (s *fdstoreTestSuite) TestInitPruneMoreThanOneFdOnCloseError(c *C) {
	s.fakeEnv["LISTEN_FDS"] = "3"
	s.fakeEnv["LISTEN_FDNAMES"] = "snapd.socket:memfd-secret-state:memfd-secret-state"

	// erroring on the last entry, will make subsequent calls
	s.errOn = []string{"close-fd: 4"}

	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, -1)

	// remove from systemd fdstore as part of the cleanup
	c.Check(s.sdNotifyCalls, DeepEquals, []string{
		"sd-notify: FDSTOREREMOVE=1\nFDNAME=memfd-secret-state",
	})
	// only fd (5) because fd (4) should have errored on close
	c.Check(s.closedFds, DeepEquals, []int{5})
	c.Check(s.closeOnExecFds, DeepEquals, []int{3, 4, 5})
}

func (s *fdstoreTestSuite) TestAdd(c *C) {
	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, -1)
	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, 7), IsNil)
	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, 7)

	// but only once
	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, 8), ErrorMatches, `cannot add file descriptor to fdstore: "memfd-secret-state" already exists`)
	// also, cannot add unknown fds
	c.Check(fdstore.Add(fdstore.FdName("unknown"), 9), ErrorMatches, `cannot add file descriptor to fdstore: unknown file descriptor name "unknown"`)
	// also, cannot add socket fds
	c.Check(fdstore.Add(fdstore.FdName("snapd.socket"), 10), ErrorMatches, "cannot add file descriptor to fdstore: sockets are not allowed")
	c.Check(fdstore.Add(fdstore.FdName("some-svc.socket"), 10), ErrorMatches, "cannot add file descriptor to fdstore: sockets are not allowed")

	c.Check(s.sdNotifyCalls, DeepEquals, []string{
		"sd-notify-with-fds: FDSTORE=1\nFDNAME=memfd-secret-state [7]",
	})
}

func (s *fdstoreTestSuite) TestAddExistingFdError(c *C) {
	s.fakeEnv["LISTEN_FDS"] = "1"
	s.fakeEnv["LISTEN_FDNAMES"] = "memfd-secret-state"

	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, 3)
	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, 7), ErrorMatches, `cannot add file descriptor to fdstore: "memfd-secret-state" already exists`)
	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, 3)

	c.Check(s.sdNotifyCalls, HasLen, 0)
}

func (s *fdstoreTestSuite) TestAddSdNotifyError(c *C) {
	s.errOn = []string{"sd-notify-with-fds: FDSTORE=1\nFDNAME=memfd-secret-state [7]"}

	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, -1)
	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, 7), ErrorMatches, `cannot add file descriptor to fdstore: boom!`)
	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, -1)

	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, 8), IsNil)
	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, 8)
}

func (s *fdstoreTestSuite) TestRemove(c *C) {
	s.fakeEnv["LISTEN_FDS"] = "3"
	s.fakeEnv["LISTEN_FDNAMES"] = "memfd-secret-state:snapd.socket:snapd.socket"

	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, 3)
	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, 7), ErrorMatches, `cannot add file descriptor to fdstore: "memfd-secret-state" already exists`)
	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, 3)
	c.Check(fdstore.Remove(fdstore.FdNameMemfdSecretState), IsNil)
	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, 7), IsNil)
	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, 7)

	// cannot remove socket fds
	c.Check(fdstore.Remove(fdstore.FdName("snapd.socket")), ErrorMatches, "cannot remove file descriptor from fdstore: sockets cannot be removed")

	c.Check(s.sdNotifyCalls, DeepEquals, []string{
		"sd-notify: FDSTOREREMOVE=1\nFDNAME=memfd-secret-state",
		"sd-notify-with-fds: FDSTORE=1\nFDNAME=memfd-secret-state [7]",
	})
	c.Check(s.closedFds, DeepEquals, []int{3})
	c.Check(s.closeOnExecFds, DeepEquals, []int{3, 4, 5})
}

func (s *fdstoreTestSuite) TestRemoveSdNotifyError(c *C) {
	s.fakeEnv["LISTEN_FDS"] = "2"
	s.fakeEnv["LISTEN_FDNAMES"] = "memfd-secret-state:snapd.socket"

	s.errOn = []string{"sd-notify: FDSTOREREMOVE=1\nFDNAME=memfd-secret-state"}

	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, 3)
	c.Check(fdstore.Remove(fdstore.FdNameMemfdSecretState), ErrorMatches, "boom!")
	c.Check(fdstore.Get(fdstore.FdNameMemfdSecretState), Equals, 3)

	c.Check(s.sdNotifyCalls, HasLen, 0)
	c.Check(s.closedFds, HasLen, 0)
	c.Check(s.closeOnExecFds, DeepEquals, []int{3, 4})
}

func (s *fdstoreTestSuite) TestActivationSocketFiles(c *C) {
	s.fakeEnv["LISTEN_FDS"] = "4"
	s.fakeEnv["LISTEN_FDNAMES"] = "snapd.socket:snapd.session-agent.socket:memfd-secret-state:snapd.socket"
	// fds starts from 3
	socketFds := fdstore.ActivationSocketFds()
	c.Check(socketFds, DeepEquals, map[string][]int{
		"snapd.socket":               {3, 6},
		"snapd.session-agent.socket": {4},
	})
}

func (s *fdstoreTestSuite) TestKnownFdNames(c *C) {
	c.Assert(fdstore.KnownFdNames(), DeepEquals, map[fdstore.FdName]bool{
		fdstore.FdName("memfd-secret-state"): true,
	})
}
