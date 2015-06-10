package snappy

import (
	"os/exec"
	"testing"

	. "launchpad.net/gocheck"
)

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&InstallSuite{})

type InstallSuite struct{}

func (s *InstallSuite) TestInstallSnapp(c *C) {
	installCommand := exec.Command("sudo", "snappy", "install", "hello-world")
	installOutput, installErr := installCommand.CombinedOutput()

	c.Assert(installErr, IsNil)
	expected := "" +
		"Installing hello-world\n" +
		"Name          Date       Version Developer \n" +
		".*\n" +
		"hello-world   .* .*  canonical \n" +
		".*\n"
	c.Assert(string(installOutput), Matches, expected)

	echoCommand := exec.Command("hello-world.echo")
	echoOutput, echoErr := echoCommand.CombinedOutput()

	c.Assert(echoErr, IsNil)
	c.Assert(string(echoOutput), Equals, "Hello World!\n")
}
