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

package osutil

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
)

func libraryPathForCore(confPath string) []string {
	root := filepath.Join(dirs.SnapMountDir, "/core/current")

	f, err := os.Open(filepath.Join(root, confPath))
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "#"):
			// nothing
		case strings.TrimSpace(line) == "":
			// nothing
		case strings.HasPrefix(line, "include "):
			l := strings.SplitN(line, "include ", 2)
			files, err := filepath.Glob(filepath.Join(root, l[1]))
			if err != nil {
				return nil
			}
			for _, f := range files {
				out = append(out, libraryPathForCore(f[len(root):])...)
			}
		default:
			out = append(out, filepath.Join(root, line))
		}

	}
	if err := scanner.Err(); err != nil {
		return nil
	}

	return out
}

func CommandFromCore(name string, arg ...string) *exec.Cmd {
	root := filepath.Join(dirs.SnapMountDir, "/core/current")

	coreLdSo := filepath.Join(root, "/lib/ld-linux.so.2")
	cmdPath := filepath.Join(root, name)

	ldLibraryPathForCore := libraryPathForCore("/etc/ld.so.conf")
	ldSoArgs := []string{"--library-path", strings.Join(ldLibraryPathForCore, ":"), cmdPath}

	allArgs := append(ldSoArgs, arg...)
	return exec.Command(coreLdSo, allArgs...)
}
