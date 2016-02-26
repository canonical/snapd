// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package tests

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/config"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/partition"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/report"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/runner"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/wait"
)

var daemonParentCmd *exec.Cmd

func init() {
	c := &check.C{}
	// Workaround for bug https://bugs.launchpad.net/snappy/+bug/1498293
	// TODO remove once the bug is fixed
	// originally added by elopio - 2015-09-30 to the rollback test, moved
	// here by --fgimenez - 2015-10-15
	wait.ForFunction(c, "regular", partition.Mode)

	cli.ExecCommand(c, "sudo", "systemctl", "stop", "snappy-autopilot.timer")
	cli.ExecCommand(c, "sudo", "systemctl", "disable", "snappy-autopilot.timer")

	cfg, err := config.ReadConfig(config.DefaultFileName)
	c.Assert(err, check.IsNil, check.Commentf("Error reading config: %v", err))

	if cfg.FromBranch {
		setUpSnapdFromBranch(c)
	}
}

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) {
	output := io.MultiWriter(
		os.Stdout,
		report.NewSubunitV2ParserReporter(&report.FileReporter{}))
	runner.TestingT(t, output)

	if daemonParentCmd != nil && daemonParentCmd.Process != nil {
		if err := stopDaemonProcess("snapd"); err != nil {
			fmt.Printf("Error stopping daemon: %v\n", err)
		}
	}
}

func setUpSnapdFromBranch(c *check.C) {
	if daemonParentCmd == nil {
		trustedKey, err := filepath.Abs("integration-tests/data/trusted.acckey")
		c.Assert(err, check.IsNil)

		cli.ExecCommand(c, "sudo", "systemctl", "stop",
			"ubuntu-snappy.snapd.service", "ubuntu-snappy.snapd.socket")

		// FIXME: for now pass a test-only trusted key through an env var
		daemonParentCmd = exec.Command("sudo", "env", "PATH="+os.Getenv("PATH"),
			"SNAPPY_TRUSTED_ACCOUNT_KEY="+trustedKey,
			"/lib/systemd/systemd-activate", "--setenv=SNAPPY_TRUSTED_ACCOUNT_KEY",
			"-l", "/run/snapd.socket", "snapd")

		err = daemonParentCmd.Start()
		c.Assert(err, check.IsNil)

		wait.ForCommand(c, `^$`, "sudo", "chmod", "0666", "/run/snapd.socket")
	}
}

func stopDaemonProcess(name string) error {
	pid, err := getPidFromName(name)
	if err != nil {
		return err
	}
	// send SIGINT to the process
	_, err = cli.ExecCommandErr("sudo", "kill", "-2", strconv.Itoa(pid))
	return err
}

func getPidFromName(name string) (int, error) {
	re := regexp.MustCompile(`.*` + name + `\[(\d+)\]:.*`)

	output, err := cli.ExecCommandErr("sudo", "journalctl", "_COMM="+name, "-q", "--lines=1")
	if err != nil {
		return 0, err
	}

	matches := re.FindStringSubmatch(output)
	if len(matches) < 2 {
		return 0, fmt.Errorf("Error extracting pid for %s from %s", name, output)
	}
	return strconv.Atoi(matches[1])
}
