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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/godbus/dbus"
	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dbusutil/dbustest"
	"github.com/snapcore/snapd/desktop/notification"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/agent"
	"github.com/snapcore/snapd/usersession/client"
)

type restSuite struct {
	testutil.BaseTest
	testutil.DBusTest
	sysdLog [][]string
	agent   *agent.SessionAgent

	fakeHome string
}

var _ = Suite(&restSuite{})

func (s *restSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.DBusTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	c.Assert(os.MkdirAll(xdgRuntimeDir, 0700), IsNil)

	s.sysdLog = nil
	restore := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	s.AddCleanup(restore)
	restore = systemd.MockStopDelays(time.Millisecond, 25*time.Second)
	s.AddCleanup(restore)
	restore = agent.MockStopTimeouts(20*time.Millisecond, time.Millisecond)
	s.AddCleanup(restore)

	s.fakeHome = c.MkDir()
	u, err := user.Current()
	c.Assert(err, IsNil)
	restore = agent.MockUserCurrent(func() (*user.User, error) {
		return &user.User{Uid: u.Uid, HomeDir: s.fakeHome}, nil
	})
	s.AddCleanup(restore)

	s.agent, err = agent.New()
	c.Assert(err, IsNil)
	s.agent.Start()
	s.AddCleanup(func() { s.agent.Stop() })
}

func (s *restSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.DBusTest.TearDownTest(c)
	s.BaseTest.TearDownTest(c)
}

type resp struct {
	Type   agent.ResponseType `json:"type"`
	Result interface{}        `json:"result"`
}

func (s *restSuite) TestSessionInfo(c *C) {
	// the agent.SessionInfo end point only supports GET requests
	c.Check(agent.SessionInfoCmd.PUT, IsNil)
	c.Check(agent.SessionInfoCmd.POST, IsNil)
	c.Check(agent.SessionInfoCmd.DELETE, IsNil)
	c.Assert(agent.SessionInfoCmd.GET, NotNil)

	c.Check(agent.SessionInfoCmd.Path, Equals, "/v1/session-info")

	s.agent.Version = "42b1"
	rec := httptest.NewRecorder()
	agent.SessionInfoCmd.GET(agent.SessionInfoCmd, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeSync)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{
		"version": "42b1",
	})
}

func (s *restSuite) TestServiceControl(c *C) {
	// the agent.Services end point only supports POST requests
	c.Assert(agent.ServiceControlCmd.GET, IsNil)
	c.Check(agent.ServiceControlCmd.PUT, IsNil)
	c.Check(agent.ServiceControlCmd.POST, NotNil)
	c.Check(agent.ServiceControlCmd.DELETE, IsNil)

	c.Check(agent.ServiceControlCmd.Path, Equals, "/v1/service-control")
}

func (s *restSuite) TestServiceControlDaemonReload(c *C) {
	s.testServiceControlDaemonReload(c, "application/json")
}

func (s *restSuite) TestServiceControlDaemonReloadComplexerContentType(c *C) {
	s.testServiceControlDaemonReload(c, "application/json; charset=utf-8")
}

func (s *restSuite) TestServiceControlDaemonReloadInvalidCharset(c *C) {
	req, err := http.NewRequest("POST", "/v1/service-control", bytes.NewBufferString(`{"action":"daemon-reload"}`))
	req.Header.Set("Content-Type", "application/json; charset=iso-8859-1")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.ServiceControlCmd.POST(agent.ServiceControlCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains,
		"unknown charset in content type")
}

func (s *restSuite) testServiceControlDaemonReload(c *C, contentType string) {
	req, err := http.NewRequest("POST", "/v1/service-control", bytes.NewBufferString(`{"action":"daemon-reload"}`))
	req.Header.Set("Content-Type", contentType)
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.ServiceControlCmd.POST(agent.ServiceControlCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeSync)
	c.Check(rsp.Result, IsNil)

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "daemon-reload"},
	})
}

func (s *restSuite) TestServiceControlStart(c *C) {
	req, err := http.NewRequest("POST", "/v1/service-control", bytes.NewBufferString(`{"action":"start","services":["snap.foo.service", "snap.bar.service"]}`))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.ServiceControlCmd.POST(agent.ServiceControlCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeSync)
	c.Check(rsp.Result, Equals, nil)

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "start", "snap.foo.service"},
		{"--user", "start", "snap.bar.service"},
	})
}

func (s *restSuite) TestServicesStartNonSnap(c *C) {
	req, err := http.NewRequest("POST", "/v1/service-control", bytes.NewBufferString(`{"action":"start","services":["snap.foo.service", "not-snap.bar.service"]}`))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.ServiceControlCmd.POST(agent.ServiceControlCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 500)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{
		"message": "cannot start non-snap service not-snap.bar.service",
	})

	// No services were started on the error.
	c.Check(s.sysdLog, HasLen, 0)
}

