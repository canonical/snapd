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

package userd

import (
	"github.com/snapcore/snapd/strutil"
	"testing"
)

var mockFileSystem = []string{
	"/var/lib/snapd/desktop/applications/mir-kiosk-scummvm_mir-kiosk-scummvm.desktop",
	"/var/lib/snapd/desktop/applications/multipass_multipass-gui.desktop",
	"/var/lib/snapd/desktop/applications/cevelop_cevelop.desktop",
	"/var/lib/snapd/desktop/applications/egmde-confined-desktop_egmde-confined-desktop.desktop",
	"/var/lib/snapd/desktop/applications/classic-snap-analyzer_classic-snap-analyzer.desktop",
	"/var/lib/snapd/desktop/applications/vlc_vlc.desktop",
	"/var/lib/snapd/desktop/applications/gnome-calculator_gnome-calculator.desktop",
	"/var/lib/snapd/desktop/applications/mir-kiosk-kodi_mir-kiosk-kodi.desktop",
	"/var/lib/snapd/desktop/applications/gnome-characters_gnome-characters.desktop",
	"/var/lib/snapd/desktop/applications/clion_clion.desktop",
	"/var/lib/snapd/desktop/applications/gnome-system-monitor_gnome-system-monitor.desktop",
	"/var/lib/snapd/desktop/applications/inkscape_inkscape.desktop",
	"/var/lib/snapd/desktop/applications/gnome-logs_gnome-logs.desktop",

	"/var/lib/snapd/desktop/applications/foo-bar/baz.desktop",
	"/var/lib/snapd/desktop/applications/baz/foo-bar.desktop",
}

func existsOnMockFileSystem(desktop_file string) bool {
	return strutil.ListContains(mockFileSystem, desktop_file)
}

func TestLauncher_desktopFileIdToFilenameSucceedsWithValidId(t *testing.T) {
	var desktopIdTests = []struct {
		id     string
		expect string
	}{
		{"mir-kiosk-scummvm_mir-kiosk-scummvm.desktop", "/var/lib/snapd/desktop/applications/mir-kiosk-scummvm_mir-kiosk-scummvm.desktop"},
		{"foo-bar-baz.desktop", "/var/lib/snapd/desktop/applications/foo-bar/baz.desktop"},
		{"baz-foo-bar.desktop", "/var/lib/snapd/desktop/applications/baz/foo-bar.desktop"},
	}

	for _, test := range desktopIdTests {
		actual, _ := desktopFileIdToFilename(existsOnMockFileSystem, test.id)
		if actual != test.expect {
			t.Errorf("desktopFileIdToFilename(%s): expected %s, actual %s", test.id, test.expect, actual)
		}
	}
}


func TestLauncher_desktopFileIdToFilenameFailsWithInvalidId(t *testing.T) {
	var desktopIdTests = []string{
		"mir-kiosk-scummvm-mir-kiosk-scummvm.desktop",
		"bar-foo-baz.desktop",
		"bar-baz-foo.desktop",
	}

	for _, id := range desktopIdTests {
		actual, err := desktopFileIdToFilename(existsOnMockFileSystem, id)
		if err == nil {
			t.Errorf("desktopFileIdToFilename(%s): expected <error>, actual %s", id, actual)
		}
	}
}
