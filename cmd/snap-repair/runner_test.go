// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	repair "github.com/snapcore/snapd/cmd/snap-repair"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

type runnerSuite struct {
	tmpdir string

	t0 time.Time

	storeSigning *assertstest.StoreStack

	brandSigning *assertstest.SigningDB
	brandAcct    *asserts.Account
	brandAcctKey *asserts.AccountKey

	modelAs *asserts.Model

	seedAssertsDir string
}

var _ = Suite(&runnerSuite{})

func (s *runnerSuite) SetUpSuite(c *C) {
	s.storeSigning = assertstest.NewStoreStack("canonical", nil)

	brandPrivKey, _ := assertstest.GenerateKey(752)

	s.brandAcct = assertstest.NewAccount(s.storeSigning, "my-brand", map[string]interface{}{
		"account-id": "my-brand",
	}, "")
	s.brandAcctKey = assertstest.NewAccountKey(s.storeSigning, s.brandAcct, nil, brandPrivKey.PublicKey(), "")
	s.brandSigning = assertstest.NewSigningDB("my-brand", brandPrivKey)

	modelAs, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model-2",
		"architecture": "armhf",
		"gadget":       "gadget",
		"kernel":       "kernel",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	s.modelAs = modelAs.(*asserts.Model)
}

func (s *runnerSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)

	s.seedAssertsDir = filepath.Join(dirs.SnapSeedDir, "assertions")

	// dummy seed yaml
	err := os.MkdirAll(dirs.SnapSeedDir, 0755)
	c.Assert(err, IsNil)
	seedYamlFn := filepath.Join(dirs.SnapSeedDir, "seed.yaml")
	err = ioutil.WriteFile(seedYamlFn, nil, 0644)
	c.Assert(err, IsNil)
	t0, err := time.Parse(time.RFC3339, "2017-08-11T15:49:49Z")
	c.Assert(err, IsNil)
	err = os.Chtimes(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), t0, t0)
	c.Assert(err, IsNil)
	s.t0 = t0
}

func (s *runnerSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

var (
	testKey = `type: account-key
authority-id: canonical
account-id: canonical
name: repair
public-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj
since: 2015-11-16T15:04:00Z
body-length: 149
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AcZrBFaFwYABAvCX5A8dTcdLdhdiuy2YRHO5CAfM5InQefkKOhNMUq2yfi3Sk6trUHxskhZkPnm4
NKx2yRr332q7AJXQHLX+DrZ29ycyoQ2NQGO3eAfQ0hjAAQFYBF8SSh5SutPu5XCVABEBAAE=

AXNpZw==
`

	testRepair = `type: repair
authority-id: canonical
brand-id: canonical
repair-id: 2
architectures:
  - amd64
  - arm64
series:
  - 16
models:
  - xyz/frobinator
timestamp: 2017-03-30T12:22:16Z
body-length: 7
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

script


AXNpZw==
`
	testHeadersResp = `{"headers":
{"architectures":["amd64","arm64"],"authority-id":"canonical","body-length":"7","brand-id":"canonical","models":["xyz/frobinator"],"repair-id":"2","series":["16"],"sign-key-sha3-384":"KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj","timestamp":"2017-03-30T12:22:16Z","type":"repair"}}`
)

func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func (s *runnerSuite) mockNow(c *C, runner *repair.Runner) (restore func()) {
	epoch := time.Unix(0, 0)
	r := repair.MockTimeNow(func() time.Time {
		return epoch
	})
	c.Check(runner.TLSTime().Equal(epoch), Equals, true)
	return r
}

func (s *runnerSuite) TestFetchJustRepair(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Equals, "/repairs/canonical/2")
		io.WriteString(w, testRepair)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	r := s.mockNow(c, runner)
	defer r()
	t0 := time.Now().UTC().Truncate(time.Minute)

	repair, aux, err := runner.Fetch("canonical", "2", -1)
	c.Assert(err, IsNil)
	c.Check(repair, NotNil)
	c.Check(aux, HasLen, 0)
	c.Check(repair.BrandID(), Equals, "canonical")
	c.Check(repair.RepairID(), Equals, "2")
	c.Check(repair.Body(), DeepEquals, []byte("script\n"))

	c.Check(runner.TLSTime().Before(t0), Equals, false)
}

