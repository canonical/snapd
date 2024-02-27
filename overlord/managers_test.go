// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2021 Canonical Ltd
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
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/arch/archtest"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/bootloader/grubenv"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/kcmdline"
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
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/servicestate/servicestatetest"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/systemd/systemdtest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/wrappers"
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
	Options      *snap.SnapshotOptions
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

	logbuf *bytes.Buffer
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

	// TODO: temporary: skip due to timeouts on riscv64
	if runtime.GOARCH == "riscv64" || os.Getenv("SNAPD_SKIP_SLOW_TESTS") != "" {
		c.Skip("skipping slow tests")
	}

	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })

	// needed for system key generation
	s.AddCleanup(osutil.MockMountInfo(""))

	s.AddCleanup(archtest.MockArchitecture("amd64"))
	snippets := []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte("console=ttyS0 console=tty1 panic=-1")},
	}
	s.AddCleanup(assets.MockSnippetsForEdition("grub.cfg:static-cmdline", snippets))
	s.AddCleanup(assets.MockSnippetsForEdition("grub-recovery.cfg:static-cmdline", snippets))

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
	r := snapshotstate.MockBackendSave(func(_ context.Context, id uint64, si *snap.Info, cfg map[string]interface{}, usernames []string,
		options *snap.SnapshotOptions, _ *dirs.SnapDirOptions) (*client.Snapshot, error) {
		s.automaticSnapshots = append(s.automaticSnapshots, automaticSnapshotCall{InstanceName: si.InstanceName(), SnapConfig: cfg, Usernames: usernames, Options: options})
		return nil, nil
	})
	s.AddCleanup(r)

	s.AddCleanup(ifacestate.MockConnectRetryTimeout(connectRetryTimeout))

	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
	s.AddCleanup(func() { os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS") })

	// create a fake systemd environment
	os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755)

	r = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, nil); out != nil {
			return out, nil
		}
		if out, ok := systemdtest.HandleMockListMountUnitsOutput(cmd, nil); ok {
			return out, nil
		}
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
	st := o.State()
	st.Lock()
	st.Set("seeded", true)
	// registered
	err = assertstate.Add(st, sysdb.GenericClassicModel())
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "generic",
		Model:  "generic-classic",
		Serial: "serialserial",
	})
	st.Unlock()
	c.Assert(err, IsNil)
	err = o.StartUp()
	c.Assert(err, IsNil)
	o.InterfaceManager().DisableUDevMonitor()
	s.o = o

	s.AddCleanup(snapstate.MockEnsuredMountsUpdated(s.o.SnapManager(), true))

	st.Lock()
	defer st.Unlock()

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

	// add pi snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "pi",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a8, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a8), IsNil)
	c.Assert(s.storeSigning.Add(a8), IsNil)

	// add pi-kernel snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "pi-kernel",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a9, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a9), IsNil)
	c.Assert(s.storeSigning.Add(a9), IsNil)

	// add core18 snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "core18",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a10, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a10), IsNil)
	c.Assert(s.storeSigning.Add(a10), IsNil)

	// add core20 snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "core20",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a11, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a11), IsNil)
	c.Assert(s.storeSigning.Add(a11), IsNil)

	// add snapd snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "snapd",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a12, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a12), IsNil)
	c.Assert(s.storeSigning.Add(a12), IsNil)

	// add core itself
	snapstate.Set(st, "core", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", SnapID: fakeSnapID("core"), Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "os",
		Flags: snapstate.Flags{
			Required: true,
		},
	})

	// commonly used core and snapd revisions in tests
	defaultInfoFile := `
VERSION=2.54.3+git1.g479e745-dirty
SNAPD_APPARMOR_REEXEC=1
`
	for _, snapName := range []string{"snapd", "core"} {
		for _, rev := range []string{"1", "11", "30"} {
			infoFile := filepath.Join(dirs.SnapMountDir, snapName, rev, dirs.CoreLibExecDir, "info")
			err = os.MkdirAll(filepath.Dir(infoFile), 0755)
			c.Assert(err, IsNil)
			err = os.WriteFile(infoFile, []byte(defaultInfoFile), 0644)
			c.Assert(err, IsNil)
		}
	}

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
	err = os.WriteFile(snapdCloudInitRestrictedFile, nil, 0644)
	c.Assert(err, IsNil)

	logbuf, restore := logger.MockLogger()
	s.AddCleanup(restore)
	s.logbuf = logbuf
}

func (s *baseMgrsSuite) makeSerialAssertionInState(c *C, st *state.State, brandID, model, serialN string) *asserts.Serial {
	encDevKey, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, IsNil)
	serial, err := s.brands.Signing(brandID).Sign(asserts.SerialType, map[string]interface{}{
		"brand-id":            brandID,
		"model":               model,
		"serial":              serialN,
		"device-key":          string(encDevKey),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(st, serial)
	c.Assert(err, IsNil)
	return serial.(*asserts.Serial)
}

// mockRestartAndSettle expects the state to be locked
func (s *baseMgrsSuite) mockRestartAndSettle(c *C, st *state.State, chg *state.Change) {
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)

	st.Unlock()
	defer st.Lock()
	err := s.o.Settle(settleTimeout)
	c.Check(err, IsNil)
}

// XXX: We have some very similar code in hookstate/ctlcmd/is_connected_test.go
//
//	should this be moved to overlord/snapstate/snapstatetest as a common
//	helper
func (ms *baseMgrsSuite) mockInstalledSnapWithFiles(c *C, snapYaml string, files [][]string) *snap.Info {
	return ms.mockInstalledSnapWithRevAndFiles(c, snapYaml, snap.R(1), files)
}

func (ms *baseMgrsSuite) mockInstalledSnapWithRevAndFiles(c *C, snapYaml string, rev snap.Revision, files [][]string) *snap.Info {
	st := ms.o.State()

	info := snaptest.MockSnapWithFiles(c, snapYaml, &snap.SideInfo{Revision: rev}, files)
	si := &snap.SideInfo{
		RealName: info.SnapName(),
		SnapID:   fakeSnapID(info.SnapName()),
		Revision: info.Revision,
	}
	snapstate.Set(st, info.InstanceName(), &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  info.Revision,
		SnapType: string(info.Type()),
	})
	return info
}

type mgrsSuite struct {
	baseMgrsSuite
}

type mgrsSuiteCore struct {
	baseMgrsSuite
}

var _ = Suite(&mgrsSuiteCore{})

func (s *mgrsSuiteCore) SetUpTest(c *C) {
	// We check for OnClassic when creating the manager. But we need
	// to add the clean-up after the call BaseTest.SetUpTest() to avoid
	// it panicking.
	restore := release.MockOnClassic(false)
	s.baseMgrsSuite.SetUpTest(c)
	s.AddCleanup(restore)
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

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "foo"}, snapPath, "", "", snapstate.Flags{DevMode: true}, nil)
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

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "foo"}, snapPath, "", "", snapstate.Flags{DevMode: true}, nil)
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
	// just README, bin, snapd, and core (snapd and core are there because we
	// have info files for those snaps which need to be read from the snapstate
	// Ensure loop)
	c.Check(snapdirs, HasLen, 4)
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
	c.Assert(s.automaticSnapshots, DeepEquals, []automaticSnapshotCall{{"foo", map[string]interface{}{"key": "value"}, nil, nil}})
}

func (s *mgrsSuite) TestHappyRemoveWithQuotas(c *C) {
	// TODO: we need a variant of this test which disables the memory cgroup
	// after installing the snap to ensure that the snap can be removed
	// successfully when the memory cgroup is disabled
	r := systemd.MockSystemdVersion(248, nil)
	defer r()

	r = servicestate.EnsureQuotaUsability()
	defer r()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
  daemon: simple
`
	s.installLocalTestSnap(c, snapYamlContent+"version: 1.0")

	// put the snap in a quota group
	err := servicestatetest.MockQuotaInState(st, "quota-grp", "", []string{"foo"}, nil,
		quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build())
	c.Assert(err, IsNil)

	ts, err := snapstate.Remove(st, "foo", snap.R(0), &snapstate.RemoveFlags{Purge: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("remove-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("remove-snap change failed with: %v", chg.Err()))

	// ensure that the quota group no longer contains the snap we removed
	grp, err := servicestate.AllQuotas(st)
	c.Assert(grp, HasLen, 1)
	c.Assert(grp["quota-grp"].Snaps, HasLen, 0)
	c.Assert(err, IsNil)
}

func (s *mgrsSuite) TestHappyRefreshWithQuotasInServiceUnitMaintained(c *C) {
	// TODO: here we need a variant of this test which sets the memory cgroup
	// as enabled here, and then disables it after installing the first revision
	// before performing the refresh in order to catch a regression like what we
	// had in https://github.com/snapcore/snapd/pull/11339

	r := systemd.MockSystemdVersion(248, nil)
	defer r()

	r = servicestate.EnsureQuotaUsability()
	defer r()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
  daemon: simple
`
	si := s.installLocalTestSnap(c, snapYamlContent+"version: 1.0")

	// add the snap to a quota group
	ts, err := servicestate.CreateQuota(st, "grp", servicestate.CreateQuotaOptions{
		Snaps:          []string{"foo"},
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	})
	c.Assert(err, IsNil)
	quotaUpdateChg := st.NewChange("update-quota", "...")
	quotaUpdateChg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// ensure that the service unit was written with the Slice=... setting
	c.Assert(si.Apps["bar"].ServiceFile(), testutil.FileContains, "Slice=snap.grp.slice")

	// now refresh the snap
	s.installLocalTestSnap(c, snapYamlContent+"version: 2.0")

	// ensure that the snap service unit still has the Slice=... setting
	c.Assert(si.Apps["bar"].ServiceFile(), testutil.FileContains, "Slice=snap.grp.slice")

	// and also ensure that the snap is still referenced
	// this is copied from servicestate/internal.AllQuotas, because
	// servicestate.AllQuotas errors when the memory cgroup is disabled
	var quotas map[string]*quota.Group
	err = st.Get("quotas", &quotas)
	c.Assert(err, IsNil)
	c.Assert(quotas, HasLen, 1)
	c.Assert(quotas["grp"].Snaps, DeepEquals, []string{"foo"})
}

func fakeSnapID(name string) string {
	if id := naming.WellKnownSnapID(name); id != "" {
		return id
	}
	return snaptest.AssertedSnapID(name)
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
	"snap-yaml": @SNAP_YAML@,
        "base": @BASE@,
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

	s.makeStoreSnapRevision(c, info.SnapName(), revno, snapDigest, size)

	return snapPath, snapDigest
}

func (s *baseMgrsSuite) makeStoreTestSnap(c *C, snapYaml string, revno string) (path, digest string) {
	return s.makeStoreTestSnapWithFiles(c, snapYaml, revno, nil)
}

func (s *baseMgrsSuite) makeStoreSnapRevision(c *C, name, revno, digest string, size uint64) asserts.Assertion {
	headers := map[string]interface{}{
		"snap-id":       fakeSnapID(name),
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-revision": revno,
		"developer-id":  "devdevdev",
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapRev)
	c.Assert(err, IsNil, Commentf("cannot add snap revision %v", headers))
	return snapRev
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

func (s *baseMgrsSuite) newestThatCanRead(name string, epoch snap.Epoch) (info *snap.Info, rawInfo, rev string) {
	if s.serveSnapPath[name] == "" {
		return nil, "", ""
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
		rawInfo, err := snapf.ReadFile("meta/snap.yaml")
		if err != nil {
			panic(err)
		}
		if info.Epoch.CanRead(epoch) {
			return info, string(rawInfo), rev
		}
		idx--
		if idx < 0 {
			return nil, "", ""
		}
		path = s.serveOldPaths[name][idx]
		rev = s.serveOldRevs[name][idx]
	}
}

func (s *baseMgrsSuite) mockStore(c *C) *httptest.Server {
	var baseURL *url.URL
	fillHit := func(hitTemplate, revno string, info *snap.Info, rawInfo string) string {
		epochBuf, err := json.Marshal(info.Epoch)
		if err != nil {
			panic(err)
		}
		rawInfoBuf, err := json.Marshal(rawInfo)
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
		hit = strings.Replace(hit, `@SNAP_YAML@`, string(rawInfoBuf), -1)
		baseStr := "null"
		if info.Base != "" {
			baseStr = fmt.Sprintf("%q", info.Base)
		}
		hit = strings.Replace(hit, `@BASE@`, baseStr, -1)
		return hit
	}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// all URLS are /api/v1/snaps/... or /v2/snaps/ or /v2/assertions/... so
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
			if comps[2] == "assertions" {
				// preserve "assertions" component
				comps = comps[2:]
			} else {
				// drop common "snap" component
				comps = comps[3:]
			}
			comps[0] = "v2:" + comps[0]
		}

		switch comps[0] {
		case "auth:nonces":
			w.Write([]byte(`{"nonce": "NONCE"}`))
			return
		case "auth:sessions":
			// quick validity check
			reqBody, err := ioutil.ReadAll(r.Body)
			c.Check(err, IsNil)
			c.Check(bytes.Contains(reqBody, []byte("nonce: NONCE")), Equals, true)
			c.Check(bytes.Contains(reqBody, []byte(fmt.Sprintf("serial: %s", s.expectedSerial))), Equals, true)
			c.Check(bytes.Contains(reqBody, []byte(fmt.Sprintf("store: %s", s.expectedStore))), Equals, true)

			c.Check(s.sessionMacaroon, Not(Equals), "")
			w.WriteHeader(200)
			w.Write([]byte(fmt.Sprintf(`{"macaroon": "%s"}`, s.sessionMacaroon)))
			return
		case "v2:assertions":
			ref := &asserts.Ref{
				Type:       asserts.Type(comps[1]),
				PrimaryKey: comps[2:],
			}

			seq := r.URL.Query().Get("sequence")
			resolve := func() (asserts.Assertion, error) { return ref.Resolve(s.storeSigning.Find) }
			if seq == "latest" {
				hdrs, err := asserts.HeadersFromSequenceKey(ref.Type, ref.PrimaryKey)
				if err != nil {
					panic(err)
				}
				resolve = func() (asserts.Assertion, error) {
					return s.storeSigning.FindSequence(ref.Type, hdrs, -1, ref.Type.MaxSupportedFormat())
				}
			}

			a, err := resolve()
			if errors.Is(err, &asserts.NotFoundError{}) {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(404)
				w.Write([]byte(`{"error-list":[{"code":"not-found","message":"..."}]}`))
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
					Revision int `json:"revision"`
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
						urls = append(urls, fmt.Sprintf("%s/v2/assertions/%s", baseURL.String(), ref.Unique()))

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
				if a.Action == "install" || a.Action == "download" {
					name = a.Name
					epoch = a.Epoch
				}

				info, rawInfo, revno := s.newestThatCanRead(name, epoch)
				if info == nil {
					// no match
					continue
				}

				if a.Revision != 0 {
					revno = strconv.Itoa(a.Revision)
				}

				results = append(results, resultJSON{
					Result:      a.Action,
					SnapID:      a.SnapID,
					InstanceKey: a.InstanceKey,
					Name:        name,
					Snap:        json.RawMessage(fillHit(snapV2, revno, info, rawInfo)),
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

	// validity checks
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
	// but serves as a validity check for the Without case that follows
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

	// validity checks
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

	affected, tasksets, err := snapstate.InstallMany(st, snapNames, nil, 0, nil)
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

	// validity checks
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
	affected, tasksets, err = snapstate.UpdateMany(context.TODO(), st, nil, nil, 0, &snapstate.Flags{})
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

func (s *mgrsSuite) TestTransactionalInstallManyFails(c *C) {
	// test transactional install through store, failing case

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

	affected, tasksets, err := snapstate.InstallMany(st, snapNames, nil, 0, &snapstate.Flags{Transaction: client.TransactionAllSnaps})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, snapNames)
	chg := st.NewChange("install-snaps", "...")
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

	// confirm it failed
	c.Assert(chg.Status(), Equals, state.ErrorStatus, Commentf("install-snap change not failed"))

	// validity checks
	for _, name := range snapNames {
		_, err := snapstate.CurrentInfo(st, name)
		c.Assert(err, DeepEquals,
			&snap.NotInstalledError{Snap: name, Rev: snap.Revision{N: 0}})
	}
}

func (s *mgrsSuite) TestTransactionalInstallManyOkUpdateManyFails(c *C) {
	// test transactional install many through store, which works, and update
	// many with a failure, so all refreshes fail

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

	affected, tasksets, err := snapstate.InstallMany(st, snapNames, nil, 0, &snapstate.Flags{Transaction: client.TransactionAllSnaps})
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

	// validity checks
	for _, name := range snapNames {
		info, err := snapstate.CurrentInfo(st, name)
		c.Assert(err, IsNil)
		c.Assert(info.Revision, Equals, snap.R(1))
		c.Assert(info.SnapID, Equals, fakeSnapID(name))
		c.Assert(info.Epoch.String(), Equals, "0")
	}

	// now add some more snap revisions
	revno := "2"
	for _, name := range snapNames {
		snapPath, _ := s.makeStoreTestSnap(c,
			fmt.Sprintf("{name: %s, version: 1}", name), revno)
		s.serveSnap(snapPath, revno)
	}

	// refresh
	affected, tasksets, err = snapstate.UpdateMany(context.TODO(), st, nil, nil, 0,
		&snapstate.Flags{Transaction: client.TransactionAllSnaps})
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

		// All failed: still on rev 1 (epoch 0)
		c.Assert(info.Revision, Equals, snap.R(1))
		c.Assert(info.SnapID, Equals, fakeSnapID(name))
		c.Assert(info.Epoch.String(), Equals, "0")
	}
}

func (s *mgrsSuite) TestTransactionalInstallManyOkUpdateManyOk(c *C) {
	// test transactional install many through store and update
	// many, both with success

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

	affected, tasksets, err := snapstate.InstallMany(st, snapNames, nil, 0, &snapstate.Flags{Transaction: client.TransactionAllSnaps})
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

	// validity checks
	for _, name := range snapNames {
		info, err := snapstate.CurrentInfo(st, name)
		c.Assert(err, IsNil)
		c.Assert(info.Revision, Equals, snap.R(1))
		c.Assert(info.SnapID, Equals, fakeSnapID(name))
	}

	// now add some more snap revisions
	revno := "2"
	for _, name := range snapNames {
		snapPath, _ := s.makeStoreTestSnap(c,
			fmt.Sprintf("{name: %s, version: 1}", name), revno)
		s.serveSnap(snapPath, revno)
	}

	// refresh
	affected, tasksets, err = snapstate.UpdateMany(context.TODO(), st, nil, nil, 0,
		&snapstate.Flags{Transaction: client.TransactionAllSnaps})
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

	for _, name := range snapNames {
		comment := Commentf("%q", name)
		info, err := snapstate.CurrentInfo(st, name)
		c.Assert(err, IsNil, comment)

		c.Check(info.Revision, Equals, snap.R(revno))
		c.Check(info.SnapID, Equals, fakeSnapID(name))
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

	affected, tasksets, err := snapstate.InstallMany(st, snapNames, nil, 0, nil)
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

	// validity checks
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
	affected, tasksets, err = snapstate.UpdateMany(context.TODO(), st, nil, nil, 0, &snapstate.Flags{})
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

	ts, _, err := snapstate.InstallPath(st, si, snapPath, "", "", snapstate.Flags{DevMode: true}, nil)
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

	_, _, err = snapstate.InstallPath(st, si, snapPath, "bar_instance", "", snapstate.Flags{DevMode: true}, nil)
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

	_, _, err = snapstate.InstallPath(st, si, snapPath, "bar_invalid_instance_name", "", snapstate.Flags{DevMode: true}, nil)
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

	ts, _, err := snapstate.InstallPath(st, si, snapPath, "", "", snapstate.Flags{DevMode: true}, nil)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "bar", SnapID: "bar-id", Revision: snap.R(1)},
		}),
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

	updated, tss, err := snapstate.UpdateMany(context.TODO(), st, []string{"foo"}, nil, 0, nil)
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
	updated, tss, err = snapstate.UpdateMany(context.TODO(), st, []string{"foo"}, nil, 0, nil)
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

func (s *mgrsSuiteCore) TestInstallCoreSnapUpdatesBootloaderEnvAndSplitsAcrossRestart(c *C) {
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

	// InstallPath does not set any restart boundaries by itself, this is something
	// that must be handled where we use it, and actually schedule the change.
	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "core"}, snapPath, "", "", snapstate.Flags{}, nil)
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// final steps will are postponed until we are in the restarted snapd
	ok, rst := restart.Pending(st)
	c.Assert(ok, Equals, true)
	c.Assert(rst, Equals, restart.RestartSystem)

	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	// this is already set
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core_99.snap",
		"snap_try_core":   "core_x1.snap",
		"snap_try_kernel": "",
		"snap_mode":       boot.TryStatus,
	})

	// simulate successful restart happened, technically "core" is of type
	// "os", but for the purpose of the mock it is handled like a base
	s.mockSuccessfulReboot(c, chg, bloader, []snap.Type{snap.TypeBase})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core_x1.snap",
		"snap_try_core":   "",
		"snap_try_kernel": "",
		"snap_mode":       "",
	})
}

func (s *mgrsSuiteCore) TestInstallCoreSnapUpdatesBootloaderEnvAndFailWithRollback(c *C) {
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

	// InstallPath does not set any restart boundaries by itself, this is something
	// that must be handled where we use it, and actually schedule the change.
	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "core"}, snapPath, "", "", snapstate.Flags{}, nil)
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	// this is already set
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core_99.snap",
		"snap_try_core":   "core_x1.snap",
		"snap_try_kernel": "",
		"snap_mode":       boot.TryStatus,
	})

	// simulate a reboot in which bootloader updates the env
	s.mockRollbackAcrossReboot(c, chg, bloader, []snap.Type{snap.TypeBase})

	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.ErrorStatus, Commentf("install-snap change did not fail"))
	tLink := findKind(chg, "link-snap")
	c.Assert(tLink, NotNil)
	c.Assert(tLink.Status(), Equals, state.UndoneStatus)

	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core_99.snap",
		"snap_try_core":   "",
		"snap_try_kernel": "",
		"snap_mode":       "",
	})
}

type rebootEnv interface {
	SetTryingDuringReboot(which []snap.Type) error
	SetRollbackAcrossReboot(which []snap.Type) error
}

func (s *baseMgrsSuite) mockSuccessfulReboot(c *C, chg *state.Change, be rebootEnv, which []snap.Type) {
	st := s.o.State()
	restarting, restartType := restart.Pending(st)
	c.Assert(restarting, Equals, true, Commentf("mockSuccessfulReboot called when there was no pending restart"))
	c.Assert(restartType, Equals, restart.RestartSystem, Commentf("mockSuccessfulReboot called but restartType is not SystemRestart but %v", restartType))
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	if len(which) > 0 {
		err := be.SetTryingDuringReboot(which)
		c.Assert(err, IsNil)
	}
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	defer st.Lock()
	err := s.o.DeviceManager().Ensure()
	c.Assert(err, IsNil)
}

func (s *baseMgrsSuite) mockRollbackAcrossReboot(c *C, chg *state.Change, be rebootEnv, which []snap.Type) {
	st := s.o.State()
	restarting, restartType := restart.Pending(st)
	c.Assert(restarting, Equals, true, Commentf("mockRollbackAcrossReboot called when there was no pending restart"))
	c.Assert(restartType, Equals, restart.RestartSystem, Commentf("mockRollbackAcrossReboot called but restartType is not SystemRestart but %v", restartType))
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	err := be.SetRollbackAcrossReboot(which)
	c.Assert(err, IsNil)
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	s.o.Settle(settleTimeout)
	st.Lock()
}

func (s *mgrsSuiteCore) TestInstallKernelSnapUpdatesBootloaderEnv(c *C) {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, packageKernel, si1, [][]string{
		{"meta/kernel.yaml", ""},
	})
	si2 := &snap.SideInfo{RealName: "core18", Revision: snap.R(2)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
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

	// InstallPath does not set any restart boundaries by itself, this is something
	// that must be handled where we use it, and actually schedule the change.
	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "pc-kernel"}, snapPath, "", "", snapstate.Flags{}, nil)
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	// run, this will trigger a wait for the restart
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	// we are in restarting state and the change is not done yet
	restarting, _ := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core18_2.snap",
		"snap_try_core":   "",
		"snap_kernel":     "pc-kernel_123.snap",
		"snap_try_kernel": "pc-kernel_x1.snap",
		"snap_mode":       boot.TryStatus,
	})
	// pretend we restarted
	s.mockSuccessfulReboot(c, chg, bloader, []snap.Type{snap.TypeKernel})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core18_2.snap",
		"snap_try_core":   "",
		"snap_kernel":     "pc-kernel_x1.snap",
		"snap_try_kernel": "",
		"snap_mode":       "",
	})
}

func (s *mgrsSuiteCore) TestInstallKernelSnapUndoUpdatesBootloaderEnv(c *C) {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, packageKernel, si1, [][]string{
		{"meta/kernel.yaml", ""},
	})
	si2 := &snap.SideInfo{RealName: "core18", Revision: snap.R(2)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
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

	// InstallPath does not set any restart boundaries by itself, this is something
	// that must be handled where we use it, and actually schedule the change.
	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "pc-kernel"}, snapPath, "", "", snapstate.Flags{}, nil)
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
	restarting, _ := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Check(chg.Status(), Equals, state.WaitStatus)
	// pretend we restarted
	s.mockSuccessfulReboot(c, chg, bloader, []snap.Type{snap.TypeKernel})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// undoing will have retriggered a restart, and put the change
	// back into wait
	c.Assert(chg.Status(), Equals, state.WaitStatus)

	// and we undo the bootvars and trigger a reboot
	c.Check(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core18_2.snap",
		"snap_try_core":   "",
		"snap_try_kernel": "",
		"snap_kernel":     "pc-kernel_123.snap",
		"snap_mode":       boot.DefaultStatus,
	})
	restarting, _ = restart.Pending(st)
	c.Check(restarting, Equals, true)

	// pretend we restarted back to the old kernel
	s.mockSuccessfulReboot(c, chg, bloader, nil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// bootvars should not have changed
	c.Check(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core18_2.snap",
		"snap_try_core":   "",
		"snap_try_kernel": "",
		"snap_kernel":     "pc-kernel_123.snap",
		"snap_mode":       "",
	})
}

func (s *mgrsSuiteCore) TestInstallKernelSnap20UpdatesBootloaderEnv(c *C) {
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
	c.Assert(s.o.DeviceManager().ReloadModeenv(), IsNil)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	si1 := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, packageKernel, si1, [][]string{
		{"meta/kernel.yaml", ""},
	})
	si2 := &snap.SideInfo{RealName: "core20", Revision: snap.R(1)}
	snapstate.Set(st, "core20", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
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

	// InstallPath does not set any restart boundaries by itself, this is something
	// that must be handled where we use it, and actually schedule the change.
	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "pc-kernel"}, kernelSnapPath, "", "", snapstate.Flags{}, nil)
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
	restarting, _ := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Check(chg.Status(), Equals, state.WaitStatus)

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
	s.mockSuccessfulReboot(c, chg, bloader, []snap.Type{snap.TypeKernel})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	// also check that we are active on the second revision
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "pc-kernel", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Sequence.Revisions, HasLen, 2)
	c.Check(snapst.Sequence, DeepEquals, snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1, &kernelSnapInfo.SideInfo}))
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

func (s *mgrsSuiteCore) TestInstallKernelSnap20UndoUpdatesBootloaderEnv(c *C) {
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
	c.Assert(s.o.DeviceManager().ReloadModeenv(), IsNil)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	si1 := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, packageKernel, si1, [][]string{
		{"meta/kernel.yaml", ""},
	})
	si2 := &snap.SideInfo{RealName: "core20", Revision: snap.R(1)}
	snapstate.Set(st, "core20", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
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

	// InstallPath does not set any restart boundaries by itself, this is something
	// that must be handled where we use it, and actually schedule the change.
	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "pc-kernel"}, kernelSnapPath, "", "", snapstate.Flags{}, nil)
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
	restarting, _ := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Check(chg.Status(), Equals, state.WaitStatus)
	// pretend we restarted
	s.mockSuccessfulReboot(c, chg, bloader, []snap.Type{snap.TypeKernel})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// should result back into WaitStatus as the undo should have retriggered
	// a restart
	c.Assert(chg.Status(), Equals, state.WaitStatus)

	// we should have triggered a reboot to undo the boot changes
	restarting, _ = restart.Pending(st)
	c.Check(restarting, Equals, true)

	// we revert to the previous working kernel, so kernel_status is unset now
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"kernel_status": "",
	})

	// we should not have extracted any more kernel assets than before, since
	// the fallback kernel was already extracted
	extractedKernels = bloader.ExtractKernelAssetsCalls
	c.Assert(extractedKernels, HasLen, 1) // same as above check

	// also check that we are active on the first revision again
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "pc-kernel", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Sequence.Revisions, HasLen, 1)
	c.Check(snapst.Sequence, DeepEquals, snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}))
	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Current, DeepEquals, snap.R(1))

	// we will reboot to go back to the old kernel, we should now
	// have the old kernel on the bootloader and no try kernel
	finalCurrentKernel, err := bloader.Kernel()
	c.Assert(err, IsNil)
	c.Assert(finalCurrentKernel.Filename(), Equals, firstKernel.Filename())

	_, err = bloader.TryKernel()
	c.Assert(err, Equals, bootloader.ErrNoTryKernelRef)

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

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: snapName}, snapPath, "", "", snapstate.Flags{DevMode: true}, nil)
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
	ts, err := snapstate.Revert(st, "foo", snapstate.Flags{}, "")
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
	updated, tss, err := snapstate.UpdateMany(context.TODO(), st, nil, nil, 0, nil)
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
	err = assertstate.RefreshSnapDeclarations(st, 0, nil)
	c.Assert(err, IsNil)

	updated, tss, err := snapstate.UpdateMany(context.TODO(), st, nil, nil, 0, nil)
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

func (s *mgrsSuite) TestHappyRemoteInstallAndUpdateWithAndWithoutAppsForAutoAliases(c *C) {
	// there is a single snap declaration that covers all tracks/channels,
	// because of this it can list auto aliases for apps that do not exist
	// in a particular channel the the snap is installed from and track
	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
		"aliases": []interface{}{
			map[string]interface{}{"name": "app1", "target": "app1"},
			map[string]interface{}{"name": "app2", "target": "app2"},
		},
	})

	fooYamlJustApp1 := `name: foo
version: @VERSION@
apps:
 app1:
  command: bin/app1
`

	fooPath, _ := s.makeStoreTestSnap(c, strings.Replace(fooYamlJustApp1, "@VERSION@", "1.0", -1), "10")
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
	app2Alias := filepath.Join(dirs.SnapBinariesDir, "app2")
	c.Check(app1Alias, testutil.SymlinkTargetEquals, "foo.app1")
	c.Check(app2Alias, testutil.FileAbsent)

	fooYamlBothApps := `name: foo
version: @VERSION@
apps:
 app1:
  command: bin/app1
 app2:
  command: bin/app2
`

	// new foo version/revision with both apps
	fooPath, _ = s.makeStoreTestSnap(c, strings.Replace(fooYamlBothApps, "@VERSION@", "1.5", -1), "15")
	s.serveSnap(fooPath, "15")

	// refresh all
	updated, tss, err := snapstate.UpdateMany(context.TODO(), st, nil, nil, 0, nil)
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
		"app1": {Auto: "app1"},
		"app2": {Auto: "app2"},
	})

	c.Check(app1Alias, testutil.SymlinkTargetEquals, "foo.app1")
	c.Check(app2Alias, testutil.SymlinkTargetEquals, "foo.app2")

	// new revision has just one app again
	fooPath, _ = s.makeStoreTestSnap(c, strings.Replace(fooYamlJustApp1, "@VERSION@", "2.0", -1), "20")
	s.serveSnap(fooPath, "20")

	// refresh all
	updated, tss, err = snapstate.UpdateMany(context.TODO(), st, nil, nil, 0, nil)
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
	c.Check(info.Revision, Equals, snap.R(20))
	c.Check(info.Version, Equals, "2.0")

	var snapst3 snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst3)
	c.Assert(err, IsNil)
	c.Check(snapst3.AutoAliasesDisabled, Equals, false)
	c.Check(snapst3.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"app1": {Auto: "app1"},
	})

	c.Check(app1Alias, testutil.SymlinkTargetEquals, "foo.app1")
	c.Check(app2Alias, testutil.FileAbsent)
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

