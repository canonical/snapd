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

package main_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
)

func makeMockRepairState(c *C) {
	// the canonical script dir content
	basedir := filepath.Join(dirs.SnapRepairRunDir, "canonical/1")
	err := os.MkdirAll(basedir, 0700)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r3.retry"), []byte("repair: canonical-1\nsummary: repair one\noutput:\nretry output"), 0600)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r3.script"), []byte("#!/bin/sh\necho retry output"), 0700)
	c.Assert(err, IsNil)

	// my-brand
	basedir = filepath.Join(dirs.SnapRepairRunDir, "my-brand/1")
	err = os.MkdirAll(basedir, 0700)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r1.done"), []byte("repair: my-brand-1\nsummary: my-brand repair one\noutput:\ndone output"), 0600)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r1.script"), []byte("#!/bin/sh\necho done output"), 0700)
	c.Assert(err, IsNil)

	basedir = filepath.Join(dirs.SnapRepairRunDir, "my-brand/2")
	err = os.MkdirAll(basedir, 0700)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r2.skip"), []byte("repair: my-brand-2\nsummary: my-brand repair two\noutput:\nskip output"), 0600)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r2.script"), []byte("#!/bin/sh\necho skip output"), 0700)
	c.Assert(err, IsNil)

	basedir = filepath.Join(dirs.SnapRepairRunDir, "my-brand/3")
	err = os.MkdirAll(basedir, 0700)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r0.running"), []byte("repair: my-brand-3\nsummary: my-brand repair three\noutput:\nrunning output"), 0600)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r0.script"), []byte("#!/bin/sh\necho running output"), 0700)
	c.Assert(err, IsNil)
}
