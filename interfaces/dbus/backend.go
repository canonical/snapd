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
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/timings"
	"github.com/snapcore/snapd/wrappers"
)

// Backend is responsible for maintaining DBus policy files.
type Backend struct{}

// Initialize does nothing.
func (b *Backend) Initialize(*interfaces.SecurityBackendOptions) error {
	return nil
}

// Name returns the name of the backend.
func (b *Backend) Name() interfaces.SecuritySystem {
	return "dbus"
}

func shouldCopyConfigFiles(snapInfo *snap.Info) bool {
	// Only copy config files on classic distros
	if !release.OnClassic {
		return false
	}
	// Only copy config files if we have been reexecuted
	if reexecd, _ := snapdtool.IsReexecd(); !reexecd {
		return false
	}
	switch snapInfo.Type() {
	case snap.TypeOS:
		// XXX: ugly but we need to make sure that the content
		// of the "snapd" snap wins
		//
		// TODO: this is also racy but the content of the
		// files in core and snapd is identical.  Cleanup
		// after link-snap and setup-profiles are unified
		return !osutil.FileExists(filepath.Join(snapInfo.MountDir(), "../..", "snapd/current"))
	case snap.TypeSnapd:
		return true
	default:
		return false
	}
}

// setupDbusServiceForUserd will setup the service file for the new
// `snap userd` instance on re-exec. If there are leftover service files in
// place which are no longer required, those files will be deleted.
func setupDbusServiceForUserd(snapInfo *snap.Info) error {
	coreOrSnapdRoot := snapInfo.MountDir()

	// Only ever append to this list. If a file is no longer present on the
	// root, it needs to needs to remain here so the previously-installed
	// service file can be removed, if present.
	for _, srv := range []string{
		"io.snapcraft.Launcher.service",
		"io.snapcraft.Prompt.service",
		"io.snapcraft.Settings.service",
	} {
		dst := filepath.Join("/usr/share/dbus-1/services/", srv)
		src := filepath.Join(coreOrSnapdRoot, dst)

		// we only need the GlobalRootDir for testing
		dst = filepath.Join(dirs.GlobalRootDir, dst)
		if !osutil.FileExists(src) {
			if osutil.FileExists(dst) {
				if err := os.Remove(dst); err != nil {
					return err
				}
			}
			continue
		}
		if !osutil.FilesAreEqual(src, dst) {
			if err := osutil.CopyFile(src, dst, osutil.CopyFlagPreserveAll); err != nil {
				return err
			}
		}
	}
	return nil
}

func setupHostDBusConf(snapInfo *snap.Info) error {
	sessionContent, systemContent, err := wrappers.DeriveSnapdDBusConfig(snapInfo)
	if err != nil {
		return err
	}

	// We don't use `dirs.SnapDBusSessionPolicyDir because we want
	// to match the path the package on the host system uses.
	dest := filepath.Join(dirs.GlobalRootDir, "/usr/share/dbus-1/session.d")
	if err = os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	_, _, err = osutil.EnsureDirState(dest, "snapd.*.conf", sessionContent)
	if err != nil {
		return err
	}

	dest = filepath.Join(dirs.GlobalRootDir, "/usr/share/dbus-1/system.d")
	if err = os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	_, _, err = osutil.EnsureDirState(dest, "snapd.*.conf", systemContent)
	if err != nil {
		return err
	}

	return nil
}

// Setup creates dbus configuration files specific to a given snap.
//
// If there are leftover configuration files for services which are no longer
// included, those files will be removed as well.
//
// DBus has no concept of a complain mode so confinment type is ignored.
func (b *Backend) Setup(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) error {
	snapName := appSet.InstanceName()
	// Get the snippets that apply to this snap
	spec, err := repo.SnapSpecification(b.Name(), appSet)
	if err != nil {
		return fmt.Errorf("cannot obtain dbus specification for snap %q: %s", snapName, err)
	}

	snapInfo := appSet.Info()

	// copy some config files when installing core/snapd if we reexec
	if shouldCopyConfigFiles(snapInfo) {
		if err := setupDbusServiceForUserd(snapInfo); err != nil {
			logger.Noticef("cannot create host `snap userd` dbus service file: %s", err)
		}
		// TODO: Make this conditional on the dbus-activation
		// feature flag.
		if err := setupHostDBusConf(snapInfo); err != nil {
			logger.Noticef("cannot create host dbus config: %s", err)
		}
	}

	// Get the files that this snap should have
	content := b.deriveContent(spec.(*Specification), appSet)

	glob := fmt.Sprintf("%s.conf", interfaces.SecurityTagGlob(snapName))
	dir := dirs.SnapDBusSystemPolicyDir
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
	_, _, err := osutil.EnsureDirState(dirs.SnapDBusSystemPolicyDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize DBus configuration files for snap %q: %s", snapName, err)
	}
	return nil
}

// deriveContent combines security snippets collected from all the interfaces
// affecting a given snap into a content map applicable to EnsureDirState.
func (b *Backend) deriveContent(spec *Specification, appSet *interfaces.SnapAppSet) (content map[string]osutil.FileState) {
	for _, r := range appSet.Runnables() {
		appSnippets := spec.SnippetForTag(r.SecurityTag)
		if appSnippets == "" {
			continue
		}
		if content == nil {
			content = make(map[string]osutil.FileState)
		}

		addContent(r.SecurityTag, appSnippets, content)
	}

	return content
}

func addContent(securityTag string, snippet string, content map[string]osutil.FileState) {
	var buffer bytes.Buffer
	buffer.Write(xmlHeader)
	buffer.WriteString(snippet)
	buffer.Write(xmlFooter)

	content[fmt.Sprintf("%s.conf", securityTag)] = &osutil.MemoryFileState{
		Content: buffer.Bytes(),
		Mode:    0644,
	}
}

func (b *Backend) NewSpecification(appSet *interfaces.SnapAppSet) interfaces.Specification {
	return &Specification{appSet: appSet}
}

// SandboxFeatures returns list of features supported by snapd for dbus communication.
func (b *Backend) SandboxFeatures() []string {
	return []string{"mediated-bus-access"}
}
