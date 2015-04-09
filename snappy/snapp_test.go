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
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/partition"
	"launchpad.net/snappy/systemd"

	. "launchpad.net/gocheck"
)

type SnapTestSuite struct {
	tempdir string
}

var _ = Suite(&SnapTestSuite{})

func (s *SnapTestSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	newPartition = func() (p partition.Interface) {
		return new(MockPartition)
	}

	SetRootDir(s.tempdir)
	os.MkdirAll(snapServicesDir, 0755)

	clickSystemHooksDir = filepath.Join(s.tempdir, "/usr/share/click/hooks")
	os.MkdirAll(clickSystemHooksDir, 0755)

	// we may not have debsig-verify installed (and we don't need it
	// for the unittests)
	runDebsigVerify = func(snapFile string, allowUnauth bool) (err error) {
		return nil
	}
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}

	// fake "du"
	duCmd = makeFakeDuCommand(c)

	// do not attempt to hit the real store servers in the tests
	storeSearchURI = ""
	storeDetailsURI = ""
	storeBulkURI = ""

	aaExec = filepath.Join(s.tempdir, "aa-exec")
	err := ioutil.WriteFile(aaExec, []byte(mockAaExecScript), 0755)
	c.Assert(err, IsNil)

	// ensure we do not look at the system
	systemImageRoot = s.tempdir
}

func (s *SnapTestSuite) TearDownTest(c *C) {
	// ensure all functions are back to their original state
	regenerateAppArmorRules = regenerateAppArmorRulesImpl
	InstalledSnapNamesByType = installedSnapNamesByTypeImpl
	duCmd = "du"
}

func (s *SnapTestSuite) makeInstalledMockSnap() (yamlFile string, err error) {
	return makeInstalledMockSnap(s.tempdir, "")
}

func makeSnapActive(packageYamlPath string) (err error) {
	snapdir := filepath.Dir(filepath.Dir(packageYamlPath))
	parent := filepath.Dir(snapdir)
	err = os.Symlink(snapdir, filepath.Join(parent, "current"))

	return err
}

func (s *SnapTestSuite) TestLocalSnapInvalidPath(c *C) {
	snap := NewInstalledSnapPart("invalid-path")
	c.Assert(snap, IsNil)
}

func (s *SnapTestSuite) TestLocalSnapSimple(c *C) {
	snapYaml, err := s.makeInstalledMockSnap()
	c.Assert(err, IsNil)

	snap := NewInstalledSnapPart(snapYaml)
	c.Assert(snap, NotNil)
	c.Assert(snap.Name(), Equals, "hello-app")
	c.Assert(snap.Version(), Equals, "1.10")
	c.Assert(snap.IsActive(), Equals, false)

	services := snap.Services()
	c.Assert(services, HasLen, 1)
	c.Assert(services[0].Name, Equals, "svc1")

	// ensure we get valid Date()
	st, err := os.Stat(snap.basedir)
	c.Assert(err, IsNil)
	c.Assert(snap.Date(), Equals, st.ModTime())

	c.Assert(snap.basedir, Equals, filepath.Join(s.tempdir, "apps", "hello-app", "1.10"))
	c.Assert(snap.InstalledSize(), Not(Equals), -1)
}

func (s *SnapTestSuite) TestLocalSnapHash(c *C) {
	snapYaml, err := s.makeInstalledMockSnap()
	c.Assert(err, IsNil)

	hashesFile := filepath.Join(filepath.Dir(snapYaml), "hashes.yaml")
	err = ioutil.WriteFile(hashesFile, []byte("archive-sha512: F00F00"), 0644)
	c.Assert(err, IsNil)

	snap := NewInstalledSnapPart(snapYaml)
	c.Assert(snap.Hash(), Equals, "F00F00")
}

