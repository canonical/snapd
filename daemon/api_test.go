// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/healthstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type apiSuite struct {
	APIBaseSuite
}

var _ = check.Suite(&apiSuite{})

func (s *apiSuite) TestUsersOnlyRoot(c *check.C) {
	for _, cmd := range api {
		if strings.Contains(cmd.Path, "user") {
			c.Check(cmd.RootOnly, check.Equals, true, check.Commentf(cmd.Path))
		}
	}
}

func (s *apiSuite) TestSnapInfoOneIntegration(c *check.C) {
	d := s.daemon(c)
	s.vars = map[string]string{"name": "foo"}

	// we have v0 [r5] installed
	s.mkInstalledInState(c, d, "foo", "bar", "v0", snap.R(5), false, "")
	// and v1 [r10] is current
	s.mkInstalledInState(c, d, "foo", "bar", "v1", snap.R(10), true, `title: title
description: description
summary: summary
license: GPL-3.0
base: base18
apps:
  cmd:
    command: some.cmd
  cmd2:
    command: other.cmd
  cmd3:
    command: other.cmd
    common-id: org.foo.cmd
  svc1:
    command: somed1
    daemon: simple
  svc2:
    command: somed2
    daemon: forking
  svc3:
    command: somed3
    daemon: oneshot
  svc4:
    command: somed4
    daemon: notify
  svc5:
    command: some5
    timer: mon1,12:15
    daemon: simple
  svc6:
    command: some6
    daemon: simple
    sockets:
       sock:
         listen-stream: $SNAP_COMMON/run.sock
  svc7:
    command: some7
    daemon: simple
    sockets:
       other-sock:
         listen-stream: $SNAP_COMMON/other-run.sock
`)
	df := s.mkInstalledDesktopFile(c, "foo_cmd.desktop", "[Desktop]\nExec=foo.cmd %U")
	s.SysctlBufs = [][]byte{
		[]byte(`Type=simple
Id=snap.foo.svc1.service
ActiveState=fumbling
UnitFileState=enabled
`),
		[]byte(`Type=forking
Id=snap.foo.svc2.service
ActiveState=active
UnitFileState=disabled
`),
		[]byte(`Type=oneshot
Id=snap.foo.svc3.service
ActiveState=reloading
UnitFileState=static
`),
		[]byte(`Type=notify
Id=snap.foo.svc4.service
ActiveState=inactive
UnitFileState=potatoes
`),
		[]byte(`Type=simple
Id=snap.foo.svc5.service
ActiveState=inactive
UnitFileState=static
`),
		[]byte(`Id=snap.foo.svc5.timer
ActiveState=active
UnitFileState=enabled
`),
		[]byte(`Type=simple
Id=snap.foo.svc6.service
ActiveState=inactive
UnitFileState=static
`),
		[]byte(`Id=snap.foo.svc6.sock.socket
ActiveState=active
UnitFileState=enabled
`),
		[]byte(`Type=simple
Id=snap.foo.svc7.service
ActiveState=inactive
UnitFileState=static
`),
		[]byte(`Id=snap.foo.svc7.other-sock.socket
ActiveState=inactive
UnitFileState=enabled
`),
	}

	var snapst snapstate.SnapState
	st := s.d.overlord.State()
	st.Lock()
	st.Set("health", map[string]healthstate.HealthState{
		"foo": {Status: healthstate.OkayStatus},
	})
	err := snapstate.Get(st, "foo", &snapst)
	st.Unlock()
	c.Assert(err, check.IsNil)

	// modify state
	snapst.TrackingChannel = "beta"
	snapst.IgnoreValidation = true
	snapst.CohortKey = "some-long-cohort-key"
	st.Lock()
	snapstate.Set(st, "foo", &snapst)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/snaps/foo", nil)
	c.Assert(err, check.IsNil)
	rsp, ok := getSnapInfo(snapCmd, req, nil).(*resp)
	c.Assert(ok, check.Equals, true)

	c.Assert(rsp, check.NotNil)
	c.Assert(rsp.Result, check.FitsTypeOf, &client.Snap{})
	m := rsp.Result.(*client.Snap)

	// installed-size depends on vagaries of the filesystem, just check type
	c.Check(m.InstalledSize, check.FitsTypeOf, int64(0))
	m.InstalledSize = 0
	// ditto install-date
	c.Check(m.InstallDate, check.FitsTypeOf, time.Time{})
	m.InstallDate = time.Time{}

	meta := &Meta{}
	expected := &resp{
		Type:   ResponseTypeSync,
		Status: 200,
		Result: &client.Snap{
			ID:               "foo-id",
			Name:             "foo",
			Revision:         snap.R(10),
			Version:          "v1",
			Channel:          "stable",
			TrackingChannel:  "beta",
			IgnoreValidation: true,
			Title:            "title",
			Summary:          "summary",
			Description:      "description",
			Developer:        "bar",
			Publisher: &snap.StoreAccount{
				ID:          "bar-id",
				Username:    "bar",
				DisplayName: "Bar",
				Validation:  "unproven",
			},
			Status:      "active",
			Health:      &client.SnapHealth{Status: "okay"},
			Icon:        "/v2/icons/foo/icon",
			Type:        string(snap.TypeApp),
			Base:        "base18",
			Private:     false,
			DevMode:     false,
			JailMode:    false,
			Confinement: string(snap.StrictConfinement),
			TryMode:     false,
			MountedFrom: filepath.Join(dirs.SnapBlobDir, "foo_10.snap"),
			Apps: []client.AppInfo{
				{
					Snap: "foo", Name: "cmd",
					DesktopFile: df,
				}, {
					// no desktop file
					Snap: "foo", Name: "cmd2",
				}, {
					// has AppStream ID
					Snap: "foo", Name: "cmd3",
					CommonID: "org.foo.cmd",
				}, {
					// services
					Snap: "foo", Name: "svc1",
					Daemon:  "simple",
					Enabled: true,
					Active:  false,
				}, {
					Snap: "foo", Name: "svc2",
					Daemon:  "forking",
					Enabled: false,
					Active:  true,
				}, {
					Snap: "foo", Name: "svc3",
					Daemon:  "oneshot",
					Enabled: true,
					Active:  true,
				}, {
					Snap: "foo", Name: "svc4",
					Daemon:  "notify",
					Enabled: false,
					Active:  false,
				}, {
					Snap: "foo", Name: "svc5",
					Daemon:  "simple",
					Enabled: true,
					Active:  false,
					Activators: []client.AppActivator{
						{Name: "svc5", Type: "timer", Active: true, Enabled: true},
					},
				}, {
					Snap: "foo", Name: "svc6",
					Daemon:  "simple",
					Enabled: true,
					Active:  false,
					Activators: []client.AppActivator{
						{Name: "sock", Type: "socket", Active: true, Enabled: true},
					},
				}, {
					Snap: "foo", Name: "svc7",
					Daemon:  "simple",
					Enabled: true,
					Active:  false,
					Activators: []client.AppActivator{
						{Name: "other-sock", Type: "socket", Active: false, Enabled: true},
					},
				},
			},
			Broken:    "",
			Contact:   "",
			License:   "GPL-3.0",
			CommonIDs: []string{"org.foo.cmd"},
			CohortKey: "some-long-cohort-key",
		},
		Meta: meta,
	}

	c.Check(rsp.Result, check.DeepEquals, expected.Result)
}

func (s *apiSuite) TestSnapInfoWithAuth(c *check.C) {
	s.daemon(c)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/find/?q=name:gfoo", nil)
	c.Assert(err, check.IsNil)

	c.Assert(s.user, check.IsNil)

	_, ok := searchStore(findCmd, req, user).(*resp)
	c.Assert(ok, check.Equals, true)
	// ensure user was set
	c.Assert(s.user, check.DeepEquals, user)
}

func (s *apiSuite) TestSnapInfoNotFound(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	c.Check(getSnapInfo(snapCmd, req, nil).(*resp).Status, check.Equals, 404)
}

func (s *apiSuite) TestSnapInfoNoneFound(c *check.C) {
	s.vars = map[string]string{"name": "foo"}

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	c.Check(getSnapInfo(snapCmd, req, nil).(*resp).Status, check.Equals, 404)
}

func (s *apiSuite) TestSnapInfoIgnoresRemoteErrors(c *check.C) {
	s.vars = map[string]string{"name": "foo"}
	s.err = errors.New("weird")

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	rsp := getSnapInfo(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 404)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestMapLocalFields(c *check.C) {
	media := snap.MediaInfos{
		{
			Type: "screenshot",
			URL:  "https://example.com/shot1.svg",
		}, {
			Type: "icon",
			URL:  "https://example.com/icon.png",
		}, {
			Type: "screenshot",
			URL:  "https://example.com/shot2.svg",
		},
	}

	publisher := snap.StoreAccount{
		ID:          "some-dev-id",
		Username:    "some-dev",
		DisplayName: "Some Developer",
		Validation:  "poor",
	}
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			SnapID:            "some-snap-id",
			RealName:          "some-snap",
			EditedTitle:       "A Title",
			EditedSummary:     "a summary",
			EditedDescription: "the\nlong\ndescription",
			Channel:           "bleeding/edge",
			Contact:           "alice@example.com",
			Revision:          snap.R(7),
			Private:           true,
		},
		InstanceKey: "instance",
		SnapType:    "app",
		Base:        "the-base",
		Version:     "v1.0",
		License:     "MIT",
		Broken:      "very",
		Confinement: "very strict",
		CommonIDs:   []string{"foo", "bar"},
		Media:       media,
		DownloadInfo: snap.DownloadInfo{
			Size:     42,
			Sha3_384: "some-sum",
		},
		Publisher: publisher,
	}

	// make InstallDate work
	c.Assert(os.MkdirAll(info.MountDir(), 0755), check.IsNil)
	c.Assert(os.Symlink("7", filepath.Join(info.MountDir(), "..", "current")), check.IsNil)

	info.Apps = map[string]*snap.AppInfo{
		"foo": {Snap: info, Name: "foo", Command: "foo"},
		"bar": {Snap: info, Name: "bar", Command: "bar"},
	}
	about := aboutSnap{
		info: info,
		snapst: &snapstate.SnapState{
			Active:          true,
			TrackingChannel: "flaky/beta",
			Current:         snap.R(7),
			Flags: snapstate.Flags{
				IgnoreValidation: true,
				DevMode:          true,
				JailMode:         true,
			},
		},
	}

	expected := &client.Snap{
		ID:               "some-snap-id",
		Name:             "some-snap_instance",
		Summary:          "a summary",
		Description:      "the\nlong\ndescription",
		Developer:        "some-dev",
		Publisher:        &publisher,
		Icon:             "https://example.com/icon.png",
		Type:             "app",
		Base:             "the-base",
		Version:          "v1.0",
		Revision:         snap.R(7),
		Channel:          "bleeding/edge",
		TrackingChannel:  "flaky/beta",
		InstallDate:      info.InstallDate(),
		InstalledSize:    42,
		Status:           "active",
		Confinement:      "very strict",
		IgnoreValidation: true,
		DevMode:          true,
		JailMode:         true,
		Private:          true,
		Broken:           "very",
		Contact:          "alice@example.com",
		Title:            "A Title",
		License:          "MIT",
		CommonIDs:        []string{"foo", "bar"},
		MountedFrom:      filepath.Join(dirs.SnapBlobDir, "some-snap_instance_7.snap"),
		Media:            media,
		Apps: []client.AppInfo{
			{Snap: "some-snap_instance", Name: "bar"},
			{Snap: "some-snap_instance", Name: "foo"},
		},
	}
	c.Check(mapLocal(about, nil), check.DeepEquals, expected)
}

func (s *apiSuite) TestMapLocalOfTryResolvesSymlink(c *check.C) {
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0755), check.IsNil)

	info := snap.Info{SideInfo: snap.SideInfo{RealName: "hello", Revision: snap.R(1)}}
	snapst := snapstate.SnapState{}
	mountFile := info.MountFile()
	about := aboutSnap{info: &info, snapst: &snapst}

	// if not a 'try', then MountedFrom is just MountFile()
	c.Check(mapLocal(about, nil).MountedFrom, check.Equals, mountFile)

	// if it's a try, then MountedFrom resolves the symlink
	// (note it doesn't matter, here, whether the target of the link exists)
	snapst.TryMode = true
	c.Assert(os.Symlink("/xyzzy", mountFile), check.IsNil)
	c.Check(mapLocal(about, nil).MountedFrom, check.Equals, "/xyzzy")

	// if the readlink fails, it's unset
	c.Assert(os.Remove(mountFile), check.IsNil)
	c.Check(mapLocal(about, nil).MountedFrom, check.Equals, "")
}

func (s *apiSuite) TestListIncludesAll(c *check.C) {
	// Very basic check to help stop us from not adding all the
	// commands to the command list.
	found := countCommandDecls(c, check.Commentf("TestListIncludesAll"))

	c.Check(found, check.Equals, len(api),
		check.Commentf(`At a glance it looks like you've not added all the Commands defined in api to the api list.`))
}