func (s *mgrsSuite) TestInstallWithAssumesIsRefusedEarly(c *C) {
	s.prereqSnapAssertions(c)

	revno := "1"
	snapYamlContent := `name: some-snap
version: 1.0
assumes: [something-that-is-not-provided]
`
	snapPath, _ := s.makeStoreTestSnap(c, snapYamlContent, revno)
	s.serveSnap(snapPath, revno)

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	_, err := snapstate.Install(context.TODO(), st, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" assumes unsupported features: something-that-is-not-provided \(try to refresh snapd\)`)
}

func (s *mgrsSuite) TestUpdateWithAssumesIsRefusedEarly(c *C) {
	s.prereqSnapAssertions(c)

	revno := "40"
	snapYamlContent := `name: some-snap
version: 1.0
assumes: [something-that-is-not-provided]
`
	snapPath, _ := s.makeStoreTestSnap(c, snapYamlContent, revno)
	s.serveSnap(snapPath, revno)

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(1)}
	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	_, err := snapstate.Update(st, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" assumes unsupported features: something-that-is-not-provided \(try to refresh snapd\)`)
}

func (s *mgrsSuite) TestUpdateManyWithAssumesIsRefusedEarly(c *C) {
	s.prereqSnapAssertions(c)

	revno := "40"
	snapYamlContent := `name: some-snap
version: 1.0
assumes: [something-that-is-not-provided]
`
	snapPath, _ := s.makeStoreTestSnap(c, snapYamlContent, revno)
	s.serveSnap(snapPath, revno)

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(1)}
	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	// updateMany will just skip snaps with assumes but not error
	affected, tss, err := snapstate.UpdateMany(context.TODO(), st, nil, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Check(tss, HasLen, 0)
	c.Check(affected, HasLen, 0)
	// the skipping is logged though
	c.Check(s.logbuf.String(), testutil.Contains, `cannot update "some-snap": snap "some-snap" assumes unsupported features: something-that-is-not-provided (try`)
}

type storeCtxSetupSuite struct {
	o  *overlord.Overlord
	sc store.DeviceAndAuthContext

	storeSigning   *assertstest.StoreStack
	restoreTrusted func()

	brands *assertstest.SigningAccounts

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

	ts1, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: snapName1, SnapID: fakeSnapID(snapName1), Revision: snap.R(3)}, snapPath1, "", "", snapstate.Flags{DevMode: true}, nil)
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts1)

	ts2, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: snapName2, SnapID: fakeSnapID(snapName2), Revision: snap.R(3)}, snapPath2, "", "", snapstate.Flags{DevMode: true}, nil)
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
	ts2, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "snap2", SnapID: fakeSnapID("snap2"), Revision: snap.R(3)}, snapPath, "", "", snapstate.Flags{DevMode: true}, nil)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(st, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{oi}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	repo := s.o.InterfaceManager().Repository()

	// add snaps to the repo to have plugs/slots
	c.Assert(repo.AddSnap(snapInfo), IsNil)
	c.Assert(repo.AddSnap(otherInfo), IsNil)
	c.Assert(repo.AddSnap(coreInfo), IsNil)

	// refresh all
	err := assertstate.RefreshSnapDeclarations(st, 0, nil)
	c.Assert(err, IsNil)

	updates, tts, err := snapstate.UpdateMany(context.TODO(), st, []string{"core", "some-snap", "other-snap"}, nil, 0, nil)
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
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// simulate successful restart happened
	restart.MockPending(st, restart.RestartUnset)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si0, si1, si2}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	repo := s.o.InterfaceManager().Repository()

	// add snaps to the repo to have plugs/slots
	c.Assert(repo.AddSnap(snapInfo), IsNil)
	c.Assert(repo.AddSnap(coreInfo), IsNil)

	// refresh all
	err := assertstate.RefreshSnapDeclarations(st, 0, nil)
	c.Assert(err, IsNil)

	updates, tts, err := snapstate.UpdateMany(context.TODO(), st, []string{"some-snap"}, nil, 0, nil)
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

// addSnapServices adds service units for the applications from the snap which
// are services. The services do not get enabled or started.
func (s *mgrsSuite) addSnapServices(snapInfo *snap.Info) error {
	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		snapInfo: nil,
	}
	ensureOpts := &wrappers.EnsureSnapServicesOptions{}
	return wrappers.EnsureSnapServices(m, ensureOpts, nil, progress.Null)
}

func (s *mgrsSuite) TestUpdateFailOnUnlinkRestores(c *C) {
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		// inject an error during removal
		if cmd[0] == "--no-reload" && cmd[1] == "disable" && cmd[2] == "snap.some-snap.svc1.service" {
			return nil, fmt.Errorf("timeout")
		}
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, nil); out != nil {
			return out, nil
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	const someSnapYaml = `name: some-snap
version: 1.0
apps:
   svc1:
     command: bin.sh
     daemon: simple
`
	snapPath, _ := s.makeStoreTestSnap(c, someSnapYaml, "40")
	s.serveSnap(snapPath, "40")

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	si1 := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(1)}
	snapInfo := snaptest.MockSnap(c, someSnapYaml, si1)
	s.addSnapServices(snapInfo)

	// ensure service file is present for our service
	svcPath := path.Join(dirs.GlobalRootDir, "etc/systemd/system/snap.some-snap.svc1.service")
	c.Check(svcPath, testutil.FilePresent)

	// some-snap has inactive revisions
	si0 := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(0)}
	si2 := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(2)}
	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si0, si1, si2}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	// refresh all
	err := assertstate.RefreshSnapDeclarations(st, 0, nil)
	c.Assert(err, IsNil)

	updates, tts, err := snapstate.UpdateMany(context.TODO(), st, []string{"some-snap"}, nil, 0, nil)
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

	// ensure that the error was observed
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	var unlinkTask *state.Task
	for _, t := range chg.Tasks() {
		if t.Kind() == "unlink-current-snap" {
			unlinkTask = t
			break
		}
	}
	c.Check(unlinkTask, NotNil)
	c.Check(unlinkTask.Status(), Equals, state.ErrorStatus)

	// ensure that service files are still present
	c.Check(svcPath, testutil.FilePresent)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(st, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{oi}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	repo := s.o.InterfaceManager().Repository()

	// add snaps to the repo to have plugs/slots
	c.Assert(repo.AddSnap(snapInfo), IsNil)
	c.Assert(repo.AddSnap(otherInfo), IsNil)

	// refresh all
	err := assertstate.RefreshSnapDeclarations(st, 0, nil)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(st, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{oi}),
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
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(st, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{oi}),
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
	isKernel
	hasModeenv
)

func validateInstallTasks(c *C, tasks []*state.Task, name, revno string, flags int) int {
	var i int
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Mount snap "%s" (%s)`, name, revno))
	i++
	if flags&isGadget != 0 || flags&isKernel != 0 {
		what := "gadget"
		if flags&isKernel != 0 {
			what = "kernel"
		}
		if flags&isKernel != 0 && flags&hasModeenv != 0 {
			c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Setup kernel driver tree for "%s" (%s)`, name, revno))
			i++
		}
		c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Update assets from %s "%s" (%s)`, what, name, revno))
		i++
	}
	if flags&isGadget != 0 {
		c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Update kernel command line from gadget %q (%s)`, name, revno))
		i++
	}
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Copy snap "%s" data`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Setup snap "%s" (%s) security profiles`, name, revno))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Make snap "%s" (%s) available to the system`, name, revno))
	i++
	if flags&isKernel != 0 && flags&hasModeenv != 0 {
		c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Cleanup kernel driver tree for "%s" (%s)`, name, revno))
		i++
	}
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Automatically connect eligible plugs and slots of snap "%s"`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Set automatic aliases for snap "%s"`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Setup snap "%s" aliases`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Run install hook of "%s" snap if present`, name))
	i++
	if flags&noConfigure == 0 {
		c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Run default-configure hook of "%s" snap if present`, name))
		i++
	}
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
	if flags&isGadget != 0 || flags&isKernel != 0 {
		what := "gadget"
		if flags&isKernel != 0 {
			what = "kernel"
		}
		if flags&isKernel != 0 && flags&hasModeenv != 0 {
			c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Setup kernel driver tree for "%s" (%s)`, name, revno))
			i++
		}
		c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Update assets from %s %q (%s)`, what, name, revno))
		i++
	}
	if flags&isGadget != 0 {
		c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Update kernel command line from gadget %q (%s)`, name, revno))
		i++
	}
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Copy snap "%s" data`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Setup snap "%s" (%s) security profiles`, name, revno))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Make snap "%s" (%s) available to the system`, name, revno))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Automatically connect eligible plugs and slots of snap "%s"`, name))
	i++
	if flags&isKernel != 0 && flags&hasModeenv != 0 {
		c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Cleanup kernel driver tree for "%s" (%s)`, name, revno))
		i++
	}
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
	if flags&noConfigure == 0 {
		c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Run configure hook of "%s" snap if present`, name))
		i++
	}
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Run health check of "%s" snap`, name))
	i++
	return i
}

func validateRecoverySystemTasks(c *C, tasks []*state.Task, label string) int {
	var i int
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Create recovery system with label %q`, label))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Finalize recovery system with label %q`, label))
	i++
	return i
}

func validateGadgetSwitchTasks(c *C, tasks []*state.Task, label, rev string) int {
	var i int
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Update assets from gadget %q (%s) for remodel`, label, rev))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Update kernel command line from gadget %q (%s) for remodel`, label, rev))
	i++
	return i
}

// byReadyTime sorts a list of tasks by their "ready" time
type byReadyTime []*state.Task

func (a byReadyTime) Len() int           { return len(a) }
func (a byReadyTime) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byReadyTime) Less(i, j int) bool { return a[i].ReadyTime().Before(a[j].ReadyTime()) }

func (s *mgrsSuiteCore) TestRemodelRequiredSnapsAdded(c *C) {
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

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

	snapstatetest.InstallEssentialSnaps(c, st, "core", nil, bloader)

	// pretend we have an old required snap installed
	si1 := &snap.SideInfo{RealName: "old-required-snap-1", Revision: snap.R(1)}
	snapstate.Set(st, "old-required-snap-1", &snapstate.SnapState{
		SnapType: "app",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
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
	s.makeSerialAssertionInState(c, st, "my-brand", "my-model", "serialserialserial")

	// create a new model
	newModel := s.brands.Model("my-brand", "my-model", modelDefaults, map[string]interface{}{
		"required-snaps": []interface{}{"foo", "bar", "baz"},
		"revision":       "1",
	})

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	c.Check(devicestate.RemodelingChange(st), NotNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	c.Check(devicestate.RemodelingChange(st), IsNil)

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

func (s *mgrsSuiteCore) TestRemodelRequiredSnapsAddedUndo(c *C) {
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

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

	snapstatetest.InstallEssentialSnaps(c, st, "core", nil, bloader)

	// pretend we have an old required snap installed
	si1 := &snap.SideInfo{RealName: "old-required-snap-1", Revision: snap.R(1)}
	snapstate.Set(st, "old-required-snap-1", &snapstate.SnapState{
		SnapType: "app",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
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
	s.makeSerialAssertionInState(c, st, "my-brand", "my-model", "serialserialserial")

	// create a new model
	newModel := s.brands.Model("my-brand", "my-model", modelDefaults, map[string]interface{}{
		"required-snaps": []interface{}{"foo", "bar", "baz"},
		"revision":       "1",
	})

	devicestate.InjectSetModelError(fmt.Errorf("boom"))
	defer devicestate.InjectSetModelError(nil)

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
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
		c.Assert(err, testutil.ErrorIs, state.ErrNoState)
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

func (s *mgrsSuiteCore) TestRemodelDifferentBase(c *C) {
	// make "core18" snap available in the store
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
	s.makeSerialAssertionInState(c, st, "can0nical", "my-model", "serialserialserial")

	// create a new model
	newModel := s.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"base":     "core18",
		"revision": "1",
	})

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, ErrorMatches, "cannot remodel from core to bases yet")
	c.Assert(chg, IsNil)
}

func (ms *mgrsSuiteCore) TestRemodelSwitchToDifferentBase(c *C) {
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

	snapstatetest.InstallSnap(c, st, "name: pc-kernel\nversion: 1\ntype: kernel\n", nil, &snap.SideInfo{
		SnapID:   fakeSnapID("pc-kernel"),
		Revision: snap.R(1),
		RealName: "pc-kernel",
	}, snapstatetest.InstallSnapOptions{Required: true})

	si := &snap.SideInfo{RealName: "core18", SnapID: fakeSnapID("core18"), Revision: snap.R(1)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "base",
	})
	si2 := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	gadgetSnapYaml := "name: pc\nversion: 1.0\ntype: gadget"
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
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

	// add "pc-20" snap to fake store
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "pc-20",
		"publisher-id": "can0nical",
	})
	const pc20Yaml = `name: pc-20
type: gadget
version: 20.04
base: core20
`
	snapPath, _ := ms.makeStoreTestSnapWithFiles(c, pc20Yaml, "2", [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})
	ms.serveSnap(snapPath, "2")

	// add "core20" snap to fake store
	const core20Yaml = `name: core20
type: base
version: 20.04`
	snapPath, _ = ms.makeStoreTestSnap(c, core20Yaml, "2")
	ms.serveSnap(snapPath, "2")

	// add "foo" snap to fake store
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
	})
	snapPath, _ = ms.makeStoreTestSnap(c, `{name: "foo", version: 1.0, base: "core20"}`, "1")
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
	ms.makeSerialAssertionInState(c, st, "can0nical", "my-model", "serialserialserial")

	// create a new model
	newModel := ms.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"base":           "core20",
		"gadget":         "pc-20",
		"revision":       "1",
		"required-snaps": []interface{}{"foo"},
	})

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// system waits for a restart because of the new base
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoStatus)

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
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
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

	ms.mockRestartAndSettle(c, st, chg)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	// ensure tasks were run in the right order
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	// first all downloads/checks in sequential order
	var i int
	i += validateDownloadCheckTasks(c, tasks[i:], "core20", "2", "stable")
	i += validateDownloadCheckTasks(c, tasks[i:], "pc-20", "2", "stable")
	i += validateDownloadCheckTasks(c, tasks[i:], "foo", "1", "stable")

	// then all installs in sequential order
	i += validateInstallTasks(c, tasks[i:], "core20", "2", noConfigure)
	i += validateInstallTasks(c, tasks[i:], "pc-20", "2", isGadget)
	i += validateInstallTasks(c, tasks[i:], "foo", "1", 0)

	// ensure that we only have the tasks we checked (plus the one
	// extra "set-model" task)
	c.Assert(tasks, HasLen, i+1)
}

func (ms *mgrsSuiteCore) TestRemodelSwitchToDifferentBaseUndo(c *C) {
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

	snapstatetest.InstallSnap(c, st, "name: pc-kernel\nversion: 1\ntype: kernel\n", nil, &snap.SideInfo{
		SnapID:   fakeSnapID("pc-kernel"),
		Revision: snap.R(1),
		RealName: "pc-kernel",
	}, snapstatetest.InstallSnapOptions{Required: true})

	si := &snap.SideInfo{RealName: "core18", SnapID: fakeSnapID("core18"), Revision: snap.R(1)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "base",
	})
	snaptest.MockSnapWithFiles(c, "name: core18\ntype: base\nversion: 1.0", si, nil)

	si2 := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	gadgetSnapYaml := "name: pc\nversion: 1.0\ntype: gadget"
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
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

	// add "pc-20" snap to fake store
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "pc-20",
		"publisher-id": "can0nical",
	})
	const pc20Yaml = `name: pc-20
type: gadget
version: 20.04
base: core20
`
	snapPath, _ := ms.makeStoreTestSnapWithFiles(c, pc20Yaml, "2", [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})
	ms.serveSnap(snapPath, "2")

	// add "core20" snap to fake store
	const core20Yaml = `name: core20
type: base
version: 20.04`
	snapPath, _ = ms.makeStoreTestSnap(c, core20Yaml, "2")
	ms.serveSnap(snapPath, "2")

	// add "foo" snap to fake store
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
	})
	snapPath, _ = ms.makeStoreTestSnap(c, `{name: "foo", version: 1.0, base: "core20"}`, "1")
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
	ms.makeSerialAssertionInState(c, st, "can0nical", "my-model", "serialserialserial")

	// create a new model
	newModel := ms.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"base":           "core20",
		"gadget":         "pc-20",
		"revision":       "1",
		"required-snaps": []interface{}{"foo"},
	})

	devicestate.InjectSetModelError(fmt.Errorf("boom"))
	defer devicestate.InjectSetModelError(nil)

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// system waits for a restart because of the new base
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoStatus)

	// check that the boot vars got updated as expected
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_mode":       boot.TryStatus,
		"snap_core":       "core18_1.snap",
		"snap_try_core":   "core20_2.snap",
		"snap_kernel":     "pc-kernel_1.snap",
		"snap_try_kernel": "",
	})
	// simulate successful restart happened
	ms.mockSuccessfulReboot(c, chg, bloader, []snap.Type{snap.TypeBase})
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

	ms.mockRestartAndSettle(c, st, chg)

	// we are in restarting state
	restarting, restartType := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Check(restartType, Equals, restart.RestartSystem)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// Restart and re-run to completion
	ms.mockRestartAndSettle(c, st, chg)
	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// and the undo gave us our old base back
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core18_1.snap",
		"snap_try_core":   "",
		"snap_kernel":     "pc-kernel_1.snap",
		"snap_try_kernel": "",
		"snap_mode":       boot.DefaultStatus,
	})
}

func (ms *mgrsSuiteCore) TestRemodelSwitchToDifferentBaseUndoOnRollback(c *C) {
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

	snapstatetest.InstallSnap(c, st, "name: pc-kernel\nversion: 1\ntype: kernel\n", nil, &snap.SideInfo{
		SnapID:   fakeSnapID("pc-kernel"),
		Revision: snap.R(1),
		RealName: "pc-kernel",
	}, snapstatetest.InstallSnapOptions{Required: true})

	snapstatetest.InstallSnap(c, st, "name: core18\nversion: 1\ntype: base\n", nil, &snap.SideInfo{
		SnapID:   fakeSnapID("core18"),
		Revision: snap.R(1),
		RealName: "core18",
	}, snapstatetest.InstallSnapOptions{Required: true})

	si2 := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	gadgetSnapYaml := "name: pc\nversion: 1.0\ntype: gadget"
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
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

	// add "pc-20" snap to fake store
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "pc-20",
		"publisher-id": "can0nical",
	})
	const pc20Yaml = `name: pc-20
type: gadget
version: 20.04
base: core20
`
	snapPath, _ := ms.makeStoreTestSnapWithFiles(c, pc20Yaml, "2", [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})
	ms.serveSnap(snapPath, "2")

	// add "core20" snap to fake store
	const core20Yaml = `name: core20
type: base
version: 20.04`
	snapPath, _ = ms.makeStoreTestSnap(c, core20Yaml, "2")
	ms.serveSnap(snapPath, "2")

	// add "foo" snap to fake store
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
	})
	snapPath, _ = ms.makeStoreTestSnap(c, `{name: "foo", version: 1.0, base: "core20"}`, "1")
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
	ms.makeSerialAssertionInState(c, st, "can0nical", "my-model", "serialserialserial")

	// create a new model
	newModel := ms.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"base":           "core20",
		"gadget":         "pc-20",
		"revision":       "1",
		"required-snaps": []interface{}{"foo"},
	})

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// system waits for a restart because of the new base
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoStatus)

	// check that the boot vars got updated as expected
	c.Assert(bloader.BootVars, DeepEquals, map[string]string{
		"snap_mode":       boot.TryStatus,
		"snap_core":       "core18_1.snap",
		"snap_try_core":   "core20_2.snap",
		"snap_kernel":     "pc-kernel_1.snap",
		"snap_try_kernel": "",
	})
	// simulate successful restart happened
	ms.mockRollbackAcrossReboot(c, chg, bloader, []snap.Type{snap.TypeBase})
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

	ms.mockRestartAndSettle(c, st, chg)

	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// and we are *not* in restarting state
	restarting, _ := restart.Pending(st)
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

func (ms *mgrsSuite) TestRefreshSimpleSameRev(c *C) {
	// the "some-snap" in rev1
	snapYaml := "name: some-snap\nversion: 1.0"
	revStr := "1"
	// is available in the store
	snapPath, _ := ms.makeStoreTestSnap(c, snapYaml, revStr)
	ms.serveSnap(snapPath, revStr)

	mockServer := ms.mockStore(c)
	ms.AddCleanup(mockServer.Close)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// and some-snap:rev1 is also installed
	info := ms.mockInstalledSnapWithRevAndFiles(c, snapYaml, snap.R(revStr), nil)

	// now refresh from rev1 to rev1
	revOpts := &snapstate.RevisionOptions{Revision: snap.R(revStr)}
	ts, err := snapstate.Update(st, "some-snap", revOpts, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	chg := st.NewChange("refresh", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	// the snap file is in the right place
	c.Check(info.MountFile(), testutil.FilePresent)

	// rev1 is installed
	var snapst snapstate.SnapState
	snapstate.Get(st, "some-snap", &snapst)
	info, err = snapst.CurrentInfo()
	c.Assert(err, IsNil)
	c.Assert(info.Revision, Equals, snap.R(1))
}

func (ms *mgrsSuite) TestRefreshSimplePrevRev(c *C) {
	// the "some-snap" in rev1
	snapYaml := "name: some-snap\nversion: 1.0"
	revStr := "1"
	// is available in the store
	snapPath, _ := ms.makeStoreTestSnap(c, snapYaml, revStr)
	ms.serveSnap(snapPath, revStr)

	mockServer := ms.mockStore(c)
	ms.AddCleanup(mockServer.Close)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// and some-snap at both rev1, rev2 are installed
	info := snaptest.MockSnapWithFiles(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)}, nil)
	snaptest.MockSnapWithFiles(c, snapYaml, &snap.SideInfo{Revision: snap.R(2)}, nil)
	si1 := &snap.SideInfo{
		RealName: info.SnapName(),
		SnapID:   fakeSnapID(info.SnapName()),
		Revision: snap.R(1),
	}
	si2 := &snap.SideInfo{
		RealName: info.SnapName(),
		SnapID:   fakeSnapID(info.SnapName()),
		Revision: snap.R(2),
	}
	snapstate.Set(st, info.InstanceName(), &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1, si2}),
		Current:  snap.R(2),
		SnapType: string(info.Type()),
	})

	// now refresh from rev2 to the local rev1
	revOpts := &snapstate.RevisionOptions{Revision: snap.R(revStr)}
	ts, err := snapstate.Update(st, "some-snap", revOpts, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	chg := st.NewChange("refresh", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	// the snap file is in the right place
	c.Check(info.MountFile(), testutil.FilePresent)

	var snapst snapstate.SnapState
	snapstate.Get(st, "some-snap", &snapst)
	info, err = snapst.CurrentInfo()
	c.Assert(err, IsNil)
	c.Assert(info.Revision, Equals, snap.R(1))
}

func (ms *mgrsSuite) TestRefreshSimpleSameRevFromLocalFile(c *C) {
	// the "some-snap" in rev1
	snapYaml := "name: some-snap\nversion: 1.0"
	revStr := "1"

	// pretend we got a temp snap file from e.g. the snapd daemon
	tmpSnapFile := makeTestSnap(c, snapYaml)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// and some-snap:rev1 is also installed
	info := ms.mockInstalledSnapWithRevAndFiles(c, snapYaml, snap.R(revStr), nil)

	// now refresh from rev1 to rev1
	flags := snapstate.Flags{RemoveSnapPath: true}
	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "some-snap", Revision: snap.R(revStr)}, tmpSnapFile, "", "", flags, nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("refresh", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	// the temp file got cleaned up
	snapsup, err := snapstate.TaskSnapSetup(chg.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.RemoveSnapPath, Equals, true)
	c.Check(snapsup.SnapPath, testutil.FileAbsent)

	// the snap file is in the right place
	c.Check(info.MountFile(), testutil.FilePresent)

	var snapst snapstate.SnapState
	snapstate.Get(st, "some-snap", &snapst)
	info, err = snapst.CurrentInfo()
	c.Assert(err, IsNil)
	c.Assert(info.Revision, Equals, snap.R(1))
}

func (ms *mgrsSuite) TestRefreshSimpleRevertToLocalFromLocalFile(c *C) {
	// the "some-snap" in rev1
	snapYaml := "name: some-snap\nversion: 1.0"
	revStr := "1"

	// pretend we got a temp snap file from e.g. the snapd daemon
	tmpSnapFile := makeTestSnap(c, snapYaml)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// and some-snap at both rev1, rev2 are installed
	info := snaptest.MockSnapWithFiles(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)}, nil)
	snaptest.MockSnapWithFiles(c, snapYaml, &snap.SideInfo{Revision: snap.R(2)}, nil)
	si1 := &snap.SideInfo{
		RealName: info.SnapName(),
		SnapID:   fakeSnapID(info.SnapName()),
		Revision: snap.R(1),
	}
	si2 := &snap.SideInfo{
		RealName: info.SnapName(),
		SnapID:   fakeSnapID(info.SnapName()),
		Revision: snap.R(2),
	}
	snapstate.Set(st, info.InstanceName(), &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1, si2}),
		Current:  snap.R(2),
		SnapType: string(info.Type()),
	})

	// now refresh from rev2 to rev1
	flags := snapstate.Flags{RemoveSnapPath: true}
	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "some-snap", Revision: snap.R(revStr)}, tmpSnapFile, "", "", flags, nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("refresh", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	// the temp file got cleaned up
	snapsup, err := snapstate.TaskSnapSetup(chg.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.RemoveSnapPath, Equals, true)
	c.Check(snapsup.SnapPath, testutil.FileAbsent)

	// the snap file is in the right place
	c.Check(info.MountFile(), testutil.FilePresent)

	var snapst snapstate.SnapState
	snapstate.Get(st, "some-snap", &snapst)
	info, err = snapst.CurrentInfo()
	c.Assert(err, IsNil)
	c.Assert(info.Revision, Equals, snap.R(1))
}

type kernelSuite struct {
	baseMgrsSuite

	bloader *boottest.Bootenv16
}

var _ = Suite(&kernelSuite{})

func (s *kernelSuite) SetUpTest(c *C) {
	// We check for OnClassic when creating the manager. But we need
	// to add the clean-up after the call BaseTest.SetUpTest() to avoid
	// it panicking.
	restore := release.MockOnClassic(false)
	s.baseMgrsSuite.SetUpTest(c)
	s.AddCleanup(restore)

	s.bloader = boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	s.bloader.SetBootKernel("pc-kernel_1.snap")
	s.bloader.SetBootBase("core_1.snap")
	bootloader.Force(s.bloader)
	s.AddCleanup(func() { bootloader.Force(nil) })

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
	s.makeSerialAssertionInState(c, st, "can0nical", "my-model", "serialserialserial")

	// make a mock "pc-kernel" kernel
	si := &snap.SideInfo{RealName: "pc-kernel", SnapID: fakeSnapID("pc-kernel"), Revision: snap.R(1)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "kernel",
	})
	snaptest.MockSnapWithFiles(c, "name: pc-kernel\ntype: kernel\nversion: 1.0", si, nil)

	// make a mock "pc" gadget
	si2 := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	gadgetSnapYaml := "name: pc\nversion: 1.0\ntype: gadget"
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
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

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// system waits for a restart because of the new kernel
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoStatus)

	// simulate successful restart happened
	s.mockSuccessfulReboot(c, chg, s.bloader, []snap.Type{snap.TypeKernel})

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
	i += validateRefreshTasks(c, tasks[i:], "pc-kernel", "2", isKernel)
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

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// system waits for a restart because of the new kernel
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoStatus)

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
	ms.mockSuccessfulReboot(c, chg, ms.bloader, []snap.Type{snap.TypeKernel})
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
	i += validateInstallTasks(c, tasks[i:], "brand-kernel", "2", isKernel)
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

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// system waits for a restart because of the new kernel
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoStatus)

	// simulate successful restart happened
	ms.mockSuccessfulReboot(c, chg, ms.bloader, []snap.Type{snap.TypeKernel})

	// continue
	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// we are in restarting state
	restarting, restartType := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Check(restartType, Equals, restart.RestartSystem)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// Restart and re-run to completion
	ms.mockRestartAndSettle(c, st, chg)
	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// and the undo gave us our old kernel back
	c.Assert(ms.bloader.BootVars, DeepEquals, map[string]string{
		"snap_core":       "core_1.snap",
		"snap_try_core":   "",
		"snap_try_kernel": "",
		"snap_kernel":     "pc-kernel_1.snap",
		"snap_mode":       boot.DefaultStatus,
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

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// system waits for a restart because of the new kernel
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoStatus)

	// simulate rollback of the kernel during reboot
	ms.mockRollbackAcrossReboot(c, chg, ms.bloader, []snap.Type{snap.TypeKernel})

	// continue
	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// the change was not successful
	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// and we are *not* in restarting state
	restarting, _ := restart.Pending(st)
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

func (s *mgrsSuiteCore) TestRemodelStoreSwitch(c *C) {
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

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

	snapstatetest.InstallEssentialSnaps(c, st, "core", nil, bloader)

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

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
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

func (s *mgrsSuiteCore) TestRemodelSwitchGadgetTrack(c *C) {
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

	snapstatetest.InstallSnap(c, st, "name: pc-kernel\nversion: 1\ntype: kernel\n", nil, &snap.SideInfo{
		SnapID:   fakeSnapID("pc-kernel"),
		Revision: snap.R(1),
		RealName: "pc-kernel",
	}, snapstatetest.InstallSnapOptions{Required: true})

	snapstatetest.InstallSnap(c, st, "name: core\nversion: 1\ntype: base\n", nil, &snap.SideInfo{
		SnapID:   fakeSnapID("core"),
		Revision: snap.R(1),
		RealName: "core",
	}, snapstatetest.InstallSnapOptions{Required: true})

	err := bloader.SetBootVars(map[string]string{
		"snap_mode":   boot.DefaultStatus,
		"snap_core":   "core_1.snap",
		"snap_kernel": "pc-kernel_1.snap",
	})
	c.Assert(err, IsNil)

	si := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "gadget",
	})
	gadgetSnapYaml := "name: pc\nversion: 2.0\ntype: gadget"
	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
        structure:
          - name: mbr
            type: mbr
            size: 440
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
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)
	s.makeSerialAssertionInState(c, st, "can0nical", "my-model", "serialserialserial")

	// create a new model
	newModel := s.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"gadget":   "pc=18",
		"revision": "1",
	})

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{"volume-id": {0: {}}},
			map[string]map[int]*gadget.OnDiskStructure{
				"volume-id": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["volume-id"]),
			}, nil
	})
	defer r()

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
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

type mockUpdater struct {
	updateCalls int
	onUpdate    error
}

func (m *mockUpdater) Backup() error { return nil }

func (m *mockUpdater) Rollback() error { return nil }

func (m *mockUpdater) Update() error {
	m.updateCalls++
	return m.onUpdate
}

func (s *mgrsSuiteCore) TestRemodelSwitchToDifferentGadget(c *C) {
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	err := bloader.SetBootVars(map[string]string{
		"snap_mode":   boot.DefaultStatus,
		"snap_core":   "core18_1.snap",
		"snap_kernel": "pc-kernel_1.snap",
	})
	c.Check(err, IsNil)

	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	snapstatetest.InstallSnap(c, st, "name: pc-kernel\nversion: 1\ntype: kernel\n", nil, &snap.SideInfo{
		SnapID:   fakeSnapID("pc-kernel"),
		Revision: snap.R(1),
		RealName: "pc-kernel",
	}, snapstatetest.InstallSnapOptions{Required: true})

	si := &snap.SideInfo{RealName: "core18", SnapID: fakeSnapID("core18"), Revision: snap.R(1)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "base",
	})
	si2 := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	gadgetSnapYaml := "name: pc\nversion: 1.0\ntype: gadget"
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
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

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"volume-id": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
				},
			},
			map[string]map[int]*gadget.OnDiskStructure{
				"volume-id": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["volume-id"]),
			},
			nil
	})
	defer r()

	updaterForStructureCalls := 0
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, rootDir, rollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		updaterForStructureCalls++
		c.Assert(ps.Name(), Equals, "foo")
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
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)
	s.makeSerialAssertionInState(c, st, "can0nical", "my-model", "serialserialserial")

	// create a new model
	newModel := s.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"gadget":   "other-pc=18",
		"revision": "1",
	})

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// gadget updater was set up
	c.Check(updaterForStructureCalls, Equals, 1)

	// gadget update requests a restart
	restarting, _ := restart.Pending(st)
	c.Check(restarting, Equals, true)

	// simulate successful restart happened
	s.mockRestartAndSettle(c, st, chg)

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

func (s *mgrsSuiteCore) TestRemodelSwitchToIncompatibleGadget(c *C) {
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	err := bloader.SetBootVars(map[string]string{
		"snap_mode":   boot.DefaultStatus,
		"snap_core":   "core_1.snap",
		"snap_kernel": "pc-kernel_1.snap",
	})
	c.Check(err, IsNil)

	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	snapstatetest.InstallSnap(c, st, "name: pc-kernel\nversion: 1\ntype: kernel\n", nil, &snap.SideInfo{
		SnapID:   fakeSnapID("pc-kernel"),
		Revision: snap.R(1),
		RealName: "pc-kernel",
	}, snapstatetest.InstallSnapOptions{Required: true})

	si := &snap.SideInfo{RealName: "core18", SnapID: fakeSnapID("core18"), Revision: snap.R(1)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "base",
	})
	si2 := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	gadgetSnapYaml := "name: pc\nversion: 1.0\ntype: gadget"
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
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
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)
	s.makeSerialAssertionInState(c, st, "can0nical", "my-model", "serialserialserial")

	// create a new model
	newModel := s.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"gadget":   "other-pc=18",
		"revision": "1",
	})

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*cannot remodel to an incompatible gadget: .*new valid structure size range.*is not compatible with current.*`)
}

