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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

var validServiceFilePrefixes = []string{
	// headers
	"[D-BUS Service]",
	"Name=",
	"Exec=",
}

func isValidServiceFilePrefix(line string) bool {
	for _, prefix := range validServiceFilePrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}

	return false
}

func sanitizeDBusServiceFile(s *snap.Info, serviceFile string, rawcontent []byte) []byte {
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
		if !isValidServiceFilePrefix(line) {
			continue
		}
		// rewrite exec lines to an absolute path for the binary
		if strings.HasPrefix(line, "Exec=") {
			var err error
			line, err = rewriteExecLine(s, serviceFile, line, "")
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

// AddSnapDBusServiceFile puts any dbus files in place
func AddSnapDBusServiceFiles(s *snap.Info) error {
	if err := os.MkdirAll(dirs.SnapDBusServicesFilesDir, 0755); err != nil {
		return err
	}

	baseDir := s.MountDir()

	servicesFiles, err := filepath.Glob(filepath.Join(baseDir, "meta", "gui", "*.service"))
	if err != nil {
		return fmt.Errorf("cannot get service files for %v: %s", baseDir, err)
	}

	for _, df := range servicesFiles {
		content, err := ioutil.ReadFile(df)
		if err != nil {
			return err
		}

		installedServiceFileName := filepath.Join(dirs.SnapDBusServicesFilesDir, fmt.Sprintf("%s_%s", s.Name(), filepath.Base(df)))
		content = sanitizeDBusServiceFile(s, installedServiceFileName, content)
		if err := osutil.AtomicWriteFile(installedServiceFileName, []byte(content), 0755, 0); err != nil {
			return err
		}
	}

	return nil
}

// RemoveSnapDBusServiceFiles removes the added services files for the applications in the snap.
func RemoveSnapDBusServiceFiles(s *snap.Info) error {
	glob := filepath.Join(dirs.SnapDBusServicesFilesDir, s.Name()+"_*.services")
	activeServicesFiles, err := filepath.Glob(glob)
	if err != nil {
		return fmt.Errorf("cannot get service files for %v: %s", glob, err)
	}
	for _, f := range activeServicesFiles {
		os.Remove(f)
	}

	return nil
}
