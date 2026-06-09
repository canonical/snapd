// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025-2026 Canonical Ltd
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
	"net"
	"os"
	"sort"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/systemd/fdstore"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type fdstoreTestSuite struct {
	testutil.BaseTest

	sdNotifyCalls  []string
	errOn          []string
	closeOnExecFds []int
	closeFds       []int
	lastDupFd      int
	duplicatedFds  []int
}

var _ = Suite(&fdstoreTestSuite{})

func (s *fdstoreTestSuite) SetUpTest(c *C) {
	s.sdNotifyCalls = nil
	s.errOn = nil
	s.closeOnExecFds = nil
	s.closeFds = nil
	s.lastDupFd = 1000
	s.duplicatedFds = nil

	os.Setenv("LISTEN_PID", "1984")
	os.Unsetenv("LISTEN_FDS")
	os.Unsetenv("LISTEN_FDNAMES")
	s.AddCleanup(func() {
		os.Unsetenv("LISTEN_PID")
		os.Unsetenv("LISTEN_FDS")
		os.Unsetenv("LISTEN_FDNAMES")
	})
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
	s.AddCleanup(fdstore.MockSdNotifyWithFds(func(notifyState string, files ...*os.File) error {
		fds := make([]int, len(files))
		for i := range files {
			fds[i] = int(files[i].Fd())
		}
		call := fmt.Sprintf("sd-notify-with-fds: %s %v", notifyState, fds)
		if strutil.ListContains(s.errOn, call) {
			return errors.New("boom!")
		}
		s.sdNotifyCalls = append(s.sdNotifyCalls, call)
		return nil
	}))
	s.AddCleanup(fdstore.MockUnixCloseOnExec(func(fd int) {
		s.closeOnExecFds = append(s.closeOnExecFds, fd)
	}))
	s.AddCleanup(fdstore.MockUnixDup(func(oldfd int) (fd int, err error) {
		s.duplicatedFds = append(s.duplicatedFds, oldfd)
		s.lastDupFd++
		return s.lastDupFd, nil
	}))
	s.AddCleanup(fdstore.MockOsFileClose(func(f *os.File) error {
		s.closeFds = append(s.closeFds, int(f.Fd()))
		return nil
	}))
	s.AddCleanup(systemd.MockSystemdVersion(236, nil))
	s.AddCleanup(fdstore.Clear)
}

func (s *fdstoreTestSuite) TestGet(c *C) {
	os.Setenv("LISTEN_FDS", "5")
	os.Setenv("LISTEN_FDNAMES", "snapd.socket:invalid:snapd.socket:memfd-secret-state:snapd.socket")
	// fds starts from 3

	s.lastDupFd = 1998

	file, err := fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, IsNil)
	c.Check(file.Fd(), Equals, uintptr(1999))
	c.Check(file.Name(), Equals, "memfd-secret-state")
	c.Check(s.duplicatedFds, DeepEquals, []int{6})

	// fdstore is lazily initialized once, and clears passed environment
	c.Assert(os.Getenv("LISTEN_PID"), Equals, "")
	c.Assert(os.Getenv("LISTEN_FDS"), Equals, "")
	c.Assert(os.Getenv("LISTEN_FDNAMES"), Equals, "")

	// more checks
	file, err = fdstore.Get("no-fd") // doesn't exist
	c.Assert(err, ErrorMatches, `cannot get file descriptor named "no-fd": file descriptor not found`)
	c.Assert(err, testutil.ErrorIs, fdstore.ErrNotFound)
	c.Check(file, IsNil)
	file, err = fdstore.Get("invalid") // should have been pruned by initialization
	c.Check(file, IsNil)
	c.Assert(err, ErrorMatches, `cannot get file descriptor named "invalid": file descriptor not found`)
	file, err = fdstore.Get("snapd.socket") // sockets are not returned
	c.Assert(err, ErrorMatches, `internal error: cannot get file descriptor named "snapd.socket": socket found, use ActivationListeners instead`)
	c.Check(file, IsNil)

	file, err = fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, IsNil)
	c.Check(file.Fd(), Equals, uintptr(2000))
	c.Check(file.Name(), Equals, "memfd-secret-state")
	c.Check(s.duplicatedFds, DeepEquals, []int{6, 6})

	// check remove call for "invalid" fd
	c.Check(s.sdNotifyCalls, DeepEquals, []string{
		"sd-notify: FDSTOREREMOVE=1\nFDNAME=invalid",
	})
	// 1999 and 2000 from dupicated fds on Get
	c.Check(s.closeOnExecFds, DeepEquals, []int{3, 4, 5, 6, 7, 1999, 2000})
}

