// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"strings"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

// valid simple prefixes
var validDesktopFilePrefixes = []string{
	// headers
	"[Desktop Entry]",
	"[Desktop Action ",
	// https://specifications.freedesktop.org/desktop-entry-spec/latest/ar01s05.html
	"Type=",
	"Version=",
	"Name=",
	"GenericName=",
	"NoDisplay=",
	"Comment=",
	"Icon=",
	"Hidden=",
	"OnlyShowIn=",
	"NotShowIn=",
	"Exec=",
	// Note that we do not support TryExec, it does not make sense
	// in the snap context
	"Terminal=",
	"Actions=",
	"MimeType=",
	"Categories=",
	"Keywords=",
	"StartupNotify=",
	"StartupWMClass=",
}

// name desktop file keys are localized as key[LOCALE]=:
//   lang_COUNTRY@MODIFIER
//   lang_COUNTRY
//   lang@MODIFIER
//   lang
var validLocalizedDesktopFilePrefixes = []string{
	"Name",
	"GenericName",
	"Comment",
	"Keywords",
}

func isValidDesktopFilePrefix(line string) bool {
	for _, prefix := range validDesktopFilePrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}

	return false
}

func trimLang(s string) string {
	const langChars = "@_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	if s == "" || s[0] != '[' {
		return s
	}
	t := strings.TrimLeft(s[1:], langChars)
	if t != "" && t[0] == ']' {
		return t[1:]
	}
	return s
}

func isValidLocalizedDesktopFilePrefix(line string) bool {
	for _, prefix := range validLocalizedDesktopFilePrefixes {
		s := strings.TrimPrefix(line, prefix)
		if s == line {
			continue
		}
		if strings.HasPrefix(trimLang(s), "=") {
			return true
		}
	}
	return false
}

// rewriteExecLine rewrites a "Exec=" line to use the wrapper path for snap application.
func rewriteExecLine(s *snap.Info, line string) (string, error) {
	cmd := strings.SplitN(line, "=", 2)[1]
	for _, app := range s.Apps {
		wrapper := app.WrapperPath()
		validCmd := filepath.Base(wrapper)
		// check the prefix to allow %flag style args
		// this is ok because desktop files are not run through sh
		// so we don't have to worry about the arguments too much
		if cmd == validCmd {
			return "Exec=" + wrapper, nil
		} else if strings.HasPrefix(cmd, validCmd+" ") {
			return fmt.Sprintf("Exec=%s%s", wrapper, line[len("Exec=")+len(validCmd):]), nil
		}
	}

	return "", fmt.Errorf("invalid exec command: %q", cmd)
}

func sanitizeDesktopFile(s *snap.Info, rawcontent []byte) []byte {
	newContent := []string{}

	scanner := bufio.NewScanner(bytes.NewReader(rawcontent))
	for scanner.Scan() {
		line := scanner.Text()

		// whitespace/comments are just copied
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			newContent = append(newContent, line)
			continue
		}

		// ignore everything we have not whitelisted
		if !isValidDesktopFilePrefix(line) && !isValidLocalizedDesktopFilePrefix(line) {
			continue
		}
		// rewrite exec lines to an absolute path for the binary
		if strings.HasPrefix(line, "Exec=") {
			var err error
			line, err = rewriteExecLine(s, line)
			if err != nil {
				// something went wrong, ignore the line
				continue
			}
		}

		// do variable substitution
		line = strings.Replace(line, "${SNAP}", s.MountDir(), -1)
		newContent = append(newContent, line)
	}

	return []byte(strings.Join(newContent, "\n"))
}

// AddSnapDesktopFiles puts in place the desktop files for the applications from the snap.
func AddSnapDesktopFiles(s *snap.Info) error {
	if err := os.MkdirAll(dirs.SnapDesktopFilesDir, 0755); err != nil {
		return err
	}

	baseDir := s.MountDir()

	desktopFiles, err := filepath.Glob(filepath.Join(baseDir, "meta", "gui", "*.desktop"))
	if err != nil {
		return fmt.Errorf("cannot get desktop files for %v: %s", baseDir, err)
	}

	for _, df := range desktopFiles {
		content, err := ioutil.ReadFile(df)
		if err != nil {
			return err
		}

		content = sanitizeDesktopFile(s, content)

		installedDesktopFileName := filepath.Join(dirs.SnapDesktopFilesDir, fmt.Sprintf("%s_%s", s.Name(), filepath.Base(df)))
		if err := osutil.AtomicWriteFile(installedDesktopFileName, []byte(content), 0755, 0); err != nil {
			return err
		}
	}

	return nil
}

// RemoveSnapDesktopFiles removes the added desktop files for the applications in the snap.
func RemoveSnapDesktopFiles(s *snap.Info) error {
	glob := filepath.Join(dirs.SnapDesktopFilesDir, s.Name()+"_*.desktop")
	activeDesktopFiles, err := filepath.Glob(glob)
	if err != nil {
		return fmt.Errorf("cannot get desktop files for %v: %s", glob, err)
	}
	for _, f := range activeDesktopFiles {
		os.Remove(f)
	}

	return nil
}
