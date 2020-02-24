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
	"syscall"

	"github.com/snapcore/snapd/logger"
)

var (
	// default marker file location
	defaultMarkerFile = "/run/snapd-recovery-chooser-triggered"

	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr

	// TODO:UC20: hardcoded to use the demo UI tool
	toolPath = demoUITool

	executeAction = executeMenuAction
)

func outputUI(out io.Writer, menu *Menu) error {
	enc := json.NewEncoder(out)
	if err := enc.Encode(menu); err != nil {
		return fmt.Errorf("cannot serialize menu structure: %v", err)
	}
	return nil
}

// Response is sent by the UI tool and contains the choice made by the user
type Response struct {
	// ID of menu entry selected
	ID string
}

func runUI(uiTool string, menu *Menu) (action string, err error) {
	var menuAsBytes bytes.Buffer
	if err := outputUI(&menuAsBytes, menu); err != nil {
		return "", err
	}

	logger.Noticef("spawning UI")
	// the UI uses the same tty as current process
	cmd := exec.Command(uiTool)
	cmd.Stdin = &menuAsBytes
	// reuse stderr
	cmd.Stderr = os.Stderr
	// the chooser may be invoked via console-conf service which uses
	// KillMethod=process, so make sure the UI process dies when we die
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
	}

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("cannot obtain output of the UI process: %v", err)
	}

	logger.Noticef("UI completed")

	var resp Response
	dec := json.NewDecoder(bytes.NewBuffer(out))
	if err := dec.Decode(&resp); err != nil {
		return "", fmt.Errorf("cannot decode response: %v", err)
	}
	return resp.ID, nil
}

func cleanupTriggerMarker() error {
	err := os.Remove(defaultMarkerFile)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func chooser() error {
	// TODO:UC20: build actual UI by querying snapd for available actions
	menu := demoUI()

	// for local testing
	if os.Getenv("USE_STDOUT") != "" {
		if err := outputUI(Stdout, menu); err != nil {
			return fmt.Errorf("cannot serialize UI to stdout: %v", err)
		}
		return nil
	}

	uiTool, err := toolPath()
	if err != nil {
		return fmt.Errorf("cannot obtain chooser UI tool: %v", err)
	}

	if _, err := os.Stat(uiTool); err != nil {
		if os.IsNotExist(err) {
			logger.Noticef("chooser UI tool %q not found in the system", uiTool)
			// UI tool not found, cleanup the marker, nothing to do
			return cleanupTriggerMarker()
		}

		return fmt.Errorf("cannot stat UI tool binary: %v", err)
	}

	action, err := runUI(uiTool, menu)
	if err != nil {
		return fmt.Errorf("UI process failed: %v", err)

	}

	logger.Noticef("got response: %+v", action)

	// TODO:UC20 trigger corresponding action in snapd
	if err := executeAction(action); err != nil {
		return fmt.Errorf("cannot execute action %q: %v", action, err)
	}

	return nil
}

func main() {
	if err := logger.SimpleSetup(); err != nil {
		fmt.Fprintf(Stderr, "cannot initialize logger: %v\n", err)
		os.Exit(1)
	}
	if err := chooser(); err != nil {
		fmt.Fprintf(Stderr, "%v\n", err)
		os.Exit(1)
	}
}
