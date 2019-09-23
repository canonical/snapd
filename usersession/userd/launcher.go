// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/strutil/shlex"
	"github.com/snapcore/snapd/usersession/userd/ui"
)

const launcherIntrospectionXML = `
<interface name="org.freedesktop.DBus.Peer">
	<method name='Ping'>
	</method>
	<method name='GetMachineId'>
               <arg type='s' name='machine_uuid' direction='out'/>
	</method>
</interface>
<interface name='io.snapcraft.Launcher'>
	<method name='OpenURL'>
		<arg type='s' name='url' direction='in'/>
	</method>
	<method name='OpenDesktopEntryEnv'>
		<arg type='s' name='desktop_file_id' direction='in'/>
		<arg type='as' name='env' direction='in'/>
	</method>
	<method name="OpenFile">
		<arg type="s" name="parent_window" direction="in"/>
		<arg type="h" name="fd" direction="in"/>
	</method>
</interface>`

var (
	allowedURLSchemes = []string{"http", "https", "mailto", "snap", "help"}
	allowedEnvVars    = []string{"DISPLAY", "WAYLAND_DISPLAY", "XDG_CURRENT_DESKTOP", "XDG_SESSION_DESKTOP", "XDG_SESSION_TYPE"}
)

// Launcher implements the 'io.snapcraft.Launcher' DBus interface.
type Launcher struct {
	conn *dbus.Conn
}

// Name returns the name of the interface this object implements
func (s *Launcher) Name() string {
	return "io.snapcraft.Launcher"
}

// BasePath returns the base path of the object
func (s *Launcher) BasePath() dbus.ObjectPath {
	return "/io/snapcraft/Launcher"
}

// IntrospectionData gives the XML formatted introspection description
// of the DBus service.
func (s *Launcher) IntrospectionData() string {
	return launcherIntrospectionXML
}

func makeAccessDeniedError(err error) *dbus.Error {
	return &dbus.Error{
		Name: "org.freedesktop.DBus.Error.AccessDenied",
		Body: []interface{}{err.Error()},
	}
}

// OpenURL implements the 'OpenURL' method of the 'io.snapcraft.Launcher'
// DBus interface. Before the provided url is passed to xdg-open the scheme is
// validated against a list of allowed schemes. All other schemes are denied.
func (s *Launcher) OpenURL(addr string, sender dbus.Sender) *dbus.Error {
	u, err := url.Parse(addr)
	if err != nil {
		return &dbus.ErrMsgInvalidArg
	}

	if !strutil.ListContains(allowedURLSchemes, u.Scheme) {
		return makeAccessDeniedError(fmt.Errorf("Supplied URL scheme %q is not allowed", u.Scheme))
	}

	snap, err := snapFromSender(s.conn, sender)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	xdg_data_dirs := []string{}
	xdg_data_dirs = append(xdg_data_dirs, fmt.Sprintf(filepath.Join(dirs.SnapMountDir, snap, "current/usr/share")))
	for _, dir := range strings.Split(os.Getenv("XDG_DATA_DIRS"), ":") {
		xdg_data_dirs = append(xdg_data_dirs, dir)
	}

	cmd := exec.Command("xdg-open", addr)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("XDG_DATA_DIRS=%s", strings.Join(xdg_data_dirs, ":")))

	if err := cmd.Run(); err != nil {
		return dbus.MakeFailedError(fmt.Errorf("cannot open supplied URL"))
	}

	return nil
}

