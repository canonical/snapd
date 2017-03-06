// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package daemon

import (
	"bytes"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/sha3"
	"golang.org/x/net/context"
	"gopkg.in/check.v1"
	"gopkg.in/macaroon.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type apiBaseSuite struct {
	rsnaps            []*snap.Info
	err               error
	vars              map[string]string
	storeSearch       store.Search
	suggestedCurrency string
	d                 *Daemon
	user              *auth.UserState
	restoreBackends   func()
	refreshCandidates []*store.RefreshCandidate
	buyOptions        *store.BuyOptions
	buyResult         *store.BuyResult
	storeSigning      *assertstest.StoreStack
	restoreRelease    func()
	trustedRestorer   func()
}

func (s *apiBaseSuite) SnapInfo(spec store.SnapSpec, user *auth.UserState) (*snap.Info, error) {
	s.user = user
	if len(s.rsnaps) > 0 {
		return s.rsnaps[0], s.err
	}
	return nil, s.err
}

func (s *apiBaseSuite) Find(search *store.Search, user *auth.UserState) ([]*snap.Info, error) {
	s.storeSearch = *search
	s.user = user

	return s.rsnaps, s.err
}

func (s *apiBaseSuite) ListRefresh(snaps []*store.RefreshCandidate, user *auth.UserState) ([]*snap.Info, error) {
	s.refreshCandidates = snaps
	s.user = user

	return s.rsnaps, s.err
}

func (s *apiBaseSuite) SuggestedCurrency() string {
	return s.suggestedCurrency
}

func (s *apiBaseSuite) Download(context.Context, string, string, *snap.DownloadInfo, progress.Meter, *auth.UserState) error {
	panic("Download not expected to be called")
}

func (s *apiBaseSuite) Buy(options *store.BuyOptions, user *auth.UserState) (*store.BuyResult, error) {
	s.buyOptions = options
	s.user = user
	return s.buyResult, s.err
}

func (s *apiBaseSuite) ReadyToBuy(user *auth.UserState) error {
	s.user = user
	return s.err
}

func (s *apiBaseSuite) Assertion(*asserts.AssertionType, []string, *auth.UserState) (asserts.Assertion, error) {
	panic("Assertion not expected to be called")
}

func (s *apiBaseSuite) Sections(*auth.UserState) ([]string, error) {
	panic("Sections not expected to be called")
}

func (s *apiBaseSuite) muxVars(*http.Request) map[string]string {
	return s.vars
}

func (s *apiBaseSuite) SetUpSuite(c *check.C) {
	muxVars = s.muxVars
	s.restoreRelease = release.MockForcedDevmode(false)

	snapstate.CanAutoRefresh = func(*state.State) (bool, error) {
		return false, nil
	}
}

func (s *apiBaseSuite) TearDownSuite(c *check.C) {
	muxVars = nil
	s.restoreRelease()
}

var (
	rootPrivKey, _  = assertstest.GenerateKey(1024)
	storePrivKey, _ = assertstest.GenerateKey(752)
)

func (s *apiBaseSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, check.IsNil)
	c.Assert(os.MkdirAll(dirs.SnapMountDir, 0755), check.IsNil)

	s.rsnaps = nil
	s.suggestedCurrency = ""
	s.storeSearch = store.Search{}
	s.err = nil
	s.vars = nil
	s.user = nil
	s.d = nil
	s.refreshCandidates = nil
	// Disable real security backends for all API tests
	s.restoreBackends = ifacestate.MockSecurityBackends(nil)

	s.buyOptions = nil
	s.buyResult = nil

	s.storeSigning = assertstest.NewStoreStack("can0nical", rootPrivKey, storePrivKey)
	s.trustedRestorer = sysdb.InjectTrusted(s.storeSigning.Trusted)

	assertstateRefreshSnapDeclarations = nil
	snapstateCoreInfo = nil
	snapstateInstall = nil
	snapstateInstallMany = nil
	snapstateInstallPath = nil
	snapstateRefreshCandidates = nil
	snapstateRemoveMany = nil
	snapstateRevert = nil
	snapstateRevertToRevision = nil
	snapstateTryPath = nil
	snapstateUpdate = nil
	snapstateUpdateMany = nil
}

func (s *apiBaseSuite) TearDownTest(c *check.C) {
	s.trustedRestorer()
	s.d = nil
	s.restoreBackends()
	unsafeReadSnapInfo = unsafeReadSnapInfoImpl
	ensureStateSoon = ensureStateSoonImpl
	dirs.SetRootDir("")

	assertstateRefreshSnapDeclarations = assertstate.RefreshSnapDeclarations
	snapstateCoreInfo = snapstate.CoreInfo
	snapstateInstall = snapstate.Install
	snapstateInstallMany = snapstate.InstallMany
	snapstateInstallPath = snapstate.InstallPath
	snapstateRefreshCandidates = snapstate.RefreshCandidates
	snapstateRemoveMany = snapstate.RemoveMany
	snapstateRevert = snapstate.Revert
	snapstateRevertToRevision = snapstate.RevertToRevision
	snapstateTryPath = snapstate.TryPath
	snapstateUpdate = snapstate.Update
	snapstateUpdateMany = snapstate.UpdateMany
}

func (s *apiBaseSuite) daemon(c *check.C) *Daemon {
	if s.d != nil {
		panic("called daemon() twice")
	}
	d, err := New()
	c.Assert(err, check.IsNil)
	d.addRoutes()

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	snapstate.ReplaceStore(st, s)
	// mark as already seeded
	st.Set("seeded", true)

	s.d = d
	return d
}

func (s *apiBaseSuite) mkInstalled(c *check.C, name, developer, version string, revision snap.Revision, active bool, extraYaml string) *snap.Info {
	return s.mkInstalledInState(c, nil, name, developer, version, revision, active, extraYaml)
}

func (s *apiBaseSuite) mkInstalledInState(c *check.C, daemon *Daemon, name, developer, version string, revision snap.Revision, active bool, extraYaml string) *snap.Info {
	snapID := name + "-id"
	// Collect arguments into a snap.SideInfo structure
	sideInfo := &snap.SideInfo{
		SnapID:   snapID,
		RealName: name,
		Revision: revision,
		Channel:  "stable",
	}

	// Collect other arguments into a yaml string
	yamlText := fmt.Sprintf(`
name: %s
version: %s
%s`, name, version, extraYaml)
	contents := ""

	// Mock the snap on disk
	snapInfo := snaptest.MockSnap(c, yamlText, contents, sideInfo)

	c.Assert(os.MkdirAll(snapInfo.DataDir(), 0755), check.IsNil)
	metadir := filepath.Join(snapInfo.MountDir(), "meta")
	guidir := filepath.Join(metadir, "gui")
	c.Assert(os.MkdirAll(guidir, 0755), check.IsNil)
	c.Check(ioutil.WriteFile(filepath.Join(guidir, "icon.svg"), []byte("yadda icon"), 0644), check.IsNil)

	if daemon != nil {
		st := daemon.overlord.State()
		st.Lock()
		defer st.Unlock()

		err := assertstate.Add(st, s.storeSigning.StoreAccountKey(""))
		if _, ok := err.(*asserts.RevisionError); !ok {
			c.Assert(err, check.IsNil)
		}

		devAcct := assertstest.NewAccount(s.storeSigning, developer, map[string]interface{}{
			"account-id": developer + "-id",
		}, "")
		err = assertstate.Add(st, devAcct)
		if _, ok := err.(*asserts.RevisionError); !ok {
			c.Assert(err, check.IsNil)
		}

		snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
			"series":       "16",
			"snap-id":      snapID,
			"snap-name":    name,
			"publisher-id": devAcct.AccountID(),
			"timestamp":    time.Now().Format(time.RFC3339),
		}, nil, "")
		c.Assert(err, check.IsNil)
		err = assertstate.Add(st, snapDecl)
		if _, ok := err.(*asserts.RevisionError); !ok {
			c.Assert(err, check.IsNil)
		}

		h := sha3.Sum384([]byte(fmt.Sprintf("%s%s", name, revision)))
		dgst, err := asserts.EncodeDigest(crypto.SHA3_384, h[:])
		c.Assert(err, check.IsNil)
		snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
			"snap-sha3-384": string(dgst),
			"snap-size":     "999",
			"snap-id":       snapID,
			"snap-revision": fmt.Sprintf("%s", revision),
			"developer-id":  devAcct.AccountID(),
			"timestamp":     time.Now().Format(time.RFC3339),
		}, nil, "")
		c.Assert(err, check.IsNil)
		err = assertstate.Add(st, snapRev)
		c.Assert(err, check.IsNil)

		var snapst snapstate.SnapState
		snapstate.Get(st, name, &snapst)
		snapst.Active = active
		snapst.Sequence = append(snapst.Sequence, &snapInfo.SideInfo)
		snapst.Current = snapInfo.SideInfo.Revision
		snapst.Channel = "beta"

		snapstate.Set(st, name, &snapst)
	}

	return snapInfo
}

func (s *apiBaseSuite) mkGadget(c *check.C, store string) {
	yamlText := fmt.Sprintf(`name: test
version: 1
type: gadget
gadget: {store: {id: %q}}
`, store)
	contents := ""
	snaptest.MockSnap(c, yamlText, contents, &snap.SideInfo{Revision: snap.R(1)})
	c.Assert(os.Symlink("1", filepath.Join(dirs.SnapMountDir, "test", "current")), check.IsNil)
}

type apiSuite struct {
	apiBaseSuite
}

var _ = check.Suite(&apiSuite{})

func (s *apiSuite) TestSnapInfoOneIntegration(c *check.C) {
	d := s.daemon(c)
	s.vars = map[string]string{"name": "foo"}

	// we have v0 [r5] installed
	s.mkInstalledInState(c, d, "foo", "bar", "v0", snap.R(5), false, "")
	// and v1 [r10] is current
	s.mkInstalledInState(c, d, "foo", "bar", "v1", snap.R(10), true, "description: description\nsummary: summary")

	req, err := http.NewRequest("GET", "/v2/snaps/foo", nil)
	c.Assert(err, check.IsNil)
	rsp, ok := getSnapInfo(snapCmd, req, nil).(*resp)
	c.Assert(ok, check.Equals, true)

	c.Assert(rsp, check.NotNil)
	c.Assert(rsp.Result, check.FitsTypeOf, map[string]interface{}{})
	m := rsp.Result.(map[string]interface{})

	// installed-size depends on vagaries of the filesystem, just check type
	c.Check(m["installed-size"], check.FitsTypeOf, int64(0))
	delete(m, "installed-size")
	// ditto install-date
	c.Check(m["install-date"], check.FitsTypeOf, time.Time{})
	delete(m, "install-date")

	meta := &Meta{}
	expected := &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: map[string]interface{}{
			"id":               "foo-id",
			"name":             "foo",
			"revision":         snap.R(10),
			"version":          "v1",
			"channel":          "stable",
			"tracking-channel": "beta",
			"summary":          "summary",
			"description":      "description",
			"developer":        "bar",
			"status":           "active",
			"icon":             "/v2/icons/foo/icon",
			"type":             string(snap.TypeApp),
			"resource":         "/v2/snaps/foo",
			"private":          false,
			"devmode":          false,
			"jailmode":         false,
			"confinement":      snap.StrictConfinement,
			"trymode":          false,
			"apps":             []appJSON{},
			"broken":           "",
			"contact":          "",
		},
		Meta: meta,
	}

	c.Check(rsp.Result, check.DeepEquals, expected.Result)
}

func (s *apiSuite) TestSnapInfoWithAuth(c *check.C) {
	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/find/?q=name:gfoo", nil)
	c.Assert(err, check.IsNil)

	c.Assert(s.user, check.IsNil)

	_, ok := searchStore(findCmd, req, user).(*resp)
	c.Assert(ok, check.Equals, true)
	// ensure user was set
	c.Assert(s.user, check.DeepEquals, user)
}

func (s *apiSuite) TestSnapInfoNotFound(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	c.Check(getSnapInfo(snapCmd, req, nil).(*resp).Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestSnapInfoNoneFound(c *check.C) {
	s.vars = map[string]string{"name": "foo"}

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	c.Check(getSnapInfo(snapCmd, req, nil).(*resp).Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestSnapInfoIgnoresRemoteErrors(c *check.C) {
	s.vars = map[string]string{"name": "foo"}
	s.err = errors.New("weird")

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	rsp := getSnapInfo(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestListIncludesAll(c *check.C) {
	// Very basic check to help stop us from not adding all the
	// commands to the command list.
	//
	// It could get fancier, looking deeper into the AST to see
	// exactly what's being defined, but it's probably not worth
	// it; this gives us most of the benefits of that, with a
	// fraction of the work.
	//
	// NOTE: there's probably a
	// better/easier way of doing this (patches welcome)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "api.go", nil, 0)
	if err != nil {
		panic(err)
	}

	found := 0

	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.ValueSpec:
			found += len(v.Values)
			return false
		}
		return true
	})

	exceptions := []string{ // keep sorted, for scanning ease
		"isEmailish",
		"api",
		"maxReadBuflen",
		"muxVars",
		"errNothingToInstall",
		"defaultCoreSnapName",
		"oldDefaultSnapCoreName",
		"errDevJailModeConflict",
		"errNoJailMode",
		"errClassicDevmodeConflict",
		// snapInstruction vars:
		"snapInstructionDispTable",
		"snapstateInstall",
		"snapstateUpdate",
		"snapstateInstallPath",
		"snapstateTryPath",
		"snapstateCoreInfo",
		"snapstateUpdateMany",
		"snapstateInstallMany",
		"snapstateRemoveMany",
		"snapstateRefreshCandidates",
		"snapstateRevert",
		"snapstateRevertToRevision",
		"assertstateRefreshSnapDeclarations",
		"unsafeReadSnapInfo",
		"osutilAddUser",
		"setupLocalUser",
		"storeUserInfo",
		"postCreateUserUcrednetGetUID",
		"ensureStateSoon",
	}
	c.Check(found, check.Equals, len(api)+len(exceptions),
		check.Commentf(`At a glance it looks like you've not added all the Commands defined in api to the api list. If that is not the case, please add the exception to the "exceptions" list in this test.`))
}

func (s *apiSuite) TestRootCmd(c *check.C) {
	// check it only does GET
	c.Check(rootCmd.PUT, check.IsNil)
	c.Check(rootCmd.POST, check.IsNil)
	c.Check(rootCmd.DELETE, check.IsNil)
	c.Assert(rootCmd.GET, check.NotNil)

	rec := httptest.NewRecorder()
	c.Check(rootCmd.Path, check.Equals, "/")

	rootCmd.GET(rootCmd, nil, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/json")

	expected := []interface{}{"TBD"}
	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *apiSuite) TestSysInfo(c *check.C) {
	// check it only does GET
	c.Check(sysInfoCmd.PUT, check.IsNil)
	c.Check(sysInfoCmd.POST, check.IsNil)
	c.Check(sysInfoCmd.DELETE, check.IsNil)
	c.Assert(sysInfoCmd.GET, check.NotNil)

	rec := httptest.NewRecorder()
	c.Check(sysInfoCmd.Path, check.Equals, "/v2/system-info")

	s.daemon(c).Version = "42b1"

	restore := release.MockReleaseInfo(&release.OS{ID: "distro-id", VersionID: "1.2"})
	defer restore()
	restore = release.MockOnClassic(true)
	defer restore()
	sysInfoCmd.GET(sysInfoCmd, nil, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/json")

	expected := map[string]interface{}{
		"series":  "16",
		"version": "42b1",
		"os-release": map[string]interface{}{
			"id":         "distro-id",
			"version-id": "1.2",
		},
		"on-classic": true,
		"managed":    false,
	}
	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	// Ensure that we had a kernel-verrsion but don't check the actual value.
	const kernelVersionKey = "kernel-version"
	c.Check(rsp.Result.(map[string]interface{})[kernelVersionKey], check.Not(check.Equals), "")
	delete(rsp.Result.(map[string]interface{}), kernelVersionKey)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *apiSuite) makeMyAppsServer(statusCode int, data string) *httptest.Server {
	mockMyAppsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		io.WriteString(w, data)
	}))
	store.MyAppsMacaroonACLAPI = mockMyAppsServer.URL + "/acl/"
	return mockMyAppsServer
}

func (s *apiSuite) makeSSOServer(statusCode int, data string) *httptest.Server {
	mockSSOServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		io.WriteString(w, data)
	}))
	store.UbuntuoneDischargeAPI = mockSSOServer.URL + "/tokens/discharge"
	return mockSSOServer
}

