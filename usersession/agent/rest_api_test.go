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
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/mvo5/goconfigparser"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/desktop/notification"
	"github.com/snapcore/snapd/desktop/notification/notificationtest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/agent"
	"github.com/snapcore/snapd/usersession/client"
)

type restSuite struct {
	testutil.BaseTest
	testutil.DBusTest
	agent  *agent.SessionAgent
	notify *notificationtest.FdoServer
}

var _ = Suite(&restSuite{})

func (s *restSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.DBusTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	c.Assert(os.MkdirAll(xdgRuntimeDir, 0700), IsNil)

	var err error
	s.notify, err = notificationtest.NewFdoServer()
	c.Assert(err, IsNil)
	s.AddCleanup(func() { s.notify.Stop() })

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
	Result any                `json:"result"`
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
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeSync)
	c.Check(rsp.Result, DeepEquals, map[string]any{
		"version": "42b1",
	})
}

func (s *restSuite) TestPostPendingRefreshNotificationMalformedContentType(c *C) {
	req := httptest.NewRequest("POST", "/v1/notifications/pending-refresh", bytes.NewBufferString(""))
	req.Header.Set("Content-Type", "text/plain/joke")
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 400)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]any{"message": "cannot parse content type: mime: unexpected content after media subtype"})
}

func (s *restSuite) TestPostPendingRefreshNotificationUnsupportedContentType(c *C) {
	req := httptest.NewRequest("POST", "/v1/notifications/pending-refresh", bytes.NewBufferString(""))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 400)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]any{"message": "unknown content type: text/plain"})
}

func (s *restSuite) TestPostPendingRefreshNotificationUnsupportedContentEncoding(c *C) {
	req := httptest.NewRequest("POST", "/v1/notifications/pending-refresh", bytes.NewBufferString(""))
	req.Header.Set("Content-Type", "application/json; charset=EBCDIC")
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 400)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]any{"message": "unknown charset in content type: application/json; charset=EBCDIC"})
}

func (s *restSuite) TestPostPendingRefreshNotificationMalformedRequestBody(c *C) {
	req := httptest.NewRequest("POST", "/v1/notifications/pending-refresh",
		bytes.NewBufferString(`{"instance-name":syntaxerror}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 400)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]any{"message": "cannot decode request body into pending snap refresh info: invalid character 's' looking for beginning of value"})
}

func (s *restSuite) TestPostPendingRefreshNotificationNoSessionBus(c *C) {
	restore := agent.MockNoBus(s.agent)
	defer restore()

	req := httptest.NewRequest("POST", "/v1/notifications/pending-refresh",
		bytes.NewBufferString(`{"instance-name":"pkg"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 500)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]any{"message": "cannot connect to the session bus"})
}

func (s *restSuite) testPostPendingRefreshNotificationBody(c *C, refreshInfo *client.PendingSnapRefreshInfo) {
	reqBody, err := json.Marshal(refreshInfo)
	c.Assert(err, IsNil)
	req := httptest.NewRequest("POST", "/v1/notifications/pending-refresh", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeSync)
	c.Check(rsp.Result, IsNil)
}

func (s *restSuite) TestPostPendingRefreshNotificationHappeningNow(c *C) {
	refreshInfo := &client.PendingSnapRefreshInfo{InstanceName: "pkg"}
	s.testPostPendingRefreshNotificationBody(c, refreshInfo)
	notifications := s.notify.GetAll()
	c.Assert(notifications, HasLen, 1)
	n := notifications[0]
	c.Check(n.AppName, Equals, "")
	c.Check(n.Icon, Equals, "")
	c.Check(n.Summary, Equals, `pkg is updating now!`)
	c.Check(n.Body, Equals, "")
	c.Check(n.Actions, DeepEquals, []string{})
	c.Check(n.Hints, DeepEquals, map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(notification.CriticalUrgency)),
		"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
	})
	c.Check(n.Expires, Equals, int32(0))
}

