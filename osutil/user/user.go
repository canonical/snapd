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
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/strutil"
)

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
	return func() {
	}
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

var minUserUID sys.UserID = 1000
var shells []string

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

var mu sync.Mutex
var me *User
var meErr error

func Current() (*User, error) {
	mu.Lock()
	defer mu.Unlock()
	if me == nil && meErr == nil {
		me, meErr = FromUID(sys.Getuid())
	}
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

var passwds []string

func init() {
	if release.OnClassic {
		passwds = []string{
			"/etc/passwd",
		}
	} else {
		// order could be important
		passwds = []string{
			"/var/lib/extrausers/passwd",
			"/etc/passwd",
		}
	}
}

// an Iter will iterate over the user database.
type Iter struct {
	pwd *os.File
	cur *User
	err error
}

// User returns the user found by Scan.
func (it *Iter) User() *User {
	return it.cur
}

// Scan advances the iterator until it finds a user that matches all
// the given conditions.
func (it *Iter) Scan(conds ...func(*User) bool) bool {
	if it.err != nil {
		return false
	}

	if it.cur == nil {
		it.cur = &User{
			name: "root",
			home: "/root",
		}
		return true
	}

	if it.pwd == nil {
		it.pwd, it.err = os.Open("/home")
		if it.err != nil {
			return false
		}
	}

	names, err := it.pwd.Readdirnames(1)
	if err != nil {
		if err != io.EOF {
			it.err = err
		}
		return false
	}

	name := filepath.Base(names[0])
	u, err := user.Lookup(name)
	if err != nil {
		it.err = err
		return false
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		it.err = err
		return false
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		it.err = err
		return false
	}
	it.cur = &User{
		name: name,
		home: names[0],
		uid:  sys.UserID(uid),
		gid:  sys.GroupID(gid),
	}
	return true
}

// Finish closes down any connections the iterator might have to
// whatever backend(s) it's inspecting. The error is returned for
// convenience, but can also be obtained from Err().
func (it *Iter) Finish() error {
	if it == nil {
		return nil
	}
	if it.pwd != nil {
		e := it.pwd.Close()
		if it.err == nil && e != nil {
			it.err = e
		}
		it.pwd = nil
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
