package partition

import (
	"io/ioutil"
	"os"
	"path"

	. "gopkg.in/check.v1"
)

type UtilsTestSuite struct {
	tmp string
}

var _ = Suite(&UtilsTestSuite{})

func (s *UtilsTestSuite) SetUpTest(c *C) {
	var err error
	s.tmp, err = ioutil.TempDir("", "")
	c.Assert(err, IsNil)
	c.Assert(s.tmp, NotNil)
}

func (s *UtilsTestSuite) TearDownTest(c *C) {
	os.RemoveAll(s.tmp)
}

func (s *UtilsTestSuite) TestFileExists(c *C) {
	c.Assert(fileExists("/i-do-not-exist"), Equals, false)
	fname := path.Join(s.tmp, "foo")
	f, err := os.OpenFile(fname, os.O_CREATE|os.O_RDWR, 0700)
	c.Assert(err, IsNil)
	f.Close()
	c.Assert(fileExists(fname), Equals, true)
}

func (s *UtilsTestSuite) TestIsDirectory(c *C) {
	c.Assert(isDirectory("/i-do-not-exist"), Equals, false)
	dname := path.Join(s.tmp, "bar")
	os.Mkdir(dname, 0700)
	c.Assert(isDirectory(dname), Equals, true)
}

func (s *UtilsTestSuite) TestRunCommand(c *C) {
	err := runCommand([]string{"false"})
	c.Assert(err, NotNil)

	err = runCommand([]string{"no-such-command"})
	c.Assert(err, NotNil)
}

func (s *UtilsTestSuite) TestRunCommandWithStdout(c *C) {
	output, err := runCommandWithStdout([]string{"sh", "-c", "printf foo"})
	c.Assert(err, IsNil)
	c.Assert(output, DeepEquals, []string{"foo"})
}
