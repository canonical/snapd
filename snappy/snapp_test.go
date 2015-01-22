package snappy

import (
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path"

	. "gopkg.in/check.v1"
)

const (
	PACKAGE_HELLO = `
name: hello-app
version: 1.10
vendor: Michael Vogt <mvo@ubuntu.com>
icon: meta/hello.svg
binaries:
 - name: bin/hello
`
)

type SnappTestSuite struct {
	tempdir string
}

var _ = Suite(&SnappTestSuite{})

func (s *SnappTestSuite) SetUpTest(c *C) {
	var err error
	s.tempdir, err = ioutil.TempDir("", "snapp-test-")
	if err != nil {
		panic("Can not create temp dir")
	}
}

func (s *SnappTestSuite) TearDownTest(c *C) {
	os.RemoveAll(s.tempdir)
}

func (s *SnappTestSuite) makeMockSnapp() (snapp_dir string, err error) {
	meta_dir := path.Join(s.tempdir, "apps", "hello-app", "1.10", "meta")
	err = os.MkdirAll(meta_dir, 0777)
	if err != nil {
		return "", err
	}
	yaml_file := path.Join(meta_dir, "package.yaml")
	ioutil.WriteFile(yaml_file, []byte(PACKAGE_HELLO), 0666)

	snapp_dir, _ = path.Split(meta_dir)
	return yaml_file, err
}

func makeSnappActive(package_yaml_path string) (err error) {
	snappdir := path.Dir(path.Dir(package_yaml_path))
	parent := path.Dir(snappdir)
	err = os.Symlink(snappdir, path.Join(parent, "current"))

	return err
}

func (s *SnappTestSuite) TestLocalSnappInvalidPath(c *C) {
	snapp := NewInstalledSnappPart("invalid-path")
	c.Assert(snapp, IsNil)
}

func (s *SnappTestSuite) TestLocalSnappSimple(c *C) {
	snapp_yaml, err := s.makeMockSnapp()
	c.Assert(err, IsNil)

	snapp := NewInstalledSnappPart(snapp_yaml)
	c.Assert(snapp, NotNil)
	c.Assert(snapp.Name(), Equals, "hello-app")
	c.Assert(snapp.Version(), Equals, "1.10")
	c.Assert(snapp.IsActive(), Equals, false)

	c.Assert(snapp.basedir, Equals, path.Join(s.tempdir, "apps", "hello-app", "1.10"))
}

func (s *SnappTestSuite) TestLocalSnappActive(c *C) {
	snapp_yaml, err := s.makeMockSnapp()
	c.Assert(err, IsNil)
	makeSnappActive(snapp_yaml)

	snapp := NewInstalledSnappPart(snapp_yaml)
	c.Assert(snapp.IsActive(), Equals, true)
}

func (s *SnappTestSuite) TestLocalSnappRepositoryInvalid(c *C) {
	snapp := NewLocalSnappRepository("invalid-path")
	c.Assert(snapp, IsNil)
}

func (s *SnappTestSuite) TestLocalSnappRepositorySimple(c *C) {
	yaml_path, err := s.makeMockSnapp()
	c.Assert(err, IsNil)
	err = makeSnappActive(yaml_path)
	c.Assert(err, IsNil)

	snapp := NewLocalSnappRepository(path.Join(s.tempdir, "apps"))
	c.Assert(snapp, NotNil)

	installed, err := snapp.GetInstalled()
	c.Assert(err, IsNil)
	c.Assert(len(installed), Equals, 1)
	c.Assert(installed[0].Name(), Equals, "hello-app")
	c.Assert(installed[0].Version(), Equals, "1.10")
}

const MockSearchJson = `{
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

const MockUpdatesJson = `
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

type MockUbuntuStoreServer struct {
	quit chan int

	searchUri string
}

func (s *SnappTestSuite) TestUbuntuStoreRepositorySearch(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, MockSearchJson)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snapp := NewUbuntuStoreSnappRepository()
	c.Assert(snapp, NotNil)
	snapp.searchUri = mockServer.URL + "/%s"

	results, err := snapp.Search("xkcd")
	c.Assert(err, IsNil)
	c.Assert(len(results), Equals, 1)
	c.Assert(results[0].Name(), Equals, "xkcd-webserver.mvo")
	c.Assert(results[0].Version(), Equals, "0.1")
	c.Assert(results[0].Description(), Equals, "Show random XKCD comic")
}

func mockGetInstalledSnappNamesByType(mockSnapps []string) (mockRestorer func()) {
	origFunc := GetInstalledSnappNamesByType
	GetInstalledSnappNamesByType = func(snappType string) (res []string, err error) {
		return mockSnapps, nil
	}
	return func() {
		GetInstalledSnappNamesByType = origFunc
	}
}

func (s *SnappTestSuite) TestUbuntuStoreRepositoryGetUpdates(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json_req, err := ioutil.ReadAll(r.Body)
		c.Assert(err, IsNil)
		c.Assert(string(json_req), Equals, `{"name":["hello-world"]}`)
		io.WriteString(w, MockUpdatesJson)
	}))

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	snapp := NewUbuntuStoreSnappRepository()
	c.Assert(snapp, NotNil)
	snapp.bulkUri = mockServer.URL + "/updates/"

	// override the real GetInstalledSnappNamesByType to return our
	// mock data
	mockRestorer := mockGetInstalledSnappNamesByType([]string{"hello-world"})
	defer mockRestorer()

	// the actual test
	results, err := snapp.GetUpdates()
	c.Assert(err, IsNil)
	c.Assert(len(results), Equals, 1)
	c.Assert(results[0].Name(), Equals, "hello-world")
	c.Assert(results[0].Version(), Equals, "1.0.5")
}

func (s *SnappTestSuite) TestUbuntuStoreRepositoryGetUpdatesNoSnapps(c *C) {

	snapp := NewUbuntuStoreSnappRepository()
	c.Assert(snapp, NotNil)

	// ensure we do not hit the net if there is nothing installed
	// (otherwise the store will send us all snapps)
	snapp.bulkUri = "http://i-do.not-exist.really-not"
	mockRestorer := mockGetInstalledSnappNamesByType([]string{})
	defer mockRestorer()

	// the actual test
	results, err := snapp.GetUpdates()
	c.Assert(err, IsNil)
	c.Assert(len(results), Equals, 0)
}
