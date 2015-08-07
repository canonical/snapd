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

package autopkgtest

import (
	"fmt"
	"strings"
	"testing"

	"gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

const (
	sourceCodePath      = "sourceCodePath"
	testArtifactsPath   = "testArtifactsPath"
	testFilter          = "testFilter"
	integrationTestName = "integrationTestName"
	imgPath             = "imgPath"
)

type AutopkgtestSuite struct {
	execCalls        map[string]int
	execReturnValues []string
	execReturnIndex  int
	backExecCommand  func(...string) error

	mkDirCalls           map[string]int
	backPrepareTargetDir func(string)

	tplExecuteCalls map[string]int
	backTplExecute  func(string, string, interface{}) error

	subject *Autopkgtest
}

var _ = check.Suite(&AutopkgtestSuite{})

func (s *AutopkgtestSuite) SetUpSuite(c *check.C) {
	s.execCalls = make(map[string]int)
	s.mkDirCalls = make(map[string]int)
	s.tplExecuteCalls = make(map[string]int)

	s.backExecCommand = execCommand
	s.backPrepareTargetDir = prepareTargetDir
	s.backTplExecute = tplExecute

	execCommand = s.fakeExecCommand
	prepareTargetDir = s.fakePrepareTargetDir
	tplExecute = s.fakeTplExecute

	s.subject = NewAutopkgtest(sourceCodePath, testArtifactsPath, testFilter, integrationTestName)
}

func (s *AutopkgtestSuite) TearDownSuite(c *check.C) {
	execCommand = s.backExecCommand
	prepareTargetDir = s.backPrepareTargetDir
	tplExecute = s.backTplExecute
}

func (s *AutopkgtestSuite) SetUpTest(c *check.C) {
	s.execCalls = make(map[string]int)
	s.mkDirCalls = make(map[string]int)
	s.tplExecuteCalls = make(map[string]int)
}

func (s *AutopkgtestSuite) fakeExecCommand(args ...string) (err error) {
	s.execCalls[strings.Join(args, " ")]++
	return
}

func (s *AutopkgtestSuite) fakePrepareTargetDir(path string) {
	s.mkDirCalls[path]++
}

func (s *AutopkgtestSuite) fakeTplExecute(tplFile, outputFile string, data interface{}) (err error) {
	s.tplExecuteCalls[tplExecuteCmd(tplFile, outputFile, data)]++
	return
}

func (s *AutopkgtestSuite) TestAdtRunCallsTplExecuteLocal(c *check.C) {
	s.subject.AdtRunLocal(imgPath)

	expectedTplExecuteCall := tplExecuteCmd(controlTpl,
		controlFile, struct{ Filter, Test string }{integrationTestName, testFilter})

	c.Assert(s.tplExecuteCalls[expectedTplExecuteCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedTplExecuteCall))
}

func tplExecuteCmd(tplFile, outputFile string, data interface{}) string {
	return fmt.Sprint(tplFile, outputFile, data)
}
