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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/asserts"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/overlord/auth"
	"github.com/ubuntu-core/snappy/overlord/ifacestate"
	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/release"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/snaptest"
	"github.com/ubuntu-core/snappy/snappy"
	"github.com/ubuntu-core/snappy/store"
	"github.com/ubuntu-core/snappy/testutil"
)

type apiSuite struct {
	rsnaps            []*snap.Info
	err               error
	vars              map[string]string
	searchTerm        string
	channel           string
	suggestedCurrency string
	overlord          *fakeOverlord
	d                 *Daemon
	auther            store.Authenticator
	restoreBackends   func()
}

var _ = check.Suite(&apiSuite{})

func (s *apiSuite) Snap(name, channel string, auther store.Authenticator) (*snap.Info, error) {
	s.auther = auther
	if len(s.rsnaps) > 0 {
		return s.rsnaps[0], s.err
	}
	return nil, s.err
}

func (s *apiSuite) FindSnaps(searchTerm, channel string, auther store.Authenticator) ([]*snap.Info, error) {
	s.searchTerm = searchTerm
	s.channel = channel
	s.auther = auther

	return s.rsnaps, s.err
}

func (s *apiSuite) SuggestedCurrency() string {
	return s.suggestedCurrency
}

func (s *apiSuite) muxVars(*http.Request) map[string]string {
	return s.vars
}

func (s *apiSuite) SetUpSuite(c *check.C) {
	newRemoteRepo = func() metarepo {
		return s
	}
	muxVars = s.muxVars
}

func (s *apiSuite) TearDownSuite(c *check.C) {
	newRemoteRepo = nil
	muxVars = nil
}

func (s *apiSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, check.IsNil)
	c.Assert(os.MkdirAll(dirs.SnapSnapsDir, 0755), check.IsNil)

	s.rsnaps = nil
	s.suggestedCurrency = ""
	s.err = nil
	s.vars = nil
	s.overlord = &fakeOverlord{
		configs: map[string]string{},
	}
	s.auther = nil
	s.d = nil
	// Disable real security backends for all API tests
	s.restoreBackends = ifacestate.MockSecurityBackends(nil)
}

func (s *apiSuite) TearDownTest(c *check.C) {
	s.d = nil
	s.restoreBackends()
	snapstateInstall = snapstate.Install
	snapstateGet = snapstate.Get
	snapstateInstallPath = snapstate.InstallPath
	readSnapInfo = readSnapInfoImpl
}

func (s *apiSuite) daemon(c *check.C) *Daemon {
	if s.d != nil {
		panic("called daemon() twice")
	}
	d, err := New()
	c.Assert(err, check.IsNil)
	d.addRoutes()
	s.d = d
	return d
}

func (s *apiSuite) mkManifest(c *check.C, pkgType snap.Type) {
	// creating the part to get its manifest path is cheating, a little
	sideInfo := snap.SideInfo{
		OfficialName:      "foo",
		Developer:         "bar",
		Revision:          2147483647,
		EditedDescription: " bla bla bla",
	}

	c.Assert(snappy.SaveManifest(&snap.Info{
		Type:     pkgType,
		Version:  "1",
		SideInfo: sideInfo,
	}), check.IsNil)
}

func (s *apiSuite) mkInstalled(c *check.C, name, developer, version string, revno int, active bool, extraYaml string) *snap.Info {
	return s.mkInstalledInState(c, nil, name, developer, version, revno, active, extraYaml)
}

func (s *apiSuite) mkInstalledInState(c *check.C, daemon *Daemon, name, developer, version string, revno int, active bool, extraYaml string) *snap.Info {
	// Collect arguments into a snap.SideInfo structure
	sideInfo := &snap.SideInfo{
		OfficialName: name,
		Developer:    developer,
		Revision:     revno,
		Channel:      "stable",
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

	err := snappy.SaveManifest(snapInfo)
	c.Assert(err, check.IsNil)

	if daemon != nil {
		st := daemon.overlord.State()
		st.Lock()
		defer st.Unlock()

		var snapst snapstate.SnapState
		snapstate.Get(st, name, &snapst)
		snapst.Active = active
		snapst.Sequence = append(snapst.Sequence, &snapInfo.SideInfo)

		snapstate.Set(st, name, &snapst)
	}

	if active {
		err := snappy.UpdateCurrentSymlink(snapInfo, nil)
		c.Assert(err, check.IsNil)
	}

	return snapInfo
}

func (s *apiSuite) mkGadget(c *check.C, store string) {
	yamlText := fmt.Sprintf(`name: test
version: 1
type: gadget
gadget: {store: {id: %q}}
`, store)
	snaptest.MockSnap(c, yamlText, &snap.SideInfo{Revision: 1})
	c.Assert(os.Symlink("1", filepath.Join(dirs.SnapSnapsDir, "test", "current")), check.IsNil)
}

func (s *apiSuite) TestSnapInfoOneIntegration(c *check.C) {
	d := s.daemon(c)
	s.vars = map[string]string{"name": "foo"}

	// the store tells us about v2
	s.rsnaps = []*snap.Info{{
		Type:    snap.TypeApp,
		Version: "v2",
		SideInfo: snap.SideInfo{
			OfficialName:      "foo",
			EditedSummary:     "summary",
			EditedDescription: "description",
			Developer:         "bar",
			Size:              2,
			Revision:          20,
		},
		IconURL: "meta/gui/icon.svg",
		Prices: map[string]float64{
			"GBP": 1.23,
			"EUR": 2.34,
		},
	}}
	s.suggestedCurrency = "GBP"

	// we have v0 [r5] installed
	s.mkInstalledInState(c, d, "foo", "bar", "v0", 5, false, "")
	// and v1 [r10] is current
	s.mkInstalledInState(c, d, "foo", "bar", "v1", 10, true, "")

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	rsp, ok := getSnapInfo(snapCmd, req).(*resp)
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

	meta := &Meta{
		SuggestedCurrency: "GBP",
	}
	expected := &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: map[string]interface{}{
			"name":             "foo",
			"revision":         10,
			"version":          "v1",
			"summary":          "summary",
			"description":      "description",
			"developer":        "bar",
			"status":           "active",
			"icon":             "/v2/icons/foo/icon",
			"type":             string(snap.TypeApp),
			"vendor":           "",
			"download-size":    int64(2),
			"resource":         "/v2/snaps/foo",
			"update-available": 20,
			// XXX: fix this later "rollback-available": 5,
			"channel": "stable",
			"prices": map[string]float64{
				"GBP": 1.23,
				"EUR": 2.34,
			},
		},
		Meta: meta,
	}

	c.Check(rsp, check.DeepEquals, expected)
}

