// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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

package daemon_test

import (
	"context"
	"crypto"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/sha3"
	"gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

// TODO: as we split api_test.go and move more tests to live in daemon_test
// instead of daemon, split out functionality from APIBaseSuite
// to only the relevant suite when possible

type apiBaseSuite struct {
	testutil.BaseTest

	storetest.Store

	rsnaps            []*snap.Info
	err               error
	vars              map[string]string
	storeSearch       store.Search
	suggestedCurrency string
	d                 *daemon.Daemon
	user              *auth.UserState
	ctx               context.Context
	currentSnaps      []*store.CurrentSnap
	actions           []*store.SnapAction

	restoreRelease func()

	StoreSigning *assertstest.StoreStack
	Brands       *assertstest.SigningAccounts

	systemctlRestorer func()
	SysctlBufs        [][]byte

	connectivityResult map[string]bool

	restoreSanitize func()
	restoreMuxVars  func()

	authUser *auth.UserState

	expectedReadAccess  daemon.AccessChecker
	expectedWriteAccess daemon.AccessChecker
}

func (s *apiBaseSuite) pokeStateLock() {
	// the store should be called without the state lock held. Try
	// to acquire it.
	st := s.d.Overlord().State()
	st.Lock()
	st.Unlock()
}

func (s *apiBaseSuite) SnapInfo(ctx context.Context, spec store.SnapSpec, user *auth.UserState) (*snap.Info, error) {
	s.pokeStateLock()
	s.user = user
	s.ctx = ctx
	if len(s.rsnaps) > 0 {
		return s.rsnaps[0], s.err
	}
	return nil, s.err
}

func (s *apiBaseSuite) Find(ctx context.Context, search *store.Search, user *auth.UserState) ([]*snap.Info, error) {
	s.pokeStateLock()

	s.storeSearch = *search
	s.user = user
	s.ctx = ctx

	return s.rsnaps, s.err
}

func (s *apiBaseSuite) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	s.pokeStateLock()
	if assertQuery != nil {
		toResolve, toResolveSeq, err := assertQuery.ToResolve()
		if err != nil {
			return nil, nil, err
		}
		if len(toResolve) != 0 || len(toResolveSeq) != 0 {
			panic("no assertion query support")
		}
	}

	if ctx == nil {
		panic("context required")
	}
	s.currentSnaps = currentSnaps
	s.actions = actions
	s.user = user

	sars := make([]store.SnapActionResult, len(s.rsnaps))
	for i, rsnap := range s.rsnaps {
		sars[i] = store.SnapActionResult{Info: rsnap}
	}
	return sars, nil, s.err
}

func (s *apiBaseSuite) SuggestedCurrency() string {
	s.pokeStateLock()

	return s.suggestedCurrency
}

func (s *apiBaseSuite) ConnectivityCheck() (map[string]bool, error) {
	s.pokeStateLock()

	return s.connectivityResult, s.err
}

func (s *apiBaseSuite) muxVars(*http.Request) map[string]string {
	return s.vars
}

func (s *apiBaseSuite) SetUpSuite(c *check.C) {
	s.restoreMuxVars = daemon.MockMuxVars(s.muxVars)
	s.restoreRelease = sandbox.MockForceDevMode(false)
	s.systemctlRestorer = systemd.MockSystemctl(s.systemctl)
	s.restoreSanitize = snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
}

func (s *apiBaseSuite) TearDownSuite(c *check.C) {
	s.restoreMuxVars()
	s.restoreRelease()
	s.systemctlRestorer()
	s.restoreSanitize()
}

func (s *apiBaseSuite) systemctl(args ...string) (buf []byte, err error) {
	if len(s.SysctlBufs) > 0 {
		buf, s.SysctlBufs = s.SysctlBufs[0], s.SysctlBufs[1:]
	}
	return buf, err
}

var (
	brandPrivKey, _ = assertstest.GenerateKey(752)
)

