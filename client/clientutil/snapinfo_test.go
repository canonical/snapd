// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2020 Canonical Ltd
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

package clientutil_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type cmdSuite struct {
	testutil.BaseTest
}

func (s *cmdSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

var _ = Suite(&cmdSuite{})

func (*cmdSuite) TestClientSnapFromSnapInfo(c *C) {
	si := &snap.Info{
		SnapType:      snap.TypeApp,
		SuggestedName: "",
		InstanceKey:   "insta",
		Version:       "v1",
		Confinement:   snap.StrictConfinement,
		License:       "Proprietary",
		Publisher: snap.StoreAccount{
			ID:          "ZvtzsxbsHivZLdvzrt0iqW529riGLfXJ",
			Username:    "thingyinc",
			DisplayName: "Thingy Inc.",
			Validation:  "unproven",
		},
		Base: "core18",
		SideInfo: snap.SideInfo{
			RealName:          "the-snap",
			SnapID:            "snapidid",
			Revision:          snap.R(99),
			EditedTitle:       "the-title",
			EditedSummary:     "the-summary",
			EditedDescription: "the-description",
			Channel:           "latest/stable",
			EditedLinks: map[string][]string{
				"contact": {"https://thingy.com"},
				"website": {"http://example.com/thingy"},
			},
			LegacyEditedContact: "https://thingy.com",
			Private:             true,
		},
		Channels: map[string]*snap.ChannelSnapInfo{},
		Tracks:   []string{},
		Prices:   map[string]float64{},
		Media: []snap.MediaInfo{
			{Type: "icon", URL: "https://dashboard.snapcraft.io/site_media/appmedia/2017/12/Thingy.png"},
			{Type: "screenshot", URL: "https://dashboard.snapcraft.io/site_media/appmedia/2018/01/Thingy_01.png"},
			{Type: "screenshot", URL: "https://dashboard.snapcraft.io/site_media/appmedia/2018/01/Thingy_02.png", Width: 600, Height: 200},
		},
		CommonIDs: []string{"org.thingy"},
		StoreURL:  "https://snapcraft.io/thingy",
		Broken:    "broken",
		Categories: []snap.CategoryInfo{
			{Featured: true, Name: "featured"},
			{Featured: false, Name: "productivity"},
		},
	}
	mylog.
		// valid InstallDate
		Check(os.MkdirAll(si.MountDir(), 0755))

	mylog.Check(os.Symlink(si.Revision.String(), filepath.Join(filepath.Dir(si.MountDir()), "current")))


	ci := mylog.Check2(clientutil.ClientSnapFromSnapInfo(si, nil))
	c.Check(err, IsNil)

	// check that fields are filled
	// see daemon/snap.go for fields filled after this
	expectedZeroFields := []string{
		"Screenshots", // unused nowadays
		"DownloadSize",
		"InstalledSize",
		"Health",
		"Status",
		"TrackingChannel",
		"IgnoreValidation",
		"CohortKey",
		"DevMode",
		"TryMode",
		"JailMode",
		"MountedFrom",
		"Hold",
		"GatingHold",
		"RefreshInhibit",
	}
	var checker func(string, reflect.Value)
	checker = func(pfx string, x reflect.Value) {
		t := x.Type()
		for i := 0; i < x.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				// not exported, ignore
				continue
			}
			v := x.Field(i)
			if f.Anonymous {
				checker(pfx+f.Name+".", v)
				continue
			}
			if reflect.DeepEqual(v.Interface(), reflect.Zero(f.Type).Interface()) {
				name := pfx + f.Name
				c.Check(expectedZeroFields, testutil.Contains, name, Commentf("%s not set", name))
			}
		}
	}
	x := reflect.ValueOf(ci).Elem()
	checker("", x)

	// check some values
	c.Check(ci.Name, Equals, "the-snap_insta")
	c.Check(ci.Type, Equals, "app")
	c.Check(ci.ID, Equals, si.ID())
	c.Check(ci.Revision, Equals, snap.R(99))
	c.Check(ci.Version, Equals, "v1")
	c.Check(ci.Title, Equals, "the-title")
	c.Check(ci.Summary, Equals, "the-summary")
	c.Check(ci.Description, Equals, "the-description")
	c.Check(ci.Icon, Equals, si.Media.IconURL())
	c.Check(ci.Links, DeepEquals, si.Links())
	c.Check(ci.Links, DeepEquals, si.EditedLinks)
	c.Check(ci.Contact, Equals, si.Contact())
	c.Check(ci.Website, Equals, si.Website())
	c.Check(ci.StoreURL, Equals, si.StoreURL)
	c.Check(ci.Developer, Equals, "thingyinc")
	c.Check(ci.Publisher, DeepEquals, &si.Publisher)
	c.Check(ci.Categories, DeepEquals, si.Categories)
}

type testStatusDecorator struct {
	calls int
}

func (sd *testStatusDecorator) DecorateWithStatus(appInfo *client.AppInfo, app *snap.AppInfo) error {
	sd.calls++
	if appInfo.Snap != app.Snap.InstanceName() || appInfo.Name != app.Name {
		panic("mismatched")
	}
	appInfo.Enabled = true
	appInfo.Active = true
	return nil
}

