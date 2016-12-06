// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"bufio"
	"fmt"
	"os"
	"unicode"

	"github.com/cheggaaa/pb"
	"golang.org/x/crypto/ssh/terminal"
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

// NullProgress is a Meter that does nothing
type NullProgress struct {
}

// Start does nothing
func (t *NullProgress) Start(label string, total float64) {
}

// Set does nothing
func (t *NullProgress) Set(current float64) {
}

// SetTotal does nothing
func (t *NullProgress) SetTotal(total float64) {
}

// Finished does nothing
func (t *NullProgress) Finished() {
}

// Write does nothing
func (t *NullProgress) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// Notify does nothing
func (t *NullProgress) Notify(string) {}

// Spin does nothing
func (t *NullProgress) Spin(msg string) {
}

const clearUntilEOL = "\033[K"

// TextProgress show progress on the terminal
type TextProgress struct {
	Meter
	pbar     *pb.ProgressBar
	spinStep int
}

// NewTextProgress returns a new TextProgress type
func NewTextProgress() *TextProgress {
	return &TextProgress{}
}

// Start starts showing progress
func (t *TextProgress) Start(label string, total float64) {
	t.pbar = pb.New64(int64(total))
	t.pbar.ShowSpeed = true
	t.pbar.Units = pb.U_BYTES
	t.pbar.Prefix(label)
	t.pbar.Start()
}

// Set sets the progress to the current value
func (t *TextProgress) Set(current float64) {
	t.pbar.Set(int(current))
}

// SetTotal set the total steps needed
func (t *TextProgress) SetTotal(total float64) {
	t.pbar.Total = int64(total)
}

// Finished stops displaying the progress
func (t *TextProgress) Finished() {
	if t.pbar != nil {
		// workaround silly pb that always does a fmt.Println() on
		// finish (unless NotPrint is set)
		t.pbar.NotPrint = true
		t.pbar.Finish()
		t.pbar.NotPrint = false
	}
	fmt.Printf("\r\033[K")
}

// Write is there so that progress can implment a Writer and can be
// used to display progress of io operations
func (t *TextProgress) Write(p []byte) (n int, err error) {
	return t.pbar.Write(p)
}

// Spin advances a spinner, i.e. can be used to show progress for operations
// that have a unknown duration
func (t *TextProgress) Spin(msg string) {
	states := `|/-\`

	// clear until end of line
	fmt.Printf("\r[%c] %s%s", states[t.spinStep], msg, clearUntilEOL)
	t.spinStep++
	if t.spinStep >= len(states) {
		t.spinStep = 0
	}
}

// Agreed asks the user whether they agree to the given license text
func (t *TextProgress) Agreed(intro, license string) bool {
	if _, err := fmt.Println(intro); err != nil {
		return false
	}

	// XXX: send it through a pager instead of this ugly thing
	if _, err := fmt.Println(license); err != nil {
		return false
	}

	reader := bufio.NewReader(os.Stdin)
	if _, err := fmt.Print("Do you agree? [y/n] "); err != nil {
		return false
	}
	r, _, err := reader.ReadRune()
	if err != nil {
		return false
	}

	return unicode.ToLower(r) == 'y'
}

// Notify the user of miscellaneous events
func (*TextProgress) Notify(msg string) {
	fmt.Printf("\r%s%s\n", msg, clearUntilEOL)
}

// MakeProgressBar creates an appropriate progress (which may be a
// NullProgress bar if there is no associated terminal).
func MakeProgressBar() Meter {
	var pbar Meter
	if attachedToTerminal() {
		pbar = NewTextProgress()
	} else {
		pbar = &NullProgress{}
	}

	return pbar
}

// attachedToTerminal returns true if the calling process is attached to
// a terminal device.
var attachedToTerminal = func() bool {
	fd := int(os.Stdin.Fd())

	return terminal.IsTerminal(fd)
}
