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

// Control holds the delegations done by the device to operators.
type Control struct {
	// the key is the operator ID
	operators map[string]*operator
}

// Delegate delegates the given views with the provided authentication methods to the operator.
func (cc *Control) Delegate(operatorID string, views, authMeth []string) error {
	if !validAccountID.MatchString(operatorID) {
		return fmt.Errorf("invalid operator ID: %s", operatorID)
	}

	if cc.operators == nil {
		cc.operators = make(map[string]*operator)
	}

	op, ok := cc.operators[operatorID]
	if !ok {
		op = &operator{ID: operatorID}
	}

	err := op.delegate(views, authMeth)
	if err != nil {
		return err
	}

	cc.operators[operatorID] = op
	return nil
}

// Undelegate withdraws access to the views that have been delegated with the provided authentication methods.
func (cc *Control) Undelegate(operatorID string, views, authMeth []string) error {
	op, ok := cc.operators[operatorID]
	if !ok {
		return nil // nothing is delegated to this operator
	}

	err := op.undelegate(views, authMeth)
	if err != nil {
		return err
	}

	if len(op.Delegations) == 0 {
		delete(cc.operators, operatorID)
	}

	return nil
}

// IsDelegated checks if the view is delegated to the operator with the given authentication methods.
func (cc *Control) IsDelegated(operatorID, view string, authMeth []string) (bool, error) {
	op, ok := cc.operators[operatorID]
	if !ok {
		return false, nil // nothing is delegated to this operator
	}

	return op.isDelegated(view, authMeth)
}

// Groups returns the groups in a format that can be used to assemble the next revision
// of the confdb-control assertion.
func (cc *Control) Groups() []any {
	// Group operators by authentication and views
	// i.e. authentication > views > operator-ids
	authMap := map[authentication]map[viewRef][]string{}
	// auths will accumulate the authentication methods in use
	var auths []authentication

	// Group operators by auth and view
	for _, op := range cc.operators {
		for view, auth := range op.Delegations {
			if _, exists := authMap[auth]; !exists {
				authMap[auth] = map[viewRef][]string{}
				auths = append(auths, auth)
			}

			authMap[auth][view] = append(authMap[auth][view], op.ID)
		}
	}

	// Sort auths for consistent output
	sort.Slice(auths, func(i, j int) bool { return auths[i] < auths[j] })

	var groups []any
	for _, auth := range auths {
		authStrs := auth.toStrings()

		// Group by unique operator sets
		opGroups := make(map[string][]string)

		for view, ops := range authMap[auth] {
			sort.Strings(ops)
			key := strings.Join(ops, ",")
			opGroups[key] = append(opGroups[key], view.String())
		}

		for ops, views := range opGroups {
			sort.Strings(views)
			groups = append(groups, map[string]any{
				"operators":       toInterfaceSlice(strings.Split(ops, ",")),
				"authentications": toInterfaceSlice(authStrs),
				"views":           toInterfaceSlice(views),
			})
		}
	}

	return groups
}

// Clone returns a deep copy of Control.
func (cc Control) Clone() Control {
	clone := Control{operators: make(map[string]*operator)}

	for id, op := range cc.operators {
		if op != nil {
			clone.operators[id] = op.clone()
		}
	}

	return clone
}

// authentication limits what keys can be used to sign messages used to remotely manage confdbs.
type authentication uint8

const (
	// Only the operator's keys can be used to sign the messages.
	OperatorKey authentication = 1 << iota
	// Messages can be signed on behalf of the operator by the store.
	Store
)

const (
	allAuth authentication = OperatorKey | Store
)

// newAuthentication converts []string to authentication and validates it.
func newAuthentication(authMeth []string) (authentication, error) {
	var auth authentication
	for _, method := range authMeth {
		switch method {
		case "operator-key":
			auth |= OperatorKey
		case "store":
			auth |= Store
		default:
			return 0, fmt.Errorf("invalid authentication method: %s", method)
		}
	}
	return auth, nil
}

// Convert authentication to a sorted []string.
func (a authentication) toStrings() []string {
	var keys []string
	if a&OperatorKey == OperatorKey {
		keys = append(keys, "operator-key")
	}

	if a&Store == Store {
		keys = append(keys, "store")
	}

	return keys
}

