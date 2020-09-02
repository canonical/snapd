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
	"context"
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
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

func (s *themesSuite) daemon(c *C) {
	s.apiBaseSuite.daemon(c)

	st := s.d.overlord.State()
	st.Lock()
	defer st.Unlock()
	snapstate.ReplaceStore(st, s)
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

func (s *themesSuite) TestCheckThemeStatusForType(c *C) {
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
	toInstall := make(map[string]*snap.Info)

	status, err := checkThemeStatusForType(ctx, s, nil, "gtk-theme-", []string{"Installed", "Installed", "Available", "Unavailable"}, []string{"Installed"}, toInstall)
	c.Check(err, IsNil)
	c.Check(status, DeepEquals, map[string]themeStatus{
		"Installed":   themeInstalled,
		"Available":   themeAvailable,
		"Unavailable": themeUnavailable,
	})
	c.Check(toInstall, HasLen, 1)
	c.Check(toInstall["gtk-theme-available"], NotNil)
}

func (s *themesSuite) TestCheckThemeStatusForTypeStripsSuffixes(c *C) {
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
	toInstall := make(map[string]*snap.Info)

	status, err := checkThemeStatusForType(ctx, s, nil, "gtk-theme-", []string{"Yaru-dark"}, nil, toInstall)
	c.Check(err, IsNil)
	c.Check(status, DeepEquals, map[string]themeStatus{
		"Yaru-dark": themeAvailable,
	})
	c.Check(toInstall, HasLen, 1)
	c.Check(toInstall["gtk-theme-yaru"], NotNil)
}

func (s *themesSuite) TestCheckThemeStatusForTypeReturnsErrors(c *C) {
	s.daemon(c)

	s.err = errors.New("store error")

	ctx := context.Background()
	toInstall := make(map[string]*snap.Info)

	status, err := checkThemeStatusForType(ctx, s, nil, "gtk-theme-", []string{"Theme"}, nil, toInstall)
	c.Check(err, Equals, s.err)
	c.Check(status, IsNil)
	c.Check(toInstall, HasLen, 0)
}