func (s *fdstoreTestSuite) TestGetLowSystemdVersionError(c *C) {
	restore := systemd.MockSystemdVersion(235, nil)
	defer restore()

	_, err := fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, ErrorMatches, `cannot get file descriptor from fdstore: unsupported systemd version: systemd version 235 is too old \(expected at least 236\)`)
	c.Assert(err, testutil.ErrorIs, fdstore.ErrUnsupportedSystemdVersion)
}

func (s *fdstoreTestSuite) TestInitBadPIDError(c *C) {
	os.Setenv("LISTEN_PID", "1999") // not 1984
	os.Setenv("LISTEN_FDS", "3")
	os.Setenv("LISTEN_FDNAMES", "snapd.socket:memfd-secret-state:snapd.socket")

	// PID mismatch ignores passed fds
	listeners, err := fdstore.ActivationListeners()
	c.Check(err, IsNil)
	c.Check(listeners, IsNil)
	_, err = fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, ErrorMatches, `cannot get file descriptor named "memfd-secret-state": file descriptor not found`)

	// passed environment variables are cleared
	c.Assert(os.Getenv("LISTEN_PID"), Equals, "")
	c.Assert(os.Getenv("LISTEN_FDS"), Equals, "")
	c.Assert(os.Getenv("LISTEN_FDNAMES"), Equals, "")
}

func (s *fdstoreTestSuite) TestInitNoFds(c *C) {
	_, err := fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, ErrorMatches, `cannot get file descriptor named "memfd-secret-state": file descriptor not found`)
	listeners, err := fdstore.ActivationListeners()
	c.Check(err, IsNil)
	c.Check(listeners, IsNil)
}

func (s *fdstoreTestSuite) TestInitEnvMismatchError(c *C) {
	// two fds, three fd-names
	os.Setenv("LISTEN_FDS", "2")
	os.Setenv("LISTEN_FDNAMES", "snapd.socket:other.socket:memfd-secret-state")

	_, err := fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, ErrorMatches, `cannot get file descriptor named "memfd-secret-state": file descriptor not found`)
	listeners, err := fdstore.ActivationListeners()
	c.Check(err, IsNil)
	c.Check(listeners, IsNil)
}

func (s *fdstoreTestSuite) TestAdd(c *C) {
	s.lastDupFd = 1973

	_, err := fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, ErrorMatches, `cannot get file descriptor named "memfd-secret-state": file descriptor not found`)

	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, os.NewFile(7, "")), IsNil)
	// 7 is duplicated as 1974
	c.Check(s.duplicatedFds, DeepEquals, []int{7})

	file, err := fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, IsNil)
	c.Check(file.Fd(), Equals, uintptr(1975))
	c.Check(s.duplicatedFds, DeepEquals, []int{7, 1974})

	// but only once
	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, os.NewFile(8, "")), ErrorMatches, `cannot add file descriptor to fdstore: "memfd-secret-state" already exists`)
	// also, cannot add unknown fds
	c.Check(fdstore.Add(fdstore.FdName("unknown"), os.NewFile(9, "")), ErrorMatches, `cannot add file descriptor to fdstore: unknown file descriptor name "unknown"`)
	// also, cannot add socket fds
	c.Check(fdstore.Add(fdstore.FdName("snapd.socket"), os.NewFile(10, "")), ErrorMatches, "cannot add file descriptor to fdstore: sockets are not allowed")
	c.Check(fdstore.Add(fdstore.FdName("some-svc.socket"), os.NewFile(10, "")), ErrorMatches, "cannot add file descriptor to fdstore: sockets are not allowed")

	c.Check(s.sdNotifyCalls, DeepEquals, []string{
		"sd-notify-with-fds: FDSTORE=1\nFDNAME=memfd-secret-state [1974]",
	})
}

