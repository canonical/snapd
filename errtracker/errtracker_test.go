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

package errtracker_test

import (
	"crypto/sha512"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/mgo.v2/bson"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/errtracker"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type ErrtrackerTestSuite struct {
	testutil.BaseTest

	tmpdir string

	hostBuildID   string
	coreBuildID   string
	distroRelease string
}

var _ = Suite(&ErrtrackerTestSuite{})

var truePath = osutil.LookPathDefault("true", "/bin/true")
var falsePath = osutil.LookPathDefault("false", "/bin/false")

const someJournalEntry = "Mar 29 22:08:00 localhost kernel: [81B blob data]"

func (s *ErrtrackerTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)

	p := filepath.Join(s.tmpdir, "machine-id")
	err := ioutil.WriteFile(p, []byte("bbb1a6a5bcdb418380056a2d759c3f7c"), 0644)
	c.Assert(err, IsNil)
	s.AddCleanup(errtracker.MockMachineIDPaths([]string{p}))
	s.AddCleanup(errtracker.MockHostSnapd(truePath))
	s.AddCleanup(errtracker.MockCoreSnapd(falsePath))
	s.AddCleanup(errtracker.MockReExec(func() string {
		return "yes"
	}))
	mockDetectVirt := testutil.MockCommand(c, "systemd-detect-virt", "echo none")
	s.AddCleanup(mockDetectVirt.Restore)

	s.hostBuildID, err = osutil.ReadBuildID(truePath)
	c.Assert(err, IsNil)
	s.coreBuildID, err = osutil.ReadBuildID(falsePath)
	c.Assert(err, IsNil)
	if release.ReleaseInfo.ID == "ubuntu" {
		s.distroRelease = fmt.Sprintf("%s %s", strings.Title(release.ReleaseInfo.ID), release.ReleaseInfo.VersionID)
	} else {
		s.distroRelease = fmt.Sprintf("%s %s", release.ReleaseInfo.ID, release.ReleaseInfo.VersionID)
	}

	mockCpuinfo := filepath.Join(s.tmpdir, "cpuinfo")
	mockSelfCmdline := filepath.Join(s.tmpdir, "self.cmdline")
	mockSelfExe := filepath.Join(s.tmpdir, "self.exe")
	mockSelfCwd := filepath.Join(s.tmpdir, "self.cwd")

	c.Assert(ioutil.WriteFile(mockCpuinfo, []byte(`
processor	: 0
bugs		: very yes
etc		: ...

processor	: 42
bugs		: very yes
`[1:]), 0644), IsNil)
	c.Assert(ioutil.WriteFile(mockSelfCmdline, []byte("foo\x00bar\x00baz"), 0644), IsNil)
	c.Assert(os.Symlink("target of /proc/self/exe", mockSelfExe), IsNil)
	c.Assert(os.Symlink("target of /proc/self/cwd", mockSelfCwd), IsNil)

	s.AddCleanup(errtracker.MockOsGetenv(func(s string) string {
		switch s {
		case "SHELL":
			return "/bin/sh"
		case "XDG_CURRENT_DESKTOP":
			return "Unity"
		}
		return ""
	}))
	s.AddCleanup(errtracker.MockProcCpuinfo(mockCpuinfo))
	s.AddCleanup(errtracker.MockProcSelfCmdline(mockSelfCmdline))
	s.AddCleanup(errtracker.MockProcSelfExe(mockSelfExe))
	s.AddCleanup(errtracker.MockProcSelfCwd(mockSelfCwd))
	s.AddCleanup(testutil.MockCommand(c, "journalctl", "echo "+someJournalEntry).Restore)
}

