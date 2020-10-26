// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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

package overlord_test

// test the various managers and their operation together through overlord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	snapshotbackend "github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

var (
	settleTimeout           = testutil.HostScaledTimeout(45 * time.Second)
	aggressiveSettleTimeout = testutil.HostScaledTimeout(50 * time.Millisecond)
	connectRetryTimeout     = testutil.HostScaledTimeout(70 * time.Millisecond)
)

type automaticSnapshotCall struct {
	InstanceName string
	SnapConfig   map[string]interface{}
	Usernames    []string
	Flags        *snapshotbackend.Flags
}

type baseMgrsSuite struct {
	testutil.BaseTest

	tempdir string

	storeSigning *assertstest.StoreStack
	brands       *assertstest.SigningAccounts

	devAcct *asserts.Account

	serveIDtoName map[string]string
	serveSnapPath map[string]string
	serveRevision map[string]string
	serveOldPaths map[string][]string
	serveOldRevs  map[string][]string

	hijackServeSnap func(http.ResponseWriter)

	checkDeviceAndAuthContext func(store.DeviceAndAuthContext)
	expectedSerial            string
	expectedStore             string
	sessionMacaroon           string

	o *overlord.Overlord

	failNextDownload string

	automaticSnapshots []automaticSnapshotCall
}

var (
	_ = Suite(&mgrsSuite{})
	_ = Suite(&storeCtxSetupSuite{})
)

var (
	brandPrivKey, _ = assertstest.GenerateKey(752)

	develPrivKey, _ = assertstest.GenerateKey(752)

	deviceKey, _ = assertstest.GenerateKey(752)
)

func verifyLastTasksetIsRerefresh(c *C, tts []*state.TaskSet) {
	ts := tts[len(tts)-1]
	c.Assert(ts.Tasks(), HasLen, 1)
	c.Check(ts.Tasks()[0].Kind(), Equals, "check-rerefresh")
}

func (s *baseMgrsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })

	// needed for system key generation
	s.AddCleanup(osutil.MockMountInfo(""))

	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)

	// needed by hooks
	s.AddCleanup(testutil.MockCommand(c, "snap", "").Restore)

	restoreCheckFreeSpace := snapstate.MockOsutilCheckFreeSpace(func(string, uint64) error { return nil })
	s.AddCleanup(restoreCheckFreeSpace)

	oldSetupInstallHook := snapstate.SetupInstallHook
	oldSetupRemoveHook := snapstate.SetupRemoveHook
	snapstate.SetupRemoveHook = hookstate.SetupRemoveHook
	snapstate.SetupInstallHook = hookstate.SetupInstallHook
	s.AddCleanup(func() {
		snapstate.SetupRemoveHook = oldSetupRemoveHook
		snapstate.SetupInstallHook = oldSetupInstallHook
	})

	s.automaticSnapshots = nil
	r := snapshotstate.MockBackendSave(func(_ context.Context, id uint64, si *snap.Info, cfg map[string]interface{}, usernames []string, flags *snapshotbackend.Flags) (*client.Snapshot, error) {
		s.automaticSnapshots = append(s.automaticSnapshots, automaticSnapshotCall{InstanceName: si.InstanceName(), SnapConfig: cfg, Usernames: usernames, Flags: flags})
		return nil, nil
	})
	s.AddCleanup(r)

	s.AddCleanup(ifacestate.MockConnectRetryTimeout(connectRetryTimeout))

	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
	s.AddCleanup(func() { os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS") })

	// create a fake systemd environment
	os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755)

	r = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	})
	s.AddCleanup(r)

	s.storeSigning = assertstest.NewStoreStack("can0nical", nil)
	s.brands = assertstest.NewSigningAccounts(s.storeSigning)
	s.brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"validation": "verified",
	})
	s.AddCleanup(sysdb.InjectTrusted(s.storeSigning.Trusted))

	s.devAcct = assertstest.NewAccount(s.storeSigning, "devdevdev", map[string]interface{}{
		"account-id": "devdevdev",
	}, "")
	err = s.storeSigning.Add(s.devAcct)
	c.Assert(err, IsNil)

	s.serveIDtoName = make(map[string]string)
	s.serveSnapPath = make(map[string]string)
	s.serveRevision = make(map[string]string)
	s.serveOldPaths = make(map[string][]string)
	s.serveOldRevs = make(map[string][]string)
	s.hijackServeSnap = nil

	s.checkDeviceAndAuthContext = nil
	s.expectedSerial = ""
	s.expectedStore = ""
	s.sessionMacaroon = ""

	s.AddCleanup(ifacestate.MockSecurityBackends(nil))

	o, err := overlord.New(nil)
	c.Assert(err, IsNil)
	err = o.StartUp()
	c.Assert(err, IsNil)
	o.InterfaceManager().DisableUDevMonitor()
	s.o = o
	st := s.o.State()
	st.Lock()
	defer st.Unlock()
	st.Set("seeded", true)
	// registered
	err = assertstate.Add(st, sysdb.GenericClassicModel())
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "generic",
		Model:  "generic-classic",
		Serial: "serialserial",
	})

	// add "core" snap declaration
	headers := map[string]interface{}{
		"series":       "16",
		"snap-name":    "core",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	err = assertstate.Add(st, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	a, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(st, a)
	c.Assert(err, IsNil)
	s.serveRevision["core"] = "1"
	s.serveIDtoName[fakeSnapID("core")] = "core"
	err = s.storeSigning.Add(a)
	c.Assert(err, IsNil)

	// add "snap1" snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "snap1",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a2, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a2), IsNil)
	c.Assert(s.storeSigning.Add(a2), IsNil)

	// add "snap2" snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "snap2",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a3, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a3), IsNil)
	c.Assert(s.storeSigning.Add(a3), IsNil)

	// add "some-snap" snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "some-snap",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a4, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a4), IsNil)
	c.Assert(s.storeSigning.Add(a4), IsNil)

	// add "other-snap" snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "other-snap",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a5, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a5), IsNil)
	c.Assert(s.storeSigning.Add(a5), IsNil)

	// add pc-kernel snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "pc-kernel",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a6, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a6), IsNil)
	c.Assert(s.storeSigning.Add(a6), IsNil)

	// add pc snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "pc",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a7, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a7), IsNil)
	c.Assert(s.storeSigning.Add(a7), IsNil)

	// add core itself
	snapstate.Set(st, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", SnapID: fakeSnapID("core"), Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "os",
		Flags: snapstate.Flags{
			Required: true,
		},
	})

	// don't actually try to talk to the store on snapstate.Ensure
	// needs doing after the call to devicestate.Manager (which happens in overlord.New)
	snapstate.CanAutoRefresh = nil

	st.Set("refresh-privacy-key", "privacy-key")

	// For triggering errors
	erroringHandler := func(task *state.Task, _ *tomb.Tomb) error {
		return errors.New("error out")
	}
	s.o.TaskRunner().AddHandler("error-trigger", erroringHandler, nil)

	// setup cloud-init as restricted so that tests by default don't run the
	// full EnsureCloudInitRestricted logic in the devicestate mgr
	snapdCloudInitRestrictedFile := filepath.Join(dirs.GlobalRootDir, "etc/cloud/cloud.cfg.d/zzzz_snapd.cfg")
	err = os.MkdirAll(filepath.Dir(snapdCloudInitRestrictedFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(snapdCloudInitRestrictedFile, nil, 0644)
	c.Assert(err, IsNil)
}

type mgrsSuite struct {
	baseMgrsSuite
}

func makeTestSnapWithFiles(c *C, snapYamlContent string, files [][]string) string {
	info, err := snap.InfoFromSnapYaml([]byte(snapYamlContent))
	c.Assert(err, IsNil)

	for _, app := range info.Apps {
		// files is a list of (filename, content)
		files = append(files, []string{app.Command, ""})
	}

	return snaptest.MakeTestSnapWithFiles(c, snapYamlContent, files)
}

func makeTestSnap(c *C, snapYamlContent string) string {
	return makeTestSnapWithFiles(c, snapYamlContent, nil)
}

func (s *mgrsSuite) TestHappyLocalInstall(c *C) {
	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
`
	snapPath := makeTestSnap(c, snapYamlContent+"version: 1.0")

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "foo"}, snapPath, "", "", snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	snap, err := snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)

	// ensure that the binary wrapper file got generated with the right
	// name
	binaryWrapper := filepath.Join(dirs.SnapBinariesDir, "foo.bar")
	c.Assert(osutil.IsSymlink(binaryWrapper), Equals, true)

	// data dirs
	c.Assert(osutil.IsDirectory(snap.DataDir()), Equals, true)
	c.Assert(osutil.IsDirectory(snap.CommonDataDir()), Equals, true)

	// snap file and its mounting

	// after install the snap file is in the right dir
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "foo_x1.snap")), Equals, true)

	// ensure the right unit is created
	mup := systemd.MountUnitPath(filepath.Join(dirs.StripRootDir(dirs.SnapMountDir), "foo/x1"))
	c.Assert(mup, testutil.FileMatches, fmt.Sprintf("(?ms).*^Where=%s/foo/x1", dirs.StripRootDir(dirs.SnapMountDir)))
	c.Assert(mup, testutil.FileMatches, "(?ms).*^What=/var/lib/snapd/snaps/foo_x1.snap")
}

func (s *mgrsSuite) TestLocalInstallUndo(c *C) {
	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
hooks:
  install:
  configure:
`
	snapPath := makeTestSnap(c, snapYamlContent+"version: 1.0")

	installHook := false
	defer hookstate.MockRunHook(func(ctx *hookstate.Context, _ *tomb.Tomb) ([]byte, error) {
		switch ctx.HookName() {
		case "install":
			installHook = true
			_, _, err := ctlcmd.Run(ctx, []string{"set", "installed=true"}, 0)
			c.Assert(err, IsNil)
			return nil, nil
		case "configure":
			return nil, errors.New("configure failed")
		}
		return nil, nil
	})()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "foo"}, snapPath, "", "", snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.ErrorStatus, Commentf("install-snap unexpectedly succeeded"))

	// check undo statutes
	for _, t := range chg.Tasks() {
		which := t.Kind()
		expectedStatus := state.UndoneStatus
		switch t.Kind() {
		case "prerequisites":
			expectedStatus = state.DoneStatus
		case "run-hook":
			var hs hookstate.HookSetup
			err := t.Get("hook-setup", &hs)
			c.Assert(err, IsNil)
			switch hs.Hook {
			case "install":
				expectedStatus = state.UndoneStatus
			case "configure":
				expectedStatus = state.ErrorStatus
			case "check-health":
				expectedStatus = state.HoldStatus
			}
			which += fmt.Sprintf("[%s]", hs.Hook)
		}
		c.Assert(t.Status(), Equals, expectedStatus, Commentf("%s", which))
	}

	// install hooks was called
	c.Check(installHook, Equals, true)

	// nothing in snaps
	all, err := snapstate.All(st)
	c.Assert(err, IsNil)
	c.Check(all, HasLen, 1)
	_, ok := all["core"]
	c.Check(ok, Equals, true)

	// nothing in config
	var config map[string]*json.RawMessage
	err = st.Get("config", &config)
	c.Assert(err, IsNil)
	c.Check(config, HasLen, 1)
	_, ok = config["core"]
	c.Check(ok, Equals, true)

	snapdirs, err := filepath.Glob(filepath.Join(dirs.SnapMountDir, "*"))
	c.Assert(err, IsNil)
	// just README and bin
	c.Check(snapdirs, HasLen, 2)
	for _, d := range snapdirs {
		c.Check(filepath.Base(d), Not(Equals), "foo")
	}
}