func (s *fdstoreTestSuite) TestAddExistingFdError(c *C) {
	os.Setenv("LISTEN_FDS", "1")
	os.Setenv("LISTEN_FDNAMES", "memfd-secret-state")

	s.lastDupFd = 1999

	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, os.NewFile(7, "")), ErrorMatches, `cannot add file descriptor to fdstore: "memfd-secret-state" already exists`)

	file, err := fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, IsNil)
	c.Check(file.Fd(), Equals, uintptr(2000))
	c.Check(s.duplicatedFds, DeepEquals, []int{3})

	c.Check(s.sdNotifyCalls, HasLen, 0)
}

func (s *fdstoreTestSuite) TestAddSdNotifyError(c *C) {
	s.lastDupFd = 2026

	s.errOn = []string{"sd-notify-with-fds: FDSTORE=1\nFDNAME=memfd-secret-state [2027]"}

	_, err := fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, ErrorMatches, `cannot get file descriptor named "memfd-secret-state": file descriptor not found`)

	c.Check(s.closeFds, DeepEquals, []int(nil))
	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, os.NewFile(7, "")), ErrorMatches, `cannot add file descriptor to fdstore: boom!`)
	// duplicated (as 2027) before sd-notify error
	c.Check(s.duplicatedFds, DeepEquals, []int{7})
	// duplicated fd should be closed on error
	c.Check(s.closeFds, DeepEquals, []int{2027})

	_, err = fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, ErrorMatches, `cannot get file descriptor named "memfd-secret-state": file descriptor not found`)

	// 8 is duplicated as 2028
	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, os.NewFile(8, "")), IsNil)
	file, err := fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, IsNil)
	c.Check(file.Fd(), Equals, uintptr(2029)) // 2029 is duplicated from 2028 (which is duplicate from 8)
	c.Check(s.duplicatedFds, DeepEquals, []int{7, 8, 2028})
}

func (s *fdstoreTestSuite) TestAddLowSystemdVersionError(c *C) {
	restore := systemd.MockSystemdVersion(235, nil)
	defer restore()

	err := fdstore.Add(fdstore.FdNameMemfdSecretState, os.NewFile(7, ""))
	c.Assert(err, ErrorMatches, `cannot add file descriptor to fdstore: unsupported systemd version: systemd version 235 is too old \(expected at least 236\)`)
	c.Assert(err, testutil.ErrorIs, fdstore.ErrUnsupportedSystemdVersion)

	c.Check(s.sdNotifyCalls, HasLen, 0)
}

