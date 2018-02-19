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
	"bytes"
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

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	repair "github.com/snapcore/snapd/cmd/snap-repair"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type baseRunnerSuite struct {
	tmpdir string

	seedTime time.Time
	t0       time.Time

	storeSigning *assertstest.StoreStack

	brandSigning *assertstest.SigningDB
	brandAcct    *asserts.Account
	brandAcctKey *asserts.AccountKey

	modelAs *asserts.Model

	seedAssertsDir string

	repairRootAcctKey *asserts.AccountKey
	repairsAcctKey    *asserts.AccountKey

	repairsSigning *assertstest.SigningDB

	restoreLogger func()
}

func (s *baseRunnerSuite) SetUpSuite(c *C) {
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

	repairRootKey, _ := assertstest.GenerateKey(1024)

	s.repairRootAcctKey = assertstest.NewAccountKey(s.storeSigning.RootSigning, s.storeSigning.TrustedAccount, nil, repairRootKey.PublicKey(), "")

	repairsKey, _ := assertstest.GenerateKey(752)

	repairRootSigning := assertstest.NewSigningDB("canonical", repairRootKey)

	s.repairsAcctKey = assertstest.NewAccountKey(repairRootSigning, s.storeSigning.TrustedAccount, nil, repairsKey.PublicKey(), "")

	s.repairsSigning = assertstest.NewSigningDB("canonical", repairsKey)
}

func (s *baseRunnerSuite) SetUpTest(c *C) {
	_, s.restoreLogger = logger.MockLogger()

	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)

	s.seedAssertsDir = filepath.Join(dirs.SnapSeedDir, "assertions")

	// dummy seed yaml
	err := os.MkdirAll(dirs.SnapSeedDir, 0755)
	c.Assert(err, IsNil)
	seedYamlFn := filepath.Join(dirs.SnapSeedDir, "seed.yaml")
	err = ioutil.WriteFile(seedYamlFn, nil, 0644)
	c.Assert(err, IsNil)
	seedTime, err := time.Parse(time.RFC3339, "2017-08-11T15:49:49Z")
	c.Assert(err, IsNil)
	err = os.Chtimes(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), seedTime, seedTime)
	c.Assert(err, IsNil)
	s.seedTime = seedTime

	s.t0 = time.Now().UTC().Truncate(time.Minute)
}

func (s *baseRunnerSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
	s.restoreLogger()
}

func (s *baseRunnerSuite) signSeqRepairs(c *C, repairs []string) []string {
	var seq []string
	for _, rpr := range repairs {
		decoded, err := asserts.Decode([]byte(rpr))
		c.Assert(err, IsNil)
		signed, err := s.repairsSigning.Sign(asserts.RepairType, decoded.Headers(), decoded.Body(), "")
		c.Assert(err, IsNil)
		buf := &bytes.Buffer{}
		enc := asserts.NewEncoder(buf)
		enc.Encode(signed)
		enc.Encode(s.repairsAcctKey)
		seq = append(seq, buf.String())
	}
	return seq
}

const freshStateJSON = `{"device":{"brand":"my-brand","model":"my-model"},"time-lower-bound":"2017-08-11T15:49:49Z"}`

func (s *baseRunnerSuite) freshState(c *C) {
	err := os.MkdirAll(dirs.SnapRepairDir, 0775)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(dirs.SnapRepairStateFile, []byte(freshStateJSON), 0600)
	c.Assert(err, IsNil)
}

type runnerSuite struct {
	baseRunnerSuite

	restore func()
}

func (s *runnerSuite) SetUpSuite(c *C) {
	s.baseRunnerSuite.SetUpSuite(c)
	s.restore = httputil.SetUserAgentFromVersion("1", "snap-repair")
}

func (s *runnerSuite) TearDownSuite(c *C) {
	s.restore()
}

var _ = Suite(&runnerSuite{})

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
summary: repair two
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

