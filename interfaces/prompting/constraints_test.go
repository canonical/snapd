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

package prompting_test

import (
	"fmt"
	"time"

	// TODO: remove once PR #13849 is merged
	"testing"

	. "gopkg.in/check.v1"

	// TODO: add this once PR #13730 is merged:
	// doublestar "github.com/bmatcuk/doublestar/v4"

	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"

	// TODO: remove once PR #13849 is merged
	"github.com/snapcore/snapd/dirs"
)

// TODO: remove once PR #13849 is merged
func Test(t *testing.T) { TestingT(t) }

// TODO: remove once PR #13849 is merged
type promptingSuite struct {
	tmpdir string
}

// TODO: remove once PR #13849 is merged
var _ = Suite(&promptingSuite{})

// TODO: remove once PR #13849 is merged
func (s *promptingSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
}

func (s *promptingSuite) TestConstraintsValidateForInterface(c *C) {
	cases := []struct {
		iface   string
		pattern string
		perms   []string
		errStr  string
	}{
		{
			"foo",
			"invalid/path",
			[]string{"read"},
			"unsupported interface.*",
		},
		// TODO: add this once PR #13730 is merged:
		// {
		//	"home",
		//	"invalid/path",
		//	[]string{"read"},
		//	"invalid path pattern.*",
		// },
		{
			"home",
			"/valid/path",
			[]string{},
			fmt.Sprintf("%v", prompting.ErrPermissionsListEmpty),
		},
	}
	for _, testCase := range cases {
		constraints := &prompting.Constraints{
			PathPattern: testCase.pattern,
			Permissions: testCase.perms,
		}
		err := constraints.ValidateForInterface(testCase.iface)
		c.Check(err, ErrorMatches, testCase.errStr)
	}
}

func (*promptingSuite) TestConstraintsMatch(c *C) {
	cases := []struct {
		pattern string
		path    string
		matches bool
	}{
		{
			"/home/test/Documents/foo.txt",
			"/home/test/Documents/foo.txt",
			true,
		},
		// TODO: add this once PR #13730 is merged:
		// {
		//	"/home/test/Documents/foo",
		//	"/home/test/Documents/foo.txt",
		//	false,
		// },
	}
	for _, testCase := range cases {
		constraints := &prompting.Constraints{
			PathPattern: testCase.pattern,
			Permissions: []string{"read"},
		}
		result, err := constraints.Match(testCase.path)
		c.Check(err, IsNil, Commentf("test case: %+v", testCase))
		c.Check(result, Equals, testCase.matches, Commentf("test case: %+v", testCase))
	}
}

