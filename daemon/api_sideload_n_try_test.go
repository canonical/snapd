// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package daemon_test

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

var (
	_ = check.Suite(&sideloadSuite{})
	_ = check.Suite(&trySuite{})
)

type sideloadSuite struct {
	apiBaseSuite
}

var sideLoadBodyWithoutDevMode = "" +
	"----hello--\r\n" +
	"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
	"\r\n" +
	"xyzzy\r\n" +
	"----hello--\r\n" +
	"Content-Disposition: form-data; name=\"dangerous\"\r\n" +
	"\r\n" +
	"true\r\n" +
	"----hello--\r\n" +
	"Content-Disposition: form-data; name=\"snap-path\"\r\n" +
	"\r\n" +
	"a/b/local.snap\r\n" +
	"----hello--\r\n"

func (s *sideloadSuite) TestSideloadSnapOnNonDevModeDistro(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	chgSummary := s.sideloadCheck(c, body, head, "local", snapstate.Flags{RemoveSnapPath: true})
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "a/b/local.snap"`)
}

func (s *sideloadSuite) TestSideloadSnapOnDevModeDistro(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	restore := sandbox.MockForceDevMode(true)
	defer restore()
	flags := snapstate.Flags{RemoveSnapPath: true}
	chgSummary := s.sideloadCheck(c, body, head, "local", flags)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "a/b/local.snap"`)
}

func (s *sideloadSuite) TestSideloadSnapDevMode(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	// try a multipart/form-data upload
	flags := snapstate.Flags{RemoveSnapPath: true}
	flags.DevMode = true
	chgSummary := s.sideloadCheck(c, body, head, "local", flags)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "x"`)
}

func (s *sideloadSuite) TestSideloadSnapJailMode(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"jailmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"dangerous\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	// try a multipart/form-data upload
	flags := snapstate.Flags{JailMode: true, RemoveSnapPath: true}
	chgSummary := s.sideloadCheck(c, body, head, "local", flags)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "x"`)
}

func (s *sideloadSuite) sideloadCheck(c *check.C, content string, head map[string]string, expectedInstanceName string, expectedFlags snapstate.Flags) string {
	d := s.daemonWithFakeSnapManager(c)

	soon := 0
	var origEnsureStateSoon func(*state.State)
	origEnsureStateSoon, restore := daemon.MockEnsureStateSoon(func(st *state.State) {
		soon++
		origEnsureStateSoon(st)
	})
	defer restore()

	c.Assert(expectedInstanceName != "", check.Equals, true, check.Commentf("expected instance name must be set"))
	mockedName, _ := snap.SplitInstanceName(expectedInstanceName)

	// setup done
	installQueue := []string{}
	defer daemon.MockUnsafeReadSnapInfo(func(path string) (*snap.Info, error) {
		return &snap.Info{SuggestedName: mockedName}, nil
	})()

	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		// NOTE: ubuntu-core is not installed in developer mode
		c.Check(flags, check.Equals, snapstate.Flags{})
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	defer daemon.MockSnapstateInstallPath(func(s *state.State, si *snap.SideInfo, path, name, channel string, flags snapstate.Flags) (*state.TaskSet, *snap.Info, error) {
		c.Check(flags, check.DeepEquals, expectedFlags)

		c.Check(path, testutil.FileEquals, "xyzzy")

		c.Check(name, check.Equals, expectedInstanceName)

		installQueue = append(installQueue, si.RealName+"::"+path)
		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), &snap.Info{SuggestedName: name}, nil
	})()

	buf := bytes.NewBufferString(content)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	for k, v := range head {
		req.Header.Set(k, v)
	}

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeAsync)
	n := 1
	c.Assert(installQueue, check.HasLen, n)
	c.Check(installQueue[n-1], check.Matches, "local::.*/"+regexp.QuoteMeta(dirs.LocalInstallBlobTempPrefix)+".*")

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	c.Check(soon, check.Equals, 1)

	c.Assert(chg.Tasks(), check.HasLen, n)

	st.Unlock()
	s.waitTrivialChange(c, chg)
	st.Lock()

	c.Check(chg.Kind(), check.Equals, "install-snap")
	var names []string
	err = chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{expectedInstanceName})
	var apiData map[string]interface{}
	err = chg.Get("api-data", &apiData)
	c.Assert(err, check.IsNil)
	c.Check(apiData, check.DeepEquals, map[string]interface{}{
		"snap-name": expectedInstanceName,
	})

	return chg.Summary()
}

