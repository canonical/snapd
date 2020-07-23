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
// `snap userd` instance on re-exec
func setupDbusServiceForUserd(snapInfo *snap.Info) error {
	coreOrSnapdRoot := snapInfo.MountDir()

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
// DBus has no concept of a complain mode so confinment type is ignored.
func (b *Backend) Setup(snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) error {
	snapName := snapInfo.InstanceName()
	// Get the snippets that apply to this snap
	spec, err := repo.SnapSpecification(b.Name(), snapName)
	if err != nil {
		return fmt.Errorf("cannot obtain dbus specification for snap %q: %s", snapName, err)
	}

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
	_, _, err := osutil.EnsureDirState(dirs.SnapDBusSystemPolicyDir, glob, nil)
	if err != nil {
		return fmt.Errorf("cannot synchronize DBus configuration files for snap %q: %s", snapName, err)
	}
	return nil
}

// snapServiceActivationFiles returns the list of service activation files for a snap.
func snapServiceActivationFiles(dir, snapName string) (services []string, err error) {
	glob := filepath.Join(dir, "*.service")
	matches, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}
	for _, match := range matches {
		serviceSnap, err := snapNameFromServiceFile(match)
		if err != nil {
			return nil, err
		}
		if serviceSnap == snapName {
			services = append(services, filepath.Base(match))
		}
	}
	return services, nil
}

// removeServiceActivation removes D-Bus service activation files associated with a snap.
func (b *Backend) removeServiceActivation(snapName string) error {
	for _, servicesDir := range []string{
		dirs.SnapDBusSessionServicesDir,
		dirs.SnapDBusSystemServicesDir,
	} {
		toRemove, err := snapServiceActivationFiles(servicesDir, snapName)
		if err != nil {
			return err
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
func snapNameFromServiceFile(filename string) (owner string, err error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return "", err
	}
	snapKey := []byte("\nX-Snap=")
	pos := bytes.Index(content, snapKey)
	if pos == -1 {
		return "", nil
	}
	snapName := content[pos+len(snapKey):]
	pos = bytes.IndexRune(snapName, '\n')
	if pos != -1 {
		snapName = snapName[:pos]
	}
	return string(snapName), nil
}

func (b *Backend) deriveContentServices(snapInfo *snap.Info, servicesDir string, services map[string]*Service) (globs []string, content map[string]osutil.FileState, err error) {
	snapName := snapInfo.InstanceName()
	// Prime globs with the already existing service activation
	// files associated with this snap.
	globs, err = snapServiceActivationFiles(servicesDir, snapName)
	if err != nil {
		return nil, nil, err
	}
	content = make(map[string]osutil.FileState)
	for _, service := range services {
		filename := service.BusName + ".service"
		if old, err := snapNameFromServiceFile(filepath.Join(servicesDir, filename)); err != nil {
			if !os.IsNotExist(err) {
				return nil, nil, fmt.Errorf("cannot add session dbus name %q: %q", service.BusName, err)
			}
		} else if old == "" {
			return nil, nil, fmt.Errorf("cannot add session dbus name %q, already taken by non-snap application", service.BusName)
		} else if old != snapName {
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

// BusNameOwner returns the snap that owns a particular bus name, or an empty string.
func BusNameOwner(bus, name string) (owner string, err error) {
	var servicesDir string
	switch bus {
	case "session":
		servicesDir = dirs.SnapDBusSessionServicesDir
	case "system":
		servicesDir = dirs.SnapDBusSystemServicesDir
	default:
		return "", fmt.Errorf("unknown D-Bus bus %q", bus)
	}
	serviceFile := filepath.Join(servicesDir, name+".service")
	return snapNameFromServiceFile(serviceFile)
}
