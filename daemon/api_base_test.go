// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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
	"context"
	"crypto"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/sha3"
	"gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
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

type APIBaseSuite struct {
	testutil.BaseTest

	storetest.Store

	rsnaps            []*snap.Info
	err               error
	vars              map[string]string
	storeSearch       store.Search
	suggestedCurrency string
	d                 *Daemon
	user              *auth.UserState
	ctx               context.Context
	currentSnaps      []*store.CurrentSnap
	actions           []*store.SnapAction
	buyOptions        *client.BuyOptions
	buyResult         *client.BuyResult

	restoreRelease func()

	StoreSigning *assertstest.StoreStack
	Brands       *assertstest.SigningAccounts

	systemctlRestorer func()
	SysctlBufs        [][]byte

	connectivityResult     map[string]bool
	loginUserStoreMacaroon string
	loginUserDischarge     string

	restoreSanitize func()
}

func (s *APIBaseSuite) PokeStateLock() {
	// the store should be called without the state lock held. Try
	// to acquire it.
	st := s.d.overlord.State()
	st.Lock()
	st.Unlock()
}

func (s *APIBaseSuite) SnapInfo(ctx context.Context, spec store.SnapSpec, user *auth.UserState) (*snap.Info, error) {
	s.PokeStateLock()
	s.user = user
	s.ctx = ctx
	if len(s.rsnaps) > 0 {
		return s.rsnaps[0], s.err
	}
	return nil, s.err
}

func (s *APIBaseSuite) Find(ctx context.Context, search *store.Search, user *auth.UserState) ([]*snap.Info, error) {
	s.PokeStateLock()

	s.storeSearch = *search
	s.user = user
	s.ctx = ctx

	return s.rsnaps, s.err
}