func (s *promptingSuite) TestConstraintsMatchUnhappy(c *C) {
	badPath := `bad\pattern\`
	badConstraints := &prompting.Constraints{
		PathPattern: badPath,
		Permissions: []string{"read"},
	}
	matches, err := badConstraints.Match(badPath)
	// TODO: change to this once PR #13730 is merged:
	// c.Check(err, Equals, doublestar.ErrBadPattern)
	// c.Check(matches, Equals, false)
	c.Check(err, Equals, nil)
	c.Check(matches, Equals, true)
}

func (s *promptingSuite) TestConstraintsRemovePermission(c *C) {
	cases := []struct {
		initial []string
		remove  string
		final   []string
		err     error
	}{
		{
			[]string{"read", "write", "execute"},
			"read",
			[]string{"write", "execute"},
			nil,
		},
		{
			[]string{"read", "write", "execute"},
			"write",
			[]string{"read", "execute"},
			nil,
		},
		{
			[]string{"read", "write", "execute"},
			"execute",
			[]string{"read", "write"},
			nil,
		},
		{
			[]string{"read", "write", "read"},
			"read",
			[]string{"write"},
			nil,
		},
		{
			[]string{"read"},
			"read",
			[]string{},
			nil,
		},
		{
			[]string{"read", "read"},
			"read",
			[]string{},
			nil,
		},
		{
			[]string{"read", "write", "execute"},
			"append",
			[]string{"read", "write", "execute"},
			prompting.ErrPermissionNotInList,
		},
		{
			[]string{},
			"read",
			[]string{},
			prompting.ErrPermissionNotInList,
		},
	}
	for _, testCase := range cases {
		constraints := &prompting.Constraints{
			PathPattern: "/path/to/foo",
			Permissions: testCase.initial,
		}
		err := constraints.RemovePermission(testCase.remove)
		c.Check(err, Equals, testCase.err)
		c.Check(constraints.Permissions, DeepEquals, testCase.final)
	}
}

func (s *promptingSuite) TestConstraintsContainPermissions(c *C) {
	cases := []struct {
		constPerms []string
		queryPerms []string
		contained  bool
	}{
		{
			[]string{"read", "write", "execute"},
			[]string{"read", "write", "execute"},
			true,
		},
		{
			[]string{"execute", "write", "read"},
			[]string{"read", "write", "execute"},
			true,
		},
		{
			[]string{"read", "write", "execute"},
			[]string{"read"},
			true,
		},
		{
			[]string{"read", "write", "execute"},
			[]string{"execute"},
			true,
		},
		{
			[]string{"read", "write", "execute"},
			[]string{"read", "write", "execute", "append"},
			false,
		},
		{
			[]string{"read", "write", "execute"},
			[]string{"read", "append"},
			false,
		},
		{
			[]string{"foo", "bar", "baz"},
			[]string{"foo", "bar"},
			true,
		},
		{
			[]string{"foo", "bar", "baz"},
			[]string{"fizz", "buzz"},
			false,
		},
	}
	for _, testCase := range cases {
		constraints := &prompting.Constraints{
			PathPattern: "arbitrary",
			Permissions: testCase.constPerms,
		}
		contained := constraints.ContainPermissions(testCase.queryPerms)
		c.Check(contained, Equals, testCase.contained, Commentf("testCase: %+v", testCase))
	}
}

func constructPermissionsMaps() []map[string]map[string]interface{} {
	var permissionsMaps []map[string]map[string]interface{}
	// interfaceFilePermissionsMaps
	filePermissionsMaps := make(map[string]map[string]interface{})
	for iface, permsMap := range prompting.InterfaceFilePermissionsMaps {
		filePermissionsMaps[iface] = make(map[string]interface{}, len(permsMap))
		for perm, val := range permsMap {
			filePermissionsMaps[iface][perm] = val
		}
	}
	permissionsMaps = append(permissionsMaps, filePermissionsMaps)
	// TODO: do the same for other maps of permissions maps in the future
	return permissionsMaps
}

func (s *promptingSuite) TestInterfacesAndPermissionsCompleteness(c *C) {
	permissionsMaps := constructPermissionsMaps()
	// Check that every interface in interfacePriorities is also in
	// interfacePermissionsAvailable and exactly one of the permissions maps.
	// Also, check that the permissions for a given interface in
	// interfacePermissionsAvailable are identical to the permissions in the
	// interface's permissions map.
	// Also, check that each priority only occurs once.
	for iface, perms := range prompting.InterfacePermissionsAvailable {
		availablePerms, err := prompting.AvailablePermissions(iface)
		c.Check(err, IsNil, Commentf("interface missing from interfacePermissionsAvailable: %s", iface))
		c.Check(perms, Not(HasLen), 0, Commentf("interface has no available permissions: %s", iface))
		c.Check(availablePerms, DeepEquals, perms)
		found := false
		for _, permsMaps := range permissionsMaps {
			pMap, exists := permsMaps[iface]
			if !exists {
				continue
			}
			c.Check(found, Equals, false, Commentf("interface found in more than one map of interface permissions maps: %s", iface))
			found = true
			// Check that permissions in the list and map are identical
			c.Check(pMap, HasLen, len(perms), Commentf("permissions list and map inconsistent for interface: %s", iface))
			for _, perm := range perms {
				_, exists := pMap[perm]
				c.Check(exists, Equals, true, Commentf("missing permission mapping for %s interface permission: %s", iface, perm))
			}
		}
		if !found {
			c.Errorf("interface not included in any map of interface permissions maps: %s", iface)
		}
	}
}

func (s *promptingSuite) TestInterfaceFilePermissionsMapsCorrectness(c *C) {
	for iface, permsMap := range prompting.InterfaceFilePermissionsMaps {
		seenPermissions := notify.FilePermission(0)
		for name, mask := range permsMap {
			if duplicate := seenPermissions & mask; duplicate != notify.FilePermission(0) {
				c.Errorf("AppArmor file permission found in more than one permission map for %s interface: %s", iface, duplicate.String())
			}
			c.Check(mask&notify.AA_MAY_OPEN, Equals, notify.FilePermission(0), Commentf("AA_MAY_OPEN may not be included in permissions maps, but %s interface includes it in the map for permission: %s", iface, name))
			seenPermissions |= mask
		}
	}
}

func (s *promptingSuite) TestAvailablePermissions(c *C) {
	for iface, perms := range prompting.InterfacePermissionsAvailable {
		available, err := prompting.AvailablePermissions(iface)
		c.Check(err, IsNil)
		c.Check(available, DeepEquals, perms)
	}
	available, err := prompting.AvailablePermissions("foo")
	c.Check(err, ErrorMatches, ".*unsupported interface.*")
	c.Check(available, IsNil)
}

func (s *promptingSuite) TestAbstractPermissionsFromAppArmorFilePermissionsHappy(c *C) {
	cases := []struct {
		iface string
		mask  notify.FilePermission
		list  []string
	}{
		{
			"home",
			notify.AA_MAY_READ,
			[]string{"read"},
		},
		{
			"home",
			notify.AA_MAY_WRITE | notify.AA_MAY_APPEND | notify.AA_MAY_CREATE | notify.AA_MAY_DELETE | notify.AA_MAY_RENAME | notify.AA_MAY_CHMOD | notify.AA_MAY_LOCK | notify.AA_MAY_LINK,
			[]string{"write"},
		},
		{
			"home",
			notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP,
			[]string{"execute"},
		},
		{
			"home",
			notify.AA_MAY_OPEN,
			[]string{"read"},
		},
		{
			"home",
			notify.AA_MAY_OPEN | notify.AA_MAY_WRITE,
			[]string{"write"},
		},
		{
			"home",
			notify.AA_MAY_EXEC | notify.AA_MAY_WRITE | notify.AA_MAY_READ,
			[]string{"read", "write", "execute"},
		},
	}
	for _, testCase := range cases {
		perms, err := prompting.AbstractPermissionsFromAppArmorPermissions(testCase.iface, testCase.mask)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(perms, DeepEquals, testCase.list)
	}
}

func (s *promptingSuite) TestAbstractPermissionsFromAppArmorFilePermissionsUnhappy(c *C) {
	cases := []struct {
		iface  string
		perms  interface{}
		errStr string
	}{
		{
			"home",
			"not a file permission",
			"failed to parse the given permissions as file permissions",
		},
		{
			"home",
			notify.FilePermission(1 << 17),
			"received unexpected permission for interface.*",
		},
		{
			"home",
			notify.AA_MAY_GETATTR | notify.AA_MAY_READ,
			"received unexpected permission for interface.*",
		},
		{
			"home",
			notify.FilePermission(0),
			"no abstract permissions.*",
		},
	}
	for _, testCase := range cases {
		perms, err := prompting.AbstractPermissionsFromAppArmorPermissions(testCase.iface, testCase.perms)
		c.Check(perms, IsNil, Commentf("received unexpected non-nil permissions list for test case: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.errStr)
	}
}

func (s *promptingSuite) TestAbstractPermissionsFromListHappy(c *C) {
	cases := []struct {
		iface   string
		initial []string
		final   []string
	}{
		{
			"home",
			[]string{"write", "read", "execute"},
			[]string{"read", "write", "execute"},
		},
		{
			"home",
			[]string{"execute", "write", "read"},
			[]string{"read", "write", "execute"},
		},
		{
			"home",
			[]string{"write", "write", "write"},
			[]string{"write"},
		},
	}
	for _, testCase := range cases {
		perms, err := prompting.AbstractPermissionsFromList(testCase.iface, testCase.initial)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(perms, DeepEquals, testCase.final, Commentf("testCase: %+v", testCase))
	}
}

func (s *promptingSuite) TestAbstractPermissionsFromListUnhappy(c *C) {
	cases := []struct {
		iface  string
		perms  []string
		errStr string
	}{
		{
			"foo",
			[]string{"read"},
			"unsupported interface.*",
		},
		{
			"home",
			[]string{"access"},
			"unsupported permission.*",
		},
		{
			"home",
			[]string{"read", "write", "access"},
			"unsupported permission.*",
		},
		{
			"home",
			[]string{},
			fmt.Sprintf("%v", prompting.ErrPermissionsListEmpty),
		},
	}
	for _, testCase := range cases {
		perms, err := prompting.AbstractPermissionsFromList(testCase.iface, testCase.perms)
		c.Check(perms, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.errStr, Commentf("testCase: %+v", testCase))
	}
}

func (s *promptingSuite) TestAbstractPermissionsToAppArmorFilePermissionsHappy(c *C) {
	cases := []struct {
		iface string
		list  []string
		mask  notify.FilePermission
	}{
		{
			"home",
			[]string{"read"},
			notify.AA_MAY_OPEN | notify.AA_MAY_READ,
		},
		{
			"home",
			[]string{"write"},
			notify.AA_MAY_OPEN | notify.AA_MAY_WRITE | notify.AA_MAY_APPEND | notify.AA_MAY_CREATE | notify.AA_MAY_DELETE | notify.AA_MAY_RENAME | notify.AA_MAY_CHMOD | notify.AA_MAY_LOCK | notify.AA_MAY_LINK,
		},
		{
			"home",
			[]string{"execute"},
			notify.AA_MAY_OPEN | notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP,
		},
		{
			"home",
			[]string{"read", "execute"},
			notify.AA_MAY_OPEN | notify.AA_MAY_READ | notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP,
		},
		{
			"home",
			[]string{"execute", "write", "read"},
			notify.AA_MAY_OPEN | notify.AA_MAY_READ | notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP | notify.AA_MAY_WRITE | notify.AA_MAY_APPEND | notify.AA_MAY_CREATE | notify.AA_MAY_DELETE | notify.AA_MAY_RENAME | notify.AA_MAY_CHMOD | notify.AA_MAY_LOCK | notify.AA_MAY_LINK,
		},
	}
	for _, testCase := range cases {
		ret, err := prompting.AbstractPermissionsToAppArmorPermissions(testCase.iface, testCase.list)
		c.Check(err, IsNil)
		perms, ok := ret.(notify.FilePermission)
		c.Check(ok, Equals, true, Commentf("failed to parse return value as FilePermission for test case: %+v", testCase))
		c.Check(perms, Equals, testCase.mask)
	}
}

func (s *promptingSuite) TestAbstractPermissionsToAppArmorFilePermissionsUnhappy(c *C) {
	cases := []struct {
		iface  string
		perms  []string
		errStr string
	}{
		{
			"home",
			[]string{},
			fmt.Sprintf("%v", prompting.ErrPermissionsListEmpty),
		},
		{
			"home",
			[]string{"foo"},
			"no AppArmor file permission mapping .* abstract permission.*",
		},
		{
			"home",
			[]string{"access"},
			"no AppArmor file permission mapping .* abstract permission.*",
		},
		{
			"home",
			[]string{"read", "foo", "write"},
			"no AppArmor file permission mapping .* abstract permission.*",
		},
	}
	for _, testCase := range cases {
		_, err := prompting.AbstractPermissionsToAppArmorPermissions(testCase.iface, testCase.perms)
		c.Check(err, ErrorMatches, testCase.errStr)
	}
}

func (s *promptingSuite) TestValidateConstraintsOutcomeLifespanExpiration(c *C) {
	goodInterface := "home"
	badInterface := "foo"
	goodConstraints := &prompting.Constraints{
		PathPattern: "/path/to/something",
		Permissions: []string{"read", "write", "execute"},
	}
	badConstraints := &prompting.Constraints{
		PathPattern: "/path{with*,groups?}/**",
		Permissions: []string{"read", "write", "append"},
	}
	goodOutcome := prompting.OutcomeDeny
	badOutcome := prompting.OutcomeUnset
	goodLifespan := prompting.LifespanTimespan
	badLifespan := prompting.LifespanType("foo")
	currTime := time.Now()
	goodExpiration := currTime.Add(10 * time.Second).Format(time.RFC3339)
	badExpiration := currTime.Add(-1 * time.Second).Format(time.RFC3339)

	err := prompting.ValidateConstraintsOutcomeLifespanExpiration(goodInterface, goodConstraints, goodOutcome, goodLifespan, goodExpiration, currTime)
	c.Check(err, IsNil)
	err = prompting.ValidateConstraintsOutcomeLifespanExpiration(badInterface, goodConstraints, goodOutcome, goodLifespan, goodExpiration, currTime)
	c.Check(err, NotNil)
	err = prompting.ValidateConstraintsOutcomeLifespanExpiration(goodInterface, badConstraints, goodOutcome, goodLifespan, goodExpiration, currTime)
	c.Check(err, ErrorMatches, "unsupported permission.*")
	err = prompting.ValidateConstraintsOutcomeLifespanExpiration(goodInterface, goodConstraints, badOutcome, goodLifespan, goodExpiration, currTime)
	c.Check(err, ErrorMatches, "invalid outcome.*")
	err = prompting.ValidateConstraintsOutcomeLifespanExpiration(goodInterface, goodConstraints, goodOutcome, badLifespan, goodExpiration, currTime)
	c.Check(err, ErrorMatches, "invalid lifespan.*")
	err = prompting.ValidateConstraintsOutcomeLifespanExpiration(goodInterface, goodConstraints, goodOutcome, goodLifespan, badExpiration, currTime)
	c.Check(err, ErrorMatches, "invalid expiration.*")
}

func (s *promptingSuite) TestValidateConstraintsOutcomeLifespanDuration(c *C) {
	goodInterface := "home"
	badInterface := "foo"
	goodConstraints := &prompting.Constraints{
		PathPattern: "/path/to/something",
		Permissions: []string{"read"},
	}
	// badConstraints := &prompting.Constraints{
	//	PathPattern: "bad\\path",
	//	Permissions: []string{"read"},
	// }
	goodOutcome := prompting.OutcomeAllow
	badOutcome := prompting.OutcomeUnset
	goodLifespan := prompting.LifespanTimespan
	badLifespan := prompting.LifespanUnset
	goodDuration := "10s"
	badDuration := "foo"

	_, err := prompting.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, goodOutcome, goodLifespan, goodDuration)
	c.Check(err, IsNil)
	_, err = prompting.ValidateConstraintsOutcomeLifespanDuration(badInterface, goodConstraints, goodOutcome, goodLifespan, goodDuration)
	c.Check(err, NotNil)
	// TODO: add this once PR #13730 is merged:
	// _, err = prompting.ValidateConstraintsOutcomeLifespanDuration(goodInterface, badConstraints, goodOutcome, goodLifespan, goodDuration)
	// c.Check(err, ErrorMatches, "invalid path pattern.*")
	_, err = prompting.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, badOutcome, goodLifespan, goodDuration)
	c.Check(err, ErrorMatches, "invalid outcome.*")
	_, err = prompting.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, goodOutcome, badLifespan, goodDuration)
	c.Check(err, ErrorMatches, "invalid lifespan.*")
	_, err = prompting.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, goodOutcome, goodLifespan, badDuration)
	c.Check(err, ErrorMatches, "invalid duration.*")
}
