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
// {
//     "label": "<system-label",
//     "action": {} // client.SystemAction object
// }
//
// No action is forwarded to snapd if the chooser UI exits with an error code or
// the response structure is invalid.
//
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	tool := filepath.Join(dirs.GlobalRootDir, "usr/bin/console-conf")

	if _, err := os.Stat(tool); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("chooser UI tool %q does not exist", tool)
		}
		return nil, fmt.Errorf("cannot stat UI tool binary: %v", err)
	}
	// TODO:UC20 update once upstream console-conf has the right command
	// line switches
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
	err := os.Remove(defaultMarkerFile)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func chooser(cli *client.Client) error {
	snappyTesting := os.Getenv("SNAPPY_TESTING") != ""

	if _, err := os.Stat(defaultMarkerFile); err != nil && !snappyTesting {
		if os.IsNotExist(err) {
			return fmt.Errorf("cannot run chooser without the marker file")
		} else {
			return fmt.Errorf("cannot check the marker file: %v", err)
		}
	}
	// consume the trigger file
	defer cleanupTriggerMarker()

	systems, err := cli.ListSystems()
	if err != nil {
		return err
	}

	systemsForUI := &ChooserSystems{
		Systems: systems,
	}

	// for local testing
	if snappyTesting {
		if err := outputForUI(Stdout, systemsForUI); err != nil {
			return fmt.Errorf("cannot serialize UI to stdout: %v", err)
		}
		return nil
	}

	uiTool, err := chooserTool()
	if err != nil {
		return fmt.Errorf("cannot locate the chooser UI tool: %v", err)
	}

	response, err := runUI(uiTool, systemsForUI)
	if err != nil {
		return fmt.Errorf("UI process failed: %v", err)
	}

	logger.Noticef("got response: %+v", response)

	if err := cli.DoSystemAction(response.Label, &response.Action); err != nil {
		return fmt.Errorf("cannot request system action: %v", err)
	}
	return nil
}

func main() {
	if err := logger.SimpleSetup(); err != nil {
		fmt.Fprintf(Stderr, "cannot initialize logger: %v\n", err)
		os.Exit(1)
	}

	if err := chooser(client.New(nil)); err != nil {
		logger.Noticef("cannot run recovery chooser: %v", err)
		fmt.Fprintf(Stderr, "%v\n", err)
		os.Exit(1)
	}
}
