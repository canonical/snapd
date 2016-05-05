// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/policy"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/snapenv"
	"github.com/ubuntu-core/snappy/store"
	"github.com/ubuntu-core/snappy/systemd"

	. "gopkg.in/check.v1"
)

type SnapTestSuite struct {
	tempdir  string
	secbase  string
	storeCfg *store.SnapUbuntuStoreConfig
	overlord Overlord
}

var _ = Suite(&SnapTestSuite{})

func (s *SnapTestSuite) SetUpTest(c *C) {
	s.secbase = policy.SecBase
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)

	policy.SecBase = filepath.Join(s.tempdir, "security")
	os.MkdirAll(dirs.SnapServicesDir, 0755)
	os.MkdirAll(dirs.SnapSeccompDir, 0755)
	os.MkdirAll(dirs.SnapSnapsDir, 0755)

	// create a fake systemd environment
	os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755)

	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}

	// do not attempt to hit the real store servers in the tests
	nowhereURI, _ := url.Parse("")
	s.storeCfg = &store.SnapUbuntuStoreConfig{
		SearchURI: nowhereURI,
		BulkURI:   nowhereURI,
	}
	storeConfig = s.storeCfg
}

func (s *SnapTestSuite) TearDownTest(c *C) {
	// ensure all functions are back to their original state
	storeConfig = nil
	policy.SecBase = s.secbase
	ActiveSnapIterByType = activeSnapIterByTypeImpl
	stripGlobalRootDir = stripGlobalRootDirImpl
}

func makeSnapActive(snapYamlPath string) (err error) {
	snapdir := filepath.Dir(filepath.Dir(snapYamlPath))
	parent := filepath.Dir(snapdir)
	err = os.Symlink(snapdir, filepath.Join(parent, "current"))

	return err
}

func (s *SnapTestSuite) TestLocalSnapInvalidPath(c *C) {
	_, err := NewInstalledSnap("invalid-path")
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestLocalSnapSimple(c *C) {
	snapYaml, err := makeInstalledMockSnap("", 15)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnap(snapYaml)
	c.Assert(err, IsNil)
	c.Assert(snap, NotNil)
	c.Check(snap.Name(), Equals, "hello-snap")
	c.Check(snap.Version(), Equals, "1.10")
	c.Check(snap.IsActive(), Equals, false)
	c.Check(snap.Info().Summary(), Equals, "hello in summary")
	c.Check(snap.Info().Description(), Equals, "Hello...")
	c.Check(snap.Info().Revision, Equals, 15)

	mountDir := snap.Info().MountDir()
	_, err = os.Stat(mountDir)
	c.Assert(err, IsNil)

	c.Assert(mountDir, Equals, filepath.Join(dirs.SnapSnapsDir, helloSnapComposedName, "15"))
}

func (s *SnapTestSuite) TestLocalSnapActive(c *C) {
	snapYaml, err := makeInstalledMockSnap("", 11)
	c.Assert(err, IsNil)
	makeSnapActive(snapYaml)

	snap, err := NewInstalledSnap(snapYaml)
	c.Assert(err, IsNil)
	c.Assert(snap.IsActive(), Equals, true)
}

func (s *SnapTestSuite) TestLocalSnapRepositoryInvalidIsStillOk(c *C) {
	dirs.SnapSnapsDir = "invalid-path"

	installed, err := (&Overlord{}).Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 0)
}

func (s *SnapTestSuite) TestLocalSnapRepositorySimple(c *C) {
	yamlPath, err := makeInstalledMockSnap("", 11)
	c.Assert(err, IsNil)
	err = makeSnapActive(yamlPath)
	c.Assert(err, IsNil)

	installed, err := (&Overlord{}).Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)
	c.Assert(installed[0].Name(), Equals, "hello-snap")
	c.Assert(installed[0].Version(), Equals, "1.10")
}

const (
	funkyAppName = "8nzc1x4iim2xj1g2ul64"
)