func (s *runnerSuite) TestFetchScriptTooBig(c *C) {
	restore := repair.MockMaxRepairScriptSize(4)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Equals, "/repairs/canonical/2")
		io.WriteString(w, testRepair)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, _, err := runner.Fetch("canonical", "2", -1)
	c.Assert(err, ErrorMatches, `assertion body length 7 exceeds maximum body size 4 for "repair".*`)
	c.Assert(n, Equals, 1)
}

var (
	testRetryStrategy = retry.LimitCount(5, retry.LimitTime(1*time.Second,
		retry.Exponential{
			Initial: 1 * time.Millisecond,
			Factor:  1,
		},
	))
)

func (s *runnerSuite) TestFetch500(c *C) {
	restore := repair.MockFetchRetryStrategy(testRetryStrategy)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(500)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, _, err := runner.Fetch("canonical", "2", -1)
	c.Assert(err, ErrorMatches, "cannot fetch repair, unexpected status 500")
	c.Assert(n, Equals, 5)
}

func (s *runnerSuite) TestFetchEmpty(c *C) {
	restore := repair.MockFetchRetryStrategy(testRetryStrategy)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(200)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, _, err := runner.Fetch("canonical", "2", -1)
	c.Assert(err, Equals, io.ErrUnexpectedEOF)
	c.Assert(n, Equals, 5)
}

func (s *runnerSuite) TestFetchBroken(c *C) {
	restore := repair.MockFetchRetryStrategy(testRetryStrategy)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(200)
		io.WriteString(w, "xyz:")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, _, err := runner.Fetch("canonical", "2", -1)
	c.Assert(err, Equals, io.ErrUnexpectedEOF)
	c.Assert(n, Equals, 5)
}

func (s *runnerSuite) TestFetchNotFound(c *C) {
	restore := repair.MockFetchRetryStrategy(testRetryStrategy)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(404)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	r := s.mockNow(c, runner)
	defer r()
	t0 := time.Now().UTC().Truncate(time.Minute)

	_, _, err := runner.Fetch("canonical", "2", -1)
	c.Assert(err, Equals, repair.ErrRepairNotFound)
	c.Assert(n, Equals, 1)

	c.Check(runner.TLSTime().Before(t0), Equals, false)
}

func (s *runnerSuite) TestFetchIfNoneMatchNotModified(c *C) {
	restore := repair.MockFetchRetryStrategy(testRetryStrategy)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		c.Check(r.Header.Get("If-None-Match"), Equals, `"0"`)
		w.WriteHeader(304)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	r := s.mockNow(c, runner)
	defer r()
	t0 := time.Now().UTC().Truncate(time.Minute)

	_, _, err := runner.Fetch("canonical", "2", 0)
	c.Assert(err, Equals, repair.ErrRepairNotModified)
	c.Assert(n, Equals, 1)

	c.Check(runner.TLSTime().Before(t0), Equals, false)
}

func (s *runnerSuite) TestFetchIdMismatch(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		io.WriteString(w, testRepair)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, _, err := runner.Fetch("canonical", "4", -1)
	c.Assert(err, ErrorMatches, `cannot fetch repair, repair id mismatch canonical/2 != canonical/4`)
}

func (s *runnerSuite) TestFetchWrongFirstType(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Equals, "/repairs/canonical/2")
		io.WriteString(w, testKey)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, _, err := runner.Fetch("canonical", "2", -1)
	c.Assert(err, ErrorMatches, `cannot fetch repair, unexpected first assertion "account-key"`)
}

