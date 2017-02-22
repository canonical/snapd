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
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/mgo.v2/bson"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/errtracker"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type ErrtrackerTestSuite struct {
	restorer []func()
}

var _ = Suite(&ErrtrackerTestSuite{})

func (s *ErrtrackerTestSuite) SetUpTest(c *C) {
	p := filepath.Join(c.MkDir(), "machine-id")
	err := ioutil.WriteFile(p, []byte("bbb1a6a5bcdb418380056a2d759c3f7c"), 0644)
	c.Assert(err, IsNil)
	s.restorer = append(s.restorer, errtracker.MockMachineIDPath(p))
	s.restorer = append(s.restorer, errtracker.MockUsrBinSnap("/bin/true"))
}

func (s *ErrtrackerTestSuite) TearDownTest(c *C) {
	for _, f := range s.restorer {
		f()
	}
}

func (s *ErrtrackerTestSuite) TestReport(c *C) {
	n := 0
	identifier := ""
	usrBinSnapID, err := osutil.GetBuildID("/bin/true")
	c.Assert(err, IsNil)

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
				"ProblemType":        "Snap",
				"DistroRelease":      fmt.Sprintf("%s %s", strings.Title(release.ReleaseInfo.ID), release.ReleaseInfo.VersionID),
				"UsrBinSnapBuildID":  usrBinSnapID.String(),
				"Snap":               "some-snap",
				"Date":               "Fri Feb 17 09:51:00 2017",
				"Channel":            "beta",
				"ErrorMessage":       "failed to do stuff",
				"DuplicateSignature": "snap-install: failed to do stuff",
				"Architecture":       arch.UbuntuArchitecture(),
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

	id, err := errtracker.Report("some-snap", "beta", "failed to do stuff")
	c.Check(err, IsNil)
	c.Check(id, Equals, "c14388aa-f78d-11e6-8df0-fa163eaf9b83 OOPSID")
	c.Check(n, Equals, 1)

	// run again, verify identifier is unchanged
	id, err = errtracker.Report("some-other-snap", "edge", "failed to do more stuff")
	c.Check(err, IsNil)
	c.Check(id, Equals, "c14388aa-f78d-11e6-8df0-fa163eaf9b83 OOPSID")
	c.Check(n, Equals, 2)
}
