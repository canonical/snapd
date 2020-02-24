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

package main

import (
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

// demoUI outputs the skeleton of a demo UI based on the spec
func demoUI() *Menu {
	// TODO:UC20: talk to snapd to build the list of options

	prevVersionMenu := Menu{
		Description: "Start into a previous version:",
		Entries: []Entry{
			{
				ID:   "20190917",
				Text: "20190917 (last used 2019-09-17 21:07)",
			}, {
				ID:   "20190901",
				Text: "20190917 (never used)",
			},
		},
	}
	reinstallMenu := Menu{
		Header:      "! Reinstalling will start immediately and will delete all data on this device.",
		Description: "Reinstall the system using version:",
		Entries: []Entry{
			{
				ID:   "20190917",
				Text: "20190917 (last used 2019-09-17 21:07)",
			}, {
				ID:   "20190901",
				Text: "20190917 (never used)",
			},
		},
	}
	shellMenu := Menu{
		Description: "Enter shell using version",
		Entries: []Entry{
			{
				ID:   "20190917",
				Text: "20190917 (last used 2019-09-17 21:07)",
			}, {
				ID:   "20190901",
				Text: "20190917 (never used)",
			},
		},
	}
	menu := Menu{
		Header:      "Ubuntu Core for YoyoDyne Retro Encabulator 550",
		Description: "Use arrow/number keys then Enter, or volume buttons then power button",
		Entries: []Entry{
			{
				ID:   "normal-start",
				Text: "Start normally",
			}, {
				ID:      "prev-version",
				Text:    "Start into a previous version",
				Submenu: &prevVersionMenu,
			}, {
				ID:   "self-test",
				Text: "Self-test",
			}, {
				ID:      "enter-shell",
				Text:    "Enter shell",
				Submenu: &shellMenu,
			}, {
				ID:      "reinstall",
				Text:    "Reinstall",
				Submenu: &reinstallMenu,
			},
		},
	}
	return ResolveMenu(&menu)
}

// demoUITool returns a hardcoded path to the demo tool
func demoUITool() (string, error) {
	return filepath.Join(dirs.DistroLibExecDir, "snap-chooser-ui-demo"), nil
}
