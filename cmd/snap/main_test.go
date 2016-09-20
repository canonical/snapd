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
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"

	snap "github.com/snapcore/snapd/cmd/snap"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type BaseSnapSuite struct {
	testutil.BaseTest
	stdin  *bytes.Buffer
	stdout *bytes.Buffer
	stderr *bytes.Buffer

	AuthFile string
}

func (s *BaseSnapSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.stdin = bytes.NewBuffer(nil)
	s.stdout = bytes.NewBuffer(nil)
	s.stderr = bytes.NewBuffer(nil)
	snap.Stdin = s.stdin
	snap.Stdout = s.stdout
	snap.Stderr = s.stderr
	s.AuthFile = filepath.Join(c.MkDir(), "json")
	os.Setenv(TestAuthFileEnvKey, s.AuthFile)
}

func (s *BaseSnapSuite) TearDownTest(c *C) {
	snap.Stdin = os.Stdin
	snap.Stdout = os.Stdout
	snap.Stderr = os.Stderr
	c.Assert(s.AuthFile == "", Equals, false)
	err := os.Unsetenv(TestAuthFileEnvKey)
	c.Assert(err, IsNil)
	s.BaseTest.TearDownTest(c)
}

func (s *BaseSnapSuite) Stdout() string {
	return s.stdout.String()
}

func (s *BaseSnapSuite) Stderr() string {
	return s.stderr.String()
}

func (s *BaseSnapSuite) RedirectClientToTestServer(handler func(http.ResponseWriter, *http.Request)) {
	server := httptest.NewServer(http.HandlerFunc(handler))
	s.BaseTest.AddCleanup(func() { server.Close() })
	snap.ClientConfig.BaseURL = server.URL
	s.BaseTest.AddCleanup(func() { snap.ClientConfig.BaseURL = "" })
}

func (s *BaseSnapSuite) Login(c *C) {
	err := osutil.AtomicWriteFile(s.AuthFile, []byte(TestAuthFileContents), 0600, 0)
	c.Assert(err, IsNil)
}

func (s *BaseSnapSuite) Logout(c *C) {
	c.Assert(s.AuthFile == "", Equals, false)
	err := os.Remove(s.AuthFile)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(s.AuthFile), Equals, false)
}

type SnapSuite struct {
	BaseSnapSuite
}

var _ = Suite(&SnapSuite{})

// DecodedRequestBody returns the JSON-decoded body of the request.
func DecodedRequestBody(c *C, r *http.Request) map[string]interface{} {
	var body map[string]interface{}
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&body)
	c.Assert(err, IsNil)
	return body
}

// EncodeResponseBody writes JSON-serialized body to the response writer.
func EncodeResponseBody(c *C, w http.ResponseWriter, body interface{}) {
	encoder := json.NewEncoder(w)
	err := encoder.Encode(body)
	c.Assert(err, IsNil)
}

func mockArgs(args ...string) (restore func()) {
	old := os.Args
	os.Args = args
	return func() { os.Args = old }
}

func mockVersion(v string) (restore func()) {
	old := cmd.Version
	cmd.Version = v
	return func() { cmd.Version = old }
}

