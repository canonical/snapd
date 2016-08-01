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
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/check.v1"
	"gopkg.in/macaroon.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
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

type apiSuite struct {
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
	paymentMethods    *store.PaymentInformation
	storeSigning      *assertstest.StoreStack
	restoreRelease    func()
}

var _ = check.Suite(&apiSuite{})

func (s *apiSuite) Snap(name, channel string, devmode bool, user *auth.UserState) (*snap.Info, error) {
	s.user = user
	if len(s.rsnaps) > 0 {
		return s.rsnaps[0], s.err
	}
	return nil, s.err
}

func (s *apiSuite) Find(search *store.Search, user *auth.UserState) ([]*snap.Info, error) {
	s.storeSearch = *search
	s.user = user

	return s.rsnaps, s.err
}

func (s *apiSuite) ListRefresh(snaps []*store.RefreshCandidate, user *auth.UserState) ([]*snap.Info, error) {
	s.refreshCandidates = snaps
	s.user = user

	return s.rsnaps, s.err
}

func (s *apiSuite) SuggestedCurrency() string {
	return s.suggestedCurrency
}

func (s *apiSuite) Download(string, *snap.DownloadInfo, progress.Meter, *auth.UserState) (string, error) {
	panic("Download not expected to be called")
}

func (s *apiSuite) Buy(options *store.BuyOptions) (*store.BuyResult, error) {
	s.buyOptions = options
	return s.buyResult, s.err
}

func (s *apiSuite) PaymentMethods(user *auth.UserState) (*store.PaymentInformation, error) {
	s.user = user
	return s.paymentMethods, s.err
}

func (s *apiSuite) muxVars(*http.Request) map[string]string {
	return s.vars
}

func (s *apiSuite) SetUpSuite(c *check.C) {
	muxVars = s.muxVars
	s.restoreRelease = release.MockReleaseInfo(&release.OS{
		ID:        "ubuntu",
		VersionID: "mocked",
	})
}

func (s *apiSuite) TearDownSuite(c *check.C) {
	muxVars = nil
	s.restoreRelease()
}

func (s *apiSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, check.IsNil)
	c.Assert(os.MkdirAll(dirs.SnapSnapsDir, 0755), check.IsNil)

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
	s.paymentMethods = nil
	rootPrivKey, _ := assertstest.GenerateKey(1024)
	storePrivKey, _ := assertstest.GenerateKey(752)
	s.storeSigning = assertstest.NewStoreStack("can0nical", rootPrivKey, storePrivKey)
}

func (s *apiSuite) TearDownTest(c *check.C) {
	s.d = nil
	s.restoreBackends()
	snapstateInstall = snapstate.Install
	snapstateGet = snapstate.Get
	snapstateInstallPath = snapstate.InstallPath
	readSnapInfo = readSnapInfoImpl
	ensureStateSoon = ensureStateSoonImpl
	dirs.SetRootDir("")
}

func (s *apiSuite) daemon(c *check.C) *Daemon {
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

	s.d = d
	return d
}

func (s *apiSuite) mkInstalled(c *check.C, name, developer, version string, revno snap.Revision, active bool, extraYaml string) *snap.Info {
	return s.mkInstalledInState(c, nil, name, developer, version, revno, active, extraYaml)
}

func (s *apiSuite) mkInstalledInState(c *check.C, daemon *Daemon, name, developer, version string, revno snap.Revision, active bool, extraYaml string) *snap.Info {
	// Collect arguments into a snap.SideInfo structure
	sideInfo := &snap.SideInfo{
		SnapID:    "funky-snap-id",
		RealName:  name,
		Developer: developer,
		Revision:  revno,
		Channel:   "stable",
	}

	// Collect other arguments into a yaml string
	yamlText := fmt.Sprintf(`
name: %s
version: %s
%s`, name, version, extraYaml)

	// Mock the snap on disk
	snapInfo := snaptest.MockSnap(c, yamlText, sideInfo)

	c.Assert(os.MkdirAll(snapInfo.DataDir(), 0755), check.IsNil)
	metadir := filepath.Join(snapInfo.MountDir(), "meta")
	guidir := filepath.Join(metadir, "gui")
	c.Assert(os.MkdirAll(guidir, 0755), check.IsNil)
	c.Check(ioutil.WriteFile(filepath.Join(guidir, "icon.svg"), []byte("yadda icon"), 0644), check.IsNil)

	if daemon != nil {
		st := daemon.overlord.State()
		st.Lock()
		defer st.Unlock()

		var snapst snapstate.SnapState
		snapstate.Get(st, name, &snapst)
		snapst.Active = active
		snapst.Sequence = append(snapst.Sequence, &snapInfo.SideInfo)
		snapst.Current = snapInfo.SideInfo.Revision

		snapstate.Set(st, name, &snapst)
	}

	return snapInfo
}

