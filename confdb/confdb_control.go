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

// ViewRef holds the reference to account/confdb/view as parsed from the
// confdb-control assertion.
type ViewRef struct {
	Account string
	Confdb  string
	View    string
}

// AddControlGroup adds the group to an operator under the given authentication.
func (op *Operator) AddControlGroup(views, auth []string) error {
	if len(auth) == 0 {
		return errors.New(`cannot add group: "auth" must be a non-empty list`)
	}

	authentication, err := convertToAuthenticationMethods(auth)
	if err != nil {
		return fmt.Errorf("cannot add group: %w", err)
	}

	if len(views) == 0 {
		return errors.New(`cannot add group: "views" must be a non-empty list`)
	}

	parsedViews := []*ViewRef{}
	for _, view := range views {
		viewPath := strings.Split(view, "/")
		if len(viewPath) != 3 {
			return fmt.Errorf(`view "%s" must be in the format account/confdb/view`, view)
		}

		account := viewPath[0]
		if !validAccountID.MatchString(account) {
			return fmt.Errorf("invalid Account ID %s", account)
		}

		confdb := viewPath[1]
		if !ValidConfdbName.MatchString(confdb) {
			return fmt.Errorf("invalid confdb name %s", confdb)
		}

		viewName := viewPath[2]
		if !ValidViewName.MatchString(viewName) {
			return fmt.Errorf("invalid view name %s", viewName)
		}

		parsedView := &ViewRef{
			Account: account,
			Confdb:  confdb,
			View:    viewName,
		}
		parsedViews = append(parsedViews, parsedView)
	}

	group := &ControlGroup{
		Authentication: authentication,
		Views:          parsedViews,
	}
	op.Groups = append(op.Groups, group)

	return nil
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