// OpenDesktopEntryEnv implements the 'OpenDesktopEntryEnv' method of the 'io.snapcraft.Launcher'
// DBus interface. The desktop_file_id is described here:
// https://standards.freedesktop.org/desktop-entry-spec/desktop-entry-spec-latest.html#desktop-file-id
func (s *Launcher) OpenDesktopEntryEnv(desktop_file_id string, env []string, sender dbus.Sender) *dbus.Error {
	desktop_file, err := desktopFileIdToFilename(desktop_file_id)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	exec_command, err := readExecCommandFromDesktopFile(desktop_file)

	if err != nil {
		return dbus.MakeFailedError(err)
	}

	args, err := shlex.Split(exec_command)
	if err != nil {
		return dbus.MakeFailedError(err)
	}

	i := 0
	for {
		if strings.HasPrefix(args[i], "%") {
			// Passing exec variables between confined snaps raises unanswered questions and they are not required
			// for the simple cases.  For now, we don't have support for passing them in the dbus API and drop
			// them from the command.
			// https://standards.freedesktop.org/desktop-entry-spec/desktop-entry-spec-latest.html#exec-variables
			args = append(args[:i], args[i+1:]...)
		} else {
			i++
		}

		if i == len(args) {
			break
		}
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = os.Environ()
	for _, e := range env {
		if !strutil.ListContains(allowedEnvVars, strings.SplitN(e, "=", 2)[0]) {
			return dbus.MakeFailedError(fmt.Errorf("Supplied environment variable %q is not allowed", e))
		}

		cmd.Env = append(cmd.Env, e)
	}

	if err := cmd.Start(); err != nil {
		return dbus.MakeFailedError(fmt.Errorf("cannot run %q", exec_command))
	}

	return nil
}

// findDesktopFile recursively tries each subdirectory that can be formed from the (split) desktop file ID.
func findDesktopFile(base_dir string, splitFileId []string) *string {
	desktop_file := filepath.Join(base_dir, strings.Join(splitFileId, "-"))
	fileStat, err := os.Stat(desktop_file)

	if err == nil  && !fileStat.IsDir() {
		return &desktop_file
	}

  // Iterate through the potential subdirectories formed by the first i elements of the desktop file ID
	for i := 1; i != len(splitFileId)-1; i++ {
		desktop_file := findDesktopFile(filepath.Join(base_dir, strings.Join(splitFileId[:i], "-")), splitFileId[i:])
		if desktop_file != nil {
			return desktop_file
		}
	}

	return nil
}

// desktopFileIdToFilename determines the path associated with a desktop file ID.
func desktopFileIdToFilename(desktop_file_id string) (string, error) {

	// Currently the caller only has access to /var/lib/snapd/desktop/applications/, so we just look there
	// and ignore https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html
	base_dir := dirs.SnapDesktopFilesDir

	desktop_file := findDesktopFile(base_dir, strings.Split(desktop_file_id, "-"))

	if desktop_file != nil {
		return *desktop_file, nil
	}

	return "", fmt.Errorf("cannot find desktop file for %q", desktop_file_id)
}

// readExecCommandFromDesktopFile parses the desktop file to get the Exec entry.
func readExecCommandFromDesktopFile(desktop_file string) (string, error) {
	var launch string

	file, err := os.Open(desktop_file)
	if err != nil {
		return launch, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)

	in_desktop_section := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return launch, err
		}

		line = strings.TrimSpace(line)

		if line == "[Desktop Entry]" {
			in_desktop_section = true
		} else if strings.HasPrefix(line, "[Desktop Action") {
			// maybe later we'll add support here
			in_desktop_section = false
		} else if strings.HasPrefix(line, "[") {
			in_desktop_section = false
		} else if in_desktop_section && strings.HasPrefix(line, "Exec=") {
			launch = strings.TrimPrefix(line, "Exec=")
			break
		}
	}

	return launch, nil
}

// fdToFilename determines the path associated with an open file descriptor.
//
// The file descriptor cannot be opened using O_PATH and must refer to
// a regular file or to a directory. The symlink at /proc/self/fd/<fd>
// is read to determine the filename. The descriptor is also fstat'ed
// and the resulting device number and inode number are compared to
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

func (s *Launcher) OpenFile(parentWindow string, clientFd dbus.UnixFD, sender dbus.Sender) *dbus.Error {
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
