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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	snapshotbackend "github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type automaticSnapshotCall struct {
	InstanceName string
	SnapConfig   map[string]interface{}
	Usernames    []string
	Flags        *snapshotbackend.Flags
}

type mgrsSuite struct {
	tempdir string

	restore func()

	restoreSystemctl func()

	storeSigning   *assertstest.StoreStack
	restoreTrusted func()
	mockSnapCmd    *testutil.MockCmd

	devAcct *asserts.Account

	serveIDtoName map[string]string
	serveSnapPath map[string]string
	serveRevision map[string]string
	serveOldPaths map[string][]string
	serveOldRevs  map[string][]string

	hijackServeSnap func(http.ResponseWriter)

	o *overlord.Overlord

	failNextDownload string

	automaticSnapshots []automaticSnapshotCall

	restoreBackends func()
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

const (
	aggressiveSettleTimeout = 50 * time.Millisecond
	connectRetryTimeout     = 70 * time.Millisecond
)

func verifyLastTasksetIsRerefresh(c *C, tts []*state.TaskSet) {
	ts := tts[len(tts)-1]
	c.Assert(ts.Tasks(), HasLen, 1)
	c.Check(ts.Tasks()[0].Kind(), Equals, "check-rerefresh")
}

func (ms *mgrsSuite) SetUpTest(c *C) {
	ms.tempdir = c.MkDir()
	dirs.SetRootDir(ms.tempdir)
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)

	// needed by hooks
	ms.mockSnapCmd = testutil.MockCommand(c, "snap", "")

	oldSetupInstallHook := snapstate.SetupInstallHook
	oldSetupRemoveHook := snapstate.SetupRemoveHook
	snapstate.SetupRemoveHook = hookstate.SetupRemoveHook
	snapstate.SetupInstallHook = hookstate.SetupInstallHook

	ms.automaticSnapshots = nil
	restoreBackendSave := snapshotstate.MockBackendSave(func(_ context.Context, id uint64, si *snap.Info, cfg map[string]interface{}, usernames []string, flags *snapshotbackend.Flags) (*client.Snapshot, error) {
		ms.automaticSnapshots = append(ms.automaticSnapshots, automaticSnapshotCall{InstanceName: si.InstanceName(), SnapConfig: cfg, Usernames: usernames, Flags: flags})
		return nil, nil
	})

	restoreConnectRetryTimeout := ifacestate.MockConnectRetryTimeout(connectRetryTimeout)

	ms.restore = func() {
		snapstate.SetupRemoveHook = oldSetupRemoveHook
		snapstate.SetupInstallHook = oldSetupInstallHook
		restoreBackendSave()
		restoreConnectRetryTimeout()
	}

	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")

	// create a fake systemd environment
	os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755)

	ms.restoreSystemctl = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	})

	ms.storeSigning = assertstest.NewStoreStack("can0nical", nil)
	ms.restoreTrusted = sysdb.InjectTrusted(ms.storeSigning.Trusted)

	ms.devAcct = assertstest.NewAccount(ms.storeSigning, "devdevdev", map[string]interface{}{
		"account-id": "devdevdev",
	}, "")
	err = ms.storeSigning.Add(ms.devAcct)
	c.Assert(err, IsNil)

	ms.serveIDtoName = make(map[string]string)
	ms.serveSnapPath = make(map[string]string)
	ms.serveRevision = make(map[string]string)
	ms.serveOldPaths = make(map[string][]string)
	ms.serveOldRevs = make(map[string][]string)
	ms.hijackServeSnap = nil

	ms.restoreBackends = ifacestate.MockSecurityBackends(nil)

	o, err := overlord.New()
	c.Assert(err, IsNil)
	o.InterfaceManager().DisableUDevMonitor()
	ms.o = o
	st := ms.o.State()
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
	err = assertstate.Add(st, ms.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	a, err := ms.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(st, a)
	c.Assert(err, IsNil)
	ms.serveRevision["core"] = "1"
	ms.serveIDtoName[fakeSnapID("core")] = "core"
	err = ms.storeSigning.Add(a)
	c.Assert(err, IsNil)

	// add "snap1" snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "snap1",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a2, err := ms.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a2), IsNil)
	c.Assert(ms.storeSigning.Add(a2), IsNil)

	// add "snap2" snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "snap2",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a3, err := ms.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a3), IsNil)
	c.Assert(ms.storeSigning.Add(a3), IsNil)

	// add "some-snap" snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "some-snap",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a4, err := ms.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a4), IsNil)
	c.Assert(ms.storeSigning.Add(a4), IsNil)

	// add "other-snap" snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "other-snap",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a5, err := ms.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a5), IsNil)
	c.Assert(ms.storeSigning.Add(a5), IsNil)

	// add pc-kernel snap declaration
	headers = map[string]interface{}{
		"series":       "16",
		"snap-name":    "pc-kernel",
		"publisher-id": "can0nical",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	headers["snap-id"] = fakeSnapID(headers["snap-name"].(string))
	a6, err := ms.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(st, a6), IsNil)
	c.Assert(ms.storeSigning.Add(a6), IsNil)

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
}

func (ms *mgrsSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	ms.restoreTrusted()
	ms.restore()
	ms.restoreSystemctl()
	ms.mockSnapCmd.Restore()
	os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS")
	ms.restoreBackends()
}

var settleTimeout = 15 * time.Second

func makeTestSnap(c *C, snapYamlContent string) string {
	info, err := snap.InfoFromSnapYaml([]byte(snapYamlContent))
	c.Assert(err, IsNil)

	var files [][]string
	for _, app := range info.Apps {
		// files is a list of (filename, content)
		files = append(files, []string{app.Command, ""})
	}

	return snaptest.MakeTestSnapWithFiles(c, snapYamlContent, files)
}