func (s *ErrtrackerTestSuite) TestReport(c *C) {
	n := 0
	identifier := ""

	snapConfineProfile := filepath.Join(s.tmpdir, "/etc/apparmor.d/usr.lib.snapd.snap-confine")
	err := os.MkdirAll(filepath.Dir(snapConfineProfile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(snapConfineProfile, []byte("# fake profile of snap-confine"), 0644)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(snapConfineProfile+".dpkg-new", []byte{0}, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(snapConfineProfile+".real", []byte{0}, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(snapConfineProfile+".real.dpkg-new", []byte{0}, 0644)
	c.Assert(err, IsNil)

	prev := errtracker.SnapdVersion
	defer func() { errtracker.SnapdVersion = prev }()
	errtracker.SnapdVersion = "some-snapd-version"

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Matches, "/[a-z0-9]+")
			identifier = r.URL.Path
			b, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)

			var data map[string]string
			err = bson.Unmarshal(b, &data)
			c.Assert(err, IsNil)
			c.Check(data, DeepEquals, map[string]string{
				"DistroRelease":      s.distroRelease,
				"HostSnapdBuildID":   s.hostBuildID,
				"CoreSnapdBuildID":   s.coreBuildID,
				"SnapdVersion":       "some-snapd-version",
				"Date":               "Fri Feb 17 09:51:00 2017",
				"KernelVersion":      osutil.KernelVersion(),
				"ErrorMessage":       "failed to do stuff",
				"DuplicateSignature": "[failed to do stuff]",
				"Architecture":       arch.UbuntuArchitecture(),
				"DidSnapdReExec":     "yes",

				"ProblemType": "Snap",
				"Snap":        "some-snap",
				"Channel":     "beta",

				"ProcCpuinfoMinimal": "processor\t: 42\nbugs\t\t: very yes\n",
				"ExecutablePath":     "target of /proc/self/exe",
				"ProcCwd":            "target of /proc/self/cwd",
				"ProcCmdline":        "foo\x00bar\x00baz",
				"ProcEnviron":        "SHELL=/bin/sh",
				"JournalError":       someJournalEntry + "\n",
				"SourcePackage":      "snapd",
				"CurrentDesktop":     "Unity",
				"DetectedVirt":       "none",

				"MD5SumSnapConfineAppArmorProfile":            "7a7aa5f21063170c1991b84eb8d86de1",
				"MD5SumSnapConfineAppArmorProfileDpkgNew":     "93b885adfe0da089cdf634904fd59f71",
				"MD5SumSnapConfineAppArmorProfileReal":        "93b885adfe0da089cdf634904fd59f71",
				"MD5SumSnapConfineAppArmorProfileRealDpkgNew": "93b885adfe0da089cdf634904fd59f71",
			})
			fmt.Fprintf(w, "c14388aa-f78d-11e6-8df0-fa163eaf9b83 OOPSID")
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Matches, identifier)
			fmt.Fprintf(w, "xxxxx-f78d-11e6-8df0-fa163eaf9b83 OOPSID")
		default:
			c.Fatalf("expected one request, got %d", n+1)
		}

		n++
	}

	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()
	restorer := errtracker.MockCrashDbURL(server.URL)
	defer restorer()
	restorer = errtracker.MockTimeNow(func() time.Time { return time.Date(2017, 2, 17, 9, 51, 0, 0, time.UTC) })
	defer restorer()

	id, err := errtracker.Report("some-snap", "failed to do stuff", "[failed to do stuff]", map[string]string{
		"Channel": "beta",
	})
	c.Check(err, IsNil)
	c.Check(id, Equals, "c14388aa-f78d-11e6-8df0-fa163eaf9b83 OOPSID")
	c.Check(n, Equals, 1)

	// run again with the *same* dupSig and verify that it won't send
	// that again
	id, err = errtracker.Report("some-snap", "failed to do stuff", "[failed to do stuff]", map[string]string{
		"Channel": "beta",
	})
	c.Check(err, IsNil)
	c.Check(id, Equals, "already-reported")
	c.Check(n, Equals, 1)

	// run again with different data, verify identifier is unchanged
	id, err = errtracker.Report("some-other-snap", "failed to do more stuff", "[failed to do more stuff]", nil)
	c.Check(err, IsNil)
	c.Check(id, Equals, "xxxxx-f78d-11e6-8df0-fa163eaf9b83 OOPSID")
	c.Check(n, Equals, 2)
}

