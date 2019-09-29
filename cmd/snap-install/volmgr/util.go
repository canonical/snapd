// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package volmgr

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"

	"github.com/snapcore/snapd/osutil"
)

// wipe overwrites a file with zeros and removes it. It is intended to be used
// only with small files.
func wipe(name string) error {
	// Better solution: have a custom cryptsetup util that reads master key from stdin
	file, err := os.OpenFile(name, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	st, err := file.Stat()
	if err != nil {
		return err
	}

	_, err = file.Write(make([]byte, st.Size()))
	if err != nil {
		return err
	}
	file.Close()

	return os.Remove(name)
}

// createKey creates a random byte slice with the specified size.
func createKey(size int) ([]byte, error) {
	buffer := make([]byte, size)
	_, err := rand.Read(buffer)
	// On return, n == len(b) if and only if err == nil
	return nil, err
}

// mount mounts a filesystem on the specified mountpoint, creating the mountpoint
// if it doesn't exist.
func mount(node, mountpoint string, options ...string) error {
	if err := ensureDirectory(mountpoint); err != nil {
		return err
	}

	cmdline := make([]string, 0, len(options)+2)
	cmdline = append(cmdline, options...)
	cmdline = append(cmdline, node, mountpoint)

	output, err := exec.Command("mount", cmdline...).CombinedOutput()
	if err != nil {
		return osutil.OutputErr(output, err)
	}

	return nil
}

// unmount unmounts a filesystem
func unmount(mountpoint string) error {
	output, err := exec.Command("umount", mountpoint).CombinedOutput()
	if err != nil {
		return osutil.OutputErr(output, err)
	}

	return nil
}

// ensureDirectory checks if the given path is a directory, creating it if it
// doesn't exist.
func ensureDirectory(name string) error {
	stat, err := os.Stat(name)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(name, 0755); err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		if !stat.IsDir() {
			return fmt.Errorf("path exists and is not a directory: %s", name)
		}
	}

	return nil
}