func (s *apiSuite) TestLoginUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(loginCmd, req, nil).(*resp)

	state.Lock()
	user, err := auth.User(state, 1)
	state.Unlock()
	c.Check(err, check.IsNil)

	expected := userResponseData{
		ID:    1,
		Email: "email@.com",

		Macaroon:   user.Macaroon,
		Discharges: user.Discharges,
	}

	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	c.Check(user.ID, check.Equals, 1)
	c.Check(user.Username, check.Equals, "")
	c.Check(user.Email, check.Equals, "email@.com")
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, s.loginUserStoreMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
	// snapd macaroon was setup too
	snapdMacaroon, err := auth.MacaroonDeserialize(user.Macaroon)
	c.Check(err, check.IsNil)
	c.Check(snapdMacaroon.Id(), check.Equals, "1")
	c.Check(snapdMacaroon.Location(), check.Equals, "snapd")
}

func (s *apiSuite) TestLoginUserWithUsername(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	buf := bytes.NewBufferString(`{"username": "username", "email": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(loginCmd, req, nil).(*resp)

	state.Lock()
	user, err := auth.User(state, 1)
	state.Unlock()
	c.Check(err, check.IsNil)

	expected := userResponseData{
		ID:         1,
		Username:   "username",
		Email:      "email@.com",
		Macaroon:   user.Macaroon,
		Discharges: user.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	c.Check(user.ID, check.Equals, 1)
	c.Check(user.Username, check.Equals, "username")
	c.Check(user.Email, check.Equals, "email@.com")
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, s.loginUserStoreMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
	// snapd macaroon was setup too
	snapdMacaroon, err := auth.MacaroonDeserialize(user.Macaroon)
	c.Check(err, check.IsNil)
	c.Check(snapdMacaroon.Id(), check.Equals, "1")
	c.Check(snapdMacaroon.Location(), check.Equals, "snapd")
}

func (s *apiSuite) TestLoginUserNoEmailWithExistentLocalUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

	// setup local-only user
	state.Lock()
	localUser, err := auth.NewUser(state, "username", "email@test.com", "", nil)
	state.Unlock()
	c.Assert(err, check.IsNil)

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	buf := bytes.NewBufferString(`{"username": "username", "email": "", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, localUser.Macaroon))

	rsp := loginUser(loginCmd, req, localUser).(*resp)

	expected := userResponseData{
		ID:       1,
		Username: "username",
		Email:    "email@test.com",

		Macaroon:   localUser.Macaroon,
		Discharges: localUser.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	state.Lock()
	user, err := auth.User(state, localUser.ID)
	state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(user.Username, check.Equals, "username")
	c.Check(user.Email, check.Equals, localUser.Email)
	c.Check(user.Macaroon, check.Equals, localUser.Macaroon)
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, s.loginUserStoreMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
}

func (s *apiSuite) TestLoginUserWithExistentLocalUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

	// setup local-only user
	state.Lock()
	localUser, err := auth.NewUser(state, "username", "email@test.com", "", nil)
	state.Unlock()
	c.Assert(err, check.IsNil)

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	buf := bytes.NewBufferString(`{"username": "username", "email": "email@test.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, localUser.Macaroon))

	rsp := loginUser(loginCmd, req, localUser).(*resp)

	expected := userResponseData{
		ID:       1,
		Username: "username",
		Email:    "email@test.com",

		Macaroon:   localUser.Macaroon,
		Discharges: localUser.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	state.Lock()
	user, err := auth.User(state, localUser.ID)
	state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(user.Username, check.Equals, "username")
	c.Check(user.Email, check.Equals, localUser.Email)
	c.Check(user.Macaroon, check.Equals, localUser.Macaroon)
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, s.loginUserStoreMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
}

func (s *apiSuite) TestLoginUserNewEmailWithExistentLocalUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

	// setup local-only user
	state.Lock()
	localUser, err := auth.NewUser(state, "username", "email@test.com", "", nil)
	state.Unlock()
	c.Assert(err, check.IsNil)

	s.loginUserStoreMacaroon = "user-macaroon"
	s.loginUserDischarge = "the-discharge-macaroon-serialized-data"
	// same local user, but using a new SSO account
	buf := bytes.NewBufferString(`{"username": "username", "email": "new.email@test.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, localUser.Macaroon))

	rsp := loginUser(loginCmd, req, localUser).(*resp)

	expected := userResponseData{
		ID:       1,
		Username: "username",
		Email:    "new.email@test.com",

		Macaroon:   localUser.Macaroon,
		Discharges: localUser.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	state.Lock()
	user, err := auth.User(state, localUser.ID)
	state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(user.Username, check.Equals, "username")
	c.Check(user.Email, check.Equals, expected.Email)
	c.Check(user.Macaroon, check.Equals, localUser.Macaroon)
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, s.loginUserStoreMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
}

func (s *apiSuite) TestLogoutUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/logout", nil)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", `Macaroon root="macaroon", discharge="discharge"`)

	rsp := logoutUser(logoutCmd, req, user).(*resp)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)

	state.Lock()
	_, err = auth.User(state, user.ID)
	state.Unlock()
	c.Check(err, check.Equals, auth.ErrInvalidUser)
}

func (s *apiSuite) TestLoginUserBadRequest(c *check.C) {
	buf := bytes.NewBufferString(`hello`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestLoginUserDeveloperAPIError(c *check.C) {
	s.daemon(c)

	s.err = fmt.Errorf("error-from-login-user")
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 401)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "error-from-login-user")
}

func (s *apiSuite) TestLoginUserTwoFactorRequiredError(c *check.C) {
	s.daemon(c)

	s.err = store.ErrAuthenticationNeeds2fa
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 401)
	c.Check(rsp.Result.(*errorResult).Kind, check.Equals, client.ErrorKindTwoFactorRequired)
}

func (s *apiSuite) TestLoginUserTwoFactorFailedError(c *check.C) {
	s.daemon(c)

	s.err = store.Err2faFailed
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 401)
	c.Check(rsp.Result.(*errorResult).Kind, check.Equals, client.ErrorKindTwoFactorFailed)
}

func (s *apiSuite) TestLoginUserInvalidCredentialsError(c *check.C) {
	s.daemon(c)

	s.err = store.ErrInvalidCredentials
	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 401)
	c.Check(rsp.Result.(*errorResult).Message, check.Equals, "invalid credentials")
}

func (s *apiSuite) TestUserFromRequestNoHeader(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.Equals, auth.ErrInvalidAuth)
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderNoMacaroons(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", "Invalid")

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.ErrorMatches, "authorization header misses Macaroon prefix")
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderIncomplete(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", `Macaroon root=""`)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.ErrorMatches, "invalid authorization header")
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderCorrectMissingUser(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", `Macaroon root="macaroon", discharge="discharge"`)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.Equals, auth.ErrInvalidAuth)
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderValidUser(c *check.C) {
	state := snapCmd.d.overlord.State()
	state.Lock()
	expectedUser, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, expectedUser.Macaroon))

	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.IsNil)
	c.Check(user, check.DeepEquals, expectedUser)
}

func (s *apiSuite) TestSnapsInfoOnePerIntegration(c *check.C) {
	s.checkSnapInfoOnePerIntegration(c, false, nil)
}

func (s *apiSuite) TestSnapsInfoOnePerIntegrationSome(c *check.C) {
	s.checkSnapInfoOnePerIntegration(c, false, []string{"foo", "baz"})
}

func (s *apiSuite) TestSnapsInfoOnePerIntegrationAll(c *check.C) {
	s.checkSnapInfoOnePerIntegration(c, true, nil)
}

func (s *apiSuite) TestSnapsInfoOnePerIntegrationAllSome(c *check.C) {
	s.checkSnapInfoOnePerIntegration(c, true, []string{"foo", "baz"})
}

func (s *apiSuite) checkSnapInfoOnePerIntegration(c *check.C, all bool, names []string) {
	d := s.daemon(c)

	type tsnap struct {
		name   string
		dev    string
		ver    string
		rev    int
		active bool

		wanted bool
	}

	tsnaps := []tsnap{
		{name: "foo", dev: "bar", ver: "v0.9", rev: 1},
		{name: "foo", dev: "bar", ver: "v1", rev: 5, active: true},
		{name: "bar", dev: "baz", ver: "v2", rev: 10, active: true},
		{name: "baz", dev: "qux", ver: "v3", rev: 15, active: true},
		{name: "qux", dev: "mip", ver: "v4", rev: 20, active: true},
	}
	numExpected := 0

	for _, snp := range tsnaps {
		if all || snp.active {
			if len(names) == 0 {
				numExpected++
				snp.wanted = true
			}
			for _, n := range names {
				if snp.name == n {
					numExpected++
					snp.wanted = true
					break
				}
			}
		}
		s.mkInstalledInState(c, d, snp.name, snp.dev, snp.ver, snap.R(snp.rev), snp.active, "")
	}

	q := url.Values{}
	if all {
		q.Set("select", "all")
	}
	if len(names) > 0 {
		q.Set("snaps", strings.Join(names, ","))
	}
	req, err := http.NewRequest("GET", "/v2/snaps?"+q.Encode(), nil)
	c.Assert(err, check.IsNil)

	rsp, ok := getSnapsInfo(snapsCmd, req, nil).(*resp)
	c.Assert(ok, check.Equals, true)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.NotNil)

	snaps := snapList(rsp.Result)
	c.Check(snaps, check.HasLen, numExpected)

	for _, s := range tsnaps {
		if !((all || s.active) && s.wanted) {
			continue
		}
		var got map[string]interface{}
		for _, got = range snaps {
			if got["name"].(string) == s.name && got["revision"].(string) == snap.R(s.rev).String() {
				break
			}
		}
		c.Check(got["name"], check.Equals, s.name)
		c.Check(got["version"], check.Equals, s.ver)
		c.Check(got["revision"], check.Equals, snap.R(s.rev).String())
		c.Check(got["developer"], check.Equals, s.dev)
		c.Check(got["confinement"], check.Equals, "strict")
	}
}

func (s *apiSuite) TestSnapsInfoOnlyLocal(c *check.C) {
	d := s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(10), true, "")
	st := s.d.overlord.State()
	st.Lock()
	st.Set("health", map[string]healthstate.HealthState{
		"local": {Status: healthstate.OkayStatus},
	})
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/snaps?sources=local", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"local"})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "local")
	c.Check(snaps[0]["health"], check.DeepEquals, map[string]interface{}{
		"status":    "okay",
		"revision":  "unset",
		"timestamp": "0001-01-01T00:00:00Z",
	})
}

func (s *apiSuite) TestSnapsInfoAllMixedPublishers(c *check.C) {
	d := s.daemon(c)

	// the first 'local' is from a 'local' snap
	s.mkInstalledInState(c, d, "local", "", "v1", snap.R(-1), false, "")
	s.mkInstalledInState(c, d, "local", "foo", "v2", snap.R(1), false, "")
	s.mkInstalledInState(c, d, "local", "foo", "v3", snap.R(2), true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?select=all", nil)
	c.Assert(err, check.IsNil)
	rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeSync)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 3)

	publisher := map[string]interface{}{
		"id":           "foo-id",
		"username":     "foo",
		"display-name": "Foo",
		"validation":   "unproven",
	}

	c.Check(snaps[0]["publisher"], check.IsNil)
	c.Check(snaps[1]["publisher"], check.DeepEquals, publisher)
	c.Check(snaps[2]["publisher"], check.DeepEquals, publisher)
}

func (s *apiSuite) TestSnapsInfoAll(c *check.C) {
	d := s.daemon(c)

	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(1), false, "")
	s.mkInstalledInState(c, d, "local", "foo", "v2", snap.R(2), false, "")
	s.mkInstalledInState(c, d, "local", "foo", "v3", snap.R(3), true, "")
	s.mkInstalledInState(c, d, "local_foo", "foo", "v4", snap.R(4), true, "")
	brokenInfo := s.mkInstalledInState(c, d, "local_bar", "foo", "v5", snap.R(5), true, "")
	// make sure local_bar is 'broken'
	err := os.Remove(filepath.Join(brokenInfo.MountDir(), "meta", "snap.yaml"))
	c.Assert(err, check.IsNil)

	expectedHappy := map[string]bool{
		"local":     true,
		"local_foo": true,
		"local_bar": true,
	}
	for _, t := range []struct {
		q        string
		numSnaps int
		typ      ResponseType
	}{
		{"?select=enabled", 3, "sync"},
		{`?select=`, 3, "sync"},
		{"", 3, "sync"},
		{"?select=all", 5, "sync"},
		{"?select=invalid-field", 0, "error"},
	} {
		c.Logf("trying: %v", t)
		req, err := http.NewRequest("GET", fmt.Sprintf("/v2/snaps%s", t.q), nil)
		c.Assert(err, check.IsNil)
		rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)
		c.Assert(rsp.Type, check.Equals, t.typ)

		if rsp.Type != "error" {
			snaps := snapList(rsp.Result)
			c.Assert(snaps, check.HasLen, t.numSnaps)
			seen := map[string]bool{}
			for _, s := range snaps {
				seen[s["name"].(string)] = true
			}
			c.Assert(seen, check.DeepEquals, expectedHappy)
		}
	}
}

