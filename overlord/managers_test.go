// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"encoding/json"
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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type mgrsSuite struct {
	tempdir string

	aa     *testutil.MockCmd
	udev   *testutil.MockCmd
	umount *testutil.MockCmd

	snapDiscardNs *testutil.MockCmd

	prevctlCmd func(...string) ([]byte, error)

	storeSigning   *assertstest.StoreStack
	restoreTrusted func()

	devAcct *asserts.Account

	o *overlord.Overlord

	serveIDtoName map[string]string
	serveSnapPath map[string]string
	serveRevision map[string]string
}

var (
	_ = Suite(&mgrsSuite{})
	_ = Suite(&authContextSetupSuite{})
)

var (
	rootPrivKey, _  = assertstest.GenerateKey(1024)
	storePrivKey, _ = assertstest.GenerateKey(752)

	brandPrivKey, _ = assertstest.GenerateKey(752)

	develPrivKey, _ = assertstest.GenerateKey(752)

	deviceKey, _ = assertstest.GenerateKey(752)
)

func (ms *mgrsSuite) SetUpTest(c *C) {
	ms.tempdir = c.MkDir()
	dirs.SetRootDir(ms.tempdir)
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)

	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
	snapstate.CanAutoRefresh = nil

	// create a fake systemd environment
	os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755)

	ms.prevctlCmd = systemd.SystemctlCmd
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}
	ms.aa = testutil.MockCommand(c, "apparmor_parser", "")
	ms.udev = testutil.MockCommand(c, "udevadm", "")
	ms.umount = testutil.MockCommand(c, "umount", "")
	ms.snapDiscardNs = testutil.MockCommand(c, "snap-discard-ns", "")
	dirs.DistroLibExecDir = ms.snapDiscardNs.BinDir()

	ms.storeSigning = assertstest.NewStoreStack("can0nical", rootPrivKey, storePrivKey)
	ms.restoreTrusted = sysdb.InjectTrusted(ms.storeSigning.Trusted)

	ms.devAcct = assertstest.NewAccount(ms.storeSigning, "devdevdev", map[string]interface{}{
		"account-id": "devdevdev",
	}, "")
	err = ms.storeSigning.Add(ms.devAcct)
	c.Assert(err, IsNil)

	o, err := overlord.New()
	c.Assert(err, IsNil)
	ms.o = o
	st := ms.o.State()
	st.Lock()
	st.Set("seeded", true)
	st.Unlock()

	ms.serveIDtoName = make(map[string]string)
	ms.serveSnapPath = make(map[string]string)
	ms.serveRevision = make(map[string]string)
}

func (ms *mgrsSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	ms.restoreTrusted()
	os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS")
	systemd.SystemctlCmd = ms.prevctlCmd
	ms.udev.Restore()
	ms.aa.Restore()
	ms.umount.Restore()
	ms.snapDiscardNs.Restore()
}

func makeTestSnap(c *C, snapYamlContent string) string {
	return snaptest.MakeTestSnapWithFiles(c, snapYamlContent, nil)
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

	ts, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "foo"}, snapPath, "", snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle()
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
	mup := systemd.MountUnitPath("/snap/foo/x1")
	content, err := ioutil.ReadFile(mup)
	c.Assert(err, IsNil)
	c.Assert(string(content), Matches, "(?ms).*^Where=/snap/foo/x1")
	c.Assert(string(content), Matches, "(?ms).*^What=/var/lib/snapd/snaps/foo_x1.snap")

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

	ts, err := snapstate.Remove(st, "foo", snap.R(0))
	c.Assert(err, IsNil)
	chg := st.NewChange("remove-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle()
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
	mup := systemd.MountUnitPath("/snap/foo/x1")
	c.Assert(osutil.FileExists(mup), Equals, false)
}

func fakeSnapID(name string) string {
	const suffix = "idididididididididididididididid"
	return name + suffix[len(name)+1:]
}

