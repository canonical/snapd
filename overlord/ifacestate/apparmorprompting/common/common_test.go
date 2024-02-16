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

package common_test

import (
	"encoding/base32"
	"encoding/binary"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
)

func Test(t *testing.T) { TestingT(t) }

type commonSuite struct {
	tmpdir string
}

var _ = Suite(&commonSuite{})

func (s *commonSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
}

func (s *commonSuite) TestConstraintsValidateForInterface(c *C) {
	goodConstraints := &common.Constraints{
		PathPattern: "/path/to/foo",
		Permissions: []common.PermissionType{common.PermissionRead},
	}
	// badConstraints := &common.Constraints{
	//	PathPattern: "bad\\pattern",
	//	Permissions: []common.PermissionType{common.PermissionRead},
	// }
	goodInterface := "home"
	badInterface := "foo"

	c.Check(goodConstraints.ValidateForInterface(goodInterface), IsNil)
	c.Check(goodConstraints.ValidateForInterface(badInterface), NotNil)
	// TODO: add this once PR #13730 is merged:
	// c.Check(badConstraints.ValidateForInterface(goodInterface), Equals, common.ErrInvalidPathPattern)
}

func (s *commonSuite) TestConstraintsRemovePermission(c *C) {
	cases := []struct {
		initial []common.PermissionType
		remove  common.PermissionType
		final   []common.PermissionType
		err     error
	}{
		{
			[]common.PermissionType{common.PermissionRead, common.PermissionWrite, common.PermissionExecute},
			common.PermissionRead,
			[]common.PermissionType{common.PermissionWrite, common.PermissionExecute},
			nil,
		},
		{
			[]common.PermissionType{common.PermissionRead, common.PermissionWrite, common.PermissionExecute},
			common.PermissionWrite,
			[]common.PermissionType{common.PermissionRead, common.PermissionExecute},
			nil,
		},
		{
			[]common.PermissionType{common.PermissionRead, common.PermissionWrite, common.PermissionExecute},
			common.PermissionExecute,
			[]common.PermissionType{common.PermissionRead, common.PermissionWrite},
			nil,
		},
		{
			[]common.PermissionType{common.PermissionRead, common.PermissionWrite, common.PermissionRead},
			common.PermissionRead,
			[]common.PermissionType{common.PermissionWrite},
			nil,
		},
		{
			[]common.PermissionType{common.PermissionRead},
			common.PermissionRead,
			[]common.PermissionType{},
			nil,
		},
		{
			[]common.PermissionType{common.PermissionRead, common.PermissionRead},
			common.PermissionRead,
			[]common.PermissionType{},
			nil,
		},
		{
			[]common.PermissionType{common.PermissionRead, common.PermissionWrite, common.PermissionExecute},
			common.PermissionAppend,
			[]common.PermissionType{common.PermissionRead, common.PermissionWrite, common.PermissionExecute},
			common.ErrPermissionNotInList,
		},
		{
			[]common.PermissionType{},
			common.PermissionRead,
			[]common.PermissionType{},
			common.ErrPermissionNotInList,
		},
	}
	for _, testCase := range cases {
		constraints := &common.Constraints{
			PathPattern: "/path/to/foo",
			Permissions: testCase.initial,
		}
		err := constraints.RemovePermission(testCase.remove)
		c.Check(err, Equals, testCase.err)
		c.Check(constraints.Permissions, DeepEquals, testCase.final)
	}
}

func (s *commonSuite) TestTimestamps(c *C) {
	before := time.Now()
	ts := common.CurrentTimestamp()
	after := time.Now()
	parsedTime, err := common.TimestampToTime(ts)
	c.Assert(err, IsNil)
	c.Assert(parsedTime.After(before), Equals, true)
	c.Assert(parsedTime.Before(after), Equals, true)
}