func (s *apiSuite) TestFind(c *check.C) {
	s.daemon(c)

	s.suggestedCurrency = "EUR"

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}

	req, err := http.NewRequest("GET", "/v2/find?q=hi", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "store")
	c.Check(snaps[0]["prices"], check.IsNil)
	c.Check(snaps[0]["channels"], check.IsNil)

	c.Check(rsp.SuggestedCurrency, check.Equals, "EUR")

	c.Check(s.storeSearch, check.DeepEquals, store.Search{Query: "hi"})
	c.Check(s.currentSnaps, check.HasLen, 0)
	c.Check(s.actions, check.HasLen, 0)
}

func (s *apiSuite) TestFindRefreshes(c *check.C) {
	snapstateRefreshCandidates = snapstate.RefreshCandidates
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}
	s.MockSnap(c, "name: store\nversion: 1.0")

	req, err := http.NewRequest("GET", "/v2/find?select=refresh", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "store")
	c.Check(s.currentSnaps, check.HasLen, 1)
	c.Check(s.actions, check.HasLen, 1)
}

func (s *apiSuite) TestFindRefreshSideloaded(c *check.C) {
	snapstateRefreshCandidates = snapstate.RefreshCandidates
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}

	s.MockSnap(c, "name: store\nversion: 1.0")

	var snapst snapstate.SnapState
	st := s.d.overlord.State()
	st.Lock()
	err := snapstate.Get(st, "store", &snapst)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Assert(snapst.Sequence, check.HasLen, 1)

	// clear the snapid
	snapst.Sequence[0].SnapID = ""
	st.Lock()
	snapstate.Set(st, "store", &snapst)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/find?select=refresh", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 0)
	c.Check(s.currentSnaps, check.HasLen, 0)
	c.Check(s.actions, check.HasLen, 0)
}

func (s *apiSuite) TestFindPrivate(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{}

	req, err := http.NewRequest("GET", "/v2/find?q=foo&select=private", nil)
	c.Assert(err, check.IsNil)

	_ = searchStore(findCmd, req, nil).(*resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{
		Query:   "foo",
		Private: true,
	})
}

func (s *apiSuite) TestFindUserAgentContextCreated(c *check.C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/find", nil)
	c.Assert(err, check.IsNil)
	req.Header.Add("User-Agent", "some-agent/1.0")

	_ = searchStore(findCmd, req, nil).(*resp)

	c.Check(store.ClientUserAgent(s.ctx), check.Equals, "some-agent/1.0")
}

func (s *apiSuite) TestFindOneUserAgentContextCreated(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SnapType: snap.TypeApp,
		Version:  "v2",
		SideInfo: snap.SideInfo{
			RealName: "banana",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}
	req, err := http.NewRequest("GET", "/v2/find?name=foo", nil)
	c.Assert(err, check.IsNil)
	req.Header.Add("User-Agent", "some-agent/1.0")

	_ = searchStore(findCmd, req, nil).(*resp)

	c.Check(store.ClientUserAgent(s.ctx), check.Equals, "some-agent/1.0")
}

func (s *apiSuite) TestFindPrefix(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{}

	req, err := http.NewRequest("GET", "/v2/find?name=foo*", nil)
	c.Assert(err, check.IsNil)

	_ = searchStore(findCmd, req, nil).(*resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{Query: "foo", Prefix: true})
}

func (s *apiSuite) TestFindSection(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{}

	req, err := http.NewRequest("GET", "/v2/find?q=foo&section=bar", nil)
	c.Assert(err, check.IsNil)

	_ = searchStore(findCmd, req, nil).(*resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{
		Query:    "foo",
		Category: "bar",
	})
}

func (s *apiSuite) TestFindScope(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{}

	req, err := http.NewRequest("GET", "/v2/find?q=foo&scope=creep", nil)
	c.Assert(err, check.IsNil)

	_ = searchStore(findCmd, req, nil).(*resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{
		Query: "foo",
		Scope: "creep",
	})
}

func (s *apiSuite) TestFindCommonID(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
		CommonIDs: []string{"org.foo"},
	}}
	s.MockSnap(c, "name: store\nversion: 1.0")

	req, err := http.NewRequest("GET", "/v2/find?name=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["common-ids"], check.DeepEquals, []interface{}{"org.foo"})
}

func (s *apiSuite) TestFindByCommonID(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
		CommonIDs: []string{"org.foo"},
	}}
	s.MockSnap(c, "name: store\nversion: 1.0")

	req, err := http.NewRequest("GET", "/v2/find?common-id=org.foo", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(s.storeSearch, check.DeepEquals, store.Search{CommonID: "org.foo"})
}

func (s *apiSuite) TestFindOne(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Base: "base0",
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "verified",
		},
		Channels: map[string]*snap.ChannelSnapInfo{
			"stable": {
				Revision: snap.R(42),
			},
		},
	}}
	s.MockSnap(c, "name: store\nversion: 1.0")

	req, err := http.NewRequest("GET", "/v2/find?name=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["name"], check.Equals, "store")
	c.Check(snaps[0]["base"], check.Equals, "base0")
	c.Check(snaps[0]["publisher"], check.DeepEquals, map[string]interface{}{
		"id":           "foo-id",
		"username":     "foo",
		"display-name": "Foo",
		"validation":   "verified",
	})
	m := snaps[0]["channels"].(map[string]interface{})["stable"].(map[string]interface{})

	c.Check(m["revision"], check.Equals, "42")
}

func (s *apiSuite) TestFindOneNotFound(c *check.C) {
	s.daemon(c)

	s.err = store.ErrSnapNotFound
	s.MockSnap(c, "name: store\nversion: 1.0")

	req, err := http.NewRequest("GET", "/v2/find?name=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{})
	c.Check(rsp.Status, check.Equals, 404)
}

func (s *apiSuite) TestFindRefreshNotOther(c *check.C) {
	for _, other := range []string{"name", "q", "common-id"} {
		req, err := http.NewRequest("GET", "/v2/find?select=refresh&"+other+"=foo*", nil)
		c.Assert(err, check.IsNil)

		rsp := searchStore(findCmd, req, nil).(*resp)
		c.Check(rsp.Type, check.Equals, ResponseTypeError)
		c.Check(rsp.Status, check.Equals, 400)
		c.Check(rsp.Result.(*errorResult).Message, check.Equals, "cannot use '"+other+"' with 'select=refresh'")
	}
}

func (s *apiSuite) TestFindNotTogether(c *check.C) {
	queries := map[string]string{"q": "foo", "name": "foo*", "common-id": "foo"}
	for ki, vi := range queries {
		for kj, vj := range queries {
			if ki == kj {
				continue
			}

			req, err := http.NewRequest("GET", fmt.Sprintf("/v2/find?%s=%s&%s=%s", ki, vi, kj, vj), nil)
			c.Assert(err, check.IsNil)

			rsp := searchStore(findCmd, req, nil).(*resp)
			c.Check(rsp.Type, check.Equals, ResponseTypeError)
			c.Check(rsp.Status, check.Equals, 400)
			exp1 := "cannot use '" + ki + "' and '" + kj + "' together"
			exp2 := "cannot use '" + kj + "' and '" + ki + "' together"
			c.Check(rsp.Result.(*errorResult).Message, check.Matches, exp1+"|"+exp2)
		}
	}
}

func (s *apiSuite) TestFindBadQueryReturnsCorrectErrorKind(c *check.C) {
	s.daemon(c)

	s.err = store.ErrBadQuery
	req, err := http.NewRequest("GET", "/v2/find?q=return-bad-query-please", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, "bad query")
	c.Check(rsp.Result.(*errorResult).Kind, check.Equals, client.ErrorKindBadQuery)
}

func (s *apiSuite) TestFindPriced(c *check.C) {
	s.daemon(c)

	s.suggestedCurrency = "GBP"

	s.rsnaps = []*snap.Info{{
		SnapType: snap.TypeApp,
		Version:  "v2",
		Prices: map[string]float64{
			"GBP": 1.23,
			"EUR": 2.34,
		},
		MustBuy: true,
		SideInfo: snap.SideInfo{
			RealName: "banana",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}

	req, err := http.NewRequest("GET", "/v2/find?q=banana&channel=stable", nil)
	c.Assert(err, check.IsNil)
	rsp, ok := searchStore(findCmd, req, nil).(*resp)
	c.Assert(ok, check.Equals, true)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)

	snap := snaps[0]
	c.Check(snap["name"], check.Equals, "banana")
	c.Check(snap["prices"], check.DeepEquals, map[string]interface{}{
		"EUR": 2.34,
		"GBP": 1.23,
	})
	c.Check(snap["status"], check.Equals, "priced")

	c.Check(rsp.SuggestedCurrency, check.Equals, "GBP")
}

func (s *apiSuite) TestFindScreenshotted(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SnapType: snap.TypeApp,
		Version:  "v2",
		Media: []snap.MediaInfo{
			{
				Type:   "screenshot",
				URL:    "http://example.com/screenshot.png",
				Width:  800,
				Height: 1280,
			},
			{
				Type: "screenshot",
				URL:  "http://example.com/screenshot2.png",
			},
		},
		MustBuy: true,
		SideInfo: snap.SideInfo{
			RealName: "test-screenshot",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}

	req, err := http.NewRequest("GET", "/v2/find?q=test-screenshot", nil)
	c.Assert(err, check.IsNil)
	rsp, ok := searchStore(findCmd, req, nil).(*resp)
	c.Assert(ok, check.Equals, true)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)

	c.Check(snaps[0]["name"], check.Equals, "test-screenshot")
	c.Check(snaps[0]["media"], check.DeepEquals, []interface{}{
		map[string]interface{}{
			"type":   "screenshot",
			"url":    "http://example.com/screenshot.png",
			"width":  float64(800),
			"height": float64(1280),
		},
		map[string]interface{}{
			"type": "screenshot",
			"url":  "http://example.com/screenshot2.png",
		},
	})
}

func (s *apiSuite) TestSnapsInfoOnlyStore(c *check.C) {
	d := s.daemon(c)

	s.suggestedCurrency = "EUR"

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(10), true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=store", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"store"})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "store")
	c.Check(snaps[0]["prices"], check.IsNil)

	c.Check(rsp.SuggestedCurrency, check.Equals, "EUR")
}

func (s *apiSuite) TestSnapsStoreConfinement(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{
		{
			// no explicit confinement in this one
			SideInfo: snap.SideInfo{
				RealName: "foo",
			},
		},
		{
			Confinement: snap.StrictConfinement,
			SideInfo: snap.SideInfo{
				RealName: "bar",
			},
		},
		{
			Confinement: snap.DevModeConfinement,
			SideInfo: snap.SideInfo{
				RealName: "baz",
			},
		},
	}

	req, err := http.NewRequest("GET", "/v2/find", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 3)

	for i, ss := range [][2]string{
		{"foo", string(snap.StrictConfinement)},
		{"bar", string(snap.StrictConfinement)},
		{"baz", string(snap.DevModeConfinement)},
	} {
		name, mode := ss[0], ss[1]
		c.Check(snaps[i]["name"], check.Equals, name, check.Commentf(name))
		c.Check(snaps[i]["confinement"], check.Equals, mode, check.Commentf(name))
	}
}

func (s *apiSuite) TestSnapsInfoStoreWithAuth(c *check.C) {
	s.daemon(c)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/snaps?sources=store", nil)
	c.Assert(err, check.IsNil)

	c.Assert(s.user, check.IsNil)

	_ = getSnapsInfo(snapsCmd, req, user).(*resp)

	// ensure user was set
	c.Assert(s.user, check.DeepEquals, user)
}

func (s *apiSuite) TestSnapsInfoLocalAndStore(c *check.C) {
	d := s.daemon(c)

	s.rsnaps = []*snap.Info{{
		Version: "v42",
		SideInfo: snap.SideInfo{
			RealName: "remote",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(10), true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=local,store", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)

	// presence of 'store' in sources bounces request over to /find
	c.Assert(rsp.Sources, check.DeepEquals, []string{"store"})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["version"], check.Equals, "v42")

	// as does a 'q'
	req, err = http.NewRequest("GET", "/v2/snaps?q=what", nil)
	c.Assert(err, check.IsNil)
	rsp = getSnapsInfo(snapsCmd, req, nil).(*resp)
	snaps = snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["version"], check.Equals, "v42")

	// otherwise, local only
	req, err = http.NewRequest("GET", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)
	rsp = getSnapsInfo(snapsCmd, req, nil).(*resp)
	snaps = snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["version"], check.Equals, "v1")
}

func (s *apiSuite) TestSnapsInfoDefaultSources(c *check.C) {
	d := s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "remote",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(10), true, "")

	req, err := http.NewRequest("GET", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"local"})
	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
}

