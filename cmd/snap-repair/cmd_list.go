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
	"github.com/snapcore/snapd/osutil"
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

func showRepairOutput(w io.Writer, issuer, seq, rev string) error {
	basedir := filepath.Join(dirs.SnapRepairRunDir, issuer, seq)
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

type repairTrace struct {
	issuer string
	seq    string
	rev    string
	status string
}

func (c *cmdList) Execute(args []string) error {

	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	defer w.Flush()

	// FIXME: this will not currently list the repairs that are
	//        skipped because of e.g. wrong architecture

	// directory structure is:
	//  canonical/
	//    1/
	//      r0.000001.2017-08-22T102401.output
	//      r0.000001.2017-08-22T102401.retry
	//      script.r0
	//      r1.000001.2017-08-23T102401.output
	//      r1.000001.2017-08-23T102401.done
	//      script.r0
	//    2/
	//      r3.000001.2017-08-24T102401.output
	//      r3.000001.2017-08-24T102401.done
	//      script.r3
	var repairTraces []repairTrace
	issuersContent, err := ioutil.ReadDir(dirs.SnapRepairRunDir)
	if os.IsNotExist(err) {
		fmt.Fprintf(Stdout, "no repairs yet\n")
		return nil
	}
	if err != nil {
		return err
	}
	for _, issuer := range issuersContent {
		if !issuer.IsDir() {
			continue
		}
		issuerName := issuer.Name()

		seqDir := filepath.Join(dirs.SnapRepairRunDir, issuerName)
		sequences, err := ioutil.ReadDir(seqDir)
		if err != nil {
			continue
		}
		for _, seq := range sequences {
			seqName := seq.Name()

			artifactsDir := filepath.Join(dirs.SnapRepairRunDir, issuerName, seqName)
			artifacts, err := ioutil.ReadDir(artifactsDir)
			if err != nil {
				continue
			}
			for _, artifact := range artifacts {
				artifactName := artifact.Name()
				if strings.HasSuffix(artifactName, ".output") {
					t := repairTrace{
						issuer: issuerName,
						seq:    seqName,
						rev:    "?",
						status: "unknown",
					}

					base := filepath.Join(artifactsDir, artifactName[:len(artifactName)-len(".output")])
					switch {
					case osutil.FileExists(base + ".retry"):
						t.status = "retry"
					case osutil.FileExists(base + ".done"):
						t.status = "done"
					case osutil.FileExists(base + ".skip"):
						t.status = "skip"
					}

					var rev int
					if _, err := fmt.Sscanf(artifactName, "r%d.", &rev); err == nil {
						t.rev = strconv.Itoa(rev)
					}

					repairTraces = append(repairTraces, t)
				}
			}
		}
	}

	fmt.Fprintf(w, "Issuer\tSeq\tRev\tStatus\n")
	for _, t := range repairTraces {
		fmt.Fprintf(w, "%s\t%v\t%v\t%s\n", t.issuer, t.seq, t.rev, t.status)
		if c.Verbose {
			if err := showRepairOutput(w, t.issuer, t.seq, t.rev); err != nil {
				fmt.Fprintf(w, " no details: %s\n", err)
			}
		}

	}

	return nil
}
