package snappy

import (
	"os/exec"
	"testing"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&InstallSuite{})

type InstallSuite struct{}

func (s *InstallSuite) TearDownTest(c *C) {
	uninstallCommand := exec.Command("sudo", "snappy", "hello-world")
	_, uninstallErr := installCommand.CombinedOutput()
	c.Assert(uninstallErr, IsNil)
}

func (s *InstallSuite) TestInstallSnapp(c *C) {
	installCommand := exec.Command("sudo", "snappy", "install", "hello-world")
	_, installErr := installCommand.CombinedOutput()

	c.Assert(installErr, IsNil)

	echoCommand := exec.Command("hello-world.echo")
	echoOutput, echoErr := echoCommand.CombinedOutput()

	c.Assert(echoErr, IsNil)
	c.Assert(string(echoOutput), Equals, "Hello World!\n")
}
