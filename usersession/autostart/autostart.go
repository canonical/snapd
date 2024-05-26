// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package autostart

import (
	"bytes"
	"fmt"
	"log/syslog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/desktop/desktopentry"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

var currentDesktop = strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":")

func autostartCmd(snapName, desktopFilePath string) (*exec.Cmd, error) {
	desktopFile := filepath.Base(desktopFilePath)

	info := mylog.Check2(snap.ReadCurrentInfo(snapName))

	var app *snap.AppInfo
	for _, candidate := range info.Apps {
		if candidate.Autostart == desktopFile {
			app = candidate
			break
		}
	}
	if app == nil {
		return nil, fmt.Errorf("cannot match desktop file with snap %s applications", snapName)
	}

	de := mylog.Check2(desktopentry.Read(desktopFilePath))

	if !de.ShouldAutostart(currentDesktop) {
		return nil, fmt.Errorf("skipped")
	}
	args := mylog.Check2(de.ExpandExec(nil))

	logger.Debugf("exec line: %v", args)

	// NOTE: Ignore the actual argv[0] in Exec=.. line and replace it with a
	// command of the snap application. Any arguments passed in the Exec=..
	// line to the original command are preserved.
	cmd := exec.Command(app.WrapperPath(), args[1:]...)
	return cmd, nil
}

// failedAutostartError keeps track of errors that occurred when starting an
// application for a specific desktop file, desktop file name is as a key
type failedAutostartError map[string]error

func (f failedAutostartError) Error() string {
	var out bytes.Buffer

	dfiles := make([]string, 0, len(f))
	for desktopFile := range f {
		dfiles = append(dfiles, desktopFile)
	}
	sort.Strings(dfiles)
	for _, desktopFile := range dfiles {
		fmt.Fprintf(&out, "- %q: %v\n", desktopFile, f[desktopFile])
	}
	return out.String()
}

func makeStdStreams(identifier string) (stdout *os.File, stderr *os.File) {
	stdout = mylog.Check2(systemd.NewJournalStreamFile(identifier, syslog.LOG_INFO, false))

	stderr = mylog.Check2(systemd.NewJournalStreamFile(identifier, syslog.LOG_WARNING, false))

	return stdout, stderr
}

var userCurrent = user.Current

func MockUserCurrent(f func() (*user.User, error)) (restore func()) {
	osutil.MustBeTestBinary("mocking can only be done in tests")
	old := userCurrent
	userCurrent = f
	return func() {
		userCurrent = old
	}
}

// AutostartSessionApps starts applications which have placed their desktop
// files in $SNAP_USER_DATA/.config/autostart. Takes a path to the user's snap dir.
//
// NOTE: By the spec, the actual path is $SNAP_USER_DATA/${XDG_CONFIG_DIR}/autostart
func AutostartSessionApps(usrSnapDir string) error {
	glob := filepath.Join(usrSnapDir, "*/current/.config/autostart/*.desktop")
	matches := mylog.Check2(filepath.Glob(glob))

	failedApps := make(failedAutostartError)
	for _, desktopFilePath := range matches {
		desktopFile := filepath.Base(desktopFilePath)
		logger.Debugf("autostart desktop file %v", desktopFile)

		// /home/foo/snap/some-snap/current/.config/autostart/some-app.desktop ->
		//    some-snap/current/.config/autostart/some-app.desktop
		noHomePrefix := strings.TrimPrefix(desktopFilePath, usrSnapDir+"/")
		// some-snap/current/.config/autostart/some-app.desktop -> some-snap
		snapName := noHomePrefix[0:strings.IndexByte(noHomePrefix, '/')]

		logger.Debugf("snap name: %q", snapName)

		cmd := mylog.Check2(autostartCmd(snapName, desktopFilePath))

		// similarly to gnome-session, use the desktop file name as
		// identifier, see:
		// https://github.com/GNOME/gnome-session/blob/099c19099de8e351f6cc0f2110ad27648780a0fe/gnome-session/gsm-autostart-app.c#L948
		cmd.Stdout, cmd.Stderr = makeStdStreams(desktopFile)
		mylog.Check(cmd.Start())

	}
	if len(failedApps) > 0 {
		return failedApps
	}
	return nil
}