func (s *apiSuite) TestSnapInfoWithAuth(c *check.C) {
	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", `Macaroon root="macaroon", discharge="discharge"`)

	c.Assert(s.auther, check.IsNil)

	_, ok := getSnapInfo(snapCmd, req).(*resp)
	c.Assert(ok, check.Equals, true)
	// ensure authenticator was set
	c.Assert(s.auther, check.DeepEquals, user.Authenticator())
}

func (s *apiSuite) TestSnapInfoNotFound(c *check.C) {
	s.vars = map[string]string{"name": "foo"}
	s.err = snappy.ErrPackageNotFound

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	c.Check(getSnapInfo(snapCmd, req).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestSnapInfoNoneFound(c *check.C) {
	s.vars = map[string]string{"name": "foo"}

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	c.Check(getSnapInfo(snapCmd, req).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestSnapInfoIgnoresRemoteErrors(c *check.C) {
	s.vars = map[string]string{"name": "foo"}
	s.err = errors.New("weird")

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	rsp := getSnapInfo(snapCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestSnapInfoWeirdRoute(c *check.C) {
	// can't really happen

	d := s.daemon(c)

	// use the wrong command to force the issue
	wrongCmd := &Command{Path: "/{what}", d: d}
	s.vars = map[string]string{"name": "foo"}
	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			OfficialName: "foo",
		},
	}}
	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	c.Check(getSnapInfo(wrongCmd, req).Self(nil, nil).(*resp).Status, check.Equals, http.StatusInternalServerError)
}

func (s *apiSuite) TestSnapInfoBadRoute(c *check.C) {
	// can't really happen, v2

	d := s.daemon(c)

	// get the route and break it
	route := d.router.Get(snapCmd.Path)
	c.Assert(route.Name("foo").GetError(), check.NotNil)

	s.vars = map[string]string{"name": "foo"}
	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			OfficialName: "foo",
		},
	}}

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	rsp := getSnapInfo(snapCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusInternalServerError)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, `route can't build URL .*`)
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
		"newRemoteRepo",
		"errNothingToInstall",
		// snapInstruction vars:
		"snapInstructionDispTable",
		"snapstateInstall",
		"snapstateUpdate",
		"snapstateInstallPath",
		"snapstateGet",
		"readSnapInfo",
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

	rootCmd.GET(rootCmd, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/json")

	expected := []interface{}{"TBD"}
	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *apiSuite) mkrelease() {
	// set up release
	release.Override(release.Release{
		Flavor: "flavor",
		Series: "series",
	})
}

