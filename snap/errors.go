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

package snap

import (
	"fmt"
	"io"
	"os"
)

type AlreadyInstalledError struct {
	Snap string
}

func (e AlreadyInstalledError) Error() string {
	return fmt.Sprintf("snap %q is already installed", e.Snap)
}

type NotInstalledError struct {
	Snap string
	Rev  Revision
}

func (e NotInstalledError) Error() string {
	if e.Rev.Unset() {
		return fmt.Sprintf("snap %q is not installed", e.Snap)
	}
	return fmt.Sprintf("revision %s of snap %q is not installed", e.Rev, e.Snap)
}

// NotSnapError is returned if a operation expects a snap file or snap dir
// but no valid input is provided.
type NotSnapError struct {
	Path string

	Err error
}

func (e NotSnapError) Error() string {
	return fmt.Sprintf("cannot process snap or snapdir: %v", e.Err)
}

func buildNotSnapErrorContext(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if stat.IsDir() {
		if _, err := f.Readdir(1); err == io.EOF {
			return fmt.Errorf("directory %q is empty", path)
		}
		return fmt.Errorf("directory %q is invalid", path)
	}

	var header [10]byte
	if _, err := f.Read(header[:]); err != nil {
		return err
	}
	return fmt.Errorf("file %q is invalid (header %v)", path, header)
}

func NewNotSnapErrorWithContext(path string) NotSnapError {
	return NotSnapError{Path: path, Err: buildNotSnapErrorContext(path)}
}