func (s *apiBaseSuite) SetUpTest(c *check.C) {
	s.BaseTest.SetUpTest(c)

	ctlcmds := testutil.MockCommand(c, "systemctl", "").Also("journalctl", "")
	s.AddCleanup(ctlcmds.Restore)

	s.SysctlBufs = nil

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	restore := osutil.MockMountInfo("")
	s.AddCleanup(restore)

	c.Assert(err, check.IsNil)
	c.Assert(os.MkdirAll(dirs.SnapMountDir, 0755), check.IsNil)
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0755), check.IsNil)

	s.rsnaps = nil
	s.suggestedCurrency = ""
	s.storeSearch = store.Search{}
	s.err = nil
	s.vars = nil
	s.user = nil
	s.d = nil
	s.ctx = nil
	s.currentSnaps = nil
	s.actions = nil
	s.authUser = nil

	// TODO: consider making the default ReadAccess expectation
	// authenticatedAccess, but that would need even more test changes
	s.expectedReadAccess = daemon.OpenAccess{}
	s.expectedWriteAccess = daemon.AuthenticatedAccess{}

	// Disable real security backends for all API tests
	s.AddCleanup(ifacestate.MockSecurityBackends(nil))

	s.StoreSigning = assertstest.NewStoreStack("can0nical", nil)
	s.AddCleanup(sysdb.InjectTrusted(s.StoreSigning.Trusted))

	s.Brands = assertstest.NewSigningAccounts(s.StoreSigning)
	s.Brands.Register("my-brand", brandPrivKey, nil)

	s.AddCleanup(daemon.MockSystemUserFromRequest(func(r *http.Request) (*user.User, error) {
		if s.authUser != nil {
			return &user.User{
				Uid:      "1337",
				Gid:      "42",
				Username: s.authUser.Username,
				Name:     s.authUser.Username,
				HomeDir:  "",
			}, nil
		}
		return &user.User{
			Uid:      "0",
			Gid:      "0",
			Username: "root",
			Name:     "root",
			HomeDir:  "",
		}, nil
	}))
}

func (s *apiBaseSuite) mockModel(st *state.State, model *asserts.Model) {
	// realistic model setup
	if model == nil {
		model = s.Brands.Model("can0nical", "pc", map[string]interface{}{
			"architecture": "amd64",
			"gadget":       "gadget",
			"kernel":       "kernel",
		})
	}

	snapstate.DeviceCtx = devicestate.DeviceCtx

	assertstatetest.AddMany(st, model)

	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  model.BrandID(),
		Model:  model.Model(),
		Serial: "serialserial",
	})
}

func (s *apiBaseSuite) daemonWithStore(c *check.C, sto snapstate.StoreService) *daemon.Daemon {
	if s.d != nil {
		panic("called daemon*() twice")
	}
	d, err := daemon.NewAndAddRoutes()
	c.Assert(err, check.IsNil)

	st := d.Overlord().State()
	// mark as already seeded
	st.Lock()
	st.Set("seeded", true)
	// and registered
	s.mockModel(st, nil)
	st.Unlock()
	c.Assert(d.Overlord().StartUp(), check.IsNil)

	st.Lock()
	defer st.Unlock()
	snapstate.ReplaceStore(st, sto)

	// don't actually try to talk to the store on snapstate.Ensure
	// needs doing after the call to devicestate.Manager (which
	// happens in daemon.New via overlord.New)
	snapstate.CanAutoRefresh = nil

	s.d = d
	return d
}

func (s *apiBaseSuite) resetDaemon() {
	if s.d != nil {
		s.d.Overlord().Stop()
	}
	s.d = nil
}

func (s *apiBaseSuite) daemon(c *check.C) *daemon.Daemon {
	return s.daemonWithStore(c, s)
}

func (s *apiBaseSuite) daemonWithOverlordMock() *daemon.Daemon {
	if s.d != nil {
		panic("called daemon*() twice")
	}

	o := overlord.Mock()
	s.d = daemon.NewWithOverlord(o)
	return s.d
}

func (s *apiBaseSuite) daemonWithOverlordMockAndStore() *daemon.Daemon {
	if s.d != nil {
		panic("called daemon*() twice")
	}

	o := overlord.Mock()
	d := daemon.NewWithOverlord(o)

	st := d.Overlord().State()
	// adds an assertion db
	assertstate.Manager(st, o.TaskRunner())
	st.Lock()
	defer st.Unlock()
	snapstate.ReplaceStore(st, s)

	s.d = d
	return d
}

