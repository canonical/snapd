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

// Package apparmor implements integration between snappy and
// ubuntu-core-launcher around apparmor.
//
// Snappy creates apparmor profiles for each application (for each snap)
// present in the system.  Upon each execution of ubuntu-core-launcher, the
// profile is attached to the running process. Prior to that the profile must
// be parsed, compiled and loaded into the kernel using the support tool
// "apparmor_parser".
//
// Each apparmor profile contains a simple <header><content><footer> structure.
// The header specified an identifier that is relevant to the kernel. The
// identifier can be either the full path of the executable or an abstract
// identifier not related to the executable name.
//
// The actual profiles are stored in /var/lib/snappy/apparmor/profiles.
// This directory is also hard-coded in ubuntu-core-launcher.
//
// NOTE: A systemd job (TODO: specify which) loads all snappy-specific apparmor
// profiles into the kernel during the boot process.
package apparmor

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/dbus"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

// Configurator is responsible for maintaining apparmor profiles for ubuntu-core-launcher.
type Configurator struct {
	pendingReload []string
	pendingUnload []Profile
}

// ConfigureSnapSecurity creates security artefacts specific to a given snap.
// The snap can be in developer mode to make security violations non-fatal to
// the offending application process.
//
// This method only deals with files. Actual kernel changes (reloading and
// unloading profiles) is deferred to a call to Finalize()
func (cfg *Configurator) ConfigureSnapSecurity(repo *interfaces.Repository, snapInfo *snap.Info, developerMode bool) error {
	// Get the snippets that apply to this snap
	snippets, err := repo.SecuritySnippetsForSnap(snapInfo.Name, cfg.SecuritySystem())
	if err != nil {
		return fmt.Errorf("cannot obtain security snippets for snap %q: %s", snapInfo.Name, err)
	}
	// Get the files that this snap should have
	dir, glob, content, err := cfg.DirStateForInstalledSnap(snapInfo, developerMode, snippets)
	if err != nil {
		return fmt.Errorf("cannot obtain expected apparmor files for snap %q: %s", snapInfo.Name, err)
	}
	// Ensure that files are correctly on disk
	changed, removed, err := osutil.EnsureDirState(dir, glob, content)
	// Record changes so that we can do clean-up in Finalize. This has to
	// happen even if EnsureDirState has failed.
	for _, baseName := range changed {
		cfg.pendingReload = append(cfg.pendingReload, filepath.Join(dir, baseName))
	}
	for _, baseName := range removed {
		// XXX: This depends on ProfileName and ProfileFile returning the same
		// value.  Ideally, if DirStateForSnap was inlined, we'd store the
		// unambiguous profile name here.
		cfg.pendingUnload = append(cfg.pendingUnload, Profile{Name: baseName})
	}
	if err != nil {
		return fmt.Errorf("cannot synchronize apparmor files for snap %q: %s", snapInfo.Name, err)
	}
	return nil
}

// DeconfigureSnapSecurity removes security artefacts of a given snap.
//
// This method should be called after removing a snap.
//
// This method only deals with the filesystem. Kernel changes (unloading
// profiles) is deferred to a call to Finalize()
func (cfg *Configurator) DeconfigureSnapSecurity(snapInfo *snap.Info) error {
	dir, glob := cfg.DirStateForRemovedSnap(snapInfo)
	changed, removed, err := osutil.EnsureDirState(dir, glob, nil)
	if len(changed) > 0 {
		panic(fmt.Sprintf("removed snaps cannot have security files but we got %s", changed))
	}
	for _, baseName := range removed {
		// XXX: This depends on ProfileName and ProfileFile returning the same
		// value.  Ideally, if DirStateForSnap was inlined, we'd store the
		// unambiguous profile name here.
		cfg.pendingUnload = append(cfg.pendingUnload, Profile{Name: baseName})
	}
	if err != nil {
		return fmt.Errorf("cannot synchronize apparmor files for snap %q: %s", snapInfo.Name, err)
	}
	return nil
}

// Finalize does post-processing after using ConfigureSnapSecurity on any number of snaps.
func (cfg *Configurator) Finalize() error {
	for i, fname := range cfg.pendingReload {
		err := LoadProfile(fname)
		if err != nil {
			cfg.pendingReload = cfg.pendingReload[i:]
			return fmt.Errorf("cannot load apparmor profile %q: %s", fname, err)
		}
	}
	cfg.pendingReload = nil
	for i, profile := range cfg.pendingUnload {
		err := profile.Unload()
		if err != nil {
			cfg.pendingUnload = cfg.pendingUnload[i:]
			return fmt.Errorf("cannot unload apparmor profile %q: %s", profile.Name, err)
		}
	}
	cfg.pendingUnload = nil
	return nil
}

// SecuritySystem returns the constant interfaces.SecurityAppArmor.
func (cfg *Configurator) SecuritySystem() interfaces.SecuritySystem {
	return interfaces.SecurityAppArmor
}

