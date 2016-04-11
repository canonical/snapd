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
	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/release"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snappy"
	"github.com/ubuntu-core/snappy/store"
	"github.com/ubuntu-core/snappy/testutil"
)

type apiSuite struct {
	rsnaps     []*snap.Info
	err        error
	vars       map[string]string
	searchTerm string
	channel    string
	overlord   *fakeOverlord
	d          *Daemon
}

var _ = check.Suite(&apiSuite{})

func (s *apiSuite) Snap(string, string) (*snap.Info, error) {
	if len(s.rsnaps) > 0 {
		return s.rsnaps[0], s.err
	}
	return nil, s.err
}

func (s *apiSuite) FindSnaps(searchTerm, channel string) ([]*snap.Info, error) {
	s.searchTerm = searchTerm
	s.channel = channel

	return s.rsnaps, s.err
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
	snapstateInstall = snapstate.Install
}

func (s *apiSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, check.IsNil)
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapLockFile), 0755), check.IsNil)
	c.Assert(os.MkdirAll(dirs.SnapSnapsDir, 0755), check.IsNil)

	s.rsnaps = nil
	s.err = nil
	s.vars = nil
	s.overlord = &fakeOverlord{
		configs: map[string]string{},
	}
	s.d = nil
}

func (s *apiSuite) TearDownTest(c *check.C) {
	s.d = nil
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
	skelInfo := &snap.Info{
		SideInfo: snap.SideInfo{
			OfficialName: name,
			Developer:    developer,
			Revision:     revno,
		},
		Version: version,
	}

	c.Assert(os.MkdirAll(skelInfo.DataDir(), 0755), check.IsNil)

	metadir := filepath.Join(skelInfo.MountDir(), "meta")
	c.Assert(os.MkdirAll(metadir, 0755), check.IsNil)

	guidir := filepath.Join(metadir, "gui")
	c.Assert(os.MkdirAll(guidir, 0755), check.IsNil)

	c.Check(ioutil.WriteFile(filepath.Join(guidir, "icon.svg"), []byte("yadda icon"), 0644), check.IsNil)

	yamlPath := filepath.Join(metadir, "snap.yaml")
	content := fmt.Sprintf(`
name: %s
version: %s
%s`, name, version, extraYaml)
	c.Check(ioutil.WriteFile(yamlPath, []byte(content), 0644), check.IsNil)
	c.Check(ioutil.WriteFile(filepath.Join(metadir, "hashes.yaml"), []byte(nil), 0644), check.IsNil)

	err := snappy.SaveManifest(skelInfo)
	c.Assert(err, check.IsNil)

	// indirect through NewInstalledSnap to load extraYaml
	localSnap, err := snappy.NewInstalledSnap(yamlPath)
	c.Assert(err, check.IsNil)

	if active {
		err := snappy.UpdateCurrentSymlink(localSnap, nil)
		c.Assert(err, check.IsNil)
	}

	return localSnap.Info()
}

func (s *apiSuite) mkGadget(c *check.C, store string) {
	content := []byte(fmt.Sprintf(`name: test
version: 1
type: gadget
gadget: {store: {id: %q}}
`, store))

	d := filepath.Join(dirs.SnapSnapsDir, "test")
	m := filepath.Join(d, "1", "meta")
	c.Assert(os.MkdirAll(m, 0755), check.IsNil)
	c.Assert(os.Symlink("1", filepath.Join(d, "current")), check.IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(m, "snap.yaml"), content, 0644), check.IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(m, "hashes.yaml"), []byte(nil), 0644), check.IsNil)
}

func (s *apiSuite) TestSnapInfoOneIntegration(c *check.C) {
	s.daemon(c)
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
			IconURL:           "meta/gui/icon.svg",
			Revision:          20,
		},
	}}

	// we have v0 [r5] installed
	s.mkInstalled(c, "foo", "bar", "v0", 5, false, "")
	// and v1 [r10] is current
	s.mkInstalled(c, "foo", "bar", "v1", 10, true, "")

	rsp, ok := getSnapInfo(snapCmd, nil).(*resp)
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

	expected := &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: map[string]interface{}{
			"name":               "foo",
			"revision":           10,
			"version":            "v1",
			"summary":            "summary",
			"description":        "description",
			"developer":          "bar",
			"status":             "active",
			"icon":               "/v2/icons/foo/icon",
			"type":               string(snap.TypeApp),
			"vendor":             "",
			"download-size":      int64(2),
			"resource":           "/v2/snaps/foo",
			"update-available":   20,
			"rollback-available": 5,
			"channel":            "stable",
		},
	}

	c.Check(rsp, check.DeepEquals, expected)
}

