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
	"strings"
	"text/tabwriter"

	"github.com/snapcore/snapd/dirs"
)

func init() {
	const (
		short = "Shows specific repairs run on this device"
		long  = ""
	)

	if _, err := parser.AddCommand("show", short, long, &cmdShow{}); err != nil {
		panic(err)
	}

}

type cmdShow struct {
	Positional struct {
		Repair []string `positional-arg-name:"<repair>"`
	} `positional-args:"yes"`
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

func showRepairOutput(w io.Writer, issuer, seq string) error {
	basedir := filepath.Join(dirs.SnapRepairRunDir, issuer, seq)
	dirents, err := ioutil.ReadDir(basedir)
	if err != nil {
		return err
	}
	for _, dent := range dirents {
		name := dent.Name()
		rev := revFromFilename(name)
		if strings.HasSuffix(name, ".retry") || strings.HasSuffix(name, ".done") || strings.HasSuffix(name, ".skip") {
			status := filepath.Ext(name)[1:]
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", issuer, seq, rev, status)
			fmt.Fprintf(w, " output:\n")
			outputIndented(w, filepath.Join(basedir, name))
		}
		if strings.HasSuffix(name, ".script") {
			fmt.Fprintf(w, " script:\n")
			outputIndented(w, filepath.Join(basedir, name))
		}
	}

	return nil
}

func (c *cmdShow) Execute([]string) error {
	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	defer w.Flush()

	for _, repair := range c.Positional.Repair {
		i := strings.LastIndex(repair, "-")
		if i < 0 {
			continue
		}
		brand := repair[:i]
		seq := repair[i+1:]
		showRepairOutput(Stdout, brand, seq)
		fmt.Fprintf(Stdout, "\n")
	}

	return nil
}
