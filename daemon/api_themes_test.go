// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020-2024 Canonical Ltd
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

package daemon_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http/httptest"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/store"
)

var _ = Suite(&themesSuite{})

type themesSuite struct {
	apiBaseSuite

	available map[string]*snap.Info
}

func (s *themesSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)

	s.available = make(map[string]*snap.Info)
	s.err = store.ErrSnapNotFound
}

func (s *themesSuite) SnapExists(ctx context.Context, spec store.SnapSpec, user *auth.UserState) (naming.SnapRef, *channel.Channel, error) {
	s.pokeStateLock()
	if info := s.available[spec.Name]; info != nil {
		ch, err := channel.Parse(info.Channel, "")
		if err != nil {
			panic(fmt.Sprintf("bad Info Channel: %v", err))
		}
		return info, &ch, nil
	}
	return nil, nil, s.err
}

func (s *themesSuite) daemon(c *C) *daemon.Daemon {
	return s.apiBaseSuite.daemonWithStore(c, s)
}

func (s *themesSuite) expectThemesAccess() {
	s.expectReadAccess(daemon.InterfaceOpenAccess{Interfaces: []string{"snap-themes-control"}})
	s.expectWriteAccess(daemon.InterfaceAuthenticatedAccess{Interfaces: []string{"snap-themes-control"}, Polkit: "io.snapcraft.snapd.manage"})
}

func (s *themesSuite) TestInstalledThemes(c *C) {
	d := s.daemon(c)
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

	gtkThemes, iconThemes, soundThemes, err := daemon.InstalledThemes(d.Overlord())
	c.Check(err, IsNil)
	c.Check(gtkThemes, DeepEquals, []string{"Bar-gtk", "Foo-gtk", "Foo-gtk-dark"})
	c.Check(iconThemes, DeepEquals, []string{"Bar-icons", "Foo-icons"})
	c.Check(soundThemes, DeepEquals, []string{"Bar-sounds", "Foo-sounds"})
}

func (s *themesSuite) TestThemePackageCandidates(c *C) {
	// The package name includes the passed in prefix
	c.Check(daemon.ThemePackageCandidates("gtk-theme-", "Yaru"), DeepEquals, []string{"gtk-theme-yaru"})
	c.Check(daemon.ThemePackageCandidates("icon-theme-", "Yaru"), DeepEquals, []string{"icon-theme-yaru"})
	c.Check(daemon.ThemePackageCandidates("sound-theme-", "Yaru"), DeepEquals, []string{"sound-theme-yaru"})

	// If a theme name includes multiple dash separated
	// components, multiple possible package names are returned,
	// from most specific to least.
	c.Check(daemon.ThemePackageCandidates("gtk-theme-", "Yaru-dark"), DeepEquals, []string{"gtk-theme-yaru-dark", "gtk-theme-yaru"})
	c.Check(daemon.ThemePackageCandidates("gtk-theme-", "Matcha-dark-azul"), DeepEquals, []string{"gtk-theme-matcha-dark-azul", "gtk-theme-matcha-dark", "gtk-theme-matcha"})

	// Digits are accepted in package names
	c.Check(daemon.ThemePackageCandidates("gtk-theme-", "abc123xyz"), DeepEquals, []string{"gtk-theme-abc123xyz"})

	// In addition to case folding, bad characters are converted to dashes
	c.Check(daemon.ThemePackageCandidates("icon-theme-", "Breeze_Snow"), DeepEquals, []string{"icon-theme-breeze-snow", "icon-theme-breeze"})

	// Groups of bad characters are collapsed to a single dash,
	// with leading and trailing dashes removed
	c.Check(daemon.ThemePackageCandidates("gtk-theme-", "+foo_"), DeepEquals, []string{"gtk-theme-foo"})
	c.Check(daemon.ThemePackageCandidates("gtk-theme-", "foo-_--bar+-"), DeepEquals, []string{"gtk-theme-foo-bar", "gtk-theme-foo"})
}

func (s *themesSuite) TestThemeStatusForPrefix(c *C) {
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
	status := make(map[string]daemon.ThemeStatus)
	toInstall := make(map[string]bool)

	err := daemon.CollectThemeStatusForPrefix(ctx, s, nil, "gtk-theme-", []string{"Installed", "Installed", "Available", "Unavailable"}, []string{"Installed"}, status, toInstall)
	c.Check(err, IsNil)
	c.Check(status, DeepEquals, map[string]daemon.ThemeStatus{
		"Installed":   daemon.ThemeInstalled,
		"Available":   daemon.ThemeAvailable,
		"Unavailable": daemon.ThemeUnavailable,
	})
	c.Check(toInstall, HasLen, 1)
	c.Check(toInstall["gtk-theme-available"], NotNil)
}

func (s *themesSuite) TestThemeStatusForPrefixStripsSuffixes(c *C) {
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
	status := make(map[string]daemon.ThemeStatus)
	toInstall := make(map[string]bool)

	err := daemon.CollectThemeStatusForPrefix(ctx, s, nil, "gtk-theme-", []string{"Yaru-dark"}, nil, status, toInstall)
	c.Check(err, IsNil)
	c.Check(status, DeepEquals, map[string]daemon.ThemeStatus{
		"Yaru-dark": daemon.ThemeAvailable,
	})
	c.Check(toInstall, HasLen, 1)
	c.Check(toInstall["gtk-theme-yaru"], NotNil)
}

