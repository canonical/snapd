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
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// Backend is responsible for maintaining DBus policy files.
type Backend struct{}

// Initialize does nothing.
func (b *Backend) Initialize() error {
	return nil
}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return "dbus"
}

// setupDbusServiceForUserd will setup the service file for the new
// `snap userd` instance on re-exec
func setupDbusServiceForUserd(snapInfo *snap.Info) error {
	coreRoot := snapInfo.MountDir()

	for _, srv := range []string{
		"io.snapcraft.Launcher.service",
		"io.snapcraft.Settings.service",
	} {
		dst := filepath.Join("/usr/share/dbus-1/services/", srv)
		src := filepath.Join(coreRoot, dst)
		if !osutil.FilesAreEqual(src, dst) {
			if err := osutil.CopyFile(src, dst, osutil.CopyFlagPreserveAll); err != nil {
				return err
			}
		}
	}
	return nil
}

// Setup creates dbus configuration files specific to a given snap.
//
// DBus has no concept of a complain mode so confinment type is ignored.
func (b *Backend) Setup(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
	snapName := snapInfo.InstanceName()
	// Get the snippets that apply to this snap
	spec, err := repo.SnapSpecification(b.Name(), snapName)
	if err != nil {
		return fmt.Errorf("cannot obtain dbus specification for snap %q: %s", snapName, err)
	}

	// core on classic is special
	//
	// TODO: we need to deal with the "snapd" snap here soon
	if snapName == "core" && release.OnClassic {
		if err := setupDbusServiceForUserd(snapInfo); err != nil {
			logger.Noticef("cannot create host `snap userd` dbus service file: %s", err)
		}
	}

	// Get the files that this snap should have
	content, err := b.deriveContent(spec.(*Specification), snapInfo)
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
	glob := fmt.Sprintf("%s.conf", interfaces.SecurityTagGlob(snapName))
	_, _, err := osutil.EnsureDirState(dirs.SnapBusPolicyDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize DBus configuration files for snap %q: %s", snapName, err)
	}
	return nil
}

// deriveContent combines security snippets collected from all the interfaces
// affecting a given snap into a content map applicable to EnsureDirState.
func (b *Backend) deriveContent(spec *Specification, snapInfo *snap.Info) (content map[string]*osutil.FileState, err error) {
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

// SandboxFeatures returns list of features supported by snapd for dbus communication.
func (b *Backend) SandboxFeatures() []string {
	return []string{"mediated-bus-access"}
}