func (s *APIBaseSuite) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	s.PokeStateLock()
	if assertQuery != nil {
		toResolve, err := assertQuery.ToResolve()
		if err != nil {
			return nil, nil, err
		}
		if len(toResolve) != 0 {
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

func (s *APIBaseSuite) SuggestedCurrency() string {
	s.PokeStateLock()

	return s.suggestedCurrency
}

func (s *APIBaseSuite) Buy(options *client.BuyOptions, user *auth.UserState) (*client.BuyResult, error) {
	s.PokeStateLock()

	s.buyOptions = options
	s.user = user
	return s.buyResult, s.err
}

func (s *APIBaseSuite) ReadyToBuy(user *auth.UserState) error {
	s.PokeStateLock()

	s.user = user
	return s.err
}

func (s *APIBaseSuite) ConnectivityCheck() (map[string]bool, error) {
	s.PokeStateLock()

	return s.connectivityResult, s.err
}

func (s *APIBaseSuite) LoginUser(username, password, otp string) (string, string, error) {
	s.PokeStateLock()

	return s.loginUserStoreMacaroon, s.loginUserDischarge, s.err
}

func (s *APIBaseSuite) muxVars(*http.Request) map[string]string {
	return s.vars
}

func (s *APIBaseSuite) SetUpSuite(c *check.C) {
	muxVars = s.muxVars
	s.restoreRelease = sandbox.MockForceDevMode(false)
	s.systemctlRestorer = systemd.MockSystemctl(s.systemctl)
	s.restoreSanitize = snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
}

func (s *APIBaseSuite) TearDownSuite(c *check.C) {
	muxVars = nil
	s.restoreRelease()
	s.systemctlRestorer()
	s.restoreSanitize()
}

func (s *APIBaseSuite) systemctl(args ...string) (buf []byte, err error) {
	if len(s.SysctlBufs) > 0 {
		buf, s.SysctlBufs = s.SysctlBufs[0], s.SysctlBufs[1:]
	}
	return buf, err
}

var (
	BrandPrivKey, _ = assertstest.GenerateKey(752)
)

func (s *APIBaseSuite) SetUpTest(c *check.C) {
	s.BaseTest.SetUpTest(c)

	ctlcmds := testutil.MockCommand(c, "systemctl", "").Also("journalctl", "")
	s.AddCleanup(ctlcmds.Restore)

	s.SysctlBufs = nil

	dirs.SetRootDir(c.MkDir())
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
	s.currentSnaps = nil
	s.actions = nil
	// Disable real security backends for all API tests
	s.AddCleanup(ifacestate.MockSecurityBackends(nil))

	s.buyOptions = nil
	s.buyResult = nil

	s.StoreSigning = assertstest.NewStoreStack("can0nical", nil)
	s.AddCleanup(sysdb.InjectTrusted(s.StoreSigning.Trusted))

	s.Brands = assertstest.NewSigningAccounts(s.StoreSigning)
	s.Brands.Register("my-brand", BrandPrivKey, nil)

	assertstateRefreshSnapDeclarations = nil
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
	snapstateSwitch = nil
}

func (s *APIBaseSuite) TearDownTest(c *check.C) {
	s.d = nil
	s.ctx = nil

	unsafeReadSnapInfo = unsafeReadSnapInfoImpl
	ensureStateSoon = ensureStateSoonImpl
	dirs.SetRootDir("")

	assertstateRefreshSnapDeclarations = assertstate.RefreshSnapDeclarations
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
	snapstateSwitch = snapstate.Switch

	s.BaseTest.TearDownTest(c)
}

func (s *APIBaseSuite) MockModel(c *check.C, st *state.State, model *asserts.Model) {
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

func (s *APIBaseSuite) DaemonWithStore(c *check.C, sto snapstate.StoreService) *Daemon {
	if s.d != nil {
		panic("called Daemon*() twice")
	}
	d, err := New()
	c.Assert(err, check.IsNil)
	d.addRoutes()

	c.Assert(d.overlord.StartUp(), check.IsNil)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	snapstate.ReplaceStore(st, sto)
	// mark as already seeded
	st.Set("seeded", true)
	// registered
	s.MockModel(c, st, nil)

	// don't actually try to talk to the store on snapstate.Ensure
	// needs doing after the call to devicestate.Manager (which
	// happens in daemon.New via overlord.New)
	snapstate.CanAutoRefresh = nil

	s.d = d
	return d
}

func (s *APIBaseSuite) ResetDaemon() {
	s.d = nil
}

func (s *APIBaseSuite) Daemon(c *check.C) *Daemon {
	return s.daemon(c)
}

// XXX this one will go away
func (s *APIBaseSuite) daemon(c *check.C) *Daemon {
	return s.DaemonWithStore(c, s)
}

// XXX this one will go away
func (s *APIBaseSuite) daemonWithOverlordMock(c *check.C) *Daemon {
	return s.DaemonWithOverlordMock(c)
}

func (s *APIBaseSuite) DaemonWithOverlordMock(c *check.C) *Daemon {
	if s.d != nil {
		panic("called Daemon*() twice")
	}
	d, err := New()
	c.Assert(err, check.IsNil)
	d.addRoutes()

	o := overlord.Mock()
	d.overlord = o

	st := d.overlord.State()
	// adds an assertion db
	assertstate.Manager(st, o.TaskRunner())
	st.Lock()
	defer st.Unlock()
	snapstate.ReplaceStore(st, s)

	s.d = d
	return d
}

type fakeSnapManager struct{}

func newFakeSnapManager(st *state.State, runner *state.TaskRunner) *fakeSnapManager {
	runner.AddHandler("fake-install-snap", func(t *state.Task, _ *tomb.Tomb) error {
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

// sanity
var _ overlord.StateManager = (*fakeSnapManager)(nil)

func (s *APIBaseSuite) daemonWithFakeSnapManager(c *check.C) *Daemon {
	d := s.daemonWithOverlordMock(c)
	st := d.overlord.State()
	runner := d.overlord.TaskRunner()
	d.overlord.AddManager(newFakeSnapManager(st, runner))
	d.overlord.AddManager(runner)
	c.Assert(d.overlord.StartUp(), check.IsNil)
	return d
}

func (s *APIBaseSuite) waitTrivialChange(c *check.C, chg *state.Change) {
	err := s.d.overlord.Settle(5 * time.Second)
	c.Assert(err, check.IsNil)
	c.Assert(chg.IsReady(), check.Equals, true)
}

func (s *APIBaseSuite) mkInstalledDesktopFile(c *check.C, name, content string) string {
	df := filepath.Join(dirs.SnapDesktopFilesDir, name)
	err := os.MkdirAll(filepath.Dir(df), 0755)
	c.Assert(err, check.IsNil)
	err = ioutil.WriteFile(df, []byte(content), 0644)
	c.Assert(err, check.IsNil)
	return df
}

// XXX this one will go away
func (s *APIBaseSuite) mkInstalledInState(c *check.C, daemon *Daemon, instanceName, developer, version string, revision snap.Revision, active bool, extraYaml string) *snap.Info {
	return s.MkInstalledInState(c, daemon, instanceName, developer, version, revision, active, extraYaml)
}

func (s *APIBaseSuite) MkInstalledInState(c *check.C, daemon *Daemon, instanceName, developer, version string, revision snap.Revision, active bool, extraYaml string) *snap.Info {
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
	c.Check(ioutil.WriteFile(filepath.Join(guidir, "icon.svg"), []byte("yadda icon"), 0644), check.IsNil)

	if daemon == nil {
		return snapInfo
	}
	st := daemon.overlord.State()
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	snapstate.Get(st, instanceName, &snapst)
	snapst.Active = active
	snapst.Sequence = append(snapst.Sequence, &snapInfo.SideInfo)
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

	content, err := ioutil.ReadFile(snapInfo.MountFile())
	c.Assert(err, check.IsNil)
	h := sha3.Sum384(content)
	dgst, err := asserts.EncodeDigest(crypto.SHA3_384, h[:])
	c.Assert(err, check.IsNil)
	snapRev, err := s.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": string(dgst),
		"snap-size":     "999",
		"snap-id":       snapID,
		"snap-revision": fmt.Sprintf("%s", revision),
		"developer-id":  devAcct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)

	assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""), devAcct, snapDecl, snapRev)

	return snapInfo
}

func handlerCommand(c *check.C, d *Daemon, req *http.Request) (cmd *Command, vars map[string]string) {
	m := &mux.RouteMatch{}
	if !d.router.Match(req, m) {
		c.Fatalf("no command for URL %q", req.URL)
	}
	cmd, ok := m.Handler.(*Command)
	if !ok {
		c.Fatalf("no command for URL %q", req.URL)
	}
	return cmd, m.Vars
}

func (s *APIBaseSuite) CheckGetOnly(c *check.C, req *http.Request) {
	if s.d == nil {
		panic("call s.Daemon(c) etc in your test first")
	}

	cmd, _ := handlerCommand(c, s.d, req)
	c.Check(cmd.POST, check.IsNil)
	c.Check(cmd.PUT, check.IsNil)
	c.Check(cmd.GET, check.NotNil)
}

func (s *APIBaseSuite) Req(c *check.C, req *http.Request, u *auth.UserState) Response {
	if s.d == nil {
		panic("call s.Daemon(c) etc in your test first")
	}

	cmd, vars := handlerCommand(c, s.d, req)
	s.vars = vars
	var f ResponseFunc
	switch req.Method {
	case "GET":
		f = cmd.GET
	case "POST":
		f = cmd.POST
	case "PUT":
		f = cmd.PUT
	default:
		c.Fatalf("unsupported HTTP method %q", req.Method)
	}
	if f == nil {
		c.Fatalf("no support for %q for %q", req.Method, req.URL)
	}
	return f(cmd, req, u)
}

func (s *APIBaseSuite) ServeHTTP(c *check.C, w http.ResponseWriter, req *http.Request) {
	if s.d == nil {
		panic("call s.Daemon(c) etc in your test first")
	}

	cmd, vars := handlerCommand(c, s.d, req)
	s.vars = vars

	cmd.ServeHTTP(w, req)
}

func (s *APIBaseSuite) SimulateConflict(name string) {
	if s.d == nil {
		panic("call s.Daemon(c) etc in your test first")
	}

	o := s.d.overlord
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
