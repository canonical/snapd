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
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/policy"
	"github.com/ubuntu-core/snappy/release"
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

	release.Override(release.Release{Flavor: "core", Series: "15.04"})

	// create a fake systemd environment
	os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755)

	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}

	// fake udevadm
	runUdevAdm = func(args ...string) error {
		return nil
	}

	// do not attempt to hit the real store servers in the tests
	nowhereURI, _ := url.Parse("")
	s.storeCfg = &store.SnapUbuntuStoreConfig{
		SearchURI:  nowhereURI,
		DetailsURI: nowhereURI,
		BulkURI:    nowhereURI,
	}
	storeConfig = s.storeCfg

	aaExec = filepath.Join(s.tempdir, "aa-exec")
	err := ioutil.WriteFile(aaExec, []byte(mockAaExecScript), 0755)
	c.Assert(err, IsNil)

	runAppArmorParser = mockRunAppArmorParser

	makeMockSecurityEnv(c)
}

func (s *SnapTestSuite) TearDownTest(c *C) {
	// ensure all functions are back to their original state
	storeConfig = nil
	policy.SecBase = s.secbase
	regenerateAppArmorRules = regenerateAppArmorRulesImpl
	ActiveSnapIterByType = activeSnapIterByTypeImpl
	duCmd = "du"
	stripGlobalRootDir = stripGlobalRootDirImpl
	runUdevAdm = runUdevAdmImpl
}

func (s *SnapTestSuite) makeInstalledMockSnap(yamls ...string) (yamlFile string, err error) {
	yaml := ""
	if len(yamls) > 0 {
		yaml = yamls[0]
	}

	return makeInstalledMockSnap(s.tempdir, yaml)
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
	snapYaml, err := s.makeInstalledMockSnap()
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnap(snapYaml)
	c.Assert(err, IsNil)
	c.Assert(snap, NotNil)
	c.Check(snap.Name(), Equals, "hello-snap")
	c.Check(snap.Version(), Equals, "1.10")
	c.Check(snap.IsActive(), Equals, false)
	c.Check(snap.Description(), Equals, "Hello")
	c.Check(snap.IsInstalled(), Equals, true)

	apps := snap.Apps()
	c.Assert(apps, HasLen, 2)
	c.Assert(apps["svc1"].Name, Equals, "svc1")

	// ensure we get valid Date()
	st, err := os.Stat(snap.basedir)
	c.Assert(err, IsNil)
	c.Assert(snap.Date(), Equals, st.ModTime())

	c.Assert(snap.basedir, Equals, filepath.Join(s.tempdir, "snaps", helloSnapComposedName, "1.10"))
	c.Assert(snap.InstalledSize(), Not(Equals), -1)
}

