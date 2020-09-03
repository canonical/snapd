// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"net/http/httptest"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

var _ = Suite(&themesSuite{})

type themesSuite struct {
	apiBaseSuite

	available map[string]*snap.Info
	err       error
}

func (s *themesSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)

	s.available = make(map[string]*snap.Info)
	s.err = store.ErrSnapNotFound
}

func (s *themesSuite) SnapInfo(ctx context.Context, spec store.SnapSpec, user *auth.UserState) (*snap.Info, error) {
	s.pokeStateLock()
	if info := s.available[spec.Name]; info != nil {
		return info, nil
	}
	return nil, s.err
}

func (s *themesSuite) daemon(c *C) *Daemon {
	d := s.apiBaseSuite.daemon(c)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	snapstate.ReplaceStore(st, s)
	return d
}

func (s *themesSuite) TestGetInstalledThemes(c *C) {
	s.daemon(c)
	s.mockSnap(c, `name: snap1
version: 42
slots:
  gtk-3-themes:
    interface: content
    content: gtk-3-themes
    source:
      read:
       - $SNAP/share/themes/Foo-gtk
       - $SNAP/share/themes/Foo-gtk-dark
  icon-themes:
    interface: content
    content: icon-themes
    source:
      read:
       - $SNAP/share/icons/Foo-icons
  sound-themes:
    interface: content
    content: sound-themes
    source:
      read:
       - $SNAP/share/sounds/Foo-sounds`)
	s.mockSnap(c, `name: snap2
version: 42
slots:
  gtk-3-themes:
    interface: content
    content: gtk-3-themes
    source:
      read:
       - $SNAP/share/themes/Bar-gtk
  icon-themes:
    interface: content
    content: icon-themes
    source:
      read:
       - $SNAP/share/icons/Bar-icons
  sound-themes:
    interface: content
    content: sound-themes
    source:
      read:
       - $SNAP/share/sounds/Bar-sounds`)
	s.mockSnap(c, `name: not-a-theme
version: 42
slots:
  foo:
    interface: content
    content: foo
    read: $SNAP/foo`)

	gtkThemes, iconThemes, soundThemes, err := getInstalledThemes(s.d)
	c.Check(err, IsNil)
	c.Check(gtkThemes, DeepEquals, []string{"Bar-gtk", "Foo-gtk", "Foo-gtk-dark"})
	c.Check(iconThemes, DeepEquals, []string{"Bar-icons", "Foo-icons"})
	c.Check(soundThemes, DeepEquals, []string{"Bar-sounds", "Foo-sounds"})
}

func (s *themesSuite) TestThemePackageCandidates(c *C) {
	// The package name includes the passed in prefix
	c.Check(themePackageCandidates("gtk-theme-", "Yaru"), DeepEquals, []string{"gtk-theme-yaru"})
	c.Check(themePackageCandidates("icon-theme-", "Yaru"), DeepEquals, []string{"icon-theme-yaru"})
	c.Check(themePackageCandidates("sound-theme-", "Yaru"), DeepEquals, []string{"sound-theme-yaru"})

	// If a theme name includes multiple dash separated
	// components, multiple possible package names are returned,
	// from most specific to least.
	c.Check(themePackageCandidates("gtk-theme-", "Yaru-dark"), DeepEquals, []string{"gtk-theme-yaru-dark", "gtk-theme-yaru"})
	c.Check(themePackageCandidates("gtk-theme-", "Matcha-dark-azul"), DeepEquals, []string{"gtk-theme-matcha-dark-azul", "gtk-theme-matcha-dark", "gtk-theme-matcha"})

	// In addition to case folding, bad characters are converted to dashes
	c.Check(themePackageCandidates("icon-theme-", "Breeze_Snow"), DeepEquals, []string{"icon-theme-breeze-snow", "icon-theme-breeze"})

	// Groups of bad characters are collapsed to a single dash,
	// with leading and trailing dashes removed
	c.Check(themePackageCandidates("gtk-theme-", "+foo_"), DeepEquals, []string{"gtk-theme-foo"})
	c.Check(themePackageCandidates("gtk-theme-", "foo-_--bar+-"), DeepEquals, []string{"gtk-theme-foo-bar", "gtk-theme-foo"})
}