func (s *apiSuite) mkGadget(c *check.C, store string) {
	yamlText := fmt.Sprintf(`name: test
version: 1
type: gadget
gadget: {store: {id: %q}}
`, store)
	snaptest.MockSnap(c, yamlText, &snap.SideInfo{Revision: snap.R(1)})
	c.Assert(os.Symlink("1", filepath.Join(dirs.SnapSnapsDir, "test", "current")), check.IsNil)
}

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
			"id":          "funky-snap-id",
			"name":        "foo",
			"revision":    snap.R(10),
			"version":     "v1",
			"channel":     "stable",
			"summary":     "summary",
			"description": "description",
			"developer":   "bar",
			"status":      "active",
			"icon":        "/v2/icons/foo/icon",
			"type":        string(snap.TypeApp),
			"resource":    "/v2/snaps/foo",
			"private":     false,
			"devmode":     false,
			"confinement": snap.StrictConfinement,
			"trymode":     false,
			"apps":        []appJSON{},
			"broken":      "",
		},
		Meta: meta,
	}

	c.Check(rsp.Result, check.DeepEquals, expected.Result)
}

func (s *apiSuite) TestSnapInfoWithAuth(c *check.C) {
	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "macaroon", []string{"discharge"})
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
		"api",
		"maxReadBuflen",
		"muxVars",
		"errNothingToInstall",
		"errModeConflict",
		"errNoJailMode",
		// snapInstruction vars:
		"snapInstructionDispTable",
		"snapstateInstall",
		"snapstateUpdate",
		"snapstateInstallPath",
		"snapstateTryPath",
		"snapstateGet",
		"readSnapInfo",
		"osutilAddExtraUser",
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
	}
	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
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

	return store.MacaroonSerialize(m)
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
	serializedMacaroon, err := s.makeStoreMacaroon()
	c.Assert(err, check.IsNil)
	responseData, err := s.makeStoreMacaroonResponse(serializedMacaroon)
	c.Assert(err, check.IsNil)
	mockMyAppsServer := s.makeMyAppsServer(200, responseData)
	defer mockMyAppsServer.Close()

	discharge := `{"discharge_macaroon": "the-discharge-macaroon-serialized-data"}`
	mockSSOServer := s.makeSSOServer(200, discharge)
	defer mockSSOServer.Close()

	buf := bytes.NewBufferString(`{"username": "username", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	expected := loginResponseData{
		Macaroon:   serializedMacaroon,
		Discharges: []string{"the-discharge-macaroon-serialized-data"},
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	expectedUser := auth.UserState{
		ID:         1,
		Username:   "username",
		Macaroon:   serializedMacaroon,
		Discharges: []string{"the-discharge-macaroon-serialized-data"},
	}
	expectedUser.StoreMacaroon = expectedUser.Macaroon
	expectedUser.StoreDischarges = expectedUser.Discharges

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.User(state, 1)
	state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(*user, check.DeepEquals, expectedUser)
}

func (s *apiSuite) TestLogoutUser(c *check.C) {
	d := s.daemon(c)
	state := d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "macaroon", []string{"discharge"})
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

	buf := bytes.NewBufferString(`{"username": "username", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusInternalServerError)
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

	buf := bytes.NewBufferString(`{"username": "username", "password": "password"}`)
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

	buf := bytes.NewBufferString(`{"username": "username", "password": "password"}`)
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

	buf := bytes.NewBufferString(`{"username": "username", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusUnauthorized)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "cannot authenticate on snap store")
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
	req.Header.Set("Authorization", `Macaroon root="macaroon"`)

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
	expectedUser, err := auth.NewUser(state, "username", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", `Macaroon root="macaroon", discharge="discharge"`)

	state.Lock()
	user, err := UserFromRequest(state, req)
	state.Unlock()

	c.Check(err, check.IsNil)
	c.Check(user, check.DeepEquals, expectedUser)
}

func (s *apiSuite) TestUserFromRequestHeaderValidUserMultipleDischarges(c *check.C) {
	state := snapCmd.d.overlord.State()
	state.Lock()
	expectedUser, err := auth.NewUser(state, "username", "macaroon", []string{"discharge2", "discharge1"})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, _ := http.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Authorization", `Macaroon root="macaroon", discharge="discharge1", discharge="discharge2"`)

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
			RealName:  "store",
			Developer: "foo",
		},
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

func (s *apiSuite) TestFind(c *check.C) {
	s.suggestedCurrency = "EUR"

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName:  "store",
			Developer: "foo",
		},
	}}

	req, err := http.NewRequest("GET", "/v2/find?q=hi", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req, nil).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "store")
	c.Check(snaps[0]["prices"], check.IsNil)

	c.Check(rsp.SuggestedCurrency, check.Equals, "EUR")

	c.Check(s.storeSearch, check.DeepEquals, store.Search{Query: "hi"})
	c.Check(s.refreshCandidates, check.HasLen, 0)
}

func (s *apiSuite) TestFindRefreshes(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName:  "store",
			Developer: "foo",
		},
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

func (s *apiSuite) TestFindOne(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName:  "store",
			Developer: "foo",
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
			RealName:  "banana",
			Developer: "foo",
		},
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

func (s *apiSuite) TestSnapsInfoOnlyStore(c *check.C) {
	d := s.daemon(c)

	s.suggestedCurrency = "EUR"

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName:  "store",
			Developer: "foo",
		},
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
			Confinement: snap.DevmodeConfinement,
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
		{"baz", string(snap.DevmodeConfinement)},
	} {
		name, mode := ss[0], ss[1]
		c.Check(snaps[i]["name"], check.Equals, name, check.Commentf(name))
		c.Check(snaps[i]["confinement"], check.Equals, mode, check.Commentf(name))
	}
}

func (s *apiSuite) TestSnapsInfoStoreWithAuth(c *check.C) {
	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "macaroon", []string{"discharge"})
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
			RealName:  "remote",
			Developer: "foo",
		},
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
			RealName:  "remote",
			Developer: "foo",
		},
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
			RealName:  "remote",
			Developer: "foo",
		},
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
	user, err := auth.NewUser(state, "username", "macaroon", []string{"discharge"})
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
	inst := &snapInstruction{}

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

var sideLoadBodyWithoutDevMode = "" +
	"----hello--\r\n" +
	"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
	"\r\n" +
	"xyzzy\r\n" +
	"----hello--\r\n" +
	"Content-Disposition: form-data; name=\"snap-path\"\r\n" +
	"\r\n" +
	"a/b/local.snap\r\n" +
	"----hello--\r\n"

func (s *apiSuite) TestSideloadSnapOnNonDevModeDistro(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	chgSummary := s.sideloadCheck(c, body, head, 0, false)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "a/b/local.snap"`)
}

