// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/snapcore/snapd/overlord/state"

	"github.com/jessevdk/go-flags"
)

type command interface {
	setStdout(w *tabwriter.Writer)

	Execute(args []string) error
}

type baseCommand struct {
	out *tabwriter.Writer
	st  *state.State

	Positional struct {
		StateFilePath string `positional-args:"yes" positional-arg-name:":state-file"`
	} `positional-args:"yes"`
}

func (c *baseCommand) setStdout(w *tabwriter.Writer) {
	c.out = w
}

type commandInfo struct {
	shortHelp string
	longHelp  string
	generator func() command
}

var commands = make(map[string]*commandInfo)

func addCommand(name, shortHelp, longHelp string, generator func() command) {
	commands[name] = &commandInfo{
		shortHelp: shortHelp,
		longHelp:  longHelp,
		generator: generator,
	}
}

func loadState(path string) (*state.State, error) {
	if path == "" {
		path = "state.json"
	}
	r, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the state file: %s", err)
	}
	defer r.Close()

	var s *state.State
	s, err = state.ReadState(nil, r)
	if err != nil {
		return nil, err
	}

	return s, nil
}

type byChangeID []*state.Change

func (c byChangeID) Len() int           { return len(c) }
func (c byChangeID) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
func (c byChangeID) Less(i, j int) bool { return c[i].ID() < c[j].ID() }

func formatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

func run() error {
	parser := flags.NewParser(nil, flags.HelpFlag|flags.PassDoubleDash)

	w := tabwriter.NewWriter(os.Stdout, 5, 3, 2, ' ', 0)

	for name, cmdInfo := range commands {
		cmd := cmdInfo.generator()
		cmd.setStdout(w)

		_, err := parser.AddCommand(name, cmdInfo.shortHelp, cmdInfo.longHelp, cmd)
		if err != nil {
			return fmt.Errorf("cannot add command %q: %s", name, err)
		}
	}

	_, err := parser.ParseArgs(os.Args[1:])
	return err
}

func main() {
	if err := run(); err != nil {
		if e, ok := err.(*flags.Error); ok && e.Type == flags.ErrHelp {
			fmt.Fprintf(os.Stderr, "%s\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "cannot inspect state: %s\n", err)
		}
		os.Exit(1)
	}
}