func (s *themesSuite) TestThemeStatusForPrefixIgnoresUnstable(c *C) {
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
	status := make(map[string]daemon.ThemeStatus)
	toInstall := make(map[string]bool)

	err := daemon.CollectThemeStatusForPrefix(ctx, s, nil, "gtk-theme-", []string{"Yaru"}, nil, status, toInstall)
	c.Check(err, IsNil)
	c.Check(status, DeepEquals, map[string]daemon.ThemeStatus{
		"Yaru": daemon.ThemeUnavailable,
	})
	c.Check(toInstall, HasLen, 0)
}

func (s *themesSuite) TestThemeStatusForPrefixReturnsErrors(c *C) {
	s.daemon(c)

	s.err = errors.New("store error")

	ctx := context.Background()
	status := make(map[string]daemon.ThemeStatus)
	toInstall := make(map[string]bool)

	err := daemon.CollectThemeStatusForPrefix(ctx, s, nil, "gtk-theme-", []string{"Theme"}, nil, status, toInstall)
	c.Check(err, Equals, s.err)
	c.Check(status, DeepEquals, map[string]daemon.ThemeStatus{
		"Theme": daemon.ThemeUnavailable,
	})
	c.Check(toInstall, HasLen, 0)
}

func (s *themesSuite) TestThemeStatusAndCandidateSnaps(c *C) {
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
	status, candidateSnaps, err := daemon.ThemeStatusAndCandidateSnaps(ctx, s.d, nil, []string{"Foo-gtk", "Bar-gtk", "Baz-gtk"}, []string{"Foo-icons", "Bar-icons", "Baz-icons"}, []string{"Foo-sounds", "Bar-sounds", "Baz-sounds"})
	c.Check(err, IsNil)
	c.Check(status.GtkThemes, DeepEquals, map[string]daemon.ThemeStatus{
		"Foo-gtk": daemon.ThemeInstalled,
		"Bar-gtk": daemon.ThemeAvailable,
		"Baz-gtk": daemon.ThemeUnavailable,
	})
	c.Check(status.IconThemes, DeepEquals, map[string]daemon.ThemeStatus{
		"Foo-icons": daemon.ThemeInstalled,
		"Bar-icons": daemon.ThemeAvailable,
		"Baz-icons": daemon.ThemeUnavailable,
	})
	c.Check(status.SoundThemes, DeepEquals, map[string]daemon.ThemeStatus{
		"Foo-sounds": daemon.ThemeInstalled,
		"Bar-sounds": daemon.ThemeAvailable,
		"Baz-sounds": daemon.ThemeUnavailable,
	})
	c.Check(candidateSnaps, DeepEquals, map[string]bool{
		"gtk-theme-bar":   true,
		"icon-theme-bar":  true,
		"sound-theme-bar": true,
	})
}

func (s *themesSuite) TestThemesCmdGet(c *C) {
	s.expectThemesAccess()
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

	req := httptest.NewRequest("GET", "/v2/accessories/themes?gtk-theme=Foo-gtk&gtk-theme=Bar&icon-theme=Foo-icons&sound-theme=Foo-sounds", nil)
	rsp := s.syncReq(c, req, nil)

	c.Check(rsp.Type, Equals, daemon.ResponseTypeSync)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Result, DeepEquals, daemon.ThemeStatusResponse{
		GtkThemes: map[string]daemon.ThemeStatus{
			"Foo-gtk": daemon.ThemeAvailable,
			"Bar":     daemon.ThemeUnavailable,
		},
		IconThemes: map[string]daemon.ThemeStatus{
			"Foo-icons": daemon.ThemeAvailable,
		},
		SoundThemes: map[string]daemon.ThemeStatus{
			"Foo-sounds": daemon.ThemeAvailable,
		},
	})
}

func (s *themesSuite) daemonWithIfaceMgr(c *C) *daemon.Daemon {
	d := s.apiBaseSuite.daemonWithOverlordMock()

	overlord := d.Overlord()
	st := overlord.State()
	runner := overlord.TaskRunner()
	hookMgr, err := hookstate.Manager(st, runner)
	c.Assert(err, IsNil)
	overlord.AddManager(hookMgr)
	ifaceMgr, err := ifacestate.Manager(st, hookMgr, runner, nil, nil)
	c.Assert(err, IsNil)
	overlord.AddManager(ifaceMgr)
	overlord.AddManager(runner)
	c.Assert(overlord.StartUp(), IsNil)

	st.Lock()
	defer st.Unlock()
	snapstate.ReplaceStore(st, s)
	return d
}

func (s *themesSuite) TestThemesCmdPost(c *C) {
	s.expectThemesAccess()
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
	restore := daemon.MockSnapstateInstallWithGoal(func(ctx context.Context, st *state.State, g snapstate.InstallGoal, opts snapstate.Options) ([]*snap.Info, []*state.TaskSet, error) {
		goal, ok := g.(*storeInstallGoalRecorder)
		c.Assert(ok, Equals, true, Commentf("unexpected InstallGoal type %T", g))
		c.Assert(goal.snaps, HasLen, 3)

		t := st.NewTask("fake-theme-install", "Theme install")
		return storeSnapInfos(goal.snaps), []*state.TaskSet{state.NewTaskSet(t)}, nil
	})
	defer restore()

	buf := bytes.NewBufferString(`{"gtk-themes":["Foo-gtk"],"icon-themes":["Foo-icons"],"sound-themes":["Foo-sounds"]}`)
	req := httptest.NewRequest("POST", "/v2/accessories/themes", buf)
	rsp := s.asyncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 202)

	st := s.d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Check(chg.Kind(), Equals, "install-themes")
	c.Check(chg.Summary(), Equals, `Install snaps "gtk-theme-foo", "icon-theme-foo", "sound-theme-foo"`)
	var names []string
	err := chg.Get("snap-names", &names)
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"gtk-theme-foo", "icon-theme-foo", "sound-theme-foo"})
}