func (s *restSuite) TestPostPendingRefreshNotificationFewDays(c *C) {
	refreshInfo := &client.PendingSnapRefreshInfo{
		InstanceName:  "pkg",
		TimeRemaining: time.Hour * 72,
	}
	s.testPostPendingRefreshNotificationBody(c, refreshInfo)
	notifications := s.notify.GetAll()
	c.Assert(notifications, HasLen, 1)
	n := notifications[0]
	// boring stuff is checked above
	c.Check(n.Summary, Equals, `Update available for pkg.`)
	c.Check(n.Body, Equals, "Close the application to update now. It will update automatically in 3 days.")
	c.Check(n.Hints, DeepEquals, map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(notification.LowUrgency)),
		"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
	})
}

func (s *restSuite) TestPostPendingRefreshNotificationFewHours(c *C) {
	refreshInfo := &client.PendingSnapRefreshInfo{
		InstanceName:  "pkg",
		TimeRemaining: time.Hour * 7,
	}
	s.testPostPendingRefreshNotificationBody(c, refreshInfo)
	notifications := s.notify.GetAll()
	c.Assert(notifications, HasLen, 1)
	n := notifications[0]
	// boring stuff is checked above
	c.Check(n.Summary, Equals, `Update available for pkg.`)
	c.Check(n.Body, Equals, "Close the application to update now. It will update automatically in 7 hours.")
	c.Check(n.Hints, DeepEquals, map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(notification.NormalUrgency)),
		"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
	})
}

func (s *restSuite) TestPostPendingRefreshNotificationFewMinutes(c *C) {
	refreshInfo := &client.PendingSnapRefreshInfo{
		InstanceName:  "pkg",
		TimeRemaining: time.Minute * 15,
	}
	s.testPostPendingRefreshNotificationBody(c, refreshInfo)
	notifications := s.notify.GetAll()
	c.Assert(notifications, HasLen, 1)
	n := notifications[0]
	// boring stuff is checked above
	c.Check(n.Summary, Equals, `Update available for pkg.`)
	c.Check(n.Body, Equals, "Close the application to update now. It will update automatically in 15 minutes.")
	c.Check(n.Hints, DeepEquals, map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(notification.CriticalUrgency)),
		"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
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
	err = os.WriteFile(desktopFilePath, []byte(`
[Desktop Entry]
Icon=app.png
	`), 0644)
	c.Assert(err, IsNil)

	s.testPostPendingRefreshNotificationBody(c, refreshInfo)
	notifications := s.notify.GetAll()
	c.Assert(notifications, HasLen, 1)
	n := notifications[0]
	// boring stuff is checked above
	c.Check(n.Icon, Equals, "app.png")
	c.Check(n.Hints, DeepEquals, map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(notification.CriticalUrgency)),
		"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
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
	err = os.WriteFile(desktopFilePath, []byte(`garbage!`), 0644)
	c.Assert(err, IsNil)

	s.testPostPendingRefreshNotificationBody(c, refreshInfo)
	notifications := s.notify.GetAll()
	c.Assert(notifications, HasLen, 1)
	n := notifications[0]
	// boring stuff is checked above
	c.Check(n.Icon, Equals, "") // Icon is not provided
	c.Check(n.Hints, DeepEquals, map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(notification.CriticalUrgency)),
		"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
	})
}

func (s *restSuite) TestPostPendingRefreshNotificationNotificationServerFailure(c *C) {
	s.notify.SetError(&dbus.Error{Name: "org.freedesktop.DBus.Error.Failed"})

	refreshInfo := &client.PendingSnapRefreshInfo{
		InstanceName: "pkg",
	}
	reqBody, err := json.Marshal(refreshInfo)
	c.Assert(err, IsNil)
	req := httptest.NewRequest("POST", "/v1/notifications/pending-refresh", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	agent.PendingRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 500)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeError)
	c.Check(rsp.Result, DeepEquals, map[string]any{"message": "cannot send notification message: org.freedesktop.DBus.Error.Failed"})
}

