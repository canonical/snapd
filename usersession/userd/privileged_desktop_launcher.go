// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package userd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/desktop/desktopentry"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/systemd"
)

const privilegedLauncherIntrospectionXML = `
<interface name="org.freedesktop.DBus.Peer">
	<method name='Ping'>
	</method>
	<method name='GetMachineId'>
               <arg type='s' name='machine_uuid' direction='out'/>
	</method>
</interface>
<interface name='io.snapcraft.PrivilegedDesktopLauncher'>
	<method name='OpenDesktopEntry'>
		<arg type='s' name='desktop_file_id' direction='in'/>
	</method>
</interface>`

// PrivilegedDesktopLauncher implements the 'io.snapcraft.PrivilegedDesktopLauncher' DBus interface.
type PrivilegedDesktopLauncher struct {
	conn *dbus.Conn
}

// Name returns the name of the interface this object implements
func (s *PrivilegedDesktopLauncher) Interface() string {
	return "io.snapcraft.PrivilegedDesktopLauncher"
}

// BasePath returns the base path of the object
func (s *PrivilegedDesktopLauncher) ObjectPath() dbus.ObjectPath {
	return "/io/snapcraft/PrivilegedDesktopLauncher"
}

// IntrospectionData gives the XML formatted introspection description
// of the DBus service.
func (s *PrivilegedDesktopLauncher) IntrospectionData() string {
	return privilegedLauncherIntrospectionXML
}

// OpenDesktopEntry implements the 'OpenDesktopEntry' method of the 'io.snapcraft.DesktopLauncher'
// DBus interface. The desktopFileID is described here:
// https://standards.freedesktop.org/desktop-entry-spec/desktop-entry-spec-latest.html#desktop-file-id
func (s *PrivilegedDesktopLauncher) OpenDesktopEntry(desktopFileID string, sender dbus.Sender) *dbus.Error {
	desktopFile, err := desktopFileIDToFilename(desktopFileID)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	err = verifyDesktopFileLocation(desktopFile)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	de, err := desktopentry.Read(desktopFile)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	args, err := de.ExpandExec(nil)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	err = systemd.EnsureAtLeast(236)
	if err == nil {
		// systemd 236 introduced the --collect option to systemd-run,
		// which specifies that the unit should be garbage collected
		// even if it fails.
		//   https://github.com/systemd/systemd/pull/7314
		args = append([]string{"systemd-run", "--user", "--collect", "--"}, args...)
	} else if systemd.IsSystemdTooOld(err) {
		args = append([]string{"systemd-run", "--user", "--"}, args...)
	} else {
		// systemd not available
		return dbus.MakeFailedError(err)
	}

	cmd := exec.Command(args[0], args[1:]...)

	if err := cmd.Run(); err != nil {
		return dbus.MakeFailedError(fmt.Errorf("cannot run %q: %v", args, err))
	}

	return nil
}

var regularFileExists = osutil.RegularFileExists

// desktopFileSearchPath returns the list of directories where desktop
// files may be located.  It implements the lookup rules documented in
// the XDG Base Directory specification.
func desktopFileSearchPath() []string {
	var desktopDirs []string

	// First check $XDG_DATA_HOME, which defaults to $HOME/.local/share
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		homeDir := os.Getenv("HOME")
		if homeDir != "" {
			dataHome = filepath.Join(homeDir, ".local/share")
		}
	}
	if dataHome != "" {
		desktopDirs = append(desktopDirs, filepath.Join(dataHome, "applications"))
	}

	// Next check $XDG_DATA_DIRS, with default from spec
	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share/:/usr/share/"
	}
	for _, dir := range strings.Split(dataDirs, ":") {
		if dir == "" {
			continue
		}
		desktopDirs = append(desktopDirs, filepath.Join(dir, "applications"))
	}

	return desktopDirs
}

// findDesktopFile recursively tries each subdirectory that can be formed from the (split) desktop file ID.
// Per https://standards.freedesktop.org/desktop-entry-spec/desktop-entry-spec-latest.html#desktop-file-id,
// if desktop entries have dashes in the name ('-'), this could be an indication of subdirectories, so search
// for those too. Eg, given foo-bar_baz_norf.desktop the following are searched for:
//
//	o .../foo-bar_baz-norf.desktop
//	o .../foo/bar_baz-norf.desktop
//	o .../foo/bar_baz/norf.desktop
//	o .../foo-bar_baz/norf.desktop
//
// We're not required to diagnose multiple files matching the desktop file ID.
func findDesktopFile(baseDir string, splitFileId []string) (string, error) {
	desktopFile := filepath.Join(baseDir, strings.Join(splitFileId, "-"))

	exists, isReg, _ := regularFileExists(desktopFile)
	if exists && isReg {
		return desktopFile, nil
	}

	// Iterate through the potential subdirectories formed by the first i elements of the desktop file ID.
	for i := 1; i != len(splitFileId); i++ {
		prefix := strings.Join(splitFileId[:i], "-")
		// Don't treat empty or "." components as directory
		// prefixes.  The ".." case is already filtered out by
		// the isValidDesktopFileID regexp.
		if prefix == "" || prefix == "." {
			continue
		}
		desktopFile, err := findDesktopFile(filepath.Join(baseDir, prefix), splitFileId[i:])
		if err == nil {
			return desktopFile, nil
		}
	}

	return "", fmt.Errorf("could not find desktop file")
}

// isValidDesktopFileID is based on the "File naming" section of the
// Desktop Entry Specification, without the restriction on components
// not starting with a digit (which desktop files created by snapd may
// not satisfy).
var isValidDesktopFileID = regexp.MustCompile(`^[A-Za-z0-9-_]+(\.[A-Za-z0-9-_]+)*.desktop$`).MatchString

// desktopFileIDToFilename determines the path associated with a desktop file ID.
func desktopFileIDToFilename(desktopFileID string) (string, error) {
	if !isValidDesktopFileID(desktopFileID) {
		return "", fmt.Errorf("cannot find desktop file for %q", desktopFileID)
	}

	splitDesktopID := strings.Split(desktopFileID, "-")
	for _, baseDir := range desktopFileSearchPath() {
		if desktopFile, err := findDesktopFile(baseDir, splitDesktopID); err == nil {
			return desktopFile, nil
		}
	}

	return "", fmt.Errorf("cannot find desktop file for %q", desktopFileID)
}

// verifyDesktopFileLocation checks the desktop file location:
// we only consider desktop files in dirs.SnapDesktopFilesDir
func verifyDesktopFileLocation(desktopFile string) error {
	if filepath.Clean(desktopFile) != desktopFile {
		return fmt.Errorf("desktop file has unclean path: %q", desktopFile)
	}

	if !strings.HasPrefix(desktopFile, dirs.SnapDesktopFilesDir+"/") {
		// We currently only support launching snap applications from desktop files in
		// /var/lib/snapd/desktop/applications.
		return fmt.Errorf("only launching snap applications from %s is supported", dirs.SnapDesktopFilesDir)
	}

	return nil
}
