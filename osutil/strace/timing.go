// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package strace

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
)

// ExeRuntime is the runtime of an individual executable
type ExeRuntime struct {
	Exe string
	// FIXME: move to time.Duration
	TotalSec float64
}

// ExecveTiming measures the execve calls timings under strace. This is
// useful for performance analysis. It keeps the N slowest samples.
type ExecveTiming struct {
	TotalTime   float64
	exeRuntimes []ExeRuntime

	nSlowestSamples int
}

// NewExecveTiming returns a new ExecveTiming struct that keeps
// the given amount of the slowest exec samples.
func NewExecveTiming(nSlowestSamples int) *ExecveTiming {
	return &ExecveTiming{nSlowestSamples: nSlowestSamples}
}

func (stt *ExecveTiming) AddExeRuntime(exe string, totalSec float64) {
	stt.exeRuntimes = append(stt.exeRuntimes, ExeRuntime{
		Exe:      exe,
		TotalSec: totalSec,
	})
	stt.prune()
}

func (st *ExecveTiming) SetNrSamples(n int) {
	st.nSlowestSamples = n
}

// prune() ensures the number of exeRuntimes stays with the nSlowestSamples
// limit
func (stt *ExecveTiming) prune() {
	for len(stt.exeRuntimes) > stt.nSlowestSamples {
		fastest := 0
		for idx, rt := range stt.exeRuntimes {
			if rt.TotalSec < stt.exeRuntimes[fastest].TotalSec {
				fastest = idx
			}
		}
		// delete fastest element
		stt.exeRuntimes = append(stt.exeRuntimes[:fastest], stt.exeRuntimes[fastest+1:]...)
	}
}

func (stt *ExecveTiming) Display(w io.Writer) {
	if len(stt.exeRuntimes) == 0 {
		return
	}
	fmt.Fprintf(w, "Slowest %d exec calls during snap run:\n", len(stt.exeRuntimes))
	for _, rt := range stt.exeRuntimes {
		fmt.Fprintf(w, "  %2.3fs %s\n", rt.TotalSec, rt.Exe)
	}
	fmt.Fprintf(w, "Total time: %2.3fs\n", stt.TotalTime)
}

type perfStart struct {
	Start float64
	Exe   string
}

// lines look like:
// PID   TIME              SYSCALL
// 17363 1542815326.700248 execve("/snap/brave/44/usr/bin/update-mime-database", ["update-mime-database", "/home/egon/snap/brave/44/.local/"...], 0x1566008 /* 69 vars */) = 0
var execveRE = regexp.MustCompile(`([0-9]+)\ +([0-9.]+) execve\(\"([^"]+)\"`)

// lines look like:
// PID   TIME              SYSCALL
// 14157 1542875582.816782 execveat(3, "", ["snap-update-ns", "--from-snap-confine", "test-snapd-tools"], 0x7ffce7dd6160 /* 0 vars */, AT_EMPTY_PATH) = 0
var execveatRE = regexp.MustCompile(`([0-9]+)\ +([0-9.]+) execveat\(.*\["([^"]+)"`)

// lines look like (both SIGTERM and SIGCHLD need to be handled):
// PID   TIME                  SIGNAL
// 17559 1542815330.242750 --- SIGCHLD {si_signo=SIGCHLD, si_code=CLD_EXITED, si_pid=17643, si_uid=1000, si_status=0, si_utime=0, si_stime=0} ---
var sigChldTermRE = regexp.MustCompile(`[0-9]+\ +([0-9.]+).*SIG(CHLD|TERM)\ {.*si_pid=([0-9]+),`)

// all lines start with this:
// PID   TIME
// 21616 1542882400.198907 ....
var timeRE = regexp.MustCompile(`[0-9]+\ +([0-9.]+).*`)

func TraceExecveTimings(straceLog string) (*ExecveTiming, error) {
	slog, err := os.Open(straceLog)
	if err != nil {
		return nil, err
	}
	defer slog.Close()

	snapTrace := NewExecveTiming(10)
	pidToPerf := make(map[string]perfStart)

	var start, line string
	r := bufio.NewScanner(slog)
	for r.Scan() {
		line = r.Text()
		if m := timeRE.FindStringSubmatch(line); len(m) > 0 && start == "" {
			start = m[1]
		}

		// look for new Execs
		handleExecMatch := func(match []string) error {
			if len(match) == 0 {
				return nil
			}
			pid := match[1]
			execStart, err := strconv.ParseFloat(match[2], 64)
			if err != nil {
				return err
			}
			exe := match[3]
			// deal with subsequent execve()
			if perf, ok := pidToPerf[pid]; ok {
				snapTrace.AddExeRuntime(perf.Exe, execStart-perf.Start)
			}
			pidToPerf[pid] = perfStart{Start: execStart, Exe: exe}
			return nil
		}
		match := execveRE.FindStringSubmatch(line)
		if err := handleExecMatch(match); err != nil {
			return nil, err
		}
		match = execveatRE.FindStringSubmatch(line)
		if err := handleExecMatch(match); err != nil {
			return nil, err
		}
		match = sigChldTermRE.FindStringSubmatch(line)
		if len(match) > 0 {
			sigTime, err := strconv.ParseFloat(match[1], 64)
			if err != nil {
				return nil, err
			}
			sigPid := match[3]
			if perf, ok := pidToPerf[sigPid]; ok {
				snapTrace.AddExeRuntime(perf.Exe, sigTime-perf.Start)
				delete(pidToPerf, sigPid)
			}
		}

	}
	if m := timeRE.FindStringSubmatch(line); len(m) > 0 {
		tStart, err := strconv.ParseFloat(start, 64)
		if err != nil {
			return nil, err
		}
		tEnd, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return nil, err
		}
		snapTrace.TotalTime = tEnd - tStart
	}

	if r.Err() != nil {
		return nil, r.Err()
	}

	return snapTrace, nil
}