const TestAuthFileEnvKey = "SNAPPY_STORE_AUTH_DATA_FILENAME"
const TestAuthFileContents = `{"macaroon":"MDAzMWxvY2F0aW9uIG15YXBwcy5kZXZlbG9wZXIuc3RhZ2luZy51YnVudHUuY29tCjAwMTZpZGVudGlmaWVyIE15QXBwcwowMDUzY2lkIG15YXBwcy5kZXZlbG9wZXIuc3RhZ2luZy51YnVudHUuY29tfHZhbGlkX3NpbmNlfDIwMTYtMDktMTNUMTA6NDg6MDcuMjUxNzQ4CjAyZDFjaWQgeyJzZWNyZXQiOiAiVGF0dmE3VUJwYkZweHh3MHB2NTRrcS9yVjFnckUyZWt5QTkrQlZnTEhrbmpGam9tY0dGQ2lUZEI2cDJNVWNTZm0wUEFSRUxWZ3gzcG90Sm9MWWVjcmhsaWlFM2xGZGZUU1ZaSXNia0xZMXF1cFduVWZ3Y1RZOWRib1cwamNWV1EzL0RtcmFIV1NjQ0VsM3ZtenZOczJtV3dkRldxYlY1UEluNldMeFNBUy9PckdUOXk4YzZaZ0ZVbHZ2a2lGY3N3NHJjME45ZVhKcGxQZXV3NnZiU2tqQWlSYklFekc2N1IwRnhhc1JhZkUzd3NaTTJjeEdXQ0dmUitTeEh6dnV4Q1VtZm41d1liTVkxdnlCMmFNTEpTNE5rejJtdTEwbTZ1SFBGMnNsWmRNWklOUTZSRC9vTzBQMVpkZ1hmYSt6NnRYREEwcVFHSEJ3TkZPVE9MRDdKWXdUcG9DTy9BNzJPVHgxSUp2ellidzlYVVlQRFVUZ2MzSXBCc0NIRWI5UDJUMXdZRjJwdEhrcUdXZFMvSmk0UkQ4NDZLZTJuN1lCUmN5L0xJbzFURWNseEpOb3IzeDhBVzlRWlZwNWZHNWE2dElGT2pqZVlZbEw4a0wrcndkcURaWkxmOTZxZGdtTkRVQnF1V1BmcGd0VEE2U3Q3WFBHWnpMZzBoUTduWHhEVjVBT0NGMXhubXFseWYzTTdnQ0tIRzVqWmx2ZkRHVUk3OWg4THJvc00yaDZZVnk5ZllwczVOck0vdXJ1THpvSXlic1dtaWw0RnVmelFDbWl6YVZCMXpZSlMyRUVCTVNLVUJCdFZmL2owTThDMTdFajU5R1REeGZ6SW9rRkFHNzlQWG1ySUpJaDU4TWsrckM3ZDBabWo3QmNUS3dqUDQ1Uk5vRWFDamhJdEdXQkU9IiwgInZlcnNpb24iOiAxfQowMDUxdmlkIBlxMHnHn-0WPt1EvRG_z5C7s6JEAExK29jBHPTC1viEEdFLT-D5eZJhQIweP-q_vlKN1GtyVrAcLCbshbLxlIdP2-HS-uZriwowMDIwY2wgbG9naW4uc3RhZ2luZy51YnVudHUuY29tCjAwNTdjaWQgbXlhcHBzLmRldmVsb3Blci5zdGFnaW5nLnVidW50dS5jb218YWNsfFsicGFja2FnZV9hY2Nlc3MiLCAicGFja2FnZV9wdXJjaGFzZSJdCjAwMmZzaWduYXR1cmUgayfZk0IsVki5dqXN3HlDV0KApbES60t5pd1J5ERASJkK","discharges":["MDAyNmxvY2F0aW9uIGxvZ2luLnN0YWdpbmcudWJ1bnR1LmNvbQowMmQ4aWRlbnRpZmllciB7InNlY3JldCI6ICJUYXR2YTdVQnBiRnB4eHcwcHY1NGtxL3JWMWdyRTJla3lBOStCVmdMSGtuakZqb21jR0ZDaVRkQjZwMk1VY1NmbTBQQVJFTFZneDNwb3RKb0xZZWNyaGxpaUUzbEZkZlRTVlpJc2JrTFkxcXVwV25VZndjVFk5ZGJvVzBqY1ZXUTMvRG1yYUhXU2NDRWwzdm16dk5zMm1Xd2RGV3FiVjVQSW42V0x4U0FTL09yR1Q5eThjNlpnRlVsdnZraUZjc3c0cmMwTjllWEpwbFBldXc2dmJTa2pBaVJiSUV6RzY3UjBGeGFzUmFmRTN3c1pNMmN4R1dDR2ZSK1N4SHp2dXhDVW1mbjV3WWJNWTF2eUIyYU1MSlM0Tmt6Mm11MTBtNnVIUEYyc2xaZE1aSU5RNlJEL29PMFAxWmRnWGZhK3o2dFhEQTBxUUdIQndORk9UT0xEN0pZd1Rwb0NPL0E3Mk9UeDFJSnZ6WWJ3OVhVWVBEVVRnYzNJcEJzQ0hFYjlQMlQxd1lGMnB0SGtxR1dkUy9KaTRSRDg0NktlMm43WUJSY3kvTElvMVRFY2x4Sk5vcjN4OEFXOVFaVnA1Zkc1YTZ0SUZPamplWVlsTDhrTCtyd2RxRFpaTGY5NnFkZ21ORFVCcXVXUGZwZ3RUQTZTdDdYUEdaekxnMGhRN25YeERWNUFPQ0YxeG5tcWx5ZjNNN2dDS0hHNWpabHZmREdVSTc5aDhMcm9zTTJoNllWeTlmWXBzNU5yTS91cnVMem9JeWJzV21pbDRGdWZ6UUNtaXphVkIxellKUzJFRUJNU0tVQkJ0VmYvajBNOEMxN0VqNTlHVER4ZnpJb2tGQUc3OVBYbXJJSkloNThNaytyQzdkMFptajdCY1RLd2pQNDVSTm9FYUNqaEl0R1dCRT0iLCAidmVyc2lvbiI6IDF9CjAwZGVjaWQgbG9naW4uc3RhZ2luZy51YnVudHUuY29tfGFjY291bnR8ZXlKMWMyVnlibUZ0WlNJNklDSm9lbFJLUm5reklpd2dJbTl3Wlc1cFpDSTZJQ0pvZWxSS1Jua3pJaXdnSW1ScGMzQnNZWGx1WVcxbElqb2dJbEJsZEdVZ1YyOXZaSE1pTENBaVpXMWhhV3dpT2lBaWMzUmhaMmx1Wnl0bGJXRnBiRUJ3WlhSbExYZHZiMlJ6TG1OdmJTSXNJQ0pwYzE5MlpYSnBabWxsWkNJNklIUnlkV1Y5CjAwNDhjaWQgbG9naW4uc3RhZ2luZy51YnVudHUuY29tfHZhbGlkX3NpbmNlfDIwMTYtMDktMTNUMTA6NDg6MDguNTYzNjk0CjAwNDZjaWQgbG9naW4uc3RhZ2luZy51YnVudHUuY29tfGxhc3RfYXV0aHwyMDE2LTA5LTEzVDEwOjQ4OjA4LjU2MzY5NAowMDQ0Y2lkIGxvZ2luLnN0YWdpbmcudWJ1bnR1LmNvbXxleHBpcmVzfDIwMTctMDktMTNUMTA6NDg6MDguNTYzNzY5CjAwMmZzaWduYXR1cmUg_ADfFwfJjjN3Eorq2NAQVcNRwwAk5-jZQWUgRKRrii4K"]}`