func (s *runnerSuite) mockBrokenTimeNowSetToEpoch(c *C, runner *repair.Runner) (restore func()) {
	epoch := time.Unix(0, 0)
	r := repair.MockTimeNow(func() time.Time {
		return epoch
	})
	c.Check(runner.TLSTime().Equal(epoch), Equals, true)
	return r
}

func (s *runnerSuite) checkBrokenTimeNowMitigated(c *C, runner *repair.Runner) {
	c.Check(runner.TLSTime().Before(s.t0), Equals, false)
}

func (s *runnerSuite) TestFetchJustRepair(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		c.Check(strings.Contains(ua, "snap-repair"), Equals, true)
		c.Check(r.Header.Get("Accept"), Equals, "application/x.ubuntu.assertion")
		c.Check(r.URL.Path, Equals, "/repairs/canonical/2")
		io.WriteString(w, testRepair)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	r := s.mockBrokenTimeNowSetToEpoch(c, runner)
	defer r()

	repair, aux, err := runner.Fetch("canonical", 2, -1)
	c.Assert(err, IsNil)
	c.Check(repair, NotNil)
	c.Check(aux, HasLen, 0)
	c.Check(repair.BrandID(), Equals, "canonical")
	c.Check(repair.RepairID(), Equals, 2)
	c.Check(repair.Body(), DeepEquals, []byte("script\n"))

	s.checkBrokenTimeNowMitigated(c, runner)
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

	_, _, err := runner.Fetch("canonical", 2, -1)
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

	_, _, err := runner.Fetch("canonical", 2, -1)
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

	_, _, err := runner.Fetch("canonical", 2, -1)
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

	_, _, err := runner.Fetch("canonical", 2, -1)
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

	r := s.mockBrokenTimeNowSetToEpoch(c, runner)
	defer r()

	_, _, err := runner.Fetch("canonical", 2, -1)
	c.Assert(err, Equals, repair.ErrRepairNotFound)
	c.Assert(n, Equals, 1)

	s.checkBrokenTimeNowMitigated(c, runner)
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

	r := s.mockBrokenTimeNowSetToEpoch(c, runner)
	defer r()

	_, _, err := runner.Fetch("canonical", 2, 0)
	c.Assert(err, Equals, repair.ErrRepairNotModified)
	c.Assert(n, Equals, 1)

	s.checkBrokenTimeNowMitigated(c, runner)
}

func (s *runnerSuite) TestFetchIgnoreSupersededRevision(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, testRepair)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	_, _, err := runner.Fetch("canonical", 2, 2)
	c.Assert(err, Equals, repair.ErrRepairNotModified)
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

	_, _, err := runner.Fetch("canonical", 4, -1)
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

	_, _, err := runner.Fetch("canonical", 2, -1)
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

	repair, aux, err := runner.Fetch("canonical", 2, -1)
	c.Assert(err, IsNil)
	c.Check(repair, NotNil)
	c.Check(aux, HasLen, 1)
	_, ok := aux[0].(*asserts.AccountKey)
	c.Check(ok, Equals, true)
}

func (s *runnerSuite) TestPeek(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		c.Check(strings.Contains(ua, "snap-repair"), Equals, true)
		c.Check(r.Header.Get("Accept"), Equals, "application/json")
		c.Check(r.URL.Path, Equals, "/repairs/canonical/2")
		io.WriteString(w, testHeadersResp)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	r := s.mockBrokenTimeNowSetToEpoch(c, runner)
	defer r()

	h, err := runner.Peek("canonical", 2)
	c.Assert(err, IsNil)
	c.Check(h["series"], DeepEquals, []interface{}{"16"})
	c.Check(h["architectures"], DeepEquals, []interface{}{"amd64", "arm64"})
	c.Check(h["models"], DeepEquals, []interface{}{"xyz/frobinator"})

	s.checkBrokenTimeNowMitigated(c, runner)
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

	_, err := runner.Peek("canonical", 2)
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

	_, err := runner.Peek("canonical", 2)
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

	r := s.mockBrokenTimeNowSetToEpoch(c, runner)
	defer r()

	_, err := runner.Peek("canonical", 2)
	c.Assert(err, Equals, repair.ErrRepairNotFound)
	c.Assert(n, Equals, 1)

	s.checkBrokenTimeNowMitigated(c, runner)
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

	_, err := runner.Peek("canonical", 4)
	c.Assert(err, ErrorMatches, `cannot peek repair headers, repair id mismatch canonical/2 != canonical/4`)
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

	c.Check(runner.TimeLowerBound().Equal(s.seedTime), Equals, true)
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
	c.Check(runner.TLSTime().Equal(s.seedTime), Equals, true)
}