func (s *ErrtrackerTestSuite) TestReportUnderTesting(c *C) {
	os.Setenv("SNAPPY_TESTING", "1")
	defer os.Unsetenv("SNAPPY_TESTING")

	n := 0
	prev := errtracker.SnapdVersion
	defer func() { errtracker.SnapdVersion = prev }()
	errtracker.SnapdVersion = "some-snapd-version"

	handler := func(w http.ResponseWriter, r *http.Request) {
		n++
	}

	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()
	restorer := errtracker.MockCrashDbURL(server.URL)
	defer restorer()
	restorer = errtracker.MockTimeNow(func() time.Time { return time.Date(2017, 2, 17, 9, 51, 0, 0, time.UTC) })
	defer restorer()

	id, err := errtracker.Report("some-snap", "failed to do stuff", "[failed to do stuff]", map[string]string{
		"Channel": "beta",
	})
	c.Check(err, IsNil)
	c.Check(id, Equals, "oops-not-sent")
	c.Check(n, Equals, 0)
}

func (s *ErrtrackerTestSuite) TestTriesAllKnownMachineIDs(c *C) {
	p := filepath.Join(c.MkDir(), "machine-id")
	machineID := []byte("bbb1a6a5bcdb418380056a2d759c3f7c")
	err := ioutil.WriteFile(p, machineID, 0644)
	c.Assert(err, IsNil)
	s.AddCleanup(errtracker.MockMachineIDPaths([]string{"/does/not/exist", p}))

	n := 0
	var identifiers []string
	handler := func(w http.ResponseWriter, r *http.Request) {
		identifiers = append(identifiers, r.URL.Path)
		n++
	}

	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()
	restorer := errtracker.MockCrashDbURL(server.URL)
	defer restorer()
	restorer = errtracker.MockTimeNow(func() time.Time { return time.Date(2017, 2, 17, 9, 51, 0, 0, time.UTC) })
	defer restorer()

	_, err = errtracker.Report("some-snap", "failed to do stuff", "[failed to do stuff]", map[string]string{
		"Channel": "beta",
	})
	c.Check(err, IsNil)
	c.Check(n, Equals, 1)
	c.Check(identifiers, DeepEquals, []string{fmt.Sprintf("/%x", sha512.Sum512(machineID))})
}

func (s *ErrtrackerTestSuite) TestReportRepair(c *C) {
	n := 0
	prev := errtracker.SnapdVersion
	defer func() { errtracker.SnapdVersion = prev }()
	errtracker.SnapdVersion = "some-snapd-version"

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Matches, "/[a-z0-9]+")
			b, err := ioutil.ReadAll(r.Body)
			c.Assert(err, IsNil)

			var data map[string]string
			err = bson.Unmarshal(b, &data)
			c.Assert(err, IsNil)
			c.Check(data, DeepEquals, map[string]string{
				"DistroRelease":    s.distroRelease,
				"HostSnapdBuildID": s.hostBuildID,
				"CoreSnapdBuildID": s.coreBuildID,
				"SnapdVersion":     "some-snapd-version",
				"Date":             "Fri Feb 17 09:51:00 2017",
				"KernelVersion":    osutil.KernelVersion(),
				"Architecture":     arch.UbuntuArchitecture(),
				"DidSnapdReExec":   "yes",

				"ProblemType":        "Repair",
				"Repair":             `"repair (1; brand-id:canonical)"`,
				"ErrorMessage":       "failure in script",
				"DuplicateSignature": "[dupSig]",
				"BrandID":            "canonical",

				"ProcCpuinfoMinimal": "processor\t: 42\nbugs\t\t: very yes\n",
				"ExecutablePath":     "target of /proc/self/exe",
				"ProcCwd":            "target of /proc/self/cwd",
				"ProcCmdline":        "foo\x00bar\x00baz",
				"ProcEnviron":        "SHELL=/bin/sh",
				"JournalError":       someJournalEntry + "\n",
				"SourcePackage":      "snapd",
				"CurrentDesktop":     "Unity",
				"DetectedVirt":       "none",
			})
			fmt.Fprintf(w, "c14388aa-f78d-11e6-8df0-fa163eaf9b83 OOPSID")
		default:
			c.Fatalf("expected one request, got %d", n+1)
		}

		n++
	}

	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()
	restorer := errtracker.MockCrashDbURL(server.URL)
	defer restorer()
	restorer = errtracker.MockTimeNow(func() time.Time { return time.Date(2017, 2, 17, 9, 51, 0, 0, time.UTC) })
	defer restorer()

	id, err := errtracker.ReportRepair(`"repair (1; brand-id:canonical)"`, "failure in script", "[dupSig]", map[string]string{
		"BrandID": "canonical",
	})
	c.Check(err, IsNil)
	c.Check(id, Equals, "c14388aa-f78d-11e6-8df0-fa163eaf9b83 OOPSID")
	c.Check(n, Equals, 1)
}

