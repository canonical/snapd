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

func (s *InstallSuite) installSnap(c *C, packageName string) []byte {
	return s.execCommand(c, "sudo", "snappy", "install", packageName)
}

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
	installOutput := s.installSnap(c, "hello-world")

	expected := "" +
		"Installing hello-world\n" +
		"Name          Date       Version Developer \n" +
		".*\n" +
		"hello-world   .* .*  canonical \n" +
		".*\n"
	c.Assert(string(installOutput), Matches, expected)
}

func (s *InstallSuite) TestCallBinaryFromInstalledSnap(c *C) {
	s.installSnap(c, "hello-world")

	echoOutput := s.execCommand(c, "hello-world.echo")

	c.Assert(string(echoOutput), Equals, "Hello World!\n")
}

func (s *InstallSuite) TestInfoMustPrintInstalledPackageInformation(c *C) {
	s.installSnap(c, "hello-world")

	infoOutput := s.execCommand(c, "sudo", "snappy", "info")

	expected := "^apps:.*<hello-world>"
	c.Assert(string(infoOutput), Matches, expected)
}