func (s *sideloadSuite) TestSideloadSnapJailModeAndDevmode(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"jailmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	s.daemonWithOverlordMock(c)

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, check.Equals, "cannot use devmode and jailmode flags together")
}

func (s *sideloadSuite) TestSideloadSnapJailModeInDevModeOS(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"jailmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	s.daemonWithOverlordMock(c)

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	restore := sandbox.MockForceDevMode(true)
	defer restore()

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, check.Equals, "this system cannot honour the jailmode flag")
}

func (s *sideloadSuite) TestLocalInstallSnapDeriveSideInfo(c *check.C) {
	d := s.daemonWithOverlordMock(c)
	// add the assertions first
	st := d.Overlord().State()

	dev1Acct := assertstest.NewAccount(s.StoreSigning, "devel1", nil, "")

	snapDecl, err := s.StoreSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "x-id",
		"snap-name":    "x",
		"publisher-id": dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)

	snapRev, err := s.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": "YK0GWATaZf09g_fvspYPqm_qtaiqf-KjaNj5uMEQCjQpuXWPjqQbeBINL5H_A0Lo",
		"snap-size":     "5",
		"snap-id":       "x-id",
		"snap-revision": "41",
		"developer-id":  dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)

	func() {
		st.Lock()
		defer st.Unlock()
		assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""), dev1Acct, snapDecl, snapRev)
	}()

	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x.snap\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n"
	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	defer daemon.MockSnapstateInstallPath(func(s *state.State, si *snap.SideInfo, path, name, channel string, flags snapstate.Flags) (*state.TaskSet, *snap.Info, error) {
		c.Check(flags, check.Equals, snapstate.Flags{RemoveSnapPath: true})
		c.Check(si, check.DeepEquals, &snap.SideInfo{
			RealName: "x",
			SnapID:   "x-id",
			Revision: snap.R(41),
		})

		return state.NewTaskSet(), &snap.Info{SuggestedName: "x"}, nil
	})()

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeAsync)

	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Summary(), check.Equals, `Install "x" snap from file "x.snap"`)
	var names []string
	err = chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{"x"})
	var apiData map[string]interface{}
	err = chg.Get("api-data", &apiData)
	c.Assert(err, check.IsNil)
	c.Check(apiData, check.DeepEquals, map[string]interface{}{
		"snap-name": "x",
	})
}

func (s *sideloadSuite) TestSideloadSnapNoSignaturesDangerOff(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n"
	s.daemonWithOverlordMock(c)

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	// this is the prefix used for tempfiles for sideloading
	glob := filepath.Join(os.TempDir(), "snapd-sideload-pkg-*")
	glbBefore, _ := filepath.Glob(glob)
	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, check.Equals, `cannot find signatures with metadata for snap "x"`)
	glbAfter, _ := filepath.Glob(glob)
	c.Check(len(glbBefore), check.Equals, len(glbAfter))
}

func (s *sideloadSuite) TestSideloadSnapNotValidFormFile(c *check.C) {
	s.daemon(c)

	// try a multipart/form-data upload with missing "name"
	content := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}

	buf := bytes.NewBufferString(content)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	for k, v := range head {
		req.Header.Set(k, v)
	}

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Assert(rsp.Result.(*daemon.ErrorResult).Message, check.Matches, `cannot find "snap" file field in provided multipart/form-data payload`)
}

