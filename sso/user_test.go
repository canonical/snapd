// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !integrationcoverage

/*
 * Copyright (C) 2016 Canonical Ltd
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

package sso

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os/user"
	"path/filepath"
	"testing"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { check.TestingT(t) }

type createUserSuite struct {
	testutil.BaseTest
}

var _ = check.Suite(&createUserSuite{})

func (s *createUserSuite) redirectToTestSSO(handler func(http.ResponseWriter, *http.Request)) {
	server := httptest.NewServer(http.HandlerFunc(handler))
	s.BaseTest.AddCleanup(func() { server.Close() })
	SSOBaseURL = server.URL
	s.BaseTest.AddCleanup(func() { SSOBaseURL = "" })
}

func (s *createUserSuite) TestCreateUser(c *check.C) {
	n := 0
	s.redirectToTestSSO(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/api/v2/keys/popper@lse.ac.uk")
			fmt.Fprintln(w, `{"username": "karl", "ssh_keys": ["ssh-rsa AAAAB3Nz karl@hennie"]}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})

	addUserName := ""
	addUserKeys := []string{}
	addUser = func(name string, sshKeys []string) error {
		addUserName = name
		addUserKeys = sshKeys
		return nil
	}

	name, err := CreateUser("popper@lse.ac.uk")
	c.Assert(err, check.IsNil)
	c.Check(name, check.Equals, "karl")
	c.Check(addUserName, check.Equals, "karl")
	c.Check(addUserKeys, check.DeepEquals, []string{"ssh-rsa AAAAB3Nz karl@hennie"})
}

func (s *createUserSuite) TestAddUser(c *check.C) {
	mockHome := c.MkDir()
	userLookup = func(string) (*user.User, error) {
		return &user.User{
			HomeDir: mockHome,
		}, nil
	}
	runCommandArgs := []string{}
	runCommand = func(args ...string) error {
		runCommandArgs = args
		return nil
	}
	err := addUser("karl", []string{"ssh-key1", "ssh-key2"})
	c.Assert(err, check.IsNil)
	c.Check(runCommandArgs, check.DeepEquals, []string{
		"adduser", "--extrausers", "--disabled-password", "karl",
	})
	sshKeys, err := ioutil.ReadFile(filepath.Join(mockHome, ".ssh", "authorized_keys"))
	c.Assert(err, check.IsNil)
	c.Check(string(sshKeys), check.Equals, "ssh-key1\nssh-key2")
}
