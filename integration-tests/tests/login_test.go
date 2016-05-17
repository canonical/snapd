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
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/check.v1"

	"github.com/kr/pty"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/wait"
)

const (
	loginHost        = "login.ubuntu.com"
	invalidLoginName = "loginname"
	validLoginName   = "loginname@example.com"
	password         = "password"
)

var stdout bytes.Buffer

func authenticate(loginName, pass string) error {
	cmds, _ := cli.AddOptionsToCommand([]string{"snap", "login", loginName})
	cmd := exec.Command(cmds[0], cmds[1:]...)
	f, err := pty.Start(cmd)
	if err != nil {
		return err
	}

	err = cmdInteract(f, "Password: ", pass)
	if err != nil {
		return err
	}
	io.Copy(&stdout, f)

	return nil
}

func cmdInteract(f *os.File, prompt, input string) error {
	buf := make([]byte, len(prompt))

	_, err := io.ReadFull(f, buf)
	if err != nil {
		return err
	}

	if string(buf) != prompt {
		return fmt.Errorf("got unexpected prompt: %q, expecting %s", string(buf), prompt)
	}

	if _, err := f.Write([]byte(input + "\n")); err != nil {
		return err
	}

	return nil
}

var _ = check.Suite(&loginSuite{})

type loginSuite struct {
	common.SnappySuite

	server         *httptest.Server
	serverAddrPort string
}

func (s *loginSuite) TestStoreEmptyLoginNameError(c *check.C) {
	output, err := cli.ExecCommandErr("sudo", "snap", "login")

	c.Assert(err, check.NotNil, check.Commentf("expecting empty login error"))
	c.Assert(output, check.Equals, "error: the required argument `email` was not provided\n")
}

func (s *loginSuite) TestStoreInvalidLoginError(c *check.C) {
	err := authenticate(invalidLoginName, password)
	c.Assert(err, check.IsNil, check.Commentf("error writting credentials"))

	expectedMsg := "Invalid request data"
	err = wait.ForFunction(c, expectedMsg, func() (string, error) { return stdout.String(), err })
	c.Assert(err, check.IsNil, check.Commentf("didn't get expected invalid data error: %v", err))
}

func (s *loginSuite) TestStoreInvalidCredentialsError(c *check.C) {
	err := authenticate(validLoginName, password)
	c.Assert(err, check.IsNil, check.Commentf("error writting credentials"))

	expectedMsg := "Provided email/password is not correct"
	err = wait.ForFunction(c, expectedMsg, func() (string, error) { return stdout.String(), err })
	c.Assert(err, check.IsNil, check.Commentf("didn't get expected invalid credentials error: %v", err))
}

func (s *loginSuite) TestFakeServerIsDetected(c *check.C) {
	s.setUpHTTPServer()
	s.setUpIPTables(c)
	defer s.tearDownIPTables(c)
	defer s.tearDownHTTPServer()

	err := authenticate(validLoginName, password)
	c.Assert(err, check.IsNil, check.Commentf("error writting credentials"))

	expectedMsg := fmt.Sprintf("Post https://%s/api/v2/tokens/discharge: x509: certificate is valid for example.com, not %s", loginHost, loginHost)
	err = wait.ForFunction(c, expectedMsg, func() (string, error) { return stdout.String(), err })
	c.Assert(err, check.IsNil, check.Commentf("didn't get expected fake server error: %v", err))
}

func (s *loginSuite) handler(w http.ResponseWriter, r *http.Request) {
	log.Panicf("in fake server handler, we should never get this far")
}

func (s *loginSuite) setUpHTTPServer() {
	s.server = httptest.NewTLSServer(http.HandlerFunc(s.handler))

	// URL is of the form https://ipaddr:port without trailing slash
	s.serverAddrPort = strings.TrimLeft(s.server.URL, "https://")
}

func (s *loginSuite) setUpIPTables(c *check.C) {
	cmd := ipTablesAddCommand(s.serverAddrPort)
	_, err := cli.ExecCommandErr(cmd...)
	c.Assert(err, check.IsNil, check.Commentf("Error setting up iptables"))
}

func (s *loginSuite) tearDownHTTPServer() {
	s.server.Close()
}

func (s *loginSuite) tearDownIPTables(c *check.C) {
	cmd := ipTablesDelCommand(s.serverAddrPort)
	_, err := cli.ExecCommandErr(cmd...)
	c.Assert(err, check.IsNil, check.Commentf("Error tearing down iptables"))
}

func ipTablesAddCommand(serverAddrPort string) []string {
	return ipTablesCommand("A", serverAddrPort)
}

func ipTablesDelCommand(serverAddrPort string) []string {
	return ipTablesCommand("D", serverAddrPort)
}

// action can be A for adding or D for deleting
func ipTablesCommand(action, serverAddrPort string) []string {
	return []string{"sudo",
		"iptables", "-t", "nat", "-" + action, "OUTPUT", "-p", "tcp",
		"-d", loginHost, "--dport", "443", "-j", "DNAT",
		"--to-destination", serverAddrPort}
}

var _ = check.Suite(&storeSuite{})

type storeSuite struct {
	common.SnappySuite
}

func (s *storeSuite) TestStoreLoginLogout(c *check.C) {
	username := os.Getenv("TEST_USER_NAME")
	password := os.Getenv("TEST_USER_PASSWORD")

	if username == "" || password == "" {
		c.Skip("$TEST_USER_NAME or $TEST_USER_PASSWORD not defined")
	}

	err := authenticate(username, password)
	c.Assert(err, check.IsNil)

	output := cli.ExecCommand(c, "snap", "logout")
	c.Assert(output, check.Equals, "\n")
}