func (s *apiSuite) TestSnapsInfoFilterRemote(c *check.C) {
	s.daemon(c)

	s.rsnaps = nil

	req, err := http.NewRequest("GET", "/v2/snaps?q=foo&sources=store", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{Query: "foo"})

	c.Assert(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostSnapBadRequest(c *check.C) {
	buf := bytes.NewBufferString(`hello`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostSnapBadAction(c *check.C) {
	buf := bytes.NewBufferString(`{"action": "potato"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostSnapBadChannel(c *check.C) {
	buf := bytes.NewBufferString(`{"channel": "1/2/3/4"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostSnap(c *check.C) {
	s.testPostSnap(c, false)
}

func (s *apiSuite) TestPostSnapWithChannel(c *check.C) {
	s.testPostSnap(c, true)
}

func (s *apiSuite) testPostSnap(c *check.C, withChannel bool) {
	d := s.daemonWithOverlordMock(c)

	soon := 0
	ensureStateSoon = func(st *state.State) {
		soon++
		ensureStateSoonImpl(st)
	}

	s.vars = map[string]string{"name": "foo"}

	snapInstructionDispTable["install"] = func(inst *snapInstruction, _ *state.State) (string, []*state.TaskSet, error) {
		if withChannel {
			// channel in -> channel out
			c.Check(inst.Channel, check.Equals, "xyzzy")
		} else {
			// no channel in -> no channel out
			c.Check(inst.Channel, check.Equals, "")
		}
		return "foooo", nil, nil
	}
	defer func() {
		snapInstructionDispTable["install"] = snapInstall
	}()

	var buf *bytes.Buffer
	if withChannel {
		buf = bytes.NewBufferString(`{"action": "install", "channel": "xyzzy"}`)
	} else {
		buf = bytes.NewBufferString(`{"action": "install"}`)
	}
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Summary(), check.Equals, "foooo")
	var names []string
	err = chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{"foo"})

	c.Check(soon, check.Equals, 1)
}

func (s *apiSuite) TestPostSnapChannel(c *check.C) {
	d := s.daemonWithOverlordMock(c)

	soon := 0
	ensureStateSoon = func(st *state.State) {
		soon++
		ensureStateSoonImpl(st)
	}

	s.vars = map[string]string{"name": "foo"}

	snapInstructionDispTable["install"] = func(*snapInstruction, *state.State) (string, []*state.TaskSet, error) {
		return "foooo", nil, nil
	}
	defer func() {
		snapInstructionDispTable["install"] = snapInstall
	}()

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Summary(), check.Equals, "foooo")
	var names []string
	err = chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{"foo"})

	c.Check(soon, check.Equals, 1)
}

func (s *apiSuite) TestPostSnapVerifySnapInstruction(c *check.C) {
	s.daemonWithOverlordMock(c)

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/ubuntu-core", buf)
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"name": "ubuntu-core"}

	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, `cannot install "ubuntu-core", please use "core" instead`)
}

func (s *apiSuite) TestPostSnapCohortRandoAction(c *check.C) {
	s.daemonWithOverlordMock(c)
	s.vars = map[string]string{"name": "some-snap"}
	const expectedErr = "cohort-key can only be specified for install, refresh, or switch"

	for _, action := range []string{"remove", "revert", "enable", "disable", "xyzzy"} {
		buf := strings.NewReader(fmt.Sprintf(`{"action": "%s", "cohort-key": "32"}`, action))
		req, err := http.NewRequest("POST", "/v2/snaps/some-snap", buf)
		c.Assert(err, check.IsNil)

		rsp := postSnap(snapCmd, req, nil).(*resp)

		c.Check(rsp.Type, check.Equals, ResponseTypeError)
		c.Check(rsp.Status, check.Equals, 400, check.Commentf("%q", action))
		c.Check(rsp.Result.(*errorResult).Message, check.Equals, expectedErr, check.Commentf("%q", action))
	}
}

func (s *apiSuite) TestPostSnapLeaveCohortRandoAction(c *check.C) {
	s.daemonWithOverlordMock(c)
	s.vars = map[string]string{"name": "some-snap"}
	const expectedErr = "leave-cohort can only be specified for refresh or switch"

	for _, action := range []string{"install", "remove", "revert", "enable", "disable", "xyzzy"} {
		buf := strings.NewReader(fmt.Sprintf(`{"action": "%s", "leave-cohort": true}`, action))
		req, err := http.NewRequest("POST", "/v2/snaps/some-snap", buf)
		c.Assert(err, check.IsNil)

		rsp := postSnap(snapCmd, req, nil).(*resp)

		c.Check(rsp.Type, check.Equals, ResponseTypeError)
		c.Check(rsp.Status, check.Equals, 400, check.Commentf("%q", action))
		c.Check(rsp.Result.(*errorResult).Message, check.Equals, expectedErr, check.Commentf("%q", action))
	}
}

func (s *apiSuite) TestPostSnapCohortIncompat(c *check.C) {
	s.daemonWithOverlordMock(c)
	s.vars = map[string]string{"name": "some-snap"}

	type T struct {
		opts   string
		errmsg string
	}

	for i, t := range []T{
		// TODO: more?
		{`"cohort-key": "what", "revision": "42"`, `cannot specify both cohort-key and revision`},
		{`"cohort-key": "what", "leave-cohort": true`, `cannot specify both cohort-key and leave-cohort`},
	} {
		buf := strings.NewReader(fmt.Sprintf(`{"action": "refresh", %s}`, t.opts))
		req, err := http.NewRequest("POST", "/v2/snaps/some-snap", buf)
		c.Assert(err, check.IsNil, check.Commentf("%d (%s)", i, t.opts))

		rsp := postSnap(snapCmd, req, nil).(*resp)

		c.Check(rsp.Type, check.Equals, ResponseTypeError, check.Commentf("%d (%s)", i, t.opts))
		c.Check(rsp.Status, check.Equals, 400, check.Commentf("%d (%s)", i, t.opts))
		c.Check(rsp.Result.(*errorResult).Message, check.Equals, t.errmsg, check.Commentf("%d (%s)", i, t.opts))
	}
}

func (s *apiSuite) TestPostSnapVerifyMultiSnapInstruction(c *check.C) {
	s.daemonWithOverlordMock(c)

	buf := strings.NewReader(`{"action": "install","snaps":["ubuntu-core"]}`)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/json")

	rsp := postSnaps(snapsCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, `cannot install "ubuntu-core", please use "core" instead`)
}

func (s *apiSuite) TestPostSnapsNoWeirdses(c *check.C) {
	s.daemonWithOverlordMock(c)

	// one could add more actions here ... 🤷
	for _, action := range []string{"install", "refresh", "remove"} {
		for weird, v := range map[string]string{
			"channel":      `"beta"`,
			"revision":     `"1"`,
			"devmode":      "true",
			"jailmode":     "true",
			"cohort-key":   `"what"`,
			"leave-cohort": "true",
			"purge":        "true",
		} {
			buf := strings.NewReader(fmt.Sprintf(`{"action": "%s","snaps":["foo","bar"], "%s": %s}`, action, weird, v))
			req, err := http.NewRequest("POST", "/v2/snaps", buf)
			c.Assert(err, check.IsNil)
			req.Header.Set("Content-Type", "application/json")

			rsp := postSnaps(snapsCmd, req, nil).(*resp)

			c.Check(rsp.Type, check.Equals, ResponseTypeError)
			c.Check(rsp.Status, check.Equals, 400)
			c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, `unsupported option provided for multi-snap operation`)
		}
	}
}

func (s *apiSuite) TestPostSnapSetsUser(c *check.C) {
	d := s.daemon(c)
	ensureStateSoon = func(st *state.State) {}

	snapInstructionDispTable["install"] = func(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
		return fmt.Sprintf("<install by user %d>", inst.userID), nil, nil
	}
	defer func() {
		snapInstructionDispTable["install"] = snapInstall
	}()

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", `Macaroon root="macaroon", discharge="discharge"`)

	rsp := postSnap(snapCmd, req, user).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Summary(), check.Equals, "<install by user 1>")
}

func (s *apiSuite) TestPostSnapDispatch(c *check.C) {
	inst := &snapInstruction{Snaps: []string{"foo"}}

	type T struct {
		s    string
		impl snapActionFunc
	}

	actions := []T{
		{"install", snapInstall},
		{"refresh", snapUpdate},
		{"remove", snapRemove},
		{"revert", snapRevert},
		{"enable", snapEnable},
		{"disable", snapDisable},
		{"switch", snapSwitch},
		{"xyzzy", nil},
	}

	for _, action := range actions {
		inst.Action = action.s
		// do you feel dirty yet?
		c.Check(fmt.Sprintf("%p", action.impl), check.Equals, fmt.Sprintf("%p", inst.dispatch()))
	}
}

func (s *apiSuite) TestPostSnapEnableDisableSwitchRevision(c *check.C) {
	for _, action := range []string{"enable", "disable", "switch"} {
		buf := bytes.NewBufferString(`{"action": "` + action + `", "revision": "42"}`)
		req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
		c.Assert(err, check.IsNil)

		rsp := postSnap(snapCmd, req, nil).(*resp)

		c.Check(rsp.Type, check.Equals, ResponseTypeError)
		c.Check(rsp.Status, check.Equals, 400)
		c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "takes no revision")
	}
}

var sideLoadBodyWithoutDevMode = "" +
	"----hello--\r\n" +
	"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
	"\r\n" +
	"xyzzy\r\n" +
	"----hello--\r\n" +
	"Content-Disposition: form-data; name=\"dangerous\"\r\n" +
	"\r\n" +
	"true\r\n" +
	"----hello--\r\n" +
	"Content-Disposition: form-data; name=\"snap-path\"\r\n" +
	"\r\n" +
	"a/b/local.snap\r\n" +
	"----hello--\r\n"

func (s *apiSuite) TestSideloadSnapOnNonDevModeDistro(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	chgSummary := s.sideloadCheck(c, body, head, "local", snapstate.Flags{RemoveSnapPath: true})
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "a/b/local.snap"`)
}

func (s *apiSuite) TestSideloadSnapOnDevModeDistro(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	restore := sandbox.MockForceDevMode(true)
	defer restore()
	flags := snapstate.Flags{RemoveSnapPath: true}
	chgSummary := s.sideloadCheck(c, body, head, "local", flags)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "a/b/local.snap"`)
}

func (s *apiSuite) TestSideloadSnapDevMode(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	// try a multipart/form-data upload
	flags := snapstate.Flags{RemoveSnapPath: true}
	flags.DevMode = true
	chgSummary := s.sideloadCheck(c, body, head, "local", flags)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "x"`)
}

func (s *apiSuite) TestSideloadSnapJailMode(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"jailmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"dangerous\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	// try a multipart/form-data upload
	flags := snapstate.Flags{JailMode: true, RemoveSnapPath: true}
	chgSummary := s.sideloadCheck(c, body, head, "local", flags)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "x"`)
}

func (s *apiSuite) sideloadCheck(c *check.C, content string, head map[string]string, expectedInstanceName string, expectedFlags snapstate.Flags) string {
	d := s.daemonWithFakeSnapManager(c)

	soon := 0
	ensureStateSoon = func(st *state.State) {
		soon++
		ensureStateSoonImpl(st)
	}

	c.Assert(expectedInstanceName != "", check.Equals, true, check.Commentf("expected instance name must be set"))
	mockedName, _ := snap.SplitInstanceName(expectedInstanceName)

	// setup done
	installQueue := []string{}
	unsafeReadSnapInfo = func(path string) (*snap.Info, error) {
		return &snap.Info{SuggestedName: mockedName}, nil
	}

	snapstateInstall = func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		// NOTE: ubuntu-core is not installed in developer mode
		c.Check(flags, check.Equals, snapstate.Flags{})
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	snapstateInstallPath = func(s *state.State, si *snap.SideInfo, path, name, channel string, flags snapstate.Flags) (*state.TaskSet, *snap.Info, error) {
		c.Check(flags, check.DeepEquals, expectedFlags)

		c.Check(path, testutil.FileEquals, "xyzzy")

		c.Check(name, check.Equals, expectedInstanceName)

		installQueue = append(installQueue, si.RealName+"::"+path)
		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), &snap.Info{SuggestedName: name}, nil
	}

	buf := bytes.NewBufferString(content)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	for k, v := range head {
		req.Header.Set(k, v)
	}

	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeAsync)
	n := 1
	c.Assert(installQueue, check.HasLen, n)
	c.Check(installQueue[n-1], check.Matches, "local::.*/"+regexp.QuoteMeta(dirs.LocalInstallBlobTempPrefix)+".*")

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	c.Check(soon, check.Equals, 1)

	c.Assert(chg.Tasks(), check.HasLen, n)

	st.Unlock()
	s.waitTrivialChange(c, chg)
	st.Lock()

	c.Check(chg.Kind(), check.Equals, "install-snap")
	var names []string
	err = chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{expectedInstanceName})
	var apiData map[string]interface{}
	err = chg.Get("api-data", &apiData)
	c.Assert(err, check.IsNil)
	c.Check(apiData, check.DeepEquals, map[string]interface{}{
		"snap-name": expectedInstanceName,
	})

	return chg.Summary()
}