func (s *apiSuite) makeStoreMacaroon() (string, error) {
	m, err := macaroon.New([]byte("secret"), "some id", "location")
	if err != nil {
		return "", err
	}
	err = m.AddFirstPartyCaveat("caveat")
	if err != nil {
		return "", err
	}
	err = m.AddThirdPartyCaveat([]byte("shared-secret"), "third-party-caveat", store.UbuntuoneLocation)
	if err != nil {
		return "", err
	}

	return auth.MacaroonSerialize(m)
}

func (s *apiSuite) makeStoreMacaroonResponse(serializedMacaroon string) (string, error) {
	data := map[string]string{
		"macaroon": serializedMacaroon,
	}
	expectedData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	return string(expectedData), nil
}

func (s *apiSuite) TestLoginUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

	serializedMacaroon, err := s.makeStoreMacaroon()
	c.Assert(err, check.IsNil)
	responseData, err := s.makeStoreMacaroonResponse(serializedMacaroon)
	c.Assert(err, check.IsNil)
	mockMyAppsServer := s.makeMyAppsServer(200, responseData)
	defer mockMyAppsServer.Close()

	discharge := `{"discharge_macaroon": "the-discharge-macaroon-serialized-data"}`
	mockSSOServer := s.makeSSOServer(200, discharge)
	defer mockSSOServer.Close()

	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(loginCmd, req, nil).(*resp)

	state.Lock()
	user, err := auth.User(state, 1)
	state.Unlock()
	c.Check(err, check.IsNil)

	expected := userResponseData{
		ID:    1,
		Email: "email@.com",

		Macaroon:   user.Macaroon,
		Discharges: user.Discharges,
	}

	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	c.Check(user.ID, check.Equals, 1)
	c.Check(user.Username, check.Equals, "")
	c.Check(user.Email, check.Equals, "email@.com")
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, serializedMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
	// snapd macaroon was setup too
	snapdMacaroon, err := auth.MacaroonDeserialize(user.Macaroon)
	c.Check(err, check.IsNil)
	c.Check(snapdMacaroon.Id(), check.Equals, "1")
	c.Check(snapdMacaroon.Location(), check.Equals, "snapd")
}

func (s *apiSuite) TestLoginUserWithUsername(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

	serializedMacaroon, err := s.makeStoreMacaroon()
	c.Assert(err, check.IsNil)
	responseData, err := s.makeStoreMacaroonResponse(serializedMacaroon)
	c.Assert(err, check.IsNil)
	mockMyAppsServer := s.makeMyAppsServer(200, responseData)
	defer mockMyAppsServer.Close()

	discharge := `{"discharge_macaroon": "the-discharge-macaroon-serialized-data"}`
	mockSSOServer := s.makeSSOServer(200, discharge)
	defer mockSSOServer.Close()

	buf := bytes.NewBufferString(`{"username": "username", "email": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(loginCmd, req, nil).(*resp)

	state.Lock()
	user, err := auth.User(state, 1)
	state.Unlock()
	c.Check(err, check.IsNil)

	expected := userResponseData{
		ID:         1,
		Username:   "username",
		Email:      "email@.com",
		Macaroon:   user.Macaroon,
		Discharges: user.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	c.Check(user.ID, check.Equals, 1)
	c.Check(user.Username, check.Equals, "username")
	c.Check(user.Email, check.Equals, "email@.com")
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, serializedMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
	// snapd macaroon was setup too
	snapdMacaroon, err := auth.MacaroonDeserialize(user.Macaroon)
	c.Check(err, check.IsNil)
	c.Check(snapdMacaroon.Id(), check.Equals, "1")
	c.Check(snapdMacaroon.Location(), check.Equals, "snapd")
}

func (s *apiSuite) TestLoginUserNoEmailWithExistentLocalUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

	// setup local-only user
	state.Lock()
	localUser, err := auth.NewUser(state, "username", "email@test.com", "", nil)
	state.Unlock()
	c.Assert(err, check.IsNil)

	serializedMacaroon, err := s.makeStoreMacaroon()
	c.Assert(err, check.IsNil)
	responseData, err := s.makeStoreMacaroonResponse(serializedMacaroon)
	c.Assert(err, check.IsNil)
	mockMyAppsServer := s.makeMyAppsServer(200, responseData)
	defer mockMyAppsServer.Close()

	discharge := `{"discharge_macaroon": "the-discharge-macaroon-serialized-data"}`
	mockSSOServer := s.makeSSOServer(200, discharge)
	defer mockSSOServer.Close()

	buf := bytes.NewBufferString(`{"username": "username", "email": "", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, localUser.Macaroon))

	rsp := loginUser(loginCmd, req, localUser).(*resp)

	expected := userResponseData{
		ID:       1,
		Username: "username",
		Email:    "email@test.com",

		Macaroon:   localUser.Macaroon,
		Discharges: localUser.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	state.Lock()
	user, err := auth.User(state, localUser.ID)
	state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(user.Username, check.Equals, "username")
	c.Check(user.Email, check.Equals, localUser.Email)
	c.Check(user.Macaroon, check.Equals, localUser.Macaroon)
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, serializedMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
}

func (s *apiSuite) TestLoginUserWithExistentLocalUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()

	// setup local-only user
	state.Lock()
	localUser, err := auth.NewUser(state, "username", "email@test.com", "", nil)
	state.Unlock()
	c.Assert(err, check.IsNil)

	serializedMacaroon, err := s.makeStoreMacaroon()
	c.Assert(err, check.IsNil)
	responseData, err := s.makeStoreMacaroonResponse(serializedMacaroon)
	c.Assert(err, check.IsNil)
	mockMyAppsServer := s.makeMyAppsServer(200, responseData)
	defer mockMyAppsServer.Close()

	discharge := `{"discharge_macaroon": "the-discharge-macaroon-serialized-data"}`
	mockSSOServer := s.makeSSOServer(200, discharge)
	defer mockSSOServer.Close()

	buf := bytes.NewBufferString(`{"username": "username", "email": "email@test.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, localUser.Macaroon))

	rsp := loginUser(loginCmd, req, localUser).(*resp)

	expected := userResponseData{
		ID:       1,
		Username: "username",
		Email:    "email@test.com",

		Macaroon:   localUser.Macaroon,
		Discharges: localUser.Discharges,
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	state.Lock()
	user, err := auth.User(state, localUser.ID)
	state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(user.Username, check.Equals, "username")
	c.Check(user.Email, check.Equals, localUser.Email)
	c.Check(user.Macaroon, check.Equals, localUser.Macaroon)
	c.Check(user.Discharges, check.IsNil)
	c.Check(user.StoreMacaroon, check.Equals, serializedMacaroon)
	c.Check(user.StoreDischarges, check.DeepEquals, []string{"the-discharge-macaroon-serialized-data"})
}

func (s *apiSuite) TestLogoutUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/logout", nil)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", `Macaroon root="macaroon", discharge="discharge"`)

	rsp := logoutUser(logoutCmd, req, user).(*resp)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)

	state.Lock()
	_, err = auth.User(state, user.ID)
	state.Unlock()
	c.Check(err, check.ErrorMatches, "invalid user")
}

func (s *apiSuite) TestLoginUserBadRequest(c *check.C) {
	buf := bytes.NewBufferString(`hello`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestLoginUserMyAppsError(c *check.C) {
	mockMyAppsServer := s.makeMyAppsServer(200, "{}")
	defer mockMyAppsServer.Close()

	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusUnauthorized)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "cannot get snap access permission")
}

func (s *apiSuite) TestLoginUserTwoFactorRequiredError(c *check.C) {
	serializedMacaroon, err := s.makeStoreMacaroon()
	c.Assert(err, check.IsNil)
	responseData, err := s.makeStoreMacaroonResponse(serializedMacaroon)
	c.Assert(err, check.IsNil)
	mockMyAppsServer := s.makeMyAppsServer(200, responseData)
	defer mockMyAppsServer.Close()

	discharge := `{"code": "TWOFACTOR_REQUIRED"}`
	mockSSOServer := s.makeSSOServer(401, discharge)
	defer mockSSOServer.Close()

	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusUnauthorized)
	c.Check(rsp.Result.(*errorResult).Kind, check.Equals, errorKindTwoFactorRequired)
}

func (s *apiSuite) TestLoginUserTwoFactorFailedError(c *check.C) {
	serializedMacaroon, err := s.makeStoreMacaroon()
	c.Assert(err, check.IsNil)
	responseData, err := s.makeStoreMacaroonResponse(serializedMacaroon)
	c.Assert(err, check.IsNil)
	mockMyAppsServer := s.makeMyAppsServer(200, responseData)
	defer mockMyAppsServer.Close()

	discharge := `{"code": "TWOFACTOR_FAILURE"}`
	mockSSOServer := s.makeSSOServer(403, discharge)
	defer mockSSOServer.Close()

	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusUnauthorized)
	c.Check(rsp.Result.(*errorResult).Kind, check.Equals, errorKindTwoFactorFailed)
}

func (s *apiSuite) TestLoginUserInvalidCredentialsError(c *check.C) {
	serializedMacaroon, err := s.makeStoreMacaroon()
	c.Assert(err, check.IsNil)
	responseData, err := s.makeStoreMacaroonResponse(serializedMacaroon)
	c.Assert(err, check.IsNil)
	mockMyAppsServer := s.makeMyAppsServer(200, responseData)
	defer mockMyAppsServer.Close()

	discharge := `{"code": "INVALID_CREDENTIALS"}`
	mockSSOServer := s.makeSSOServer(401, discharge)
	defer mockSSOServer.Close()

	buf := bytes.NewBufferString(`{"username": "email@.com", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusUnauthorized)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "cannot authenticate to snap store")
}

func (s *apiSuite) TestUserFromRequestNoHeader(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.Equals, auth.ErrInvalidAuth)
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderNoMacaroons(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", "Invalid")

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.ErrorMatches, "authorization header misses Macaroon prefix")
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderIncomplete(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", `Macaroon root=""`)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.ErrorMatches, "invalid authorization header")
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderCorrectMissingUser(c *check.C) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", `Macaroon root="macaroon", discharge="discharge"`)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.Equals, auth.ErrInvalidAuth)
	c.Check(user, check.IsNil)
}

func (s *apiSuite) TestUserFromRequestHeaderValidUser(c *check.C) {
	state := snapCmd.d.overlord.State()
	state.Lock()
	expectedUser, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, expectedUser.Macaroon))

	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.IsNil)
	c.Check(user, check.DeepEquals, expectedUser)
}

func (s *apiSuite) TestSnapsInfoOnePerIntegration(c *check.C) {
	d := s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	type tsnap struct {
		name string
		dev  string
		ver  string
		rev  int
	}

	tsnaps := []tsnap{
		{"foo", "bar", "v1", 5},
		{"bar", "baz", "v2", 10},
		{"baz", "qux", "v3", 15},
		{"qux", "mip", "v4", 20},
	}

	for _, snp := range tsnaps {
		s.mkInstalledInState(c, d, snp.name, snp.dev, snp.ver, snap.R(snp.rev), false, "")
	}

	rsp, ok := getSnapsInfo(snapsCmd, req, nil).(*resp)
	c.Assert(ok, check.Equals, true)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Result, check.NotNil)

	snaps := snapList(rsp.Result)
	c.Check(snaps, check.HasLen, len(tsnaps))

	for _, s := range tsnaps {
		var got map[string]interface{}
		for _, got = range snaps {
			if got["name"].(string) == s.name {
				break
			}
		}
		c.Check(got["name"], check.Equals, s.name)
		c.Check(got["version"], check.Equals, s.ver)
		c.Check(got["revision"], check.Equals, snap.R(s.rev).String())
		c.Check(got["developer"], check.Equals, s.dev)
		c.Check(got["confinement"], check.Equals, "strict")
	}
}

func (s *apiSuite) TestSnapsInfoOnlyLocal(c *check.C) {
	d := s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: "foo",
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(10), true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=local", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"local"})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "local")
}

func (s *apiSuite) TestSnapsInfoAll(c *check.C) {
	d := s.daemon(c)

	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(1), false, "")
	s.mkInstalledInState(c, d, "local", "foo", "v2", snap.R(2), false, "")
	s.mkInstalledInState(c, d, "local", "foo", "v3", snap.R(3), true, "")

	for _, t := range []struct {
		q        string
		numSnaps int
		typ      ResponseType
	}{
		{"?select=enabled", 1, "sync"},
		{`?select=`, 1, "sync"},
		{"", 1, "sync"},
		{"?select=all", 3, "sync"},
		{"?select=invalid-field", 0, "error"},
	} {
		req, err := http.NewRequest("GET", fmt.Sprintf("/v2/snaps%s", t.q), nil)
		c.Assert(err, check.IsNil)
		rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)
		c.Assert(rsp.Type, check.Equals, t.typ)

		if rsp.Type != "error" {
			snaps := snapList(rsp.Result)
			c.Assert(snaps, check.HasLen, t.numSnaps)
			c.Assert(snaps[0]["name"], check.Equals, "local")
		}
	}
}

func (s *apiSuite) TestFind(c *check.C) {
	s.suggestedCurrency = "EUR"

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: "foo",
	}}

	req, err := http.NewRequest("GET", "/v2/find?q=hi", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "store")
	c.Check(snaps[0]["prices"], check.IsNil)
	c.Check(snaps[0]["screenshots"], check.IsNil)
	c.Check(snaps[0]["channels"], check.IsNil)

	c.Check(rsp.SuggestedCurrency, check.Equals, "EUR")

	c.Check(s.storeSearch, check.DeepEquals, store.Search{Query: "hi"})
	c.Check(s.refreshCandidates, check.HasLen, 0)
}

