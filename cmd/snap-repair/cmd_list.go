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
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/snapcore/snapd/dirs"
)

func init() {
	const (
		short = "List repairs run on this device"
		long  = ""
	)

	if _, err := parser.AddCommand("list", short, long, &cmdList{}); err != nil {
		panic(err)
	}

}

type cmdList struct {
	Verbose bool `long:"verbose"`
}

func outputIndented(w io.Writer, path string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(w, "  error: %s\n", err)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fmt.Fprintf(w, "  %s\n", scanner.Text())
	}
	if scanner.Err() != nil {
		fmt.Fprintf(w, "  error: %s\n", scanner.Err())
	}

}

func showRepairOutput(w io.Writer, issuer string, seq, rev int) error {
	basedir := filepath.Join(dirs.SnapRepairRunDir, issuer, strconv.Itoa(seq))
	dirents, err := ioutil.ReadDir(basedir)
	if err != nil {
		return err
	}
	for _, dent := range dirents {
		name := dent.Name()
		if strings.HasSuffix(name, ".output") {
			fmt.Fprintf(w, " output:\n")
			outputIndented(w, filepath.Join(basedir, name))
		}
		if strings.HasPrefix(name, "script.") {
			fmt.Fprintf(w, " script:\n")
			outputIndented(w, filepath.Join(basedir, name))
		}
	}

	return nil
}

func (c *cmdList) Execute(args []string) error {
	runner := NewRunner()
	err := runner.readState()
	if os.IsNotExist(err) {
		fmt.Fprintf(Stdout, "no repairs yet\n")
		return nil
	}
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintf(w, "Issuer\tSeq\tRev\tStatus\n")
	for issuer, repairs := range runner.state.Sequences {
		for _, repairState := range repairs {
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\n", issuer, repairState.Sequence, repairState.Revision, repairState.Status)
			if c.Verbose {
				if err := showRepairOutput(w, issuer, repairState.Sequence, repairState.Revision); err != nil {
					fmt.Fprintf(w, " no details: %s\n", err)
				}
			}

		}
	}

	return nil
}