func (s *apiSuite) TestSideloadSnapJailModeAndDevmode(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"jailmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	s.daemonWithOverlordMock(c)

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Equals, "cannot use devmode and jailmode flags together")
}

func (s *apiSuite) TestSideloadSnapJailModeInDevModeOS(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"jailmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	s.daemonWithOverlordMock(c)

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	restore := sandbox.MockForceDevMode(true)
	defer restore()

	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Equals, "this system cannot honour the jailmode flag")
}

func (s *apiSuite) TestLocalInstallSnapDeriveSideInfo(c *check.C) {
	d := s.daemonWithOverlordMock(c)
	// add the assertions first
	st := d.overlord.State()

	dev1Acct := assertstest.NewAccount(s.StoreSigning, "devel1", nil, "")

	snapDecl, err := s.StoreSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "x-id",
		"snap-name":    "x",
		"publisher-id": dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)

	snapRev, err := s.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": "YK0GWATaZf09g_fvspYPqm_qtaiqf-KjaNj5uMEQCjQpuXWPjqQbeBINL5H_A0Lo",
		"snap-size":     "5",
		"snap-id":       "x-id",
		"snap-revision": "41",
		"developer-id":  dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)

	func() {
		st.Lock()
		defer st.Unlock()
		assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""), dev1Acct, snapDecl, snapRev)
	}()

	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x.snap\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n"
	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	snapstateInstallPath = func(s *state.State, si *snap.SideInfo, path, name, channel string, flags snapstate.Flags) (*state.TaskSet, *snap.Info, error) {
		c.Check(flags, check.Equals, snapstate.Flags{RemoveSnapPath: true})
		c.Check(si, check.DeepEquals, &snap.SideInfo{
			RealName: "x",
			SnapID:   "x-id",
			Revision: snap.R(41),
		})

		return state.NewTaskSet(), &snap.Info{SuggestedName: "x"}, nil
	}

	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeAsync)

	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Summary(), check.Equals, `Install "x" snap from file "x.snap"`)
	var names []string
	err = chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{"x"})
	var apiData map[string]interface{}
	err = chg.Get("api-data", &apiData)
	c.Assert(err, check.IsNil)
	c.Check(apiData, check.DeepEquals, map[string]interface{}{
		"snap-name": "x",
	})
}

func (s *apiSuite) TestSideloadSnapNoSignaturesDangerOff(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n"
	s.daemonWithOverlordMock(c)

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	// this is the prefix used for tempfiles for sideloading
	glob := filepath.Join(os.TempDir(), "snapd-sideload-pkg-*")
	glbBefore, _ := filepath.Glob(glob)
	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Equals, `cannot find signatures with metadata for snap "x"`)
	glbAfter, _ := filepath.Glob(glob)
	c.Check(len(glbBefore), check.Equals, len(glbAfter))
}

func (s *apiSuite) TestSideloadSnapNotValidFormFile(c *check.C) {
	newTestDaemon(c)

	// try a multipart/form-data upload with missing "name"
	content := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}

	buf := bytes.NewBufferString(content)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	for k, v := range head {
		req.Header.Set(k, v)
	}

	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Assert(rsp.Result.(*errorResult).Message, check.Matches, `cannot find "snap" file field in provided multipart/form-data payload`)
}

func (s *apiSuite) TestSideloadSnapChangeConflict(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"dangerous\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	s.daemonWithOverlordMock(c)

	unsafeReadSnapInfo = func(path string) (*snap.Info, error) {
		return &snap.Info{SuggestedName: "foo"}, nil
	}

	snapstateInstallPath = func(s *state.State, si *snap.SideInfo, path, name, channel string, flags snapstate.Flags) (*state.TaskSet, *snap.Info, error) {
		return nil, nil, &snapstate.ChangeConflictError{Snap: "foo"}
	}

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Kind, check.Equals, client.ErrorKindSnapChangeConflict)
}

func (s *apiSuite) TestSideloadSnapInstanceName(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode +
		"Content-Disposition: form-data; name=\"name\"\r\n" +
		"\r\n" +
		"local_instance\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	chgSummary := s.sideloadCheck(c, body, head, "local_instance", snapstate.Flags{RemoveSnapPath: true})
	c.Check(chgSummary, check.Equals, `Install "local_instance" snap from file "a/b/local.snap"`)
}