func (s *apiSuite) TestSysInfo(c *check.C) {
	// check it only does GET
	c.Check(sysInfoCmd.PUT, check.IsNil)
	c.Check(sysInfoCmd.POST, check.IsNil)
	c.Check(sysInfoCmd.DELETE, check.IsNil)
	c.Assert(sysInfoCmd.GET, check.NotNil)

	rec := httptest.NewRecorder()
	c.Check(sysInfoCmd.Path, check.Equals, "/v2/system-info")

	s.mkrelease()

	sysInfoCmd.GET(sysInfoCmd, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/json")

	expected := map[string]interface{}{
		"flavor": "flavor",
		"series": "series",
	}
	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *apiSuite) TestSysInfoStore(c *check.C) {
	rec := httptest.NewRecorder()
	c.Check(sysInfoCmd.Path, check.Equals, "/v2/system-info")

	s.mkrelease()
	s.mkGadget(c, "some-store")

	sysInfoCmd.GET(sysInfoCmd, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)

	expected := map[string]interface{}{
		"flavor": "flavor",
		"series": "series",
		"store":  "some-store",
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
	store.MyAppsPackageAccessAPI = mockMyAppsServer.URL + "/acl/package_access/"
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

func (s *apiSuite) TestLoginUser(c *check.C) {
	macaroon := `{"macaroon": "the-macaroon-serialized-data"}`
	mockMyAppsServer := s.makeMyAppsServer(200, macaroon)
	defer mockMyAppsServer.Close()

	discharge := `{"discharge_macaroon": "the-discharge-macaroon-serialized-data"}`
	mockSSOServer := s.makeSSOServer(200, discharge)
	defer mockSSOServer.Close()

	buf := bytes.NewBufferString(`{"username": "username", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req).(*resp)

	expected := loginResponseData{
		Macaroon:   "the-macaroon-serialized-data",
		Discharges: []string{"the-discharge-macaroon-serialized-data"},
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Assert(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	expectedUser := auth.UserState{
		ID:         1,
		Username:   "username",
		Macaroon:   "the-macaroon-serialized-data",
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

	rsp := logoutUser(logoutCmd, req).(*resp)
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

	rsp := loginUser(snapCmd, req).(*resp)

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

	rsp := loginUser(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusInternalServerError)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "cannot get package access macaroon")
}

func (s *apiSuite) TestLoginUserTwoFactorRequiredError(c *check.C) {
	macaroon := `{"macaroon": "the-macaroon-serialized-data"}`
	mockMyAppsServer := s.makeMyAppsServer(200, macaroon)
	defer mockMyAppsServer.Close()

	discharge := `{"code": "TWOFACTOR_REQUIRED"}`
	mockSSOServer := s.makeSSOServer(401, discharge)
	defer mockSSOServer.Close()

	buf := bytes.NewBufferString(`{"username": "username", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusUnauthorized)
	c.Check(rsp.Result.(*errorResult).Kind, check.Equals, errorKindTwoFactorRequired)
}

func (s *apiSuite) TestLoginUserTwoFactorFailedError(c *check.C) {
	macaroon := `{"macaroon": "the-macaroon-serialized-data"}`
	mockMyAppsServer := s.makeMyAppsServer(200, macaroon)
	defer mockMyAppsServer.Close()

	discharge := `{"code": "TWOFACTOR_FAILURE"}`
	mockSSOServer := s.makeSSOServer(403, discharge)
	defer mockSSOServer.Close()

	buf := bytes.NewBufferString(`{"username": "username", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusUnauthorized)
	c.Check(rsp.Result.(*errorResult).Kind, check.Equals, errorKindTwoFactorFailed)
}

func (s *apiSuite) TestLoginUserInvalidCredentialsError(c *check.C) {
	macaroon := `{"macaroon": "the-macaroon-serialized-data"}`
	mockMyAppsServer := s.makeMyAppsServer(200, macaroon)
	defer mockMyAppsServer.Close()

	discharge := `{"code": "INVALID_CREDENTIALS"}`
	mockSSOServer := s.makeSSOServer(401, discharge)
	defer mockSSOServer.Close()

	buf := bytes.NewBufferString(`{"username": "username", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp := loginUser(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusUnauthorized)
	c.Check(rsp.Result.(*errorResult).Message, testutil.Contains, "cannot get discharge macaroon")
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
		s.mkInstalledInState(c, d, snp.name, snp.dev, snp.ver, snp.rev, false, "")
	}

	rsp, ok := getSnapsInfo(snapsCmd, req).(*resp)
	c.Assert(ok, check.Equals, true)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Result, check.NotNil)

	c.Check(rsp.Meta.Paging, check.DeepEquals, &Paging{Page: 1, Pages: 1})

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
		c.Check(got["revision"], check.Equals, float64(s.rev))
		c.Check(got["developer"], check.Equals, s.dev)
	}
}

func (s *apiSuite) TestSnapsInfoOnlyLocal(c *check.C) {
	d := s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			OfficialName: "store",
			Developer:    "foo",
		},
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", 10, true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=local", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"local"})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "local")
}

func (s *apiSuite) TestFind(c *check.C) {
	s.suggestedCurrency = "EUR"

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			OfficialName: "store",
			Developer:    "foo",
		},
	}}

	req, err := http.NewRequest("GET", "/v2/find?q=hi&channel=potato", nil)
	c.Assert(err, check.IsNil)

	rsp := searchStore(findCmd, req).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "store")
	c.Check(snaps[0]["prices"], check.IsNil)

	c.Check(rsp.SuggestedCurrency, check.Equals, "EUR")

	c.Check(s.searchTerm, check.Equals, "hi")
	c.Check(s.channel, check.Equals, "potato")
}

func (s *apiSuite) TestSnapsInfoOnlyStore(c *check.C) {
	d := s.daemon(c)

	s.suggestedCurrency = "EUR"

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			OfficialName: "store",
			Developer:    "foo",
		},
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", 10, true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=store", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"store"})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "store")
	c.Check(snaps[0]["prices"], check.IsNil)

	c.Check(rsp.SuggestedCurrency, check.Equals, "EUR")
}

