// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	testbedIP           = "1.1.1.1"
	testbedPort         = 90
	adtrunTpl           = "adt-run -B -q --override-control %s --built-tree %s --output-dir %s --setup-commands touch /run/autopkgtest_no_reboot.stamp %s"
)

type AutoPkgTestSuite struct {
	execCalls        map[string]int
	execReturnValues []string
	execReturnIndex  int
	backExecCommand  func(...string) error

	mkDirCalls           map[string]int
	backPrepareTargetDir func(string)

	tplExecuteCalls map[string]int
	backTplExecute  func(string, string, interface{}) error
	tplError        bool

	subject *AutoPkgTest
}

var _ = check.Suite(&AutoPkgTestSuite{})

func (s *AutoPkgTestSuite) SetUpSuite(c *check.C) {
	s.backExecCommand = execCommand
	s.backPrepareTargetDir = prepareTargetDir
	s.backTplExecute = tplExecute

	execCommand = s.fakeExecCommand
	prepareTargetDir = s.fakePrepareTargetDir
	tplExecute = s.fakeTplExecute
}

func (s *AutoPkgTestSuite) TearDownSuite(c *check.C) {
	execCommand = s.backExecCommand
	prepareTargetDir = s.backPrepareTargetDir
	tplExecute = s.backTplExecute
}

func (s *AutoPkgTestSuite) SetUpTest(c *check.C) {
	s.execCalls = make(map[string]int)
	s.mkDirCalls = make(map[string]int)
	s.tplExecuteCalls = make(map[string]int)
	s.tplError = false

	s.subject = &AutoPkgTest{
		SourceCodePath:      sourceCodePath,
		TestArtifactsPath:   testArtifactsPath,
		TestFilter:          testFilter,
		IntegrationTestName: integrationTestName,
		ShellOnFail:         false,
		Env:                 nil,
	}
}

func (s *AutoPkgTestSuite) fakeExecCommand(args ...string) (err error) {
	s.execCalls[strings.Join(args, " ")]++
	return
}

func (s *AutoPkgTestSuite) fakePrepareTargetDir(path string) {
	s.mkDirCalls[path]++
}

func (s *AutoPkgTestSuite) fakeTplExecute(tplFile, outputFile string, data interface{}) (err error) {
	s.tplExecuteCalls[tplExecuteCmd(tplFile, outputFile, data)]++
	if s.tplError {
		err = errors.New("Error while rendering control file template!")
	}
	return
}

func (s *AutoPkgTestSuite) TestAdtRunLocalCallsTplExecute(c *check.C) {
	s.subject.AdtRunLocal(imgPath)

	expectedTplExecuteCall := tplExecuteCmd(controlTpl,
		controlFile, struct{ Filter, Test string }{testFilter, integrationTestName})

	c.Assert(s.tplExecuteCalls[expectedTplExecuteCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedTplExecuteCall))
}

func (s *AutoPkgTestSuite) TestAdtRunLocalCallsPrepareTargetDir(c *check.C) {
	s.subject.AdtRunLocal(imgPath)

	expectedMkDirCall := outputDir(testArtifactsPath)

	c.Assert(s.mkDirCalls[expectedMkDirCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedMkDirCall))
}

func (s *AutoPkgTestSuite) TestAdtRunLocalCallsExecCommand(c *check.C) {
	s.subject.AdtRunLocal(imgPath)

	testOutputDir := outputDir(testArtifactsPath)
	expectedExecCommadCall := adtrunLocalCmd(controlFile, sourceCodePath, testOutputDir, imgPath)

	c.Assert(s.execCalls[expectedExecCommadCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedExecCommadCall))
}

func (s *AutoPkgTestSuite) TestAdtRunLocalReturnsTplError(c *check.C) {
	s.tplError = true
	err := s.subject.AdtRunLocal(imgPath)

	c.Assert(err, check.NotNil, check.Commentf("Expected error from tpl not received!"))
}

func (s *AutoPkgTestSuite) TestAdtRunRemoteCallsTplExecute(c *check.C) {
	s.subject.AdtRunRemote(testbedIP, testbedPort)

	expectedTplExecuteCall := tplExecuteCmd(controlTpl,
		controlFile, struct{ Filter, Test string }{testFilter, integrationTestName})

	c.Assert(s.tplExecuteCalls[expectedTplExecuteCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedTplExecuteCall))
}

