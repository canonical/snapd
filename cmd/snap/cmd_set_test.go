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

package main_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"gopkg.in/check.v1"

	snapset "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
)

type snapSetSuite struct {
	BaseSnapSuite

	setConfApiCalls int
}

var _ = check.Suite(&snapSetSuite{})

func (s *snapSetSuite) SetUpTest(c *check.C) {
	s.BaseSnapSuite.SetUpTest(c)
	s.setConfApiCalls = 0
}

func (s *snapSetSuite) TestInvalidSetParameters(c *check.C) {
	invalidParameters := []string{"set", "snap-name", "key", "value"}
	_, err := snapset.Parser(snapset.Client()).ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, ".*invalid configuration:.*(want key=value).*")
	c.Check(s.setConfApiCalls, check.Equals, 0)
}

func (s *snapSetSuite) TestSnapSetIntegrationString(c *check.C) {
	// and mock the server
	s.mockSetConfigServer(c, "value")

	// Set a config value for the active snap
	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "snapname", "key=value"})
	c.Assert(err, check.IsNil)
	c.Check(s.setConfApiCalls, check.Equals, 1)
}

func (s *snapSetSuite) TestSnapSetIntegrationNumber(c *check.C) {
	// and mock the server
	s.mockSetConfigServer(c, json.Number("1.2"))

	// Set a config value for the active snap
	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "snapname", "key=1.2"})
	c.Assert(err, check.IsNil)
	c.Check(s.setConfApiCalls, check.Equals, 1)
}

func (s *snapSetSuite) TestSnapSetIntegrationBigInt(c *check.C) {
	// and mock the server
	s.mockSetConfigServer(c, json.Number("1234567890"))

	// Set a config value for the active snap
	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "snapname", "key=1234567890"})
	c.Assert(err, check.IsNil)
	c.Check(s.setConfApiCalls, check.Equals, 1)
}

func (s *snapSetSuite) TestSnapSetIntegrationJson(c *check.C) {
	// and mock the server
	s.mockSetConfigServer(c, map[string]interface{}{"subkey": "value"})

	// Set a config value for the active snap
	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "snapname", `key={"subkey":"value"}`})
	c.Assert(err, check.IsNil)
	c.Check(s.setConfApiCalls, check.Equals, 1)
}

func (s *snapSetSuite) TestSnapSetIntegrationUnsetWithExclamationMark(c *check.C) {
	// and mock the server
	s.mockSetConfigServer(c, nil)

	// Unset config value via exclamation mark
	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "snapname", "key!"})
	c.Assert(err, check.IsNil)
	c.Check(s.setConfApiCalls, check.Equals, 1)
}

func (s *snapSetSuite) TestSnapSetIntegrationStringWithExclamationMark(c *check.C) {
	// and mock the server
	s.mockSetConfigServer(c, "value!")

	// Set a config value ending with exclamation mark
	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "snapname", "key=value!"})
	c.Assert(err, check.IsNil)
	c.Check(s.setConfApiCalls, check.Equals, 1)
}

func (s *snapSetSuite) TestSnapSetParseStrictJSON(c *check.C) {
	// mock server
	s.mockSetConfigServer(c, map[string]interface{}{"a": "b", "c": json.Number("1"), "d": map[string]interface{}{"e": "f"}})

	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "snapname", "-t", `key={"a":"b", "c":1, "d": {"e": "f"}}`})
	c.Assert(err, check.IsNil)
	c.Check(s.setConfApiCalls, check.Equals, 1)
}

func (s *snapSetSuite) TestSnapSetFailParsingWithStrictJSON(c *check.C) {
	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "snapname", "-t", `key=notJSON`})
	c.Assert(err, check.ErrorMatches, "failed to parse JSON:.*")
}

func (s *snapSetSuite) TestSnapSetFailOnStrictJSONAndString(c *check.C) {
	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "snapname", "-t", "-s", "key={}"})
	c.Assert(err, check.ErrorMatches, "cannot use -t and -s together")
}

func (s *snapSetSuite) TestSnapSetAsString(c *check.C) {
	// mock server
	value := `{"a":"b", "c":1}`
	s.mockSetConfigServer(c, value)

	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "snapname", "-s", fmt.Sprintf("key=%s", value)})
	c.Assert(err, check.IsNil)
	c.Check(s.setConfApiCalls, check.Equals, 1)
}

