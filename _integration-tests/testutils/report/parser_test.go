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
	"bytes"
	"fmt"

	check "gopkg.in/check.v1"
)

var _ = check.Suite(&ParserReportSuite{})

type ParserReportSuite struct {
	subject *ParserReporter
	output  bytes.Buffer
	testID  string
}

func (s *ParserReportSuite) SetUpSuite(c *check.C) {
	s.subject = &ParserReporter{Next: &s.output}
	s.testID = "testSuite.TestName"
}

func (s *ParserReportSuite) SetUpTest(c *check.C) {
	s.output.Reset()
}

func (s *ParserReportSuite) TestParserReporterOutputsNothingWithNotParseableInput(c *check.C) {
	s.subject.Write([]byte("Not parseable"))

	expected := ""
	actual := s.output.String()

	c.Assert(actual, check.Equals, expected,
		check.Commentf("Obtained unexpected text output %s", actual))
}

func (s *ParserReportSuite) TestParserReporterOutputsAnnounce(c *check.C) {
	s.subject.Write([]byte(fmt.Sprintf("****** Running %s\n", s.testID)))

	expected := fmt.Sprintf("test: %s\n", s.testID)
	actual := s.output.String()

	c.Assert(actual, check.Equals, expected,
		check.Commentf("Expected text output %s not found, actual %s",
			expected, actual))
}

func (s *ParserReportSuite) TestParserReporterReturnsTheNumberOfBytesWritten(c *check.C) {
	actual, err := s.subject.Write([]byte(fmt.Sprintf("****** Running %s\n", s.testID)))
	c.Assert(err, check.IsNil, check.Commentf("Error while writing to output %s", err))

	expected := len([]byte(fmt.Sprintf("test: %s\n", s.testID)))

	c.Assert(actual, check.Equals, expected,
		check.Commentf("Expected length output %d not found, actual %d",
			expected, actual))
}

func (s *ParserReportSuite) TestParserReporterOutputsSuccess(c *check.C) {
	s.subject.Write([]byte(fmt.Sprintf("PASS: /tmp/snappy-tests-job/18811/src/github.com/ubuntu-core/snappy/_integration-tests/tests/apt_test.go:34: %s      0.005s\n", s.testID)))

	expected := fmt.Sprintf("success: %s\n", s.testID)
	actual := s.output.String()

	c.Assert(actual, check.Equals, expected,
		check.Commentf("Expected text output %s not found, actual %s",
			expected, actual))
}

func (s *ParserReportSuite) TestParserReporterOutputsFailure(c *check.C) {
	s.subject.Write([]byte(fmt.Sprintf("FAIL: /tmp/snappy-tests-job/710/src/github.com/ubuntu-core/snappy/_integration-tests/tests/installFramework_test.go:85: %s\n", s.testID)))

	expected := fmt.Sprintf("failure: %s\n", s.testID)
	actual := s.output.String()

	c.Assert(actual, check.Equals, expected,
		check.Commentf("Expected text output %s not found, actual %s",
			expected, actual))
}

func (s *ParserReportSuite) TestParserReporterOutputsSkip(c *check.C) {
	skipReason := "skip reason"
	s.subject.Write([]byte(fmt.Sprintf("SKIP: /tmp/snappy-tests-job/21647/src/github.com/ubuntu-core/snappy/_integration-tests/tests/info_test.go:36: %s (%s)\n", s.testID, skipReason)))

	expected := fmt.Sprintf("skip: %s [\n%s\n]\n", s.testID, skipReason)
	actual := s.output.String()

	c.Assert(actual, check.Equals, expected,
		check.Commentf("Expected text output %s not found, actual %s",
			expected, actual))
}