const (
	searchHit = `{
	"anon_download_url": "@URL@",
	"architecture": [
	    "all"
	],
	"channel": "stable",
	"content": "application",
	"description": "this is a description",
        "developer_id": "devdevdev",
	"download_url": "@URL@",
	"icon_url": "@ICON@",
	"origin": "bar",
	"package_name": "@NAME@",
	"revision": @REVISION@,
	"snap_id": "@SNAPID@",
	"summary": "Foo",
	"version": "@VERSION@"
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
		"snap-id":       fakeSnapID(info.Name()),
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

func (ms *mgrsSuite) mockStore(c *C) *httptest.Server {
	var baseURL string
	fillHit := func(name string) string {
		snapf, err := snap.Open(ms.serveSnapPath[name])
		if err != nil {
			panic(err)
		}
		info, err := snap.ReadInfoFromSnapFile(snapf, nil)
		if err != nil {
			panic(err)
		}
		hit := strings.Replace(searchHit, "@URL@", baseURL+"/snap/"+name, -1)
		hit = strings.Replace(hit, "@NAME@", name, -1)
		hit = strings.Replace(hit, "@SNAPID@", fakeSnapID(name), -1)
		hit = strings.Replace(hit, "@ICON@", baseURL+"/icon", -1)
		hit = strings.Replace(hit, "@VERSION@", info.Version, -1)
		hit = strings.Replace(hit, "@REVISION@", ms.serveRevision[name], -1)
		return hit
	}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		comps := strings.Split(r.URL.Path, "/")
		if len(comps) == 0 {
			panic("unexpected url path: " + r.URL.Path)

		}
		switch comps[1] {
		case "assertions":
			ref := &asserts.Ref{
				Type:       asserts.Type(comps[2]),
				PrimaryKey: comps[3:],
			}
			a, err := ref.Resolve(ms.storeSigning.Find)
			if err == asserts.ErrNotFound {
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
		case "details":
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, fillHit(comps[2]))
		case "metadata":
			dec := json.NewDecoder(r.Body)
			var input struct {
				Snaps []struct {
					SnapID   string `json:"snap_id"`
					Revision int    `json:"revision"`
				} `json:"snaps"`
			}
			err := dec.Decode(&input)
			if err != nil {
				panic(err)
			}
			var hits []json.RawMessage
			for _, s := range input.Snaps {
				name := ms.serveIDtoName[s.SnapID]
				if snap.R(s.Revision) == snap.R(ms.serveRevision[name]) {
					continue
				}
				hits = append(hits, json.RawMessage(fillHit(name)))
			}
			w.WriteHeader(http.StatusOK)
			output, err := json.Marshal(map[string]interface{}{
				"_embedded": map[string]interface{}{
					"clickindex:package": hits,
				},
			})
			if err != nil {
				panic(err)
			}
			w.Write(output)
		case "snap":
			snapR, err := os.Open(ms.serveSnapPath[comps[2]])
			if err != nil {
				panic(err)
			}
			io.Copy(w, snapR)
		default:
			panic("unexpected url path: " + r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)

	baseURL = mockServer.URL

	detailsURL, err := url.Parse(baseURL + "/details/")
	c.Assert(err, IsNil)
	bulkURL, err := url.Parse(baseURL + "/metadata")
	c.Assert(err, IsNil)
	assertionsURL, err := url.Parse(baseURL + "/assertions/")
	c.Assert(err, IsNil)
	storeCfg := store.Config{
		DetailsURI:    detailsURL,
		BulkURI:       bulkURL,
		AssertionsURI: assertionsURL,
	}

	mStore := store.New(&storeCfg, nil)
	st := ms.o.State()
	st.Lock()
	snapstate.ReplaceStore(ms.o.State(), mStore)
	st.Unlock()

	return mockServer
}

func (ms *mgrsSuite) serveSnap(snapPath, revno string) {
	snapf, err := snap.Open(snapPath)
	if err != nil {
		panic(err)
	}
	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	if err != nil {
		panic(err)
	}
	name := info.Name()
	ms.serveIDtoName[fakeSnapID(name)] = name
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
	err = ms.o.Settle()
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
	err = ms.o.Settle()
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
	content, err := ioutil.ReadFile(svcFile)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), "/var/snap/foo/"+revno), Equals, true)
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
	err := assertstate.Add(st, ms.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(st, ms.devAcct)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, snapDecl)
	c.Assert(err, IsNil)

	ts, err := snapstate.InstallPath(st, si, snapPath, "", snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle()
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
	mup := systemd.MountUnitPath("/snap/foo/55")
	content, err := ioutil.ReadFile(mup)
	c.Assert(err, IsNil)
	c.Assert(string(content), Matches, "(?ms).*^Where=/snap/foo/55")
	c.Assert(string(content), Matches, "(?ms).*^What=/var/lib/snapd/snaps/foo_55.snap")
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
	err := assertstate.Add(st, ms.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(st, ms.devAcct)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, snapDecl)
	c.Assert(err, IsNil)

	ts, err := snapstate.InstallPath(st, si, snapPath, "", snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle()
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
	err = ms.o.Settle()
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

	updated, tss, err := snapstate.UpdateMany(st, []string{"foo"}, 0)
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
	updated, tss, err = snapstate.UpdateMany(st, []string{"foo"}, 0)
	c.Assert(err, IsNil)
	c.Assert(updated, DeepEquals, []string{"foo"})
	c.Assert(tss, HasLen, 1)
	chg = st.NewChange("upgrade-snaps", "...")
	chg.AddAll(tss[0])

	st.Unlock()
	err = ms.o.Settle()
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	info, err = snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)

	c.Check(info.Revision, Equals, snap.R(50))
}

// core & kernel

func (ms *mgrsSuite) TestInstallCoreSnapUpdatesBootloader(c *C) {
	bootloader := boottest.NewMockBootloader("mock", c.MkDir())
	partition.ForceBootloader(bootloader)
	defer partition.ForceBootloader(nil)

	restore := release.MockOnClassic(false)
	defer restore()

	const packageOS = `
name: core
version: 16.04-1
type: os
`
	snapPath := makeTestSnap(c, packageOS)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()

	ts, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "core"}, snapPath, "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle()
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	c.Assert(bootloader.BootVars, DeepEquals, map[string]string{
		"snap_try_core": "core_x1.snap",
		"snap_mode":     "try",
	})
}

func (ms *mgrsSuite) TestInstallKernelSnapUpdatesBootloader(c *C) {
	bootloader := boottest.NewMockBootloader("mock", c.MkDir())
	partition.ForceBootloader(bootloader)
	defer partition.ForceBootloader(nil)

	restore := release.MockOnClassic(false)
	defer restore()

	brandAcct := assertstest.NewAccount(ms.storeSigning, "my-brand", map[string]interface{}{
		"account-id":   "my-brand",
		"verification": "certified",
	}, "")
	brandAccKey := assertstest.NewAccountKey(ms.storeSigning, brandAcct, nil, brandPrivKey.PublicKey(), "")

	brandSigning := assertstest.NewSigningDB("my-brand", brandPrivKey)
	model, err := brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"architecture": "amd64",
		"store":        "my-brand-store-id",
		"gadget":       "gadget",
		"kernel":       "krnl",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	const packageKernel = `
name: krnl
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
	err = assertstate.Add(st, ms.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(st, brandAcct)
	c.Assert(err, IsNil)
	err = assertstate.Add(st, brandAccKey)
	c.Assert(err, IsNil)
	auth.SetDevice(st, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
	})
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)

	ts, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: "krnl"}, snapPath, "", snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle()
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	c.Assert(bootloader.BootVars, DeepEquals, map[string]string{
		"snap_try_kernel": "krnl_x1.snap",
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
	snapName := info.Name()
	var snapst snapstate.SnapState
	snapstate.Get(st, snapName, &snapst)

	ts, err := snapstate.InstallPath(st, &snap.SideInfo{RealName: snapName}, snapPath, "", snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	chg := st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle()
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
	err = ms.o.Settle()
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
	err = ms.o.Settle()
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
    aliases: [foo_]
  bar:
    command: bin/bar
    aliases: [bar,bar1]
`
	ms.installLocalTestSnap(c, fooYaml)

	ts, err := snapstate.Alias(st, "foo", []string{"foo_", "bar", "bar1"})
	c.Assert(err, IsNil)
	chg := st.NewChange("alias", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle()
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("alias change failed with: %v", chg.Err()))

	foo_Alias := filepath.Join(dirs.SnapBinariesDir, "foo_")
	dest, err := os.Readlink(foo_Alias)
	c.Assert(err, IsNil)

	c.Check(dest, Equals, "foo")

	barAlias := filepath.Join(dirs.SnapBinariesDir, "bar")
	dest, err = os.Readlink(barAlias)
	c.Assert(err, IsNil)

	c.Check(dest, Equals, "foo.bar")

	bar1Alias := filepath.Join(dirs.SnapBinariesDir, "bar1")
	dest, err = os.Readlink(bar1Alias)
	c.Assert(err, IsNil)

	c.Check(dest, Equals, "foo.bar")

	var allAliases map[string]map[string]string
	err = st.Get("aliases", &allAliases)
	c.Assert(err, IsNil)
	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"foo": {
			"foo_": "enabled",
			"bar":  "enabled",
			"bar1": "enabled",
		},
	})

	ms.removeSnap(c, "foo")

	c.Check(osutil.IsSymlink(foo_Alias), Equals, false)
	c.Check(osutil.IsSymlink(barAlias), Equals, false)
	c.Check(osutil.IsSymlink(bar1Alias), Equals, false)

	allAliases = nil
	err = st.Get("aliases", &allAliases)
	c.Assert(err, IsNil)
	c.Check(allAliases, HasLen, 0)
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
    aliases: [foo_]
`
	ms.installLocalTestSnap(c, fooYaml)

	ts, err := snapstate.Alias(st, "foo", []string{"foo_"})
	c.Assert(err, IsNil)
	chg := st.NewChange("alias", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle()
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("alias change failed with: %v", chg.Err()))

	foo_Alias := filepath.Join(dirs.SnapBinariesDir, "foo_")
	dest, err := os.Readlink(foo_Alias)
	c.Assert(err, IsNil)

	c.Check(dest, Equals, "foo")

	var allAliases map[string]map[string]string
	err = st.Get("aliases", &allAliases)
	c.Assert(err, IsNil)
	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"foo": {
			"foo_": "enabled",
		},
	})

	ts, err = snapstate.Unalias(st, "foo", []string{"foo_"})
	c.Assert(err, IsNil)
	chg = st.NewChange("unalias", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle()
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("unalias change failed with: %v", chg.Err()))

	c.Check(osutil.IsSymlink(foo_Alias), Equals, false)

	allAliases = nil
	err = st.Get("aliases", &allAliases)
	c.Assert(err, IsNil)
	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"foo": {
			"foo_": "disabled",
		},
	})
}