func (s *ErrtrackerTestSuite) TestReportWithWhoopsieDisabled(c *C) {
	mockCmd := testutil.MockCommand(c, "systemctl", "echo disabled; exit 1")
	defer mockCmd.Restore()

	handler := func(w http.ResponseWriter, r *http.Request) {
		c.Fatalf("The server should not be hit from here")
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()
	restorer := errtracker.MockCrashDbURL(server.URL)
	defer restorer()

	id, err := errtracker.Report("some-snap", "failed to do stuff", "[failed to do stuff]", nil)
	c.Check(err, IsNil)
	c.Check(id, Equals, "")
}

func (s *ErrtrackerTestSuite) TestReportWithNoWhoopsieInstalled(c *C) {
	mockCmd := testutil.MockCommand(c, "systemctl", "echo Failed to get unit file state for whoopsie.service; exit 1")
	defer mockCmd.Restore()

	n := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "1234-oopsid")
		n++
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()
	restorer := errtracker.MockCrashDbURL(server.URL)
	defer restorer()

	id, err := errtracker.Report("some-snap", "failed to do stuff", "[failed to do stuff]", nil)
	c.Check(err, IsNil)
	c.Check(id, Equals, "1234-oopsid")
	c.Check(n, Equals, 1)
}

func (s *ErrtrackerTestSuite) TestProcCpuinfo(c *C) {
	fn := filepath.Join(s.tmpdir, "cpuinfo")
	// sanity check
	buf, err := ioutil.ReadFile(fn)
	c.Assert(err, IsNil)
	c.Check(string(buf), Equals, `
processor	: 0
bugs		: very yes
etc		: ...

processor	: 42
bugs		: very yes
`[1:])

	// just the last processor entry
	c.Check(errtracker.ProcCpuinfoMinimal(), Equals, `
processor	: 42
bugs		: very yes
`[1:])

	// if no processor line, just return the whole thing
	c.Assert(ioutil.WriteFile(fn, []byte("yadda yadda\n"), 0644), IsNil)
	c.Check(errtracker.ProcCpuinfoMinimal(), Equals, "yadda yadda\n")

	c.Assert(os.Remove(fn), IsNil)
	c.Check(errtracker.ProcCpuinfoMinimal(), Matches, "error: .* no such file or directory")
}

func (s *ErrtrackerTestSuite) TestProcExe(c *C) {
	c.Check(errtracker.ProcExe(), Equals, "target of /proc/self/exe")
	c.Assert(os.Remove(filepath.Join(s.tmpdir, "self.exe")), IsNil)
	c.Check(errtracker.ProcExe(), Matches, "error: .* no such file or directory")
}

func (s *ErrtrackerTestSuite) TestProcCwd(c *C) {
	c.Check(errtracker.ProcCwd(), Equals, "target of /proc/self/cwd")
	c.Assert(os.Remove(filepath.Join(s.tmpdir, "self.cwd")), IsNil)
	c.Check(errtracker.ProcCwd(), Matches, "error: .* no such file or directory")
}

func (s *ErrtrackerTestSuite) TestProcCmdline(c *C) {
	c.Check(errtracker.ProcCmdline(), Equals, "foo\x00bar\x00baz")
	c.Assert(os.Remove(filepath.Join(s.tmpdir, "self.cmdline")), IsNil)
	c.Check(errtracker.ProcCmdline(), Matches, "error: .* no such file or directory")
}

