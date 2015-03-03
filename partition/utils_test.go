package partition

import (
	. "launchpad.net/gocheck"
)

type UtilsTestSuite struct {
}

var _ = Suite(&UtilsTestSuite{})

func (s *UtilsTestSuite) SetUpTest(c *C) {
}

func (s *UtilsTestSuite) TestRunCommand(c *C) {
	err := runCommandImpl("false")
	c.Assert(err, NotNil)

	err = runCommandImpl("no-such-command")
	c.Assert(err, NotNil)
}

func (s *UtilsTestSuite) TestRunCommandWithStdout(c *C) {
	runCommandWithStdout = runCommandWithStdoutImpl
	output, err := runCommandWithStdout("sh", "-c", "printf 'foo\nbar'")
	c.Assert(err, IsNil)
	c.Assert(output, DeepEquals, "foo\nbar")
}
