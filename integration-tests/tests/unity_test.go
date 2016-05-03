// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,classiconly

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"os"
	"os/exec"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/wait"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&unitySuite{})

const (
	display       = ":99.0"
	appName       = "ubuntu-clock-app"
	appBinaryName = appName + ".clock"
)

type unitySuite struct {
	common.SnappySuite
}

func (s *unitySuite) TestUnitySnapCanBeStarted(c *check.C) {
	_, err := cli.ExecCommandErr("sudo", "snap", "install", appName)
	c.Assert(err, check.IsNil)
	defer cli.ExecCommand(c, "sudo", "snap", "remove", appName)

	mainCmd := exec.Command("xvfb-run", "--server-args",
		fmt.Sprintf("%s -screen 0 1200x960x24 -ac +extension RANDR", display),
		appBinaryName)

	err = mainCmd.Start()
	c.Assert(err, check.IsNil, check.Commentf("error starting %s, %v", appName, err))

	c.Assert(mainCmd.Process, check.Not(check.IsNil))

	expected := `(?ms).*"qmlscene: clockMainView": \("qmlscene" "com\.ubuntu\.clock"\).*`
	err = wait.ForFunction(c, expected, func() (string, error) {
		probeCmd := exec.Command("xwininfo", "-tree", "-root")
		probeCmd.Env = append(os.Environ(), "DISPLAY="+display)

		// the following error is ignored because of the failure of the first wininfo calls,
		// "xwininfo: error: unable to open display" before xvfb has created the xserver.
		// We won't loose the real error conditions, if there's a problem wait.ForFunction won't
		// find the given pattern and below the output of the command is printed
		outputByte, _ := probeCmd.CombinedOutput()
		output := string(outputByte)
		fmt.Println(output)

		return output, nil
	})
	c.Assert(err, check.IsNil, check.Commentf("error getting window info: %v", err))

	err = mainCmd.Process.Kill()
	c.Assert(err, check.IsNil, check.Commentf("error interrupting %s, %v", appName, err))

	// at this point the Xvfb, ubuntu-clock-app.clock and qmlscene processes are still alive
	//and the snap can't be removed
	cli.ExecCommand(c, "sudo", "killall", "-9", appBinaryName, "Xvfb", "qmlscene")
}