func (s *mgrsSuiteCore) TestHappyDeviceRegistrationWithPrepareDeviceHook(c *C) {
	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

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

	mockServer, extraCerts := devicestatetest.MockDeviceService(c, bhv)
	defer mockServer.Close()
	fname := filepath.Join(dirs.SnapdStoreSSLCertsDir, "test-server-certs.pem")
	err = os.MkdirAll(filepath.Dir(fname), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(fname, extraCerts, 0644)
	c.Assert(err, IsNil)

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

func (s *mgrsSuiteCore) TestRemodelReregistration(c *C) {
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

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

	snapstatetest.InstallEssentialSnaps(c, st, "core", nil, bloader)

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

	mockDeviceService, extraCerts := devicestatetest.MockDeviceService(c, bhv)
	defer mockDeviceService.Close()
	fname := filepath.Join(dirs.SnapdStoreSSLCertsDir, "test-server-certs.pem")
	err = os.MkdirAll(filepath.Dir(fname), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(fname, extraCerts, 0644)
	c.Assert(err, IsNil)

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

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
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

const pcGadgetSnapYaml = `
version: 1.0
name: pc
type: gadget
base: core20
`

const pcGadget22SnapYaml = `
version: 1.0
name: pc
type: gadget
base: core22
`

const oldPcGadgetSnapYaml = `
version: 1.0
name: old-pc
type: gadget
base: core20
`

const pcKernelSnapYaml = `
version: 1.0
name: pc-kernel
type: kernel
`

const pcKernel22SnapYaml = `
version: 1.0
name: pc-kernel
type: kernel
base: core22
`

const core20SnapYaml = `
version: 1.0
name: core20
type: base
`

const core22SnapYaml = `
version: 1.0
name: core22
type: base
`

const snapdSnapYaml = `
version: 1.0
name: snapd
type: snapd
`

const oldPcGadgetYamlForRemodel = `
volumes:
  pc:
    schema: gpt
    bootloader: grub
    structure:
      - name: ubuntu-seed
        filesystem: vfat
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        role: system-seed
        size: 100M
        content:
          - source: grubx64.efi
            target: grubx64.efi
      - name: ubuntu-boot
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        role: system-boot
        filesystem: ext4
        size: 100M
      - name: ubuntu-data
        role: system-data
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        filesystem: ext4
        size: 500M
`

const grubBootConfig = "# Snapd-Boot-Config-Edition: 1\n"

var (
	pcGadgetFiles = [][]string{
		{"meta/gadget.yaml", pcGadgetYaml},
		{"grub.conf", ""},
		{"grub.conf", ""},
		// SHA3-384: 21e42a075b0d7bb6177c0eb3b3a1c8c6de6d4b4f902759eae5555e9cf3bebd21277a27102fd5426da989bde96c0cf848
		{"bootx64.efi", "content"},
		{"grubx64.efi", "content"},
	}
	pcTrack22GadgetFiles = [][]string{
		{"meta/gadget.yaml", pcGadgetTrack22Yaml},
		{"grub.conf", ""},
		{"grub.conf", ""},
		// SHA3-384: 21e42a075b0d7bb6177c0eb3b3a1c8c6de6d4b4f902759eae5555e9cf3bebd21277a27102fd5426da989bde96c0cf848
		{"bootx64.efi", "content"},
		{"grubx64.efi", "content"},
	}
	oldPcGadgetFiles = append(pcGadgetFiles, [][]string{
		{"meta/gadget.yaml", oldPcGadgetYamlForRemodel},
		// SHA3-384: 7e5c973da86f7398deffd45b9225175da1dd6ae8fcffa1a20219b32bab9f4846da10e823736cd818ceada74d35337c98
		{"grubx64.efi", "old-gadget-content"},
		{"cmdline.extra", "foo bar baz"},
	}...)
	pcKernelFiles = [][]string{
		{"kernel.efi", "kernel-efi"},
	}
	pcKernel22Files = [][]string{
		{"kernel.efi", "kernel-efi"},
	}
	snapYamlsForRemodel = map[string]string{
		"old-pc":    oldPcGadgetSnapYaml,
		"pc":        pcGadgetSnapYaml,
		"pc-kernel": pcKernelSnapYaml,
		"core20":    core20SnapYaml,
		"core22":    core22SnapYaml,
		"snapd":     snapdSnapYaml,
		"baz":       "version: 1.0\nname: baz\nbase: core20",
	}
	snapFilesForRemodel = map[string][][]string{
		"old-pc": oldPcGadgetFiles,
		"pc":     pcGadgetFiles,
		// use a different fileset, such that the pc snap with this
		// content will have a different digest than the regular pc snap
		"pc-rev-33": append(pcGadgetFiles, []string{
			"this-is-new", "new-in-pc-rev-33",
		}),
		"pc-kernel": pcKernelFiles,
		// similar reasoning as for the pc snap
		"pc-kernel-rev-33": append(pcKernelFiles, []string{
			"this-is-new", "new-in-pc-kernel-rev-33",
		}),
		// and again
		"core20-rev-33": {
			{"this-is-new", "new-in-core20-rev-33"},
		},
		"pc-kernel-track-22": pcKernel22Files,
		"pc-track-22": append(pcTrack22GadgetFiles, []string{
			"cmdline.extra", "uc22",
		}),
	}

	// headers of a regular UC20 model assertion
	uc20ModelDefaults = map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              fakeSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              fakeSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	}
)

func (s *mgrsSuiteCore) makeInstalledSnapInStateForRemodel(c *C, name string, rev snap.Revision, channel string) *snap.Info {
	si := &snap.SideInfo{
		RealName: name,
		SnapID:   fakeSnapID(name),
		Revision: rev,
	}
	snapInfo := snaptest.MakeSnapFileAndDir(c, snapYamlsForRemodel[name],
		snapFilesForRemodel[name], si)
	snapstate.Set(s.o.State(), name, &snapstate.SnapState{
		SnapType:        string(snapInfo.Type()),
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:         si.Revision,
		TrackingChannel: channel,
	})
	sha3_384, size, err := asserts.SnapFileSHA3_384(snapInfo.MountFile())
	c.Assert(err, IsNil)

	snapRev := s.makeStoreSnapRevision(c, name, rev.String(), sha3_384, size)
	err = assertstate.Add(s.o.State(), snapRev)
	c.Assert(err, IsNil)
	return snapInfo
}

func (s *mgrsSuiteCore) testRemodelUC20WithRecoverySystem(c *C, encrypted bool) {
	restore := release.MockOnClassic(false)
	defer restore()

	// mock directories that need to be tweaked by the test
	c.Assert(os.MkdirAll(boot.InitramfsUbuntuSeedDir, 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Base(dirs.SnapSeedDir), 0755), IsNil)
	// this is a bind mount in a real system
	c.Assert(os.Symlink(boot.InitramfsUbuntuSeedDir, dirs.SnapSeedDir), IsNil)

	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "proc"), 0755), IsNil)
	restore = kcmdline.MockProcCmdline(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"))
	defer restore()

	// mock state related to boot assets
	for _, env := range []string{
		filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/ubuntu/grub.cfg"),
		filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/ubuntu/grubenv"),
		filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/grubx64.efi"),
		filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/bootx64.efi"),
		filepath.Join(boot.InitramfsUbuntuBootDir, "EFI/ubuntu/grub.cfg"),
		filepath.Join(boot.InitramfsUbuntuBootDir, "EFI/ubuntu/grubenv"),
		filepath.Join(boot.InitramfsUbuntuBootDir, "EFI/boot/grubx64.efi"),
		filepath.Join(dirs.GlobalRootDir, "/boot/grub/grub.cfg"),
		filepath.Join(dirs.GlobalRootDir, "/boot/grub/grubenv"),
	} {
		c.Assert(os.MkdirAll(filepath.Dir(env), 0755), IsNil)
		switch filepath.Base(env) {
		case "grubenv":
			e := grubenv.NewEnv(env)
			c.Assert(e.Save(), IsNil)
		case "grub.cfg":
			c.Assert(os.WriteFile(env, []byte(grubBootConfig), 0644), IsNil)
		case "grubx64.efi", "bootx64.efi":
			c.Assert(os.WriteFile(env, []byte("content"), 0644), IsNil)
		default:
			c.Assert(os.WriteFile(env, nil, 0644), IsNil)
		}
	}

	if encrypted {
		// boot assets are measured on an encrypted system
		assetsCacheDir := filepath.Join(dirs.SnapBootAssetsDirUnder(dirs.GlobalRootDir), "grub")
		c.Assert(os.MkdirAll(assetsCacheDir, 0755), IsNil)
		for _, cachedAsset := range []string{
			"grubx64.efi-21e42a075b0d7bb6177c0eb3b3a1c8c6de6d4b4f902759eae5555e9cf3bebd21277a27102fd5426da989bde96c0cf848",
			"bootx64.efi-21e42a075b0d7bb6177c0eb3b3a1c8c6de6d4b4f902759eae5555e9cf3bebd21277a27102fd5426da989bde96c0cf848",
		} {
			err := os.WriteFile(filepath.Join(assetsCacheDir, cachedAsset), []byte("content"), 0644)
			c.Assert(err, IsNil)
		}
	}

	// state of booted kernel
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "boot/grub/pc-kernel_2.snap"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/boot/grub/pc-kernel_2.snap/kernel.efi"),
		[]byte("kernel-efi"), 0755), IsNil)
	c.Assert(os.Symlink("pc-kernel_2.snap/kernel.efi", filepath.Join(dirs.GlobalRootDir, "boot/grub/kernel.efi")), IsNil)

	if encrypted {
		stamp := filepath.Join(dirs.SnapFDEDir, "sealed-keys")
		c.Assert(os.MkdirAll(filepath.Dir(stamp), 0755), IsNil)
		c.Assert(os.WriteFile(stamp, nil, 0644), IsNil)
	}

	// new snaps from the store
	for _, name := range []string{"foo", "bar"} {
		s.prereqSnapAssertions(c, map[string]interface{}{
			"snap-name": name,
		})
		snapPath, _ := s.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 1.0, base: core20}", name), "1")
		s.serveSnap(snapPath, "1")
	}

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// already installed snaps that do not have their snap delarations added
	// in set up and need a new revision in the store
	for _, name := range []string{"baz"} {
		decl := s.prereqSnapAssertions(c, map[string]interface{}{
			"snap-name":    name,
			"publisher-id": "can0nical",
		})
		c.Assert(assertstate.Add(st, decl), IsNil)
		snapPath, _ := s.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 1.0, base: core20}", name), "22")
		s.serveSnap(snapPath, "22")
	}

	err := assertstate.Add(st, s.devAcct)
	c.Assert(err, IsNil)
	// snaps in state
	pcInfo := s.makeInstalledSnapInStateForRemodel(c, "pc", snap.R(1), "20/stable")
	pcKernelInfo := s.makeInstalledSnapInStateForRemodel(c, "pc-kernel", snap.R(2), "20/stable")
	coreInfo := s.makeInstalledSnapInStateForRemodel(c, "core20", snap.R(3), "latest/stable")
	snapdInfo := s.makeInstalledSnapInStateForRemodel(c, "snapd", snap.R(4), "latest/stable")
	bazInfo := s.makeInstalledSnapInStateForRemodel(c, "baz", snap.R(21), "4.14/stable")

	// state of the current model
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755), IsNil)

	model := s.brands.Model("can0nical", "my-model", uc20ModelDefaults)

	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)
	s.makeSerialAssertionInState(c, st, "can0nical", "my-model", "serialserialserial")

	// now create a minimal uc20 seed dir with snaps/assertions
	seed20 := &seedtest.TestingSeed20{
		SeedSnaps: seedtest.SeedSnaps{
			StoreSigning: s.storeSigning,
			Brands:       s.brands,
		},
		SeedDir: dirs.SnapSeedDir,
	}
	restore = seed.MockTrusted(s.storeSigning.Trusted)
	defer restore()

	seed20.MakeSeedWithModel(c, "1234", model, []*seedwriter.OptionsSnap{
		{Path: pcInfo.MountFile()},
		{Path: pcKernelInfo.MountFile()},
		{Path: coreInfo.MountFile()},
		{Path: snapdInfo.MountFile()},
		{Path: bazInfo.MountFile()},
	})

	// create a new model
	newModel := s.brands.Model("can0nical", "my-model", uc20ModelDefaults, map[string]interface{}{
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              fakeSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              fakeSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":     "foo",
				"presence": "required",
			},
			map[string]interface{}{
				"name":     "bar",
				"presence": "required",
			},
			map[string]interface{}{
				"name":     "baz",
				"presence": "required",
				// use a different default channel
				"default-channel": "4.15/edge",
			}},
		"revision": "1",
	})

	// mock the modeenv file
	m := &boot.Modeenv{
		Mode:                             "run",
		Base:                             "core20_3.snap",
		CurrentKernels:                   []string{"pc-kernel_2.snap"},
		CurrentRecoverySystems:           []string{"1234"},
		GoodRecoverySystems:              []string{"1234"},
		CurrentKernelCommandLines:        []string{"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1"},
		CurrentTrustedRecoveryBootAssets: nil,
		CurrentTrustedBootAssets:         nil,

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	if encrypted {
		m.CurrentTrustedRecoveryBootAssets = map[string][]string{
			// see gadget content
			"grubx64.efi": {"21e42a075b0d7bb6177c0eb3b3a1c8c6de6d4b4f902759eae5555e9cf3bebd21277a27102fd5426da989bde96c0cf848"},
			"bootx64.efi": {"21e42a075b0d7bb6177c0eb3b3a1c8c6de6d4b4f902759eae5555e9cf3bebd21277a27102fd5426da989bde96c0cf848"},
		}
		m.CurrentTrustedBootAssets = map[string][]string{
			"grubx64.efi": {"21e42a075b0d7bb6177c0eb3b3a1c8c6de6d4b4f902759eae5555e9cf3bebd21277a27102fd5426da989bde96c0cf848"},
		}
	}

	// make sure cmdline matches what we expect in the modeenv
	c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"),
		[]byte("snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1"), 0644),
		IsNil)

	err = m.WriteTo("")
	c.Assert(err, IsNil)
	c.Assert(s.o.DeviceManager().ReloadModeenv(), IsNil)

	bl, err := bootloader.Find(boot.InitramfsUbuntuSeedDir, &bootloader.Options{Role: bootloader.RoleRecovery})
	c.Assert(err, IsNil)

	err = bl.SetBootVars(map[string]string{
		"snap_kernel":                 "pc-kernel_2.snap",
		"snapd_good_recovery_systems": "1234",
	})
	c.Assert(err, IsNil)

	secbootResealCalls := 0
	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		secbootResealCalls++
		if !encrypted {
			return fmt.Errorf("unexpected call")
		}
		return nil
	})
	defer restore()

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)

	c.Check(devicestate.RemodelingChange(st), NotNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))

	dumpTasks(c, "after setteling", chg.Tasks())

	c.Check(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	c.Check(devicestate.RemodelingChange(st), NotNil)
	restarting, kind := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystemNow)

	now := time.Now()
	expectedLabel := now.Format("20060102")

	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentRecoverySystems, DeepEquals, []string{"1234", expectedLabel})
	c.Check(m.GoodRecoverySystems, DeepEquals, []string{"1234"})

	vars, err := bl.GetBootVars("try_recovery_system", "recovery_system_status", "snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "try",
		// nothing has been added to good recovery systems for the
		// bootloader
		"snapd_good_recovery_systems": "1234",
	})

	// the new required-snap "foo" is not installed yet
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

	// simulate successful reboot to recovery and back
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// this would be done by snap-bootstrap in initramfs
	err = bl.SetBootVars(map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "tried",
	})
	c.Assert(err, IsNil)

	// reset, so that after-reboot handling of tried system is executed
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("remodel change failed: %v", chg.Err()))
	// boot variables for probing recovery system are cleared, new system is
	// added as recovery capable one
	vars, err = bl.GetBootVars("try_recovery_system", "recovery_system_status",
		"snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"try_recovery_system":         "",
		"recovery_system_status":      "",
		"snapd_good_recovery_systems": "1234," + expectedLabel,
	})

	for _, name := range []string{"core20", "pc-kernel", "pc", "snapd", "foo", "bar", "baz"} {
		c.Logf("name: %v", name)
		var snapst snapstate.SnapState
		err = snapstate.Get(st, name, &snapst)
		c.Assert(err, IsNil)
		switch name {
		case "baz":
			// the new tracking channel is applied by snapd
			c.Check(snapst.TrackingChannel, Equals, "4.15/edge")
		case "pc", "pc-kernel":
			c.Check(snapst.TrackingChannel, Equals, "20/stable")
		default:
			c.Check(snapst.TrackingChannel, Equals, "latest/stable")
		}
	}

	// ensure sorting is correct
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	c.Logf("tasks: %v", len(tasks))
	for _, tsk := range tasks {
		c.Logf("  %3s %v", tsk.ID(), tsk.Summary())
	}

	var i int
	// first all downloads/checks in sequential order
	for _, name := range []string{"foo", "bar", "baz"} {
		expectedChannel := "latest/stable"
		expectedRev := "1"
		if name == "baz" {
			expectedChannel = "4.15/edge"
			expectedRev = "22"
		}
		i += validateDownloadCheckTasks(c, tasks[i:], name, expectedRev, expectedChannel)
	}
	// then create recovery
	i += validateRecoverySystemTasks(c, tasks[i:], expectedLabel)
	// then all installs in sequential order
	for _, name := range []string{"foo", "bar"} {
		i += validateInstallTasks(c, tasks[i:], name, "1", 0)
	}
	// and snaps with changed channel get refreshed
	i += validateRefreshTasks(c, tasks[i:], "baz", "22", 0)

	// ensure that we only have the tasks we checked (plus the one
	// extra "set-model" task)
	c.Assert(tasks, HasLen, i+1)

	const usesSnapd = true
	sd := seedtest.ValidateSeed(c, boot.InitramfsUbuntuSeedDir, expectedLabel, usesSnapd, s.storeSigning.Trusted)
	c.Assert(sd.Model(), DeepEquals, newModel)

	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentRecoverySystems, DeepEquals, []string{"1234", expectedLabel})
	c.Check(m.GoodRecoverySystems, DeepEquals, []string{"1234", expectedLabel})
	if encrypted {
		// boot assets are measured and tracked in modeenv in an encrypted system
		c.Check(m, testutil.JsonEquals, &boot.Modeenv{
			Mode:                      "run",
			Base:                      "core20_3.snap",
			CurrentKernels:            []string{"pc-kernel_2.snap"},
			CurrentRecoverySystems:    []string{"1234", expectedLabel},
			GoodRecoverySystems:       []string{"1234", expectedLabel},
			CurrentKernelCommandLines: []string{"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1"},
			CurrentTrustedRecoveryBootAssets: map[string][]string{
				"grubx64.efi": {"21e42a075b0d7bb6177c0eb3b3a1c8c6de6d4b4f902759eae5555e9cf3bebd21277a27102fd5426da989bde96c0cf848"},
				"bootx64.efi": {"21e42a075b0d7bb6177c0eb3b3a1c8c6de6d4b4f902759eae5555e9cf3bebd21277a27102fd5426da989bde96c0cf848"},
			},
			CurrentTrustedBootAssets: map[string][]string{
				"grubx64.efi": {"21e42a075b0d7bb6177c0eb3b3a1c8c6de6d4b4f902759eae5555e9cf3bebd21277a27102fd5426da989bde96c0cf848"},
			},

			Model:          newModel.Model(),
			BrandID:        newModel.BrandID(),
			Grade:          string(newModel.Grade()),
			ModelSignKeyID: newModel.SignKeyID(),
		})
	} else {
		c.Check(m, testutil.JsonEquals, &boot.Modeenv{
			Mode:                      "run",
			Base:                      "core20_3.snap",
			CurrentKernels:            []string{"pc-kernel_2.snap"},
			CurrentRecoverySystems:    []string{"1234", expectedLabel},
			GoodRecoverySystems:       []string{"1234", expectedLabel},
			CurrentKernelCommandLines: []string{"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1"},

			Model:          newModel.Model(),
			BrandID:        newModel.BrandID(),
			Grade:          string(newModel.Grade()),
			ModelSignKeyID: newModel.SignKeyID(),
		})
	}

	// new model has been written to ubuntu-boot/device/model
	var modelBytes bytes.Buffer
	c.Assert(asserts.NewEncoder(&modelBytes).Encode(newModel), IsNil)
	c.Assert(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileEquals, modelBytes.String())
	if encrypted {
		// keys were resealed
		c.Assert(secbootResealCalls, Not(Equals), 0)
	} else {
		c.Assert(secbootResealCalls, Equals, 0)
	}

	var seededSystems []map[string]interface{}
	err = st.Get("seeded-systems", &seededSystems)
	c.Assert(err, IsNil)
	c.Assert(seededSystems, HasLen, 1)
	// since we can't mock the seed timestamp, make sure it's within a
	// reasonable range, and then clear it
	c.Assert(seededSystems[0]["seed-time"], FitsTypeOf, "")
	ts, err := time.Parse(time.RFC3339Nano, seededSystems[0]["seed-time"].(string))
	c.Assert(err, IsNil)
	// should be more than enough for the test to finish
	c.Check(ts.Before(now.Add(10*time.Minute)), Equals, true, Commentf("seed-time is too late: %v", ts))
	seededSystems[0]["seed-time"] = ""
	c.Check(seededSystems, DeepEquals, []map[string]interface{}{
		{
			"system":    expectedLabel,
			"model":     newModel.Model(),
			"brand-id":  newModel.BrandID(),
			"revision":  float64(newModel.Revision()),
			"timestamp": newModel.Timestamp().Format(time.RFC3339Nano),
			// cleared earlier
			"seed-time": "",
		},
	})
}

func (s *mgrsSuiteCore) TestRemodelUC20WithRecoverySystemEncrypted(c *C) {
	const encrypted bool = true
	s.testRemodelUC20WithRecoverySystem(c, encrypted)
}

func (s *mgrsSuiteCore) TestRemodelUC20WithRecoverySystemUnencrypted(c *C) {
	const encrypted bool = false
	s.testRemodelUC20WithRecoverySystem(c, encrypted)
}

func (s *mgrsSuiteCore) testRemodelUC20WithRecoverySystemSimpleSetUp(c *C, modelExtras ...map[string]interface{}) {
	restore := release.MockOnClassic(false)
	s.AddCleanup(restore)

	// mock directories that need to be tweaked by the test
	c.Assert(os.MkdirAll(boot.InitramfsUbuntuSeedDir, 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Base(dirs.SnapSeedDir), 0755), IsNil)
	// this is a bind mount in a real system
	c.Assert(os.Symlink(boot.InitramfsUbuntuSeedDir, dirs.SnapSeedDir), IsNil)

	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "proc"), 0755), IsNil)
	restore = kcmdline.MockProcCmdline(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"))
	s.AddCleanup(restore)

	// mock state related to boot assets
	for _, env := range []string{
		filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/ubuntu/grub.cfg"),
		filepath.Join(boot.InitramfsUbuntuBootDir, "EFI/ubuntu/grub.cfg"),
		filepath.Join(dirs.GlobalRootDir, "/boot/grub/grub.cfg"),
		filepath.Join(dirs.GlobalRootDir, "/boot/grub/grubenv"),
	} {
		c.Assert(os.MkdirAll(filepath.Dir(env), 0755), IsNil)
		switch filepath.Base(env) {
		case "grubenv":
			e := grubenv.NewEnv(env)
			c.Assert(e.Save(), IsNil)
		case "grub.cfg":
			c.Assert(os.WriteFile(env, []byte(grubBootConfig), 0644), IsNil)
		default:
			c.Fatalf("unexpected file %v", env)
		}
	}

	// state of booted kernel
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "boot/grub/pc-kernel_2.snap"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/boot/grub/pc-kernel_2.snap/kernel.efi"),
		[]byte("kernel-efi"), 0755), IsNil)
	c.Assert(os.Symlink("pc-kernel_2.snap/kernel.efi", filepath.Join(dirs.GlobalRootDir, "boot/grub/kernel.efi")), IsNil)

	mockServer := s.mockStore(c)
	s.AddCleanup(mockServer.Close)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	err := assertstate.Add(st, s.devAcct)
	c.Assert(err, IsNil)
	// snaps in state
	pcInfo := s.makeInstalledSnapInStateForRemodel(c, "pc", snap.R(1), "20/stable")
	pcKernelInfo := s.makeInstalledSnapInStateForRemodel(c, "pc-kernel", snap.R(2), "20/stable")
	coreInfo := s.makeInstalledSnapInStateForRemodel(c, "core20", snap.R(3), "latest/stable")
	snapdInfo := s.makeInstalledSnapInStateForRemodel(c, "snapd", snap.R(4), "latest/stable")

	// state of the current model
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755), IsNil)

	modelArgs := []map[string]interface{}{uc20ModelDefaults}
	modelArgs = append(modelArgs, modelExtras...)

	model := s.brands.Model("can0nical", "my-model", modelArgs...)

	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)
	s.makeSerialAssertionInState(c, st, "can0nical", "my-model", "serialserialserial")

	// now create a minimal uc20 seed dir with snaps/assertions
	seed20 := &seedtest.TestingSeed20{
		SeedSnaps: seedtest.SeedSnaps{
			StoreSigning: s.storeSigning,
			Brands:       s.brands,
		},
		SeedDir: dirs.SnapSeedDir,
	}
	restore = seed.MockTrusted(s.storeSigning.Trusted)
	defer restore()

	seed20.MakeSeedWithModel(c, "1234", model, []*seedwriter.OptionsSnap{
		{Path: pcInfo.MountFile()},
		{Path: pcKernelInfo.MountFile()},
		{Path: coreInfo.MountFile()},
		{Path: snapdInfo.MountFile()},
	})

	// mock the modeenv file
	m := &boot.Modeenv{
		Mode:                             "run",
		Base:                             "core20_3.snap",
		CurrentKernels:                   []string{"pc-kernel_2.snap"},
		CurrentRecoverySystems:           []string{"1234"},
		GoodRecoverySystems:              []string{"1234"},
		CurrentKernelCommandLines:        []string{"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1"},
		CurrentTrustedRecoveryBootAssets: nil,
		CurrentTrustedBootAssets:         nil,

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	// make sure cmdline matches what we expect in the modeenv
	c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"),
		[]byte("snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1"), 0644),
		IsNil)

	err = m.WriteTo("")
	c.Assert(err, IsNil)
	c.Assert(s.o.DeviceManager().ReloadModeenv(), IsNil)

	bl, err := bootloader.Find(boot.InitramfsUbuntuSeedDir, &bootloader.Options{Role: bootloader.RoleRecovery})
	c.Assert(err, IsNil)

	err = bl.SetBootVars(map[string]string{
		"snap_kernel": "pc-kernel_2.snap",
	})
	c.Assert(err, IsNil)

	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		return fmt.Errorf("unexpected call")
	})
	s.AddCleanup(restore)
}

func (s *mgrsSuiteCore) TestRemodelUC20DifferentKernelChannel(c *C) {
	s.testRemodelUC20WithRecoverySystemSimpleSetUp(c)
	// use a different set of files, such that the snap digest must also be different
	snapPath, _ := s.makeStoreTestSnapWithFiles(c, snapYamlsForRemodel["pc-kernel"], "33", snapFilesForRemodel["pc-kernel-rev-33"])
	s.serveSnap(snapPath, "33")
	newModel := s.brands.Model("can0nical", "my-model", uc20ModelDefaults, map[string]interface{}{
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              fakeSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "21/stable",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              fakeSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
		"revision": "1",
	})

	bl, err := bootloader.Find(boot.InitramfsUbuntuSeedDir, &bootloader.Options{Role: bootloader.RoleRecovery})
	c.Assert(err, IsNil)

	rbl, err := bootloader.Find(dirs.GlobalRootDir, &bootloader.Options{Role: bootloader.RoleRunMode})
	c.Assert(err, IsNil)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	now := time.Now()
	expectedLabel := now.Format("20060102")

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{"pc": {}},
			map[string]map[int]*gadget.OnDiskStructure{
				"pc": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
			}, nil
	})
	defer r()

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))

	// first comes a reboot to the new recovery system
	c.Check(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	c.Check(devicestate.RemodelingChange(st), NotNil)
	restarting, kind := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystemNow)
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentRecoverySystems, DeepEquals, []string{"1234", expectedLabel})
	c.Check(m.GoodRecoverySystems, DeepEquals, []string{"1234"})
	vars, err := bl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "try",
	})
	const usesSnapd = true
	sd := seedtest.ValidateSeed(c, boot.InitramfsUbuntuSeedDir, expectedLabel, usesSnapd, s.storeSigning.Trusted)
	// rev-33 ships a new file
	verifyModelEssentialSnapHasContent(c, sd, "pc-kernel", "this-is-new", "new-in-pc-kernel-rev-33")

	// simulate successful reboot to recovery and back
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// this would be done by snap-bootstrap in initramfs
	err = bl.SetBootVars(map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "tried",
	})
	c.Assert(err, IsNil)
	// reset, so that after-reboot handling of tried system is executed
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	// we're installing a new kernel, so another reboot
	restarting, kind = restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystem)
	c.Assert(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	// and we've rebooted
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// kernel has booted
	vars, err = rbl.GetBootVars("kernel_status")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"kernel_status": "try",
	})
	err = rbl.SetBootVars(map[string]string{
		"kernel_status": "trying",
	})
	c.Assert(err, IsNil)

	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("remodel change failed: %v", chg.Err()))

	var snapst snapstate.SnapState
	err = snapstate.Get(st, "pc-kernel", &snapst)
	c.Assert(err, IsNil)

	// and the kernel tracking channel has been updated
	c.Check(snapst.TrackingChannel, Equals, "21/stable")

	// ensure sorting is correct
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	var i int
	// first all downloads/checks in sequential order
	i += validateDownloadCheckTasks(c, tasks[i:], "pc-kernel", "33", "21/stable")
	// then create recovery
	i += validateRecoverySystemTasks(c, tasks[i:], expectedLabel)
	// then all installs in sequential order
	validateRefreshTasks(c, tasks[i:], "pc-kernel", "33", isKernel|hasModeenv)
}

func (s *mgrsSuiteCore) TestRemodelUC20DifferentGadgetChannel(c *C) {
	s.testRemodelUC20WithRecoverySystemSimpleSetUp(c)
	// use a different set of files, such that the snap digest must also be different
	snapPath, _ := s.makeStoreTestSnapWithFiles(c, snapYamlsForRemodel["pc"], "33", snapFilesForRemodel["pc-rev-33"])
	s.serveSnap(snapPath, "33")
	newModel := s.brands.Model("can0nical", "my-model", uc20ModelDefaults, map[string]interface{}{
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              fakeSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              fakeSnapID("pc"),
				"type":            "gadget",
				"default-channel": "21/edge",
			},
		},
		"revision": "1",
	})
	bl, err := bootloader.Find(boot.InitramfsUbuntuSeedDir, &bootloader.Options{Role: bootloader.RoleRecovery})
	c.Assert(err, IsNil)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	now := time.Now()
	expectedLabel := now.Format("20060102")

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"pc": {
					0: {
						RootMountPoint: "/foo-seed",
					},
					1: {
						RootMountPoint: "/foo-boot",
					},
					2: {
						RootMountPoint: "/foo-data",
					},
				},
			},
			map[string]map[int]*gadget.OnDiskStructure{
				"pc": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
			}, nil
	})
	defer r()

	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, rootDir, rollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		// use a mock updater which does nothing
		return &mockUpdater{
			onUpdate: gadget.ErrNoUpdate,
		}, nil
	})
	defer restore()

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))

	// first comes a reboot to the new recovery system
	c.Check(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	c.Check(devicestate.RemodelingChange(st), NotNil)
	restarting, kind := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystemNow)
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentRecoverySystems, DeepEquals, []string{"1234", expectedLabel})
	c.Check(m.GoodRecoverySystems, DeepEquals, []string{"1234"})
	vars, err := bl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "try",
	})
	const usesSnapd = true
	sd := seedtest.ValidateSeed(c, boot.InitramfsUbuntuSeedDir, expectedLabel, usesSnapd, s.storeSigning.Trusted)
	// rev-33 ships a new file
	verifyModelEssentialSnapHasContent(c, sd, "pc", "this-is-new", "new-in-pc-rev-33")

	// simulate successful reboot to recovery and back
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// this would be done by snap-bootstrap in initramfs
	err = bl.SetBootVars(map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "tried",
	})
	c.Assert(err, IsNil)
	// reset, so that after-reboot handling of tried system is executed
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	// no more reboots, the gadget assets have not changed

	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("remodel change failed: %v", chg.Err()))

	var snapst snapstate.SnapState
	err = snapstate.Get(st, "pc", &snapst)
	c.Assert(err, IsNil)
	// and the kernel tracking channel has been updated
	c.Check(snapst.TrackingChannel, Equals, "21/edge")

	// ensure sorting is correct
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	var i int
	// first all downloads/checks in sequential order
	i += validateDownloadCheckTasks(c, tasks[i:], "pc", "33", "21/edge")
	// then create recovery
	i += validateRecoverySystemTasks(c, tasks[i:], expectedLabel)
	// then all installs in sequential order
	validateRefreshTasks(c, tasks[i:], "pc", "33", isGadget)
}