/* acquired via:
curl -s --data-binary '{"name":["8nzc1x4iim2xj1g2ul64.chipaca"]}'  -H 'content-type: application/json' https://search.apps.ubuntu.com/api/v1/click-metadata
*/
const MockUpdatesJSON = `[
    {
        "status": "Published",
        "name": "8nzc1x4iim2xj1g2ul64.chipaca",
        "package_name": "8nzc1x4iim2xj1g2ul64",
        "origin": "chipaca",
        "changelog": "",
        "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/04/hello.svg_Dlrd3L4.png",
        "title": "Returns for store credit only.",
        "binary_filesize": 65375,
        "anon_download_url": "https://public.apps.ubuntu.com/anon/download/chipaca/8nzc1x4iim2xj1g2ul64.chipaca/8nzc1x4iim2xj1g2ul64.chipaca_42_all.snap",
        "allow_unauthenticated": true,
        "revision": 3,
        "version": "42",
        "download_url": "https://public.apps.ubuntu.com/download/chipaca/8nzc1x4iim2xj1g2ul64.chipaca/8nzc1x4iim2xj1g2ul64.chipaca_42_all.snap",
        "download_sha512": "5364253e4a988f4f5c04380086d542f410455b97d48cc6c69ca2a5877d8aef2a6b2b2f83ec4f688cae61ebc8a6bf2cdbd4dbd8f743f0522fc76540429b79df42"
    }
]`

func mockActiveSnapIterByType(mockSnaps []string) {
	ActiveSnapIterByType = func(f func(*snap.Info) string, snapTs ...snap.Type) (res []string, err error) {
		return mockSnaps, nil
	}
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryUpdates(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		c.Assert(string(jsonReq), Equals, `{"name":["`+funkyAppName+`"]}`)
		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	var err error
	s.storeCfg.BulkURI, err = url.Parse(mockServer.URL + "/updates/")
	c.Assert(err, IsNil)
	repo := store.NewUbuntuStoreSnapRepository(s.storeCfg, "")
	c.Assert(repo, NotNil)

	// override the real ActiveSnapIterByType to return our
	// mock data
	mockActiveSnapIterByType([]string{funkyAppName})

	// the actual test
	results, err := snapUpdates(repo)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name(), Equals, funkyAppName)
	c.Assert(results[0].Revision, Equals, 3)
	c.Assert(results[0].Version, Equals, "42")
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryUpdatesNoSnaps(c *C) {

	var err error
	s.storeCfg.SearchURI, err = url.Parse("https://some-uri")
	c.Assert(err, IsNil)
	repo := store.NewUbuntuStoreSnapRepository(s.storeCfg, "")
	c.Assert(repo, NotNil)

	mockActiveSnapIterByType([]string{})

	// the actual test
	results, err := snapUpdates(repo)
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 0)
}