func (s *SnapTestSuite) TestLocalSnapActive(c *C) {
	snapYaml, err := s.makeInstalledMockSnap()
	c.Assert(err, IsNil)
	makeSnapActive(snapYaml)

	snap := NewInstalledSnapPart(snapYaml)
	c.Assert(snap.IsActive(), Equals, true)
}

func (s *SnapTestSuite) TestLocalSnapFrameworks(c *C) {
	snapYaml, err := makeInstalledMockSnap(s.tempdir, `name: foo
version: 1.0
frameworks:
 - one
 - two
`)
	c.Assert(err, IsNil)

	snap := NewInstalledSnapPart(snapYaml)
	fmk, err := snap.Frameworks()
	c.Assert(err, IsNil)
	c.Check(fmk, DeepEquals, []string{"one", "two"})
}

func (s *SnapTestSuite) TestLocalSnapRepositoryInvalid(c *C) {
	snap := NewLocalSnapRepository("invalid-path")
	c.Assert(snap, IsNil)
}

func (s *SnapTestSuite) TestLocalSnapRepositorySimple(c *C) {
	yamlPath, err := s.makeInstalledMockSnap()
	c.Assert(err, IsNil)
	err = makeSnapActive(yamlPath)
	c.Assert(err, IsNil)

	snap := NewLocalSnapRepository(filepath.Join(s.tempdir, "apps"))
	c.Assert(snap, NotNil)

	installed, err := snap.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)
	c.Assert(installed[0].Name(), Equals, "hello-app")
	c.Assert(installed[0].Version(), Equals, "1.10")
}

/* acquired via:
   curl  -H 'accept: application/hal+json' -H "X-Ubuntu-Frameworks: ubuntu-core-15.04-dev1" -H "X-Ubuntu-Architecture: amd64" https://search.apps.ubuntu.com/api/v1/search?q=hello
*/
const MockSearchJSON = `{
  "_links": {
    "self": {
      "href": "https:\/\/search.apps.ubuntu.com\/api\/v1\/search?q=xkcd"
    },
    "curies": [
      {
        "templated": true,
        "name": "clickindex",
        "href": "https:\/\/search.apps.ubuntu.com\/docs\/relations.html{#rel}"
      }
    ]
  },
  "_embedded": {
    "clickindex:package": [
      {
        "prices": null,
        "_links": {
          "self": {
            "href": "https:\/\/search.apps.ubuntu.com\/api\/v1\/package\/com.ubuntu.snappy.xkcd-webserver"
          }
        },
        "version": "0.1",
        "ratings_average": 0.0,
        "content": "application",
        "price": 0.0,
        "icon_url": "https:\/\/myapps.developer.ubuntu.com\/site_media\/appmedia\/2014\/12\/xkcd.svg.png",
        "title": "Show random XKCD comic",
        "name": "xkcd-webserver.mvo",
        "publisher": "Canonical"
      }
    ]
  }
}`

/* acquired via:
curl --data-binary '{"name":["docker","foo","com.ubuntu.snappy.hello-world","httpd-minimal-golang-example","owncloud","xkcd-webserver"]}'  -H 'content-type: application/json' https://myapps.developer.ubuntu.com/dev/api/click-metadata/
*/
const MockUpdatesJSON = `
[
    {
        "status": "Published",
        "name": "hello-world",
        "changelog": "",
        "icon_url": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/01/hello.svg.png",
        "title": "Hello world example",
        "binary_filesize": 31166,
        "anon_download_url": "https://public.apps.ubuntu.com/anon/download/com.ubuntu.snappy/hello-world/hello-world_1.0.5_all.snap",
        "allow_unauthenticated": true,
        "version": "1.0.5",
        "download_url": "https://public.apps.ubuntu.com/download/com.ubuntu.snappy/hello-world/hello-world_1.0.5_all.snap",
        "download_sha512": "3e8b192e18907d8195c2e380edd048870eda4f6dbcba8f65e4625d6efac3c37d11d607147568ade6f002b6baa30762c6da02e7ee462de7c56301ddbdc10d87f6"
    }
]
`