func (s *sideloadSuite) TestSideloadSnapChangeConflict(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"dangerous\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	s.daemonWithOverlordMock(c)

	defer daemon.MockUnsafeReadSnapInfo(func(path string) (*snap.Info, error) {
		return &snap.Info{SuggestedName: "foo"}, nil
	})()

	defer daemon.MockSnapstateInstallPath(func(s *state.State, si *snap.SideInfo, path, name, channel string, flags snapstate.Flags) (*state.TaskSet, *snap.Info, error) {
		return nil, nil, &snapstate.ChangeConflictError{Snap: "foo"}
	})()

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Result.(*daemon.ErrorResult).Kind, check.Equals, client.ErrorKindSnapChangeConflict)
}

func (s *sideloadSuite) TestSideloadSnapInstanceName(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode +
		"Content-Disposition: form-data; name=\"name\"\r\n" +
		"\r\n" +
		"local_instance\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	chgSummary := s.sideloadCheck(c, body, head, "local_instance", snapstate.Flags{RemoveSnapPath: true})
	c.Check(chgSummary, check.Equals, `Install "local_instance" snap from file "a/b/local.snap"`)
}

func (s *sideloadSuite) TestSideloadSnapInstanceNameNoKey(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode +
		"Content-Disposition: form-data; name=\"name\"\r\n" +
		"\r\n" +
		"local\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	chgSummary := s.sideloadCheck(c, body, head, "local", snapstate.Flags{RemoveSnapPath: true})
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "a/b/local.snap"`)
}

func (s *sideloadSuite) TestSideloadSnapInstanceNameMismatch(c *check.C) {
	s.daemonWithFakeSnapManager(c)

	defer daemon.MockUnsafeReadSnapInfo(func(path string) (*snap.Info, error) {
		return &snap.Info{SuggestedName: "bar"}, nil
	})()

	body := sideLoadBodyWithoutDevMode +
		"Content-Disposition: form-data; name=\"name\"\r\n" +
		"\r\n" +
		"foo_instance\r\n" +
		"----hello--\r\n"

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, check.Equals, `instance name "foo_instance" does not match snap name "bar"`)
}

func (s *sideloadSuite) TestInstallPathUnaliased(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"unaliased\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	// try a multipart/form-data upload
	flags := snapstate.Flags{Unaliased: true, RemoveSnapPath: true, DevMode: true}
	chgSummary := s.sideloadCheck(c, body, head, "local", flags)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "x"`)
}

type trySuite struct {
	apiBaseSuite
}

