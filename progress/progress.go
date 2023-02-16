// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/snapcore/snapd/osutil"
)

// Meter is an interface to show progress to the user
type Meter interface {
	// Start progress with max "total" steps
	Start(label string, total float64)

	// set progress to the "current" step
	Set(current float64)

	// set "total" steps needed
	SetTotal(total float64)

	// Finish the progress display
	Finished()

	// Indicate indefinite activity by showing a spinner
	Spin(msg string)

	// interface for writer
	Write(p []byte) (n int, err error)

	// notify the user of miscellaneous events
	Notify(string)
}

// NullMeter is a Meter that does nothing
type NullMeter struct{}

// Null is a default NullMeter instance
var Null = NullMeter{}

func (NullMeter) Start(string, float64)       {}
func (NullMeter) Set(float64)                 {}
func (NullMeter) SetTotal(float64)            {}
func (NullMeter) Finished()                   {}
func (NullMeter) Write(p []byte) (int, error) { return len(p), nil }
func (NullMeter) Notify(string)               {}
func (NullMeter) Spin(msg string)             {}

// QuietMeter is a Meter that _just_ shows Notify()s.
type QuietMeter struct {
	NullMeter
	w io.Writer
}

func (m QuietMeter) Notify(msg string) {
	w := m.w
	if w == nil {
		w = stdout
	}
	fmt.Fprintln(w, msg)
}

// testMeter, if set, is returned by MakeProgressBar; set it from tests.
var testMeter Meter

func MockMeter(meter Meter) func() {
	testMeter = meter
	return func() {
		testMeter = nil
	}
}

var inTesting bool = (osutil.IsTestBinary()) || os.Getenv("SPREAD_SYSTEM") != ""

var isTerminal = func() bool {
	return !inTesting && terminal.IsTerminal(int(os.Stdin.Fd()))
}

// MakeProgressBar creates an appropriate progress.Meter for the environ in
// which it is called:
//
//   - if MockMeter has been called, return that.
//   - if w is not nil nor os.Stdout, a QuietMeter outputting to it is returned.
//   - if no terminal is attached, or we think we're running a test,
//     a minimalistic QuietMeter outputting to stdout is returned.
//   - otherwise, an ANSIMeter is returned.
func MakeProgressBar(w io.Writer) Meter {
	if testMeter != nil {
		return testMeter
	}
	if (w == nil || w == os.Stdout) && isTerminal() {
		return &ANSIMeter{}
	}

	return QuietMeter{w: w}
}