func (s *apiSuite) TestSnapInfoNotFound(c *check.C) {
	s.vars = map[string]string{"name": "foo"}
	s.err = snappy.ErrPackageNotFound

	c.Check(getSnapInfo(snapCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestSnapInfoNoneFound(c *check.C) {
	s.vars = map[string]string{"name": "foo"}

	c.Check(getSnapInfo(snapCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestSnapInfoIgnoresRemoteErrors(c *check.C) {
	s.vars = map[string]string{"name": "foo"}
	s.err = errors.New("weird")

	rsp := getSnapInfo(snapCmd, nil).Self(nil, nil).(*resp)

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
	c.Check(getSnapInfo(wrongCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusInternalServerError)
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

	rsp := getSnapInfo(snapCmd, nil).Self(nil, nil).(*resp)

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
		"apiCompatLevel",
		"api",
		"maxReadBuflen",
		"muxVars",
		"newRemoteRepo",
		"checkSnap",
		"pkgActionDispatch",
		// snapInstruction vars:
		"snapstateInstall",
		"getConfigurator",
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
		Flavor:  "flavor",
		Series:  "release",
		Channel: "channel",
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
		"flavor":          "flavor",
		"release":         "release",
		"default-channel": "channel",
		"api-compat":      apiCompatLevel,
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
		"flavor":          "flavor",
		"release":         "release",
		"default-channel": "channel",
		"api-compat":      apiCompatLevel,
		"store":           "some-store",
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

	rsp := getOpInfo(operationCmd, nil).Self(nil, nil).(*resp)
	buf := bytes.NewBufferString(`{"username": "username", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp = loginUser(snapCmd, req).(*resp)

	expected := loginResponseData{
		Macaroon:   "the-macaroon-serialized-data",
		Discharges: []string{"the-discharge-macaroon-serialized-data"},
	}
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.DeepEquals, expected)

	var auth authState
	expectedState := userAuthState{
		Username:   "username",
		Macaroon:   "the-macaroon-serialized-data",
		Discharges: []string{"the-discharge-macaroon-serialized-data"},
	}
	state := snapCmd.d.overlord.State()
	state.Lock()
	state.Get("auth", &auth)
	state.Unlock()
	c.Check(auth, check.DeepEquals, authState{Users: []userAuthState{expectedState}})
}

func (s *apiSuite) TestLoginUserBadRequest(c *check.C) {
	rsp := getOpInfo(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)

	buf := bytes.NewBufferString(`hello`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp = loginUser(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestLoginUserMyAppsError(c *check.C) {
	mockMyAppsServer := s.makeMyAppsServer(200, "{}")
	defer mockMyAppsServer.Close()

	rsp := getOpInfo(operationCmd, nil).Self(nil, nil).(*resp)
	buf := bytes.NewBufferString(`{"username": "username", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp = loginUser(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusInternalServerError)
	c.Check(rsp.Result.(*errorResult).Message, check.Equals, "cannot get package access macaroon")
}

func (s *apiSuite) TestLoginUserTwoFactorRequiredError(c *check.C) {
	macaroon := `{"macaroon": "the-macaroon-serialized-data"}`
	mockMyAppsServer := s.makeMyAppsServer(200, macaroon)
	defer mockMyAppsServer.Close()

	discharge := `{"code": "TWOFACTOR_REQUIRED"}`
	mockSSOServer := s.makeSSOServer(401, discharge)
	defer mockSSOServer.Close()

	rsp := getOpInfo(operationCmd, nil).Self(nil, nil).(*resp)
	buf := bytes.NewBufferString(`{"username": "username", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp = loginUser(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusUnauthorized)
	c.Check(rsp.Result.(*errorResult).Kind, check.Equals, errorKindTwoFactorRequired)
}

func (s *apiSuite) TestLoginUserInvalidCredentialsError(c *check.C) {
	macaroon := `{"macaroon": "the-macaroon-serialized-data"}`
	mockMyAppsServer := s.makeMyAppsServer(200, macaroon)
	defer mockMyAppsServer.Close()

	discharge := `{"code": "INVALID_CREDENTIALS"}`
	mockSSOServer := s.makeSSOServer(401, discharge)
	defer mockSSOServer.Close()

	rsp := getOpInfo(operationCmd, nil).Self(nil, nil).(*resp)
	buf := bytes.NewBufferString(`{"username": "username", "password": "password"}`)
	req, err := http.NewRequest("POST", "/v2/login", buf)
	c.Assert(err, check.IsNil)

	rsp = loginUser(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusUnauthorized)
	c.Check(rsp.Result.(*errorResult).Message, check.Equals, "cannot get discharge authorization")
}

func (s *apiSuite) TestSnapsInfoOnePerIntegration(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	type tmpSt struct {
		name string
		dev  string
		ver  string
		rev  int
	}

	ddirs := []tmpSt{
		{"foo", "bar", "v1", 5},
		{"bar", "baz", "v2", 10},
		{"baz", "qux", "v3", 15},
		{"qux", "mip", "v4", 20},
	}

	for _, d := range ddirs {
		s.mkInstalled(c, d.name, d.dev, d.ver, d.rev, false, "")
	}

	rsp, ok := getSnapsInfo(snapsCmd, req).(*resp)
	c.Assert(ok, check.Equals, true)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Result, check.NotNil)

	meta, ok := rsp.Result.(map[string]interface{})
	c.Assert(ok, check.Equals, true)
	c.Assert(meta, check.NotNil)
	c.Check(meta["paging"], check.DeepEquals, map[string]interface{}{"pages": 1, "page": 1, "count": len(ddirs)})

	snaps, ok := meta["snaps"].(map[string]map[string]interface{})
	c.Assert(ok, check.Equals, true)
	c.Check(snaps, check.NotNil)
	c.Check(snaps, check.HasLen, len(ddirs))

	for _, s := range ddirs {
		got := snaps[s.name]
		c.Assert(got, check.NotNil, check.Commentf(s.name))
		c.Check(got["name"], check.Equals, s.name)
		c.Check(got["version"], check.Equals, s.ver)
		c.Check(got["revision"], check.Equals, s.rev)
		c.Check(got["developer"], check.Equals, s.dev)
	}
}

func (s *apiSuite) TestSnapsInfoOnlyLocal(c *check.C) {
	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			OfficialName: "store",
			Developer:    "foo",
		},
	}}
	s.mkInstalled(c, "local", "foo", "v1", 10, true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=local", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["sources"], check.DeepEquals, []string{"local"})

	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps["local"], check.NotNil)
}

func (s *apiSuite) TestSnapsInfoOnlyStore(c *check.C) {
	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			OfficialName: "store",
			Developer:    "foo",
		},
	}}
	s.mkInstalled(c, "local", "foo", "v1", 10, true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=store", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["sources"], check.DeepEquals, []string{"store"})

	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps["store"], check.NotNil)
}

func (s *apiSuite) TestSnapsInfoLocalAndStore(c *check.C) {
	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			OfficialName: "remote",
			Developer:    "foo",
		},
	}}
	s.mkInstalled(c, "local", "foo", "v1", 10, true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=local,store", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["sources"], check.DeepEquals, []string{"local", "store"})

	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 2)
}

func (s *apiSuite) TestSnapsInfoDefaultSources(c *check.C) {
	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			OfficialName: "remote",
			Developer:    "foo",
		},
	}}
	s.mkInstalled(c, "local", "foo", "v1", 10, true, "")

	req, err := http.NewRequest("GET", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["sources"], check.DeepEquals, []string{"local", "store"})
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

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["sources"], check.HasLen, 0)

	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 0)
}

func (s *apiSuite) TestSnapsInfoFilterLocal(c *check.C) {
	s.rsnaps = nil
	s.mkInstalled(c, "foo", "foo", "v1", 10, true, "")
	s.mkInstalled(c, "bar", "bar", "v1", 10, true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?q=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["snaps"], check.NotNil)

	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps["foo"], check.NotNil)
}

func (s *apiSuite) TestSnapsInfoFilterRemote(c *check.C) {
	s.rsnaps = nil

	req, err := http.NewRequest("GET", "/v2/snaps?q=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	c.Check(s.searchTerm, check.Equals, "foo")

	result := rsp.Result.(map[string]interface{})
	c.Assert(result["snaps"], check.NotNil)
}

func (s *apiSuite) TestSnapsInfoAppsOnly(c *check.C) {
	s.mkInstalled(c, "app", "foo", "v1", 10, true, "type: app")
	s.mkInstalled(c, "framework", "foo", "v1", 10, true, "type: framework")

	req, err := http.NewRequest("GET", "/v2/snaps?types=app", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps["app"], check.NotNil)
}

func (s *apiSuite) TestSnapsInfoFrameworksOnly(c *check.C) {
	s.mkInstalled(c, "app", "foo", "v1", 10, true, "type: app")
	s.mkInstalled(c, "framework", "foo", "v1", 10, true, "type: framework")

	req, err := http.NewRequest("GET", "/v2/snaps?types=framework", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps["framework"], check.NotNil)
}

func (s *apiSuite) TestSnapsInfoAppsAndFrameworks(c *check.C) {
	s.mkInstalled(c, "app", "foo", "v1", 10, true, "type: app")
	s.mkInstalled(c, "framework", "foo", "v1", 10, true, "type: framework")

	req, err := http.NewRequest("GET", "/v2/snaps?types=app,framework", nil)
	c.Assert(err, check.IsNil)

	rsp := getSnapsInfo(snapsCmd, req).(*resp)

	result := rsp.Result.(map[string]interface{})
	snaps := result["snaps"].(map[string]map[string]interface{})
	c.Assert(snaps, check.HasLen, 2)
}

func (s *apiSuite) TestDeleteOpNotFound(c *check.C) {
	s.vars = map[string]string{"uuid": "42"}
	rsp := deleteOp(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestDeleteOpStillRunning(c *check.C) {
	d := s.daemon(c)

	d.tasks["42"] = &Task{}
	s.vars = map[string]string{"uuid": "42"}
	rsp := deleteOp(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
}

func (s *apiSuite) TestDeleteOp(c *check.C) {
	d := s.daemon(c)

	task := &Task{}
	d.tasks["42"] = task
	task.tomb.Kill(nil)
	s.vars = map[string]string{"uuid": "42"}
	rsp := deleteOp(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, http.StatusOK)
}

func (s *apiSuite) TestGetOpInfoIntegration(c *check.C) {
	d := s.daemon(c)

	s.vars = map[string]string{"uuid": "42"}
	rsp := getOpInfo(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)

	ch := make(chan struct{})

	t := d.AddTask(func() interface{} {
		ch <- struct{}{}
		return "hello"
	})

	id := t.UUID()
	s.vars = map[string]string{"uuid": id}

	rsp = getOpInfo(operationCmd, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.DeepEquals, map[string]interface{}{
		"resource":   "/v2/operations/" + id,
		"status":     TaskRunning,
		"may-cancel": false,
		"created-at": FormatTime(t.CreatedAt()),
		"updated-at": FormatTime(t.UpdatedAt()),
		"output":     nil,
	})
	tf1 := t.UpdatedAt().UTC().UnixNano()

	<-ch
	time.Sleep(time.Millisecond)

	rsp = getOpInfo(operationCmd, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusOK)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.DeepEquals, map[string]interface{}{
		"resource":   "/v2/operations/" + id,
		"status":     TaskSucceeded,
		"may-cancel": false,
		"created-at": FormatTime(t.CreatedAt()),
		"updated-at": FormatTime(t.UpdatedAt()),
		"output":     "hello",
	})

	tf2 := t.UpdatedAt().UTC().UnixNano()

	c.Check(tf1 < tf2, check.Equals, true)
}

func (s *apiSuite) TestPostSnapBadRequest(c *check.C) {
	s.vars = map[string]string{"uuid": "42"}
	rsp := getOpInfo(operationCmd, nil).Self(nil, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusNotFound)

	buf := bytes.NewBufferString(`hello`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp = postSnap(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
	c.Check(rsp.Result, check.NotNil)
}

func (s *apiSuite) TestPostSnapBadAction(c *check.C) {
	s.vars = map[string]string{"uuid": "42"}
	c.Check(getOpInfo(operationCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)

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

	s.vars = map[string]string{"uuid": "42"}
	c.Check(getOpInfo(operationCmd, nil).Self(nil, nil).(*resp).Status, check.Equals, http.StatusNotFound)

	ch := make(chan struct{})

	pkgActionDispatch = func(*snapInstruction) func() interface{} {
		return func() interface{} {
			ch <- struct{}{}
			return "hi"
		}
	}
	defer func() {
		pkgActionDispatch = pkgActionDispatchImpl
	}()

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := postSnap(snapCmd, req).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)
	m := rsp.Result.(map[string]interface{})
	c.Assert(m["resource"], check.Matches, "/v2/operations/.*")

	uuid := m["resource"].(string)[len("/v2/operations/"):]

	task := d.GetTask(uuid)
	c.Assert(task, check.NotNil)

	c.Check(task.State(), check.Equals, TaskRunning)

	<-ch
	time.Sleep(time.Millisecond)

	task = d.GetTask(uuid)
	c.Assert(task, check.NotNil)
	c.Check(task.State(), check.Equals, TaskSucceeded)
	c.Check(task.Output(), check.Equals, "hi")
}

func (s *apiSuite) TestPostSnapDispatch(c *check.C) {
	inst := &snapInstruction{}

	type T struct {
		s string
		m func() interface{}
	}

	actions := []T{
		{"install", inst.install},
		{"update", inst.update},
		{"remove", inst.remove},
		{"rollback", inst.rollback},
		{"xyzzy", nil},
	}

	for _, action := range actions {
		inst.Action = action.s
		// do you feel dirty yet?
		c.Check(fmt.Sprintf("%p", action.m), check.Equals, fmt.Sprintf("%p", inst.dispatch()))
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

func (s *apiSuite) TestSnapGetConfig(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/snaps/foo/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	getConfigurator = func() configurator {
		return s.overlord
	}

	configStr := "some: config"
	s.overlord.configs["foo"] = configStr
	s.vars = map[string]string{"name": "foo"}
	s.mkInstalled(c, "foo", "bar", "v1", 10, true, "")

	rsp := snapConfig(snapsCmd, req).(*resp)

	c.Check(rsp, check.DeepEquals, &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: configStr,
	})
}

func (s *apiSuite) TestSnapGetConfigMissing(c *check.C) {
	s.vars = map[string]string{"name": "foo"}

	req, err := http.NewRequest("GET", "/v2/snaps/foo/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := snapConfig(snapsCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestSnapGetConfigInactive(c *check.C) {
	s.vars = map[string]string{"name": "foo"}

	s.mkInstalled(c, "foo", "bar", "v1", 10, false, "")

	req, err := http.NewRequest("GET", "/v2/snaps/foo/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := snapConfig(snapsCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
}

func (s *apiSuite) TestSnapGetConfigNoConfig(c *check.C) {
	s.vars = map[string]string{"name": "foo"}
	getConfigurator = func() configurator {
		return s.overlord
	}

	s.mkInstalled(c, "foo", "bar", "v1", 10, true, "")
	req, err := http.NewRequest("GET", "/v2/snaps/foo/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := snapConfig(snapsCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusInternalServerError)
}

//FIXME: renenable config for GA
/*
func (s *apiSuite) TestSnapPutConfig(c *check.C) {
	newConfigStr := "some other config"
	req, err := http.NewRequest("PUT", "/v2/snaps/foo/config", bytes.NewBufferString(newConfigStr))
	c.Assert(err, check.IsNil)

	//configStr := "some: config"
	getConfigurator = func() configurator {
		return s.overlord
	}

	s.vars = map[string]string{"name": "foo"}
	s.mkInstalled(c, "foo", "bar", "v1", 10, true, "")

	rsp := snapConfig(snapConfigCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp, check.DeepEquals, &resp{
		Type:   ResponseTypeSync,
		Status: http.StatusOK,
		Result: newConfigStr,
	})
}
*/

func (s *apiSuite) TestSnapPutConfigMissing(c *check.C) {
	s.vars = map[string]string{"name": "foo"}

	req, err := http.NewRequest("PUT", "/v2/snaps/foo/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := snapConfig(snapsCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusNotFound)
}

func (s *apiSuite) TestSnapPutConfigInactive(c *check.C) {
	s.vars = map[string]string{"name": "foo"}

	s.mkInstalled(c, "foo", "bar", "v1", 10, false, "")

	req, err := http.NewRequest("PUT", "/v2/snaps/foo/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := snapConfig(snapsCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusBadRequest)
}

func (s *apiSuite) TestSnapPutConfigNoConfig(c *check.C) {
	s.vars = map[string]string{"name": "foo"}

	s.mkInstalled(c, "foo", "bar", "v1", 10, true, "")

	req, err := http.NewRequest("PUT", "/v2/snaps/foo/config", bytes.NewBuffer(nil))
	c.Assert(err, check.IsNil)

	rsp := snapConfig(snapsCmd, req).Self(nil, nil).(*resp)

	c.Check(rsp.Status, check.Equals, http.StatusInternalServerError)
}

func (s *apiSuite) TestSideloadSnap(c *check.C) {
	// try a direct upload, with no x-allow-unsigned header
	s.sideloadCheck(c, "xyzzy", false, nil)
	// try a direct upload *with* an x-allow-unsigned header
	s.sideloadCheck(c, "xyzzy", true, map[string]string{"X-Allow-Unsigned": "Very Yes"})
	// try a multipart/form-data upload without allow-unsigned
	s.sideloadCheck(c, "----hello--\r\nContent-Disposition: form-data; name=\"x\"; filename=\"x\"\r\n\r\nxyzzy\r\n----hello----\r\n", false, map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"})
	// and one *with* allow-unsigned
	s.sideloadCheck(c, "----hello--\r\nContent-Disposition: form-data; name=\"unsigned-ok\"\r\n\r\n----hello--\r\nContent-Disposition: form-data; name=\"x\"; filename=\"x\"\r\n\r\nxyzzy\r\n----hello----\r\n", false, map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"})
}

func (s *apiSuite) sideloadCheck(c *check.C, content string, unsignedExpected bool, head map[string]string) {
	ch := make(chan struct{})
	tmpfile, err := ioutil.TempFile("", "test-")
	c.Assert(err, check.IsNil)
	_, err = tmpfile.WriteString(content)
	c.Check(err, check.IsNil)
	_, err = tmpfile.Seek(0, 0)
	c.Check(err, check.IsNil)

	// setup done
	var expectedFlags snappy.InstallFlags
	if unsignedExpected {
		expectedFlags |= snappy.AllowUnauthenticated
	}

	checkSnap = func(fn string, flags snappy.InstallFlags) error {
		c.Check(flags, check.Equals, expectedFlags)

		bs, err := ioutil.ReadFile(fn)
		c.Check(err, check.IsNil)
		c.Check(string(bs), check.Equals, "xyzzy")

		ch <- struct{}{}

		return nil
	}
	defer func() { checkSnap = checkSnapImpl }()

	req, err := http.NewRequest("POST", "/v2/snaps", tmpfile)
	c.Assert(err, check.IsNil)
	for k, v := range head {
		req.Header.Set(k, v)
	}

	rsp := sideloadSnap(snapsCmd, req).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeAsync)

	<-ch
}

func (s *apiSuite) TestAppIconGet(c *check.C) {
	// have an active foo in the system
	info := s.mkInstalled(c, "foo", "bar", "v1", 10, true, "")

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
	// have an *in*active foo in the system
	info := s.mkInstalled(c, "foo", "bar", "v1", 10, false, "")

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
	// have an *in*active foo in the system
	info := s.mkInstalled(c, "foo", "bar", "v1", 10, true, "")

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

	snapstateInstall = func(s *state.State, name, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		overlord: d.overlord,
		Action:   "install",
	}

	d.overlord.Loop()
	defer d.overlord.Stop()
	err := inst.dispatch()()

	c.Check(calledFlags, check.Equals, snappy.DoInstallGC)
	c.Check(err, check.IsNil)
}

func (s *apiSuite) TestInstallFails(c *check.C) {
	snapstateInstall = func(s *state.State, name, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
		t := s.NewTask("fake-install-snap-error", "Install task")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		overlord: d.overlord,
		Action:   "install",
	}

	d.overlord.Loop()
	defer d.overlord.Stop()
	err := inst.dispatch()()

	c.Check(err, check.ErrorMatches, `(?sm).*Install task \(fake-install-snap-error errored\)`)
}

func (s *apiSuite) TestInstallLeaveOld(c *check.C) {
	calledFlags := snappy.InstallFlags(42)

	snapstateInstall = func(s *state.State, name, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	}

	d := s.daemon(c)
	inst := &snapInstruction{
		overlord: d.overlord,
		Action:   "install",
		LeaveOld: true,
	}

	d.overlord.Loop()
	defer d.overlord.Stop()
	err := inst.dispatch()()

	c.Check(calledFlags, check.Equals, snappy.InstallFlags(0))
	c.Check(err, check.IsNil)
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
		overlord: d.overlord,
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

func makePlug(ifaceName string) *interfaces.Plug {
	snapInfo := &snap.Info{
		SuggestedName: "producer",
	}
	plugInfo := &snap.PlugInfo{
		Snap:      snapInfo,
		Name:      "plug",
		Interface: ifaceName,
		Attrs:     map[string]interface{}{"key": "value"},
		Label:     "label",
	}
	appInfo := &snap.AppInfo{
		Snap:  snapInfo,
		Name:  "app",
		Plugs: map[string]*snap.PlugInfo{"plug": plugInfo},
	}
	snapInfo.Apps = map[string]*snap.AppInfo{"app": appInfo}
	plugInfo.Apps = snapInfo.Apps
	snapInfo.Plugs = map[string]*snap.PlugInfo{"plug": plugInfo}
	return &interfaces.Plug{PlugInfo: plugInfo}
}

func makeConnectedPlug() *interfaces.Plug {
	plug := makePlug("interface")
	plug.Connections = []interfaces.SlotRef{{Snap: "consumer", Name: "slot"}}
	return plug
}

func makeSlot(ifaceName string) *interfaces.Slot {
	snapInfo := &snap.Info{
		SuggestedName: "consumer",
	}
	slotInfo := &snap.SlotInfo{
		Snap:      snapInfo,
		Name:      "slot",
		Interface: ifaceName,
		Attrs:     map[string]interface{}{"key": "value"},
		Label:     "label",
	}
	appInfo := &snap.AppInfo{
		Snap:  snapInfo,
		Name:  "app",
		Slots: map[string]*snap.SlotInfo{"slot": slotInfo},
	}
	snapInfo.Apps = map[string]*snap.AppInfo{"app": appInfo}
	slotInfo.Apps = snapInfo.Apps
	snapInfo.Slots = map[string]*snap.SlotInfo{"slot": slotInfo}
	return &interfaces.Slot{SlotInfo: slotInfo}
}

func makeConnectedSlot() *interfaces.Slot {
	slot := makeSlot("interface")
	slot.Connections = []interfaces.PlugRef{{Snap: "producer", Name: "plug"}}
	return slot
}

func (s *apiSuite) TestGetPlugs(c *check.C) {
	d := s.daemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(makePlug("interface"))
	d.interfaces.AddSlot(makeSlot("interface"))
	d.interfaces.Connect("producer", "plug", "consumer", "slot")
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
					"snap":      "producer",
					"plug":      "plug",
					"interface": "interface",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
					"connections": []interface{}{
						map[string]interface{}{"snap": "consumer", "slot": "slot"},
					},
				},
			},
			"slots": []interface{}{
				map[string]interface{}{
					"snap":      "consumer",
					"slot":      "slot",
					"interface": "interface",
					"attrs":     map[string]interface{}{"key": "value"},
					"apps":      []interface{}{"app"},
					"label":     "label",
					"connections": []interface{}{
						map[string]interface{}{"snap": "producer", "plug": "plug"},
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
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(makePlug("interface"))
	d.interfaces.AddSlot(makeSlot("interface"))
	action := &interfaceAction{
		Action: "connect",
		Plugs:  []plugJSON{{Snap: "producer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "consumer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result":      nil,
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Plugs: []*interfaces.Plug{makeConnectedPlug()},
		Slots: []*interfaces.Slot{makeConnectedSlot()},
	})
}

func (s *apiSuite) TestConnectPlugFailureInterfaceMismatch(c *check.C) {
	d := s.daemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "other-interface"})
	d.interfaces.AddPlug(makePlug("interface"))
	d.interfaces.AddSlot(makeSlot("other-interface"))
	action := &interfaceAction{
		Action: "connect",
		Plugs:  []plugJSON{{Snap: "producer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "consumer", Name: "slot"}},
	}
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
			"message": `cannot connect plug "producer:plug" (interface "interface") to "consumer:slot" (interface "other-interface")`,
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Plugs: []*interfaces.Plug{makePlug("interface")},
		Slots: []*interfaces.Slot{makeSlot("other-interface")},
	})
}

func (s *apiSuite) TestConnectPlugFailureNoSuchPlug(c *check.C) {
	d := s.daemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddSlot(makeSlot("interface"))
	action := &interfaceAction{
		Action: "connect",
		Plugs:  []plugJSON{{Snap: "producer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "consumer", Name: "slot"}},
	}
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
			"message": `cannot connect plug "plug" from snap "producer", no such plug`,
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Slots: []*interfaces.Slot{makeSlot("interface")},
	})
}

func (s *apiSuite) TestConnectPlugFailureNoSuchSlot(c *check.C) {
	d := s.daemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(makePlug("interface"))
	action := &interfaceAction{
		Action: "connect",
		Plugs:  []plugJSON{{Snap: "producer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "consumer", Name: "slot"}},
	}
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
			"message": `cannot connect plug to slot "slot" from snap "consumer", no such slot`,
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Plugs: []*interfaces.Plug{makePlug("interface")},
	})
}

func (s *apiSuite) TestDisconnectPlugSuccess(c *check.C) {
	d := s.daemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(makePlug("interface"))
	d.interfaces.AddSlot(makeSlot("interface"))
	d.interfaces.Connect("producer", "plug", "consumer", "slot")
	action := &interfaceAction{
		Action: "disconnect",
		Plugs:  []plugJSON{{Snap: "producer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "consumer", Name: "slot"}},
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/interfaces", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	interfacesCmd.POST(interfacesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"result":      nil,
		"status":      "OK",
		"status-code": 200.0,
		"type":        "sync",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Plugs: []*interfaces.Plug{makePlug("interface")},
		Slots: []*interfaces.Slot{makeSlot("interface")},
	})
}

func (s *apiSuite) TestDisconnectPlugFailureNoSuchPlug(c *check.C) {
	d := s.daemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddSlot(makeSlot("interface"))
	action := &interfaceAction{
		Action: "disconnect",
		Plugs:  []plugJSON{{Snap: "producer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "consumer", Name: "slot"}},
	}
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
			"message": `cannot disconnect plug "plug" from snap "producer", no such plug`,
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Slots: []*interfaces.Slot{makeSlot("interface")},
	})
}

func (s *apiSuite) TestDisconnectPlugFailureNoSuchSlot(c *check.C) {
	d := s.daemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(makePlug("interface"))
	action := &interfaceAction{
		Action: "disconnect",
		Plugs:  []plugJSON{{Snap: "producer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "consumer", Name: "slot"}},
	}
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
			"message": `cannot disconnect plug from slot "slot" from snap "consumer", no such slot`,
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Plugs: []*interfaces.Plug{makePlug("interface")},
	})
}

func (s *apiSuite) TestDisconnectPlugFailureNotConnected(c *check.C) {
	d := s.daemon(c)
	d.interfaces.AddInterface(&interfaces.TestInterface{InterfaceName: "interface"})
	d.interfaces.AddPlug(makePlug("interface"))
	d.interfaces.AddSlot(makeSlot("interface"))
	action := &interfaceAction{
		Action: "disconnect",
		Plugs:  []plugJSON{{Snap: "producer", Name: "plug"}},
		Slots:  []slotJSON{{Snap: "consumer", Name: "slot"}},
	}
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
			"message": `cannot disconnect plug "plug" from snap "producer" from slot "slot" from snap "consumer", it is not connected`,
		},
		"status":      "Bad Request",
		"status-code": 400.0,
		"type":        "error",
	})
	c.Assert(d.interfaces.Interfaces(), check.DeepEquals, &interfaces.Interfaces{
		Plugs: []*interfaces.Plug{makePlug("interface")},
		Slots: []*interfaces.Slot{makeSlot("interface")},
	})
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

func setupChanges(st *state.State) {
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
}

func (s *apiSuite) TestStateChangesDefaultToInProgress(c *check.C) {
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

	c.Check(string(res), testutil.Contains, `{"kind":"install","summary":"install...","status":"Do","tasks":[{"kind":"download","summary":"1...","status":"Do","log":["INFO: l11","INFO: l12"]}`)
}

func (s *apiSuite) TestStateChangesInProgress(c *check.C) {
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

	c.Check(string(res), testutil.Contains, `{"kind":"install","summary":"install...","status":"Do","tasks":[{"kind":"download","summary":"1...","status":"Do","log":["INFO: l11","INFO: l12"]}`)
}

func (s *apiSuite) TestStateChangesAll(c *check.C) {
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

	c.Check(string(res), testutil.Contains, `{"kind":"install","summary":"install...","status":"Do","tasks":[{"kind":"download","summary":"1...","status":"Do","log":["INFO: l11","INFO: l12"]}`)
	c.Check(string(res), testutil.Contains, `{"kind":"remove","summary":"remove..","status":"Error","tasks":[{"kind":"unlink","summary":"1...","status":"Error","log":["ERROR: rm failed"]}]}`)
}

func (s *apiSuite) TestStateChangesReady(c *check.C) {
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

	c.Check(string(res), testutil.Contains, `{"kind":"remove","summary":"remove..","status":"Error","tasks":[{"kind":"unlink","summary":"1...","status":"Error","log":["ERROR: rm failed"]}]}`)
}