func (ms *mgrsSuite) TestHappyLocalInstall(c *C) {
	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
`
	snapPath := makeTestSnap(c, snapYamlContent+"version: 1.0")

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "foo"}, snapPath, "", "", snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestHappyRemove(c *C) {
	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
`
	snapInfo := ms.installLocalTestSnap(c, snapYamlContent+"version: 1.0")

	// set config
	tr := config.NewTransaction(st)
	c.Assert(tr.Set("foo", "key", "value"), IsNil)
	tr.Commit()

	ts, err := snapstate.Remove(st, "foo", snap.R(0))
	c.Assert(err, IsNil)
	chg := st.NewChange("remove-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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
	c.Assert(ms.automaticSnapshots, DeepEquals, []automaticSnapshotCall{{"foo", map[string]interface{}{"key": "value"}, nil, &snapshotbackend.Flags{Auto: true}}})
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
            "url": "@URL@"
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

func (ms *mgrsSuite) prereqSnapAssertions(c *C, extraHeaders ...map[string]interface{}) *asserts.SnapDeclaration {
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
		a, err := ms.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
		c.Assert(err, IsNil)
		err = ms.storeSigning.Add(a)
		c.Assert(err, IsNil)
		snapDecl = a.(*asserts.SnapDeclaration)
	}
	return snapDecl
}

func (ms *mgrsSuite) makeStoreTestSnap(c *C, snapYaml string, revno string) (path, digest string) {
	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)

	snapPath := makeTestSnap(c, snapYaml)

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
	snapRev, err := ms.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = ms.storeSigning.Add(snapRev)
	c.Assert(err, IsNil)

	return snapPath, snapDigest
}

func (ms *mgrsSuite) pathFor(name, revno string) string {
	if revno == ms.serveRevision[name] {
		return ms.serveSnapPath[name]
	}
	for i, r := range ms.serveOldRevs[name] {
		if r == revno {
			return ms.serveOldPaths[name][i]
		}
	}
	return "/not/found"
}

func (ms *mgrsSuite) newestThatCanRead(name string, epoch snap.Epoch) (info *snap.Info, rev string) {
	if ms.serveSnapPath[name] == "" {
		return nil, ""
	}
	idx := len(ms.serveOldPaths[name])
	rev = ms.serveRevision[name]
	path := ms.serveSnapPath[name]
	for {
		snapf, err := snap.Open(path)
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
		path = ms.serveOldPaths[name][idx]
		rev = ms.serveOldRevs[name][idx]
	}
}

