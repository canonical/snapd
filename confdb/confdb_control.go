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

type AuthenticationMethod string

const (
	OperatorKey AuthenticationMethod = "operator-key"
	Store       AuthenticationMethod = "store"
)

// isValidAuthenticationMethod checks if a string is a valid AuthenticationMethod
func isValidAuthenticationMethod(value string) bool {
	switch AuthenticationMethod(value) {
	case OperatorKey, Store:
		return true
	default:
		return false
	}
}

// convertToAuthenticationMethod converts and validates a []string to []AuthenticationMethod
func convertToAuthenticationMethod(methods []string) ([]AuthenticationMethod, error) {
	sort.Slice(methods, func(i, j int) bool {
		return methods[i] < methods[j]
	})

	// remove duplicates
	methods = compact(methods)

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
	Views          []string
}

// AddControlGroup adds the group to an operator under the given authentication.
func (op *Operator) AddControlGroup(views, auth []string) error {
	if len(auth) == 0 {
		return errors.New(`cannot add group: "auth" must be a non-empty list`)
	}

	authentication, err := convertToAuthenticationMethod(auth)
	if err != nil {
		return fmt.Errorf("cannot add group: %w", err)
	}

	if len(views) == 0 {
		return errors.New(`cannot add group: "views" must be a non-empty list`)
	}

	for _, view := range views {
		viewPath := strings.Split(view, "/")
		if len(viewPath) != 3 {
			return fmt.Errorf(`view "%s" must be in the format account/confdb/view`, view)
		}

		if !validAccountID.MatchString(viewPath[0]) {
			return fmt.Errorf("invalid Account ID %s", viewPath[0])
		}

		if !ValidConfdbName.MatchString(viewPath[1]) {
			return fmt.Errorf("invalid confdb name %s", viewPath[1])
		}

		if !ValidViewName.MatchString(viewPath[2]) {
			return fmt.Errorf("invalid view name %s", viewPath[2])
		}
	}

	group := &ControlGroup{
		Authentication: authentication,
		Views:          views,
	}
	op.Groups = append(op.Groups, group)

	return nil
}

// compact replaces consecutive runs of equal elements with a single copy.
// The provided slice s should be sorted.
func compact[T comparable](s []T) []T {
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