func (s *SnapTestSuite) TestLocalSnapActive(c *C) {
	snapYaml, err := s.makeInstalledMockSnap()
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
	yamlPath, err := s.makeInstalledMockSnap()
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
	funkyAppName      = "8nzc1x4iim2xj1g2ul64"
	funkyAppDeveloper = "chipaca"
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
        "version": "42",
        "download_url": "https://public.apps.ubuntu.com/download/chipaca/8nzc1x4iim2xj1g2ul64.chipaca/8nzc1x4iim2xj1g2ul64.chipaca_42_all.snap",
        "download_sha512": "5364253e4a988f4f5c04380086d542f410455b97d48cc6c69ca2a5877d8aef2a6b2b2f83ec4f688cae61ebc8a6bf2cdbd4dbd8f743f0522fc76540429b79df42"
    }
]`

type MockUbuntuStoreServer struct {
	quit chan int

	searchURI string
}

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
	c.Assert(results[0].Version(), Equals, "42")
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryUpdatesNoSnaps(c *C) {

	var err error
	s.storeCfg.DetailsURI, err = url.Parse("https://some-uri")
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
	yamlFile, err := makeInstalledMockSnap(s.tempdir, "")
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

	r := &store.RemoteSnap{}
	r.Pkg.AnonDownloadURL = mockServer.URL + "/snap"
	r.Pkg.IconURL = mockServer.URL + "/icon"
	r.Pkg.Name = "foo"
	r.Pkg.Developer = "bar"
	r.Pkg.Description = "this is a description"
	r.Pkg.Version = "1.0"

	mStore := store.NewUbuntuStoreSnapRepository(s.storeCfg, "")
	p := &MockProgressMeter{}
	name, err := installRemote(mStore, r, 0, p)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	st, err := os.Stat(snapPackage)
	c.Assert(err, IsNil)
	c.Assert(p.written, Equals, int(st.Size()))

	installed, err := ListInstalled()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)

	c.Check(installed[0].Developer(), Equals, "bar")
	c.Check(installed[0].Description(), Equals, "this is a description")

	_, err = os.Stat(filepath.Join(dirs.SnapMetaDir, "foo_1.0.manifest"))
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

	r := &store.RemoteSnap{}
	r.Pkg.AnonDownloadURL = mockServer.URL + "/snap"
	r.Pkg.Developer = testDeveloper
	r.Pkg.IconURL = mockServer.URL + "/icon"
	r.Pkg.Name = "foo"
	r.Pkg.Developer = "bar"
	r.Pkg.Version = "1.0"

	mStore := store.NewUbuntuStoreSnapRepository(s.storeCfg, "")
	p := &MockProgressMeter{}
	name, err := installRemote(mStore, r, 0, p)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	c.Check(p.notified, HasLen, 0)

	_, err = installRemote(mStore, r, 0, p)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	c.Check(p.notified, HasLen, 1)
	c.Check(p.notified[0], Matches, "Waiting for .* stop.")
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

	yamlFile, err := makeInstalledMockSnap(s.tempdir, packageHello)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnap(yamlFile)
	c.Assert(err, IsNil)
	c.Assert(snap, NotNil)

	c.Assert(snap.Name(), Equals, "hello-snap")
	c.Assert(snap.Developer(), Equals, testDeveloper)
	c.Assert(snap.Version(), Equals, "1.10")
	c.Assert(snap.IsActive(), Equals, false)

	apps := snap.Apps()
	c.Assert(apps, HasLen, 3)

	c.Assert(apps["svc1"].Name, Equals, "svc1")
	c.Assert(apps["svc1"].Description, Equals, "Service #1")

	c.Assert(apps["svc2"].Name, Equals, "svc2")
	c.Assert(apps["svc2"].Description, Equals, "Service #2")

	// ensure we get valid Date()
	st, err := os.Stat(snap.basedir)
	c.Assert(err, IsNil)
	c.Assert(snap.Date(), Equals, st.ModTime())

	c.Assert(snap.basedir, Equals, filepath.Join(s.tempdir, "snaps", helloSnapComposedName, "1.10"))
	c.Assert(snap.InstalledSize(), Not(Equals), -1)
}

func (s *SnapTestSuite) TestSnapYamlMultipleArchitecturesParsing(c *C) {
	y := filepath.Join(s.tempdir, "snap.yaml")
	ioutil.WriteFile(y, []byte(`name: fatbinary
version: 1.0
architectures: [i386, armhf]
`), 0644)
	m, err := parseSnapYamlFile(y)
	c.Assert(err, IsNil)
	c.Assert(m.Architectures, DeepEquals, []string{"i386", "armhf"})
}

func (s *SnapTestSuite) TestSnapYamlSingleArchitecturesParsing(c *C) {
	y := filepath.Join(s.tempdir, "snap.yaml")
	ioutil.WriteFile(y, []byte(`name: fatbinary
version: 1.0
architectures: [i386]
`), 0644)
	m, err := parseSnapYamlFile(y)
	c.Assert(err, IsNil)
	c.Assert(m.Architectures, DeepEquals, []string{"i386"})
}

func (s *SnapTestSuite) TestSnapYamlNoArchitecturesParsing(c *C) {
	y := filepath.Join(s.tempdir, "snap.yaml")
	ioutil.WriteFile(y, []byte(`name: fatbinary
version: 1.0
`), 0644)
	m, err := parseSnapYamlFile(y)
	c.Assert(err, IsNil)
	c.Assert(m.Architectures, DeepEquals, []string{"all"})
}

func (s *SnapTestSuite) TestSnapYamlBadArchitectureParsing(c *C) {
	data := []byte(`name: fatbinary
version: 1.0
architectures:
  armhf:
    no
`)
	_, err := parseSnapYamlData(data, false)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestSnapYamlWorseArchitectureParsing(c *C) {
	data := []byte(`name: fatbinary
version: 1.0
architectures:
  - armhf:
      sometimes
`)
	_, err := parseSnapYamlData(data, false)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestSnapYamlLicenseParsing(c *C) {
	y := filepath.Join(s.tempdir, "snap.yaml")
	ioutil.WriteFile(y, []byte(`
