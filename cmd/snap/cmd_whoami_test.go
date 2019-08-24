package main_test

import (
	"net/http"

	"github.com/snapcore/snapd/osutil"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestWhoamiLoggedInUser(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Errorf("whoami does not hit the API, it just reads ~/.snap/auth.json (which is mocked in the tests)")
	})

	s.Login(c)

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"whoami"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout(), Equals, "email: hello@mail.com\n")

	s.Logout(c)
}

func (s *SnapSuite) TestWhoamiNotLoggedInUser(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Errorf("whoami does not hit the API, it just reads ~/.snap/auth.json (which is mocked in the tests)")
	})

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"whoami"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout(), Equals, "email: -\n")
}

func (s *SnapSuite) TestWhoamiExtraParamError(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"whoami", "test"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "too many arguments for command")
}

func (s *SnapSuite) TestWhoamiEmptyAuthFile(c *C) {
	s.Login(c)

	wErr := osutil.AtomicWriteFile(s.AuthFile, []byte(``), 0600, 0)
	c.Assert(wErr, IsNil)

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"whoami"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "EOF")

	s.Logout(c)
}
