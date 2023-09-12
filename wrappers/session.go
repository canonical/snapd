// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package wrappers

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

var isValidSessionFileLine = regexp.MustCompile(strings.Join([]string{
	// NOTE (mostly to self): as much as possible keep the
	// individual regexp simple, optimize for legibility
	//
	// empty lines and comments
	`^\s*$`,
	`^\s*#`,
	// headers
	`^\[Desktop Entry\]$`,
	// https://specifications.freedesktop.org/desktop-entry-spec/latest/ar01s05.html
	"^Type=",
	"^Version=",
	"^Name" + localizedSuffix,
	"^Comment" + localizedSuffix,
	"^DesktopNames=",
	"^Exec=",
	// Note that we do not support TryExec, it does not make sense
	// in the snap context
	"^X-GDM-BypassXsession=",
	// Ubuntu extension
	"^X-GDM-SessionRegisters=",
}, "|")).Match

func findCommand(s *snap.Info, cmd string) (string, error) {
	// Disallow anything that might try and get out of the snap path.
	if strings.Contains(filepath.Clean(cmd), "..") {
		return "", fmt.Errorf("exec command has non-local path: %q", cmd)
	}
	if filepath.IsAbs(cmd) {
		return "", fmt.Errorf("exec command has absolute path: %q", cmd)
	}

	// Check if is in the snap directory.
	baseDir := s.MountDir()
	path := filepath.Join(baseDir, cmd)
	_, err := os.Stat(path)
	if err == nil {
		return path, nil
	}

	// Check if in one of the standard directories that are in the path.
	for _, dir := range []string{"bin", "usr/bin"} {
		path := filepath.Join(baseDir, dir, cmd)
		_, err := os.Stat(path)
		if err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("invalid exec command: %q", cmd)
}

// rewriteExecLine rewrites a "Exec=" line to use the session wrapper.
func rewriteSessionExecLine(s *snap.Info, sessionFile, line string) (string, error) {
	cmd := strings.SplitN(line, "=", 2)[1]

	// The executable must be provided by this snap
	var err error
	absCmd, err := findCommand(s, cmd)
	if err != nil {
		return "", err
	}

	newExec := fmt.Sprintf("Exec=ubuntu-core-desktop-session-wrapper %s", absCmd)
	logger.Noticef("rewriting desktop file %q to %q", sessionFile, newExec)

	return newExec, nil
}

func sanitizeSessionFile(s *snap.Info, sessionFile string, rawcontent []byte) []byte {
	var newContent bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(rawcontent))
	for i := 0; scanner.Scan(); i++ {
		bline := scanner.Bytes()

		if !isValidSessionFileLine(bline) {
			logger.Debugf("ignoring line %d (%q) in source of session file %q", i, bline, filepath.Base(sessionFile))
			continue
		}

		// rewrite exec lines to use the session wrapper.
		if bytes.HasPrefix(bline, []byte("Exec=")) {
			var err error
			line, err := rewriteSessionExecLine(s, sessionFile, string(bline))
			if err != nil {
				// something went wrong, ignore the line
				continue
			}
			bline = []byte(line)
		}

		newContent.Grow(len(bline) + 1)
		newContent.Write(bline)
		newContent.WriteByte('\n')

		// insert snap name
		if bytes.Equal(bline, []byte("[Desktop Entry]")) {
			newContent.Write([]byte("X-SnapInstanceName=" + s.InstanceName() + "\n"))
		}
	}

	return newContent.Bytes()
}

func deriveSessionFilesContent(s *snap.Info) (map[string]osutil.FileState, error) {
	baseDir := s.MountDir()
	sessionFiles, err := filepath.Glob(filepath.Join(baseDir, "usr/share/wayland-sessions", "*.desktop"))
	if err != nil {
		return nil, fmt.Errorf("cannot get wayland sessions for %v: %s", baseDir, err)
	}

	content := make(map[string]osutil.FileState)
	for _, df := range sessionFiles {
		base := filepath.Base(df)
		fileContent, err := ioutil.ReadFile(df)
		if err != nil {
			return nil, err
		}
		// FIXME: don't blindly use the snap desktop filename, mangle it
		// but we can't just use the app name because a desktop file
		// may call the same app with multiple parameters, e.g.
		// --create-new, --open-existing etc
		base = fmt.Sprintf("%s_%s", s.DesktopPrefix(), base)
		installedDesktopFileName := filepath.Join(dirs.SnapDesktopFilesDir, base)
		fileContent = sanitizeDesktopFile(s, installedDesktopFileName, fileContent)
		content[base] = &osutil.MemoryFileState{
			Content: fileContent,
			Mode:    0644,
		}
	}
	return content, nil
}

// EnsureSnapSessionFiles puts in place the session files from the snap.
//
// It also removes session files from the applications of the old snap revision to ensure
// that only new snap session files exist.
func EnsureSnapSessionFiles(s *snap.Info) (err error) {
	// Session files only supported on core desktop.
	if !release.OnCoreDesktop {
		return nil
	}

	// Desktop slot required to be allowed to provide sessions.
	if s.Slots["desktop"] == nil {
		return nil
	}

	if err := os.MkdirAll(dirs.SnapWaylandSessionsDir, 0755); err != nil {
		return err
	}

	content, err := deriveSessionFilesContent(s)
	if err != nil {
		return err
	}

	sessionFilesGlob := fmt.Sprintf("%s_*.desktop", s.DesktopPrefix())
	_, _, err = osutil.EnsureDirState(dirs.SnapWaylandSessionsDir, sessionFilesGlob, content)
	if err != nil {
		return err
	}

	return nil
}

// RemoveSnapSessionFiles removes the added session files for the snap.
func RemoveSnapSessionFiles(s *snap.Info) error {
	if !osutil.IsDirectory(dirs.SnapWaylandSessionsDir) {
		return nil
	}

	sessionFilesGlob := fmt.Sprintf("%s_*.desktop", s.DesktopPrefix())
	_, _, err := osutil.EnsureDirState(dirs.SnapWaylandSessionsDir, sessionFilesGlob, nil)
	if err != nil {
		return err
	}

	return nil
}