func (ms *mgrsSuite) mockStore(c *C) *httptest.Server {
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
		hit = strings.Replace(hit, `@TYPE@`, string(info.Type), -1)
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
		} else { // v2
			if len(comps) <= 3 {
				panic("unexpected url path: " + r.URL.Path)
			}
			comps = comps[3:]
			comps[0] = "v2:" + comps[0]
		}

		switch comps[0] {
		case "assertions":
			ref := &asserts.Ref{
				Type:       asserts.Type(comps[1]),
				PrimaryKey: comps[2:],
			}
			a, err := ref.Resolve(ms.storeSigning.Find)
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
			if ms.failNextDownload == comps[1] {
				ms.failNextDownload = ""
				w.WriteHeader(418)
				return
			}
			if ms.hijackServeSnap != nil {
				ms.hijackServeSnap(w)
				return
			}
			snapR, err := os.Open(ms.pathFor(comps[1], comps[2]))
			if err != nil {
				panic(err)
			}
			io.Copy(w, snapR)
		case "v2:refresh":
			dec := json.NewDecoder(r.Body)
			var input struct {
				Actions []struct {
					Action      string     `json:"action"`
					SnapID      string     `json:"snap-id"`
					Name        string     `json:"name"`
					InstanceKey string     `json:"instance-key"`
					Epoch       snap.Epoch `json:"epoch"`
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
			}
			var results []resultJSON
			for _, a := range input.Actions {
				name := ms.serveIDtoName[a.SnapID]
				epoch := id2epoch[a.SnapID]
				if a.Action == "install" {
					name = a.Name
					epoch = a.Epoch
				}

				info, revno := ms.newestThatCanRead(name, epoch)
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
	st := ms.o.State()
	st.Lock()
	snapstate.ReplaceStore(ms.o.State(), mStore)
	st.Unlock()

	return mockServer
}

// serveSnap starts serving the snap at snapPath, moving the current
// one onto the list of previous ones if already set.
func (ms *mgrsSuite) serveSnap(snapPath, revno string) {
	snapf, err := snap.Open(snapPath)
	if err != nil {
		panic(err)
	}
	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	if err != nil {
		panic(err)
	}
	name := info.SnapName()
	ms.serveIDtoName[fakeSnapID(name)] = name

	if oldPath := ms.serveSnapPath[name]; oldPath != "" {
		oldRev := ms.serveRevision[name]
		if oldRev == "" {
			panic("old path set but not old revision")
		}
		ms.serveOldPaths[name] = append(ms.serveOldPaths[name], oldPath)
		ms.serveOldRevs[name] = append(ms.serveOldRevs[name], oldRev)
	}
	ms.serveSnapPath[name] = snapPath
	ms.serveRevision[name] = revno
}

func (ms *mgrsSuite) TestHappyRemoteInstallAndUpgradeSvc(c *C) {
	// test install through store and update, plus some mechanics
	// of update
	// TODO: ok to split if it gets too messy to maintain

	ms.prereqSnapAssertions(c)

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
	snapPath, digest := ms.makeStoreTestSnap(c, strings.Replace(snapYamlContent, "@VERSION@", ver, -1), revno)
	ms.serveSnap(snapPath, revno)

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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
	snapPath, digest = ms.makeStoreTestSnap(c, strings.Replace(snapYamlContent, "@VERSION@", ver, -1), revno)
	ms.serveSnap(snapPath, revno)

	ts, err = snapstate.Update(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg = st.NewChange("upgrade-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestHappyRemoteInstallAndUpdateWithEpochBump(c *C) {
	// test install through store and update, where there's an epoch bump in the upgrade
	// this does less checks on the details of install/update than TestHappyRemoteInstallAndUpgradeSvc

	ms.prereqSnapAssertions(c)

	snapPath, _ := ms.makeStoreTestSnap(c, "{name: foo, version: 0}", "1")
	ms.serveSnap(snapPath, "1")

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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
		snapPath, _ := ms.makeStoreTestSnap(c, "{name: foo, version: 0, epoch: "+epoch+"}", revno)
		ms.serveSnap(snapPath, revno)
	}

	// refresh

	ts, err = snapstate.Update(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg = st.NewChange("upgrade-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestHappyRemoteInstallAndUpdateWithPostHocEpochBump(c *C) {
	// test install through store and update, where there is an epoch
	// bump in the upgrade that comes in after the initial update is
	// computed.

	// this is mostly checking the same as TestHappyRemoteInstallAndUpdateWithEpochBump
	// but serves as a sanity check for the Without case that follows
	// (these two together serve as a test for the refresh filtering)
	ms.testHappyRemoteInstallAndUpdateWithMaybeEpochBump(c, true)
}

func (ms *mgrsSuite) TestHappyRemoteInstallAndUpdateWithoutEpochBump(c *C) {
	// test install through store and update, where there _isn't_ an epoch bump in the upgrade
	// note that there _are_ refreshes available after the refresh,
	// but they're not an epoch bump so they're ignored
	ms.testHappyRemoteInstallAndUpdateWithMaybeEpochBump(c, false)
}

func (ms *mgrsSuite) testHappyRemoteInstallAndUpdateWithMaybeEpochBump(c *C, doBump bool) {
	ms.prereqSnapAssertions(c)

	snapPath, _ := ms.makeStoreTestSnap(c, "{name: foo, version: 1}", "1")
	ms.serveSnap(snapPath, "1")

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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
	snapPath, _ = ms.makeStoreTestSnap(c, "{name: foo, version: 2}", "2")
	ms.serveSnap(snapPath, "2")

	// refresh

	ts, err = snapstate.Update(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg = st.NewChange("upgrade-snap", "...")
	chg.AddAll(ts)

	// add another new revision, after the update was computed (maybe with an epoch bump)
	if doBump {
		snapPath, _ = ms.makeStoreTestSnap(c, "{name: foo, version: 3, epoch: 1*}", "3")
	} else {
		snapPath, _ = ms.makeStoreTestSnap(c, "{name: foo, version: 3}", "3")
	}
	ms.serveSnap(snapPath, "3")

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestHappyRemoteInstallAndUpdateManyWithEpochBump(c *C) {
	// test install through store and update many, where there's an epoch bump in the upgrade
	// this does less checks on the details of install/update than TestHappyRemoteInstallAndUpgradeSvc

	snapNames := []string{"aaaa", "bbbb", "cccc"}
	for _, name := range snapNames {
		ms.prereqSnapAssertions(c, map[string]interface{}{"snap-name": name})
		snapPath, _ := ms.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 0}", name), "1")
		ms.serveSnap(snapPath, "1")
	}

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
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
	err = ms.o.Settle(settleTimeout)
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
			snapPath, _ := ms.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 0, epoch: %s}", name, epoch), revno)
			ms.serveSnap(snapPath, revno)
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
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestHappyRemoteInstallAndUpdateManyWithEpochBumpAndOneFailing(c *C) {
	// test install through store and update, where there's an epoch bump in the upgrade and one of them fails

	snapNames := []string{"aaaa", "bbbb", "cccc"}
	for _, name := range snapNames {
		ms.prereqSnapAssertions(c, map[string]interface{}{"snap-name": name})
		snapPath, _ := ms.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 0}", name), "1")
		ms.serveSnap(snapPath, "1")
	}

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
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
	err = ms.o.Settle(settleTimeout)
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
			snapPath, _ := ms.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 0, epoch: %s}", name, epoch), revno)
			ms.serveSnap(snapPath, revno)
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
	ms.failNextDownload = "cccc"
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestHappyLocalInstallWithStoreMetadata(c *C) {
	snapDecl := ms.prereqSnapAssertions(c)

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

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// have the snap-declaration in the system db
	err := assertstate.Add(st, ms.devAcct)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, snapDecl)
	c.Assert(err, IsNil)

	ts, _, err := snapstate.InstallPath(st, si, snapPath, "", "", snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestParallelInstanceLocalInstallSnapNameMismatch(c *C) {
	snapDecl := ms.prereqSnapAssertions(c)

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

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// have the snap-declaration in the system db
	err := assertstate.Add(st, ms.devAcct)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, snapDecl)
	c.Assert(err, IsNil)

	_, _, err = snapstate.InstallPath(st, si, snapPath, "bar_instance", "", snapstate.Flags{DevMode: true})
	c.Assert(err, ErrorMatches, `cannot install snap "bar_instance", the name does not match the metadata "foo"`)
}

func (ms *mgrsSuite) TestParallelInstanceLocalInstallInvalidInstanceName(c *C) {
	snapDecl := ms.prereqSnapAssertions(c)

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

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// have the snap-declaration in the system db
	err := assertstate.Add(st, ms.devAcct)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, snapDecl)
	c.Assert(err, IsNil)

	_, _, err = snapstate.InstallPath(st, si, snapPath, "bar_invalid_instance_name", "", snapstate.Flags{DevMode: true})
	c.Assert(err, ErrorMatches, `invalid instance name: invalid instance key: "invalid_instance_name"`)
}

func (ms *mgrsSuite) TestCheckInterfaces(c *C) {
	snapDecl := ms.prereqSnapAssertions(c)

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

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// have the snap-declaration in the system db
	err := assertstate.Add(st, ms.devAcct)
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
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), ErrorMatches, `(?s).*installation not allowed by "network" slot rule of interface "network".*`)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
}

func (ms *mgrsSuite) TestHappyRefreshControl(c *C) {
	// test install through store and update, plus some mechanics
	// of update
	// TODO: ok to split if it gets too messy to maintain

	ms.prereqSnapAssertions(c)

	snapYamlContent := `name: foo
version: @VERSION@
`

	ver := "1.0"
	revno := "42"
	snapPath, _ := ms.makeStoreTestSnap(c, strings.Replace(snapYamlContent, "@VERSION@", ver, -1), revno)
	ms.serveSnap(snapPath, revno)

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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
	snapDeclBar, err := ms.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = ms.storeSigning.Add(snapDeclBar)
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

	develAccKey := assertstest.NewAccountKey(ms.storeSigning, ms.devAcct, nil, develPrivKey.PublicKey(), "")
	err = ms.storeSigning.Add(develAccKey)
	c.Assert(err, IsNil)

	ver = "2.0"
	revno = "50"
	snapPath, _ = ms.makeStoreTestSnap(c, strings.Replace(snapYamlContent, "@VERSION@", ver, -1), revno)
	ms.serveSnap(snapPath, revno)

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
	err = ms.storeSigning.Add(barValidation)
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
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	info, err = snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)

	c.Check(info.Revision, Equals, snap.R(50))
}

// core & kernel

func findKind(chg *state.Change, kind string) *state.Task {
	for _, t := range chg.Tasks() {
		if t.Kind() == kind {
			return t
		}
	}
	return nil
}

func (ms *mgrsSuite) TestInstallCoreSnapUpdatesBootloaderAndSplitsAcrossRestart(c *C) {
	loader := boottest.NewMockBootloader("mock", c.MkDir())
	bootloader.Force(loader)
	defer bootloader.Force(nil)

	restore := release.MockOnClassic(false)
	defer restore()

	brandAcct := assertstest.NewAccount(ms.storeSigning, "my-brand", map[string]interface{}{
		"account-id":   "my-brand",
		"verification": "verified",
	}, "")
	brandAccKey := assertstest.NewAccountKey(ms.storeSigning, brandAcct, nil, brandPrivKey.PublicKey(), "")

	brandSigning := assertstest.NewSigningDB("my-brand", brandPrivKey)
	model := makeModelAssertion(c, brandSigning, nil)

	const packageOS = `
name: core
version: 16.04-1
type: os
`
	snapPath := makeTestSnap(c, packageOS)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// setup model assertion
	err := assertstate.Add(st, brandAcct)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, brandAccKey)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "core"}, snapPath, "", "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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
	c.Assert(loader.BootVars, DeepEquals, map[string]string{
		"snap_try_core": "core_x1.snap",
		"snap_mode":     "try",
	})

	// simulate successful restart happened
	state.MockRestarting(st, state.RestartUnset)
	loader.BootVars["snap_mode"] = ""
	loader.BootVars["snap_core"] = "core_x1.snap"

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

}