func (s *apiSuite) TestSnapsInfoStoreWithAuth(c *check.C) {
	state := snapCmd.d.overlord.State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/snaps?sources=store", nil)
	c.Assert(err, check.IsNil)
	req.Header.Set("Authorization", `Macaroon root="macaroon", discharge="discharge"`)

	c.Assert(s.auther, check.IsNil)

	_ = getSnapsInfo(snapsCmd, req).(*resp)

	// ensure authenticator was set
	c.Assert(s.auther, check.DeepEquals, user.Authenticator())
}

func (s *apiSuite) TestSnapsInfoLocalAndStore(c *check.C) {
	d := s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			OfficialName: "remote",
			Developer:    "foo",
		},
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", 10, true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=local,store", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"local", "store"})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 2)
}

func (s *apiSuite) TestSnapsInfoDefaultSources(c *check.C) {
	d := s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			OfficialName: "remote",
			Developer:    "foo",
		},
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", 10, true, "")

	req, err := http.NewRequest("GET", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"local", "store"})
}

func (s *apiSuite) TestSnapsInfoUnknownSource(c *check.C) {
	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			OfficialName: "remote",
			Developer:    "foo",
		},
	}}
	s.mkInstalled(c, "local", "foo", "v1", 10, true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=unknown", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	c.Assert(rsp.Sources, check.HasLen, 0)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 0)
}

func (s *apiSuite) TestSnapsInfoFilterLocal(c *check.C) {
	d := s.daemon(c)

	s.rsnaps = nil
	s.mkInstalledInState(c, d, "foo", "foo", "v1", 10, true, "")
	s.mkInstalledInState(c, d, "bar", "bar", "v1", 10, true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?q=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "foo")
}

