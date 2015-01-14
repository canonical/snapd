package snappy

import (
	"io/ioutil"
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

func (s *SnappTestSuite) TestLocalSnappInvalidPath(c *C) {
	snapp := NewLocalSnappPart("invalid-path")
	c.Assert(snapp, IsNil)
}

func (s *SnappTestSuite) TestLocalSnappSimple(c *C) {

	snapp_yaml, err := s.makeMockSnapp()
	c.Assert(err, IsNil)

	snapp := NewLocalSnappPart(snapp_yaml)
	c.Assert(snapp, NotNil)
	c.Assert(snapp.Name(), Equals, "hello-app")
	c.Assert(snapp.Version(), Equals, "1.10")

	c.Assert(snapp.basedir, Equals, path.Join(s.tempdir, "apps", "hello-app", "1.10"))
}

func (s *SnappTestSuite) TestLocalSnappRepositoryInvalid(c *C) {

	snapp := NewLocalSnappRepository("invalid-path")
	c.Assert(snapp, IsNil)
}

func (s *SnappTestSuite) TestLocalSnappRepositorySimple(c *C) {

	_, err := s.makeMockSnapp()
	c.Assert(err, IsNil)

	snapp := NewLocalSnappRepository(path.Join(s.tempdir, "apps"))
	c.Assert(snapp, NotNil)

	installed, err := snapp.GetInstalled()
	c.Assert(err, IsNil)
	c.Assert(len(installed), Equals, 1)
	c.Assert(installed[0].Name(), Equals, "hello-app")
	c.Assert(installed[0].Version(), Equals, "1.10")
}