name: foo
version: 1.0
license-agreement: explicit`), 0644)
	m, err := parseSnapYamlFile(y)
	c.Assert(err, IsNil)
	c.Assert(m.LicenseAgreement, Equals, "explicit")
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryGadgetStoreId(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ensure we get the right header
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Assert(storeID, Equals, "my-store")
		w.WriteHeader(404)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	// install custom gadget snap with store-id
	snapYamlFn, err := makeInstalledMockSnap(s.tempdir, `name: gadget-test
version: 1.0
gadget:
  store:
    id: my-store
type: gadget
`)
	c.Assert(err, IsNil)
	makeSnapActive(snapYamlFn)

	s.storeCfg.DetailsURI, err = url.Parse(mockServer.URL)
	c.Assert(err, IsNil)
	repo := NewConfiguredUbuntuStoreSnapRepository()
	c.Assert(repo, NotNil)

	// we just ensure that the right header is set
	repo.Snap("xkcd", "edge")
}

func (s *SnapTestSuite) TestUninstallBuiltIn(c *C) {
	// install custom gadget snap with store-id
	gadgetYaml, err := makeInstalledMockSnap(s.tempdir, `name: gadget-test
version: 1.0
gadget:
  store:
    id: my-store
  software:
    built-in:
      - hello-snap
type: gadget
`)
	c.Assert(err, IsNil)
	makeSnapActive(gadgetYaml)

	snapYamlFn, err := makeInstalledMockSnap(s.tempdir, "")
	c.Assert(err, IsNil)
	makeSnapActive(snapYamlFn)

	p := &MockProgressMeter{}

	installed, err := (&Overlord{}).Installed()
	c.Assert(err, IsNil)
	snaps := FindSnapsByName("hello-snap", installed)
	c.Assert(snaps, HasLen, 1)
	c.Check(s.overlord.Uninstall(snaps[0], p), Equals, ErrPackageNotRemovable)
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

func (s *SnapTestSuite) TestSnapYamlSecurityBinaryParsing(c *C) {
	m, err := parseSnapYamlData(securityBinarySnapYaml, false)
	c.Assert(err, IsNil)

	c.Assert(m.Apps["testme"].Name, Equals, "testme")
	c.Assert(m.Apps["testme"].Command, Equals, "bin/testme")
	c.Assert(m.Plugs["testme"].SecurityCaps, HasLen, 1)
	c.Assert(m.Plugs["testme"].SecurityCaps[0], Equals, "foo_group")
	c.Assert(m.Plugs["testme"].SecurityTemplate, Equals, "foo_template")

	c.Assert(m.Apps["testme-override"].Name, Equals, "testme-override")
	c.Assert(m.Apps["testme-override"].Command, Equals, "bin/testme-override")
	c.Assert(m.Plugs["testme-override"].SecurityCaps, HasLen, 0)
	c.Assert(m.Plugs["testme-override"].SecurityOverride.ReadPaths[0], Equals, "/foo")
	c.Assert(m.Plugs["testme-override"].SecurityOverride.Syscalls[0], Equals, "bar")

	c.Assert(m.Apps["testme-policy"].Name, Equals, "testme-policy")
	c.Assert(m.Apps["testme-policy"].Command, Equals, "bin/testme-policy")
	c.Assert(m.Plugs["testme-policy"].SecurityCaps, HasLen, 0)
	c.Assert(m.Plugs["testme-policy"].SecurityPolicy.AppArmor, Equals, "meta/testme-policy.profile")
}

var securityServiceSnapYaml = []byte(`name: test-snap
version: 1.2.8
apps:
 testme-service:
   command: bin/testme-service.start
   daemon: forking
   stop-command: bin/testme-service.stop
   description: "testme service"
   plugs: [testme-service]

plugs:
 testme-service:
   interface: old-security
   caps:
     - "network-client"
     - "foo_group"
   security-template: "foo_template"
