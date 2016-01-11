// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package caps

const (
	INITIAL = iota
	GRANTED = iota
	REVOKED = iota
)

type mockSecurity struct {
	SecurityName   string
	StateMap       map[string]int
	GrantErrorMap  map[string]error
	RevokeErrorMap map[string]error
}

func (sec *mockSecurity) GrantPermissions(snapName string, cap *Capability) error {
	if err := sec.GrantErrorMap[snapName]; err != nil {
		return err
	}
	if sec.StateMap == nil {
		sec.StateMap = make(map[string]int)
	}
	sec.StateMap[snapName] = GRANTED
	return nil
}

func (sec *mockSecurity) RevokePermissions(snapName string, cap *Capability) error {
	if err := sec.RevokeErrorMap[snapName]; err != nil {
		return err
	}
	if sec.StateMap == nil {
		sec.StateMap = make(map[string]int)
	}
	sec.StateMap[snapName] = REVOKED
	return nil
}

func (sec *mockSecurity) SetGrantPermissionsError(snapName string, err error) {
	if sec.GrantErrorMap == nil {
		sec.GrantErrorMap = make(map[string]error)
	}
	sec.GrantErrorMap[snapName] = err
}

func (sec *mockSecurity) SetRevokePermissionsError(snapName string, err error) {
	if sec.RevokeErrorMap == nil {
		sec.RevokeErrorMap = make(map[string]error)
	}
	sec.RevokeErrorMap[snapName] = err
}
