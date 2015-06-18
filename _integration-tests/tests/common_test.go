package tests

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	. "gopkg.in/check.v1"
)

type CommonSuite struct{}

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

func execCommand(c *C, cmds ...string) []byte {
	cmd := exec.Command(cmds[0], cmds[1:len(cmds)]...)
	output, err := cmd.CombinedOutput()
	c.Assert(err, IsNil, Commentf("Error: %v", output))
	return output
}

func (s *CommonSuite) SetUpSuite(c *C) {
	execCommand(c, "sudo", "systemctl", "stop", "snappy-autopilot.timer")
}

func (s *CommonSuite) SetupTest(c *C) {
	afterReboot := os.Getenv("ADT_REBOOT_MARK")
	if afterReboot == "" {
		fmt.Printf("****** Running %s", c.TestName())
	} else {
		if afterReboot == c.TestName() {
			fmt.Printf("****** Resuming %s after reboot", c.TestName())
		} else {
			c.Skip("")
		}
	}
}
