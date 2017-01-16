// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package dbus implements interaction between snappy and dbus.
//
// Snappy creates dbus configuration files that describe how various
// services on the system bus can communicate with other peers.
//
// Each configuration is an XML file containing <busconfig>...</busconfig>.
// Particular security snippets define whole <policy>...</policy> entires.
// This is explained in detail in https://dbus.freedesktop.org/doc/dbus-daemon.1.html
package dbus

import (
	"bytes"
	"fmt"
	"os"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// Backend is responsible for maintaining DBus policy files.
type Backend struct{}

// Name returns the name of the backend.
func (b *Backend) Name() string {
	return "dbus"
}

// Setup creates dbus configuration files specific to a given snap.
//
// DBus has no concept of a complain mode so confinment type is ignored.
func (b *Backend) Setup(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
	// Get the snippets that apply to this snap
	snippets, err := repo.SecuritySnippetsForSnap(snapInfo.Name(), interfaces.SecurityDBus)
	if err != nil {
		return fmt.Errorf("cannot obtain DBus security snippets for snap %q: %s", snapInfo.Name(), err)
	}

	if err := b.setupBusconfig(snapInfo, snippets); err != nil {
		return err
	}
	if err := b.setupBusservices(snapInfo, snippets); err != nil {
		return err
	}

	return nil
}

func (b *Backend) setupBusconfig(snapInfo *snap.Info, snippets map[string][][]byte) error {
	snapName := snapInfo.Name()

	// Get the files that this snap should have
	content, err := b.combineSnippetsBusconfig(snapInfo, snippets)
	if err != nil {
		return fmt.Errorf("cannot obtain expected DBus configuration files for snap %q: %s", snapName, err)
	}
	glob := fmt.Sprintf("%s.conf", interfaces.SecurityTagGlob(snapName))
	dir := dirs.SnapBusPolicyDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for DBus configuration files %q: %s", dir, err)
	}
	_, _, err = osutil.EnsureDirState(dir, glob, content)
	if err != nil {
		return fmt.Errorf("cannot synchronize DBus configuration files for snap %q: %s", snapName, err)
	}
	return nil
}

// Remove removes dbus configuration files of a given snap.
//
// This method should be called after removing a snap.
func (b *Backend) Remove(snapName string) error {
	if err := b.removeBusconfig(snapName); err != nil {
		return err
	}
	if err := b.removeBusservices(snapName); err != nil {
		return err
	}
	return nil
}

func (b *Backend) removeBusconfig(snapName string) error {
	glob := fmt.Sprintf("%s.conf", interfaces.SecurityTagGlob(snapName))
	_, _, err := osutil.EnsureDirState(dirs.SnapBusPolicyDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize DBus configuration files for snap %q: %s", snapName, err)
	}
	return nil
}

// combineSnippetsBusconfig combines security snippets collected from
// all the interfaces affecting a given snap into a content map
// applicable to EnsureDirState.
func (b *Backend) combineSnippetsBusconfig(snapInfo *snap.Info, snippets map[string][][]byte) (content map[string]*osutil.FileState, err error) {
	for _, appInfo := range snapInfo.Apps {
		securityTag := appInfo.SecurityTag()
		appSnippets := snippets[securityTag]
		if len(appSnippets) == 0 {
			continue
		}
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}

		addContentBusconfig(securityTag, appSnippets, content)
	}

	for _, hookInfo := range snapInfo.Hooks {
		securityTag := hookInfo.SecurityTag()
		hookSnippets := snippets[securityTag]
		if len(hookSnippets) == 0 {
			continue
		}
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}

		addContentBusconfig(securityTag, hookSnippets, content)
	}

	return content, nil
}

func addContentBusconfig(securityTag string, executableSnippets [][]byte, content map[string]*osutil.FileState) {
	var buffer bytes.Buffer
	buffer.Write(xmlHeader)
	for _, snippet := range executableSnippets {
		buffer.Write(snippet)
		buffer.WriteRune('\n')
	}
	buffer.Write(xmlFooter)

	content[fmt.Sprintf("%s.conf", securityTag)] = &osutil.FileState{
		Content: buffer.Bytes(),
		Mode:    0644,
	}
}

func (b *Backend) setupBusservices(snapInfo *snap.Info, snippets map[string][][]byte) error {
	snapName := snapInfo.Name()

	content := map[string]*osutil.FileState{}
	for _, appInfo := range snapInfo.Apps {
		securityTag := appInfo.SecurityTag()

		// FIXME: layer violation, we really should do that in the
		//        dbus interface but without PR #2613 its really tricky
		for _, slot := range appInfo.Slots {
			if slot.Interface != "dbus" {
				continue
			}
			bus, ok := slot.Attrs["bus"].(string)
			if !ok {
				continue
			}
			if bus != "session" {
				continue
			}
			name, ok := slot.Attrs["name"].(string)
			if !ok {
				continue
			}
			isService := slot.Attrs["service"].(bool)
			if !isService {
				continue
			}

			var buffer bytes.Buffer
			buffer.Write([]byte(fmt.Sprintf(`[D-BUS Service]
Name=%s
Exec=%s
`, name, appInfo.LauncherCommand())))
			content[fmt.Sprintf("%s.service", securityTag)] = &osutil.FileState{
				Content: buffer.Bytes(),
				Mode:    0644,
			}
		}
	}

	glob := fmt.Sprintf("%s.service", interfaces.SecurityTagGlob(snapName))
	dir := dirs.SnapDBusSessionServicesFilesDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for DBus service files %q: %s", dir, err)
	}
	_, _, err := osutil.EnsureDirState(dir, glob, content)
	if err != nil {
		return fmt.Errorf("cannot synchronize DBus service files for snap %q: %s", snapName, err)
	}
	return nil
}

func (b *Backend) removeBusservices(snapName string) error {
	glob := fmt.Sprintf("%s.service", interfaces.SecurityTagGlob(snapName))
	_, _, err := osutil.EnsureDirState(dirs.SnapSessionDBusServicesFilesDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize DBus service files for snap %q: %s", snapName, err)
	}
	return nil
}
