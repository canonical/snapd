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
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
)

func waitWhileInhibited(snapName string) error {
	hint, err := runinhibit.IsLocked(snapName)
	if err != nil {
		return err
	}
	if hint == runinhibit.HintNotInhibited {
		return nil
	}

	// wait for HintInhibitedForRefresh set by gate-auto-refresh hook handler
	// when it has finished; the hook starts with HintInhibitedGateRefresh lock
	// and then either unlocks it or changes to HintInhibitedForRefresh (see
	// gateAutoRefreshHookHandler in hooks.go).
	// waitInhibitUnlock will return also on HintNotInhibited.
	notInhibited, err := waitInhibitUnlock(snapName, runinhibit.HintInhibitedForRefresh, nil)
	if err != nil {
		return err
	}
	if notInhibited {
		return nil
	}

	if isGraphicalSession() && hasZenityExecutable() {
		return zenityFlow(snapName, hint)
	}
	// terminal and headless
	return textFlow(snapName, hint)
}

func inhibitMessage(snapName string, hint runinhibit.Hint) string {
	switch hint {
	case runinhibit.HintInhibitedForRefresh:
		return fmt.Sprintf(i18n.G("snap package %q is being refreshed, please wait"), snapName)
	default:
		return fmt.Sprintf(i18n.G("snap package cannot be used now: %s"), string(hint))
	}
}

var isGraphicalSession = func() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

var hasZenityExecutable = func() bool {
	return osutil.ExecutableExists("zenity")
}

func zenityFlow(snapName string, hint runinhibit.Hint) error {
	zenityTitle := i18n.G("Snap package waiting for update")

	// Run zenity with a progress bar.
	// TODO: while we are waiting ask snapd for progress updates and send those
	// to zenity via stdin.
	zenityDied := make(chan error, 1)

	// TODO: use a dbus API to allow integration with native desktop environment.
	cmd := exec.Command(
		"zenity",
		// [generic options]
		"--title="+zenityTitle,
		// [progress options]
		"--progress",
		"--text="+inhibitMessage(snapName, hint),
		"--pulsate",
	)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Make sure that zenity is eventually terminated.
	defer cmd.Process.Signal(os.Interrupt)
	// Wait for zenity to terminate and store the error code.
	// The way we invoke zenity --progress makes it wait forever.
	// so it will typically be an external operation.
	go func() {
		zenityErr := cmd.Wait()
		if zenityErr != nil {
			zenityErr = fmt.Errorf("zenity error: %s\n", zenityErr)
		}
		zenityDied <- zenityErr
	}()

	if _, err := waitInhibitUnlock(snapName, runinhibit.HintNotInhibited, zenityDied); err != nil {
		return err
	}

	return nil
}

func textFlow(snapName string, hint runinhibit.Hint) error {
	fmt.Fprintf(Stdout, "%s\n", inhibitMessage(snapName, hint))
	pb := progress.MakeProgressBar()
	pb.Spin(i18n.G("please wait..."))
	_, err := waitInhibitUnlock(snapName, runinhibit.HintNotInhibited, nil)
	pb.Finished()
	return err
}

var isLocked = runinhibit.IsLocked

// waitInhibitUnlock waits until the runinhibit lock hint has a specific waitFor value
// or isn't inhibited anymore. In addition the optional errCh channel is monitored
// for an error - any error is printed to stderr and immediately returns false (the error
// value isn't returned).
var waitInhibitUnlock = func(snapName string, waitFor runinhibit.Hint, errCh <-chan error) (notInhibited bool, err error) {
	// Every 0.5s check if the inhibition file is still present.
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-errCh:
			if err != nil {
				fmt.Fprintf(Stderr, "%s", err)
			}
			return false, nil
		case <-ticker.C:
			// Half a second has elapsed, let's check again.
			hint, err := isLocked(snapName)
			if err != nil {
				return false, err
			}
			if hint == runinhibit.HintNotInhibited {
				return true, nil
			}
			if hint == waitFor {
				return false, nil
			}
		}
	}
}
