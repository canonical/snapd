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
	"fmt"
	"io"
	"os"
	"time"
	"unicode"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/mattn/go-runewidth"
	"github.com/snapcore/snapd/strutil/quantity"
)

var stdout io.Writer = os.Stdout

// ANSIMeter is a progress.Meter that uses ANSI escape codes to make
// better use of the available horizontal space.
type ANSIMeter struct {
	label   []rune
	total   float64
	written float64
	spin    int
	t0      time.Time
}

// these are the bits of the ANSI escapes (beyond \r) that we use
// (names of the terminfo capabilities, see terminfo(5))
var (
	// clear to end of line
	clrEOL = "\033[K"
	// make cursor invisible
	cursorInvisible = "\033[?25l"
	// make cursor visible
	cursorVisible = "\033[?25h"
	// turn on reverse video
	enterReverseMode = "\033[7m"
	// go back to normal video
	exitAttributeMode = "\033[0m"
)

var termWidth = func() int {
	col, _, _ := terminal.GetSize(0)
	if col <= 0 {
		// give up
		col = 80
	}
	return col
}

func init() {
	// The width of some characters in unicode can not be determined before renderering.
	// For example, we use the character '…' (U+2026) to indicate that the text is too long. We assume it will just
	// take 1 column in terminals.
	// With EastAsianWidth = true (the default value if LC_ALL is set to one of CJK locales), runewidth assumes
	// the width of the character '…' is 2. But we expected it to be 1.
	//
	// But in the real world, most of terminal applications render it as 1 column, even if the UI language of
	// the console application is CJK. This appears to be true for GNOME Terminal, Konsole, Tilda and
	// Linux Console with default monospace font on Ubuntu Noble, all of them rendered it as 1 column.
	// To get closer to the real world, we disable EastAsianWidth here. And we will show correct progress
	// bar in most of terminal applications.
	//
	// See "Ambiguous Characters" in http://www.unicode.org/reports/tr11/
	runewidth.DefaultCondition.EastAsianWidth = false

	// Speed up the process of handling the width of characters.
	runewidth.DefaultCondition.CreateLUT()
}

func (p *ANSIMeter) Start(label string, total float64) {
	p.label = []rune(label)
	p.total = total
	p.t0 = time.Now().UTC()
	fmt.Fprint(stdout, cursorInvisible)
}

func runeWidth(runes []rune) int {
	width := 0
	for _, r := range runes {
		width += runewidth.RuneWidth(r)
	}
	return width
}

func norm(col int, msg []rune) []rune {
	if col <= 0 {
		return []rune{}
	}
	out := make([]rune, col)
	width := 0
	i := 0
	for i = 0; i < len(msg); i++ {
		r := msg[i]
		w := runewidth.RuneWidth(r)
		if width+w > col {
			if width == col {
				width -= runewidth.RuneWidth(out[i-1])
				i -= 1
			}
			// '…' (U+2026) is used to indicate text is too long. In most of
			// terminal applications, this character is rendered as 1 column.
			out[i] = '…'
			width += 1
			i += 1
			break
		}
		out[i] = r
		width += w
	}

	for ; width < col; i++ {
		out[i] = ' '
		width += 1
	}
	return out[:i]
}

func (p *ANSIMeter) SetTotal(total float64) {
	p.total = total
}

func (p *ANSIMeter) percent() string {
	if p.total == 0. {
		return "---%"
	}
	q := p.written * 100 / p.total
	if q > 999.4 || q < 0. {
		return "???%"
	}
	return fmt.Sprintf("%3.0f%%", q)
}

var formatDuration = quantity.FormatDuration

func (p *ANSIMeter) Set(current float64) {
	if current < 0 {
		current = 0
	}
	if current > p.total {
		current = p.total
	}

	p.written = current
	col := termWidth()
	// time left: 5
	//    gutter: 1
	//     speed: 8
	//    gutter: 1
	//   percent: 4
	//    gutter: 1
	//          =====
	//           20
	// and we want to leave at least 10 for the label, so:
	//  * if      width <= 15, don't show any of this (progress bar is good enough)
	//  * if 15 < width <= 20, only show time left (time left + gutter = 6)
	//  * if 20 < width <= 29, also show percentage (percent + gutter = 5
	//  * if 29 < width      , also show speed (speed+gutter = 9)
	var percent, speed, timeleft string
	if col > 15 {
		since := time.Now().UTC().Sub(p.t0).Seconds()
		per := since / p.written
		left := (p.total - p.written) * per
		// XXX: duration unit string is controlled by translations, and
		// may carry a multibyte unit suffix
		timeleft = " " + formatDuration(left)
		if col > 20 {
			percent = " " + p.percent()
			if col > 29 {
				speed = " " + quantity.FormatBPS(p.written, since, -1)
			}
		}
	}

	rpercent := []rune(percent)
	rspeed := []rune(speed)
	rtimeleft := []rune(timeleft)
	msg := make([]rune, 0, col)
	// XXX: assuming terminal can display `col` number of runes
	msg = append(msg, norm(col-len(rpercent)-len(rspeed)-runeWidth(rtimeleft), p.label)...)
	msg = append(msg, rpercent...)
	msg = append(msg, rspeed...)
	msg = append(msg, rtimeleft...)
	i := int(current * float64(len(msg)) / p.total)
	fmt.Fprint(stdout, "\r", enterReverseMode, string(msg[:i]), exitAttributeMode, string(msg[i:]))
}

var spinner = []string{"/", "-", "\\", "|"}

func (p *ANSIMeter) Spin(msgstr string) {
	msg := []rune(msgstr)
	col := termWidth()
	if col-2 >= runeWidth(msg) {
		fmt.Fprint(stdout, "\r", string(norm(col-2, msg)), " ", spinner[p.spin])
		p.spin++
		if p.spin >= len(spinner) {
			p.spin = 0
		}
	} else {
		fmt.Fprint(stdout, "\r", string(norm(col, msg)))
	}
}

func (*ANSIMeter) Finished() {
	fmt.Fprint(stdout, "\r", exitAttributeMode, cursorVisible, clrEOL)
}

func (*ANSIMeter) Notify(msgstr string) {
	col := termWidth()
	fmt.Fprint(stdout, "\r", exitAttributeMode, clrEOL)

	msg := []rune(msgstr)
	var i int
	for runeWidth(msg) > col {
		endOfLine := 0
		lineWidth := 0
		for i, r := range msg {
			w := runewidth.RuneWidth(r)
			if w+lineWidth > col {
				break
			}
			if unicode.IsSpace(r) {
				endOfLine = i
			}
			lineWidth += w
		}

		if endOfLine >= 1 {
			// found a space; print up to but not including it, and skip it
			i = endOfLine + 1
		} else {
			// didn't find anything; print the whole thing and try again
			endOfLine = i
		}

		fmt.Fprintln(stdout, string(msg[:endOfLine]))
		msg = msg[i:]
	}
	fmt.Fprintln(stdout, string(msg))
}

func (p *ANSIMeter) Write(bs []byte) (n int, err error) {
	n = len(bs)
	p.Set(p.written + float64(n))

	return
}
