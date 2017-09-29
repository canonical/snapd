package main_test

import (
	"net/http"

	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func assertRequest(c *check.C, r *http.Request, method, path string) {
	c.Assert(r.Method, check.Equals, method)
	c.Assert(r.URL.Path, check.Equals, path)
}

func (s *SnapSuite) TestStoreSet(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "PUT", "/v2/store")

		w.Write([]byte(`{
			"type": "sync",
			"result": {
				"url": "foo"
			}
		}`))
	})

	_, err := snap.Parser().ParseArgs([]string{"store", "foo"})
	c.Assert(err, check.IsNil)
}

func (s *SnapSuite) TestStoreRevert(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		assertRequest(c, r, "DELETE", "/v2/store")

		w.Write([]byte(`{
			"type": "sync"
		}`))
	})

	_, err := snap.Parser().ParseArgs([]string{"store", "--revert"})
	c.Assert(err, check.IsNil)
}

func (s *SnapSuite) TestStoreAPIError(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"type": "error",
			"result": {
				"message": "an error message",
				"status": 400
			}
		}`))
	})

	_, err := snap.Parser().ParseArgs([]string{"store", "--revert"})
	c.Check(err, check.ErrorMatches, "an error message")
}