func (s *mgrsSuite) TestHappyRemove(c *C) {
	oldEstimateSnapshotSize := snapstate.EstimateSnapshotSize
	snapstate.EstimateSnapshotSize = func(st *state.State, instanceName string, users []string) (uint64, error) {
		return 0, nil
	}
	defer func() {
		snapstate.EstimateSnapshotSize = oldEstimateSnapshotSize
	}()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
`
	snapInfo := s.installLocalTestSnap(c, snapYamlContent+"version: 1.0")

	// set config
	tr := config.NewTransaction(st)
	c.Assert(tr.Set("foo", "key", "value"), IsNil)
	tr.Commit()

	ts, err := snapstate.Remove(st, "foo", snap.R(0), nil)
	c.Assert(err, IsNil)
	chg := st.NewChange("remove-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("remove-snap change failed with: %v", chg.Err()))

	// ensure that the binary wrapper file got removed
	binaryWrapper := filepath.Join(dirs.SnapBinariesDir, "foo.bar")
	c.Assert(osutil.FileExists(binaryWrapper), Equals, false)

	// data dirs
	c.Assert(osutil.FileExists(snapInfo.DataDir()), Equals, false)
	c.Assert(osutil.FileExists(snapInfo.CommonDataDir()), Equals, false)

	// snap file and its mount
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "foo_x1.snap")), Equals, false)
	mup := systemd.MountUnitPath(filepath.Join(dirs.StripRootDir(dirs.SnapMountDir), "foo/x1"))
	c.Assert(osutil.FileExists(mup), Equals, false)

	// automatic snapshot was created
	c.Assert(s.automaticSnapshots, DeepEquals, []automaticSnapshotCall{{"foo", map[string]interface{}{"key": "value"}, nil, &snapshotbackend.Flags{Auto: true}}})
}

func fakeSnapID(name string) string {
	const suffix = "idididididididididididididididid"
	return name + suffix[len(name)+1:]
}

const (
	snapV2 = `{
	"architectures": [
	    "all"
	],
        "download": {
			"url": "@URL@",
			"size": 123
        },
        "epoch": @EPOCH@,
        "type": "@TYPE@",
	"name": "@NAME@",
	"revision": @REVISION@,
	"snap-id": "@SNAPID@",
	"summary": "Foo",
	"description": "this is a description",
	"version": "@VERSION@",
        "publisher": {
           "id": "devdevdev",
           "name": "bar"
         },
         "media": [
            {"type": "icon", "url": "@ICON@"}
         ]
}`
)

var fooSnapID = fakeSnapID("foo")

func (s *baseMgrsSuite) prereqSnapAssertions(c *C, extraHeaders ...map[string]interface{}) *asserts.SnapDeclaration {
	if len(extraHeaders) == 0 {
		extraHeaders = []map[string]interface{}{{}}
	}
	var snapDecl *asserts.SnapDeclaration
	for _, extraHeaders := range extraHeaders {
		headers := map[string]interface{}{
			"series":       "16",
			"snap-name":    "foo",
			"publisher-id": "devdevdev",
			"timestamp":    time.Now().Format(time.RFC3339),
		}
		for h, v := range extraHeaders {
			headers[h] = v
		}
		headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
		a, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
		c.Assert(err, IsNil)
		err = s.storeSigning.Add(a)
		c.Assert(err, IsNil)
		snapDecl = a.(*asserts.SnapDeclaration)
	}
	return snapDecl
}

func (s *baseMgrsSuite) makeStoreTestSnapWithFiles(c *C, snapYaml string, revno string, files [][]string) (path, digest string) {
	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)

	snapPath := makeTestSnapWithFiles(c, snapYaml, files)

	snapDigest, size, err := asserts.SnapFileSHA3_384(snapPath)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"snap-id":       fakeSnapID(info.SnapName()),
		"snap-sha3-384": snapDigest,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-revision": revno,
		"developer-id":  "devdevdev",
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapRev)
	c.Assert(err, IsNil)

	return snapPath, snapDigest
}

func (s *baseMgrsSuite) makeStoreTestSnap(c *C, snapYaml string, revno string) (path, digest string) {
	return s.makeStoreTestSnapWithFiles(c, snapYaml, revno, nil)
}

func (s *baseMgrsSuite) pathFor(name, revno string) string {
	if revno == s.serveRevision[name] {
		return s.serveSnapPath[name]
	}
	for i, r := range s.serveOldRevs[name] {
		if r == revno {
			return s.serveOldPaths[name][i]
		}
	}
	return "/not/found"
}

func (s *baseMgrsSuite) newestThatCanRead(name string, epoch snap.Epoch) (info *snap.Info, rev string) {
	if s.serveSnapPath[name] == "" {
		return nil, ""
	}
	idx := len(s.serveOldPaths[name])
	rev = s.serveRevision[name]
	path := s.serveSnapPath[name]
	for {
		snapf, err := snapfile.Open(path)
		if err != nil {
			panic(err)
		}
		info, err := snap.ReadInfoFromSnapFile(snapf, nil)
		if err != nil {
			panic(err)
		}
		if info.Epoch.CanRead(epoch) {
			return info, rev
		}
		idx--
		if idx < 0 {
			return nil, ""
		}
		path = s.serveOldPaths[name][idx]
		rev = s.serveOldRevs[name][idx]
	}
}

func (s *baseMgrsSuite) mockStore(c *C) *httptest.Server {
	var baseURL *url.URL
	fillHit := func(hitTemplate, revno string, info *snap.Info) string {
		epochBuf, err := json.Marshal(info.Epoch)
		if err != nil {
			panic(err)
		}
		name := info.SnapName()

		hit := strings.Replace(hitTemplate, "@URL@", baseURL.String()+"/api/v1/snaps/download/"+name+"/"+revno, -1)
		hit = strings.Replace(hit, "@NAME@", name, -1)
		hit = strings.Replace(hit, "@SNAPID@", fakeSnapID(name), -1)
		hit = strings.Replace(hit, "@ICON@", baseURL.String()+"/icon", -1)
		hit = strings.Replace(hit, "@VERSION@", info.Version, -1)
		hit = strings.Replace(hit, "@REVISION@", revno, -1)
		hit = strings.Replace(hit, `@TYPE@`, string(info.Type()), -1)
		hit = strings.Replace(hit, `@EPOCH@`, string(epochBuf), -1)
		return hit
	}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// all URLS are /api/v1/snaps/... or /v2/snaps/... so
		// check the url is sane and discard the common prefix
		// to simplify indexing into the comps slice.
		comps := strings.Split(r.URL.Path, "/")
		if len(comps) < 2 {
			panic("unexpected url path: " + r.URL.Path)
		}
		if comps[1] == "api" { //v1
			if len(comps) <= 4 {
				panic("unexpected url path: " + r.URL.Path)
			}
			comps = comps[4:]
			if comps[0] == "auth" {
				comps[0] = "auth:" + comps[1]
			}
		} else { // v2
			if len(comps) <= 3 {
				panic("unexpected url path: " + r.URL.Path)
			}
			comps = comps[3:]
			comps[0] = "v2:" + comps[0]
		}

		switch comps[0] {
		case "auth:nonces":
			w.Write([]byte(`{"nonce": "NONCE"}`))
			return
		case "auth:sessions":
			// quick sanity check
			reqBody, err := ioutil.ReadAll(r.Body)
			c.Check(err, IsNil)
			c.Check(bytes.Contains(reqBody, []byte("nonce: NONCE")), Equals, true)
			c.Check(bytes.Contains(reqBody, []byte(fmt.Sprintf("serial: %s", s.expectedSerial))), Equals, true)
			c.Check(bytes.Contains(reqBody, []byte(fmt.Sprintf("store: %s", s.expectedStore))), Equals, true)

			c.Check(s.sessionMacaroon, Not(Equals), "")
			w.WriteHeader(200)
			w.Write([]byte(fmt.Sprintf(`{"macaroon": "%s"}`, s.sessionMacaroon)))
			return
		case "assertions":
			ref := &asserts.Ref{
				Type:       asserts.Type(comps[1]),
				PrimaryKey: comps[2:],
			}
			a, err := ref.Resolve(s.storeSigning.Find)
			if asserts.IsNotFound(err) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(404)
				w.Write([]byte(`{"status": 404}`))
				return
			}
			if err != nil {
				panic(err)
			}
			w.Header().Set("Content-Type", asserts.MediaType)
			w.WriteHeader(200)
			w.Write(asserts.Encode(a))
			return
		case "download":
			if s.sessionMacaroon != "" {
				// FIXME: download is still using the old headers!
				c.Check(r.Header.Get("X-Device-Authorization"), Equals, fmt.Sprintf(`Macaroon root="%s"`, s.sessionMacaroon))
			}
			if s.failNextDownload == comps[1] {
				s.failNextDownload = ""
				w.WriteHeader(418)
				return
			}
			if s.hijackServeSnap != nil {
				s.hijackServeSnap(w)
				return
			}
			snapR, err := os.Open(s.pathFor(comps[1], comps[2]))
			if err != nil {
				panic(err)
			}
			io.Copy(w, snapR)
		case "v2:refresh":
			if s.sessionMacaroon != "" {
				c.Check(r.Header.Get("Snap-Device-Authorization"), Equals, fmt.Sprintf(`Macaroon root="%s"`, s.sessionMacaroon))
			}
			dec := json.NewDecoder(r.Body)
			var input struct {
				Actions []struct {
					Action      string     `json:"action"`
					SnapID      string     `json:"snap-id"`
					Name        string     `json:"name"`
					InstanceKey string     `json:"instance-key"`
					Epoch       snap.Epoch `json:"epoch"`
					// assertions
					Key        string `json:"key"`
					Assertions []struct {
						Type        string   `json:"type"`
						PrimaryKey  []string `json:"primary-key"`
						IfNewerThan *int     `json:"if-newer-than"`
					}
				} `json:"actions"`
				Context []struct {
					SnapID string     `json:"snap-id"`
					Epoch  snap.Epoch `json:"epoch"`
				} `json:"context"`
			}
			if err := dec.Decode(&input); err != nil {
				panic(err)
			}
			id2epoch := make(map[string]snap.Epoch, len(input.Context))
			for _, s := range input.Context {
				id2epoch[s.SnapID] = s.Epoch
			}
			type resultJSON struct {
				Result      string          `json:"result"`
				SnapID      string          `json:"snap-id"`
				Name        string          `json:"name"`
				Snap        json.RawMessage `json:"snap"`
				InstanceKey string          `json:"instance-key"`
				// For assertions
				Key           string   `json:"key"`
				AssertionURLs []string `json:"assertion-stream-urls"`
			}
			var results []resultJSON
			for _, a := range input.Actions {
				if a.Action == "fetch-assertions" {
					urls := []string{}
					for _, ar := range a.Assertions {
						ref := &asserts.Ref{
							Type:       asserts.Type(ar.Type),
							PrimaryKey: ar.PrimaryKey,
						}
						_, err := ref.Resolve(s.storeSigning.Find)
						if err != nil {
							panic("missing assertions not supported")
						}
						urls = append(urls, fmt.Sprintf("%s/api/v1/snaps/assertions/%s", baseURL.String(), ref.Unique()))

					}
					results = append(results, resultJSON{
						Result:        "fetch-assertions",
						Key:           a.Key,
						AssertionURLs: urls,
					})
					continue
				}
				name := s.serveIDtoName[a.SnapID]
				epoch := id2epoch[a.SnapID]
				if a.Action == "install" {
					name = a.Name
					epoch = a.Epoch
				}

				info, revno := s.newestThatCanRead(name, epoch)
				if info == nil {
					// no match
					continue
				}
				results = append(results, resultJSON{
					Result:      a.Action,
					SnapID:      a.SnapID,
					InstanceKey: a.InstanceKey,
					Name:        name,
					Snap:        json.RawMessage(fillHit(snapV2, revno, info)),
				})
			}
			w.WriteHeader(200)
			output, err := json.Marshal(map[string]interface{}{
				"results": results,
			})
			if err != nil {
				panic(err)
			}
			w.Write(output)

		default:
			panic("unexpected url path: " + r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)

	baseURL, _ = url.Parse(mockServer.URL)
	storeCfg := store.Config{
		StoreBaseURL: baseURL,
	}

	mStore := store.New(&storeCfg, nil)
	st := s.o.State()
	st.Lock()
	snapstate.ReplaceStore(s.o.State(), mStore)
	st.Unlock()

	// this will be used by remodeling cases
	storeNew := func(cfg *store.Config, dac store.DeviceAndAuthContext) *store.Store {
		cfg.StoreBaseURL = baseURL
		if s.checkDeviceAndAuthContext != nil {
			s.checkDeviceAndAuthContext(dac)
		}
		return store.New(cfg, dac)
	}

	s.AddCleanup(overlord.MockStoreNew(storeNew))

	return mockServer
}

// serveSnap starts serving the snap at snapPath, moving the current
// one onto the list of previous ones if already set.
func (s *baseMgrsSuite) serveSnap(snapPath, revno string) {
	snapf, err := snapfile.Open(snapPath)
	if err != nil {
		panic(err)
	}
	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	if err != nil {
		panic(err)
	}
	name := info.SnapName()
	s.serveIDtoName[fakeSnapID(name)] = name

	if oldPath := s.serveSnapPath[name]; oldPath != "" {
		oldRev := s.serveRevision[name]
		if oldRev == "" {
			panic("old path set but not old revision")
		}
		s.serveOldPaths[name] = append(s.serveOldPaths[name], oldPath)
		s.serveOldRevs[name] = append(s.serveOldRevs[name], oldRev)
	}
	s.serveSnapPath[name] = snapPath
	s.serveRevision[name] = revno
}

func (s *mgrsSuite) TestHappyRemoteInstallAndUpgradeSvc(c *C) {
	// test install through store and update, plus some mechanics
	// of update
	// TODO: ok to split if it gets too messy to maintain

	s.prereqSnapAssertions(c)

	snapYamlContent := `name: foo
version: @VERSION@
apps:
 bar:
  command: bin/bar
 svc:
  command: svc
  daemon: forking
`

	ver := "1.0"
	revno := "42"
	snapPath, digest := s.makeStoreTestSnap(c, strings.Replace(snapYamlContent, "@VERSION@", ver, -1), revno)
	s.serveSnap(snapPath, revno)

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(context.TODO(), st, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	info, err := snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)

	c.Check(info.Revision, Equals, snap.R(42))
	c.Check(info.SnapID, Equals, fooSnapID)
	c.Check(info.Version, Equals, "1.0")
	c.Check(info.Summary(), Equals, "Foo")
	c.Check(info.Description(), Equals, "this is a description")
	c.Assert(osutil.FileExists(info.MountFile()), Equals, true)

	pubAcct, err := assertstate.Publisher(st, info.SnapID)
	c.Assert(err, IsNil)
	c.Check(pubAcct.AccountID(), Equals, "devdevdev")
	c.Check(pubAcct.Username(), Equals, "devdevdev")

	snapRev42, err := assertstate.DB(st).Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": digest,
	})
	c.Assert(err, IsNil)
	c.Check(snapRev42.(*asserts.SnapRevision).SnapID(), Equals, fooSnapID)
	c.Check(snapRev42.(*asserts.SnapRevision).SnapRevision(), Equals, 42)

	// check service was setup properly
	svcFile := filepath.Join(dirs.SnapServicesDir, "snap.foo.svc.service")
	c.Assert(osutil.FileExists(svcFile), Equals, true)
	stat, err := os.Stat(svcFile)
	c.Assert(err, IsNil)
	// should _not_ be executable
	c.Assert(stat.Mode().String(), Equals, "-rw-r--r--")

	// Refresh

	ver = "2.0"
	revno = "50"
	snapPath, digest = s.makeStoreTestSnap(c, strings.Replace(snapYamlContent, "@VERSION@", ver, -1), revno)
	s.serveSnap(snapPath, revno)

	ts, err = snapstate.Update(st, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg = st.NewChange("upgrade-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	info, err = snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)

	c.Check(info.Revision, Equals, snap.R(50))
	c.Check(info.SnapID, Equals, fooSnapID)
	c.Check(info.Version, Equals, "2.0")

	snapRev50, err := assertstate.DB(st).Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": digest,
	})
	c.Assert(err, IsNil)
	c.Check(snapRev50.(*asserts.SnapRevision).SnapID(), Equals, fooSnapID)
	c.Check(snapRev50.(*asserts.SnapRevision).SnapRevision(), Equals, 50)

	// check updated wrapper
	symlinkTarget, err := os.Readlink(info.Apps["bar"].WrapperPath())
	c.Assert(err, IsNil)
	c.Assert(symlinkTarget, Equals, "/usr/bin/snap")

	// check updated service file
	c.Assert(svcFile, testutil.FileContains, "/var/snap/foo/"+revno)
}

func (s *mgrsSuite) TestHappyRemoteInstallAndUpdateWithEpochBump(c *C) {
	// test install through store and update, where there's an epoch bump in the upgrade
	// this does less checks on the details of install/update than TestHappyRemoteInstallAndUpgradeSvc

	s.prereqSnapAssertions(c)

	snapPath, _ := s.makeStoreTestSnap(c, "{name: foo, version: 0}", "1")
	s.serveSnap(snapPath, "1")

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(context.TODO(), st, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// confirm it worked
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	// sanity checks
	info, err := snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)
	c.Assert(info.Revision, Equals, snap.R(1))
	c.Assert(info.SnapID, Equals, fooSnapID)
	c.Assert(info.Epoch.String(), Equals, "0")

	// now add some more snaps
	for i, epoch := range []string{"1*", "2*", "3*"} {
		revno := fmt.Sprint(i + 2)
		snapPath, _ := s.makeStoreTestSnap(c, "{name: foo, version: 0, epoch: "+epoch+"}", revno)
		s.serveSnap(snapPath, revno)
	}

	// refresh

	ts, err = snapstate.Update(st, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg = st.NewChange("upgrade-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	info, err = snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)

	c.Check(info.Revision, Equals, snap.R(4))
	c.Check(info.SnapID, Equals, fooSnapID)
	c.Check(info.Epoch.String(), Equals, "3*")
}

func (s *mgrsSuite) TestHappyRemoteInstallAndUpdateWithPostHocEpochBump(c *C) {
	// test install through store and update, where there is an epoch
	// bump in the upgrade that comes in after the initial update is
	// computed.

	// this is mostly checking the same as TestHappyRemoteInstallAndUpdateWithEpochBump
	// but serves as a sanity check for the Without case that follows
	// (these two together serve as a test for the refresh filtering)
	s.testHappyRemoteInstallAndUpdateWithMaybeEpochBump(c, true)
}

func (s *mgrsSuite) TestHappyRemoteInstallAndUpdateWithoutEpochBump(c *C) {
	// test install through store and update, where there _isn't_ an epoch bump in the upgrade
	// note that there _are_ refreshes available after the refresh,
	// but they're not an epoch bump so they're ignored
	s.testHappyRemoteInstallAndUpdateWithMaybeEpochBump(c, false)
}

func (s *mgrsSuite) testHappyRemoteInstallAndUpdateWithMaybeEpochBump(c *C, doBump bool) {
	s.prereqSnapAssertions(c)

	snapPath, _ := s.makeStoreTestSnap(c, "{name: foo, version: 1}", "1")
	s.serveSnap(snapPath, "1")

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(context.TODO(), st, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// confirm it worked
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	// sanity checks
	info, err := snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)
	c.Assert(info.Revision, Equals, snap.R(1))
	c.Assert(info.SnapID, Equals, fooSnapID)
	c.Assert(info.Epoch.String(), Equals, "0")

	// add a new revision
	snapPath, _ = s.makeStoreTestSnap(c, "{name: foo, version: 2}", "2")
	s.serveSnap(snapPath, "2")

	// refresh

	ts, err = snapstate.Update(st, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg = st.NewChange("upgrade-snap", "...")
	chg.AddAll(ts)

	// add another new revision, after the update was computed (maybe with an epoch bump)
	if doBump {
		snapPath, _ = s.makeStoreTestSnap(c, "{name: foo, version: 3, epoch: 1*}", "3")
	} else {
		snapPath, _ = s.makeStoreTestSnap(c, "{name: foo, version: 3}", "3")
	}
	s.serveSnap(snapPath, "3")

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	info, err = snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)

	if doBump {
		// if the epoch bumped, then we should've re-refreshed
		c.Check(info.Revision, Equals, snap.R(3))
		c.Check(info.SnapID, Equals, fooSnapID)
		c.Check(info.Epoch.String(), Equals, "1*")
	} else {
		// if the epoch did not bump, then we should _not_ have re-refreshed
		c.Check(info.Revision, Equals, snap.R(2))
		c.Check(info.SnapID, Equals, fooSnapID)
		c.Check(info.Epoch.String(), Equals, "0")
	}
}

func (s *mgrsSuite) TestHappyRemoteInstallAndUpdateManyWithEpochBump(c *C) {
	// test install through store and update many, where there's an epoch bump in the upgrade
	// this does less checks on the details of install/update than TestHappyRemoteInstallAndUpgradeSvc

	snapNames := []string{"aaaa", "bbbb", "cccc"}
	for _, name := range snapNames {
		s.prereqSnapAssertions(c, map[string]interface{}{"snap-name": name})
		snapPath, _ := s.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 0}", name), "1")
		s.serveSnap(snapPath, "1")
	}

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	affected, tasksets, err := snapstate.InstallMany(st, snapNames, 0)
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, snapNames)
	chg := st.NewChange("install-snaps", "...")
	for _, taskset := range tasksets {
		chg.AddAll(taskset)
	}

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// confirm it worked
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	// sanity checks
	for _, name := range snapNames {
		info, err := snapstate.CurrentInfo(st, name)
		c.Assert(err, IsNil)
		c.Assert(info.Revision, Equals, snap.R(1))
		c.Assert(info.SnapID, Equals, fakeSnapID(name))
		c.Assert(info.Epoch.String(), Equals, "0")
	}

	// now add some more snap revisions with increasing epochs
	for _, name := range snapNames {
		for i, epoch := range []string{"1*", "2*", "3*"} {
			revno := fmt.Sprint(i + 2)
			snapPath, _ := s.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 0, epoch: %s}", name, epoch), revno)
			s.serveSnap(snapPath, revno)
		}
	}

	// refresh

	affected, tasksets, err = snapstate.UpdateMany(context.TODO(), st, nil, 0, &snapstate.Flags{})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, snapNames)
	chg = st.NewChange("upgrade-snaps", "...")
	for _, taskset := range tasksets {
		chg.AddAll(taskset)
	}

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	for _, name := range snapNames {
		info, err := snapstate.CurrentInfo(st, name)
		c.Assert(err, IsNil)

		c.Check(info.Revision, Equals, snap.R(4))
		c.Check(info.SnapID, Equals, fakeSnapID(name))
		c.Check(info.Epoch.String(), Equals, "3*")
	}
}

func (s *mgrsSuite) TestHappyRemoteInstallAndUpdateManyWithEpochBumpAndOneFailing(c *C) {
	// test install through store and update, where there's an epoch bump in the upgrade and one of them fails

	snapNames := []string{"aaaa", "bbbb", "cccc"}
	for _, name := range snapNames {
		s.prereqSnapAssertions(c, map[string]interface{}{"snap-name": name})
		snapPath, _ := s.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 0}", name), "1")
		s.serveSnap(snapPath, "1")
	}

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	affected, tasksets, err := snapstate.InstallMany(st, snapNames, 0)
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, snapNames)
	chg := st.NewChange("install-snaps", "...")
	for _, taskset := range tasksets {
		chg.AddAll(taskset)
	}

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// confirm it worked
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	// sanity checks
	for _, name := range snapNames {
		info, err := snapstate.CurrentInfo(st, name)
		c.Assert(err, IsNil)
		c.Assert(info.Revision, Equals, snap.R(1))
		c.Assert(info.SnapID, Equals, fakeSnapID(name))
		c.Assert(info.Epoch.String(), Equals, "0")
	}

	// now add some more snap revisions with increasing epochs
	for _, name := range snapNames {
		for i, epoch := range []string{"1*", "2*", "3*"} {
			revno := fmt.Sprint(i + 2)
			snapPath, _ := s.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 0, epoch: %s}", name, epoch), revno)
			s.serveSnap(snapPath, revno)
		}
	}

	// refresh
	affected, tasksets, err = snapstate.UpdateMany(context.TODO(), st, nil, 0, &snapstate.Flags{})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, snapNames)
	chg = st.NewChange("upgrade-snaps", "...")
	for _, taskset := range tasksets {
		chg.AddAll(taskset)
	}

	st.Unlock()
	// the download for the refresh above will be performed below, during 'settle'.
	// fail the refresh of cccc by failing its download
	s.failNextDownload = "cccc"
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), NotNil)
	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	for _, name := range snapNames {
		comment := Commentf("%q", name)
		info, err := snapstate.CurrentInfo(st, name)
		c.Assert(err, IsNil, comment)

		if name == "cccc" {
			// the failed one: still on rev 1 (epoch 0)
			c.Assert(info.Revision, Equals, snap.R(1))
			c.Assert(info.SnapID, Equals, fakeSnapID(name))
			c.Assert(info.Epoch.String(), Equals, "0")
		} else {
			// the non-failed ones: refreshed to rev 4 (epoch 3*)
			c.Check(info.Revision, Equals, snap.R(4), comment)
			c.Check(info.SnapID, Equals, fakeSnapID(name), comment)
			c.Check(info.Epoch.String(), Equals, "3*", comment)
		}
	}
}

func (s *mgrsSuite) TestHappyLocalInstallWithStoreMetadata(c *C) {
	snapDecl := s.prereqSnapAssertions(c)

	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
`
	snapPath := makeTestSnap(c, snapYamlContent+"version: 1.5")

	si := &snap.SideInfo{
		RealName: "foo",
		SnapID:   fooSnapID,
		Revision: snap.R(55),
	}

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// have the snap-declaration in the system db
	err := assertstate.Add(st, s.devAcct)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, snapDecl)
	c.Assert(err, IsNil)

	ts, _, err := snapstate.InstallPath(st, si, snapPath, "", "", snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	info, err := snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)
	c.Check(info.Revision, Equals, snap.R(55))
	c.Check(info.SnapID, Equals, fooSnapID)
	c.Check(info.Version, Equals, "1.5")

	// ensure that the binary wrapper file got generated with the right
	// name
	binaryWrapper := filepath.Join(dirs.SnapBinariesDir, "foo.bar")
	c.Assert(osutil.IsSymlink(binaryWrapper), Equals, true)

	// data dirs
	c.Assert(osutil.IsDirectory(info.DataDir()), Equals, true)
	c.Assert(osutil.IsDirectory(info.CommonDataDir()), Equals, true)

	// snap file and its mounting

	// after install the snap file is in the right dir
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "foo_55.snap")), Equals, true)

	// ensure the right unit is created
	mup := systemd.MountUnitPath(filepath.Join(dirs.StripRootDir(dirs.SnapMountDir), "foo/55"))
	c.Assert(mup, testutil.FileMatches, fmt.Sprintf("(?ms).*^Where=%s/foo/55", dirs.StripRootDir(dirs.SnapMountDir)))
	c.Assert(mup, testutil.FileMatches, "(?ms).*^What=/var/lib/snapd/snaps/foo_55.snap")
}

