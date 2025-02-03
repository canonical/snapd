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
	"strings"
)

var (
	validAccountID = regexp.MustCompile("^(?:[a-z0-9A-Z]{32}|[-a-z0-9]{2,28})$")
)

// Authentication limits what keys can be used to sign messages used to remotely manage confdbs.
type Authentication uint8

const (
	// Only the operator's keys can be used to sign the messages.
	OperatorKey Authentication = 1 << iota
	// Messages can be signed on behalf of the operator by the store.
	Store
)

const (
	AllAuth Authentication = OperatorKey | Store
)

// ConvertStringsToAuthentication converts []string to Authentication and validates it.
func ConvertStringsToAuthentication(methods []string) (Authentication, error) {
	var auth Authentication
	for _, method := range methods {
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

// ConvertAuthenticationToStrings converts Authentication to a SORTED []string.
func ConvertAuthenticationToStrings(auth Authentication) []string {
	keys := []string{}
	if auth&OperatorKey == OperatorKey {
		keys = append(keys, "operator-key")
	}

	if auth&Store == Store {
		keys = append(keys, "store")
	}

	return keys
}

// Operator holds the delegations for a single operator.
type Operator struct {
	ID          string
	Delegations map[ViewRef]Authentication
}

// ViewRef holds the reference to account/confdb/view as parsed from the
// confdb-control assertion.
type ViewRef struct {
	Account string
	Confdb  string
	View    string
}

// String returns the string representation of the ViewRef.
func (v *ViewRef) String() string {
	return fmt.Sprintf("%s/%s/%s", v.Account, v.Confdb, v.View)
}

// convertToViewRefs converts []string to []ViewRef and validates it.
func convertToViewRefs(views []string) ([]ViewRef, error) {
	var result []ViewRef
	for _, view := range views {
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

		result = append(result, ViewRef{Account: account, Confdb: confdb, View: viewName})
	}

	return result, nil
}

// Delegate grants remote access to the views under the given auth.
func (op *Operator) Delegate(views, rawAuth []string) error {
	if len(rawAuth) == 0 {
		return errors.New(`cannot delegate: "authentications" must be a non-empty list`)
	}

	auth, err := ConvertStringsToAuthentication(rawAuth)
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
		op.Delegations = map[ViewRef]Authentication{}
	}

	for _, viewRef := range viewRefs {
		op.Delegations[viewRef] |= auth
	}

	return nil
}

// Undelegate withdraws remote access to the views that have been delegated with the given auth.
func (op *Operator) Undelegate(views, rawAuth []string) error {
	auth := AllAuth // if no authentication is provided, revoke all auth methods
	var err error
	if len(rawAuth) > 0 {
		auth, err = ConvertStringsToAuthentication(rawAuth)
		if err != nil {
			return fmt.Errorf("cannot undelegate: %w", err)
		}
	}

	var viewRefs []ViewRef
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

// IsDelegated checks if the view is delegated to the operator with the given auth.
func (op *Operator) IsDelegated(view string, rawAuth []string) (bool, error) {
	viewRefs, err := convertToViewRefs([]string{view})
	if err != nil {
		return false, err
	}

	auth, err := ConvertStringsToAuthentication(rawAuth)
	if err != nil {
		return false, err
	}

	delegatedWith := op.Delegations[viewRefs[0]]
	return delegatedWith&auth == auth, nil
}