func (s *fdstoreTestSuite) TestRemove(c *C) {
	os.Setenv("LISTEN_FDS", "3")
	os.Setenv("LISTEN_FDNAMES", "memfd-secret-state:snapd.socket:snapd.socket")

	s.lastDupFd = 1000

	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, os.NewFile(7, "")), ErrorMatches, `cannot add file descriptor to fdstore: "memfd-secret-state" already exists`)

	file, err := fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, IsNil)
	c.Check(file.Fd(), Equals, uintptr(1001))
	c.Check(s.duplicatedFds, DeepEquals, []int{3})

	c.Check(s.closeFds, DeepEquals, []int(nil))
	c.Check(fdstore.Remove(fdstore.FdNameMemfdSecretState), IsNil)
	c.Check(s.closeFds, DeepEquals, []int{3})

	// cannot remove again
	c.Check(fdstore.Remove(fdstore.FdNameMemfdSecretState), ErrorMatches, `cannot remove file descriptor from fdstore: file descriptor not found`)

	c.Check(fdstore.Add(fdstore.FdNameMemfdSecretState, os.NewFile(7, "")), IsNil)
	// 7 is duplicated as 1002
	c.Check(s.duplicatedFds, DeepEquals, []int{3, 7})

	file, err = fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, IsNil)
	c.Check(file.Fd(), Equals, uintptr(1003))
	c.Check(s.duplicatedFds, DeepEquals, []int{3, 7, 1002})

	// cannot remove socket fds
	c.Check(fdstore.Remove(fdstore.FdName("snapd.socket")), ErrorMatches, "cannot remove file descriptor from fdstore: sockets cannot be removed")
	// or unknown fds
	c.Check(fdstore.Remove(fdstore.FdName("unknown")), ErrorMatches, `cannot remove file descriptor from fdstore: file descriptor not found`)

	c.Check(s.sdNotifyCalls, DeepEquals, []string{
		"sd-notify: FDSTOREREMOVE=1\nFDNAME=memfd-secret-state",
		"sd-notify-with-fds: FDSTORE=1\nFDNAME=memfd-secret-state [1002]",
	})
	// 1001 and 1003 are duplicated from Get, 1002 is duplicated from Add
	c.Check(s.closeOnExecFds, DeepEquals, []int{3, 4, 5, 1001, 1002, 1003})
	c.Check(s.closeFds, DeepEquals, []int{3})
}

func (s *fdstoreTestSuite) TestRemoveSdNotifyError(c *C) {
	os.Setenv("LISTEN_FDS", "2")
	os.Setenv("LISTEN_FDNAMES", "memfd-secret-state:snapd.socket")

	s.lastDupFd = 1000

	s.errOn = []string{"sd-notify: FDSTOREREMOVE=1\nFDNAME=memfd-secret-state"}

	c.Check(fdstore.Remove(fdstore.FdNameMemfdSecretState), ErrorMatches, "boom!")

	file, err := fdstore.Get(fdstore.FdNameMemfdSecretState)
	c.Assert(err, IsNil)
	c.Check(file.Fd(), Equals, uintptr(1001))
	c.Check(s.duplicatedFds, DeepEquals, []int{3})

	c.Check(s.sdNotifyCalls, HasLen, 0)
}

func (s *fdstoreTestSuite) TestRemoveLowSystemdVersionError(c *C) {
	os.Setenv("LISTEN_FDS", "2")
	os.Setenv("LISTEN_FDNAMES", "memfd-secret-state:snapd.socket")

	restore := systemd.MockSystemdVersion(235, nil)
	defer restore()

	err := fdstore.Remove(fdstore.FdNameMemfdSecretState)
	c.Assert(err, ErrorMatches, `cannot remove file descriptor from fdstore: unsupported systemd version: systemd version 235 is too old \(expected at least 236\)`)
	c.Assert(err, testutil.ErrorIs, fdstore.ErrUnsupportedSystemdVersion)

	c.Check(s.sdNotifyCalls, HasLen, 0)
	c.Check(s.closeOnExecFds, DeepEquals, []int{3, 4})
}

type fakeListener struct {
	f *os.File
}

func (*fakeListener) Accept() (net.Conn, error) { panic("unexpected") }
func (*fakeListener) Close() error              { panic("unexpected") }
func (*fakeListener) Addr() net.Addr            { panic("unexpected") }
func (l *fakeListener) String() string          { return fmt.Sprintf("%s (%d)", l.f.Name(), l.f.Fd()) }