func (s *apiSuite) TestSideloadSnapInstanceNameNoKey(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode +
		"Content-Disposition: form-data; name=\"name\"\r\n" +
		"\r\n" +
		"local\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	chgSummary := s.sideloadCheck(c, body, head, "local", snapstate.Flags{RemoveSnapPath: true})
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "a/b/local.snap"`)
}

func (s *apiSuite) TestSideloadSnapInstanceNameMismatch(c *check.C) {
	s.daemonWithFakeSnapManager(c)

	unsafeReadSnapInfo = func(path string) (*snap.Info, error) {
		return &snap.Info{SuggestedName: "bar"}, nil
	}

	body := sideLoadBodyWithoutDevMode +
		"Content-Disposition: form-data; name=\"name\"\r\n" +
		"\r\n" +
		"foo_instance\r\n" +
		"----hello--\r\n"

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Equals, `instance name "foo_instance" does not match snap name "bar"`)
}

func (s *apiSuite) TestTrySnap(c *check.C) {
	d := s.daemonWithFakeSnapManager(c)

	var err error

	// mock a try dir
	tryDir := c.MkDir()
	snapYaml := filepath.Join(tryDir, "meta", "snap.yaml")
	err = os.MkdirAll(filepath.Dir(snapYaml), 0755)
	c.Assert(err, check.IsNil)
	err = ioutil.WriteFile(snapYaml, []byte("name: foo\nversion: 1.0\n"), 0644)
	c.Assert(err, check.IsNil)

	reqForFlags := func(f snapstate.Flags) *http.Request {
		b := "" +
			"--hello\r\n" +
			"Content-Disposition: form-data; name=\"action\"\r\n" +
			"\r\n" +
			"try\r\n" +
			"--hello\r\n" +
			"Content-Disposition: form-data; name=\"snap-path\"\r\n" +
			"\r\n" +
			tryDir + "\r\n" +
			"--hello"

		snip := "\r\n" +
			"Content-Disposition: form-data; name=%q\r\n" +
			"\r\n" +
			"true\r\n" +
			"--hello"

		if f.DevMode {
			b += fmt.Sprintf(snip, "devmode")
		}
		if f.JailMode {
			b += fmt.Sprintf(snip, "jailmode")
		}
		if f.Classic {
			b += fmt.Sprintf(snip, "classic")
		}
		b += "--\r\n"

		req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(b))
		c.Assert(err, check.IsNil)
		req.Header.Set("Content-Type", "multipart/thing; boundary=hello")

		return req
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()

	for _, t := range []struct {
		flags snapstate.Flags
		desc  string
	}{
		{snapstate.Flags{}, "core; -"},
		{snapstate.Flags{DevMode: true}, "core; devmode"},
		{snapstate.Flags{JailMode: true}, "core; jailmode"},
		{snapstate.Flags{Classic: true}, "core; classic"},
	} {
		soon := 0
		ensureStateSoon = func(st *state.State) {
			soon++
			ensureStateSoonImpl(st)
		}

		tryWasCalled := true
		snapstateTryPath = func(s *state.State, name, path string, flags snapstate.Flags) (*state.TaskSet, error) {
			c.Check(flags, check.DeepEquals, t.flags, check.Commentf(t.desc))
			tryWasCalled = true
			t := s.NewTask("fake-install-snap", "Doing a fake try")
			return state.NewTaskSet(t), nil
		}

		snapstateInstall = func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
			if name != "core" {
				c.Check(flags, check.DeepEquals, t.flags, check.Commentf(t.desc))
			}
			t := s.NewTask("fake-install-snap", "Doing a fake install")
			return state.NewTaskSet(t), nil
		}

		// try the snap (without an installed core)
		st.Unlock()
		rsp := postSnaps(snapsCmd, reqForFlags(t.flags), nil).(*resp)
		st.Lock()
		c.Assert(rsp.Type, check.Equals, ResponseTypeAsync, check.Commentf(t.desc))
		c.Assert(tryWasCalled, check.Equals, true, check.Commentf(t.desc))

		chg := st.Change(rsp.Change)
		c.Assert(chg, check.NotNil, check.Commentf(t.desc))

		c.Assert(chg.Tasks(), check.HasLen, 1, check.Commentf(t.desc))

		st.Unlock()
		s.waitTrivialChange(c, chg)
		st.Lock()

		c.Check(chg.Kind(), check.Equals, "try-snap", check.Commentf(t.desc))
		c.Check(chg.Summary(), check.Equals, fmt.Sprintf(`Try "%s" snap from %s`, "foo", tryDir), check.Commentf(t.desc))
		var names []string
		err = chg.Get("snap-names", &names)
		c.Assert(err, check.IsNil, check.Commentf(t.desc))
		c.Check(names, check.DeepEquals, []string{"foo"}, check.Commentf(t.desc))
		var apiData map[string]interface{}
		err = chg.Get("api-data", &apiData)
		c.Assert(err, check.IsNil, check.Commentf(t.desc))
		c.Check(apiData, check.DeepEquals, map[string]interface{}{
			"snap-name": "foo",
		}, check.Commentf(t.desc))

		c.Check(soon, check.Equals, 1, check.Commentf(t.desc))
	}
}

func (s *apiSuite) TestTrySnapRelative(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := trySnap(snapsCmd, req, nil, "relative-path", snapstate.Flags{}).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "need an absolute path")
}

func (s *apiSuite) TestTrySnapNotDir(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := trySnap(snapsCmd, req, nil, "/does/not/exist", snapstate.Flags{}).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "not a snap directory")
}

func (s *apiSuite) TestTryChangeConflict(c *check.C) {
	s.daemonWithOverlordMock(c)

	// mock a try dir
	tryDir := c.MkDir()

	unsafeReadSnapInfo = func(path string) (*snap.Info, error) {
		return &snap.Info{SuggestedName: "foo"}, nil
	}

	snapstateTryPath = func(s *state.State, name, path string, flags snapstate.Flags) (*state.TaskSet, error) {
		return nil, &snapstate.ChangeConflictError{Snap: "foo"}
	}

	req, err := http.NewRequest("POST", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := trySnap(snapsCmd, req, nil, tryDir, snapstate.Flags{}).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Kind, check.Equals, client.ErrorKindSnapChangeConflict)
}

func (s *apiSuite) TestAppIconGet(c *check.C) {
	d := s.daemon(c)

	// have an active foo in the system
	info := s.mkInstalledInState(c, d, "foo", "bar", "v1", snap.R(10), true, "")

	// have an icon for it in the package itself
	iconfile := filepath.Join(info.MountDir(), "meta", "gui", "icon.ick")
	c.Assert(os.MkdirAll(filepath.Dir(iconfile), 0755), check.IsNil)
	c.Check(ioutil.WriteFile(iconfile, []byte("ick"), 0644), check.IsNil)

	s.vars = map[string]string{"name": "foo"}
	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Body.String(), check.Equals, "ick")
}

func (s *apiSuite) TestAppIconGetInactive(c *check.C) {
	d := s.daemon(c)

	// have an *in*active foo in the system
	info := s.mkInstalledInState(c, d, "foo", "bar", "v1", snap.R(10), false, "")

	// have an icon for it in the package itself
	iconfile := filepath.Join(info.MountDir(), "meta", "gui", "icon.ick")
	c.Assert(os.MkdirAll(filepath.Dir(iconfile), 0755), check.IsNil)
	c.Check(ioutil.WriteFile(iconfile, []byte("ick"), 0644), check.IsNil)

	s.vars = map[string]string{"name": "foo"}
	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Body.String(), check.Equals, "ick")
}

func (s *apiSuite) TestAppIconGetNoIcon(c *check.C) {
	d := s.daemon(c)

	// have an *in*active foo in the system
	info := s.mkInstalledInState(c, d, "foo", "bar", "v1", snap.R(10), true, "")

	// NO ICON!
	err := os.RemoveAll(filepath.Join(info.MountDir(), "meta", "gui", "icon.svg"))
	c.Assert(err, check.IsNil)

	s.vars = map[string]string{"name": "foo"}
	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code/100, check.Equals, 4)
}

func (s *apiSuite) TestAppIconGetNoApp(c *check.C) {
	s.daemon(c)

	s.vars = map[string]string{"name": "foo"}
	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 404)
}

func (s *apiSuite) TestNotInstalledSnapIcon(c *check.C) {
	info := &snap.Info{SuggestedName: "notInstalledSnap", Media: []snap.MediaInfo{{Type: "icon", URL: "icon.svg"}}}
	iconfile := snapIcon(info)
	c.Check(iconfile, check.Equals, "")
}

func (s *apiSuite) TestInstallOnNonDevModeDistro(c *check.C) {
	s.testInstall(c, false, snapstate.Flags{}, snap.R(0))
}
func (s *apiSuite) TestInstallOnDevModeDistro(c *check.C) {
	s.testInstall(c, true, snapstate.Flags{}, snap.R(0))
}
func (s *apiSuite) TestInstallRevision(c *check.C) {
	s.testInstall(c, false, snapstate.Flags{}, snap.R(42))
}

func (s *apiSuite) testInstall(c *check.C, forcedDevmode bool, flags snapstate.Flags, revision snap.Revision) {
	calledFlags := snapstate.Flags{}
	installQueue := []string{}
	restore := sandbox.MockForceDevMode(forcedDevmode)
	defer restore()

	snapstateInstall = func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		installQueue = append(installQueue, name)
		c.Check(revision, check.Equals, opts.Revision)

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	defer func() {
		snapstateInstall = nil
	}()

	d := s.daemonWithFakeSnapManager(c)

	var buf bytes.Buffer
	if revision.Unset() {
		buf.WriteString(`{"action": "install"}`)
	} else {
		fmt.Fprintf(&buf, `{"action": "install", "revision": %s}`, revision.String())
	}
	req, err := http.NewRequest("POST", "/v2/snaps/some-snap", &buf)
	c.Assert(err, check.IsNil)

	s.vars = map[string]string{"name": "some-snap"}
	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Assert(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	c.Check(chg.Tasks(), check.HasLen, 1)

	st.Unlock()
	s.waitTrivialChange(c, chg)
	st.Lock()

	c.Check(chg.Status(), check.Equals, state.DoneStatus)
	c.Check(calledFlags, check.Equals, flags)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(chg.Kind(), check.Equals, "install-snap")
	c.Check(chg.Summary(), check.Equals, `Install "some-snap" snap`)
}

func (s *apiSuite) TestInstallUserAgentContextCreated(c *check.C) {
	snapstateInstall = func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		s.ctx = ctx
		t := st.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}
	defer func() {
		snapstateInstall = nil
	}()

	s.daemonWithFakeSnapManager(c)

	var buf bytes.Buffer
	buf.WriteString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/some-snap", &buf)
	req.RemoteAddr = "pid=100;uid=0;socket=;"
	c.Assert(err, check.IsNil)
	req.Header.Add("User-Agent", "some-agent/1.0")

	s.vars = map[string]string{"name": "some-snap"}
	rec := httptest.NewRecorder()
	snapCmd.ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	c.Check(store.ClientUserAgent(s.ctx), check.Equals, "some-agent/1.0")
}

func (s *apiSuite) TestRefresh(c *check.C) {
	var calledFlags snapstate.Flags
	calledUserID := 0
	installQueue := []string{}
	assertstateCalledUserID := 0

	snapstateUpdate = func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		calledUserID = userID
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		assertstateCalledUserID = userID
		return nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "refresh",
		Snaps:  []string{"some-snap"},
		userID: 17,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(assertstateCalledUserID, check.Equals, 17)
	c.Check(calledFlags, check.DeepEquals, snapstate.Flags{})
	c.Check(calledUserID, check.Equals, 17)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *apiSuite) TestRefreshDevMode(c *check.C) {
	var calledFlags snapstate.Flags
	calledUserID := 0
	installQueue := []string{}

	snapstateUpdate = func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		calledUserID = userID
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		return nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:  "refresh",
		DevMode: true,
		Snaps:   []string{"some-snap"},
		userID:  17,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	flags := snapstate.Flags{}
	flags.DevMode = true
	c.Check(calledFlags, check.DeepEquals, flags)
	c.Check(calledUserID, check.Equals, 17)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *apiSuite) TestRefreshClassic(c *check.C) {
	var calledFlags snapstate.Flags

	snapstateUpdate = func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		return nil, nil
	}
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		return nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:  "refresh",
		Classic: true,
		Snaps:   []string{"some-snap"},
		userID:  17,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags, check.DeepEquals, snapstate.Flags{Classic: true})
}

func (s *apiSuite) TestRefreshIgnoreValidation(c *check.C) {
	var calledFlags snapstate.Flags
	calledUserID := 0
	installQueue := []string{}

	snapstateUpdate = func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		calledUserID = userID
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		return nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:           "refresh",
		IgnoreValidation: true,
		Snaps:            []string{"some-snap"},
		userID:           17,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	flags := snapstate.Flags{}
	flags.IgnoreValidation = true

	c.Check(calledFlags, check.DeepEquals, flags)
	c.Check(calledUserID, check.Equals, 17)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *apiSuite) TestRefreshIgnoreRunning(c *check.C) {
	var calledFlags snapstate.Flags
	installQueue := []string{}

	snapstateUpdate = func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		return nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:        "refresh",
		IgnoreRunning: true,
		Snaps:         []string{"some-snap"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	flags := snapstate.Flags{}
	flags.IgnoreRunning = true

	c.Check(calledFlags, check.DeepEquals, flags)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *apiSuite) TestRefreshCohort(c *check.C) {
	cohort := ""

	snapstateUpdate = func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		cohort = opts.CohortKey

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		return nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "refresh",
		Snaps:  []string{"some-snap"},
		snapRevisionOptions: snapRevisionOptions{
			CohortKey: "xyzzy",
		},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(cohort, check.Equals, "xyzzy")
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *apiSuite) TestRefreshLeaveCohort(c *check.C) {
	var leave *bool

	snapstateUpdate = func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		leave = &opts.LeaveCohort

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		return nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:              "refresh",
		snapRevisionOptions: snapRevisionOptions{LeaveCohort: true},
		Snaps:               []string{"some-snap"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(*leave, check.Equals, true)
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *apiSuite) TestSwitchInstruction(c *check.C) {
	var cohort, channel string
	var leave *bool
	snapstateSwitch = func(s *state.State, name string, opts *snapstate.RevisionOptions) (*state.TaskSet, error) {
		cohort = opts.CohortKey
		leave = &opts.LeaveCohort
		channel = opts.Channel

		t := s.NewTask("fake-switch", "Doing a fake switch")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	st := d.overlord.State()

	type T struct {
		channel string
		cohort  string
		leave   bool
		summary string
	}
	table := []T{
		{"", "some-cohort", false, `Switch "some-snap" snap to cohort "…me-cohort"`},
		{"some-channel", "", false, `Switch "some-snap" snap to channel "some-channel"`},
		{"some-channel", "some-cohort", false, `Switch "some-snap" snap to channel "some-channel" and cohort "…me-cohort"`},
		{"", "", true, `Switch "some-snap" snap away from cohort`},
		{"some-channel", "", true, `Switch "some-snap" snap to channel "some-channel" and away from cohort`},
	}

	for _, t := range table {
		cohort, channel = "", ""
		leave = nil
		inst := &snapInstruction{
			Action: "switch",
			snapRevisionOptions: snapRevisionOptions{
				CohortKey:   t.cohort,
				LeaveCohort: t.leave,
				Channel:     t.channel,
			},
			Snaps: []string{"some-snap"},
		}

		st.Lock()
		summary, _, err := inst.dispatch()(inst, st)
		st.Unlock()
		c.Check(err, check.IsNil)

		c.Check(cohort, check.Equals, t.cohort)
		c.Check(channel, check.Equals, t.channel)
		c.Check(summary, check.Equals, t.summary)
		c.Check(*leave, check.Equals, t.leave)
	}
}

func (s *apiSuite) TestPostSnapOp(c *check.C) {
	s.testPostSnapsOp(c, "application/json")
}

func (s *apiSuite) TestPostSnapOpMoreComplexContentType(c *check.C) {
	s.testPostSnapsOp(c, "application/json; charset=utf-8")
}

func (s *apiSuite) TestPostSnapOpInvalidCharset(c *check.C) {
	buf := bytes.NewBufferString(`{"action": "refresh"}`)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/json; charset=iso-8859-1")

	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "unknown charset in content type")
}

func (s *apiSuite) testPostSnapsOp(c *check.C, contentType string) {
	assertstateRefreshSnapDeclarations = func(*state.State, int) error { return nil }
	snapstateUpdateMany = func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 0)
		t := s.NewTask("fake-refresh-all", "Refreshing everything")
		return []string{"fake1", "fake2"}, []*state.TaskSet{state.NewTaskSet(t)}, nil
	}

	d := s.daemonWithOverlordMock(c)

	buf := bytes.NewBufferString(`{"action": "refresh"}`)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", contentType)

	rsp, ok := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(ok, check.Equals, true)
	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Check(chg.Summary(), check.Equals, `Refresh snaps "fake1", "fake2"`)
	var apiData map[string]interface{}
	c.Check(chg.Get("api-data", &apiData), check.IsNil)
	c.Check(apiData["snap-names"], check.DeepEquals, []interface{}{"fake1", "fake2"})
}

func (s *apiSuite) TestRefreshAll(c *check.C) {
	refreshSnapDecls := false
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		refreshSnapDecls = true
		return assertstate.RefreshSnapDeclarations(s, userID)
	}
	d := s.daemon(c)

	for _, tst := range []struct {
		snaps []string
		msg   string
	}{
		{nil, "Refresh all snaps: no updates"},
		{[]string{"fake"}, `Refresh snap "fake"`},
		{[]string{"fake1", "fake2"}, `Refresh snaps "fake1", "fake2"`},
	} {
		refreshSnapDecls = false

		snapstateUpdateMany = func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
			c.Check(names, check.HasLen, 0)
			t := s.NewTask("fake-refresh-all", "Refreshing everything")
			return tst.snaps, []*state.TaskSet{state.NewTaskSet(t)}, nil
		}

		inst := &snapInstruction{Action: "refresh"}
		st := d.overlord.State()
		st.Lock()
		res, err := snapUpdateMany(inst, st)
		st.Unlock()
		c.Assert(err, check.IsNil)
		c.Check(res.Summary, check.Equals, tst.msg)
		c.Check(refreshSnapDecls, check.Equals, true)
	}
}

func (s *apiSuite) TestRefreshAllNoChanges(c *check.C) {
	refreshSnapDecls := false
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		refreshSnapDecls = true
		return assertstate.RefreshSnapDeclarations(s, userID)
	}

	snapstateUpdateMany = func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 0)
		return nil, nil, nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{Action: "refresh"}
	st := d.overlord.State()
	st.Lock()
	res, err := snapUpdateMany(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Refresh all snaps: no updates`)
	c.Check(refreshSnapDecls, check.Equals, true)
}

func (s *apiSuite) TestRefreshMany(c *check.C) {
	refreshSnapDecls := false
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		refreshSnapDecls = true
		return nil
	}

	snapstateUpdateMany = func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 2)
		t := s.NewTask("fake-refresh-2", "Refreshing two")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{Action: "refresh", Snaps: []string{"foo", "bar"}}
	st := d.overlord.State()
	st.Lock()
	res, err := snapUpdateMany(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Refresh snaps "foo", "bar"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)
	c.Check(refreshSnapDecls, check.Equals, true)
}

func (s *apiSuite) TestRefreshMany1(c *check.C) {
	refreshSnapDecls := false
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		refreshSnapDecls = true
		return nil
	}

	snapstateUpdateMany = func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 1)
		t := s.NewTask("fake-refresh-1", "Refreshing one")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{Action: "refresh", Snaps: []string{"foo"}}
	st := d.overlord.State()
	st.Lock()
	res, err := snapUpdateMany(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Refresh snap "foo"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)
	c.Check(refreshSnapDecls, check.Equals, true)
}

func (s *apiSuite) TestInstallMany(c *check.C) {
	snapstateInstallMany = func(s *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 2)
		t := s.NewTask("fake-install-2", "Install two")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{Action: "install", Snaps: []string{"foo", "bar"}}
	st := d.overlord.State()
	st.Lock()
	res, err := snapInstallMany(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Install snaps "foo", "bar"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)
}

func (s *apiSuite) TestInstallManyEmptyName(c *check.C) {
	snapstateInstallMany = func(_ *state.State, _ []string, _ int) ([]string, []*state.TaskSet, error) {
		return nil, nil, errors.New("should not be called")
	}
	d := s.daemon(c)
	inst := &snapInstruction{Action: "install", Snaps: []string{"", "bar"}}
	st := d.overlord.State()
	st.Lock()
	res, err := snapInstallMany(inst, st)
	st.Unlock()
	c.Assert(res, check.IsNil)
	c.Assert(err, check.ErrorMatches, "cannot install snap with empty name")
}

func (s *apiSuite) TestRemoveMany(c *check.C) {
	snapstateRemoveMany = func(s *state.State, names []string) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 2)
		t := s.NewTask("fake-remove-2", "Remove two")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{Action: "remove", Snaps: []string{"foo", "bar"}}
	st := d.overlord.State()
	st.Lock()
	res, err := snapRemoveMany(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Remove snaps "foo", "bar"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)
}