`)

func (s *SnapTestSuite) TestSnapYamlSecurityServiceParsing(c *C) {
	m, err := parseSnapYamlData(securityServiceSnapYaml, false)
	c.Assert(err, IsNil)

	c.Assert(m.Apps["testme-service"].Name, Equals, "testme-service")
	c.Assert(m.Apps["testme-service"].Command, Equals, "bin/testme-service.start")
	c.Assert(m.Apps["testme-service"].Stop, Equals, "bin/testme-service.stop")
	c.Assert(m.Plugs["testme-service"].SecurityCaps, HasLen, 2)
	c.Assert(m.Plugs["testme-service"].SecurityCaps[0], Equals, "network-client")
	c.Assert(m.Plugs["testme-service"].SecurityCaps[1], Equals, "foo_group")
	c.Assert(m.Plugs["testme-service"].SecurityTemplate, Equals, "foo_template")
}

func (s *SnapTestSuite) TestNeedsAppArmorUpdateSecurityPolicy(c *C) {
	// if a security policy is defined, never flag for update
	sd := &SecurityDefinitions{SecurityPolicy: &SecurityPolicyDefinition{}}
	c.Check(sd.NeedsAppArmorUpdate(nil, nil), Equals, false)
}

func (s *SnapTestSuite) TestNeedsAppArmorUpdateSecurityOverride(c *C) {
	// if a security override is defined, always flag for update
	sd := &SecurityDefinitions{SecurityOverride: &SecurityOverrideDefinition{}}
	c.Check(sd.NeedsAppArmorUpdate(nil, nil), Equals, true)
}

func (s *SnapTestSuite) TestNeedsAppArmorUpdateTemplatePresent(c *C) {
	// if the template is in the map, it needs updating
	sd := &SecurityDefinitions{SecurityTemplate: "foo_bar"}
	c.Check(sd.NeedsAppArmorUpdate(nil, map[string]bool{"foo_bar": true}), Equals, true)
}

func (s *SnapTestSuite) TestNeedsAppArmorUpdateTemplateAbsent(c *C) {
	// if the template is not in the map, it does not
	sd := &SecurityDefinitions{SecurityTemplate: "foo_bar"}
	c.Check(sd.NeedsAppArmorUpdate(nil, nil), Equals, false)
}

func (s *SnapTestSuite) TestNeedsAppArmorUpdatePolicyPresent(c *C) {
	// if the cap is in the map, it needs updating
	sd := &SecurityDefinitions{SecurityCaps: []string{"foo_bar"}}
	c.Check(sd.NeedsAppArmorUpdate(map[string]bool{"foo_bar": true}, nil), Equals, true)
}

func (s *SnapTestSuite) TestNeedsAppArmorUpdatePolicyAbsent(c *C) {
	// if the cap is not in the map, it does not
	sd := &SecurityDefinitions{SecurityCaps: []string{"foo_quux"}}
	c.Check(sd.NeedsAppArmorUpdate(map[string]bool{"foo_bar": true}, nil), Equals, false)
}

func (s *SnapTestSuite) TestDetectIllegalYamlBinaries(c *C) {
	_, err := parseSnapYamlData([]byte(`name: foo
version: 1.0
apps:
 tes!me:
   command: someething
`), false)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestDetectIllegalYamlService(c *C) {
	_, err := parseSnapYamlData([]byte(`name: foo
version: 1.0
apps:
 tes!me:
   command: something
   daemon: forking
`), false)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestIllegalPackageNameWithDeveloper(c *C) {
	_, err := parseSnapYamlData([]byte(`name: foo.something
version: 1.0
`), false)

	c.Assert(err, Equals, ErrPackageNameNotSupported)
}

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

func (s *SnapTestSuite) TestParseHardwareYaml(c *C) {
	m, err := parseSnapYamlData(hardwareYaml, false)
	c.Assert(err, IsNil)
	c.Assert(m.Gadget.Hardware.Assign[0].PartID, Equals, "device-hive-iot-hal")
	c.Assert(m.Gadget.Hardware.Assign[0].Rules[0].Kernel, Equals, "ttyUSB0")
	c.Assert(m.Gadget.Hardware.Assign[0].Rules[1].Subsystem, Equals, "tty")
	c.Assert(m.Gadget.Hardware.Assign[0].Rules[1].WithDriver, Equals, "pl2303")
	c.Assert(m.Gadget.Hardware.Assign[0].Rules[1].WithAttrs[0], Equals, "idVendor=0xf00f00")
	c.Assert(m.Gadget.Hardware.Assign[0].Rules[1].WithAttrs[1], Equals, "idProduct=0xb00")
}

var expectedUdevRule = `KERNEL=="ttyUSB0", TAG:="snappy-assign", ENV{SNAPPY_APP}:="device-hive-iot-hal"

