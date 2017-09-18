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

func showRepairOutput(w io.Writer, repair string) error {
	i := strings.LastIndex(repair, "-")
	if i < 0 {
		return fmt.Errorf("cannot parse repair %q", repair)
	}
	brand := repair[:i]
	seq := repair[i+1:]

	basedir := filepath.Join(dirs.SnapRepairRunDir, brand, seq)
	dirents, err := ioutil.ReadDir(basedir)
	if os.IsNotExist(err) {
		return fmt.Errorf("cannot find repair %q", fmt.Sprintf("%s-%s", brand, seq))
	}
	if err != nil {
		return fmt.Errorf("cannot read snap repair directory: %v", err)
	}
	for _, dent := range dirents {
		name := dent.Name()
		rev := revFromFilename(name)
		if strings.HasSuffix(name, ".retry") || strings.HasSuffix(name, ".done") || strings.HasSuffix(name, ".skip") {
			status := filepath.Ext(name)[1:]
			fmt.Fprintf(w, "%s  %s  %s\n", repair, rev, status)
			fmt.Fprintf(w, " output:\n")
			outputIndented(w, filepath.Join(basedir, name))
			fmt.Fprintf(w, " script:\n")
			scriptName := filepath.Join(basedir, name[:strings.LastIndex(name, ".")]+".script")
			outputIndented(w, scriptName)
		}
	}

	return nil
}

func (c *cmdShow) Execute([]string) error {
	for _, repair := range c.Positional.Repair {
		if err := showRepairOutput(Stdout, repair); err != nil {
			return err
		}
		fmt.Fprintf(Stdout, "\n")
	}

	return nil
}
