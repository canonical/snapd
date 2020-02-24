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
	// ID of menu entry selected
	ID string
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
	if os.Getenv("USE_STDOUT") != "" {
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

	// TODO:UC20 trigger corresponding action in snapd

	return nil
}

func main() {
	if err := logger.SimpleSetup(); err != nil {
		fmt.Fprintf(Stderr, "cannot initialize logger: %v\n", err)
		os.Exit(1)
	}

	if err := chooser(client.New(nil)); err != nil {
		fmt.Fprintf(Stderr, "%v\n", err)
		os.Exit(1)
	}
}