func (s *SnapTestSuite) TestMakeConfigEnv(c *C) {
	yamlFile, err := makeInstalledMockSnap("", 11)
	c.Assert(err, IsNil)
	snap, err := NewInstalledSnap(yamlFile)
	c.Assert(err, IsNil)
	c.Assert(snap, NotNil)

	os.Setenv("SNAP_NAME", "override-me")
	defer os.Setenv("SNAP_NAME", "")

	env := makeSnapHookEnv(snap)

	// now ensure that the environment we get back is what we want
	envMap := snapenv.MakeMapFromEnvList(env)
	// regular env is unaltered
	c.Assert(envMap["PATH"], Equals, os.Getenv("PATH"))
	// SNAP_* is overriden
	c.Assert(envMap["SNAP_NAME"], Equals, "hello-snap")
	c.Assert(envMap["SNAP_VERSION"], Equals, "1.10")
	c.Check(envMap["LC_ALL"], Equals, "C.UTF-8")
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryInstallRemoteSnap(c *C) {
	snapPackage := makeTestSnapPackage(c, "")
	snapR, err := os.Open(snapPackage)
	c.Assert(err, IsNil)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/snap":
			io.Copy(w, snapR)
		default:
			panic("unexpected url path: " + r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	r := &snap.Info{}
	r.OfficialName = "foo"
	r.Revision = 42
	r.Developer = "bar"
	r.EditedDescription = "this is a description"
	r.Version = "1.0"
	r.AnonDownloadURL = mockServer.URL + "/snap"
	r.DownloadURL = mockServer.URL + "/snap"
	r.IconURL = mockServer.URL + "/icon"

	mStore := store.NewUbuntuStoreSnapRepository(s.storeCfg, "")
	p := &MockProgressMeter{}
	name, err := installRemote(mStore, r, 0, p)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	st, err := os.Stat(snapPackage)
	c.Assert(err, IsNil)
	c.Assert(p.written, Equals, int(st.Size()))

	installed, err := (&Overlord{}).Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)

	c.Check(installed[0].Info().Revision, Equals, 42)
	c.Check(installed[0].Developer(), Equals, "bar")
	c.Check(installed[0].Info().Description(), Equals, "this is a description")

	_, err = os.Stat(filepath.Join(dirs.SnapMetaDir, "foo_42.manifest"))
	c.Check(err, IsNil)
}

func (s *SnapTestSuite) TestRemoteSnapUpgradeService(c *C) {
	snapPackage := makeTestSnapPackage(c, `name: foo
version: 1.0
apps:
 svc:
  command: svc
  daemon: forking
`)
	snapR, err := os.Open(snapPackage)
	c.Assert(err, IsNil)

	iconContent := "icon"
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/snap":
			io.Copy(w, snapR)
			snapR.Seek(0, 0)
		case "/icon":
			fmt.Fprintf(w, iconContent)
		default:
			panic("unexpected url path: " + r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	r := &snap.Info{}
	r.OfficialName = "foo"
	r.Developer = "bar"
	r.Version = "1.0"
	r.Developer = testDeveloper
	r.AnonDownloadURL = mockServer.URL + "/snap"
	r.DownloadURL = mockServer.URL + "/snap"
	r.IconURL = mockServer.URL + "/icon"

	mStore := store.NewUbuntuStoreSnapRepository(s.storeCfg, "")
	p := &MockProgressMeter{}
	name, err := installRemote(mStore, r, 0, p)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	c.Check(p.notified, HasLen, 0)

	name, err = installRemote(mStore, r, 0, p)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
}

func (s *SnapTestSuite) TestErrorOnUnsupportedArchitecture(c *C) {
	const packageHello = `name: hello-snap
version: 1.10
architectures:
    - yadayada
    - blahblah
`

	snapPkg := makeTestSnapPackage(c, packageHello)
	_, err := s.overlord.Install(snapPkg, 0, &MockProgressMeter{})
	errorMsg := fmt.Sprintf("package's supported architectures (yadayada, blahblah) is incompatible with this system (%s)", arch.UbuntuArchitecture())
	c.Assert(err.Error(), Equals, errorMsg)
}

func (s *SnapTestSuite) TestServicesWithPorts(c *C) {
	const packageHello = `name: hello-snap
version: 1.10
apps:
 hello:
  command: bin/hello
 svc1:
   command: svc1
   type: forking
   description: "Service #1"
   ports:
      external:
        ui:
          port: 8080/tcp
        nothing:
          port: 8081/tcp
          negotiable: yes
 svc2:
   command: svc2
   type: forking
   description: "Service #2"
`

	yamlFile, err := makeInstalledMockSnap(packageHello, 11)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnap(yamlFile)
	c.Assert(err, IsNil)
	c.Assert(snap, NotNil)

	c.Assert(snap.Name(), Equals, "hello-snap")
	c.Assert(snap.Developer(), Equals, testDeveloper)
	c.Assert(snap.Version(), Equals, "1.10")
	c.Assert(snap.IsActive(), Equals, false)

	apps := snap.Info().Apps
	c.Assert(apps, HasLen, 3)

	c.Assert(apps["svc1"].Name, Equals, "svc1")

	c.Assert(apps["svc2"].Name, Equals, "svc2")
}

var securityBinarySnapYaml = []byte(`name: test-snap
version: 1.2.8
apps:
 testme:
   command: bin/testme
   description: "testme client"
   plugs: [testme]
 testme-override:
   command: bin/testme-override
   plugs: [testme-override]
 testme-policy:
   command: bin/testme-policy
   plugs: [testme-policy]

plugs:
 testme:
   interface: old-security
   caps:
     - "foo_group"
   security-template: "foo_template"
 testme-override:
   interface: old-security
   security-override:
     read-paths:
         - "/foo"
     syscalls:
         - "bar"
 testme-policy:
   interface: old-security
   security-policy:
     apparmor: meta/testme-policy.profile

`)

var hardwareYaml = []byte(`name: gadget-foo
version: 1.0
gadget:
 hardware:
  assign:
   - part-id: device-hive-iot-hal
     rules:
     - kernel: ttyUSB0
     - subsystem: tty
       with-subsystems: usb-serial
       with-driver: pl2303
       with-attrs:
       - idVendor=0xf00f00
       - idProduct=0xb00
       with-props:
       - BAUD=9600
       - META1=foo*
       - META2=foo?
       - META3=foo[a-z]
       - META4=a|b
`)
