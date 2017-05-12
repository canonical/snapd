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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

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

func (r *repair) ranStamp() string {
	return filepath.Join(r.dir(), "ran")
}

func (r *repair) script() string {
	return filepath.Join(r.dir(), "script")
}

func (r *repair) log() string {
	return filepath.Join(r.dir(), fmt.Sprintf("%s.output", r.ra.RepairID()))
}

func (r *repair) wasRun() bool {
	return osutil.FileExists(r.ranStamp())
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
	output, err := execCommand(r.script()).CombinedOutput()
	if err := ioutil.WriteFile(r.log(), output, 0644); err != nil {
		logger.Noticef("cannot write log: %s", err)
	}
	if err := ioutil.WriteFile(r.ranStamp(), nil, 0644); err != nil {
		logger.Noticef("cannot write stamp: %s", err)
	}
	logger.Noticef("finished repair %s, logs in ", r.ra.RepairID(), r.log())

	return err
}

// FIXME: bypass the assertion DB entirely and collect all repair
//        bits in /var/lib/snapd/repair/ ?
// FIXME: create a copy of the critical assertion code to protect
//        against catastrophic bugs in the assertions implementation?
var findRepairAssertions = func() ([]asserts.Assertion, error) {
	db, err := sysdb.Open()
	if err != nil {
		fmt.Errorf("cannot open system assertion database: %s", err)
	}

	// FIXME: add appropriate headers for filtering etc
	var headers map[string]string
	assertType := asserts.Type("repair")

	return db.FindMany(assertType, headers)
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
		fmt.Errorf("cannot find repair assertion: %s", err)
	}

	// run repair assertions that not already ran
	for _, a := range assertions {
		// the asserts package guarantees that this cast will work
		r := repair{ra: a.(*asserts.Repair)}
		if r.wasRun() {
			continue
		}

		if err := r.run(); err != nil {
			fmt.Fprintf(Stderr, "error running %s: %s", r.ra.RepairID(), err)
		}
	}

	return nil
}