// asUserAuth fakes authorization into the request as for root
func (s *apiBaseSuite) asRootAuth(req *http.Request) {
	req.RemoteAddr = fmt.Sprintf("pid=100;uid=0;socket=%s;", dirs.SnapdSocket)
}

// asUserAuth adds authorization to the request as for a logged in user
func (s *apiBaseSuite) asUserAuth(c *check.C, req *http.Request) {
	if s.d == nil {
		panic("call s.daemon(c) etc in your test first")
	}
	if s.authUser == nil {
		st := s.d.Overlord().State()
		st.Lock()
		u, err := auth.NewUser(st, auth.NewUserParams{
			Username:   "username",
			Email:      "email@test.com",
			Macaroon:   "macaroon",
			Discharges: []string{"discharge"},
		})
		st.Unlock()
		c.Assert(err, check.IsNil)
		s.authUser = u
	}
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, s.authUser.Macaroon))
	req.RemoteAddr = fmt.Sprintf("pid=100;uid=1000;socket=%s;", dirs.SnapdSocket)
}

type fakeSnapManager struct{}

func newFakeSnapManager(st *state.State, runner *state.TaskRunner) *fakeSnapManager {
	runner.AddHandler("fake-install-snap", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)
	runner.AddHandler("fake-install-component", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)
	runner.AddHandler("fake-install-snap-error", func(t *state.Task, _ *tomb.Tomb) error {
		return fmt.Errorf("fake-install-snap-error errored")
	}, nil)

	return &fakeSnapManager{}
}

func (m *fakeSnapManager) Ensure() error {
	return nil
}

// expected interface is implemented
var _ overlord.StateManager = (*fakeSnapManager)(nil)

func (s *apiBaseSuite) daemonWithFakeSnapManager(c *check.C) *daemon.Daemon {
	d := s.daemonWithOverlordMockAndStore()
	st := d.Overlord().State()
	runner := d.Overlord().TaskRunner()
	d.Overlord().AddManager(newFakeSnapManager(st, runner))
	d.Overlord().AddManager(runner)
	c.Assert(d.Overlord().StartUp(), check.IsNil)
	return d
}

func (s *apiBaseSuite) waitTrivialChange(c *check.C, chg *state.Change) {
	err := s.d.Overlord().Settle(5 * time.Second)
	c.Assert(err, check.IsNil)
	c.Assert(chg.IsReady(), check.Equals, true)
}

func (s *apiBaseSuite) mkInstalledDesktopFile(c *check.C, name, content string) string {
	df := filepath.Join(dirs.SnapDesktopFilesDir, name)
	err := os.MkdirAll(filepath.Dir(df), 0755)
	c.Assert(err, check.IsNil)
	err = os.WriteFile(df, []byte(content), 0644)
	c.Assert(err, check.IsNil)
	return df
}

func (s *apiBaseSuite) mockSnap(c *check.C, yamlText string) *snap.Info {
	if s.d == nil {
		panic("call s.daemon(c) etc in your test first")
	}

	appSet := ifacetest.MockSnapAndAppSet(c, yamlText, nil, &snap.SideInfo{Revision: snap.R(1)})
	snapInfo := appSet.Info()

	st := s.d.Overlord().State()

	st.Lock()
	defer st.Unlock()

	// Put a side info into the state
	snapstate.Set(st, snapInfo.InstanceName(), &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{
				RealName: snapInfo.SnapName(),
				Revision: snapInfo.Revision,
				SnapID:   "ididid",
			},
		}),
		Current:  snapInfo.Revision,
		SnapType: string(snapInfo.Type()),
	})

	// Put the snap into the interface repository
	repo := s.d.Overlord().InterfaceManager().Repository()
	err := repo.AddAppSet(appSet)
	c.Assert(err, check.IsNil)
	return snapInfo
}

