// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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
	"io/ioutil"
	"os"
	"testing"

	"gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

var _ = check.Suite(&FileReportSuite{})

type FileReportSuite struct {
	subject *FileReporter
	path    string
}

func (s *FileReportSuite) SetUpSuite(c *check.C) {
	s.path = getFilePath(reporterFilePath)
}

func (s *FileReportSuite) SetUpTest(c *check.C) {
	s.subject = &FileReporter{}
}

func (s *FileReportSuite) TearDownTest(c *check.C) {
	os.Remove(s.path)
}

func (s *FileReportSuite) TestFileReporterCreatesOutputFile(c *check.C) {
	s.subject.Write([]byte("Test"))

	_, err := os.Stat(s.path)

	c.Assert(err, check.IsNil,
		check.Commentf("Output file not found %s, error %s",
			reporterFilePath, err))
}

func (s *FileReportSuite) TestFileReporterWritesGivenData(c *check.C) {
	s.subject.Write([]byte("Test"))

	content, err := ioutil.ReadFile(s.path)

	c.Assert(err, check.IsNil,
		check.Commentf("Error reading file %s, %s", s.path, err))

	c.Assert(string(content), check.Equals, "Test",
		check.Commentf("Expected content '%s' not found, actual '%s'",
			"Test", string(content)))
}

func (s *FileReportSuite) TestFileReporterHonoursAdtEnv(c *check.C) {
	back := os.Getenv("ADT_ARTIFACTS")
	defer os.Setenv("ADT_ARTIFACTS", back)
	os.Setenv("ADT_ARTIFACTS", "/tmp")

	s.subject.Write([]byte("Test"))

	path := getFilePath(reporterFilePath)
	_, err := ioutil.ReadFile(path)

	c.Assert(err, check.IsNil,
		check.Commentf("Error reading file %s, %s", path, err))
}

func (s *FileReportSuite) TestFileReporterPreservesPreviousFile(c *check.C) {
	previousData := "prevdata"
	err := ioutil.WriteFile(s.path, []byte(previousData), 0644)
	c.Assert(err, check.IsNil,
		check.Commentf("Obtained error while writing file %s, %s", s.path, err))

	s.subject.Write([]byte("-postdata"))

	content, err := ioutil.ReadFile(s.path)

	c.Assert(err, check.IsNil,
		check.Commentf("Obtained error while reading file %s, %s", s.path, err))

	c.Assert(string(content), check.Equals, "prevdata-postdata",
		check.Commentf("Found previous data in output file! %s", string(content)))
}

func (s *FileReportSuite) TestFileReporterDoNotRecreateOutputFile(c *check.C) {
	s.subject.Write([]byte("Start"))
	s.subject.Write([]byte("-End"))

	content, err := ioutil.ReadFile(s.path)
	c.Assert(err, check.IsNil,
		check.Commentf("Obtained error while reading file %s, %s", s.path, err))

	c.Assert(string(content), check.Equals, "Start-End",
		check.Commentf("Not found appended data in output file! content %s, not found %s",
			string(content), "Start-End"))
}

func (s *FileReportSuite) TestFileReporterReturnsTheNumberOfBytesWritten(c *check.C) {
	testData := "Test data"
	actual, err := s.subject.Write([]byte(testData))
	c.Assert(err, check.IsNil, check.Commentf("Error while writing to output %s", err))

	expected := len([]byte(testData))

	c.Assert(actual, check.Equals, expected,
		check.Commentf("Expected length output %d not found, actual %d",
			expected, actual))
}