func (s *SnapSuite) TestErrorResult(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "cannot do something"}}`)
	})

	restore := mockArgs("snap", "install", "foo")
	defer restore()

	err := snap.RunMain()
	c.Assert(err, ErrorMatches, `cannot do something`)
}

func (s *SnapSuite) TestAccessDeniedHint(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "access denied", "kind": "login-required"}, "status-code": 401}`)
	})

	restore := mockArgs("snap", "install", "foo")
	defer restore()

	err := snap.RunMain()
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `access denied (try with sudo)`)
}

func (s *SnapSuite) TestExtraArgs(c *C) {
	restore := mockArgs("snap", "abort", "1", "xxx", "zzz")
	defer restore()

	err := snap.RunMain()
	c.Assert(err, ErrorMatches, `too many arguments for command`)
}

func (s *SnapSuite) TestVersionOnClassic(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":{"on-classic":true,"os-release":{"id":"ubuntu","version-id":"12.34"},"series":"56","version":"7.89"}}`)
	})
	restore := mockArgs("snap", "--version")
	defer restore()
	restore = mockVersion("4.56")
	defer restore()

	c.Assert(func() { snap.RunMain() }, PanicMatches, `internal error: exitStatus\{0\} .*`)
	c.Assert(s.Stdout(), Equals, "snap    4.56\nsnapd   7.89\nseries  56\nubuntu  12.34\n")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestVersionOnAllSnap(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":{"os-release":{"id":"ubuntu","version-id":"12.34"},"series":"56","version":"7.89"}}`)
	})
	restore := mockArgs("snap", "--version")
	defer restore()
	restore = mockVersion("4.56")
	defer restore()

	c.Assert(func() { snap.RunMain() }, PanicMatches, `internal error: exitStatus\{0\} .*`)
	c.Assert(s.Stdout(), Equals, "snap    4.56\nsnapd   7.89\nseries  56\n")
	c.Assert(s.Stderr(), Equals, "")
}
