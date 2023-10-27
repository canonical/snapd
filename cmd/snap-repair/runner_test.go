// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2020 Canonical Ltd
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
	"github.com/snapcore/snapd/boot"
	repair "github.com/snapcore/snapd/cmd/snap-repair"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/testutil"
)

type baseRunnerSuite struct {
	testutil.BaseTest

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
	s.BaseTest.SetUpTest(c)

	_, restoreLogger := logger.MockLogger()
	s.AddCleanup(restoreLogger)

	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
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

func checkStateJSON(c *C, file string, exp map[string]interface{}) {
	stateFile := map[string]interface{}{}
	b, err := ioutil.ReadFile(file)
	c.Assert(err, IsNil)
	err = json.Unmarshal(b, &stateFile)
	c.Assert(err, IsNil)
	c.Check(stateFile, DeepEquals, exp)
}

func (s *baseRunnerSuite) freshState(c *C) {
	// assume base: core18
	s.freshStateWithBaseAndMode(c, "core18", "")
}

func (s *baseRunnerSuite) freshStateWithBaseAndMode(c *C, base, mode string) {
	err := os.MkdirAll(dirs.SnapRepairDir, 0775)
	c.Assert(err, IsNil)
	stateJSON := map[string]interface{}{
		"device": map[string]string{
			"brand": "my-brand",
			"model": "my-model",
			"base":  base,
			"mode":  mode,
		},
		"time-lower-bound": "2017-08-11T15:49:49Z",
	}
	b, err := json.Marshal(stateJSON)
	c.Assert(err, IsNil)

	err = os.WriteFile(dirs.SnapRepairStateFile, b, 0600)
	c.Assert(err, IsNil)
}

type runnerSuite struct {
	baseRunnerSuite

	restore func()
}

func (s *runnerSuite) SetUpSuite(c *C) {
	s.baseRunnerSuite.SetUpSuite(c)
	s.restore = snapdenv.SetUserAgentFromVersion("1", nil, "snap-repair")
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

func (s *runnerSuite) TestLoadStateInitStateFail(c *C) {
	err := os.MkdirAll(dirs.SnapSeedDir, 0755)
	c.Assert(err, IsNil)

	restore := makeReadOnly(c, filepath.Dir(dirs.SnapSeedDir))
	defer restore()

	runner := repair.NewRunner()
	err = runner.LoadState()
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

	exp := map[string]interface{}{
		"device": map[string]interface{}{
			"brand": "my-brand",
			"model": "my-model",
			"base":  "core18",
			"mode":  "",
		},
		"sequences": map[string]interface{}{
			"canonical": []interface{}{
				map[string]interface{}{
					// all json numbers are floats
					"sequence": 1.0,
					"revision": 3.0,
					"status":   0.0,
				},
			},
		},
		"time-lower-bound": "2017-08-11T15:49:49Z",
	}

	checkStateJSON(c, dirs.SnapRepairStateFile, exp)
}

type dev struct {
	base string
	mode string
}

func (s *runnerSuite) TestApplicable(c *C) {

	scenarios := []struct {
		device     *dev
		headers    map[string]interface{}
		applicable bool
	}{
		{nil, nil, true},
		{nil, map[string]interface{}{"series": []interface{}{"18"}}, false},
		{nil, map[string]interface{}{"series": []interface{}{"18", "16"}}, true},
		{nil, map[string]interface{}{"series": "18"}, false},
		{nil, map[string]interface{}{"series": []interface{}{18}}, false},
		{nil, map[string]interface{}{"architectures": []interface{}{arch.DpkgArchitecture()}}, true},
		{nil, map[string]interface{}{"architectures": []interface{}{"other-arch"}}, false},
		{nil, map[string]interface{}{"architectures": []interface{}{"other-arch", arch.DpkgArchitecture()}}, true},
		{nil, map[string]interface{}{"architectures": arch.DpkgArchitecture()}, false},
		{nil, map[string]interface{}{"models": []interface{}{"my-brand/my-model"}}, true},
		{nil, map[string]interface{}{"models": []interface{}{"other-brand/other-model"}}, false},
		{nil, map[string]interface{}{"models": []interface{}{"other-brand/other-model", "my-brand/my-model"}}, true},
		{nil, map[string]interface{}{"models": "my-brand/my-model"}, false},
		// modes for uc16 / uc18 devices
		{nil, map[string]interface{}{"modes": []interface{}{}}, true},
		{nil, map[string]interface{}{"modes": []interface{}{"run"}}, false},
		{nil, map[string]interface{}{"modes": []interface{}{"recover"}}, false},
		{nil, map[string]interface{}{"modes": []interface{}{"run", "recover"}}, false},
		// run mode for uc20 devices
		{&dev{mode: "run"}, map[string]interface{}{"modes": []interface{}{}}, true},
		{&dev{mode: "run"}, map[string]interface{}{"modes": []interface{}{"run"}}, true},
		{&dev{mode: "run"}, map[string]interface{}{"modes": []interface{}{"recover"}}, false},
		{&dev{mode: "run"}, map[string]interface{}{"modes": []interface{}{"run", "recover"}}, true},
		// recover mode for uc20 devices
		{&dev{mode: "recover"}, map[string]interface{}{"modes": []interface{}{}}, false},
		{&dev{mode: "recover"}, map[string]interface{}{"modes": []interface{}{"run"}}, false},
		{&dev{mode: "recover"}, map[string]interface{}{"modes": []interface{}{"recover"}}, true},
		{&dev{mode: "recover"}, map[string]interface{}{"modes": []interface{}{"run", "recover"}}, true},
		// bases for uc16 devices
		{&dev{base: "core"}, map[string]interface{}{"bases": []interface{}{"core"}}, true},
		{&dev{base: "core"}, map[string]interface{}{"bases": []interface{}{"core18"}}, false},
		{&dev{base: "core"}, map[string]interface{}{"bases": []interface{}{"core", "core18"}}, true},
		// bases for uc18 devices
		{&dev{base: "core18"}, map[string]interface{}{"bases": []interface{}{"core18"}}, true},
		{&dev{base: "core18"}, map[string]interface{}{"bases": []interface{}{"core"}}, false},
		{&dev{base: "core18"}, map[string]interface{}{"bases": []interface{}{"core", "core18"}}, true},
		// bases for uc20 devices
		{&dev{base: "core20"}, map[string]interface{}{"bases": []interface{}{"core20"}}, true},
		{&dev{base: "core20"}, map[string]interface{}{"bases": []interface{}{"core"}}, false},
		{&dev{base: "core20"}, map[string]interface{}{"bases": []interface{}{"core", "core20"}}, true},
		// model prefix matches
		{nil, map[string]interface{}{"models": []interface{}{"my-brand/*"}}, true},
		{nil, map[string]interface{}{"models": []interface{}{"my-brand/my-mod*"}}, true},
		{nil, map[string]interface{}{"models": []interface{}{"my-brand/xxx*"}}, false},
		{nil, map[string]interface{}{"models": []interface{}{"my-brand/my-mod*", "my-brand/xxx*"}}, true},
		{nil, map[string]interface{}{"models": []interface{}{"my*"}}, false},
		{nil, map[string]interface{}{"disabled": "true"}, false},
		{nil, map[string]interface{}{"disabled": "false"}, true},
	}

	for _, scen := range scenarios {
		if scen.device == nil {
			s.freshState(c)
		} else {
			s.freshStateWithBaseAndMode(c, scen.device.base, scen.device.mode)
		}

		runner := repair.NewRunner()
		err := runner.LoadState()
		c.Assert(err, IsNil)

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
		c.Check(ua, testutil.Contains, "snap-repair")

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

	_, err := runner.Next("canonical")
	c.Assert(err, Equals, repair.ErrRepairNotFound)

	// we saved new time lower bound
	stateFileExp := map[string]interface{}{
		"device": map[string]interface{}{
			"brand": "my-brand",
			"model": "my-model",
			"base":  "core18",
			"mode":  "",
		},
		"time-lower-bound": runner.TimeLowerBound().Format(time.RFC3339),
	}

	checkStateJSON(c, dirs.SnapRepairStateFile, stateFileExp)
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
body-length: 17
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

#!/bin/sh
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

	err = rpr.Run()
	c.Assert(err, IsNil)
	c.Check(filepath.Join(dirs.SnapRepairRunDir, "canonical", "1", "r0.script"), testutil.FileEquals, "#!/bin/sh\nexit 0\n")
}

func (s *runnerSuite) TestRepairBasicRun20RecoverEnv(c *C) {
	seqRepairs := []string{`type: repair
authority-id: canonical
brand-id: canonical
repair-id: 1
summary: repair one
series:
  - 16
bases:
  - core20
modes:
  - recover
  - run
timestamp: 2017-07-02T12:00:00Z
body-length: 81
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj

#!/bin/sh
env | grep SNAP_SYSTEM_MODE
echo "done" >&$SNAP_REPAIR_STATUS_FD
exit 0

AXNpZw==`}

	seqRepairs = s.signSeqRepairs(c, seqRepairs)

	r1 := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r1()
	r2 := repair.MockTrustedRepairRootKeys([]*asserts.AccountKey{s.repairRootAcctKey})
	defer r2()

	for _, mode := range []string{"recover", "run"} {
		s.freshStateWithBaseAndMode(c, "core20", mode)

		mockServer := makeMockServer(c, &seqRepairs, false)
		defer mockServer.Close()

		runner := repair.NewRunner()
		runner.BaseURL = mustParseURL(mockServer.URL)
		runner.LoadState()

		rpr, err := runner.Next("canonical")
		c.Assert(err, IsNil)

		err = rpr.Run()
		c.Assert(err, IsNil)
		c.Check(filepath.Join(dirs.SnapRepairRunDir, "canonical", "1", "r0.script"), testutil.FileEquals, `#!/bin/sh
env | grep SNAP_SYSTEM_MODE
echo "done" >&$SNAP_REPAIR_STATUS_FD
exit 0`)

		c.Check(filepath.Join(dirs.SnapRepairRunDir, "canonical", "1", "r0.done"), testutil.FileEquals, fmt.Sprintf(`repair: canonical-1
revision: 0
summary: repair one
output:
SNAP_SYSTEM_MODE=%s
`, mode))
		// ensure correct permissions
		fi, err := os.Stat(filepath.Join(dirs.SnapRepairRunDir, "canonical", "1", "r0.done"))
		c.Assert(err, IsNil)
		c.Check(fi.Mode(), Equals, os.FileMode(0600))
	}
}

func (s *runnerSuite) TestRepairModesAndBases(c *C) {
	repairTempl := `type: repair
authority-id: canonical
brand-id: canonical
repair-id: 1
summary: uc20 recovery repair 
timestamp: 2017-07-03T12:00:00Z
body-length: 17
sign-key-sha3-384: KPIl7M4vQ9d4AUjkoU41TGAwtOMLc_bWUCeW8AvdRWD4_xcP60Oo4ABsFNo6BtXj
%[1]s
#!/bin/sh
exit 0


AXNpZw==
	`

	r1 := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r1()
	r2 := repair.MockTrustedRepairRootKeys([]*asserts.AccountKey{s.repairRootAcctKey})
	defer r2()

	tt := []struct {
		device    *dev
		modes     []string
		bases     []string
		shouldRun bool
		comment   string
	}{
		// uc20 recover mode assertion
		{
			&dev{"core20", "recover"},
			[]string{"recover"},
			[]string{"core20"},
			true,
			"uc20 recover mode w/ uc20 recover mode assertion",
		},
		{
			&dev{"core20", "run"},
			[]string{"recover"},
			[]string{"core20"},
			false,
			"uc20 run mode w/ uc20 recover mode assertion",
		},
		{
			&dev{base: "core"},
			[]string{"recover"},
			[]string{"core20"},
			false,
			"uc16 w/ uc20 recover mode assertion",
		},
		{
			&dev{base: "core18"},
			[]string{"recover"},
			[]string{"core20"},
			false,
			"uc18 w/ uc20 recover mode assertion",
		},

		// uc20 run mode assertion
		{
			&dev{"core20", "recover"},
			[]string{"run"},
			[]string{"core20"},
			false,
			"uc20 recover mode w/ uc20 run mode assertion",
		},
		{
			&dev{"core20", "run"},
			[]string{"run"},
			[]string{"core20"},
			true,
			"uc20 run mode w/ uc20 run mode assertion",
		},
		{
			&dev{base: "core"},
			[]string{"run"},
			[]string{"core20"},
			false,
			"uc16 w/ uc20 run mode assertion",
		},
		{
			&dev{base: "core18"},
			[]string{"run"},
			[]string{"core20"},
			false,
			"uc18 w/ uc20 run mode assertion",
		},

		// all uc20 modes assertion
		{
			&dev{"core20", "recover"},
			[]string{"run", "recover"},
			[]string{"core20"},
			true,
			"uc20 recover mode w/ all uc20 modes assertion",
		},
		{
			&dev{"core20", "run"},
			[]string{"run", "recover"},
			[]string{"core20"},
			true,
			"uc20 run mode w/ all uc20 modes assertion",
		},
		{
			&dev{base: "core"},
			[]string{"run", "recover"},
			[]string{"core20"},
			false,
			"uc16 w/ all uc20 modes assertion",
		},
		{
			&dev{base: "core18"},
			[]string{"run", "recover"},
			[]string{"core20"},
			false,
			"uc18 w/ all uc20 modes assertion",
		},

		// alternate uc20 run mode only assertion
		{
			&dev{"core20", "recover"},
			[]string{},
			[]string{"core20"},
			false,
			"uc20 recover mode w/ alternate uc20 run mode assertion",
		},
		{
			&dev{"core20", "run"},
			[]string{},
			[]string{"core20"},
			true,
			"uc20 run mode w/ alternate uc20 run mode assertion",
		},
		{
			&dev{base: "core"},
			[]string{},
			[]string{"core20"},
			false,
			"uc16 w/ alternate uc20 run mode assertion",
		},
		{
			&dev{base: "core18"},
			[]string{},
			[]string{"core20"},
			false,
			"uc18 w/ alternate uc20 run mode assertion",
		},
		{
			&dev{base: "core"},
			[]string{"run"},
			[]string{},
			false,
			"uc16 w/ uc20 run mode assertion",
		},
		{
			&dev{base: "core18"},
			[]string{"run"},
			[]string{},
			false,
			"uc16 w/ uc20 run mode assertion",
		},

		// all except uc20 recover mode assertion
		{
			&dev{"core20", "recover"},
			[]string{},
			[]string{},
			false,
			"uc20 recover mode w/ all except uc20 recover mode assertion",
		},
		{
			&dev{"core20", "run"},
			[]string{},
			[]string{},
			true,
			"uc20 run mode w/ all except uc20 recover mode assertion",
		},
		{
			&dev{base: "core"},
			[]string{},
			[]string{},
			true,
			"uc16 w/ all except uc20 recover mode assertion",
		},
		{
			&dev{base: "core18"},
			[]string{},
			[]string{},
			true,
			"uc18 w/ all except uc20 recover mode assertion",
		},

		// uc16 and uc18 assertion
		{
			&dev{"core20", "recover"},
			[]string{},
			[]string{"core", "core18"},
			false,
			"uc20 recover mode w/ uc16 and uc18 assertion",
		},
		{
			&dev{"core20", "run"},
			[]string{},
			[]string{"core", "core18"},
			false,
			"uc20 run mode w/ uc16 and uc18 assertion",
		},
		{
			&dev{base: "core"},
			[]string{},
			[]string{"core", "core18"},
			true,
			"uc16 w/ uc16 and uc18 assertion",
		},
		{
			&dev{base: "core18"},
			[]string{},
			[]string{"core", "core18"},
			true,
			"uc18 w/ uc16 and uc18 assertion",
		},

		// just uc16 assertion
		{
			&dev{"core20", "recover"},
			[]string{},
			[]string{"core"},
			false,
			"uc20 recover mode w/ just uc16 assertion",
		},
		{
			&dev{"core20", "run"},
			[]string{},
			[]string{"core"},
			false,
			"uc20 run mode w/ just uc16 assertion",
		},
		{
			&dev{base: "core"},
			[]string{},
			[]string{"core"},
			true,
			"uc16 w/ just uc16 assertion",
		},
		{
			&dev{base: "core18"},
			[]string{},
			[]string{"core"},
			false,
			"uc18 w/ just uc16 assertion",
		},

		// just uc18 assertion
		{
			&dev{"core20", "recover"},
			[]string{},
			[]string{"core18"},
			false,
			"uc20 recover mode w/ just uc18 assertion",
		},
		{
			&dev{"core20", "run"},
			[]string{},
			[]string{"core18"},
			false,
			"uc20 run mode w/ just uc18 assertion",
		},
		{
			&dev{base: "core"},
			[]string{},
			[]string{"core18"},
			false,
			"uc16 w/ just uc18 assertion",
		},
		{
			&dev{base: "core18"},
			[]string{},
			[]string{"core18"},
			true,
			"uc18 w/ just uc18 assertion",
		},
	}
	for _, t := range tt {
		comment := Commentf(t.comment)
		cleanups := []func(){}

		// generate the assertion with the bases and modes
		basesStr := ""
		if len(t.bases) != 0 {
			basesStr = "bases:\n"
			for _, base := range t.bases {
				basesStr += "  - " + base + "\n"
			}
		}
		modesStr := ""
		if len(t.modes) != 0 {
			modesStr = "modes:\n"
			for _, mode := range t.modes {
				modesStr += "  - " + mode + "\n"
			}
		}

		seqRepairs := s.signSeqRepairs(c, []string{fmt.Sprintf(repairTempl, basesStr+modesStr)})

		mockServer := makeMockServer(c, &seqRepairs, false)
		cleanups = append(cleanups, func() { mockServer.Close() })

		if t.device == nil {
			s.freshState(c)
		} else {
			s.freshStateWithBaseAndMode(c, t.device.base, t.device.mode)
		}

		runner := repair.NewRunner()
		runner.BaseURL = mustParseURL(mockServer.URL)
		runner.LoadState()

		script := filepath.Join(dirs.SnapRepairRunDir, "canonical", "1", "r0.script")

		rpr, err := runner.Next("canonical")
		if t.shouldRun {
			c.Assert(err, IsNil, comment)

			// run the repair and make sure the script is there
			err = rpr.Run()
			c.Assert(err, IsNil, comment)
			c.Check(script, testutil.FileEquals, "#!/bin/sh\nexit 0\n", comment)

			// remove the script for the next iteration
			cleanups = append(cleanups, func() { c.Assert(os.RemoveAll(dirs.SnapRepairRunDir), IsNil) })
		} else {
			c.Assert(err, Equals, repair.ErrRepairNotFound, comment)
			c.Check(script, testutil.FileAbsent, comment)
		}

		for _, r := range cleanups {
			r()
		}
	}
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
	c.Check(dirs.SnapRepairStateFile, testutil.FileContains, fmt.Sprintf(`{"device":{"brand":"","model":"","base":"","mode":""},"sequences":{"canonical":[{"sequence":1,"revision":0,"status":%d}`, status))
}

// tests related to correct execution of script
type runScriptSuite struct {
	baseRunnerSuite

	seqRepairs []string

	mockServer *httptest.Server
	runner     *repair.Runner

	runDir string
}

var _ = Suite(&runScriptSuite{})

func (s *runScriptSuite) SetUpTest(c *C) {
	s.baseRunnerSuite.SetUpTest(c)
	s.runDir = filepath.Join(dirs.SnapRepairRunDir, "canonical", "1")

	s.AddCleanup(snapdenv.SetUserAgentFromVersion("1", nil, "snap-repair"))
}

// setupRunner must be called from the tests so that the *C passed into contains
// the tests' state and not the SetUpTest's state (otherwise, assertion failures
// in the mock server go unreported).
func (s *runScriptSuite) setupRunner(c *C) {
	s.mockServer = makeMockServer(c, &s.seqRepairs, false)
	s.AddCleanup(func() { s.mockServer.Close() })

	s.runner = repair.NewRunner()
	s.runner.BaseURL = mustParseURL(s.mockServer.URL)
	s.runner.LoadState()
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

func (s *runScriptSuite) verifyOutput(c *C, name, expectedOutput string) {
	c.Check(filepath.Join(s.runDir, name), testutil.FileEquals, expectedOutput)
	// ensure correct permissions
	fi, err := os.Stat(filepath.Join(s.runDir, name))
	c.Assert(err, IsNil)
	c.Check(fi.Mode(), Equals, os.FileMode(0600))
}

func (s *runScriptSuite) TestRepairBasicRunHappy(c *C) {
	s.setupRunner(c)
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
	s.setupRunner(c)
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
}

func (s *runScriptSuite) TestRepairBasicSkip(c *C) {
	s.setupRunner(c)
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
	s.setupRunner(c)
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
	s.setupRunner(c)
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
	s.setupRunner(c)
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

	c.Check(filepath.Join(s.runDir, "r0.retry"), testutil.FileMatches, `(?ms).*^PATH=.*:.*/run/snapd/repair/tools.*`)
	c.Check(filepath.Join(s.runDir, "r0.retry"), testutil.FileContains, `/repair -> /usr/lib/snapd/snap-repair`)

	// run again and ensure no error happens
	err = rpr.Run()
	c.Assert(err, IsNil)

}

// shared1620RunnerSuite is embedded by runner16Suite and
// runner20Suite and the tests are run once with a simulated uc16 and
// once with a simulated uc20 environment
type shared1620RunnerSuite struct {
	baseRunnerSuite

	writeSeedAssert func(c *C, fname string, a asserts.Assertion)

	// this is so we can check device details that will be different in the
	// 20 version of tests from the 16 version of tests
	expBase string
	expMode string
}

func (s *shared1620RunnerSuite) TestTLSTime(c *C) {
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

func (s *shared1620RunnerSuite) TestLoadStateInitState(c *C) {
	// validity
	c.Check(osutil.IsDirectory(dirs.SnapRepairDir), Equals, false)
	c.Check(osutil.FileExists(dirs.SnapRepairStateFile), Equals, false)
	// setup realistic seed/assertions
	r := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r()
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

	base, mode := runner.BaseMode()
	c.Check(base, Equals, s.expBase)
	c.Check(mode, Equals, s.expMode)

	c.Check(runner.TimeLowerBound().Equal(s.seedTime), Equals, true)
}

type runner16Suite struct {
	shared1620RunnerSuite
}

var _ = Suite(&runner16Suite{})

func (s *runner16Suite) SetUpTest(c *C) {
	s.shared1620RunnerSuite.SetUpTest(c)

	s.shared1620RunnerSuite.expBase = "core"
	s.shared1620RunnerSuite.expMode = ""

	s.seedAssertsDir = filepath.Join(dirs.SnapSeedDir, "assertions")

	// sample seed yaml
	err := os.MkdirAll(s.seedAssertsDir, 0755)
	c.Assert(err, IsNil)
	seedYamlFn := filepath.Join(dirs.SnapSeedDir, "seed.yaml")
	err = os.WriteFile(seedYamlFn, nil, 0644)
	c.Assert(err, IsNil)
	seedTime, err := time.Parse(time.RFC3339, "2017-08-11T15:49:49Z")
	c.Assert(err, IsNil)
	err = os.Chtimes(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), seedTime, seedTime)
	c.Assert(err, IsNil)
	s.seedTime = seedTime

	s.t0 = time.Now().UTC().Truncate(time.Minute)

	s.writeSeedAssert = s.writeSeedAssert16
}

func (s *runner16Suite) writeSeedAssert16(c *C, fname string, a asserts.Assertion) {
	err := os.WriteFile(filepath.Join(s.seedAssertsDir, fname), asserts.Encode(a), 0644)
	c.Assert(err, IsNil)
}

func (s *runner16Suite) rmSeedAssert16(c *C, fname string) {
	err := os.Remove(filepath.Join(s.seedAssertsDir, fname))
	c.Assert(err, IsNil)
}

func (s *runner16Suite) TestLoadStateInitDeviceInfoFail(c *C) {
	// validity
	c.Check(osutil.IsDirectory(dirs.SnapRepairDir), Equals, false)
	c.Check(osutil.FileExists(dirs.SnapRepairStateFile), Equals, false)
	// setup realistic seed/assertions
	r := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer r()

	const errPrefix = "cannot set device information: "
	tests := []struct {
		breakFunc   func()
		expectedErr string
	}{
		{func() { s.rmSeedAssert16(c, "model") }, errPrefix + "no model assertion in seed data"},
		{func() { s.rmSeedAssert16(c, "brand.account") }, errPrefix + "no brand account assertion in seed data"},
		{func() { s.rmSeedAssert16(c, "brand.account-key") }, errPrefix + `cannot find public key.*`},
		{func() {
			// broken signature
			blob := asserts.Encode(s.brandAcct)
			err := os.WriteFile(filepath.Join(s.seedAssertsDir, "brand.account"), blob[:len(blob)-3], 0644)
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

type runner20Suite struct {
	shared1620RunnerSuite
}

var _ = Suite(&runner20Suite{})

var mockModeenv = []byte(`
mode=run
model=my-brand/my-model-2
base=core20_1.snap
`)

func (s *runner20Suite) SetUpTest(c *C) {
	s.shared1620RunnerSuite.SetUpTest(c)

	s.shared1620RunnerSuite.expBase = "core20"
	s.shared1620RunnerSuite.expMode = "run"

	s.seedAssertsDir = filepath.Join(dirs.SnapSeedDir, "/systems/20201212/assertions")
	err := os.MkdirAll(s.seedAssertsDir, 0755)
	c.Assert(err, IsNil)

	// write sample modeenv
	err = os.MkdirAll(filepath.Dir(dirs.SnapModeenvFile), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(dirs.SnapModeenvFile, mockModeenv, 0644)
	c.Assert(err, IsNil)
	// validate that modeenv is actually valid
	_, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)

	seedTime, err := time.Parse(time.RFC3339, "2017-08-11T15:49:49Z")
	c.Assert(err, IsNil)
	err = os.Chtimes(dirs.SnapModeenvFile, seedTime, seedTime)
	c.Assert(err, IsNil)
	s.seedTime = seedTime
	s.t0 = time.Now().UTC().Truncate(time.Minute)

	s.writeSeedAssert = s.writeSeedAssert20
}

func (s *runner20Suite) writeSeedAssert20(c *C, fname string, a asserts.Assertion) {
	var fn string
	if _, ok := a.(*asserts.Model); ok {
		fn = filepath.Join(s.seedAssertsDir, "../model")
	} else {
		fn = filepath.Join(s.seedAssertsDir, fname)
	}
	err := os.WriteFile(fn, asserts.Encode(a), 0644)
	c.Assert(err, IsNil)

	// ensure model assertion file has the correct seed time
	if _, ok := a.(*asserts.Model); ok {
		err = os.Chtimes(fn, s.seedTime, s.seedTime)
		c.Assert(err, IsNil)
	}
}

func (s *runner20Suite) TestLoadStateInitDeviceInfoModeenvInvalidContent(c *C) {
	runner := repair.NewRunner()

	for _, tc := range []struct {
		modelStr    string
		expectedErr string
	}{
		{
			`invalid-key-value`,
			"cannot set device information: No option model in section ",
		}, {
			`model=`,
			`cannot set device information: cannot find brand/model in modeenv model string ""`,
		}, {
			`model=brand-but-no-model`,
			`cannot set device information: cannot find brand/model in modeenv model string "brand-but-no-model"`,
		},
	} {
		err := os.WriteFile(dirs.SnapModeenvFile, []byte(tc.modelStr), 0644)
		c.Assert(err, IsNil)
		err = runner.LoadState()
		c.Check(err, ErrorMatches, tc.expectedErr)
	}
}

func (s *runner20Suite) TestLoadStateInitDeviceInfoModeenvIncorrectPermissions(c *C) {
	runner := repair.NewRunner()

	err := os.Chmod(dirs.SnapModeenvFile, 0300)
	c.Assert(err, IsNil)
	s.AddCleanup(func() {
		err := os.Chmod(dirs.SnapModeenvFile, 0644)
		c.Assert(err, IsNil)
	})
	err = runner.LoadState()
	c.Check(err, ErrorMatches, "cannot set device information: open /.*/modeenv: permission denied")
}

func (s *runnerSuite) TestStoreOffline(c *C) {
	runner := repair.NewRunner()

	data, err := json.Marshal(repair.RepairConfig{
		StoreOffline: true,
	})
	c.Assert(err, IsNil)

	err = os.MkdirAll(filepath.Dir(dirs.SnapRepairConfigFile), 0755)
	c.Assert(err, IsNil)

	err = osutil.AtomicWriteFile(dirs.SnapRepairConfigFile, data, 0644, 0)
	c.Assert(err, IsNil)

	_, _, err = runner.Fetch("canonical", 2, -1)
	c.Assert(err, testutil.ErrorIs, repair.ErrStoreOffline)

	_, err = runner.Peek("brand", 0)
	c.Assert(err, testutil.ErrorIs, repair.ErrStoreOffline)
}

func (s *runnerSuite) TestStoreOnlineIfFileBroken(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, Equals, "/repairs/canonical/2")
		accept := r.Header.Get("Accept")
		switch accept {
		case "application/x.ubuntu.assertion":
			io.WriteString(w, testRepair)
			io.WriteString(w, "\n")
			io.WriteString(w, testKey)
		case "application/json":
			io.WriteString(w, testHeadersResp)
		default:
			c.Errorf("unexpected 'Accept' header: %s", accept)
		}
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	err := os.MkdirAll(filepath.Dir(dirs.SnapRepairConfigFile), 0755)
	c.Assert(err, IsNil)

	runner := repair.NewRunner()
	runner.BaseURL = mustParseURL(mockServer.URL)

	// file is missing
	_, _, err = runner.Fetch("canonical", 2, -1)
	c.Assert(err, IsNil)

	_, err = runner.Peek("canonical", 2)
	c.Assert(err, IsNil)

	// file is invalid json
	err = osutil.AtomicWriteFile(dirs.SnapRepairConfigFile, []byte("}{"), 0644, 0)
	c.Assert(err, IsNil)

	_, _, err = runner.Fetch("canonical", 2, -1)
	c.Assert(err, IsNil)

	_, err = runner.Peek("canonical", 2)
	c.Assert(err, IsNil)
}