func (s *restSuite) testPostFinishRefreshNotificationBody(c *C, refreshInfo *client.FinishedSnapRefreshInfo) {
	reqBody, err := json.Marshal(refreshInfo)
	c.Assert(err, IsNil)
	req := httptest.NewRequest("POST", "/v1/notifications/finish-refresh", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	agent.FinishRefreshNotificationCmd.POST(agent.PendingRefreshNotificationCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeSync)
	c.Check(rsp.Result, IsNil)
}

func (s *restSuite) TestPostCloseRefreshNotification(c *C) {
	// add a notification first
	refreshInfo := &client.PendingSnapRefreshInfo{InstanceName: "some-snap"}
	s.testPostPendingRefreshNotificationBody(c, refreshInfo)

	closeInfo := &client.FinishedSnapRefreshInfo{InstanceName: "some-snap"}
	s.testPostFinishRefreshNotificationBody(c, closeInfo)
	notifications := s.notify.GetAll()
	c.Assert(notifications, HasLen, 1)
	n := notifications[0]
	// boring stuff is checked above
	c.Check(n.Summary, Equals, `some-snap was updated.`)
	c.Check(n.Body, Equals, "Ready to launch.")
	c.Check(n.Hints, DeepEquals, map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(notification.LowUrgency)),
		"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
	})
}

func createDesktopFile(c *C, desktopFilePath string, icon string, name string, localizedNames map[string]string) {
	data := []byte("[Desktop Entry]\nName=" + name + "\n")
	if icon != "" {
		data = append(data, []byte("Icon="+icon+"\n")...)
	}
	for key, value := range localizedNames {
		data = append(data, []byte("Name["+key+"]="+value+"\n")...)
	}
	c.Assert(os.MkdirAll(path.Dir(desktopFilePath), 0755), IsNil)
	c.Assert(os.WriteFile(desktopFilePath, data, 0644), IsNil)
}

func createLocalizedDesktopFile(c *C, name string, localizedNames map[string]string) *goconfigparser.ConfigParser {
	tmpFile := filepath.Join(c.MkDir(), "desktop.desktop")
	createDesktopFile(c, tmpFile, "", name, localizedNames)
	parser := goconfigparser.New()
	c.Assert(parser.ReadFile(tmpFile), IsNil)
	return parser
}

func createSnapInfo(snapName string) *snap.Info {
	si := snap.Info{
		SideInfo: snap.SideInfo{
			RealName: snapName,
		},
		Apps: make(map[string]*snap.AppInfo, 5),
	}
	return &si
}

func addAppToSnap(c *C, snapinfo *snap.Info, app string, isService bool, icon string, name string) {
	newInfo := snap.AppInfo{
		Snap: snapinfo,
		Name: app,
	}
	if isService {
		newInfo.Daemon = "daemon"
	}
	snapinfo.Apps[app] = &newInfo
	createDesktopFile(c, newInfo.DesktopFile(), icon, name, nil)
}

func (s *restSuite) TestGuessAppDataNoIconPrefixEqualApp(c *C) {
	si := createSnapInfo("app1")
	addAppToSnap(c, si, "app1", false, "", "")
	icon, name := agent.GuessAppData(si, "", "")
	c.Check(icon, Equals, "")
	c.Check(name, Equals, "")
}

func (s *restSuite) TestGuessAppDataNoIconPrefixDifferentApp(c *C) {
	si := createSnapInfo("snap1")
	addAppToSnap(c, si, "app1", false, "", "")
	icon, name := agent.GuessAppData(si, "", "")
	c.Check(icon, Equals, "")
	c.Check(name, Equals, "")
}

func (s *restSuite) TestGuessAppDataPrefixDifferentApp(c *C) {
	si := createSnapInfo("snap1")
	addAppToSnap(c, si, "app1", false, "iconname", "appname")
	icon, name := agent.GuessAppData(si, "", "")
	c.Check(icon, Equals, "iconname")
	c.Check(name, Equals, "appname")
}