func (s *commonSuite) TestNewIDAndTimestamp(c *C) {
	before := time.Now()
	id := common.NewID()
	idPaired, timestampPaired := common.NewIDAndTimestamp()
	after := time.Now()
	data1, err := base32.StdEncoding.DecodeString(id)
	c.Assert(err, IsNil)
	data2, err := base32.StdEncoding.DecodeString(idPaired)
	c.Assert(err, IsNil)
	parsedNs := int64(binary.BigEndian.Uint64(data1))
	parsedNsPaired := int64(binary.BigEndian.Uint64(data2))
	parsedTime := time.Unix(parsedNs/1000000000, parsedNs%1000000000)
	parsedTimePaired := time.Unix(parsedNsPaired/1000000000, parsedNsPaired%1000000000)
	c.Assert(parsedTime.After(before), Equals, true)
	c.Assert(parsedTime.Before(after), Equals, true)
	c.Assert(parsedTimePaired.After(before), Equals, true)
	c.Assert(parsedTimePaired.Before(after), Equals, true)
	parsedTimestamp, err := common.TimestampToTime(timestampPaired)
	c.Assert(err, IsNil)
	c.Assert(parsedTimePaired, Equals, parsedTimestamp)
}

func (s *commonSuite) TestLabelToSnapAppHappy(c *C) {
	cases := []struct {
		label string
		snap  string
		app   string
	}{
		{
			label: "snap.nextcloud.occ",
			snap:  "nextcloud",
			app:   "occ",
		},
		{
			label: "snap.lxd.lxc",
			snap:  "lxd",
			app:   "lxc",
		},
		{
			label: "snap.firefox.firefox",
			snap:  "firefox",
			app:   "firefox",
		},
	}
	for _, testCase := range cases {
		snap, app, err := common.LabelToSnapApp(testCase.label)
		c.Check(err, IsNil)
		c.Check(snap, Equals, testCase.snap)
		c.Check(app, Equals, testCase.app)
	}
}

func (s *commonSuite) TestLabelToSnapAppUnhappy(c *C) {
	cases := []string{
		"snap",
		"snap.nextcloud",
		"nextcloud.occ",
		"snap.nextcloud.nextcloud.occ",
		"SNAP.NEXTCLOUD.OCC",
	}
	for _, label := range cases {
		snap, app, err := common.LabelToSnapApp(label)
		c.Check(err, Equals, common.ErrInvalidSnapLabel)
		c.Check(snap, Equals, label)
		c.Check(app, Equals, label)
	}
}

func (s *commonSuite) TestSelectSingleInterface(c *C) {
	defaultInterface := "other"
	fakeIface := "foo"
	c.Check(common.SelectSingleInterface([]string{}), Equals, defaultInterface, Commentf("input: []string{}"))
	c.Check(common.SelectSingleInterface([]string{""}), Equals, defaultInterface, Commentf(`input: []string{""}`))
	c.Check(common.SelectSingleInterface([]string{fakeIface}), Equals, defaultInterface, Commentf(`input: []string{""}`))
	for iface := range common.InterfacePriorities {
		c.Check(common.SelectSingleInterface([]string{iface}), Equals, iface)
		fakeList := []string{iface, fakeIface}
		c.Check(common.SelectSingleInterface(fakeList), Equals, iface)
		fakeList = []string{fakeIface, iface}
		c.Check(common.SelectSingleInterface(fakeList), Equals, iface)
	}
	c.Check(common.SelectSingleInterface([]string{"home", "camera", "foo"}), Equals, "home")
}