func (s *apiSuite) TestFindRefreshes(c *check.C) {
	snapstateRefreshCandidates = snapstate.RefreshCandidates
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: "foo",
	}}
	s.mockSnap(c, "name: store\nversion: 1.0")

	req, err := http.NewRequest("GET", "/v2/find?select=refresh", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "store")
	c.Check(s.refreshCandidates, check.HasLen, 1)
}

func (s *apiSuite) TestFindRefreshSideloaded(c *check.C) {
	snapstateRefreshCandidates = snapstate.RefreshCandidates
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: "foo",
	}}

	s.mockSnap(c, "name: store\nversion: 1.0")

	var snapst snapstate.SnapState
	st := s.d.overlord.State()
	st.Lock()
	err := snapstate.Get(st, "store", &snapst)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Assert(snapst.Sequence, check.HasLen, 1)

	// clear the snapid
	snapst.Sequence[0].SnapID = ""
	st.Lock()
	snapstate.Set(st, "store", &snapst)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/find?select=refresh", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "store")
	c.Check(s.refreshCandidates, check.HasLen, 0)
}

func (s *apiSuite) TestFindPrivate(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{}

	req, err := http.NewRequest("GET", "/v2/find?q=foo&select=private", nil)
	c.Assert(err, check.IsNil)

	_ = searchStore(findCmd, req, nil).(*resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{
		Query:   "foo",
		Private: true,
	})
}

func (s *apiSuite) TestFindPrefix(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{}

	req, err := http.NewRequest("GET", "/v2/find?name=foo*", nil)
	c.Assert(err, check.IsNil)

	_ = searchStore(findCmd, req, nil).(*resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{Query: "foo", Prefix: true})
}

func (s *apiSuite) TestFindSection(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{}

	req, err := http.NewRequest("GET", "/v2/find?q=foo&section=bar", nil)
	c.Assert(err, check.IsNil)

	_ = searchStore(findCmd, req, nil).(*resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{
		Query:   "foo",
		Section: "bar",
	})
}

func (s *apiSuite) TestFindOne(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: "foo",
		Channels: map[string]*snap.ChannelSnapInfo{
			"stable": {
				Revision: snap.R(42),
			},
		},
	}}
	s.mockSnap(c, "name: store\nversion: 1.0")

	req, err := http.NewRequest("GET", "/v2/find?name=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["name"], check.Equals, "store")
	m := snaps[0]["channels"].(map[string]interface{})["stable"].(map[string]interface{})

	c.Check(m["revision"], check.Equals, "42")
}

func (s *apiSuite) TestFindOneNotFound(c *check.C) {
	s.daemon(c)

	s.err = store.ErrSnapNotFound
	s.mockSnap(c, "name: store\nversion: 1.0")

	req, err := http.NewRequest("GET", "/v2/find?name=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{})
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestFindRefreshNotQ(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/find?select=refresh&q=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, "cannot use 'q' with 'select=refresh'")
}

func (s *apiSuite) TestFindPriced(c *check.C) {
	s.suggestedCurrency = "GBP"

	s.rsnaps = []*snap.Info{{
		Type:    snap.TypeApp,
		Version: "v2",
		Prices: map[string]float64{
			"GBP": 1.23,
			"EUR": 2.34,
		},
		MustBuy: true,
		SideInfo: snap.SideInfo{
			RealName: "banana",
		},
		Publisher: "foo",
	}}

	req, err := http.NewRequest("GET", "/v2/find?q=banana&channel=stable", nil)
	c.Assert(err, check.IsNil)
	rsp, ok := searchStore(findCmd, req, nil).(*resp)
	c.Assert(ok, check.Equals, true)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)

	snap := snaps[0]
	c.Check(snap["name"], check.Equals, "banana")
	c.Check(snap["prices"], check.DeepEquals, map[string]interface{}{
		"EUR": 2.34,
		"GBP": 1.23,
	})
	c.Check(snap["status"], check.Equals, "priced")

	c.Check(rsp.SuggestedCurrency, check.Equals, "GBP")
}

func (s *apiSuite) TestFindScreenshotted(c *check.C) {
	s.rsnaps = []*snap.Info{{
		Type:    snap.TypeApp,
		Version: "v2",
		Screenshots: []snap.ScreenshotInfo{
			{
				URL:    "http://example.com/screenshot.png",
				Width:  800,
				Height: 1280,
			},
			{
				URL: "http://example.com/screenshot2.png",
			},
		},
		MustBuy: true,
		SideInfo: snap.SideInfo{
			RealName: "test-screenshot",
		},
		Publisher: "foo",
	}}

	req, err := http.NewRequest("GET", "/v2/find?q=test-screenshot", nil)
	c.Assert(err, check.IsNil)
	rsp, ok := searchStore(findCmd, req, nil).(*resp)
	c.Assert(ok, check.Equals, true)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)

	c.Check(snaps[0]["name"], check.Equals, "test-screenshot")
	c.Check(snaps[0]["screenshots"], check.DeepEquals, []interface{}{
		map[string]interface{}{
			"url":    "http://example.com/screenshot.png",
			"width":  float64(800),
			"height": float64(1280),
		},
		map[string]interface{}{
			"url": "http://example.com/screenshot2.png",
		},
	})
}

func (s *apiSuite) TestSnapsInfoOnlyStore(c *check.C) {
	d := s.daemon(c)

	s.suggestedCurrency = "EUR"

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: "foo",
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(10), true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=store", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"store"})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "store")
	c.Check(snaps[0]["prices"], check.IsNil)

	c.Check(rsp.SuggestedCurrency, check.Equals, "EUR")
}

func (s *apiSuite) TestSnapsStoreConfinement(c *check.C) {
	s.rsnaps = []*snap.Info{
		{
			// no explicit confinement in this one
			SideInfo: snap.SideInfo{
				RealName: "foo",
			},
		},
		{
			Confinement: snap.StrictConfinement,
			SideInfo: snap.SideInfo{
				RealName: "bar",
			},
		},
		{
			Confinement: snap.DevModeConfinement,
			SideInfo: snap.SideInfo{
				RealName: "baz",
			},
		},
	}

	req, err := http.NewRequest("GET", "/v2/find", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 3)

	for i, ss := range [][2]string{
		{"foo", string(snap.StrictConfinement)},
		{"bar", string(snap.StrictConfinement)},
		{"baz", string(snap.DevModeConfinement)},
	} {
		name, mode := ss[0], ss[1]
		c.Check(snaps[i]["name"], check.Equals, name, check.Commentf(name))
		c.Check(snaps[i]["confinement"], check.Equals, mode, check.Commentf(name))
	}
}

func (s *apiSuite) TestSnapsInfoStoreWithAuth(c *check.C) {
	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/snaps?sources=store", nil)
	c.Assert(err, check.IsNil)

	c.Assert(s.user, check.IsNil)

	_ = getSnapsInfo(snapsCmd, req, user).(*resp)

	// ensure user was set
	c.Assert(s.user, check.DeepEquals, user)
}

func (s *apiSuite) TestSnapsInfoLocalAndStore(c *check.C) {
	d := s.daemon(c)

	s.rsnaps = []*snap.Info{{
		Version: "v42",
		SideInfo: snap.SideInfo{
			RealName: "remote",
		},
		Publisher: "foo",
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(10), true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=local,store", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)

	// presence of 'store' in sources bounces request over to /find
	c.Assert(rsp.Sources, check.DeepEquals, []string{"store"})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["version"], check.Equals, "v42")

	// as does a 'q'
	req, err = http.NewRequest("GET", "/v2/snaps?q=what", nil)
	c.Assert(err, check.IsNil)
	rsp = getSnapsInfo(snapsCmd, req, nil).(*resp)
	snaps = snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["version"], check.Equals, "v42")

	// otherwise, local only
	req, err = http.NewRequest("GET", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)
	rsp = getSnapsInfo(snapsCmd, req, nil).(*resp)
	snaps = snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["version"], check.Equals, "v1")
}

func (s *apiSuite) TestSnapsInfoDefaultSources(c *check.C) {
	d := s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "remote",
		},
		Publisher: "foo",
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(10), true, "")

	req, err := http.NewRequest("GET", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"local"})
	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
}

func (s *apiSuite) TestSnapsInfoUnknownSource(c *check.C) {
	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "remote",
		},
		Publisher: "foo",
	}}
	s.mkInstalled(c, "local", "foo", "v1", snap.R(10), true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=unknown", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)

	c.Check(rsp.Sources, check.DeepEquals, []string{"local"})

	snaps := snapList(rsp.Result)
	c.Check(snaps, check.HasLen, 1)
}