func (s *apiSuite) TestSideloadSnapOnDevModeDistro(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	restore := release.MockReleaseInfo(&release.OS{ID: "x-devmode-distro"})
	defer restore()
	chgSummary := s.sideloadCheck(c, body, head, snapstate.DevMode, false)
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
	restore := release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()
	chgSummary := s.sideloadCheck(c, body, head, snapstate.DevMode, true)
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
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	// try a multipart/form-data upload
	chgSummary := s.sideloadCheck(c, body, head, snapstate.JailMode, true)
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
	d := newTestDaemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	rsp := sideloadSnap(snapsCmd, req, nil).(*resp)
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
	d := newTestDaemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	restore := release.MockReleaseInfo(&release.OS{ID: "x-devmode-distro"})
	defer restore()

	rsp := sideloadSnap(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Equals, "this system cannot honour the jailmode flag")
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

	rsp := sideloadSnap(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Assert(rsp.Result.(*errorResult).Message, check.Matches, `cannot find "snap" file field in provided multipart/form-data payload`)
}

func (s *apiSuite) TestTrySnap(c *check.C) {
	d := newTestDaemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

	soon := 0
	ensureStateSoon = func(st *state.State) {
		soon++
		ensureStateSoonImpl(st)
	}

	req, err := http.NewRequest("POST", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	// mock a try dir
	tryDir := c.MkDir()
	snapYaml := filepath.Join(tryDir, "meta", "snap.yaml")
	err = os.MkdirAll(filepath.Dir(snapYaml), 0755)
	c.Assert(err, check.IsNil)
	err = ioutil.WriteFile(snapYaml, []byte("name: foo\nversion: 1.0\n"), 0644)
	c.Assert(err, check.IsNil)

	tryWasCalled := true
	snapstateTryPath = func(s *state.State, name, path string, flags snapstate.Flags) (*state.TaskSet, error) {
		tryWasCalled = true
		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	// try the snap
	rsp := trySnap(snapsCmd, req, nil, tryDir, 0).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeAsync)
	c.Assert(tryWasCalled, check.Equals, true)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	c.Assert(chg.Tasks(), check.HasLen, 1)

	st.Unlock()
	<-chg.Ready()
	st.Lock()

	c.Check(chg.Kind(), check.Equals, "try-snap")
	c.Check(chg.Summary(), check.Equals, fmt.Sprintf(`Try "%s" snap from %q`, "foo", tryDir))
	var names []string
	err = chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{"foo"})
	var apiData map[string]interface{}
	err = chg.Get("api-data", &apiData)
	c.Assert(err, check.IsNil)
	c.Check(apiData, check.DeepEquals, map[string]interface{}{
		"snap-name": "foo",
	})

	c.Check(soon, check.Equals, 1)
}

func (s *apiSuite) TestTrySnapRelative(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := trySnap(snapsCmd, req, nil, "relative-path", 0).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "need an absolute path")
}