func (s *runnerSuite) TestFetchRepairPlusKey(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Equals, "/repairs/canonical/2")
		io.WriteString(w, testRepair)
		io.WriteString(w, "\n")
		io.WriteString(w, testKey)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	repair, aux, err := runner.Fetch("canonical", "2", -1)
	c.Assert(err, IsNil)
	c.Check(repair, NotNil)
	c.Check(aux, HasLen, 1)
	_, ok := aux[0].(*asserts.AccountKey)
	c.Check(ok, Equals, true)
}

func (s *runnerSuite) TestPeek(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/json")
		c.Check(r.URL.Path, Equals, "/repairs/canonical/2")
		io.WriteString(w, testHeadersResp)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	r := s.mockNow(c, runner)
	defer r()
	t0 := time.Now().UTC().Truncate(time.Minute)

	h, err := runner.Peek("canonical", "2")
	c.Assert(err, IsNil)
	c.Check(h["series"], DeepEquals, []interface{}{"16"})
	c.Check(h["architectures"], DeepEquals, []interface{}{"amd64", "arm64"})
	c.Check(h["models"], DeepEquals, []interface{}{"xyz/frobinator"})

	c.Check(runner.TLSTime().Before(t0), Equals, false)
}

func (s *runnerSuite) TestPeek500(c *C) {
	restore := repair.MockPeekRetryStrategy(testRetryStrategy)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(500)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, err := runner.Peek("canonical", "2")
	c.Assert(err, ErrorMatches, "cannot peek repair headers, unexpected status 500")
	c.Assert(n, Equals, 5)
}

func (s *runnerSuite) TestPeekInvalid(c *C) {
	restore := repair.MockPeekRetryStrategy(testRetryStrategy)
	defer restore()

	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(200)
		io.WriteString(w, "{")
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, err := runner.Peek("canonical", "2")
	c.Assert(err, Equals, io.ErrUnexpectedEOF)
	c.Assert(n, Equals, 5)
}

func (s *runnerSuite) TestPeekNotFound(c *C) {
	n := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(404)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	r := s.mockNow(c, runner)
	defer r()
	t0 := time.Now().UTC().Truncate(time.Minute)

	_, err := runner.Peek("canonical", "2")
	c.Assert(err, Equals, repair.ErrRepairNotFound)
	c.Assert(n, Equals, 1)

	c.Check(runner.TLSTime().Before(t0), Equals, false)
}

func (s *runnerSuite) TestPeekIdMismatch(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Header.Get("Accept"), Equals, "application/json")
		io.WriteString(w, testHeadersResp)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, err := runner.Peek("canonical", "4")
	c.Assert(err, ErrorMatches, `cannot peek repair headers, repair id mismatch canonical/2 != canonical/4`)
}

func (s *runnerSuite) freshState(c *C) {
	err := os.MkdirAll(dirs.SnapRepairDir, 0775)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(dirs.SnapRepairStateFile, []byte(`{"device": {"brand":"my-brand","model":"my-model"},"time-lower-bound":"2017-08-11T15:49:49Z"}`), 0600)
	c.Assert(err, IsNil)
}

func (s *runnerSuite) TestLoadState(c *C) {
	s.freshState(c)

	runner := repair.NewRunner()
	err := runner.LoadState()
	c.Assert(err, IsNil)
	brand, model := runner.BrandModel()
	c.Check(brand, Equals, "my-brand")
	c.Check(model, Equals, "my-model")
}

func (s *runnerSuite) initSeed(c *C) {
	err := os.MkdirAll(s.seedAssertsDir, 0775)
	c.Assert(err, IsNil)
}

func (s *runnerSuite) writeSeedAssert(c *C, fname string, a asserts.Assertion) {
	err := ioutil.WriteFile(filepath.Join(s.seedAssertsDir, fname), asserts.Encode(a), 0644)
	c.Assert(err, IsNil)
}

func (s *runnerSuite) rmSeedAssert(c *C, fname string) {
	err := os.Remove(filepath.Join(s.seedAssertsDir, fname))
	c.Assert(err, IsNil)
}

