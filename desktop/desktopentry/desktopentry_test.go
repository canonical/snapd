// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package desktopentry_test

import (
	"bytes"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/desktop/desktopentry"
)

func Test(t *testing.T) { TestingT(t) }

type desktopentrySuite struct{}

var _ = Suite(&desktopentrySuite{})

const browserDesktopEntry = `
[Desktop Entry]
Version=1.0
Type=Application
Name=Web Browser
Exec=browser %u
Icon=${SNAP}/default256.png
Actions=NewWindow;NewPrivateWindow;

# A comment
[Desktop Action NewWindow]
Name=Open a New Window
Exec=browser -new-window

[Desktop Action NewPrivateWindow]
Name=Open a New Private Window
Exec=browser -private-window
Icon=${SNAP}/private.png
`

func (s *desktopentrySuite) TestParse(c *C) {
	r := bytes.NewBufferString(browserDesktopEntry)
	de, err := desktopentry.Parse("/path/browser.desktop", r)
	c.Assert(err, IsNil)

	c.Check(de.Name, Equals, "Web Browser")
	c.Check(de.Icon, Equals, "${SNAP}/default256.png")
	c.Check(de.Exec, Equals, "browser %u")
	c.Check(de.Actions, HasLen, 2)

	c.Assert(de.Actions["NewWindow"], NotNil)
	c.Check(de.Actions["NewWindow"].Name, Equals, "Open a New Window")
	c.Check(de.Actions["NewWindow"].Icon, Equals, "")
	c.Check(de.Actions["NewWindow"].Exec, Equals, "browser -new-window")

	c.Assert(de.Actions["NewPrivateWindow"], NotNil)
	c.Check(de.Actions["NewPrivateWindow"].Name, Equals, "Open a New Private Window")
	c.Check(de.Actions["NewPrivateWindow"].Icon, Equals, "${SNAP}/private.png")
	c.Check(de.Actions["NewPrivateWindow"].Exec, Equals, "browser -private-window")
}

func (s *desktopentrySuite) TestParseBad(c *C) {
	for i, tc := range []struct {
		in  string
		err string
	}{{
		in: `
[Desktop Entry]
[Desktop Entry]
`,
		err: `desktop file "/path/foo.desktop" has multiple \[Desktop Entry\] groups`,
	}, {
		in: `
[Desktop Entry]
Actions=known;
[Desktop Action known]
[Desktop Action unknown]
`,
		err: `desktop file "/path/foo.desktop" contains unknown action "unknown"`,
	}, {
		in: `
[Desktop Entry]
Actions=known;
[Desktop Action known]
[Desktop Action known]
`,
		err: `desktop file "/path/foo.desktop" has multiple "\[Desktop Action known\]" groups`,
	}, {
		in: `
[Desktop Entry]
NoEqualsSign
`,
		err: `desktop file "/path/foo.desktop" badly formed`,
	}} {
		c.Logf("tc %d", i)
		r := bytes.NewBufferString(tc.in)
		de, err := desktopentry.Parse("/path/foo.desktop", r)
		c.Check(de, IsNil)
		c.Check(err, ErrorMatches, tc.err)
	}
}

func (s *desktopentrySuite) TestShouldAutostart(c *C) {
	allGood := `[Desktop Entry]
Exec=foo --bar
`
	hidden := `[Desktop Entry]
Exec=foo --bar
Hidden=true
`
	hiddenFalse := `[Desktop Entry]
Exec=foo --bar
Hidden=false
`
	justGNOME := `[Desktop Entry]
Exec=foo --bar
OnlyShowIn=GNOME;
`
	notInGNOME := `[Desktop Entry]
Exec=foo --bar
NotShownIn=GNOME;
`
	notInGNOMEAndKDE := `[Desktop Entry]
Exec=foo --bar
NotShownIn=GNOME;KDE;
`
	hiddenGNOMEextension := `[Desktop Entry]
Exec=foo --bar
X-GNOME-Autostart-enabled=false
`
	GNOMEextension := `[Desktop Entry]
Exec=foo --bar
X-GNOME-Autostart-enabled=true
`

	for i, tc := range []struct {
		in        string
		current   string
		autostart bool
	}{{
		in:        allGood,
		autostart: true,
	}, {
		in:        hidden,
		autostart: false,
	}, {
		in:        hiddenFalse,
		autostart: true,
	}, {
		in:        justGNOME,
		current:   "GNOME",
		autostart: true,
	}, {
		in:        justGNOME,
		current:   "ubuntu:GNOME",
		autostart: true,
	}, {
		in:        justGNOME,
		current:   "KDE",
		autostart: false,
	}, {
		in:        notInGNOME,
		current:   "GNOME",
		autostart: false,
	}, {
		in:        notInGNOME,
		current:   "ubuntu:GNOME",
		autostart: false,
	}, {
		in:        notInGNOME,
		current:   "KDE",
		autostart: true,
	}, {
		in:        notInGNOMEAndKDE,
		current:   "XFCE",
		autostart: true,
	}, {
		in:        notInGNOMEAndKDE,
		current:   "ubuntu:GNOME",
		autostart: false,
	}, {
		in:        hiddenGNOMEextension,
		current:   "GNOME",
		autostart: false,
	}, {
		in:        hiddenGNOMEextension,
		current:   "KDE",
		autostart: true,
	}, {
		in:        GNOMEextension,
		current:   "GNOME",
		autostart: true,
	}, {
		in:        GNOMEextension,
		current:   "KDE",
		autostart: true,
	}} {
		c.Logf("tc %d", i)
		r := bytes.NewBufferString(tc.in)
		de, err := desktopentry.Parse("/path/foo.desktop", r)
		c.Check(err, IsNil)
		currentDesktop := strings.Split(tc.current, ":")
		c.Check(de.ShouldAutostart(currentDesktop), Equals, tc.autostart)
	}
}

func (s *desktopentrySuite) TestExpandExec(c *C) {
	r := bytes.NewBufferString(browserDesktopEntry)
	de, err := desktopentry.Parse("/path/browser.desktop", r)
	c.Assert(err, IsNil)

	args, err := de.ExpandExec([]string{"http://example.org"})
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, []string{"browser", "http://example.org"})

	// When called with no URIs, the %U code expands to nothing
	args, err = de.ExpandExec(nil)
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, []string{"browser"})

	// If the Exec line is missing, an error is returned
	de.Exec = ""
	_, err = de.ExpandExec(nil)
	c.Check(err, ErrorMatches, `desktop file "/path/browser.desktop" has no Exec line`)
}

func (s *desktopentrySuite) TestExpandActionExec(c *C) {
	r := bytes.NewBufferString(browserDesktopEntry)
	de, err := desktopentry.Parse("/path/browser.desktop", r)
	c.Assert(err, IsNil)

	args, err := de.ExpandActionExec("NewWindow", nil)
	c.Assert(err, IsNil)
	c.Check(args, DeepEquals, []string{"browser", "-new-window"})

	// Expanding a non-existent action, an error is returned
	_, err = de.ExpandActionExec("UnknownAction", nil)
	c.Check(err, ErrorMatches, `desktop file "/path/browser.desktop" does not have action "UnknownAction"`)

	// If the action is missing its Exec line, an error is returned
	de.Actions["NewWindow"].Exec = ""
	_, err = de.ExpandActionExec("NewWindow", nil)
	c.Check(err, ErrorMatches, `desktop file "/path/browser.desktop" action "NewWindow" has no Exec line`)
}
