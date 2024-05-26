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

package strace_test

import (
	"bytes"
	"os"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil/strace"
)

type timingSuite struct{}

var _ = Suite(&timingSuite{})

func (s *timingSuite) TestNewExecveTiming(c *C) {
	et := strace.NewExecveTiming(10)
	c.Assert(et, FitsTypeOf, &strace.ExecveTiming{})
}

func (s *timingSuite) TestDisplayExeRuntimes(c *C) {
	// setup mock traces
	stt := strace.NewExecveTiming(3)
	stt.TotalTime = 2.71828
	stt.AddExeRuntime("slow", 1.0001)
	stt.AddExeRuntime("fast", 0.1002)
	stt.AddExeRuntime("really-fast", 0.0301)
	stt.AddExeRuntime("medium", 0.5003)

	// display works and shows the slowest 3 calls in order
	buf := bytes.NewBuffer(nil)
	stt.Display(buf)
	c.Check(buf.String(), Equals, `Slowest 3 exec calls during snap run:
  1.000s slow
  0.100s fast
  0.500s medium
Total time: 2.718s
`)
}

func (s *timingSuite) TestExecveTimingPrunes(c *C) {
	stt := strace.NewExecveTiming(3)

	// simple cases
	stt.AddExeRuntime("t0", 2)
	c.Check(stt.ExeRuntimes(), HasLen, 1)
	stt.AddExeRuntime("t1", 1)
	c.Check(stt.ExeRuntimes(), HasLen, 2)
	stt.AddExeRuntime("t2", 5)
	c.Check(stt.ExeRuntimes(), HasLen, 3)

	// starts pruing the fastest call, keeps order otherwise
	stt.AddExeRuntime("t3", 4)
	c.Check(stt.ExeRuntimes(), HasLen, 3)
	c.Check(stt.ExeRuntimes(), DeepEquals, []strace.ExeRuntime{
		{Exe: "t0", TotalSec: 2},
		{Exe: "t2", TotalSec: 5},
		{Exe: "t3", TotalSec: 4},
	})
}

// generated with:
//
//	sudo /usr/lib/snapd/snap-discard-ns test-snapd-tools && sudo strace -u $USER -o strace.log -f -e trace=execve,execveat -ttt test-snapd-tools.echo foo && cat strace.log
var sampleStraceSimple = []byte(`21616 1542882400.198907 execve("/snap/bin/test-snapd-tools.echo", ["test-snapd-tools.echo", "foo"], 0x7fff7f275f48 /* 27 vars */) = 0
21616 1542882400.204710 execve("/snap/core/current/usr/bin/snap", ["test-snapd-tools.echo", "foo"], 0xc42011c8c0 /* 27 vars */ <unfinished ...>
21621 1542882400.204845 +++ exited with 0 +++
21620 1542882400.204853 +++ exited with 0 +++
21619 1542882400.204857 +++ exited with 0 +++
21618 1542882400.204861 +++ exited with 0 +++
21617 1542882400.204875 +++ exited with 0 +++
21616 1542882400.205199 <... execve resumed> ) = 0
21616 1542882400.220845 execve("/snap/core/5976/usr/lib/snapd/snap-confine", ["/snap/core/5976/usr/lib/snapd/sn"..., "snap.test-snapd-tools.echo", "/usr/lib/snapd/snap-exec", "test-snapd-tools.echo", "foo"], 0xc8200a3600 /* 41 vars */ <unfinished ...>
21625 1542882400.220994 +++ exited with 0 +++
21624 1542882400.220999 +++ exited with 0 +++
21623 1542882400.221002 +++ exited with 0 +++
21622 1542882400.221005 +++ exited with 0 +++
21616 1542882400.221634 <... execve resumed> ) = 0
21629 1542882400.356625 execveat(3, "", ["snap-update-ns", "--from-snap-confine", "test-snapd-tools"], 0x7ffeaf4faa40 /* 0 vars */, AT_EMPTY_PATH) = 0
21631 1542882400.360708 +++ exited with 0 +++
21632 1542882400.360723 +++ exited with 0 +++
21630 1542882400.360727 +++ exited with 0 +++
21633 1542882400.360842 +++ exited with 0 +++
21629 1542882400.360848 +++ exited with 0 +++
21616 1542882400.360869 --- SIGCHLD {si_signo=SIGCHLD, si_code=CLD_EXITED, si_pid=21629, si_uid=1000, si_status=0, si_utime=0, si_stime=0} ---
21626 1542882400.375793 +++ exited with 0 +++
21616 1542882400.375836 --- SIGCHLD {si_signo=SIGCHLD, si_code=CLD_EXITED, si_pid=21626, si_uid=1000, si_status=0, si_utime=0, si_stime=0} ---
21616 1542882400.377349 execve("/usr/lib/snapd/snap-exec", ["/usr/lib/snapd/snap-exec", "test-snapd-tools.echo", "foo"], 0x23ebc80 /* 45 vars */) = 0
21616 1542882400.383698 execve("/snap/test-snapd-tools/6/bin/echo", ["/snap/test-snapd-tools/6/bin/ech"..., "foo"], 0xc420072f00 /* 47 vars */ <unfinished ...>
21638 1542882400.383855 +++ exited with 0 +++
21637 1542882400.383862 +++ exited with 0 +++
21636 1542882400.383877 +++ exited with 0 +++
21634 1542882400.383884 +++ exited with 0 +++
21635 1542882400.383890 +++ exited with 0 +++
21616 1542882400.384105 <... execve resumed> ) = 0
21616 1542882400.384974 +++ exited with 0 +++
`)

func (s *timingSuite) TestTraceExecveTimings(c *C) {
	f := mylog.Check2(os.CreateTemp("", "strace-extract-test-"))

	defer os.Remove(f.Name())
	_ = mylog.Check2(f.Write(sampleStraceSimple))

	f.Sync()

	st := mylog.Check2(strace.TraceExecveTimings(f.Name(), 10))

	c.Assert(st.TotalTime, Equals, 0.1860671043395996)
	c.Assert(st.ExeRuntimes(), DeepEquals, []strace.ExeRuntime{
		{Exe: "/snap/bin/test-snapd-tools.echo", TotalSec: 0.005803108215332031},
		{Exe: "/snap/core/current/usr/bin/snap", TotalSec: 0.016134977340698242},
		{Exe: "snap-update-ns", TotalSec: 0.0042438507080078125},
		{Exe: "/snap/core/5976/usr/lib/snapd/snap-confine", TotalSec: 0.15650391578674316},
		{Exe: "/usr/lib/snapd/snap-exec", TotalSec: 0.006349086761474609},
	})
}
