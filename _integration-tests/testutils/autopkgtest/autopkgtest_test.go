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
	adtrunTpl           = "adt-run -B --setup-commands touch /run/autopkgtest_no_reboot.stamp --override-control %s --built-tree %s --output-dir %s %s"
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
	tplError        bool

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
	s.tplError = false
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
	if s.tplError {
		err = errors.New("Error while rendering control file template!")
	}
	return
}

func (s *AutopkgtestSuite) TestAdtRunLocalCallsTplExecute(c *check.C) {
	s.subject.AdtRunLocal(imgPath)

	expectedTplExecuteCall := tplExecuteCmd(controlTpl,
		controlFile, struct{ Filter, Test string }{integrationTestName, testFilter})

	c.Assert(s.tplExecuteCalls[expectedTplExecuteCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedTplExecuteCall))
}

func (s *AutopkgtestSuite) TestAdtRunLocalCallsPrepareTargetDir(c *check.C) {
	s.subject.AdtRunLocal(imgPath)

	expectedMkDirCall := outputDir(testArtifactsPath)

	c.Assert(s.mkDirCalls[expectedMkDirCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedMkDirCall))
}

func (s *AutopkgtestSuite) TestAdtRunLocalCallsExecCommand(c *check.C) {
	s.subject.AdtRunLocal(imgPath)

	outputDir := outputDir(testArtifactsPath)
	expectedExecCommadCall := adtrunLocalCmd(controlFile, sourceCodePath, outputDir, imgPath)

	c.Assert(s.execCalls[expectedExecCommadCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedExecCommadCall))
}

func (s *AutopkgtestSuite) TestAdtRunLocalReturnsTplError(c *check.C) {
	s.tplError = true
	err := s.subject.AdtRunLocal(imgPath)

	c.Assert(err, check.NotNil, check.Commentf("Expected error from tpl not received!"))
}

func (s *AutopkgtestSuite) TestAdtRunRemoteCallsTplExecute(c *check.C) {
	s.subject.AdtRunRemote(testbedIP, testbedPort)

	expectedTplExecuteCall := tplExecuteCmd(controlTpl,
		controlFile, struct{ Filter, Test string }{integrationTestName, testFilter})

	c.Assert(s.tplExecuteCalls[expectedTplExecuteCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedTplExecuteCall))
}

func (s *AutopkgtestSuite) TestAdtRunRemoteCallsPrepareTargetDir(c *check.C) {
	s.subject.AdtRunRemote(testbedIP, testbedPort)

	expectedMkDirCall := outputDir(testArtifactsPath)

	c.Assert(s.mkDirCalls[expectedMkDirCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedMkDirCall))
}

func (s *AutopkgtestSuite) TestAdtRunRemoteCallsSSHCopyId(c *check.C) {
	s.subject.AdtRunRemote(testbedIP, testbedPort)

	expectedExecCommadCall := fmt.Sprintf("ssh-copy-id -p %s ubuntu@%s", strconv.Itoa(testbedPort), testbedIP)

	outputDir := outputDir(testArtifactsPath)
	adtrunRemoteCmd(controlFile, sourceCodePath, outputDir, testbedIP, testbedPort)

	c.Assert(s.execCalls[expectedExecCommadCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedExecCommadCall))
}

func (s *AutopkgtestSuite) TestAdtRunRemoteCallsExecCommand(c *check.C) {
	s.subject.AdtRunRemote(testbedIP, testbedPort)

	outputDir := outputDir(testArtifactsPath)
	fmt.Print("eee", s.execCalls)
	expectedExecCommadCall := adtrunRemoteCmd(controlFile, sourceCodePath, outputDir, testbedIP, testbedPort)

	c.Assert(s.execCalls[expectedExecCommadCall],
		check.Equals, 1,
		check.Commentf("Expected call %s not executed 1 time", expectedExecCommadCall))
}

func (s *AutopkgtestSuite) TestAdtRunRemoteReturnsTplError(c *check.C) {
	s.tplError = true
	err := s.subject.AdtRunRemote(testbedIP, testbedPort)

	c.Assert(err, check.NotNil, check.Commentf("Expected error from tpl not received!"))
}

func tplExecuteCmd(tplFile, outputFile string, data interface{}) string {
	return fmt.Sprint(tplFile, outputFile, data)
}

func outputDir(basePath string) string {
	return filepath.Join(basePath, "output")
}

func adtrunLocalCmd(controlFile, sourceCodePath, outputDir, imgPath string) string {
	options := fmt.Sprintf("--- ssh -s /usr/share/autopkgtest/ssh-setup/snappy -- -i %s", imgPath)
	return adtrunCommonCmd(controlFile, sourceCodePath, outputDir, options)
}

func adtrunRemoteCmd(controlFile, sourceCodePath, outputDir, testbedIP string, testbedPort int) string {
	port := strconv.Itoa(testbedPort)
	idFile := filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa")
	options := fmt.Sprintf("--- ssh -H %s -p %s -l ubuntu -i %s --reboot", testbedIP, port, idFile)

	return adtrunCommonCmd(controlFile, sourceCodePath, outputDir, options)
}

func adtrunCommonCmd(controlFile, sourceCodePath, outputDir, options string) string {
	return fmt.Sprintf(adtrunTpl, controlFile, sourceCodePath, outputDir, options)
}