func (s *AutoPkgTestSuite) TestAdtRunRemoteCallsPrepareTargetDir(c *check.C) {
	s.subject.AdtRunRemote(testbedIP, testbedPort)

	expectedMkDirCall := outputDir(testArtifactsPath)

	c.Assert(s.mkDirCalls[expectedMkDirCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedMkDirCall))
}

func (s *AutoPkgTestSuite) TestAdtRunRemoteCallsExecCommand(c *check.C) {
	s.subject.AdtRunRemote(testbedIP, testbedPort)

	testOutputDir := outputDir(testArtifactsPath)
	expectedExecCommadCall := adtrunRemoteCmd(controlFile, sourceCodePath, testOutputDir, testbedIP, testbedPort)

	c.Assert(s.execCalls[expectedExecCommadCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedExecCommadCall))
}

func (s *AutoPkgTestSuite) TestAdtRunRemoteReturnsTplError(c *check.C) {
	s.tplError = true
	err := s.subject.AdtRunRemote(testbedIP, testbedPort)

	c.Assert(err, check.NotNil, check.Commentf("Expected error from tpl not received!"))
}

func (s *AutoPkgTestSuite) TestAdtRunShellOnFail(c *check.C) {
	scenarios := []struct {
		shellOnFail     bool
		testbedOptions  string
		expectedOptions string
	}{
		{true, "testbed-options", "--shell-fail testbed-options"},
		{false, "testbed-options", "testbed-options"},
	}

	for _, t := range scenarios {
		s.subject.ShellOnFail = t.shellOnFail
		s.subject.adtRun(t.testbedOptions)

		testOutputDir := outputDir(testArtifactsPath)
		expectedCommandCall := fmt.Sprintf(
			adtrunTpl, controlFile, sourceCodePath, testOutputDir, t.expectedOptions)
		c.Check(s.execCalls[expectedCommandCall], check.Equals, 1,
			check.Commentf("Expected call %s not executed 1 time", expectedCommandCall))
	}
}

func (s *AutoPkgTestSuite) TestAdtRunEnv(c *check.C) {
	s.subject.Env = map[string]string{"var1": "value1"}
	s.subject.adtRun("testbed-options")

	testOutputDir := outputDir(testArtifactsPath)
	expectedCommandCall := fmt.Sprintf(
		adtrunTpl, controlFile, sourceCodePath, testOutputDir, "--env var1=value1 testbed-options")
	c.Check(s.execCalls[expectedCommandCall], check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedCommandCall))
}

func (s *AutoPkgTestSuite) TestAdtRunLocalAddsQuietFlag(c *check.C) {
	s.adtRunAddsQuietFlag(c, true)
}

func (s *AutoPkgTestSuite) TestAdtRunRemoteAddsQuietFlag(c *check.C) {
	s.adtRunAddsQuietFlag(c, false)
}

func (s *AutoPkgTestSuite) adtRunAddsQuietFlag(c *check.C, local bool) {
	s.subject.Verbose = true

	if local {
		s.subject.AdtRunLocal(imgPath)
	} else {
		s.subject.AdtRunRemote(testbedIP, testbedPort)
	}

	match := false
	for call := range s.execCalls {
		if strings.HasPrefix(call, "adt-run") {
			if strings.Contains(call, " -q ") {
				match = true
				break
			}
		}
	}
	c.Assert(match, check.Equals, false, check.Commentf("quiet flag found in adt-run call with verbose=true"))
}

func tplExecuteCmd(tplFile, outputFile string, data interface{}) string {
	return fmt.Sprint(tplFile, outputFile, data)
}

func outputDir(basePath string) string {
	return filepath.Join(basePath, "output")
}

func adtrunLocalCmd(controlFile, sourceCodePath, outputDir, imgPath string) string {
	options := fmt.Sprintf("--- ssh -s /usr/share/autopkgtest/ssh-setup/snappy -- -b -i %s", imgPath)
	return adtrunCommonCmd(controlFile, sourceCodePath, outputDir, options)
}

func adtrunRemoteCmd(controlFile, sourceCodePath, outputDir, testbedIP string, testbedPort int) string {
	port := strconv.Itoa(testbedPort)
	idFile := filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa")
	options := fmt.Sprintf("--- ssh -H %s -p %s -l ubuntu -i %s --reboot --timeout-ssh %d",
		testbedIP, port, idFile, sshTimeout)

	return adtrunCommonCmd(controlFile, sourceCodePath, outputDir, options)
}

func adtrunCommonCmd(controlFile, sourceCodePath, outputDir, options string) string {
	return fmt.Sprintf(adtrunTpl, controlFile, sourceCodePath, outputDir, options)
}
