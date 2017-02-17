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
	"bytes"
	"debug/elf"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
)

func parseCoreLdSoConf(confPath string) []string {
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
				out = append(out, parseCoreLdSoConf(f[len(root):])...)
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

func elfInterp(cmd string) (string, error) {
	el, err := elf.Open(cmd)
	if err != nil {
		return "", err
	}
	defer el.Close()

	for _, prog := range el.Progs {
		if prog.Type == elf.PT_INTERP {
			r := prog.Open()
			interp, err := ioutil.ReadAll(r)
			if err != nil {
				return "", nil
			}

			return string(bytes.Trim(interp, "\x00")), nil
		}
	}

	return "", fmt.Errorf("cannot find PT_INTERP header")
}

// CommandFromCore runs a command from the core snap using the proper
// interpreter and library paths.
//
// At the moment it can only run ELF files, expects a standard ld.so
// interpreter, and can't handle RPATH.
func CommandFromCore(name string, cmdArgs ...string) (*exec.Cmd, error) {
	root := filepath.Join(dirs.SnapMountDir, "/core/current")

	cmdPath := filepath.Join(root, name)
	interp, err := elfInterp(cmdPath)
	if err != nil {
		return nil, err
	}
	coreLdSo := filepath.Join(root, interp)
	// we cannot use EvalSymlink here because we need to resolve
	// relative and absolute symlinks differently. A absolute
	// symlink is relative to root of the core snap.
	seen := map[string]bool{}
	for IsSymlink(coreLdSo) {
		link, err := os.Readlink(coreLdSo)
		if err != nil {
			return nil, err
		}
		if filepath.IsAbs(link) {
			coreLdSo = filepath.Join(root, link)
		} else {
			coreLdSo = filepath.Join(filepath.Dir(coreLdSo), link)
		}
		if seen[coreLdSo] {
			return nil, fmt.Errorf("cannot run command from core: symlink cycle found")
		}
		seen[coreLdSo] = true
	}

	ldLibraryPathForCore := parseCoreLdSoConf("/etc/ld.so.conf")

	ldSoArgs := []string{"--library-path", strings.Join(ldLibraryPathForCore, ":"), cmdPath}
	allArgs := append(ldSoArgs, cmdArgs...)
	return exec.Command(coreLdSo, allArgs...), nil
}