func makeReadOnly(c *C, dir string) (restore func()) {
	// skip tests that need this because uid==0 does not honor
	// write permissions in directories (yay, unix)
	if os.Getuid() == 0 {
		// FIXME: we could use osutil.Chattr() here
		c.Skip("too lazy to make path readonly as root")
	}
	err := os.Chmod(dir, 0555)
	c.Assert(err, IsNil)
	return func() {
		err := os.Chmod(dir, 0755)
		c.Assert(err, IsNil)
	}
}

func (s *runnerSuite) TestLoadStateInitStateFail(c *C) {
	restore := makeReadOnly(c, filepath.Dir(dirs.SnapSeedDir))
	defer restore()

	runner := repair.NewRunner()
	err := runner.LoadState()
	c.Check(err, ErrorMatches, `cannot create repair state directory:.*`)
}

func (s *runnerSuite) TestSaveStateFail(c *C) {
	s.freshState(c)

	runner := repair.NewRunner()
	err := runner.LoadState()
	c.Assert(err, IsNil)

	restore := makeReadOnly(c, dirs.SnapRepairDir)
	defer restore()

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

	c.Check(dirs.SnapRepairStateFile, testutil.FileEquals, `{"device":{"brand":"my-brand","model":"my-model"},"sequences":{"canonical":[{"sequence":1,"revision":3,"status":0}]},"time-lower-bound":"2017-08-11T15:49:49Z"}`)
}

func (s *runnerSuite) TestApplicable(c *C) {
	s.freshState(c)
	runner := repair.NewRunner()
	err := runner.LoadState()
	c.Assert(err, IsNil)

	scenarios := []struct {
		headers    map[string]interface{}
		applicable bool
	}{
		{nil, true},
		{map[string]interface{}{"series": []interface{}{"18"}}, false},
		{map[string]interface{}{"series": []interface{}{"18", "16"}}, true},
		{map[string]interface{}{"series": "18"}, false},
		{map[string]interface{}{"series": []interface{}{18}}, false},
		{map[string]interface{}{"architectures": []interface{}{arch.UbuntuArchitecture()}}, true},
		{map[string]interface{}{"architectures": []interface{}{"other-arch"}}, false},
		{map[string]interface{}{"architectures": []interface{}{"other-arch", arch.UbuntuArchitecture()}}, true},
		{map[string]interface{}{"architectures": arch.UbuntuArchitecture()}, false},
		{map[string]interface{}{"models": []interface{}{"my-brand/my-model"}}, true},
		{map[string]interface{}{"models": []interface{}{"other-brand/other-model"}}, false},
		{map[string]interface{}{"models": []interface{}{"other-brand/other-model", "my-brand/my-model"}}, true},
		{map[string]interface{}{"models": "my-brand/my-model"}, false},
		// model prefix matches
		{map[string]interface{}{"models": []interface{}{"my-brand/*"}}, true},
		{map[string]interface{}{"models": []interface{}{"my-brand/my-mod*"}}, true},
		{map[string]interface{}{"models": []interface{}{"my-brand/xxx*"}}, false},
		{map[string]interface{}{"models": []interface{}{"my-brand/my-mod*", "my-brand/xxx*"}}, true},
		{map[string]interface{}{"models": []interface{}{"my*"}}, false},
		{map[string]interface{}{"disabled": "true"}, false},
		{map[string]interface{}{"disabled": "false"}, true},
	}

	for _, scen := range scenarios {
		ok := runner.Applicable(scen.headers)
		c.Check(ok, Equals, scen.applicable, Commentf("%v", scen))
	}
}

