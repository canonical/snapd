// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package runner

import (
	"bytes"
	"flag"
	"regexp"
	"testing"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&GenericTestSuite{})

type GenericTestSuite struct {
}

func (m *GenericTestSuite) TestSuccess(c *check.C) {
	c.Succeed()
}

func (m *GenericTestSuite) TestFail(c *check.C) {
	c.Fail()
}

func TestRunnerPipesOutput(t *testing.T) {
	back := getFlagValue("v")
	flag.Set("check.v", "true")
	defer flag.Set("check.v", back)

	var output bytes.Buffer
	// passing here a different *testing.T so that the results
	// of the tests in the target suite do not pollute the results of
	// this test
	TestingT(new(testing.T), &output)

	expectedOutput := `(?msi).*FAIL: runner_test.go:.*: GenericTestSuite.TestFail.*PASS: runner_test.go:.*: GenericTestSuite.TestSuccess.*`

	if match, _ := regexp.MatchString(expectedOutput, output.String()); !match {
		t.Errorf("Expected value not obtained in the output writer!! Expected: %s, Actual: %s",
			expectedOutput, output.String())
	}
}

func TestRunnerAcceptsStreamFlag(t *testing.T) {
	flag.Set("check.vv", "true")
	defer flag.Set("check.vv", "false")

	var output bytes.Buffer
	TestingT(new(testing.T), &output)

	expectedOutput := `(?msi)START: runner_test.go:.*: GenericTestSuite.TestFail.*
FAIL: runner_test.go:.*: GenericTestSuite.TestFail.*
START: runner_test.go:.*: GenericTestSuite.TestSuccess
PASS: runner_test.go:.*: GenericTestSuite.TestSuccess.*`

	if match, _ := regexp.MatchString(expectedOutput, output.String()); !match {
		t.Errorf("Expected value not obtained in the output writer!! Expected: %s, Actual: %s",
			expectedOutput, output.String())
	}
}

func TestRunnerAcceptsFilterFlag(t *testing.T) {
	flag.Set("check.f", "GenericTestSuite.TestSuccess")
	defer flag.Set("check.f", "")

	var output bytes.Buffer
	TestingT(new(testing.T), &output)

	unExpectedOutput := `(?msi).*FAIL: runner_test.go:.*: GenericTestSuite.TestFail.*`

	if match, _ := regexp.MatchString(unExpectedOutput, output.String()); match {
		t.Errorf("Unexpected value obtained in the output writer!! Unexpected: %s, Actual: %s",
			unExpectedOutput, output.String())
	}
}

func getFlagValue(name string) string {
	currentFlag := flag.CommandLine.Lookup("check.v")
	if currentFlag != nil {
		return "true"
	}
	return "false"
}