func (s *apiSuite) TestTrySnapNotDir(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := trySnap(snapsCmd, req, nil, "/does/not/exist", 0).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "not a snap directory")
}

func (s *apiSuite) sideloadCheck(c *check.C, content string, head map[string]string, expectedFlags snapstate.Flags, hasUbuntuCore bool) string {
	d := newTestDaemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

	soon := 0
	ensureStateSoon = func(st *state.State) {
		soon++
		ensureStateSoonImpl(st)
	}

	// setup done
	installQueue := []string{}
	readSnapInfo = func(path string) (*snap.Info, error) {
		return &snap.Info{SuggestedName: "local"}, nil
	}

	snapstateGet = func(s *state.State, name string, snapst *snapstate.SnapState) error {
		if hasUbuntuCore {
			return nil
		}
		// pretend we do not have a state for ubuntu-core
		return state.ErrNoState
	}
	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		// NOTE: ubuntu-core is not installed in developer mode
		c.Check(flags, check.Equals, snapstate.Flags(0))
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	snapstateInstallPath = func(s *state.State, name, path, channel string, flags snapstate.Flags) (*state.TaskSet, error) {
		c.Check(flags, check.Equals, expectedFlags)

		bs, err := ioutil.ReadFile(path)
		c.Check(err, check.IsNil)
		c.Check(string(bs), check.Equals, "xyzzy")

		installQueue = append(installQueue, name+"::"+path)
		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	buf := bytes.NewBufferString(content)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	for k, v := range head {
		req.Header.Set(k, v)
	}

	rsp := sideloadSnap(snapsCmd, req, nil).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeAsync)
	n := 1
	if !hasUbuntuCore {
		n++
	}
	c.Assert(installQueue, check.HasLen, n)
	if !hasUbuntuCore {
		c.Check(installQueue[0], check.Equals, "ubuntu-core")
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

func (s *apiSuite) TestInstallOnNonDevModeDistro(c *check.C) {
	s.testInstall(c, &release.OS{ID: "ubuntu"}, snapstate.Flags(0))
}
func (s *apiSuite) TestInstallOnDevModeDistro(c *check.C) {
	s.testInstall(c, &release.OS{ID: "x-devmode-distro"}, snapstate.DevMode)
}

func (s *apiSuite) testInstall(c *check.C, releaseInfo *release.OS, flags snapstate.Flags) {
	calledFlags := snapstate.Flags(42)
	installQueue := []string{}
	restore := release.MockReleaseInfo(releaseInfo)
	defer restore()

	snapstateGet = func(s *state.State, name string, snapst *snapstate.SnapState) error {
		// we have ubuntu-core
		return nil
	}
	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
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
	calledFlags := snapstate.Flags(42)
	calledUserID := 0
	installQueue := []string{}

	snapstateGet = func(s *state.State, name string, snapst *snapstate.SnapState) error {
		// we have ubuntu-core
		return nil
	}
	snapstateUpdate = func(s *state.State, name, channel string, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		calledUserID = userID
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "refresh",
		snap:   "some-snap",
		userID: 17,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags, check.Equals, snapstate.Flags(0))
	c.Check(calledUserID, check.Equals, 17)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *apiSuite) TestRefreshDevMode(c *check.C) {
	calledFlags := snapstate.Flags(42)
	calledUserID := 0
	installQueue := []string{}

	snapstateGet = func(s *state.State, name string, snapst *snapstate.SnapState) error {
		// we have ubuntu-core
		return nil
	}
	snapstateUpdate = func(s *state.State, name, channel string, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		calledUserID = userID
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:  "refresh",
		snap:    "some-snap",
		userID:  17,
		DevMode: true,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags, check.Equals, snapstate.Flags(snapstate.DevMode))
	c.Check(calledUserID, check.Equals, 17)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *apiSuite) TestInstallMissingUbuntuCore(c *check.C) {
	installQueue := []*state.Task{}

	snapstateGet = func(s *state.State, name string, snapst *snapstate.SnapState) error {
		// pretend we do not have a state for ubuntu-core
		return state.ErrNoState
	}
	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
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
	// the two "ubuntu-core" install tasks
	c.Check(installQueue[0].Summary(), check.Equals, "ubuntu-core")
	c.Check(installQueue[0].WaitTasks(), check.HasLen, 0)
	c.Check(installQueue[1].WaitTasks(), check.HasLen, 0)
	// the two "some-snap" install tasks
	c.Check(installQueue[2].Summary(), check.Equals, "some-snap")
	c.Check(installQueue[2].WaitTasks(), check.HasLen, 2)
	c.Check(installQueue[3].WaitTasks(), check.HasLen, 2)
}

// Installing ubuntu-core when not having ubuntu-core doesn't misbehave and try
// to install ubuntu-core twice.
func (s *apiSuite) TestInstallUbuntuCoreWhenMissing(c *check.C) {
	installQueue := []*state.Task{}

	snapstateGet = func(s *state.State, name string, snapst *snapstate.SnapState) error {
		// pretend we do not have a state for ubuntu-core
		return state.ErrNoState
	}
	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		t1 := s.NewTask("fake-install-snap", name)
		t2 := s.NewTask("fake-install-snap", "second task is just here so that we can check that the wait is correctly added to all tasks")
		installQueue = append(installQueue, t1, t2)
		return state.NewTaskSet(t1, t2), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "install",
		snap:   "ubuntu-core",
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(installQueue, check.HasLen, 2)
	// the only "ubuntu-core" install tasks
	c.Check(installQueue[0].Summary(), check.Equals, "ubuntu-core")
	c.Check(installQueue[0].WaitTasks(), check.HasLen, 0)
	c.Check(installQueue[1].WaitTasks(), check.HasLen, 0)
}

func (s *apiSuite) TestInstallFails(c *check.C) {
	snapstateGet = func(s *state.State, name string, snapst *snapstate.SnapState) error {
		// we have ubuntu-core
		return nil
	}

	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
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
	calledFlags := snapstate.Flags(42)

	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
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

	c.Check(calledFlags, check.Equals, snapstate.Flags(0))
	c.Check(err, check.IsNil)
}

func (s *apiSuite) TestInstallDevMode(c *check.C) {
	var calledFlags snapstate.Flags

	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action: "install",
		// Install the snap in developer mode
		DevMode: true,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	// DevMode was converted to the snapstate.DevMode flag
	c.Check(calledFlags&snapstate.DevMode, check.Equals, snapstate.Flags(snapstate.DevMode))
}

func (s *apiSuite) TestInstallJailMode(c *check.C) {
	var calledFlags snapstate.Flags

	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:   "install",
		JailMode: true,
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags&snapstate.JailMode, check.Equals, snapstate.Flags(snapstate.JailMode))
}

func (s *apiSuite) TestInstallJailModeDevModeOS(c *check.C) {
	restore := release.MockReleaseInfo(&release.OS{ID: "x-devmode-distro"})
	defer restore()

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:   "install",
		JailMode: true,
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
	}

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.dispatch()(inst, st)
	c.Check(err, check.ErrorMatches, "cannot use devmode and jailmode flags together")
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

	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	repo := d.overlord.InterfaceManager().Repository()
	repo.Connect("consumer", "plug", "producer", "slot")

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

	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
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

	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "different"})
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
- Connect consumer:plug to producer:slot (cannot connect plug "consumer:plug" (interface "test") to "producer:slot" (interface "different"))`)

	repo := d.overlord.InterfaceManager().Repository()
	plug := repo.Plug("consumer", "plug")
	slot := repo.Slot("producer", "slot")
	c.Assert(plug.Connections, check.HasLen, 0)
	c.Assert(slot.Connections, check.HasLen, 0)
}

func (s *apiSuite) TestConnectPlugFailureNoSuchPlug(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
	// there is no consumer, no plug defined
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
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, `cannot perform the following tasks:
- Connect consumer:plug to producer:slot (cannot connect plug "plug" from snap "consumer", no such plug)`)

	repo := d.overlord.InterfaceManager().Repository()
	slot := repo.Slot("producer", "slot")
	c.Assert(slot.Connections, check.HasLen, 0)
}

func (s *apiSuite) TestConnectPlugFailureNoSuchSlot(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	// there is no producer, no slot defined

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
	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Equals, `cannot perform the following tasks:
- Connect consumer:plug to producer:slot (cannot connect plug to slot "slot" from snap "producer", no such slot)`)

	repo := d.overlord.InterfaceManager().Repository()
	plug := repo.Plug("consumer", "plug")
	c.Assert(plug.Connections, check.HasLen, 0)
}

func (s *apiSuite) TestDisconnectPlugSuccess(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
	s.mockSnap(c, consumerYaml)
	s.mockSnap(c, producerYaml)

	repo := d.overlord.InterfaceManager().Repository()
	repo.Connect("consumer", "plug", "producer", "slot")

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

	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
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
- Disconnect consumer:plug from producer:slot (cannot disconnect plug "plug" from snap "consumer", no such plug)`)

	repo := d.overlord.InterfaceManager().Repository()
	slot := repo.Slot("producer", "slot")
	c.Assert(slot.Connections, check.HasLen, 0)
}

func (s *apiSuite) TestDisconnectPlugFailureNoSuchSlot(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
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
- Disconnect consumer:plug from producer:slot (cannot disconnect plug from slot "slot" from snap "producer", no such slot)`)

	repo := d.overlord.InterfaceManager().Repository()
	plug := repo.Plug("consumer", "plug")
	c.Assert(plug.Connections, check.HasLen, 0)
}

func (s *apiSuite) TestDisconnectPlugFailureNotConnected(c *check.C) {
	d := s.daemon(c)

	s.mockIface(c, &interfaces.TestInterface{InterfaceName: "test"})
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
- Disconnect consumer:plug from producer:slot (cannot disconnect plug "plug" from snap "consumer" from slot "slot" from snap "producer", it is not connected)`)

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

