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

package tests

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"

	"github.com/kr/pty"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&configExampleSuite{})

type configExampleSuite struct {
	common.SnappySuite
}

var configTests = []struct {
	snap      string
	developer string
	message   string
}{
	{"config-example", "", "test config example message"},
	{"config-example-bash", ".canonical", "test config example bash message"},
}

func (s *configExampleSuite) TestPrintMessageFromConfig(c *check.C) {
	c.Skip("port to snapd")

	for _, t := range configTests {
		common.InstallSnap(c, t.snap+t.developer+"/edge")
		defer common.RemoveSnap(c, t.snap)

		config := fmt.Sprintf(`config:
  %s:
    msg: |
      %s`, t.snap, t.message)

		configFile, err := ioutil.TempFile("", "snappy-cfg")
		defer func() { configFile.Close(); os.Remove(configFile.Name()) }()
		c.Assert(err, check.IsNil, check.Commentf("Error creating temp file: %s", err))
		_, err = configFile.Write([]byte(config))
		c.Assert(err, check.IsNil, check.Commentf("Error writing the conf to the temp file: %s", err))

		cli.ExecCommand(c, "sudo", "snappy", "config", t.snap, configFile.Name())

		output := cli.ExecCommand(c, t.snap+".hello")
		c.Assert(output, check.Equals, t.message, check.Commentf("Wrong message"))
	}
}

var _ = check.Suite(&licensedExampleSuite{})

type licensedExampleSuite struct {
	common.SnappySuite
}

func (s *licensedExampleSuite) TestAcceptLicenseMustInstallSnap(c *check.C) {
	c.Skip("port to snapd")

	cmds := []string{"sudo", "snappy", "install", "licensed.canonical/edge"}
	cmdsCover, err := cli.AddOptionsToCommand(cmds)
	c.Assert(err, check.IsNil, check.Commentf("Error adding coverage options, %q", err))

	cmd := exec.Command(cmdsCover[0], cmdsCover[1:]...)
	f, err := pty.Start(cmd)
	c.Assert(err, check.IsNil, check.Commentf("Error starting pty: %s", err))
	defer common.RemoveSnap(c, "licensed.canonical")

	s.assertLicense(c, f)
	// Accept the license.
	_, err = f.Write([]byte("y\n"))
	c.Assert(err, check.IsNil, check.Commentf("Error writing to pty: %s", err))

	cmd.Wait()

	c.Assert(s.isSnapInstalled(c), check.Equals, true, check.Commentf("The snap was not installed"))
}

func (s *licensedExampleSuite) TestDeclineLicenseMustNotInstallSnap(c *check.C) {
	c.Skip("port to snapd")

	cmds := []string{"sudo", "snappy", "install", "licensed.canonical/edge"}
	cmdsCover, err := cli.AddOptionsToCommand(cmds)
	c.Assert(err, check.IsNil, check.Commentf("Error adding coverage options, %q", err))

	cmd := exec.Command(cmdsCover[0], cmdsCover[1:]...)
	f, err := pty.Start(cmd)
	c.Assert(err, check.IsNil, check.Commentf("Error starting pty: %s", err))

	s.assertLicense(c, f)
	// Decline the license.
	_, err = f.Write([]byte("n\n"))
	c.Assert(err, check.IsNil, check.Commentf("Error writing to pty: %s", err))

	cmd.Wait()

	c.Assert(s.isSnapInstalled(c), check.Equals, false, check.Commentf("The snap was installed"))
}

func (s *licensedExampleSuite) assertLicense(c *check.C, f *os.File) {
	output := s.readUntilPrompt(c, f)
	expected := "(?s)Installing licensed.canonical" +
		".*" +
		"licensed requires that you accept the following license before continuing" +
		"This product is meant for educational purposes only. .* No other warranty expressed or implied."
	c.Assert(output, check.Matches, expected)
}

func (s *licensedExampleSuite) readUntilPrompt(c *check.C, f *os.File) string {
	var output string
	scanner := bufio.NewScanner(f)

	scanLinesUntilPrompt := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			// We have a full newline-terminated line.
			if data[i-1] == '\r' {
				return i + 1, data[0 : i-1], nil
			}
			return i + 1, data[0:i], nil
		}
		// XXX Returning EOF means that this line will not be consumed by Scan.
		// The fix for this will be released in go 1.6.
		// https://github.com/golang/go/issues/11836
		if string(data) == "Do you agree? [y/n] " {
			return len(data), data, io.EOF
		}
		// Request more data.
		return 0, nil, nil
	}

	scanner.Split(scanLinesUntilPrompt)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Println(line)
		output += line
	}
	c.Assert(scanner.Err(), check.IsNil, check.Commentf("Error reading from pty: %s", scanner.Err()))
	return output
}

func (s *licensedExampleSuite) isSnapInstalled(c *check.C) bool {
	infoOutput := cli.ExecCommand(c, "snappy", "info")

	expectedInfo := "(?ms)" +
		".*" +
		"^apps: .*licensed\\.canonical.*\n"
	matches, _ := regexp.MatchString("^"+expectedInfo+"$", infoOutput)
	return matches
}
