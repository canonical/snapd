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

// osutil.user implements a local user lookup and iteration module.
//
// Right now it's very dumb, and will not be happy on systems with a
// lot of users. These things will get fixed, but the API this exposes
// should survive that fix.
package user

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/strutil"
)

var minUserUID sys.UserID
var shells = []string{"/bin/sh"}

func init() {
	setup()
}

func setup() {
	minUserUID = minUID()

	shells = nil
	if f, err := os.Open(filepath.Join(dirs.GlobalRootDir, "/etc/shells")); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 || line[0] == '#' {
				continue
			}
			shells = append(shells, string(line))
		}
	}

	me, meErr = FromUID(sys.Getuid())
}

// Mock is exposed to be called from tests. If in your tests you changed things
// that impact what users look like you might need to call user.Mock() so it
// re-reads some bits of data (don't forget to call the returned restore
// function in your test teardown, otherwise things'll be weird).
//
// Currently, this looks up:
//  * UID_MIN (from /etc/login.defs; man 5 login.defs)
//  * the list of valid login shells (from /etc/shells; man 5 shells)
//  * the current user (from getuid(2) and /etc/passwd)
func Mock() (restore func()) {
	oldMinUserUID := minUserUID
	oldShells := shells
	oldMe := me
	oldMeErr := meErr

	setup()

	return func() {
		minUserUID = oldMinUserUID
		shells = oldShells
		me = oldMe
		meErr = oldMeErr
	}
}

// get UID_MIN from /etc/login.defs
func minUID() sys.UserID {
	uid := sys.UserID(1000) // default according to login.defs(5)

	f, err := os.Open(filepath.Join(dirs.GlobalRootDir, "/etc/login.defs"))
	if err != nil {
		return uid
	}
	defer f.Close()
	prefix := []byte("UID_MIN")
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if !bytes.HasPrefix(line, prefix) {
			continue
		}
		fields := bytes.Fields(line)
		if len(fields) != 2 || !bytes.Equal(fields[0], prefix) {
			continue
		}
		if u, err := strconv.ParseUint(string(fields[1]), 10, 32); err == nil {
			uid = sys.UserID(u)
			break
		}
	}

	return uid
}

// IsNonSystem is a filter that will return true only if the given
// user is not a "system" user. The exact meaning of this is
// backend-dependent, but in general system users aren't 'people'.
func IsNonSystem(u *User) bool {
	return !u.isSystemUser()
}

type User struct {
	name  string
	home  string
	shell string
	uid   sys.UserID
	gid   sys.GroupID
}

func (u *User) Name() string {
	return u.name
}

func (u *User) Home() string {
	return u.home
}

func (u *User) UID() sys.UserID {
	return u.uid
}

func (u *User) GID() sys.GroupID {
	return u.gid
}

func (u *User) isSystemUser() bool {
	if u.uid > 0 && u.uid < minUserUID {
		return true
	}

	if u.name == "nobody" {
		return true
	}

	if len(shells) == 0 {
		// no /etc/shells --> all shells are user shells
		return false
	}

	return !strutil.ListContains(shells, u.shell)
}

func (u *User) satisfiesAll(conds []func(*User) bool) bool {
	for _, cond := range conds {
		if !cond(u) {
			return false
		}
	}

	return true
}

var me *User
var meErr error

func Current() (*User, error) {
	return me, meErr
}

// NotFound is returned when no users are found that match the given filter.
var NotFound = errors.New("user not found")

// first tries to return a user that matches the given filter.
func first(filter func(*User) bool) (*User, error) {
	var it Iter
	defer it.Finish()
	for it.Scan(filter) {
		return it.User(), nil
	}

	err := NotFound
	if it.err != nil {
		err = it.err
	}

	return nil, err
}

// FromUID tries to find a user with the given UID.
func FromUID(uid sys.UserID) (*User, error) {
	return first(func(u *User) bool {
		return u.uid == uid
	})
}

// FromName tries to find a user with the given name.
func FromName(name string) (*User, error) {
	return first(func(u *User) bool {
		return u.name == name
	})
}

var passwds = []string{
	// order could be important
	"/var/lib/extrausers/passwd",
	"/etc/passwd",
}

// an Iter will iterate over the user database.
type Iter struct {
	pwi     int
	pwf     *os.File
	scanner *bufio.Scanner
	cur     *User
	err     error
}

// User returns the user found by Scan.
func (it *Iter) User() *User {
	return it.cur
}

// nextFile finds the next passwd file and sets up the scanner to read it
// returns true if it succeeded, false otherwise.
func (it *Iter) nextFile() bool {
	for it.scanner == nil {
		if it.pwi >= len(passwds) {
			// no more files to scan
			return false
		}
		if it.pwf != nil {
			it.err = it.pwf.Close()
		}
		if it.err != nil {
			return false
		}
		it.pwf, it.err = os.Open(filepath.Join(dirs.GlobalRootDir, passwds[it.pwi]))
		it.pwi++
		if it.err != nil {
			// ignore missing files
			if !os.IsNotExist(it.err) {
				return false
			}
			it.err = nil
			// try next file
			continue
		}
		it.scanner = bufio.NewScanner(it.pwf)
	}

	return true
}

// Scan advances the iterator until it finds a user that matches all
// the given conditions.
func (it *Iter) Scan(conds ...func(*User) bool) bool {
	if it.err != nil {
		return false
	}

	if it.scanner == nil {
		if ok := it.nextFile(); !ok {
			return false
		}
	}

	for it.scanner.Scan() {
		it.cur = nil
		line := bytes.TrimSpace(it.scanner.Bytes())
		if len(line) == 0 || line[0] == '#' {
			// blank or comment; ignore
			continue
		}
		fields := bytes.SplitN(line, []byte{':'}, 8)
		if len(fields) != 7 {
			// bogus line; ignore
			continue
		}
		// root:x:0:0:root:/root:/bin/bash
		uid, err := strconv.ParseUint(string(fields[2]), 10, 32)
		if err != nil {
			continue
		}
		gid, err := strconv.ParseUint(string(fields[3]), 10, 32)
		if err != nil {
			continue
		}
		u := &User{
			name:  string(fields[0]),
			home:  string(fields[5]),
			shell: string(fields[6]),
			uid:   sys.UserID(uid),
			gid:   sys.GroupID(gid),
		}
		if !u.satisfiesAll(conds) {
			continue
		}

		it.cur = u
		return true
	}

	// reached the end of the current file; try again with the next one
	it.err = it.scanner.Err()
	it.scanner = nil

	return it.Scan(conds...)
}

// Finish closes down any connections the iterator might have to
// whatever backend(s) it's inspecting. The error is returned for
// convenience, but can also be obtained from Err().
func (it *Iter) Finish() error {
	if it == nil {
		return nil
	}
	if it.pwf != nil {
		e := it.pwf.Close()
		if it.err == nil && e != nil {
			it.err = e
		}
		it.pwf = nil
	}
	return it.err
}

// Err returns the Iter's error.
func (it *Iter) Err() error {
	return it.err
}

// Fake returns a fake user for use in testing.
func Fake(name, home string, uid sys.UserID, gid sys.GroupID) *User {
	return &User{
		name:  name,
		home:  home,
		shell: "/bin/sh",
		uid:   uid,
		gid:   gid,
	}
}