func (s *themesSuite) TestGetThemeStatusForType(c *C) {
	s.daemon(c)

	s.available = map[string]*snap.Info{
		"gtk-theme-available": {
			SuggestedName: "gtk-theme-available",
			SideInfo: snap.SideInfo{
				Channel: "stable",
			},
		},
		"gtk-theme-installed": {
			SuggestedName: "gtk-theme-installed",
			SideInfo: snap.SideInfo{
				Channel: "stable",
			},
		},
	}

	ctx := context.Background()
	toInstall := make(map[string]bool)

	status, err := getThemeStatusForType(ctx, s, nil, "gtk-theme-", []string{"Installed", "Installed", "Available", "Unavailable"}, []string{"Installed"}, toInstall)
	c.Check(err, IsNil)
	c.Check(status, DeepEquals, map[string]themeStatus{
		"Installed":   themeInstalled,
		"Available":   themeAvailable,
		"Unavailable": themeUnavailable,
	})
	c.Check(toInstall, HasLen, 1)
	c.Check(toInstall["gtk-theme-available"], NotNil)
}

func (s *themesSuite) TestGetThemeStatusForTypeStripsSuffixes(c *C) {
	s.daemon(c)

	s.available = map[string]*snap.Info{
		"gtk-theme-yaru": {
			SuggestedName: "gtk-theme-yaru",
			SideInfo: snap.SideInfo{
				Channel: "stable",
			},
		},
	}

	ctx := context.Background()
	toInstall := make(map[string]bool)

	status, err := getThemeStatusForType(ctx, s, nil, "gtk-theme-", []string{"Yaru-dark"}, nil, toInstall)
	c.Check(err, IsNil)
	c.Check(status, DeepEquals, map[string]themeStatus{
		"Yaru-dark": themeAvailable,
	})
	c.Check(toInstall, HasLen, 1)
	c.Check(toInstall["gtk-theme-yaru"], NotNil)
}

func (s *themesSuite) TestGetThemeStatusForTypeIgnoresUnstable(c *C) {
	s.daemon(c)

	s.available = map[string]*snap.Info{
		"gtk-theme-yaru": {
			SuggestedName: "gtk-theme-yaru",
			SideInfo: snap.SideInfo{
				Channel: "edge",
			},
		},
	}

	ctx := context.Background()
	toInstall := make(map[string]bool)

	status, err := getThemeStatusForType(ctx, s, nil, "gtk-theme-", []string{"Yaru"}, nil, toInstall)
	c.Check(err, IsNil)
	c.Check(status, DeepEquals, map[string]themeStatus{
		"Yaru": themeUnavailable,
	})
	c.Check(toInstall, HasLen, 0)
}

func (s *themesSuite) TestGetThemeStatusForTypeReturnsErrors(c *C) {
	s.daemon(c)

	s.err = errors.New("store error")

	ctx := context.Background()
	toInstall := make(map[string]bool)

	status, err := getThemeStatusForType(ctx, s, nil, "gtk-theme-", []string{"Theme"}, nil, toInstall)
	c.Check(err, Equals, s.err)
	c.Check(status, IsNil)
	c.Check(toInstall, HasLen, 0)
}

func (s *themesSuite) TestGetThemeStatus(c *C) {
	s.daemon(c)
	s.mockSnap(c, `name: snap1
version: 42
slots:
  gtk-3-themes:
    interface: content
    content: gtk-3-themes
    source:
      read:
       - $SNAP/share/themes/Foo-gtk
  icon-themes:
    interface: content
    content: icon-themes
    source:
      read:
       - $SNAP/share/icons/Foo-icons
  sound-themes:
    interface: content
    content: sound-themes
    source:
      read:
       - $SNAP/share/sounds/Foo-sounds`)
	s.available = map[string]*snap.Info{
		"gtk-theme-bar": {
			SuggestedName: "gtk-theme-bar",
			SideInfo: snap.SideInfo{
				Channel: "stable",
			},
		},
		"icon-theme-bar": {
			SuggestedName: "icon-theme-bar",
			SideInfo: snap.SideInfo{
				Channel: "stable",
			},
		},
		"sound-theme-bar": {
			SuggestedName: "sound-theme-bar",
			SideInfo: snap.SideInfo{
				Channel: "stable",
			},
		},
	}

	ctx := context.Background()
	status, toInstall, err := getThemeStatus(ctx, themesCmd, nil, []string{"Foo-gtk", "Bar-gtk", "Baz-gtk"}, []string{"Foo-icons", "Bar-icons", "Baz-icons"}, []string{"Foo-sounds", "Bar-sounds", "Baz-sounds"})
	c.Check(err, IsNil)
	c.Check(status.GtkThemes, DeepEquals, map[string]themeStatus{
		"Foo-gtk": themeInstalled,
		"Bar-gtk": themeAvailable,
		"Baz-gtk": themeUnavailable,
	})
	c.Check(status.IconThemes, DeepEquals, map[string]themeStatus{
		"Foo-icons": themeInstalled,
		"Bar-icons": themeAvailable,
		"Baz-icons": themeUnavailable,
	})
	c.Check(status.SoundThemes, DeepEquals, map[string]themeStatus{
		"Foo-sounds": themeInstalled,
		"Bar-sounds": themeAvailable,
		"Baz-sounds": themeUnavailable,
	})
	c.Check(toInstall, DeepEquals, []string{"gtk-theme-bar", "icon-theme-bar", "sound-theme-bar"})
}