func (s *apiSuite) TestAssertOK(c *check.C) {
	// Setup
	restore := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer restore()
	d := s.daemon(c)
	// add store key
	err := d.overlord.AssertManager().DB().Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, check.IsNil)

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
	_, err = d.overlord.AssertManager().DB().Find(asserts.AccountType, map[string]string{
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
		"cannot decode request body into an assertion")
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
	restore := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer restore()
	d := s.daemon(c)
	// add store key
	err := d.overlord.AssertManager().DB().Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, check.IsNil)
	acct := assertstest.NewAccount(s.storeSigning, "developer1", map[string]interface{}{
		"account-id": "developer1-id",
	}, "")
	err = d.overlord.AssertManager().DB().Add(acct)
	c.Assert(err, check.IsNil)
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
	restore := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer restore()
	d := s.daemon(c)
	// add store key
	err := d.overlord.AssertManager().DB().Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, check.IsNil)

	acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	err = d.overlord.AssertManager().DB().Add(acct)
	c.Assert(err, check.IsNil)
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
	restore := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer restore()
	d := s.daemon(c)
	// add store key
	err := d.overlord.AssertManager().DB().Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, check.IsNil)

	acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	err = d.overlord.AssertManager().DB().Add(acct)
	c.Assert(err, check.IsNil)
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