func (s *apiSuite) TestSnapsInfoFilterRemote(c *check.C) {
	s.rsnaps = nil

	req, err := http.NewRequest("GET", "/v2/snaps?q=foo&sources=store", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req, nil).(*resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{Query: "foo"})

	c.Assert(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostSnapBadRequest(c *check.C) {
	buf := bytes.NewBufferString(`hello`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostSnapBadAction(c *check.C) {
	buf := bytes.NewBufferString(`{"action": "potato"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostSnap(c *check.C) {
	d := s.daemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

	soon := 0
	ensureStateSoon = func(st *state.State) {
		soon++
		ensureStateSoonImpl(st)
	}

	s.vars = map[string]string{"name": "foo"}

	snapInstructionDispTable["install"] = func(*snapInstruction, *state.State) (string, []*state.TaskSet, error) {
		return "foooo", nil, nil
	}
	defer func() {
		snapInstructionDispTable["install"] = snapInstall
	}()

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Summary(), check.Equals, "foooo")
	var names []string
	err = chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{"foo"})
	st.Unlock()

	c.Check(soon, check.Equals, 1)
}

func (s *apiSuite) TestPostSnapSetsUser(c *check.C) {
	d := s.daemon(c)
	ensureStateSoon = func(st *state.State) {}

	snapInstructionDispTable["install"] = func(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
		return fmt.Sprintf("<install by user %d>", inst.userID), nil, nil
	}
	defer func() {
		snapInstructionDispTable["install"] = snapInstall
	}()

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", `Macaroon root="macaroon", discharge="discharge"`)

	rsp := postSnap(snapCmd, req, user).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Summary(), check.Equals, "<install by user 1>")
	st.Unlock()
}

func (s *apiSuite) TestPostSnapDispatch(c *check.C) {
	inst := &snapInstruction{Snaps: []string{"foo"}}

	type T struct {
		s    string
		impl snapActionFunc
	}

	actions := []T{
		{"install", snapInstall},
		{"refresh", snapUpdate},
		{"remove", snapRemove},
		{"revert", snapRevert},
		{"enable", snapEnable},
		{"disable", snapDisable},
		{"xyzzy", nil},
	}

	for _, action := range actions {
		inst.Action = action.s
		// do you feel dirty yet?
		c.Check(fmt.Sprintf("%p", action.impl), check.Equals, fmt.Sprintf("%p", inst.dispatch()))
	}
}

func (s *apiSuite) TestPostSnapEnableDisableRevision(c *check.C) {
	for _, action := range []string{"enable", "disable"} {
		buf := bytes.NewBufferString(`{"action": "` + action + `", "revision": "42"}`)
		req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
		c.Assert(err, check.IsNil)

		rsp := postSnap(snapCmd, req, nil).(*resp)

		c.Check(rsp.Type, check.Equals, ResponseTypeError)
		c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
		c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "takes no revision")
	}
}

var sideLoadBodyWithoutDevMode = "" +
	"----hello--\r\n" +
	"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
	"\r\n" +
	"xyzzy\r\n" +
	"----hello--\r\n" +
	"Content-Disposition: form-data; name=\"dangerous\"\r\n" +
	"\r\n" +
	"true\r\n" +
	"----hello--\r\n" +
	"Content-Disposition: form-data; name=\"snap-path\"\r\n" +
	"\r\n" +
	"a/b/local.snap\r\n" +
	"----hello--\r\n"

func (s *apiSuite) TestSideloadSnapOnNonDevModeDistro(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	chgSummary := s.sideloadCheck(c, body, head, snapstate.Flags{RemoveSnapPath: true}, false)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "a/b/local.snap"`)
}

func (s *apiSuite) TestSideloadSnapOnDevModeDistro(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	restore := release.MockForcedDevmode(true)
	defer restore()
	flags := snapstate.Flags{RemoveSnapPath: true}
	chgSummary := s.sideloadCheck(c, body, head, flags, false)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "a/b/local.snap"`)
}

func (s *apiSuite) TestSideloadSnapDevMode(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	// try a multipart/form-data upload
	flags := snapstate.Flags{RemoveSnapPath: true}
	flags.DevMode = true
	chgSummary := s.sideloadCheck(c, body, head, flags, true)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "x"`)
}

func (s *apiSuite) TestSideloadSnapJailMode(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"jailmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"dangerous\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	// try a multipart/form-data upload
	flags := snapstate.Flags{JailMode: true, RemoveSnapPath: true}
	chgSummary := s.sideloadCheck(c, body, head, flags, true)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "x"`)
}

func (s *apiSuite) TestSideloadSnapJailModeAndDevmode(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"jailmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	d := s.daemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Equals, "cannot use devmode and jailmode flags together")
}

func (s *apiSuite) TestSideloadSnapJailModeInDevModeOS(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"jailmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	d := s.daemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	restore := release.MockForcedDevmode(true)
	defer restore()

	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Equals, "this system cannot honour the jailmode flag")
}

func (s *apiSuite) TestLocalInstallSnapDeriveSideInfo(c *check.C) {
	d := s.daemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()
	// add the assertions first
	st := d.overlord.State()
	assertAdd(st, s.storeSigning.StoreAccountKey(""))

	dev1Acct := assertstest.NewAccount(s.storeSigning, "devel1", nil, "")
	assertAdd(st, dev1Acct)

	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "x-id",
		"snap-name":    "x",
		"publisher-id": dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)
	assertAdd(st, snapDecl)

	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": "YK0GWATaZf09g_fvspYPqm_qtaiqf-KjaNj5uMEQCjQpuXWPjqQbeBINL5H_A0Lo",
		"snap-size":     "5",
		"snap-id":       "x-id",
		"snap-revision": "41",
		"developer-id":  dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)
	assertAdd(st, snapRev)

	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x.snap\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n"
	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	snapstateCoreInfo = func(s *state.State) (*snap.Info, error) {
		return nil, nil
	}
	snapstateInstallPath = func(s *state.State, si *snap.SideInfo, path, channel string, flags snapstate.Flags) (*state.TaskSet, error) {
		c.Check(flags, check.Equals, snapstate.Flags{RemoveSnapPath: true})
		c.Check(si, check.DeepEquals, &snap.SideInfo{
			RealName: "x",
			SnapID:   "x-id",
			Revision: snap.R(41),
		})

		return state.NewTaskSet(), nil
	}

	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeAsync)

	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Summary(), check.Equals, `Install "x" snap from file "x.snap"`)
	var names []string
	err = chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{"x"})
	var apiData map[string]interface{}
	err = chg.Get("api-data", &apiData)
	c.Assert(err, check.IsNil)
	c.Check(apiData, check.DeepEquals, map[string]interface{}{
		"snap-name": "x",
	})
}

func (s *apiSuite) TestSideloadSnapNoSignaturesDangerOff(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n"
	d := s.daemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	// this is the prefix used for tempfiles for sideloading
	glob := filepath.Join(os.TempDir(), "snapd-sideload-pkg-*")
	glbBefore, _ := filepath.Glob(glob)
	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Equals, `cannot find signatures with metadata for snap "x"`)
	glbAfter, _ := filepath.Glob(glob)
	c.Check(len(glbBefore), check.Equals, len(glbAfter))
}

func (s *apiSuite) TestSideloadSnapNotValidFormFile(c *check.C) {
	newTestDaemon(c)

	// try a multipart/form-data upload with missing "name"
	content := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}

	buf := bytes.NewBufferString(content)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	for k, v := range head {
		req.Header.Set(k, v)
	}

	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Assert(rsp.Result.(*errorResult).Message, check.Matches, `cannot find "snap" file field in provided multipart/form-data payload`)
}

func (s *apiSuite) TestTrySnap(c *check.C) {
	d := s.daemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

	req, err := http.NewRequest("POST", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	// mock a try dir
	tryDir := c.MkDir()
	snapYaml := filepath.Join(tryDir, "meta", "snap.yaml")
	err = os.MkdirAll(filepath.Dir(snapYaml), 0755)
	c.Assert(err, check.IsNil)
	err = ioutil.WriteFile(snapYaml, []byte("name: foo\nversion: 1.0\n"), 0644)
	c.Assert(err, check.IsNil)

	for _, t := range []struct {
		coreInfoErr error
		nTasks      int
		installSnap string
		flags       snapstate.Flags
		desc        string
	}{
		// core installed
		{nil, 1, "", snapstate.Flags{}, "core; -"},
		{nil, 1, "", snapstate.Flags{DevMode: true}, "core; devmode"},
		{nil, 1, "", snapstate.Flags{JailMode: true}, "core; jailmode"},
		{nil, 1, "", snapstate.Flags{Classic: true}, "core; classic"},
		// no-core-installed
		{state.ErrNoState, 2, "core", snapstate.Flags{}, "no core; -"},
		{state.ErrNoState, 2, "core", snapstate.Flags{DevMode: true}, "no core; devmode"},
		{state.ErrNoState, 2, "core", snapstate.Flags{JailMode: true}, "no core; jailmode"},
		{state.ErrNoState, 2, "core", snapstate.Flags{Classic: true}, "no core; classic"},
	} {
		soon := 0
		ensureStateSoon = func(st *state.State) {
			soon++
			ensureStateSoonImpl(st)
		}

		tryWasCalled := true
		snapstateTryPath = func(s *state.State, name, path string, flags snapstate.Flags) (*state.TaskSet, error) {
			c.Check(flags, check.DeepEquals, t.flags, check.Commentf(t.desc))
			tryWasCalled = true
			t := s.NewTask("fake-install-snap", "Doing a fake try")
			return state.NewTaskSet(t), nil
		}

		installSnap := ""
		snapstateInstall = func(s *state.State, name, channel string, revision snap.Revision, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
			if name != "core" {
				c.Check(flags, check.DeepEquals, t.flags, check.Commentf(t.desc))
			}
			installSnap = name
			t := s.NewTask("fake-install-snap", "Doing a fake install")
			return state.NewTaskSet(t), nil
		}

		snapstateCoreInfo = func(s *state.State) (*snap.Info, error) {
			return nil, t.coreInfoErr
		}

		// try the snap (without an installed core)
		rsp := trySnap(snapsCmd, req, nil, tryDir, t.flags).(*resp)
		c.Assert(rsp.Type, check.Equals, ResponseTypeAsync, check.Commentf(t.desc))
		c.Assert(tryWasCalled, check.Equals, true, check.Commentf(t.desc))

		st := d.overlord.State()
		st.Lock()
		chg := st.Change(rsp.Change)
		c.Assert(chg, check.NotNil, check.Commentf(t.desc))

		c.Assert(chg.Tasks(), check.HasLen, t.nTasks, check.Commentf(t.desc))
		c.Check(installSnap, check.Equals, t.installSnap, check.Commentf(t.desc))

		st.Unlock()
		<-chg.Ready()
		st.Lock()

		c.Check(chg.Kind(), check.Equals, "try-snap", check.Commentf(t.desc))
		c.Check(chg.Summary(), check.Equals, fmt.Sprintf(`Try "%s" snap from %s`, "foo", tryDir), check.Commentf(t.desc))
		var names []string
		err = chg.Get("snap-names", &names)
		c.Assert(err, check.IsNil, check.Commentf(t.desc))
		c.Check(names, check.DeepEquals, []string{"foo"}, check.Commentf(t.desc))
		var apiData map[string]interface{}
		err = chg.Get("api-data", &apiData)
		c.Assert(err, check.IsNil, check.Commentf(t.desc))
		c.Check(apiData, check.DeepEquals, map[string]interface{}{
			"snap-name": "foo",
		}, check.Commentf(t.desc))

		c.Check(soon, check.Equals, 1, check.Commentf(t.desc))
		st.Unlock()
	}
}

func (s *apiSuite) TestTrySnapRelative(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := trySnap(snapsCmd, req, nil, "relative-path", snapstate.Flags{}).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "need an absolute path")
}

func (s *apiSuite) TestTrySnapNotDir(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := trySnap(snapsCmd, req, nil, "/does/not/exist", snapstate.Flags{}).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "not a snap directory")
}

func (s *apiSuite) sideloadCheck(c *check.C, content string, head map[string]string, expectedFlags snapstate.Flags, hasCoreSnap bool) string {
	d := s.daemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

	soon := 0
	ensureStateSoon = func(st *state.State) {
		soon++
		ensureStateSoonImpl(st)
	}

	// setup done
	installQueue := []string{}
	unsafeReadSnapInfo = func(path string) (*snap.Info, error) {
		return &snap.Info{SuggestedName: "local"}, nil
	}

	snapstateCoreInfo = func(s *state.State) (*snap.Info, error) {
		if hasCoreSnap {
			return nil, nil
		}
		// pretend we do not have a state for ubuntu-core
		return nil, state.ErrNoState
	}
	snapstateInstall = func(s *state.State, name, channel string, revision snap.Revision, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		// NOTE: ubuntu-core is not installed in developer mode
		c.Check(flags, check.Equals, snapstate.Flags{})
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	snapstateInstallPath = func(s *state.State, si *snap.SideInfo, path, channel string, flags snapstate.Flags) (*state.TaskSet, error) {
		c.Check(flags, check.DeepEquals, expectedFlags)

		bs, err := ioutil.ReadFile(path)
		c.Check(err, check.IsNil)
		c.Check(string(bs), check.Equals, "xyzzy")

		installQueue = append(installQueue, si.RealName+"::"+path)
		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	buf := bytes.NewBufferString(content)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	for k, v := range head {
		req.Header.Set(k, v)
	}

	rsp := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeAsync)
	n := 1
	if !hasCoreSnap {
		n++
	}
	c.Assert(installQueue, check.HasLen, n)
	if !hasCoreSnap {
		c.Check(installQueue[0], check.Equals, defaultCoreSnapName)
	}
	c.Check(installQueue[n-1], check.Matches, "local::.*/snapd-sideload-pkg-.*")

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	c.Check(soon, check.Equals, 1)

	c.Assert(chg.Tasks(), check.HasLen, n)

	st.Unlock()
	<-chg.Ready()
	st.Lock()

	c.Check(chg.Kind(), check.Equals, "install-snap")
	var names []string
	err = chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{"local"})
	var apiData map[string]interface{}
	err = chg.Get("api-data", &apiData)
	c.Assert(err, check.IsNil)
	c.Check(apiData, check.DeepEquals, map[string]interface{}{
		"snap-name": "local",
	})

	return chg.Summary()
}

func (s *apiSuite) runGetConf(c *check.C, keys []string) map[string]interface{} {
	s.vars = map[string]string{"name": "test-snap"}
	req, err := http.NewRequest("GET", "/v2/snaps/test-snap/conf?keys="+strings.Join(keys, ","), nil)
	c.Check(err, check.IsNil)
	rec := httptest.NewRecorder()
	snapConfCmd.GET(snapConfCmd, req, nil).ServeHTTP(rec, req)

	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	return body["result"].(map[string]interface{})
}

func (s *apiSuite) TestGetConfSingleKey(c *check.C) {
	d := s.daemon(c)

	// Set a config that we'll get in a moment
	d.overlord.State().Lock()
	tr := config.NewTransaction(d.overlord.State())
	tr.Set("test-snap", "test-key1", "test-value1")
	tr.Set("test-snap", "test-key2", "test-value2")
	tr.Commit()
	d.overlord.State().Unlock()

	result := s.runGetConf(c, []string{"test-key1"})
	c.Check(result, check.DeepEquals, map[string]interface{}{"test-key1": "test-value1"})

	result = s.runGetConf(c, []string{"test-key1", "test-key2"})
	c.Check(result, check.DeepEquals, map[string]interface{}{"test-key1": "test-value1", "test-key2": "test-value2"})
}

func (s *apiSuite) TestSetConf(c *check.C) {
	d := s.daemon(c)
	s.mockSnap(c, configYaml)

	// Mock the hook runner
	hookRunner := testutil.MockCommand(c, "snap", "")
	defer hookRunner.Restore()

	d.overlord.Loop()
	defer d.overlord.Stop()

	text, err := json.Marshal(map[string]interface{}{"key": "value"})
	c.Assert(err, check.IsNil)

	buffer := bytes.NewBuffer(text)
	req, err := http.NewRequest("PUT", "/v2/snaps/config-snap/conf", buffer)
	c.Assert(err, check.IsNil)

	s.vars = map[string]string{"name": "config-snap"}

	rec := httptest.NewRecorder()
	snapConfCmd.PUT(snapConfCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)

	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.IsNil)

	// Check that the configure hook was run correctly
	c.Check(hookRunner.Calls(), check.DeepEquals, [][]string{{
		"snap", "run", "--hook", "configure", "-r", "unset", "config-snap",
	}})
}

func (s *apiSuite) TestAppIconGet(c *check.C) {
	d := s.daemon(c)

	// have an active foo in the system
	info := s.mkInstalledInState(c, d, "foo", "bar", "v1", snap.R(10), true, "")

	// have an icon for it in the package itself
	iconfile := filepath.Join(info.MountDir(), "meta", "gui", "icon.ick")
	c.Assert(os.MkdirAll(filepath.Dir(iconfile), 0755), check.IsNil)
	c.Check(ioutil.WriteFile(iconfile, []byte("ick"), 0644), check.IsNil)

	s.vars = map[string]string{"name": "foo"}
	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Body.String(), check.Equals, "ick")
}

func (s *apiSuite) TestAppIconGetInactive(c *check.C) {
	d := s.daemon(c)

	// have an *in*active foo in the system
	info := s.mkInstalledInState(c, d, "foo", "bar", "v1", snap.R(10), false, "")

	// have an icon for it in the package itself
	iconfile := filepath.Join(info.MountDir(), "meta", "gui", "icon.ick")
	c.Assert(os.MkdirAll(filepath.Dir(iconfile), 0755), check.IsNil)
	c.Check(ioutil.WriteFile(iconfile, []byte("ick"), 0644), check.IsNil)

	s.vars = map[string]string{"name": "foo"}
	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Body.String(), check.Equals, "ick")
}

func (s *apiSuite) TestAppIconGetNoIcon(c *check.C) {
	d := s.daemon(c)

	// have an *in*active foo in the system
	info := s.mkInstalledInState(c, d, "foo", "bar", "v1", snap.R(10), true, "")

	// NO ICON!
	err := os.RemoveAll(filepath.Join(info.MountDir(), "meta", "gui", "icon.svg"))
	c.Assert(err, check.IsNil)

	s.vars = map[string]string{"name": "foo"}
	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code/100, check.Equals, 4)
}

func (s *apiSuite) TestAppIconGetNoApp(c *check.C) {
	s.daemon(c)

	s.vars = map[string]string{"name": "foo"}
	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 404)
}

func (s *apiSuite) TestNotInstalledSnapIcon(c *check.C) {
	info := &snap.Info{SuggestedName: "notInstalledSnap", IconURL: "icon.svg"}
	iconfile := snapIcon(info)
	c.Check(iconfile, testutil.Contains, "icon.svg")
}

func (s *apiSuite) TestInstallOnNonDevModeDistro(c *check.C) {
	s.testInstall(c, false, snapstate.Flags{}, snap.R(0))
}
func (s *apiSuite) TestInstallOnDevModeDistro(c *check.C) {
	s.testInstall(c, true, snapstate.Flags{}, snap.R(0))
}
func (s *apiSuite) TestInstallRevision(c *check.C) {
	s.testInstall(c, false, snapstate.Flags{}, snap.R(42))
}

func (s *apiSuite) testInstall(c *check.C, forcedDevmode bool, flags snapstate.Flags, revision snap.Revision) {
	calledFlags := snapstate.Flags{}
	installQueue := []string{}
	restore := release.MockForcedDevmode(forcedDevmode)
	defer restore()

	snapstateCoreInfo = func(s *state.State) (*snap.Info, error) {
		// we have core
		return nil, nil
	}
	snapstateInstall = func(s *state.State, name, channel string, revno snap.Revision, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		installQueue = append(installQueue, name)
		c.Check(revision, check.Equals, revno)

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	defer func() {
		snapstateCoreInfo = nil
		snapstateInstall = nil
	}()

	d := s.daemon(c)

	d.overlord.Loop()
	defer d.overlord.Stop()

	var buf bytes.Buffer
	if revision.Unset() {
		buf.WriteString(`{"action": "install"}`)
	} else {
		fmt.Fprintf(&buf, `{"action": "install", "revision": %s}`, revision.String())
	}
	req, err := http.NewRequest("POST", "/v2/snaps/some-snap", &buf)
	c.Assert(err, check.IsNil)

	s.vars = map[string]string{"name": "some-snap"}
	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Assert(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	c.Check(chg.Tasks(), check.HasLen, 1)

	st.Unlock()
	<-chg.Ready()
	st.Lock()

	c.Check(chg.Status(), check.Equals, state.DoneStatus)
	c.Check(calledFlags, check.Equals, flags)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(chg.Kind(), check.Equals, "install-snap")
	c.Check(chg.Summary(), check.Equals, `Install "some-snap" snap`)
}

func (s *apiSuite) TestRefresh(c *check.C) {
	var calledFlags snapstate.Flags
	calledUserID := 0
	installQueue := []string{}
	assertstateCalledUserID := 0

	snapstateCoreInfo = func(s *state.State) (*snap.Info, error) {
		// we have core
		return nil, nil
	}
	snapstateUpdate = func(s *state.State, name, channel string, revision snap.Revision, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		calledUserID = userID
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		assertstateCalledUserID = userID
		return nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "refresh",
		Snaps:  []string{"some-snap"},
		userID: 17,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(assertstateCalledUserID, check.Equals, 17)
	c.Check(calledFlags, check.DeepEquals, snapstate.Flags{})
	c.Check(calledUserID, check.Equals, 17)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *apiSuite) TestRefreshDevMode(c *check.C) {
	var calledFlags snapstate.Flags
	calledUserID := 0
	installQueue := []string{}

	snapstateCoreInfo = func(s *state.State) (*snap.Info, error) {
		// we have core
		return nil, nil
	}
	snapstateUpdate = func(s *state.State, name, channel string, revision snap.Revision, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		calledUserID = userID
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		return nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:  "refresh",
		DevMode: true,
		Snaps:   []string{"some-snap"},
		userID:  17,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	flags := snapstate.Flags{}
	flags.DevMode = true
	c.Check(calledFlags, check.DeepEquals, flags)
	c.Check(calledUserID, check.Equals, 17)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *apiSuite) TestRefreshClassic(c *check.C) {
	var calledFlags snapstate.Flags

	snapstateCoreInfo = func(s *state.State) (*snap.Info, error) {
		// we have ubuntu-core
		return nil, nil
	}
	snapstateUpdate = func(s *state.State, name, channel string, revision snap.Revision, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		return nil, nil
	}
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		return nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:  "refresh",
		Classic: true,
		Snaps:   []string{"some-snap"},
		userID:  17,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags, check.DeepEquals, snapstate.Flags{Classic: true})
}

func (s *apiSuite) TestRefreshIgnoreValidation(c *check.C) {
	var calledFlags snapstate.Flags
	calledUserID := 0
	installQueue := []string{}

	snapstateCoreInfo = func(s *state.State) (*snap.Info, error) {
		// we have ubuntu-core
		return nil, nil
	}
	snapstateUpdate = func(s *state.State, name, channel string, revision snap.Revision, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		calledUserID = userID
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		return nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:           "refresh",
		IgnoreValidation: true,
		Snaps:            []string{"some-snap"},
		userID:           17,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	flags := snapstate.Flags{}
	flags.IgnoreValidation = true

	c.Check(calledFlags, check.DeepEquals, flags)
	c.Check(calledUserID, check.Equals, 17)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *apiSuite) TestPostSnapsOp(c *check.C) {
	assertstateRefreshSnapDeclarations = func(*state.State, int) error { return nil }
	snapstateUpdateMany = func(s *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 0)
		t := s.NewTask("fake-refresh-all", "Refreshing everything")
		return []string{"fake1", "fake2"}, []*state.TaskSet{state.NewTaskSet(t)}, nil
	}

	d := s.daemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

	buf := bytes.NewBufferString(`{"action": "refresh"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/json")

	rsp, ok := postSnaps(snapsCmd, req, nil).(*resp)
	c.Assert(ok, check.Equals, true)
	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Check(chg.Summary(), check.Equals, `Refresh snaps "fake1", "fake2"`)
	var apiData map[string]interface{}
	c.Check(chg.Get("api-data", &apiData), check.IsNil)
	c.Check(apiData["snap-names"], check.DeepEquals, []interface{}{"fake1", "fake2"})
}

func (s *apiSuite) TestRefreshAll(c *check.C) {
	refreshSnapDecls := false
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		refreshSnapDecls = true
		return assertstate.RefreshSnapDeclarations(s, userID)
	}
	d := s.daemon(c)

	for _, tst := range []struct {
		snaps []string
		msg   string
	}{
		{nil, "Refresh all snaps: no updates"},
		{[]string{"fake"}, `Refresh snap "fake"`},
		{[]string{"fake1", "fake2"}, `Refresh snaps "fake1", "fake2"`},
	} {
		refreshSnapDecls = false

		snapstateUpdateMany = func(s *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
			c.Check(names, check.HasLen, 0)
			t := s.NewTask("fake-refresh-all", "Refreshing everything")
			return tst.snaps, []*state.TaskSet{state.NewTaskSet(t)}, nil
		}

		inst := &snapInstruction{Action: "refresh"}
		st := d.overlord.State()
		st.Lock()
		summary, _, _, err := snapUpdateMany(inst, st)
		st.Unlock()
		c.Assert(err, check.IsNil)
		c.Check(summary, check.Equals, tst.msg)
		c.Check(refreshSnapDecls, check.Equals, true)
	}
}

func (s *apiSuite) TestRefreshAllNoChanges(c *check.C) {
	refreshSnapDecls := false
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		refreshSnapDecls = true
		return assertstate.RefreshSnapDeclarations(s, userID)
	}

	snapstateUpdateMany = func(s *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 0)
		return nil, nil, nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{Action: "refresh"}
	st := d.overlord.State()
	st.Lock()
	summary, _, _, err := snapUpdateMany(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(summary, check.Equals, `Refresh all snaps: no updates`)
	c.Check(refreshSnapDecls, check.Equals, true)
}

func (s *apiSuite) TestRefreshMany(c *check.C) {
	refreshSnapDecls := false
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		refreshSnapDecls = true
		return nil
	}

	snapstateUpdateMany = func(s *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 2)
		t := s.NewTask("fake-refresh-2", "Refreshing two")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{Action: "refresh", Snaps: []string{"foo", "bar"}}
	st := d.overlord.State()
	st.Lock()
	summary, updates, _, err := snapUpdateMany(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(summary, check.Equals, `Refresh snaps "foo", "bar"`)
	c.Check(updates, check.DeepEquals, inst.Snaps)
	c.Check(refreshSnapDecls, check.Equals, true)
}

func (s *apiSuite) TestRefreshMany1(c *check.C) {
	refreshSnapDecls := false
	assertstateRefreshSnapDeclarations = func(s *state.State, userID int) error {
		refreshSnapDecls = true
		return nil
	}

	snapstateUpdateMany = func(s *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 1)
		t := s.NewTask("fake-refresh-1", "Refreshing one")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{Action: "refresh", Snaps: []string{"foo"}}
	st := d.overlord.State()
	st.Lock()
	summary, updates, _, err := snapUpdateMany(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(summary, check.Equals, `Refresh snap "foo"`)
	c.Check(updates, check.DeepEquals, inst.Snaps)
	c.Check(refreshSnapDecls, check.Equals, true)
}

func (s *apiSuite) TestInstallMany(c *check.C) {
	snapstateInstallMany = func(s *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 2)
		t := s.NewTask("fake-install-2", "Install two")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{Action: "install", Snaps: []string{"foo", "bar"}}
	st := d.overlord.State()
	st.Lock()
	summary, installs, _, err := snapInstallMany(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(summary, check.Equals, `Install snaps "foo", "bar"`)
	c.Check(installs, check.DeepEquals, inst.Snaps)
}

func (s *apiSuite) TestRemoveMany(c *check.C) {
	snapstateRemoveMany = func(s *state.State, names []string) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 2)
		t := s.NewTask("fake-remove-2", "Remove two")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{Action: "remove", Snaps: []string{"foo", "bar"}}
	st := d.overlord.State()
	st.Lock()
	summary, removes, _, err := snapRemoveMany(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(summary, check.Equals, `Remove snaps "foo", "bar"`)
	c.Check(removes, check.DeepEquals, inst.Snaps)
}

func (s *apiSuite) TestInstallMissingCoreSnap(c *check.C) {
	installQueue := []*state.Task{}

	snapstateCoreInfo = func(s *state.State) (*snap.Info, error) {
		// pretend we do not have a core
		return nil, state.ErrNoState
	}
	snapstateInstall = func(s *state.State, name, channel string, revision snap.Revision, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		t1 := s.NewTask("fake-install-snap", name)
		t2 := s.NewTask("fake-install-snap", "second task is just here so that we can check that the wait is correctly added to all tasks")
		installQueue = append(installQueue, t1, t2)
		return state.NewTaskSet(t1, t2), nil
	}

	d := s.daemon(c)

	d.overlord.Loop()
	defer d.overlord.Stop()

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/some-snap", buf)
	c.Assert(err, check.IsNil)

	s.vars = map[string]string{"name": "some-snap"}
	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Assert(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	c.Check(chg.Tasks(), check.HasLen, 4)

	c.Check(installQueue, check.HasLen, 4)
	// the two OS snap install tasks
	c.Check(installQueue[0].Summary(), check.Equals, defaultCoreSnapName)
	c.Check(installQueue[0].WaitTasks(), check.HasLen, 0)
	c.Check(installQueue[1].WaitTasks(), check.HasLen, 0)
	// the two "some-snap" install tasks
	c.Check(installQueue[2].Summary(), check.Equals, "some-snap")
	c.Check(installQueue[2].WaitTasks(), check.HasLen, 2)
	c.Check(installQueue[3].WaitTasks(), check.HasLen, 2)
}

// Installing ubuntu-core when not having ubuntu-core doesn't misbehave and try
// to install ubuntu-core twice.
func (s *apiSuite) TestInstallCoreSnapWhenMissing(c *check.C) {
	installQueue := []*state.Task{}

	snapstateCoreInfo = func(s *state.State) (*snap.Info, error) {
		// pretend we do not have a core
		return nil, state.ErrNoState
	}
	snapstateInstall = func(s *state.State, name, channel string, revision snap.Revision, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		t1 := s.NewTask("fake-install-snap", name)
		t2 := s.NewTask("fake-install-snap", "second task is just here so that we can check that the wait is correctly added to all tasks")
		installQueue = append(installQueue, t1, t2)
		return state.NewTaskSet(t1, t2), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "install",
		Snaps:  []string{defaultCoreSnapName},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(installQueue, check.HasLen, 2)
	// the only OS snap install tasks
	c.Check(installQueue[0].Summary(), check.Equals, defaultCoreSnapName)
	c.Check(installQueue[0].WaitTasks(), check.HasLen, 0)
	c.Check(installQueue[1].WaitTasks(), check.HasLen, 0)
}

func (s *apiSuite) TestInstallFails(c *check.C) {
	snapstateCoreInfo = func(s *state.State) (*snap.Info, error) {
		// we have core
		return nil, nil
	}

	snapstateInstall = func(s *state.State, name, channel string, revision snap.Revision, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		t := s.NewTask("fake-install-snap-error", "Install task")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)

	d.overlord.Loop()
	defer d.overlord.Stop()

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	c.Check(chg.Tasks(), check.HasLen, 1)

	st.Unlock()
	<-chg.Ready()
	st.Lock()

	c.Check(chg.Err(), check.ErrorMatches, `(?sm).*Install task \(fake-install-snap-error errored\)`)
}

func (s *apiSuite) TestInstallLeaveOld(c *check.C) {
	c.Skip("temporarily dropped half-baked support while sorting out flag mess")
	var calledFlags snapstate.Flags

	snapstateInstall = func(s *state.State, name, channel string, revision snap.Revision, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:   "install",
		LeaveOld: true,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Assert(err, check.IsNil)

	c.Check(calledFlags, check.DeepEquals, snapstate.Flags{})
	c.Check(err, check.IsNil)
}

func (s *apiSuite) TestInstallDevMode(c *check.C) {
	var calledFlags snapstate.Flags

	snapstateCoreInfo = func(s *state.State) (*snap.Info, error) {
		return nil, nil
	}

	snapstateInstall = func(s *state.State, name, channel string, revision snap.Revision, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "install",
		// Install the snap in developer mode
		DevMode: true,
		Snaps:   []string{"fake"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags.DevMode, check.Equals, true)
}

func (s *apiSuite) TestInstallJailMode(c *check.C) {
	var calledFlags snapstate.Flags

	snapstateCoreInfo = func(s *state.State) (*snap.Info, error) {
		return nil, nil
	}

	snapstateInstall = func(s *state.State, name, channel string, revision snap.Revision, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:   "install",
		JailMode: true,
		Snaps:    []string{"fake"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags.JailMode, check.Equals, true)
}

func (s *apiSuite) TestInstallJailModeDevModeOS(c *check.C) {
	restore := release.MockForcedDevmode(true)
	defer restore()

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:   "install",
		JailMode: true,
		Snaps:    []string{"foo"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.ErrorMatches, "this system cannot honour the jailmode flag")
}

func (s *apiSuite) TestInstallJailModeDevMode(c *check.C) {
	d := s.daemon(c)
	inst := &snapInstruction{
		Action:   "install",
		DevMode:  true,
		JailMode: true,
		Snaps:    []string{"foo"},
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.ErrorMatches, "cannot use devmode and jailmode flags together")
}

func (s *apiSuite) testRevertSnap(inst *snapInstruction, c *check.C) {
	queue := []string{}

	instFlags, err := inst.modeFlags()
	c.Assert(err, check.IsNil)

	snapstateRevert = func(s *state.State, name string, flags snapstate.Flags) (*state.TaskSet, error) {
		c.Check(flags, check.Equals, instFlags)
		queue = append(queue, name)
		return nil, nil
	}
	snapstateRevertToRevision = func(s *state.State, name string, rev snap.Revision, flags snapstate.Flags) (*state.TaskSet, error) {
		c.Check(flags, check.Equals, instFlags)
		queue = append(queue, fmt.Sprintf("%s (%s)", name, rev))
		return nil, nil
	}

	d := s.daemon(c)
	inst.Action = "revert"
	inst.Snaps = []string{"some-snap"}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)
	if inst.Revision.Unset() {
		c.Check(queue, check.DeepEquals, []string{inst.Snaps[0]})
	} else {
		c.Check(queue, check.DeepEquals, []string{fmt.Sprintf("%s (%s)", inst.Snaps[0], inst.Revision)})
	}
	c.Check(summary, check.Equals, `Revert "some-snap" snap`)
}

func (s *apiSuite) TestRevertSnap(c *check.C) {
	s.testRevertSnap(&snapInstruction{}, c)
}

func (s *apiSuite) TestRevertSnapDevMode(c *check.C) {
	s.testRevertSnap(&snapInstruction{DevMode: true}, c)
}

func (s *apiSuite) TestRevertSnapJailMode(c *check.C) {
	s.testRevertSnap(&snapInstruction{JailMode: true}, c)
}

func (s *apiSuite) TestRevertSnapClassic(c *check.C) {
	s.testRevertSnap(&snapInstruction{Classic: true}, c)
}

func (s *apiSuite) TestRevertSnapToRevision(c *check.C) {
	s.testRevertSnap(&snapInstruction{Revision: snap.R(1)}, c)
}

func (s *apiSuite) TestRevertSnapToRevisionDevMode(c *check.C) {
	s.testRevertSnap(&snapInstruction{Revision: snap.R(1), DevMode: true}, c)
}

func (s *apiSuite) TestRevertSnapToRevisionJailMode(c *check.C) {
	s.testRevertSnap(&snapInstruction{Revision: snap.R(1), JailMode: true}, c)
}

func (s *apiSuite) TestRevertSnapToRevisionClassic(c *check.C) {
	s.testRevertSnap(&snapInstruction{Revision: snap.R(1), Classic: true}, c)
}

func snapList(rawSnaps interface{}) []map[string]interface{} {
	snaps := make([]map[string]interface{}, len(rawSnaps.([]*json.RawMessage)))
	for i, raw := range rawSnaps.([]*json.RawMessage) {
		err := json.Unmarshal([]byte(*raw), &snaps[i])
		if err != nil {
			panic(err)
		}
	}
	return snaps
}

// Tests for GET /v2/interfaces

func (s *apiSuite) TestInterfaces(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	repo := d.overlord.InterfaceManager().Repository()
	connRef := interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}
	c.Assert(repo.Connect(connRef), check.IsNil)

	req, err := http.NewRequest("GET", "/v2/interfaces", nil)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.GET(interfacesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"plugs": []interface{}{
				map[string]interface{}{
					"snap":      "consumer",
					"plug":      "plug",
					"interface": "test",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
					"connections": []interface{}{
						map[string]interface{}{"snap": "producer", "slot": "slot"},
					},
				},
			},
			"slots": []interface{}{
				map[string]interface{}{
					"snap":      "producer",
					"slot":      "slot",
					"interface": "test",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
					"connections": []interface{}{
						map[string]interface{}{"snap": "consumer", "plug": "plug"},
					},
				},
			},
		},
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
}

// Test for POST /v2/interfaces

func (s *apiSuite) TestConnectPlugSuccess(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &interfaceAction{
		Action: "connect",
		Plugs:  []plugJSON{{Snap: "consumer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "producer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.IsNil)

	repo := d.overlord.InterfaceManager().Repository()
	plug := repo.Plug("consumer", "plug")
	slot := repo.Slot("producer", "slot")
	c.Assert(plug.Connections, check.HasLen, 1)
	c.Assert(slot.Connections, check.HasLen, 1)
	c.Check(plug.Connections[0], check.DeepEquals, interfaces.SlotRef{Snap: "producer", Name: "slot"})
	c.Check(slot.Connections[0], check.DeepEquals, interfaces.PlugRef{Snap: "consumer", Name: "plug"})
}

func (s *apiSuite) TestConnectPlugFailureInterfaceMismatch(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "different"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, differentProducerYaml)

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &interfaceAction{
		Action: "connect",
		Plugs:  []plugJSON{{Snap: "consumer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "producer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "cannot connect consumer:plug (\"test\" interface) to producer:slot (\"different\" interface)",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
	repo := d.overlord.InterfaceManager().Repository()
	plug := repo.Plug("consumer", "plug")
	slot := repo.Slot("producer", "slot")
	c.Assert(plug.Connections, check.HasLen, 0)
	c.Assert(slot.Connections, check.HasLen, 0)
}

func (s *apiSuite) TestConnectPlugFailureNoSuchPlug(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	// there is no consumer, no plug defined
	s.mockSnap(c, producerYaml)
	s.mockSnap(c, consumerYaml)

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &interfaceAction{
		Action: "connect",
		Plugs:  []plugJSON{{Snap: "consumer", Name: "missingplug"}},
		Slots:  []slotJSON{{Snap: "producer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)

	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "snap \"consumer\" has no plug named \"missingplug\"",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})

	repo := d.overlord.InterfaceManager().Repository()
	slot := repo.Slot("producer", "slot")
	c.Assert(slot.Connections, check.HasLen, 0)
}

func (s *apiSuite) TestConnectPlugFailureNoSuchSlot(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)
	// there is no producer, no slot defined

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &interfaceAction{
		Action: "connect",
		Plugs:  []plugJSON{{Snap: "consumer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "producer", Name: "missingslot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)

	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "snap \"producer\" has no slot named \"missingslot\"",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})

	repo := d.overlord.InterfaceManager().Repository()
	plug := repo.Plug("consumer", "plug")
	c.Assert(plug.Connections, check.HasLen, 0)
}

func (s *apiSuite) TestDisconnectPlugSuccess(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	repo := d.overlord.InterfaceManager().Repository()
	connRef := interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: interfaces.SlotRef{Snap: "producer", Name: "slot"},
	}
	c.Assert(repo.Connect(connRef), check.IsNil)

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &interfaceAction{
		Action: "disconnect",
		Plugs:  []plugJSON{{Snap: "consumer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "producer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.IsNil)

	plug := repo.Plug("consumer", "plug")
	slot := repo.Slot("producer", "slot")
	c.Assert(plug.Connections, check.HasLen, 0)
	c.Assert(slot.Connections, check.HasLen, 0)
}

func (s *apiSuite) TestDisconnectPlugFailureNoSuchPlug(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	// there is no consumer, no plug defined
	s.mockSnap(c, producerYaml)

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &interfaceAction{
		Action: "disconnect",
		Plugs:  []plugJSON{{Snap: "consumer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "producer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, `cannot perform the following tasks:
- Disconnect consumer:plug from producer:slot (snap "consumer" has no plug named "plug")`)

	repo := d.overlord.InterfaceManager().Repository()
	slot := repo.Slot("producer", "slot")
	c.Assert(slot.Connections, check.HasLen, 0)
}

func (s *apiSuite) TestDisconnectPlugFailureNoSuchSlot(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	// there is no producer, no slot defined

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &interfaceAction{
		Action: "disconnect",
		Plugs:  []plugJSON{{Snap: "consumer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "producer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, `cannot perform the following tasks:
- Disconnect consumer:plug from producer:slot (snap "producer" has no slot named "slot")`)

	repo := d.overlord.InterfaceManager().Repository()
	plug := repo.Plug("consumer", "plug")
	c.Assert(plug.Connections, check.HasLen, 0)
}

func (s *apiSuite) TestDisconnectPlugFailureNotConnected(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &ifacetest.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &interfaceAction{
		Action: "disconnect",
		Plugs:  []plugJSON{{Snap: "consumer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "producer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, `cannot perform the following tasks:
- Disconnect consumer:plug from producer:slot (cannot disconnect consumer:plug from producer:slot, it is not connected)`)

	repo := d.overlord.InterfaceManager().Repository()
	plug := repo.Plug("consumer", "plug")
	slot := repo.Slot("producer", "slot")
	c.Assert(plug.Connections, check.HasLen, 0)
	c.Assert(slot.Connections, check.HasLen, 0)
}

func (s *apiSuite) TestUnsupportedInterfaceRequest(c *check.C) {
	buf := bytes.NewBuffer([]byte(`garbage`))
	req, err := http.NewRequest("POST", "/v2/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "cannot decode request body into an interface action: invalid character 'g' looking for beginning of value",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
}

func (s *apiSuite) TestMissingInterfaceAction(c *check.C) {
	action := &interfaceAction{}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "interface action not specified",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
}

func (s *apiSuite) TestUnsupportedInterfaceAction(c *check.C) {
	s.daemon(c)
	action := &interfaceAction{Action: "foo"}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 400)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result": map[string]interface{}{
			"message": "unsupported interface action: \"foo\"",
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
}

func assertAdd(st *state.State, a asserts.Assertion) {
	st.Lock()
	defer st.Unlock()
	err := assertstate.Add(st, a)
	if err != nil {
		panic(err)
	}
}

func (s *apiSuite) TestAssertOK(c *check.C) {
	// Setup
	d := s.daemon(c)
	st := d.overlord.State()
	// add store key
	assertAdd(st, s.storeSigning.StoreAccountKey(""))

	acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	buf := bytes.NewBuffer(asserts.Encode(acct))
	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions", buf)
	c.Assert(err, check.IsNil)
	rsp := doAssert(assertsCmd, req, nil).(*resp)
	// Verify (external)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	// Verify (internal)
	st.Lock()
	defer st.Unlock()
	_, err = assertstate.DB(st).Find(asserts.AccountType, map[string]string{
		"account-id": acct.AccountID(),
	})
	c.Check(err, check.IsNil)
}

func (s *apiSuite) TestAssertStreamOK(c *check.C) {
	// Setup
	d := s.daemon(c)
	st := d.overlord.State()

	acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	buf := &bytes.Buffer{}
	enc := asserts.NewEncoder(buf)
	err := enc.Encode(acct)
	c.Assert(err, check.IsNil)
	err = enc.Encode(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, check.IsNil)

	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions", buf)
	c.Assert(err, check.IsNil)
	rsp := doAssert(assertsCmd, req, nil).(*resp)
	// Verify (external)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	// Verify (internal)
	st.Lock()
	defer st.Unlock()
	_, err = assertstate.DB(st).Find(asserts.AccountType, map[string]string{
		"account-id": acct.AccountID(),
	})
	c.Check(err, check.IsNil)
}

func (s *apiSuite) TestAssertInvalid(c *check.C) {
	// Setup
	buf := bytes.NewBufferString("blargh")
	req, err := http.NewRequest("POST", "/v2/assertions", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	// Execute
	assertsCmd.POST(assertsCmd, req, nil).ServeHTTP(rec, req)
	// Verify (external)
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains,
		"cannot decode request body into assertions")
}

func (s *apiSuite) TestAssertError(c *check.C) {
	s.daemon(c)
	// Setup
	acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	buf := bytes.NewBuffer(asserts.Encode(acct))
	req, err := http.NewRequest("POST", "/v2/assertions", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	// Execute
	assertsCmd.POST(assertsCmd, req, nil).ServeHTTP(rec, req)
	// Verify (external)
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains, "assert failed")
}

func (s *apiSuite) TestAssertsFindManyAll(c *check.C) {
	// Setup
	d := s.daemon(c)
	// add store key
	st := d.overlord.State()
	assertAdd(st, s.storeSigning.StoreAccountKey(""))
	acct := assertstest.NewAccount(s.storeSigning, "developer1", map[string]interface{}{
		"account-id": "developer1-id",
	}, "")
	assertAdd(st, acct)

	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions/account", nil)
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"assertType": "account"}
	rec := httptest.NewRecorder()
	assertsFindManyCmd.GET(assertsFindManyCmd, req, nil).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, http.StatusOK, check.Commentf("body %q", rec.Body))
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/x.ubuntu.assertion; bundle=y")
	c.Check(rec.HeaderMap.Get("X-Ubuntu-Assertions-Count"), check.Equals, "3")
	dec := asserts.NewDecoder(rec.Body)
	a1, err := dec.Decode()
	c.Assert(err, check.IsNil)
	c.Check(a1.Type(), check.Equals, asserts.AccountType)

	a2, err := dec.Decode()
	c.Assert(err, check.IsNil)

	a3, err := dec.Decode()
	c.Assert(err, check.IsNil)

	_, err = dec.Decode()
	c.Assert(err, check.Equals, io.EOF)

	ids := []string{a1.(*asserts.Account).AccountID(), a2.(*asserts.Account).AccountID(), a3.(*asserts.Account).AccountID()}
	sort.Strings(ids)
	c.Check(ids, check.DeepEquals, []string{"can0nical", "canonical", "developer1-id"})
}

func (s *apiSuite) TestAssertsFindManyFilter(c *check.C) {
	// Setup
	d := s.daemon(c)
	// add store key
	st := d.overlord.State()
	assertAdd(st, s.storeSigning.StoreAccountKey(""))
	acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	assertAdd(st, acct)

	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions/account?username=developer1", nil)
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"assertType": "account"}
	rec := httptest.NewRecorder()
	assertsFindManyCmd.GET(assertsFindManyCmd, req, nil).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, http.StatusOK, check.Commentf("body %q", rec.Body))
	c.Check(rec.HeaderMap.Get("X-Ubuntu-Assertions-Count"), check.Equals, "1")
	dec := asserts.NewDecoder(rec.Body)
	a1, err := dec.Decode()
	c.Assert(err, check.IsNil)
	c.Check(a1.Type(), check.Equals, asserts.AccountType)
	c.Check(a1.(*asserts.Account).Username(), check.Equals, "developer1")
	c.Check(a1.(*asserts.Account).AccountID(), check.Equals, acct.AccountID())
	_, err = dec.Decode()
	c.Check(err, check.Equals, io.EOF)
}

func (s *apiSuite) TestAssertsFindManyNoResults(c *check.C) {
	// Setup
	d := s.daemon(c)
	// add store key
	st := d.overlord.State()
	assertAdd(st, s.storeSigning.StoreAccountKey(""))
	acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	assertAdd(st, acct)

	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions/account?username=xyzzyx", nil)
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"assertType": "account"}
	rec := httptest.NewRecorder()
	assertsFindManyCmd.GET(assertsFindManyCmd, req, nil).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, http.StatusOK, check.Commentf("body %q", rec.Body))
	c.Check(rec.HeaderMap.Get("X-Ubuntu-Assertions-Count"), check.Equals, "0")
	dec := asserts.NewDecoder(rec.Body)
	_, err = dec.Decode()
	c.Check(err, check.Equals, io.EOF)
}

func (s *apiSuite) TestAssertsInvalidType(c *check.C) {
	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions/foo", nil)
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"assertType": "foo"}
	rec := httptest.NewRecorder()
	assertsFindManyCmd.GET(assertsFindManyCmd, req, nil).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains, "invalid assert type")
}

func setupChanges(st *state.State) []string {
	chg1 := st.NewChange("install", "install...")
	chg1.Set("snap-names", []string{"funky-snap-name"})
	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("activate", "2...")
	chg1.AddAll(state.NewTaskSet(t1, t2))
	t1.Logf("l11")
	t1.Logf("l12")
	chg2 := st.NewChange("remove", "remove..")
	t3 := st.NewTask("unlink", "1...")
	chg2.AddTask(t3)
	t3.SetStatus(state.ErrorStatus)
	t3.Errorf("rm failed")

	return []string{chg1.ID(), chg2.ID(), t1.ID(), t2.ID(), t3.ID()}
}

func (s *apiSuite) TestStateChangesDefaultToInProgress(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := newTestDaemon(c)
	st := d.overlord.State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	// Execute
	req, err := http.NewRequest("GET", "/v2/changes", nil)
	c.Assert(err, check.IsNil)
	rsp := getChanges(stateChangesCmd, req, nil).(*resp)

	// Verify
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Assert(rsp.Result, check.HasLen, 1)

	res, err := rsp.MarshalJSON()
	c.Assert(err, check.IsNil)

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"install","summary":"install...","status":"Do","tasks":\[{"id":"\w+","kind":"download","summary":"1...","status":"Do","log":\["2016-04-21T01:02:03Z INFO l11","2016-04-21T01:02:03Z INFO l12"],"progress":{"label":"","done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z"}.*`)
}

func (s *apiSuite) TestStateChangesInProgress(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := newTestDaemon(c)
	st := d.overlord.State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	// Execute
	req, err := http.NewRequest("GET", "/v2/changes?select=in-progress", nil)
	c.Assert(err, check.IsNil)
	rsp := getChanges(stateChangesCmd, req, nil).(*resp)

	// Verify
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Assert(rsp.Result, check.HasLen, 1)

	res, err := rsp.MarshalJSON()
	c.Assert(err, check.IsNil)

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"install","summary":"install...","status":"Do","tasks":\[{"id":"\w+","kind":"download","summary":"1...","status":"Do","log":\["2016-04-21T01:02:03Z INFO l11","2016-04-21T01:02:03Z INFO l12"],"progress":{"label":"","done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z"}.*],"ready":false,"spawn-time":"2016-04-21T01:02:03Z"}.*`)
}

func (s *apiSuite) TestStateChangesAll(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := newTestDaemon(c)
	st := d.overlord.State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	// Execute
	req, err := http.NewRequest("GET", "/v2/changes?select=all", nil)
	c.Assert(err, check.IsNil)
	rsp := getChanges(stateChangesCmd, req, nil).(*resp)

	// Verify
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Assert(rsp.Result, check.HasLen, 2)

	res, err := rsp.MarshalJSON()
	c.Assert(err, check.IsNil)

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"install","summary":"install...","status":"Do","tasks":\[{"id":"\w+","kind":"download","summary":"1...","status":"Do","log":\["2016-04-21T01:02:03Z INFO l11","2016-04-21T01:02:03Z INFO l12"],"progress":{"label":"","done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z"}.*],"ready":false,"spawn-time":"2016-04-21T01:02:03Z"}.*`)
	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"remove","summary":"remove..","status":"Error","tasks":\[{"id":"\w+","kind":"unlink","summary":"1...","status":"Error","log":\["2016-04-21T01:02:03Z ERROR rm failed"],"progress":{"label":"","done":1,"total":1},"spawn-time":"2016-04-21T01:02:03Z","ready-time":"2016-04-21T01:02:03Z"}.*],"ready":true,"err":"[^"]+".*`)
}

func (s *apiSuite) TestStateChangesReady(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := newTestDaemon(c)
	st := d.overlord.State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	// Execute
	req, err := http.NewRequest("GET", "/v2/changes?select=ready", nil)
	c.Assert(err, check.IsNil)
	rsp := getChanges(stateChangesCmd, req, nil).(*resp)

	// Verify
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Assert(rsp.Result, check.HasLen, 1)

	res, err := rsp.MarshalJSON()
	c.Assert(err, check.IsNil)

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"remove","summary":"remove..","status":"Error","tasks":\[{"id":"\w+","kind":"unlink","summary":"1...","status":"Error","log":\["2016-04-21T01:02:03Z ERROR rm failed"],"progress":{"label":"","done":1,"total":1},"spawn-time":"2016-04-21T01:02:03Z","ready-time":"2016-04-21T01:02:03Z"}.*],"ready":true,"err":"[^"]+".*`)
}

func (s *apiSuite) TestStateChangesForSnapName(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := newTestDaemon(c)
	st := d.overlord.State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	// Execute
	req, err := http.NewRequest("GET", "/v2/changes?for=funky-snap-name&select=all", nil)
	c.Assert(err, check.IsNil)
	rsp := getChanges(stateChangesCmd, req, nil).(*resp)

	// Verify
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Assert(rsp.Result, check.FitsTypeOf, []*changeInfo(nil))

	res := rsp.Result.([]*changeInfo)
	c.Assert(res, check.HasLen, 1)
	c.Check(res[0].Kind, check.Equals, `install`)

	_, err = rsp.MarshalJSON()
	c.Assert(err, check.IsNil)
}

func (s *apiSuite) TestStateChange(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := newTestDaemon(c)
	st := d.overlord.State()
	st.Lock()
	ids := setupChanges(st)
	chg := st.Change(ids[0])
	chg.Set("api-data", map[string]int{"n": 42})
	st.Unlock()
	s.vars = map[string]string{"id": ids[0]}

	// Execute
	req, err := http.NewRequest("POST", "/v2/change/"+ids[0], nil)
	c.Assert(err, check.IsNil)
	rsp := getChange(stateChangeCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Verify
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.NotNil)

	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body["result"], check.DeepEquals, map[string]interface{}{
		"id":         ids[0],
		"kind":       "install",
		"summary":    "install...",
		"status":     "Do",
		"ready":      false,
		"spawn-time": "2016-04-21T01:02:03Z",
		"tasks": []interface{}{
			map[string]interface{}{
				"id":         ids[2],
				"kind":       "download",
				"summary":    "1...",
				"status":     "Do",
				"log":        []interface{}{"2016-04-21T01:02:03Z INFO l11", "2016-04-21T01:02:03Z INFO l12"},
				"progress":   map[string]interface{}{"label": "", "done": 0., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
			},
			map[string]interface{}{
				"id":         ids[3],
				"kind":       "activate",
				"summary":    "2...",
				"status":     "Do",
				"progress":   map[string]interface{}{"label": "", "done": 0., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
			},
		},
		"data": map[string]interface{}{
			"n": float64(42),
		},
	})
}

func (s *apiSuite) TestStateChangeAbort(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	soon := 0
	ensureStateSoon = func(st *state.State) {
		soon++
	}

	// Setup
	d := newTestDaemon(c)
	st := d.overlord.State()
	st.Lock()
	ids := setupChanges(st)
	st.Unlock()
	s.vars = map[string]string{"id": ids[0]}

	buf := bytes.NewBufferString(`{"action": "abort"}`)

	// Execute
	req, err := http.NewRequest("POST", "/v2/changes/"+ids[0], buf)
	c.Assert(err, check.IsNil)
	rsp := abortChange(stateChangeCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Ensure scheduled
	c.Check(soon, check.Equals, 1)

	// Verify
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.NotNil)

	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body["result"], check.DeepEquals, map[string]interface{}{
		"id":         ids[0],
		"kind":       "install",
		"summary":    "install...",
		"status":     "Hold",
		"ready":      true,
		"spawn-time": "2016-04-21T01:02:03Z",
		"ready-time": "2016-04-21T01:02:03Z",
		"tasks": []interface{}{
			map[string]interface{}{
				"id":         ids[2],
				"kind":       "download",
				"summary":    "1...",
				"status":     "Hold",
				"log":        []interface{}{"2016-04-21T01:02:03Z INFO l11", "2016-04-21T01:02:03Z INFO l12"},
				"progress":   map[string]interface{}{"label": "", "done": 1., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
				"ready-time": "2016-04-21T01:02:03Z",
			},
			map[string]interface{}{
				"id":         ids[3],
				"kind":       "activate",
				"summary":    "2...",
				"status":     "Hold",
				"progress":   map[string]interface{}{"label": "", "done": 1., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
				"ready-time": "2016-04-21T01:02:03Z",
			},
		},
	})
}

func (s *apiSuite) TestStateChangeAbortIsReady(c *check.C) {
	restore := state.MockTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := newTestDaemon(c)
	st := d.overlord.State()
	st.Lock()
	ids := setupChanges(st)
	st.Change(ids[0]).SetStatus(state.DoneStatus)
	st.Unlock()
	s.vars = map[string]string{"id": ids[0]}

	buf := bytes.NewBufferString(`{"action": "abort"}`)

	// Execute
	req, err := http.NewRequest("POST", "/v2/changes/"+ids[0], buf)
	c.Assert(err, check.IsNil)
	rsp := abortChange(stateChangeCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Verify
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result, check.NotNil)

	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body["result"], check.DeepEquals, map[string]interface{}{
		"message": fmt.Sprintf("cannot abort change %s with nothing pending", ids[0]),
	})
}

const validBuyInput = `{
		  "snap-id": "the-snap-id-1234abcd",
		  "snap-name": "the snap name",
		  "price": 1.23,
		  "currency": "EUR"
		}`

var validBuyOptions = &store.BuyOptions{
	SnapID:   "the-snap-id-1234abcd",
	Price:    1.23,
	Currency: "EUR",
}

var buyTests = []struct {
	input                string
	result               *store.BuyResult
	err                  error
	expectedStatus       int
	expectedResult       interface{}
	expectedResponseType ResponseType
	expectedBuyOptions   *store.BuyOptions
}{
	{
		// Success
		input: validBuyInput,
		result: &store.BuyResult{
			State: "Complete",
		},
		expectedStatus: http.StatusOK,
		expectedResult: &store.BuyResult{
			State: "Complete",
		},
		expectedResponseType: ResponseTypeSync,
		expectedBuyOptions:   validBuyOptions,
	},
	{
		// Fail with internal error
		input: `{
		  "snap-id": "the-snap-id-1234abcd",
		  "price": 1.23,
		  "currency": "EUR"
		}`,
		err:                  fmt.Errorf("internal error banana"),
		expectedStatus:       http.StatusInternalServerError,
		expectedResponseType: ResponseTypeError,
		expectedResult: &errorResult{
			Message: "internal error banana",
		},
		expectedBuyOptions: &store.BuyOptions{
			SnapID:   "the-snap-id-1234abcd",
			Price:    1.23,
			Currency: "EUR",
		},
	},
	{
		// Fail with unauthenticated error
		input:                validBuyInput,
		err:                  store.ErrUnauthenticated,
		expectedStatus:       http.StatusBadRequest,
		expectedResponseType: ResponseTypeError,
		expectedResult: &errorResult{
			Message: "you need to log in first",
			Kind:    "login-required",
		},
		expectedBuyOptions: validBuyOptions,
	},
	{
		// Fail with TOS not accepted
		input:                validBuyInput,
		err:                  store.ErrTOSNotAccepted,
		expectedStatus:       http.StatusBadRequest,
		expectedResponseType: ResponseTypeError,
		expectedResult: &errorResult{
			Message: "terms of service not accepted",
			Kind:    "terms-not-accepted",
		},
		expectedBuyOptions: validBuyOptions,
	},
	{
		// Fail with no payment methods
		input:                validBuyInput,
		err:                  store.ErrNoPaymentMethods,
		expectedStatus:       http.StatusBadRequest,
		expectedResponseType: ResponseTypeError,
		expectedResult: &errorResult{
			Message: "no payment methods",
			Kind:    "no-payment-methods",
		},
		expectedBuyOptions: validBuyOptions,
	},
	{
		// Fail with payment declined
		input:                validBuyInput,
		err:                  store.ErrPaymentDeclined,
		expectedStatus:       http.StatusBadRequest,
		expectedResponseType: ResponseTypeError,
		expectedResult: &errorResult{
			Message: "payment declined",
			Kind:    "payment-declined",
		},
		expectedBuyOptions: validBuyOptions,
	},
}

func (s *apiSuite) TestBuySnap(c *check.C) {
	for _, test := range buyTests {
		s.buyResult = test.result
		s.err = test.err

		buf := bytes.NewBufferString(test.input)
		req, err := http.NewRequest("POST", "/v2/buy", buf)
		c.Assert(err, check.IsNil)

		state := snapCmd.d.overlord.State()
		state.Lock()
		user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
		state.Unlock()
		c.Check(err, check.IsNil)

		rsp := postBuy(buyCmd, req, user).(*resp)

		c.Check(rsp.Status, check.Equals, test.expectedStatus)
		c.Check(rsp.Type, check.Equals, test.expectedResponseType)
		c.Assert(rsp.Result, check.FitsTypeOf, test.expectedResult)
		c.Check(rsp.Result, check.DeepEquals, test.expectedResult)

		c.Check(s.buyOptions, check.DeepEquals, test.expectedBuyOptions)
		c.Check(s.user, check.Equals, user)
	}
}

func (s *apiSuite) TestIsTrue(c *check.C) {
	form := &multipart.Form{}
	c.Check(isTrue(form, "foo"), check.Equals, false)
	for _, f := range []string{"", "false", "0", "False", "f", "try"} {
		form.Value = map[string][]string{"foo": {f}}
		c.Check(isTrue(form, "foo"), check.Equals, false, check.Commentf("expected %q to be false", f))
	}
	for _, t := range []string{"true", "1", "True", "t"} {
		form.Value = map[string][]string{"foo": {t}}
		c.Check(isTrue(form, "foo"), check.Equals, true, check.Commentf("expected %q to be true", t))
	}
}

var readyToBuyTests = []struct {
	input    error
	status   int
	respType interface{}
	response interface{}
}{
	{
		// Success
		input:    nil,
		status:   http.StatusOK,
		respType: ResponseTypeSync,
		response: true,
	},
	{
		// Not accepted TOS
		input:    store.ErrTOSNotAccepted,
		status:   http.StatusBadRequest,
		respType: ResponseTypeError,
		response: &errorResult{
			Message: "terms of service not accepted",
			Kind:    errorKindTermsNotAccepted,
		},
	},
	{
		// No payment methods
		input:    store.ErrNoPaymentMethods,
		status:   http.StatusBadRequest,
		respType: ResponseTypeError,
		response: &errorResult{
			Message: "no payment methods",
			Kind:    errorKindNoPaymentMethods,
		},
	},
}

func (s *apiSuite) TestReadyToBuy(c *check.C) {
	for _, test := range readyToBuyTests {
		s.err = test.input

		req, err := http.NewRequest("GET", "/v2/buy/ready", nil)
		c.Assert(err, check.IsNil)

		state := snapCmd.d.overlord.State()
		state.Lock()
		user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
		state.Unlock()
		c.Check(err, check.IsNil)

		rsp := readyToBuy(readyToBuyCmd, req, user).(*resp)
		c.Check(rsp.Status, check.Equals, test.status)
		c.Check(rsp.Type, check.Equals, test.respType)
		c.Assert(rsp.Result, check.FitsTypeOf, test.response)
		c.Check(rsp.Result, check.DeepEquals, test.response)
	}
}

var _ = check.Suite(&postCreateUserSuite{})

type postCreateUserSuite struct {
	apiBaseSuite

	mockUserHome string
}

func (s *postCreateUserSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.daemon(c)
	postCreateUserUcrednetGetUID = func(string) (uint32, error) {
		return 0, nil
	}
	s.mockUserHome = c.MkDir()
	userLookup = mkUserLookup(s.mockUserHome)
}

func (s *postCreateUserSuite) TearDownTest(c *check.C) {
	s.apiBaseSuite.TearDownTest(c)

	postCreateUserUcrednetGetUID = ucrednetGetUID
	userLookup = user.Lookup
	osutilAddUser = osutil.AddUser
	storeUserInfo = store.UserInfo
}

func mkUserLookup(userHomeDir string) func(string) (*user.User, error) {
	return func(username string) (*user.User, error) {
		cur, err := user.Current()
		cur.Username = username
		cur.HomeDir = userHomeDir
		return cur, err
	}
}

func (s *postCreateUserSuite) TestPostCreateUserNoSSHKeys(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	storeUserInfo = func(user string) (*store.User, error) {
		c.Check(user, check.Equals, "popper@lse.ac.uk")
		return &store.User{
			Username:         "karl",
			OpenIDIdentifier: "xxyyzz",
		}, nil
	}

	buf := bytes.NewBufferString(`{"email": "popper@lse.ac.uk"}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, `cannot create user for "popper@lse.ac.uk": no ssh keys found`)
}

func (s *postCreateUserSuite) TestPostCreateUser(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	storeUserInfo = func(user string) (*store.User, error) {
		c.Check(user, check.Equals, "popper@lse.ac.uk")
		return &store.User{
			Username:         "karl",
			SSHKeys:          []string{"ssh1", "ssh2"},
			OpenIDIdentifier: "xxyyzz",
		}, nil
	}
	osutilAddUser = func(username string, opts *osutil.AddUserOptions) error {
		c.Check(username, check.Equals, "karl")
		c.Check(opts.SSHKeys, check.DeepEquals, []string{"ssh1", "ssh2"})
		c.Check(opts.Gecos, check.Equals, "popper@lse.ac.uk,xxyyzz")
		c.Check(opts.Sudoer, check.Equals, false)
		return nil
	}

	buf := bytes.NewBufferString(`{"email": "popper@lse.ac.uk"}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	expected := &userResponseData{
		Username: "karl",
		SSHKeys:  []string{"ssh1", "ssh2"},
	}

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	// user was setup in state
	state := s.d.overlord.State()
	state.Lock()
	user, err := auth.User(state, 1)
	state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(user.Username, check.Equals, "karl")
	c.Check(user.Email, check.Equals, "popper@lse.ac.uk")
	c.Check(user.Macaroon, check.NotNil)
	// auth saved to user home dir
	outfile := filepath.Join(s.mockUserHome, ".snap", "auth.json")
	c.Check(osutil.FileExists(outfile), check.Equals, true)
	content, err := ioutil.ReadFile(outfile)
	c.Check(err, check.IsNil)
	c.Check(string(content), check.Equals, fmt.Sprintf(`{"macaroon":"%s"}`, user.Macaroon))
}

func (s *postCreateUserSuite) TestGetUserDetailsFromAssertionModelNotFound(c *check.C) {
	st := s.d.overlord.State()
	email := "foo@example.com"

	username, opts, err := getUserDetailsFromAssertion(st, email)
	c.Check(username, check.Equals, "")
	c.Check(opts, check.IsNil)
	c.Check(err, check.ErrorMatches, `cannot add system-user "foo@example.com": cannot get model assertion: no state entry for key`)
}

func (s *postCreateUserSuite) setupSigner(accountID string, signerPrivKey asserts.PrivateKey) *assertstest.SigningDB {
	st := s.d.overlord.State()

	// create fake brand signature
	signerSigning := assertstest.NewSigningDB(accountID, signerPrivKey)

	signerAcct := assertstest.NewAccount(s.storeSigning, accountID, map[string]interface{}{
		"account-id":   accountID,
		"verification": "certified",
	}, "")
	s.storeSigning.Add(signerAcct)
	assertAdd(st, signerAcct)

	signerAccKey := assertstest.NewAccountKey(s.storeSigning, signerAcct, nil, signerPrivKey.PublicKey(), "")
	s.storeSigning.Add(signerAccKey)
	assertAdd(st, signerAccKey)

	return signerSigning
}

var (
	brandPrivKey, _   = assertstest.GenerateKey(752)
	partnerPrivKey, _ = assertstest.GenerateKey(752)
	unknownPrivKey, _ = assertstest.GenerateKey(752)
)

func (s *postCreateUserSuite) makeSystemUsers(c *check.C, systemUsers []map[string]interface{}) {
	st := s.d.overlord.State()

	assertAdd(st, s.storeSigning.StoreAccountKey(""))

	brandSigning := s.setupSigner("my-brand", brandPrivKey)
	partnerSigning := s.setupSigner("partner", partnerPrivKey)
	unknownSigning := s.setupSigner("unknown", unknownPrivKey)

	signers := map[string]*assertstest.SigningDB{
		"my-brand": brandSigning,
		"partner":  partnerSigning,
		"unknown":  unknownSigning,
	}

	model, err := brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":                "16",
		"authority-id":          "my-brand",
		"brand-id":              "my-brand",
		"model":                 "my-model",
		"architecture":          "amd64",
		"gadget":                "pc",
		"kernel":                "pc-kernel",
		"required-snaps":        []interface{}{"required-snap1"},
		"system-user-authority": []interface{}{"my-brand", "partner"},
		"timestamp":             time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)
	model = model.(*asserts.Model)

	// now add model related stuff to the system
	assertAdd(st, model)

	for _, suMap := range systemUsers {
		su, err := signers[suMap["authority-id"].(string)].Sign(asserts.SystemUserType, suMap, nil, "")
		c.Assert(err, check.IsNil)
		su = su.(*asserts.SystemUser)
		// now add system-user assertion to the system
		assertAdd(st, su)
	}
	// create fake device
	st.Lock()
	err = auth.SetDevice(st, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
	})
	st.Unlock()
	c.Assert(err, check.IsNil)
}

var goodUser = map[string]interface{}{
	"authority-id": "my-brand",
	"brand-id":     "my-brand",
	"email":        "foo@bar.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model", "other-model"},
	"name":         "Boring Guy",
	"username":     "guy",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

var partnerUser = map[string]interface{}{
	"authority-id": "partner",
	"brand-id":     "my-brand",
	"email":        "p@partner.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model"},
	"name":         "Partner Guy",
	"username":     "partnerguy",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

var badUser = map[string]interface{}{
	// bad user (not valid for this model)
	"authority-id": "my-brand",
	"brand-id":     "my-brand",
	"email":        "foobar@bar.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"non-of-the-models-i-have"},
	"name":         "Random Gal",
	"username":     "gal",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

var unknownUser = map[string]interface{}{
	"authority-id": "unknown",
	"brand-id":     "my-brand",
	"email":        "x@partner.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model"},
	"name":         "XGuy",
	"username":     "xguy",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

func (s *postCreateUserSuite) TestGetUserDetailsFromAssertionHappy(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	// ensure that if we query the details from the assert DB we get
	// the expected user
	st := s.d.overlord.State()
	username, opts, err := getUserDetailsFromAssertion(st, "foo@bar.com")
	c.Check(username, check.Equals, "guy")
	c.Check(opts, check.DeepEquals, &osutil.AddUserOptions{
		Gecos:    "foo@bar.com,Boring Guy",
		Password: "$6$salt$hash",
	})
	c.Check(err, check.IsNil)
}

// FIXME: These tests all look similar, with small deltas. Would be
// nice to transform them into a table that is just the deltas, and
// run on a loop.
func (s *postCreateUserSuite) TestPostCreateUserFromAssertion(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	// mock the calls that create the user
	osutilAddUser = func(username string, opts *osutil.AddUserOptions) error {
		c.Check(username, check.Equals, "guy")
		c.Check(opts.Gecos, check.Equals, "foo@bar.com,Boring Guy")
		c.Check(opts.Sudoer, check.Equals, false)
		c.Check(opts.Password, check.Equals, "$6$salt$hash")
		return nil
	}

	defer func() {
		osutilAddUser = osutil.AddUser
	}()

	// do it!
	buf := bytes.NewBufferString(`{"email": "foo@bar.com","known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	expected := &userResponseData{
		Username: "guy",
	}

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	// ensure the user was added to the state
	st := s.d.overlord.State()
	st.Lock()
	users, err := auth.Users(st)
	c.Assert(err, check.IsNil)
	st.Unlock()
	c.Check(users, check.HasLen, 1)
}

func (s *postCreateUserSuite) TestPostCreateUserFromAssertionAllKnown(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.makeSystemUsers(c, []map[string]interface{}{goodUser, partnerUser, badUser, unknownUser})

	// mock the calls that create the user
	osutilAddUser = func(username string, opts *osutil.AddUserOptions) error {
		switch username {
		case "guy":
			c.Check(opts.Gecos, check.Equals, "foo@bar.com,Boring Guy")
		case "partnerguy":
			c.Check(opts.Gecos, check.Equals, "p@partner.com,Partner Guy")
		default:
			c.Logf("unexpected username %q", username)
			c.Fail()
		}
		c.Check(opts.Sudoer, check.Equals, false)
		c.Check(opts.Password, check.Equals, "$6$salt$hash")
		return nil
	}
	defer func() {
		osutilAddUser = osutil.AddUser
	}()

	// do it!
	buf := bytes.NewBufferString(`{"known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	// note that we get a list here instead of a single
	// userResponseData item
	c.Check(rsp.Result, check.FitsTypeOf, []userResponseData{})
	seen := map[string]bool{}
	for _, u := range rsp.Result.([]userResponseData) {
		seen[u.Username] = true
		c.Check(u, check.DeepEquals, userResponseData{Username: u.Username})
	}
	c.Check(seen, check.DeepEquals, map[string]bool{
		"guy":        true,
		"partnerguy": true,
	})

	// ensure the user was added to the state
	st := s.d.overlord.State()
	st.Lock()
	users, err := auth.Users(st)
	c.Assert(err, check.IsNil)
	st.Unlock()
	c.Check(users, check.HasLen, 2)
}

func (s *postCreateUserSuite) TestPostCreateUserFromAssertionAllKnownClassicErrors(c *check.C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	postCreateUserUcrednetGetUID = func(string) (uint32, error) {
		return 0, nil
	}
	defer func() {
		postCreateUserUcrednetGetUID = ucrednetGetUID
	}()

	// do it!
	buf := bytes.NewBufferString(`{"known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, `cannot create user: device is a classic system`)
}

func (s *postCreateUserSuite) TestPostCreateUserFromAssertionAllKnownButOwnedErrors(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	st := s.d.overlord.State()
	st.Lock()
	_, err := auth.NewUser(st, "username", "email@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Check(err, check.IsNil)

	// do it!
	buf := bytes.NewBufferString(`{"known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, `cannot create user: device already managed`)
}

func (s *postCreateUserSuite) TestPostCreateUserFromAssertionAllKnownButOwned(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	st := s.d.overlord.State()
	st.Lock()
	_, err := auth.NewUser(st, "username", "email@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Check(err, check.IsNil)

	// mock the calls that create the user
	osutilAddUser = func(username string, opts *osutil.AddUserOptions) error {
		c.Check(username, check.Equals, "guy")
		c.Check(opts.Gecos, check.Equals, "foo@bar.com,Boring Guy")
		c.Check(opts.Sudoer, check.Equals, false)
		c.Check(opts.Password, check.Equals, "$6$salt$hash")
		return nil
	}
	defer func() {
		osutilAddUser = osutil.AddUser
	}()

	// do it!
	buf := bytes.NewBufferString(`{"known":true,"force-managed":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	// note that we get a list here instead of a single
	// userResponseData item
	expected := []userResponseData{
		{Username: "guy"},
	}
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *postCreateUserSuite) TestUsersEmpty(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/users", nil)
	c.Assert(err, check.IsNil)

	rsp := getUsers(usersCmd, req, nil).(*resp)

	expected := []userResponseData{}
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *postCreateUserSuite) TestUsersHasUser(c *check.C) {
	st := s.d.overlord.State()
	st.Lock()
	u, err := auth.NewUser(st, "someuser", "mymail@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/users", nil)
	c.Assert(err, check.IsNil)

	rsp := getUsers(usersCmd, req, nil).(*resp)

	expected := []userResponseData{
		{ID: u.ID, Username: u.Username, Email: u.Email},
	}
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *postCreateUserSuite) TestSysinfoIsManaged(c *check.C) {
	st := s.d.overlord.State()
	st.Lock()
	_, err := auth.NewUser(st, "someuser", "mymail@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/system-info", nil)
	c.Assert(err, check.IsNil)

	rsp := sysInfo(sysInfoCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result.(map[string]interface{})["managed"], check.Equals, true)
}

// aliases

func (s *apiSuite) TestAliasSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.mockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) ([]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &aliasAction{
		Action:  "alias",
		Snap:    "alias-snap",
		Aliases: []string{"alias1"},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	aliasesCmd.POST(aliasesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.IsNil)

	// sanity check
	c.Check(osutil.IsSymlink(filepath.Join(dirs.SnapBinariesDir, "alias1")), check.Equals, true)
}

func (s *apiSuite) TestAliasErrors(c *check.C) {
	s.daemon(c)

	errScenarios := []struct {
		mangle func(*aliasAction)
		err    string
	}{
		{func(a *aliasAction) { a.Action = "" }, `unsupported alias action: ""`},
		{func(a *aliasAction) { a.Action = "what" }, `unsupported alias action: "what"`},
		{func(a *aliasAction) { a.Aliases = nil }, `at least one alias name is required`},
		{func(a *aliasAction) { a.Snap = "lalala" }, `cannot find snap "lalala"`},
	}

	for _, scen := range errScenarios {
		action := &aliasAction{
			Action:  "alias",
			Snap:    "alias-snap",
			Aliases: []string{"alias1"},
		}
		scen.mangle(action)

		text, err := json.Marshal(action)
		c.Assert(err, check.IsNil)
		buf := bytes.NewBuffer(text)
		req, err := http.NewRequest("POST", "/v2/aliases", buf)
		c.Assert(err, check.IsNil)

		rsp := changeAliases(aliasesCmd, req, nil).(*resp)
		c.Check(rsp.Type, check.Equals, ResponseTypeError)
		c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
		c.Check(rsp.Result.(*errorResult).Message, check.Matches, scen.err)
	}
}

func (s *apiSuite) TestUnaliasSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.mockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) ([]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.overlord.Loop()
	defer d.overlord.Stop()

	action := &aliasAction{
		Action:  "unalias",
		Snap:    "alias-snap",
		Aliases: []string{"alias1"},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	aliasesCmd.POST(aliasesCmd, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	defer st.Unlock()
	err = chg.Err()
	c.Assert(err, check.IsNil)

	var allAliases map[string]map[string]string
	err = st.Get("aliases", &allAliases)
	c.Assert(err, check.IsNil)
	c.Check(allAliases, check.DeepEquals, map[string]map[string]string{
		"alias-snap": {"alias1": "disabled"},
	})

}

func (s *apiSuite) TestResetAliasSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.mockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) ([]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.overlord.Loop()
	defer d.overlord.Stop()

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()

	st.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "disabled",
		},
	})

	action := &aliasAction{
		Action:  "reset",
		Snap:    "alias-snap",
		Aliases: []string{"alias1"},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	st.Unlock()
	aliasesCmd.POST(aliasesCmd, req, nil).ServeHTTP(rec, req)
	st.Lock()
	c.Check(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	chg := st.Change(id)
	c.Assert(chg, check.NotNil)

	st.Unlock()
	<-chg.Ready()
	st.Lock()

	err = chg.Err()
	c.Assert(err, check.IsNil)

	var allAliases map[string]map[string]string
	err = st.Get("aliases", &allAliases)
	c.Assert(err, check.IsNil)
	c.Check(allAliases, check.HasLen, 0)
}

func (s *apiSuite) TestAliases(c *check.C) {
	d := s.daemon(c)

	s.mockSnap(c, aliasYaml)

	st := d.overlord.State()
	st.Lock()
	st.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "enabled",
			"alias3": "disabled", // gone from the current revision of the snap
		},
	})
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/aliases", nil)
	c.Assert(err, check.IsNil)

	rsp := getAliases(aliasesCmd, req, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Result, check.DeepEquals, map[string]map[string]aliasStatus{
		"alias-snap": {
			"alias1": {App: "alias-snap.app", Status: "enabled"},
			"alias2": {App: "alias-snap.app2"},
			"alias3": {Status: "disabled"},
		},
	})

}
