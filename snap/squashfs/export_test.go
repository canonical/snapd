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

package squashfs

import (
	"os"
	"os/exec"
	"time"

	"gopkg.in/check.v1"
)

var (
	FromRaw                   = fromRaw
	NewUnsquashfsStderrWriter = newUnsquashfsStderrWriter
)

const MaxErrPaths = maxErrPaths

func (s stat) User() string  { return s.user }
func (s stat) Group() string { return s.group }

func MockLink(newLink func(string, string) error) (restore func()) {
	oldLink := osLink
	osLink = newLink
	return func() {
		osLink = oldLink
	}
}

func MockCommandFromSystemSnap(f func(string, ...string) (*exec.Cmd, error)) (restore func()) {
	oldCommandFromSystemSnap := cmdutilCommandFromSystemSnap
	cmdutilCommandFromSystemSnap = f
	return func() {
		cmdutilCommandFromSystemSnap = oldCommandFromSystemSnap
	}
}

// Alike compares to os.FileInfo to determine if they are sufficiently
// alike to say they refer to the same thing.
func Alike(a, b os.FileInfo, c *check.C, comment check.CommentInterface) {
	c.Check(a, check.NotNil, comment)
	c.Check(b, check.NotNil, comment)
	if a == nil || b == nil {
		return
	}

	// the .Name() of the root will be different on non-squashfs things
	_, asq := a.(*stat)
	_, bsq := b.(*stat)
	if !((asq && a.Name() == "/") || (bsq && b.Name() == "/")) {
		c.Check(a.Name(), check.Equals, b.Name(), comment)
	}

	c.Check(a.Mode(), check.Equals, b.Mode(), comment)
	if a.Mode().IsRegular() {
		c.Check(a.Size(), check.Equals, b.Size(), comment)
	}
	am := a.ModTime().UTC().Truncate(time.Minute)
	bm := b.ModTime().UTC().Truncate(time.Minute)
	c.Check(am.Equal(bm), check.Equals, true, check.Commentf("%s != %s (%s)", am, bm, comment))
}