func (s *mgrsSuite) TestParallelInstanceLocalInstallSnapNameMismatch(c *C) {
	snapDecl := s.prereqSnapAssertions(c)

	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
`
	snapPath := makeTestSnap(c, snapYamlContent+"version: 1.5")

	si := &snap.SideInfo{
		RealName: "foo",
		SnapID:   fooSnapID,
		Revision: snap.R(55),
	}

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// have the snap-declaration in the system db
	err := assertstate.Add(st, s.devAcct)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, snapDecl)
	c.Assert(err, IsNil)

	_, _, err = snapstate.InstallPath(st, si, snapPath, "bar_instance", "", snapstate.Flags{DevMode: true})
	c.Assert(err, ErrorMatches, `cannot install snap "bar_instance", the name does not match the metadata "foo"`)
}

func (s *mgrsSuite) TestParallelInstanceLocalInstallInvalidInstanceName(c *C) {
	snapDecl := s.prereqSnapAssertions(c)

	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
`
	snapPath := makeTestSnap(c, snapYamlContent+"version: 1.5")

	si := &snap.SideInfo{
		RealName: "foo",
		SnapID:   fooSnapID,
		Revision: snap.R(55),
	}

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// have the snap-declaration in the system db
	err := assertstate.Add(st, s.devAcct)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, snapDecl)
	c.Assert(err, IsNil)

	_, _, err = snapstate.InstallPath(st, si, snapPath, "bar_invalid_instance_name", "", snapstate.Flags{DevMode: true})
	c.Assert(err, ErrorMatches, `invalid instance name: invalid instance key: "invalid_instance_name"`)
}

