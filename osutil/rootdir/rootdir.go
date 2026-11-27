// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

// Package rootdir provides a global root directory prefix, used to
// relocate filesystem access when operating on a chroot or another
// root filesystem, for example while imaging or preseeding.
//
// The global is startup- or test-time configuration: it is not safe for
// concurrent mutation. Packages that derive state from the root
// directory should register a callback with AddRootDirCallback; the
// callbacks run after RootDir is updated, in registration order.
package rootdir

import (
	"fmt"
	"path/filepath"
	"strings"
)

// RootDir is the global root directory prefix. It defaults to "/".
var RootDir string = "/"

// callbacks are invoked by SetRootDir, in registration order, after
// RootDir is updated.
var callbacks []func(string)

// SetRootDir sets the global root directory. An empty value means "/".
// Registered callbacks run after RootDir is updated.
func SetRootDir(rootdir string) {
	if rootdir == "" {
		rootdir = "/"
	}
	RootDir = rootdir
	for _, c := range callbacks {
		c(rootdir)
	}
}

// AddRootDirCallback registers a callback invoked by SetRootDir after
// RootDir is updated, in registration order.
func AddRootDirCallback(c func(string)) {
	callbacks = append(callbacks, c)
}

// StripRootDir strips the global root directory prefix from dir. It
// returns an error if dir is not absolute or not under RootDir. The
// root directory itself is allowed and strips to "/.".
func StripRootDir(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("supplied path is not absolute %q", dir)
	}
	root := RootDir
	if root != "/" && !strings.HasSuffix(root, "/") {
		root += "/"
	}
	if dir != RootDir && !strings.HasPrefix(dir, root) {
		return "", fmt.Errorf("supplied path is not related to global root %q", dir)
	}
	result, err := filepath.Rel(RootDir, dir)
	if err != nil {
		return "", err
	}
	return "/" + result, nil
}
