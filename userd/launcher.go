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
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/godbus/dbus"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/userd/ui"
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
	<method name="OpenFile">
		<arg type="s" name="parent_window" direction="in"/>
		<arg type="h" name="fd" direction="in"/>
	</method>
</interface>`

var (
	allowedURLSchemes = []string{"http", "https", "mailto", "snap"}
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
func (s *Launcher) OpenURL(addr string) *dbus.Error {
	u, err := url.Parse(addr)
	if err != nil {
		return &dbus.ErrMsgInvalidArg
	}

	if !strutil.ListContains(allowedURLSchemes, u.Scheme) {
		return makeAccessDeniedError(fmt.Errorf("Supplied URL scheme %q is not allowed", u.Scheme))
	}

	if err = exec.Command("xdg-open", addr).Run(); err != nil {
		return dbus.MakeFailedError(fmt.Errorf("cannot open supplied URL"))
	}

	return nil
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
