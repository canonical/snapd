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

package tests

import (
	"fmt"
	"io/ioutil"	
	"net/http"
	"os"

	"launchpad.net/snappy/_integration-tests/testutils/cli"
	"launchpad.net/snappy/_integration-tests/testutils/common"
	"launchpad.net/snappy/_integration-tests/testutils/wait"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&helloWorldExampleSuite{})

type helloWorldExampleSuite struct {
	common.SnappySuite
}

func (s *helloWorldExampleSuite) TestCallHelloWorldBinary(c *check.C) {
	common.InstallSnap(c, "hello-world")
	s.AddCleanup(func() {
		common.RemoveSnap(c, "hello-world")
	})

	echoOutput := cli.ExecCommand(c, "hello-world.echo")

	c.Assert(echoOutput, check.Equals, "Hello World!\n")
}

func (s *helloWorldExampleSuite) TestCallHelloWorldEvilMustPrintPermissionDeniedError(c *check.C) {
	common.InstallSnap(c, "hello-world")
	s.AddCleanup(func() {
		common.RemoveSnap(c, "hello-world")
	})

	echoOutput, err := cli.ExecCommandErr("hello-world.evil")
	c.Assert(err, check.NotNil, check.Commentf("hello-world.evil did not fail"))

	expected := "" +
		"Hello Evil World!\n" +
		"This example demonstrates the app confinement\n" +
		"You should see a permission denied error next\n" +
		"/apps/hello-world.canonical/.*/bin/evil: \\d+: " +
		"/apps/hello-world.canonical/.*/bin/evil: " +
		"cannot create /var/tmp/myevil.txt: Permission denied\n"

	c.Assert(string(echoOutput), check.Matches, expected)
}

var _ = check.Suite(&pythonWebserverExampleSuite{})

type pythonWebserverExampleSuite struct {
	common.SnappySuite
}

func (s *pythonWebserverExampleSuite) TestNetworkingServiceMustBeStarted(c *check.C) {
	baseAppName := "xkcd-webserver"
	appName := baseAppName + ".canonical"
	common.InstallSnap(c, appName)
	defer common.RemoveSnap(c, appName)

	err := wait.ForServerOnPort(c, "tcp", 80)
	c.Assert(err, check.IsNil)

	resp, err := http.Get("http://localhost")
	c.Assert(err, check.IsNil)
	c.Check(resp.Status, check.Equals, "200 OK")
	c.Assert(resp.Proto, check.Equals, "HTTP/1.0")
}

var _ = check.Suite(&goWebserverExampleSuite{})

type goWebserverExampleSuite struct {
	common.SnappySuite
}

func (s *goWebserverExampleSuite) TestGetRootPathMustPrintMessage(c *check.C) {
	appName := "go-example-webserver"
	common.InstallSnap(c, appName)
	defer common.RemoveSnap(c, appName)

	err := wait.ForServerOnPort(c, "tcp6", 8081)
	c.Assert(err, check.IsNil, check.Commentf("Error waiting for server: %s", err))

	resp, err := http.Get("http://localhost:8081/")
	defer resp.Body.Close()
	c.Assert(err, check.IsNil, check.Commentf("Error getting the http resource: %s", err))
	c.Check(resp.Status, check.Equals, "200 OK", check.Commentf("Wrong reply status"))
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, check.IsNil, check.Commentf("Error reading the reply body: %s", err))	
	c.Assert(string(body), check.Equals, "Hello World\n", check.Commentf("Wrong reply body"))
}

var _ = check.Suite(&frameworkExampleSuite{})

type frameworkExampleSuite struct {
	common.SnappySuite
}

func (s *frameworkExampleSuite) TestFrameworkClient(c *check.C) {
	common.InstallSnap(c, "hello-dbus-fwk.canonical")
	defer common.RemoveSnap(c, "hello-dbus-fwk.canonical")

	common.InstallSnap(c, "hello-dbus-app.canonical")
	defer common.RemoveSnap(c, "hello-dbus-app.canonical")

	output := cli.ExecCommand(c, "hello-dbus-app.client")

	expected := "PASS\n"

	c.Assert(output, check.Equals, expected,
		check.Commentf("Expected output %s not found, %s", expected, output))
}

var _ = check.Suite(&configExampleSuite{})

type configExampleSuite struct {
	common.SnappySuite
}

var configTests = []struct {
	snap    string
	origin  string
	message string
}{
	{"config-example", "", "test config example message"},
	{"config-example-bash", ".canonical",	"test config example bash message"},
}

func (s *configExampleSuite) TestPrintMessageFromConfig(c *check.C) {
	for _, t := range configTests {
		common.InstallSnap(c, t.snap+t.origin)
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