var (
	nextRepairs = []string{`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 1
summary: repair one
timestamp: 2017-07-01T12:00:00Z
body-length: 8
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptA


AXNpZw==`,
		`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 2
summary: repair two
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
summary: repair three rev2
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
summary: repair three rev4
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
summary: repair four
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
		ua := r.Header.Get("User-Agent")
		c.Check(strings.Contains(ua, "snap-repair"), Equals, true)

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

		rpr := []byte((*seqRepairs)[seq-1])
		dec := asserts.NewDecoder(bytes.NewBuffer(rpr))
		repair, err := dec.Decode()
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
			w.Write(rpr)
		}
	}))

	c.Assert(mockServer, NotNil)

	return mockServer
}

func (s *runnerSuite) TestTrustedRepairRootKeys(c *C) {
	acctKeys := repair.TrustedRepairRootKeys()
	c.Check(acctKeys, HasLen, 1)
	c.Check(acctKeys[0].AccountID(), Equals, "canonical")
	c.Check(acctKeys[0].PublicKeyID(), Equals, "nttW6NfBXI_E-00u38W-KH6eiksfQNXuI7IiumoV49_zkbhM0sYTzSnFlwZC-W4t")
}

func (s *runnerSuite) TestVerify(c *C) {
	r1 := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r1()
	r2 := repair.MockTrustedRepairRootKeys([]*asserts.AccountKey{s.repairRootAcctKey})
	defer r2()

	runner := repair.NewRunner()

	a, err := s.repairsSigning.Sign(asserts.RepairType, map[string]interface{}{
		"brand-id":  "canonical",
		"repair-id": "2",
		"summary":   "repair two",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, []byte("#script"), "")
	c.Assert(err, IsNil)
	rpr := a.(*asserts.Repair)

	err = runner.Verify(rpr, []asserts.Assertion{s.repairsAcctKey})
	c.Check(err, IsNil)
}

func (s *runnerSuite) signSeqRepairs(c *C, repairs []string) []string {
	var seq []string
	for _, rpr := range repairs {
		decoded, err := asserts.Decode([]byte(rpr))
		c.Assert(err, IsNil)
		signed, err := s.repairsSigning.Sign(asserts.RepairType, decoded.Headers(), decoded.Body(), "")
		c.Assert(err, IsNil)
		buf := &bytes.Buffer{}
		enc := asserts.NewEncoder(buf)
		enc.Encode(signed)
		enc.Encode(s.repairsAcctKey)
		seq = append(seq, buf.String())
	}
	return seq
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
	r1 := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r1()
	r2 := repair.MockTrustedRepairRootKeys([]*asserts.AccountKey{s.repairRootAcctKey})
	defer r2()

	seqRepairs := s.signSeqRepairs(c, nextRepairs)

	mockServer := makeMockServer(c, &seqRepairs, redirectFirst)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	rpr, err := runner.Next("canonical")
	c.Assert(err, IsNil)
	c.Check(rpr.RepairID(), Equals, 1)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapRepairAssertsDir, "canonical", "1", "r0.repair")), Equals, true)

	rpr, err = runner.Next("canonical")
	c.Assert(err, IsNil)
	c.Check(rpr.RepairID(), Equals, 3)
	c.Check(filepath.Join(dirs.SnapRepairAssertsDir, "canonical", "3", "r2.repair"), testutil.FileEquals, seqRepairs[2])

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
	signed := s.signSeqRepairs(c, []string{repair3Rev4, repair4})
	seqRepairs[2] = signed[0]
	seqRepairs = append(seqRepairs, signed[1])

	runner = repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	rpr, err = runner.Next("canonical")
	c.Assert(err, IsNil)
	c.Check(rpr.RepairID(), Equals, 1)

	rpr, err = runner.Next("canonical")
	c.Assert(err, IsNil)
	c.Check(rpr.RepairID(), Equals, 3)
	// refetched new revision!
	c.Check(rpr.Revision(), Equals, 4)
	c.Check(rpr.Body(), DeepEquals, []byte("scriptC2\n"))

	// new repair
	rpr, err = runner.Next("canonical")
	c.Assert(err, IsNil)
	c.Check(rpr.RepairID(), Equals, 4)
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
summary: repair one
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
summary: repair one
series:
  - 16
timestamp: 2017-07-02T12:00:00Z
body-length: 8
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptB


AXNpZw==`}

	r1 := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r1()
	r2 := repair.MockTrustedRepairRootKeys([]*asserts.AccountKey{s.repairRootAcctKey})
	defer r2()

	seqRepairs = s.signSeqRepairs(c, seqRepairs)

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
summary: repair one rev1
series:
  - 16
disabled: true
timestamp: 2017-07-02T12:00:00Z
body-length: 7
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptX

AXNpZw==`

	seqRepairs = s.signSeqRepairs(c, seqRepairs)

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

func (s *runnerSuite) TestNextNotFound(c *C) {
	s.freshState(c)

	restore := repair.MockPeekRetryStrategy(testRetryStrategy)
	defer restore()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	// sanity
	c.Check(dirs.SnapRepairStateFile, testutil.FileEquals, freshStateJSON)

	_, err := runner.Next("canonical")
	c.Assert(err, Equals, repair.ErrRepairNotFound)

	// we saved new time lower bound
	t1 := runner.TimeLowerBound()
	expected := strings.Replace(freshStateJSON, "2017-08-11T15:49:49Z", t1.Format(time.RFC3339), 1)
	c.Check(expected, Not(Equals), freshStateJSON)
	c.Check(dirs.SnapRepairStateFile, testutil.FileEquals, expected)
}

func (s *runnerSuite) TestNextSaveStateError(c *C) {
	seqRepairs := []string{`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 1
summary: repair one
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
	restore := makeReadOnly(c, dirs.SnapRepairDir)
	defer restore()

	_, err := runner.Next("canonical")
	c.Check(err, ErrorMatches, `cannot save repair state:.*`)
}