func (s *apiSuite) TestSnapsInfoFilterRemote(c *check.C) {
	s.rsnaps = nil

	req, err := http.NewRequest("GET", "/v2/snaps?q=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	c.Check(s.searchTerm, check.Equals, "foo")

	c.Assert(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestSnapsInfoAppsOnly(c *check.C) {
	s.mkInstalled(c, "app", "foo", "v1", 10, true, "type: app")
	s.mkInstalled(c, "framework", "foo", "v1", 10, true, "type: framework")

	req, err := http.NewRequest("GET", "/v2/snaps?types=app", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "app")
}

func (s *apiSuite) TestSnapsInfoFrameworksOnly(c *check.C) {
	d := s.daemon(c)

	s.mkInstalledInState(c, d, "app", "foo", "v1", 10, true, "type: app")
	s.mkInstalledInState(c, d, "framework", "foo", "v1", 10, true, "type: framework")

	req, err := http.NewRequest("GET", "/v2/snaps?types=framework", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "framework")
}

func (s *apiSuite) TestSnapsInfoAppsAndFrameworks(c *check.C) {
	d := s.daemon(c)

	s.mkInstalledInState(c, d, "app", "foo", "v1", 10, true, "type: app")
	s.mkInstalledInState(c, d, "framework", "foo", "v1", 10, true, "type: framework")

	req, err := http.NewRequest("GET", "/v2/snaps?types=app,framework", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 2)
}

func (s *apiSuite) TestPostSnapBadRequest(c *check.C) {
	buf := bytes.NewBufferString(`hello`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostSnapBadAction(c *check.C) {
	buf := bytes.NewBufferString(`{"action": "potato"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostSnap(c *check.C) {
	d := s.daemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

	snapInstructionDispTable["install"] = func(*snapInstruction, *state.State) (string, []*state.TaskSet, error) {
		return "foooo", nil, nil
	}
	defer func() {
		snapInstructionDispTable["install"] = snapInstall
	}()

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Summary(), check.Equals, "foooo")
	st.Unlock()
}

func (s *apiSuite) TestPostSnapSetsUser(c *check.C) {
	d := s.daemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

	snapInstructionDispTable["install"] = func(inst *snapInstruction, st *state.State) (string, []*state.TaskSet, error) {
		return string(inst.userID), nil, nil
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

	rsp := postSnap(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)

	st := d.overlord.State()
	st.Lock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Summary(), check.Equals, string(user.ID))
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
		{"rollback", snapRollback},
		{"activate", snapActivate},
		{"deactivate", snapDeactivate},
		{"xyzzy", nil},
	}

	for _, action := range actions {
		inst.Action = action.s
		// do you feel dirty yet?
		c.Check(fmt.Sprintf("%p", action.impl), check.Equals, fmt.Sprintf("%p", inst.dispatch()))
	}
}

type fakeOverlord struct {
	configs map[string]string
}

func (o *fakeOverlord) Configure(s *snappy.Snap, c []byte) ([]byte, error) {
	if len(c) > 0 {
		o.configs[s.Name()] = string(c)
	}
	config, ok := o.configs[s.Name()]
	if !ok {
		return nil, fmt.Errorf("no config for %q", s.Name())
	}
	return []byte(config), nil
}

func (s *apiSuite) TestSideloadSnap(c *check.C) {
	// try a multipart/form-data upload
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap-path\"\r\n" +
		"\r\n" +
		"a/b/local.snap\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	chgSummary := s.sideloadCheck(c, body, head, 0, false)
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
	chgSummary := s.sideloadCheck(c, body, head, snappy.DeveloperMode, true)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "x"`)
}

func (s *apiSuite) TestSideloadSnapNotValidFormFile(c *check.C) {
	d := newTestDaemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

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

	rsp := sideloadSnap(snapsCmd, req).(*resp)
	c.Assert(rsp.Type, check.Equals, ResponseTypeError)
	c.Assert(rsp.Result.(*errorResult).Message, check.Matches, `cannot find "snap" file field in provided multipart/form-data payload`)
}

func (s *apiSuite) sideloadCheck(c *check.C, content string, head map[string]string, expectedFlags snappy.InstallFlags, hasUbuntuCore bool) string {
	d := newTestDaemon(c)
	d.overlord.Loop()
	defer d.overlord.Stop()

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
	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snappy.InstallFlags) (*state.TaskSet, error) {
		// NOTE: ubuntu-core is not installed in developer mode
		c.Check(flags, check.Equals, snappy.InstallFlags(0))
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	snapstateInstallPath = func(s *state.State, name, path, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
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

	rsp := sideloadSnap(snapsCmd, req).(*resp)
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

	c.Assert(chg.Tasks(), check.HasLen, n)

	st.Unlock()
	<-chg.Ready()
	st.Lock()

	c.Check(chg.Kind(), check.Equals, "install-snap")
	return chg.Summary()
}

func (s *apiSuite) TestAppIconGet(c *check.C) {
	d := s.daemon(c)

	// have an active foo in the system
	info := s.mkInstalledInState(c, d, "foo", "bar", "v1", 10, true, "")

	// have an icon for it in the package itself
	iconfile := filepath.Join(info.MountDir(), "meta", "gui", "icon.ick")
	c.Assert(os.MkdirAll(filepath.Dir(iconfile), 0755), check.IsNil)
	c.Check(ioutil.WriteFile(iconfile, []byte("ick"), 0644), check.IsNil)

	s.vars = map[string]string{"name": "foo"}
	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Body.String(), check.Equals, "ick")
}

func (s *apiSuite) TestAppIconGetInactive(c *check.C) {
	d := s.daemon(c)

	// have an *in*active foo in the system
	info := s.mkInstalledInState(c, d, "foo", "bar", "v1", 10, false, "")

	// have an icon for it in the package itself
	iconfile := filepath.Join(info.MountDir(), "meta", "gui", "icon.ick")
	c.Assert(os.MkdirAll(filepath.Dir(iconfile), 0755), check.IsNil)
	c.Check(ioutil.WriteFile(iconfile, []byte("ick"), 0644), check.IsNil)

	s.vars = map[string]string{"name": "foo"}
	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Body.String(), check.Equals, "ick")
}

func (s *apiSuite) TestAppIconGetNoIcon(c *check.C) {
	d := s.daemon(c)

	// have an *in*active foo in the system
	info := s.mkInstalledInState(c, d, "foo", "bar", "v1", 10, true, "")

	// NO ICON!
	err := os.RemoveAll(filepath.Join(info.MountDir(), "meta", "gui", "icon.svg"))
	c.Assert(err, check.IsNil)

	s.vars = map[string]string{"name": "foo"}
	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code/100, check.Equals, 4)
}

func (s *apiSuite) TestAppIconGetNoApp(c *check.C) {
	s.daemon(c)

	s.vars = map[string]string{"name": "foo"}
	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	appIconCmd.GET(appIconCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 404)
}

func (s *apiSuite) TestPkgInstructionAgreedOK(c *check.C) {
	lic := &licenseData{
		Intro:   "hi",
		License: "Void where empty",
		Agreed:  true,
	}

	inst := &snapInstruction{License: lic}

	c.Check(inst.Agreed(lic.Intro, lic.License), check.Equals, true)
}

func (s *apiSuite) TestPkgInstructionAgreedNOK(c *check.C) {
	lic := &licenseData{
		Intro:   "hi",
		License: "Void where empty",
		Agreed:  false,
	}

	inst := &snapInstruction{License: lic}

	c.Check(inst.Agreed(lic.Intro, lic.License), check.Equals, false)
}

func (s *apiSuite) TestPkgInstructionMismatch(c *check.C) {
	lic := &licenseData{
		Intro:   "hi",
		License: "Void where empty",
		Agreed:  true,
	}

	inst := &snapInstruction{License: lic}

	c.Check(inst.Agreed("blah", "yak yak"), check.Equals, false)
}

func (s *apiSuite) TestInstall(c *check.C) {
	calledFlags := snappy.InstallFlags(42)
	installQueue := []string{}

	snapstateGet = func(s *state.State, name string, snapst *snapstate.SnapState) error {
		// we have ubuntu-core
		return nil
	}
	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snappy.InstallFlags) (*state.TaskSet, error) {
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
	rsp := postSnap(snapCmd, req).(*resp)

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
	c.Check(calledFlags, check.Equals, snappy.DoInstallGC)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(chg.Kind(), check.Equals, "install-snap")
	c.Check(chg.Summary(), check.Equals, `Install "some-snap" snap`)
}

func (s *apiSuite) TestRefresh(c *check.C) {
	calledFlags := snappy.InstallFlags(42)
	calledUserID := 0
	installQueue := []string{}

	snapstateGet = func(s *state.State, name string, snapst *snapstate.SnapState) error {
		// we have ubuntu-core
		return nil
	}
	snapstateUpdate = func(s *state.State, name, channel string, userID int, flags snappy.InstallFlags) (*state.TaskSet, error) {
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

	c.Check(calledFlags, check.Equals, snappy.DoInstallGC)
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
	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snappy.InstallFlags) (*state.TaskSet, error) {
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
	rsp := postSnap(snapCmd, req).(*resp)

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
	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snappy.InstallFlags) (*state.TaskSet, error) {
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

	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snappy.InstallFlags) (*state.TaskSet, error) {
		t := s.NewTask("fake-install-snap-error", "Install task")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)

	d.overlord.Loop()
	defer d.overlord.Stop()

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req).(*resp)

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
	calledFlags := snappy.InstallFlags(42)

	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snappy.InstallFlags) (*state.TaskSet, error) {
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

	c.Check(calledFlags, check.Equals, snappy.InstallFlags(0))
	c.Check(err, check.IsNil)
}

func (s *apiSuite) TestInstallDevMode(c *check.C) {
	var calledFlags snappy.InstallFlags

	snapstateInstall = func(s *state.State, name, channel string, userID int, flags snappy.InstallFlags) (*state.TaskSet, error) {
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

	// DevMode was converted to the snappy.DeveloperMode flag
	c.Check(calledFlags&snappy.DeveloperMode, check.Equals, snappy.DeveloperMode)
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

// FIXME: license prompt broken for now
/*
func (s *apiSuite) TestInstallLicensed(c *check.C) {
	snapstateInstall = func(s *state.State, name, channel string, flags snappy.InstallFlags) (state.TaskSet, error) {
		if meter.Agreed("hi", "yak yak") {
			return nil, nil
		}

		return nil, snappy.ErrLicenseNotAccepted
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		Action:   "install",
	}

	d.overlord.Loop()
	defer d.overlord.Stop()
	lic, ok := inst.dispatch()().(*licenseData)

	c.Assert(ok, check.Equals, true)
	c.Check(lic, check.ErrorMatches, "license agreement required")
	c.Check(lic.Intro, check.Equals, "hi")
	c.Check(lic.License, check.Equals, "yak yak")
	c.Check(lic.Agreed, check.Equals, false)

	// now, pass it in
	inst.License = lic
	inst.License.Agreed = true

	err := inst.dispatch()()
	c.Check(err, check.IsNil)
}

func (s *apiSuite) TestInstallLicensedIntegration(c *check.C) {
	d := s.daemon(c)

	snapstateInstall = func(s *state.State, name, channel string, flags snappy.InstallFlags) (state.TaskSet, error) {
		if meter.Agreed("hi", "yak yak") {
			return nil, nil
		}

		return nil, snappy.ErrLicenseNotAccepted
	}

	req, err := http.NewRequest("POST", "/v2/snaps/foo", strings.NewReader(`{"action": "install"}`))
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"name": "foo"}

	res := postSnap(snapCmd, req).(*resp).Result.(map[string]interface{})
	task := d.tasks[res["resource"].(string)[16:]]
	c.Check(task, check.NotNil)

	task.tomb.Wait()
	c.Check(task.State(), check.Equals, TaskFailed)
	errRes := task.output.(errorResult)
	c.Check(errRes.Message, check.Equals, "license agreement required")
	c.Check(errRes.Kind, check.Equals, errorKindLicenseRequired)
	c.Check(errRes.Value, check.DeepEquals, &licenseData{
		Intro:   "hi",
		License: "yak yak",
	})

	req, err = http.NewRequest("POST", "/v2/snaps/foo", strings.NewReader(`{"action": "install", "license": {"intro": "hi", "license": "yak yak", "agreed": true}}`))
	c.Assert(err, check.IsNil)

	res = postSnap(snapCmd, req).(*resp).Result.(map[string]interface{})
	task = d.tasks[res["resource"].(string)[16:]]
	c.Check(task, check.NotNil)

	task.tomb.Wait()
	c.Check(task.State(), check.Equals, TaskSucceeded)
}
*/

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
	interfacesCmd.GET(interfacesCmd, req).ServeHTTP(rec, req)
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
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
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
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
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
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
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
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
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
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
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
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
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
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
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
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
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
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
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
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
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
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
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

const (
	testTrustedKey = `type: account-key
authority-id: can0nical
account-id: can0nical
public-key-id: 844efa9730eec4be
public-key-fingerprint: 716ff3cec4b9364a2bd930dc844efa9730eec4be
since: 2016-01-14T15:00:00Z
until: 2023-01-14T15:00:00Z
body-length: 376

openpgp xsBNBFaXv40BCADIlqLKFZaPaoe4TNLQv77vh4JWTlt7Z3IN2ducNqfg50q5mnkyUD2D
SckvsMy1440+a0Z83m/A7aPaO1JkLpMGfLr23VLyKCaAe0k6hg69/6aEfXhfy0yYvEOgGcBiX+fN
T6tqdRCsd+08LtisjYez7iJvmVwQ/syeduoTU4EiSVO1zlgc3eeq3TFyvcN0E1EsZ/7l2A33amTo
mtAPVyQsa1B+lTeaUgwuPBWV0oTuYcUSfYsmmsXEKx/PnzkliicnrC9QZ5CcisskVve3QwPAuLUz
2nV7/6vSRF22T4cUPF4QntjZBB6xjopdDH6wQsKyzLTTRak74moWksx8MEmVABEBAAE=

openpgp wsBcBAABCAAQBQJWl8DiCRCETvqXMO7EvgAAhjkIAEoINWjQkujtx/TFYsKh0yYcQSpT
v8O83mLRP7Ty+mH99uQ0/DbeQ1hM5st8cFgzU8SzlDCh6BUMnAl/bR/hhibFD40CBLd13kDXl1aN
APybmSYoDVRQPAPop44UF0aCrTIw4Xds3E56d2Rsn+CkNML03kRc/i0Q53uYzZwxXVnzW/gVOXDL
u/IZtjeo3KsB645MVEUxJLQmjlgMOwMvCHJgWhSvZOuf7wC0soBCN9Ufa/0M/PZFXzzn8LpjKVrX
iDXhV7cY5PceG8ZV7Duo1JadOCzpkOHmai4DcrN7ZeY8bJnuNjOwvTLkrouw9xci4IxpPDRu0T/i
K9qaJtUo4cA=`
	testAccKey = `type: account-key
authority-id: can0nical
account-id: developer1
public-key-id: adea89b00094c337
public-key-fingerprint: 5fa7b16ad5e8c8810d5a0686adea89b00094c337
since: 2016-01-14T15:00:00Z
until: 2023-01-14T15:00:00Z
body-length: 376

openpgp xsBNBFaXv5MBCACkK//qNb3UwRtDviGcCSEi8Z6d5OXok3yilQmEh0LuW6DyP9sVpm08
Vb1LGewOa5dThWGX4XKRBI/jCUnjCJQ6v15lLwHe1N7MJQ58DUxKqWFMV9yn4RcDPk6LqoFpPGdR
rbp9Ivo3PqJRMyD0wuJk9RhbaGZmILcL//BLgomE9NgQdAfZbiEnGxtkqAjeVtBtcJIj5TnCC658
ZCqwugQeO9iJuIn3GosYvvTB6tReq6GP6b4dqvoi7SqxHVhtt2zD4Y6FUZIVmvZK0qwkV0gua2az
LzPOeoVcU1AEl7HVeBk7G6GiT5jx+CjjoGa0j22LdJB9S3JXHtGYk5p9CAwhABEBAAE=

openpgp wsBcBAABCAAQBQJWl8HNCRCETvqXMO7EvgAAeuAIABn/1i8qGyaIhxOWE2cHIPYW3hq2
PWpq7qrPN5Dbp/00xrTvc6tvMQWsXlMrAsYuq3sBCxUp3JRp9XhGiQeJtb8ft10g3+3J7e8OGHjl
CfXJ3A5el8Xxp5qkFywCsLdJgNtF6+uSQ4dO8SrAwzkM7c3JzntxdiFOjDLUSyZ+rXL42jdRagTY
8bcZfb47vd68Hyz3EvSvJuHSDbcNSTd3B832cimpfq5vJ7FoDrchVn3sg+3IwekuPhG3LQn5BVtc
0ontHd+V1GaandhqBaDA01cGZN0gnqv2Haogt0P/h3nZZZJ1nTW5PLC6hs8TZdBdl3Lel8yAHD5L
ZF5jSvRDLgI=`
)

func (s *apiSuite) TestAssertOK(c *check.C) {
	// Setup
	os.MkdirAll(filepath.Dir(dirs.SnapTrustedAccountKey), 0755)
	err := ioutil.WriteFile(dirs.SnapTrustedAccountKey, []byte(testTrustedKey), 0640)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)
	buf := bytes.NewBufferString(testAccKey)
	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions", buf)
	c.Assert(err, check.IsNil)
	rsp := doAssert(assertsCmd, req).Self(nil, nil).(*resp)
	// Verify (external)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	// Verify (internal)
	_, err = d.overlord.AssertManager().DB().Find(asserts.AccountKeyType, map[string]string{
		"account-id":    "developer1",
		"public-key-id": "adea89b00094c337",
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
	assertsCmd.POST(assertsCmd, req).ServeHTTP(rec, req)
	// Verify (external)
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains,
		"can't decode request body into an assertion")
}

func (s *apiSuite) TestAssertError(c *check.C) {
	s.daemon(c)
	// Setup
	buf := bytes.NewBufferString(testAccKey)
	req, err := http.NewRequest("POST", "/v2/assertions", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	// Execute
	assertsCmd.POST(assertsCmd, req).ServeHTTP(rec, req)
	// Verify (external)
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains, "assert failed")
}

func (s *apiSuite) TestAssertsFindManyAll(c *check.C) {
	// Setup
	os.MkdirAll(filepath.Dir(dirs.SnapTrustedAccountKey), 0755)
	err := ioutil.WriteFile(dirs.SnapTrustedAccountKey, []byte(testTrustedKey), 0640)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)
	a, err := asserts.Decode([]byte(testAccKey))
	c.Assert(err, check.IsNil)
	err = d.overlord.AssertManager().DB().Add(a)
	c.Assert(err, check.IsNil)
	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions/account-key", nil)
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"assertType": "account-key"}
	rec := httptest.NewRecorder()
	assertsFindManyCmd.GET(assertsFindManyCmd, req).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, http.StatusOK, check.Commentf("body %q", rec.Body))
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/x.ubuntu.assertion; bundle=y")
	c.Check(rec.HeaderMap.Get("X-Ubuntu-Assertions-Count"), check.Equals, "2")
	dec := asserts.NewDecoder(rec.Body)
	a1, err := dec.Decode()
	c.Assert(err, check.IsNil)
	c.Check(a1.Type(), check.Equals, asserts.AccountKeyType)

	a2, err := dec.Decode()
	c.Assert(err, check.IsNil)

	_, err = dec.Decode()
	c.Assert(err, check.Equals, io.EOF)

	ids := []string{a1.(*asserts.AccountKey).AccountID(), a2.(*asserts.AccountKey).AccountID()}
	c.Check(ids, check.DeepEquals, []string{"can0nical", "developer1"})
}

func (s *apiSuite) TestAssertsFindManyFilter(c *check.C) {
	// Setup
	os.MkdirAll(filepath.Dir(dirs.SnapTrustedAccountKey), 0755)
	err := ioutil.WriteFile(dirs.SnapTrustedAccountKey, []byte(testTrustedKey), 0640)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)
	a, err := asserts.Decode([]byte(testAccKey))
	c.Assert(err, check.IsNil)
	err = d.overlord.AssertManager().DB().Add(a)
	c.Assert(err, check.IsNil)
	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions/account-key?account-id=developer1", nil)
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"assertType": "account-key"}
	rec := httptest.NewRecorder()
	assertsFindManyCmd.GET(assertsFindManyCmd, req).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, http.StatusOK, check.Commentf("body %q", rec.Body))
	c.Check(rec.HeaderMap.Get("X-Ubuntu-Assertions-Count"), check.Equals, "1")
	dec := asserts.NewDecoder(rec.Body)
	a1, err := dec.Decode()
	c.Assert(err, check.IsNil)
	c.Check(a1.Type(), check.Equals, asserts.AccountKeyType)
	c.Check(a1.(*asserts.AccountKey).AccountID(), check.Equals, "developer1")
	_, err = dec.Decode()
	c.Check(err, check.Equals, io.EOF)
}