/* acquired via
   curl -H "accept: application/hal+json" -H "X-Ubuntu-Frameworks: ubuntu-core-15.04-dev1" https://search.apps.ubuntu.com/api/v1/package/com.ubuntu.snappy.xkcd-webserver
*/
const MockDetailsJSON = `
{
  "architecture": [
    "all"
  ],
  "allow_unauthenticated": true,
  "click_version": "0.1",
  "changelog": "",
  "date_published": "2014-12-05T13:12:31.785911Z",
  "license": "Apache License",
  "name": "xkcd-webserver",
  "publisher": "Canonical",
  "blacklist_country_codes": [],
  "icon_urls": {
    "256": "https:\/\/myapps.developer.ubuntu.com\/site_media\/appmedia\/2014\/12\/xkcd.svg.png"
  },
  "prices": null,
  "framework": [
    "ubuntu-core-15.04-dev1"
  ],
  "translations": null,
  "price": 0.0,
  "click_framework": [
    "ubuntu-core-15.04-dev1"
  ],
  "description": "Snappy\nThis is meant as a fun example for a snappy package.\r\n",
  "download_sha512": "3a9152b8bff494c036f40e2ca03d1dfaa4ddcfe651eae1c9419980596f48fa95b2f2a91589305af7d55dc08e9489b8392585bbe2286118550b288368e5d9a620",
  "website": "",
  "screenshot_urls": [],
  "department": [
    "entertainment"
  ],
  "company_name": "Canonical",
  "_links": {
    "self": {
      "href": "https:\/\/search.apps.ubuntu.com\/api\/v1\/package\/com.ubuntu.snappy.xkcd-webserver"
    },
    "curies": [
      {
        "templated": true,
        "name": "clickindex",
        "href": "https:\/\/search.apps.ubuntu.com\/docs\/v1\/relations.html{#rel}"
      }
    ]
  },
  "version": "0.3.1",
  "developer_name": "Snappy App Dev",
  "content": "application",
  "anon_download_url": "https:\/\/public.apps.ubuntu.com\/anon\/download\/com.ubuntu.snappy\/xkcd-webserver\/com.ubuntu.snappy.xkcd-webserver_0.3.1_all.click",
  "binary_filesize": 21236,
  "icon_url": "https:\/\/myapps.developer.ubuntu.com\/site_media\/appmedia\/2014\/12\/xkcd.svg.png",
  "support_url": "mailto:michael.vogt@ubuntu.com",
  "title": "Show random XKCD compic via a build-in webserver",
  "ratings_average": 0.0,
  "id": 1287,
  "screenshot_url": null,
  "terms_of_service": "",
  "download_url": "https:\/\/public.apps.ubuntu.com\/download\/com.ubuntu.snappy\/xkcd-webserver\/com.ubuntu.snappy.xkcd-webserver_0.3.1_all.click",
  "video_urls": [],
  "keywords": [
    "snappy"
  ],
  "video_embedded_html_urls": [],
  "last_updated": "2014-12-05T12:33:05.928364Z",
  "status": "Published",
  "whitelist_country_codes": []
}`
const MockNoDetailsJSON = `{"errors": ["No such package"], "result": "error"}`

type MockUbuntuStoreServer struct {
	quit chan int

	searchURI string
}

func (s *SnapTestSuite) TestUbuntuStoreRepositorySearch(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, MockSearchJSON)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	storeSearchURI = mockServer.URL + "/%s"
	snap := NewUbuntuStoreSnapRepository()
	c.Assert(snap, NotNil)

	results, err := snap.Search("xkcd")
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name(), Equals, "xkcd-webserver.mvo")
	c.Assert(results[0].Version(), Equals, "0.1")
	c.Assert(results[0].Description(), Equals, "Show random XKCD comic")

	c.Assert(results[0].Channel(), Equals, "edge")
}

