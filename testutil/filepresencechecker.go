// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2018 Canonical Ltd
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

package testutil

import (
	"fmt"
	"os"

	"gopkg.in/check.v1"
)

type filePresenceChecker struct {
	*check.CheckerInfo
	statFunc func(name string) (os.FileInfo, error)
	present  bool
}

// FilePresent verifies that the given file exists, following symlinks.
var FilePresent check.Checker = &filePresenceChecker{
	CheckerInfo: &check.CheckerInfo{Name: "FilePresent", Params: []string{"filename"}},
	statFunc:    os.Stat,
	present:     true,
}

// LFilePresent verifies that the given file/symlink exists.
var LFilePresent check.Checker = &filePresenceChecker{
	CheckerInfo: &check.CheckerInfo{Name: "LFilePresent", Params: []string{"filename"}},
	statFunc:    os.Lstat,
	present:     true,
}

// FileAbsent verifies that the given file does not exist, following symlinks.
var FileAbsent check.Checker = &filePresenceChecker{
	CheckerInfo: &check.CheckerInfo{Name: "FileAbsent", Params: []string{"filename"}},
	statFunc:    os.Stat,
	present:     false,
}

// LFileAbsent verifies that the given file/symlink does not exist.
var LFileAbsent check.Checker = &filePresenceChecker{
	CheckerInfo: &check.CheckerInfo{Name: "LFileAbsent", Params: []string{"filename"}},
	statFunc:    os.Lstat,
	present:     false,
}

func (c *filePresenceChecker) Check(params []interface{}, names []string) (result bool, error string) {
	filename, ok := params[0].(string)
	if !ok {
		return false, "filename must be a string"
	}
	_, err := c.statFunc(filename)
	if os.IsNotExist(err) && c.present {
		return false, fmt.Sprintf("file %q is absent but should exist", filename)
	}
	if err == nil && !c.present {
		return false, fmt.Sprintf("file %q is present but should not exist", filename)
	}
	return true, ""
}
