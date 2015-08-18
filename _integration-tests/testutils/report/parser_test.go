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

const (
	expectedSignature byte = 0xb3
	expectedVersion   byte = 0x2
	testIDPresent     byte = 0x8 // First byte flag 00001000.
	timestampPresent  byte = 0x2 // First byte flag 00000010
	existenceStatus   byte = 1
	successStatus     byte = 3
	skippedStatus     byte = 5
	failedStatus           = 6
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

func (s *ParserReportSuite) assertSubunitPacket(c *check.C, expectedStatus byte) {
	signature := s.output.Next(1)[0]
	c.Check(signature, check.Equals, expectedSignature,
		check.Commentf("Wrong signature"))

	flags := s.output.Next(2)
	version := flags[0] >> 4
	c.Check(version, check.Equals, expectedVersion,
		check.Commentf("Wrong version"))
	c.Check(flags[0]&testIDPresent, check.Equals, testIDPresent,
		check.Commentf("Test ID not present"))
	c.Check(flags[0]&timestampPresent, check.Equals, timestampPresent,
		check.Commentf("Timestamp not present"))

	// We are not testing the other flags.

	status := flags[1] & 0x7 // Three last bits.
	c.Check(status, check.Equals, expectedStatus,
		check.Commentf("Wrong status"))

	// The size has its own test.
	// The size could be two or three bytes if the packet is big, but here we
	// are using only small packets.
	size := int(s.output.Next(1)[0])

	// We are not testing the timestamp seconds.
	s.output.Next(4)
	// Skip the nanoseconds.
	// The nanoseconds is a variable field, but we can calculate its size from
	// the end.
	currentIndex := 8
	crcSize := 4
	// One byte to store the size of the test ID.
	testIDSizeBytes := 1
	s.output.Next(size - currentIndex - testIDSizeBytes - len(s.testID) - crcSize)

	testIDSizeInt := int(s.output.Next(1)[0])
	c.Check(testIDSizeInt, check.Equals, len(s.testID))

	testID := string(s.output.Next(len(s.testID)))
	c.Check(testID, check.Equals, s.testID)

	// We are not testing the CRC checksum.
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
	s.assertSubunitPacket(c, existenceStatus)
}

func (s *ParserReportSuite) TestParserReporterReturnsThePacketSize(c *check.C) {
	actual, err := s.subject.Write([]byte(fmt.Sprintf("****** Running %s\n", s.testID)))
	c.Assert(err, check.IsNil, check.Commentf("Error while writing to output: %s", err))

	// Skip the signature and flags.
	s.output.Next(3)
	size := int(s.output.Next(1)[0])
	c.Assert(size, check.Equals, actual)
}

func (s *ParserReportSuite) TestParserReporterOutputsSuccess(c *check.C) {
	s.subject.Write([]byte(fmt.Sprintf("PASS: /tmp/snappy-tests-job/18811/src/launchpad.net/snappy/_integration-tests/tests/apt_test.go:34: %s      0.005s\n", s.testID)))

	s.assertSubunitPacket(c, successStatus)
}

func (s *ParserReportSuite) TestParserReporterOutputsFailure(c *check.C) {
	s.subject.Write([]byte(fmt.Sprintf("FAIL: /tmp/snappy-tests-job/710/src/launchpad.net/snappy/_integration-tests/tests/installFramework_test.go:85: %s\n", s.testID)))

	s.assertSubunitPacket(c, failedStatus)
}

func (s *ParserReportSuite) TestParserReporterOutputsSkip(c *check.C) {
	skipReason := "skip reason"
	s.subject.Write([]byte(fmt.Sprintf("SKIP: /tmp/snappy-tests-job/21647/src/launchpad.net/snappy/_integration-tests/tests/info_test.go:36: %s (%s)\n", s.testID, skipReason)))

	s.assertSubunitPacket(c, skippedStatus)
}