func verifyModelEssentialSnapHasContent(c *C, sd seed.Seed, name string, file, content string) {
	for _, ms := range sd.EssentialSnaps() {
		c.Logf("mode snap %q %v", ms.SnapName(), ms.Path)
		if ms.SnapName() == name {
			sf, err := snapfile.Open(ms.Path)
			c.Assert(err, IsNil)
			d, err := sf.ReadFile(file)
			c.Assert(err, IsNil)
			c.Assert(string(d), Equals, content)
			return
		}
	}
	c.Errorf("expected file %q not found seed snap of name %q", file, name)
}

func (s *mgrsSuiteCore) TestRemodelUC20DifferentBaseChannel(c *C) {
	s.testRemodelUC20WithRecoverySystemSimpleSetUp(c)
	// use a different set of files, such that the snap digest must also be different
	snapPath, _ := s.makeStoreTestSnapWithFiles(c, snapYamlsForRemodel["core20"], "33", snapFilesForRemodel["core20-rev-33"])
	s.serveSnap(snapPath, "33")
	newModel := s.brands.Model("can0nical", "my-model", uc20ModelDefaults, map[string]interface{}{
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              fakeSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              fakeSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "core20",
				"id":              fakeSnapID("core20"),
				"type":            "base",
				"default-channel": "latest/edge",
			},
		},
		"revision": "1",
	})
	bl, err := bootloader.Find(boot.InitramfsUbuntuSeedDir, &bootloader.Options{Role: bootloader.RoleRecovery})
	c.Assert(err, IsNil)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	now := time.Now()
	expectedLabel := now.Format("20060102")

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))

	// first comes a reboot to the new recovery system
	c.Check(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	c.Check(devicestate.RemodelingChange(st), NotNil)
	restarting, kind := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystemNow)
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentRecoverySystems, DeepEquals, []string{"1234", expectedLabel})
	c.Check(m.GoodRecoverySystems, DeepEquals, []string{"1234"})
	vars, err := bl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "try",
	})
	const usesSnapd = true
	sd := seedtest.ValidateSeed(c, boot.InitramfsUbuntuSeedDir, expectedLabel, usesSnapd, s.storeSigning.Trusted)
	// rev-33 ships a new file
	verifyModelEssentialSnapHasContent(c, sd, "core20", "this-is-new", "new-in-core20-rev-33")

	// simulate successful reboot to recovery and back
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// this would be done by snap-bootstrap in initramfs
	err = bl.SetBootVars(map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "tried",
	})
	c.Assert(err, IsNil)
	// reset, so that after-reboot handling of tried system is executed
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	// we are switching the core, so more reboots are expected
	restarting, kind = restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystem)
	c.Assert(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	// restarting to a new base
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m.TryBase, Equals, "core20_33.snap")
	c.Assert(m.BaseStatus, Equals, "try")
	// we've rebooted
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// and pretend we boot the base
	m.BaseStatus = "trying"
	c.Assert(m.Write(), IsNil)

	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("remodel change failed: %v", chg.Err()))

	var snapst snapstate.SnapState
	err = snapstate.Get(st, "core20", &snapst)
	c.Assert(err, IsNil)
	// and the kernel tracking channel has been updated
	c.Check(snapst.TrackingChannel, Equals, "latest/edge")

	// ensure sorting is correct
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	var i int
	// first all downloads/checks in sequential order
	i += validateDownloadCheckTasks(c, tasks[i:], "core20", "33", "latest/edge")
	// then create recovery
	i += validateRecoverySystemTasks(c, tasks[i:], expectedLabel)
	// then all refreshes in sequential order (no configure hooks for bases though)
	validateRefreshTasks(c, tasks[i:], "core20", "33", noConfigure)
}

func (s *mgrsSuiteCore) TestRemodelUC20BackToPreviousGadget(c *C) {
	s.testRemodelUC20WithRecoverySystemSimpleSetUp(c)
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "proc"), 0755), IsNil)
	restore := kcmdline.MockProcCmdline(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"))
	defer restore()
	newModel := s.brands.Model("can0nical", "my-model", uc20ModelDefaults, map[string]interface{}{
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              fakeSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "old-pc",
				"id":              fakeSnapID("old-pc"),
				"type":            "gadget",
				"default-channel": "20/edge",
			},
		},
		"revision": "1",
	})
	bl, err := bootloader.Find(boot.InitramfsUbuntuSeedDir, &bootloader.Options{Role: bootloader.RoleRecovery})
	c.Assert(err, IsNil)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	a11, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-name":    "old-pc",
		"snap-id":      fakeSnapID("old-pc"),
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a11), IsNil)
	c.Assert(s.storeSigning.Add(a11), IsNil)

	s.makeInstalledSnapInStateForRemodel(c, "old-pc", snap.R(1), "20/edge")

	now := time.Now()
	expectedLabel := now.Format("20060102")

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"pc": {
					0: {
						RootMountPoint: "/foo-seed",
					},
					1: {
						RootMountPoint: "/foo-boot",
					},
					2: {
						RootMountPoint: "/foo-data",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"pc": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
			}, nil
	})
	defer r()

	updater := &mockUpdater{}
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, rootDir, rollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		// use a mock updater pretends an update was applied
		return updater, nil
	})
	defer restore()

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	// gadget update has not been applied yet
	c.Check(updater.updateCalls, Equals, 0)

	// first comes a reboot to the new recovery system
	c.Check(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	c.Check(devicestate.RemodelingChange(st), NotNil)
	restarting, kind := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystemNow)
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentRecoverySystems, DeepEquals, []string{"1234", expectedLabel})
	c.Check(m.GoodRecoverySystems, DeepEquals, []string{"1234"})
	vars, err := bl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "try",
	})
	// simulate successful reboot to recovery
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// this would be done by snap-bootstrap in initramfs
	err = bl.SetBootVars(map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "tried",
	})
	c.Assert(err, IsNil)
	// reset, so that after-reboot handling of tried system is executed
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	// update is called only for the ubuntu-seed partition as it is the
	// only one with contents (see oldPcGadgetYamlForRemodel).
	c.Check(updater.updateCalls, Equals, 1)
	// a reboot was requested, as mock updated were applied
	restarting, kind = restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystem)

	// simulate successful reboot back
	s.mockRestartAndSettle(c, st, chg)

	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 foo bar baz",
	})

	// pretend we have the right command line
	c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"),
		[]byte("snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 foo bar baz"), 0444),
		IsNil)

	// run post boot code again
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// verify command lines again
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 foo bar baz",
	})

	// simulate successful reboot after finalizing recovery system
	s.mockRestartAndSettle(c, st, chg)
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("remodel change failed: %v", chg.Err()))

	var snapst snapstate.SnapState
	err = snapstate.Get(st, "old-pc", &snapst)
	c.Assert(err, IsNil)
	// and the gadget tracking channel is the same as in the model
	c.Check(snapst.TrackingChannel, Equals, "20/edge")

	// ensure sorting is correct
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	var i int

	// prepare first
	c.Assert(tasks[i].Summary(), Equals, `Prepare snap "old-pc" (1) for remodel`)
	i++
	// then recovery system
	i += validateRecoverySystemTasks(c, tasks[i:], expectedLabel)
	// then gadget switch with update of assets and kernel command line
	i += validateGadgetSwitchTasks(c, tasks[i:], "old-pc", "1")
	// finally new model assertion
	c.Assert(tasks[i].Summary(), Equals, `Set new model assertion`)
	i++
	c.Check(i, Equals, len(tasks))
}

func (s *mgrsSuiteCore) TestRemodelUC20ExistingGadgetSnapDifferentChannel(c *C) {
	// a remodel where the target model uses a gadget that is already
	// present (possibly due to being used by one of the previous models)
	// but tracks a different channel than what the new model ordains
	s.testRemodelUC20WithRecoverySystemSimpleSetUp(c)
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "proc"), 0755), IsNil)
	restore := kcmdline.MockProcCmdline(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"))
	defer restore()
	newModel := s.brands.Model("can0nical", "my-model", uc20ModelDefaults, map[string]interface{}{
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              fakeSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "old-pc",
				"id":              fakeSnapID("old-pc"),
				"type":            "gadget",
				"default-channel": "20/edge",
			},
		},
		"revision": "1",
	})
	bl, err := bootloader.Find(boot.InitramfsUbuntuSeedDir, &bootloader.Options{Role: bootloader.RoleRecovery})
	c.Assert(err, IsNil)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	a11, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-name":    "old-pc",
		"snap-id":      fakeSnapID("old-pc"),
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a11), IsNil)
	c.Assert(s.storeSigning.Add(a11), IsNil)

	snapInfo := s.makeInstalledSnapInStateForRemodel(c, "old-pc", snap.R(1), "20/beta")
	// there already is a snap revision assertion for this snap, just serve
	// it in the mock store
	s.serveSnap(snapInfo.MountFile(), "1")

	now := time.Now()
	expectedLabel := now.Format("20060102")

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"pc": {
					0: {
						RootMountPoint: "/foo-seed",
					},
					1: {
						RootMountPoint: "/foo-boot",
					},
					2: {
						RootMountPoint: "/foo-data",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"pc": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
			}, nil
	})
	defer r()

	updater := &mockUpdater{}
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, rootDir, rollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		// use a mock updater pretends an update was applied
		return updater, nil
	})
	defer restore()

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	// gadget update has not been applied yet
	c.Check(updater.updateCalls, Equals, 0)

	// first comes a reboot to the new recovery system
	c.Check(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	c.Check(devicestate.RemodelingChange(st), NotNil)
	restarting, kind := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystemNow)
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentRecoverySystems, DeepEquals, []string{"1234", expectedLabel})
	c.Check(m.GoodRecoverySystems, DeepEquals, []string{"1234"})
	vars, err := bl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "try",
	})
	// simulate successful reboot to recovery
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// this would be done by snap-bootstrap in initramfs
	err = bl.SetBootVars(map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "tried",
	})
	c.Assert(err, IsNil)
	// reset, so that after-reboot handling of tried system is executed
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	// update is called only for the ubuntu-seed partition as it is the
	// only one with contents (see oldPcGadgetYamlForRemodel).
	c.Check(updater.updateCalls, Equals, 1)
	// a reboot was requested, as mock updated were applied
	restarting, kind = restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystem)

	// simulate successful reboot back
	s.mockRestartAndSettle(c, st, chg)

	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 foo bar baz",
	})

	// pretend we have the right command line
	c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"),
		[]byte("snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 foo bar baz"), 0444),
		IsNil)

	// run post boot code again
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// verify command lines again
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 foo bar baz",
	})

	// simulate successful reboot for the recovery to finalize
	s.mockRestartAndSettle(c, st, chg)
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("remodel change failed: %v", chg.Err()))

	var snapst snapstate.SnapState
	err = snapstate.Get(st, "old-pc", &snapst)
	c.Assert(err, IsNil)
	// and the gadget tracking channel is the same as in the model
	c.Check(snapst.TrackingChannel, Equals, "20/edge")

	// ensure sorting is correct
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	var i int

	// prepare first
	c.Assert(tasks[i].Summary(), Equals, `Switch snap "old-pc" from channel "20/beta" to "20/edge"`)
	i++
	// then recovery system
	i += validateRecoverySystemTasks(c, tasks[i:], expectedLabel)
	// then gadget switch with update of assets and kernel command line
	i += validateGadgetSwitchTasks(c, tasks[i:], "old-pc", "1")
	// finally new model assertion
	c.Assert(tasks[i].Summary(), Equals, `Set new model assertion`)
	i++
	c.Check(i, Equals, len(tasks))
}

const prereqSnapYaml = `
name: prereq
version: 1.0
base: prereq-base
plugs:
  prereq-content:
    interface: content
    target: $SNAP/data-dir
    default-provider: prereq-content
`

func (s *mgrsSuiteCore) TestRemodelUC20SnapWithPrereqsMissingDeps(c *C) {
	s.testRemodelUC20WithRecoverySystemSimpleSetUp(c)

	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "proc"), 0755), IsNil)
	restore := kcmdline.MockProcCmdline(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"))
	defer restore()
	newModel := s.brands.Model("can0nical", "my-model", uc20ModelDefaults, map[string]interface{}{
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              fakeSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              fakeSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name": "prereq",
				"id":   fakeSnapID("prereq"),
			},
			// prepreq requires prereq-base and prereq-content
		},
		"revision": "1",
	})

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	s.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "prereq",
	})

	snapPath, _ := s.makeStoreTestSnap(c, prereqSnapYaml, "1")
	s.serveSnap(snapPath, "1")

	snapstate.Set(st, "core", nil)
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", SnapID: fakeSnapID("snapd"), Revision: snap.R(1)},
		}),
		Current:         snap.R(1),
		SnapType:        "snapd",
		TrackingChannel: "latest/stable",
		Flags: snapstate.Flags{
			Required: true,
		},
	})

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})

	msg := `cannot remodel to model that is not self contained:
  - cannot use snap "prereq": base "prereq-base" is missing
  - cannot use snap "prereq": default provider "prereq-content" or any alternative provider for content "prereq-content" is missing`

	c.Assert(err, ErrorMatches, msg)
	c.Assert(chg, IsNil)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(st, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{oi}),
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
	c.Assert(assertstate.RefreshSnapDeclarations(st, 0, nil), IsNil)

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

func dumpTasks(c *C, when string, tasks []*state.Task) {
	c.Logf("--- tasks dump %s", when)
	for _, tsk := range tasks {
		c.Logf("  -- %4s %10s %15s %s", tsk.ID(), tsk.Status(), tsk.Kind(), tsk.Summary())
	}
	for _, tsk := range tasks {
		if len(tsk.Log()) > 0 {
			c.Logf("--- %s", tsk.Kind())
			for _, l := range tsk.Log() {
				c.Logf("%s", l)
			}
		}
	}
}

func (s *mgrsSuiteCore) TestRemodelRollbackValidationSets(c *C) {
	st := s.o.State()

	st.Lock()
	vsetAssert1, err := s.brands.Signing("can0nical").Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "can0nical",
		"series":       "16",
		"account-id":   "can0nical",
		"name":         "vset-1",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       fakeSnapID("pc-kernel"),
				"revision": "2",
				"presence": "required",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, vsetAssert1), IsNil)
	c.Assert(s.storeSigning.Add(vsetAssert1), IsNil)

	vsetAssert2, err := s.brands.Signing("can0nical").Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "can0nical",
		"series":       "16",
		"account-id":   "can0nical",
		"name":         "vset-2",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       fakeSnapID("pc-kernel"),
				"revision": "33",
				"presence": "required",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, vsetAssert2), IsNil)
	c.Assert(s.storeSigning.Add(vsetAssert2), IsNil)

	vsetAssert3, err := s.brands.Signing("can0nical").Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "can0nical",
		"series":       "16",
		"account-id":   "can0nical",
		"name":         "vset-3",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "snapd",
				"id":       fakeSnapID("snapd"),
				"revision": "4",
				"presence": "required",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, vsetAssert3), IsNil)
	c.Assert(s.storeSigning.Add(vsetAssert3), IsNil)
	st.Unlock()

	modelValSets := map[string]interface{}{
		"validation-sets": []interface{}{
			map[string]interface{}{
				"account-id": "can0nical",
				"name":       "vset-1",
				"mode":       "enforce",
			},
		},
	}

	s.testRemodelUC20WithRecoverySystemSimpleSetUp(c, modelValSets)

	restore := kcmdline.MockProcCmdline(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"))
	defer restore()

	restore = backend.MockAllUsers(func(*dirs.SnapDirOptions) ([]*user.User, error) {
		usr, err := user.Current()
		if err != nil {
			return nil, err
		}

		usr.HomeDir = filepath.Join(dirs.GlobalRootDir, usr.HomeDir)
		return []*user.User{usr}, nil
	})
	defer restore()

	st.Lock()
	defer st.Unlock()

	installed, ignore, err := snapstate.InstalledSnaps(st)
	c.Assert(err, IsNil)
	vs1 := vsetAssert1.(*asserts.ValidationSet)
	vs3 := vsetAssert3.(*asserts.ValidationSet)
	err = assertstate.ApplyLocalEnforcedValidationSets(st, map[string][]string{
		vs1.SequenceKey(): vs1.At().PrimaryKey,
		vs3.SequenceKey(): vs3.At().PrimaryKey,
	}, nil, installed, ignore)
	c.Assert(err, IsNil)

	// make core22 a thing
	a11, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-name":    "core22",
		"snap-id":      fakeSnapID("core22"),
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a11), IsNil)
	c.Assert(s.storeSigning.Add(a11), IsNil)

	snapPath, _ := s.makeStoreTestSnapWithFiles(c, snapYamlsForRemodel["core22"], "1", nil)
	s.serveSnap(snapPath, "1")
	snapPath, _ = s.makeStoreTestSnapWithFiles(c, pcKernel22SnapYaml, "33", snapFilesForRemodel["pc-kernel-track-22"])
	s.serveSnap(snapPath, "33")
	snapPath, _ = s.makeStoreTestSnapWithFiles(c, pcGadget22SnapYaml, "34", snapFilesForRemodel["pc-track-22"])
	s.serveSnap(snapPath, "34")

	newModel := s.brands.Model("can0nical", "my-model", uc20ModelDefaults, map[string]interface{}{
		// replace the base
		"base": "core22",
		"validation-sets": []interface{}{
			map[string]interface{}{
				"account-id": "can0nical",
				"name":       "vset-2",
				"mode":       "enforce",
			},
		},
		"snaps": []interface{}{
			// kernel and gadget snaps with new tracks
			map[string]interface{}{
				"name": "pc-kernel",
				"id":   fakeSnapID("pc-kernel"),
				"type": "kernel",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              fakeSnapID("pc"),
				"type":            "gadget",
				"default-channel": "22",
			},
		},
		"revision": "1",
	})
	bl, err := bootloader.Find(boot.InitramfsUbuntuSeedDir, &bootloader.Options{Role: bootloader.RoleRecovery})
	c.Assert(err, IsNil)

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"pc": {
					0: {
						RootMountPoint: "/foo-seed",
					},
					1: {
						RootMountPoint: "/foo-boot",
					},
					2: {
						RootMountPoint: "/foo-data",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"pc": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
			}, nil
	})
	defer r()

	// remodel updates a gadget, setup a mock updater that pretends an
	// update was applied
	updater := &mockUpdater{}
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, rootDir, rollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		// use a mock updater pretends an update was applied
		return updater, nil
	})
	defer restore()

	now := time.Now()
	expectedLabel := now.Format("20060102")

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	dumpTasks(c, "at the beginning", chg.Tasks())

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	// gadget update has been not been applied yet
	c.Check(updater.updateCalls, Equals, 0)

	dumpTasks(c, "after recovery system", chg.Tasks())

	// first comes a reboot to the new recovery system
	c.Check(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	c.Check(devicestate.RemodelingChange(st), NotNil)
	restarting, kind := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystemNow)
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentRecoverySystems, DeepEquals, []string{"1234", expectedLabel})
	c.Check(m.GoodRecoverySystems, DeepEquals, []string{"1234"})
	vars, err := bl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "try",
	})

	// simulate successful reboot to recovery and back
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)

	// this would be done by snap-bootstrap in initramfs
	err = bl.SetBootVars(map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "tried",
	})
	c.Assert(err, IsNil)
	// reset, so that after-reboot handling of tried system is executed
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// next we'll observe kernel getting installed
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	dumpTasks(c, "after kernel install", chg.Tasks())
	// gadget update has been not been applied yet
	c.Check(updater.updateCalls, Equals, 0)

	restarting, kind = restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystem)
	c.Assert(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	// and we've rebooted
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// pretend the kernel has booted
	rbl, err := bootloader.Find(dirs.GlobalRootDir, &bootloader.Options{Role: bootloader.RoleRunMode})
	c.Assert(err, IsNil)
	vars, err = rbl.GetBootVars("kernel_status")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"kernel_status": "try",
	})
	err = rbl.SetBootVars(map[string]string{
		"kernel_status": "trying",
	})
	c.Assert(err, IsNil)

	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// next the base
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	dumpTasks(c, "after base install", chg.Tasks())
	// gadget update has been not been applied yet
	c.Check(updater.updateCalls, Equals, 0)

	// restarting to a new base
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m.TryBase, Equals, "core22_1.snap")
	c.Assert(m.BaseStatus, Equals, "try")
	// we've rebooted
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// and pretend we boot the base
	m.BaseStatus = "trying"
	c.Assert(m.Write(), IsNil)

	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// next the gadget which updates the command line
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// gadget update for the seed partition has been applied
	c.Check(updater.updateCalls, Equals, 1)

	dumpTasks(c, "after gadget install", chg.Tasks())

	// the gadget has updated the kernel command line
	restarting, kind = restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystem)
	c.Assert(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 uc22",
	})
	// we've rebooted
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// pretend we have the right command line
	c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"),
		[]byte("snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 uc22"), 0444),
		IsNil)

	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	devicestate.InjectSetModelError(fmt.Errorf("boom"))
	defer devicestate.InjectSetModelError(nil)

	// we need to reboot twice here to fully report the failure
	s.mockRestartAndSettle(c, st, chg)
	s.mockRestartAndSettle(c, st, chg)

	dumpTasks(c, "after failing to set the new model", chg.Tasks())

	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// list validation sets that are currently tracked
	currentSets, err := assertstate.TrackedEnforcedValidationSets(st)
	c.Assert(err, IsNil)
	c.Check(currentSets.Keys(), testutil.DeepUnsortedMatches, []snapasserts.ValidationSetKey{
		"16/can0nical/vset-1/1",
		"16/can0nical/vset-3/1",
	})

	history, err := assertstate.ValidationSetsHistory(st)

	c.Assert(err, IsNil)

	// TODO: note the two empty maps here. this is from us having to remove the
	// validation set from the previous model and then tracking the new model in
	// a separate action. do we need a new function in asserstate for replacing
	// the validation sets atomically?
	c.Check(history, DeepEquals, []map[string]*assertstate.ValidationSetTracking{
		{
			"can0nical/vset-1": {
				AccountID: "can0nical",
				Name:      "vset-1",
				Mode:      assertstate.Enforce,
				Current:   1,
			},
			"can0nical/vset-3": {
				AccountID: "can0nical",
				Name:      "vset-3",
				Mode:      assertstate.Enforce,
				Current:   1,
			},
		},
		{
			"can0nical/vset-3": {
				AccountID: "can0nical",
				Name:      "vset-3",
				Mode:      assertstate.Enforce,
				Current:   1,
			},
		},
		{
			"can0nical/vset-2": {
				AccountID: "can0nical",
				Name:      "vset-2",
				Mode:      assertstate.Enforce,
				Current:   1,
			},
			"can0nical/vset-3": {
				AccountID: "can0nical",
				Name:      "vset-3",
				Mode:      assertstate.Enforce,
				Current:   1,
			},
		},
		{
			"can0nical/vset-3": {
				AccountID: "can0nical",
				Name:      "vset-3",
				Mode:      assertstate.Enforce,
				Current:   1,
			},
		},
		{
			"can0nical/vset-1": {
				AccountID: "can0nical",
				Name:      "vset-1",
				Mode:      assertstate.Enforce,
				Current:   1,
			},
			"can0nical/vset-3": {
				AccountID: "can0nical",
				Name:      "vset-3",
				Mode:      assertstate.Enforce,
				Current:   1,
			},
		},
	})
}

func (s *mgrsSuiteCore) TestRemodelReplaceValidationSets(c *C) {
	st := s.o.State()

	st.Lock()
	// this validation set only appears in the first model
	vsetAssert1, err := s.brands.Signing("can0nical").Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "can0nical",
		"series":       "16",
		"account-id":   "can0nical",
		"name":         "vset-1",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       fakeSnapID("pc-kernel"),
				"revision": "2",
				"presence": "required",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, vsetAssert1), IsNil)
	c.Assert(s.storeSigning.Add(vsetAssert1), IsNil)

	// this validation set only appears in the second model
	vsetAssert2, err := s.brands.Signing("can0nical").Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "can0nical",
		"series":       "16",
		"account-id":   "can0nical",
		"name":         "vset-2",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       fakeSnapID("pc-kernel"),
				"revision": "33",
				"presence": "required",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, vsetAssert2), IsNil)
	c.Assert(s.storeSigning.Add(vsetAssert2), IsNil)

	// this validation set appears in neither model, bus is still tracked by the
	// system
	vsetAssert3, err := s.brands.Signing("can0nical").Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "can0nical",
		"series":       "16",
		"account-id":   "can0nical",
		"name":         "vset-3",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "snapd",
				"id":       fakeSnapID("snapd"),
				"revision": "4",
				"presence": "required",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, vsetAssert3), IsNil)
	c.Assert(s.storeSigning.Add(vsetAssert3), IsNil)

	// this validation set appears in both models
	vsetAssert4, err := s.brands.Signing("can0nical").Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "can0nical",
		"series":       "16",
		"account-id":   "can0nical",
		"name":         "vset-4",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc",
				"id":       fakeSnapID("pc"),
				"presence": "required",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, vsetAssert4), IsNil)
	c.Assert(s.storeSigning.Add(vsetAssert4), IsNil)

	st.Unlock()

	modelValSets := map[string]interface{}{
		"validation-sets": []interface{}{
			map[string]interface{}{
				"account-id": "can0nical",
				"name":       "vset-1",
				"mode":       "enforce",
			},
			map[string]interface{}{
				"account-id": "can0nical",
				"name":       "vset-4",
				"mode":       "enforce",
			},
		},
	}

	s.testRemodelUC20WithRecoverySystemSimpleSetUp(c, modelValSets)

	restore := kcmdline.MockProcCmdline(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"))
	defer restore()

	restore = backend.MockAllUsers(func(*dirs.SnapDirOptions) ([]*user.User, error) {
		usr, err := user.Current()
		if err != nil {
			return nil, err
		}

		usr.HomeDir = filepath.Join(dirs.GlobalRootDir, usr.HomeDir)
		return []*user.User{usr}, nil
	})
	defer restore()

	st.Lock()
	defer st.Unlock()

	installed, ignore, err := snapstate.InstalledSnaps(st)
	c.Assert(err, IsNil)
	vs1 := vsetAssert1.(*asserts.ValidationSet)
	vs3 := vsetAssert3.(*asserts.ValidationSet)
	vs4 := vsetAssert4.(*asserts.ValidationSet)
	err = assertstate.ApplyLocalEnforcedValidationSets(st, map[string][]string{
		vs1.SequenceKey(): vs1.At().PrimaryKey,
		vs3.SequenceKey(): vs3.At().PrimaryKey,
		vs4.SequenceKey(): vs4.At().PrimaryKey,
	}, nil, installed, ignore)
	c.Assert(err, IsNil)

	// make core22 a thing
	a11, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-name":    "core22",
		"snap-id":      fakeSnapID("core22"),
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a11), IsNil)
	c.Assert(s.storeSigning.Add(a11), IsNil)

	snapPath, _ := s.makeStoreTestSnapWithFiles(c, snapYamlsForRemodel["core22"], "1", nil)
	s.serveSnap(snapPath, "1")
	snapPath, _ = s.makeStoreTestSnapWithFiles(c, pcKernel22SnapYaml, "33", snapFilesForRemodel["pc-kernel-track-22"])
	s.serveSnap(snapPath, "33")
	snapPath, _ = s.makeStoreTestSnapWithFiles(c, pcGadget22SnapYaml, "34", snapFilesForRemodel["pc-track-22"])
	s.serveSnap(snapPath, "34")

	newModel := s.brands.Model("can0nical", "my-model", uc20ModelDefaults, map[string]interface{}{
		// replace the base
		"base": "core22",
		"validation-sets": []interface{}{
			map[string]interface{}{
				"account-id": "can0nical",
				"name":       "vset-2",
				"mode":       "enforce",
			},
			map[string]interface{}{
				"account-id": "can0nical",
				"name":       "vset-4",
				"mode":       "enforce",
			},
		},
		"snaps": []interface{}{
			// kernel and gadget snaps with new tracks
			map[string]interface{}{
				"name": "pc-kernel",
				"id":   fakeSnapID("pc-kernel"),
				"type": "kernel",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              fakeSnapID("pc"),
				"type":            "gadget",
				"default-channel": "22",
			},
		},
		"revision": "1",
	})
	bl, err := bootloader.Find(boot.InitramfsUbuntuSeedDir, &bootloader.Options{Role: bootloader.RoleRecovery})
	c.Assert(err, IsNil)

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"pc": {
					0: {
						RootMountPoint: "/foo-seed",
					},
					1: {
						RootMountPoint: "/foo-boot",
					},
					2: {
						RootMountPoint: "/foo-data",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"pc": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
			}, nil
	})
	defer r()

	// remodel updates a gadget, setup a mock updater that pretends an
	// update was applied
	updater := &mockUpdater{}
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, rootDir, rollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		// use a mock updater pretends an update was applied
		return updater, nil
	})
	defer restore()

	now := time.Now()
	expectedLabel := now.Format("20060102")

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	dumpTasks(c, "at the beginning", chg.Tasks())

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	// gadget update has been not been applied yet
	c.Check(updater.updateCalls, Equals, 0)

	dumpTasks(c, "after recovery system", chg.Tasks())

	// first comes a reboot to the new recovery system
	c.Check(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	c.Check(devicestate.RemodelingChange(st), NotNil)
	restarting, kind := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystemNow)
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentRecoverySystems, DeepEquals, []string{"1234", expectedLabel})
	c.Check(m.GoodRecoverySystems, DeepEquals, []string{"1234"})
	vars, err := bl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "try",
	})

	// simulate successful reboot to recovery and back
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)

	// this would be done by snap-bootstrap in initramfs
	err = bl.SetBootVars(map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "tried",
	})
	c.Assert(err, IsNil)
	// reset, so that after-reboot handling of tried system is executed
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// next we'll observe kernel getting installed
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	dumpTasks(c, "after kernel install", chg.Tasks())
	// gadget update has been not been applied yet
	c.Check(updater.updateCalls, Equals, 0)

	restarting, kind = restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystem)
	c.Assert(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	// and we've rebooted
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// pretend the kernel has booted
	rbl, err := bootloader.Find(dirs.GlobalRootDir, &bootloader.Options{Role: bootloader.RoleRunMode})
	c.Assert(err, IsNil)
	vars, err = rbl.GetBootVars("kernel_status")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"kernel_status": "try",
	})
	err = rbl.SetBootVars(map[string]string{
		"kernel_status": "trying",
	})
	c.Assert(err, IsNil)

	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// next the base
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	dumpTasks(c, "after base install", chg.Tasks())
	// gadget update has been not been applied yet
	c.Check(updater.updateCalls, Equals, 0)

	// restarting to a new base
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m.TryBase, Equals, "core22_1.snap")
	c.Assert(m.BaseStatus, Equals, "try")
	// we've rebooted
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// and pretend we boot the base
	m.BaseStatus = "trying"
	c.Assert(m.Write(), IsNil)

	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// next the gadget which updates the command line
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// gadget update for the seed partition has been applied
	c.Check(updater.updateCalls, Equals, 1)

	dumpTasks(c, "after gadget install", chg.Tasks())

	// the gadget has updated the kernel command line
	restarting, kind = restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystem)
	c.Assert(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 uc22",
	})
	// we've rebooted
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// pretend we have the right command line
	c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"),
		[]byte("snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 uc22"), 0444),
		IsNil)

	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	dumpTasks(c, "after settle before assert", chg.Tasks())

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("remodel change failed: %v", chg.Err()))

	dumpTasks(c, "after set-model", chg.Tasks())

	var snapst snapstate.SnapState
	err = snapstate.Get(st, "core22", &snapst)
	c.Assert(err, IsNil)

	// ensure sorting is correct
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	var i int
	// first all downloads/checks in sequential order
	i += validateDownloadCheckTasks(c, tasks[i:], "pc-kernel", "33", "latest/stable")
	i += validateDownloadCheckTasks(c, tasks[i:], "core22", "1", "latest/stable")
	i += validateDownloadCheckTasks(c, tasks[i:], "pc", "34", "22/stable")
	// then create recovery
	i += validateRecoverySystemTasks(c, tasks[i:], expectedLabel)
	// then all refreshes and install in sequential order (no configure hooks for bases though)
	i += validateRefreshTasks(c, tasks[i:], "pc-kernel", "33", isKernel|hasModeenv)
	i += validateInstallTasks(c, tasks[i:], "core22", "1", noConfigure)
	i += validateRefreshTasks(c, tasks[i:], "pc", "34", isGadget)
	// finally new model assertion
	c.Assert(tasks[i].Summary(), Equals, `Set new model assertion`)
	i++
	c.Check(i, Equals, len(tasks))

	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 uc22",
	})
	c.Check(m.CurrentRecoverySystems, DeepEquals, []string{
		"1234", expectedLabel,
	})
	c.Check(m.GoodRecoverySystems, DeepEquals, []string{
		"1234", expectedLabel,
	})
	c.Check(m.Base, Equals, "core22_1.snap")

	// list validation sets that are currently tracked
	currentSets, err := assertstate.TrackedEnforcedValidationSets(st)
	c.Assert(err, IsNil)
	c.Check(currentSets.Keys(), testutil.DeepUnsortedMatches, []snapasserts.ValidationSetKey{
		"16/can0nical/vset-2/1",
		"16/can0nical/vset-3/1",
		"16/can0nical/vset-4/1",
	})
}

