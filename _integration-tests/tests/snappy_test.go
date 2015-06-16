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

func (s *InstallSuite) execCommand(c *C, cmds ...string) []byte {
	cmd := exec.Command(cmds[0], cmds[1:len(cmds)]...)
	output, err := cmd.CombinedOutput()
	c.Assert(err, IsNil, Commentf("Error: %v", output))
	return output
}

func (s *InstallSuite) SetUpSuite(c *C) {
	s.execCommand(c, "sudo", "systemctl", "stop", "snappy-autopilot.timer")
}

func (s *InstallSuite) TearDownTest(c *C) {
	s.execCommand(c, "sudo", "snappy", "remove", "hello-world")
}

func (s *InstallSuite) TestInstallSnapMustPrintPackageInformation(c *C) {
	installOutput := s.execCommand(c, "sudo", "snappy", "install", "hello-world")

	expected := "" +
		"Installing hello-world\n" +
		"Name          Date       Version Developer \n" +
		".*\n" +
		"hello-world   .* .*  canonical \n" +
		".*\n"
	// Check the output of the install command.
	c.Assert(string(installOutput), Matches, expected)
}

func (s *InstallSuite) TestCallBinaryFromInstalledSnap(c *C) {
	s.execCommand(c, "sudo", "snappy", "install", "hello-world")

	echoOutput := s.execCommand(c, "hello-world.echo")

	// Assert the output of the hello-world.echo command.
	c.Assert(string(echoOutput), Equals, "Hello World!\n")
}

func (s *InstallSuite) TestInfoMustPrintInstalledPackageInformation(c *C) {
	infoOutput := s.execCommand(c, "sudo", "snappy", "info")

	expected := "^apps:.*<hello-world>"
	// Check the output of the info command.
	c.Assert(string(infoOutput), Matches, expected)
}