func (s *apiSuite) TestInstallFails(c *check.C) {
	snapstateInstall = func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		t := s.NewTask("fake-install-snap-error", "Install task")
		return state.NewTaskSet(t), nil
	}

	d := s.daemonWithFakeSnapManager(c)
	s.vars = map[string]string{"name": "hello-world"}
	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Assert(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	c.Check(chg.Tasks(), check.HasLen, 1)

	st.Unlock()
	s.waitTrivialChange(c, chg)
	st.Lock()

	c.Check(chg.Err(), check.ErrorMatches, `(?sm).*Install task \(fake-install-snap-error errored\)`)
}

func (s *apiSuite) TestInstallLeaveOld(c *check.C) {
	c.Skip("temporarily dropped half-baked support while sorting out flag mess")
	var calledFlags snapstate.Flags

	snapstateInstall = func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:   "install",
		LeaveOld: true,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Assert(err, check.IsNil)

	c.Check(calledFlags, check.DeepEquals, snapstate.Flags{})
	c.Check(err, check.IsNil)
}

func (s *apiSuite) TestInstall(c *check.C) {
	var calledName string

	snapstateInstall = func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledName = name

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "install",
		// Install the snap in developer mode
		DevMode: true,
		Snaps:   []string{"fake"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)
	c.Check(calledName, check.Equals, "fake")
}

func (s *apiSuite) TestInstallCohort(c *check.C) {
	var calledName string
	var calledCohort string

	snapstateInstall = func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledName = name
		calledCohort = opts.CohortKey

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "install",
		snapRevisionOptions: snapRevisionOptions{
			CohortKey: "To the legion of the lost ones, to the cohort of the damned.",
		},
		Snaps: []string{"fake"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	msg, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)
	c.Check(calledName, check.Equals, "fake")
	c.Check(calledCohort, check.Equals, "To the legion of the lost ones, to the cohort of the damned.")
	c.Check(msg, check.Equals, `Install "fake" snap from "…e damned." cohort`)
}

func (s *apiSuite) TestInstallDevMode(c *check.C) {
	var calledFlags snapstate.Flags

	snapstateInstall = func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "install",
		// Install the snap in developer mode
		DevMode: true,
		Snaps:   []string{"fake"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags.DevMode, check.Equals, true)
}

func (s *apiSuite) TestInstallJailMode(c *check.C) {
	var calledFlags snapstate.Flags

	snapstateInstall = func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:   "install",
		JailMode: true,
		Snaps:    []string{"fake"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags.JailMode, check.Equals, true)
}

func (s *apiSuite) TestInstallJailModeDevModeOS(c *check.C) {
	restore := sandbox.MockForceDevMode(true)
	defer restore()

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:   "install",
		JailMode: true,
		Snaps:    []string{"foo"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.ErrorMatches, "this system cannot honour the jailmode flag")
}

func (s *apiSuite) TestInstallEmptyName(c *check.C) {
	snapstateInstall = func(ctx context.Context, _ *state.State, _ string, _ *snapstate.RevisionOptions, _ int, _ snapstate.Flags) (*state.TaskSet, error) {
		return nil, errors.New("should not be called")
	}
	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "install",
		Snaps:  []string{""},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.ErrorMatches, "cannot install snap with empty name")
}

func (s *apiSuite) TestInstallJailModeDevMode(c *check.C) {
	d := s.daemon(c)
	inst := &snapInstruction{
		Action:   "install",
		DevMode:  true,
		JailMode: true,
		Snaps:    []string{"foo"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.ErrorMatches, "cannot use devmode and jailmode flags together")
}

func (s *apiSuite) testRevertSnap(inst *snapInstruction, c *check.C) {
	queue := []string{}

	instFlags, err := inst.modeFlags()
	c.Assert(err, check.IsNil)

	snapstateRevert = func(s *state.State, name string, flags snapstate.Flags) (*state.TaskSet, error) {
		c.Check(flags, check.Equals, instFlags)
		queue = append(queue, name)
		return nil, nil
	}
	snapstateRevertToRevision = func(s *state.State, name string, rev snap.Revision, flags snapstate.Flags) (*state.TaskSet, error) {
		c.Check(flags, check.Equals, instFlags)
		queue = append(queue, fmt.Sprintf("%s (%s)", name, rev))
		return nil, nil
	}

	d := s.daemon(c)
	inst.Action = "revert"
	inst.Snaps = []string{"some-snap"}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)
	if inst.Revision.Unset() {
		c.Check(queue, check.DeepEquals, []string{inst.Snaps[0]})
	} else {
		c.Check(queue, check.DeepEquals, []string{fmt.Sprintf("%s (%s)", inst.Snaps[0], inst.Revision)})
	}
	c.Check(summary, check.Equals, `Revert "some-snap" snap`)
}

func (s *apiSuite) TestRevertSnap(c *check.C) {
	s.testRevertSnap(&snapInstruction{}, c)
}

func (s *apiSuite) TestRevertSnapDevMode(c *check.C) {
	s.testRevertSnap(&snapInstruction{DevMode: true}, c)
}

func (s *apiSuite) TestRevertSnapJailMode(c *check.C) {
	s.testRevertSnap(&snapInstruction{JailMode: true}, c)
}

func (s *apiSuite) TestRevertSnapClassic(c *check.C) {
	s.testRevertSnap(&snapInstruction{Classic: true}, c)
}

func (s *apiSuite) TestRevertSnapToRevision(c *check.C) {
	s.testRevertSnap(&snapInstruction{snapRevisionOptions: snapRevisionOptions{Revision: snap.R(1)}}, c)
}

func (s *apiSuite) TestRevertSnapToRevisionDevMode(c *check.C) {
	s.testRevertSnap(&snapInstruction{snapRevisionOptions: snapRevisionOptions{Revision: snap.R(1)}, DevMode: true}, c)
}

func (s *apiSuite) TestRevertSnapToRevisionJailMode(c *check.C) {
	s.testRevertSnap(&snapInstruction{snapRevisionOptions: snapRevisionOptions{Revision: snap.R(1)}, JailMode: true}, c)
}

func (s *apiSuite) TestRevertSnapToRevisionClassic(c *check.C) {
	s.testRevertSnap(&snapInstruction{snapRevisionOptions: snapRevisionOptions{Revision: snap.R(1)}, Classic: true}, c)
}

func snapList(rawSnaps interface{}) []map[string]interface{} {
	snaps := make([]map[string]interface{}, len(rawSnaps.([]*json.RawMessage)))
	for i, raw := range rawSnaps.([]*json.RawMessage) {
		err := json.Unmarshal([]byte(*raw), &snaps[i])
		if err != nil {
			panic(err)
		}
	}
	return snaps
}

func (s *apiSuite) TestIsTrue(c *check.C) {
	form := &multipart.Form{}
	c.Check(isTrue(form, "foo"), check.Equals, false)
	for _, f := range []string{"", "false", "0", "False", "f", "try"} {
		form.Value = map[string][]string{"foo": {f}}
		c.Check(isTrue(form, "foo"), check.Equals, false, check.Commentf("expected %q to be false", f))
	}
	for _, t := range []string{"true", "1", "True", "t"} {
		form.Value = map[string][]string{"foo": {t}}
		c.Check(isTrue(form, "foo"), check.Equals, true, check.Commentf("expected %q to be true", t))
	}
}

// aliases

func (s *apiSuite) TestAliasSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.MockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &aliasAction{
		Action: "alias",
		Snap:   "alias-snap",
		App:    "app",
		Alias:  "alias1",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	aliasesCmd.POST(aliasesCmd, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.IsNil)

	// sanity check
	c.Check(osutil.IsSymlink(filepath.Join(dirs.SnapBinariesDir, "alias1")), check.Equals, true)
}

func (s *apiSuite) TestAliasChangeConflict(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	s.daemon(c)

	s.MockSnap(c, aliasYaml)

	s.SimulateConflict("alias-snap")

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	action := &aliasAction{
		Action: "alias",
		Snap:   "alias-snap",
		App:    "app",
		Alias:  "alias1",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	aliasesCmd.POST(aliasesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 409)

	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"status-code": 409.,
		"status":      "Conflict",
		"result": map[string]interface{}{
			"message": `snap "alias-snap" has "manip" change in progress`,
			"kind":    "snap-change-conflict",
			"value": map[string]interface{}{
				"change-kind": "manip",
				"snap-name":   "alias-snap",
			},
		},
		"type": "error"})
}

func (s *apiSuite) TestAliasErrors(c *check.C) {
	s.daemon(c)

	errScenarios := []struct {
		mangle func(*aliasAction)
		err    string
	}{
		{func(a *aliasAction) { a.Action = "" }, `unsupported alias action: ""`},
		{func(a *aliasAction) { a.Action = "what" }, `unsupported alias action: "what"`},
		{func(a *aliasAction) { a.Snap = "lalala" }, `snap "lalala" is not installed`},
		{func(a *aliasAction) { a.Alias = ".foo" }, `invalid alias name: ".foo"`},
		{func(a *aliasAction) { a.Aliases = []string{"baz"} }, `cannot interpret request, snaps can no longer be expected to declare their aliases`},
	}

	for _, scen := range errScenarios {
		action := &aliasAction{
			Action: "alias",
			Snap:   "alias-snap",
			App:    "app",
			Alias:  "alias1",
		}
		scen.mangle(action)

		text, err := json.Marshal(action)
		c.Assert(err, check.IsNil)
		buf := bytes.NewBuffer(text)
		req, err := http.NewRequest("POST", "/v2/aliases", buf)
		c.Assert(err, check.IsNil)

		rsp := changeAliases(aliasesCmd, req, nil).(*resp)
		c.Check(rsp.Type, check.Equals, ResponseTypeError)
		c.Check(rsp.Status, check.Equals, 400)
		c.Check(rsp.Result.(*errorResult).Message, check.Matches, scen.err)
	}
}

func (s *apiSuite) TestUnaliasSnapSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.MockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &aliasAction{
		Action: "unalias",
		Snap:   "alias-snap",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	aliasesCmd.POST(aliasesCmd, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	c.Check(chg.Summary(), check.Equals, `Disable all aliases for snap "alias-snap"`)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	defer st.Unlock()
	err = chg.Err()
	c.Assert(err, check.IsNil)

	// sanity check
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "alias-snap", &snapst)
	c.Assert(err, check.IsNil)
	c.Check(snapst.AutoAliasesDisabled, check.Equals, true)
}

func (s *apiSuite) TestUnaliasDWIMSnapSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.MockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &aliasAction{
		Action: "unalias",
		Snap:   "alias-snap",
		Alias:  "alias-snap",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	aliasesCmd.POST(aliasesCmd, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	c.Check(chg.Summary(), check.Equals, `Disable all aliases for snap "alias-snap"`)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	defer st.Unlock()
	err = chg.Err()
	c.Assert(err, check.IsNil)

	// sanity check
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "alias-snap", &snapst)
	c.Assert(err, check.IsNil)
	c.Check(snapst.AutoAliasesDisabled, check.Equals, true)
}

func (s *apiSuite) TestUnaliasAliasSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.MockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &aliasAction{
		Action: "alias",
		Snap:   "alias-snap",
		App:    "app",
		Alias:  "alias1",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	aliasesCmd.POST(aliasesCmd, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.IsNil)

	// unalias
	action = &aliasAction{
		Action: "unalias",
		Alias:  "alias1",
	}
	text, err = json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf = bytes.NewBuffer(text)
	req, err = http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec = httptest.NewRecorder()
	aliasesCmd.POST(aliasesCmd, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id = body["change"].(string)

	st.Lock()
	chg = st.Change(id)
	c.Check(chg.Summary(), check.Equals, `Remove manual alias "alias1" for snap "alias-snap"`)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	defer st.Unlock()
	err = chg.Err()
	c.Assert(err, check.IsNil)

	// sanity check
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapBinariesDir, "alias1")), check.Equals, false)
}

func (s *apiSuite) TestUnaliasDWIMAliasSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.MockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &aliasAction{
		Action: "alias",
		Snap:   "alias-snap",
		App:    "app",
		Alias:  "alias1",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	aliasesCmd.POST(aliasesCmd, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.IsNil)

	// DWIM unalias an alias
	action = &aliasAction{
		Action: "unalias",
		Snap:   "alias1",
		Alias:  "alias1",
	}
	text, err = json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf = bytes.NewBuffer(text)
	req, err = http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec = httptest.NewRecorder()
	aliasesCmd.POST(aliasesCmd, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id = body["change"].(string)

	st.Lock()
	chg = st.Change(id)
	c.Check(chg.Summary(), check.Equals, `Remove manual alias "alias1" for snap "alias-snap"`)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	defer st.Unlock()
	err = chg.Err()
	c.Assert(err, check.IsNil)

	// sanity check
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapBinariesDir, "alias1")), check.Equals, false)
}

func (s *apiSuite) TestPreferSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.MockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &aliasAction{
		Action: "prefer",
		Snap:   "alias-snap",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	aliasesCmd.POST(aliasesCmd, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	c.Check(chg.Summary(), check.Equals, `Prefer aliases of snap "alias-snap"`)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	defer st.Unlock()
	err = chg.Err()
	c.Assert(err, check.IsNil)

	// sanity check
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "alias-snap", &snapst)
	c.Assert(err, check.IsNil)
	c.Check(snapst.AutoAliasesDisabled, check.Equals, false)
}

