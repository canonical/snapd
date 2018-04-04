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
	"sort"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil/shlex"
)

// expandDesktopFields processes the input string and expands any %<char>
// patterns. '%%' expands to '%', all other patterns expand to empty strings.
func expandDesktopFields(in string) string {
	raw := []rune(in)
	out := make([]rune, 0, len(raw))

	var hasKey bool
	for _, r := range raw {
		if hasKey {
			hasKey = false
			// only allow %% -> % expansion, drop other keys
			if r == '%' {
				out = append(out, r)
			}
			continue
		} else if r == '%' {
			hasKey = true
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func findExec(desktopFileContent []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewBuffer(desktopFileContent))
	execCmd := ""
	for scanner.Scan() {
		bline := scanner.Bytes()

		if !bytes.HasPrefix(bline, []byte("Exec=")) {
			continue
		}

		full := string(bline[len("Exec="):])
		execCmd = expandDesktopFields(full)
		break
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	execCmd = strings.TrimSpace(execCmd)
	if execCmd == "" {
		return "", fmt.Errorf("Exec not found or invalid")
	}
	return execCmd, nil
}

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
		return nil, fmt.Errorf("cannot match desktop file with snap %q applications", snapName)
	}

	content, err := ioutil.ReadFile(desktopFilePath)
	if err != nil {
		return nil, err
	}

	// NOTE: Ignore all fields and just look for Exec=..., this also means
	// that fields with meaning such as TryExec, X-GNOME-Autostart and so on
	// are ignored

	command, err := findExec(content)
	if err != nil {
		return nil, fmt.Errorf("cannot determine startup command: %v", err)
	}
	logger.Debugf("exec line: %v", command)

	split, err := shlex.Split(command)
	if err != nil {
		return nil, fmt.Errorf("invalid application startup command: %v", err)
	}

	// NOTE: Ignore the actual argv[0] in Exec=.. line and replace it with a
	// command of the snap application. Any arguments passed in the Exec=..
	// line to the original command are preserved.
	cmd := exec.Command(app.WrapperPath(), split[1:]...)
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

var userCurrent = user.Current

// AutostartSessionApps starts applications which have placed their desktop
// files in $SNAP_USER_DATA/.config/autostart
//
// NOTE: By the spec, the actual path is $SNAP_USER_DATA/${XDG_CONFIG_DIR}/autostart
func AutostartSessionApps() error {
	usr, err := userCurrent()
	if err != nil {
		return err
	}

	usrSnapDir := filepath.Join(usr.HomeDir, dirs.UserHomeSnapDir)

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
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		if err := cmd.Start(); err != nil {
			failedApps[desktopFile] = fmt.Errorf("cannot autostart %q: %v", desktopFile, err)
		}
	}
	if len(failedApps) > 0 {
		return failedApps
	}
	return nil
}