func (s *restSuite) TestServicesStartFailureStopsServices(c *C) {
	var sysdLog [][]string
	restore := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if cmd[0] == "--user" && cmd[1] == "start" && cmd[2] == "snap.bar.service" {
			return nil, fmt.Errorf("start failure")
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer restore()

	req, err := http.NewRequest("POST", "/v1/service-control", bytes.NewBufferString(`{"action":"start","services":["snap.foo.service", "snap.bar.service"]}`))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.ServiceControlCmd.POST(agent.ServiceControlCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 500)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{
		"message": "some user services failed to start",
		"kind":    "service-control",
		"value": map[string]interface{}{
			"start-errors": map[string]interface{}{
				"snap.bar.service": "start failure",
			},
			"stop-errors": map[string]interface{}{},
		},
	})

	c.Check(sysdLog, DeepEquals, [][]string{
		{"--user", "start", "snap.foo.service"},
		{"--user", "start", "snap.bar.service"},
		{"--user", "stop", "snap.foo.service"},
		{"--user", "show", "--property=ActiveState", "snap.foo.service"},
	})
}

func (s *restSuite) TestServicesStartFailureReportsStopFailures(c *C) {
	var sysdLog [][]string
	restore := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if cmd[0] == "--user" && cmd[1] == "start" && cmd[2] == "snap.bar.service" {
			return nil, fmt.Errorf("start failure")
		}
		if cmd[0] == "--user" && cmd[1] == "stop" && cmd[2] == "snap.foo.service" {
			return nil, fmt.Errorf("stop failure")
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer restore()

	req, err := http.NewRequest("POST", "/v1/service-control", bytes.NewBufferString(`{"action":"start","services":["snap.foo.service", "snap.bar.service"]}`))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.ServiceControlCmd.POST(agent.ServiceControlCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 500)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{
		"message": "some user services failed to start",
		"kind":    "service-control",
		"value": map[string]interface{}{
			"start-errors": map[string]interface{}{
				"snap.bar.service": "start failure",
			},
			"stop-errors": map[string]interface{}{
				"snap.foo.service": "stop failure",
			},
		},
	})

	c.Check(sysdLog, DeepEquals, [][]string{
		{"--user", "start", "snap.foo.service"},
		{"--user", "start", "snap.bar.service"},
		{"--user", "stop", "snap.foo.service"},
	})
}

func (s *restSuite) TestServicesStop(c *C) {
	req, err := http.NewRequest("POST", "/v1/service-control", bytes.NewBufferString(`{"action":"stop","services":["snap.foo.service", "snap.bar.service"]}`))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.ServiceControlCmd.POST(agent.ServiceControlCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeSync)
	c.Check(rsp.Result, Equals, nil)

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "stop", "snap.foo.service"},
		{"--user", "show", "--property=ActiveState", "snap.foo.service"},
		{"--user", "stop", "snap.bar.service"},
		{"--user", "show", "--property=ActiveState", "snap.bar.service"},
	})
}

func (s *restSuite) TestServicesStopNonSnap(c *C) {
	req, err := http.NewRequest("POST", "/v1/service-control", bytes.NewBufferString(`{"action":"stop","services":["snap.foo.service", "not-snap.bar.service"]}`))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.ServiceControlCmd.POST(agent.ServiceControlCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 500)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{
		"message": "cannot stop non-snap service not-snap.bar.service",
	})

	// No services were started on the error.
	c.Check(s.sysdLog, HasLen, 0)
}

func (s *restSuite) TestServicesStopReportsTimeout(c *C) {
	var sysdLog [][]string
	restore := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		// Ignore "show" spam
		if cmd[1] != "show" {
			sysdLog = append(sysdLog, cmd)
		}
		if cmd[len(cmd)-1] == "snap.bar.service" {
			return []byte("ActiveState=active\n"), nil
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer restore()

	req, err := http.NewRequest("POST", "/v1/service-control", bytes.NewBufferString(`{"action":"stop","services":["snap.foo.service", "snap.bar.service"]}`))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.ServiceControlCmd.POST(agent.ServiceControlCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 500)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{
		"message": "some user services failed to stop",
		"kind":    "service-control",
		"value": map[string]interface{}{
			"stop-errors": map[string]interface{}{
				"snap.bar.service": "snap.bar.service failed to stop: timeout",
			},
		},
	})

	c.Check(sysdLog, DeepEquals, [][]string{
		{"--user", "stop", "snap.foo.service"},
		{"--user", "stop", "snap.bar.service"},
	})
}

