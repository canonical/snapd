// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

package assertstate

// expose for testing
var (
	DoFetch                                   = doFetch
	ValidationSetAssertionForEnforce          = validationSetAssertionForEnforce
	ValidationSetAssertionForMonitor          = validationSetAssertionForMonitor
	AddCurrentTrackingToValidationSetsHistory = addCurrentTrackingToValidationSetsHistory
	ValidationSetsHistoryTop                  = validationSetsHistoryTop
)

func MockMaxGroups(n int) (restore func()) {
	oldMaxGroups := maxGroups
	maxGroups = n
	return func() {
		maxGroups = oldMaxGroups
	}
}

func MockMaxValidationSetsHistorySize(n int) (restore func()) {
	oldMaxValidationSetsHistorySize := maxValidationSetsHistorySize
	maxValidationSetsHistorySize = n
	return func() {
		maxValidationSetsHistorySize = oldMaxValidationSetsHistorySize
	}
}
