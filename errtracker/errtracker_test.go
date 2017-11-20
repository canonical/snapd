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

	s.hostBuildID, err = osutil.ReadBuildID(truePath)
	c.Assert(err, IsNil)
	s.coreBuildID, err = osutil.ReadBuildID(falsePath)
	c.Assert(err, IsNil)
	if release.ReleaseInfo.ID == "ubuntu" {
		s.distroRelease = fmt.Sprintf("%s %s", strings.Title(release.ReleaseInfo.ID), release.ReleaseInfo.VersionID)
	} else {
		s.distroRelease = fmt.Sprintf("%s %s", release.ReleaseInfo.ID, release.ReleaseInfo.VersionID)
	}
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
				"KernelVersion":      release.KernelVersion(),
				"ErrorMessage":       "failed to do stuff",
				"DuplicateSignature": "[failed to do stuff]",
				"Architecture":       arch.UbuntuArchitecture(),
				"DidSnapdReExec":     "yes",

				"ProblemType": "Snap",
				"Snap":        "some-snap",
				"Channel":     "beta",

				"MD5SumSnapConfineAppArmorProfile":            "7a7aa5f21063170c1991b84eb8d86de1",
				"MD5SumSnapConfineAppArmorProfileDpkgNew":     "93b885adfe0da089cdf634904fd59f71",
				"MD5SumSnapConfineAppArmorProfileReal":        "93b885adfe0da089cdf634904fd59f71",
				"MD5SumSnapConfineAppArmorProfileRealDpkgNew": "93b885adfe0da089cdf634904fd59f71",
			})
			fmt.Fprintf(w, "c14388aa-f78d-11e6-8df0-fa163eaf9b83 OOPSID")
		case 1:
			c.Check(r.Method, Equals, "POST")
			c.Check(r.URL.Path, Matches, identifier)
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

	id, err := errtracker.Report("some-snap", "failed to do stuff", "[failed to do stuff]", map[string]string{
		"Channel": "beta",
	})
	c.Check(err, IsNil)
	c.Check(id, Equals, "c14388aa-f78d-11e6-8df0-fa163eaf9b83 OOPSID")
	c.Check(n, Equals, 1)

	// run again, verify identifier is unchanged
	id, err = errtracker.Report("some-other-snap", "failed to do more stuff", "[failed to do more stuff]", nil)
	c.Check(err, IsNil)
	c.Check(id, Equals, "c14388aa-f78d-11e6-8df0-fa163eaf9b83 OOPSID")
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
				"KernelVersion":    release.KernelVersion(),
				"Architecture":     arch.UbuntuArchitecture(),
				"DidSnapdReExec":   "yes",

				"ProblemType":        "Repair",
				"Repair":             `"repair (1; brand-id:canonical)"`,
				"ErrorMessage":       "failure in script",
				"DuplicateSignature": "[dupSig]",
				"BrandID":            "canonical",
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
