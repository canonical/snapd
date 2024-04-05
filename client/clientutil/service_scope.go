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

package clientutil

import (
	"fmt"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/strutil"
)

// ServiceScopeOptions represents shared options between service operations
// that change the scope of services affected.
type ServiceScopeOptions struct {
	System    bool   `long:"system"`
	User      bool   `long:"user"`
	Usernames string `long:"users"`
}

func (us *ServiceScopeOptions) Validate() error {
	switch {
	case us.System && us.User:
		return fmt.Errorf("--system and --user cannot be used in conjunction with each other")
	case us.Usernames != "" && us.User:
		return fmt.Errorf("--user and --users cannot be used in conjunction with each other")
	case us.Usernames != "" && us.Usernames != "all":
		return fmt.Errorf("only \"all\" is supported as a value for --users")
	}
	return nil
}

func (us *ServiceScopeOptions) Scope() client.ScopeSelector {
	switch {
	case (us.User || us.Usernames != "") && !us.System:
		return client.ScopeSelector([]string{"user"})
	case !(us.User || us.Usernames != "") && us.System:
		return client.ScopeSelector([]string{"system"})
	}
	return nil
}

func (us *ServiceScopeOptions) Users() client.UserSelector {
	switch {
	case us.User:
		return client.UserSelector{
			Selector: client.UserSelectionSelf,
		}
	case us.Usernames == "all":
		return client.UserSelector{
			Selector: client.UserSelectionAll,
		}
	}
	// Currently not reachable as us.Usernames can only be 'all' for now, but when
	// we introduce support for lists of usernames, this will be hit.
	return client.UserSelector{
		Selector: client.UserSelectionList,
		Names:    strutil.CommaSeparatedList(us.Usernames),
	}
}