func (s *themesSuite) TestThemesCmd(c *C) {
	c.Check(themesCmd.GET, NotNil)
	c.Check(themesCmd.POST, NotNil)
	c.Check(themesCmd.PUT, IsNil)

	c.Check(themesCmd.Path, Equals, "/v2/themes")

	s.daemon(c)
	s.available = map[string]*snap.Info{
		"gtk-theme-foo": {
			SuggestedName: "gtk-theme-foo",
			SideInfo: snap.SideInfo{
				Channel: "stable",
			},
		},
		"icon-theme-foo": {
			SuggestedName: "icon-theme-foo",
			SideInfo: snap.SideInfo{
				Channel: "stable",
			},
		},
		"sound-theme-foo": {
			SuggestedName: "sound-theme-foo",
			SideInfo: snap.SideInfo{
				Channel: "stable",
			},
		},
	}

	req := httptest.NewRequest("GET", "/v2/themes?gtk-theme=Foo-gtk&gtk-theme=Bar&icon-theme=Foo-icons&sound-theme=Foo-sounds", nil)
	rec := httptest.NewRecorder()
	themesCmd.GET(themesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)

	var body map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	c.Assert(err, IsNil)
	c.Check(body, DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"gtk-themes": map[string]interface{}{
				"Foo-gtk": "available",
				"Bar":     "unavailable",
			},
			"icon-themes": map[string]interface{}{
				"Foo-icons": "available",
			},
			"sound-themes": map[string]interface{}{
				"Foo-sounds": "available",
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

func (s *themesSuite) daemonWithIfaceMgr(c *C) *Daemon {
	d := s.apiBaseSuite.daemonWithOverlordMock(c)

	st := d.overlord.State()
	runner := d.overlord.TaskRunner()
	hookMgr, err := hookstate.Manager(st, runner)
	c.Assert(err, IsNil)
	d.overlord.AddManager(hookMgr)
	ifaceMgr, err := ifacestate.Manager(st, hookMgr, runner, nil, nil)
	c.Assert(err, IsNil)
	d.overlord.AddManager(ifaceMgr)
	d.overlord.AddManager(runner)
	c.Assert(d.overlord.StartUp(), IsNil)

	st.Lock()
	defer st.Unlock()
	snapstate.ReplaceStore(st, s)
	return d
}

func (s *themesSuite) TestThemesCmdPost(c *C) {
	s.daemonWithIfaceMgr(c)

	s.available = map[string]*snap.Info{
		"gtk-theme-foo": {
			SuggestedName: "gtk-theme-foo",
			SideInfo: snap.SideInfo{
				Channel: "stable",
			},
		},
		"icon-theme-foo": {
			SuggestedName: "icon-theme-foo",
			SideInfo: snap.SideInfo{
				Channel: "stable",
			},
		},
		"sound-theme-foo": {
			SuggestedName: "sound-theme-foo",
			SideInfo: snap.SideInfo{
				Channel: "stable",
			},
		},
	}
	snapstateInstallMany = func(s *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
		t := s.NewTask("fake-theme-install", "Theme install")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	}

	buf := bytes.NewBufferString(`{"gtk-themes":["Foo-gtk"],"icon-themes":["Foo-icons"],"sound-themes":["Foo-sounds"]}`)
	req := httptest.NewRequest("POST", "/v2/themes", buf)
	rec := httptest.NewRecorder()
	themesCmd.POST(themesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 202)

	var rsp resp
	err := json.Unmarshal(rec.Body.Bytes(), &rsp)
	c.Assert(err, IsNil)
	c.Check(rsp.Type, Equals, ResponseTypeAsync)

	st := s.d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Check(chg.Kind(), Equals, "install-themes")
	c.Check(chg.Summary(), Equals, `Install snaps "gtk-theme-foo", "icon-theme-foo", "sound-theme-foo"`)
	var names []string
	err = chg.Get("snap-names", &names)
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"gtk-theme-foo", "icon-theme-foo", "sound-theme-foo"})
}