func (s *mgrsSuite) TestCheckInterfaces(c *C) {
	snapDecl := s.prereqSnapAssertions(c)

	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
slots:
 network:
`
	snapPath := makeTestSnap(c, snapYamlContent+"version: 1.5")

	si := &snap.SideInfo{
		RealName: "foo",
		SnapID:   fooSnapID,
		Revision: snap.R(55),
	}

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// have the snap-declaration in the system db
	err := assertstate.Add(st, s.devAcct)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, snapDecl)
	c.Assert(err, IsNil)

	// mock SanitizePlugsSlots so that unknown interfaces are not rejected
	restoreSanitize := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	defer restoreSanitize()

	ts, _, err := snapstate.InstallPath(st, si, snapPath, "", "", snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), ErrorMatches, `(?s).*installation not allowed by "network" slot rule of interface "network".*`)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
}

func (s *mgrsSuite) TestHappyRefreshControl(c *C) {
	// test install through store and update, plus some mechanics
	// of update
	// TODO: ok to split if it gets too messy to maintain

	s.prereqSnapAssertions(c)

	snapYamlContent := `name: foo
version: @VERSION@
`

	ver := "1.0"
	revno := "42"
	snapPath, _ := s.makeStoreTestSnap(c, strings.Replace(snapYamlContent, "@VERSION@", ver, -1), revno)
	s.serveSnap(snapPath, revno)

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(context.TODO(), st, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	info, err := snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)

	c.Check(info.Revision, Equals, snap.R(42))

	// Refresh

	// Setup refresh control

	headers := map[string]interface{}{
		"series":          "16",
		"snap-id":         "bar-id",
		"snap-name":       "bar",
		"publisher-id":    "devdevdev",
		"refresh-control": []interface{}{fooSnapID},
		"timestamp":       time.Now().Format(time.RFC3339),
	}
	snapDeclBar, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapDeclBar)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, snapDeclBar)
	c.Assert(err, IsNil)

	snapstate.Set(st, "bar", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "bar", SnapID: "bar-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	develSigning := assertstest.NewSigningDB("devdevdev", develPrivKey)

	develAccKey := assertstest.NewAccountKey(s.storeSigning, s.devAcct, nil, develPrivKey.PublicKey(), "")
	err = s.storeSigning.Add(develAccKey)
	c.Assert(err, IsNil)

	ver = "2.0"
	revno = "50"
	snapPath, _ = s.makeStoreTestSnap(c, strings.Replace(snapYamlContent, "@VERSION@", ver, -1), revno)
	s.serveSnap(snapPath, revno)

	updated, tss, err := snapstate.UpdateMany(context.TODO(), st, []string{"foo"}, 0, nil)
	c.Check(updated, IsNil)
	c.Check(tss, IsNil)
	// no validation we, get an error
	c.Check(err, ErrorMatches, `cannot refresh "foo" to revision 50: no validation by "bar"`)

	// setup validation
	headers = map[string]interface{}{
		"series":                 "16",
		"snap-id":                "bar-id",
		"approved-snap-id":       fooSnapID,
		"approved-snap-revision": "50",
		"timestamp":              time.Now().Format(time.RFC3339),
	}
	barValidation, err := develSigning.Sign(asserts.ValidationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(barValidation)
	c.Assert(err, IsNil)

	// ... and try again
	updated, tss, err = snapstate.UpdateMany(context.TODO(), st, []string{"foo"}, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(updated, DeepEquals, []string{"foo"})
	c.Assert(tss, HasLen, 2)
	verifyLastTasksetIsRerefresh(c, tss)
	chg = st.NewChange("upgrade-snaps", "...")
	chg.AddAll(tss[0])

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	info, err = snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)

	c.Check(info.Revision, Equals, snap.R(50))
}

// core & kernel

var modelDefaults = map[string]interface{}{
	"architecture": "amd64",
	"store":        "my-brand-store-id",
	"gadget":       "pc",
	"kernel":       "pc-kernel",
}

func findKind(chg *state.Change, kind string) *state.Task {
	for _, t := range chg.Tasks() {
		if t.Kind() == kind {
			return t
		}
	}
	return nil
}

func (s *mgrsSuite) TestInstallCoreSnapUpdatesBootloaderEnvAndSplitsAcrossRestart(c *C) {
	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootBase("core_99.snap")

	restore := release.MockOnClassic(false)
	defer restore()

	model := s.brands.Model("my-brand", "my-model", modelDefaults)

	const packageOS = `
name: core
version: 16.04-1
type: os
`
	snapPath := makeTestSnap(c, packageOS)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// setup model assertion
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "core"}, snapPath, "", "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// final steps will are post poned until we are in the restarted snapd
	ok, rst := st.Restarting()
	c.Assert(ok, Equals, true)
	c.Assert(rst, Equals, state.RestartSystem)

	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoingStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	// this is already set
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core_99.snap",
		"snap_try_core":   "core_x1.snap",
		"snap_try_kernel": "",
		"snap_mode":       boot.TryStatus,
	})

	// simulate successful restart happened
	state.MockRestarting(st, state.RestartUnset)
	bloader.BootVars["snap_mode"] = boot.DefaultStatus
	bloader.SetBootBase("core_x1.snap")

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))
}

type rebootEnv interface {
	SetTryingDuringReboot(which []snap.Type) error
	SetRollbackAcrossReboot(which []snap.Type) error
}

func (s *baseMgrsSuite) mockSuccessfulReboot(c *C, be rebootEnv, which []snap.Type) {
	st := s.o.State()
	restarting, restartType := st.Restarting()
	c.Assert(restarting, Equals, true, Commentf("mockSuccessfulReboot called when there was no pending restart"))
	c.Assert(restartType, Equals, state.RestartSystem, Commentf("mockSuccessfulReboot called but restartType is not SystemRestart but %v", restartType))
	state.MockRestarting(st, state.RestartUnset)
	err := be.SetTryingDuringReboot(which)
	c.Assert(err, IsNil)
	s.o.DeviceManager().ResetBootOk()
	st.Unlock()
	defer st.Lock()
	err = s.o.DeviceManager().Ensure()
	c.Assert(err, IsNil)
}

func (s *baseMgrsSuite) mockRollbackAcrossReboot(c *C, be rebootEnv, which []snap.Type) {
	st := s.o.State()
	restarting, restartType := st.Restarting()
	c.Assert(restarting, Equals, true, Commentf("mockRollbackAcrossReboot called when there was no pending restart"))
	c.Assert(restartType, Equals, state.RestartSystem, Commentf("mockRollbackAcrossReboot called but restartType is not SystemRestart but %v", restartType))
	state.MockRestarting(st, state.RestartUnset)
	err := be.SetRollbackAcrossReboot(which)
	c.Assert(err, IsNil)
	s.o.DeviceManager().ResetBootOk()
	st.Unlock()
	s.o.Settle(settleTimeout)
	st.Lock()
}

func (s *mgrsSuite) TestInstallKernelSnapUpdatesBootloaderEnv(c *C) {
	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore := release.MockOnClassic(false)
	defer restore()

	model := s.brands.Model("my-brand", "my-model", modelDefaults)

	const packageKernel = `
name: pc-kernel
version: 4.0-1
type: kernel`

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	snapPath := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// pretend we have core18/pc-kernel
	bloader.BootVars = map[string]string{
		"snap_core":   "core18_2.snap",
		"snap_kernel": "pc-kernel_123.snap",
		"snap_mode":   boot.DefaultStatus,
	}
	si1 := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(123)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, packageKernel, si1, [][]string{
		{"meta/kernel.yaml", ""},
	})
	si2 := &snap.SideInfo{RealName: "core18", Revision: snap.R(2)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
	})

	// setup model assertion
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "pc-kernel"}, snapPath, "", "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	// run, this will trigger a wait for the restart
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	// we are in restarting state and the change is not done yet
	restarting, _ := st.Restarting()
	c.Check(restarting, Equals, true)
	c.Check(chg.Status(), Equals, state.DoingStatus)

	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core18_2.snap",
		"snap_try_core":   "",
		"snap_kernel":     "pc-kernel_123.snap",
		"snap_try_kernel": "pc-kernel_x1.snap",
		"snap_mode":       boot.TryStatus,
	})
	// pretend we restarted
	s.mockSuccessfulReboot(c, bloader, []snap.Type{snap.TypeKernel})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))
}

func (s *mgrsSuite) TestInstallKernelSnapUndoUpdatesBootloaderEnv(c *C) {
	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore := release.MockOnClassic(false)
	defer restore()

	model := s.brands.Model("my-brand", "my-model", modelDefaults)

	const packageKernel = `
name: pc-kernel
version: 4.0-1
type: kernel`

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	snapPath := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// pretend we have core18/pc-kernel
	bloader.BootVars = map[string]string{
		"snap_core":   "core18_2.snap",
		"snap_kernel": "pc-kernel_123.snap",
		"snap_mode":   boot.DefaultStatus,
	}
	si1 := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(123)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, packageKernel, si1, [][]string{
		{"meta/kernel.yaml", ""},
	})
	si2 := &snap.SideInfo{RealName: "core18", Revision: snap.R(2)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
	})

	// setup model assertion
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "pc-kernel"}, snapPath, "", "", snapstate.Flags{})
	c.Assert(err, IsNil)

	terr := st.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(ts.Tasks()[len(ts.Tasks())-1])
	ts.AddTask(terr)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	// run, this will trigger a wait for the restart
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core18_2.snap",
		"snap_kernel":     "pc-kernel_123.snap",
		"snap_try_kernel": "pc-kernel_x1.snap",
		"snap_mode":       boot.TryStatus,
		"snap_try_core":   "",
	})

	// we are in restarting state and the change is not done yet
	restarting, _ := st.Restarting()
	c.Check(restarting, Equals, true)
	c.Check(chg.Status(), Equals, state.DoingStatus)
	// pretend we restarted
	s.mockSuccessfulReboot(c, bloader, []snap.Type{snap.TypeKernel})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// and we undo the bootvars and trigger a reboot
	c.Check(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core18_2.snap",
		"snap_try_core":   "",
		"snap_try_kernel": "pc-kernel_123.snap",
		"snap_kernel":     "pc-kernel_x1.snap",
		"snap_mode":       boot.TryStatus,
	})
	restarting, _ = st.Restarting()
	c.Check(restarting, Equals, true)
}

func (s *mgrsSuite) TestInstallKernelSnap20UpdatesBootloaderEnv(c *C) {
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// we have revision 1 installed
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	restore := bloader.SetEnabledKernel(kernel)
	defer restore()

	restore = release.MockOnClassic(false)
	defer restore()

	uc20ModelDefaults := map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"store":        "my-brand-store-id",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			}},
	}

	model := s.brands.Model("my-brand", "my-model", uc20ModelDefaults)

	const packageKernel = `
name: pc-kernel
version: 4.0-1
type: kernel`

	files := [][]string{
		{"kernel.efi", "I'm a kernel.efi"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	kernelSnapSideInfo := &snap.SideInfo{RealName: "pc-kernel"}
	kernelSnapPath, kernelSnapInfo := snaptest.MakeTestSnapInfoWithFiles(c, packageKernel, files, kernelSnapSideInfo)

	// mock the modeenv file
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191127",
		Base:           "core20_1.snap",
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	si1 := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, packageKernel, si1, [][]string{
		{"meta/kernel.yaml", ""},
	})
	si2 := &snap.SideInfo{RealName: "core20", Revision: snap.R(1)}
	snapstate.Set(st, "core20", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
	})

	// setup model assertion
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "pc-kernel"}, kernelSnapPath, "", "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	// run, this will trigger a wait for the restart
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"kernel_status": boot.TryStatus,
	})

	// we are in restarting state and the change is not done yet
	restarting, _ := st.Restarting()
	c.Check(restarting, Equals, true)
	c.Check(chg.Status(), Equals, state.DoingStatus)

	// the kernelSnapInfo we mocked earlier will not have a revision set for the
	// SideInfo, but since the previous revision was "1", the next revision will
	// be x1 since it's unasserted, so we can set the Revision on the SideInfo
	// here to make comparison easier
	kernelSnapInfo.SideInfo.Revision = snap.R(-1)

	// the current kernel in the bootloader is still the same
	currentKernel, err := bloader.Kernel()
	c.Assert(err, IsNil)
	firstKernel := snap.Info{SideInfo: *si1}
	c.Assert(currentKernel.Filename(), Equals, firstKernel.Filename())

	// the current try kernel in the bootloader is our new kernel
	currentTryKernel, err := bloader.TryKernel()
	c.Assert(err, IsNil)
	c.Assert(currentTryKernel.Filename(), Equals, kernelSnapInfo.Filename())

	// check that we extracted the kernel snap assets
	extractedKernels := bloader.ExtractKernelAssetsCalls
	c.Assert(extractedKernels, HasLen, 1)
	c.Assert(extractedKernels[0].Filename(), Equals, kernelSnapInfo.Filename())

	// pretend we restarted
	s.mockSuccessfulReboot(c, bloader, []snap.Type{snap.TypeKernel})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	// also check that we are active on the second revision
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "pc-kernel", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Sequence, HasLen, 2)
	c.Check(snapst.Sequence, DeepEquals, []*snap.SideInfo{si1, &kernelSnapInfo.SideInfo})
	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Current, DeepEquals, snap.R(-1))

	// since we need to do a reboot to go back to the old kernel, we should now
	// have kernel on the bootloader as the new one, and no try kernel on the
	// bootloader
	finalCurrentKernel, err := bloader.Kernel()
	c.Assert(err, IsNil)
	c.Assert(finalCurrentKernel.Filename(), Equals, kernelSnapInfo.Filename())

	_, err = bloader.TryKernel()
	c.Assert(err, Equals, bootloader.ErrNoTryKernelRef)

	// finally check that GetCurrentBoot gives us the new kernel
	dev, err := devicestate.DeviceCtx(st, nil, nil)
	c.Assert(err, IsNil)
	sn, err := boot.GetCurrentBoot(snap.TypeKernel, dev)
	c.Assert(err, IsNil)
	c.Assert(sn.Filename(), Equals, kernelSnapInfo.Filename())
}

func (s *mgrsSuite) TestInstallKernelSnap20UndoUpdatesBootloaderEnv(c *C) {
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// we have revision 1 installed
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	restore := bloader.SetEnabledKernel(kernel)
	defer restore()

	restore = release.MockOnClassic(false)
	defer restore()

	uc20ModelDefaults := map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"store":        "my-brand-store-id",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			}},
	}

	model := s.brands.Model("my-brand", "my-model", uc20ModelDefaults)

	const packageKernel = `
name: pc-kernel
version: 4.0-1
type: kernel`

	files := [][]string{
		{"kernel.efi", "I'm a kernel.efi"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	kernelSnapSideInfo := &snap.SideInfo{RealName: "pc-kernel"}
	kernelSnapPath, kernelSnapInfo := snaptest.MakeTestSnapInfoWithFiles(c, packageKernel, files, kernelSnapSideInfo)

	// mock the modeenv file
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191127",
		Base:           "core20_1.snap",
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	si1 := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, packageKernel, si1, [][]string{
		{"meta/kernel.yaml", ""},
	})
	si2 := &snap.SideInfo{RealName: "core20", Revision: snap.R(1)}
	snapstate.Set(st, "core20", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
	})

	// setup model assertion
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "pc-kernel"}, kernelSnapPath, "", "", snapstate.Flags{})
	c.Assert(err, IsNil)

	terr := st.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(ts.Tasks()[len(ts.Tasks())-1])
	ts.AddTask(terr)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	// run, this will trigger a wait for the restart
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"kernel_status": boot.TryStatus,
	})

	// the kernelSnapInfo we mocked earlier will not have a revision set for the
	// SideInfo, but since the previous revision was "1", the next revision will
	// be x1 since it's unasserted, so we can set the Revision on the SideInfo
	// here to make comparison easier
	kernelSnapInfo.SideInfo.Revision = snap.R(-1)

	// check that we extracted the kernel snap assets
	extractedKernels := bloader.ExtractKernelAssetsCalls
	c.Assert(extractedKernels, HasLen, 1)
	c.Assert(extractedKernels[0].Filename(), Equals, kernelSnapInfo.Filename())

	// the current kernel in the bootloader is still the same
	currentKernel, err := bloader.Kernel()
	c.Assert(err, IsNil)
	firstKernel := snap.Info{SideInfo: *si1}
	c.Assert(currentKernel.Filename(), Equals, firstKernel.Filename())

	// the current try kernel in the bootloader is our new kernel
	currentTryKernel, err := bloader.TryKernel()
	c.Assert(err, IsNil)
	c.Assert(currentTryKernel.Filename(), Equals, kernelSnapInfo.Filename())

	// we are in restarting state and the change is not done yet
	restarting, _ := st.Restarting()
	c.Check(restarting, Equals, true)
	c.Check(chg.Status(), Equals, state.DoingStatus)
	// pretend we restarted
	s.mockSuccessfulReboot(c, bloader, []snap.Type{snap.TypeKernel})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// we should have triggered a reboot to undo the boot changes
	restarting, _ = st.Restarting()
	c.Check(restarting, Equals, true)

	// we need to reboot with a "new" try kernel, so kernel_status was set again
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"kernel_status": boot.TryStatus,
	})

	// we should not have extracted any more kernel assets than before, since
	// the fallback kernel was already extracted
	extractedKernels = bloader.ExtractKernelAssetsCalls
	c.Assert(extractedKernels, HasLen, 1) // same as above check

	// also check that we are active on the first revision again
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "pc-kernel", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Sequence, DeepEquals, []*snap.SideInfo{si1})
	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Current, DeepEquals, snap.R(1))

	// since we need to do a reboot to go back to the old kernel, we should now
	// have kernel on the bootloader as the new one, and the try kernel on the
	// booloader as the old one
	finalCurrentKernel, err := bloader.Kernel()
	c.Assert(err, IsNil)
	c.Assert(finalCurrentKernel.Filename(), Equals, kernelSnapInfo.Filename())

	finalTryKernel, err := bloader.TryKernel()
	c.Assert(err, IsNil)
	c.Assert(finalTryKernel.Filename(), Equals, firstKernel.Filename())

	// TODO:UC20: this test should probably simulate another reboot and confirm
	// that at the end of everything we have GetCurrentBoot() return the old
	// kernel we reverted back to again
}

func (s *mgrsSuite) installLocalTestSnap(c *C, snapYamlContent string) *snap.Info {
	st := s.o.State()

	snapPath := makeTestSnap(c, snapYamlContent)
	snapf, err := snapfile.Open(snapPath)
	c.Assert(err, IsNil)
	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, IsNil)

	// store current state
	snapName := info.InstanceName()
	var snapst snapstate.SnapState
	snapstate.Get(st, snapName, &snapst)

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: snapName}, snapPath, "", "", snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	return info
}

func (s *mgrsSuite) removeSnap(c *C, name string) {
	st := s.o.State()

	ts, err := snapstate.Remove(st, name, snap.R(0), &snapstate.RemoveFlags{Purge: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("remove-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("remove-snap change failed with: %v", chg.Err()))
}

func (s *mgrsSuite) TestHappyRevert(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	x1Yaml := `name: foo
version: 1.0
apps:
 x1:
  command: bin/bar
`
	x1binary := filepath.Join(dirs.SnapBinariesDir, "foo.x1")

	x2Yaml := `name: foo
version: 2.0
apps:
 x2:
  command: bin/bar
`
	x2binary := filepath.Join(dirs.SnapBinariesDir, "foo.x2")

	s.installLocalTestSnap(c, x1Yaml)
	s.installLocalTestSnap(c, x2Yaml)

	// ensure we are on x2
	_, err := os.Lstat(x2binary)
	c.Assert(err, IsNil)
	_, err = os.Lstat(x1binary)
	c.Assert(err, ErrorMatches, ".*no such file.*")

	// now do the revert
	ts, err := snapstate.Revert(st, "foo", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("revert-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("revert-snap change failed with: %v", chg.Err()))

	// ensure that we use x1 now
	_, err = os.Lstat(x1binary)
	c.Assert(err, IsNil)
	_, err = os.Lstat(x2binary)
	c.Assert(err, ErrorMatches, ".*no such file.*")

	// ensure that x1,x2 is still there, revert just moves the "current"
	// pointer
	for _, fn := range []string{"foo_x2.snap", "foo_x1.snap"} {
		p := filepath.Join(dirs.SnapBlobDir, fn)
		c.Assert(osutil.FileExists(p), Equals, true)
	}
}

func (s *mgrsSuite) TestHappyAlias(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	fooYaml := `name: foo
version: 1.0
apps:
    foo:
        command: bin/foo
`
	s.installLocalTestSnap(c, fooYaml)

	ts, err := snapstate.Alias(st, "foo", "foo", "foo_")
	c.Assert(err, IsNil)
	chg := st.NewChange("alias", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("alias change failed with: %v", chg.Err()))

	foo_Alias := filepath.Join(dirs.SnapBinariesDir, "foo_")
	dest, err := os.Readlink(foo_Alias)
	c.Assert(err, IsNil)

	c.Check(dest, Equals, "foo")

	var snapst snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"foo_": {Manual: "foo"},
	})

	s.removeSnap(c, "foo")

	c.Check(osutil.IsSymlink(foo_Alias), Equals, false)
}

func (s *mgrsSuite) TestHappyUnalias(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	fooYaml := `name: foo
version: 1.0
apps:
    foo:
        command: bin/foo
`
	s.installLocalTestSnap(c, fooYaml)

	ts, err := snapstate.Alias(st, "foo", "foo", "foo_")
	c.Assert(err, IsNil)
	chg := st.NewChange("alias", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("alias change failed with: %v", chg.Err()))

	foo_Alias := filepath.Join(dirs.SnapBinariesDir, "foo_")
	dest, err := os.Readlink(foo_Alias)
	c.Assert(err, IsNil)

	c.Check(dest, Equals, "foo")

	ts, snapName, err := snapstate.RemoveManualAlias(st, "foo_")
	c.Assert(err, IsNil)
	c.Check(snapName, Equals, "foo")
	chg = st.NewChange("unalias", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("unalias change failed with: %v", chg.Err()))

	c.Check(osutil.IsSymlink(foo_Alias), Equals, false)

	var snapst snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, HasLen, 0)
}

func (s *mgrsSuite) TestHappyRemoteInstallAutoAliases(c *C) {
	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
		"aliases": []interface{}{
			map[string]interface{}{"name": "app1", "target": "app1"},
			map[string]interface{}{"name": "app2", "target": "app2"},
		},
	})

	snapYamlContent := `name: foo
version: @VERSION@
apps:
 app1:
  command: bin/app1
 app2:
  command: bin/app2
`

	ver := "1.0"
	revno := "42"
	snapPath, _ := s.makeStoreTestSnap(c, strings.Replace(snapYamlContent, "@VERSION@", ver, -1), revno)
	s.serveSnap(snapPath, revno)

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(context.TODO(), st, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	var snapst snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"app1": {Auto: "app1"},
		"app2": {Auto: "app2"},
	})

	// check disk
	app1Alias := filepath.Join(dirs.SnapBinariesDir, "app1")
	dest, err := os.Readlink(app1Alias)
	c.Assert(err, IsNil)
	c.Check(dest, Equals, "foo.app1")

	app2Alias := filepath.Join(dirs.SnapBinariesDir, "app2")
	dest, err = os.Readlink(app2Alias)
	c.Assert(err, IsNil)
	c.Check(dest, Equals, "foo.app2")
}

func (s *mgrsSuite) TestHappyRemoteInstallAndUpdateAutoAliases(c *C) {
	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
		"aliases": []interface{}{
			map[string]interface{}{"name": "app1", "target": "app1"},
		},
	})

	fooYaml := `name: foo
version: @VERSION@
apps:
 app1:
  command: bin/app1
 app2:
  command: bin/app2
`

	fooPath, _ := s.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.0", -1), "10")
	s.serveSnap(fooPath, "10")

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(context.TODO(), st, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	info, err := snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)
	c.Check(info.Revision, Equals, snap.R(10))
	c.Check(info.Version, Equals, "1.0")

	var snapst snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"app1": {Auto: "app1"},
	})

	app1Alias := filepath.Join(dirs.SnapBinariesDir, "app1")
	dest, err := os.Readlink(app1Alias)
	c.Assert(err, IsNil)
	c.Check(dest, Equals, "foo.app1")

	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
		"aliases": []interface{}{
			map[string]interface{}{"name": "app2", "target": "app2"},
		},
		"revision": "1",
	})

	// new foo version/revision
	fooPath, _ = s.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.5", -1), "15")
	s.serveSnap(fooPath, "15")

	// refresh all
	updated, tss, err := snapstate.UpdateMany(context.TODO(), st, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(updated, DeepEquals, []string{"foo"})
	c.Assert(tss, HasLen, 2)
	verifyLastTasksetIsRerefresh(c, tss)
	chg = st.NewChange("upgrade-snaps", "...")
	chg.AddAll(tss[0])

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	info, err = snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)
	c.Check(info.Revision, Equals, snap.R(15))
	c.Check(info.Version, Equals, "1.5")

	var snapst2 snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst2)
	c.Assert(err, IsNil)
	c.Check(snapst2.AutoAliasesDisabled, Equals, false)
	c.Check(snapst2.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"app2": {Auto: "app2"},
	})

	c.Check(osutil.IsSymlink(app1Alias), Equals, false)

	app2Alias := filepath.Join(dirs.SnapBinariesDir, "app2")
	dest, err = os.Readlink(app2Alias)
	c.Assert(err, IsNil)
	c.Check(dest, Equals, "foo.app2")
}

func (s *mgrsSuite) TestHappyRemoteInstallAndUpdateAutoAliasesUnaliased(c *C) {
	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
		"aliases": []interface{}{
			map[string]interface{}{"name": "app1", "target": "app1"},
		},
	})

	fooYaml := `name: foo
version: @VERSION@
apps:
 app1:
  command: bin/app1
 app2:
  command: bin/app2
`

	fooPath, _ := s.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.0", -1), "10")
	s.serveSnap(fooPath, "10")

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(context.TODO(), st, "foo", nil, 0, snapstate.Flags{Unaliased: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	info, err := snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)
	c.Check(info.Revision, Equals, snap.R(10))
	c.Check(info.Version, Equals, "1.0")

	var snapst snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"app1": {Auto: "app1"},
	})

	app1Alias := filepath.Join(dirs.SnapBinariesDir, "app1")
	c.Check(osutil.IsSymlink(app1Alias), Equals, false)

	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
		"aliases": []interface{}{
			map[string]interface{}{"name": "app2", "target": "app2"},
		},
		"revision": "1",
	})

	// new foo version/revision
	fooPath, _ = s.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.5", -1), "15")
	s.serveSnap(fooPath, "15")

	// refresh foo
	ts, err = snapstate.Update(st, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg = st.NewChange("upgrade-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	info, err = snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)
	c.Check(info.Revision, Equals, snap.R(15))
	c.Check(info.Version, Equals, "1.5")

	var snapst2 snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst2)
	c.Assert(err, IsNil)
	c.Check(snapst2.AutoAliasesDisabled, Equals, true)
	c.Check(snapst2.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"app2": {Auto: "app2"},
	})

	c.Check(osutil.IsSymlink(app1Alias), Equals, false)

	app2Alias := filepath.Join(dirs.SnapBinariesDir, "app2")
	c.Check(osutil.IsSymlink(app2Alias), Equals, false)
}

func (s *mgrsSuite) TestHappyOrthogonalRefreshAutoAliases(c *C) {
	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
		"aliases": []interface{}{
			map[string]interface{}{"name": "app1", "target": "app1"},
		},
	}, map[string]interface{}{
		"snap-name": "bar",
	})

	fooYaml := `name: foo
version: @VERSION@
apps:
 app1:
  command: bin/app1
 app2:
  command: bin/app2
`

	barYaml := `name: bar
version: @VERSION@
apps:
 app1:
  command: bin/app1
 app3:
  command: bin/app3
`

	fooPath, _ := s.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.0", -1), "10")
	s.serveSnap(fooPath, "10")

	barPath, _ := s.makeStoreTestSnap(c, strings.Replace(barYaml, "@VERSION@", "2.0", -1), "20")
	s.serveSnap(barPath, "20")

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(context.TODO(), st, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	ts, err = snapstate.Install(context.TODO(), st, "bar", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg = st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	info, err := snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)
	c.Check(info.Revision, Equals, snap.R(10))
	c.Check(info.Version, Equals, "1.0")

	info, err = snapstate.CurrentInfo(st, "bar")
	c.Assert(err, IsNil)
	c.Check(info.Revision, Equals, snap.R(20))
	c.Check(info.Version, Equals, "2.0")

	var snapst snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"app1": {Auto: "app1"},
	})

	// foo gets a new version/revision and a change of automatic aliases
	// bar gets only the latter
	// app1 is transferred from foo to bar
	// UpdateMany after a snap-declaration refresh handles all of this
	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
		"aliases": []interface{}{
			map[string]interface{}{"name": "app2", "target": "app2"},
		},
		"revision": "1",
	}, map[string]interface{}{
		"snap-name": "bar",
		"aliases": []interface{}{
			map[string]interface{}{"name": "app1", "target": "app1"},
			map[string]interface{}{"name": "app3", "target": "app3"},
		},
		"revision": "1",
	})

	// new foo version/revision
	fooPath, _ = s.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.5", -1), "15")
	s.serveSnap(fooPath, "15")

	// refresh all
	err = assertstate.RefreshSnapDeclarations(st, 0)
	c.Assert(err, IsNil)

	updated, tss, err := snapstate.UpdateMany(context.TODO(), st, nil, 0, nil)
	c.Assert(err, IsNil)
	sort.Strings(updated)
	c.Assert(updated, DeepEquals, []string{"bar", "foo"})
	c.Assert(tss, HasLen, 4)
	verifyLastTasksetIsRerefresh(c, tss)
	chg = st.NewChange("upgrade-snaps", "...")
	chg.AddAll(tss[0])
	chg.AddAll(tss[1])
	chg.AddAll(tss[2])

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	info, err = snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)
	c.Check(info.Revision, Equals, snap.R(15))
	c.Check(info.Version, Equals, "1.5")

	var snapst2 snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst2)
	c.Assert(err, IsNil)
	c.Check(snapst2.AutoAliasesDisabled, Equals, false)
	c.Check(snapst2.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"app2": {Auto: "app2"},
	})
	var snapst3 snapstate.SnapState
	err = snapstate.Get(st, "bar", &snapst3)
	c.Assert(err, IsNil)
	c.Check(snapst3.AutoAliasesDisabled, Equals, false)
	c.Check(snapst3.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"app1": {Auto: "app1"},
		"app3": {Auto: "app3"},
	})

	app2Alias := filepath.Join(dirs.SnapBinariesDir, "app2")
	dest, err := os.Readlink(app2Alias)
	c.Assert(err, IsNil)
	c.Check(dest, Equals, "foo.app2")

	app1Alias := filepath.Join(dirs.SnapBinariesDir, "app1")
	dest, err = os.Readlink(app1Alias)
	c.Assert(err, IsNil)
	c.Check(dest, Equals, "bar.app1")
	app3Alias := filepath.Join(dirs.SnapBinariesDir, "app3")
	dest, err = os.Readlink(app3Alias)
	c.Assert(err, IsNil)
	c.Check(dest, Equals, "bar.app3")
}

func (s *mgrsSuite) TestHappyStopWhileDownloadingHeader(c *C) {
	s.prereqSnapAssertions(c)

	snapYamlContent := `name: foo
version: 1.0
`
	snapPath, _ := s.makeStoreTestSnap(c, snapYamlContent, "42")
	s.serveSnap(snapPath, "42")

	stopped := make(chan struct{})
	s.hijackServeSnap = func(_ http.ResponseWriter) {
		s.o.Stop()
		close(stopped)
	}

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(context.TODO(), st, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	s.o.Loop()

	<-stopped

	st.Lock()
	c.Assert(chg.Status(), Equals, state.DoingStatus, Commentf("install-snap change failed with: %v", chg.Err()))
}

func (s *mgrsSuite) TestHappyStopWhileDownloadingBody(c *C) {
	s.prereqSnapAssertions(c)

	snapYamlContent := `name: foo
version: 1.0
`
	snapPath, _ := s.makeStoreTestSnap(c, snapYamlContent, "42")
	s.serveSnap(snapPath, "42")

	stopped := make(chan struct{})
	s.hijackServeSnap = func(w http.ResponseWriter) {
		w.WriteHeader(200)
		// best effort to reach the body reading part in the client
		w.Write(make([]byte, 10000))
		time.Sleep(100 * time.Millisecond)
		w.Write(make([]byte, 10000))
		s.o.Stop()
		close(stopped)
	}

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(context.TODO(), st, "foo", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	s.o.Loop()

	<-stopped

	st.Lock()
	c.Assert(chg.Status(), Equals, state.DoingStatus, Commentf("install-snap change failed with: %v", chg.Err()))
}

type storeCtxSetupSuite struct {
	o  *overlord.Overlord
	sc store.DeviceAndAuthContext

	storeSigning   *assertstest.StoreStack
	restoreTrusted func()

	brands *assertstest.SigningAccounts

	deviceKey asserts.PrivateKey

	model  *asserts.Model
	serial *asserts.Serial

	restoreBackends func()
}

func (s *storeCtxSetupSuite) SetUpTest(c *C) {
	tempdir := c.MkDir()
	dirs.SetRootDir(tempdir)
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)

	captureStoreCtx := func(_ *store.Config, dac store.DeviceAndAuthContext) *store.Store {
		s.sc = dac
		return store.New(nil, nil)
	}
	r := overlord.MockStoreNew(captureStoreCtx)
	defer r()

	s.storeSigning = assertstest.NewStoreStack("can0nical", nil)
	s.restoreTrusted = sysdb.InjectTrusted(s.storeSigning.Trusted)

	s.brands = assertstest.NewSigningAccounts(s.storeSigning)
	s.brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})
	assertstest.AddMany(s.storeSigning, s.brands.AccountsAndKeys("my-brand")...)

	s.model = s.brands.Model("my-brand", "my-model", modelDefaults)

	encDevKey, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, IsNil)
	serial, err := s.brands.Signing("my-brand").Sign(asserts.SerialType, map[string]interface{}{
		"authority-id":        "my-brand",
		"brand-id":            "my-brand",
		"model":               "my-model",
		"serial":              "7878",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	s.serial = serial.(*asserts.Serial)

	s.restoreBackends = ifacestate.MockSecurityBackends(nil)

	o, err := overlord.New(nil)
	c.Assert(err, IsNil)
	o.InterfaceManager().DisableUDevMonitor()
	s.o = o

	st := o.State()
	st.Lock()
	defer st.Unlock()

	assertstatetest.AddMany(st, s.storeSigning.StoreAccountKey(""))
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
}

func (s *storeCtxSetupSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.restoreBackends()
	s.restoreTrusted()
}

func (s *storeCtxSetupSuite) TestStoreID(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	st.Unlock()
	storeID, err := s.sc.StoreID("fallback")
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "fallback")

	// setup model in system statey
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  s.serial.BrandID(),
		Model:  s.serial.Model(),
		Serial: s.serial.Serial(),
	})
	err = assertstate.Add(st, s.model)
	c.Assert(err, IsNil)

	st.Unlock()
	storeID, err = s.sc.StoreID("fallback")
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "my-brand-store-id")
}

func (s *storeCtxSetupSuite) TestDeviceSessionRequestParams(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	st.Unlock()
	_, err := s.sc.DeviceSessionRequestParams("NONCE")
	st.Lock()
	c.Check(err, Equals, store.ErrNoSerial)

	// setup model, serial and key in system state
	err = assertstate.Add(st, s.model)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, s.serial)
	c.Assert(err, IsNil)
	kpMgr, err := asserts.OpenFSKeypairManager(dirs.SnapDeviceDir)
	c.Assert(err, IsNil)
	err = kpMgr.Put(deviceKey)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  s.serial.BrandID(),
		Model:  s.serial.Model(),
		Serial: s.serial.Serial(),
		KeyID:  deviceKey.PublicKey().ID(),
	})

	st.Unlock()
	params, err := s.sc.DeviceSessionRequestParams("NONCE")
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(strings.HasPrefix(params.EncodedRequest(), "type: device-session-request\n"), Equals, true)
	c.Check(params.EncodedSerial(), DeepEquals, string(asserts.Encode(s.serial)))
	c.Check(params.EncodedModel(), DeepEquals, string(asserts.Encode(s.model)))

}

func (s *storeCtxSetupSuite) TestProxyStoreParams(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	defURL, err := url.Parse("http://store")
	c.Assert(err, IsNil)

	st.Unlock()
	proxyStoreID, proxyStoreURL, err := s.sc.ProxyStoreParams(defURL)
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(proxyStoreID, Equals, "")
	c.Check(proxyStoreURL, Equals, defURL)

	// setup proxy store reference and assertion
	operatorAcct := assertstest.NewAccount(s.storeSigning, "foo-operator", nil, "")
	err = assertstate.Add(st, operatorAcct)
	c.Assert(err, IsNil)
	stoAs, err := s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":       "foo",
		"operator-id": operatorAcct.AccountID(),
		"url":         "http://foo.internal",
		"timestamp":   time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(st, stoAs)
	c.Assert(err, IsNil)
	tr := config.NewTransaction(st)
	err = tr.Set("core", "proxy.store", "foo")
	c.Assert(err, IsNil)
	tr.Commit()

	fooURL, err := url.Parse("http://foo.internal")
	c.Assert(err, IsNil)

	st.Unlock()
	proxyStoreID, proxyStoreURL, err = s.sc.ProxyStoreParams(defURL)
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(proxyStoreID, Equals, "foo")
	c.Check(proxyStoreURL, DeepEquals, fooURL)
}

const snapYamlContent1 = `name: snap1
plugs:
 shared-data-plug:
  interface: content
  target: import
  content: mylib
apps:
 bar:
  command: bin/bar
`
const snapYamlContent2 = `name: snap2
slots:
 shared-data-slot:
  interface: content
  content: mylib
  read:
   - /
apps:
 bar:
  command: bin/bar
`

func (s *mgrsSuite) testTwoInstalls(c *C, snapName1, snapYaml1, snapName2, snapYaml2 string) {
	snapPath1 := makeTestSnap(c, snapYaml1+"version: 1.0")
	snapPath2 := makeTestSnap(c, snapYaml2+"version: 1.0")

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts1, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: snapName1, SnapID: fakeSnapID(snapName1), Revision: snap.R(3)}, snapPath1, "", "", snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts1)

	ts2, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: snapName2, SnapID: fakeSnapID(snapName2), Revision: snap.R(3)}, snapPath2, "", "", snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)

	ts2.WaitAll(ts1)
	chg.AddAll(ts2)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	tasks := chg.Tasks()
	connectTask := tasks[len(tasks)-2]
	c.Assert(connectTask.Kind(), Equals, "connect")

	setupProfilesTask := tasks[len(tasks)-1]
	c.Assert(setupProfilesTask.Kind(), Equals, "setup-profiles")

	// verify connect task data
	var plugRef interfaces.PlugRef
	var slotRef interfaces.SlotRef
	c.Assert(connectTask.Get("plug", &plugRef), IsNil)
	c.Assert(connectTask.Get("slot", &slotRef), IsNil)
	c.Assert(plugRef.Snap, Equals, "snap1")
	c.Assert(plugRef.Name, Equals, "shared-data-plug")
	c.Assert(slotRef.Snap, Equals, "snap2")
	c.Assert(slotRef.Name, Equals, "shared-data-slot")

	// verify that connection was made
	var conns map[string]interface{}
	c.Assert(st.Get("conns", &conns), IsNil)
	c.Assert(conns, HasLen, 1)

	repo := s.o.InterfaceManager().Repository()
	cn, err := repo.Connected("snap1", "shared-data-plug")
	c.Assert(err, IsNil)
	c.Assert(cn, HasLen, 1)
	c.Assert(cn, DeepEquals, []*interfaces.ConnRef{{
		PlugRef: interfaces.PlugRef{Snap: "snap1", Name: "shared-data-plug"},
		SlotRef: interfaces.SlotRef{Snap: "snap2", Name: "shared-data-slot"},
	}})
}

func (s *mgrsSuite) TestTwoInstallsWithAutoconnectPlugSnapFirst(c *C) {
	s.testTwoInstalls(c, "snap1", snapYamlContent1, "snap2", snapYamlContent2)
}

func (s *mgrsSuite) TestTwoInstallsWithAutoconnectSlotSnapFirst(c *C) {
	s.testTwoInstalls(c, "snap2", snapYamlContent2, "snap1", snapYamlContent1)
}

func (s *mgrsSuite) TestRemoveAndInstallWithAutoconnectHappy(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	_ = s.installLocalTestSnap(c, snapYamlContent1+"version: 1.0")

	ts, err := snapstate.Remove(st, "snap1", snap.R(0), &snapstate.RemoveFlags{Purge: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("remove-snap", "...")
	chg.AddAll(ts)

	snapPath := makeTestSnap(c, snapYamlContent2+"version: 1.0")
	chg2 := st.NewChange("install-snap", "...")
	ts2, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "snap2", SnapID: fakeSnapID("snap2"), Revision: snap.R(3)}, snapPath, "", "", snapstate.Flags{DevMode: true})
	chg2.AddAll(ts2)
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("remove-snap change failed with: %v", chg.Err()))
	c.Assert(chg2.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))
}

const otherSnapYaml = `name: other-snap
version: 1.0
apps:
   baz:
        command: bin/bar
        plugs: [media-hub]
`

func (s *mgrsSuite) TestUpdateManyWithAutoconnect(c *C) {
	const someSnapYaml = `name: some-snap
version: 1.0
apps:
   foo:
        command: bin/bar
        plugs: [network,home]
        slots: [media-hub]
`

	const coreSnapYaml = `name: core
type: os
version: @VERSION@`

	snapPath, _ := s.makeStoreTestSnap(c, someSnapYaml, "40")
	s.serveSnap(snapPath, "40")

	snapPath, _ = s.makeStoreTestSnap(c, otherSnapYaml, "50")
	s.serveSnap(snapPath, "50")

	corePath, _ := s.makeStoreTestSnap(c, strings.Replace(coreSnapYaml, "@VERSION@", "30", -1), "30")
	s.serveSnap(corePath, "30")

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	st.Set("conns", map[string]interface{}{})

	si := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(1)}
	snapInfo := snaptest.MockSnap(c, someSnapYaml, si)
	c.Assert(snapInfo.Plugs, HasLen, 2)

	oi := &snap.SideInfo{RealName: "other-snap", SnapID: fakeSnapID("other-snap"), Revision: snap.R(1)}
	otherInfo := snaptest.MockSnap(c, otherSnapYaml, oi)
	c.Assert(otherInfo.Plugs, HasLen, 1)

	csi := &snap.SideInfo{RealName: "core", SnapID: fakeSnapID("core"), Revision: snap.R(1)}
	coreInfo := snaptest.MockSnap(c, strings.Replace(coreSnapYaml, "@VERSION@", "1", -1), csi)

	// add implicit slots
	coreInfo.Slots["network"] = &snap.SlotInfo{
		Name:      "network",
		Snap:      coreInfo,
		Interface: "network",
	}
	coreInfo.Slots["home"] = &snap.SlotInfo{
		Name:      "home",
		Snap:      coreInfo,
		Interface: "home",
	}

	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(st, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{oi},
		Current:  snap.R(1),
		SnapType: "app",
	})

	repo := s.o.InterfaceManager().Repository()

	// add snaps to the repo to have plugs/slots
	c.Assert(repo.AddSnap(snapInfo), IsNil)
	c.Assert(repo.AddSnap(otherInfo), IsNil)
	c.Assert(repo.AddSnap(coreInfo), IsNil)

	// refresh all
	err := assertstate.RefreshSnapDeclarations(st, 0)
	c.Assert(err, IsNil)

	updates, tts, err := snapstate.UpdateMany(context.TODO(), st, []string{"core", "some-snap", "other-snap"}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(updates, HasLen, 3)
	c.Assert(tts, HasLen, 4)
	verifyLastTasksetIsRerefresh(c, tts)

	// to make TaskSnapSetup work
	chg := st.NewChange("refresh", "...")
	for _, ts := range tts[:len(tts)-1] {
		chg.AddAll(ts)
	}

	// force hold state to hit ignore status of findSymmetricAutoconnect
	tts[2].Tasks()[0].SetStatus(state.HoldStatus)

	st.Unlock()
	err = s.o.Settle(3 * time.Second)
	st.Lock()
	c.Assert(err, IsNil)

	// simulate successful restart happened
	state.MockRestarting(st, state.RestartUnset)
	tts[2].Tasks()[0].SetStatus(state.DefaultStatus)
	st.Unlock()

	err = s.o.Settle(settleTimeout)
	st.Lock()

	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// check connections
	var conns map[string]interface{}
	st.Get("conns", &conns)
	c.Assert(conns, DeepEquals, map[string]interface{}{
		"some-snap:home core:home":                 map[string]interface{}{"interface": "home", "auto": true},
		"some-snap:network core:network":           map[string]interface{}{"interface": "network", "auto": true},
		"other-snap:media-hub some-snap:media-hub": map[string]interface{}{"interface": "media-hub", "auto": true},
	})

	connections, err := repo.Connections("some-snap")
	c.Assert(err, IsNil)
	c.Assert(connections, HasLen, 3)
}

func (s *mgrsSuite) TestUpdateWithAutoconnectAndInactiveRevisions(c *C) {
	const someSnapYaml = `name: some-snap
version: 1.0
apps:
   foo:
        command: bin/bar
        plugs: [network]
`
	const coreSnapYaml = `name: core
type: os
version: 1`

	snapPath, _ := s.makeStoreTestSnap(c, someSnapYaml, "40")
	s.serveSnap(snapPath, "40")

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	si1 := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(1)}
	snapInfo := snaptest.MockSnap(c, someSnapYaml, si1)
	c.Assert(snapInfo.Plugs, HasLen, 1)

	csi := &snap.SideInfo{RealName: "core", SnapID: fakeSnapID("core"), Revision: snap.R(1)}
	coreInfo := snaptest.MockSnap(c, coreSnapYaml, csi)

	// add implicit slots
	coreInfo.Slots["network"] = &snap.SlotInfo{
		Name:      "network",
		Snap:      coreInfo,
		Interface: "network",
	}

	// some-snap has inactive revisions
	si0 := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(0)}
	si2 := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(2)}
	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si0, si1, si2},
		Current:  snap.R(1),
		SnapType: "app",
	})

	repo := s.o.InterfaceManager().Repository()

	// add snaps to the repo to have plugs/slots
	c.Assert(repo.AddSnap(snapInfo), IsNil)
	c.Assert(repo.AddSnap(coreInfo), IsNil)

	// refresh all
	err := assertstate.RefreshSnapDeclarations(st, 0)
	c.Assert(err, IsNil)

	updates, tts, err := snapstate.UpdateMany(context.TODO(), st, []string{"some-snap"}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(updates, HasLen, 1)
	c.Assert(tts, HasLen, 2)
	verifyLastTasksetIsRerefresh(c, tts)

	// to make TaskSnapSetup work
	chg := st.NewChange("refresh", "...")
	chg.AddAll(tts[0])

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()

	c.Assert(err, IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// check connections
	var conns map[string]interface{}
	st.Get("conns", &conns)
	c.Assert(conns, DeepEquals, map[string]interface{}{
		"some-snap:network core:network": map[string]interface{}{"interface": "network", "auto": true},
	})
}

const someSnapYaml = `name: some-snap
version: 1.0
apps:
   foo:
        command: bin/bar
        slots: [media-hub]
`

func (s *mgrsSuite) testUpdateWithAutoconnectRetry(c *C, updateSnapName, removeSnapName string) {
	snapPath, _ := s.makeStoreTestSnap(c, someSnapYaml, "40")
	s.serveSnap(snapPath, "40")

	snapPath, _ = s.makeStoreTestSnap(c, otherSnapYaml, "50")
	s.serveSnap(snapPath, "50")

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	st.Set("conns", map[string]interface{}{})

	si := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(1)}
	snapInfo := snaptest.MockSnap(c, someSnapYaml, si)
	c.Assert(snapInfo.Slots, HasLen, 1)

	oi := &snap.SideInfo{RealName: "other-snap", SnapID: fakeSnapID("other-snap"), Revision: snap.R(1)}
	otherInfo := snaptest.MockSnap(c, otherSnapYaml, oi)
	c.Assert(otherInfo.Plugs, HasLen, 1)

	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(st, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{oi},
		Current:  snap.R(1),
		SnapType: "app",
	})

	repo := s.o.InterfaceManager().Repository()

	// add snaps to the repo to have plugs/slots
	c.Assert(repo.AddSnap(snapInfo), IsNil)
	c.Assert(repo.AddSnap(otherInfo), IsNil)

	// refresh all
	err := assertstate.RefreshSnapDeclarations(st, 0)
	c.Assert(err, IsNil)

	ts, err := snapstate.Update(st, updateSnapName, nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	// to make TaskSnapSetup work
	chg := st.NewChange("refresh", "...")
	chg.AddAll(ts)

	// remove other-snap
	ts2, err := snapstate.Remove(st, removeSnapName, snap.R(0), &snapstate.RemoveFlags{Purge: true})
	c.Assert(err, IsNil)
	chg2 := st.NewChange("remove-snap", "...")
	chg2.AddAll(ts2)

	// force hold state on first removal task to hit Retry error
	ts2.Tasks()[0].SetStatus(state.HoldStatus)

	// Settle is not converging here because of the task in Hold status, therefore
	// it always hits given timeout before we carry on with the test. We're
	// interested in hitting the retry condition on auto-connect task, so
	// instead of passing a generous timeout to Settle(), repeat Settle() a number
	// of times with an aggressive timeout and break as soon as we reach the desired
	// state of auto-connect task.
	var retryCheck bool
	var autoconnectLog string
	for i := 0; i < 50 && !retryCheck; i++ {
		st.Unlock()
		s.o.Settle(aggressiveSettleTimeout)
		st.Lock()

		for _, t := range st.Tasks() {
			if t.Kind() == "auto-connect" && t.Status() == state.DoingStatus && strings.Contains(strings.Join(t.Log(), ""), "Waiting") {
				autoconnectLog = strings.Join(t.Log(), "")
				retryCheck = true
				break
			}
		}
	}

	c.Check(retryCheck, Equals, true)
	c.Assert(autoconnectLog, Matches, `.*Waiting for conflicting change in progress: conflicting snap.*`)

	// back to default state, that will unblock autoconnect
	ts2.Tasks()[0].SetStatus(state.DefaultStatus)
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Check(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// check connections
	var conns map[string]interface{}
	st.Get("conns", &conns)
	c.Assert(conns, HasLen, 0)
}

func (s *mgrsSuite) TestUpdateWithAutoconnectRetrySlotSide(c *C) {
	s.testUpdateWithAutoconnectRetry(c, "some-snap", "other-snap")
}

func (s *mgrsSuite) TestUpdateWithAutoconnectRetryPlugSide(c *C) {
	s.testUpdateWithAutoconnectRetry(c, "other-snap", "some-snap")
}

func (s *mgrsSuite) TestDisconnectIgnoredOnSymmetricRemove(c *C) {
	const someSnapYaml = `name: some-snap
version: 1.0
apps:
   foo:
        command: bin/bar
        slots: [media-hub]
hooks:
   disconnect-slot-media-hub:
`
	const otherSnapYaml = `name: other-snap
version: 1.0
apps:
   baz:
        command: bin/bar
        plugs: [media-hub]
hooks:
   disconnect-plug-media-hub:
`
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	st.Set("conns", map[string]interface{}{
		"other-snap:media-hub some-snap:media-hub": map[string]interface{}{"interface": "media-hub", "auto": false},
	})

	si := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(1)}
	snapInfo := snaptest.MockSnap(c, someSnapYaml, si)
	c.Assert(snapInfo.Slots, HasLen, 1)

	oi := &snap.SideInfo{RealName: "other-snap", SnapID: fakeSnapID("other-snap"), Revision: snap.R(1)}
	otherInfo := snaptest.MockSnap(c, otherSnapYaml, oi)
	c.Assert(otherInfo.Plugs, HasLen, 1)

	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(st, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{oi},
		Current:  snap.R(1),
		SnapType: "app",
	})

	repo := s.o.InterfaceManager().Repository()

	// add snaps to the repo to have plugs/slots
	c.Assert(repo.AddSnap(snapInfo), IsNil)
	c.Assert(repo.AddSnap(otherInfo), IsNil)
	repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "other-snap", Name: "media-hub"},
		SlotRef: interfaces.SlotRef{Snap: "some-snap", Name: "media-hub"},
	}, nil, nil, nil, nil, nil)

	flags := &snapstate.RemoveFlags{Purge: true}
	ts, err := snapstate.Remove(st, "some-snap", snap.R(0), flags)
	c.Assert(err, IsNil)
	chg := st.NewChange("uninstall", "...")
	chg.AddAll(ts)

	// remove other-snap
	ts2, err := snapstate.Remove(st, "other-snap", snap.R(0), flags)
	c.Assert(err, IsNil)
	chg2 := st.NewChange("uninstall", "...")
	chg2.AddAll(ts2)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// check connections
	var conns map[string]interface{}
	st.Get("conns", &conns)
	c.Assert(conns, HasLen, 0)

	var disconnectInterfacesCount, slotHookCount, plugHookCount int
	for _, t := range st.Tasks() {
		if t.Kind() == "auto-disconnect" {
			disconnectInterfacesCount++
		}
		if t.Kind() == "run-hook" {
			var hsup hookstate.HookSetup
			c.Assert(t.Get("hook-setup", &hsup), IsNil)
			if hsup.Hook == "disconnect-plug-media-hub" {
				plugHookCount++
			}
			if hsup.Hook == "disconnect-slot-media-hub" {
				slotHookCount++
			}
		}
	}
	c.Assert(plugHookCount, Equals, 1)
	c.Assert(slotHookCount, Equals, 1)
	c.Assert(disconnectInterfacesCount, Equals, 2)

	var snst snapstate.SnapState
	err = snapstate.Get(st, "other-snap", &snst)
	c.Assert(err, Equals, state.ErrNoState)
	_, err = repo.Connected("other-snap", "media-hub")
	c.Assert(err, ErrorMatches, `snap "other-snap" has no plug or slot named "media-hub"`)
}

func (s *mgrsSuite) TestDisconnectOnUninstallRemovesAutoconnection(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	st.Set("conns", map[string]interface{}{
		"other-snap:media-hub some-snap:media-hub": map[string]interface{}{"interface": "media-hub", "auto": true},
	})

	si := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(1)}
	snapInfo := snaptest.MockSnap(c, someSnapYaml, si)

	oi := &snap.SideInfo{RealName: "other-snap", SnapID: fakeSnapID("other-snap"), Revision: snap.R(1)}
	otherInfo := snaptest.MockSnap(c, otherSnapYaml, oi)

	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(st, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{oi},
		Current:  snap.R(1),
		SnapType: "app",
	})

	repo := s.o.InterfaceManager().Repository()

	// add snaps to the repo to have plugs/slots
	c.Assert(repo.AddSnap(snapInfo), IsNil)
	c.Assert(repo.AddSnap(otherInfo), IsNil)
	repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "other-snap", Name: "media-hub"},
		SlotRef: interfaces.SlotRef{Snap: "some-snap", Name: "media-hub"},
	}, nil, nil, nil, nil, nil)

	ts, err := snapstate.Remove(st, "some-snap", snap.R(0), &snapstate.RemoveFlags{Purge: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("uninstall", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// check connections; auto-connection should be removed completely from conns on uninstall.
	var conns map[string]interface{}
	st.Get("conns", &conns)
	c.Assert(conns, HasLen, 0)
}

// TODO: add a custom checker in testutils for this and similar
func validateDownloadCheckTasks(c *C, tasks []*state.Task, name, revno, channel string) int {
	var i int
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Ensure prerequisites for "%s" are available`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Download snap "%s" (%s) from channel "%s"`, name, revno, channel))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Fetch and check assertions for snap "%s" (%s)`, name, revno))
	i++
	return i
}

