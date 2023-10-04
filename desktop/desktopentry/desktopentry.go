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

package desktopentry

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/snapcore/snapd/strutil"
)

// DesktopEntry represents a freedesktop.org desktop entry file.
//
// The various fields are as defined in the specification:
// https://specifications.freedesktop.org/desktop-entry-spec/desktop-entry-spec-latest.html
type DesktopEntry struct {
	Filename string
	Name     string
	Icon     string
	Exec     string

	Hidden                bool
	OnlyShowIn            []string
	NotShownIn            []string
	GnomeAutostartEnabled bool

	Actions map[string]*Action
}

// Action represents an application action defined in a desktop entry file.
type Action struct {
	Name string
	Icon string
	Exec string
}

type groupState int

const (
	unknownGroup groupState = iota
	desktopEntryGroup
	desktopActionGroup
)

func splitList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ';' })
}

// Read parses a desktop entry file.
func Read(filename string) (*DesktopEntry, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parse(filename, f)
}

func parse(filename string, r io.Reader) (*DesktopEntry, error) {
	de := &DesktopEntry{
		Filename: filename,
		// If X-GNOME-Autostart-Enabled is not present, it is
		// treated as if it is set to true:
		// https://gitlab.gnome.org/GNOME/gnome-session/-/blob/543881614a6e8333d1f39108edd8eb6218cec619/gnome-session/gsm-autostart-app.c#L116-129
		GnomeAutostartEnabled: true,
	}
	var (
		currentGroup          = unknownGroup
		seenDesktopEntryGroup = false
		actions               []string
		currentAction         *Action
	)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Ignore empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Start of a new group
		if strings.HasPrefix(line, "[") {
			if line == "[Desktop Entry]" {
				if seenDesktopEntryGroup {
					return nil, fmt.Errorf("desktop file %q has multiple [Desktop Entry] groups", filename)
				}
				seenDesktopEntryGroup = true
				currentGroup = desktopEntryGroup
			} else if strings.HasPrefix(line, "[Desktop Action ") && strings.HasSuffix(line, "]") {
				action := line[len("[Desktop Action ") : len(line)-1]
				if !strutil.ListContains(actions, action) {
					return nil, fmt.Errorf("desktop file %q contains unknown action %q", filename, action)
				}
				if de.Actions[action] != nil {
					return nil, fmt.Errorf("desktop file %q has multiple %q groups", filename, line)
				}
				currentGroup = desktopActionGroup
				if de.Actions == nil {
					de.Actions = make(map[string]*Action, len(actions))
				}
				currentAction = &Action{}
				de.Actions[action] = currentAction
			} else {
				// Ignore other groups
				currentGroup = unknownGroup
			}
			continue
		}

		split := strings.SplitN(line, "=", 2)
		if len(split) != 2 {
			return nil, fmt.Errorf("desktop file %q badly formed in line %q", filename, line)
		}
		// Trim whitespace around the equals sign
		key := strings.TrimRight(split[0], "\t\v\f\r ")
		value := strings.TrimLeft(split[1], "\t\v\f\r ")
		switch currentGroup {
		case unknownGroup:
			// Ignore keys in unknown groups
		case desktopEntryGroup:
			switch key {
			case "Name":
				de.Name = value
			case "Icon":
				de.Icon = value
			case "Exec":
				de.Exec = value
			case "Hidden":
				de.Hidden = value == "true"
			case "OnlyShowIn":
				de.OnlyShowIn = splitList(value)
			case "NotShownIn":
				de.NotShownIn = splitList(value)
			case "X-GNOME-Autostart-enabled":
				de.GnomeAutostartEnabled = value == "true"
			case "Actions":
				actions = splitList(value)
			default:
				// Ignore all other keys
			}
		case desktopActionGroup:
			switch key {
			case "Name":
				currentAction.Name = value
			case "Icon":
				currentAction.Icon = value
			case "Exec":
				currentAction.Exec = value
			default:
				// Ignore all other keys
			}
		}
	}
	return de, nil
}

func isOneOfIn(of []string, other []string) bool {
	for _, one := range of {
		if strutil.ListContains(other, one) {
			return true
		}
	}
	return false
}

// ShouldAutostart returns true if this desktop file should autostart
// on the given desktop.
//
// currentDesktop is the value of $XDG_CURRENT_DESKTOP split on colon
// characters.
func (de *DesktopEntry) ShouldAutostart(currentDesktop []string) bool {
	// See https://standards.freedesktop.org/autostart-spec/autostart-spec-latest.html
	// for details on how Hidden, OnlyShowIn, NotShownIn are handled.

	if de.Hidden {
		return false
	}
	if !de.GnomeAutostartEnabled {
		// GNOME specific extension, see gnome-session:
		// https://github.com/GNOME/gnome-session/blob/c449df5269e02c59ae83021a3110ec1b338a2bba/gnome-session/gsm-autostart-app.c#L110..L145
		if strutil.ListContains(currentDesktop, "GNOME") {
			return false
		}
	}
	if de.OnlyShowIn != nil {
		if !isOneOfIn(currentDesktop, de.OnlyShowIn) {
			return false
		}
	}
	if de.NotShownIn != nil {
		if isOneOfIn(currentDesktop, de.NotShownIn) {
			return false
		}
	}
	return true
}

// ExpandExec returns the command line used to launch this desktop entry.
//
// Macros will be expanded, with the %f, %F, %u, and %U macros using
// the provided list of URIs
func (de *DesktopEntry) ExpandExec(uris []string) ([]string, error) {
	if de.Exec == "" {
		return nil, fmt.Errorf("desktop file %q has no Exec line", de.Filename)
	}
	return expandExec(de, de.Exec, uris)
}

// ExpandActionExec returns the command line used to launch the named
// action of the desktop entry.
func (de *DesktopEntry) ExpandActionExec(action string, uris []string) ([]string, error) {
	if de.Actions[action] == nil {
		return nil, fmt.Errorf("desktop file %q does not have action %q", de.Filename, action)
	}
	if de.Actions[action].Exec == "" {
		return nil, fmt.Errorf("desktop file %q action %q has no Exec line", de.Filename, action)
	}
	return expandExec(de, de.Actions[action].Exec, uris)
}
