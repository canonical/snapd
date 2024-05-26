// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"regexp"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"gopkg.in/check.v1"
)

type symlinkTargetChecker struct {
	*check.CheckerInfo
	exact bool
}

// SymlinkTargetEquals verifies that the given file is a symbolic link with the given target.
var SymlinkTargetEquals check.Checker = &symlinkTargetChecker{
	CheckerInfo: &check.CheckerInfo{Name: "SymlinkTargetEquals", Params: []string{"filename", "target"}},
	exact:       true,
}

// SymlinkTargetContains verifies that the given file is a symbolic link whose target contains the provided text.
var SymlinkTargetContains check.Checker = &symlinkTargetChecker{
	CheckerInfo: &check.CheckerInfo{Name: "SymlinkTargetContains", Params: []string{"filename", "target"}},
}

// SymlinkTargetMatches verifies that the given file is a symbolic link whose target matches the provided regular expression.
var SymlinkTargetMatches check.Checker = &symlinkTargetChecker{
	CheckerInfo: &check.CheckerInfo{Name: "SymlinkTargetMatches", Params: []string{"filename", "regex"}},
}

func (c *symlinkTargetChecker) Check(params []interface{}, names []string) (result bool, error string) {
	filename, ok := params[0].(string)
	if !ok {
		return false, "Filename must be a string"
	}
	if names[1] == "regex" {
		regexpr, ok := params[1].(string)
		if !ok {
			return false, "Regex must be a string"
		}
		rx := mylog.Check2(regexp.Compile(regexpr))

		params[1] = rx
	}
	return symlinkTargetCheck(filename, params[1], c.exact)
}

func symlinkTargetCheck(filename string, expectedTarget interface{}, exact bool) (result bool, error string) {
	target := mylog.Check2(os.Readlink(filename))

	if exact {
		switch expectedTarget := expectedTarget.(type) {
		case string:
			result = target == expectedTarget
		default:
			error = fmt.Sprintf("Cannot compare symbolic link target with something of type %T", expectedTarget)
		}
	} else {
		switch expectedTarget := expectedTarget.(type) {
		case string:
			result = strings.Contains(target, expectedTarget)
		case *regexp.Regexp:
			result = expectedTarget.MatchString(target)
		default:
			error = fmt.Sprintf("Cannot compare symbolic link target with something of type %T", expectedTarget)
		}
	}
	if !result {
		if error == "" {
			error = fmt.Sprintf("Failed to match with symbolic link target:\n%v", target)
		}
		return result, error
	}
	return result, ""
}
