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

	"github.com/snapcore/snapd/dirs"
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
	Current  string      `json:"current"`
	Sequence []*sideInfo `json:"sequence"`
}

type cmdSnapd struct{}

var errNoSnapd = errors.New("no snapd sequence file found")

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
		return "", err
	}

	var prev string
	for i, si := range seq.Sequence {
		if seq.Current == si.Revision {
			if i == 0 {
				return "", fmt.Errorf("no revision to go back to")
			}
			prev = seq.Sequence[i-1].Revision
			break
		}
	}
	if prev == "" {
		return "", fmt.Errorf("internal error: current not found in sequence: %v %v", seq.Current, seq.Sequence)
	}

	return prev, nil
}

// FIXME: also do error reporting via errtracker
func (c *cmdSnapd) Execute(args []string) error {
	// find previous snapd
	prevRev, err := prevRevision("snapd")
	if err != nil {
		if err == errNoSnapd {
			return nil
		}
		return err
	}
	// stop the socket unit so that we can start snapd on its own
	output, err := exec.Command("systemctl", "stop", "snapd.socket").CombinedOutput()
	if err != nil {
		return osutil.OutputErr(output, err)
	}

	// start previous snapd
	snapdPath := filepath.Join(dirs.SnapMountDir, "snapd", prevRev, "/usr/lib/snapd/snapd")
	cmd := exec.Command(snapdPath)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "SNAPD_REVERT_TO_REV="+prevRev)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return osutil.OutputErr(output, err)
	}

	// at this point our manually started snapd stopped and
	// removed the /run/snap* sockets (this is a feature of
	// golang) - we need to restart snapd.socket to make them
	// available again.
	output, err = exec.Command("systemctl", "restart", "snapd.socket").CombinedOutput()
	if err != nil {
		return osutil.OutputErr(output, err)
	}

	return nil
}