func (s *restSuite) TestGuessAppDataPrefixEqualApp(c *C) {
	si := createSnapInfo("app1")
	addAppToSnap(c, si, "app1", false, "iconname1", "appname1")
	addAppToSnap(c, si, "app2", false, "iconname2", "appname2")
	icon, name := agent.GuessAppData(si, "", "")
	c.Check(icon, Equals, "iconname1")
	c.Check(name, Equals, "appname1")
}

func (s *restSuite) TestGuessAppDataServicePrefixEqualApp(c *C) {
	si := createSnapInfo("app1")
	addAppToSnap(c, si, "app1", true, "iconname", "appname")
	icon, name := agent.GuessAppData(si, "", "")
	c.Check(icon, Equals, "")
	c.Check(name, Equals, "")
}

func (s *restSuite) TestGuessAppDataServicePrefixDifferentApp(c *C) {
	si := createSnapInfo("snap1")
	addAppToSnap(c, si, "app1", true, "iconname", "appname")
	icon, name := agent.GuessAppData(si, "", "")
	c.Check(icon, Equals, "")
	c.Check(name, Equals, "")
}

func (s *restSuite) TestGuessAppDataServiceTwoApps(c *C) {
	si := createSnapInfo("app1")
	addAppToSnap(c, si, "app1", true, "iconname1", "appname1")
	addAppToSnap(c, si, "app2", false, "iconname2", "appname2")
	icon, name := agent.GuessAppData(si, "", "")
	c.Check(icon, Equals, "iconname2")
	c.Check(name, Equals, "appname2")
}

func (s *restSuite) TestGuessAppDataServiceTwoAppsServices(c *C) {
	si := createSnapInfo("app1")
	addAppToSnap(c, si, "app1", true, "iconname1", "appname1")
	addAppToSnap(c, si, "app2", true, "iconname2", "appname2")
	icon, name := agent.GuessAppData(si, "", "")
	c.Check(icon, Equals, "")
	c.Check(name, Equals, "")
}

func (s *restSuite) TestGuessAppDataServiceTwoAppsOneServicePrefixDifferent(c *C) {
	si := createSnapInfo("snap1")
	addAppToSnap(c, si, "app1", true, "iconname1", "appname1")
	addAppToSnap(c, si, "app2", false, "iconname2", "appname2")
	icon, name := agent.GuessAppData(si, "", "")
	c.Check(icon, Equals, "iconname2")
	c.Check(name, Equals, "appname2")
}

func (s *restSuite) TestGuessAppDataTwoAppsPrefixDifferent(c *C) {
	si := createSnapInfo("snap1")
	addAppToSnap(c, si, "app1", false, "iconname1", "appname1")
	addAppToSnap(c, si, "app2", false, "iconname2", "appname2")
	icon, name := agent.GuessAppData(si, "", "")
	if (icon != "iconname1") && (icon != "iconname2") {
		c.Fail()
	}
	if (icon == "iconname1") && (name != "appname1") {
		c.Fail()
	}
	if (icon == "iconname2") && (name != "appname2") {
		c.Fail()
	}
}

func (s *restSuite) TestGuessAppDataWithKey(c *C) {
	si := createSnapInfo("snap1")
	addAppToSnap(c, si, "app1", false, "iconname", "appname")
	icon, name := agent.GuessAppData(si, "", "akey")
	c.Check(icon, Equals, "iconname")
	c.Check(name, Equals, "appname (akey)")
}