func (s *runnerSuite) TestLoadStateInitState(c *C) {
	// sanity
	c.Check(osutil.IsDirectory(dirs.SnapRepairDir), Equals, false)
	c.Check(osutil.FileExists(dirs.SnapRepairStateFile), Equals, false)
	// setup realistic seed/assertions
	r := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r()
	s.initSeed(c)
	s.writeSeedAssert(c, "store.account-key", s.storeSigning.StoreAccountKey(""))
	s.writeSeedAssert(c, "brand.account", s.brandAcct)
	s.writeSeedAssert(c, "brand.account-key", s.brandAcctKey)
	s.writeSeedAssert(c, "model", s.modelAs)

	runner := repair.NewRunner()
	err := runner.LoadState()
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(dirs.SnapRepairStateFile), Equals, true)

	brand, model := runner.BrandModel()
	c.Check(brand, Equals, "my-brand")
	c.Check(model, Equals, "my-model-2")

	c.Check(runner.TimeLowerBound().Equal(s.t0), Equals, true)
}

func (s *runnerSuite) TestLoadStateInitDeviceInfoFail(c *C) {
	// sanity
	c.Check(osutil.IsDirectory(dirs.SnapRepairDir), Equals, false)
	c.Check(osutil.FileExists(dirs.SnapRepairStateFile), Equals, false)
	// setup realistic seed/assertions
	r := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r()
	s.initSeed(c)

	const errPrefix = "cannot set device information: "
	tests := []struct {
		breakFunc   func()
		expectedErr string
	}{
		{func() { s.rmSeedAssert(c, "model") }, errPrefix + "no model assertion in seed data"},
		{func() { s.rmSeedAssert(c, "brand.account") }, errPrefix + "no brand account assertion in seed data"},
		{func() { s.rmSeedAssert(c, "brand.account-key") }, errPrefix + `cannot find public key.*`},
		{func() {
			// broken signature
			blob := asserts.Encode(s.brandAcct)
			err := ioutil.WriteFile(filepath.Join(s.seedAssertsDir, "brand.account"), blob[:len(blob)-3], 0644)
			c.Assert(err, IsNil)
		}, errPrefix + "cannot decode signature:.*"},
		{func() { s.writeSeedAssert(c, "model2", s.modelAs) }, errPrefix + "multiple models in seed assertions"},
	}

	for _, test := range tests {
		s.writeSeedAssert(c, "store.account-key", s.storeSigning.StoreAccountKey(""))
		s.writeSeedAssert(c, "brand.account", s.brandAcct)
		s.writeSeedAssert(c, "brand.account-key", s.brandAcctKey)
		s.writeSeedAssert(c, "model", s.modelAs)

		test.breakFunc()

		runner := repair.NewRunner()
		err := runner.LoadState()
		c.Check(err, ErrorMatches, test.expectedErr)
	}
}

func (s *runnerSuite) TestTLSTime(c *C) {
	s.freshState(c)
	runner := repair.NewRunner()
	err := runner.LoadState()
	c.Assert(err, IsNil)
	epoch := time.Unix(0, 0)
	r := repair.MockTimeNow(func() time.Time {
		return epoch
	})
	defer r()
	c.Check(runner.TLSTime().Equal(s.t0), Equals, true)
}

func (s *runnerSuite) TestLoadStateInitStateFail(c *C) {
	par := filepath.Dir(dirs.SnapSeedDir)
	err := os.Chmod(par, 0555)
	c.Assert(err, IsNil)
	defer os.Chmod(par, 0775)

	runner := repair.NewRunner()
	err = runner.LoadState()
	c.Check(err, ErrorMatches, `cannot create repair state directory:.*`)
}

