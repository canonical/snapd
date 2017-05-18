// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

var (
	execCommand = exec.Command
)

type repair struct {
	ra *asserts.Repair
}

func (r *repair) dir() string {
	return filepath.Join(dirs.SnapRepairDir, r.ra.RepairID())
}

func (r *repair) doneStamp() string {
	return filepath.Join(r.dir(), "done")
}

func (r *repair) script() string {
	return filepath.Join(r.dir(), "script")
}

func (r *repair) log() string {
	return filepath.Join(r.dir(), fmt.Sprintf("%s.output", r.ra.RepairID()))
}

func (r *repair) wasRun() bool {
	return osutil.FileExists(r.doneStamp())
}

func (r *repair) run() error {
	// FIXME: deal with disk-full conditions in some way?
	if err := os.MkdirAll(r.dir(), 0755); err != nil {
		return err
	}

	logger.Noticef("starting repair %s", r.ra.RepairID())
	if err := ioutil.WriteFile(r.script(), r.ra.Body(), 0755); err != nil {
		return err
	}

	// create the status pipe for the script
	stR, stW, err := os.Pipe()
	if err != nil {
		return err
	}
	defer stR.Close()

	// run the script
	var buf bytes.Buffer
	cmd := execCommand(r.script())
	cmd.ExtraFiles = []*os.File{stW}
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "SNAP_REPAIR_STATUS_FD=3")
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err = cmd.Start()
	stW.Close()
	if err != nil {
		return err
	}

	// check output
	err = cmd.Wait()
	// FIXME: what do we do about the log? append? overwrite?
	if err := ioutil.WriteFile(r.log(), buf.Bytes(), 0644); err != nil {
		logger.Noticef("cannot write log: %s", err)
	}
	if err != nil {
		return fmt.Errorf("cannot run repair %s: %s", r.ra.RepairID(), err)
	}

	// check status
	status, err := ioutil.ReadAll(stR)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(status)) == "done-permanently" {
		if err := ioutil.WriteFile(r.doneStamp(), nil, 0644); err != nil {
			logger.Noticef("cannot write stamp: %s", err)
		}
	}

	logger.Noticef("finished repair %s, logs in ", r.ra.RepairID(), r.log())
	return nil
}

type byRepairID []*asserts.Repair

func (a byRepairID) Len() int {
	return len(a)
}
func (a byRepairID) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

// FIXME: move into the assertion itself?
func splitID(id string) (string, int) {
	l := strings.SplitN(id, "-", 2)
	prefix := l[0]
	seq, err := strconv.Atoi(l[1])
	if err != nil {
		panic(err)
	}
	return prefix, seq
}

func (a byRepairID) Less(i, j int) bool {
	aPrefix, aSeq := splitID(a[i].RepairID())
	bPrefix, bSeq := splitID(a[j].RepairID())
	if aPrefix == bPrefix {
		return aSeq < bSeq
	}
	return aPrefix < bPrefix
}

// FIXME: bypass the assertion DB entirely and collect all repair
//        bits in /var/lib/snapd/repair/
// FIXME: create a copy of the critical assertion code to protect
//        against catastrophic bugs in the assertions implementation?
var findRepairAssertions = func() ([]*asserts.Repair, error) {
	db, err := sysdb.Open()
	if err != nil {
		return nil, fmt.Errorf("cannot open system assertion database: %s", err)
	}

	// FIXME: add appropriate headers for filtering etc
	var headers map[string]string
	assertType := asserts.Type("repair")

	assertions, err := db.FindMany(assertType, headers)
	if err != nil {
		return nil, err
	}

	repairAssertions := make([]*asserts.Repair, len(assertions))
	for i, a := range assertions {
		repairAssertions[i] = a.(*asserts.Repair)
	}
	sort.Sort(byRepairID(repairAssertions))

	return repairAssertions, nil
}

func runRepair() error {
	if release.OnClassic {
		return fmt.Errorf("cannot run repairs on a classic system")
	}

	assertions, err := findRepairAssertions()
	if err == asserts.ErrNotFound {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot find repair assertion: %s", err)
	}

	// run repair assertions that not already ran
	for _, a := range assertions {
		// the asserts package guarantees that this cast will work
		r := repair{ra: a}
		if r.wasRun() {
			continue
		}

		if err := r.run(); err != nil {
			fmt.Fprintf(Stderr, "error running %s: %s", r.ra.RepairID(), err)
		}
	}

	return nil
}
