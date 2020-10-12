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
	"syscall"
	"time"

	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/strutil/shlex"
	"github.com/snapcore/snapd/usersession/userd/ui"
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
	desktopFile, err := desktopFileIDToFilename(osutil.RegularFileExists, desktopFileID)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	err = verifyDesktopFileLocation(desktopFile)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	exec_command, err := readExecCommandFromDesktopFile(desktopFile)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	args, err := parseExecCommand(exec_command)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

  args = append([]string{"systemd-run", "--user", "--"}, args...)

	cmd := exec.Command(args[0], args[1:]...)

	if cmd.Run() != nil {
		return dbus.MakeFailedError(fmt.Errorf("cannot run %q", exec_command))
	}

	return nil
}


type fileExists func(string) bool

// findDesktopFile recursively tries each subdirectory that can be formed from the (split) desktop file ID.
// Per https://standards.freedesktop.org/desktop-entry-spec/desktop-entry-spec-latest.html#desktop-file-id,
// if desktop entries have dashes in the name ('-'), this could be an indication of subdirectories, so search
// for those too. Eg, given foo-bar_baz_norf.desktop the following are searched for:
//   o .../foo-bar_baz-norf.desktop
//   o .../foo/bar_baz-norf.desktop
//   o .../foo/bar_baz/norf.desktop
//   o .../foo-bar_baz/norf.desktop
// We're not required to diagnose multiple files matching the desktop file ID.
func findDesktopFile(desktopFileExists fileExists, baseDir string, splitFileId []string) (string, error) {
	desktopFile := filepath.Join(baseDir, strings.Join(splitFileId, "-"))

	if desktopFileExists(desktopFile) {
		return desktopFile, nil
	}

	// Iterate through the potential subdirectories formed by the first i elements of the desktop file ID.
	// Maybe this is overkill: At the time of writing, the only use is in desktopFileIDToFilename() and there
	// we're only checking dirs.SnapDesktopFilesDir (not all entries in $XDG_DATA_DIRS) and we know that snapd
	// does not create subdirectories in that location.
	for i := 1; i != len(splitFileId); i++ {
		desktopFile, err := findDesktopFile(desktopFileExists, filepath.Join(baseDir, strings.Join(splitFileId[:i], "-")), splitFileId[i:])
		if err == nil {
			return desktopFile, err
		}
	}

	return "", fmt.Errorf("could not find desktop file")
}

// desktopFileIDToFilename determines the path associated with a desktop file ID.
func desktopFileIDToFilename(desktopFileExists fileExists, desktopFileID string) (string, error) {

	// OpenDesktopEntry() currently only supports launching snap applications from
	// desktop files in /var/lib/snapd/desktop/applications and these desktop files are
	// written by snapd and considered safe for userd to process.
	// Since we only access /var/lib/snapd/desktop/applications, ignore
	// https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html
	baseDir := dirs.SnapDesktopFilesDir

	desktopFile, err := findDesktopFile(desktopFileExists, baseDir, strings.Split(desktopFileID, "-"))

	if err == nil {
		return desktopFile, nil
	}

	return "", fmt.Errorf("cannot find desktop file for %q", desktopFileID)
}

// verifyDesktopFileLocation checks the desktop file location:
// we only consider desktop files in dirs.SnapDesktopFilesDir
func verifyDesktopFileLocation(desktopFile string) error {
	if !strings.HasPrefix(desktopFile, dirs.SnapDesktopFilesDir+"/") {
		// We currently only support launching snap applications from desktop files in
		// /var/lib/snapd/desktop/applications and these desktop files are written by snapd and
		// considered safe for userd to process. If other directories are added in the future,
		// verifyDesktopFileLocation() and parseExecCommand() may need to be updated.
		return fmt.Errorf("only launching snap applications from /var/lib/snapd/desktop/applications is supported")
	}

	if filepath.Clean(desktopFile) != desktopFile {
		return fmt.Errorf("desktop file has unclean path: %q", desktopFile)
	}

	return nil
}

