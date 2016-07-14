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
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/policy"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/systemd"

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

	sn, err := NewInstalledSnap(snapYaml)
	c.Assert(err, IsNil)
	c.Assert(sn, NotNil)
	c.Check(sn.Name(), Equals, "hello-snap")
	c.Check(sn.Version(), Equals, "1.10")
	c.Check(sn.IsActive(), Equals, false)
	c.Check(sn.Info().Summary(), Equals, "hello in summary")
	c.Check(sn.Info().Description(), Equals, "Hello...")
	c.Check(sn.Info().Revision, Equals, snap.R(15))

	mountDir := sn.Info().MountDir()
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
	r.RealName = "foo"
	r.Revision = snap.R(42)
	r.Developer = "bar"
	r.EditedDescription = "this is a description"
	r.Version = "1.0"
	r.AnonDownloadURL = mockServer.URL + "/snap"
	r.DownloadURL = mockServer.URL + "/snap"
	r.IconURL = mockServer.URL + "/icon"

	mStore := store.NewUbuntuStoreSnapRepository(s.storeCfg, "", nil)
	p := &MockProgressMeter{}
	name, err := installRemote(mStore, r, LegacyInhibitHooks, p)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	st, err := os.Stat(snapPackage)
	c.Assert(err, IsNil)
	c.Assert(p.written, Equals, int(st.Size()))

	installed, err := (&Overlord{}).Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)

	c.Check(installed[0].Info().Revision, Equals, snap.R(42))
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
	r.RealName = "foo"
	r.Developer = "bar"
	r.Version = "1.0"
	r.Revision = snap.R(10)
	r.Developer = testDeveloper
	r.AnonDownloadURL = mockServer.URL + "/snap"
	r.DownloadURL = mockServer.URL + "/snap"
	r.IconURL = mockServer.URL + "/icon"

	mStore := store.NewUbuntuStoreSnapRepository(s.storeCfg, "", nil)
	p := &MockProgressMeter{}
	name, err := installRemote(mStore, r, LegacyInhibitHooks, p)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	c.Check(p.notified, HasLen, 0)

	name, err = installRemote(mStore, r, LegacyInhibitHooks, p)
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
	_, err := s.overlord.install(snapPkg, 0, &MockProgressMeter{})
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