func (s *ErrtrackerTestSuite) TestJournalError(c *C) {
	jctl := testutil.MockCommand(c, "journalctl", "echo "+someJournalEntry)
	defer jctl.Restore()
	c.Check(errtracker.JournalError(), Equals, someJournalEntry+"\n")
	c.Check(jctl.Calls(), DeepEquals, [][]string{
		{"journalctl", "-b", "--priority=warning..err", "--lines=1000"},
	})
}

func (s *ErrtrackerTestSuite) TestJournalErrorSilentError(c *C) {
	jctl := testutil.MockCommand(c, "journalctl", "kill $$")
	defer jctl.Restore()
	c.Check(errtracker.JournalError(), Matches, "error: signal: [Tt]erminated")
	c.Check(jctl.Calls(), DeepEquals, [][]string{
		{"journalctl", "-b", "--priority=warning..err", "--lines=1000"},
	})
}

func (s *ErrtrackerTestSuite) TestJournalErrorError(c *C) {
	jctl := testutil.MockCommand(c, "journalctl", "echo OOPS; exit 1")
	defer jctl.Restore()
	c.Check(errtracker.JournalError(), Equals, "OOPS\n\nerror: exit status 1")
	c.Check(jctl.Calls(), DeepEquals, [][]string{
		{"journalctl", "-b", "--priority=warning..err", "--lines=1000"},
	})
}

func (s *ErrtrackerTestSuite) TestEnviron(c *C) {
	defer errtracker.MockOsGetenv(func(s string) string {
		switch s {
		case "SHELL":
			// marked as safe
			return "/bin/sh"
		case "GPG_AGENT_INFO":
			// not marked as safe
			return ".gpg-agent:0:1"
		case "TERM":
			// not really set
			return ""
		case "PATH":
			// special handling from here down
			return "/something/random:/sbin/:/home/ubuntu/bin:/bin:/snap/bin"
		case "XDG_RUNTIME_DIR":
			return "/some/thing"
		case "LD_PRELOAD":
			return "foo"
		case "LD_LIBRARY_PATH":
			return "bar"
		}
		return ""
	})()

	env := strings.Split(errtracker.Environ(), "\n")
	sort.Strings(env)

	c.Check(env, DeepEquals, []string{
		"LD_LIBRARY_PATH=<set>",
		"LD_PRELOAD=<set>",
		// note also /sbin/ -> /sbin
		"PATH=(custom):/sbin:(user):/bin:/snap/bin",
		"SHELL=/bin/sh",
		"XDG_RUNTIME_DIR=<set>",
	})
}

func (s *ErrtrackerTestSuite) TestReportsDB(c *C) {
	db, err := errtracker.NewReportsDB(filepath.Join(s.tmpdir, "foo.db"))
	c.Assert(err, IsNil)

	c.Check(db.AlreadyReported("some-dup-sig"), Equals, false)

	err = db.MarkReported("some-dup-sig")
	c.Check(err, IsNil)

	c.Check(db.AlreadyReported("some-dup-sig"), Equals, true)
	c.Check(db.AlreadyReported("other-dup-sig"), Equals, false)
}

func (s *ErrtrackerTestSuite) TestReportsDBCleanup(c *C) {
	db, err := errtracker.NewReportsDB(filepath.Join(s.tmpdir, "foo.db"))
	c.Assert(err, IsNil)

	errtracker.SetReportDBCleanupTime(db, 1*time.Millisecond)

	err = db.MarkReported("some-dup-sig")
	c.Check(err, IsNil)

	time.Sleep(10 * time.Millisecond)
	err = db.MarkReported("other-dup-sig")
	c.Check(err, IsNil)

	// this one got cleaned out
	c.Check(db.AlreadyReported("some-dup-sig"), Equals, false)
	// this one is still fresh
	c.Check(db.AlreadyReported("other-dup-sig"), Equals, true)
}

func (s *ErrtrackerTestSuite) TestReportsDBnilDoesNotCrash(c *C) {
	db, err := errtracker.NewReportsDB("/proc/1/environ")
	c.Assert(err, NotNil)
	c.Check(db, IsNil)

	c.Check(db.AlreadyReported("dupSig"), Equals, false)
	c.Check(db.MarkReported("dupSig"), ErrorMatches, "cannot mark error report as reported with an uninitialized reports database")
}
