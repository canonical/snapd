// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package user

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	DefaultGetentSearchPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
)

var (
	getentSearchPath = DefaultGetentSearchPath
)

func findGetent(searchPath string) (string, error) {
	// try to look for getent in a couple of places, such that even when running
	// with modified PATH we still can locate the executable
	for _, dir := range filepath.SplitList(searchPath) {
		p := filepath.Join(dir, "getent")
		if fi, err := os.Stat(p); err == nil {
			if !fi.IsDir() && fi.Mode().Perm()&0111 != 0 {
				return p, nil
			}
		}
	}
	return "", errors.New("cannot locate getent executable")
}

func getEnt(params ...string) ([]byte, error) {
	getentCmd, err := findGetent(getentSearchPath)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(getentCmd, params...)
	cmd.Stdin = nil

	outBuf, err := cmd.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			if exitError.ExitCode() == 2 {
				return nil, nil
			}
			return nil, fmt.Errorf("getent returned an error: %q", exitError.Stderr)
		}
		return nil, fmt.Errorf("getent could not be executed: %w", err)
	}

	return outBuf, nil
}

// lookupFromGetent calls getent, parses and filters its output
// The component at `index` will need to match `expectedValue`.
// If `isKey`, then `expectedValue` will also be passed as parameter
// to getent along `database`. `numComponents` should be 4 for groups
// and 7 for users.
func lookupFromGetent(database string, index int, expectedValue string, isKey bool, numComponents int) ([]string, error) {
	params := []string{database}
	if isKey {
		params = append(params, expectedValue)
	}
	buf, err := getEnt(params...)
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(buf))
	for scanner.Scan() {
		components := strings.SplitN(scanner.Text(), ":", numComponents)
		if len(components) != numComponents {
			continue
		}

		if components[index] != expectedValue {
			continue
		}

		return components, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func isNumeric(value string) bool {
	for _, c := range value {
		// We check only the first character
		return '0' <= c && c <= '9'
	}
	return false
}

func isKey(index int, expectedValue string) bool {
	numeric := isNumeric(expectedValue)
	return (index == 0 && !numeric) || (index == 2 && numeric)
}

type groupMatcher interface {
	index() int
	expectedValue() string
}

type groupnameMatcher struct {
	value string
}

func (m groupnameMatcher) index() int {
	return 0
}

func (m groupnameMatcher) expectedValue() string {
	return m.value
}

func groupMatchGroupname(groupname string) groupMatcher {
	return groupnameMatcher{
		value: groupname,
	}
}

func lookupGroupFromGetent(matcher groupMatcher) (*Group, error) {
	components, err := lookupFromGetent("group", matcher.index(), matcher.expectedValue(), isKey(matcher.index(), matcher.expectedValue()), 4)

	if err != nil {
		return nil, err
	}

	if components == nil {
		return nil, nil
	}

	return &Group{
		Name: components[0],
		Gid:  components[2],
	}, nil
}

type userMatcher interface {
	index() int
	expectedValue() string
}

type usernameMatcher struct {
	value string
}

func (m usernameMatcher) index() int {
	return 0
}

func (m usernameMatcher) expectedValue() string {
	return m.value
}

func userMatchUsername(username string) userMatcher {
	return usernameMatcher{
		value: username,
	}
}

type uidMatcher struct {
	value int
}

func (m uidMatcher) index() int {
	return 2
}

func (m uidMatcher) expectedValue() string {
	return strconv.Itoa(m.value)
}

func userMatchUid(uid int) userMatcher {
	return uidMatcher{
		value: uid,
	}
}

func lookupUserFromGetent(matcher userMatcher) (*User, error) {
	components, err := lookupFromGetent("passwd", matcher.index(), matcher.expectedValue(), isKey(matcher.index(), matcher.expectedValue()), 7)

	if err != nil {
		return nil, err
	}

	if components == nil {
		return nil, nil
	}

	return &User{
		Username: components[0],
		Uid:      components[2],
		Gid:      components[3],
		Name:     components[4],
		HomeDir:  components[5],
	}, nil
}

// OverrideGetentSearchPath allows overriding getent search path. Its only
// purpose is to be used in tests.
func OverrideGetentSearchPath(p string) {
	// TODO should use osutil.MustBeTestBinary() but we cannot import due to
	// cyclic dependencies
	getentSearchPath = p
}
