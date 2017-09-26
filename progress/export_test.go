// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package progress

var FormatAmount = formatAmount
var FormatBPS = formatBPS
var FormatDuration = formatDuration
var ClrEOL = clrEOL
var ExitAttributeMode = exitAttributeMode

func MockEmptyEscapes() func() {
	oldClrEOL := clrEOL
	oldCursorInvisible := cursorInvisible
	oldCursorVisible := cursorVisible
	oldEnterReverseMode := enterReverseMode
	oldExitAttributeMode := exitAttributeMode

	clrEOL = ""
	cursorInvisible = ""
	cursorVisible = ""
	enterReverseMode = ""
	exitAttributeMode = ""

	return func() {
		clrEOL = oldClrEOL
		cursorInvisible = oldCursorInvisible
		cursorVisible = oldCursorVisible
		enterReverseMode = oldEnterReverseMode
		exitAttributeMode = oldExitAttributeMode
	}
}