func (s *runnerSuite) TestSaveStateFail(c *C) {
	s.freshState(c)

	runner := repair.NewRunner()
	err := runner.LoadState()
	c.Assert(err, IsNil)

	err = os.Chmod(dirs.SnapRepairDir, 0555)
	c.Assert(err, IsNil)
	defer os.Chmod(dirs.SnapRepairDir, 0775)

	// no error because this is a no-op
	err = runner.SaveState()
	c.Check(err, IsNil)

	// mark as modified
	runner.SetStateModified(true)

	err = runner.SaveState()
	c.Check(err, ErrorMatches, `cannot save repair state:.*`)
}

func (s *runnerSuite) TestSaveState(c *C) {
	s.freshState(c)

	runner := repair.NewRunner()
	err := runner.LoadState()
	c.Assert(err, IsNil)

	runner.SetSequence("canonical", []*repair.RepairState{
		{Sequence: 1, Revision: 3},
	})
	// mark as modified
	runner.SetStateModified(true)

	err = runner.SaveState()
	c.Assert(err, IsNil)

	data, err := ioutil.ReadFile(dirs.SnapRepairStateFile)
	c.Assert(err, IsNil)
	c.Check(string(data), Equals, `{"device":{"brand":"my-brand","model":"my-model"},"sequences":{"canonical":[{"sequence":1,"revision":3,"status":0}]},"time-lower-bound":"2017-08-11T15:49:49Z"}`)
}

var (
	nextRepairs = []string{`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 1
timestamp: 2017-07-01T12:00:00Z
body-length: 8
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptA


AXNpZw==`,
		`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 2
series:
  - 33
timestamp: 2017-07-02T12:00:00Z
body-length: 8
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptB


AXNpZw==`,
		`type: repair
revision: 2
authority-id: canonical
brand-id: canonical
repair-id: 3
series:
  - 16
timestamp: 2017-07-03T12:00:00Z
body-length: 8
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptC


AXNpZw==
`}

	repair3Rev4 = `type: repair
revision: 4
authority-id: canonical
brand-id: canonical
repair-id: 3
series:
  - 16
timestamp: 2017-07-03T12:00:00Z
body-length: 9
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptC2


AXNpZw==
`

	repair4 = `type: repair
authority-id: canonical
brand-id: canonical
repair-id: 4
timestamp: 2017-07-03T12:00:00Z
body-length: 8
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptD


AXNpZw==
`
)

func makeMockServer(c *C, seqRepairs *[]string, redirectFirst bool) *httptest.Server {
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		urlPath := r.URL.Path
		if redirectFirst && r.Header.Get("Accept") == asserts.MediaType {
			if !strings.HasPrefix(urlPath, "/final/") {
				// redirect
				finalURL := mockServer.URL + "/final" + r.URL.Path
				w.Header().Set("Location", finalURL)
				w.WriteHeader(302)
				return
			}
			urlPath = strings.TrimPrefix(urlPath, "/final")
		}

		c.Check(strings.HasPrefix(urlPath, "/repairs/canonical/"), Equals, true)

		seq, err := strconv.Atoi(strings.TrimPrefix(urlPath, "/repairs/canonical/"))
		c.Assert(err, IsNil)

		if seq > len(*seqRepairs) {
			w.WriteHeader(404)
			return
		}

		repair, err := asserts.Decode([]byte((*seqRepairs)[seq-1]))
		c.Assert(err, IsNil)

		switch r.Header.Get("Accept") {
		case "application/json":
			b, err := json.Marshal(map[string]interface{}{
				"headers": repair.Headers(),
			})
			c.Assert(err, IsNil)
			w.Write(b)
		case asserts.MediaType:
			etag := fmt.Sprintf(`"%d"`, repair.Revision())
			if strings.Contains(r.Header.Get("If-None-Match"), etag) {
				w.WriteHeader(304)
				return
			}
			w.Write(asserts.Encode(repair))
		}
	}))

	c.Assert(mockServer, NotNil)

	return mockServer
}

