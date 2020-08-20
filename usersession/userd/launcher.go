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
	"github.com/snapcore/snapd/osutil"
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
		<arg type='s' name='desktopFileID' direction='in'/>
		<arg type='as' name='env' direction='in'/>
	</method>
	<method name="OpenFile">
		<arg type="s" name="parent_window" direction="in"/>
		<arg type="h" name="fd" direction="in"/>
	</method>
</interface>`

// allowedURLSchemes are those that can be passed to xdg-open so that it may
// launch the handler for the url scheme on behalf of the snap (and therefore
// outside of the calling snap's confinement). Historically we've been
// conservative about adding url schemes but the thinking was refined in
// https://github.com/snapcore/snapd/pull/7731#pullrequestreview-362900171
//
// The current criteria for adding url schemes is:
// * understanding and documenting the scheme in this file
// * the scheme itself does not cause xdg-open to open files (eg, file:// or
//   matching '^[[:alpha:]+\.\-]+:' (from xdg-open source))
// * verifying that the recipient of the url (ie, what xdg-open calls) won't
//   process file paths/etc that can be leveraged to break out of the sandbox
//   (but understanding how the url can drive the recipient application is
//   important)
//
// This code uses golang's net/url.Parse() which will help ensure the url is
// ok before passing to xdg-open. xdg-open itself properly quotes the url so
// shell metacharacters are blocked.
var (
	allowedURLSchemes = []string{
		// apt: the scheme allows specifying a package for xdg-open to pass to an
		//   apt-handling application, like gnome-software, apturl, etc which are all
		//   protected by policykit
		//   - scheme: apt:<name of package>
		//   - https://github.com/snapcore/snapd/pull/7731
		"apt",
		// help: the scheme allows for specifying a help URL. This code ensures that
		//   the url is parseable
		//   - scheme: help://topic
		//   - https://github.com/snapcore/snapd/pull/6493
		"help",
		// http/https: the scheme allows specifying a web URL. This code ensures that
		//   the url is parseable
		//   - scheme: http(s)://example.com
		"http",
		"https",
		// mailto: the scheme allows for specifying an email address
		//   - scheme: mailto:foo@example.com
		"mailto",
		// msteams: the scheme is a thin wrapper around https.
		//   - scheme: msteams:...
		//   - https://github.com/snapcore/snapd/pull/8761
		"msteams",
		// TODO: document slack URL scheme.
		"slack",
		// snap: the scheme allows specifying a package for xdg-open to pass to a
		//   snap-handling installer application, like snap-store, etc which are
		//   protected by policykit/snap login
		//   - https://github.com/snapcore/snapd/pull/5181
		"snap",
		// zoommtg: the scheme is a modified web url scheme
		//   - scheme: https://medium.com/zoom-developer-blog/zoom-url-schemes-748b95fd9205
		//     (eg, zoommtg://zoom.us/...)
		//   - https://github.com/snapcore/snapd/pull/8304
		"zoommtg",
		// zoomphonecall: another zoom URL scheme, for dialing phone numbers
		//   - https://github.com/snapcore/snapd/pull/8910
		"zoomphonecall",
		// zoomus: alternative name for zoommtg
		//   - https://github.com/snapcore/snapd/pull/8910
		"zoomus",
	}

	// allowedEnvVars are those environment variables that snaps who have access
	// to OpenDesktopEntryEnv() can set for the launched snap's environment.
	// - DISPLAY: set the X11 display
	// - WAYLAND_DISPLAY: set the wayland display
	// - XDG_CURRENT_DESKTOP: set identifiers for desktop environments
	// - XDG_SESSION_DESKTOP: set identifier for the desktop environment
	// - XDG_SESSION_TYPE: set the session type (e.g. "wayland" or "x11")
	allowedEnvVars = []string{"DISPLAY", "WAYLAND_DISPLAY", "XDG_CURRENT_DESKTOP", "XDG_SESSION_DESKTOP", "XDG_SESSION_TYPE"}
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
// DBus interface. The desktopFileID is described here:
// https://standards.freedesktop.org/desktop-entry-spec/desktop-entry-spec-latest.html#desktop-file-id
func (s *Launcher) OpenDesktopEntryEnv(desktopFileID string, env []string, sender dbus.Sender) *dbus.Error {
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

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = os.Environ()
	for _, e := range env {
		if !strutil.ListContains(allowedEnvVars, strings.SplitN(e, "=", 2)[0]) {
			return dbus.MakeFailedError(fmt.Errorf("Supplied environment variable %q is not allowed", e))
		}

		cmd.Env = append(cmd.Env, e)
	}

	// XXX: this avoids defunct processes but causes userd to persist
	// until all children are gone (currently, this is not a problem since
	// userd is long running once started)
	go cmd.Wait()

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

	// OpenDesktopEntryEnv() currently only supports launching snap applications from
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

// verifyDesktopFileLocation checks the desktop file location and access.
// 1. we only consider desktop files in dirs.SnapDesktopFilesDir
// 2. the desktop file itself and all directories above it are root owned without group/other write
// 3. the Exec line has an expected prefix
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