func (s *mgrsSuiteCore) TestRemodelUC20ToUC22(c *C) {
	const mockSnapdRefresh = false
	s.testRemodelUC20ToUC22(c, mockSnapdRefresh)
}

func (s *mgrsSuiteCore) TestRemodelUC20ToUC22MockSnapdRefresh(c *C) {
	const mockSnapdRefresh = true
	s.testRemodelUC20ToUC22(c, mockSnapdRefresh)
}

func (s *mgrsSuiteCore) testRemodelUC20ToUC22(c *C, mockSnapdRefresh bool) {
	s.testRemodelUC20WithRecoverySystemSimpleSetUp(c)
	restore := kcmdline.MockProcCmdline(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"))
	defer restore()

	restore = backend.MockAllUsers(func(*dirs.SnapDirOptions) ([]*user.User, error) {
		usr, err := user.Current()
		if err != nil {
			return nil, err
		}

		usr.HomeDir = filepath.Join(dirs.GlobalRootDir, usr.HomeDir)
		return []*user.User{usr}, nil
	})
	defer restore()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// make core22 a thing
	a11, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-name":    "core22",
		"snap-id":      fakeSnapID("core22"),
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a11), IsNil)
	c.Assert(s.storeSigning.Add(a11), IsNil)

	snapPath, _ := s.makeStoreTestSnapWithFiles(c, snapYamlsForRemodel["core22"], "1", nil)
	s.serveSnap(snapPath, "1")
	snapPath, _ = s.makeStoreTestSnapWithFiles(c, pcKernel22SnapYaml, "33", snapFilesForRemodel["pc-kernel-track-22"])
	s.serveSnap(snapPath, "33")
	snapPath, _ = s.makeStoreTestSnapWithFiles(c, pcGadget22SnapYaml, "34", snapFilesForRemodel["pc-track-22"])
	s.serveSnap(snapPath, "34")

	newModel := s.brands.Model("can0nical", "my-model", uc20ModelDefaults, map[string]interface{}{
		// replace the base
		"base": "core22",
		"snaps": []interface{}{
			// kernel and gadget snaps with new tracks
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              fakeSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "22",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              fakeSnapID("pc"),
				"type":            "gadget",
				"default-channel": "22",
			},
		},
		"revision": "1",
	})
	bl, err := bootloader.Find(boot.InitramfsUbuntuSeedDir, &bootloader.Options{Role: bootloader.RoleRecovery})
	c.Assert(err, IsNil)

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"pc": {
					0: {
						RootMountPoint: "/foo-seed",
					},
					1: {
						RootMountPoint: "/foo-boot",
					},
					2: {
						RootMountPoint: "/foo-data",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"pc": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
			}, nil
	})
	defer r()

	// remodel updates a gadget, setup a mock updater that pretends an
	// update was applied
	updater := &mockUpdater{}
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, rootDir, rollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		// use a mock updater pretends an update was applied
		return updater, nil
	})
	defer restore()

	now := time.Now()
	expectedLabel := now.Format("20060102")

	chg, err := devicestate.Remodel(st, newModel, nil, nil, devicestate.RemodelOptions{})
	c.Assert(err, IsNil)
	dumpTasks(c, "at the beginning", chg.Tasks())

	if mockSnapdRefresh {
		// on a snapd refresh, we might upgrade from a snapd that didn't set
		// "test-system" on "recovery-system-setup". in that case, we should
		// test to make sure that we still test the recovery system due to this
		// being a remodel.
		err := mockTestSystemDefaultToFalse(chg)
		c.Assert(err, IsNil)
	}

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	// gadget update has been not been applied yet
	c.Check(updater.updateCalls, Equals, 0)

	dumpTasks(c, "after recovery system", chg.Tasks())

	// first comes a reboot to the new recovery system
	c.Check(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	c.Check(devicestate.RemodelingChange(st), NotNil)
	restarting, kind := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystemNow)
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.CurrentRecoverySystems, DeepEquals, []string{"1234", expectedLabel})
	c.Check(m.GoodRecoverySystems, DeepEquals, []string{"1234"})
	vars, err := bl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "try",
	})
	// simulate successful reboot to recovery and back
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// this would be done by snap-bootstrap in initramfs
	err = bl.SetBootVars(map[string]string{
		"try_recovery_system":    expectedLabel,
		"recovery_system_status": "tried",
	})
	c.Assert(err, IsNil)
	// reset, so that after-reboot handling of tried system is executed
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// next we'll observe kernel getting installed
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	dumpTasks(c, "after kernel install", chg.Tasks())
	// gadget update has been not been applied yet
	c.Check(updater.updateCalls, Equals, 0)

	restarting, kind = restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystem)
	c.Assert(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	// and we've rebooted
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// pretend the kernel has booted
	rbl, err := bootloader.Find(dirs.GlobalRootDir, &bootloader.Options{Role: bootloader.RoleRunMode})
	c.Assert(err, IsNil)
	vars, err = rbl.GetBootVars("kernel_status")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"kernel_status": "try",
	})
	err = rbl.SetBootVars(map[string]string{
		"kernel_status": "trying",
	})
	c.Assert(err, IsNil)

	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// next the base
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	dumpTasks(c, "after base install", chg.Tasks())
	// gadget update has been not been applied yet
	c.Check(updater.updateCalls, Equals, 0)

	// restarting to a new base
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m.TryBase, Equals, "core22_1.snap")
	c.Assert(m.BaseStatus, Equals, "try")
	// we've rebooted
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// and pretend we boot the base
	m.BaseStatus = "trying"
	c.Assert(m.Write(), IsNil)

	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// next the gadget which updates the command line
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// gadget update for the seed partition has been applied
	c.Check(updater.updateCalls, Equals, 1)

	dumpTasks(c, "after gadget install", chg.Tasks())

	// the gadget has updated the kernel command line
	restarting, kind = restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystem)
	c.Assert(chg.Status(), Equals, state.WaitStatus, Commentf("remodel change failed: %v", chg.Err()))
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 uc22",
	})
	// we've rebooted
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// pretend we have the right command line
	c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "proc/cmdline"),
		[]byte("snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 uc22"), 0444),
		IsNil)

	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("remodel change failed: %v", chg.Err()))

	dumpTasks(c, "after set-model", chg.Tasks())

	var snapst snapstate.SnapState
	err = snapstate.Get(st, "core22", &snapst)
	c.Assert(err, IsNil)

	// ensure sorting is correct
	tasks := chg.Tasks()
	sort.Sort(byReadyTime(tasks))

	var i int
	// first all downloads/checks in sequential order
	i += validateDownloadCheckTasks(c, tasks[i:], "pc-kernel", "33", "22/stable")
	i += validateDownloadCheckTasks(c, tasks[i:], "core22", "1", "latest/stable")
	i += validateDownloadCheckTasks(c, tasks[i:], "pc", "34", "22/stable")
	// then create recovery
	i += validateRecoverySystemTasks(c, tasks[i:], expectedLabel)
	// then all refreshes and install in sequential order (no configure hooks for bases though)
	i += validateRefreshTasks(c, tasks[i:], "pc-kernel", "33", isKernel|hasModeenv)
	i += validateInstallTasks(c, tasks[i:], "core22", "1", noConfigure)
	i += validateRefreshTasks(c, tasks[i:], "pc", "34", isGadget)
	// finally new model assertion
	c.Assert(tasks[i].Summary(), Equals, `Set new model assertion`)
	i++
	c.Check(i, Equals, len(tasks))

	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 uc22",
	})
	c.Check(m.CurrentRecoverySystems, DeepEquals, []string{
		"1234", expectedLabel,
	})
	c.Check(m.GoodRecoverySystems, DeepEquals, []string{
		"1234", expectedLabel,
	})
	c.Check(m.Base, Equals, "core22_1.snap")
}

func mockTestSystemDefaultToFalse(chg *state.Change) error {
	for _, t := range chg.Tasks() {
		if t.Kind() != "create-recovery-system" {
			continue
		}

		var setup map[string]interface{}
		if err := t.Get("recovery-system-setup", &setup); err != nil {
			return err
		}

		setup["test-system"] = false
		t.Set("recovery-system-setup", setup)

		return nil
	}

	return errors.New("no create-recovery-system task found")
}

func (s *mgrsSuiteCore) TestInstallKernelSnapRollbackUpdatesBootloaderEnv(c *C) {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, packageKernel, si1, [][]string{
		{"meta/kernel.yaml", ""},
	})
	si2 := &snap.SideInfo{RealName: "core18", Revision: snap.R(2)}
	snapstate.Set(st, "core18", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
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

	// InstallPath does not set any restart boundaries by itself, this is something
	// that must be handled where we use it, and actually schedule the change.
	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "pc-kernel"}, snapPath, "", "", snapstate.Flags{}, nil)
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
	restarting, _ := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Check(chg.Status(), Equals, state.WaitStatus)
	s.mockRollbackAcrossReboot(c, chg, bloader, []snap.Type{snap.TypeKernel})

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

func (s *mgrsSuite) TestUC18SnapdRefreshUpdatesSnapServiceUnitsAndRestartsKilledUnits(c *C) {
	restore := release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()
	// reload directories
	dirs.SetRootDir(dirs.GlobalRootDir)
	restore = release.MockOnClassic(false)
	defer restore()
	bl := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bl)
	defer bootloader.Force(nil)
	const snapdSnap = `
name: snapd
version: 1.0
type: snapd`
	snapPath := snaptest.MakeTestSnapWithFiles(c, snapdSnap, nil)
	si := &snap.SideInfo{RealName: "snapd"}

	st := s.o.State()
	st.Lock()

	// we must be seeded
	st.Set("seeded", true)

	// add the test snap service
	testSnapSideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	snapstate.Set(st, "test-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{testSnapSideInfo}),
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	})
	snaptest.MockSnapWithFiles(c, `name: test-snap
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple
`, testSnapSideInfo, nil)

	// add the snap service unit with Requires=
	unitTempl := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application test-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
%[3]s=usr-lib-snapd.mount
After=usr-lib-snapd.mount
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run test-snap.svc1
SyslogIdentifier=test-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/test-snap/42
TimeoutStopSec=30
Type=simple

[Install]
WantedBy=multi-user.target
`

	initialUnitFile := fmt.Sprintf(unitTempl,
		systemd.EscapeUnitNamePath(filepath.Join(dirs.SnapMountDir, "test-snap", "42.mount")),
		dirs.GlobalRootDir,
		"Requires",
	)

	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(dirs.SnapServicesDir, "snap.test-snap.svc1.service"), []byte(initialUnitFile), 0644)
	c.Assert(err, IsNil)

	// we also need to setup the usr-lib-snapd.mount file too
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = os.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	// the modification time of the usr-lib-snapd.mount file is the first
	// timestamp we use, then the stop time of the snap svc, then the stop time
	// of usr-lib-snapd.mount
	t0 := time.Now()
	t1 := t0.Add(1 * time.Hour)
	t2 := t0.Add(2 * time.Hour)

	err = os.Chtimes(usrLibSnapdMountFile, t0, t0)
	c.Assert(err, IsNil)

	systemctlCalls := 0
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		systemctlCalls++

		c.Logf("call: %v", systemctlCalls)
		switch systemctlCalls {
		// first 3 calls are for the snapd refresh itself
		case 1:
			c.Check(cmd, DeepEquals, []string{"daemon-reload"})
			return nil, nil
		case 2:
			c.Check(cmd, DeepEquals, []string{"--no-reload", "enable", "snap-snapd-x1.mount"})
			return nil, nil
		case 3:
			c.Check(cmd, DeepEquals, []string{"reload-or-restart", "snap-snapd-x1.mount"})
			return nil, nil
			// next we get the calls for the rewritten service files after snapd
			// restarts
		case 4:
			c.Check(cmd, DeepEquals, []string{"daemon-reload"})
			return nil, nil
		case 5:
			c.Check(cmd, DeepEquals, []string{"--no-reload", "enable", "usr-lib-snapd.mount"})
			return nil, nil
		case 6:
			c.Check(cmd, DeepEquals, []string{"stop", "usr-lib-snapd.mount"})
			return nil, nil
		case 7:
			c.Check(cmd, DeepEquals, []string{"show", "--property=ActiveState", "usr-lib-snapd.mount"})
			return []byte("ActiveState=inactive"), nil
		case 8:
			c.Check(cmd, DeepEquals, []string{"start", "usr-lib-snapd.mount"})
			return nil, nil
		case 9:
			c.Check(cmd, DeepEquals, []string{"daemon-reload"})
			return nil, nil
		case 10:
			c.Check(cmd, DeepEquals, []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"})
			return []byte("InactiveEnterTimestamp=" + t2.Format("Mon 2006-01-02 15:04:05 MST")), nil
		case 11:
			c.Check(cmd, DeepEquals, []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"})
			return []byte("InactiveEnterTimestamp=" + t1.Format("Mon 2006-01-02 15:04:05 MST")), nil
		case 12:
			c.Check(cmd, DeepEquals, []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"})
			return []byte(`Type=notify
Id=snap.test-snap.svc1.service
Names=snap.test-snap.svc1.service
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no
`), nil
		case 13:
			c.Check(cmd, DeepEquals, []string{"start", "snap.test-snap.svc1.service"})
			return nil, nil
		default:
			c.Errorf("unexpected call to systemctl: %+v", cmd)
			return nil, fmt.Errorf("broken test")
		}
	})
	s.AddCleanup(r)
	// make sure that we get the expected number of systemctl calls
	s.AddCleanup(func() { c.Assert(systemctlCalls, Equals, 13) })

	// also add the snapd snap to state which we will refresh
	si1 := &snap.SideInfo{RealName: "snapd", Revision: snap.R(1)}
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		SnapType: "snapd",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, "name: snapd\ntype: snapd\nversion: 123", si1, nil)

	// setup model assertion
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	// model := s.brands.Model("my-brand", "my-model", modelDefaults)
	model := s.brands.Model("my-brand", "my-model", map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
		"base":         "core18",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	ts, _, err := snapstate.InstallPath(st, si, snapPath, "", "", snapstate.Flags{}, nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	// make sure we don't try to ensure snap services before the restart
	r = servicestate.MockEnsuredSnapServices(s.o.ServiceManager(), true)
	defer r()

	// run, this will trigger wait for restart
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// check the snapd task state
	c.Check(chg.Status(), Equals, state.DoingStatus)
	restarting, kind := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartDaemon)

	// now we do want the ensure loop to run though
	r = servicestate.MockEnsuredSnapServices(s.o.ServiceManager(), false)
	defer r()

	// mock a restart of snapd to progress with the change
	restart.MockPending(st, restart.RestartUnset)

	// let the change run its course
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	defer st.Unlock()
	c.Assert(err, IsNil)

	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("change failed: %v", chg.Err()))

	// we don't restart since the unit file was just rewritten, no services were
	// killed
	restarting, _ = restart.Pending(st)
	c.Check(restarting, Equals, false)

	// the unit file was rewritten to use Wants= now
	rewrittenUnitFile := fmt.Sprintf(unitTempl,
		systemd.EscapeUnitNamePath(filepath.Join(dirs.SnapMountDir, "test-snap", "42.mount")),
		dirs.GlobalRootDir,
		"Wants",
	)
	c.Assert(filepath.Join(dirs.SnapServicesDir, "snap.test-snap.svc1.service"), testutil.FileEquals, rewrittenUnitFile)
}

func (s *mgrsSuite) TestUC18SnapdRefreshUpdatesSnapServiceUnitsAndAttemptsToRestartsKilledUnitsButFails(c *C) {
	restore := release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()
	// reload directories
	dirs.SetRootDir(dirs.GlobalRootDir)
	restore = release.MockOnClassic(false)
	defer restore()
	bl := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	const snapdSnap = `
name: snapd
version: 1.0
type: snapd`
	snapPath := snaptest.MakeTestSnapWithFiles(c, snapdSnap, nil)
	si := &snap.SideInfo{RealName: "snapd"}

	st := s.o.State()
	st.Lock()

	// we must be seeded
	st.Set("seeded", true)

	// add the test snap service
	testSnapSideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	snapstate.Set(st, "test-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{testSnapSideInfo}),
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	})
	snaptest.MockSnapWithFiles(c, `name: test-snap
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple
`, testSnapSideInfo, nil)

	// add the snap service unit with Requires=
	unitTempl := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application test-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
%[3]s=usr-lib-snapd.mount
After=usr-lib-snapd.mount
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run test-snap.svc1
SyslogIdentifier=test-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/test-snap/42
TimeoutStopSec=30
Type=simple

[Install]
WantedBy=multi-user.target
`

	initialUnitFile := fmt.Sprintf(unitTempl,
		systemd.EscapeUnitNamePath(filepath.Join(dirs.SnapMountDir, "test-snap", "42.mount")),
		dirs.GlobalRootDir,
		"Requires",
	)

	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(dirs.SnapServicesDir, "snap.test-snap.svc1.service"), []byte(initialUnitFile), 0644)
	c.Assert(err, IsNil)

	// we also need to setup the usr-lib-snapd.mount file too
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = os.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	// the modification time of the usr-lib-snapd.mount file is the first
	// timestamp we use, then the stop time of the snap svc, then the stop time
	// of usr-lib-snapd.mount
	t0 := time.Now()
	t1 := t0.Add(1 * time.Hour)
	t2 := t0.Add(2 * time.Hour)

	err = os.Chtimes(usrLibSnapdMountFile, t0, t0)
	c.Assert(err, IsNil)

	systemctlCalls := 0
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		systemctlCalls++

		switch systemctlCalls {
		// first 3 calls are for the snapd refresh itself
		case 1:
			c.Check(cmd, DeepEquals, []string{"daemon-reload"})
			return nil, nil
		case 2:
			c.Check(cmd, DeepEquals, []string{"--no-reload", "enable", "snap-snapd-x1.mount"})
			return nil, nil
		case 3:
			c.Check(cmd, DeepEquals, []string{"reload-or-restart", "snap-snapd-x1.mount"})
			return nil, nil
			// next we get the calls for the rewritten service files after snapd
			// restarts
		case 4:
			c.Check(cmd, DeepEquals, []string{"daemon-reload"})
			return nil, nil
		case 5:
			c.Check(cmd, DeepEquals, []string{"--no-reload", "enable", "usr-lib-snapd.mount"})
			return nil, nil
		case 6:
			c.Check(cmd, DeepEquals, []string{"stop", "usr-lib-snapd.mount"})
			return nil, nil
		case 7:
			c.Check(cmd, DeepEquals, []string{"show", "--property=ActiveState", "usr-lib-snapd.mount"})
			return []byte("ActiveState=inactive"), nil
		case 8:
			c.Check(cmd, DeepEquals, []string{"start", "usr-lib-snapd.mount"})
			return nil, nil
		case 9:
			c.Check(cmd, DeepEquals, []string{"daemon-reload"})
			return nil, nil
		case 10:
			c.Check(cmd, DeepEquals, []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"})
			return []byte("InactiveEnterTimestamp=" + t2.Format("Mon 2006-01-02 15:04:05 MST")), nil
		case 11:
			c.Check(cmd, DeepEquals, []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"})
			return []byte("InactiveEnterTimestamp=" + t1.Format("Mon 2006-01-02 15:04:05 MST")), nil
		case 12:
			c.Check(cmd, DeepEquals, []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"})
			return []byte(`Type=notify
Id=snap.test-snap.svc1.service
Names=snap.test-snap.svc1.service
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no
`), nil
		case 13:
			// starting the snap fails
			c.Check(cmd, DeepEquals, []string{"start", "snap.test-snap.svc1.service"})
			return nil, fmt.Errorf("the snap service is having a bad day")
		case 14:
			// because starting the snap fails, we will automatically try to
			// undo the starting of the snap by stopping it, hence the request
			// to stop it
			// TODO: is this desirable? in the field, what if stopping the
			// service also dies?
			c.Check(cmd, DeepEquals, []string{"stop", "snap.test-snap.svc1.service"})
			return nil, fmt.Errorf("the snap service is still having a bad day")
		case 15:
			c.Check(cmd, DeepEquals, []string{"show", "--property=ActiveState", "snap.test-snap.svc1.service"})
			return nil, nil
		default:
			c.Errorf("unexpected call to systemctl: %+v", cmd)
			return nil, fmt.Errorf("broken test")
		}
	})
	s.AddCleanup(r)
	// make sure that we get the expected number of systemctl calls
	s.AddCleanup(func() { c.Assert(systemctlCalls, Equals, 15) })

	// also add the snapd snap to state which we will refresh
	si1 := &snap.SideInfo{RealName: "snapd", Revision: snap.R(1)}
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		SnapType: "snapd",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, "name: snapd\ntype: snapd\nversion: 123", si1, nil)

	// setup model assertion
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	// model := s.brands.Model("my-brand", "my-model", modelDefaults)
	model := s.brands.Model("my-brand", "my-model", map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
		"base":         "core18",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	ts, _, err := snapstate.InstallPath(st, si, snapPath, "", "", snapstate.Flags{}, nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	// make sure we don't try to ensure snap services before the restart
	r = servicestate.MockEnsuredSnapServices(s.o.ServiceManager(), true)
	defer r()

	// run, this will trigger wait for restart
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// check the snapd task state
	c.Check(chg.Status(), Equals, state.DoingStatus)
	restarting, kind := restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartDaemon)

	// now we do want the ensure loop to run though
	r = servicestate.MockEnsuredSnapServices(s.o.ServiceManager(), false)
	defer r()

	// mock a restart of snapd to progress with the change
	restart.MockPending(st, restart.RestartUnset)

	// let the change try to run its course
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, ErrorMatches, `state ensure errors: \[error trying to restart killed services, immediately rebooting: the snap service is having a bad day\]`)

	// the change is still in doing status
	c.Check(chg.Status(), Equals, state.DoingStatus)

	// we do end up restarting now, since we tried to restart the service but
	// failed and so to be safe as possible we reboot the system immediately
	restarting, kind = restart.Pending(st)
	c.Check(restarting, Equals, true)
	c.Assert(kind, Equals, restart.RestartSystemNow)

	// the unit file was rewritten to use Wants= now
	rewrittenUnitFile := fmt.Sprintf(unitTempl,
		systemd.EscapeUnitNamePath(filepath.Join(dirs.SnapMountDir, "test-snap", "42.mount")),
		dirs.GlobalRootDir,
		"Wants",
	)
	c.Assert(filepath.Join(dirs.SnapServicesDir, "snap.test-snap.svc1.service"), testutil.FileEquals, rewrittenUnitFile)

	// simulate a final restart to demonstrate that the change still finishes
	// properly - note that this isn't a fully honest test, since in reality it
	// should be done with a real overlord.RestartBehavior implemented like what
	// daemon actually provides and here we are using nil, but for the purposes
	// of this test it's enough to ensure that 1) a restart is requested and 2)
	// the manager ensure loop doesn't fail after we restart since the unit
	// files don't need to be rewritten
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)

	// we want the service ensure loop to run again to show it doesn't break
	// anything
	r = servicestate.MockEnsuredSnapServices(s.o.ServiceManager(), false)
	defer r()

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	defer st.Unlock()
	c.Assert(err, IsNil)

	// the change is now fully done
	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (s *mgrsSuite) testUC20RunUpdateManagedBootConfig(c *C, snapPath string, si *snap.SideInfo, bl bootloader.Bootloader, updated bool) {
	restore := release.MockOnClassic(false)
	defer restore()

	// pretend we booted with the right kernel
	bl.SetBootVars(map[string]string{"snap_kernel": "pc-kernel_1.snap"})

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

	// mock the modeenv file
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191127",
		Base:           "core20_1.snap",
		CurrentKernelCommandLines: []string{
			// We expect bl to be a mock bootloader with no default command line"
			"snapd_recovery_mode=run",
		},
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)
	c.Assert(s.o.DeviceManager().ReloadModeenv(), IsNil)

	st := s.o.State()
	st.Lock()
	// defer st.Unlock()
	st.Set("seeded", true)

	si1 := &snap.SideInfo{RealName: "snapd", Revision: snap.R(1)}
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		SnapType: "snapd",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, "name: snapd\ntype: snapd\nversion: 123", si1, nil)

	si2 := &snap.SideInfo{RealName: "core20", Revision: snap.R(1)}
	snapstate.Set(st, "core20", &snapstate.SnapState{
		SnapType: "base",

		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  si2.Revision,
	})
	si3 := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si3}),
		Current:  si3.Revision,
	})
	si4 := &snap.SideInfo{RealName: "pc", Revision: snap.R(1)}
	snapstate.Set(st, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si4}),
		Current:  si4.Revision,
	})
	const pcGadget = `
name: pc
type: gadget
`
	const pcGadgetYaml = `
volumes:
  pc:
    bootloader: grub
`
	snaptest.MockSnapWithFiles(c, pcGadget, si4, [][]string{
		{"meta/gadget.yaml", pcGadgetYaml},
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

	// InstallPath does not set any restart boundaries by itself, this is something
	// that must be handled where we use it, and actually schedule the change.
	ts, _, err := snapstate.InstallPath(st, si, snapPath, "", "", snapstate.Flags{}, nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	// run, this will trigger wait for restart with snapd snap (or be done
	// with core)
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	if si.RealName == "core" {
		// core on UC20 is done at this point
		c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("failed: %v", chg.Err()))
		c.Assert(chg.Err(), IsNil)
	} else {
		// boot config is updated after link-snap, so first comes the
		// daemon restart
		c.Check(chg.Status(), Equals, state.DoingStatus)
		restarting, kind := restart.Pending(st)
		c.Check(restarting, Equals, true)
		c.Assert(kind, Equals, restart.RestartDaemon)

		// simulate successful daemon restart happened
		restart.MockPending(st, restart.RestartUnset)
		restart.MockAfterRestartForChange(chg)

		// let the change run its course
		st.Unlock()
		err = s.o.Settle(settleTimeout)
		st.Lock()
		c.Assert(err, IsNil)

		restarting, kind = restart.Pending(st)
		if updated {
			c.Check(chg.Status(), Equals, state.WaitStatus, Commentf("change failed: %v", chg.Err()))
			// boot config updated, thus a system restart was
			// requested
			c.Check(restarting, Equals, true)
			c.Assert(kind, Equals, restart.RestartSystem)
		} else {
			c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("change failed: %v", chg.Err()))
			c.Check(restarting, Equals, false)
		}
	}
}

func (s *mgrsSuite) TestUC20SnapdUpdatesManagedBootConfig(c *C) {
	mabloader := bootloadertest.Mock("mock", c.MkDir()).WithTrustedAssets()
	bootloader.Force(mabloader)
	defer bootloader.Force(nil)

	mabloader.Updated = true

	const snapdSnap = `
name: snapd
version: 1.0
type: snapd`
	snapPath := snaptest.MakeTestSnapWithFiles(c, snapdSnap, nil)
	si := &snap.SideInfo{RealName: "snapd"}

	const updated = true
	s.testUC20RunUpdateManagedBootConfig(c, snapPath, si, mabloader, updated)

	c.Check(mabloader.UpdateCalls, Equals, 1)
}

func (s *mgrsSuite) TestUC20SnapdUpdateManagedBootNotNeededConfig(c *C) {
	mabloader := bootloadertest.Mock("mock", c.MkDir()).WithTrustedAssets()
	bootloader.Force(mabloader)
	defer bootloader.Force(nil)

	// nothing was updated, eg. boot config editions are the same
	mabloader.Updated = false

	const snapdSnap = `
name: snapd
version: 1.0
type: snapd`
	snapPath := snaptest.MakeTestSnapWithFiles(c, snapdSnap, nil)
	si := &snap.SideInfo{RealName: "snapd"}

	const updated = false
	s.testUC20RunUpdateManagedBootConfig(c, snapPath, si, mabloader, updated)

	c.Check(mabloader.UpdateCalls, Equals, 1)
}

func (s *mgrsSuite) TestUC20CoreDoesNotUpdateManagedBootConfig(c *C) {
	mabloader := bootloadertest.Mock("mock", c.MkDir()).WithTrustedAssets()
	bootloader.Force(mabloader)
	defer bootloader.Force(nil)

	const coreSnap = `
name: core
version: 1.0
type: base`
	snapPath := snaptest.MakeTestSnapWithFiles(c, coreSnap, nil)
	si := &snap.SideInfo{RealName: "core"}

	const updated = false
	s.testUC20RunUpdateManagedBootConfig(c, snapPath, si, mabloader, updated)
	c.Check(mabloader.UpdateCalls, Equals, 0)
}

func (s *mgrsSuite) testNonUC20RunUpdateManagedBootConfig(c *C, snapPath string, si *snap.SideInfo, bl bootloader.Bootloader) {
	// non UC20 device model

	restore := release.MockOnClassic(false)
	defer restore()

	// pretend we booted with the right kernel & base
	bl.SetBootVars(map[string]string{
		"snap_core":   "core_1.snap",
		"snap_kernel": "pc-kernel_1.snap",
	})

	model := s.brands.Model("my-brand", "my-model", modelDefaults)

	st := s.o.State()
	st.Lock()
	// defer st.Unlock()
	st.Set("seeded", true)

	si1 := &snap.SideInfo{RealName: "snapd", Revision: snap.R(1)}
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		SnapType: "snapd",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
	})
	si2 := &snap.SideInfo{RealName: "core", Revision: snap.R(1)}
	snapstate.Set(st, "core", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  si2.Revision,
	})
	si3 := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si3}),
		Current:  si3.Revision,
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

	// InstallPath does not set any restart boundaries by itself, this is something
	// that must be handled where we use it, and actually schedule the change.
	ts, _, err := snapstate.InstallPath(st, si, snapPath, "", "", snapstate.Flags{}, nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	// run, this will trigger a wait for the restart
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	restarting, restartType := restart.Pending(st)
	switch restartType {
	case restart.RestartDaemon:
		c.Check(chg.Status(), Equals, state.DoingStatus)
	default:
		c.Check(chg.Status(), Equals, state.WaitStatus)
	}
	c.Check(restarting, Equals, true)

	// simulate successful restart happened
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	if si.RealName == "core" {
		// pretend we switched to a new core
		bl.SetBootVars(map[string]string{
			"snap_core":   "core_x1.snap",
			"snap_kernel": "pc-kernel_1.snap",
		})
	}

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)
}

func (s *mgrsSuite) TestNonUC20DoesNotUpdateManagedBootConfig(c *C) {
	mabloader := bootloadertest.Mock("mock", c.MkDir()).WithTrustedAssets()
	bootloader.Force(mabloader)
	defer bootloader.Force(nil)

	const coreSnap = `
name: core
version: 1.0
type: base`
	snapPath := snaptest.MakeTestSnapWithFiles(c, coreSnap, nil)
	si := &snap.SideInfo{RealName: "core"}

	s.testNonUC20RunUpdateManagedBootConfig(c, snapPath, si, mabloader)
	c.Check(mabloader.UpdateCalls, Equals, 0)
}

func (s *mgrsSuite) TestNonUC20SnapdNoUpdateNotManagedBootConfig(c *C) {
	mabloader := bootloadertest.Mock("mock", c.MkDir()).WithTrustedAssets()
	bootloader.Force(mabloader)
	defer bootloader.Force(nil)

	const snapdSnap = `
name: snapd
version: 1.0
type: snapd`
	snapPath := snaptest.MakeTestSnapWithFiles(c, snapdSnap, nil)
	si := &snap.SideInfo{RealName: "snapd"}

	s.testNonUC20RunUpdateManagedBootConfig(c, snapPath, si, mabloader)
	c.Check(mabloader.UpdateCalls, Equals, 0)
}

const pcGadget = `
name: pc
version: 1.0
type: gadget
`
const pcGadgetYaml = `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: ubuntu-seed
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        role: system-seed
        filesystem: vfat
        size: 100M
      - name: ubuntu-boot
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        role: system-boot
        filesystem: ext4
        size: 100M
      - name: ubuntu-data
        role: system-data
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        filesystem: ext4
        size: 500M
`

const pcGadgetTrack22Yaml = `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: ubuntu-seed
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        role: system-seed
        filesystem: vfat
        size: 100M
        content:
          - source: grubx64.efi
            target: grubx64.efi
      - name: ubuntu-boot
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        role: system-boot
        filesystem: ext4
        size: 100M
      - name: ubuntu-data
        role: system-data
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        filesystem: ext4
        size: 500M
`

