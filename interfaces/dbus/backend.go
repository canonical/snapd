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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// Backend is responsible for maintaining DBus policy files.
type Backend struct{}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return "dbus"
}

// setupHostDBusSessionConf will ensure that we have a dbus configuration
// that points to /var/lib/snapd/dbus/services. This is needed for systems
// that re-exec snapd and do not have this configuration as part of the
// packaged snapd.
func setupHostDBusSessionConf() error {
	dbusSessionConf := filepath.Join(dirs.GlobalRootDir, "/usr/share/dbus-1/session.d/snapd.conf")
	if osutil.FileExists(dbusSessionConf) {
		return nil
	}

	// using a different filename to ensure we not get into dpkg conffile
	// prompt hell
	reexecDbusSessionConf := filepath.Join(dirs.GlobalRootDir, "/etc/dbus-1/session.d/snapd.conf")
	sessionBusConfig := []byte(`<busconfig>
 <servicedir>/var/lib/snapd/dbus/services/</servicedir>
</busconfig>
`)

	content := map[string]*osutil.FileState{
		filepath.Base(reexecDbusSessionConf): &osutil.FileState{
			Content: sessionBusConfig,
			Mode:    0644,
		},
	}
	_, _, err := osutil.EnsureDirState(filepath.Dir(reexecDbusSessionConf), filepath.Base(reexecDbusSessionConf), content)
	return err
}

// Setup creates dbus configuration files specific to a given snap.
//
// DBus has no concept of a complain mode so confinment type is ignored.
func (b *Backend) Setup(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
	snapName := snapInfo.Name()
	// Get the snippets that apply to this snap
	spec, err := repo.SnapSpecification(b.Name(), snapName)
	if err != nil {
		return fmt.Errorf("cannot obtain dbus specification for snap %q: %s", snapName, err)
	}

	// ensure we have a *host* /etc/dbus/snapd.conf configuration
	if err := setupHostDBusSessionConf(); err != nil {
		logger.Noticef("cannot create host dbus session config: %s", err)
	}
	if err := b.setupBusConf(snapInfo, spec); err != nil {
		return err
	}
	if err := b.setupBusActivatedSessionServ(snapInfo, spec); err != nil {
		return err
	}

	return nil
}

func (b *Backend) setupBusConf(snapInfo *snap.Info, spec interfaces.Specification) error {
	snapName := snapInfo.Name()

	// Get the files that this snap should have
	content, err := b.deriveContentBusConf(spec.(*Specification), snapInfo)
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

func (b *Backend) deriveContentSessionServ(spec *Specification, snapInfo *snap.Info) (content map[string]*osutil.FileState, err error) {
	content = map[string]*osutil.FileState{}
	for securityTag, service := range spec.SessionServices() {
		fname := fmt.Sprintf("%s.service", securityTag)
		content[fname] = &osutil.FileState{
			Content: []byte(service.Content),
			Mode:    0644,
		}
	}

	return content, nil
}

// check for global conflicts over the dbus name
func checkSessionServiceDbusNameConflicts(snapInfo *snap.Info, service *SessionService) error {
	glob := filepath.Join(dirs.SnapDBusSessionServicesFilesDir, "*.service")
	matches, err := filepath.Glob(glob)
	if err != nil {
		return err
	}

	needle := fmt.Sprintf("\nName=%s\n", service.DbusName)
	self := interfaces.SecurityTagGlob(snapInfo.Name())
	for _, match := range matches {
		matched, err := filepath.Match(self, filepath.Base(match))
		if err != nil {
			return fmt.Errorf("internal error, cannot match session dbus names: %s", err)
		}
		if matched {
			continue
		}

		content, err := ioutil.ReadFile(match)
		if err != nil {
			return fmt.Errorf("cannot check for session dbus name conflicts: %s", err)
		}
		if strings.Contains(string(content), needle) {
			return fmt.Errorf("cannot add session dbus name %q, already taken by: %q", service.DbusName, match)
		}
	}

	return nil
}

func (b *Backend) setupBusActivatedSessionServ(snapInfo *snap.Info, spec interfaces.Specification) error {
	snapName := snapInfo.Name()
	realSpec := spec.(*Specification)

	// check conflicts
	for _, service := range realSpec.SessionServices() {
		if err := checkSessionServiceDbusNameConflicts(snapInfo, service); err != nil {
			return err
		}
	}

	content, err := b.deriveContentSessionServ(realSpec, snapInfo)
	if err != nil {
		return err
	}

	glob := fmt.Sprintf("%s.service", interfaces.SecurityTagGlob(snapName))
	dir := dirs.SnapDBusSessionServicesFilesDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for DBus service files: %s", err)
	}
	_, _, err = osutil.EnsureDirState(dir, glob, content)
	if err != nil {
		return fmt.Errorf("cannot synchronize DBus service files for snap %q: %s", snapName, err)
	}
	return nil
}

// Remove removes dbus configuration files of a given snap.
//
// This method should be called after removing a snap.
func (b *Backend) Remove(snapName string) error {
	if err := b.removeBusConf(snapName); err != nil {
		return err
	}
	if err := b.removeBusActivatedSessionServ(snapName); err != nil {
		return err
	}

	return nil
}

// Remove the DBus busconfig policy for the snap
func (b *Backend) removeBusConf(snapName string) error {
	glob := fmt.Sprintf("%s.conf", interfaces.SecurityTagGlob(snapName))
	_, _, err := osutil.EnsureDirState(dirs.SnapBusPolicyDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize DBus configuration files for snap %q: %s", snapName, err)
	}
	return nil
}

func (b *Backend) removeBusActivatedSessionServ(snapName string) error {
	glob := fmt.Sprintf("%s.service", interfaces.SecurityTagGlob(snapName))
	_, _, err := osutil.EnsureDirState(dirs.SnapBusPolicyDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize DBus service files for snap %q: %s", snapName, err)
	}
	return nil
}

// deriveContentBusConf combines security snippets for busconfig
// policy collected from all the interfaces affecting a given snap into a
// content map applicable to EnsureDirState.
func (b *Backend) deriveContentBusConf(spec *Specification, snapInfo *snap.Info) (content map[string]*osutil.FileState, err error) {
	for _, appInfo := range snapInfo.Apps {
		securityTag := appInfo.SecurityTag()
		appSnippets := spec.SnippetForTag(securityTag)
		if appSnippets == "" {
			continue
		}
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}

		addContent(securityTag, appSnippets, content)
	}

	for _, hookInfo := range snapInfo.Hooks {
		securityTag := hookInfo.SecurityTag()
		hookSnippets := spec.SnippetForTag(securityTag)
		if hookSnippets == "" {
			continue
		}
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}

		addContent(securityTag, hookSnippets, content)
	}

	return content, nil
}

func addContent(securityTag string, snippet string, content map[string]*osutil.FileState) {
	var buffer bytes.Buffer
	buffer.Write(xmlHeader)
	buffer.WriteString(snippet)
	buffer.Write(xmlFooter)

	content[fmt.Sprintf("%s.conf", securityTag)] = &osutil.FileState{
		Content: buffer.Bytes(),
		Mode:    0644,
	}
}

func (b *Backend) NewSpecification() interfaces.Specification {
	return &Specification{}
}
