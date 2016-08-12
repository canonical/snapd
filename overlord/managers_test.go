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
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
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

	aa         *testutil.MockCmd
	udev       *testutil.MockCmd
	prevctlCmd func(...string) ([]byte, error)

	o *overlord.Overlord
}

var _ = Suite(&mgrsSuite{})

func (ms *mgrsSuite) SetUpTest(c *C) {
	ms.tempdir = c.MkDir()
	dirs.SetRootDir(ms.tempdir)
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)

	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")

	// create a fake systemd environment
	os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755)

	ms.prevctlCmd = systemd.SystemctlCmd
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}
	ms.aa = testutil.MockCommand(c, "apparmor_parser", "")
	ms.udev = testutil.MockCommand(c, "udevadm", "")

	o, err := overlord.New()
	c.Assert(err, IsNil)
	ms.o = o
}

func (ms *mgrsSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS")
	systemd.SystemctlCmd = ms.prevctlCmd
	ms.udev.Restore()
	ms.aa.Restore()
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

	ts, err := snapstate.InstallPath(st, "foo", snapPath, "", snapstate.DevMode)
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
	mup := systemd.MountUnitPath("/snap/foo/x1", "mount")
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
	snap := ms.installLocalTestSnap(c, snapYamlContent+"version: 1.0")

	ts, err := snapstate.Remove(st, "foo")
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
	c.Assert(osutil.FileExists(snap.DataDir()), Equals, false)
	c.Assert(osutil.FileExists(snap.CommonDataDir()), Equals, false)

	// snap file and its mount
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "foo_x1.snap")), Equals, false)
	mup := systemd.MountUnitPath("/snap/foo/x1", "mount")
	c.Assert(osutil.FileExists(mup), Equals, false)
}

const fooSearchHit = `{
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
	"package_name": "foo",
	"revision": @REVISION@,
	"snap_id": "idididididididididididididididid",
	"summary": "Foo",
	"version": "@VERSION@"
}`

func (ms *mgrsSuite) TestHappyRemoteInstallAndUpgradeSvc(c *C) {
	// test install through store and update, plus some mechanics
	// of update
	// TODO: ok to split if it gets too messy to maintain

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

	snapPath := makeTestSnap(c, strings.Replace(snapYamlContent, "@VERSION@", ver, -1))
	snapR, err := os.Open(snapPath)
	c.Assert(err, IsNil)

	var baseURL string
	fillHit := func() string {
		hit := strings.Replace(fooSearchHit, "@URL@", baseURL+"/snap", -1)
		hit = strings.Replace(hit, "@ICON@", baseURL+"/icon", -1)
		hit = strings.Replace(hit, "@VERSION@", ver, -1)
		hit = strings.Replace(hit, "@REVISION@", revno, -1)
		return hit
	}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/details/foo":
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, fillHit())
		case "/metadata":
			w.WriteHeader(http.StatusOK)
			output := `{
    "_embedded": {
	    "clickindex:package": [@HIT@]
    }
}`
			output = strings.Replace(output, "@HIT@", fillHit(), 1)
			io.WriteString(w, output)
		case "/snap":
			io.Copy(w, snapR)
		default:
			panic("unexpected url path: " + r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	baseURL = mockServer.URL

	detailsURL, err := url.Parse(baseURL + "/details/")
	c.Assert(err, IsNil)
	bulkURL, err := url.Parse(baseURL + "/metadata")
	c.Assert(err, IsNil)
	storeCfg := store.Config{
		DetailsURI: detailsURL,
		BulkURI:    bulkURL,
	}

	mStore := store.New(&storeCfg, "", nil)

	st := ms.o.State()
	st.Lock()
	defer st.Unlock()
	snapstate.ReplaceStore(ms.o.State(), mStore)

	ts, err := snapstate.Install(st, "foo", "stable", 0, 0)
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
	c.Check(info.SnapID, Equals, "idididididididididididididididid")
	c.Check(info.DeveloperID, Equals, "devdevdev")
	c.Check(info.Version, Equals, "1.0")
	c.Check(info.Summary(), Equals, "Foo")
	c.Check(info.Description(), Equals, "this is a description")
	c.Check(info.Developer, Equals, "bar")

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
	snapPath = makeTestSnap(c, strings.Replace(snapYamlContent, "@VERSION@", ver, -1))
	snapR, err = os.Open(snapPath)
	c.Assert(err, IsNil)

	ts, err = snapstate.Update(st, "foo", "stable", 0, 0)
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
	c.Check(info.SnapID, Equals, "idididididididididididididididid")
	c.Check(info.DeveloperID, Equals, "devdevdev")
	c.Check(info.Version, Equals, "2.0")

	// check udpated wrapper
	symlinkTarget, err := os.Readlink(info.Apps["bar"].WrapperPath())
	c.Assert(err, IsNil)
	c.Assert(symlinkTarget, Equals, "/usr/bin/snap")

	// check updated service file
	content, err := ioutil.ReadFile(svcFile)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), "/var/snap/foo/"+revno), Equals, true)
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

	ts, err := snapstate.InstallPath(st, "core", snapPath, "", 0)
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

	ts, err := snapstate.InstallPath(st, "krnl", snapPath, "", 0)
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

	ts, err := snapstate.InstallPath(st, snapName, snapPath, "", snapstate.DevMode)
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
	ts, err := snapstate.Revert(st, "foo")
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
