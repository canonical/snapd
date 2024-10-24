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

package registry

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var (
	validAccountID = regexp.MustCompile("^(?:[a-z0-9A-Z]{32}|[-a-z0-9]{2,28})$")
)

type AuthenticationMethod int

const (
	OperatorKey AuthenticationMethod = iota
	Store
)

var authMethodMap = map[string]AuthenticationMethod{
	"operator-key": OperatorKey,
	"store":        Store,
}

// StringToAuthenticationMethod converts a string to AuthenticationMethod.
func StringToAuthenticationMethod(s string) (AuthenticationMethod, error) {
	if method, ok := authMethodMap[s]; ok {
		return method, nil
	}
	return -1, fmt.Errorf("unknown authentication method: %s", s)
}

var authMethodStringMap = map[AuthenticationMethod]string{
	OperatorKey: "operator-key",
	Store:       "store",
}

// String prints the string representation of AuthenticationMethod.
func (auth AuthenticationMethod) String() string {
	if s, ok := authMethodStringMap[auth]; ok {
		return s
	}
	return "unknown"
}

// Operator holds the delegations for a single operator.
type Operator struct {
	OperatorID string
	Groups     []*Group
}

// Group holds a set of views delegated through the given authentication.
type Group struct {
	Authentication []AuthenticationMethod
	Views          []string
}

// groupWithView returns the group that holds the given view.
func (operator *Operator) groupWithView(view string) (*Group, int) {
	for _, group := range operator.Groups {
		index, ok := slices.BinarySearch(group.Views, view)
		if ok {
			return group, index
		}
	}

	return nil, 0
}

// groupWithAuthentication returns the group with the given authentication.
// The authentication should be sorted.
func (operator *Operator) groupWithAuthentication(authentication []AuthenticationMethod) *Group {
	for _, group := range operator.Groups {
		if slices.Equal(group.Authentication, authentication) {
			return group
		}
	}

	return nil
}

// IsDelegated checks if <accountID>/<registry>/<view> is delegated to
// the operator under the authentication method <auth>.
func (operator *Operator) IsDelegated(view string, auth AuthenticationMethod) bool {
	group, _ := operator.groupWithView(view)
	if group == nil {
		return false
	}

	return slices.Contains(group.Authentication, auth)
}

// Delegate delegates the given views under the provided authentication methods.
func (operator *Operator) Delegate(views []string, authentication []AuthenticationMethod) error {
	if len(views) == 0 {
		return fmt.Errorf(`"views" must be a non-empty list`)
	}

	if len(authentication) == 0 {
		return fmt.Errorf(`"authentication" must be a non-empty list`)
	}
	slices.Sort(authentication)

	var err error
	for _, view := range views {
		viewPath := strings.Split(view, "/")
		if len(viewPath) != 3 {
			return fmt.Errorf(`"%s" must be in the format account/registry/view`, view)
		}

		if !validAccountID.MatchString(viewPath[0]) {
			return fmt.Errorf("invalid Account ID %s", viewPath[0])
		}

		if !ValidRegistryName.MatchString(viewPath[1]) {
			return fmt.Errorf("invalid registry name %s", viewPath[1])
		}

		if !ValidViewName.MatchString(viewPath[2]) {
			return fmt.Errorf("invalid view name %s", viewPath[2])
		}

		err = operator.delegateOne(view, authentication)
		if err != nil {
			return err
		}
	}

	operator.compact()
	return nil
}

// delegateOne grants remote registry control to <account-id>/<registry>/<view>.
func (operator *Operator) delegateOne(view string, authentication []AuthenticationMethod) error {
	newAuth := authentication
	existingGroup, viewIdx := operator.groupWithView(view)
	if existingGroup != nil {
		newAuth = append(newAuth, existingGroup.Authentication...)
		slices.Sort(newAuth)
		newAuth = slices.Compact(newAuth)
	}

	newGroup := operator.groupWithAuthentication(newAuth)
	if existingGroup == newGroup && existingGroup != nil {
		// already delegated, nothing to do
		return nil
	}

	if newGroup == nil {
		newGroup = &Group{Authentication: newAuth, Views: []string{view}}
		operator.Groups = append(operator.Groups, newGroup)
	} else {
		newGroup.Views = append(newGroup.Views, view)
		slices.Sort(newGroup.Views)
	}

	if existingGroup != nil {
		// remove the view from the old group
		existingGroup.Views = slices.Delete(existingGroup.Views, viewIdx, viewIdx+1)
	}

	return nil
}

// Revoke withdraws remote access to the views that have been delegated under
// the authentication methods.
func (operator *Operator) Revoke(views []string, authentication []AuthenticationMethod) error {
	if len(authentication) == 0 {
		// if no authentication is provided, revoke all auth methods
		authentication = []AuthenticationMethod{OperatorKey, Store}
	}
	slices.Sort(authentication)

	var err error
	for _, view := range views {
		err = operator.revokeOne(view, authentication)
		if err != nil {
			return err
		}
	}

	operator.compact()
	return nil
}

// revokeOne revokes remote registry control over <account-id>/<registry>/<view>.
func (operator *Operator) revokeOne(view string, authentication []AuthenticationMethod) error {
	group, viewIdx := operator.groupWithView(view)
	if group == nil {
		// not delegated, nothing to do
		return nil
	}

	remaining := make([]AuthenticationMethod, 0, len(group.Authentication))
	for _, existingAuth := range group.Authentication {
		if !slices.Contains(authentication, existingAuth) {
			remaining = append(remaining, existingAuth)
		}
	}

	// remove the view from the group
	group.Views = slices.Delete(group.Views, viewIdx, viewIdx+1)

	if len(remaining) != 0 {
		// delegate the view with the remaining authentication method(s)
		return operator.delegateOne(view, remaining)
	}

	return nil
}

// compact removes empty groups.
func (operator *Operator) compact() {
	groups := make([]*Group, 0, len(operator.Groups))
	for _, group := range operator.Groups {
		if len(group.Views) != 0 {
			groups = append(groups, group)
		}
	}

	operator.Groups = groups
}