func (s *mgrsSuiteCore) testGadgetKernelCommandLine(c *C, gadgetPath string, gadgetSideInfo *snap.SideInfo,
	bl bootloader.Bootloader, currentFiles [][]string, currentModeenvCmdline string,
	commandLineAfterReboot string, update bool) {
	restore := release.MockOnClassic(false)
	defer restore()

	cmdlineAfterRebootPath := filepath.Join(c.MkDir(), "mock-cmdline")
	c.Assert(os.WriteFile(cmdlineAfterRebootPath, []byte(commandLineAfterReboot), 0644), IsNil)

	// pretend we booted with the right kernel
	bl.SetBootVars(map[string]string{"snap_kernel": "pc-kernel_1.snap"})

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

	// mock the modeenv file
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191127",
		Base:           "core20_1.snap",
		// leave this line to keep gofmt 1.10 happy
		CurrentKernelCommandLines: []string{currentModeenvCmdline},
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)
	c.Assert(s.o.DeviceManager().ReloadModeenv(), IsNil)

	st := s.o.State()
	st.Lock()
	st.Set("seeded", true)

	si1 := &snap.SideInfo{RealName: "snapd", Revision: snap.R(1)}
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		SnapType: "snapd",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
	})
	snaptest.MockSnapWithFiles(c, "name: snapd\ntype: snapd\nversion: 123", si1, nil)

	si2 := &snap.SideInfo{RealName: "core20", Revision: snap.R(1)}
	snapstate.Set(st, "core20", &snapstate.SnapState{
		SnapType: "base",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  si2.Revision,
	})
	si3 := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si3}),
		Current:  si3.Revision,
	})
	si4 := &snap.SideInfo{RealName: "pc", Revision: snap.R(1)}
	snapstate.Set(st, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si4}),
		Current:  si4.Revision,
	})
	snaptest.MockSnapWithFiles(c, pcGadget, si4, currentFiles)

	// setup model assertion
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{"pc": {}},
			map[string]map[int]*gadget.OnDiskStructure{
				"pc": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
			},
			nil
	})
	defer r()

	ts, _, err := snapstate.InstallPath(st, gadgetSideInfo, gadgetPath, "", "", snapstate.Flags{}, nil)
	c.Assert(err, IsNil)

	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	if update {
		// after link-snap, a system restart will be requested
		c.Check(chg.Status(), Equals, state.WaitStatus, Commentf("change failed: %v", chg.Err()))
		restarting, kind := restart.Pending(st)
		c.Check(restarting, Equals, true)
		c.Assert(kind, Equals, restart.RestartSystem)

		// simulate successful system restart happened
		s.mockRestartAndSettle(c, st, chg)

		// after restart, the change should be done
		c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("change failed: %v", chg.Err()))

		m, err := boot.ReadModeenv("")
		c.Assert(err, IsNil)
		// old and pending command line
		c.Assert(m.CurrentKernelCommandLines, HasLen, 2)

		restore := kcmdline.MockProcCmdline(cmdlineAfterRebootPath)
		defer restore()

		// reset bootstate, so that after-reboot command line is
		// asserted
		st.Unlock()
		s.o.DeviceManager().ResetToPostBootState()
		err = s.o.DeviceManager().Ensure()
		st.Lock()
		c.Assert(err, IsNil)

		// let the change run its course
		st.Unlock()
		err = s.o.Settle(settleTimeout)
		st.Lock()
		c.Assert(err, IsNil)
	}

	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("change failed: %v", chg.Err()))
}

func (s *mgrsSuiteCore) TestGadgetKernelCommandLineAddCmdline(c *C) {
	mabloader := bootloadertest.Mock("mock", c.MkDir()).WithTrustedAssets()
	mabloader.StaticCommandLine = "mock static"
	bootloader.Force(mabloader)
	defer bootloader.Force(nil)

	err := mabloader.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "",
	})
	c.Assert(err, IsNil)

	// add new gadget snap kernel command line drop-in file
	sf := snaptest.MakeTestSnapWithFiles(c, pcGadget, [][]string{
		{"meta/gadget.yaml", pcGadgetYaml},
		{"cmdline.extra", "args from gadget"},
	})

	const currentCmdline = "snapd_recovery_mode=run mock static"
	const update = true
	currentFiles := [][]string{{"meta/gadget.yaml", pcGadgetYaml}}
	const cmdlineAfterReboot = "snapd_recovery_mode=run mock static args from gadget"
	s.testGadgetKernelCommandLine(c, sf, &snap.SideInfo{RealName: "pc"}, mabloader,
		currentFiles, currentCmdline, cmdlineAfterReboot, update)

	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run mock static args from gadget",
	})
	vars, err := mabloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "mock static args from gadget",
	})
}

func (s *mgrsSuiteCore) TestGadgetKernelCommandLineRemoveCmdline(c *C) {
	mabloader := bootloadertest.Mock("mock", c.MkDir()).WithTrustedAssets()
	mabloader.StaticCommandLine = "mock static"
	bootloader.Force(mabloader)
	defer bootloader.Force(nil)

	err := mabloader.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "args from gadget",
		"snapd_full_cmdline_args":  "",
	})
	c.Assert(err, IsNil)

	// current gadget has the command line
	currentFiles := [][]string{
		{"meta/gadget.yaml", pcGadgetYaml},
		{"cmdline.extra", "args from old gadget"},
	}
	// add new gadget snap kernel command line without the file
	sf := snaptest.MakeTestSnapWithFiles(c, pcGadget, [][]string{
		{"meta/gadget.yaml", pcGadgetYaml},
	})

	const currentCmdline = "snapd_recovery_mode=run mock static args from old gadget"
	const update = true
	const cmdlineAfterReboot = "snapd_recovery_mode=run mock static"
	s.testGadgetKernelCommandLine(c, sf, &snap.SideInfo{RealName: "pc"}, mabloader,
		currentFiles, currentCmdline, cmdlineAfterReboot, update)

	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run mock static",
	})
	vars, err := mabloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "mock static",
	})
}

func (s *mgrsSuiteCore) TestGadgetKernelCommandLineNoChange(c *C) {
	mabloader := bootloadertest.Mock("mock", c.MkDir()).WithTrustedAssets()
	mabloader.StaticCommandLine = "mock static"
	bootloader.Force(mabloader)
	defer bootloader.Force(nil)

	err := mabloader.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "args from gadget",
		"snapd_full_cmdline_args":  "",
	})
	c.Assert(err, IsNil)
	// current gadget has the command line
	currentFiles := [][]string{
		{"meta/gadget.yaml", pcGadgetYaml},
		{"cmdline.extra", "args from gadget"},
	}
	// add new gadget snap kernel command line drop-in file
	sf := snaptest.MakeTestSnapWithFiles(c, pcGadget, [][]string{
		{"meta/gadget.yaml", pcGadgetYaml},
		{"cmdline.extra", "args from gadget"},
	})

	const currentCmdline = "snapd_recovery_mode=run mock static args from gadget"
	const update = false
	const cmdlineAfterReboot = "snapd_recovery_mode=run mock static args from gadget"
	s.testGadgetKernelCommandLine(c, sf, &snap.SideInfo{RealName: "pc"}, mabloader,
		currentFiles, currentCmdline, cmdlineAfterReboot, update)

	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run mock static args from gadget",
	})
	// bootenv is unchanged
	vars, err := mabloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "args from gadget",
		"snapd_full_cmdline_args":  "",
	})
}

func (s *mgrsSuiteCore) TestGadgetKernelCommandLineTransitionExtraToFull(c *C) {
	mabloader := bootloadertest.Mock("mock", c.MkDir()).WithTrustedAssets()
	mabloader.StaticCommandLine = "mock static"
	bootloader.Force(mabloader)
	defer bootloader.Force(nil)

	err := mabloader.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "extra args",
		"snapd_full_cmdline_args":  "",
	})
	c.Assert(err, IsNil)

	// add new gadget snap kernel command line drop-in file
	sf := snaptest.MakeTestSnapWithFiles(c, pcGadget, [][]string{
		{"meta/gadget.yaml", pcGadgetYaml},
		{"cmdline.full", "full args"},
	})

	const currentCmdline = "snapd_recovery_mode=run mock static extra args"
	const update = true
	currentFiles := [][]string{
		{"meta/gadget.yaml", pcGadgetYaml},
		{"cmdline.extra", "extra args"},
	}
	const cmdlineAfterReboot = "snapd_recovery_mode=run full args"
	s.testGadgetKernelCommandLine(c, sf, &snap.SideInfo{RealName: "pc"}, mabloader,
		currentFiles, currentCmdline, cmdlineAfterReboot, update)

	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run full args",
	})
	vars, err := mabloader.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "full args",
	})
}

func (s *mgrsSuiteCore) testUpdateKernelBaseSingleRebootSetup(c *C) (*boottest.RunBootenv20, *state.Change) {
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	s.AddCleanup(func() { bootloader.Force(nil) })

	// a revision which is assumed to be installed
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	restore := bloader.SetEnabledKernel(kernel)
	s.AddCleanup(restore)

	restore = release.MockOnClassic(false)
	s.AddCleanup(restore)

	mockServer := s.mockStore(c)
	s.AddCleanup(func() { mockServer.Close() })

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	model := s.brands.Model("can0nical", "my-model", uc20ModelDefaults)
	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// mock the modeenv file
	m := &boot.Modeenv{
		Mode:                   "run",
		Base:                   "core20_1.snap",
		CurrentKernels:         []string{"pc-kernel_1.snap"},
		CurrentRecoverySystems: []string{"1234"},
		GoodRecoverySystems:    []string{"1234"},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)
	c.Assert(s.o.DeviceManager().ReloadModeenv(), IsNil)

	pcKernelYaml := "name: pc-kernel\nversion: 1.0\ntype: kernel"
	baseYaml := "name: core20\nversion: 1.0\ntype: base"
	siKernel := &snap.SideInfo{RealName: "pc-kernel", SnapID: fakeSnapID("pc-kernel"), Revision: snap.R(1)}
	snaptest.MockSnap(c, pcKernelYaml, siKernel)
	siBase := &snap.SideInfo{RealName: "core20", SnapID: fakeSnapID("core20"), Revision: snap.R(1)}
	snaptest.MockSnap(c, baseYaml, siBase)
	snapYamlContent := "name: some-snap\nversion: 1.0\nbase: core20"
	siSnap := &snap.SideInfo{RealName: "some-snap", SnapID: fakeSnapID("some-snap"), Revision: snap.R(1)}
	snaptest.MockSnap(c, snapYamlContent, siSnap)
	siSnapd := &snap.SideInfo{RealName: "snapd", Revision: snap.R(1)}
	snaptest.MockSnapWithFiles(c, "name: snapd\ntype: snapd\nversion: 123", siSnapd, nil)

	// test setup adds core, get rid of it
	snapstate.Set(st, "core", nil)
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siKernel}),
		Current:  snap.R(1),
		SnapType: "kernel",
	})
	snapstate.Set(st, "core20", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siBase}),
		Current:  snap.R(1),
		SnapType: "base",
	})
	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siSnap}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siSnapd}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})

	p, _ := s.makeStoreTestSnap(c, pcKernelYaml, "2")
	s.serveSnap(p, "2")
	p, _ = s.makeStoreTestSnap(c, baseYaml, "2")
	s.serveSnap(p, "2")
	p, _ = s.makeStoreTestSnap(c, snapYamlContent, "2")
	s.serveSnap(p, "2")

	affected, tss, err := snapstate.UpdateMany(context.Background(), st, []string{"pc-kernel", "core20", "some-snap"}, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(affected, DeepEquals, []string{"core20", "pc-kernel", "some-snap"})
	chg := st.NewChange("update-many", "...")
	for _, ts := range tss {
		// skip the taskset of UpdateMany that does the
		// check-rerefresh, see tsWithoutReRefresh for details
		if ts.Tasks()[0].Kind() == "check-rerefresh" {
			c.Logf("skipping rerefresh")
			continue
		}
		chg.AddAll(ts)
	}
	return bloader, chg
}

func (s *mgrsSuiteCore) TestUpdateKernelBaseSingleRebootHappy(c *C) {
	bloader, chg := s.testUpdateKernelBaseSingleRebootSetup(c)
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	st.Unlock()
	err := s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))

	// final steps will are postponed until we are in the restarted snapd
	ok, rst := restart.Pending(st)
	c.Assert(ok, Equals, true)
	c.Assert(rst, Equals, restart.RestartSystem)

	// auto connects aren't done yet
	autoConnects := 0
	for _, tsk := range chg.Tasks() {
		if tsk.Kind() == "auto-connect" {
			expectedStatus := state.DoStatus
			snapsup, err := snapstate.TaskSnapSetup(tsk)
			c.Assert(err, IsNil)
			switch snapsup.InstanceName() {
			case "core20", "pc-kernel":
				expectedStatus = state.DoStatus
			}
			c.Assert(tsk.Status(), Equals, expectedStatus,
				Commentf("%q has status other than %s", tsk.Summary(), expectedStatus))
			autoConnects++
		}
	}
	// one for kernel, one for base, one for some-snap
	c.Check(autoConnects, Equals, 3)

	// try snaps are set
	currentTryKernel, err := bloader.TryKernel()
	c.Assert(err, IsNil)
	c.Assert(currentTryKernel.Filename(), Equals, "pc-kernel_2.snap")
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.BaseStatus, Equals, boot.TryStatus)
	c.Check(m.TryBase, Equals, "core20_2.snap")

	// simulate successful restart happened
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	err = bloader.SetTryingDuringReboot([]snap.Type{snap.TypeKernel})
	c.Assert(err, IsNil)
	m.BaseStatus = boot.TryingStatus
	c.Assert(m.Write(), IsNil)
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// go on
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("change failed with: %v", chg.Err()))
}

func (s *mgrsSuiteCore) TestUpdateKernelBaseSingleRebootKernelUndo(c *C) {
	bloader, chg := s.testUpdateKernelBaseSingleRebootSetup(c)
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	st.Unlock()
	err := s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))

	// final steps will are postponed until we are in the restarted snapd
	ok, rst := restart.Pending(st)
	c.Assert(ok, Equals, true)
	c.Assert(rst, Equals, restart.RestartSystem)

	// auto connects aren't done yet
	autoConnects := 0
	for _, tsk := range chg.Tasks() {
		if tsk.Kind() == "auto-connect" {
			expectedStatus := state.DoStatus
			snapsup, err := snapstate.TaskSnapSetup(tsk)
			c.Assert(err, IsNil)
			switch snapsup.InstanceName() {
			case "core20", "pc-kernel":
				expectedStatus = state.DoStatus
			}
			c.Assert(tsk.Status(), Equals, expectedStatus,
				Commentf("%q has status other than %s", tsk.Summary(), expectedStatus))
			autoConnects++
		}
	}
	// one for kernel, one for base, one for some-snap
	c.Check(autoConnects, Equals, 3)

	// try snaps are set
	currentTryKernel, err := bloader.TryKernel()
	c.Assert(err, IsNil)
	c.Assert(currentTryKernel.Filename(), Equals, "pc-kernel_2.snap")
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.BaseStatus, Equals, boot.TryStatus)
	c.Check(m.TryBase, Equals, "core20_2.snap")

	// simulate successful restart happened
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	// pretend the kernel panics during boot, kernel status gets reset to ""
	err = bloader.SetRollbackAcrossReboot([]snap.Type{snap.TypeKernel})
	c.Assert(err, IsNil)
	s.o.DeviceManager().ResetToPostBootState()

	// go on
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	// devicemgr's ensure boot ok tries to launch a revert
	c.Check(err, ErrorMatches, `.*snap "pc-kernel" has "update-many" change in progress.*snap "core20" has "update-many" change in progress.*`)

	c.Assert(chg.Status(), Equals, state.ErrorStatus, Commentf("change failed with: %v", chg.Err()))
	c.Assert(chg.Err(), ErrorMatches, `(?ms).*cannot finish core20 installation, there was a rollback across reboot\)`)
	// there is no try kernel, bootloader references only the old one
	_, err = bloader.TryKernel()
	c.Assert(err, Equals, bootloader.ErrNoTryKernelRef)
	kpi, err := bloader.Kernel()
	c.Assert(err, IsNil)
	c.Assert(kpi.Filename(), Equals, "pc-kernel_1.snap")
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Assert(m.BaseStatus, Equals, "")
	c.Assert(m.TryBase, Equals, "")
	c.Assert(m.Base, Equals, "core20_1.snap")

	for _, tsk := range chg.Tasks() {
		if tsk.Kind() == "link-snap" {
			snapsup, err := snapstate.TaskSnapSetup(tsk)
			c.Assert(err, IsNil)
			if snapsup.SnapName() == "some-snap" {
				// some-snap is only installed after base and
				// kernel, since we aborted at that stage, it
				// will be in the held status
				c.Assert(tsk.Status(), Equals, state.HoldStatus,
					Commentf("%q has status other than held", tsk.Summary()))
			} else {
				// link-snap of kernel and base are undone
				c.Assert(tsk.Status(), Equals, state.UndoneStatus,
					Commentf("%q has status other than undone", tsk.Summary()))
			}
		}
	}
}

func (s *mgrsSuiteCore) testUpdateKernelBaseSingleRebootWithGadgetSetup(c *C, snapYamlGadget string) (*boottest.RunBootenv20, []*state.TaskSet, *state.Change) {
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	s.AddCleanup(func() { bootloader.Force(nil) })

	// a revision which is assumed to be installed
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	restore := bloader.SetEnabledKernel(kernel)
	s.AddCleanup(restore)

	restore = release.MockOnClassic(false)
	s.AddCleanup(restore)

	mockServer := s.mockStore(c)
	s.AddCleanup(func() { mockServer.Close() })

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	model := s.brands.Model("can0nical", "my-model", uc20ModelDefaults)
	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// mock the modeenv file
	m := &boot.Modeenv{
		Mode:                   "run",
		Base:                   "core20_1.snap",
		CurrentKernels:         []string{"pc-kernel_1.snap"},
		CurrentRecoverySystems: []string{"1234"},
		GoodRecoverySystems:    []string{"1234"},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)
	c.Assert(s.o.DeviceManager().ReloadModeenv(), IsNil)

	pcKernelYaml := "name: pc-kernel\nversion: 1.0\ntype: kernel"
	baseYaml := "name: core20\nversion: 1.0\ntype: base"
	siKernel := &snap.SideInfo{RealName: "pc-kernel", SnapID: fakeSnapID("pc-kernel"), Revision: snap.R(1)}
	snaptest.MockSnap(c, pcKernelYaml, siKernel)
	siBase := &snap.SideInfo{RealName: "core20", SnapID: fakeSnapID("core20"), Revision: snap.R(1)}
	snaptest.MockSnap(c, baseYaml, siBase)
	siSnapd := &snap.SideInfo{RealName: "snapd", Revision: snap.R(1), SnapID: fakeSnapID("snapd")}
	snaptest.MockSnap(c, "name: snapd\ntype: snapd\nversion: 123", siSnapd)
	siGadget := &snap.SideInfo{RealName: "pc", Revision: snap.R(1), SnapID: fakeSnapID("pc")}
	snaptest.MockSnapWithFiles(c, snapYamlGadget, siGadget, [][]string{
		{"meta/gadget.yaml", pcGadgetYaml},
	})

	// test setup adds core, get rid of it
	snapstate.Set(st, "core", nil)
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siKernel}),
		Current:  snap.R(1),
		SnapType: "kernel",
	})
	snapstate.Set(st, "core20", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siBase}),
		Current:  snap.R(1),
		SnapType: "base",
	})
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siSnapd}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})
	snapstate.Set(st, "pc", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siGadget}),
		Current:  snap.R(1),
		SnapType: "gadget",
	})

	p, _ := s.makeStoreTestSnap(c, pcKernelYaml, "2")
	s.serveSnap(p, "2")
	p, _ = s.makeStoreTestSnap(c, baseYaml, "2")
	s.serveSnap(p, "2")
	p, _ = s.makeStoreTestSnap(c, "name: snapd\ntype: snapd\nversion: 123", "2")
	s.serveSnap(p, "2")
	p, _ = s.makeStoreTestSnapWithFiles(c, snapYamlGadget, "2", [][]string{
		{"meta/gadget.yaml", pcGadgetYaml},
	})
	s.serveSnap(p, "2")

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"pc": {
					0: {
						RootMountPoint: "/run/mnt/ubuntu-seed",
					},
					1: {
						RootMountPoint: "/run/mnt/ubuntu-boot",
					},
					2: {
						RootMountPoint: "/run/mnt/ubuntu-data",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"pc": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
			}, nil
	})
	defer r()
	s.makeMockedDisk(c, []string{"ubuntu-seed", "ubuntu-boot", "ubuntu-data"})
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/foo": gadgettest.MockExtraVolumeDiskMapping,
	})
	defer restore()

	affected, tss, err := snapstate.UpdateMany(context.Background(), st, []string{"pc-kernel", "core20", "pc", "snapd"}, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(affected, DeepEquals, []string{"core20", "pc", "pc-kernel", "snapd"})
	chg := st.NewChange("update-many", "...")
	for _, ts := range tss {
		// skip the taskset of UpdateMany that does the
		// check-rerefresh, see tsWithoutReRefresh for details
		if ts.Tasks()[0].Kind() == "check-rerefresh" {
			c.Logf("skipping rerefresh")
			continue
		}
		chg.AddAll(ts)
	}
	return bloader, tss, chg
}

// makeMockDisk mocks a disk compatible with pcGadgetYaml constant.
func (bs *baseMgrsSuite) makeMockedDisk(c *C, partNames []string) {
	const oneMeg = uint64(quantity.SizeMiB)

	// mock /dev/disk/by-label/{structureName}
	byLabelDir := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label/")
	err := os.MkdirAll(byLabelDir, 0755)
	c.Assert(err, IsNil)
	fakeDiskDeviceNode := filepath.Join(dirs.GlobalRootDir, "/dev/vda")
	fakePartDeviceNode := fakeDiskDeviceNode + "p1"
	// create fakedevice node
	err = os.WriteFile(fakePartDeviceNode, nil, 0644)
	c.Assert(err, IsNil)

	for _, partName := range partNames {
		// and point the mocked by-label entry to the fakedevice node
		err = os.Symlink(fakePartDeviceNode, filepath.Join(byLabelDir, partName))
		c.Assert(err, IsNil)

		// mock /proc/self/mountinfo with the above generated paths
		bs.AddCleanup(osutil.MockMountInfo(fmt.Sprintf("26 27 8:3 / %[1]s/run/mnt/%[2]s rw,relatime shared:7 - vfat %[1]s/dev/fakedevice0p1 rw", dirs.GlobalRootDir, partName)))

		// and mock the mount point
		err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", partName), 0755)
		c.Assert(err, IsNil)
	}

	mockDisk := &disks.MockDiskMapping{
		DevNode: fakeDiskDeviceNode,

		DevPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda",
		DevNum:  "600:1",
		// assume 34 sectors at end for GPT headers backup
		DiskUsableSectorEnd: 5120*oneMeg/512 - 34,
		DiskSizeInBytes:     5120 * oneMeg,
		SectorSizeBytes:     512,
		DiskSchema:          "gpt",
		ID:                  "f0eef013-a777-4a27-aaf0-dbb5cf68c2b6",
		Structure: []disks.Partition{
			{
				KernelDeviceNode: "/dev/vda1",
				KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda2",
				PartitionUUID:    "4b436628-71ba-43f9-aa12-76b84fe32728",
				PartitionLabel:   "ubuntu-seed",
				PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
				FilesystemUUID:   "04D6-5AE2",
				FilesystemLabel:  "ubuntu-seed",
				FilesystemType:   "vfat",
				StartInBytes:     oneMeg,
				SizeInBytes:      100 * oneMeg,
				Major:            600,
				Minor:            3,
				DiskIndex:        1,
			},
			{
				KernelDeviceNode: "/dev/vda2",
				KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda3",
				PartitionUUID:    "ade3ba65-7831-fd40-bbe2-e01c9774ed5b",
				PartitionLabel:   "ubuntu-boot",
				PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
				FilesystemUUID:   "5b3e775a-407d-4af7-aa16-b92a8b7507e6",
				FilesystemLabel:  "ubuntu-boot",
				FilesystemType:   "ext4",
				StartInBytes:     (1 + 100) * oneMeg,
				SizeInBytes:      100 * oneMeg,
				Major:            600,
				Minor:            4,
				DiskIndex:        2,
			},
			{
				KernelDeviceNode: "/dev/vda3",
				KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda5",
				PartitionUUID:    "4994f0e5-1ead-1a4d-b696-2d8cb1fa980d",
				PartitionLabel:   "ubuntu-data",
				PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
				FilesystemUUID:   "4e29a1e9-526d-48fc-a5c2-4f97e7e011e2",
				FilesystemLabel:  "ubuntu-data",
				FilesystemType:   "ext4",
				StartInBytes:     (100 + 100 + 1) * oneMeg,
				// including the last usable sector - the offset
				SizeInBytes: 500 * oneMeg,
				Major:       600,
				Minor:       6,
				DiskIndex:   3,
			},
		},
	}

	// mock device nodes
	disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		fakePartDeviceNode: mockDisk,
	})

	disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		fakeDiskDeviceNode: mockDisk,
	})
}

func (s *mgrsSuiteCore) TestUpdateKernelBaseSingleRebootWithGadgetWithExplicitBase(c *C) {
	// verify a scenario when the update contains snapd, kernel, base and
	// the gadget, in which case we revert to having at least 2 reboots due
	// to the kernel depending on the gadget and the gadget depending on the
	// base

	const pcGadget = `
name: pc
version: 1.0
type: gadget
base: core20
`
	bloader, _, chg := s.testUpdateKernelBaseSingleRebootWithGadgetSetup(c, pcGadget)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	st.Unlock()
	err := s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	c.Logf(s.logbuf.String())

	// snapd is updated first (as it's a prerequisite for the base)
	ok, rst := restart.Pending(st)
	c.Assert(ok, Equals, true)
	c.Assert(rst, Equals, restart.RestartDaemon)
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)

	autoConnectStatus := func(inDoing, inWait string, done []string) {
		autoConnectCount := 0
		for _, tsk := range chg.Tasks() {
			if tsk.Kind() == "auto-connect" {
				autoConnectCount++
				expectedStatus := state.DoStatus
				snapsup, err := snapstate.TaskSnapSetup(tsk)
				c.Assert(err, IsNil)
				if snapsup.InstanceName() == inDoing {
					expectedStatus = state.DoingStatus
				} else if snapsup.InstanceName() == inWait {
					expectedStatus = state.DoStatus
				} else if strutil.ListContains(done, snapsup.InstanceName()) {
					expectedStatus = state.DoneStatus
				}
				c.Check(tsk.Status(), Equals, expectedStatus,
					Commentf("%q has status other than %s", tsk.Summary(), expectedStatus))
			}
		}
		// one for snapd, one for kernel, one for gadget, one for base
		c.Check(autoConnectCount, Equals, 4)
	}
	autoConnectStatus("snapd", "", nil)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	c.Logf(s.logbuf.String())

	ok, rst = restart.Pending(st)
	c.Assert(ok, Equals, true)
	c.Assert(rst, Equals, restart.RestartSystem)

	autoConnectStatus("", "core20", []string{"snapd"})
	autoConnectStatus("", "pc", []string{"snapd"})
	autoConnectStatus("", "pc-kernel", []string{"snapd"})

	// we are trying out a new base
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.BaseStatus, Equals, boot.TryStatus)
	c.Check(m.TryBase, Equals, "core20_2.snap")

	// we are trying out a new kernel
	currentTryKernel, err := bloader.TryKernel()
	c.Assert(err, IsNil)
	c.Assert(currentTryKernel.Filename(), Equals, "pc-kernel_2.snap")

	// simulate successful restart happened
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	err = bloader.SetTryingDuringReboot([]snap.Type{snap.TypeKernel})
	c.Assert(err, IsNil)
	m.BaseStatus = boot.TryingStatus
	c.Assert(m.Write(), IsNil)
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// go on
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("change failed with: %v", chg.Err()))
}

func rearrangeBaseKernelForCyclicDependency(st *state.State, tss []*state.TaskSet) error {
	st.Lock()
	defer st.Unlock()
	var baseTs, kernelTs *state.TaskSet
	for _, ts := range tss {
		for _, t := range ts.Tasks() {
			snapsup, err := snapstate.TaskSnapSetup(t)
			if err != nil {
				continue
			}
			if snapsup.Type == snap.TypeKernel {
				kernelTs = ts
				break
			} else if snapsup.Type == snap.TypeBase {
				baseTs = ts
				break
			}
		}
	}

	haveBase, haveKernel := baseTs != nil, kernelTs != nil
	if !haveBase && !haveKernel {
		// neither base nor kernel update
		return nil
	}
	if haveBase != haveKernel {
		// have one but not the other
		return nil
	}

	// both kernel and boot base are being updated, reorder link-snap and
	// auto-connect tasks from both snaps such that we end up with the
	// following ordering:
	//
	// tasks of base and kernel ->
	//     link-snap(base) ->
	//        link-snap(kernel)(r) ->
	//            auto-connect(base) ->
	//                auto-connect(kernel) ->
	//                    remaining tasks of base and kernel
	//
	// where (r) denotes the task that can effectively request a reboot

	beforeLinkSnapKernel := kernelTs.MaybeEdge(snapstate.BeforeMaybeRebootEdge)
	linkSnapKernel := kernelTs.MaybeEdge(snapstate.MaybeRebootEdge)
	autoConnectKernel := kernelTs.MaybeEdge(snapstate.MaybeRebootWaitEdge)

	if linkSnapKernel == nil || autoConnectKernel == nil || beforeLinkSnapKernel == nil {
		return fmt.Errorf("internal error: cannot identify link-snap or auto-connect or the preceding task for the kernel snap")
	}
	kernelLanes := linkSnapKernel.Lanes()

	linkSnapBase := baseTs.MaybeEdge(snapstate.MaybeRebootEdge)
	autoConnectBase := baseTs.MaybeEdge(snapstate.MaybeRebootWaitEdge)
	afterAutoConnectBase := baseTs.MaybeEdge(snapstate.AfterMaybeRebootWaitEdge)
	if linkSnapBase == nil || autoConnectBase == nil || afterAutoConnectBase == nil {
		return fmt.Errorf("internal error: cannot identify link-snap or auto-connect or the following task for the base snap")
	}
	baseLanes := linkSnapBase.Lanes()

	for _, lane := range kernelLanes {
		linkSnapBase.JoinLane(lane)
		autoConnectBase.JoinLane(lane)
	}
	for _, lane := range baseLanes {
		linkSnapKernel.JoinLane(lane)
		autoConnectKernel.JoinLane(lane)
	}
	// make link-snap base wait for the last task directly preceding
	// link-snap of the kernel
	linkSnapBase.WaitFor(beforeLinkSnapKernel)
	// order: link-snap-base -> link-snap-kernel
	linkSnapKernel.WaitFor(linkSnapBase)
	// order: link-snap-kernel -> auto-connect-base
	autoConnectBase.WaitFor(linkSnapKernel)
	// order: auto-connect-base -> auto-connect-kernel
	autoConnectKernel.WaitFor(autoConnectBase)
	// make the first task after auto-connect base wait for auto-connect
	// kernel, this task already waits for auto-connect of base
	afterAutoConnectBase.WaitFor(autoConnectKernel)

	// cannot-reboot indicates that a task cannot invoke a reboot
	linkSnapBase.Set("cannot-reboot", true)

	// first auto connect will wait for reboot, but the restart pending flag
	// will be cleared for the second one that runs
	return nil
}

func (s *mgrsSuiteCore) TestUpdateKernelBaseSingleRebootWithGadgetWithExplicitBaseBuggy(c *C) {
	// verify a buggy scenario when the update contains snapd, kernel, base
	// and the gadget, in which case the buggy behavior will cause a cyclic
	// dependency between kernel, gadget and base, which then gets fixed by
	// calling AbortUnreadyLanes()

	// enable buggy behavior
	restore := snapstate.MockEnforceSingleRebootForBaseKernelGadget(true)
	defer restore()
	const pcGadget = `
name: pc
version: 1.0
type: gadget
base: core20
`
	_, tss, chg := s.testUpdateKernelBaseSingleRebootWithGadgetSetup(c, pcGadget)
	c.Assert(rearrangeBaseKernelForCyclicDependency(s.o.State(), tss), IsNil)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// cyclic dependency check would detect the problem
	c.Assert(chg.CheckTaskDependencies(), ErrorMatches, `dependency cycle involving tasks \[.*\]`)

	var snapst snapstate.SnapState
	err := snapstate.Get(st, "snapd", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(1))

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	c.Logf(s.logbuf.String())
	dumpTasks(c, "after run", chg.Tasks())

	// first comes the snapd restart
	ok, rst := restart.Pending(st)
	c.Assert(ok, Equals, true)
	c.Assert(rst, Equals, restart.RestartDaemon)
	restart.MockPending(st, restart.RestartUnset)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	c.Logf(s.logbuf.String())
	dumpTasks(c, "after run", chg.Tasks())

	// final steps will are postponed until we are in the restarted snapd
	ok, rst = restart.Pending(st)
	c.Assert(ok, Equals, false)
	c.Assert(rst, Equals, restart.RestartUnset)

	// settle has exited as there are no more tasks that can be run due to
	// the circular dependency, we expect all tasks to be in either Do or
	// Done states
	for _, tsk := range chg.Tasks() {
		if tsk.Status() != state.DoneStatus && tsk.Status() != state.DoStatus {
			c.Errorf("unexpected status %s of task %s %s", tsk.Status(), tsk.ID(), tsk.Summary())
			c.FailNow()
		}
	}

	chg.AbortUnreadyLanes()

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	c.Logf(s.logbuf.String())
	dumpTasks(c, "after abort", chg.Tasks())
	c.Assert(chg.IsReady(), Equals, true)
	c.Assert(chg.Status(), Equals, state.UndoneStatus)
	// snapd should have been hept
	for _, tsk := range chg.Tasks() {
		if tsk.Kind() == "link-snap" {
			snapsup, err := snapstate.TaskSnapSetup(tsk)
			c.Assert(err, IsNil)
			if snapsup.InstanceName() == "snapd" {
				c.Assert(tsk.Status(), Equals, state.DoneStatus)
			}
		}
	}

	// snapd update is kept
	err = snapstate.Get(st, "snapd", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(2))

	for _, name := range []string{"pc-kernel", "pc", "core20"} {
		err = snapstate.Get(st, name, &snapst)
		c.Assert(err, IsNil)
		// the current is the old revision
		c.Assert(snapst.Current, Equals, snap.R(1))
	}
}

