package tests

import (
	"os/exec"
	"testing"

	. "gopkg.in/check.v1"
)

// Test is used to hook up gocheck into the "go test" runner
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
