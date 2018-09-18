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

import (
	"io"
)

var (
	ClrEOL            = clrEOL
	CursorInvisible   = cursorInvisible
	CursorVisible     = cursorVisible
	EnterReverseMode  = enterReverseMode
	ExitAttributeMode = exitAttributeMode
)

func MockEmptyEscapes() func() {
	clrEOL = ""
	cursorInvisible = ""
	cursorVisible = ""
	enterReverseMode = ""
	exitAttributeMode = ""

	return func() {
		clrEOL = ClrEOL
		cursorInvisible = CursorInvisible
		cursorVisible = CursorVisible
		enterReverseMode = EnterReverseMode
		exitAttributeMode = ExitAttributeMode
	}
}

func MockSimpleEscapes() func() {
	// set them to the tcap name (in all caps)
	clrEOL = "<CE>"
	cursorInvisible = "<VI>"
	cursorVisible = "<VS>"
	enterReverseMode = "<MR>"
	exitAttributeMode = "<ME>"

	return func() {
		clrEOL = ClrEOL
		cursorInvisible = CursorInvisible
		cursorVisible = CursorVisible
		enterReverseMode = EnterReverseMode
		exitAttributeMode = ExitAttributeMode
	}
}

func (p *ANSIMeter) Percent() string {
	return p.percent()
}

func (p *ANSIMeter) SetWritten(written float64) {
	p.written = written
}

func (p *ANSIMeter) GetWritten() float64 {
	return p.written
}

func (p *ANSIMeter) GetTotal() float64 {
	return p.total
}

func MockTermWidth(f func() int) func() {
	origTermWidth := termWidth
	termWidth = f
	return func() {
		termWidth = origTermWidth
	}
}

func MockStdout(w io.Writer) func() {
	origStdout := stdout
	stdout = w
	return func() {
		stdout = origStdout
	}
}

var (
	Norm    = norm
	Spinner = spinner
)
