// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package report

import (
	"fmt"
	"io"
	"regexp"
)

const (
	commonPattern       = `(?U)%s\: \/.*: (.*)`
	announcePattern     = `(?U)\*\*\*\*\*\* Running (.*)\n`
	successPatternSufix = `\s*\d*\.\d*s\n`
	skipPatternSufix    = `\s*\((.*)\)\n`
)

var (
	announceRegexp = regexp.MustCompile(announcePattern)
	successRegexp  = regexp.MustCompile(fmt.Sprintf(commonPattern, "PASS") + successPatternSufix)
	failureRegexp  = regexp.MustCompile(fmt.Sprintf(commonPattern, "FAIL") + "\n")
	skipRegexp     = regexp.MustCompile(fmt.Sprintf(commonPattern, "SKIP") + skipPatternSufix)
)

// ParserReporter is a type implementing io.Writer that
// parses the input data and sends the results to the Next
// reporter
//
// The input data is expected to be of the form of the textual
// output of gocheck with verbose mode enabled, and the output
// will be of the form defined by the subunit format before
// the binary encoding. There are constants reflecting the
// expected patterns for this texts.
// Additionally, it doesn't take  into account the SKIPs done
// from a SetUpTest method, due to the nature of the snappy test
// suite we are using those for resuming execution after a reboot
// and they shouldn't be reflected as skipped tests in the final
// output. For the same reason we use a special marker for the
// test's announce.
type ParserReporter struct {
	Next io.Writer
}

func (fr *ParserReporter) Write(data []byte) (n int, err error) {
	var outputStr string

	if matches := announceRegexp.FindStringSubmatch(string(data)); len(matches) == 2 {
		outputStr = fmt.Sprintf("test: %s\n", matches[1])

	} else if matches := successRegexp.FindStringSubmatch(string(data)); len(matches) == 2 {
		outputStr = fmt.Sprintf("success: %s\n", matches[1])

	} else if matches := failureRegexp.FindStringSubmatch(string(data)); len(matches) == 2 {
		outputStr = fmt.Sprintf("failure: %s\n", matches[1])

	} else if matches := skipRegexp.FindStringSubmatch(string(data)); len(matches) == 3 {
		outputStr = fmt.Sprintf("skip: %s [\n%s]\n", matches[1], matches[2])
	}

	outputByte := []byte(outputStr)
	n = len(outputByte)
	fr.Next.Write(outputByte)
	return
}
