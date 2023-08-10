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

	"github.com/snapcore/snapd/desktop/desktopentry"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

var (
	currentDesktop = strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":")
)

func autostartCmd(snapName, desktopFilePath string) (*exec.Cmd, error) {
	desktopFile := filepath.Base(desktopFilePath)

	info, err := snap.ReadCurrentInfo(snapName)
	if err != nil {
		return nil, err
	}

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

	de, err := desktopentry.Read(desktopFilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot parse desktop file for application %s in snap %s: %v", app.Name, snapName, err)
	}
	if !de.ShouldAutostart(currentDesktop) {
		return nil, fmt.Errorf("skipped")
	}
	args, err := de.ExpandExec(nil)
	if err != nil {
		return nil, fmt.Errorf("invalid application startup command: %v", err)
	}
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
	var err error

	stdout, err = systemd.NewJournalStreamFile(identifier, syslog.LOG_INFO, false)
	if err != nil {
		logger.Noticef("failed to set up stdout journal stream for %q: %v", identifier, err)
		stdout = os.Stdout
	}

	stderr, err = systemd.NewJournalStreamFile(identifier, syslog.LOG_WARNING, false)
	if err != nil {
		logger.Noticef("failed to set up stderr journal stream for %q: %v", identifier, err)
		stderr = os.Stderr
	}

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
	matches, err := filepath.Glob(glob)
	if err != nil {
		return err
	}

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

		cmd, err := autostartCmd(snapName, desktopFilePath)
		if err != nil {
			failedApps[desktopFile] = err
			continue
		}

		// similarly to gnome-session, use the desktop file name as
		// identifier, see:
		// https://github.com/GNOME/gnome-session/blob/099c19099de8e351f6cc0f2110ad27648780a0fe/gnome-session/gsm-autostart-app.c#L948
		cmd.Stdout, cmd.Stderr = makeStdStreams(desktopFile)
		if err := cmd.Start(); err != nil {
			failedApps[desktopFile] = fmt.Errorf("cannot autostart %q: %v", desktopFile, err)
		}
	}
	if len(failedApps) > 0 {
		return failedApps
	}
	return nil
}
