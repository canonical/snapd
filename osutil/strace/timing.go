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

	"github.com/ddkwork/golibrary/mylog"
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

func (stt *ExecveTiming) addExeRuntime(exe string, totalSec float64) {
	stt.exeRuntimes = append(stt.exeRuntimes, ExeRuntime{
		Exe:      exe,
		TotalSec: totalSec,
	})
	stt.prune()
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

type exeStart struct {
	start float64
	exe   string
}

type pidTracker struct {
	pidToExeStart map[string]exeStart
}

func newPidTracker() *pidTracker {
	return &pidTracker{
		pidToExeStart: make(map[string]exeStart),
	}
}

func (pt *pidTracker) Get(pid string) (startTime float64, exe string) {
	if exeStart, ok := pt.pidToExeStart[pid]; ok {
		return exeStart.start, exeStart.exe
	}
	return 0, ""
}

func (pt *pidTracker) Add(pid string, startTime float64, exe string) {
	pt.pidToExeStart[pid] = exeStart{start: startTime, exe: exe}
}

func (pt *pidTracker) Del(pid string) {
	delete(pt.pidToExeStart, pid)
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

func handleExecMatch(trace *ExecveTiming, pt *pidTracker, match []string) error {
	if len(match) == 0 {
		return nil
	}
	// the pid of the process that does the execve{,at}()
	pid := match[1]
	execStart := mylog.Check2(strconv.ParseFloat(match[2], 64))

	exe := match[3]
	// deal with subsequent execve()
	if start, exe := pt.Get(pid); exe != "" {
		trace.addExeRuntime(exe, execStart-start)
	}
	pt.Add(pid, execStart, exe)
	return nil
}

func handleSignalMatch(trace *ExecveTiming, pt *pidTracker, match []string) error {
	if len(match) == 0 {
		return nil
	}
	sigTime := mylog.Check2(strconv.ParseFloat(match[1], 64))

	sigPid := match[3]
	if start, exe := pt.Get(sigPid); exe != "" {
		trace.addExeRuntime(exe, sigTime-start)
		pt.Del(sigPid)
	}
	return nil
}

func TraceExecveTimings(straceLog string, nSlowest int) (*ExecveTiming, error) {
	slog := mylog.Check2(os.Open(straceLog))

	defer slog.Close()

	// pidTracker maps the "pid" string to the executable
	pidTracker := newPidTracker()

	var line string
	var start, end, tmp float64
	trace := NewExecveTiming(nSlowest)
	r := bufio.NewScanner(slog)
	for r.Scan() {
		line = r.Text()
		if start == 0.0 {
			mylog.Check2(fmt.Sscanf(line, "%f %f ", &tmp, &start))
		}
		// handleExecMatch looks for execve{,at}() calls and
		// uses the pidTracker to keep track of execution of
		// things. Because of fork() we may see many pids and
		// within each pid we can see multiple execve{,at}()
		// calls.
		// An example of pids/exec transitions:
		// $ snap run --trace-exec test-snapd-sh -c "/bin/true"
		//    pid 20817 execve("snap-confine")
		//    pid 20817 execve("snap-exec")
		//    pid 20817 execve("/snap/test-snapd-sh/x2/bin/sh")
		//    pid 20817 execve("/bin/sh")
		//    pid 2023  execve("/bin/true")
		match := execveRE.FindStringSubmatch(line)
		mylog.Check(handleExecMatch(trace, pidTracker, match))

		match = execveatRE.FindStringSubmatch(line)
		mylog.Check(handleExecMatch(trace, pidTracker, match))

		// handleSignalMatch looks for SIG{CHLD,TERM} signals and
		// maps them via the pidTracker to the execve{,at}() calls
		// of the terminating PID to calculate the total time of
		// a execve{,at}() call.
		match = sigChldTermRE.FindStringSubmatch(line)
		mylog.Check(handleSignalMatch(trace, pidTracker, match))

	}
	mylog.Check2(fmt.Sscanf(line, "%f %f", &tmp, &end))

	trace.TotalTime = end - start

	if r.Err() != nil {
		return nil, r.Err()
	}

	return trace, nil
}
