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
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"gopkg.in/check.v1"
)

type fileContentChecker struct {
	*check.CheckerInfo
	exact bool
}

// FileEquals verifies that the given file's content is equal to the string (or
// fmt.Stringer), []byte provided, or the contents referred by a FileContentRef.
var FileEquals check.Checker = &fileContentChecker{
	CheckerInfo: &check.CheckerInfo{Name: "FileEquals", Params: []string{"filename", "contents"}},
	exact:       true,
}

// FileContains verifies that the given file's content contains
// the string (or fmt.Stringer) or []byte provided.
var FileContains check.Checker = &fileContentChecker{
	CheckerInfo: &check.CheckerInfo{Name: "FileContains", Params: []string{"filename", "contents"}},
}

// FileMatches verifies that the given file's content matches
// the string provided.
var FileMatches check.Checker = &fileContentChecker{
	CheckerInfo: &check.CheckerInfo{Name: "FileMatches", Params: []string{"filename", "regex"}},
}

// FileContentRef refers to the content of file by its name, to use in
// conjunction with FileEquals.
type FileContentRef string

func (c *fileContentChecker) Check(params []interface{}, names []string) (result bool, error string) {
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
	return fileContentCheck(filename, params[1], c.exact)
}

func fileContentCheck(filename string, content interface{}, exact bool) (result bool, error string) {
	buf := mylog.Check2(os.ReadFile(filename))

	presentableBuf := string(buf)
	if exact {
		switch content := content.(type) {
		case string:
			result = presentableBuf == content
		case []byte:
			result = bytes.Equal(buf, content)
			presentableBuf = "<binary data>"
		case fmt.Stringer:
			result = presentableBuf == content.String()
		case FileContentRef:
			referenceFilename := string(content)
			reference := mylog.Check2(os.ReadFile(referenceFilename))

			result = bytes.Equal(buf, reference)
			if !result {
				error = fmt.Sprintf("Failed to match contents with reference file %q:\n%v", referenceFilename, presentableBuf)
			}

		default:
			error = fmt.Sprintf("Cannot compare file contents with something of type %T", content)
		}
	} else {
		switch content := content.(type) {
		case string:
			result = strings.Contains(presentableBuf, content)
		case []byte:
			result = bytes.Contains(buf, content)
			presentableBuf = "<binary data>"
		case *regexp.Regexp:
			result = content.Match(buf)
		case fmt.Stringer:
			result = strings.Contains(presentableBuf, content.String())
		case FileContentRef:
			error = "Non-exact match with reference file is not supported"
		default:
			error = fmt.Sprintf("Cannot compare file contents with something of type %T", content)
		}
	}
	if !result {
		if error == "" {
			error = fmt.Sprintf("Failed to match with file contents:\n%v", presentableBuf)
		}
		return result, error
	}
	return result, ""
}
