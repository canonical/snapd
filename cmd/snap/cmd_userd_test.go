// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !darwin

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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

package main_test

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/autostart"
)

type userdSuite struct {
	BaseSnapSuite
	testutil.DBusTest

	agentSocketPath string
}

var _ = Suite(&userdSuite{})

func (s *userdSuite) SetUpTest(c *C) {
	s.BaseSnapSuite.SetUpTest(c)
	s.DBusTest.SetUpTest(c)

	_, restore := logger.MockLogger()
	s.AddCleanup(restore)

	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	c.Assert(os.MkdirAll(xdgRuntimeDir, 0700), IsNil)
	s.agentSocketPath = fmt.Sprintf("%s/snapd-session-agent.socket", xdgRuntimeDir)
}

func (s *userdSuite) TearDownTest(c *C) {
	s.BaseSnapSuite.TearDownTest(c)
	s.DBusTest.TearDownTest(c)
}

func (s *userdSuite) TestUserdBadCommandline(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"userd", "extra-arg"})
	c.Assert(err, ErrorMatches, "too many arguments for command")
}

type mockSignal struct{}

func (m *mockSignal) String() string {
	return "<test signal>"
}

func (m *mockSignal) Signal() {}

func (s *userdSuite) TestUserdDBus(c *C) {
	sigCh := make(chan os.Signal, 1)
	sigStopCalls := 0

	restore := snap.MockSignalNotify(func(sig ...os.Signal) (chan os.Signal, func()) {
		c.Assert(sig, DeepEquals, []os.Signal{syscall.SIGINT, syscall.SIGTERM})
		return sigCh, func() { sigStopCalls++ }
	})
	defer restore()

	go func() {
		myPid := os.Getpid()

		defer func() {
			sigCh <- &mockSignal{}
		}()

		names := map[string]bool{
			"io.snapcraft.Launcher": false,
			"io.snapcraft.Settings": false,
		}
		for i := 0; i < 1000; i++ {
			seenCount := 0
			for name, seen := range names {
				if seen {
					seenCount++
					continue
				}
				pid, err := testutil.DBusGetConnectionUnixProcessID(s.SessionBus, name)
				c.Logf("name: %v pid: %v err: %v", name, pid, err)
				if pid == myPid {
					names[name] = true
					seenCount++
				}
			}
			if seenCount == len(names) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		c.Fatalf("not all names have appeared on the bus: %v", names)
	}()

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"userd"})
	c.Assert(err, IsNil)
	c.Check(rest, DeepEquals, []string{})
	c.Check(strings.ToLower(s.Stdout()), Equals, "exiting on <test signal>.\n")
	c.Check(sigStopCalls, Equals, 1)
}

func (s *userdSuite) makeAgentClient() *http.Client {
	transport := &http.Transport{
		Dial: func(_, _ string) (net.Conn, error) {
			return net.Dial("unix", s.agentSocketPath)
		},
		DisableKeepAlives: true,
	}
	return &http.Client{Transport: transport}
}

func (s *userdSuite) TestSessionAgentSocket(c *C) {
	sigCh := make(chan os.Signal, 1)
	sigStopCalls := 0

	restore := snap.MockSignalNotify(func(sig ...os.Signal) (chan os.Signal, func()) {
		c.Assert(sig, DeepEquals, []os.Signal{syscall.SIGINT, syscall.SIGTERM})
		return sigCh, func() { sigStopCalls++ }
	})
	defer restore()

	go func() {
		defer func() {
			sigCh <- &mockSignal{}
		}()

		// Wait for command to create socket file
		for i := 0; i < 1000; i++ {
			if osutil.FileExists(s.agentSocketPath) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Check that agent functions
		client := s.makeAgentClient()
		response, err := client.Get("http://localhost/v1/session-info")
		c.Assert(err, IsNil)
		defer response.Body.Close()
		c.Check(response.StatusCode, Equals, 200)
	}()

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"userd", "--agent"})
	c.Assert(err, IsNil)
	c.Check(rest, DeepEquals, []string{})
	c.Check(strings.ToLower(s.Stdout()), Equals, "exiting on <test signal>.\n")
	c.Check(sigStopCalls, Equals, 1)
}

func (s *userdSuite) TestSignalNotify(c *C) {
	ch, stop := snap.SignalNotify(syscall.SIGUSR1)
	defer stop()
	go func() {
		myPid := os.Getpid()
		me, err := os.FindProcess(myPid)
		c.Assert(err, IsNil)
		err = me.Signal(syscall.SIGUSR1)
		c.Assert(err, IsNil)
	}()
	select {
	case sig := <-ch:
		c.Assert(sig, Equals, syscall.SIGUSR1)
	case <-time.After(5 * time.Second):
		c.Fatal("signal not received within 5s")
	}
}