// readExecCommandFromDesktopFile parses the desktop file to get the Exec entry and
// checks that the BAMF_DESKTOP_FILE_HINT is present and refers to the desktop file.
func readExecCommandFromDesktopFile(desktopFile string) (string, error) {
	var launch string

	file, err := os.Open(desktopFile)
	if err != nil {
		return launch, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)

	in_desktop_section := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "[Desktop Entry]" {
			in_desktop_section = true
		} else if strings.HasPrefix(line, "[Desktop Action ") {
			// maybe later we'll add support here
			in_desktop_section = false
		} else if strings.HasPrefix(line, "[") {
			in_desktop_section = false
		} else if in_desktop_section && strings.HasPrefix(line, "Exec=") {
			launch = strings.TrimPrefix(line, "Exec=")
			break
		}
	}

	expectedPrefix := fmt.Sprintf("env BAMF_DESKTOP_FILE_HINT=%s /snap/bin/", desktopFile)
	if !strings.HasPrefix(launch, expectedPrefix) {
		return "", fmt.Errorf("Desktop file %q has an unsupported 'Exec' value: %q", desktopFile, launch)
	}

	return launch, nil
}

// Parse the Exec command by stripping any exec variables.
// Passing exec variables (eg, %foo) between confined snaps is unsupported. Currently,
// we do not have support for passing them in the D-Bus API but there are security
// implications that must be thought through regarding the influence of the launching
// snap over the launcher wrt exec variables. For now we simply filter them out.
// https://standards.freedesktop.org/desktop-entry-spec/desktop-entry-spec-latest.html#exec-variables
func parseExecCommand(exec_command string) ([]string, error) {
	args, err := shlex.Split(exec_command)
	if err != nil {
		return []string{}, err
	}

	i := 0
	for {
		// We want to keep literal '%' (expressed as '%%') but filter our exec variables
		// like '%foo'
		if strings.HasPrefix(args[i], "%%") {
			args[i] = strings.TrimPrefix(args[i], "%")
			i++
		} else if strings.HasPrefix(args[i], "%") {
			switch args[i] {
			case "%f", "%F", "%u", "%U", "%i":
				args = append(args[:i], args[i+1:]...)
			default:
				return []string{}, fmt.Errorf("cannot run %q", exec_command)
			}
		} else {
			i++
		}

		if i == len(args) {
			break
		}
	}
	return args, nil
}

// fdToFilename determines the path associated with an open file descriptor.
//
// The file descriptor cannot be opened using O_PATH and must refer to
// a regular file or to a directory. The symlink at /proc/self/fd/<fd>
// is read to determine the filename. The descriptor is also fstat'ed
// and the resulting device number and inode number are compared to, "%d", "%D", "%n", "%N",
// stat on the path determined earlier. The numbers must match.
func fdToFilename(fd int) (string, error) {
	flags, err := sys.FcntlGetFl(fd)
	if err != nil {
		return "", err
	}
	// File descriptors opened with O_PATH do not imply access to
	// the file in question.
	if flags&sys.O_PATH != 0 {
		return "", fmt.Errorf("cannot use file descriptors opened using O_PATH")
	}

	// Determine the file name associated with the passed file descriptor.
	filename, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		return "", err
	}

	var fileStat, fdStat syscall.Stat_t
	if err := syscall.Stat(filename, &fileStat); err != nil {
		return "", err
	}
	if err := syscall.Fstat(fd, &fdStat); err != nil {
		return "", err
	}

	// Sanity check to ensure we've got the right file
	if fdStat.Dev != fileStat.Dev || fdStat.Ino != fileStat.Ino {
		return "", fmt.Errorf("cannot determine file name")
	}

	fileType := fileStat.Mode & syscall.S_IFMT
	if fileType != syscall.S_IFREG && fileType != syscall.S_IFDIR {
		return "", fmt.Errorf("cannot open anything other than regular files or directories")
	}

	return filename, nil
}

func (s *PrivilegedDesktopLauncher) OpenFile(parentWindow string, clientFd dbus.UnixFD, sender dbus.Sender) *dbus.Error {
	// godbus transfers ownership of this file descriptor to us
	fd := int(clientFd)
	defer syscall.Close(fd)

	filename, err := fdToFilename(fd)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	snap, err := snapFromSender(s.conn, sender)
	if err != nil {
		return dbus.MakeFailedError(err)
	}
	dialog, err := ui.New()
	if err != nil {
		return dbus.MakeFailedError(err)
	}
	answeredYes := dialog.YesNo(
		i18n.G("Allow opening file?"),
		fmt.Sprintf(i18n.G("Allow snap %q to open file %q?"), snap, filename),
		&ui.DialogOptions{
			Timeout: 5 * 60 * time.Second,
			Footer:  i18n.G("This dialog will close automatically after 5 minutes of inactivity."),
		},
	)
	if !answeredYes {
		return dbus.MakeFailedError(fmt.Errorf("permission denied"))
	}

	if err = exec.Command("xdg-open", filename).Run(); err != nil {
		return dbus.MakeFailedError(fmt.Errorf("cannot open supplied URL"))
	}

	return nil
}
