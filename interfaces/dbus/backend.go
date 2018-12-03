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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
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

// setupHostDBusConf will ensure that we have a dbus configuration
// that points to /var/lib/snapd/dbus/{services,system-services}. This
// is needed for systems that re-exec snapd and do not have this
// configuration as part of the packaged snapd.
func setupHostDBusConf(snapInfo *snap.Info) error {
	coreRoot := snapInfo.MountDir()
	for _, conf := range []string{
		"session.d/snapd-session.conf",
		"system.d/snapd-system.conf",
	} {
		dst := filepath.Join("/usr/share/dbus-1/", conf)
		src := filepath.Join(coreRoot, dst)
		if !osutil.FilesAreEqual(src, dst) {
			if err := osutil.CopyFile(src, dst, osutil.CopyFlagPreserveAll); err != nil {
				return err
			}
		}
	}
	return nil
}

// setupDbusServiceForUserd will setup the service file for the new
// `snap userd` instance on re-exec
func setupDbusServiceForUserd(snapInfo *snap.Info) error {
	coreOrSnapdRoot := snapInfo.MountDir()

	// fugly - but we need to make sure that the content of the
	// "snapd" snap wins
	//
	// TODO: this is also racy but the content of the files in core and
	// snapd is identical cleanup after link-snap and
	// setup-profiles are unified
	if snapInfo.InstanceName() == "core" && osutil.FileExists(filepath.Join(coreOrSnapdRoot, "../..", "snapd/current")) {
		return nil
	}

	for _, srv := range []string{
		"io.snapcraft.Launcher.service",
		"io.snapcraft.Settings.service",
	} {
		dst := filepath.Join("/usr/share/dbus-1/services/", srv)
		src := filepath.Join(coreOrSnapdRoot, dst)

		// we only need the GlobalRootDir for testing
		dst = filepath.Join(dirs.GlobalRootDir, dst)
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
func (b *Backend) Setup(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) error {
	snapName := snapInfo.InstanceName()
	// Get the snippets that apply to this snap
	spec, err := repo.SnapSpecification(b.Name(), snapName)
	if err != nil {
		return fmt.Errorf("cannot obtain dbus specification for snap %q: %s", snapName, err)
	}

	// core/snapd on classic are special
	if (snapInfo.GetType() == snap.TypeOS || snapInfo.GetType() == snap.TypeSnapd) && release.OnClassic {
		if err := setupDbusServiceForUserd(snapInfo); err != nil {
			logger.Noticef("cannot create host `snap userd` dbus service file: %s", err)
		}
		if err := setupHostDBusConf(snapInfo); err != nil {
			logger.Noticef("cannot create host dbus config: %s", err)
		}
	}

	if err := b.setupBusConfig(snapInfo, spec); err != nil {
		return err
	}
	if err := b.setupServiceActivation(snapInfo, spec); err != nil {
		return err
	}
	return nil
}

func (b *Backend) setupBusConfig(snapInfo *snap.Info, spec interfaces.Specification) error {
	snapName := snapInfo.InstanceName()
	// Get the files that this snap should have
	content, err := b.deriveContentConfig(spec.(*Specification), snapInfo)
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

func (b *Backend) setupServiceActivation(snapInfo *snap.Info, spec interfaces.Specification) error {
	snapName := snapInfo.InstanceName()
	s := spec.(*Specification)
	for _, bus := range []struct {
		dir      string
		services map[string]*Service
	}{
		{dirs.SnapDBusSessionServicesDir, s.SessionServices()},
		{dirs.SnapDBusSystemServicesDir, s.SystemServices()},
	} {
		globs, content, err := b.deriveContentServices(snapInfo, bus.dir, bus.services)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(bus.dir, 0755); err != nil {
			return err
		}
		_, _, err = osutil.EnsureDirStateGlobs(bus.dir, globs, content)
		if err != nil {
			return fmt.Errorf("cannot synchronize DBus service activation files for snap %q: %s", snapName, err)
		}
	}
	return nil
}

// Remove removes dbus configuration files of a given snap.
//
// This method should be called after removing a snap.
func (b *Backend) Remove(snapName string) error {
	if err := b.removeBusConfig(snapName); err != nil {
		return err
	}
	if err := b.removeServiceActivation(snapName); err != nil {
		return err
	}
	return nil
}

// removeBusConfig removes D-Bus configuration files associated with a snap.
func (b *Backend) removeBusConfig(snapName string) error {
	glob := fmt.Sprintf("%s.conf", interfaces.SecurityTagGlob(snapName))
	_, _, err := osutil.EnsureDirState(dirs.SnapBusPolicyDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize DBus configuration files for snap %q: %s", snapName, err)
	}
	return nil
}

// removeServiceActivation removes D-Bus service activation files associated with a snap.
func (b *Backend) removeServiceActivation(snapName string) error {
	for _, servicesDir := range []string{
		dirs.SnapDBusSessionServicesDir,
		dirs.SnapDBusSystemServicesDir,
	} {
		glob := filepath.Join(servicesDir, "*.service")
		matches, err := filepath.Glob(glob)
		if err != nil {
			return err
		}
		toRemove := []string{}
		for _, match := range matches {
			serviceSnap := snapNameFromServiceFile(match)
			if serviceSnap == snapName {
				toRemove = append(toRemove, filepath.Base(match))
			}
		}
		_, _, err = osutil.EnsureDirStateGlobs(servicesDir, toRemove, nil)
		if err != nil {
			return fmt.Errorf("cannot synchronize DBus service files for snap %q: %s", snapName, err)
		}
	}
	return nil
}

// deriveContent combines security snippets collected from all the interfaces
// affecting a given snap into a content map applicable to EnsureDirState.
func (b *Backend) deriveContentConfig(spec *Specification, snapInfo *snap.Info) (content map[string]osutil.FileState, err error) {
	for _, appInfo := range snapInfo.Apps {
		securityTag := appInfo.SecurityTag()
		appSnippets := spec.SnippetForTag(securityTag)
		if appSnippets == "" {
			continue
		}
		if content == nil {
			content = make(map[string]osutil.FileState)
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
			content = make(map[string]osutil.FileState)
		}

		addContent(securityTag, hookSnippets, content)
	}

	return content, nil
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

// snapNameFromServiceFile returns the snap name for the D-Bus service activation file.
func snapNameFromServiceFile(filename string) string {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Noticef("can not read service file %s: %s", filename, err)
		}
		return ""
	}
	snapKey := []byte("\nX-Snap=")
	pos := bytes.Index(content, snapKey)
	if pos == -1 {
		return ""
	}
	snapName := content[pos+len(snapKey):]
	pos = bytes.IndexRune(snapName, '\n')
	if pos != -1 {
		snapName = snapName[:pos]
	}
	return string(snapName)
}

func (b *Backend) deriveContentServices(snapInfo *snap.Info, servicesDir string, services map[string]*Service) (globs []string, content map[string]osutil.FileState, err error) {
	globs = []string{}
	content = make(map[string]osutil.FileState)
	snapName := snapInfo.InstanceName()
	for _, service := range services {
		filename := service.BusName + ".service"
		if old := snapNameFromServiceFile(filepath.Join(servicesDir, filename)); old != "" && old != snapName {
			return nil, nil, fmt.Errorf("cannot add session dbus name %q, already taken by: %q", service.BusName, old)
		}
		globs = append(globs, filename)
		content[filename] = &osutil.MemoryFileState{
			Content: service.Content,
			Mode:    0644,
		}
	}
	return globs, content, nil
}

func (b *Backend) NewSpecification() interfaces.Specification {
	return &Specification{}
}

// SandboxFeatures returns list of features supported by snapd for dbus communication.
func (b *Backend) SandboxFeatures() []string {
	return []string{"mediated-bus-access"}
}