func (s *runnerSuite) TestNextVerifyNoKey(c *C) {
	seqRepairs := []string{`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 1
summary: repair one
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
	c.Check(err, ErrorMatches, `cannot verify repair canonical-1: cannot find public key.*`)

	c.Check(runner.Sequence("canonical"), HasLen, 0)
}

func (s *runnerSuite) TestNextVerifySelfSigned(c *C) {
	randoKey, _ := assertstest.GenerateKey(752)

	randomSigning := assertstest.NewSigningDB("canonical", randoKey)
	randoKeyEncoded, err := asserts.EncodePublicKey(randoKey.PublicKey())
	c.Assert(err, IsNil)
	acctKey, err := randomSigning.Sign(asserts.AccountKeyType, map[string]interface{}{
		"authority-id":        "canonical",
		"account-id":          "canonical",
		"public-key-sha3-384": randoKey.PublicKey().ID(),
		"name":                "repairs",
		"since":               time.Now().UTC().Format(time.RFC3339),
	}, randoKeyEncoded, "")
	c.Assert(err, IsNil)

	rpr, err := randomSigning.Sign(asserts.RepairType, map[string]interface{}{
		"brand-id":  "canonical",
		"repair-id": "1",
		"summary":   "repair one",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, []byte("scriptB\n"), "")
	c.Assert(err, IsNil)

	buf := &bytes.Buffer{}
	enc := asserts.NewEncoder(buf)
	enc.Encode(rpr)
	enc.Encode(acctKey)
	seqRepairs := []string{buf.String()}

	mockServer := makeMockServer(c, &seqRepairs, false)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	_, err = runner.Next("canonical")
	c.Check(err, ErrorMatches, `cannot verify repair canonical-1: circular assertions`)

	c.Check(runner.Sequence("canonical"), HasLen, 0)
}

func (s *runnerSuite) TestNextVerifyAllKeysOK(c *C) {
	r1 := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r1()
	r2 := repair.MockTrustedRepairRootKeys([]*asserts.AccountKey{s.repairRootAcctKey})
	defer r2()

	decoded, err := asserts.Decode([]byte(nextRepairs[0]))
	c.Assert(err, IsNil)
	signed, err := s.repairsSigning.Sign(asserts.RepairType, decoded.Headers(), decoded.Body(), "")
	c.Assert(err, IsNil)

	// stream with all keys (any order) works as well
	buf := &bytes.Buffer{}
	enc := asserts.NewEncoder(buf)
	enc.Encode(signed)
	enc.Encode(s.storeSigning.TrustedKey)
	enc.Encode(s.repairRootAcctKey)
	enc.Encode(s.repairsAcctKey)
	seqRepairs := []string{buf.String()}

	mockServer := makeMockServer(c, &seqRepairs, false)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	rpr, err := runner.Next("canonical")
	c.Assert(err, IsNil)
	c.Check(rpr.RepairID(), Equals, 1)
}

func (s *runnerSuite) TestRepairSetStatus(c *C) {
	seqRepairs := []string{`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 1
summary: repair one
timestamp: 2017-07-02T12:00:00Z
body-length: 8
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

scriptB


AXNpZw==`}

	r1 := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r1()
	r2 := repair.MockTrustedRepairRootKeys([]*asserts.AccountKey{s.repairRootAcctKey})
	defer r2()

	seqRepairs = s.signSeqRepairs(c, seqRepairs)

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
summary: repair one
series:
  - 16
timestamp: 2017-07-02T12:00:00Z
body-length: 7
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

exit 0


AXNpZw==`}

	r1 := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r1()
	r2 := repair.MockTrustedRepairRootKeys([]*asserts.AccountKey{s.repairRootAcctKey})
	defer r2()

	seqRepairs = s.signSeqRepairs(c, seqRepairs)

	mockServer := makeMockServer(c, &seqRepairs, false)
	defer mockServer.Close()

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)
	runner.LoadState()

	rpr, err := runner.Next("canonical")
	c.Assert(err, IsNil)

	rpr.Run()
	c.Check(filepath.Join(dirs.SnapRepairRunDir, "canonical", "1", "r0.script"), testutil.FileEquals, "exit 0\n")
}

