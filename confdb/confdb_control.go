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

package confdb

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	validAccountID = regexp.MustCompile("^(?:[a-z0-9A-Z]{32}|[-a-z0-9]{2,28})$")
)

// AuthenticationMethod limits what keys can be used to sign messages used to remotely
// manage confdbs.
type AuthenticationMethod string

const (
	// Only the operator's keys can be used to sign the messages.
	OperatorKey AuthenticationMethod = "operator-key"
	// Messages can be signed on behalf of the operator by the store.
	Store AuthenticationMethod = "store"
)

// isValidAuthenticationMethod checks if a string is a valid AuthenticationMethod.
func isValidAuthenticationMethod(value string) bool {
	switch AuthenticationMethod(value) {
	case OperatorKey, Store:
		return true
	default:
		return false
	}
}

// convertToAuthenticationMethods converts []string to []AuthenticationMethod and validates it.
func convertToAuthenticationMethods(methods []string) ([]AuthenticationMethod, error) {
	sort.Slice(methods, func(i, j int) bool {
		return methods[i] < methods[j]
	})

	// remove duplicates
	methods = unique(methods)

	var result []AuthenticationMethod
	for _, method := range methods {
		if !isValidAuthenticationMethod(method) {
			return nil, fmt.Errorf("invalid authentication method: %s", method)
		}
		result = append(result, AuthenticationMethod(method))
	}
	return result, nil
}

// Operator holds the delegations for a single operator.
type Operator struct {
	ID     string
	Groups []*ControlGroup
}

// ControlGroup holds a set of views delegated through the given authentication.
type ControlGroup struct {
	Authentication []AuthenticationMethod
	Views          []*ViewRef
}

// findView binary searches the position of the given view in the control group.
func (g *ControlGroup) findView(view *ViewRef) (int, bool) {
	left, right := 0, len(g.Views)-1

	for left <= right {
		mid := (left + right) / 2
		cmp := g.Views[mid].compare(view)

		if cmp == 0 {
			return mid, true
		} else if cmp < 0 {
			left = mid + 1
		} else {
			right = mid - 1
		}
	}

	return 0, false
}

// deleteViewAt removes the view at the given index.
func (g *ControlGroup) deleteViewAt(idx int) {
	g.Views = append(g.Views[:idx], g.Views[idx+1:]...)
}

// ViewRef holds the reference to account/confdb/view as parsed from the
// confdb-control assertion.
type ViewRef struct {
	Account string
	Confdb  string
	View    string
}

// newViewRef parses account/confdb/view into ViewRef
func newViewRef(view string) (*ViewRef, error) {
	viewPath := strings.Split(view, "/")
	if len(viewPath) != 3 {
		return nil, fmt.Errorf(`view "%s" must be in the format account/confdb/view`, view)
	}

	account := viewPath[0]
	if !validAccountID.MatchString(account) {
		return nil, fmt.Errorf("invalid Account ID %s", account)
	}

	confdb := viewPath[1]
	if !ValidConfdbName.MatchString(confdb) {
		return nil, fmt.Errorf("invalid confdb name %s", confdb)
	}

	viewName := viewPath[2]
	if !ValidViewName.MatchString(viewName) {
		return nil, fmt.Errorf("invalid view name %s", viewName)
	}

	return &ViewRef{
		Account: account,
		Confdb:  confdb,
		View:    viewName,
	}, nil
}

// String returns the string representation of the ViewRef.
func (v *ViewRef) String() string {
	return fmt.Sprintf("%s/%s/%s", v.Account, v.Confdb, v.View)
}

// compare compares two ViewRefs lexicographically based the Account, Confdb, & View field.
func (v *ViewRef) compare(b *ViewRef) int {
	if v.Account != b.Account {
		if v.Account < b.Account {
			return -1
		}
		return 1
	}

	if v.Confdb != b.Confdb {
		if v.Confdb < b.Confdb {
			return -1
		}
		return 1
	}

	if v.View != b.View {
		if v.View < b.View {
			return -1
		}
		return 1
	}

	return 0
}

// groupWithView returns the group that holds the given view.
func (op *Operator) groupWithView(view *ViewRef) (*ControlGroup, int) {
	for _, group := range op.Groups {
		index, ok := group.findView(view)
		if ok {
			return group, index
		}
	}

	return nil, 0
}

// groupWithAuthentication returns the group with the given auth.
// The provided auth should be sorted.
func (op *Operator) groupWithAuthentication(auth []AuthenticationMethod) *ControlGroup {
	for _, group := range op.Groups {
		if checkListEqual(group.Authentication, auth) {
			return group
		}
	}

	return nil
}

