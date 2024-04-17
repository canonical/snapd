// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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
	"io"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
)

func (cs *clientSuite) TestClientRemoveUser(c *C) {
	removed, err := cs.cli.RemoveUser(&client.RemoveUserOptions{})
	c.Assert(err, ErrorMatches, "cannot remove a user without providing a username")
	c.Assert(removed, IsNil)

	cs.rsp = `{
		"type": "sync",
		"result": {
                   "removed": [{"id": 11, "username": "one-user", "email": "user@test.com"}]
                }
	}`
	removed, err = cs.cli.RemoveUser(&client.RemoveUserOptions{Username: "one-user"})
	c.Assert(cs.req.Method, Equals, "POST")
	c.Assert(cs.req.URL.Path, Equals, "/v2/users")
	c.Assert(err, IsNil)
	c.Assert(removed, DeepEquals, []*client.User{
		{ID: 11, Username: "one-user", Email: "user@test.com"},
	})

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, `{"action":"remove","username":"one-user"}`)
}

func (cs *clientSuite) TestClientRemoveUserError(c *C) {
	removed, err := cs.cli.RemoveUser(nil)
	c.Assert(err, ErrorMatches, "cannot remove a user without providing a username")
	c.Assert(removed, IsNil)
	removed, err = cs.cli.RemoveUser(&client.RemoveUserOptions{})
	c.Assert(err, ErrorMatches, "cannot remove a user without providing a username")
	c.Assert(removed, IsNil)

	cs.rsp = `{
		"type": "error",
		"result": {"message": "no can do"}
	}`
	removed, err = cs.cli.RemoveUser(&client.RemoveUserOptions{Username: "one-user"})
	c.Assert(cs.req.Method, Equals, "POST")
	c.Assert(cs.req.URL.Path, Equals, "/v2/users")
	c.Assert(err, ErrorMatches, "no can do")
	c.Assert(removed, IsNil)

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, `{"action":"remove","username":"one-user"}`)
}

func (cs *clientSuite) TestClientCreateUser(c *C) {
	_, err := cs.cli.CreateUser(nil)
	c.Assert(err, ErrorMatches, "cannot create a user without providing an email")
	_, err = cs.cli.CreateUser(&client.CreateUserOptions{})
	c.Assert(err, ErrorMatches, "cannot create a user without providing an email")

	cs.rsp = `{
		"type": "sync",
		"result": [{
                        "username": "karl",
                        "ssh-keys": ["one", "two"]
		}]
	}`
	rsp, err := cs.cli.CreateUser(&client.CreateUserOptions{Email: "one@email.com", Sudoer: true, Known: true})
	c.Assert(cs.req.Method, Equals, "POST")
	c.Assert(cs.req.URL.Path, Equals, "/v2/users")
	c.Assert(err, IsNil)

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, IsNil)
	c.Assert(string(body), Equals, `{"action":"create","email":"one@email.com","sudoer":true,"known":true}`)

	c.Assert(rsp, DeepEquals, &client.CreateUserResult{
		Username: "karl",
		SSHKeys:  []string{"one", "two"},
	})
}

var createUsersTests = []struct {
	options   []*client.CreateUserOptions
	bodies    []string
	responses []string
	results   []*client.CreateUserResult
	error     string
}{{
	// nothing in -> nothing out
	options: nil,
}, {
	options: []*client.CreateUserOptions{nil},
	error:   "cannot create user from store details without an email to query for",
}, {
	options: []*client.CreateUserOptions{{}},
	error:   "cannot create user from store details without an email to query for",
}, {
	options: []*client.CreateUserOptions{{
		Email:  "one@example.com",
		Sudoer: true,
	}, {
		Known: true,
	}},
	bodies: []string{
		`{"action":"create","email":"one@example.com","sudoer":true}`,
		`{"action":"create","known":true}`,
	},
	responses: []string{
		`{"type": "sync", "result": [{"username": "one", "ssh-keys":["a", "b"]}]}`,
		`{"type": "sync", "result": [{"username": "two"}, {"username": "three"}]}`,
	},
	results: []*client.CreateUserResult{{
		Username: "one",
		SSHKeys:  []string{"a", "b"},
	}, {
		Username: "two",
	}, {
		Username: "three",
	},
	},
}, {
	options: []*client.CreateUserOptions{{
		Automatic: true,
	}},
	bodies: []string{
		`{"action":"create","automatic":true}`,
	},
	responses: []string{
		// for automatic result can be empty
		`{"type": "sync", "result": []}`,
	},
},
}

func (cs *clientSuite) TestClientCreateUsers(c *C) {
	for _, test := range createUsersTests {
		cs.reqs = nil
		cs.rsps = test.responses

		results, err := cs.cli.CreateUsers(test.options)
		if test.error != "" {
			c.Assert(err, ErrorMatches, test.error)
		}
		c.Assert(results, DeepEquals, test.results)

		var bodies []string
		for _, req := range cs.reqs {
			c.Assert(req.Method, Equals, "POST")
			c.Assert(req.URL.Path, Equals, "/v2/users")
			data, err := io.ReadAll(req.Body)
			c.Assert(err, IsNil)
			bodies = append(bodies, string(data))
		}

		c.Assert(bodies, DeepEquals, test.bodies)
	}
}

func (cs *clientSuite) TestClientJSONError(c *C) {
	cs.rsp = `some non-json error message`
	_, err := cs.cli.SysInfo()
	c.Assert(err, ErrorMatches, `cannot obtain system details: cannot decode "some non-json error message": invalid char.*`)
}

func (cs *clientSuite) TestUsers(c *C) {
	cs.rsp = `{"type": "sync", "result":
                     [{"username": "foo","email":"foo@example.com"},
                      {"username": "bar","email":"bar@example.com"}]}`
	users, err := cs.cli.Users()
	c.Check(err, IsNil)
	c.Check(users, DeepEquals, []*client.User{
		{Username: "foo", Email: "foo@example.com"},
		{Username: "bar", Email: "bar@example.com"},
	})
}