func (s *trySuite) TestTrySnap(c *check.C) {
	d := s.daemonWithFakeSnapManager(c)

	var err error

	// mock a try dir
	tryDir := c.MkDir()
	snapYaml := filepath.Join(tryDir, "meta", "snap.yaml")
	err = os.MkdirAll(filepath.Dir(snapYaml), 0755)
	c.Assert(err, check.IsNil)
	err = ioutil.WriteFile(snapYaml, []byte("name: foo\nversion: 1.0\n"), 0644)
	c.Assert(err, check.IsNil)

	reqForFlags := func(f snapstate.Flags) *http.Request {
		b := "" +
			"--hello\r\n" +
			"Content-Disposition: form-data; name=\"action\"\r\n" +
			"\r\n" +
			"try\r\n" +
			"--hello\r\n" +
			"Content-Disposition: form-data; name=\"snap-path\"\r\n" +
			"\r\n" +
			tryDir + "\r\n" +
			"--hello"

		snip := "\r\n" +
			"Content-Disposition: form-data; name=%q\r\n" +
			"\r\n" +
			"true\r\n" +
			"--hello"

		if f.DevMode {
			b += fmt.Sprintf(snip, "devmode")
		}
		if f.JailMode {
			b += fmt.Sprintf(snip, "jailmode")
		}
		if f.Classic {
			b += fmt.Sprintf(snip, "classic")
		}
		b += "--\r\n"

		req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(b))
		c.Assert(err, check.IsNil)
		req.Header.Set("Content-Type", "multipart/thing; boundary=hello")

		return req
	}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()

	soon := 0
	var origEnsureStateSoon func(*state.State)
	origEnsureStateSoon, restore := daemon.MockEnsureStateSoon(func(st *state.State) {
		soon++
		origEnsureStateSoon(st)
	})
	defer restore()

	for _, t := range []struct {
		flags snapstate.Flags
		desc  string
	}{
		{snapstate.Flags{}, "core; -"},
		{snapstate.Flags{DevMode: true}, "core; devmode"},
		{snapstate.Flags{JailMode: true}, "core; jailmode"},
		{snapstate.Flags{Classic: true}, "core; classic"},
	} {
		soon = 0

		tryWasCalled := true
		defer daemon.MockSnapstateTryPath(func(s *state.State, name, path string, flags snapstate.Flags) (*state.TaskSet, error) {
			c.Check(flags, check.DeepEquals, t.flags, check.Commentf(t.desc))
			tryWasCalled = true
			t := s.NewTask("fake-install-snap", "Doing a fake try")
			return state.NewTaskSet(t), nil
		})()

		defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
			if name != "core" {
				c.Check(flags, check.DeepEquals, t.flags, check.Commentf(t.desc))
			}
			t := s.NewTask("fake-install-snap", "Doing a fake install")
			return state.NewTaskSet(t), nil
		})()

		// try the snap (without an installed core)
		st.Unlock()
		rsp := s.req(c, reqForFlags(t.flags), nil).(*daemon.Resp)
		st.Lock()
		c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeAsync, check.Commentf(t.desc))
		c.Assert(tryWasCalled, check.Equals, true, check.Commentf(t.desc))

		chg := st.Change(rsp.Change)
		c.Assert(chg, check.NotNil, check.Commentf(t.desc))

		c.Assert(chg.Tasks(), check.HasLen, 1, check.Commentf(t.desc))

		st.Unlock()
		s.waitTrivialChange(c, chg)
		st.Lock()

		c.Check(chg.Kind(), check.Equals, "try-snap", check.Commentf(t.desc))
		c.Check(chg.Summary(), check.Equals, fmt.Sprintf(`Try "%s" snap from %s`, "foo", tryDir), check.Commentf(t.desc))
		var names []string
		err = chg.Get("snap-names", &names)
		c.Assert(err, check.IsNil, check.Commentf(t.desc))
		c.Check(names, check.DeepEquals, []string{"foo"}, check.Commentf(t.desc))
		var apiData map[string]interface{}
		err = chg.Get("api-data", &apiData)
		c.Assert(err, check.IsNil, check.Commentf(t.desc))
		c.Check(apiData, check.DeepEquals, map[string]interface{}{
			"snap-name": "foo",
		}, check.Commentf(t.desc))

		c.Check(soon, check.Equals, 1, check.Commentf(t.desc))
	}
}

func (s *trySuite) TestTrySnapRelative(c *check.C) {
	d := s.daemon(c)
	st := d.Overlord().State()

	req, err := http.NewRequest("POST", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := daemon.TrySnap(st, req, nil, "relative-path", snapstate.Flags{}).(*daemon.Resp)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, testutil.Contains, "need an absolute path")
}

func (s *trySuite) TestTrySnapNotDir(c *check.C) {
	d := s.daemon(c)
	st := d.Overlord().State()

	req, err := http.NewRequest("POST", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := daemon.TrySnap(st, req, nil, "/does/not/exist", snapstate.Flags{}).(*daemon.Resp)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, testutil.Contains, "not a snap directory")
}

func (s *trySuite) TestTryChangeConflict(c *check.C) {
	d := s.daemonWithOverlordMock(c)
	st := d.Overlord().State()

	// mock a try dir
	tryDir := c.MkDir()

	defer daemon.MockUnsafeReadSnapInfo(func(path string) (*snap.Info, error) {
		return &snap.Info{SuggestedName: "foo"}, nil
	})()

	defer daemon.MockSnapstateTryPath(func(s *state.State, name, path string, flags snapstate.Flags) (*state.TaskSet, error) {
		return nil, &snapstate.ChangeConflictError{Snap: "foo"}
	})()

	req, err := http.NewRequest("POST", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := daemon.TrySnap(st, req, nil, tryDir, snapstate.Flags{}).(*daemon.Resp)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Result.(*daemon.ErrorResult).Kind, check.Equals, client.ErrorKindSnapChangeConflict)
}
