// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

// The `snap-recovery-chooser` acts as a proxy between the chooser UI process
// and the actual snapd daemon.
//
// It obtains the list of seed systems and their actions from the snapd API and
// passed that directly to the standard input of the UI process. The UI process
// is expected to present the list of options to the user and print out a JSON
// object with the choice to its standard output.
//
// The JSON object carrying the list of systems is the client.ChooserSystems
// structure. The response is defined as follows:
//
//	{
//	    "label": "<system-label",
//	    "action": {} // client.SystemAction object
//	}
//
// No action is forwarded to snapd if the chooser UI exits with an error code or
// the response structure is invalid.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/syslog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
)

var (
	// default marker file location
	defaultMarkerFile = "/run/snapd-recovery-chooser-triggered"

	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr

	chooserTool = consoleConfWrapperUITool
)

func consoleConfWrapperUITool() (*exec.Cmd, error) {
	// console conf may either be provided as a snap or be part of
	// the boot base
	candidateTools := []string{
		filepath.Join(dirs.GlobalRootDir, "usr/bin/console-conf"),
		filepath.Join(dirs.SnapBinariesDir, "console-conf"),
	}

	var tool string

	for _, maybeTool := range candidateTools {
		mylog.Check2(os.Stat(maybeTool))
	}
	if tool == "" {
		return nil, fmt.Errorf("chooser UI tools %q do not exist", candidateTools)
	}

	return exec.Command(tool, "--recovery-chooser-mode"), nil
}

// ChooserSystems carries the list of available recovery systems
type ChooserSystems struct {
	Systems []client.System `json:"systems,omitempty"`
}

func outputForUI(out io.Writer, sys *ChooserSystems) error {
	enc := json.NewEncoder(out)
	mylog.Check(enc.Encode(sys))

	return nil
}

// Response is sent by the UI tool and contains the choice made by the user
type Response struct {
	Label  string              `json:"label"`
	Action client.SystemAction `json:"action"`
}

func runUI(cmd *exec.Cmd, sys *ChooserSystems) (rsp *Response, err error) {
	var asBytes bytes.Buffer
	mylog.Check(outputForUI(&asBytes, sys))

	logger.Noticef("spawning UI")
	// the UI uses the same tty as current process
	cmd.Stdin = &asBytes
	// reuse stderr
	cmd.Stderr = os.Stderr
	// the chooser may be invoked via console-conf service which uses
	// KillMethod=process, so make sure the UI process dies when we die
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
	}

	out := mylog.Check2(cmd.Output())

	logger.Noticef("UI completed")

	var resp Response
	dec := json.NewDecoder(bytes.NewBuffer(out))
	mylog.Check(dec.Decode(&resp))

	return &resp, nil
}

func cleanupTriggerMarker() error {
	if mylog.Check(os.Remove(defaultMarkerFile)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func chooser(cli *client.Client) (reboot bool, err error) {
	mylog.Check2(os.Stat(defaultMarkerFile))

	// consume the trigger file
	defer cleanupTriggerMarker()

	systems := mylog.Check2(cli.ListSystems())

	systemsForUI := &ChooserSystems{
		Systems: systems,
	}

	uiTool := mylog.Check2(chooserTool())

	response := mylog.Check2(runUI(uiTool, systemsForUI))

	logger.Noticef("got response: %+v", response)
	mylog.Check(cli.DoSystemAction(response.Label, &response.Action))

	if maintErr, ok := cli.Maintenance().(*client.Error); ok && maintErr.Kind == client.ErrorKindSystemRestart {
		reboot = true
	}
	return reboot, nil
}

var syslogNew = func(p syslog.Priority, tag string) (io.Writer, error) { return syslog.New(p, tag) }

func loggerWithSyslogMaybe() error {
	maybeSyslog := func() error {
		if os.Getenv("TERM") == "" {
			// set up the syslog logger only when we're running on a
			// terminal
			return fmt.Errorf("not on terminal, syslog not needed")
		}
		syslogWriter := mylog.Check2(syslogNew(syslog.LOG_INFO|syslog.LOG_DAEMON, "snap-recovery-chooser"))

		l := mylog.Check2(logger.New(syslogWriter, logger.DefaultFlags, nil))

		logger.SetLogger(l)
		return nil
	}
	mylog.Check(maybeSyslog())
	// try simple setup

	return nil
}

func main() {
	mylog.Check(loggerWithSyslogMaybe())

	reboot := mylog.Check2(chooser(client.New(nil)))

	if reboot {
		fmt.Fprintf(Stderr, "The system is rebooting...\n")
	}
}
