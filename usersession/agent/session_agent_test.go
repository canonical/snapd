// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package agent_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/desktop/notification/notificationtest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/agent"
)

func Test(t *testing.T) { TestingT(t) }

type sessionAgentSuite struct {
	testutil.DBusTest
	socketPath string
	client     *http.Client
}

var _ = Suite(&sessionAgentSuite{})

func (s *sessionAgentSuite) SetUpTest(c *C) {
	s.DBusTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	c.Assert(os.MkdirAll(xdgRuntimeDir, 0700), IsNil)
	s.socketPath = fmt.Sprintf("%s/snapd-session-agent.socket", xdgRuntimeDir)

	transport := &http.Transport{
		Dial: func(_, _ string) (net.Conn, error) {
			return net.Dial("unix", s.socketPath)
		},
		DisableKeepAlives: true,
	}
	s.client = &http.Client{Transport: transport}
}

func (s *sessionAgentSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	logger.SetLogger(logger.NullLogger)
	s.DBusTest.TearDownTest(c)
}

func (s *sessionAgentSuite) TestStartStop(c *C) {
	agent := mylog.Check2(agent.New())

	agent.Version = "42"
	agent.Start()
	defer func() { c.Check(agent.Stop(), IsNil) }()

	// The agent has connected to the session bus
	var hasOwner bool
	c.Check(s.DBusTest.SessionBus.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "io.snapcraft.SessionAgent").Store(&hasOwner), IsNil)
	c.Check(hasOwner, Equals, true)

	// The agent is listening for REST API requests
	response := mylog.Check2(s.client.Get("http://localhost/v1/session-info"))

	defer response.Body.Close()
	c.Check(response.StatusCode, Equals, 200)

	var rst struct {
		Result struct {
			Version string `json:"version"`
		} `json:"result"`
	}
	c.Assert(json.NewDecoder(response.Body).Decode(&rst), IsNil)
	c.Check(rst.Result.Version, Equals, "42")
	response.Body.Close()

	c.Check(agent.Stop(), IsNil)
}

func (s *sessionAgentSuite) TestDying(c *C) {
	agent := mylog.Check2(agent.New())

	agent.Start()
	select {
	case <-agent.Dying():
		c.Error("agent.Dying() channel closed prematurely")
	default:
	}
	go func() {
		time.Sleep(5 * time.Millisecond)
		c.Check(agent.Stop(), IsNil)
	}()
	select {
	case <-agent.Dying():
	case <-time.After(2 * time.Second):
		c.Error("agent.Dying() channel was not closed when agent stopped")
	}
}

func (s *sessionAgentSuite) TestExitOnIdle(c *C) {
	agent := mylog.Check2(agent.New())

	agent.IdleTimeout = 150 * time.Millisecond
	startTime := time.Now()
	agent.Start()
	defer agent.Stop()

	makeRequest := func() {
		response := mylog.Check2(s.client.Get("http://localhost/v1/session-info"))

		defer response.Body.Close()
		c.Check(response.StatusCode, Equals, 200)
	}
	makeRequest()
	time.Sleep(25 * time.Millisecond)
	makeRequest()

	select {
	case <-agent.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("agent did not exit after idle timeout expired")
	}
	elapsed := time.Since(startTime)
	if elapsed < 175*time.Millisecond || elapsed > 450*time.Millisecond {
		// The idle timeout should have been extended when we
		// issued a second request after 25ms.
		c.Errorf("Expected ellaped time close to 175 ms, but got %v", elapsed)
	}
}

func (s *sessionAgentSuite) TestFdoNotification(c *C) {
	desktopFile := "[Desktop Entry]\nIcon=/path/appicon.png"
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapDesktopFilesDir), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapDesktopFilesDir, "app.desktop"), []byte(desktopFile), 0644), IsNil)

	backend := mylog.Check2(notificationtest.NewFdoServer())

	defer backend.Stop()

	agent := mylog.Check2(agent.New())

	agent.IdleTimeout = 150 * time.Millisecond
	agent.Start()
	defer agent.Stop()

	// simulate snap refresh message
	makeRequest := func() {
		data := bytes.NewBufferString(`{"instance-name":"some-snap", "busy-app-name":"App", "busy-app-desktop-entry":"app"}`)
		response := mylog.Check2(s.client.Post("http://localhost/v1/notifications/pending-refresh", "application/json", data))

		defer response.Body.Close()
		c.Check(response.StatusCode, Equals, 200)
	}
	makeRequest()

	// wait for a while, we want the message to trigger FDO notification
	time.Sleep(50 * time.Millisecond)

	// our fake FDO backend should receive the notification
	fdoNotification := backend.Get(1)
	c.Assert(fdoNotification, NotNil)
	c.Check(fdoNotification.AppName, Equals, "App")
	c.Check(fdoNotification.Icon, Equals, "/path/appicon.png")

	// trigger notification close signal over dbus
	c.Assert(backend.Close(1, 0), IsNil)

	select {
	case <-agent.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("agent did not exit after idle timeout expired")
	}
}