func (ms *mgrsSuite) TestHappyRemoteInstallAutoAliases(c *C) {
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "foo",
		"auto-aliases": []interface{}{"app1", "app2"},
	})

	snapYamlContent := `name: foo
version: @VERSION@
apps:
 app1:
  command: bin/app1
  aliases: [app1]
 app2:
  command: bin/app2
  aliases: [app2]
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
	err = ms.o.Settle()
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	var allAliases map[string]map[string]string
	err = st.Get("aliases", &allAliases)
	c.Assert(err, IsNil)
	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"foo": {
			"app1": "auto",
			"app2": "auto",
		},
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
		"snap-name":    "foo",
		"auto-aliases": []interface{}{"app1"},
	})

	fooYaml := `name: foo
version: @VERSION@
apps:
 app1:
  command: bin/app1
  aliases: [app1]
 app2:
  command: bin/app2
  aliases: [app2]
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
	err = ms.o.Settle()
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	info, err := snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)
	c.Check(info.Revision, Equals, snap.R(10))
	c.Check(info.Version, Equals, "1.0")

	var allAliases map[string]map[string]string
	err = st.Get("aliases", &allAliases)
	c.Check(err, IsNil)
	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"foo": {
			"app1": "auto",
		},
	})
	app1Alias := filepath.Join(dirs.SnapBinariesDir, "app1")
	dest, err := os.Readlink(app1Alias)
	c.Assert(err, IsNil)
	c.Check(dest, Equals, "foo.app1")

	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "foo",
		"auto-aliases": []interface{}{"app2"},
		"revision":     "1",
	})

	// new foo version/revision
	fooPath, _ = ms.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.5", -1), "15")
	ms.serveSnap(fooPath, "15")

	// refresh all
	updated, tss, err := snapstate.UpdateMany(st, nil, 0)
	c.Assert(err, IsNil)
	c.Assert(updated, DeepEquals, []string{"foo"})
	c.Assert(tss, HasLen, 1)
	chg = st.NewChange("upgrade-snaps", "...")
	chg.AddAll(tss[0])

	st.Unlock()
	err = ms.o.Settle()
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	info, err = snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)
	c.Check(info.Revision, Equals, snap.R(15))
	c.Check(info.Version, Equals, "1.5")

	allAliases = nil
	err = st.Get("aliases", &allAliases)
	c.Check(err, IsNil)
	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"foo": {
			"app2": "auto",
		},
	})

	c.Check(osutil.IsSymlink(app1Alias), Equals, false)

	app2Alias := filepath.Join(dirs.SnapBinariesDir, "app2")
	dest, err = os.Readlink(app2Alias)
	c.Assert(err, IsNil)
	c.Check(dest, Equals, "foo.app2")
}

