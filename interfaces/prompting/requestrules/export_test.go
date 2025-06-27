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

package requestrules

import (
	"time"

	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/testutil"
)

var (
	ErrNoUserSession   = errNoUserSession
	JoinInternalErrors = joinInternalErrors
	UserSessionPath    = userSessionPath
)

type RulesDBJSON rulesDBJSON

func MockUserSessionIDXattr() (xattr string, restore func()) {
	// Test code doesn't have CAP_SYS_ADMIN, so replace the "trusted" namespace
	// with "user" for the sake of testing.
	testXattr := "user.snapd_user_session_id"
	restore = testutil.Mock(&userSessionIDXattr, testXattr)
	return testXattr, restore
}

func (rule *Rule) Validate(currTime time.Time) (expired bool, err error) {
	return rule.validate(currTime)
}

func (rdb *RuleDB) IsPathPermAllowed(user uint32, snap string, iface string, path string, permission string) (bool, error) {
	return rdb.isPathPermAllowed(user, snap, iface, path, permission)
}

func (rdb *RuleDB) ReadOrAssignUserSessionID(user uint32) (userSessionID prompting.IDType, err error) {
	return rdb.readOrAssignUserSessionID(user)
}

func MockIsPathPermAllowed(f func(rdb *RuleDB, user uint32, snap string, iface string, path string, permission string) (bool, error)) func() {
	return testutil.Mock(&isPathPermAllowedByRuleDB, f)
}