func (s *fdstoreTestSuite) TestActivationListeners(c *C) {
	os.Setenv("LISTEN_FDS", "4")
	os.Setenv("LISTEN_FDNAMES", "snapd.socket:snapd.session-agent.socket:memfd-secret-state:snapd.socket")
	// fds starts from 3

	restore := fdstore.MockNetFileListener(func(f *os.File) (ln net.Listener, err error) {
		return &fakeListener{f}, nil
	})
	defer restore()

	listeners, err := fdstore.ActivationListeners()
	c.Assert(err, IsNil)
	c.Assert(listeners, HasLen, 3)
	sort.Slice(listeners, func(i, j int) bool {
		return listeners[i].(*fakeListener).String() < listeners[j].(*fakeListener).String()
	})
	c.Check(listeners[0].(*fakeListener).String(), Equals, "snapd.session-agent.socket (4)")
	c.Check(listeners[1].(*fakeListener).String(), Equals, "snapd.socket (3)")
	c.Check(listeners[2].(*fakeListener).String(), Equals, "snapd.socket (6)")

	// another time
	listeners, err = fdstore.ActivationListeners()
	c.Assert(err, IsNil)
	c.Assert(listeners, HasLen, 3)
	sort.Slice(listeners, func(i, j int) bool {
		return listeners[i].(*fakeListener).String() < listeners[j].(*fakeListener).String()
	})
	c.Check(listeners[0].(*fakeListener).String(), Equals, "snapd.session-agent.socket (4)")
	c.Check(listeners[1].(*fakeListener).String(), Equals, "snapd.socket (3)")
	c.Check(listeners[2].(*fakeListener).String(), Equals, "snapd.socket (6)")
}

func (s *fdstoreTestSuite) TestActivationListenersMissingFdNamesEnv(c *C) {
	os.Setenv("LISTEN_FDS", "4")

	restore := fdstore.MockNetFileListener(func(f *os.File) (ln net.Listener, err error) {
		return &fakeListener{f}, nil
	})
	defer restore()

	// make sure that older versions of systemd (e.g. v219 on amazon-linux-2)
	// are supported where the $LISTEN_FDNAMES env var is not passed.
	listeners, err := fdstore.ActivationListeners()
	c.Assert(err, IsNil)
	c.Assert(listeners, HasLen, 4)
	sort.Slice(listeners, func(i, j int) bool {
		return listeners[i].(*fakeListener).String() < listeners[j].(*fakeListener).String()
	})
	c.Check(listeners[0].(*fakeListener).String(), Equals, "activation-fd-0.socket (3)")
	c.Check(listeners[1].(*fakeListener).String(), Equals, "activation-fd-1.socket (4)")
	c.Check(listeners[2].(*fakeListener).String(), Equals, "activation-fd-2.socket (5)")
	c.Check(listeners[3].(*fakeListener).String(), Equals, "activation-fd-3.socket (6)")
}

type fakeClosableListener struct {
	closed int
}

func (*fakeClosableListener) Accept() (net.Conn, error) { panic("unexpected") }
func (l *fakeClosableListener) Close() error {
	l.closed++
	return nil
}
func (*fakeClosableListener) Addr() net.Addr { panic("unexpected") }

func (s *fdstoreTestSuite) TestActivationListenersCleansUpCollectedListenersOnError(c *C) {
	os.Setenv("LISTEN_FDS", "3")
	os.Setenv("LISTEN_FDNAMES", "snapd.socket:memfd-secret-state:snapd.socket")

	created := make([]*fakeClosableListener, 0, 1)
	calls := 0
	restore := fdstore.MockNetFileListener(func(f *os.File) (ln net.Listener, err error) {
		if f.Name() != "snapd.socket" {
			c.Fatalf("unexpected fd: %q", f.Name())
		}

		calls++
		if calls == 1 {
			l := &fakeClosableListener{}
			created = append(created, l)
			return l, nil
		}

		return nil, errors.New("boom")
	})
	defer restore()

	listeners, err := fdstore.ActivationListeners()
	c.Assert(err, ErrorMatches, "boom")
	c.Check(listeners, IsNil)
	c.Assert(created, HasLen, 1)
	c.Check(created[0].closed, Equals, 1)
}

func (s *fdstoreTestSuite) TestKnownFdNames(c *C) {
	c.Assert(fdstore.KnownFdNames(), DeepEquals, map[fdstore.FdName]bool{
		fdstore.FdName("memfd-secret-state"): true,
	})
}