const (
	noConfigure = 1 << iota
	isGadget
)

func validateInstallTasks(c *C, tasks []*state.Task, name, revno string, flags int) int {
	var i int
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Mount snap "%s" (%s)`, name, revno))
	i++
	if flags&isGadget != 0 {
		c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Update assets from gadget "%s" (%s)`, name, revno))
		i++
	}
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Copy snap "%s" data`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Setup snap "%s" (%s) security profiles`, name, revno))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Export content from snap "%s" (%s)`, name, revno))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Make snap "%s" (%s) available to the system`, name, revno))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Automatically connect eligible plugs and slots of snap "%s"`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Set automatic aliases for snap "%s"`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Setup snap "%s" aliases`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Run install hook of "%s" snap if present`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Start snap "%s" (%s) services`, name, revno))
	i++
	if flags&noConfigure == 0 {
		c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Run configure hook of "%s" snap if present`, name))
		i++
	}
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Run health check of "%s" snap`, name))
	i++
	return i
}

func validateRefreshTasks(c *C, tasks []*state.Task, name, revno string, flags int) int {
	var i int
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Mount snap "%s" (%s)`, name, revno))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Run pre-refresh hook of "%s" snap if present`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Stop snap "%s" services`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Remove aliases for snap "%s"`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Make current revision for snap "%s" unavailable`, name))
	i++
	if flags&isGadget != 0 {
		c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Update assets from gadget %q (%s)`, name, revno))
		i++

	}
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Copy snap "%s" data`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Setup snap "%s" (%s) security profiles`, name, revno))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Export content from snap "%s" (%s)`, name, revno))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Make snap "%s" (%s) available to the system`, name, revno))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Automatically connect eligible plugs and slots of snap "%s"`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Set automatic aliases for snap "%s"`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Setup snap "%s" aliases`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Run post-refresh hook of "%s" snap if present`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Start snap "%s" (%s) services`, name, revno))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Clean up "%s" (%s) install`, name, revno))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Run configure hook of "%s" snap if present`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Run health check of "%s" snap`, name))
	i++
	return i
}

// byReadyTime sorts a list of tasks by their "ready" time
type byReadyTime []*state.Task

func (a byReadyTime) Len() int           { return len(a) }
func (a byReadyTime) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byReadyTime) Less(i, j int) bool { return a[i].ReadyTime().Before(a[j].ReadyTime()) }

func (s *mgrsSuite) TestRemodelRequiredSnapsAdded(c *C) {
	for _, name := range []string{"foo", "bar", "baz"} {
		s.prereqSnapAssertions(c, map[string]interface{}{
			"snap-name": name,
		})
		snapPath, _ := s.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 1.0}", name), "1")
		s.serveSnap(snapPath, "1")
	}

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// pretend we have an old required snap installed
	si1 := &snap.SideInfo{RealName: "old-required-snap-1", Revision: snap.R(1)}
	snapstate.Set(st, "old-required-snap-1", &snapstate.SnapState{
		SnapType: "app",
		Active:   true,
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
		Flags:    snapstate.Flags{Required: true},
	})

	// create/set custom model assertion
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)

	model := s.brands.Model("my-brand", "my-model", modelDefaults)

	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// create a new model
	newModel := s.brands.Model("my-brand", "my-model", modelDefaults, map[string]interface{}{
		"required-snaps": []interface{}{"foo", "bar", "baz"},
		"revision":       "1",
	})

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	c.Check(devicestate.Remodeling(st), Equals, true)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	c.Check(devicestate.Remodeling(st), Equals, false)

	// the new required-snap "foo" is installed
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst)
	c.Assert(err, IsNil)
	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil)
	c.Check(info.Revision, Equals, snap.R(1))
	c.Check(info.Version, Equals, "1.0")

	// and marked required
	c.Check(snapst.Required, Equals, true)

	// and core is still marked required
	err = snapstate.Get(st, "core", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Required, Equals, true)

	// but old-required-snap-1 is no longer marked required
	err = snapstate.Get(st, "old-required-snap-1", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Required, Equals, false)

	// ensure sorting is correct
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	var i int
	// first all downloads/checks in sequential order
	for _, name := range []string{"foo", "bar", "baz"} {
		i += validateDownloadCheckTasks(c, tasks[i:], name, "1", "stable")
	}
	// then all installs in sequential order
	for _, name := range []string{"foo", "bar", "baz"} {
		i += validateInstallTasks(c, tasks[i:], name, "1", 0)
	}
	// ensure that we only have the tasks we checked (plus the one
	// extra "set-model" task)
	c.Assert(tasks, HasLen, i+1)
}

func (s *mgrsSuite) TestRemodelRequiredSnapsAddedUndo(c *C) {
	for _, name := range []string{"foo", "bar", "baz"} {
		s.prereqSnapAssertions(c, map[string]interface{}{
			"snap-name": name,
		})
		snapPath, _ := s.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 1.0}", name), "1")
		s.serveSnap(snapPath, "1")
	}

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// pretend we have an old required snap installed
	si1 := &snap.SideInfo{RealName: "old-required-snap-1", Revision: snap.R(1)}
	snapstate.Set(st, "old-required-snap-1", &snapstate.SnapState{
		SnapType: "app",
		Active:   true,
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
		Flags:    snapstate.Flags{Required: true},
	})

	// create/set custom model assertion
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	curModel := s.brands.Model("my-brand", "my-model", modelDefaults)

	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, curModel)
	c.Assert(err, IsNil)

	// create a new model
	newModel := s.brands.Model("my-brand", "my-model", modelDefaults, map[string]interface{}{
		"required-snaps": []interface{}{"foo", "bar", "baz"},
		"revision":       "1",
	})

	devicestate.InjectSetModelError(fmt.Errorf("boom"))
	defer devicestate.InjectSetModelError(nil)

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// None of the new snaps got installed
	var snapst snapstate.SnapState
	for _, snapName := range []string{"foo", "bar", "baz"} {
		err = snapstate.Get(st, snapName, &snapst)
		c.Assert(err, Equals, state.ErrNoState)
	}

	// old-required-snap-1 is still marked required
	err = snapstate.Get(st, "old-required-snap-1", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Required, Equals, true)

	// check tasks are in undo state
	for _, t := range chg.Tasks() {
		if t.Kind() == "link-snap" {
			c.Assert(t.Status(), Equals, state.UndoneStatus)
		}
	}

	model, err := s.o.DeviceManager().Model()
	c.Assert(err, IsNil)
	c.Assert(model, DeepEquals, curModel)
}

func (s *mgrsSuite) TestRemodelDifferentBase(c *C) {
	// make "core18" snap available in the store
	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "core18",
	})
	snapYamlContent := `name: core18