func (s *mgrsSuiteCore) TestUpdateKernelBaseSingleRebootWithGadgetWithBuggySelfHeal(c *C) {
	// pretend it's a buggy snapd version that generates the change, then
	// snapd gets updated as part of the auto-refresh, during which we
	// restart to the new snapd which uses a new prune interval that
	// effectively aborts unready lanes and thus the buggy change completes,
	// while the new version of snaps remains
	restore := snapstate.MockEnforceSingleRebootForBaseKernelGadget(true)
	defer restore()
	const pcGadget = `
name: pc
version: 1.0
type: gadget
base: core20
`
	_, tss, chg := s.testUpdateKernelBaseSingleRebootWithGadgetSetup(c, pcGadget)
	c.Assert(rearrangeBaseKernelForCyclicDependency(s.o.State(), tss), IsNil)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	// cyclic dependency check would detect the problem
	c.Assert(chg.CheckTaskDependencies(), ErrorMatches, `dependency cycle involving tasks \[.*\]`)

	var snapst snapstate.SnapState
	err := snapstate.Get(st, "snapd", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(1))

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	c.Logf(s.logbuf.String())
	dumpTasks(c, "after run", chg.Tasks())

	// first comes the snapd restart
	ok, rst := restart.Pending(st)
	c.Assert(ok, Equals, true)
	c.Assert(rst, Equals, restart.RestartDaemon)
	restart.MockPending(st, restart.RestartUnset)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	c.Logf(s.logbuf.String())
	dumpTasks(c, "after run", chg.Tasks())

	// final steps will are postponed until we are in the restarted snapd
	ok, rst = restart.Pending(st)
	c.Assert(ok, Equals, false)
	c.Assert(rst, Equals, restart.RestartUnset)

	// settle has exited as there are no more tasks that can be run due to
	// the circular dependency, we expect all tasks to be in either Do or
	// Done states
	for _, tsk := range chg.Tasks() {
		if tsk.Status() != state.DoneStatus && tsk.Status() != state.DoStatus {
			c.Errorf("unexpected status %s of task %s %s", tsk.Status(), tsk.ID(), tsk.Summary())
			c.FailNow()
		}
	}

	// start settle and wait for prune to kick in
	restoreIntv := overlord.MockPruneInterval(200*time.Millisecond, 1000*time.Millisecond, 1000*time.Millisecond)
	defer restoreIntv()

	st.Unlock()
	s.o.Loop()

	checkTicker := time.NewTicker(time.Second)
	timeout := time.After(settleTimeout)
waitLoop:
	for {
		select {
		case <-checkTicker.C:
			st.Lock()
			rdy := chg.IsReady()
			st.Unlock()
			if rdy {
				break waitLoop
			}
		case <-timeout:
			c.Errorf("timeout waiting for prune to complete")
			c.FailNow()
		}
	}

	err = s.o.Stop()
	c.Assert(err, IsNil)

	st.Lock()

	c.Assert(chg.IsReady(), Equals, true)

	dumpTasks(c, "after prune", chg.Tasks())

	// snapd should have been hept
	for _, tsk := range chg.Tasks() {
		if tsk.Kind() == "link-snap" {
			snapsup, err := snapstate.TaskSnapSetup(tsk)
			c.Assert(err, IsNil)
			if snapsup.InstanceName() == "snapd" {
				c.Assert(tsk.Status(), Equals, state.DoneStatus)
			}
		}
	}

	// snapd update is kept
	err = snapstate.Get(st, "snapd", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(2))

	for _, name := range []string{"pc-kernel", "pc", "core20"} {
		err = snapstate.Get(st, name, &snapst)
		c.Assert(err, IsNil)
		// the current is the old revision
		c.Assert(snapst.Current, Equals, snap.R(1))
	}
}

func (s *mgrsSuiteCore) TestUpdateKernelBaseSingleRebootWithGadgetWithExplicitBaseHappy(c *C) {
	// verify a scenario when the update contains snapd, kernel, base
	// and the gadget, in which case the single-reboot code will not take effect.
	const pcGadget = `
name: pc
version: 1.0
type: gadget
base: core20
`
	bloader, _, chg := s.testUpdateKernelBaseSingleRebootWithGadgetSetup(c, pcGadget)

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, "snapd", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(1))

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	c.Logf(s.logbuf.String())
	dumpTasks(c, "after run", chg.Tasks())

	// first comes the snapd restart
	ok, rst := restart.Pending(st)
	c.Assert(ok, Equals, true)
	c.Assert(rst, Equals, restart.RestartDaemon)
	restart.MockPending(st, restart.RestartUnset)

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil, Commentf(s.logbuf.String()))
	c.Logf(s.logbuf.String())
	dumpTasks(c, "after run", chg.Tasks())

	// Snapd is done updating, and a restart has been requested after
	// running all pre-boot tasks for base, gadget and kernel.
	ok, rst = restart.Pending(st)
	c.Assert(ok, Equals, true)
	c.Assert(rst, Equals, restart.RestartSystem)

	// we are trying out a new base
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(m.BaseStatus, Equals, boot.TryStatus)
	c.Check(m.TryBase, Equals, "core20_2.snap")

	// we are trying out a new kernel
	currentTryKernel, err := bloader.TryKernel()
	c.Assert(err, IsNil)
	c.Assert(currentTryKernel.Filename(), Equals, "pc-kernel_2.snap")

	// simulate successful restart happened
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	err = bloader.SetTryingDuringReboot([]snap.Type{snap.TypeKernel})
	c.Assert(err, IsNil)
	m.BaseStatus = boot.TryingStatus
	c.Assert(m.Write(), IsNil)
	s.o.DeviceManager().ResetToPostBootState()
	st.Unlock()
	err = s.o.DeviceManager().Ensure()
	st.Lock()
	c.Assert(err, IsNil)

	// go on
	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.IsReady(), Equals, true)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// updates has been applied correctly
	err = snapstate.Get(st, "snapd", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Current, Equals, snap.R(2))

	for _, name := range []string{"pc-kernel", "pc", "core20"} {
		err = snapstate.Get(st, name, &snapst)
		c.Assert(err, IsNil)
		// the current is the old revision
		c.Assert(snapst.Current, Equals, snap.R(2))
	}
}

type gadgetUpdatesSuite struct {
	baseMgrsSuite

	bloader *boottest.Bootenv16
}

var _ = Suite(&gadgetUpdatesSuite{})

func (ms *gadgetUpdatesSuite) SetUpTest(c *C) {
	// We check for OnClassic when creating the manager. But we need
	// to add the clean-up after the call BaseTest.SetUpTest() to avoid
	// it panicking.
	restore := release.MockOnClassic(false)
	ms.baseMgrsSuite.SetUpTest(c)
	ms.AddCleanup(restore)

	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	ms.AddCleanup(func() { bootloader.Force(nil) })
	bloader.BootVars = map[string]string{
		"snap_core":   "core18_2.snap",
		"snap_kernel": "pc-kernel_1.snap",
		"snap_mode":   boot.DefaultStatus,
	}
	ms.bloader = bloader

	mockServer := ms.mockStore(c)
	ms.AddCleanup(mockServer.Close)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// setup model assertion
	model := ms.brands.Model("can0nical", "my-model", modelDefaults, map[string]interface{}{
		"gadget": "pi",
		"kernel": "pi-kernel",
	})
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "my-model",
		Serial: "serialserial",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)
}

// makeMockDev mocks /dev/disk/by-label/{structureName} and the mount
// point /run/mnt/{structureName} under the test rootdir and for
// osutil.LoadMountInfo for use by gadget code for test gadgets using
// structureName. This is useful for e.g. end-to-end testing of gadget
// assets installs/updates.
func (ms *gadgetUpdatesSuite) makeMockedDev(c *C, structureName string) {
	// mock /dev/disk/by-label/{structureName}
	byLabelDir := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label/")
	err := os.MkdirAll(byLabelDir, 0755)
	c.Assert(err, IsNil)
	fakeDiskDeviceNode := filepath.Join(dirs.GlobalRootDir, "/dev/fakedevice0")
	fakePartDeviceNode := fakeDiskDeviceNode + "p1"
	// create fakedevice node
	err = os.WriteFile(fakePartDeviceNode, nil, 0644)
	c.Assert(err, IsNil)
	// and point the mocked by-label entry to the fakedevice node
	err = os.Symlink(fakePartDeviceNode, filepath.Join(byLabelDir, structureName))
	c.Assert(err, IsNil)

	// mock /proc/self/mountinfo with the above generated paths
	ms.AddCleanup(osutil.MockMountInfo(fmt.Sprintf("26 27 8:3 / %[1]s/run/mnt/%[2]s rw,relatime shared:7 - vfat %[1]s/dev/fakedevice0p1 rw", dirs.GlobalRootDir, structureName)))

	// and mock the mount point
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName), 0755)
	c.Assert(err, IsNil)

	mockDisk := &disks.MockDiskMapping{
		DevNode: fakeDiskDeviceNode,

		// copied from ExpectedRaspiMockDiskMapping
		DevPath:             "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
		DevNum:              "8:2",
		DiskUsableSectorEnd: 30528 * 1024 * 1024 / 512,
		DiskSizeInBytes:     30528 * 1024 * 1024,
		SectorSizeBytes:     512,
		DiskSchema:          "dos",
		ID:                  "7c301cbd",

		Structure: []disks.Partition{
			{
				PartitionUUID:    "7c301cbd-01",
				PartitionType:    "0C",
				FilesystemLabel:  structureName,
				FilesystemUUID:   "0E09-0822",
				FilesystemType:   "vfat",
				Major:            8,
				Minor:            3,
				KernelDeviceNode: fakePartDeviceNode,
				KernelDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p1",
				DiskIndex:        1,
				StartInBytes:     1024 * 1024,
				SizeInBytes:      1200 * 1024 * 1024,
			},
		},
	}

	// mock device nodes
	disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		fakePartDeviceNode: mockDisk,
	})

	disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		fakeDiskDeviceNode: mockDisk,
	})
}

// tsWithoutReRefresh removes the re-refresh task from the given taskset.
//
// It assumes that re-refresh is the last task and will fail if that is
// not the case.
//
// This is needed because settle() will not converge with the re-refresh
// task because re-refresh will always be in doing state.
//
// TODO: have variant of Settle() that ends if ensure next time is
// stable or in the future by a value larger than some threshold, and
// then we would mock the rerefresh interval to something large and
// distinct from practical wait time even on slow systems. Once that
// is done this function can be removed.
func tsWithoutReRefresh(c *C, ts *state.TaskSet) *state.TaskSet {
	refreshIdx := len(ts.Tasks()) - 1
	c.Assert(ts.Tasks()[refreshIdx].Kind(), Equals, "check-rerefresh")
	ts = state.NewTaskSet(ts.Tasks()[:refreshIdx-1]...)
	return ts
}

// mockSnapUpgradeWithFiles will put a "rev 2" of the given snapYaml/files
// into the mock snapstore
func (ms *gadgetUpdatesSuite) mockSnapUpgradeWithFiles(c *C, snapYaml string, files [][]string) {
	snapPath, _ := ms.makeStoreTestSnapWithFiles(c, snapYaml, "2", files)
	ms.serveSnap(snapPath, "2")
}

func (ms *gadgetUpdatesSuite) TestRefreshGadgetUpdates(c *C) {
	structureName := "ubuntu-seed"
	gadgetYaml := fmt.Sprintf(`
volumes:
    volume-id:
        schema: mbr
        bootloader: u-boot
        structure:
          - name: %s
            filesystem: vfat
            type: 0C
            size: 1200M
            content:
              - source: boot-assets/
                target: /
              - source: foo.img
                target: /subdir/foo-renamed.img`, structureName)
	newGadgetYaml := gadgetYaml + `
            update:
              edition: 2
`
	ms.makeMockedDev(c, structureName)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// we have an installed gadget
	gadgetSnapYaml := "name: pi\nversion: 1.0\ntype: gadget"
	ms.mockInstalledSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})

	// add new gadget snap to fake store
	ms.mockSnapUpgradeWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", newGadgetYaml},
		{"boot-assets/bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev2"},
		{"boot-assets/bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev2"},
		{"boot-assets/overlays/uart0.dtbo", "uart0.dtbo rev2"},
		{"foo.img", "foo rev2"},
	})

	ts, err := snapstate.Update(st, "pi", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	// remove the re-refresh as it will prevent settle from converging
	ts = tsWithoutReRefresh(c, ts)

	chg := st.NewChange("upgrade-gadget", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// pretend we restarted
	c.Assert(chg.Status(), Equals, state.WaitStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))
	ms.mockRestartAndSettle(c, st, chg)

	// verify that change has completed
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	// check that files/dirs got updated and subdirs are correct
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "subdir/foo-renamed.img"), testutil.FileContains, "foo rev2")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-2-b.dtb"), testutil.FileContains, "bcm2710-rpi-2-b.dtb rev2")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-3-b.dtb"), testutil.FileContains, "bcm2710-rpi-3-b.dtb rev2")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "overlays/uart0.dtbo"), testutil.FileContains, "uart0.dtbo rev2")
}

func (ms *gadgetUpdatesSuite) TestGadgetWithKernelRefKernelRefresh(c *C) {
	kernelYaml := `
assets:
  pidtbs:
    update: true
    content:
    - dtbs/broadcom/
    - dtbs/overlays/`

	structureName := "ubuntu-seed"
	gadgetYaml := fmt.Sprintf(`
volumes:
    volume-id:
        schema: mbr
        bootloader: u-boot
        structure:
          - name: %s
            filesystem: vfat
            type: 0C
            size: 1200M
            content:
              - source: boot-assets/
                target: /
              - source: $kernel:pidtbs/dtbs/broadcom/
                target: /
              - source: $kernel:pidtbs/dtbs/overlays/
                target: /overlays`, structureName)
	ms.makeMockedDev(c, structureName)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// we have an installed gadget with kernel refs
	gadgetSnapYaml := "name: pi\nversion: 1.0\ntype: gadget"
	ms.mockInstalledSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
		{"boot-assets/start.elf", "start.elf rev1"},
	})
	// we have an installed kernel with kernel.yaml
	kernelSnapYaml := "name: pi-kernel\nversion: 1.0\ntype: kernel"
	ms.mockInstalledSnapWithFiles(c, kernelSnapYaml, [][]string{
		{"meta/kernel.yaml", kernelYaml},
	})

	// add new kernel snap to fake store
	ms.mockSnapUpgradeWithFiles(c, kernelSnapYaml, [][]string{
		{"meta/kernel.yaml", kernelYaml},
		{"dtbs/broadcom/bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev2"},
		{"dtbs/broadcom/bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev2"},
		{"dtbs/overlays/uart0.dtbo", "uart0.dtbo rev2"},
	})

	ts, err := snapstate.Update(st, "pi-kernel", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	// remove the re-refresh as it will prevent settle from converging
	ts = tsWithoutReRefresh(c, ts)

	chg := st.NewChange("upgrade-kernel", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// pretend we restarted
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoStatus, Commentf("install-snap change failed with: %v", chg.Err()))
	// pretend we restarted
	ms.mockSuccessfulReboot(c, chg, ms.bloader, []snap.Type{snap.TypeKernel})

	// settle again
	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	// check that files/dirs got updated and subdirs are correct
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-2-b.dtb"), testutil.FileContains, "bcm2710-rpi-2-b.dtb rev2")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-3-b.dtb"), testutil.FileContains, "bcm2710-rpi-3-b.dtb rev2")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "overlays/uart0.dtbo"), testutil.FileContains, "uart0.dtbo rev2")
	// BUT the gadget content is ignored and not copied again
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "start.elf"), testutil.FileAbsent)
}

func (ms *gadgetUpdatesSuite) TestGadgetWithKernelRefGadgetRefresh(c *C) {
	kernelYaml := `
assets:
  pidtbs:
    update: true
    content:
    - dtbs/broadcom/
    - dtbs/overlays/`

	structureName := "ubuntu-seed"
	gadgetYaml := fmt.Sprintf(`
volumes:
    volume-id:
        schema: mbr
        bootloader: u-boot
        structure:
          - name: %s
            filesystem: vfat
            type: 0C
            size: 1200M
            content:
              - source: boot-assets/
                target: /
              - source: $kernel:pidtbs/dtbs/broadcom/
                target: /
              - source: $kernel:pidtbs/dtbs/overlays/
                target: /overlays`, structureName)
	newGadgetYaml := gadgetYaml + `
            update:
              edition: 2
`
	ms.makeMockedDev(c, structureName)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// we have an installed gadget with kernel refs
	gadgetSnapYaml := "name: pi\nversion: 1.0\ntype: gadget"
	ms.mockInstalledSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})
	// we have an installed kernel with kernel.yaml
	kernelSnapYaml := "name: pi-kernel\nversion: 1.0\ntype: kernel"
	ms.mockInstalledSnapWithFiles(c, kernelSnapYaml, [][]string{
		{"meta/kernel.yaml", kernelYaml},
		{"dtbs/broadcom/bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev2"},
		{"dtbs/broadcom/bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev2"},
		{"dtbs/overlays/uart0.dtbo", "uart0.dtbo rev2"},
	})

	// add new gadget snap to fake store that has an "update: true"
	// for the kernel ref structure
	ms.mockSnapUpgradeWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", newGadgetYaml},
		{"boot-assets/start.elf", "start.elf rev2"},
	})

	ts, err := snapstate.Update(st, "pi", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	// remove the re-refresh as it will prevent settle from converging
	ts = tsWithoutReRefresh(c, ts)

	chg := st.NewChange("upgrade-gadget", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// pretend we restarted
	c.Assert(chg.Status(), Equals, state.WaitStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))
	ms.mockRestartAndSettle(c, st, chg)

	// verify that change is now done
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	// check that files/dirs got updated and subdirs are correct
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-2-b.dtb"), testutil.FileContains, "bcm2710-rpi-2-b.dtb rev2")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-3-b.dtb"), testutil.FileContains, "bcm2710-rpi-3-b.dtb rev2")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "overlays/uart0.dtbo"), testutil.FileContains, "uart0.dtbo rev2")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "start.elf"), testutil.FileContains, "start.elf rev2")
}

func (ms *gadgetUpdatesSuite) TestGadgetWithKernelRefUpgradeFromOld(c *C) {
	kernelYaml := `
assets:
  pidtbs:
    update: true
    content:
    - dtbs/broadcom/
    - dtbs/overlays/`

	structureName := "ubuntu-seed"
	oldGadgetYaml := fmt.Sprintf(`
volumes:
    volume-id:
        schema: mbr
        bootloader: u-boot
        structure:
          - name: %s
            filesystem: vfat
            type: 0C
            size: 1200M
            content:
              - source: boot-assets/
                target: /`, structureName)
	// Note that there is no "edition" jump here for the new "$kernel:ref"
	// content. This is driven by the kernel.yaml "update: true" value.
	newGadgetYaml := fmt.Sprintf(`
volumes:
    volume-id:
        schema: mbr
        bootloader: u-boot
        structure:
          - name: %s
            filesystem: vfat
            type: 0C
            size: 1200M
            content:
              - source: boot-assets/
                target: /
              - source: $kernel:pidtbs/dtbs/broadcom/
                target: /
              - source: $kernel:pidtbs/dtbs/overlays/
                target: /overlays`, structureName)
	ms.makeMockedDev(c, structureName)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// we have an installed old style pi gadget
	gadgetSnapYaml := "name: pi\nversion: 1.0\ntype: gadget"
	ms.mockInstalledSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", oldGadgetYaml},
		{"boot-assets/start.elf", "start.elf rev1"},
		{"boot-assets/bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev1"},
		{"boot-assets/bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev1"},
	})
	// we have old style boot assets in the bootloader dir
	snaptest.PopulateDir(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName), [][]string{
		{"start.elf", "start.elf rev1"},
		{"bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev1"},
		{"bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev1"},
	})

	// we have an installed old-style kernel snap
	kernelSnapYaml := "name: pi-kernel\nversion: 1.0\ntype: kernel"
	ms.mockInstalledSnapWithFiles(c, kernelSnapYaml, nil)

	// add new kernel snap with kernel-refs to fake store
	ms.mockSnapUpgradeWithFiles(c, kernelSnapYaml, [][]string{
		{"meta/kernel.yaml", kernelYaml},
		{"dtbs/broadcom/bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev2-from-kernel"},
		{"dtbs/broadcom/bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev2-from-kernel"},
		{"dtbs/overlays/uart0.dtbo", "uart0.dtbo rev2-from-kernel"},
	})

	// add new gadget snap with kernel-refs to fake store
	ms.mockSnapUpgradeWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", newGadgetYaml},
		{"boot-assets/start.elf", "start.elf rev1"},
		// notice: no dtbs anymore in the gadget
	})

	affected, tasksets, err := snapstate.UpdateMany(context.TODO(), st, nil, nil, 0, &snapstate.Flags{})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, []string{"pi", "pi-kernel"})

	chg := st.NewChange("upgrade-snaps", "...")
	for _, ts := range tasksets {
		// skip the taskset of UpdateMany that does the
		// check-rerefresh, see tsWithoutReRefresh for details
		if ts.Tasks()[0].Kind() == "check-rerefresh" {
			continue
		}
		chg.AddAll(ts)
	}

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// At this point the gadget and kernel are updated and the kernel
	// required a restart. Check that *before* this restart the DTB
	// files from the kernel are in place.
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-2-b.dtb"), testutil.FileContains, "bcm2710-rpi-2-b.dtb rev2-from-kernel")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-3-b.dtb"), testutil.FileContains, "bcm2710-rpi-3-b.dtb rev2-from-kernel")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "overlays/uart0.dtbo"), testutil.FileContains, "uart0.dtbo rev2-from-kernel")
	//  gadget content is not updated because there is no edition update
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "start.elf"), testutil.FileContains, "start.elf rev1")

	// pretend we restarted after 'link-snap'
	ms.mockSuccessfulReboot(c, chg, ms.bloader, []snap.Type{snap.TypeKernel})

	// settle again
	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)
}

func (ms *gadgetUpdatesSuite) mockSnapUpgradeWithFilesWithRev(c *C, snapYaml string, rev string, files [][]string) {
	snapPath, _ := ms.makeStoreTestSnapWithFiles(c, snapYaml, rev, files)
	ms.serveSnap(snapPath, rev)
}

func (ms *gadgetUpdatesSuite) TestOldGadgetOldKernelRefreshToKernelRefWithGadgetAssetsCyclicDependency(c *C) {
	kernelYaml := `
assets:
  pidtbs:
    update: true
    content:
    - dtbs/broadcom/
    - dtbs/overlays/`

	structureName := "ubuntu-seed"
	oldGadgetYaml := fmt.Sprintf(`
volumes:
    volume-id:
        schema: mbr
        bootloader: u-boot
        structure:
          - name: %s
            filesystem: vfat
            type: 0C
            size: 1200M
            content:
              - source: boot-assets/
                target: /`, structureName)
	// the gadget specifically bumps the edition to trigger a cirular dependency
	finalDesiredGadgetYaml := fmt.Sprintf(`
volumes:
    volume-id:
        schema: mbr
        bootloader: u-boot
        structure:
          - name: %s
            filesystem: vfat
            type: 0C
            size: 1200M
            update:
              edition: 1
            content:
              - source: boot-assets/
                target: /
              - source: $kernel:pidtbs/dtbs/broadcom/
                target: /
              - source: $kernel:pidtbs/dtbs/overlays/
                target: /overlays`, structureName)
	ms.makeMockedDev(c, structureName)

	intermediaryGadgetYaml := fmt.Sprintf(`
volumes:
    volume-id:
        schema: mbr
        bootloader: u-boot
        structure:
          - name: %s
            filesystem: vfat
            type: 0C
            size: 1200M
            content:
              - source: boot-assets/
                target: /
              - source: $kernel:pidtbs/dtbs/broadcom/
                target: /
              - source: $kernel:pidtbs/dtbs/overlays/
                target: /overlays`, structureName)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// we have an installed old style pi gadget
	gadgetSnapYaml := "name: pi\nversion: 1.0\ntype: gadget"
	ms.mockInstalledSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", oldGadgetYaml},
		{"boot-assets/start.elf", "start.elf rev1"},
		{"boot-assets/bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev1"},
		{"boot-assets/bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev1"},
	})
	// we have old style boot assets in the bootloader dir
	snaptest.PopulateDir(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName), [][]string{
		{"start.elf", "start.elf rev0"},
		{"bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev0"},
		{"bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev0"},
	})

	// we have an installed old-style kernel snap
	kernelSnapYaml := "name: pi-kernel\nversion: 1.0\ntype: kernel"
	ms.mockInstalledSnapWithFiles(c, kernelSnapYaml, nil)

	// add new kernel snap with kernel-refs to fake store
	ms.mockSnapUpgradeWithFilesWithRev(c, kernelSnapYaml, "2", [][]string{
		{"meta/kernel.yaml", kernelYaml},
		{"dtbs/broadcom/bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev2-from-kernel"},
		{"dtbs/broadcom/bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev2-from-kernel"},
		{"dtbs/overlays/uart0.dtbo", "uart0.dtbo rev2-from-kernel"},
	})

	// add new gadget snap with kernel-refs to fake store
	ms.mockSnapUpgradeWithFilesWithRev(c, gadgetSnapYaml, "2", [][]string{
		{"meta/gadget.yaml", finalDesiredGadgetYaml},
		{"boot-assets/start.elf", "start.elf rev1"},
		// notice: no dtbs anymore in the gadget
	})

	// we can neither update the gadget nor the kernel because of circular
	// dependency, as the new gadget refers to assets from the kernel, but
	// those are not present in the old (installed) kernel, and the new
	// kernel provides assets that are not consumed by the old (installed)
	// gadget

	affected, tasksets, err := snapstate.UpdateMany(context.TODO(), st, []string{"pi"}, nil, 0, &snapstate.Flags{})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, []string{"pi"})

	addTaskSetsToChange := func(chg *state.Change, tss []*state.TaskSet) {
		for _, ts := range tasksets {
			// skip the taskset of UpdateMany that does the
			// check-rerefresh, see tsWithoutReRefresh for details
			if ts.Tasks()[0].Kind() == "check-rerefresh" {
				continue
			}
			chg.AddAll(ts)
		}
	}
	chg := st.NewChange("upgrade-snaps", "...")
	addTaskSetsToChange(chg, tasksets)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), ErrorMatches, `(?s).*\(cannot resolve content for structure #0 \("ubuntu-seed"\) at index 1: cannot find "pidtbs" in kernel info .*\)`)

	restarting, _ := restart.Pending(st)
	c.Assert(restarting, Equals, false, Commentf("unexpected restart"))

	// let's try updating the kernel;
	affected, tasksets, err = snapstate.UpdateMany(context.TODO(), st, []string{"pi-kernel"}, nil, 0, &snapstate.Flags{})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, []string{"pi-kernel"})

	chg = st.NewChange("upgrade-snaps", "...")
	addTaskSetsToChange(chg, tasksets)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	// A restart request is made by 'unlink-current-snap', which needs to be handled
	// here. This comment is added after changes to the restart system which now
	// correctly marks changes for reboot and does not skip reboots in unit tests which
	// the old restart code would. Only unit tests were affected as restart.Request() does
	// not actually restart in unit tests, instead the task is marked Done/Undone and allows
	// the change to continue executing, even though a restart was required. The new restart code
	// works differently, and puts a change into WaitStatus instead, together with the task that asked, and
	// these must be manually cleared by simulating a reboot.
	ms.mockRestartAndSettle(c, st, chg)

	c.Assert(chg.Err(), ErrorMatches, `(?s).*\(gadget does not consume any of the kernel assets needing synced update "pidtbs"\)`)
	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// but we can actually perform the full upgrade set if we first refresh
	// to an intermediate gadget revision which does not declare an update,
	// but does now reference the kernel assets, to force going through this
	// revision, declare it uses a transitional epoch 1* (which can read
	// epoch 0, the default)
	gadgetSnapYamlIntermediate := `
name: pi
version: 1.0
type: gadget
epoch: 1*
`
	// while the final gadget will have an epoch 1
	gadgetSnapYamlFinal := `
name: pi
version: 1.0
type: gadget
epoch: 1
`
	// make both revisions available in the fake store
	ms.mockSnapUpgradeWithFilesWithRev(c, gadgetSnapYamlIntermediate, "3", [][]string{
		{"meta/gadget.yaml", intermediaryGadgetYaml},
		{"boot-assets/start.elf", "start.elf rev1"},
		// the intermediary gadget snap has these files but it doesn't really
		// mattter since update does not set an edition, so no update is
		// attempted using these files
		{"bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev1"},
		{"bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev1"},
	})
	ms.mockSnapUpgradeWithFilesWithRev(c, gadgetSnapYamlFinal, "4", [][]string{
		{"meta/gadget.yaml", finalDesiredGadgetYaml},
		{"boot-assets/start.elf", "start.elf rev1"},
	})

	affected, tasksets, err = snapstate.UpdateMany(context.TODO(), st, []string{"pi"}, nil, 0, &snapstate.Flags{})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, []string{"pi"})

	chg = st.NewChange("upgrade-snaps", "...")
	addTaskSetsToChange(chg, tasksets)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// file content is still unchanged because there is no edition bump
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-2-b.dtb"),
		testutil.FileContains, "bcm2710-rpi-2-b.dtb rev0")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-3-b.dtb"),
		testutil.FileContains, "bcm2710-rpi-3-b.dtb rev0")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "overlays/uart0.dtbo"),
		testutil.FileAbsent)
	//  gadget content is not updated because there is no edition update
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "start.elf"),
		testutil.FileContains, "start.elf rev0")

	// thus there is no reboot either
	restarting, _ = restart.Pending(st)
	c.Assert(restarting, Equals, false, Commentf("unexpected restart"))

	// and now we can perform a refresh of the kernel
	affected, tasksets, err = snapstate.UpdateMany(context.TODO(), st, []string{"pi-kernel"}, nil, 0, &snapstate.Flags{})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, []string{"pi-kernel"})

	chg = st.NewChange("upgrade-snaps", "...")
	addTaskSetsToChange(chg, tasksets)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// At this point the gadget and kernel are updated and the kernel
	// required a restart. Check that *before* this restart the DTB
	// files from the kernel are in place.
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-2-b.dtb"),
		testutil.FileContains, "bcm2710-rpi-2-b.dtb rev2-from-kernel")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-3-b.dtb"),
		testutil.FileContains, "bcm2710-rpi-3-b.dtb rev2-from-kernel")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "overlays/uart0.dtbo"),
		testutil.FileContains, "uart0.dtbo rev2-from-kernel")
	//  gadget content is not updated because there is no edition update
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "start.elf"),
		testutil.FileContains, "start.elf rev0")

	// pretend we restarted, both a kernel and boot assets update
	ms.mockSuccessfulReboot(c, chg, ms.bloader, []snap.Type{snap.TypeKernel})
	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	var ss snapstate.SnapState
	c.Assert(snapstate.Get(st, "pi", &ss), IsNil)
	// the transitional revision
	c.Assert(ss.Current, Equals, snap.R(3))

	// also check that the gadget asset updates for the second refresh of
	// the gadget snap get applied since that is important for some use
	// cases and is probably why folks got into the circular dependency in
	// the first place, for this we add another revision of the gadget snap

	affected, tasksets, err = snapstate.UpdateMany(context.TODO(), st, []string{"pi"}, nil, 0, &snapstate.Flags{})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, []string{"pi"})

	chg = st.NewChange("upgrade-snaps", "...")
	addTaskSetsToChange(chg, tasksets)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)

	// gadget assets that come from the kernel are unchanged
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-2-b.dtb"),
		testutil.FileContains, "bcm2710-rpi-2-b.dtb rev2-from-kernel")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-3-b.dtb"),
		testutil.FileContains, "bcm2710-rpi-3-b.dtb rev2-from-kernel")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "overlays/uart0.dtbo"),
		testutil.FileContains, "uart0.dtbo rev2-from-kernel")
	//  but an assets that comes directly from the gadget was updated
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "start.elf"),
		testutil.FileContains, "start.elf rev1")

	// pretend we restarted for the gadget refresh
	ms.mockSuccessfulReboot(c, chg, ms.bloader, nil)
	// and let the change run until it is done
	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	c.Assert(snapstate.Get(st, "pi", &ss), IsNil)
	// the final revision
	c.Assert(ss.Current, Equals, snap.R(4))
}

func snapTaskStatusForChange(chg *state.Change) map[string]state.Status {
	taskStates := make(map[string]state.Status)
	for _, t := range chg.Tasks() {
		if snapsup, err := snapstate.TaskSnapSetup(t); err == nil {
			taskStates[snapsup.SnapName()+":"+t.Kind()] = t.Status()
		}
	}
	return taskStates
}