// DirStateForInstalledSnap returns input for EnsureDirState() describing
// apparmor profiles for the given snap.
func (cfg *Configurator) DirStateForInstalledSnap(snapInfo *snap.Info, developerMode bool, snippets map[string][][]byte) (dir, glob string, content map[string]*osutil.FileState, err error) {
	dir = Directory()
	glob = profileGlob(snapInfo)
	for _, appInfo := range snapInfo.Apps {
		s := make([][]byte, 0, len(snippets[appInfo.Name])+2)
		s = append(s, aaHeader(appInfo, developerMode))
		s = append(s, snippets[appInfo.Name]...)
		s = append(s, []byte("}\n"))
		fileContent := bytes.Join(s, []byte("\n"))
		if content == nil {
			content = make(map[string]*osutil.FileState)
		}
		content[ProfileFile(appInfo)] = &osutil.FileState{
			Content: fileContent,
			Mode:    0644,
		}
	}
	return dir, glob, content, nil
}

// DirStateForRemovedSnap returns input for EnsureDirState() for removing any
// apparmor files that used to belong to a removed snap.
func (cfg *Configurator) DirStateForRemovedSnap(snapInfo *snap.Info) (dir, glob string) {
	dir = Directory()
	glob = profileGlob(snapInfo)
	return dir, glob
}

// ProfileName returns the name of the apparmor profile file for a specific app.
//
// The return value must be used as an argument to ubuntu-core-launcher.
func ProfileName(appInfo *snap.AppInfo) string {
	return interfaces.SecurityTag(appInfo)
}

// ProfileFile returns the name of the apparmor profile file for a specific app.
func ProfileFile(appInfo *snap.AppInfo) string {
	return interfaces.SecurityTag(appInfo)
}

// profileGlob returns a glob matching names of all the apparmor profile
// files for a specific snap.
//
// The returned pattern must match the return value from ProfileName.
func profileGlob(snapInfo *snap.Info) string {
	return interfaces.SecurityGlob(snapInfo)
}

// Directory returns the apparmor configuration directory.
//
// The return value must be changed in lock-step with the systemd job that
// loads profiles on boot.
func Directory() string {
	return dirs.SnapAppArmorDir
}

// legacyVariablees returns text defining some apparmor variables that work
// with supported apparmor templates.
//
// The variables are expanded by apparmor parser. They are (currently):
//  - APP_APPNAME
//  - APP_ID_DBUS
//  - APP_PKGNAME_DBUS
//  - APP_PKGNAME
//  - APP_VERSION
//  - INSTALL_DIR
// They can be changed but this has to match changes in template.go.
//
// In addition, the set of variables listed here interacts with old-security
// interface since there the base template is provided by a particular 3rd
// party snap, not by snappy.
func legacyVariables(appInfo *snap.AppInfo) string {
	// XXX: Straw-man: can we just expose the following apparmor variables...
	//
	// @{APP_NAME}=app.Name
	// @{SNAP_NAME}=app.SnapName
	// @{SNAP_REVISION}=app.Revision
	// @{SNAP_SECURITY_TAG}=app.SecurityTag()
	//
	// ...have everything work correctly?
	return "" +
		fmt.Sprintf("@{APP_APPNAME}=\"%s\"\n", appInfo.Name) +
		// TODO: replace with app.SecurityTag()
		fmt.Sprintf("@{APP_ID_DBUS}=\"%s\"\n",
			dbus.SafePath(fmt.Sprintf("%s.%s_%s_%s",
				appInfo.Snap.Name, appInfo.Snap.Developer, appInfo.Name, appInfo.Snap.Version))) +
		// XXX: How is this different from APP_ID_DBUS?
		fmt.Sprintf("@{APP_PKGNAME_DBUS}=\"%s\"\n",
			dbus.SafePath(fmt.Sprintf("%s.%s",
				appInfo.Snap.Name, appInfo.Snap.Developer))) +
		// TODO: stop using .Developer, investigate how this is used.
		fmt.Sprintf("@{APP_PKGNAME}=\"%s\"\n", fmt.Sprintf("%s.%s",
			appInfo.Snap.Name, appInfo.Snap.Developer)) +
		// TODO: switch to .Revision
		fmt.Sprintf("@{APP_VERSION}=\"%s\"\n", appInfo.Snap.Version) +
		"@{INSTALL_DIR}=\"{/snaps,/gadget}\"\n"
}

// aaHeader returns the topmost part of the generated apparmor profile.
//
// The header contains a few lines of apparmor variables that are referenced by
// the template as well as the syntax that begins the content of the actual
// profile. That same content also decides if the profile is enforcing or
// advisory (complain). This is used to implement developer mode.
func aaHeader(appInfo *snap.AppInfo, developerMode bool) []byte {
	text := strings.TrimRight(defaultTemplate, "\n}")
	if developerMode {
		// XXX: This needs to be verified
		text = strings.Replace(text, "(attach_disconnected)", "(attach_disconnected,complain)", 1)
	}
	text = strings.Replace(text, "###VAR###\n", legacyVariables(appInfo), 1)
	text = strings.Replace(text, "###PROFILEATTACH###",
		fmt.Sprintf("profile \"%s\"", ProfileName(appInfo)), 1)
	return []byte(text)
}
