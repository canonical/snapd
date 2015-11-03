// -*- Mode: Go; indent-tabs-mode: t -*-
// +build integration

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

	"github.com/testing-cabal/subunit-go"
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

// Statuser reports the status of a test.
type Statuser interface {
	Status(subunit.Event) error
}

// SubunitV2ParserReporter is a type that parses the input data
// and sends the results as a test status.
//
// The input data is expected to be of the form of the textual
// output of gocheck with verbose mode enabled, and the output
// will be of the form defined by the subunit v2 format There
// are constants reflecting the expected patterns for this texts.
// Additionally, it doesn't take  into account the SKIPs done
// from a SetUpTest method, due to the nature of the snappy test
// suite we are using those for resuming execution after a reboot
// and they shouldn't be reflected as skipped tests in the final
// output. For the same reason we use a special marker for the
// test's announce.
type SubunitV2ParserReporter struct {
	statuser Statuser
}

// NewSubunitV2ParserReporter returns a new ParserReporter that sends the report to the
// writer argument.
func NewSubunitV2ParserReporter(writer io.Writer) *SubunitV2ParserReporter {
	return &SubunitV2ParserReporter{statuser: &subunit.StreamResultToBytes{Output: writer}}
}

func (fr *SubunitV2ParserReporter) Write(data []byte) (int, error) {
	var err error

	if matches := announceRegexp.FindStringSubmatch(string(data)); len(matches) == 2 {
		err = fr.statuser.Status(subunit.Event{TestID: matches[1], Status: "exists"})
	} else if matches := successRegexp.FindStringSubmatch(string(data)); len(matches) == 2 {
		err = fr.statuser.Status(subunit.Event{TestID: matches[1], Status: "success"})
	} else if matches := failureRegexp.FindStringSubmatch(string(data)); len(matches) == 2 {
		err = fr.statuser.Status(subunit.Event{TestID: matches[1], Status: "fail"})
	} else if matches := skipRegexp.FindStringSubmatch(string(data)); len(matches) == 3 {
		err = fr.statuser.Status(subunit.Event{
			TestID:    matches[1],
			Status:    "skip",
			FileName:  "reason",
			FileBytes: []byte(matches[2]),
			MIME:      "text/plain;charset=utf8",
		})
	}

	return 0, err
}
