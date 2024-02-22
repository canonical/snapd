// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

func init() {
	const (
		short = "Run snapd failure handling"
		long  = ""
	)

	if _, err := parser.AddCommand("snapd", short, long, &cmdSnapd{}); err != nil {
		panic(err)
	}

}

// We do not import anything from snapd here for safety reasons so make a
// copy of the relevant struct data we care about.
type sideInfo struct {
	Revision string `json:"revision"`
}

type snapSeq struct {
	Current  string     `json:"current"`
	Sequence []sideInfo `json:"sequence"`
}

type cmdSnapd struct{}

var errNoSnapd = errors.New("no snapd sequence file found")
var errNoPrevious = errors.New("no revision to go back to")

func prevRevision(snapName string) (string, error) {
	seqFile := filepath.Join(dirs.SnapSeqDir, snapName+".json")
	content, err := ioutil.ReadFile(seqFile)
	if os.IsNotExist(err) {
		return "", errNoSnapd
	}
	if err != nil {
		return "", err
	}

	var seq snapSeq
	if err := json.Unmarshal(content, &seq); err != nil {
		return "", fmt.Errorf("cannot parse %q sequence file: %v", filepath.Base(seqFile), err)
	}

	var prev string
	for i, si := range seq.Sequence {
		if seq.Current == si.Revision {
			if i == 0 {
				return "", errNoPrevious
			}
			prev = seq.Sequence[i-1].Revision
			break
		}
	}
	if prev == "" {
		return "", fmt.Errorf("internal error: current %v not found in sequence: %+v", seq.Current, seq.Sequence)
	}

	return prev, nil
}

func runCmd(prog string, args []string, env []string) *exec.Cmd {
	cmd := exec.Command(prog, args...)
	cmd.Env = os.Environ()
	for _, envVar := range env {
		cmd.Env = append(cmd.Env, envVar)
	}

	cmd.Stdout = Stdout
	cmd.Stderr = Stderr

	return cmd
}

var (
	sampleForActiveInterval = 5 * time.Second
	restartSnapdCoolOffWait = 12500 * time.Millisecond
)

func (c *cmdSnapd) Execute(args []string) error {
	var snapdPath string
	// find previous the snapd snap
	prevRev, err := prevRevision("snapd")
	switch err {
	case errNoSnapd:
		// the snapd snap is not installed
		return nil
	case errNoPrevious:
		// this is the first revision of snapd to be installed on the
		// system, either a remodel or a plain snapd installation, call
		// the snapd from the core snap
		snapdPath = filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd/snapd")
		if !osutil.FileExists(snapdPath) {
			// it is possible that the core snap is not installed at
			// all, in which case we should try the snapd snap
			snapdPath = filepath.Join(dirs.SnapMountDir, "snapd", "current", "/usr/lib/snapd/snapd")
		}
		prevRev = "0"
	case nil:
		// the snapd snap was installed before, use the previous revision
		snapdPath = filepath.Join(dirs.SnapMountDir, "snapd", prevRev, "/usr/lib/snapd/snapd")
	default:
		return err
	}
	logger.Noticef("stopping snapd socket")
	// stop the socket unit so that we can start snapd on its own
	stdout, stderr, err := osutil.RunSplitOutput("systemctl", "stop", "snapd.socket")
	if err != nil {
		return osutil.OutputErrCombine(stdout, stderr, err)
	}

	logger.Noticef("restoring invoking snapd from: %v", snapdPath)
	if prevRev != "0" {
		// if prevRev was "0" it means we did *not* find a
		// previous revision and we would obey the current
		// symlink. So we overwrite the symlink only if
		// prevRev != "0".
		currentSymlink := filepath.Join(dirs.SnapMountDir, "snapd", "current")
		if err := osutil.AtomicSymlink(prevRev, currentSymlink); err != nil {
			return fmt.Errorf("cannot create symlink %s: %v", currentSymlink, err)
		}
	}
	// start previous snapd
	cmd := runCmd(snapdPath, nil, []string{"SNAPD_REVERT_TO_REV=" + prevRev, "SNAPD_DEBUG=1"})
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("snapd failed: %v", err)
	}

	isFailedCmd := runCmd("systemctl", []string{"is-failed", "snapd.socket", "snapd.service"}, nil)
	if err := isFailedCmd.Run(); err != nil {
		// the ephemeral snapd we invoked seems to have fixed
		// snapd.service and snapd.socket, check whether they get
		// reported as active for 5 * 5s
		for i := 0; i < 5; i++ {
			if i != 0 {
				time.Sleep(sampleForActiveInterval)
			}
			isActiveCmd := runCmd("systemctl", []string{"is-active", "snapd.socket", "snapd.service"}, nil)
			err := isActiveCmd.Run()
			if err == nil && osutil.FileExists(dirs.SnapdSocket) && osutil.FileExists(dirs.SnapSocket) {
				logger.Noticef("snapd is active again, sockets are available, nothing more to do")
				return nil
			}
		}
	}

	logger.Noticef("restarting snapd socket")
	// we need to reset the failure state to be able to restart again
	resetCmd := runCmd("systemctl", []string{"reset-failed", "snapd.socket", "snapd.service"}, nil)
	if err = resetCmd.Run(); err != nil {
		// don't die if we fail to reset the failed state of snapd.socket, as
		// the restart itself could still work
		logger.Noticef("failed to reset-failed snapd.socket: %v", err)
	}
	// at this point our manually started snapd stopped and
	// should have removed the /run/snap* sockets (this is a feature of
	// golang) - we need to restart snapd.socket to make them
	// available again.

	// be extra robust and if the socket file still somehow exists delete it
	// before restarting, otherwise the restart command will fail because the
	// systemd can't create the file
	// always remove to avoid TOCTOU issues but don't complain about ENOENT
	for _, fn := range []string{dirs.SnapdSocket, dirs.SnapSocket} {
		err = os.Remove(fn)
		if err != nil && !os.IsNotExist(err) {
			logger.Noticef("snapd socket %s still exists before restarting socket service, but unable to remove: %v", fn, err)
		}
	}

	restartCmd := runCmd("systemctl", []string{"restart", "snapd.socket"}, nil)
	if err := restartCmd.Run(); err != nil {
		logger.Noticef("failed to restart snapd.socket: %v", err)
		// fallback to try snapd itself
		// wait more than DefaultStartLimitIntervalSec
		//
		// TODO: consider parsing
		// systemctl show snapd -p StartLimitIntervalUSec
		// might need system-analyze timespan which is relatively new
		// for the general case
		time.Sleep(restartSnapdCoolOffWait)
		logger.Noticef("fallback, restarting snapd itself")
		restartCmd := runCmd("systemctl", []string{"restart", "snapd.service"}, nil)
		if err := restartCmd.Run(); err != nil {
			logger.Noticef("failed to restart snapd: %v", err)
		}
	}

	return nil
}
