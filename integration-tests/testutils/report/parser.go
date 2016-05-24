// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	"strings"

	"github.com/testing-cabal/subunit-go"

	"github.com/snapcore/snapd/integration-tests/testutils/common"
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
// will be of the form defined by the subunit v2 format. There
// are constants reflecting the expected patterns for this texts.
// Additionally, it doesn't take  into account the SKIPs done
// from a SetUpTest method, due to the nature of the snappy test
// suite we are using those for resuming execution after a reboot
// and they shouldn't be reflected as skipped tests in the final
// output. For the same reason we use a special marker for the
// test's announce.
type SubunitV2ParserReporter struct {
	statuser Statuser
	Next     io.Writer
}

// writerRecorder is used to record the number of bytes that subunit writes to the output.
type writerRecorder struct {
	writer io.Writer
	nbytes int
}

func (w *writerRecorder) Write(data []byte) (int, error) {
	nbytes, err := w.writer.Write(data)
	w.nbytes += nbytes
	return nbytes, err
}

// NewSubunitV2ParserReporter returns a new ParserReporter that sends the report to the
// writer argument.
func NewSubunitV2ParserReporter(writer io.Writer) *SubunitV2ParserReporter {
	wr := &writerRecorder{writer: writer}
	return &SubunitV2ParserReporter{
		statuser: &subunit.StreamResultToBytes{Output: wr},
		Next:     wr,
	}
}

func (fr *SubunitV2ParserReporter) Write(data []byte) (int, error) {
	var err error
	sdata := string(data)

	if matches := announceRegexp.FindStringSubmatch(sdata); len(matches) == 2 {
		testID := matches[1]
		if isTest(testID) && !common.IsInRebootProcess() {
			err = fr.statuser.Status(subunit.Event{TestID: matches[1], Status: "exists"})
		}
	} else if matches := successRegexp.FindStringSubmatch(sdata); len(matches) == 2 {
		testID := matches[1]
		if isTest(testID) && !common.IsInRebootProcess() {
			err = fr.statuser.Status(subunit.Event{TestID: matches[1], Status: "success"})
		}
	} else if matches := failureRegexp.FindStringSubmatch(sdata); len(matches) == 2 {
		err = fr.statuser.Status(subunit.Event{TestID: matches[1], Status: "fail"})
	} else if matches := skipRegexp.FindStringSubmatch(sdata); len(matches) == 3 {
		reason := matches[2]
		// Do not take into account skipped SetUpTest
		if strings.HasSuffix(matches[1], "SetUpTest") {
			return 0, nil
		}
		// Do not report anything about the set ups skipped because of another test's reboot.
		if checkReboot(reason) {
			return 0, nil
		}

		err = fr.statuser.Status(subunit.Event{
			TestID:    matches[1],
			Status:    "skip",
			FileName:  "reason",
			FileBytes: []byte(reason),
			MIME:      "text/plain;charset=utf8",
		})
	}
	if wr, ok := fr.Next.(*writerRecorder); ok {
		return wr.nbytes, err
	}
	return 0, err
}

func isTest(testID string) bool {
	matchesSetUp := matchString(".*\\.SetUpTest", testID)
	matchesTearDown := matchString(".*\\.TearDownTest", testID)
	return !matchesSetUp && !matchesTearDown
}

func matchString(pattern string, s string) bool {
	matched, err := regexp.MatchString(pattern, s)
	if err != nil {
		panic(err)
	}
	return matched
}

func checkReboot(reason string) bool {
	duringReboot := matchString(
		fmt.Sprintf(regexp.QuoteMeta(common.FormatSkipDuringReboot), ".*", ".*"),
		reason)
	afterReboot := matchString(
		fmt.Sprintf(regexp.QuoteMeta(common.FormatSkipAfterReboot), ".*", ".*"),
		reason)
	return duringReboot || afterReboot
}