func (s *restSuite) TestPostCloseRefreshNotificationWithIconDefault(c *C) {
	snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	// add a notification first
	mockYaml := `
name: snap-name
apps:
  other-app:
    command: /bin/foo
  snap-name:
    command: /bin/foo
`
	snaptest.MockSnapCurrent(c, mockYaml[1:], &snap.SideInfo{
		Revision: snap.R("42"),
	})

	desktopEntry := `
[Desktop Entry]
Icon=foo.png
`
	os.MkdirAll(dirs.SnapDesktopFilesDir, 0755)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapDesktopFilesDir, "snap-name_snap-name.desktop"), []byte(desktopEntry[1:]), 0644), IsNil)
	refreshInfo := &client.FinishedSnapRefreshInfo{InstanceName: "snap-name"}
	s.testPostFinishRefreshNotificationBody(c, refreshInfo)

	notifications := s.notify.GetAll()
	c.Assert(notifications, HasLen, 1)
	n := notifications[0]
	c.Check(n.Summary, Equals, `snap-name was updated.`)
	c.Check(n.Body, Equals, "Ready to launch.")
	c.Check(n.Hints, DeepEquals, map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(notification.LowUrgency)),
		"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
	})
	// boring stuff is checked above
	c.Check(n.Icon, Equals, "foo.png")
}

func (s *restSuite) TestLocalizedDesktopNameNoLocale(c *C) {
	restore := agent.MockCurrentLocale("")
	s.AddCleanup(restore)
	localizedNames := map[string]string{
		"es":    "testapp_es",
		"es_ES": "testapp_es_ES",
		"es_AR": "testapp_es_AR",
		"en_US": "testapp_en_US",
		"en":    "testapp_en",
	}
	parser := createLocalizedDesktopFile(c, "testapp", localizedNames)
	name := agent.GetLocalizedAppNameFromDesktopFile(parser, "defaultName")
	c.Assert(name, Equals, "testapp")
}

func (s *restSuite) TestLocalizedDesktopNameFullLocale(c *C) {
	restore := agent.MockCurrentLocale("es_ES")
	s.AddCleanup(restore)
	localizedNames := map[string]string{
		"es":    "testapp_es",
		"es_ES": "testapp_es_ES",
		"es_AR": "testapp_es_AR",
		"en_US": "testapp_en_US",
		"en":    "testapp_en",
	}
	parser := createLocalizedDesktopFile(c, "testapp", localizedNames)
	name := agent.GetLocalizedAppNameFromDesktopFile(parser, "defaultName")
	c.Assert(name, Equals, "testapp_es_ES")
}

func (s *restSuite) TestLocalizedDesktopNamePartialLocale(c *C) {
	restore := agent.MockCurrentLocale("es_ES")
	s.AddCleanup(restore)
	localizedNames := map[string]string{
		"es":    "testapp_es",
		"es_AR": "testapp_es_AR",
		"en_US": "testapp_en_US",
		"en":    "testapp_en",
	}
	parser := createLocalizedDesktopFile(c, "testapp", localizedNames)
	name := agent.GetLocalizedAppNameFromDesktopFile(parser, "defaultName")
	c.Assert(name, Equals, "testapp_es")
}

func (s *restSuite) TestLocalizedDesktopNameLocaleNotFound(c *C) {
	restore := agent.MockCurrentLocale("es_ES")
	s.AddCleanup(restore)
	localizedNames := map[string]string{
		"es_AR": "testapp_es_AR",
		"en_US": "testapp_en_US",
		"en":    "testapp_en",
	}
	parser := createLocalizedDesktopFile(c, "testapp", localizedNames)
	name := agent.GetLocalizedAppNameFromDesktopFile(parser, "defaultName")
	c.Assert(name, Equals, "testapp")
}

func (s *restSuite) TestPostPendingRefreshNotificationTestInstanceKeyHappeningNow(c *C) {
	refreshInfo := &client.PendingSnapRefreshInfo{InstanceName: "pkg_devel"}
	s.testPostPendingRefreshNotificationBody(c, refreshInfo)
	notifications := s.notify.GetAll()
	c.Assert(notifications, HasLen, 1)
	n := notifications[0]
	c.Check(n.AppName, Equals, "")
	c.Check(n.Icon, Equals, "")
	c.Check(n.Summary, Equals, `pkg (devel) is updating now!`)
	c.Check(n.Body, Equals, "")
	c.Check(n.Actions, DeepEquals, []string{})
	c.Check(n.Hints, DeepEquals, map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(notification.CriticalUrgency)),
		"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
	})
	c.Check(n.Expires, Equals, int32(0))
}