func (s *restSuite) TestPostPendingRefreshNotificationMalformedContentType(c *C) {
	req, err := http.NewRequest("POST", "/v1/notifications/pending-refresh", bytes.NewBufferString(""))
	req.Header.Set("Content-Type", "text/plain/joke")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 400)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{"message": "cannot parse content type: mime: unexpected content after media subtype"})
}

func (s *restSuite) TestPostPendingRefreshNotificationUnsupportedContentType(c *C) {
	req, err := http.NewRequest("POST", "/v1/notifications/pending-refresh", bytes.NewBufferString(""))
	req.Header.Set("Content-Type", "text/plain")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 400)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{"message": "unknown content type: text/plain"})
}

func (s *restSuite) TestPostPendingRefreshNotificationUnsupportedContentEncoding(c *C) {
	req, err := http.NewRequest("POST", "/v1/notifications/pending-refresh", bytes.NewBufferString(""))
	req.Header.Set("Content-Type", "application/json; charset=EBCDIC")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 400)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{"message": "unknown charset in content type: application/json; charset=EBCDIC"})
}

func (s *restSuite) TestPostPendingRefreshNotificationMalformedRequestBody(c *C) {
	req, err := http.NewRequest("POST", "/v1/notifications/pending-refresh",
		bytes.NewBufferString(`{"instance-name":syntaxerror}`))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 400)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{"message": "cannot decode request body into pending snap refresh info: invalid character 's' looking for beginning of value"})
}

func (s *restSuite) TestPostPendingRefreshNotificationNoSessionBus(c *C) {
	noDBus := func() (*dbus.Conn, error) {
		return nil, fmt.Errorf("cannot find bus")
	}
	restore := dbusutil.MockConnections(noDBus, noDBus)
	defer restore()

	req, err := http.NewRequest("POST", "/v1/notifications/pending-refresh",
		bytes.NewBufferString(`{"instance-name":"pkg"}`))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 500)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{"message": "cannot connect to the session bus: cannot find bus"})
}

func (s *restSuite) testPostPendingRefreshNotificationBody(c *C, refreshInfo *client.PendingSnapRefreshInfo, checkMsg func(c *C, msg *dbus.Message)) {
	conn, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		if checkMsg != nil {
			checkMsg(c, msg)
		}
		responseSig := dbus.SignatureOf(uint32(0))
		response := &dbus.Message{
			Type: dbus.TypeMethodReply,
			Headers: map[dbus.HeaderField]dbus.Variant{
				dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
				dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
				// dbus.FieldDestination is provided automatically by DBus test helper.
				dbus.FieldSignature: dbus.MakeVariant(responseSig),
			},
			Body: []interface{}{uint32(7)}, // NotificationID (ignored for now)
		}
		return []*dbus.Message{response}, nil
	})
	c.Assert(err, IsNil)
	restore := dbusutil.MockOnlySessionBusAvailable(conn)
	defer restore()

	reqBody, err := json.Marshal(refreshInfo)
	c.Assert(err, IsNil)
	req, err := http.NewRequest("POST", "/v1/notifications/pending-refresh", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeSync)
	c.Check(rsp.Result, IsNil)
}

func (s *restSuite) TestPostPendingRefreshNotificationHappeningNow(c *C) {
	refreshInfo := &client.PendingSnapRefreshInfo{InstanceName: "pkg"}
	s.testPostPendingRefreshNotificationBody(c, refreshInfo, func(c *C, msg *dbus.Message) {
		c.Check(msg.Body[0], Equals, "")
		c.Check(msg.Body[1], Equals, uint32(0))
		c.Check(msg.Body[2], Equals, "")
		c.Check(msg.Body[3], Equals, `Snap "pkg" is refreshing now!`)
		c.Check(msg.Body[4], Equals, "")
		c.Check(msg.Body[5], HasLen, 0)
		c.Check(msg.Body[6], DeepEquals, map[string]dbus.Variant{
			"urgency":       dbus.MakeVariant(byte(notification.CriticalUrgency)),
			"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
		})
		c.Check(msg.Body[7], Equals, int32(0))
	})
}

func (s *restSuite) TestPostPendingRefreshNotificationFewDays(c *C) {
	refreshInfo := &client.PendingSnapRefreshInfo{
		InstanceName:  "pkg",
		TimeRemaining: time.Hour * 72,
	}
	s.testPostPendingRefreshNotificationBody(c, refreshInfo, func(c *C, msg *dbus.Message) {
		c.Check(msg.Body[3], Equals, `Pending update of "pkg" snap`)
		c.Check(msg.Body[4], Equals, "Close the app to avoid disruptions (3 days left)")
		c.Check(msg.Body[6], DeepEquals, map[string]dbus.Variant{
			"urgency":       dbus.MakeVariant(byte(notification.LowUrgency)),
			"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
		})
		c.Check(msg.Body[7], Equals, int32(0))
	})
}

