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

const MOCK_SEARCH_JSON = `{
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

type MockUbuntuStoreServer struct {
	quit chan int

	searchUri string
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, MOCK_SEARCH_JSON)
}

func (s *SnappTestSuite) TestUuntuStoreRepository(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(handleSearch))
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
