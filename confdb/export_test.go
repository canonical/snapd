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

type (
	ViewRef = viewRef
)

var (
	GetValuesThroughPaths = getValuesThroughPaths
	NewAuthentication     = newAuthentication
)

type Authentication = authentication

func (a Authentication) ToStrings() []string {
	return a.toStrings()
}

// TODO: remove this once we remove the temporary test TestRequestMatch
func (v *View) MatchGetRequest(request string) (matches []requestMatch, err error) {
	opts := validationOptions{pathType: userPath}
	accessors, err := parsePathIntoAccessors(request, opts)
	if err != nil {
		return nil, err
	}
	return v.matchGetRequest(accessors)
}

func ParsePathIntoAccessors(path string) ([]accessor, error) {
	opts := validationOptions{pathType: viewPath}
	return parsePathIntoAccessors(path, opts)
}

type RequestMatch = requestMatch

func (m RequestMatch) StoragePath() string {
	return m.storagePath
}

func MockMaxValueDepth(newDepth int) (restore func()) {
	oldDepth := maxValueDepth
	maxValueDepth = newDepth
	return func() {
		maxValueDepth = oldDepth
	}
}
