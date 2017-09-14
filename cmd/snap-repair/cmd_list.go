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
	"fmt"
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
		short = "Lists repairs run on this device"
		long  = ""
	)

	if _, err := parser.AddCommand("list", short, long, &cmdList{}); err != nil {
		panic(err)
	}

}

type cmdList struct{}

type repairTrace struct {
	issuer string
	seq    string
	rev    string
	status string
}

func newRepairTrace(artifactName, issuerName, seqName, status string) repairTrace {
	return repairTrace{
		issuer: issuerName,
		seq:    seqName,
		rev:    revFromFilename(artifactName),
		status: status,
	}
}

func revFromFilename(name string) string {
	var rev int
	if _, err := fmt.Sscanf(name, "r%d.", &rev); err == nil {
		return strconv.Itoa(rev)
	}
	return "?"
}

func (c *cmdList) Execute([]string) error {
	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	defer w.Flush()

	// FIXME: this will not currently list the repairs that are
	//        skipped because of e.g. wrong architecture

	// directory structure is:
	//  canonical/
	//    1/
	//      r0.retry
	//      r0.script
	//      r1.done
	//      r1.script
	//    2/
	//      r3.done
	//      r3.script
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
				switch {
				case strings.HasSuffix(artifactName, ".retry"):
					repairTraces = append(repairTraces, newRepairTrace(artifactName, issuerName, seqName, "retry"))
				case strings.HasSuffix(artifactName, ".skip"):
					repairTraces = append(repairTraces, newRepairTrace(artifactName, issuerName, seqName, "skip"))
				case strings.HasSuffix(artifactName, ".done"):
					repairTraces = append(repairTraces, newRepairTrace(artifactName, issuerName, seqName, "done"))
				}
			}
		}
	}

	fmt.Fprintf(w, "Issuer\tSeq\tRev\tStatus\n")
	for _, t := range repairTraces {
		fmt.Fprintf(w, "%s\t%v\t%v\t%s\n", t.issuer, t.seq, t.rev, t.status)
	}

	return nil
}