version: 18.04
type: base`
	snapPath, _ := s.makeStoreTestSnap(c, snapYamlContent, "18")
	s.serveSnap(snapPath, "18")

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// create/set custom model assertion
	model := s.brands.Model("can0nical", "my-model", modelDefaults)
	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// create a new model
	newModel := s.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"base":     "core18",
		"revision": "1",
	})

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, ErrorMatches, "cannot remodel from core to bases yet")
	c.Assert(chg, IsNil)
}

func (ms *mgrsSuite) TestRemodelSwitchToDifferentBase(c *C) {
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootVars(map[string]string{
		"snap_mode":   boot.DefaultStatus,
		"snap_core":   "core18_1.snap",
		"snap_kernel": "pc-kernel_1.snap",
	})

	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "core18", SnapID: fakeSnapID("core18"), Revision: snap.R(1)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "base",
	})
	si2 := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	gadgetSnapYaml := "name: pc\nversion: 1.0\ntype: gadget"
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si2},
		Current:  snap.R(1),
		SnapType: "gadget",
	})
	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
`
	snaptest.MockSnapWithFiles(c, gadgetSnapYaml, si2, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})

	// add "core20" snap to fake store
	const core20Yaml = `name: core20
type: base
version: 20.04`
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "core20",
		"publisher-id": "can0nical",
	})
	snapPath, _ := ms.makeStoreTestSnap(c, core20Yaml, "2")
	ms.serveSnap(snapPath, "2")

	// add "foo" snap to fake store
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
	})
	snapPath, _ = ms.makeStoreTestSnap(c, `{name: "foo", version: 1.0}`, "1")
	ms.serveSnap(snapPath, "1")

	// create/set custom model assertion
	model := ms.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"base": "core18",
	})

	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// create a new model
	newModel := ms.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"base":           "core20",
		"revision":       "1",
		"required-snaps": []interface{}{"foo"},
	})

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// system waits for a restart because of the new base
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoingStatus)

	// check that the boot vars got updated as expected
	bvars, err := bloader.GetBootVars("snap_mode", "snap_core", "snap_try_core", "snap_kernel", "snap_try_kernel")
	c.Assert(err, IsNil)
	c.Assert(bvars, DeepEquals, map[string]string{
		"snap_mode":       boot.TryStatus,
		"snap_core":       "core18_1.snap",
		"snap_try_core":   "core20_2.snap",
		"snap_kernel":     "pc-kernel_1.snap",
		"snap_try_kernel": "",
	})

	// simulate successful restart happened and that the bootvars
	// got updated
	state.MockRestarting(st, state.RestartUnset)
	bloader.SetBootVars(map[string]string{
		"snap_mode":   boot.DefaultStatus,
		"snap_core":   "core20_2.snap",
		"snap_kernel": "pc-kernel_1.snap",
	})

	// continue
	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	// ensure tasks were run in the right order
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	// first all downloads/checks in sequential order
	var i int
	i += validateDownloadCheckTasks(c, tasks[i:], "core20", "2", "stable")
	i += validateDownloadCheckTasks(c, tasks[i:], "foo", "1", "stable")

	// then all installs in sequential order
	i += validateInstallTasks(c, tasks[i:], "core20", "2", noConfigure)
	i += validateInstallTasks(c, tasks[i:], "foo", "1", 0)

	// ensure that we only have the tasks we checked (plus the one
	// extra "set-model" task)
	c.Assert(tasks, HasLen, i+1)
}

