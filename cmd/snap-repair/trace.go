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

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/dirs"
)

// newRepairTraces returns all repairTrace about the given "brand" and "seq"
// that can be found. brand, seq can be filepath.Glob expressions.
func newRepairTraces(brand, seq string) ([]*repairTrace, error) {
	matches, err := filepath.Glob(filepath.Join(dirs.SnapRepairRunDir, brand, seq, "*"))
	if err != nil {
		return nil, err
	}

	var repairTraces []*repairTrace
	for _, match := range matches {
		if trace := newRepairTraceFromPath(match); trace != nil {
			repairTraces = append(repairTraces, trace)
		}
	}

	return repairTraces, nil
}

// repairTrace holds information about a repair that was run.
type repairTrace struct {
	path string
}

// newRepairTraceFromPath takes a repair log path like
// the path /var/lib/snapd/repair/run/my-brand/1/r2.done
// and contructs a repair log from that.
func newRepairTraceFromPath(path string) *repairTrace {
	rt := &repairTrace{path: path}
	if !rt.validSuffix(path) {
		return nil
	}
	return rt
}

// Repair returns the repair human readable string in the form $brand-$id
func (rt *repairTrace) Repair() string {
	seq := filepath.Base(filepath.Dir(rt.path))
	brand := filepath.Base(filepath.Dir(filepath.Dir(rt.path)))

	return fmt.Sprintf("%s-%s", brand, seq)
}

// Rev returns the revision of the repair
func (rt *repairTrace) Rev() string {
	return revFromFilepath(rt.path)
}

// Summary returns the summary of the repair that was run
func (rt *repairTrace) Summary() string {
	f, err := os.Open(rt.path)
	if err != nil {
		return "cannot read summary"
	}
	defer f.Close()

	needle := "summary: "
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		s := scanner.Text()
		if strings.HasPrefix(s, needle) {
			return s[len(needle):]
		}
	}

	return "cannot find summary"
}

// Status returns the status of the given repair {done,skip,retry,running}
func (rt *repairTrace) Status() string {
	return filepath.Ext(rt.path)[1:]
}

func indentPrefix(level int) string {
	indentPrefix := make([]byte, level)
	for i := range indentPrefix {
		indentPrefix[i] = ' '
	}
	return string(indentPrefix)
}

// WriteScriptIndented outputs the script that produced this repair output
// to the given writer w with the indent level given by indent.
func (rt *repairTrace) WriteScriptIndented(w io.Writer, indent int) {
	scriptPath := rt.path[:strings.LastIndex(rt.path, ".")] + ".script"
	f, err := os.Open(scriptPath)
	if err != nil {
		fmt.Fprintf(w, "cannot read script: %v", err)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fmt.Fprintf(w, "%s%s\n", indentPrefix(indent), scanner.Text())
	}
	if scanner.Err() != nil {
		fmt.Fprintf(w, "%serror: %s\n", indentPrefix(indent), scanner.Err())
	}
}

// WriteOutputIndented outputs the repair output to the given writer w
// with the indent level given by indent.
func (rt *repairTrace) WriteOutputIndented(w io.Writer, indent int) {
	f, err := os.Open(rt.path)
	if err != nil {
		fmt.Fprintf(w, "  error: %s\n", err)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// move forward in the log to where the actual script output starts
	for scanner.Scan() {
		if scanner.Text() == "output:" {
			break
		}
	}
	// write the script output to w
	for scanner.Scan() {
		fmt.Fprintf(w, "%s%s\n", indentPrefix(indent), scanner.Text())
	}
	if scanner.Err() != nil {
		fmt.Fprintf(w, "%serror: %s\n", indentPrefix(indent), scanner.Err())
	}
}

// validSuffix returns true if the given traceName is something repairTrace
// understands.
func (rt *repairTrace) validSuffix(traceName string) bool {
	for _, valid := range []string{".retry", ".skip", ".done", ".running"} {
		if strings.HasSuffix(traceName, valid) {
			return true
		}
	}
	return false
}

// revFromFilepath is a helper that extrace the revision number from the
// filename of the repairTrace
func revFromFilepath(name string) string {
	var rev int
	if _, err := fmt.Sscanf(filepath.Base(name), "r%d.", &rev); err == nil {
		return strconv.Itoa(rev)
	}
	return "?"
}
