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

// consoleConfWrapperUITool returns a hardcoded path to the console conf wrapper
func consoleConfWrapperUITool() (*exec.Cmd, error) {
	// console conf may either be provided as a snap or be part of
	// the boot base
	candidateTools := []string{
		filepath.Join(dirs.GlobalRootDir, "usr/bin/console-conf"),
		filepath.Join(dirs.SnapBinariesDir, "console-conf"),
	}

	var tool string

	for _, maybeTool := range candidateTools {
		if _, err := os.Stat(maybeTool); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("cannot stat UI tool binary: %v", err)
		} else {
			tool = maybeTool
			break
		}
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
	if err := enc.Encode(sys); err != nil {
		return fmt.Errorf("cannot serialize chooser options: %v", err)
	}
	return nil
}

// Response is sent by the UI tool and contains the choice made by the user
type Response struct {
	Label  string              `json:"label"`
	Action client.SystemAction `json:"action"`
}

func runUI(cmd *exec.Cmd, sys *ChooserSystems) (rsp *Response, err error) {
	var asBytes bytes.Buffer
	if err := outputForUI(&asBytes, sys); err != nil {
		return nil, err
	}

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

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("cannot collect output of the UI process: %v", err)
	}

	logger.Noticef("UI completed")

	var resp Response
	dec := json.NewDecoder(bytes.NewBuffer(out))
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("cannot decode response: %v", err)
	}
	return &resp, nil
}

func cleanupTriggerMarker() error {
	if err := os.Remove(defaultMarkerFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func chooser(cli *client.Client) (reboot bool, err error) {
	if _, err := os.Stat(defaultMarkerFile); err != nil {
		if os.IsNotExist(err) {
			return false, fmt.Errorf("cannot run chooser without the marker file")
		} else {
			return false, fmt.Errorf("cannot check the marker file: %v", err)
		}
	}
	// consume the trigger file
	defer cleanupTriggerMarker()

	systems, err := cli.ListSystems()
	if err != nil {
		return false, err
	}

	systemsForUI := &ChooserSystems{
		Systems: systems,
	}

	uiTool, err := chooserTool()
	if err != nil {
		return false, fmt.Errorf("cannot locate the chooser UI tool: %v", err)
	}

	response, err := runUI(uiTool, systemsForUI)
	if err != nil {
		return false, fmt.Errorf("UI process failed: %v", err)
	}

	logger.Noticef("got response: %+v", response)

	if err := cli.DoSystemAction(response.Label, &response.Action); err != nil {
		return false, fmt.Errorf("cannot request system action: %v", err)
	}
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
		syslogWriter, err := syslogNew(syslog.LOG_INFO|syslog.LOG_DAEMON, "snap-recovery-chooser")
		if err != nil {
			return err
		}
		l, err := logger.New(syslogWriter, logger.DefaultFlags)
		if err != nil {
			return err
		}
		logger.SetLogger(l)
		return nil
	}

	if err := maybeSyslog(); err != nil {
		// try simple setup
		return logger.SimpleSetup()
	}
	return nil
}

func main() {
	if err := loggerWithSyslogMaybe(); err != nil {
		fmt.Fprintf(Stderr, "cannot initialize logger: %v\n", err)
		os.Exit(1)
	}

	reboot, err := chooser(client.New(nil))
	if err != nil {
		logger.Noticef("cannot run recovery chooser: %v", err)
		fmt.Fprintf(Stderr, "%v\n", err)
		os.Exit(1)
	}
	if reboot {
		fmt.Fprintf(Stderr, "The system is rebooting...\n")
	}
}