func (s *apiSuite) TestGetEvents(c *check.C) {
	d := s.daemon(c)
	eventsCmd.d = d
	c.Assert(d.hub.SubscriberCount(), check.Equals, 0)

	ts := httptest.NewServer(http.HandlerFunc(eventsCmd.GET(eventsCmd, nil, nil).ServeHTTP))

	req, err := http.NewRequest("GET", ts.URL, nil)
	c.Assert(err, check.IsNil)
	req.Header.Add("Upgrade", "websocket")
	req.Header.Add("Connection", "Upgrade")
	req.Header.Add("Sec-WebSocket-Key", "xxx")
	req.Header.Add("Sec-WebSocket-Version", "13")

	client := &http.Client{}
	resp, err := client.Do(req)
	c.Assert(err, check.IsNil)
	// upgrades request
	c.Assert(resp.Header["Upgrade"], check.DeepEquals, []string{"websocket"})
	// adds subscriber - this happens at the end of the response so close the
	// server first to prevent problems with sequence ordering
	ts.Close()
	c.Assert(d.hub.SubscriberCount(), check.Equals, 1)
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

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"install","summary":"install...","status":"Do","tasks":\[{"id":"\w+","kind":"download","summary":"1...","status":"Do","log":\["2016-04-21T01:02:03Z INFO l11","2016-04-21T01:02:03Z INFO l12"],"progress":{"done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z"}.*`)
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

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"install","summary":"install...","status":"Do","tasks":\[{"id":"\w+","kind":"download","summary":"1...","status":"Do","log":\["2016-04-21T01:02:03Z INFO l11","2016-04-21T01:02:03Z INFO l12"],"progress":{"done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z"}.*],"ready":false,"spawn-time":"2016-04-21T01:02:03Z"}.*`)
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

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"install","summary":"install...","status":"Do","tasks":\[{"id":"\w+","kind":"download","summary":"1...","status":"Do","log":\["2016-04-21T01:02:03Z INFO l11","2016-04-21T01:02:03Z INFO l12"],"progress":{"done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z"}.*],"ready":false,"spawn-time":"2016-04-21T01:02:03Z"}.*`)
	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"remove","summary":"remove..","status":"Error","tasks":\[{"id":"\w+","kind":"unlink","summary":"1...","status":"Error","log":\["2016-04-21T01:02:03Z ERROR rm failed"],"progress":{"done":1,"total":1},"spawn-time":"2016-04-21T01:02:03Z","ready-time":"2016-04-21T01:02:03Z"}.*],"ready":true,"err":"[^"]+".*`)
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

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"remove","summary":"remove..","status":"Error","tasks":\[{"id":"\w+","kind":"unlink","summary":"1...","status":"Error","log":\["2016-04-21T01:02:03Z ERROR rm failed"],"progress":{"done":1,"total":1},"spawn-time":"2016-04-21T01:02:03Z","ready-time":"2016-04-21T01:02:03Z"}.*],"ready":true,"err":"[^"]+".*`)
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
				"progress":   map[string]interface{}{"done": 0., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
			},
			map[string]interface{}{
				"id":         ids[3],
				"kind":       "activate",
				"summary":    "2...",
				"status":     "Do",
				"progress":   map[string]interface{}{"done": 0., "total": 1.},
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
				"progress":   map[string]interface{}{"done": 1., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
				"ready-time": "2016-04-21T01:02:03Z",
			},
			map[string]interface{}{
				"id":         ids[3],
				"kind":       "activate",
				"summary":    "2...",
				"status":     "Hold",
				"progress":   map[string]interface{}{"done": 1., "total": 1.},
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

func (s *apiSuite) TestPostCreateUser(c *check.C) {
	storeUserInfo = func(user string) (*store.User, error) {
		c.Check(user, check.Equals, "popper@lse.ac.uk")
		return &store.User{
			Username: "karl",
			SSHKeys:  []string{"ssh1", "ssh2"},
		}, nil
	}
	osutilAddExtraUser = func(username string, sshKeys []string) error {
		c.Check(username, check.Equals, "karl")
		c.Check(sshKeys, check.DeepEquals, []string{"ssh1", "ssh2"})
		return nil
	}

	postCreateUserUcrednetGetUID = func(string) (uint32, error) {
		return 0, nil
	}
	defer func() {
		osutilAddExtraUser = osutil.AddExtraUser
		postCreateUserUcrednetGetUID = ucrednetGetUID
	}()

	buf := bytes.NewBufferString(`{"email": "popper@lse.ac.uk"}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
}

func (s *apiSuite) TestBuySnap(c *check.C) {
	s.buyResult = &store.BuyResult{State: "Complete"}
	s.err = nil

	buf := bytes.NewBufferString(`{
	  "snap-id": "the-snap-id-1234abcd",
	  "snap-name": "the snap name",
	  "price": 1.23,
	  "currency": "EUR"
	}`)
	req, err := http.NewRequest("POST", "/v2/buy", buf)
	c.Assert(err, check.IsNil)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	rsp := postBuy(buyCmd, req, user).(*resp)

	expected := buyResponseData{
		State: "Complete",
	}
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	c.Check(s.buyOptions, check.DeepEquals, &store.BuyOptions{
		SnapID:   "the-snap-id-1234abcd",
		SnapName: "the snap name",
		Price:    1.23,
		Currency: "EUR",
		User:     user,
	})
}

func (s *apiSuite) TestBuyFailMissingParameter(c *check.C) {
	s.buyResult = nil
	s.err = fmt.Errorf("Missing parameter")

	// snap name missing
	buf := bytes.NewBufferString(`{
	  "snap-id": "the-snap-id-1234abcd",
	  "price": 1.23,
	  "currency": "EUR"
	}`)
	req, err := http.NewRequest("POST", "/v2/buy", buf)
	c.Assert(err, check.IsNil)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	rsp := postBuy(buyCmd, req, user).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusInternalServerError)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, "Missing parameter")

	c.Check(s.buyOptions, check.DeepEquals, &store.BuyOptions{
		SnapID:   "the-snap-id-1234abcd",
		Price:    1.23,
		Currency: "EUR",
		User:     user,
	})
}

func (s *apiSuite) TestIsTrue(c *check.C) {
	form := &multipart.Form{}
	c.Check(isTrue(form, "foo"), check.Equals, false)
	for _, f := range []string{"", "false", "0", "False", "f", "try"} {
		form.Value = map[string][]string{"foo": []string{f}}
		c.Check(isTrue(form, "foo"), check.Equals, false, check.Commentf("expected %q to be false", f))
	}
	for _, t := range []string{"true", "1", "True", "t"} {
		form.Value = map[string][]string{"foo": []string{t}}
		c.Check(isTrue(form, "foo"), check.Equals, true, check.Commentf("expected %q to be true", t))
	}
}

func (s *apiSuite) TestPaymentMethods(c *check.C) {
	s.paymentMethods = &store.PaymentInformation{
		AllowsAutomaticPayment: true,
		Methods: []*store.PaymentMethod{
			&store.PaymentMethod{
				BackendID:           "credit_card",
				Currencies:          []string{"GBP", "USD"},
				Description:         "**** **** **** 1234 (exp 20/2020)",
				ID:                  123,
				Preferred:           true,
				RequiresInteraction: false,
			},
		},
	}
	s.err = nil

	req, err := http.NewRequest("GET", "/v2/buy/methods", nil)
	c.Assert(err, check.IsNil)

	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	rsp := getPaymentMethods(paymentMethodsCmd, req, user).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Assert(rsp.Result, check.FitsTypeOf, s.paymentMethods)
	c.Check(rsp.Result, check.DeepEquals, s.paymentMethods)
}
