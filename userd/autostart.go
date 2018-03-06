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

package userd

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil/shlex"
)

var (
	replacedDesktopKeys = []string{"%f", "%F", "%u", "%U", "%d", "%D",
		"%n", "%N", "%i", "%c", "%k", "%v", "%m"}
)

func findExec(desktopFileContent []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewBuffer(desktopFileContent))
	execCmd := ""
	for scanner.Scan() {
		bline := scanner.Bytes()

		if !bytes.HasPrefix(bline, []byte("Exec=")) {
			continue
		}

		execCmd = string(bline[len("Exec="):])
		for _, key := range replacedDesktopKeys {
			execCmd = strings.Replace(execCmd, key, "", -1)
		}
		break
	}

	execCmd = strings.TrimSpace(execCmd)
	if execCmd == "" {
		return "", fmt.Errorf("Exec not found or invalid")
	}
	return execCmd, nil
}

func getCurrentSnapInfo(snapName string) (*snap.Info, error) {
	curFn := filepath.Join(dirs.SnapMountDir, snapName, "current")
	realFn, err := os.Readlink(curFn)
	if err != nil {
		return nil, fmt.Errorf("cannot find current revision for snap %s: %s", snapName, err)
	}
	rev := filepath.Base(realFn)
	revision, err := snap.ParseRevision(rev)
	if err != nil {
		return nil, fmt.Errorf("cannot read revision %s: %s", rev, err)
	}

	return snap.ReadInfo(snapName, &snap.SideInfo{Revision: revision})
}

func tryAutostartApp(snapName, desktopFilePath string) (*exec.Cmd, error) {
	desktopFile := filepath.Base(desktopFilePath)

	info, err := getCurrentSnapInfo(snapName)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain snap information for snap %q: %v", snapName, err)
	}

	var app *snap.AppInfo
	for _, candidate := range info.Apps {
		if candidate.Autostart == desktopFile {
			app = candidate
			break
		}
	}
	if app == nil {
		return nil, fmt.Errorf("failed to match desktop file %q with snap %q applications", desktopFile, snapName)
	}

	content, err := ioutil.ReadFile(desktopFilePath)
	if err != nil {
		return nil, err
	}

	command, err := findExec(content)
	if err != nil {
		return nil, fmt.Errorf("failed to determine startup command: %v", err)
	}
	logger.Debugf("exec line: %v", command)

	split, err := shlex.Split(command)
	if err != nil {
		return nil, fmt.Errorf("invalid application startup command: %v", err)
	}

	cmd := exec.Command(app.WrapperPath(), split[1:]...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to autostart %q: %v", desktopFile, err)
	}
	return cmd, nil
}

type failedAutostartError map[string]error

func (f failedAutostartError) Error() string {
	var out bytes.Buffer
	for app, err := range f {
		fmt.Fprintf(&out, "- %q: %v\n", app, err)
	}
	return out.String()
}

var userCurrent = user.Current

// AutostartSessionApps starts applications which have placed their desktop
// files in $SNAP_USER_DATA/.config/autostart
func AutostartSessionApps() error {
	usr, err := userCurrent()
	if err != nil {
		return err
	}

	usrSnapDir := filepath.Join(usr.HomeDir, "snap")

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

		if _, err := tryAutostartApp(snapName, desktopFilePath); err != nil {
			logger.Debugf("error encountered when trying to autostart %v for snap %q: %v", desktopFile, snapName, err)
			failedApps[desktopFile] = err
		}
	}
	if len(failedApps) > 0 {
		return failedApps
	}
	return nil
}