func (s *userdSuite) TestAutostartSessionAppsRestrictsPermissions(c *C) {
	userDir := path.Join(c.MkDir(), "home")
	mockUserCurrent := func() (*user.User, error) {
		return &user.User{HomeDir: userDir}, nil
	}
	r := snap.MockUserCurrent(mockUserCurrent)
	defer r()

	r = autostart.MockUserCurrent(mockUserCurrent)
	defer r()

	// first make the "snap" dir permissive with 0755 perms
	err := os.MkdirAll(filepath.Join(userDir, "snap"), 0755)
	c.Assert(err, IsNil)

	// make sure the perms are as we expect them if somehow the dir already
	// existed, MkdirAll wouldn't have changed the perms
	st, err := os.Stat(filepath.Join(userDir, "snap"))
	c.Assert(err, IsNil)
	c.Assert(st.Mode()&os.ModePerm, Equals, os.FileMode(0755))

	// run autostart
	args, err := snap.Parser(snap.Client()).ParseArgs([]string{"userd", "--autostart"})
	c.Assert(err, IsNil)
	c.Assert(args, DeepEquals, []string{})

	// make sure that the directory was restricted
	st, err = os.Stat(filepath.Join(userDir, "snap"))
	c.Assert(err, IsNil)
	c.Assert(st.Mode()&os.ModePerm, Equals, os.FileMode(0700))
}

func (s *userdSuite) TestAutostartSessionAppsLogsWhenItCannotRestrictPermissions(c *C) {
	userDir := path.Join(c.MkDir(), "home")
	c.Assert(os.MkdirAll(filepath.Join(userDir, "snap"), 0770), IsNil)

	mockUserCurrent := func() (*user.User, error) {
		return &user.User{HomeDir: userDir}, nil
	}
	r := snap.MockUserCurrent(mockUserCurrent)
	defer r()

	r = autostart.MockUserCurrent(mockUserCurrent)
	defer r()

	r = snap.MockOsChmod(func(name string, mode os.FileMode) error {
		c.Assert(name, Equals, filepath.Join(userDir, "snap"))
		c.Assert(mode, Equals, os.FileMode(0700))

		return fmt.Errorf("cannot os.Chmod because the test says so")
	})
	defer r()

	// run autostart
	args, err := snap.Parser(snap.Client()).ParseArgs([]string{"userd", "--autostart"})
	c.Assert(err, IsNil)
	c.Assert(args, DeepEquals, []string{})

	c.Assert(s.stderr.String(), testutil.Contains, "cannot os.Chmod because the test says so")
}

func (s *userdSuite) TestAutostartSessionAppsRestrictsPermissionsNoCreateSnapDir(c *C) {
	userDir := path.Join(c.MkDir(), "home")
	mockUserCurrent := func() (*user.User, error) {
		return &user.User{HomeDir: userDir}, nil
	}
	r := snap.MockUserCurrent(mockUserCurrent)
	defer r()

	r = autostart.MockUserCurrent(mockUserCurrent)
	defer r()

	// ensure that the "snap" dir doesn't already exist
	c.Assert(filepath.Join(userDir, "snap"), testutil.FileAbsent)

	// run autostart
	args, err := snap.Parser(snap.Client()).ParseArgs([]string{"userd", "--autostart"})
	c.Assert(err, IsNil)
	c.Assert(args, DeepEquals, []string{})

	// make sure that the directory was not created
	c.Assert(filepath.Join(userDir, "snap"), testutil.FileAbsent)
}

func (s *userdSuite) TestAutostartWithBothSnapDirs(c *C) {
	userDir := path.Join(c.MkDir(), "home")
	mockUserCurrent := func() (*user.User, error) {
		return &user.User{HomeDir: userDir}, nil
	}
	r := snap.MockUserCurrent(mockUserCurrent)
	defer r()

	var autostartArgs []string
	mockAutostart := func(arg string) error {
		autostartArgs = append(autostartArgs, arg)
		return nil
	}
	autostartRestore := snap.MockAutostartSessionApps(mockAutostart)
	defer autostartRestore()

	exposedDir := filepath.Join(userDir, "snap")
	c.Assert(os.MkdirAll(exposedDir, 0770), IsNil)
	hiddenDir := filepath.Join(userDir, ".snap", "data")
	c.Assert(os.MkdirAll(hiddenDir, 0770), IsNil)

	args, err := snap.Parser(snap.Client()).ParseArgs([]string{"userd", "--autostart"})
	c.Assert(err, IsNil)
	c.Assert(args, DeepEquals, []string{})

	c.Assert(autostartArgs, testutil.DeepUnsortedMatches, []string{hiddenDir, exposedDir})
}

func (s *userdSuite) TestAutostartErrorsInBoth(c *C) {
	userDir := path.Join(c.MkDir(), "home")
	mockUserCurrent := func() (*user.User, error) {
		return &user.User{HomeDir: userDir}, nil
	}
	r := snap.MockUserCurrent(mockUserCurrent)
	defer r()

	mockAutostart := func(arg string) error {
		return fmt.Errorf("%s", arg)
	}
	autostartRestore := snap.MockAutostartSessionApps(mockAutostart)
	defer autostartRestore()

	exposedDir := filepath.Join(userDir, "snap")
	c.Assert(os.MkdirAll(exposedDir, 0770), IsNil)
	hiddenDir := filepath.Join(userDir, ".snap", "data")
	c.Assert(os.MkdirAll(hiddenDir, 0770), IsNil)

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"userd", "--autostart"})
	c.Assert(err, ErrorMatches, `autostart failed:
.*/snap
.*/\.snap/data
`)
}
