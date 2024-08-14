// Copyright (c) 2024 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package daemon_test

import (
	"fmt"
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/testutil"
)

var _ = Suite(&promptingSuite{})

type promptingSuite struct {
	apiBaseSuite
}

func (s *promptingSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectReadAccess(daemon.InterfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}})
}

func (s *promptingSuite) TestGetUserID(c *C) {
	for _, testCase := range []struct {
		path         string
		uid          string
		expectedUser uint32
		expectedCode int
		expectedErr  string
	}{
		{
			path:         "/v2/interfaces/requests/prompts",
			uid:          "invalid",
			expectedUser: 0,
			expectedCode: 403,
			expectedErr:  "cannot get remote user: ",
		},
		{
			path:         "/v2/interfaces/requests/prompts",
			uid:          "1000",
			expectedUser: 1000,
			expectedCode: 200,
			expectedErr:  "",
		},
		{
			path:         "/v2/interfaces/requests/prompts?user-id=1000",
			uid:          "1000",
			expectedUser: 0,
			expectedCode: 403,
			expectedErr:  `only admins may use the "user-id" parameter`,
		},
		{
			path:         "/v2/interfaces/requests/prompts?user-id=1000&user-id=1234",
			uid:          "0",
			expectedUser: 0,
			expectedCode: 400,
			expectedErr:  `invalid "user-id" parameter: must only include one "user-id"`,
		},
		{
			path:         "/v2/interfaces/requests/prompts?user-id=invalid",
			uid:          "0",
			expectedUser: 0,
			expectedCode: 400,
			expectedErr:  `invalid "user-id" parameter: `,
		},
		{
			path:         "/v2/interfaces/requests/prompts?user-id=-1",
			uid:          "0",
			expectedUser: 0,
			expectedCode: 400,
			expectedErr:  `invalid "user-id" parameter: user ID is not a valid uint32: `,
		},
		{
			path:         "/v2/interfaces/requests/prompts?user-id=1234",
			uid:          "0",
			expectedUser: 1234,
			expectedCode: 200,
			expectedErr:  "",
		},
	} {
		req, err := http.NewRequest("GET", testCase.path, nil)
		c.Assert(err, IsNil)
		req.RemoteAddr = fmt.Sprintf("pid=100;uid=%s;socket=;", testCase.uid)

		userID, rsp := daemon.GetUserID(req)
		if testCase.expectedErr == "" {
			c.Check(rsp, IsNil)
		} else {
			rspe, ok := rsp.(*daemon.APIError)
			c.Assert(ok, Equals, true)
			c.Check(rspe.Status, Equals, testCase.expectedCode)
			c.Check(rspe.Message, testutil.Contains, testCase.expectedErr)
		}
		c.Check(userID, Equals, testCase.expectedUser)
	}
}

func (s *promptingSuite) TestGetPrompts(c *C) {
}

func (s *promptingSuite) TestGetPrompt(c *C) {
}

func (s *promptingSuite) TestPostPrompt(c *C) {
	s.expectWriteAccess(daemon.InterfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}})
}

func (s *promptingSuite) TestGetRules(c *C) {
}

func (s *promptingSuite) TestPostRules(c *C) {
	s.expectWriteAccess(daemon.InterfaceAuthenticatedAccess{Interfaces: []string{"snap-interfaces-requests-control"}})
}

func (s *promptingSuite) TestGetRule(c *C) {
}

func (s *promptingSuite) TestPostRule(c *C) {
	s.expectWriteAccess(daemon.InterfaceAuthenticatedAccess{Interfaces: []string{"snap-interfaces-requests-control"}})
}
