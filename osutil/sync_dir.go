// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"syscall"
)

// Janitor looks after a specific directory, making sure it has exactly what is expected.
//
// Technically the janitor looks at a subset of files in a given directory. On
// demand, the janitor scans the directory and looking at contents and
// permissions of files matching a given pattern. At the same time the janitor
// has an oracle/helper that tells it exactly what should be in each of the
// files, which files should exist, etc. The janitor uses this information to
// correct any discrepancies.
//
// After checking everything the janitor notifies whoever controls it about the
// changes made. Those changes can be used to follow up with some additional
// tasks that are beyond the scope of the janitor, e.g. to run some programs.
//
// Note that Janitor doesn't use any filesystem observation APIs. All changes
// are monitored on demand.
type Janitor struct {
	// path contains the name of the directory to "observe".
	Path string
	// glob contains pattern of files to observe.
	Glob string
}

// File describes the content and-meta data of one file managed by the janitor.
type File struct {
	Content  []byte
	Mode     os.FileMode
	UID, Gid uint32
}

// Tidy the directory to match expectations.
//
// Tidy looks at the managed directory, enumerates all the files
// there that match the encapsulated glob. Files not matching the glob are
// untouched.  Unexpected files are removed. Missing files are created and
// corrupted files are fixed.
//
// The janitor stops at the first encountered error but reports all of the
// changes performed so far. Information about the performed changes is
// returned to the caller for any extra processing that might be required (e.g.
// to run some helper program).
func (j *Janitor) Tidy(oracle map[string]*File) (removed, created, fixed []string, err error) {
	found := make(map[string]bool)
	matches, err := filepath.Glob(path.Join(j.Path, j.Glob))
	if err != nil {
		return
	}
	// Analyze files that inhabit the subset defined by our glob pattern.
	for _, name := range matches {
		baseName := path.Base(name)
		var file *os.File
		if file, err = os.OpenFile(name, os.O_RDWR, 0); err != nil {
			return
		}
		defer file.Close()
		var stat os.FileInfo
		if stat, err = file.Stat(); err != nil {
			return
		}
		if expected, shouldBeHere := oracle[baseName]; shouldBeHere {
			// Check that the file has the right content and meta-data.
			isFixed := false
			if isFixed, err = j.tidyExistingFile(file, stat, expected); err != nil {
				return
			}
			if isFixed {
				fixed = append(fixed, baseName)
			}
			found[baseName] = true
		} else {
			// The file is not supposed to be here.
			if err = os.RemoveAll(name); err != nil {
				return
			}
			removed = append(removed, baseName)
		}
	}
	// Create files that were not found but are expected
	for baseName, expected := range oracle {
		if baseName != path.Base(baseName) {
			err = fmt.Errorf("expected files cannot have path component: %q", baseName)
			return
		}
		var matched bool
		matched, err = filepath.Match(j.Glob, baseName)
		if err != nil {
			return
		}
		if !matched {
			err = fmt.Errorf("expected files must match pattern: %q (pattern: %q)", baseName, j.Glob)
			return
		}
		if found[baseName] {
			continue
		}
		if err = ioutil.WriteFile(path.Join(j.Path, baseName), expected.Content, expected.Mode); err != nil {
			return
		}
		created = append(created, baseName)
	}
	return
}

// tidyExistingFile ensures that file content and meta-data matches expectations.
func (j *Janitor) tidyExistingFile(file *os.File, stat os.FileInfo, expected *File) (fixed bool, err error) {
	var fixedMeta, fixedContent bool
	if fixedMeta, err = j.tidyContent(file, stat, expected); err != nil {
		return
	}
	if fixedContent, err = j.tidyMetaData(file, stat, expected); err != nil {
		return
	}
	fixed = fixedMeta || fixedContent
	return
}

// tidyContent ensures that file content matches expectations.
//
// If file has the same size read it in memory and check that is is correct.
// Here we assume that the size is not of unexpectedly large size because we
// can hold the reference content. If the file size is not what we expected we
// overwrite it unconditionally.
func (j *Janitor) tidyContent(file *os.File, stat os.FileInfo, expected *File) (fixed bool, err error) {
	if stat.Size() == int64(len(expected.Content)) {
		var content []byte
		if content, err = ioutil.ReadFile(file.Name()); err != nil {
			return
		}
		if bytes.Equal(content, expected.Content) {
			// If the file has expected content, just return.
			return
		}
		if _, err = file.Seek(0, 0); err != nil {
			return
		}
		if _, err = file.Write(expected.Content); err != nil {
			return
		}
		fixed = true
	} else {
		if err = file.Truncate(0); err != nil {
			return
		}
		if _, err = file.Write(expected.Content); err != nil {
			return
		}
		fixed = true
	}
	return
}

// tidyMetaData ensures that file meta-data matches expectations.
//
// If file permissions, owner or group owner is different from expectations
// they are corrected..
func (j *Janitor) tidyMetaData(file *os.File, stat os.FileInfo, expected *File) (fixed bool, err error) {
	currentPerm := stat.Mode().Perm()
	expectedPerm := expected.Mode.Perm()
	if currentPerm != expectedPerm {
		if err = file.Chmod(expectedPerm); err != nil {
			return
		}
		fixed = true
	}
	if st, ok := stat.Sys().(*syscall.Stat_t); ok {
		if st.Uid != expected.UID || st.Gid != expected.Gid {
			if err = file.Chown(int(expected.UID), int(expected.Gid)); err != nil {
				return
			}
			fixed = true
		}
	}
	return
}