func (s *commonSuite) TestPermissionMaskToPermissionsList(c *C) {
	cases := []struct {
		mask notify.FilePermission
		list []common.PermissionType
	}{
		{
			notify.FilePermission(0),
			[]common.PermissionType{},
		},
		{
			notify.AA_MAY_EXEC,
			[]common.PermissionType{common.PermissionExecute},
		},
		{
			notify.AA_MAY_WRITE,
			[]common.PermissionType{common.PermissionWrite},
		},
		{
			notify.AA_MAY_READ,
			[]common.PermissionType{common.PermissionRead},
		},
		{
			notify.AA_MAY_APPEND,
			[]common.PermissionType{common.PermissionAppend},
		},
		{
			notify.AA_MAY_CREATE,
			[]common.PermissionType{common.PermissionCreate},
		},
		{
			notify.AA_MAY_DELETE,
			[]common.PermissionType{common.PermissionDelete},
		},
		{
			notify.AA_MAY_OPEN,
			[]common.PermissionType{common.PermissionOpen},
		},
		{
			notify.AA_MAY_RENAME,
			[]common.PermissionType{common.PermissionRename},
		},
		{
			notify.AA_MAY_SETATTR,
			[]common.PermissionType{common.PermissionSetAttr},
		},
		{
			notify.AA_MAY_GETATTR,
			[]common.PermissionType{common.PermissionGetAttr},
		},
		{
			notify.AA_MAY_SETCRED,
			[]common.PermissionType{common.PermissionSetCred},
		},
		{
			notify.AA_MAY_GETCRED,
			[]common.PermissionType{common.PermissionGetCred},
		},
		{
			notify.AA_MAY_CHMOD,
			[]common.PermissionType{common.PermissionChangeMode},
		},
		{
			notify.AA_MAY_CHOWN,
			[]common.PermissionType{common.PermissionChangeOwner},
		},
		{
			notify.AA_MAY_CHGRP,
			[]common.PermissionType{common.PermissionChangeGroup},
		},
		{
			notify.AA_MAY_LOCK,
			[]common.PermissionType{common.PermissionLock},
		},
		{
			notify.AA_EXEC_MMAP,
			[]common.PermissionType{common.PermissionExecuteMap},
		},
		{
			notify.AA_MAY_LINK,
			[]common.PermissionType{common.PermissionLink},
		},
		{
			notify.AA_MAY_ONEXEC,
			[]common.PermissionType{common.PermissionChangeProfileOnExec},
		},
		{
			notify.AA_MAY_CHANGE_PROFILE,
			[]common.PermissionType{common.PermissionChangeProfile},
		},
		{
			notify.AA_MAY_READ | notify.AA_MAY_WRITE | notify.AA_MAY_EXEC,
			[]common.PermissionType{common.PermissionExecute, common.PermissionWrite, common.PermissionRead},
		},
	}
	for _, testCase := range cases {
		perms, err := common.PermissionMaskToPermissionsList(testCase.mask)
		c.Assert(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Assert(perms, DeepEquals, testCase.list)
	}

	unrecognizedFilePerm := notify.FilePermission(1 << 17)
	perms, err := common.PermissionMaskToPermissionsList(unrecognizedFilePerm)
	c.Assert(err, Equals, common.ErrUnrecognizedFilePermission)
	c.Assert(perms, HasLen, 0)

	mixed := unrecognizedFilePerm | notify.AA_MAY_READ | notify.AA_MAY_WRITE
	expected := []common.PermissionType{common.PermissionWrite, common.PermissionRead}
	perms, err = common.PermissionMaskToPermissionsList(mixed)
	c.Assert(err, Equals, common.ErrUnrecognizedFilePermission)
	c.Assert(perms, DeepEquals, expected)
}

func (s *commonSuite) TestPermissionsListContains(c *C) {
	permissionsList := []common.PermissionType{
		common.PermissionExecute,
		common.PermissionWrite,
		common.PermissionRead,
		common.PermissionAppend,
		common.PermissionOpen,
	}
	for _, perm := range []common.PermissionType{
		common.PermissionExecute,
		common.PermissionWrite,
		common.PermissionRead,
		common.PermissionAppend,
		common.PermissionOpen,
	} {
		c.Check(common.PermissionsListContains(permissionsList, perm), Equals, true)
	}
	for _, perm := range []common.PermissionType{
		common.PermissionCreate,
		common.PermissionDelete,
		common.PermissionRename,
		common.PermissionChangeOwner,
		common.PermissionChangeGroup,
	} {
		c.Check(common.PermissionsListContains(permissionsList, perm), Equals, false)
	}
}

func (s *commonSuite) TestOutcomeAsBool(c *C) {
	result, err := common.OutcomeAllow.AsBool()
	c.Check(err, IsNil)
	c.Check(result, Equals, true)
	result, err = common.OutcomeDeny.AsBool()
	c.Check(err, IsNil)
	c.Check(result, Equals, false)
	_, err = common.OutcomeUnset.AsBool()
	c.Check(err, Equals, common.ErrInvalidOutcome)
	_, err = common.OutcomeType("foo").AsBool()
	c.Check(err, Equals, common.ErrInvalidOutcome)
}

func (s *commonSuite) TestValidateOutcome(c *C) {
	c.Assert(common.ValidateOutcome(common.OutcomeAllow), Equals, nil)
	c.Assert(common.ValidateOutcome(common.OutcomeDeny), Equals, nil)
	c.Assert(common.ValidateOutcome(common.OutcomeUnset), Equals, common.ErrInvalidOutcome)
	c.Assert(common.ValidateOutcome(common.OutcomeType("foo")), Equals, common.ErrInvalidOutcome)
}

func (s *commonSuite) TestValidateLifespanParseDuration(c *C) {
	unsetDuration := ""
	invalidDuration := "foo"
	negativeDuration := "-5s"
	validDuration := "10m"
	parsedValidDuration, err := time.ParseDuration(validDuration)
	c.Assert(err, IsNil)

	for _, lifespan := range []common.LifespanType{
		common.LifespanForever,
		common.LifespanSession,
		common.LifespanSingle,
	} {
		expiration, err := common.ValidateLifespanParseDuration(lifespan, unsetDuration)
		c.Check(expiration, Equals, "")
		c.Check(err, IsNil)
		for _, dur := range []string{invalidDuration, negativeDuration, validDuration} {
			expiration, err = common.ValidateLifespanParseDuration(lifespan, dur)
			c.Check(expiration, Equals, "")
			c.Check(err, Equals, common.ErrInvalidDurationForLifespan)
		}
	}

	expiration, err := common.ValidateLifespanParseDuration(common.LifespanTimespan, unsetDuration)
	c.Check(expiration, Equals, "")
	c.Check(err, Equals, common.ErrInvalidDurationEmpty)

	expiration, err = common.ValidateLifespanParseDuration(common.LifespanTimespan, invalidDuration)
	c.Check(expiration, Equals, "")
	c.Check(err, Equals, common.ErrInvalidDurationParseError)

	expiration, err = common.ValidateLifespanParseDuration(common.LifespanTimespan, negativeDuration)
	c.Check(expiration, Equals, "")
	c.Check(err, Equals, common.ErrInvalidDurationNegative)

	expiration, err = common.ValidateLifespanParseDuration(common.LifespanTimespan, validDuration)
	c.Check(err, Equals, nil)
	expirationTime, err := time.Parse(time.RFC3339, expiration)
	c.Check(err, IsNil)
	c.Check(expirationTime.After(time.Now()), Equals, true)
	c.Check(expirationTime.Before(time.Now().Add(parsedValidDuration)), Equals, true)
}

func (s *commonSuite) TestValidateConstraintsOutcomeLifespanDuration(c *C) {
	goodInterface := "home"
	badInterface := "foo"
	goodConstraints := &common.Constraints{
		PathPattern: "/path/to/something",
		Permissions: []common.PermissionType{common.PermissionRead},
	}
	// badConstraints := &common.Constraints{
	//	PathPattern: "bad\\path",
	//	Permissions: []common.PermissionType{common.PermissionRead},
	// }
	goodOutcome := common.OutcomeAllow
	badOutcome := common.OutcomeUnset
	goodLifespan := common.LifespanTimespan
	badLifespan := common.LifespanUnset
	goodDuration := "10s"
	badDuration := "foo"

	_, err := common.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, goodOutcome, goodLifespan, goodDuration)
	c.Check(err, IsNil)
	_, err = common.ValidateConstraintsOutcomeLifespanDuration(badInterface, goodConstraints, goodOutcome, goodLifespan, goodDuration)
	c.Check(err, NotNil)
	// TODO: add this once PR #13730 is merged:
	// _, err = common.ValidateConstraintsOutcomeLifespanDuration(goodInterface, badConstraints, goodOutcome, goodLifespan, goodDuration)
	// c.Check(err, Equals, common.ErrInvalidPathPattern)
	_, err = common.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, badOutcome, goodLifespan, goodDuration)
	c.Check(err, Equals, common.ErrInvalidOutcome)
	_, err = common.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, goodOutcome, badLifespan, goodDuration)
	c.Check(err, Equals, common.ErrInvalidLifespan)
	_, err = common.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, goodOutcome, goodLifespan, badDuration)
	c.Check(err, Equals, common.ErrInvalidDurationParseError)
}