func (s *apiSuite) TestAliases(c *check.C) {
	d := s.daemon(c)

	st := d.overlord.State()
	st.Lock()
	snapstate.Set(st, "alias-snap1", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap1", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Manual: "cmd1x", Auto: "cmd1"},
			"alias2": {Auto: "cmd2"},
		},
	})
	snapstate.Set(st, "alias-snap2", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap2", Revision: snap.R(12)},
		},
		Current:             snap.R(12),
		Active:              true,
		AutoAliasesDisabled: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias2": {Auto: "cmd2"},
			"alias3": {Manual: "cmd3"},
			"alias4": {Manual: "cmd4x", Auto: "cmd4"},
		},
	})
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/aliases", nil)
	c.Assert(err, check.IsNil)

	rsp := getAliases(aliasesCmd, req, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.DeepEquals, map[string]map[string]aliasStatus{
		"alias-snap1": {
			"alias1": {
				Command: "alias-snap1.cmd1x",
				Status:  "manual",
				Manual:  "cmd1x",
				Auto:    "cmd1",
			},
			"alias2": {
				Command: "alias-snap1.cmd2",
				Status:  "auto",
				Auto:    "cmd2",
			},
		},
		"alias-snap2": {
			"alias2": {
				Command: "alias-snap2.cmd2",
				Status:  "disabled",
				Auto:    "cmd2",
			},
			"alias3": {
				Command: "alias-snap2.cmd3",
				Status:  "manual",
				Manual:  "cmd3",
			},
			"alias4": {
				Command: "alias-snap2.cmd4x",
				Status:  "manual",
				Manual:  "cmd4x",
				Auto:    "cmd4",
			},
		},
	})

}

func (s *apiSuite) TestInstallUnaliased(c *check.C) {
	var calledFlags snapstate.Flags

	snapstateInstall = func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "install",
		// Install the snap without enabled automatic aliases
		Unaliased: true,
		Snaps:     []string{"fake"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags.Unaliased, check.Equals, true)
}

func (s *apiSuite) TestInstallIgnoreRunning(c *check.C) {
	var calledFlags snapstate.Flags

	snapstateInstall = func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "install",
		// Install the snap without enabled automatic aliases
		IgnoreRunning: true,
		Snaps:         []string{"fake"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags.IgnoreRunning, check.Equals, true)
}

func (s *apiSuite) TestInstallPathUnaliased(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"unaliased\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	// try a multipart/form-data upload
	flags := snapstate.Flags{Unaliased: true, RemoveSnapPath: true, DevMode: true}
	chgSummary := s.sideloadCheck(c, body, head, "local", flags)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "x"`)
}

func (s *apiSuite) TestLogsNoServices(c *check.C) {
	// NOTE this is *apiSuite, not *appSuite, so there are no
	// installed snaps with services

	cmd := testutil.MockCommand(c, "systemctl", "").Also("journalctl", "")
	defer cmd.Restore()
	s.daemon(c)
	s.d.overlord.Loop()
	defer s.d.overlord.Stop()

	req, err := http.NewRequest("GET", "/v2/logs", nil)
	c.Assert(err, check.IsNil)

	rsp := getLogs(logsCmd, req, nil).(*resp)
	c.Assert(rsp.Status, check.Equals, 404)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
}

type fakeNetError struct {
	message   string
	timeout   bool
	temporary bool
}

func (e fakeNetError) Error() string   { return e.message }
func (e fakeNetError) Timeout() bool   { return e.timeout }
func (e fakeNetError) Temporary() bool { return e.temporary }

func (s *apiSuite) TestErrToResponseNoSnapsDoesNotPanic(c *check.C) {
	si := &snapInstruction{Action: "frobble"}
	errors := []error{
		store.ErrSnapNotFound,
		&store.RevisionNotAvailableError{},
		store.ErrNoUpdateAvailable,
		store.ErrLocalSnap,
		&snap.AlreadyInstalledError{Snap: "foo"},
		&snap.NotInstalledError{Snap: "foo"},
		&snapstate.SnapNeedsDevModeError{Snap: "foo"},
		&snapstate.SnapNeedsClassicError{Snap: "foo"},
		&snapstate.SnapNeedsClassicSystemError{Snap: "foo"},
		fakeNetError{message: "other"},
		fakeNetError{message: "timeout", timeout: true},
		fakeNetError{message: "temp", temporary: true},
		errors.New("some other error"),
	}

	for _, err := range errors {
		rsp := si.errToResponse(err)
		com := check.Commentf("%v", err)
		c.Check(rsp, check.NotNil, com)
		status := rsp.(*resp).Status
		c.Check(status/100 == 4 || status/100 == 5, check.Equals, true, com)
	}
}

func (s *apiSuite) TestErrToResponseForRevisionNotAvailable(c *check.C) {
	si := &snapInstruction{Action: "frobble", Snaps: []string{"foo"}}

	thisArch := arch.DpkgArchitecture()

	err := &store.RevisionNotAvailableError{
		Action:  "install",
		Channel: "stable",
		Releases: []channel.Channel{
			snaptest.MustParseChannel("beta", thisArch),
		},
	}
	rsp := si.errToResponse(err).(*resp)
	c.Check(rsp, check.DeepEquals, &resp{
		Status: 404,
		Type:   ResponseTypeError,
		Result: &errorResult{
			Message: "no snap revision on specified channel",
			Kind:    client.ErrorKindSnapChannelNotAvailable,
			Value: map[string]interface{}{
				"snap-name":    "foo",
				"action":       "install",
				"channel":      "stable",
				"architecture": thisArch,
				"releases": []map[string]interface{}{
					{"architecture": thisArch, "channel": "beta"},
				},
			},
		},
	})

	err = &store.RevisionNotAvailableError{
		Action:  "install",
		Channel: "stable",
		Releases: []channel.Channel{
			snaptest.MustParseChannel("beta", "other-arch"),
		},
	}
	rsp = si.errToResponse(err).(*resp)
	c.Check(rsp, check.DeepEquals, &resp{
		Status: 404,
		Type:   ResponseTypeError,
		Result: &errorResult{
			Message: "no snap revision on specified architecture",
			Kind:    client.ErrorKindSnapArchitectureNotAvailable,
			Value: map[string]interface{}{
				"snap-name":    "foo",
				"action":       "install",
				"channel":      "stable",
				"architecture": thisArch,
				"releases": []map[string]interface{}{
					{"architecture": "other-arch", "channel": "beta"},
				},
			},
		},
	})

	err = &store.RevisionNotAvailableError{}
	rsp = si.errToResponse(err).(*resp)
	c.Check(rsp, check.DeepEquals, &resp{
		Status: 404,
		Type:   ResponseTypeError,
		Result: &errorResult{
			Message: "no snap revision available as specified",
			Kind:    client.ErrorKindSnapRevisionNotAvailable,
			Value:   "foo",
		},
	})
}

func (s *apiSuite) TestErrToResponseForChangeConflict(c *check.C) {
	si := &snapInstruction{Action: "frobble", Snaps: []string{"foo"}}

	err := &snapstate.ChangeConflictError{Snap: "foo", ChangeKind: "install"}
	rsp := si.errToResponse(err).(*resp)
	c.Check(rsp, check.DeepEquals, &resp{
		Status: 409,
		Type:   ResponseTypeError,
		Result: &errorResult{
			Message: `snap "foo" has "install" change in progress`,
			Kind:    client.ErrorKindSnapChangeConflict,
			Value: map[string]interface{}{
				"snap-name":   "foo",
				"change-kind": "install",
			},
		},
	})

	// only snap
	err = &snapstate.ChangeConflictError{Snap: "foo"}
	rsp = si.errToResponse(err).(*resp)
	c.Check(rsp, check.DeepEquals, &resp{
		Status: 409,
		Type:   ResponseTypeError,
		Result: &errorResult{
			Message: `snap "foo" has changes in progress`,
			Kind:    client.ErrorKindSnapChangeConflict,
			Value: map[string]interface{}{
				"snap-name": "foo",
			},
		},
	})

	// only kind
	err = &snapstate.ChangeConflictError{Message: "specific error msg", ChangeKind: "some-global-op"}
	rsp = si.errToResponse(err).(*resp)
	c.Check(rsp, check.DeepEquals, &resp{
		Status: 409,
		Type:   ResponseTypeError,
		Result: &errorResult{
			Message: "specific error msg",
			Kind:    client.ErrorKindSnapChangeConflict,
			Value: map[string]interface{}{
				"change-kind": "some-global-op",
			},
		},
	})
}

func (s *apiSuite) TestErrToResponseInsufficentSpace(c *check.C) {
	err := &snapstate.InsufficientSpaceError{
		Snaps:      []string{"foo", "bar"},
		ChangeKind: "some-change",
		Path:       "/path",
		Message:    "specific error msg",
	}
	rsp := errToResponse(err, nil, BadRequest, "%s: %v", "ERR").(*resp)
	c.Check(rsp, check.DeepEquals, &resp{
		Status: 507,
		Type:   ResponseTypeError,
		Result: &errorResult{
			Message: "specific error msg",
			Kind:    client.ErrorKindInsufficientDiskSpace,
			Value: map[string]interface{}{
				"snap-names":  []string{"foo", "bar"},
				"change-kind": "some-change",
			},
		},
	})
}

func (s *apiSuite) TestErrToResponse(c *check.C) {
	aie := &snap.AlreadyInstalledError{Snap: "foo"}
	nie := &snap.NotInstalledError{Snap: "foo"}
	cce := &snapstate.ChangeConflictError{Snap: "foo"}
	ndme := &snapstate.SnapNeedsDevModeError{Snap: "foo"}
	nc := &snapstate.SnapNotClassicError{Snap: "foo"}
	nce := &snapstate.SnapNeedsClassicError{Snap: "foo"}
	ncse := &snapstate.SnapNeedsClassicSystemError{Snap: "foo"}
	netoe := fakeNetError{message: "other"}
	nettoute := fakeNetError{message: "timeout", timeout: true}
	nettmpe := fakeNetError{message: "temp", temporary: true}

	e := errors.New("other error")

	sa1e := &store.SnapActionError{Refresh: map[string]error{"foo": store.ErrSnapNotFound}}
	sa2e := &store.SnapActionError{Refresh: map[string]error{
		"foo": store.ErrSnapNotFound,
		"bar": store.ErrSnapNotFound,
	}}
	saOe := &store.SnapActionError{Other: []error{e}}
	// this one can't happen (but fun to test):
	saXe := &store.SnapActionError{Refresh: map[string]error{"foo": sa1e}}

	makeErrorRsp := func(kind client.ErrorKind, err error, value interface{}) Response {
		return SyncResponse(&resp{
			Type:   ResponseTypeError,
			Result: &errorResult{Message: err.Error(), Kind: kind, Value: value},
			Status: 400,
		}, nil)
	}

	tests := []struct {
		err         error
		expectedRsp Response
	}{
		{store.ErrSnapNotFound, SnapNotFound("foo", store.ErrSnapNotFound)},
		{store.ErrNoUpdateAvailable, makeErrorRsp(client.ErrorKindSnapNoUpdateAvailable, store.ErrNoUpdateAvailable, "")},
		{store.ErrLocalSnap, makeErrorRsp(client.ErrorKindSnapLocal, store.ErrLocalSnap, "")},
		{aie, makeErrorRsp(client.ErrorKindSnapAlreadyInstalled, aie, "foo")},
		{nie, makeErrorRsp(client.ErrorKindSnapNotInstalled, nie, "foo")},
		{ndme, makeErrorRsp(client.ErrorKindSnapNeedsDevMode, ndme, "foo")},
		{nc, makeErrorRsp(client.ErrorKindSnapNotClassic, nc, "foo")},
		{nce, makeErrorRsp(client.ErrorKindSnapNeedsClassic, nce, "foo")},
		{ncse, makeErrorRsp(client.ErrorKindSnapNeedsClassicSystem, ncse, "foo")},
		{cce, SnapChangeConflict(cce)},
		{nettoute, makeErrorRsp(client.ErrorKindNetworkTimeout, nettoute, "")},
		{netoe, BadRequest("ERR: %v", netoe)},
		{nettmpe, BadRequest("ERR: %v", nettmpe)},
		{e, BadRequest("ERR: %v", e)},

		// action error unwrapping:
		{sa1e, SnapNotFound("foo", store.ErrSnapNotFound)},
		{saXe, SnapNotFound("foo", store.ErrSnapNotFound)},
		// action errors, unwrapped:
		{sa2e, BadRequest(`ERR: cannot refresh: snap not found: "bar", "foo"`)},
		{saOe, BadRequest("ERR: cannot refresh, install, or download: other error")},
	}

	for _, t := range tests {
		com := check.Commentf("%v", t.err)
		rsp := errToResponse(t.err, []string{"foo"}, BadRequest, "%s: %v", "ERR")
		c.Check(rsp, check.DeepEquals, t.expectedRsp, com)
	}
}