// IsDelegated checks if the view is delegated to the operator with the given auth.
func (op *Operator) IsDelegated(view string, rawAuth []string) (bool, error) {
	parsedView, err := newViewRef(view)
	if err != nil {
		return false, err
	}

	auth, err := convertToAuthenticationMethods(rawAuth)
	if err != nil {
		return false, err
	}

	group, _ := op.groupWithView(parsedView)
	if group == nil {
		return false, nil
	}

	i, j := 0, 0
	for i < len(auth) && j < len(group.Authentication) {
		if auth[i] == group.Authentication[j] {
			i++
			j++
		} else if auth[i] > group.Authentication[j] {
			j++
		} else {
			return false, nil
		}
	}

	return i == len(auth), nil
}

// AddControlGroup adds the group to an operator under the given authentication.
func (op *Operator) Delegate(views, rawAuth []string) error {
	if len(rawAuth) == 0 {
		return errors.New(`cannot delegate: "auth" must be a non-empty list`)
	}

	auth, err := convertToAuthenticationMethods(rawAuth)
	if err != nil {
		return fmt.Errorf("cannot delegate: %w", err)
	}

	if len(views) == 0 {
		return errors.New(`cannot delegate: "views" must be a non-empty list`)
	}

	for _, view := range views {
		parsedView, err := newViewRef(view)
		if err != nil {
			return fmt.Errorf("cannot delegate: %w", err)
		}

		op.delegateOne(parsedView, auth)
	}

	op.compact()
	return nil
}

// delegateOne grants remote control to the view.
func (op *Operator) delegateOne(view *ViewRef, auth []AuthenticationMethod) {
	newAuth := auth
	existingGroup, viewIdx := op.groupWithView(view)
	if existingGroup != nil {
		newAuth = append(newAuth, existingGroup.Authentication...)
		sort.Slice(newAuth, func(i, j int) bool {
			return newAuth[i] < newAuth[j]
		})
		newAuth = unique(newAuth)
	}

	newGroup := op.groupWithAuthentication(newAuth)
	if existingGroup == newGroup && existingGroup != nil {
		// already delegated, nothing to do
		return
	}

	if newGroup == nil {
		newGroup = &ControlGroup{Authentication: newAuth, Views: []*ViewRef{view}}
		op.Groups = append(op.Groups, newGroup)
	} else {
		newGroup.Views = append(newGroup.Views, view)
		sort.Slice(newGroup.Views, func(i, j int) bool {
			return newGroup.Views[i].compare(newGroup.Views[j]) < 0
		})
	}

	if existingGroup != nil {
		existingGroup.deleteViewAt(viewIdx) // remove the view from the old group
	}
}

// Revoke withdraws remote access to the views that have been delegated with the given auth.
func (op *Operator) Revoke(views []string, rawAuth []string) error {
	var err error
	var auth []AuthenticationMethod
	if len(rawAuth) == 0 {
		// if no authentication is provided, revoke all auth methods
		auth = []AuthenticationMethod{OperatorKey, Store}
	} else {
		auth, err = convertToAuthenticationMethods(rawAuth)
		if err != nil {
			return fmt.Errorf("cannot revoke: %w", err)
		}
	}

	var parsedViews []*ViewRef
	if len(views) == 0 {
		for _, group := range op.Groups {
			parsedViews = append(parsedViews, group.Views...)
		}
	} else {
		for _, view := range views {
			parsedView, err := newViewRef(view)
			if err != nil {
				return fmt.Errorf("cannot revoke: %w", err)
			}
			parsedViews = append(parsedViews, parsedView)
		}
	}

	for _, view := range parsedViews {
		op.revokeOne(view, auth)
	}

	op.compact()
	return nil
}

// revokeOne revokes remote control over the view.
func (op *Operator) revokeOne(view *ViewRef, auth []AuthenticationMethod) {
	group, viewIdx := op.groupWithView(view)
	if group == nil {
		// not delegated, nothing to do
		return
	}

	remaining := make([]AuthenticationMethod, 0, len(group.Authentication))
	for _, existingAuth := range group.Authentication {
		if !checkListContains(auth, existingAuth) {
			remaining = append(remaining, existingAuth)
		}
	}

	group.deleteViewAt(viewIdx) // remove the view from the group

	if len(remaining) > 0 {
		// delegate the view with the remaining authentication method(s)
		op.delegateOne(view, remaining)
	}
}

// compact removes empty groups.
func (op *Operator) compact() {
	groups := make([]*ControlGroup, 0, len(op.Groups))
	for _, group := range op.Groups {
		if len(group.Views) != 0 {
			groups = append(groups, group)
		}
	}

	op.Groups = groups
}

// unique replaces consecutive runs of equal elements with a single copy.
// The provided slice s should be sorted.
func unique[T comparable](s []T) []T {
	if len(s) < 2 {
		return s
	}

	j := 1
	for i := 1; i < len(s); i++ {
		if s[i] != s[i-1] {
			s[j] = s[i]
			j++
		}
	}

	return s[:j]
}

// checkListContains checks if the slice contains the given value.
func checkListContains[T comparable](s []T, v T) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}

	return false
}

// checkListEqual checks if two slices are equal.
func checkListEqual[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