func (s *apiBaseSuite) mkInstalledInState(c *check.C, d *daemon.Daemon, instanceName, developer, version string, revision snap.Revision, active bool, extraYaml string) *snap.Info {
	snapName, instanceKey := snap.SplitInstanceName(instanceName)

	if revision.Local() && developer != "" {
		panic("not supported")
	}

	var snapID string
	if revision.Store() {
		snapID = snapName + "-id"
	}
	// Collect arguments into a snap.SideInfo structure
	sideInfo := &snap.SideInfo{
		SnapID:   snapID,
		RealName: snapName,
		Revision: revision,
		Channel:  "stable",
	}

	// Collect other arguments into a yaml string
	yamlText := fmt.Sprintf(`
name: %s
version: %s
%s`, snapName, version, extraYaml)

	// Mock the snap on disk
	snapInfo := snaptest.MockSnapInstance(c, instanceName, yamlText, sideInfo)
	if active {
		dir, rev := filepath.Split(snapInfo.MountDir())
		c.Assert(os.Symlink(rev, dir+"current"), check.IsNil)
	}
	c.Assert(snapInfo.InstanceName(), check.Equals, instanceName)

	c.Assert(os.MkdirAll(snapInfo.DataDir(), 0755), check.IsNil)
	metadir := filepath.Join(snapInfo.MountDir(), "meta")
	guidir := filepath.Join(metadir, "gui")
	c.Assert(os.MkdirAll(guidir, 0755), check.IsNil)
	c.Check(os.WriteFile(filepath.Join(guidir, "icon.svg"), []byte("yadda icon"), 0644), check.IsNil)

	if d == nil {
		return snapInfo
	}
	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	snapstate.Get(st, instanceName, &snapst)
	snapst.Active = active
	snapst.Sequence.Revisions = append(snapst.Sequence.Revisions, sequence.NewRevisionSideState(&snapInfo.SideInfo, nil))
	snapst.Current = snapInfo.SideInfo.Revision
	snapst.TrackingChannel = "stable"
	snapst.InstanceKey = instanceKey

	snapstate.Set(st, instanceName, &snapst)

	if developer == "" {
		return snapInfo
	}

	devAcct := assertstest.NewAccount(s.StoreSigning, developer, map[string]interface{}{
		"account-id": developer + "-id",
	}, "")

	snapInfo.Publisher = snap.StoreAccount{
		ID:          devAcct.AccountID(),
		Username:    devAcct.Username(),
		DisplayName: devAcct.DisplayName(),
		Validation:  devAcct.Validation(),
	}

	snapDecl, err := s.StoreSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      snapID,
		"snap-name":    snapName,
		"publisher-id": devAcct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)

	content, err := os.ReadFile(snapInfo.MountFile())
	c.Assert(err, check.IsNil)
	h := sha3.Sum384(content)
	dgst, err := asserts.EncodeDigest(crypto.SHA3_384, h[:])
	c.Assert(err, check.IsNil)
	snapRev, err := s.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": string(dgst),
		"snap-size":     "999",
		"snap-id":       snapID,
		"snap-revision": revision.String(), // this must be a string
		"developer-id":  devAcct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)

	assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""), devAcct, snapDecl, snapRev)

	return snapInfo
}

func handlerCommand(c *check.C, d *daemon.Daemon, req *http.Request) (cmd *daemon.Command, vars map[string]string) {
	m := &mux.RouteMatch{}
	if !d.RouterMatch(req, m) {
		c.Fatalf("no command for URL %q", req.URL)
	}
	cmd, ok := m.Handler.(*daemon.Command)
	if !ok {
		c.Fatalf("no command for URL %q", req.URL)
	}
	return cmd, m.Vars
}

func (s *apiBaseSuite) checkGetOnly(c *check.C, req *http.Request) {
	if s.d == nil {
		panic("call s.daemon(c) etc in your test first")
	}

	cmd, _ := handlerCommand(c, s.d, req)
	c.Check(cmd.POST, check.IsNil)
	c.Check(cmd.PUT, check.IsNil)
	c.Check(cmd.GET, check.NotNil)
}

func (s *apiBaseSuite) expectOpenAccess() {
	s.expectedReadAccess = daemon.OpenAccess{}
}

