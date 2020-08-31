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
	. "gopkg.in/check.v1"
)

func (s *apiSuite) TestGetInstalledThemes(c *C) {
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

func (s *apiSuite) TestThemePackageCandidates(c *C) {
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