func (ms *mgrsSuite) TestRemodelSwitchToDifferentBaseUndo(c *C) {
	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootVars(map[string]string{
		"snap_mode":   boot.DefaultStatus,
		"snap_core":   "core18_1.snap",
		"snap_kernel": "pc-kernel_1.snap",
	})

	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "core18", SnapID: fakeSnapID("core18"), Revision: snap.R(1)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "base",
	})
	snaptest.MockSnapWithFiles(c, "name: core18\ntype: base\nversion: 1.0", si, nil)

	si2 := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	gadgetSnapYaml := "name: pc\nversion: 1.0\ntype: gadget"
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si2},
		Current:  snap.R(1),
		SnapType: "gadget",
	})
	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
`
	snaptest.MockSnapWithFiles(c, gadgetSnapYaml, si2, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})

	// add "core20" snap to fake store
	const core20Yaml = `name: core20
type: base
version: 20.04`
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "core20",
		"publisher-id": "can0nical",
	})
	snapPath, _ := ms.makeStoreTestSnap(c, core20Yaml, "2")
	ms.serveSnap(snapPath, "2")

	// add "foo" snap to fake store
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
	})
	snapPath, _ = ms.makeStoreTestSnap(c, `{name: "foo", version: 1.0}`, "1")
	ms.serveSnap(snapPath, "1")

	// create/set custom model assertion
	model := ms.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"base": "core18",
	})

	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// create a new model
	newModel := ms.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"base":           "core20",
		"revision":       "1",
		"required-snaps": []interface{}{"foo"},
	})

	devicestate.InjectSetModelError(fmt.Errorf("boom"))
	defer devicestate.InjectSetModelError(nil)

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// system waits for a restart because of the new base
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoingStatus)

	// check that the boot vars got updated as expected
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_mode":       boot.TryStatus,
		"snap_core":       "core18_1.snap",
		"snap_try_core":   "core20_2.snap",
		"snap_kernel":     "pc-kernel_1.snap",
		"snap_try_kernel": "",
	})
	// simulate successful restart happened
	ms.mockSuccessfulReboot(c, bloader, []snap.Type{snap.TypeBase})
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_mode":       boot.DefaultStatus,
		"snap_core":       "core20_2.snap",
		"snap_try_core":   "",
		"snap_kernel":     "pc-kernel_1.snap",
		"snap_try_kernel": "",
	})

	// continue
	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// and we are in restarting state
	restarting, restartType := st.Restarting()
	c.Check(restarting, Equals, true)
	c.Check(restartType, Equals, state.RestartSystem)

	// and the undo gave us our old kernel back
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core20_2.snap",
		"snap_try_core":   "core18_1.snap",
		"snap_kernel":     "pc-kernel_1.snap",
		"snap_try_kernel": "",
		"snap_mode":       boot.TryStatus,
	})
}

func (ms *mgrsSuite) TestRemodelSwitchToDifferentBaseUndoOnRollback(c *C) {
	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootVars(map[string]string{
		"snap_mode":   boot.DefaultStatus,
		"snap_core":   "core18_1.snap",
		"snap_kernel": "pc-kernel_1.snap",
	})

	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "core18", SnapID: fakeSnapID("core18"), Revision: snap.R(1)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "base",
	})
	snaptest.MockSnapWithFiles(c, "name: core18\ntype: base\nversion: 1.0", si, nil)

	si2 := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	gadgetSnapYaml := "name: pc\nversion: 1.0\ntype: gadget"
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si2},
		Current:  snap.R(1),
		SnapType: "gadget",
	})
	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
`
	snaptest.MockSnapWithFiles(c, gadgetSnapYaml, si2, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})

	// add "core20" snap to fake store
	const core20Yaml = `name: core20
type: base
version: 20.04`
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "core20",
		"publisher-id": "can0nical",
	})
	snapPath, _ := ms.makeStoreTestSnap(c, core20Yaml, "2")
	ms.serveSnap(snapPath, "2")

	// add "foo" snap to fake store
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
	})
	snapPath, _ = ms.makeStoreTestSnap(c, `{name: "foo", version: 1.0}`, "1")
	ms.serveSnap(snapPath, "1")

	// create/set custom model assertion
	model := ms.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"base": "core18",
	})

	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// create a new model
	newModel := ms.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"base":           "core20",
		"revision":       "1",
		"required-snaps": []interface{}{"foo"},
	})

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// system waits for a restart because of the new base
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoingStatus)

	// check that the boot vars got updated as expected
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_mode":       boot.TryStatus,
		"snap_core":       "core18_1.snap",
		"snap_try_core":   "core20_2.snap",
		"snap_kernel":     "pc-kernel_1.snap",
		"snap_try_kernel": "",
	})
	// simulate successful restart happened
	ms.mockRollbackAcrossReboot(c, bloader, []snap.Type{snap.TypeBase})
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_mode":       boot.DefaultStatus,
		"snap_core":       "core18_1.snap",
		"snap_try_core":   "",
		"snap_kernel":     "pc-kernel_1.snap",
		"snap_try_kernel": "",
	})

	// continue
	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// and we are *not* in restarting state
	restarting, _ := st.Restarting()
	c.Check(restarting, Equals, false)
	// bootvars unchanged
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_mode":       boot.DefaultStatus,
		"snap_core":       "core18_1.snap",
		"snap_try_core":   "",
		"snap_kernel":     "pc-kernel_1.snap",
		"snap_try_kernel": "",
	})
}

type kernelSuite struct {
	baseMgrsSuite

	bloader *boottest.Bootenv16
}

var _ = Suite(&kernelSuite{})

func (s *kernelSuite) SetUpTest(c *C) {
	s.baseMgrsSuite.SetUpTest(c)

	s.bloader = boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	s.bloader.SetBootKernel("pc-kernel_1.snap")
	s.bloader.SetBootBase("core_1.snap")
	bootloader.Force(s.bloader)
	s.AddCleanup(func() { bootloader.Force(nil) })

	restore := release.MockOnClassic(false)
	s.AddCleanup(restore)
	mockServer := s.mockStore(c)
	s.AddCleanup(mockServer.Close)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// create/set custom model assertion
	model := s.brands.Model("can0nical", "my-model", modelDefaults)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// make a mock "pc-kernel" kernel
	si := &snap.SideInfo{RealName: "pc-kernel", SnapID: fakeSnapID("pc-kernel"), Revision: snap.R(1)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "kernel",
	})
	snaptest.MockSnapWithFiles(c, "name: pc-kernel\ntype: kernel\nversion: 1.0", si, nil)

	// make a mock "pc" gadget
	si2 := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	gadgetSnapYaml := "name: pc\nversion: 1.0\ntype: gadget"
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si2},
		Current:  snap.R(1),
		SnapType: "gadget",
	})
	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
`
	snaptest.MockSnapWithFiles(c, gadgetSnapYaml, si2, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})

	// add some store snaps
	const kernelYaml = `name: pc-kernel
type: kernel
version: 2.0`
	snapPath, _ := s.makeStoreTestSnap(c, kernelYaml, "2")
	s.serveSnap(snapPath, "2")

	const brandKernelYaml = `name: brand-kernel
type: kernel
version: 1.0`
	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "brand-kernel",
		"publisher-id": "can0nical",
	})
	snapPath, _ = s.makeStoreTestSnap(c, brandKernelYaml, "2")
	s.serveSnap(snapPath, "2")

	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
	})
	snapPath, _ = s.makeStoreTestSnap(c, `{name: "foo", version: 1.0}`, "1")
	s.serveSnap(snapPath, "1")
}

func (s *kernelSuite) TestRemodelSwitchKernelTrack(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// create a new model
	newModel := s.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"kernel":         "pc-kernel=18",
		"revision":       "1",
		"required-snaps": []interface{}{"foo"},
	})

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// system waits for a restart because of the new kernel
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoingStatus)

	// simulate successful restart happened
	s.mockSuccessfulReboot(c, s.bloader, []snap.Type{snap.TypeKernel})

	// continue
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	// ensure tasks were run in the right order
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	// first all downloads/checks in sequential order
	var i int
	i += validateDownloadCheckTasks(c, tasks[i:], "pc-kernel", "2", "18")
	i += validateDownloadCheckTasks(c, tasks[i:], "foo", "1", "stable")

	// then all installs in sequential order
	i += validateRefreshTasks(c, tasks[i:], "pc-kernel", "2", 0)
	i += validateInstallTasks(c, tasks[i:], "foo", "1", 0)

	// ensure that we only have the tasks we checked (plus the one
	// extra "set-model" task)
	c.Assert(tasks, HasLen, i+1)
}

func (ms *kernelSuite) TestRemodelSwitchToDifferentKernel(c *C) {
	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// create a new model
	newModel := ms.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"kernel":         "brand-kernel",
		"revision":       "1",
		"required-snaps": []interface{}{"foo"},
	})

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// system waits for a restart because of the new kernel
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoingStatus)

	// check that the system tries to boot the new brand kernel
	c.Assert(ms.bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core_1.snap",
		"snap_kernel":     "pc-kernel_1.snap",
		"snap_try_kernel": "brand-kernel_2.snap",
		"snap_mode":       boot.TryStatus,
		"snap_try_core":   "",
	})
	// simulate successful system-restart bootenv updates (those
	// vars will be cleared by snapd on a restart)
	ms.mockSuccessfulReboot(c, ms.bloader, []snap.Type{snap.TypeKernel})
	// bootvars are as expected
	c.Assert(ms.bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core_1.snap",
		"snap_kernel":     "brand-kernel_2.snap",
		"snap_try_core":   "",
		"snap_try_kernel": "",
		"snap_mode":       boot.DefaultStatus,
	})

	// continue
	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	// bootvars are as expected (i.e. nothing has changed since this
	// test simulated that we booted successfully)
	c.Assert(ms.bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core_1.snap",
		"snap_kernel":     "brand-kernel_2.snap",
		"snap_try_kernel": "",
		"snap_try_core":   "",
		"snap_mode":       boot.DefaultStatus,
	})

	// ensure tasks were run in the right order
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	// first all downloads/checks in sequential order
	var i int
	i += validateDownloadCheckTasks(c, tasks[i:], "brand-kernel", "2", "stable")
	i += validateDownloadCheckTasks(c, tasks[i:], "foo", "1", "stable")

	// then all installs in sequential order
	i += validateInstallTasks(c, tasks[i:], "brand-kernel", "2", 0)
	i += validateInstallTasks(c, tasks[i:], "foo", "1", 0)

	// ensure that we only have the tasks we checked (plus the one
	// extra "set-model" task)
	c.Assert(tasks, HasLen, i+1)

	// ensure we did not try device registration
	for _, t := range st.Tasks() {
		if t.Kind() == "request-serial" {
			c.Fatalf("test should not create a request-serial task but did")
		}
	}
}

func (ms *kernelSuite) TestRemodelSwitchToDifferentKernelUndo(c *C) {
	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// create a new model
	newModel := ms.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"kernel":         "brand-kernel",
		"revision":       "1",
		"required-snaps": []interface{}{"foo"},
	})

	devicestate.InjectSetModelError(fmt.Errorf("boom"))
	defer devicestate.InjectSetModelError(nil)

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// system waits for a restart because of the new kernel
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoingStatus)

	// simulate successful restart happened
	ms.mockSuccessfulReboot(c, ms.bloader, []snap.Type{snap.TypeKernel})

	// continue
	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// the change was not successful
	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// and we are in restarting state
	restarting, restartType := st.Restarting()
	c.Check(restarting, Equals, true)
	c.Check(restartType, Equals, state.RestartSystem)

	// and the undo gave us our old kernel back
	c.Assert(ms.bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core_1.snap",
		"snap_try_core":   "",
		"snap_try_kernel": "pc-kernel_1.snap",
		"snap_kernel":     "brand-kernel_2.snap",
		"snap_mode":       boot.TryStatus,
	})
}

func (ms *kernelSuite) TestRemodelSwitchToDifferentKernelUndoOnRollback(c *C) {
	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// create a new model
	newModel := ms.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"kernel":         "brand-kernel",
		"revision":       "1",
		"required-snaps": []interface{}{"foo"},
	})

	devicestate.InjectSetModelError(fmt.Errorf("boom"))
	defer devicestate.InjectSetModelError(nil)

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// system waits for a restart because of the new kernel
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoingStatus)

	// simulate rollback of the kernel during reboot
	ms.mockRollbackAcrossReboot(c, ms.bloader, []snap.Type{snap.TypeKernel})

	// continue
	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// the change was not successful
	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// and we are *not* in restarting state
	restarting, _ := st.Restarting()
	c.Check(restarting, Equals, false)

	// and the undo gave us our old kernel back
	c.Assert(ms.bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core_1.snap",
		"snap_try_core":   "",
		"snap_kernel":     "pc-kernel_1.snap",
		"snap_try_kernel": "",
		"snap_mode":       boot.DefaultStatus,
	})
}

func (s *mgrsSuite) TestRemodelStoreSwitch(c *C) {
	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
	})
	snapPath, _ := s.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 1.0}", "foo"), "1")
	s.serveSnap(snapPath, "1")

	// track the creation of new DeviceAndAutContext (for new Store)
	newDAC := false

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	s.checkDeviceAndAuthContext = func(dac store.DeviceAndAuthContext) {
		// the DeviceAndAuthContext assumes state is unlocked
		st.Unlock()
		defer st.Lock()
		c.Check(dac, NotNil)
		stoID, err := dac.StoreID("")
		c.Assert(err, IsNil)
		c.Check(stoID, Equals, "switched-store")
		newDAC = true
	}

	// create/set custom model assertion
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)

	model := s.brands.Model("my-brand", "my-model", modelDefaults)

	// setup model assertion
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// have a serial as well
	kpMgr, err := asserts.OpenFSKeypairManager(dirs.SnapDeviceDir)
	c.Assert(err, IsNil)
	err = kpMgr.Put(deviceKey)
	c.Assert(err, IsNil)

	encDevKey, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, IsNil)
	serial, err := s.brands.Signing("my-brand").Sign(asserts.SerialType, map[string]interface{}{
		"authority-id":        "my-brand",
		"brand-id":            "my-brand",
		"model":               "my-model",
		"serial":              "store-switch-serial",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(st, serial)
	c.Assert(err, IsNil)

	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		KeyID:  deviceKey.PublicKey().ID(),
		Serial: "store-switch-serial",
	})

	// create a new model
	newModel := s.brands.Model("my-brand", "my-model", modelDefaults, map[string]interface{}{
		"store":          "switched-store",
		"required-snaps": []interface{}{"foo"},
		"revision":       "1",
	})

	s.expectedSerial = "store-switch-serial"
	s.expectedStore = "switched-store"
	s.sessionMacaroon = "switched-store-session"

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	// the new required-snap "foo" is installed
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst)
	c.Assert(err, IsNil)

	// and marked required
	c.Check(snapst.Required, Equals, true)

	// a new store was made
	c.Check(newDAC, Equals, true)

	// we have a session with the new store
	device, err := devicestatetest.Device(st)
	c.Assert(err, IsNil)
	c.Check(device.Serial, Equals, "store-switch-serial")
	c.Check(device.SessionMacaroon, Equals, "switched-store-session")
}

func (s *mgrsSuite) TestRemodelSwitchGadgetTrack(c *C) {
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "gadget",
	})
	gadgetSnapYaml := "name: pc\nversion: 2.0\ntype: gadget"
	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
`
	snaptest.MockSnapWithFiles(c, gadgetSnapYaml, si, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})
	snapPath, _ := s.makeStoreTestSnapWithFiles(c, gadgetSnapYaml, "2", [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})
	s.serveSnap(snapPath, "2")

	// create/set custom model assertion
	model := s.brands.Model("can0nical", "my-model", modelDefaults)

	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// create a new model
	newModel := s.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"gadget":   "pc=18",
		"revision": "1",
	})

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// ensure tasks were run in the right order
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	// first all downloads/checks in sequential order
	var i int
	i += validateDownloadCheckTasks(c, tasks[i:], "pc", "2", "18")

	// then all installs in sequential order
	i += validateRefreshTasks(c, tasks[i:], "pc", "2", isGadget)

	// ensure that we only have the tasks we checked (plus the one
	// extra "set-model" task)
	c.Assert(tasks, HasLen, i+1)
}