func makeMockRepair(script string) string {
	return fmt.Sprintf(`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 1
summary: repair one
series:
  - 16
timestamp: 2017-07-02T12:00:00Z
body-length: %d
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

%s

AXNpZw==`, len(script), script)
}

func verifyRepairStatus(c *C, status repair.RepairStatus) {
	c.Check(dirs.SnapRepairStateFile, testutil.FileContains, fmt.Sprintf(`{"device":{"brand":"","model":""},"sequences":{"canonical":[{"sequence":1,"revision":0,"status":%d}`, status))
}

// tests related to correct execution of script
type runScriptSuite struct {
	baseRunnerSuite

	seqRepairs []string

	mockServer *httptest.Server
	runner     *repair.Runner

	runDir string

	restoreErrTrackerReportRepair func()
	errReport                     struct {
		repair string
		errMsg string
		dupSig string
		extra  map[string]string
	}
}

var _ = Suite(&runScriptSuite{})

func (s *runScriptSuite) SetUpTest(c *C) {
	s.baseRunnerSuite.SetUpTest(c)

	s.mockServer = makeMockServer(c, &s.seqRepairs, false)

	s.runner = repair.NewRunner()
	s.runner.BaseURL = mustParseURL(s.mockServer.URL)
	s.runner.LoadState()

	s.runDir = filepath.Join(dirs.SnapRepairRunDir, "canonical", "1")

	s.restoreErrTrackerReportRepair = repair.MockErrtrackerReportRepair(s.errtrackerReportRepair)
}

func (s *runScriptSuite) TearDownTest(c *C) {
	s.baseRunnerSuite.TearDownTest(c)

	s.restoreErrTrackerReportRepair()
	s.mockServer.Close()
}