SUBSYSTEM=="tty", SUBSYSTEMS=="usb-serial", DRIVER=="pl2303", ATTRS{idVendor}=="0xf00f00", ATTRS{idProduct}=="0xb00", ENV{BAUD}=="9600", ENV{META1}=="foo*", ENV{META2}=="foo?", ENV{META3}=="foo[a-z]", ENV{META4}=="a|b", TAG:="snappy-assign", ENV{SNAPPY_APP}:="device-hive-iot-hal"

`

func (s *SnapTestSuite) TestGenerateHardwareYamlData(c *C) {
	m, err := parseSnapYamlData(hardwareYaml, false)
	c.Assert(err, IsNil)

	output, err := m.Gadget.Hardware.Assign[0].generateUdevRuleContent()
	c.Assert(err, IsNil)

	c.Assert(output, Equals, expectedUdevRule)
}

func (s *SnapTestSuite) TestWriteHardwareUdevEtc(c *C) {
	m, err := parseSnapYamlData(hardwareYaml, false)
	c.Assert(err, IsNil)

	dirs.SnapUdevRulesDir = c.MkDir()
	writeGadgetHardwareUdevRules(m)

	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapUdevRulesDir, "80-snappy_gadget-foo_device-hive-iot-hal.rules")), Equals, true)
}

func (s *SnapTestSuite) TestWriteHardwareUdevCleanup(c *C) {
	m, err := parseSnapYamlData(hardwareYaml, false)
	c.Assert(err, IsNil)

	dirs.SnapUdevRulesDir = c.MkDir()
	udevRulesFile := filepath.Join(dirs.SnapUdevRulesDir, "80-snappy_gadget-foo_device-hive-iot-hal.rules")
	c.Assert(ioutil.WriteFile(udevRulesFile, nil, 0644), Equals, nil)
	cleanupGadgetHardwareUdevRules(m)

	c.Assert(osutil.FileExists(udevRulesFile), Equals, false)
}

func (s *SnapTestSuite) TestWriteHardwareUdevActivate(c *C) {
	type aCmd []string
	var cmds = []aCmd{}

	runUdevAdm = func(args ...string) error {
		cmds = append(cmds, args)
		return nil
	}
	defer func() { runUdevAdm = runUdevAdmImpl }()

	err := activateGadgetHardwareUdevRules()
	c.Assert(err, IsNil)
	c.Assert(cmds[0], DeepEquals, aCmd{"udevadm", "control", "--reload-rules"})
	c.Assert(cmds[1], DeepEquals, aCmd{"udevadm", "trigger"})
	c.Assert(cmds, HasLen, 2)
}

func (s *SnapTestSuite) TestParseSnapYamlDataChecksName(c *C) {
	_, err := parseSnapYamlData([]byte(`
version: 1.0
`), false)
	c.Assert(err, ErrorMatches, "can not parse snap.yaml: missing required fields 'name'.*")
}

func (s *SnapTestSuite) TestParseSnapYamlDataChecksVersion(c *C) {
	_, err := parseSnapYamlData([]byte(`
name: foo
`), false)
	c.Assert(err, ErrorMatches, "can not parse snap.yaml: missing required fields 'version'.*")
}

func (s *SnapTestSuite) TestParseSnapYamlDataChecksMultiple(c *C) {
	_, err := parseSnapYamlData([]byte(`
`), false)
	c.Assert(err, ErrorMatches, "can not parse snap.yaml: missing required fields 'name, version'.*")
}

func (s *SnapTestSuite) TestChannelFromLocalManifest(c *C) {
	snapYaml, err := s.makeInstalledMockSnap()
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnap(snapYaml)
	c.Assert(snap.Channel(), Equals, "remote-channel")
}

func (s *SnapTestSuite) TestIcon(c *C) {
	snapYaml, err := s.makeInstalledMockSnap()
	snap, err := NewInstalledSnap(snapYaml)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(snap.basedir, "meta", "gui"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(snap.basedir, "meta", "gui", "icon.png"), nil, 0644)
	c.Assert(err, IsNil)

	c.Check(snap.Icon(), Matches, filepath.Join(dirs.SnapSnapsDir, snap.Name(), snap.Version(), "meta/gui/icon.png"))
}

func (s *SnapTestSuite) TestIconEmpty(c *C) {
	snapYaml, err := s.makeInstalledMockSnap(`name: foo
version: 1.0
`)
	snap, err := NewInstalledSnap(snapYaml)
	c.Assert(err, IsNil)
	// no icon in the yaml!
	c.Check(snap.Icon(), Equals, "")
}