func (s *restSuite) TestPostPendingRefreshNotificationTestInstanceKeyWithDesktopFileHappeningNow(c *C) {
	restore := agent.MockCurrentLocale("es_ES")
	s.AddCleanup(restore)
	localizedNames := map[string]string{
		"es":    "pkg_es",
		"es_ES": "pkg_es_ES",
		"es_AR": "pkg_es_AR",
		"en_US": "pkg_en_US",
		"en":    "pkg_en",
	}
	tmpFile := filepath.Join(dirs.SnapDesktopFilesDir, "desktop.desktop")
	createDesktopFile(c, tmpFile, "", "pkg", localizedNames)

	refreshInfo := &client.PendingSnapRefreshInfo{
		InstanceName:        "pkg_devel",
		BusyAppDesktopEntry: "desktop",
	}
	s.testPostPendingRefreshNotificationBody(c, refreshInfo)
	notifications := s.notify.GetAll()
	c.Assert(notifications, HasLen, 1)
	n := notifications[0]
	c.Check(n.AppName, Equals, "")
	c.Check(n.Icon, Equals, "")
	c.Check(n.Summary, Equals, `pkg_es_ES (devel) is updating now!`)
	c.Check(n.Body, Equals, "")
	c.Check(n.Actions, DeepEquals, []string{})
	c.Check(n.Hints, DeepEquals, map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(notification.CriticalUrgency)),
		"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
	})
	c.Check(n.Expires, Equals, int32(0))
}

func (s *restSuite) TestPostPendingRefreshNotificationTestInstanceKeyFewDays(c *C) {
	refreshInfo := &client.PendingSnapRefreshInfo{
		InstanceName:  "pkg_devel",
		TimeRemaining: time.Hour * 72,
	}
	s.testPostPendingRefreshNotificationBody(c, refreshInfo)
	notifications := s.notify.GetAll()
	c.Assert(notifications, HasLen, 1)
	n := notifications[0]
	// boring stuff is checked above
	c.Check(n.Summary, Equals, `Update available for pkg (devel).`)
	c.Check(n.Body, Equals, "Close the application to update now. It will update automatically in 3 days.")
	c.Check(n.Hints, DeepEquals, map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(notification.LowUrgency)),
		"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
	})
}

func (s *restSuite) TestPostPendingRefreshNotificationTestInstanceKeyWithDesktopFileFewDays(c *C) {
	restore := agent.MockCurrentLocale("es_ES")
	s.AddCleanup(restore)
	localizedNames := map[string]string{
		"es":    "pkg_es",
		"es_ES": "pkg_es_ES",
		"es_AR": "pkg_es_AR",
		"en_US": "pkg_en_US",
		"en":    "pkg_en",
	}
	tmpFile := filepath.Join(dirs.SnapDesktopFilesDir, "desktop.desktop")
	createDesktopFile(c, tmpFile, "", "pkg", localizedNames)

	refreshInfo := &client.PendingSnapRefreshInfo{
		InstanceName:        "pkg_devel",
		TimeRemaining:       time.Hour * 72,
		BusyAppDesktopEntry: "desktop",
	}
	s.testPostPendingRefreshNotificationBody(c, refreshInfo)
	notifications := s.notify.GetAll()
	c.Assert(notifications, HasLen, 1)
	n := notifications[0]
	// boring stuff is checked above
	c.Check(n.Summary, Equals, `Update available for pkg_es_ES (devel).`)
	c.Check(n.Body, Equals, "Close the application to update now. It will update automatically in 3 days.")
	c.Check(n.Hints, DeepEquals, map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(notification.LowUrgency)),
		"desktop-entry": dbus.MakeVariant("io.snapcraft.SessionAgent"),
	})
}