func (ms *mgrsSuite) TestHappyOrthogonalRefreshAutoAliases(c *C) {
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "foo",
		"auto-aliases": []interface{}{"app1"},
	}, map[string]interface{}{
		"snap-name": "bar",
	})

	fooYaml := `name: foo
version: @VERSION@
apps:
 app1:
  command: bin/app1
  aliases: [app1]
 app2:
  command: bin/app2
  aliases: [app2]
`

	barYaml := `name: bar
version: @VERSION@
apps:
 app1:
  command: bin/app1
  aliases: [app1]
 app3:
  command: bin/app3
  aliases: [app3]
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
	err = ms.o.Settle()
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("install-snap change failed with: %v", chg.Err()))

	ts, err = snapstate.Install(st, "bar", "stable", snap.R(0), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg = st.NewChange("install-snap", "...")
	chg.AddAll(ts)

	st.Unlock()
	err = ms.o.Settle()
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

	var allAliases map[string]map[string]string
	err = st.Get("aliases", &allAliases)
	c.Check(err, IsNil)
	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"foo": {
			"app1": "auto",
		},
	})

	// foo gets a new version/revision and a change of auto-aliases
	// bar gets only the latter
	// app1 is transferred from foo to bar
	// UpdateMany after a snap-declaration refresh handles all of this
	ms.prereqSnapAssertions(c, map[string]interface{}{
		"snap-name":    "foo",
		"auto-aliases": []interface{}{"app2"},
		"revision":     "1",
	}, map[string]interface{}{
		"snap-name":    "bar",
		"auto-aliases": []interface{}{"app1", "app3"},
		"revision":     "1",
	})

	// new foo version/revision
	fooPath, _ = ms.makeStoreTestSnap(c, strings.Replace(fooYaml, "@VERSION@", "1.5", -1), "15")
	ms.serveSnap(fooPath, "15")

	// refresh all
	err = assertstate.RefreshSnapDeclarations(st, 0)
	c.Assert(err, IsNil)

	updated, tss, err := snapstate.UpdateMany(st, nil, 0)
	c.Assert(err, IsNil)
	sort.Strings(updated)
	c.Assert(updated, DeepEquals, []string{"bar", "foo"})
	c.Assert(tss, HasLen, 3)
	chg = st.NewChange("upgrade-snaps", "...")
	chg.AddAll(tss[0])
	chg.AddAll(tss[1])
	chg.AddAll(tss[2])

	st.Unlock()
	err = ms.o.Settle()
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("upgrade-snap change failed with: %v", chg.Err()))

	info, err = snapstate.CurrentInfo(st, "foo")
	c.Assert(err, IsNil)
	c.Check(info.Revision, Equals, snap.R(15))
	c.Check(info.Version, Equals, "1.5")

	allAliases = nil
	err = st.Get("aliases", &allAliases)
	c.Check(err, IsNil)
	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"foo": {
			"app2": "auto",
		},
		"bar": {
			"app1": "auto",
			"app3": "auto",
		},
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

type authContextSetupSuite struct {
	o  *overlord.Overlord
	ac auth.AuthContext

	storeSigning   *assertstest.StoreStack
	restoreTrusted func()

	brandSigning *assertstest.SigningDB
	deviceKey    asserts.PrivateKey

	model  *asserts.Model
	serial *asserts.Serial
}

func (s *authContextSetupSuite) SetUpTest(c *C) {
	tempdir := c.MkDir()
	dirs.SetRootDir(tempdir)
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)

	captureAuthContext := func(_ *store.Config, ac auth.AuthContext) *store.Store {
		s.ac = ac
		return nil
	}
	r := overlord.MockStoreNew(captureAuthContext)
	defer r()

	s.storeSigning = assertstest.NewStoreStack("can0nical", rootPrivKey, storePrivKey)
	s.restoreTrusted = sysdb.InjectTrusted(s.storeSigning.Trusted)

	s.brandSigning = assertstest.NewSigningDB("my-brand", brandPrivKey)

	brandAcct := assertstest.NewAccount(s.storeSigning, "my-brand", map[string]interface{}{
		"account-id":   "my-brand",
		"verification": "certified",
	}, "")
	s.storeSigning.Add(brandAcct)

	brandAccKey := assertstest.NewAccountKey(s.storeSigning, brandAcct, nil, brandPrivKey.PublicKey(), "")
	s.storeSigning.Add(brandAccKey)

	model, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"architecture": "amd64",
		"store":        "my-brand-store-id",
		"gadget":       "pc",
		"kernel":       "pc-kernel",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	s.model = model.(*asserts.Model)

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

	o, err := overlord.New()
	c.Assert(err, IsNil)
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

func (s *authContextSetupSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.restoreTrusted()
}

func (s *authContextSetupSuite) TestStoreID(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	st.Unlock()
	storeID, err := s.ac.StoreID("fallback")
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "fallback")

	// setup model in system state
	auth.SetDevice(st, &auth.DeviceState{
		Brand:  s.serial.BrandID(),
		Model:  s.serial.Model(),
		Serial: s.serial.Serial(),
	})
	err = assertstate.Add(st, s.model)
	c.Assert(err, IsNil)

	st.Unlock()
	storeID, err = s.ac.StoreID("fallback")
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "my-brand-store-id")
}

func (s *authContextSetupSuite) TestDeviceSessionRequest(c *C) {
	st := s.o.State()
	st.Lock()
	defer st.Unlock()

	st.Unlock()
	_, _, err := s.ac.DeviceSessionRequest("NONCE")
	st.Lock()
	c.Check(err, Equals, auth.ErrNoSerial)

	// setup serial and key in system state
	err = assertstate.Add(st, s.serial)
	c.Assert(err, IsNil)
	kpMgr, err := asserts.OpenFSKeypairManager(dirs.SnapDeviceDir)
	c.Assert(err, IsNil)
	err = kpMgr.Put(deviceKey)
	c.Assert(err, IsNil)
	auth.SetDevice(st, &auth.DeviceState{
		Brand:  s.serial.BrandID(),
		Model:  s.serial.Model(),
		Serial: s.serial.Serial(),
		KeyID:  deviceKey.PublicKey().ID(),
	})

	st.Unlock()
	req, encSerial, err := s.ac.DeviceSessionRequest("NONCE")
	st.Lock()
	c.Assert(err, IsNil)
	c.Check(bytes.HasPrefix(req, []byte("type: device-session-request\n")), Equals, true)
	c.Check(encSerial, DeepEquals, asserts.Encode(s.serial))
}