type mockUpdater struct{}

func (m *mockUpdater) Backup() error { return nil }

func (m *mockUpdater) Rollback() error { return nil }

func (m *mockUpdater) Update() error { return nil }

func (s *mgrsSuite) TestRemodelSwitchToDifferentGadget(c *C) {
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "core18", SnapID: fakeSnapID("core18"), Revision: snap.R(1)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "base",
	})
	si2 := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	gadgetSnapYaml := "name: pc\nversion: 1.0\ntype: gadget"
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si2},
		Current:  snap.R(1),
		SnapType: "gadget",
	})
	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
        structure:
          - name: foo
            type: bare
            size: 1M
            content:
              - image: foo.img
`
	snaptest.MockSnapWithFiles(c, gadgetSnapYaml, si2, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
		{"foo.img", "foo"},
	})

	// add new gadget "other-pc" snap to fake store
	const otherPcYaml = `name: other-pc
type: gadget
version: 2`
	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "other-pc",
		"publisher-id": "can0nical",
	})
	otherGadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
        structure:
          - name: foo
            type: bare
            size: 1M
            content:
              - image: new-foo.img
`
	snapPath, _ := s.makeStoreTestSnapWithFiles(c, otherPcYaml, "2", [][]string{
		// use a compatible gadget YAML
		{"meta/gadget.yaml", otherGadgetYaml},
		{"new-foo.img", "new foo"},
	})
	s.serveSnap(snapPath, "2")

	updaterForStructureCalls := 0
	restore = gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, rootDir, rollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		updaterForStructureCalls++
		c.Assert(ps.Name, Equals, "foo")
		return &mockUpdater{}, nil
	})
	defer restore()

	// create/set custom model assertion
	model := s.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"gadget": "pc",
	})

	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// create a new model
	newModel := s.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"gadget":   "other-pc=18",
		"revision": "1",
	})

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// gadget updater was set up
	c.Check(updaterForStructureCalls, Equals, 1)

	// gadget update requests a restart
	restarting, _ := st.Restarting()
	c.Check(restarting, Equals, true)

	// simulate successful restart happened
	state.MockRestarting(st, state.RestartUnset)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// ensure tasks were run in the right order
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	// first all downloads/checks
	var i int
	i += validateDownloadCheckTasks(c, tasks[i:], "other-pc", "2", "18")

	// then all installs
	i += validateInstallTasks(c, tasks[i:], "other-pc", "2", isGadget)

	// ensure that we only have the tasks we checked (plus the one
	// extra "set-model" task)
	c.Assert(tasks, HasLen, i+1)
}

func (s *mgrsSuite) TestRemodelSwitchToIncompatibleGadget(c *C) {
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "core18", SnapID: fakeSnapID("core18"), Revision: snap.R(1)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "base",
	})
	si2 := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	gadgetSnapYaml := "name: pc\nversion: 1.0\ntype: gadget"
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si2},
		Current:  snap.R(1),
		SnapType: "gadget",
	})
	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
        structure:
          - name: foo
            type: 00000000-0000-0000-0000-0000deadcafe
            size: 10M
`
	snaptest.MockSnapWithFiles(c, gadgetSnapYaml, si2, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})

	// add new gadget "other-pc" snap to fake store
	const otherPcYaml = `name: other-pc
type: gadget
version: 2`
	// new gadget layout is incompatible, a structure that exited before has
	// a different size now
	otherGadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
        structure:
          - name: foo
            type: 00000000-0000-0000-0000-0000deadcafe
            size: 20M
`
	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "other-pc",
		"publisher-id": "can0nical",
	})
	snapPath, _ := s.makeStoreTestSnapWithFiles(c, otherPcYaml, "2", [][]string{
		{"meta/gadget.yaml", otherGadgetYaml},
	})
	s.serveSnap(snapPath, "2")

	// create/set custom model assertion
	model := s.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"gadget": "pc",
	})

	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// create a new model
	newModel := s.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"gadget":   "other-pc=18",
		"revision": "1",
	})

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*cannot remodel to an incompatible gadget: .*cannot change structure size.*`)
}

func (s *mgrsSuite) TestHappyDeviceRegistrationWithPrepareDeviceHook(c *C) {
	// just to 404 locally eager account-key requests
	mockStoreServer := s.mockStore(c)
	defer mockStoreServer.Close()

	model := s.brands.Model("my-brand", "my-model", modelDefaults, map[string]interface{}{
		"gadget": "gadget",
	})

	// reset as seeded but not registered
	// shortcut: have already device key
	kpMgr, err := asserts.OpenFSKeypairManager(dirs.SnapDeviceDir)
	c.Assert(err, IsNil)
	err = kpMgr.Put(deviceKey)
	c.Assert(err, IsNil)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
		KeyID: deviceKey.PublicKey().ID(),
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	signSerial := func(c *C, bhv *devicestatetest.DeviceServiceBehavior, headers map[string]interface{}, body []byte) (serial asserts.Assertion, ancillary []asserts.Assertion, err error) {
		brandID := headers["brand-id"].(string)
		model := headers["model"].(string)
		c.Check(brandID, Equals, "my-brand")
		c.Check(model, Equals, "my-model")
		headers["authority-id"] = brandID
		a, err := s.brands.Signing("my-brand").Sign(asserts.SerialType, headers, body, "")
		return a, nil, err
	}

	bhv := &devicestatetest.DeviceServiceBehavior{
		ReqID:            "REQID-1",
		RequestIDURLPath: "/svc/request-id",
		SerialURLPath:    "/svc/serial",
		SignSerial:       signSerial,
	}

	mockServer := devicestatetest.MockDeviceService(c, bhv)
	defer mockServer.Close()

	pDBhv := &devicestatetest.PrepareDeviceBehavior{
		DeviceSvcURL: mockServer.URL + "/svc/",
		Headers: map[string]string{
			"x-extra-header": "extra",
		},
		RegBody: map[string]string{
			"mac": "00:00:00:00:ff:00",
		},
		ProposedSerial: "12000",
	}

	r := devicestatetest.MockGadget(c, st, "gadget", snap.R(2), pDBhv)
	defer r()

	// run the whole device registration process
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	var becomeOperational *state.Change
	for _, chg := range st.Changes() {
		if chg.Kind() == "become-operational" {
			becomeOperational = chg
			break
		}
	}
	c.Assert(becomeOperational, NotNil)

	c.Check(becomeOperational.Status().Ready(), Equals, true)
	c.Check(becomeOperational.Err(), IsNil)

	device, err := devicestatetest.Device(st)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "my-brand")
	c.Check(device.Model, Equals, "my-model")
	c.Check(device.Serial, Equals, "12000")

	a, err := assertstate.DB(st).Find(asserts.SerialType, map[string]string{
		"brand-id": "my-brand",
		"model":    "my-model",
		"serial":   "12000",
	})
	c.Assert(err, IsNil)
	serial := a.(*asserts.Serial)

	var details map[string]interface{}
	err = yaml.Unmarshal(serial.Body(), &details)
	c.Assert(err, IsNil)

	c.Check(details, DeepEquals, map[string]interface{}{
		"mac": "00:00:00:00:ff:00",
	})

	c.Check(serial.DeviceKey().ID(), Equals, device.KeyID)
}

func (s *mgrsSuite) TestRemodelReregistration(c *C) {
	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
	})
	snapPath, _ := s.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 1.0}", "foo"), "1")
	s.serveSnap(snapPath, "1")

	// track the creation of new DeviceAndAutContext (for new Store)
	newDAC := false

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	s.checkDeviceAndAuthContext = func(dac store.DeviceAndAuthContext) {
		// the DeviceAndAuthContext assumes state is unlocked
		st.Unlock()
		defer st.Lock()
		c.Check(dac, NotNil)
		stoID, err := dac.StoreID("")
		c.Assert(err, IsNil)
		c.Check(stoID, Equals, "my-brand-substore")
		newDAC = true
	}

	model := s.brands.Model("my-brand", "my-model", modelDefaults, map[string]interface{}{
		"gadget": "gadget",
	})

	// setup initial device identity
	kpMgr, err := asserts.OpenFSKeypairManager(dirs.SnapDeviceDir)
	c.Assert(err, IsNil)
	err = kpMgr.Put(deviceKey)
	c.Assert(err, IsNil)

	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		KeyID:  deviceKey.PublicKey().ID(),
		Serial: "orig-serial",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	encDevKey, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, IsNil)
	serialHeaders := map[string]interface{}{
		"brand-id":            "my-brand",
		"model":               "my-model",
		"serial":              "orig-serial",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}
	serialA, err := s.brands.Signing("my-brand").Sign(asserts.SerialType, serialHeaders, nil, "")
	c.Assert(err, IsNil)
	serial := serialA.(*asserts.Serial)
	err = assertstate.Add(st, serial)
	c.Assert(err, IsNil)

	signSerial := func(c *C, bhv *devicestatetest.DeviceServiceBehavior, headers map[string]interface{}, body []byte) (serial asserts.Assertion, ancillary []asserts.Assertion, err error) {
		brandID := headers["brand-id"].(string)
		model := headers["model"].(string)
		c.Check(brandID, Equals, "my-brand")
		c.Check(model, Equals, "other-model")
		headers["authority-id"] = brandID
		a, err := s.brands.Signing("my-brand").Sign(asserts.SerialType, headers, body, "")
		return a, nil, err
	}

	bhv := &devicestatetest.DeviceServiceBehavior{
		ReqID:            "REQID-1",
		RequestIDURLPath: "/svc/request-id",
		SerialURLPath:    "/svc/serial",
		SignSerial:       signSerial,
	}

	mockDeviceService := devicestatetest.MockDeviceService(c, bhv)
	defer mockDeviceService.Close()

	r := devicestatetest.MockGadget(c, st, "gadget", snap.R(2), nil)
	defer r()

	// set registration config on gadget
	tr := config.NewTransaction(st)
	c.Assert(tr.Set("gadget", "device-service.url", mockDeviceService.URL+"/svc/"), IsNil)
	c.Assert(tr.Set("gadget", "registration.proposed-serial", "orig-serial"), IsNil)
	tr.Commit()

	// run the remodel
	// create a new model
	newModel := s.brands.Model("my-brand", "other-model", modelDefaults, map[string]interface{}{
		"store":          "my-brand-substore",
		"gadget":         "gadget",
		"required-snaps": []interface{}{"foo"},
	})

	s.expectedSerial = "orig-serial"
	s.expectedStore = "my-brand-substore"
	s.sessionMacaroon = "other-store-session"

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	device, err := devicestatetest.Device(st)
	c.Assert(err, IsNil)
	c.Check(device.Brand, Equals, "my-brand")
	c.Check(device.Model, Equals, "other-model")
	c.Check(device.Serial, Equals, "orig-serial")

	a, err := assertstate.DB(st).Find(asserts.SerialType, map[string]string{
		"brand-id": "my-brand",
		"model":    "other-model",
		"serial":   "orig-serial",
	})
	c.Assert(err, IsNil)
	serial = a.(*asserts.Serial)

	c.Check(serial.Body(), HasLen, 0)
	c.Check(serial.DeviceKey().ID(), Equals, device.KeyID)

	// the new required-snap "foo" is installed
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst)
	c.Assert(err, IsNil)

	// and marked required
	c.Check(snapst.Required, Equals, true)

	// a new store was made
	c.Check(newDAC, Equals, true)

	// we have a session with the new store
	c.Check(device.SessionMacaroon, Equals, "other-store-session")
}

func (s *mgrsSuite) TestCheckRefreshFailureWithConcurrentRemoveOfConnectedSnap(c *C) {
	hookMgr := s.o.HookManager()
	c.Assert(hookMgr, NotNil)

	// force configure hook failure for some-snap.
	hookMgr.RegisterHijack("configure", "some-snap", func(ctx *hookstate.Context) error {
		return fmt.Errorf("failing configure hook")
	})

	snapPath, _ := s.makeStoreTestSnap(c, someSnapYaml, "40")
	s.serveSnap(snapPath, "40")
	snapPath, _ = s.makeStoreTestSnap(c, otherSnapYaml, "50")
	s.serveSnap(snapPath, "50")

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	st.Set("conns", map[string]interface{}{
		"other-snap:media-hub some-snap:media-hub": map[string]interface{}{"interface": "media-hub", "auto": false},
	})

	si := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(1)}
	snapInfo := snaptest.MockSnap(c, someSnapYaml, si)

	oi := &snap.SideInfo{RealName: "other-snap", SnapID: fakeSnapID("other-snap"), Revision: snap.R(1)}
	otherInfo := snaptest.MockSnap(c, otherSnapYaml, oi)

	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(st, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{oi},
		Current:  snap.R(1),
		SnapType: "app",
	})

	// add snaps to the repo and connect them
	repo := s.o.InterfaceManager().Repository()
	c.Assert(repo.AddSnap(snapInfo), IsNil)
	c.Assert(repo.AddSnap(otherInfo), IsNil)
	_, err := repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "other-snap", Name: "media-hub"},
		SlotRef: interfaces.SlotRef{Snap: "some-snap", Name: "media-hub"},
	}, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	// refresh all
	c.Assert(assertstate.RefreshSnapDeclarations(st, 0), IsNil)

	ts, err := snapstate.Update(st, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("refresh", "...")
	chg.AddAll(ts)

	// remove other-snap
	ts2, err := snapstate.Remove(st, "other-snap", snap.R(0), &snapstate.RemoveFlags{Purge: true})
	c.Assert(err, IsNil)
	chg2 := st.NewChange("remove-snap", "...")
	chg2.AddAll(ts2)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()

	c.Check(err, IsNil)

	// the refresh change has failed due to configure hook error
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*failing configure hook.*`)
	c.Check(chg.Status(), Equals, state.ErrorStatus)

	// download-snap is one of the first tasks in the refresh change, check that it was undone
	var downloadSnapStatus state.Status
	for _, t := range chg.Tasks() {
		if t.Kind() == "download-snap" {
			downloadSnapStatus = t.Status()
			break
		}
	}
	c.Check(downloadSnapStatus, Equals, state.UndoneStatus)

	// the remove change succeeded
	c.Check(chg2.Err(), IsNil)
	c.Check(chg2.Status(), Equals, state.DoneStatus)
}

func (s *mgrsSuite) TestInstallKernelSnapRollbackUpdatesBootloaderEnv(c *C) {
	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore := release.MockOnClassic(false)
	defer restore()

	model := s.brands.Model("my-brand", "my-model", modelDefaults)

	const packageKernel = `
name: pc-kernel
version: 4.0-1
type: kernel`

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	snapPath := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// pretend we have core18/pc-kernel
	bloader.BootVars = map[string]string{
		"snap_core":   "core18_2.snap",
		"snap_kernel": "pc-kernel_123.snap",
		"snap_mode":   boot.DefaultStatus,
	}
	si1 := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(123)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, packageKernel, si1, [][]string{
		{"meta/kernel.yaml", ""},
	})
	si2 := &snap.SideInfo{RealName: "core18", Revision: snap.R(2)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
	})

	// setup model assertion
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "pc-kernel"}, snapPath, "", "", snapstate.Flags{})
	c.Assert(err, IsNil)

	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	// run, this will trigger a wait for the restart
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core18_2.snap",
		"snap_try_core":   "",
		"snap_kernel":     "pc-kernel_123.snap",
		"snap_try_kernel": "pc-kernel_x1.snap",
		"snap_mode":       boot.TryStatus,
	})

	// we are in restarting state and the change is not done yet
	restarting, _ := st.Restarting()
	c.Check(restarting, Equals, true)
	c.Check(chg.Status(), Equals, state.DoingStatus)
	s.mockRollbackAcrossReboot(c, bloader, []snap.Type{snap.TypeKernel})

	// the kernel revision got rolled back
	var snapst snapstate.SnapState
	snapstate.Get(st, "pc-kernel", &snapst)
	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil)
	c.Assert(info.Revision, Equals, snap.R(123))

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, `(?ms).*cannot finish pc-kernel installation, there was a rollback across reboot\)`)

	// and the bootvars are reset
	c.Check(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core18_2.snap",
		"snap_kernel":     "pc-kernel_123.snap",
		"snap_mode":       boot.DefaultStatus,
		"snap_try_core":   "",
		"snap_try_kernel": "",
	})
}