func (s *apiBaseSuite) expectRootAccess() {
	s.expectedReadAccess = daemon.RootAccess{}
	s.expectedWriteAccess = daemon.RootAccess{}
}

func (s *apiBaseSuite) expectAuthenticatedAccess() {
	s.expectedReadAccess = daemon.AuthenticatedAccess{}
	s.expectedWriteAccess = daemon.AuthenticatedAccess{}
}

func (s *apiBaseSuite) expectReadAccess(a daemon.AccessChecker) {
	s.expectedReadAccess = a
}

func (s *apiBaseSuite) expectWriteAccess(a daemon.AccessChecker) {
	s.expectedWriteAccess = a
}

func (s *apiBaseSuite) req(c *check.C, req *http.Request, u *auth.UserState) daemon.Response {
	if s.d == nil {
		panic("call s.daemon(c) etc in your test first")
	}

	cmd, vars := handlerCommand(c, s.d, req)
	s.vars = vars
	var f daemon.ResponseFunc
	var acc, expAcc daemon.AccessChecker
	var whichAcc string
	switch req.Method {
	case "GET":
		f = cmd.GET
		acc = cmd.ReadAccess
		expAcc = s.expectedReadAccess
		whichAcc = "ReadAccess"
	case "POST":
		f = cmd.POST
		acc = cmd.WriteAccess
		expAcc = s.expectedWriteAccess
		whichAcc = "WriteAccess"
	case "PUT":
		f = cmd.PUT
		acc = cmd.WriteAccess
		expAcc = s.expectedWriteAccess
		whichAcc = "WriteAccess"
	default:
		c.Fatalf("unsupported HTTP method %q", req.Method)
	}
	if f == nil {
		c.Fatalf("no support for %q for %q", req.Method, req.URL)
	}
	c.Check(acc, check.DeepEquals, expAcc, check.Commentf("expected %s check mismatch, use the apiBaseSuite.expect*Access methods to match the appropriate access check for the API under test", whichAcc))
	return f(cmd, req, u)
}

func (s *apiBaseSuite) jsonReq(c *check.C, req *http.Request, u *auth.UserState) *daemon.RespJSON {
	rsp, ok := s.req(c, req, u).(daemon.StructuredResponse)
	c.Assert(ok, check.Equals, true, check.Commentf("expected structured response"))
	return rsp.JSON()
}

func (s *apiBaseSuite) syncReq(c *check.C, req *http.Request, u *auth.UserState) *daemon.RespJSON {
	rsp := s.jsonReq(c, req, u)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeSync, check.Commentf("expected sync resp: %#v, result: %+v", rsp, rsp.Result))
	return rsp
}

func (s *apiBaseSuite) asyncReq(c *check.C, req *http.Request, u *auth.UserState) *daemon.RespJSON {
	rsp := s.jsonReq(c, req, u)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeAsync, check.Commentf("expected async resp: %#v, result %v", rsp, rsp.Result))
	return rsp
}

func (s *apiBaseSuite) errorReq(c *check.C, req *http.Request, u *auth.UserState) *daemon.APIError {
	rsp := s.req(c, req, u)
	rspe, ok := rsp.(*daemon.APIError)
	c.Assert(ok, check.Equals, true, check.Commentf("expected apiError resp: %#v", rsp))
	return rspe
}

func (s *apiBaseSuite) serveHTTP(c *check.C, w http.ResponseWriter, req *http.Request) {
	if s.d == nil {
		panic("call s.daemon(c) etc in your test first")
	}

	cmd, vars := handlerCommand(c, s.d, req)
	s.vars = vars

	cmd.ServeHTTP(w, req)
}

func (s *apiBaseSuite) simulateConflict(name string) {
	if s.d == nil {
		panic("call s.daemon(c) etc in your test first")
	}

	o := s.d.Overlord()
	st := o.State()
	st.Lock()
	defer st.Unlock()
	t := st.NewTask("link-snap", "...")
	snapsup := &snapstate.SnapSetup{SideInfo: &snap.SideInfo{
		RealName: name,
	}}
	t.Set("snap-setup", snapsup)
	chg := st.NewChange("manip", "...")
	chg.AddTask(t)
}
