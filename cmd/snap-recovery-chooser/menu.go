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
	"strings"
)

// Menu describes a menu with multiple entries
type Menu struct {
	Description string
	Header      string
	// Entries of the menu
	Entries []Entry
}

// Entry describes a menu entry
type Entry struct {
	// ID is a unique ID of given entry inside the whole menu (parent and submenus)
	ID string
	// Entry text
	Text string
	// Submenu if any
	Submenu *Menu `json:",omitempty"`
}

func fixupEntryID(e *Entry, parentID string) {
	if parentID != "" {
		prefix := parentID + "/"
		if !strings.HasPrefix(e.ID, prefix) {
			e.ID = prefix + e.ID
		}
	}
	if e.Submenu != nil {
		fixupSumbenuIDs(e.Submenu, e.ID)
	}
}

func fixupSumbenuIDs(m *Menu, parentID string) {
	for i := range m.Entries {
		fixupEntryID(&m.Entries[i], parentID)
	}
}

// ResolveMenu ensured that all menu entries have properly constructed IDs
func ResolveMenu(looseMenu *Menu) *Menu {
	fixupSumbenuIDs(looseMenu, "")
	return looseMenu
}