func (*cmdSuite) TestClientSnapFromSnapInfoAppsInactive(c *C) {
	si := &snap.Info{
		SnapType:      snap.TypeApp,
		SuggestedName: "",
		InstanceKey:   "insta",
		SideInfo: snap.SideInfo{
			RealName: "the-snap",
			SnapID:   "snapidid",
			Revision: snap.R(99),
		},
	}
	si.Apps = map[string]*snap.AppInfo{
		"svc": {Snap: si, Name: "svc", Daemon: "simple", DaemonScope: snap.SystemDaemon},
		"app": {Snap: si, Name: "app", CommonID: "common.id"},
	}
	// validity
	c.Check(si.IsActive(), Equals, false)
	// desktop file
	df := si.Apps["app"].DesktopFile()
	mylog.Check(os.MkdirAll(filepath.Dir(df), 0755))

	mylog.Check(os.WriteFile(df, nil, 0644))


	sd := &testStatusDecorator{}
	ci := mylog.Check2(clientutil.ClientSnapFromSnapInfo(si, sd))
	c.Check(err, IsNil)

	c.Check(ci.Name, Equals, "the-snap_insta")
	c.Check(ci.Apps, DeepEquals, []client.AppInfo{
		{
			Snap:        "the-snap_insta",
			Name:        "app",
			CommonID:    "common.id",
			DesktopFile: df,
		},
		{
			Snap:        "the-snap_insta",
			Name:        "svc",
			Daemon:      "simple",
			DaemonScope: snap.SystemDaemon,
		},
	})
	// not called on inactive snaps
	c.Check(sd.calls, Equals, 0)
}

func (*cmdSuite) TestClientSnapFromSnapInfoAppsActive(c *C) {
	si := &snap.Info{
		SnapType:      snap.TypeApp,
		SuggestedName: "",
		InstanceKey:   "insta",
		SideInfo: snap.SideInfo{
			RealName: "the-snap",
			SnapID:   "snapidid",
			Revision: snap.R(99),
		},
	}
	si.Apps = map[string]*snap.AppInfo{
		"svc": {Snap: si, Name: "svc", Daemon: "simple", DaemonScope: snap.SystemDaemon},
	}
	mylog.
		// make it active
		Check(os.MkdirAll(si.MountDir(), 0755))

	mylog.Check(os.Symlink(si.Revision.String(), filepath.Join(filepath.Dir(si.MountDir()), "current")))

	c.Check(si.IsActive(), Equals, true)

	sd := &testStatusDecorator{}
	ci := mylog.Check2(clientutil.ClientSnapFromSnapInfo(si, sd))
	c.Check(err, IsNil)
	// ... service status
	c.Check(ci.Name, Equals, "the-snap_insta")
	c.Check(ci.Apps, DeepEquals, []client.AppInfo{
		{
			Snap:        "the-snap_insta",
			Name:        "svc",
			Daemon:      "simple",
			DaemonScope: snap.SystemDaemon,
			Enabled:     true,
			Active:      true,
		},
	})

	c.Check(sd.calls, Equals, 1)
}

func (*cmdSuite) TestAppStatusNotes(c *C) {
	ai := client.AppInfo{}
	c.Check(clientutil.ClientAppInfoNotes(&ai), Equals, "-")

	ai = client.AppInfo{
		Daemon: "oneshot",
	}
	c.Check(clientutil.ClientAppInfoNotes(&ai), Equals, "-")

	ai = client.AppInfo{
		Daemon:      "simple",
		DaemonScope: snap.UserDaemon,
	}
	c.Check(clientutil.ClientAppInfoNotes(&ai), Equals, "user")

	ai = client.AppInfo{
		Daemon: "oneshot",
		Activators: []client.AppActivator{
			{Type: "timer"},
		},
	}
	c.Check(clientutil.ClientAppInfoNotes(&ai), Equals, "timer-activated")

	ai = client.AppInfo{
		Daemon: "oneshot",
		Activators: []client.AppActivator{
			{Type: "socket"},
		},
	}
	c.Check(clientutil.ClientAppInfoNotes(&ai), Equals, "socket-activated")

	ai = client.AppInfo{
		Daemon: "oneshot",
		Activators: []client.AppActivator{
			{Type: "dbus"},
		},
	}
	c.Check(clientutil.ClientAppInfoNotes(&ai), Equals, "dbus-activated")

	// check that the output is stable regardless of the order of activators
	ai = client.AppInfo{
		Daemon: "oneshot",
		Activators: []client.AppActivator{
			{Type: "timer"},
			{Type: "socket"},
			{Type: "dbus"},
		},
	}
	c.Check(clientutil.ClientAppInfoNotes(&ai), Equals, "timer-activated,socket-activated,dbus-activated")
	ai = client.AppInfo{
		Daemon:      "oneshot",
		DaemonScope: snap.UserDaemon,
		Activators: []client.AppActivator{
			{Type: "dbus"},
			{Type: "socket"},
			{Type: "timer"},
		},
	}
	c.Check(clientutil.ClientAppInfoNotes(&ai), Equals, "user,timer-activated,socket-activated,dbus-activated")
}