func (s *runnerSuite) loadSequences(c *C) map[string][]*repair.RepairState {
	data, err := ioutil.ReadFile(dirs.SnapRepairStateFile)
	c.Assert(err, IsNil)
	var x struct {
		Sequences map[string][]*repair.RepairState `json:"sequences"`
	}
	err = json.Unmarshal(data, &x)
	c.Assert(err, IsNil)
	return x.Sequences
}

func (s *runnerSuite) testNext(c *C, redirectFirst bool) {
	seqRepairs := append([]string(nil), nextRepairs...)

	mockServer := makeMockServer(c, &seqRepairs, redirectFirst)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	rpr, err := runner.Next("canonical")
	c.Assert(err, IsNil)
	c.Check(rpr.RepairID(), Equals, "1")
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapRepairAssertsDir, "canonical", "1", "repair.r0")), Equals, true)

	rpr, err = runner.Next("canonical")
	c.Assert(err, IsNil)
	c.Check(rpr.RepairID(), Equals, "3")
	strm, err := ioutil.ReadFile(filepath.Join(dirs.SnapRepairAssertsDir, "canonical", "3", "repair.r2"))
	c.Assert(err, IsNil)
	c.Check(string(strm), Equals, seqRepairs[2])

	// no more
	rpr, err = runner.Next("canonical")
	c.Check(err, Equals, repair.ErrRepairNotFound)

	expectedSeq := []*repair.RepairState{
		{Sequence: 1},
		{Sequence: 2, Status: repair.SkipStatus},
		{Sequence: 3, Revision: 2},
	}
	c.Check(runner.Sequence("canonical"), DeepEquals, expectedSeq)
	// on disk
	seqs := s.loadSequences(c)
	c.Check(seqs["canonical"], DeepEquals, expectedSeq)

	// start fresh run with new runner
	// will refetch repair 3
	seqRepairs[2] = repair3Rev4
	seqRepairs = append(seqRepairs, repair4)

	runner = repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	rpr, err = runner.Next("canonical")
	c.Assert(err, IsNil)
	c.Check(rpr.RepairID(), Equals, "1")

	rpr, err = runner.Next("canonical")
	c.Assert(err, IsNil)
	c.Check(rpr.RepairID(), Equals, "3")
	// refetched new revision!
	c.Check(rpr.Revision(), Equals, 4)
	c.Check(rpr.Body(), DeepEquals, []byte("scriptC2\n"))

	// new repair
	rpr, err = runner.Next("canonical")
	c.Assert(err, IsNil)
	c.Check(rpr.RepairID(), Equals, "4")
	c.Check(rpr.Body(), DeepEquals, []byte("scriptD\n"))

	// no more
	rpr, err = runner.Next("canonical")
	c.Check(err, Equals, repair.ErrRepairNotFound)

	c.Check(runner.Sequence("canonical"), DeepEquals, []*repair.RepairState{
		{Sequence: 1},
		{Sequence: 2, Status: repair.SkipStatus},
		{Sequence: 3, Revision: 4},
		{Sequence: 4},
	})
}

func (s *runnerSuite) TestNext(c *C) {
	redirectFirst := false
	s.testNext(c, redirectFirst)
}

func (s *runnerSuite) TestNextRedirect(c *C) {
	redirectFirst := true
	s.testNext(c, redirectFirst)
}

func (s *runnerSuite) TestNextImmediateSkip(c *C) {
	seqRepairs := []string{`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 1
series:
  - 33
timestamp: 2017-07-02T12:00:00Z
body-length: 8
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptB


AXNpZw==`}

	mockServer := makeMockServer(c, &seqRepairs, false)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	// not applicable => not returned
	_, err := runner.Next("canonical")
	c.Check(err, Equals, repair.ErrRepairNotFound)

	expectedSeq := []*repair.RepairState{
		{Sequence: 1, Status: repair.SkipStatus},
	}
	c.Check(runner.Sequence("canonical"), DeepEquals, expectedSeq)
	// on disk
	seqs := s.loadSequences(c)
	c.Check(seqs["canonical"], DeepEquals, expectedSeq)
}

