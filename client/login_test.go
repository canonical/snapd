// -*- Mode: Go; indent-tabs-mode: t -*-

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

package client_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

func (cs *clientSuite) TestClientLogin(c *check.C) {
	cs.rsp = `{"type": "sync", "result":
                     {"username": "the-user-name",
                      "macaroon": "the-root-macaroon",
                      "discharges": ["discharge-macaroon"]}}`

	outfile := filepath.Join(c.MkDir(), "json")
	os.Setenv(client.TestAuthFileEnvKey, outfile)
	defer os.Unsetenv(client.TestAuthFileEnvKey)

	c.Assert(cs.cli.LoggedInUser(), check.IsNil)

	user, err := cs.cli.Login("username", "pass", "")
	c.Check(err, check.IsNil)
	c.Check(user, check.DeepEquals, &client.User{
		Username:   "the-user-name",
		Macaroon:   "the-root-macaroon",
		Discharges: []string{"discharge-macaroon"}})

	c.Assert(cs.cli.LoggedInUser(), check.Not(check.IsNil))

	c.Check(osutil.FileExists(outfile), check.Equals, true)
	c.Check(outfile, testutil.FileEquals, `{"username":"the-user-name","macaroon":"the-root-macaroon","discharges":["discharge-macaroon"]}`)
}

func (cs *clientSuite) TestClientLoginWhenLoggedIn(c *check.C) {
	cs.rsp = `{"type": "sync", "result":
                     {"username": "the-user-name",
                      "email": "zed@bar.com",
                      "macaroon": "the-root-macaroon",
                      "discharges": ["discharge-macaroon"]}}`

	outfile := filepath.Join(c.MkDir(), "json")
	os.Setenv(client.TestAuthFileEnvKey, outfile)
	defer os.Unsetenv(client.TestAuthFileEnvKey)

	err := ioutil.WriteFile(outfile, []byte(`{"email":"foo@bar.com","macaroon":"macaroon"}`), 0600)
	c.Assert(err, check.IsNil)
	c.Assert(cs.cli.LoggedInUser(), check.DeepEquals, &client.User{
		Email:    "foo@bar.com",
		Macaroon: "macaroon",
	})

	user, err := cs.cli.Login("username", "pass", "")
	expected := &client.User{
		Email:      "zed@bar.com",
		Username:   "the-user-name",
		Macaroon:   "the-root-macaroon",
		Discharges: []string{"discharge-macaroon"},
	}
	c.Check(err, check.IsNil)
	c.Check(user, check.DeepEquals, expected)
	c.Check(cs.req.Header.Get("Authorization"), check.Matches, `Macaroon root="macaroon"`)

	c.Assert(cs.cli.LoggedInUser(), check.DeepEquals, expected)

	c.Check(osutil.FileExists(outfile), check.Equals, true)
	c.Check(outfile, testutil.FileEquals, `{"username":"the-user-name","email":"zed@bar.com","macaroon":"the-root-macaroon","discharges":["discharge-macaroon"]}`)
}

func (cs *clientSuite) TestClientLoginError(c *check.C) {
	cs.rsp = `{
		"result": {},
		"status": "Bad Request",
		"status-code": 400,
		"type": "error"
	}`

	outfile := filepath.Join(c.MkDir(), "json")
	os.Setenv(client.TestAuthFileEnvKey, outfile)
	defer os.Unsetenv(client.TestAuthFileEnvKey)

	user, err := cs.cli.Login("username", "pass", "")

	c.Check(user, check.IsNil)
	c.Check(err, check.NotNil)

	c.Check(osutil.FileExists(outfile), check.Equals, false)
}

func (cs *clientSuite) TestClientLogout(c *check.C) {
	cs.rsp = `{"type": "sync", "result": {}}`

	outfile := filepath.Join(c.MkDir(), "json")
	os.Setenv(client.TestAuthFileEnvKey, outfile)
	defer os.Unsetenv(client.TestAuthFileEnvKey)

	err := ioutil.WriteFile(outfile, []byte(`{"macaroon":"macaroon","discharges":["discharged"]}`), 0600)
	c.Assert(err, check.IsNil)

	err = cs.cli.Logout()
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, fmt.Sprintf("/v2/logout"))

	c.Check(osutil.FileExists(outfile), check.Equals, false)
}

func (cs *clientSuite) TestWriteAuthData(c *check.C) {
	outfile := filepath.Join(c.MkDir(), "json")
	os.Setenv(client.TestAuthFileEnvKey, outfile)
	defer os.Unsetenv(client.TestAuthFileEnvKey)

	authData := client.User{
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	}
	err := client.TestWriteAuth(authData)
	c.Assert(err, check.IsNil)

	c.Check(osutil.FileExists(outfile), check.Equals, true)
	c.Check(outfile, testutil.FileEquals, `{"macaroon":"macaroon","discharges":["discharge"]}`)
}

func (cs *clientSuite) TestReadAuthData(c *check.C) {
	outfile := filepath.Join(c.MkDir(), "json")
	os.Setenv(client.TestAuthFileEnvKey, outfile)
	defer os.Unsetenv(client.TestAuthFileEnvKey)

	authData := client.User{
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	}
	err := client.TestWriteAuth(authData)
	c.Assert(err, check.IsNil)

	readUser, err := client.TestReadAuth()
	c.Assert(err, check.IsNil)
	c.Check(readUser, check.DeepEquals, &authData)
}