func (s *runScriptSuite) errtrackerReportRepair(repair, errMsg, dupSig string, extra map[string]string) (string, error) {
	s.errReport.repair = repair
	s.errReport.errMsg = errMsg
	s.errReport.dupSig = dupSig
	s.errReport.extra = extra

	return "some-oops-id", nil
}

func (s *runScriptSuite) testScriptRun(c *C, mockScript string) *repair.Repair {
	r1 := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r1()
	r2 := repair.MockTrustedRepairRootKeys([]*asserts.AccountKey{s.repairRootAcctKey})
	defer r2()

	s.seqRepairs = s.signSeqRepairs(c, s.seqRepairs)

	rpr, err := s.runner.Next("canonical")
	c.Assert(err, IsNil)

	err = rpr.Run()
	c.Assert(err, IsNil)

	c.Check(filepath.Join(s.runDir, "r0.script"), testutil.FileEquals, mockScript)

	return rpr
}

func (s *runScriptSuite) verifyRundir(c *C, names []string) {
	dirents, err := ioutil.ReadDir(s.runDir)
	c.Assert(err, IsNil)
	c.Assert(dirents, HasLen, len(names))
	for i := range dirents {
		c.Check(dirents[i].Name(), Matches, names[i])
	}
}

type byMtime []os.FileInfo

func (m byMtime) Len() int           { return len(m) }
func (m byMtime) Less(i, j int) bool { return m[i].ModTime().Before(m[j].ModTime()) }
func (m byMtime) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }

func (s *runScriptSuite) verifyOutput(c *C, name, expectedOutput string) {
	c.Check(filepath.Join(s.runDir, name), testutil.FileEquals, expectedOutput)
	// ensure correct permissions
	fi, err := os.Stat(filepath.Join(s.runDir, name))
	c.Assert(err, IsNil)
	c.Check(fi.Mode(), Equals, os.FileMode(0600))
}

func (s *runScriptSuite) TestRepairBasicRunHappy(c *C) {
	script := `#!/bin/sh
echo "happy output"
echo "done" >&$SNAP_REPAIR_STATUS_FD
exit 0
`
	s.seqRepairs = []string{makeMockRepair(script)}
	s.testScriptRun(c, script)
	// verify
	s.verifyRundir(c, []string{
		`^r0.done$`,
		`^r0.script$`,
		`^work$`,
	})
	s.verifyOutput(c, "r0.done", `repair: canonical-1
revision: 0
summary: repair one
output:
happy output
`)
	verifyRepairStatus(c, repair.DoneStatus)
}

func (s *runScriptSuite) TestRepairBasicRunUnhappy(c *C) {
	script := `#!/bin/sh
echo "unhappy output"
exit 1
`
	s.seqRepairs = []string{makeMockRepair(script)}
	s.testScriptRun(c, script)
	// verify
	s.verifyRundir(c, []string{
		`^r0.retry$`,
		`^r0.script$`,
		`^work$`,
	})
	s.verifyOutput(c, "r0.retry", `repair: canonical-1
revision: 0
summary: repair one
output:
unhappy output

repair canonical-1 revision 0 failed: exit status 1`)
	verifyRepairStatus(c, repair.RetryStatus)

	c.Check(s.errReport.repair, Equals, "canonical/1")
	c.Check(s.errReport.errMsg, Equals, `repair canonical-1 revision 0 failed: exit status 1`)
	c.Check(s.errReport.dupSig, Equals, `canonical/1
repair canonical-1 revision 0 failed: exit status 1
output:
repair: canonical-1
revision: 0
summary: repair one
output:
unhappy output
`)
	c.Check(s.errReport.extra, DeepEquals, map[string]string{
		"Revision": "0",
		"RepairID": "1",
		"BrandID":  "canonical",
		"Status":   "retry",
	})
}