func (s *runnerSuite) TestNextRefetchSkip(c *C) {
	seqRepairs := []string{`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 1
series:
  - 16
timestamp: 2017-07-02T12:00:00Z
body-length: 8
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptB


AXNpZw==`}

	mockServer := makeMockServer(c, &seqRepairs, false)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	_, err := runner.Next("canonical")
	c.Assert(err, IsNil)

	expectedSeq := []*repair.RepairState{
		{Sequence: 1},
	}
	c.Check(runner.Sequence("canonical"), DeepEquals, expectedSeq)
	// on disk
	seqs := s.loadSequences(c)
	c.Check(seqs["canonical"], DeepEquals, expectedSeq)

	// new fresh run, repair becomes now unapplicable
	seqRepairs[0] = `type: repair
authority-id: canonical
revision: 1
brand-id: canonical
repair-id: 1
series:
  - 33
timestamp: 2017-07-02T12:00:00Z
body-length: 7
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptX

AXNpZw==`

	runner = repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	_, err = runner.Next("canonical")
	c.Check(err, Equals, repair.ErrRepairNotFound)

	expectedSeq = []*repair.RepairState{
		{Sequence: 1, Revision: 1, Status: repair.SkipStatus},
	}
	c.Check(runner.Sequence("canonical"), DeepEquals, expectedSeq)
	// on disk
	seqs = s.loadSequences(c)
	c.Check(seqs["canonical"], DeepEquals, expectedSeq)
}

func (s *runnerSuite) TestNext500(c *C) {
	restore := repair.MockPeekRetryStrategy(testRetryStrategy)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	_, err := runner.Next("canonical")
	c.Assert(err, ErrorMatches, "cannot peek repair headers, unexpected status 500")
}

func (s *runnerSuite) TestNextSaveStateError(c *C) {
	seqRepairs := []string{`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 1
series:
  - 33
timestamp: 2017-07-02T12:00:00Z
body-length: 8
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptB


AXNpZw==`}

	mockServer := makeMockServer(c, &seqRepairs, false)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	// break SaveState
	err := os.Chmod(dirs.SnapRepairDir, 0555)
	c.Assert(err, IsNil)
	defer os.Chmod(dirs.SnapRepairDir, 0775)

	_, err = runner.Next("canonical")
	c.Check(err, ErrorMatches, `cannot save repair state:.*`)
}

func (s *runnerSuite) TestRepairSetStatus(c *C) {
	seqRepairs := []string{`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 1
series:
  - 16
timestamp: 2017-07-02T12:00:00Z
body-length: 8
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptB


AXNpZw==`}

	mockServer := makeMockServer(c, &seqRepairs, false)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	rpr, err := runner.Next("canonical")
	c.Assert(err, IsNil)

	rpr.SetStatus(repair.DoneStatus)

	expectedSeq := []*repair.RepairState{
		{Sequence: 1, Status: repair.DoneStatus},
	}
	c.Check(runner.Sequence("canonical"), DeepEquals, expectedSeq)
	// on disk
	seqs := s.loadSequences(c)
	c.Check(seqs["canonical"], DeepEquals, expectedSeq)
}

func (s *runnerSuite) TestRepairBasicRun(c *C) {
	seqRepairs := []string{`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 1
series:
  - 16
timestamp: 2017-07-02T12:00:00Z
body-length: 7
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

exit 0


AXNpZw==`}

	mockServer := makeMockServer(c, &seqRepairs, false)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	rpr, err := runner.Next("canonical")
	c.Assert(err, IsNil)

	rpr.Run()
	scrpt, err := ioutil.ReadFile(filepath.Join(dirs.SnapRepairRunDir, "canonical", "1", "script.r0"))
	c.Assert(err, IsNil)
	c.Check(string(scrpt), Equals, "exit 0\n")

}