func (ms *mgrsSuite) TestInstallKernelSnapUpdatesBootloader(c *C) {
	loader := boottest.NewMockBootloader("mock", c.MkDir())
	bootloader.Force(loader)
	defer bootloader.Force(nil)

	restore := release.MockOnClassic(false)
	defer restore()

	brandAcct := assertstest.NewAccount(ms.storeSigning, "my-brand", map[string]interface{}{
		"account-id":   "my-brand",
		"verification": "verified",
	}, "")
	brandAccKey := assertstest.NewAccountKey(ms.storeSigning, brandAcct, nil, brandPrivKey.PublicKey(), "")

	brandSigning := assertstest.NewSigningDB("my-brand", brandPrivKey)
	model := makeModelAssertion(c, brandSigning, nil)

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

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// setup model assertion
	err := assertstate.Add(st, brandAcct)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, brandAccKey)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	ts, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "pc-kernel"}, snapPath, "", "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	// run, this will trigger a wait for the restart
	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// we are in restarting state and the change is not done yet
	restarting, _ := st.Restarting()
	c.Check(restarting, Equals, true)
	c.Check(chg.Status(), Equals, state.DoingStatus)
	// pretend we restarted
	state.MockRestarting(st, state.RestartUnset)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	c.Assert(loader.BootVars, DeepEquals, map[string]string{
		"snap_try_kernel": "pc-kernel_x1.snap",
		"snap_mode":       "try",
	})
}

func (ms *mgrsSuite) installLocalTestSnap(c *C, snapYamlContent string) *snap.Info {
	st := ms.o.State()

	snapPath := makeTestSnap(c, snapYamlContent)
	snapf, err := snap.Open(snapPath)
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
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	return info
}