func (s *apiSuite) TestAssertsFindManyNoResults(c *check.C) {
	// Setup
	os.MkdirAll(filepath.Dir(dirs.SnapTrustedAccountKey), 0755)
	err := ioutil.WriteFile(dirs.SnapTrustedAccountKey, []byte(testTrustedKey), 0640)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)
	a, err := asserts.Decode([]byte(testAccKey))
	c.Assert(err, check.IsNil)
	err = d.overlord.AssertManager().DB().Add(a)
	c.Assert(err, check.IsNil)
	// Execute
	req, err := http.NewRequest("POST", "/v2/assertions/account-key?account-id=xyzzyx", nil)
	c.Assert(err, check.IsNil)
	s.vars = map[string]string{"assertType": "account-key"}
	rec := httptest.NewRecorder()
	assertsFindManyCmd.GET(assertsFindManyCmd, req).ServeHTTP(rec, req)
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
	assertsFindManyCmd.GET(assertsFindManyCmd, req).ServeHTTP(rec, req)
	// Verify
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rec.Body.String(), testutil.Contains, "invalid assert type")
}

func (s *apiSuite) TestGetEvents(c *check.C) {
	d := s.daemon(c)
	eventsCmd.d = d
	c.Assert(d.hub.SubscriberCount(), check.Equals, 0)

	ts := httptest.NewServer(http.HandlerFunc(eventsCmd.GET(eventsCmd, nil).ServeHTTP))

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
	rsp := getChanges(stateChangesCmd, req).(*resp)

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
	rsp := getChanges(stateChangesCmd, req).(*resp)

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
	rsp := getChanges(stateChangesCmd, req).(*resp)

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
	rsp := getChanges(stateChangesCmd, req).(*resp)

	// Verify
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Assert(rsp.Result, check.HasLen, 1)

	res, err := rsp.MarshalJSON()
	c.Assert(err, check.IsNil)

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"remove","summary":"remove..","status":"Error","tasks":\[{"id":"\w+","kind":"unlink","summary":"1...","status":"Error","log":\["2016-04-21T01:02:03Z ERROR rm failed"],"progress":{"done":1,"total":1},"spawn-time":"2016-04-21T01:02:03Z","ready-time":"2016-04-21T01:02:03Z"}.*],"ready":true,"err":"[^"]+".*`)
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
	rsp := getChange(stateChangeCmd, req).(*resp)
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
	rsp := abortChange(stateChangeCmd, req).(*resp)
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
	rsp := abortChange(stateChangeCmd, req).(*resp)
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
