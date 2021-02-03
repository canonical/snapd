// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil/shlex"
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
		<arg type='s' name='desktopFileID' direction='in'/>
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
	return "/io/snapcraft/Launcher"
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

	command, icon, err := readExecCommandFromDesktopFile(desktopFile)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	args, err := parseExecCommand(command, icon)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	args = append([]string{"systemd-run", "--user", "--"}, args...)

	cmd := exec.Command(args[0], args[1:]...)

	if cmd.Run() != nil {
		return dbus.MakeFailedError(fmt.Errorf("cannot run %q", command))
	}

	return nil
}

var regularFileExists = osutil.RegularFileExists

// findDesktopFile recursively tries each subdirectory that can be formed from the (split) desktop file ID.
// Per https://standards.freedesktop.org/desktop-entry-spec/desktop-entry-spec-latest.html#desktop-file-id,
// if desktop entries have dashes in the name ('-'), this could be an indication of subdirectories, so search
// for those too. Eg, given foo-bar_baz_norf.desktop the following are searched for:
//   o .../foo-bar_baz-norf.desktop
//   o .../foo/bar_baz-norf.desktop
//   o .../foo/bar_baz/norf.desktop
//   o .../foo-bar_baz/norf.desktop
// We're not required to diagnose multiple files matching the desktop file ID.
func findDesktopFile(baseDir string, splitFileId []string) (string, error) {
	desktopFile := filepath.Join(baseDir, strings.Join(splitFileId, "-"))

	exists, isReg, _ := regularFileExists(desktopFile)
	if exists && isReg {
		return desktopFile, nil
	}

	// Iterate through the potential subdirectories formed by the first i elements of the desktop file ID.
	// Maybe this is overkill: At the time of writing, the only use is in desktopFileIDToFilename() and there
	// we're only checking dirs.SnapDesktopFilesDir (not all entries in $XDG_DATA_DIRS) and we know that snapd
	// does not create subdirectories in that location.
	for i := 1; i != len(splitFileId); i++ {
		desktopFile, err := findDesktopFile(filepath.Join(baseDir, strings.Join(splitFileId[:i], "-")), splitFileId[i:])
		if err == nil {
			return desktopFile, err
		}
	}

	return "", fmt.Errorf("could not find desktop file")
}

// desktopFileIDToFilename determines the path associated with a desktop file ID.
func desktopFileIDToFilename(desktopFileID string) (string, error) {
	// OpenDesktopEntry() currently only supports launching snap applications from
	// desktop files in /var/lib/snapd/desktop/applications and these desktop files are
	// written by snapd and considered safe for userd to process.
	// Since we only access /var/lib/snapd/desktop/applications, ignore
	// https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html
	baseDir := dirs.SnapDesktopFilesDir

	desktopFile, err := findDesktopFile(baseDir, strings.Split(desktopFileID, "-"))

	if err == nil {
		return desktopFile, nil
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
		// /var/lib/snapd/desktop/applications and these desktop files are written by snapd and
		// considered safe for userd to process. If other directories are added in the future,
		// verifyDesktopFileLocation() and parseExecCommand() may need to be updated.
		return fmt.Errorf("internal error: only launching snap applications from /var/lib/snapd/desktop/applications is supported")
	}

	return nil
}

// readExecCommandFromDesktopFile parses the desktop file to get the Exec entry and
// checks that the BAMF_DESKTOP_FILE_HINT is present and refers to the desktop file.
func readExecCommandFromDesktopFile(desktopFile string) (exec string, icon string, err error) {
	file, err := os.Open(desktopFile)
	if err != nil {
		return exec, icon, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)

	inDesktopSection := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "[Desktop Entry]" {
			inDesktopSection = true
		} else if strings.HasPrefix(line, "[Desktop Action ") {
			// maybe later we'll add support here
			inDesktopSection = false
		} else if strings.HasPrefix(line, "[") {
			inDesktopSection = false
		} else if inDesktopSection {
			if strings.HasPrefix(line, "Exec=") {
				exec = strings.TrimPrefix(line, "Exec=")
			} else if strings.HasPrefix(line, "Icon=") {
				icon = strings.TrimPrefix(line, "Icon=")
			}
		}
	}

	expectedPrefix := fmt.Sprintf("env BAMF_DESKTOP_FILE_HINT=%s %s", desktopFile, dirs.SnapBinariesDir)
	if !strings.HasPrefix(exec, expectedPrefix) {
		return "", "", fmt.Errorf("desktop file %q has an unsupported 'Exec' value: %q", desktopFile, exec)
	}

	return exec, icon, nil
}

// Parse the Exec command by stripping any exec variables.
// Passing exec variables (eg, %foo) between confined snaps is unsupported. Currently,
// we do not have support for passing them in the D-Bus API but there are security
// implications that must be thought through regarding the influence of the launching
// snap over the launcher wrt exec variables. For now we simply filter them out.
// https://standards.freedesktop.org/desktop-entry-spec/desktop-entry-spec-latest.html#exec-variables
func parseExecCommand(command string, icon string) ([]string, error) {
	origArgs, err := shlex.Split(command)
	if err != nil {
		return nil, err
	}

	args := make([]string, 0, len(origArgs))
	for _, arg := range origArgs {
		// We want to keep literal '%' (expressed as '%%') but filter our exec variables
		// like '%foo'
		if strings.HasPrefix(arg, "%%") {
			arg = arg[1:]
		} else if strings.HasPrefix(arg, "%") {
			switch arg {
			case "%f", "%F", "%u", "%U":
				// If we were launching a file with
				// the application, these variables
				// would expand to file names or URIs.
				// As we're not, they are simply
				// removed from the argument list.
			case "%i":
				args = append(args, "--icon", icon)
			default:
				return nil, fmt.Errorf("cannot run %q", command)
			}
			continue
		}
		args = append(args, arg)
	}
	return args, nil
}
