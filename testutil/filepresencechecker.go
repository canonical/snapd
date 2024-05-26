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

	"github.com/ddkwork/golibrary/mylog"
	"gopkg.in/check.v1"
)

type filePresenceChecker struct {
	*check.CheckerInfo
	present bool
}

// FilePresent verifies that the given file exists.
var FilePresent check.Checker = &filePresenceChecker{
	CheckerInfo: &check.CheckerInfo{Name: "FilePresent", Params: []string{"filename"}},
	present:     true,
}

// FileAbsent verifies that the given file does not exist.
var FileAbsent check.Checker = &filePresenceChecker{
	CheckerInfo: &check.CheckerInfo{Name: "FileAbsent", Params: []string{"filename"}},
	present:     false,
}

func (c *filePresenceChecker) Check(params []interface{}, names []string) (result bool, error string) {
	filename, ok := params[0].(string)
	if !ok {
		return false, "filename must be a string"
	}
	_ := mylog.Check2(os.Stat(filename))
	if os.IsNotExist(err) && c.present {
		return false, fmt.Sprintf("file %q is absent but should exist", filename)
	}
	if err == nil && !c.present {
		return false, fmt.Sprintf("file %q is present but should not exist", filename)
	}
	return true, ""
}