func (ms *mgrsSuite) removeSnap(c *C, name string) {
	st := ms.o.State()

	ts, err := snapstate.Remove(st, name, snap.R(0))
	c.Assert(err, IsNil)
	chg := st.NewChange("remove-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("remove-snap change failed with: %v", chg.Err()))
}

func (ms *mgrsSuite) TestHappyRevert(c *C) {
	st := ms.o.State()
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

	ms.installLocalTestSnap(c, x1Yaml)
	ms.installLocalTestSnap(c, x2Yaml)

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
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestHappyAlias(c *C) {
	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	fooYaml := `name: foo
version: 1.0
apps:
    foo:
        command: bin/foo
`
	ms.installLocalTestSnap(c, fooYaml)

	ts, err := snapstate.Alias(st, "foo", "foo", "foo_")
	c.Assert(err, IsNil)
	chg := st.NewChange("alias", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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

	ms.removeSnap(c, "foo")

	c.Check(osutil.IsSymlink(foo_Alias), Equals, false)
}

func (ms *mgrsSuite) TestHappyUnalias(c *C) {
	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	fooYaml := `name: foo
version: 1.0
apps:
    foo:
        command: bin/foo
`
	ms.installLocalTestSnap(c, fooYaml)

	ts, err := snapstate.Alias(st, "foo", "foo", "foo_")
	c.Assert(err, IsNil)
	chg := st.NewChange("alias", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestHappyRemoteInstallAutoAliases(c *C) {
	ms.prereqSnapAssertions(c, map[string]interface{}{
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
	snapPath, _ := ms.makeStoreTestSnap(c, strings.Replace(snapYamlContent, "@VERSION@", ver, -1), revno)
	ms.serveSnap(snapPath, revno)

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestHappyRemoteInstallAndUpdateAutoAliases(c *C) {
	ms.prereqSnapAssertions(c, map[string]interface{}{
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

	fooPath, _ := ms.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.0", -1), "10")
	ms.serveSnap(fooPath, "10")

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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

	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
		"aliases": []interface{}{
			map[string]interface{}{"name": "app2", "target": "app2"},
		},
		"revision": "1",
	})

	// new foo version/revision
	fooPath, _ = ms.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.5", -1), "15")
	ms.serveSnap(fooPath, "15")

	// refresh all
	updated, tss, err := snapstate.UpdateMany(context.TODO(), st, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(updated, DeepEquals, []string{"foo"})
	c.Assert(tss, HasLen, 2)
	verifyLastTasksetIsRerefresh(c, tss)
	chg = st.NewChange("upgrade-snaps", "...")
	chg.AddAll(tss[0])

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestHappyRemoteInstallAndUpdateAutoAliasesUnaliased(c *C) {
	ms.prereqSnapAssertions(c, map[string]interface{}{
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

	fooPath, _ := ms.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.0", -1), "10")
	ms.serveSnap(fooPath, "10")

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{Unaliased: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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

	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
		"aliases": []interface{}{
			map[string]interface{}{"name": "app2", "target": "app2"},
		},
		"revision": "1",
	})

	// new foo version/revision
	fooPath, _ = ms.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.5", -1), "15")
	ms.serveSnap(fooPath, "15")

	// refresh foo
	ts, err = snapstate.Update(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg = st.NewChange("upgrade-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestHappyOrthogonalRefreshAutoAliases(c *C) {
	ms.prereqSnapAssertions(c, map[string]interface{}{
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

	fooPath, _ := ms.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.0", -1), "10")
	ms.serveSnap(fooPath, "10")

	barPath, _ := ms.makeStoreTestSnap(c, strings.Replace(barYaml, "@VERSION@", "2.0", -1), "20")
	ms.serveSnap(barPath, "20")

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	ts, err = snapstate.Install(st, "bar", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg = st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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
	ms.prereqSnapAssertions(c, map[string]interface{}{
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
	fooPath, _ = ms.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.5", -1), "15")
	ms.serveSnap(fooPath, "15")

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
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestHappyStopWhileDownloadingHeader(c *C) {
	ms.prereqSnapAssertions(c)

	snapYamlContent := `name: foo
version: 1.0
`
	snapPath, _ := ms.makeStoreTestSnap(c, snapYamlContent, "42")
	ms.serveSnap(snapPath, "42")

	stopped := make(chan struct{})
	ms.hijackServeSnap = func(_ http.ResponseWriter) {
		ms.o.Stop()
		close(stopped)
	}

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	ms.o.Loop()

	<-stopped

	st.Lock()
	c.Assert(chg.Status(), Equals, state.DoingStatus, Commentf("install-snap change failed with: %v", chg.Err()))
}

func (ms *mgrsSuite) TestHappyStopWhileDownloadingBody(c *C) {
	ms.prereqSnapAssertions(c)

	snapYamlContent := `name: foo
version: 1.0
`
	snapPath, _ := ms.makeStoreTestSnap(c, snapYamlContent, "42")
	ms.serveSnap(snapPath, "42")

	stopped := make(chan struct{})
	ms.hijackServeSnap = func(w http.ResponseWriter) {
		w.WriteHeader(200)
		// best effort to reach the body reading part in the client
		w.Write(make([]byte, 10000))
		time.Sleep(100 * time.Millisecond)
		w.Write(make([]byte, 10000))
		ms.o.Stop()
		close(stopped)
	}

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.Install(st, "foo", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	ms.o.Loop()

	<-stopped

	st.Lock()
	c.Assert(chg.Status(), Equals, state.DoingStatus, Commentf("install-snap change failed with: %v", chg.Err()))
}

type storeCtxSetupSuite struct {
	o  *overlord.Overlord
	sc store.DeviceAndAuthContext

	storeSigning   *assertstest.StoreStack
	restoreTrusted func()

	brandSigning *assertstest.SigningDB
	deviceKey    asserts.PrivateKey

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

	s.brandSigning = assertstest.NewSigningDB("my-brand", brandPrivKey)

	brandAcct := assertstest.NewAccount(s.storeSigning, "my-brand", map[string]interface{}{
		"account-id":   "my-brand",
		"verification": "verified",
	}, "")
	s.storeSigning.Add(brandAcct)

	brandAccKey := assertstest.NewAccountKey(s.storeSigning, brandAcct, nil, brandPrivKey.PublicKey(), "")
	s.storeSigning.Add(brandAccKey)
	s.model = makeModelAssertion(c, s.brandSigning, nil)

	encDevKey, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, IsNil)
	serial, err := s.brandSigning.Sign(asserts.SerialType, map[string]interface{}{
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

	o, err := overlord.New()
	c.Assert(err, IsNil)
	o.InterfaceManager().DisableUDevMonitor()
	s.o = o

	st := o.State()
	st.Lock()
	defer st.Unlock()

	prereqs := []asserts.Assertion{s.storeSigning.StoreAccountKey(""), brandAcct, brandAccKey}
	for _, a := range prereqs {
		err = assertstate.Add(st, a)
		c.Assert(err, IsNil)
	}
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

func (ms *mgrsSuite) testTwoInstalls(c *C, snapName1, snapYaml1, snapName2, snapYaml2 string) {
	snapPath1 := makeTestSnap(c, snapYaml1+"version: 1.0")
	snapPath2 := makeTestSnap(c, snapYaml2+"version: 1.0")

	st := ms.o.State()
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
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	tasks := chg.Tasks()
	connectTask := tasks[len(tasks)-1]
	c.Assert(connectTask.Kind(), Equals, "connect")

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

	repo := ms.o.InterfaceManager().Repository()
	cn, err := repo.Connected("snap1", "shared-data-plug")
	c.Assert(err, IsNil)
	c.Assert(cn, HasLen, 1)
	c.Assert(cn, DeepEquals, []*interfaces.ConnRef{{
		PlugRef: interfaces.PlugRef{Snap: "snap1", Name: "shared-data-plug"},
		SlotRef: interfaces.SlotRef{Snap: "snap2", Name: "shared-data-slot"},
	}})
}

func (ms *mgrsSuite) TestTwoInstallsWithAutoconnectPlugSnapFirst(c *C) {
	ms.testTwoInstalls(c, "snap1", snapYamlContent1, "snap2", snapYamlContent2)
}

func (ms *mgrsSuite) TestTwoInstallsWithAutoconnectSlotSnapFirst(c *C) {
	ms.testTwoInstalls(c, "snap2", snapYamlContent2, "snap1", snapYamlContent1)
}

func (ms *mgrsSuite) TestRemoveAndInstallWithAutoconnectHappy(c *C) {
	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	_ = ms.installLocalTestSnap(c, snapYamlContent1+"version: 1.0")

	ts, err := snapstate.Remove(st, "snap1", snap.R(0))
	c.Assert(err, IsNil)
	chg := st.NewChange("remove-snap", "...")
	chg.AddAll(ts)

	snapPath := makeTestSnap(c, snapYamlContent2+"version: 1.0")
	chg2 := st.NewChange("install-snap", "...")
	ts2, _, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "snap2", SnapID: fakeSnapID("snap2"), Revision: snap.R(3)}, snapPath, "", "", snapstate.Flags{DevMode: true})
	chg2.AddAll(ts2)
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestUpdateManyWithAutoconnect(c *C) {
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

	snapPath, _ := ms.makeStoreTestSnap(c, someSnapYaml, "40")
	ms.serveSnap(snapPath, "40")

	snapPath, _ = ms.makeStoreTestSnap(c, otherSnapYaml, "50")
	ms.serveSnap(snapPath, "50")

	corePath, _ := ms.makeStoreTestSnap(c, strings.Replace(coreSnapYaml, "@VERSION@", "30", -1), "30")
	ms.serveSnap(corePath, "30")

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
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

	repo := ms.o.InterfaceManager().Repository()

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
	err = ms.o.Settle(3 * time.Second)
	st.Lock()
	c.Assert(err, IsNil)

	// simulate successful restart happened
	state.MockRestarting(st, state.RestartUnset)
	tts[2].Tasks()[0].SetStatus(state.DefaultStatus)
	st.Unlock()

	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestUpdateWithAutoconnectAndInactiveRevisions(c *C) {
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

	snapPath, _ := ms.makeStoreTestSnap(c, someSnapYaml, "40")
	ms.serveSnap(snapPath, "40")

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
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

	repo := ms.o.InterfaceManager().Repository()

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
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) testUpdateWithAutoconnectRetry(c *C, updateSnapName, removeSnapName string) {
	snapPath, _ := ms.makeStoreTestSnap(c, someSnapYaml, "40")
	ms.serveSnap(snapPath, "40")

	snapPath, _ = ms.makeStoreTestSnap(c, otherSnapYaml, "50")
	ms.serveSnap(snapPath, "50")

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
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

	repo := ms.o.InterfaceManager().Repository()

	// add snaps to the repo to have plugs/slots
	c.Assert(repo.AddSnap(snapInfo), IsNil)
	c.Assert(repo.AddSnap(otherInfo), IsNil)

	// refresh all
	err := assertstate.RefreshSnapDeclarations(st, 0)
	c.Assert(err, IsNil)

	ts, err := snapstate.Update(st, updateSnapName, "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	// to make TaskSnapSetup work
	chg := st.NewChange("refresh", "...")
	chg.AddAll(ts)

	// remove other-snap
	ts2, err := snapstate.Remove(st, removeSnapName, snap.R(0))
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
		ms.o.Settle(aggressiveSettleTimeout)
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
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Check(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// check connections
	var conns map[string]interface{}
	st.Get("conns", &conns)
	c.Assert(conns, HasLen, 0)
}

func (ms *mgrsSuite) TestUpdateWithAutoconnectRetrySlotSide(c *C) {
	ms.testUpdateWithAutoconnectRetry(c, "some-snap", "other-snap")
}

func (ms *mgrsSuite) TestUpdateWithAutoconnectRetryPlugSide(c *C) {
	ms.testUpdateWithAutoconnectRetry(c, "other-snap", "some-snap")
}

func (ms *mgrsSuite) TestDisconnectIgnoredOnSymmetricRemove(c *C) {
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
	st := ms.o.State()
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

	repo := ms.o.InterfaceManager().Repository()

	// add snaps to the repo to have plugs/slots
	c.Assert(repo.AddSnap(snapInfo), IsNil)
	c.Assert(repo.AddSnap(otherInfo), IsNil)
	repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "other-snap", Name: "media-hub"},
		SlotRef: interfaces.SlotRef{Snap: "some-snap", Name: "media-hub"},
	}, nil, nil, nil, nil, nil)

	ts, err := snapstate.Remove(st, "some-snap", snap.R(0))
	c.Assert(err, IsNil)
	chg := st.NewChange("uninstall", "...")
	chg.AddAll(ts)

	// remove other-snap
	ts2, err := snapstate.Remove(st, "other-snap", snap.R(0))
	c.Assert(err, IsNil)
	chg2 := st.NewChange("uninstall", "...")
	chg2.AddAll(ts2)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
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

func (ms *mgrsSuite) TestDisconnectOnUninstallRemovesAutoconnection(c *C) {
	st := ms.o.State()
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

	repo := ms.o.InterfaceManager().Repository()

	// add snaps to the repo to have plugs/slots
	c.Assert(repo.AddSnap(snapInfo), IsNil)
	c.Assert(repo.AddSnap(otherInfo), IsNil)
	repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "other-snap", Name: "media-hub"},
		SlotRef: interfaces.SlotRef{Snap: "some-snap", Name: "media-hub"},
	}, nil, nil, nil, nil, nil)

	ts, err := snapstate.Remove(st, "some-snap", snap.R(0))
	c.Assert(err, IsNil)
	chg := st.NewChange("uninstall", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// check connections; auto-connection should be removed completely from conns on uninstall.
	var conns map[string]interface{}
	st.Get("conns", &conns)
	c.Assert(conns, HasLen, 0)
}

// makeMockModel creates a model assertion using the given brandSigning DB.
// The fields can be customized with the modelExtra map.
func makeModelAssertion(c *C, brandSigning *assertstest.SigningDB, modelExtra map[string]interface{}) *asserts.Model {
	modelV := map[string]interface{}{
		"series":       "16",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"architecture": "amd64",
		"store":        "my-brand-store-id",
		"gadget":       "pc",
		"kernel":       "pc-kernel",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	for k, v := range modelExtra {
		modelV[k] = v
	}
	model, err := brandSigning.Sign(asserts.ModelType, modelV, nil, "")
	c.Assert(err, IsNil)

	return model.(*asserts.Model)
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

func validateInstallTasks(c *C, tasks []*state.Task, name, revno string) int {
	var i int
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Mount snap "%s" (%s)`, name, revno))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Copy snap "%s" data`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Setup snap "%s" (%s) security profiles`, name, revno))
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
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Run configure hook of "%s" snap if present`, name))
	i++
	return i
}

func validateRefreshTasks(c *C, tasks []*state.Task, name, revno string) int {
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
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Copy snap "%s" data`, name))
	i++
	c.Assert(tasks[i].Summary(), Equals, fmt.Sprintf(`Setup snap "%s" (%s) security profiles`, name, revno))
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
	return i
}

// byReadyTime sorts a list of tasks by their "ready" time
type byReadyTime []*state.Task

func (a byReadyTime) Len() int           { return len(a) }
func (a byReadyTime) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byReadyTime) Less(i, j int) bool { return a[i].ReadyTime().Before(a[j].ReadyTime()) }

func (ms *mgrsSuite) TestRemodelRequiredSnapsAdded(c *C) {
	for _, name := range []string{"foo", "bar", "baz"} {
		ms.prereqSnapAssertions(c, map[string]interface{}{
			"snap-name": name,
		})
		snapPath, _ := ms.makeStoreTestSnap(c, fmt.Sprintf("{name: %s, version: 1.0}", name), "1")
		ms.serveSnap(snapPath, "1")
	}

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
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
	brandAcct := assertstest.NewAccount(ms.storeSigning, "my-brand", map[string]interface{}{
		"account-id":   "my-brand",
		"verification": "verified",
	}, "")
	err := assertstate.Add(st, brandAcct)
	c.Assert(err, IsNil)
	brandAccKey := assertstest.NewAccountKey(ms.storeSigning, brandAcct, nil, brandPrivKey.PublicKey(), "")
	err = assertstate.Add(st, brandAccKey)
	c.Assert(err, IsNil)

	brandSigning := assertstest.NewSigningDB("my-brand", brandPrivKey)
	model := makeModelAssertion(c, brandSigning, nil)

	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserialserial",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// create a new model
	newModel := makeModelAssertion(c, brandSigning, map[string]interface{}{
		"required-snaps": []interface{}{"foo", "bar", "baz"},
		"revision":       "1",
	})

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

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
		i += validateInstallTasks(c, tasks[i:], name, "1")
	}
	// ensure that we only have the tasks we checked (plus the one
	// extra "set-model" task)
	c.Assert(tasks, HasLen, i+1)
}

func (ms *mgrsSuite) TestRemodelDifferentBase(c *C) {
	// make "core18" snap available in the store
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "core18",
	})
	snapYamlContent := `name: core18
version: 18.04
type: base`
	snapPath, _ := ms.makeStoreTestSnap(c, snapYamlContent, "18")
	ms.serveSnap(snapPath, "18")

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	// create/set custom model assertion
	model := makeModelAssertion(c, ms.storeSigning.SigningDB, map[string]interface{}{
		"authority-id": "can0nical",
		"brand-id":     "can0nical",
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
	newModel := makeModelAssertion(c, ms.storeSigning.SigningDB, map[string]interface{}{
		"authority-id": "can0nical",
		"brand-id":     "can0nical",
		"base":         "core18",
		"revision":     "1",
	})

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, ErrorMatches, "cannot remodel to different bases yet")
	c.Assert(chg, IsNil)
}

func (ms *mgrsSuite) TestRemodelSwitchKernelTrack(c *C) {
	loader := boottest.NewMockBootloader("mock", c.MkDir())
	bootloader.Force(loader)
	defer bootloader.Force(nil)

	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "pc-kernel", SnapID: fakeSnapID("pc-kernel"), Revision: snap.R(1)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "kernel",
	})

	const kernelYaml = `name: pc-kernel
type: kernel
version: 2.0`
	snapPath, _ := ms.makeStoreTestSnap(c, kernelYaml, "2")
	ms.serveSnap(snapPath, "2")

	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
	})
	snapPath, _ = ms.makeStoreTestSnap(c, `{name: "foo", version: 1.0}`, "1")
	ms.serveSnap(snapPath, "1")

	// create/set custom model assertion
	model := makeModelAssertion(c, ms.storeSigning.SigningDB, map[string]interface{}{
		"authority-id": "can0nical",
		"brand-id":     "can0nical",
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
	newModel := makeModelAssertion(c, ms.storeSigning.SigningDB, map[string]interface{}{
		"authority-id":   "can0nical",
		"brand-id":       "can0nical",
		"kernel":         "pc-kernel=18",
		"revision":       "1",
		"required-snaps": []interface{}{"foo"},
	})

	chg, err := devicestate.Remodel(st, newModel)
	c.Assert(err, IsNil)

	st.Unlock()
	err = ms.o.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// system waits for a restart because of the new kernel
	t := findKind(chg, "auto-connect")
	c.Assert(t, NotNil)
	c.Assert(t.Status(), Equals, state.DoingStatus)

	// simulate successful restart happened
	state.MockRestarting(st, state.RestartUnset)

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
	i += validateDownloadCheckTasks(c, tasks[i:], "pc-kernel", "2", "18")
	i += validateDownloadCheckTasks(c, tasks[i:], "foo", "1", "stable")

	// then all installs in sequential order
	i += validateRefreshTasks(c, tasks[i:], "pc-kernel", "2")
	i += validateInstallTasks(c, tasks[i:], "foo", "1")

	// ensure that we only have the tasks we checked (plus the one
	// extra "set-model" task)
	c.Assert(tasks, HasLen, i+1)
}

func (ms *mgrsSuite) TestRemodelSwitchToDifferentKernel(c *C) {
	loader := boottest.NewMockBootloader("mock", c.MkDir())
	bootloader.Force(loader)
	defer bootloader.Force(nil)

	restore := release.MockOnClassic(false)
	defer restore()

	mockServer := ms.mockStore(c)
	defer mockServer.Close()

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "pc-kernel", SnapID: fakeSnapID("pc-kernel"), Revision: snap.R(1)}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "kernel",
	})
	si2 := &snap.SideInfo{RealName: "pc", SnapID: fakeSnapID("pc"), Revision: snap.R(1)}
	gadgetYaml := "name: pc\nversion: 1.0\ntype: gadget"
	snaptest.MockSnap(c, gadgetYaml, si2)

	// add "brand-kernel" snap to fake store
	const brandKernelYaml = `name: brand-kernel
type: kernel
version: 1.0`
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "brand-kernel",
		"publisher-id": "can0nical",
	})
	snapPath, _ := ms.makeStoreTestSnap(c, brandKernelYaml, "2")
	ms.serveSnap(snapPath, "2")

	// add "foo" snap to fake store
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name": "foo",
	})
	snapPath, _ = ms.makeStoreTestSnap(c, `{name: "foo", version: 1.0}`, "1")
	ms.serveSnap(snapPath, "1")

	// create/set custom model assertion
	model := makeModelAssertion(c, ms.storeSigning.SigningDB, map[string]interface{}{
		"authority-id": "can0nical",
		"brand-id":     "can0nical",
	})
	// setup model assertion
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand: "can0nical",
		Model: "my-model",
	})
	err := assertstate.Add(st, model)
	c.Assert(err, IsNil)

	// create a new model
	newModel := makeModelAssertion(c, ms.storeSigning.SigningDB, map[string]interface{}{
		"authority-id":   "can0nical",
		"brand-id":       "can0nical",
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

	// simulate successful restart happened
	state.MockRestarting(st, state.RestartUnset)

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
	i += validateDownloadCheckTasks(c, tasks[i:], "brand-kernel", "2", "stable")
	i += validateDownloadCheckTasks(c, tasks[i:], "foo", "1", "stable")

	// then all installs in sequential order
	i += validateInstallTasks(c, tasks[i:], "brand-kernel", "2")
	i += validateInstallTasks(c, tasks[i:], "foo", "1")

	// ensure that we only have the tasks we checked (plus the one
	// extra "set-model" task)
	c.Assert(tasks, HasLen, i+1)
}

func (ms *mgrsSuite) TestHappyDeviceRegistrationWithPrepareDeviceHook(c *C) {
	brandAcct := assertstest.NewAccount(ms.storeSigning, "my-brand", map[string]interface{}{
		"account-id":   "my-brand",
		"verification": "verified",
	}, "")
	brandAccKey := assertstest.NewAccountKey(ms.storeSigning, brandAcct, nil, brandPrivKey.PublicKey(), "")

	brandSigning := assertstest.NewSigningDB("my-brand", brandPrivKey)
	model := makeModelAssertion(c, brandSigning, map[string]interface{}{
		"gadget": "gadget",
	})

	// reset as seeded but not registered
	// shortcut: have already device key
	kpMgr, err := asserts.OpenFSKeypairManager(dirs.SnapDeviceDir)
	c.Assert(err, IsNil)
	err = kpMgr.Put(deviceKey)
	c.Assert(err, IsNil)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	err = assertstate.Add(st, brandAcct)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, brandAccKey)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
		KeyID: deviceKey.PublicKey().ID(),
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	signSerial := func(c *C, bhv *devicestatetest.DeviceServiceBehavior, headers map[string]interface{}, body []byte) (asserts.Assertion, error) {
		brandID := headers["brand-id"].(string)
		model := headers["model"].(string)
		c.Check(brandID, Equals, "my-brand")
		c.Check(model, Equals, "my-model")
		headers["authority-id"] = brandID
		return brandSigning.Sign(asserts.SerialType, headers, body, "")
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
	err = ms.o.Settle(settleTimeout)
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