func (s *restSuite) TestPostPendingRefreshNotificationFewHours(c *C) {
	refreshInfo := &client.PendingSnapRefreshInfo{
		InstanceName:  "pkg",
		TimeRemaining: time.Hour * 7,
	}
	s.testPostPendingRefreshNotificationBody(c, refreshInfo, func(c *C, msg *dbus.Message) {
		// boring stuff is checked above
		c.Check(msg.Body[3], Equals, `Pending update of "pkg" snap`)
		c.Check(msg.Body[4], Equals, "Close the app to avoid disruptions (7 hours left)")
		c.Check(msg.Body[6], DeepEquals, map[string]dbus.Variant{
			"urgency":       dbus.MakeVariant(byte(notification.NormalUrgency)),
			"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
		})
	})
}

func (s *restSuite) TestPostPendingRefreshNotificationFewMinutes(c *C) {
	refreshInfo := &client.PendingSnapRefreshInfo{
		InstanceName:  "pkg",
		TimeRemaining: time.Minute * 15,
	}
	s.testPostPendingRefreshNotificationBody(c, refreshInfo, func(c *C, msg *dbus.Message) {
		// boring stuff is checked above
		c.Check(msg.Body[3], Equals, `Pending update of "pkg" snap`)
		c.Check(msg.Body[4], Equals, "Close the app to avoid disruptions (15 minutes left)")
		c.Check(msg.Body[6], DeepEquals, map[string]dbus.Variant{
			"urgency":       dbus.MakeVariant(byte(notification.CriticalUrgency)),
			"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
		})
	})
}

func (s *restSuite) TestPostPendingRefreshNotificationBusyAppDesktopFile(c *C) {
	refreshInfo := &client.PendingSnapRefreshInfo{
		InstanceName:        "pkg",
		BusyAppName:         "app",
		BusyAppDesktopEntry: "pkg_app",
	}
	err := os.MkdirAll(dirs.SnapDesktopFilesDir, 0755)
	c.Assert(err, IsNil)
	desktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "pkg_app.desktop")
	err = ioutil.WriteFile(desktopFilePath, []byte(`
[Desktop Entry]
Icon=app.png
	`), 0644)
	c.Assert(err, IsNil)

	s.testPostPendingRefreshNotificationBody(c, refreshInfo, func(c *C, msg *dbus.Message) {
		// boring stuff is checked above
		c.Check(msg.Body[2], Equals, "app.png")
		c.Check(msg.Body[6], DeepEquals, map[string]dbus.Variant{
			"urgency":       dbus.MakeVariant(byte(notification.CriticalUrgency)),
			"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
		})
	})
}

func (s *restSuite) TestPostPendingRefreshNotificationBusyAppMalformedDesktopFile(c *C) {
	refreshInfo := &client.PendingSnapRefreshInfo{
		InstanceName:        "pkg",
		BusyAppName:         "app",
		BusyAppDesktopEntry: "pkg_app",
	}
	err := os.MkdirAll(dirs.SnapDesktopFilesDir, 0755)
	c.Assert(err, IsNil)
	desktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "pkg_app.desktop")
	err = ioutil.WriteFile(desktopFilePath, []byte(`garbage!`), 0644)
	c.Assert(err, IsNil)

	s.testPostPendingRefreshNotificationBody(c, refreshInfo, func(c *C, msg *dbus.Message) {
		// boring stuff is checked above
		c.Check(msg.Body[2], Equals, "") // Icon is not provided
		c.Check(msg.Body[6], DeepEquals, map[string]dbus.Variant{
			"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
			"urgency":       dbus.MakeVariant(byte(notification.CriticalUrgency)),
		})
	})
}

func (s *restSuite) TestPostPendingRefreshNotificationNoNotificationServer(c *C) {
	conn, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		response := &dbus.Message{
			Type: dbus.TypeError,
			Headers: map[dbus.HeaderField]dbus.Variant{
				dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
				dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
				// dbus.FieldDestination is provided automatically by DBus test helper.
				dbus.FieldErrorName: dbus.MakeVariant("org.freedesktop.DBus.Error.NameHasNoOwner"),
			},
		}
		return []*dbus.Message{response}, nil
	})
	c.Assert(err, IsNil)
	restore := dbusutil.MockOnlySessionBusAvailable(conn)
	defer restore()

	refreshInfo := &client.PendingSnapRefreshInfo{
		InstanceName: "pkg",
	}
	reqBody, err := json.Marshal(refreshInfo)
	c.Assert(err, IsNil)
	req, err := http.NewRequest("POST", "/v1/notifications/pending-refresh", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 500)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{"message": "cannot send notification message: org.freedesktop.DBus.Error.NameHasNoOwner"})
}