func (s *sessionAgentSuite) TestGtkNotification(c *C) {
	desktopFile := "[Desktop Entry]\nIcon=/path/appicon.png"
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapDesktopFilesDir), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapDesktopFilesDir, "app.desktop"), []byte(desktopFile), 0644), IsNil)

	backend := mylog.Check2(notificationtest.NewGtkServer())

	defer backend.Stop()

	agent := mylog.Check2(agent.New())

	agent.IdleTimeout = 150 * time.Millisecond
	agent.Start()
	defer agent.Stop()

	// simulate snap refresh message
	makeRequest := func() {
		data := bytes.NewBufferString(`{"instance-name":"some-snap", "busy-app-name":"App", "busy-app-desktop-entry":"app"}`)
		response := mylog.Check2(s.client.Post("http://localhost/v1/notifications/pending-refresh", "application/json", data))

		defer response.Body.Close()
		c.Check(response.StatusCode, Equals, 200)
	}
	makeRequest()

	// wait for a while, we want the message to trigger FDO notification
	time.Sleep(50 * time.Millisecond)

	// our fake FDO backend should receive the notification
	gtkNotification := backend.Get("some-snap")
	c.Assert(gtkNotification, NotNil)
	c.Check(gtkNotification.DesktopID, Equals, "io.snapcraft.SessionAgent")

	// trigger notification close signal over dbus
	c.Assert(backend.Close("some-snap"), IsNil)

	select {
	case <-agent.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("agent did not exit after idle timeout expired")
	}
}

func (s *sessionAgentSuite) TestConnectFromOtherUser(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	// Mock connections to appear to come from a different user ID
	uid := uint32(sys.Geteuid())
	restore = agent.MockUcred(&syscall.Ucred{Uid: uid + 1}, nil)
	defer restore()

	sa := mylog.Check2(agent.New())

	sa.Start()
	defer sa.Stop()

	_ = mylog.Check2(s.client.Get("http://localhost/v1/session-info"))
	// This could be an EOF error or a failed read, depending on timing
	c.Assert(err, ErrorMatches, "Get \"?http://localhost/v1/session-info\"?: .*")
	logger.WithLoggerLock(func() {
		c.Check(logbuf.String(), testutil.Contains, "Blocking request from user ID")
	})
}

func (s *sessionAgentSuite) TestConnectFromRoot(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	// Mock connections to appear to come from root
	restore = agent.MockUcred(&syscall.Ucred{Uid: 0}, nil)
	defer restore()

	sa := mylog.Check2(agent.New())

	sa.Start()
	defer sa.Stop()

	response := mylog.Check2(s.client.Get("http://localhost/v1/session-info"))

	defer response.Body.Close()
	c.Check(response.StatusCode, Equals, 200)
	logger.WithLoggerLock(func() {
		c.Check(logbuf.String(), Equals, "")
	})
}

func (s *sessionAgentSuite) TestConnectWithFailedPeerCredentials(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	// Connections are dropped if peer credential lookup fails.
	restore = agent.MockUcred(nil, fmt.Errorf("SO_PEERCRED failed"))
	defer restore()

	sa := mylog.Check2(agent.New())

	sa.Start()
	defer sa.Stop()

	_ = mylog.Check2(s.client.Get("http://localhost/v1/session-info"))
	c.Assert(err, ErrorMatches, "Get \"?http://localhost/v1/session-info\"?: .*")
	logger.WithLoggerLock(func() {
		c.Check(logbuf.String(), testutil.Contains, "Failed to retrieve peer credentials: SO_PEERCRED failed")
	})
}
