package tests

import (
	"os/exec"
	"testing"

	. "gopkg.in/check.v1"
)

type CommonSuite struct{}

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

func (s *CommonSuite) execCommand(c *C, cmds ...string) []byte {
	cmd := exec.Command(cmds[0], cmds[1:len(cmds)]...)
	output, err := cmd.CombinedOutput()
	c.Assert(err, IsNil, Commentf("Error: %v", output))
	return output
}

func (s *CommonSuite) SetUpSuite(c *C) {
	s.execCommand(c, "sudo", "systemctl", "stop", "snappy-autopilot.timer")
}