func (s *snapSetSuite) mockSetConfigServer(c *check.C, expectedValue interface{}) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/snaps/snapname/conf":
			c.Check(r.Method, check.Equals, "PUT")
			c.Check(DecodedRequestBody(c, r), check.DeepEquals, map[string]interface{}{
				"key": expectedValue,
			})
			w.WriteHeader(202)
			fmt.Fprintln(w, `{"type":"async", "status-code": 202, "change": "zzz"}`)
			s.setConfApiCalls += 1
		case "/v2/changes/zzz":
			c.Check(r.Method, check.Equals, "GET")
			fmt.Fprintln(w, `{"type":"sync", "result":{"ready": true, "status": "Done"}}`)
		default:
			c.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
}

const asyncResp = `{
	"type": "async",
	"change": "123",
	"status-code": 202
}`

type registrySuite struct {
	BaseSnapSuite
	tmpDir string
}

var _ = check.Suite(&registrySuite{})

func (s *registrySuite) SetUp(c *check.C) {
	s.BaseSnapSuite.SetUpTest(c)
	s.tmpDir = c.MkDir()
}

func (s *registrySuite) mockRegistryFlag(c *check.C) (restore func()) {
	old := dirs.FeaturesDir
	dirs.FeaturesDir = s.tmpDir

	registryCtlFile := features.Registries.ControlFile()
	c.Assert(os.WriteFile(registryCtlFile, []byte(nil), 0644), check.IsNil)

	return func() {
		c.Assert(os.Remove(registryCtlFile), check.IsNil)
		dirs.FeaturesDir = old
	}
}

func (s *registrySuite) mockRegistryServer(c *check.C, expectedRequest string, nowait bool) {
	fail := func(w http.ResponseWriter, err error) {
		w.WriteHeader(500)
		fmt.Fprintf(w, `{"type": "error", "result": {"message": %q}}`, err)
		c.Error(err)
	}

	var reqs int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch reqs {
		case 0:
			c.Check(r.Method, check.Equals, "PUT")
			c.Check(r.URL.Path, check.Equals, "/v2/registry/foo/bar/baz")
			c.Check(r.URL.Query(), check.HasLen, 0)

			raw, err := io.ReadAll(r.Body)
			c.Check(err, check.IsNil)
			c.Check(string(raw), check.Equals, expectedRequest)

			w.WriteHeader(202)
			fmt.Fprintln(w, asyncResp)
		case 1:
			if nowait {
				err := fmt.Errorf("expected only one request, on %d (%v)", reqs+1, r)
				fail(w, err)
				return
			}

			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v2/changes/123")
			fmt.Fprintf(w, `{"type": "sync", "result": {"ready": true, "status": "Done"}}\n`)
		default:
			err := fmt.Errorf("expected to get 2 requests, now on %d (%v)", reqs+1, r)
			fail(w, err)
		}

		reqs++
	})
}

func (s *registrySuite) TestRegistrySet(c *check.C) {
	restore := s.mockRegistryFlag(c)
	defer restore()

	s.mockRegistryServer(c, `{"abc":"cba"}`, false)

	rest, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "foo/bar/baz", `abc="cba"`})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *registrySuite) TestRegistrySetMany(c *check.C) {
	restore := s.mockRegistryFlag(c)
	defer restore()

	s.mockRegistryServer(c, `{"abc":{"foo":1},"xyz":true}`, false)

	rest, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "foo/bar/baz", `abc={"foo":1}`, "xyz=true"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)

	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *registrySuite) TestRegistrySetInvalidAspectID(c *check.C) {
	restore := s.mockRegistryFlag(c)
	defer restore()

	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "foo//bar", "foo=bar"})
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, "registry identifier must conform to format: <account-id>/<registry>/<view>")
}

func (s *registrySuite) TestRegistrySetNoWait(c *check.C) {
	restore := s.mockRegistryFlag(c)
	defer restore()

	s.mockRegistryServer(c, `{"abc":1}`, true)

	rest, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "--no-wait", "foo/bar/baz", "abc=1"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)

	c.Check(s.Stdout(), check.Equals, "123\n")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *registrySuite) TestRegistrySetDisabledFlag(c *check.C) {
	var reqs int
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch reqs {
		default:
			err := fmt.Errorf("expected to get no requests, now on %d (%v)", reqs+1, r)
			w.WriteHeader(500)
			fmt.Fprintf(w, `{"type": "error", "result": {"message": %q}}`, err)
			c.Error(err)
		}

		reqs++
	})

	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "foo/bar/baz", "abc=1"})
	c.Assert(err, check.ErrorMatches, `the "registries" feature is disabled: set 'experimental.registries' to true`)
}

func (s *registrySuite) TestRegistrySetExclamationMark(c *check.C) {
	restore := s.mockRegistryFlag(c)
	defer restore()

	s.mockRegistryServer(c, `{"abc":null}`, false)

	_, err := snapset.Parser(snapset.Client()).ParseArgs([]string{"set", "foo/bar/baz", "abc!"})
	c.Assert(err, check.IsNil)
}
