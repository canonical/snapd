// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2024 Canonical Ltd
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

package ctlcmd

import (
	"fmt"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/strutil"
)

// userAndScopeOptions represents shared options between service operations
// that change the scope of services affected. This should more or less
// match what's in cmd/snap.
type userAndScopeOptions struct {
	System bool   `long:"system"`
	User   bool   `long:"user"`
	Users  string `long:"users"`
}

func (us *userAndScopeOptions) validateScopes() error {
	switch {
	case us.System && us.User:
		return fmt.Errorf("--system and --user cannot be used in conjunction with each other")
	case us.Users != "" && us.User:
		return fmt.Errorf("--user and --users cannot be used in conjunction with each other")
	case us.Users != "" && us.Users != "all":
		return fmt.Errorf("only \"all\" is supported as a value for --users")
	}
	return nil
}

func (us *userAndScopeOptions) serviceScope() client.ScopeSelector {
	switch {
	case (us.User || us.Users != "") && !us.System:
		return client.ScopeSelector([]string{"user"})
	case !(us.User || us.Users != "") && us.System:
		return client.ScopeSelector([]string{"system"})
	}
	return nil
}

func (us *userAndScopeOptions) serviceUsers() client.UserSelector {
	switch {
	case us.User:
		return client.UserSelector{
			Selector: client.UserSelectionSelf,
		}
	case us.Users == "all":
		return client.UserSelector{
			Selector: client.UserSelectionAll,
		}
	}
	return client.UserSelector{
		Selector: client.UserSelectionList,
		Names:    strutil.CommaSeparatedList(us.Users),
	}
}