func (s *runScriptSuite) TestRepairBasicSkip(c *C) {
	script := `#!/bin/sh
echo "other output"
echo "skip" >&$SNAP_REPAIR_STATUS_FD
exit 0
`
	s.seqRepairs = []string{makeMockRepair(script)}
	s.testScriptRun(c, script)
	// verify
	s.verifyRundir(c, []string{
		`^r0.script$`,
		`^r0.skip$`,
		`^work$`,
	})
	s.verifyOutput(c, "r0.skip", `repair: canonical-1
revision: 0
summary: repair one
output:
other output
`)
	verifyRepairStatus(c, repair.SkipStatus)
}

func (s *runScriptSuite) TestRepairBasicRunUnhappyThenHappy(c *C) {
	script := `#!/bin/sh
if [ -f zzz-ran-once ]; then
    echo "happy now"
    echo "done" >&$SNAP_REPAIR_STATUS_FD
    exit 0
fi
echo "unhappy output"
touch zzz-ran-once
exit 1
`
	s.seqRepairs = []string{makeMockRepair(script)}
	rpr := s.testScriptRun(c, script)
	s.verifyRundir(c, []string{
		`^r0.retry$`,
		`^r0.script$`,
		`^work$`,
	})
	s.verifyOutput(c, "r0.retry", `repair: canonical-1
revision: 0
summary: repair one
output:
unhappy output

repair canonical-1 revision 0 failed: exit status 1`)
	verifyRepairStatus(c, repair.RetryStatus)

	// run again, it will be happy this time
	err := rpr.Run()
	c.Assert(err, IsNil)

	s.verifyRundir(c, []string{
		`^r0.done$`,
		`^r0.retry$`,
		`^r0.script$`,
		`^work$`,
	})
	s.verifyOutput(c, "r0.done", `repair: canonical-1
revision: 0
summary: repair one
output:
happy now
`)
	verifyRepairStatus(c, repair.DoneStatus)
}

func (s *runScriptSuite) TestRepairHitsTimeout(c *C) {
	r1 := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r1()
	r2 := repair.MockTrustedRepairRootKeys([]*asserts.AccountKey{s.repairRootAcctKey})
	defer r2()

	restore := repair.MockDefaultRepairTimeout(100 * time.Millisecond)
	defer restore()

	script := `#!/bin/sh
echo "output before timeout"
sleep 100
`
	s.seqRepairs = []string{makeMockRepair(script)}
	s.seqRepairs = s.signSeqRepairs(c, s.seqRepairs)

	rpr, err := s.runner.Next("canonical")
	c.Assert(err, IsNil)

	err = rpr.Run()
	c.Assert(err, IsNil)

	s.verifyRundir(c, []string{
		`^r0.retry$`,
		`^r0.script$`,
		`^work$`,
	})
	s.verifyOutput(c, "r0.retry", `repair: canonical-1
revision: 0
summary: repair one
output:
output before timeout

repair canonical-1 revision 0 failed: repair did not finish within 100ms`)
	verifyRepairStatus(c, repair.RetryStatus)
}

func (s *runScriptSuite) TestRepairHasCorrectPath(c *C) {
	r1 := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r1()
	r2 := repair.MockTrustedRepairRootKeys([]*asserts.AccountKey{s.repairRootAcctKey})
	defer r2()

	script := `#!/bin/sh
echo PATH=$PATH
ls -l ${PATH##*:}/repair
`
	s.seqRepairs = []string{makeMockRepair(script)}
	s.seqRepairs = s.signSeqRepairs(c, s.seqRepairs)

	rpr, err := s.runner.Next("canonical")
	c.Assert(err, IsNil)

	err = rpr.Run()
	c.Assert(err, IsNil)

	c.Check(filepath.Join(s.runDir, "r0.retry"), testutil.FileMatches, fmt.Sprintf(`(?ms).*^PATH=.*:.*/run/snapd/repair/tools.*`))
	c.Check(filepath.Join(s.runDir, "r0.retry"), testutil.FileContains, `/repair -> /usr/lib/snapd/snap-repair`)

	// run again and ensure no error happens
	err = rpr.Run()
	c.Assert(err, IsNil)

}