func (ms *gadgetUpdatesSuite) TestGadgetWithKernelRefUpgradeFromOldErrorGadget(c *C) {
	kernelYaml := `
assets:
  pidtbs:
    update: true
    content:
    - dtbs/broadcom/
    - dtbs/overlays/`

	structureName := "ubuntu-seed"
	oldGadgetYaml := fmt.Sprintf(`
volumes:
    volume-id:
        schema: mbr
        bootloader: u-boot
        structure:
          - name: %s
            filesystem: vfat
            type: 0C
            size: 1200M
            content:
              - source: boot-assets/
                target: /`, structureName)
	// Note that there is no "edition" jump here for the new "$kernel:ref"
	// content. This is driven by the kernel.yaml "update: true" value.
	newGadgetYaml := fmt.Sprintf(`
volumes:
    volume-id:
        schema: mbr
        bootloader: u-boot
        structure:
          - name: %s
            filesystem: vfat
            type: 0C
            size: 1200M
            content:
              - source: boot-assets/
                target: /
              - source: $kernel:pidtbs/dtbs/broadcom/
                target: /
              - source: $kernel:pidtbs/dtbs/overlays/
                target: /overlays`, structureName)
	ms.makeMockedDev(c, structureName)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// we have an installed old style pi gadget
	gadgetSnapYaml := "name: pi\nversion: 1.0\ntype: gadget"
	ms.mockInstalledSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", oldGadgetYaml},
		{"boot-assets/start.elf", "start.elf rev1"},
		{"boot-assets/bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev1"},
		{"boot-assets/bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev1"},
	})
	// we have old style boot assets in the bootloader dir
	snaptest.PopulateDir(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName), [][]string{
		{"start.elf", "start.elf rev1"},
		{"bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev1"},
		{"bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev1"},
	})

	// we have an installed old-style kernel snap
	kernelSnapYaml := "name: pi-kernel\nversion: 1.0\ntype: kernel"
	ms.mockInstalledSnapWithFiles(c, kernelSnapYaml, nil)

	// add new kernel snap with kernel-refs to fake store
	ms.mockSnapUpgradeWithFiles(c, kernelSnapYaml, [][]string{
		{"meta/kernel.yaml", kernelYaml},
		{"dtbs/broadcom/bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev2-from-kernel"},
		{"dtbs/broadcom/bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev2-from-kernel"},
		{"dtbs/overlays/uart0.dtbo", "uart0.dtbo rev2-from-kernel"},
	})

	// add new gadget snap with kernel-refs to fake store
	ms.mockSnapUpgradeWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", newGadgetYaml},
		{"boot-assets/start.elf", "start.elf rev1"},
		// notice: no dtbs anymore in the gadget
	})

	affected, tasksets, err := snapstate.UpdateMany(context.TODO(), st, nil, nil, 0, &snapstate.Flags{})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, []string{"pi", "pi-kernel"})

	chg := st.NewChange("upgrade-snaps", "...")
	tError := st.NewTask("error-trigger", "gadget failed")
	for _, ts := range tasksets {
		// skip the taskset of UpdateMany that does the
		// check-rerefresh, see tsWithoutReRefresh for details
		tasks := ts.Tasks()
		if tasks[0].Kind() == "check-rerefresh" {
			continue
		}

		snapsup, err := snapstate.TaskSnapSetup(tasks[0])
		c.Assert(err, IsNil)
		// trigger an error as last operation of gadget refresh
		if snapsup.SnapName() == "pi" {
			last := tasks[len(tasks)-1]
			tError.WaitFor(last)
			// XXX: or just use "snap-setup" here?
			tError.Set("snap-setup-task", tasks[0].ID())
			ts.AddTask(tError)
			lanes := last.Lanes()
			c.Assert(lanes, HasLen, 1)
			for _, l := range lanes {
				tError.JoinLane(l)
			}
		}

		chg.AddAll(ts)
	}

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n- gadget failed.*`)

	// check that files/dirs from the kernel did  *not* get updated or installed
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-2-b.dtb"), testutil.FileContains, "bcm2710-rpi-2-b.dtb rev1")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-3-b.dtb"), testutil.FileContains, "bcm2710-rpi-3-b.dtb rev1")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "overlays/uart0.dtbo"), testutil.FileAbsent)

	// Ensure that tasks states are valid
	taskStates := snapTaskStatusForChange(chg)
	// The pi gadget failed in error-trigger and got rolled back
	c.Check(taskStates["pi:error-trigger"], Equals, state.ErrorStatus)
	c.Check(taskStates["pi:mount-snap"], Equals, state.UndoneStatus)
	// And the pi-kernel did not even get started
	c.Check(taskStates["pi-kernel:download-snap"], Equals, state.HoldStatus)
}

func (ms *gadgetUpdatesSuite) TestGadgetWithKernelRefUpgradeFromOldErrorKernel(c *C) {
	structureName := "ubuntu-seed"
	structureMountDir := filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName)

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"volume-id": {
					0: {
						RootMountPoint: structureMountDir,
					},
					1: {
						RootMountPoint: "/foo-data",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"volume-id": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["volume-id"]),
			}, nil
	})
	defer r()

	kernelYaml := `
assets:
  pidtbs:
    update: true
    content:
    - dtbs/broadcom/
    - dtbs/overlays/`

	oldGadgetYaml := fmt.Sprintf(`
volumes:
    volume-id:
        schema: mbr
        bootloader: u-boot
        structure:
          - name: %s
            filesystem: vfat
            type: 0C
            size: 1200M
            content:
              - source: boot-assets/
                target: /`, structureName)
	// Note that there is no "edition" jump here for the new "$kernel:ref"
	// content. This is driven by the kernel.yaml "update: true" value.
	newGadgetYaml := fmt.Sprintf(`
volumes:
    volume-id:
        schema: mbr
        bootloader: u-boot
        structure:
          - name: %s
            filesystem: vfat
            type: 0C
            size: 1200M
            content:
              - source: boot-assets/
                target: /
              - source: $kernel:pidtbs/dtbs/broadcom/
                target: /
              - source: $kernel:pidtbs/dtbs/overlays/
                target: /overlays`, structureName)
	ms.makeMockedDev(c, structureName)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// we have an installed old style pi gadget
	gadgetSnapYaml := "name: pi\nversion: 1.0\ntype: gadget"
	ms.mockInstalledSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", oldGadgetYaml},
		{"boot-assets/start.elf", "start.elf rev1"},
		{"boot-assets/bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev1"},
		{"boot-assets/bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev1"},
	})
	// we have old style boot assets in the bootloader dir
	snaptest.PopulateDir(structureMountDir, [][]string{
		{"start.elf", "start.elf rev1"},
		{"bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev1"},
		{"bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev1"},
	})

	// we have an installed old-style kernel snap
	kernelSnapYaml := "name: pi-kernel\nversion: 1.0\ntype: kernel"
	ms.mockInstalledSnapWithFiles(c, kernelSnapYaml, nil)

	// add new kernel snap with kernel-refs to fake store
	ms.mockSnapUpgradeWithFiles(c, kernelSnapYaml, [][]string{
		{"meta/kernel.yaml", kernelYaml},
		{"dtbs/broadcom/bcm2710-rpi-2-b.dtb", "bcm2710-rpi-2-b.dtb rev2-from-kernel"},
		{"dtbs/broadcom/bcm2710-rpi-3-b.dtb", "bcm2710-rpi-3-b.dtb rev2-from-kernel"},
		{"dtbs/overlays/uart0.dtbo", "uart0.dtbo rev2-from-kernel"},
	})

	// add new gadget snap with kernel-refs to fake store
	ms.mockSnapUpgradeWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", newGadgetYaml},
		{"boot-assets/start.elf", "start.elf rev1"},
		// notice: no dtbs anymore in the gadget
	})

	affected, tasksets, err := snapstate.UpdateMany(context.TODO(), st, nil, nil, 0, &snapstate.Flags{})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, []string{"pi", "pi-kernel"})

	chg := st.NewChange("upgrade-snaps", "...")
	tError := st.NewTask("error-trigger", "kernel failed")
	for _, ts := range tasksets {
		// skip the taskset of UpdateMany that does the
		// check-rerefresh, see tsWithoutReRefresh for details
		tasks := ts.Tasks()
		if tasks[0].Kind() == "check-rerefresh" {
			continue
		}

		snapsup, err := snapstate.TaskSnapSetup(tasks[0])
		c.Assert(err, IsNil)
		// trigger an error as last operation of gadget refresh
		if snapsup.SnapName() == "pi-kernel" {
			last := tasks[len(tasks)-1]
			tError.WaitFor(last)
			// XXX: or just use "snap-setup" here?
			tError.Set("snap-setup-task", tasks[0].ID())
			ts.AddTask(tError)
			lanes := last.Lanes()
			c.Assert(lanes, HasLen, 1)
			for _, l := range lanes {
				tError.JoinLane(l)
			}
		}

		chg.AddAll(ts)
	}

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(chg.Err(), IsNil)

	// At this point the gadget and kernel are updated and the kernel
	// required a restart. Check that *before* this restart the DTB
	// files from the kernel are in place.
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-2-b.dtb"), testutil.FileContains, "bcm2710-rpi-2-b.dtb rev2-from-kernel")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-3-b.dtb"), testutil.FileContains, "bcm2710-rpi-3-b.dtb rev2-from-kernel")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "overlays/uart0.dtbo"), testutil.FileContains, "uart0.dtbo rev2-from-kernel")
	//  gadget content is not updated because there is no edition update
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "start.elf"), testutil.FileContains, "start.elf rev1")

	// A restart request is made by 'link-snap'
	ms.mockSuccessfulReboot(c, chg, ms.bloader, []snap.Type{snap.TypeKernel})

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// A restart request is made by 'unlink-current-snap' (undo)
	ms.mockRestartAndSettle(c, st, chg)

	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n- kernel failed.*`)

	// Ensure that tasks states are what we expect
	taskStates := snapTaskStatusForChange(chg)
	// The pi-kernel failed in error-trigger and got rolled back
	c.Check(taskStates["pi-kernel:error-trigger"], Equals, state.ErrorStatus)
	c.Check(taskStates["pi-kernel:mount-snap"], Equals, state.UndoneStatus)
	// But the pi gadget was installed just fine
	c.Check(taskStates["pi:download-snap"], Equals, state.DoneStatus)
	c.Check(taskStates["pi:link-snap"], Equals, state.DoneStatus)

	// Note that the undo of the kernel did *not* revert the DTBs on
	// disk. The reason is that we never undo asset updates on the
	// basis that if the system booted they are probably good enough.
	// A really broken DTB can brick the device if the new DTB is written
	// to disk, the system reboots and neither new kernel nor fallback
	// kernel will boot because there is no A/B DTB. This is a flaw
	// of the Pi and u-boot.
	//
	// In the future we will integrate with the "pi-boot" mechanism that
	// allows doing a A/B boot using the config.txt "os-prefix" dir. This
	// will allow us to write the DTBs to A/B locations.
	//
	// TODO:UC20: port this so that it integrates with pi-boot and the
	//            A/B os-prefix mechanism there so that we can have
	//            robust DTB updates.
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-2-b.dtb"), testutil.FileContains, "bcm2710-rpi-2-b.dtb rev2-from-kernel")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "bcm2710-rpi-3-b.dtb"), testutil.FileContains, "bcm2710-rpi-3-b.dtb rev2-from-kernel")
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "overlays/uart0.dtbo"), testutil.FileContains, "uart0.dtbo rev2-from-kernel")
	//  gadget content is not updated because there is no edition update
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/", structureName, "start.elf"), testutil.FileContains, "start.elf rev1")
}

// deal with the missing "update-gadget-assets" tasks, see LP:#1940553
func (ms *gadgetUpdatesSuite) TestGadgetKernelRefreshFromOldBrokenSnap(c *C) {
	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// we have an install kernel/gadget
	gadgetSnapYaml := "name: pi\nversion: 1.0\ntype: gadget"
	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
        structure:
          - name: mbr
            type: mbr
            size: 440
`
	ms.mockInstalledSnapWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})
	kernelSnapYaml := "name: pi-kernel\nversion: 1.0\ntype: kernel"
	ms.mockInstalledSnapWithFiles(c, kernelSnapYaml, nil)

	// add new kernel/gadget snap
	newKernelSnapYaml := kernelSnapYaml
	ms.mockSnapUpgradeWithFiles(c, newKernelSnapYaml, nil)
	ms.mockSnapUpgradeWithFiles(c, gadgetSnapYaml, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{"volume-id": {0: {}}}, map[string]map[int]*gadget.OnDiskStructure{
			"volume-id": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["volume-id"]),
		}, nil
	})
	defer r()

	// now a refresh is simulated that does *not* contain an
	// "update-gadget-assets" task, see LP:#1940553
	snapstate.TestingLeaveOutKernelUpdateGadgetAssets = true
	defer func() { snapstate.TestingLeaveOutKernelUpdateGadgetAssets = false }()
	affected, tasksets, err := snapstate.UpdateMany(context.TODO(), st, nil, nil, 0, &snapstate.Flags{})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, []string{"pi", "pi-kernel"})

	// here we need to manipulate the change to simulate that there
	// is no "update-gadget-assets" task for the kernel, unfortunately
	// there is no "state.TaskSet.RemoveTask" nor a "state.Task.Unwait()"
	chg := st.NewChange("upgrade-snaps", "...")
	for _, ts := range tasksets {
		// skip the taskset of UpdateMany that does the
		// check-rerefresh, see tsWithoutReRefresh for details
		if ts.Tasks()[0].Kind() == "check-rerefresh" {
			continue
		}

		chg.AddAll(ts)
	}

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(chg.Err(), ErrorMatches, "cannot perform the following tasks:\n.*Mount snap \"pi-kernel\" \\(2\\) \\(cannot refresh kernel with change created by old snapd that is missing gadget update task\\)")
}

func (s *mgrsSuite) TestDownloadToDefault(c *C) {
	// should default to dirs.SnapBlobDir
	const downloadDir = ""
	s.testDownload(c, downloadDir)
}

func (s *mgrsSuite) TestDownloadToLocation(c *C) {
	downloadDir := c.MkDir()
	s.testDownload(c, downloadDir)
}

func (s *mgrsSuite) testDownload(c *C, downloadDir string) {
	s.prereqSnapAssertions(c)

	const snapRev = "1"

	testSnapPath, _ := s.makeStoreTestSnap(c, "{name: foo, version: 0}", snapRev)
	s.serveSnap(testSnapPath, snapRev)

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, info, err := snapstate.Download(context.TODO(), st, "foo", downloadDir, nil, 0, snapstate.Flags{}, nil)
	c.Assert(err, IsNil)
	chg := st.NewChange("download-snap", "...")
	chg.AddAll(ts)

	c.Check(info.SideInfo, DeepEquals, snap.SideInfo{
		RealName:          "foo",
		SnapID:            fakeSnapID("foo"),
		Revision:          snap.R(snapRev),
		EditedSummary:     "Foo",
		EditedDescription: "this is a description",
	})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// confirm that download-snap task ran
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("download-snap change failed with: %v", chg.Err()))

	expectedDownloadDir := downloadDir
	if expectedDownloadDir == "" {
		expectedDownloadDir = dirs.SnapBlobDir
	}

	snapPath := filepath.Join(expectedDownloadDir, fmt.Sprintf("%s_%s.snap", "foo", snapRev))

	exists := osutil.FileExists(snapPath)
	c.Check(exists, Equals, true)

	digest, _, err := asserts.SnapFileSHA3_384(snapPath)
	c.Assert(err, IsNil)

	// test that snap revision assertion was added by validation-snap task
	_, err = assertstate.DB(st).Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": digest,
	})
	c.Check(err, IsNil)
}

func (s *mgrsSuite) TestDownloadSpecificRevision(c *C) {
	s.prereqSnapAssertions(c)

	const snapOldRev = "1"
	const snapNewRev = "2"

	snapOldPath, _ := s.makeStoreTestSnap(c, "{name: foo, version: 1}", snapOldRev)
	s.serveSnap(snapOldPath, snapOldRev)

	snapNewPath, _ := s.makeStoreTestSnap(c, "{name: foo, version: 2}", snapNewRev)
	s.serveSnap(snapNewPath, snapNewRev)

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	ts, info, err := snapstate.Download(context.TODO(), st, "foo", "", &snapstate.RevisionOptions{
		Revision: snap.R(snapOldRev),
	}, 0, snapstate.Flags{}, nil)
	c.Assert(err, IsNil)
	chg := st.NewChange("download-snap", "...")
	chg.AddAll(ts)

	c.Check(info.SideInfo, DeepEquals, snap.SideInfo{
		RealName:          "foo",
		SnapID:            fakeSnapID("foo"),
		Revision:          snap.R(snapOldRev),
		EditedSummary:     "Foo",
		EditedDescription: "this is a description",
	})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// confirm it worked
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("download-snap change failed with: %v", chg.Err()))

	snapPath := filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_%s.snap", "foo", snapOldRev))
	exists := osutil.FileExists(snapPath)
	c.Check(exists, Equals, true)

	info, err = snap.ReadInfoFromSnapFile(squashfs.New(snapPath), nil)
	c.Assert(err, IsNil)
	c.Check(info.Version, Equals, "1")

	digest, _, err := asserts.SnapFileSHA3_384(snapPath)
	c.Assert(err, IsNil)

	// test that snap revision assertion was added by validation-snap task
	_, err = assertstate.DB(st).Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": digest,
	})
	c.Check(err, IsNil)
}

const snapYamlMonitoredAppFormat = `
name: %s
version: %s

apps:
    app:
        command: $SNAP/foo
`

func (s *mgrsSuite) TestAutoRefreshOneWithMonitoring(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	err := assertstate.Add(st, s.devAcct)
	c.Assert(err, IsNil)

	rev := snap.R(1)
	snapDecl := s.prereqSnapAssertions(c, map[string]interface{}{"snap-name": "held-with-app-running"})
	err = assertstate.Add(st, snapDecl)
	c.Assert(err, IsNil)

	s.mockInstalledSnapWithRevAndFiles(c,
		fmt.Sprintf(snapYamlMonitoredAppFormat, "held-with-app-running", "0"), rev, nil)

	// now add some more snap revisions
	revno := "2"
	snapPath, _ := s.makeStoreTestSnap(c,
		fmt.Sprintf(snapYamlMonitoredAppFormat, "held-with-app-running", "1"), revno)
	s.serveSnap(snapPath, revno)

	// auto-refresh
	affected, tasksets, err := snapstate.AutoRefresh(context.TODO(), st)
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, []string{"held-with-app-running"})
	chg := st.NewChange("refresh-snaps", "...")
	for _, taskset := range append(tasksets.PreDownload, tasksets.Refresh...) {
		chg.AddAll(taskset)
	}

	// mock a running process in the snap's context
	snaptest.PopulateDir(dirs.GlobalRootDir, [][]string{
		{"/sys/fs/cgroup/snap.held-with-app-running.app.scope/cgroup.procs", "1234\n"},
	})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	dumpTasks(c, "after settle", chg.Tasks())
	c.Assert(chg.Err(), ErrorMatches, `(?s).*snap "held-with-app-running" has running apps.*`)

	// app was not refreshed
	si, err := snapstate.CurrentInfo(st, "held-with-app-running")
	c.Assert(err, IsNil)
	c.Check(si.Revision, Equals, snap.R(1))

	var candidates map[string]interface{}
	st.Get("refresh-candidates", &candidates)
	c.Logf("candidates: %v", candidates)

	// refresh-candidate should not be empty
	c.Assert(candidates, HasLen, 1)

	// what's supposed to happen now is the file removal will be noticed, a
	// change will be kicked off and the snap will get a refresh
	st.Unlock()
	s.o.Loop()

	// remove the entry as if the app has closed
	err = os.RemoveAll(filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/snap.held-with-app-running.app.scope"))
	c.Assert(err, IsNil)

	chg = waitForReadyChangeKind(c, st, "auto-refresh")
	err = s.o.Stop()
	c.Assert(err, IsNil)
	st.Lock()
	// we found the change
	c.Assert(chg, NotNil)
	dumpTasks(c, "after loop", chg.Tasks())
	c.Check(chg.Err(), IsNil)

	// app got refreshed
	si, err = snapstate.CurrentInfo(st, "held-with-app-running")
	c.Assert(err, IsNil)
	c.Check(si.Revision, Equals, snap.R(2))
}

func (s *mgrsSuite) TestAutoRefreshWithMonitoring(c *C) {
	// similar to a test with a single snap, but 2 snaps are being
	// refreshed, one refresh gets fully completed while the other is held
	// back due to a running application
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	err := assertstate.Add(st, s.devAcct)
	c.Assert(err, IsNil)

	snapNames := []string{"aaaa", "held-with-app-running"}
	for _, name := range snapNames {
		rev := snap.R(1)
		snapDecl := s.prereqSnapAssertions(c, map[string]interface{}{"snap-name": name})
		err = assertstate.Add(st, snapDecl)
		c.Assert(err, IsNil)

		s.mockInstalledSnapWithRevAndFiles(c,
			fmt.Sprintf(snapYamlMonitoredAppFormat, name, "0"), rev, nil)
	}

	// now add some more snap revisions
	revno := "2"
	for _, name := range snapNames {
		snapPath, _ := s.makeStoreTestSnap(c,
			fmt.Sprintf(snapYamlMonitoredAppFormat, name, "1"), revno)
		s.serveSnap(snapPath, revno)
	}

	// auto-refresh
	// auto-refresh
	affected, tasksets, err := snapstate.AutoRefresh(context.TODO(), st)
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, snapNames)
	chg := st.NewChange("refresh-snaps", "...")
	for _, taskset := range append(tasksets.PreDownload, tasksets.Refresh...) {
		chg.AddAll(taskset)
	}

	// mock a running process in the snap's context
	snaptest.PopulateDir(dirs.GlobalRootDir, [][]string{
		{"/sys/fs/cgroup/snap.held-with-app-running.app.scope/cgroup.procs", "1234\n"},
	})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	dumpTasks(c, "after settle", chg.Tasks())
	c.Assert(chg.Err(), ErrorMatches, `(?s).*snap "held-with-app-running" has running apps.*`)

	si, err := snapstate.CurrentInfo(st, "aaaa")
	c.Assert(err, IsNil)
	// aaaa snap was refreshed
	c.Check(si.Revision, Equals, snap.R(2))

	// but the other snap was held
	si, err = snapstate.CurrentInfo(st, "held-with-app-running")
	c.Assert(err, IsNil)
	c.Check(si.Revision, Equals, snap.R(1))

	// state information about snap being monitored is supposed to be preserved
	var candidates map[string]interface{}
	st.Get("refresh-candidates", &candidates)
	c.Logf("candidates: %v", candidates)

	// refresh-candidate should not be empty
	c.Check(candidates, HasLen, 1)

	// what's supposed to happen now is the file removal will be noticed, a
	// change will be kicked off and the snap will get a refresh
	st.Unlock()
	s.o.Loop()

	// remove the entry as if the app has closed
	err = os.RemoveAll(filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/snap.held-with-app-running.app.scope"))
	c.Assert(err, IsNil)

	chg = waitForReadyChangeKind(c, st, "auto-refresh")
	err = s.o.Stop()
	c.Assert(err, IsNil)
	st.Lock()
	// we found the change
	c.Assert(chg, NotNil)
	dumpTasks(c, "after loop", chg.Tasks())
	c.Check(chg.Err(), IsNil)

	// app got refreshed
	si, err = snapstate.CurrentInfo(st, "held-with-app-running")
	c.Assert(err, IsNil)
	c.Check(si.Revision, Equals, snap.R(2))
}

func (s *mgrsSuite) TestAutoRefreshStoreUpdateWhileWaitingWithMonitoring(c *C) {
	// similar to a test with many snaps, but a new revision of a snap gets
	// published to the store, while we are waiting for the app to close, as
	// a result it should be picked up if refresh hints run in the meantime
	// and the unblocked refresh should update to that revision
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	err := assertstate.Add(st, s.devAcct)
	c.Assert(err, IsNil)

	snapNames := []string{"aaaa", "held-with-app-running"}
	for _, name := range snapNames {
		rev := snap.R(1)
		snapDecl := s.prereqSnapAssertions(c, map[string]interface{}{"snap-name": name})
		err = assertstate.Add(st, snapDecl)
		c.Assert(err, IsNil)

		s.mockInstalledSnapWithRevAndFiles(c,
			fmt.Sprintf(snapYamlMonitoredAppFormat, name, "0"), rev, nil)
	}

	// now add some more snap revisions
	revno := "2"
	for _, name := range snapNames {
		snapPath, _ := s.makeStoreTestSnap(c,
			fmt.Sprintf(snapYamlMonitoredAppFormat, name, "1"), revno)
		s.serveSnap(snapPath, revno)
	}

	// auto-refresh
	affected, tasksets, err := snapstate.AutoRefresh(context.TODO(), st)
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Check(affected, DeepEquals, snapNames)
	chg := st.NewChange("refresh-snaps", "...")
	for _, taskset := range append(tasksets.PreDownload, tasksets.Refresh...) {
		chg.AddAll(taskset)
	}

	// mock a running process in the snap's context
	snaptest.PopulateDir(dirs.GlobalRootDir, [][]string{
		{"/sys/fs/cgroup/snap.held-with-app-running.app.scope/cgroup.procs", "1234\n"},
	})

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	dumpTasks(c, "after settle", chg.Tasks())
	c.Assert(chg.Err(), ErrorMatches, `(?s).*snap "held-with-app-running" has running apps.*`)

	si, err := snapstate.CurrentInfo(st, "aaaa")
	c.Assert(err, IsNil)
	// aaaa snap was refreshed
	c.Check(si.Revision, Equals, snap.R(2))

	// but the other snap was held
	si, err = snapstate.CurrentInfo(st, "held-with-app-running")
	c.Assert(err, IsNil)
	c.Check(si.Revision, Equals, snap.R(1))

	// state information about snap being monitored is supposed to be preserved
	var candidates map[string]interface{}
	st.Get("refresh-candidates", &candidates)
	c.Logf("candidates: %v", candidates)

	// refresh-candidate should not be empty
	c.Check(candidates, HasLen, 1)
	c.Assert(candidates["held-with-app-running"], NotNil)

	c.Logf("create another mock revision")

	// while refresh candidates has been populated, bump the revision of the
	// snap being held
	snapPath, _ := s.makeStoreTestSnap(c,
		fmt.Sprintf(snapYamlMonitoredAppFormat, "held-with-app-running", "1.1"), "3")
	s.serveSnap(snapPath, "3")

	// pretend we can auto-refresh
	triggered := false
	snapstate.CanAutoRefresh = func(*state.State) (bool, error) {
		if !triggered {
			st.Set("last-refresh", time.Now().Add(-2*24*time.Hour))
			st.Set("last-refresh-hints", time.Now().Add(-2*24*time.Hour))
			triggered = true
		}
		return true, nil
	}

	// prevent catalog refresh
	c.Assert(os.MkdirAll(dirs.SnapCacheDir, 0755), IsNil)
	c.Assert(os.WriteFile(dirs.SnapNamesFile, nil, 0644), IsNil)

	st.Unlock()
	s.o.Settle(settleTimeout)
	st.Lock()

	dumpTasks(c, "after settle", chg.Tasks())
	c.Assert(chg.Err(), ErrorMatches, `(?s).*snap "held-with-app-running" has running apps.*`)

	// what's supposed to happen now is the file removal will be noticed, a
	// change will be kicked off and the snap will get a refresh
	st.Unlock()
	s.o.Loop()

	// remove the entry as if the app has closed, this can be done only
	// after calling Loop()
	err = os.RemoveAll(filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/snap.held-with-app-running.app.scope"))
	c.Assert(err, IsNil)

	chg = waitForReadyChangeKind(c, st, "auto-refresh")
	err = s.o.Stop()
	c.Assert(err, IsNil)
	st.Lock()
	// we found the change
	c.Assert(chg, NotNil)
	dumpTasks(c, "after loop", chg.Tasks())
	c.Check(chg.Err(), IsNil)

	// app got refreshed to the latest revision which was published while we
	// were waiting for processes to finish
	si, err = snapstate.CurrentInfo(st, "held-with-app-running")
	c.Assert(err, IsNil)
	c.Check(si.Revision, Equals, snap.R(3))
}

func (s *mgrsSuite) TestAutoRefreshStorePreDownloadWhileWaitingWithMonitoring(c *C) {
	// similar to a test which performs auto-refresh with a snap held back,
	// but this time, the snap is already active by the time we attempt
	// auto-refresh, so a pre-download change which will set up monitoring
	// in pre-download handler
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	mockServer := s.mockStore(c)
	defer mockServer.Close()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	err := assertstate.Add(st, s.devAcct)
	c.Assert(err, IsNil)

	snapNames := []string{"aaaa", "held-with-app-running"}
	for _, name := range snapNames {
		rev := snap.R(1)
		snapDecl := s.prereqSnapAssertions(c, map[string]interface{}{"snap-name": name})
		err = assertstate.Add(st, snapDecl)
		c.Assert(err, IsNil)

		s.mockInstalledSnapWithRevAndFiles(c,
			fmt.Sprintf(snapYamlMonitoredAppFormat, name, "0"), rev, nil)
	}

	// now add some more snap revisions
	revno := "2"
	for _, name := range snapNames {
		snapPath, _ := s.makeStoreTestSnap(c,
			fmt.Sprintf(snapYamlMonitoredAppFormat, name, "1"), revno)
		s.serveSnap(snapPath, revno)
	}

	// mock a running process in the snap's context before we try to launch auto-refresh
	snaptest.PopulateDir(dirs.GlobalRootDir, [][]string{
		{"/sys/fs/cgroup/snap.held-with-app-running.app.scope/cgroup.procs", "1234\n"},
	})

	// auto-refresh
	affected, tasksets, err := snapstate.AutoRefresh(context.TODO(), st)
	c.Assert(err, IsNil)
	sort.Strings(affected)
	// only the refreshed snap is treated as affected
	c.Check(affected, DeepEquals, []string{"aaaa"})
	// one snap has active apps and gets a pre-download change
	c.Check(len(tasksets.PreDownload) > 0, Equals, true)
	// other gets a full refresh
	c.Check(len(tasksets.Refresh) > 0, Equals, true)
	chgRef := st.NewChange("refresh-snaps", "...")
	for _, ts := range tasksets.Refresh {
		chgRef.AddAll(ts)
	}
	chgDl := st.NewChange("pre-download", "...")
	for _, ts := range tasksets.PreDownload {
		chgDl.AddAll(ts)
	}

	st.Unlock()
	err = s.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	dumpTasks(c, "after settle refresh-snaps", chgRef.Tasks())
	c.Assert(chgRef.Err(), IsNil)
	dumpTasks(c, "after settle pre-download", chgDl.Tasks())
	c.Assert(chgDl.Err(), IsNil)

	si, err := snapstate.CurrentInfo(st, "aaaa")
	c.Assert(err, IsNil)
	// aaaa snap was refreshed
	c.Check(si.Revision, Equals, snap.R(2))

	// the other snap is still unchanged
	si, err = snapstate.CurrentInfo(st, "held-with-app-running")
	c.Assert(err, IsNil)
	c.Check(si.Revision, Equals, snap.R(1))

	// the pre-download handler must have created a state entry for the held snap
	var candidates map[string]interface{}
	st.Get("refresh-candidates", &candidates)
	c.Logf("candidates: %v", candidates)

	// refresh-candidate should not be empty
	c.Check(candidates, HasLen, 1)
	c.Assert(candidates["held-with-app-running"], NotNil)

	// what's supposed to happen now is the file removal will be noticed, a
	// change will be kicked off and the snap will get a refresh
	st.Unlock()
	s.o.Loop()

	// remove the entry as if the app has closed, this can be done only
	// after calling Loop()
	err = os.RemoveAll(filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/snap.held-with-app-running.app.scope"))
	c.Assert(err, IsNil)

	chgAuto := waitForReadyChangeKind(c, st, "auto-refresh")
	err = s.o.Stop()
	c.Assert(err, IsNil)
	st.Lock()
	// we found the change
	c.Assert(chgAuto, NotNil)
	dumpTasks(c, "after loop", chgAuto.Tasks())
	c.Check(chgAuto.Err(), IsNil)

	// app got refreshed
	si, err = snapstate.CurrentInfo(st, "held-with-app-running")
	c.Assert(err, IsNil)
	c.Check(si.Revision, Equals, snap.R(2))
}

func waitForReadyChangeKind(c *C, st *state.State, kind string) (chg *state.Change) {
	checkTicker := time.NewTicker(time.Second)
	timeout := time.After(settleTimeout)
waitLoop:
	for {
		select {
		case <-checkTicker.C:
			st.Lock()
			if chg == nil {
				for _, maybeChg := range st.Changes() {
					if maybeChg.Kind() == kind {
						chg = maybeChg
						break
					}
				}
			}
			rdy := false
			if chg != nil {
				dumpTasks(c, "loop ticker", chg.Tasks())
				rdy = chg.IsReady()
			}
			st.Unlock()
			if rdy {
				break waitLoop
			}
		case <-timeout:
			c.Errorf("timeout waiting for change to complete")
			c.FailNow()
		}
	}

	c.Assert(chg, NotNil, Commentf("cannot find a ready change of kind %s", kind))
	return chg
}
