// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/syslog"
	"os"

	"github.com/nsf/termbox-go"

	"github.com/snapcore/snapd/logger"
)

type back struct {
	menu     *Menu
	entryIdx int
}

// EntryGoBack indicates an implicit go-back entry
const EntryGoBack = -1

type state struct {
	menu *Menu

	currentMenu     *Menu
	currentEntryIdx int

	// stack of menus
	stack []back
}

func newState(menu *Menu) *state {
	return &state{
		menu:            menu,
		currentMenu:     menu,
		currentEntryIdx: 0,
	}
}

func (s *state) CurrentMenuAndEntry() (menu *Menu, entryIdx int) {
	return s.currentMenu, s.currentEntryIdx
}

func (s *state) IsNested() bool {
	return len(s.stack) != 0
}

type view struct {
	goBackText       string
	cursor           string
	submenuIndicator string

	st *state
}

func printAt(y, x int, fg, bg termbox.Attribute, f string, parm ...interface{}) int {
	maxX, maxY := termbox.Size()
	if x > maxX || y > maxY {
		return maxX
	}
	s := fmt.Sprintf(f, parm...)
	l := len(s)
	if x+l > maxX {
		l = maxX - x
	}
	for i := 0; i < l; i++ {
		termbox.SetCell(x+i, y, rune(s[i]), fg, bg)
	}
	return l
}

func (v *view) draw() {
	st := v.st

	line := 0
	fg := termbox.ColorWhite
	bg := termbox.ColorDefault

	curr, currIdx := st.CurrentMenuAndEntry()

	termbox.Clear(bg, bg)

	if len(st.stack) != 0 {
		cursor := " "
		if st.currentEntryIdx == EntryGoBack {
			cursor = v.cursor
		}
		printAt(line, 0, fg, bg, "%s 0. %s", cursor, v.goBackText)
		line += 2
	}

	if curr.Header != "" {
		printAt(line, 0, fg, bg, curr.Header)
		line += 2
	}
	if curr.Description != "" {
		printAt(line, 0, fg, bg, curr.Description)
		line++
	}

	entriesLineStart := line
	maxLen := 0
	for idx, en := range curr.Entries {
		cursor := " "
		if idx == currIdx {
			cursor = v.cursor
		}
		if end := printAt(line, 0, fg, bg, "%s %d. %v", cursor, idx+1, en.Text); end > maxLen {
			maxLen = end
		}
		line++
	}

	for idx, en := range curr.Entries {
		if en.Submenu != nil {
			printAt(entriesLineStart+idx, maxLen+1, fg, bg, v.submenuIndicator)
		}
	}

	termbox.Flush()
}

type controller struct {
	st *state
}

func newController(st *state) *controller {
	return &controller{st: st}
}

func (c *controller) Up() {
	st := c.st

	if st.currentEntryIdx > EntryGoBack {
		if st.currentEntryIdx == 0 && !st.IsNested() {
			// the top level menu has no implicit 'back' entry
			return
		}
		st.currentEntryIdx--
	}
}

func (c *controller) Down() {
	st := c.st

	if st.currentEntryIdx < len(st.currentMenu.Entries)-1 {
		st.currentEntryIdx++
	}
}

func (c *controller) Enter() string {
	st := c.st

	if st.currentEntryIdx == EntryGoBack {
		c.back()
		return ""
	}

	// enter a menu
	sub := st.currentMenu.Entries[st.currentEntryIdx].Submenu
	if sub == nil {
		// no submenu, final option
		return st.currentMenu.Entries[st.currentEntryIdx].ID
	}

	st.stack = append(st.stack, back{
		menu:     st.currentMenu,
		entryIdx: st.currentEntryIdx,
	})

	st.currentMenu = sub
	st.currentEntryIdx = EntryGoBack
	return ""
}

func (c *controller) back() {
	st := c.st

	if len(st.stack) == 0 {
		// nothing to go back to
		return
	}

	prev := st.stack[0]
	st.stack = st.stack[1:]

	st.currentMenu = prev.menu
	st.currentEntryIdx = prev.entryIdx
}

type Menu struct {
	Header      string
	Description string
	Entries     []Entry
}

type Entry struct {
	ID      string
	Text    string
	Submenu *Menu
}

type Response struct {
	ID string
}

func setupLogger() error {
	// the process' stderr may be connected to the terminal, use
	w, err := syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, "")
	if err != nil {
		return fmt.Errorf("cannot setup syslog: %v", err)
	}

	l, err := logger.New(w, logger.DefaultFlags)
	if err != nil {
		return fmt.Errorf("cannot create new logger: %v", err)
	}

	logger.SetLogger(l)
	return nil
}

func main() {
	flag.Parse()

	if err := setupLogger(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot setup logger: %v\n", err)
	}

	if os.Getenv("TERM") == "" {
		if err := os.Setenv("TERM", "linux"); err != nil {
			logger.Panicf("cannot set TERM to 'linux': %v", err)
		}
	}

	// initialize the current (/dev/tty) terminal, it is up to the parent
	// process to have the terminal set up in a correct manner
	if err := termbox.Init(); err != nil {
		logger.Panicf("cannot initialize termui: %v", err)
	}
	// 8 colors, nothing fancy
	termbox.SetOutputMode(termbox.OutputNormal)
	// allows ESC to be present in the input
	termbox.SetInputMode(termbox.InputEsc)

	// receive the UI options
	var menu Menu
	dec := json.NewDecoder(os.Stdin)
	err := dec.Decode(&menu)
	if err != nil {
		logger.Panicf("cannot decode input stream: %v", err)
	}

	st := newState(&menu)
	c := newController(st)
	v := view{
		cursor:           ">",
		submenuIndicator: ">",
		goBackText:       "Go back",

		st: st,
	}

	v.draw()

	// selection
	selectedActionID := ""

Loop:
	for {
		ev := termbox.PollEvent()
		if ev.Type != termbox.EventKey {
			continue
		}
		// down/up keys works reliably on vty, however they are
		// unreliable on the serial console, thus allow j/k to be used
		// as well
		switch {
		case ev.Key == termbox.KeyArrowDown || ev.Ch == 'j':
			c.Down()
		case ev.Key == termbox.KeyArrowUp || ev.Ch == 'k':
			c.Up()
		case ev.Key == termbox.KeyEnter:
			// enter key works well on serial and vty
			selectedActionID = c.Enter()
			if selectedActionID != "" {
				break Loop
			}
		}
		v.draw()
	}
	termbox.Close()

	logger.Noticef("user selection: %v", selectedActionID)

	// communicate the user's choice back to the parent
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(&Response{ID: selectedActionID}); err != nil {
		logger.Panicf("cannot encode response: %v", err)
	}
}