func mockInstalledSnapNamesByType(mockSnaps []string) {
	InstalledSnapNamesByType = func(snapTs ...SnapType) (res []string, err error) {
		return mockSnaps, nil
	}
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryUpdates(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonReq, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		c.Assert(string(jsonReq), Equals, `{"name":["hello-world"]}`)
		io.WriteString(w, MockUpdatesJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	storeBulkURI = mockServer.URL + "/updates/"
	snap := NewUbuntuStoreSnapRepository()
	c.Assert(snap, NotNil)

	// override the real InstalledSnapNamesByType to return our
	// mock data
	mockInstalledSnapNamesByType([]string{"hello-world"})

	// the actual test
	results, err := snap.Updates()
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name(), Equals, "hello-world")
	c.Assert(results[0].Version(), Equals, "1.0.5")
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryUpdatesNoSnaps(c *C) {

	storeDetailsURI = "https://some-uri"
	snap := NewUbuntuStoreSnapRepository()
	c.Assert(snap, NotNil)

	// ensure we do not hit the net if there is nothing installed
	// (otherwise the store will send us all snaps)
	snap.bulkURI = "http://i-do.not-exist.really-not"
	mockInstalledSnapNamesByType([]string{})

	// the actual test
	results, err := snap.Updates()
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 0)
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryDetails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// no store ID by default
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Assert(storeID, Equals, "")

		c.Assert(strings.HasSuffix(r.URL.String(), "xkcd-webserver"), Equals, true)
		io.WriteString(w, MockDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	storeDetailsURI = mockServer.URL + "/details/%s"
	snap := NewUbuntuStoreSnapRepository()
	c.Assert(snap, NotNil)

	// the actual test
	results, err := snap.Details("xkcd-webserver")
	c.Assert(err, IsNil)
	c.Assert(results, HasLen, 1)
	c.Assert(results[0].Name(), Equals, "xkcd-webserver")
	c.Assert(results[0].Version(), Equals, "0.3.1")
	c.Assert(results[0].Hash(), Equals, "3a9152b8bff494c036f40e2ca03d1dfaa4ddcfe651eae1c9419980596f48fa95b2f2a91589305af7d55dc08e9489b8392585bbe2286118550b288368e5d9a620")
	c.Assert(results[0].Date(), Equals, time.Date(2014, time.December, 05, 12, 33, 05, 928364000, time.UTC))
	c.Assert(results[0].DownloadSize(), Equals, int64(21236))
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryNoDetails(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(strings.HasSuffix(r.URL.String(), "no-such-pkg"), Equals, true)
		w.WriteHeader(404)
		io.WriteString(w, MockNoDetailsJSON)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	storeDetailsURI = mockServer.URL + "/details/%s"
	snap := NewUbuntuStoreSnapRepository()
	c.Assert(snap, NotNil)

	// the actual test
	results, err := snap.Details("no-such-pkg")
	c.Assert(results, HasLen, 0)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestMakeConfigEnv(c *C) {
	yamlFile, err := makeInstalledMockSnap(s.tempdir, "")
	c.Assert(err, IsNil)
	snap := NewInstalledSnapPart(yamlFile)
	c.Assert(snap, NotNil)

	os.Setenv("SNAP_NAME", "override-me")
	defer os.Setenv("SNAP_NAME", "")

	env := makeSnapHookEnv(snap)

	// now ensure that the environment we get back is what we want
	envMap := helpers.MakeMapFromEnvList(env)
	// regular env is unaltered
	c.Assert(envMap["PATH"], Equals, os.Getenv("PATH"))
	// SNAP_* is overriden
	c.Assert(envMap["SNAP_NAME"], Equals, "hello-app")
	c.Assert(envMap["SNAP_VERSION"], Equals, "1.10")
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryInstallRemoveSnap(c *C) {
	snapPackage := makeTestSnapPackage(c, "")
	snapR, err := os.Open(snapPackage)
	c.Assert(err, IsNil)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(w, snapR)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snap := RemoteSnapPart{}
	snap.pkg.AnonDownloadURL = mockServer.URL + "/snap"

	p := &MockProgressMeter{}
	name, err := snap.Install(p, 0)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	st, err := os.Stat(snapPackage)
	c.Assert(err, IsNil)
	c.Assert(p.written, Equals, int(st.Size()))
}

func (s *SnapTestSuite) TestRemoteSnapUpgradeService(c *C) {
	snapPackage := makeTestSnapPackage(c, `name: foo
version: 1.0
services:
 - name: svc
`)
	snapR, err := os.Open(snapPackage)
	c.Assert(err, IsNil)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(w, snapR)
		snapR.Seek(0, 0)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snap := RemoteSnapPart{}
	snap.pkg.AnonDownloadURL = mockServer.URL + "/snap"

	p := &MockProgressMeter{}
	name, err := snap.Install(p, 0)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	c.Check(p.notified, HasLen, 0)

	_, err = snap.Install(p, 0)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	c.Check(p.notified, HasLen, 1)
	c.Check(p.notified[0], Matches, "Waiting for .* stop.")
}

func (s *SnapTestSuite) TestRemoteSnapErrors(c *C) {
	snap := RemoteSnapPart{}

	c.Assert(snap.SetActive(nil), Equals, ErrNotInstalled)
	c.Assert(snap.Uninstall(nil), Equals, ErrNotInstalled)
}

func (s *SnapTestSuite) TestServicesWithPorts(c *C) {
	const packageHello = `name: hello-app
version: 1.10
vendor: Michael Vogt <mvo@ubuntu.com>
icon: meta/hello.svg
binaries:
 - name: bin/hello
services:
 - name: svc1
   description: "Service #1"
   ports:
      external:
        ui:
          port: 8080/tcp
        nothing:
          port: 8081/tcp
          negotiable: yes
 - name: svc2
   description: "Service #2"
`

	yamlFile, err := makeInstalledMockSnap(s.tempdir, packageHello)
	c.Assert(err, IsNil)

	snap := NewInstalledSnapPart(yamlFile)
	c.Assert(snap, NotNil)

	c.Assert(snap.Name(), Equals, "hello-app")
	c.Assert(snap.Version(), Equals, "1.10")
	c.Assert(snap.IsActive(), Equals, false)

	services := snap.Services()
	c.Assert(services, HasLen, 2)

	c.Assert(services[0].Name, Equals, "svc1")
	c.Assert(services[0].Description, Equals, "Service #1")

	external1Ui, ok := services[0].Ports.External["ui"]
	c.Assert(ok, Equals, true)
	c.Assert(external1Ui.Port, Equals, "8080/tcp")
	c.Assert(external1Ui.Negotiable, Equals, false)

	external1Nothing, ok := services[0].Ports.External["nothing"]
	c.Assert(ok, Equals, true)
	c.Assert(external1Nothing.Port, Equals, "8081/tcp")
	c.Assert(external1Nothing.Negotiable, Equals, true)

	c.Assert(services[1].Name, Equals, "svc2")
	c.Assert(services[1].Description, Equals, "Service #2")

	// ensure we get valid Date()
	st, err := os.Stat(snap.basedir)
	c.Assert(err, IsNil)
	c.Assert(snap.Date(), Equals, st.ModTime())

	c.Assert(snap.basedir, Equals, filepath.Join(s.tempdir, "apps", "hello-app", "1.10"))
	c.Assert(snap.InstalledSize(), Not(Equals), -1)
}

func (s *SnapTestSuite) TestPackageYamlMultipleArchitecturesParsing(c *C) {
	y := filepath.Join(s.tempdir, "package.yaml")
	ioutil.WriteFile(y, []byte(`name: fatbinary
version: 1.0
vendor: Michael Vogt <mvo@ubuntu.com>
architecture: [i386, armhf]
`), 0644)
	m, err := parsePackageYamlFile(y)
	c.Assert(err, IsNil)
	c.Assert(m.Architectures, DeepEquals, []string{"i386", "armhf"})
}

func (s *SnapTestSuite) TestPackageYamlSingleArchitecturesParsing(c *C) {
	y := filepath.Join(s.tempdir, "package.yaml")
	ioutil.WriteFile(y, []byte(`name: fatbinary
version: 1.0
vendor: Michael Vogt <mvo@ubuntu.com>
architecture: i386
`), 0644)
	m, err := parsePackageYamlFile(y)
	c.Assert(err, IsNil)
	c.Assert(m.Architectures, DeepEquals, []string{"i386"})
}

func (s *SnapTestSuite) TestPackageYamlNoArchitecturesParsing(c *C) {
	y := filepath.Join(s.tempdir, "package.yaml")
	ioutil.WriteFile(y, []byte(`name: fatbinary
version: 1.0
vendor: Michael Vogt <mvo@ubuntu.com>
`), 0644)
	m, err := parsePackageYamlFile(y)
	c.Assert(err, IsNil)
	c.Assert(m.Architectures, DeepEquals, []string{"all"})
}

func (s *SnapTestSuite) TestPackageYamlLicenseParsing(c *C) {
	y := filepath.Join(s.tempdir, "package.yaml")
	ioutil.WriteFile(y, []byte(`explicit-license-agreement: Y`), 0644)
	m, err := parsePackageYamlFile(y)
	c.Assert(err, IsNil)
	c.Assert(m.ExplicitLicenseAgreement, Equals, true)
}

func (s *SnapTestSuite) TestUbuntuStoreRepositoryOemStoreId(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ensure we get the right header
		storeID := r.Header.Get("X-Ubuntu-Store")
		c.Assert(storeID, Equals, "my-store")
		w.WriteHeader(404)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	// install custom oem snap with store-id
	packageYaml, err := makeInstalledMockSnap(s.tempdir, `name: oem-test
version: 1.0
vendor: mvo
oem:
  store:
    id: my-store
type: oem
`)
	c.Assert(err, IsNil)
	makeSnapActive(packageYaml)

	storeDetailsURI = mockServer.URL + "/%s"
	repo := NewUbuntuStoreSnapRepository()
	c.Assert(repo, NotNil)

	// we just ensure that the right header is set
	repo.Details("xkcd")
}

var securityBinaryPackageYaml = []byte(`name: test-snap.jdstrand
version: 1.2.8
vendor: Jamie Strandboge <jamie@canonical.com>
icon: meta/hello.svg
binaries:
 - name: testme
   exec: bin/testme
   description: "testme client"
   caps:
     - "foo_group"
   security-template: "foo_template"
 - name: testme-override
   exec: bin/testme-override
   security-override:
     apparmor: meta/testme-override.apparmor
 - name: testme-policy
   exec: bin/testme-policy
   security-policy:
     apparmor: meta/testme-policy.profile
`)

func (s *SnapTestSuite) TestPackageYamlSecurityBinaryParsing(c *C) {
	m, err := parsePackageYamlData(securityBinaryPackageYaml)
	c.Assert(err, IsNil)

	c.Assert(m.Binaries[0].Name, Equals, "testme")
	c.Assert(m.Binaries[0].Exec, Equals, "bin/testme")
	c.Assert(m.Binaries[0].SecurityCaps, HasLen, 1)
	c.Assert(m.Binaries[0].SecurityCaps[0], Equals, "foo_group")
	c.Assert(m.Binaries[0].SecurityTemplate, Equals, "foo_template")

	c.Assert(m.Binaries[1].Name, Equals, "testme-override")
	c.Assert(m.Binaries[1].Exec, Equals, "bin/testme-override")
	c.Assert(m.Binaries[1].SecurityCaps, HasLen, 0)
	c.Assert(m.Binaries[1].SecurityOverride.Apparmor, Equals, "meta/testme-override.apparmor")

	c.Assert(m.Binaries[2].Name, Equals, "testme-policy")
	c.Assert(m.Binaries[2].Exec, Equals, "bin/testme-policy")
	c.Assert(m.Binaries[2].SecurityCaps, HasLen, 0)
	c.Assert(m.Binaries[2].SecurityPolicy.Apparmor, Equals, "meta/testme-policy.profile")
}

var securityServicePackageYaml = []byte(`name: test-snap.jdstrand
version: 1.2.8
vendor: Jamie Strandboge <jamie@canonical.com>
icon: meta/hello.svg
services:
 - name: testme-service
   start: bin/testme-service.start
   stop: bin/testme-service.stop
   description: "testme service"
   caps:
     - "networking"
     - "foo_group"
   security-template: "foo_template"
`)

func (s *SnapTestSuite) TestPackageYamlSecurityServiceParsing(c *C) {
	m, err := parsePackageYamlData(securityServicePackageYaml)
	c.Assert(err, IsNil)

	c.Assert(m.Services[0].Name, Equals, "testme-service")
	c.Assert(m.Services[0].Start, Equals, "bin/testme-service.start")
	c.Assert(m.Services[0].Stop, Equals, "bin/testme-service.stop")
	c.Assert(m.Services[0].SecurityCaps, HasLen, 2)
	c.Assert(m.Services[0].SecurityCaps[0], Equals, "networking")
	c.Assert(m.Services[0].SecurityCaps[1], Equals, "foo_group")
	c.Assert(m.Services[0].SecurityTemplate, Equals, "foo_template")
}

func (s *SnapTestSuite) TestPackageYamlFrameworkParsing(c *C) {
	m, err := parsePackageYamlData([]byte(`name: foo
framework: one, two
`))
	c.Assert(err, IsNil)
	c.Assert(m.Frameworks, HasLen, 2)
	c.Check(m.Frameworks, DeepEquals, []string{"one", "two"})
	c.Check(m.FrameworksForClick(), Matches, "one,two,ubuntu-core.*")
}

func (s *SnapTestSuite) TestPackageYamlFrameworksParsing(c *C) {
	m, err := parsePackageYamlData([]byte(`name: foo
frameworks:
 - one
 - two
`))
	c.Assert(err, IsNil)
	c.Assert(m.Frameworks, HasLen, 2)
	c.Check(m.Frameworks, DeepEquals, []string{"one", "two"})
	c.Check(m.FrameworksForClick(), Matches, "one,two,ubuntu-core.*")
}

func (s *SnapTestSuite) TestPackageYamlFrameworkAndFrameworksFails(c *C) {
	_, err := parsePackageYamlData([]byte(`name: foo
frameworks:
 - one
 - two
framework: three, four
`))
	c.Assert(err, Equals, ErrInvalidFrameworkSpecInYaml)
}

func (s *SnapTestSuite) TestDetectsNameClash(c *C) {
	data := []byte(`name: afoo
version: 1.0
services:
 - name: foo
binaries:
 - name: foo
`)
	yaml, err := parsePackageYamlData(data)
	c.Assert(err, IsNil)
	err = yaml.checkForNameClashes()
	c.Assert(err, ErrorMatches, ".*binary and service both called foo.*")
}

func (s *SnapTestSuite) TestDetectsMissingFrameworks(c *C) {
	data := []byte(`name: afoo
version: 1.0
frameworks:
 - missing
 - also-missing
`)
	yaml, err := parsePackageYamlData(data)
	c.Assert(err, IsNil)
	err = yaml.checkForFrameworks()
	c.Assert(err, ErrorMatches, `missing frameworks: missing, also-missing`)
}