// operator holds the delegations for a single operator.
type operator struct {
	ID          string
	Delegations map[viewRef]authentication
}

// viewRef holds the reference to account/confdb/view as parsed from the
// confdb-control assertion.
type viewRef struct {
	Account string
	Confdb  string
	View    string
}

// String returns the string representation of the viewRef.
func (v *viewRef) String() string {
	return fmt.Sprintf("%s/%s/%s", v.Account, v.Confdb, v.View)
}

// convertToViewRefs converts []string to []viewRef and validates it.
func convertToViewRefs(views []string) ([]viewRef, error) {
	var result []viewRef
	for _, view := range views {
		viewPath := strings.Split(view, "/")
		if len(viewPath) != 3 {
			return nil, fmt.Errorf(`view "%s" must be in the format account/confdb/view`, view)
		}

		account := viewPath[0]
		if !validAccountID.MatchString(account) {
			return nil, fmt.Errorf("invalid account ID: %s", account)
		}

		confdb := viewPath[1]
		if !ValidConfdbName.MatchString(confdb) {
			return nil, fmt.Errorf("invalid confdb name: %s", confdb)
		}

		viewName := viewPath[2]
		if !ValidViewName.MatchString(viewName) {
			return nil, fmt.Errorf("invalid view name: %s", viewName)
		}

		result = append(result, viewRef{Account: account, Confdb: confdb, View: viewName})
	}

	return result, nil
}

// delegate grants remote access to the views under the given auth.
func (op *operator) delegate(views, authMeth []string) error {
	if len(authMeth) == 0 {
		return errors.New(`cannot delegate: "authentications" must be a non-empty list`)
	}

	auth, err := newAuthentication(authMeth)
	if err != nil {
		return fmt.Errorf("cannot delegate: %w", err)
	}

	if len(views) == 0 {
		return errors.New(`cannot delegate: "views" must be a non-empty list`)
	}

	viewRefs, err := convertToViewRefs(views)
	if err != nil {
		return fmt.Errorf("cannot delegate: %w", err)
	}

	if op.Delegations == nil {
		op.Delegations = map[viewRef]authentication{}
	}

	for _, viewRef := range viewRefs {
		op.Delegations[viewRef] |= auth
	}

	return nil
}

// undelegate withdraws remote access to the views that have been delegated with the given auth.
func (op *operator) undelegate(views, authMeth []string) error {
	auth := allAuth // if no authentication is provided, revoke all auth methods
	var err error
	if len(authMeth) > 0 {
		auth, err = newAuthentication(authMeth)
		if err != nil {
			return fmt.Errorf("cannot undelegate: %w", err)
		}
	}

	var viewRefs []viewRef
	if len(views) == 0 {
		// if no views are provided, operate on all views
		for viewRef := range op.Delegations {
			viewRefs = append(viewRefs, viewRef)
		}
	} else {
		viewRefs, err = convertToViewRefs(views)
		if err != nil {
			return fmt.Errorf("cannot undelegate: %w", err)
		}
	}

	for _, viewRef := range viewRefs {
		if _, exists := op.Delegations[viewRef]; exists {
			op.Delegations[viewRef] &= ^auth

			if op.Delegations[viewRef] == 0 { // all remote access removed
				delete(op.Delegations, viewRef)
			}
		}
	}

	return nil
}

// isDelegated checks if the view is delegated to the operator with the given auth.
func (op *operator) isDelegated(view string, authMeth []string) (bool, error) {
	viewRefs, err := convertToViewRefs([]string{view})
	if err != nil {
		return false, err
	}

	auth, err := newAuthentication(authMeth)
	if err != nil {
		return false, err
	}

	delegatedWith := op.Delegations[viewRefs[0]]
	return delegatedWith&auth == auth, nil
}

// clone returns a deep copy of the operator.
func (op operator) clone() *operator {
	clone := &operator{
		ID:          op.ID,
		Delegations: make(map[viewRef]authentication),
	}

	for view, auth := range op.Delegations {
		clone.Delegations[view] = auth
	}

	return clone
}

// toInterfaceSlice converts []string to []interface{}.
func toInterfaceSlice(strs []string) []any {
	result := make([]any, len(strs))
	for i, str := range strs {
		result[i] = str
	}
	return result
}
