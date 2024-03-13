// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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
	"crypto/rand"
	"errors"
	"fmt"
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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
)

var (
	_ = check.Suite(&sideloadSuite{})
	_ = check.Suite(&trySuite{})
)

type sideloadSuite struct {
	apiBaseSuite
}

func (s *sideloadSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})
}

func (s *sideloadSuite) markSeeded(d *daemon.Daemon) {
	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	st.Set("seeded", true)
	model := s.Brands.Model("can0nical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "gadget",
		"kernel":       "kernel",
	})
	snapstatetest.MockDeviceModel(model)
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
	chgSummary, systemRestartImmediate := s.sideloadCheck(c, body, head, "local", snapstate.Flags{RemoveSnapPath: true, Transaction: client.TransactionPerSnap})
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "a/b/local.snap"`)
	c.Check(systemRestartImmediate, check.Equals, false)
}

func (s *sideloadSuite) TestSideloadSnapOnDevModeDistro(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	restore := sandbox.MockForceDevMode(true)
	defer restore()
	flags := snapstate.Flags{RemoveSnapPath: true, Transaction: client.TransactionPerSnap}
	chgSummary, _ := s.sideloadCheck(c, body, head, "local", flags)
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
	flags := snapstate.Flags{RemoveSnapPath: true, Transaction: client.TransactionPerSnap}
	flags.DevMode = true
	chgSummary, _ := s.sideloadCheck(c, body, head, "local", flags)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "x"`)
}

func (s *sideloadSuite) TestSideloadSnapQuotaGroup(c *check.C) {
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
		"Content-Disposition: form-data; name=\"quota-group\"\r\n" +
		"\r\n" +
		"foo\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	// try a multipart/form-data upload
	flags := snapstate.Flags{
		RemoveSnapPath: true,
		DevMode:        true,
		Transaction:    client.TransactionPerSnap,
		QuotaGroupName: "foo",
	}
	chgSummary, _ := s.sideloadCheck(c, body, head, "local", flags)
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
	flags := snapstate.Flags{JailMode: true, RemoveSnapPath: true, Transaction: client.TransactionPerSnap}
	chgSummary, _ := s.sideloadCheck(c, body, head, "local", flags)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "x"`)
}

func (s *sideloadSuite) sideloadCheck(c *check.C, content string, head map[string]string, expectedInstanceName string, expectedFlags snapstate.Flags) (summary string, systemRestartImmediate bool) {
	d := s.daemonWithFakeSnapManager(c)
	s.markSeeded(d)

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

	defer daemon.MockSnapstateInstallPath(func(s *state.State, si *snap.SideInfo, path, name, channel string, flags snapstate.Flags, prqt snapstate.PrereqTracker) (*state.TaskSet, *snap.Info, error) {
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

	rsp := s.asyncReq(c, req, nil)
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
		"snap-name":  expectedInstanceName,
		"snap-names": []interface{}{expectedInstanceName},
	})

	summary = chg.Summary()
	err = chg.Get("system-restart-immediate", &systemRestartImmediate)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		c.Error(err)
	}
	return summary, systemRestartImmediate
}

const sideLoadComponentBody = "" +
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
	"a/b/local+localcomp.comp\r\n" +
	"----hello--\r\n"

func (s *sideloadSuite) TestSideloadComponent(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadComponentBody
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	flags := snapstate.Flags{RemoveSnapPath: true, Transaction: client.TransactionPerSnap}
	csi := snap.NewComponentSideInfo(naming.NewComponentRef("local", "comp"), snap.Revision{})

	chgSummary, systemRestartImmediate := s.sideloadComponentCheck(c, body, head, "local", flags, csi)
	c.Check(chgSummary, check.Equals, `Install "comp" component for "local" snap from file "a/b/local+localcomp.comp"`)
	c.Check(systemRestartImmediate, check.Equals, false)
}

func (s *sideloadSuite) TestSideloadComponentInstanceName(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadComponentBody +
		"Content-Disposition: form-data; name=\"name\"\r\n" +
		"\r\n" +
		"local_instance\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	flags := snapstate.Flags{RemoveSnapPath: true, Transaction: client.TransactionPerSnap}
	csi := snap.NewComponentSideInfo(naming.NewComponentRef("local", "comp"), snap.Revision{})

	chgSummary, systemRestartImmediate := s.sideloadComponentCheck(c, body, head, "local_instance", flags, csi)
	c.Check(chgSummary, check.Equals, `Install "comp" component for "local_instance" snap from file "a/b/local+localcomp.comp"`)
	c.Check(systemRestartImmediate, check.Equals, false)
}

func (s *sideloadSuite) TestSideloadComponentNoDangerousFlag(c *check.C) {
	logbuf, r := logger.MockLogger()
	defer r()
	body := "----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap-path\"\r\n" +
		"\r\n" +
		"a/b/local+localcomp.comp\r\n" +
		"----hello--\r\n"
	d := s.daemonWithFakeSnapManager(c)
	s.markSeeded(d)

	defer daemon.MockUnsafeReadSnapInfo(func(path string) (*snap.Info, error) {
		return nil, daemon.BadRequest("mocking error to force reading as component")
	})()

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	rspe := s.errorReq(c, req, nil)
	c.Check(logbuf.String(), testutil.Contains,
		"cannot sideload as a component: only unasserted installation of local component with --dangerous is supported at the moment")
	c.Check(rspe.Message, check.Equals,
		`cannot find signatures with metadata for snap "a/b/local+localcomp.comp"`)
}

func (s *sideloadSuite) TestSideloadComponentForNotInstalledSnap(c *check.C) {
	logbuf, r := logger.MockLogger()
	defer r()
	body := "----hello--\r\n" +
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
		"a/b/local+localcomp.comp\r\n" +
		"----hello--\r\n"
	d := s.daemonWithFakeSnapManager(c)
	s.markSeeded(d)

	defer daemon.MockUnsafeReadSnapInfo(func(path string) (*snap.Info, error) {
		return nil, daemon.BadRequest("mocking error to force reading as component")
	})()
	defer daemon.MockReadComponentInfoFromCont(func(tempPath string, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		return &snap.ComponentInfo{
			Component: naming.NewComponentRef("local", "comp"),
			Type:      snap.TestComponent,
			Version:   "1.0",
		}, nil
	})()

	st := s.d.Overlord().State()
	st.Lock()
	ssi := &snap.SideInfo{RealName: "other", Revision: snap.R(1),
		SnapID: "some-other-snap-id"}
	snapstate.Set(st, "other", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi, nil)}),
		Current: snap.R(1),
	})
	st.Unlock()

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	rspe := s.errorReq(c, req, nil)
	c.Check(logbuf.String(), testutil.Contains,
		`cannot sideload as a snap: cannot read snap file: mocking error to force reading as component`)
	c.Check(logbuf.String(), testutil.Contains,
		`cannot sideload as a component: snap owning "local+comp" not installed`)
	c.Check(rspe.Message, check.Equals, `snap owning "local+comp" not installed`)
	c.Check(rspe.Kind, check.Equals, client.ErrorKindSnapNotInstalled)
}

func (s *sideloadSuite) sideloadComponentCheck(c *check.C, content string,
	head map[string]string, expectedInstanceName string, expectedFlags snapstate.Flags,
	expectedCompSideInfo *snap.ComponentSideInfo) (
	summary string, systemRestartImmediate bool) {

	d := s.daemonWithFakeSnapManager(c)
	s.markSeeded(d)

	st := s.d.Overlord().State()

	st.Lock()
	defer st.Unlock()
	ssi := &snap.SideInfo{RealName: "local", Revision: snap.R(1),
		SnapID: "some-snap-id"}
	snapstate.Set(st, expectedInstanceName, &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi, nil)}),
		Current: snap.R(1),
	})
	st.Unlock()

	soon := 0
	var origEnsureStateSoon func(*state.State)
	origEnsureStateSoon, restore := daemon.MockEnsureStateSoon(func(st *state.State) {
		soon++
		origEnsureStateSoon(st)
	})
	defer restore()

	c.Assert(expectedInstanceName != "", check.Equals, true, check.Commentf("expected instance name must be set"))

	// setup done
	installQueue := []string{}
	defer daemon.MockUnsafeReadSnapInfo(func(path string) (*snap.Info, error) {
		return nil, daemon.BadRequest("mocking error to force reading as component")
	})()

	defer daemon.MockReadComponentInfoFromCont(func(tempPath string, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		return snap.NewComponentInfo(
			expectedCompSideInfo.Component,
			snap.TestComponent,
			"1.0", "", "", "", nil,
		), nil
	})()

	defer daemon.MockSnapstateInstallComponentPath(func(st *state.State, csi *snap.ComponentSideInfo, info *snap.Info,
		path string, flags snapstate.Flags) (*state.TaskSet, error) {
		c.Check(csi, check.DeepEquals, expectedCompSideInfo)
		c.Check(flags, check.DeepEquals, expectedFlags)
		c.Check(path, testutil.FileEquals, "xyzzy")

		installQueue = append(installQueue, csi.Component.String()+"::"+path)
		t := st.NewTask("fake-install-component", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	buf := bytes.NewBufferString(content)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	for k, v := range head {
		req.Header.Set(k, v)
	}

	rsp := s.asyncReq(c, req, nil)
	n := 1
	c.Assert(installQueue, check.HasLen, n)
	c.Check(installQueue[n-1], check.Matches, "local\\+comp::.*/"+regexp.QuoteMeta(dirs.LocalInstallBlobTempPrefix)+".*")

	st.Lock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(soon, check.Equals, 1)
	c.Assert(chg.Tasks(), check.HasLen, n)

	st.Unlock()
	s.waitTrivialChange(c, chg)
	st.Lock()

	c.Check(chg.Kind(), check.Equals, "install-component")
	var names []string
	err = chg.Get("snap-names", &names)

	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{expectedInstanceName})
	var apiData map[string]interface{}
	err = chg.Get("api-data", &apiData)
	c.Assert(err, check.IsNil)
	c.Check(apiData, check.DeepEquals, map[string]interface{}{
		"snap-name":      expectedInstanceName,
		"snap-names":     []interface{}{expectedInstanceName},
		"component-name": expectedCompSideInfo.Component.ComponentName,
	})

	summary = chg.Summary()
	err = chg.Get("system-restart-immediate", &systemRestartImmediate)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		c.Error(err)
	}
	return summary, systemRestartImmediate
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
	s.daemonWithOverlordMockAndStore()

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Message, check.Equals, "cannot use devmode and jailmode flags together")
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
	s.daemonWithOverlordMockAndStore()

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	restore := sandbox.MockForceDevMode(true)
	defer restore()

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Message, check.Equals, "this system cannot honour the jailmode flag")
}

func (s *sideloadSuite) TestLocalInstallSnapDeriveSideInfo(c *check.C) {
	d := s.daemonWithOverlordMockAndStore()
	s.markSeeded(d)
	// add the assertions first
	st := d.Overlord().State()

	fooSnap := snaptest.MakeTestSnapWithFiles(c, `name: foo
version: 1`, nil)
	digest, size, err := asserts.SnapFileSHA3_384(fooSnap)
	c.Assert(err, check.IsNil)
	fooSnapBytes, err := os.ReadFile(fooSnap)
	c.Assert(err, check.IsNil)

	dev1Acct := assertstest.NewAccount(s.StoreSigning, "devel1", nil, "")

	snapDecl, err := s.StoreSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "foo-id",
		"snap-name":    "foo",
		"publisher-id": dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)

	snapRev, err := s.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-id":       "foo-id",
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

	bodyBuf := new(bytes.Buffer)
	bodyBuf.WriteString("----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"foo.snap\"\r\n\r\n")
	bodyBuf.Write(fooSnapBytes)
	bodyBuf.WriteString("\r\n----hello--\r\n")
	req, err := http.NewRequest("POST", "/v2/snaps", bodyBuf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	defer daemon.MockSnapstateInstallPath(func(s *state.State, si *snap.SideInfo, path, name, channel string, flags snapstate.Flags, prqt snapstate.PrereqTracker) (*state.TaskSet, *snap.Info, error) {
		c.Check(flags, check.Equals, snapstate.Flags{RemoveSnapPath: true, Transaction: client.TransactionPerSnap})
		c.Check(si, check.DeepEquals, &snap.SideInfo{
			RealName: "foo",
			SnapID:   "foo-id",
			Revision: snap.R(41),
		})

		return state.NewTaskSet(), &snap.Info{SuggestedName: "foo"}, nil
	})()

	rsp := s.asyncReq(c, req, nil)

	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Summary(), check.Equals, `Install "foo" snap from file "foo.snap"`)
	var names []string
	err = chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{"foo"})
	var apiData map[string]interface{}
	err = chg.Get("api-data", &apiData)
	c.Assert(err, check.IsNil)
	c.Check(apiData, check.DeepEquals, map[string]interface{}{
		"snap-name":  "foo",
		"snap-names": []interface{}{"foo"},
	})
}

func (s *sideloadSuite) TestSideloadSnapNoSignaturesDangerOff(c *check.C) {
	body := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"x\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n"
	d := s.daemonWithOverlordMockAndStore()
	s.markSeeded(d)

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	// this is the prefix used for tempfiles for sideloading
	glob := filepath.Join(os.TempDir(), "snapd-sideload-pkg-*")
	glbBefore, _ := filepath.Glob(glob)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Message, check.Equals, `cannot find signatures with metadata for snap "x"`)
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

	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Message, check.Matches, `cannot find "snap" file field in provided multipart/form-data payload`)
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
	d := s.daemonWithOverlordMockAndStore()
	s.markSeeded(d)

	defer daemon.MockUnsafeReadSnapInfo(func(path string) (*snap.Info, error) {
		return &snap.Info{SuggestedName: "foo"}, nil
	})()

	defer daemon.MockSnapstateInstallPath(func(s *state.State, si *snap.SideInfo, path, name, channel string, flags snapstate.Flags, prqt snapstate.PrereqTracker) (*state.TaskSet, *snap.Info, error) {
		return nil, nil, &snapstate.ChangeConflictError{Snap: "foo"}
	})()

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Kind, check.Equals, client.ErrorKindSnapChangeConflict)
}

func (s *sideloadSuite) TestSideloadSnapInstanceName(c *check.C) {
	// try a multipart/form-data upload
	body := sideLoadBodyWithoutDevMode +
		"Content-Disposition: form-data; name=\"name\"\r\n" +
		"\r\n" +
		"local_instance\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	chgSummary, _ := s.sideloadCheck(c, body, head, "local_instance", snapstate.Flags{RemoveSnapPath: true, Transaction: client.TransactionPerSnap})
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
	chgSummary, _ := s.sideloadCheck(c, body, head, "local", snapstate.Flags{RemoveSnapPath: true, Transaction: client.TransactionPerSnap})
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "a/b/local.snap"`)
}

func (s *sideloadSuite) TestSideloadSnapInstanceNameMismatch(c *check.C) {
	d := s.daemonWithFakeSnapManager(c)
	s.markSeeded(d)

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

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Message, check.Equals, `instance name "foo_instance" does not match snap name "bar"`)
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
	flags := snapstate.Flags{Unaliased: true, RemoveSnapPath: true, DevMode: true, Transaction: client.TransactionPerSnap}
	chgSummary, _ := s.sideloadCheck(c, body, head, "local", flags)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "x"`)
}

func (s *sideloadSuite) TestInstallPathSystemRestartImmediate(c *check.C) {
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
		"Content-Disposition: form-data; name=\"system-restart-immediate\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	// try a multipart/form-data upload
	flags := snapstate.Flags{RemoveSnapPath: true, DevMode: true, Transaction: client.TransactionPerSnap}
	chgSummary, systemRestartImmediate := s.sideloadCheck(c, body, head, "local", flags)
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "x"`)
	c.Check(systemRestartImmediate, check.Equals, true)
}

func (s *sideloadSuite) TestFormdataIsWrittenToCorrectTmpLocation(c *check.C) {
	oldTempDir := os.Getenv("TMPDIR")
	defer func() {
		c.Assert(os.Setenv("TMPDIR", oldTempDir), check.IsNil)
	}()
	tmpDir := c.MkDir()
	c.Assert(os.Setenv("TMPDIR", tmpDir), check.IsNil)

	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	chgSummary, _ := s.sideloadCheck(c, sideLoadBodyWithoutDevMode, head, "local", snapstate.Flags{RemoveSnapPath: true, Transaction: client.TransactionPerSnap})
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "a/b/local.snap"`)

	files, err := os.ReadDir(tmpDir)
	c.Assert(err, check.IsNil)
	c.Assert(files, check.HasLen, 0)

	matches, err := filepath.Glob(filepath.Join(dirs.SnapBlobDir, dirs.LocalInstallBlobTempPrefix+"*"))
	c.Assert(err, check.IsNil)
	c.Assert(matches, check.HasLen, 1)

	c.Assert(err, check.IsNil)
	c.Assert(matches[0], testutil.FileEquals, "xyzzy")
}

func (s *sideloadSuite) TestSideloadExceedMemoryLimit(c *check.C) {
	s.daemonWithOverlordMockAndStore()

	// check that there's a memory limit for the sum of the parts, not just each
	bufs := make([][]byte, 2)
	var body string

	for i := range bufs {
		bufs[i] = make([]byte, daemon.MaxReadBuflen/2+1)
		_, err := rand.Read(bufs[i])
		c.Assert(err, check.IsNil)

		body += "--foo\r\n" +
			"Content-Disposition: form-data; name=\"stuff\"\r\n" +
			"\r\n" +
			string(bufs[i]) +
			"\r\n"
	}

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=foo")

	apiErr := s.errorReq(c, req, nil)
	c.Check(apiErr.Message, check.Equals, `cannot read form data: exceeds memory limit`)
}

func (s *sideloadSuite) TestSideloadUsePreciselyAllMemory(c *check.C) {
	s.daemonWithOverlordMockAndStore()

	buf := make([]byte, daemon.MaxReadBuflen)
	_, err := rand.Read(buf)
	c.Assert(err, check.IsNil)

	body := "----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		string(buf) +
		"\r\n" +
		"----hello--\r\n"

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	// using the maximum memory doesn't cause the failure (not having a snap file does)
	apiErr := s.errorReq(c, req, nil)
	c.Check(apiErr.Message, check.Equals, `cannot find "snap" file field in provided multipart/form-data payload`)
}

func (s *sideloadSuite) TestSideloadCleanUpTempFilesIfRequestFailed(c *check.C) {
	s.daemonWithOverlordMockAndStore()

	// write file parts
	body := "----hello--\r\n"
	for _, name := range []string{"one", "two"} {
		body += fmt.Sprintf(
			"Content-Disposition: form-data; name=\"snap\"; filename=\"%s\"\r\n"+
				"\r\n"+
				"xyzzy\r\n", name)
	}

	// make the request fail
	buf := make([]byte, daemon.MaxReadBuflen+1)
	_, err := rand.Read(buf)
	c.Assert(err, check.IsNil)

	body += "----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		string(buf) +
		"\r\n" +
		"----hello--\r\n"

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")

	apiErr := s.errorReq(c, req, nil)
	c.Check(apiErr, check.NotNil)
	matches, err := filepath.Glob(filepath.Join(dirs.SnapBlobDir, "*"))
	c.Assert(err, check.IsNil)
	c.Check(matches, check.HasLen, 0)
}

func (s *sideloadSuite) TestSideloadCleanUpUnusedTempSnapFiles(c *check.C) {
	body := "----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"snap\"; filename=\"one\"\r\n" +
		"\r\n" +
		"xyzzy\r\n" +
		"----hello--\r\n" +
		// only files with the name 'snap' are used
		"Content-Disposition: form-data; name=\"not-snap\"; filename=\"two\"\r\n" +
		"\r\n" +
		"bla\r\n" +
		"----hello--\r\n"

	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}
	chgSummary, _ := s.sideloadCheck(c, body, head, "local", snapstate.Flags{RemoveSnapPath: true, DevMode: true, Transaction: client.TransactionPerSnap})
	c.Check(chgSummary, check.Equals, `Install "local" snap from file "one"`)

	matches, err := filepath.Glob(filepath.Join(dirs.SnapBlobDir, dirs.LocalInstallBlobTempPrefix+"*"))
	c.Assert(err, check.IsNil)
	// only the file passed into the change (the request's first file) remains
	c.Check(matches, check.HasLen, 1)
}

func (s *sideloadSuite) TestSideloadManySnaps(c *check.C) {
	d := s.daemonWithFakeSnapManager(c)
	s.markSeeded(d)
	expectedFlags := &snapstate.Flags{RemoveSnapPath: true, DevMode: true, Transaction: client.TransactionAllSnaps}

	restore := daemon.MockSnapstateInstallPathMany(func(_ context.Context, s *state.State, infos []*snap.SideInfo, tmpPaths []string, userID int, flags *snapstate.Flags) ([]*state.TaskSet, error) {
		c.Check(flags, check.DeepEquals, expectedFlags)
		c.Check(userID, check.Not(check.Equals), 0)

		var tss []*state.TaskSet
		for i, si := range infos {
			c.Check(tmpPaths[i], testutil.FileEquals, si.RealName)

			ts := state.NewTaskSet(s.NewTask("fake-install-snap", fmt.Sprintf("Doing a fake install of %q", si.RealName)))
			tss = append(tss, ts)
		}

		return tss, nil
	})
	defer restore()

	snaps := []string{"one", "two"}
	var i int
	readRest := daemon.MockUnsafeReadSnapInfo(func(string) (*snap.Info, error) {
		info := &snap.Info{SuggestedName: snaps[i]}
		i++
		return info, nil
	})
	defer readRest()

	body := "----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	body += "Content-Disposition: form-data; name=\"transaction\"\r\n" +
		"\r\n" +
		"all-snaps\r\n" +
		"----hello--\r\n"
	prefixed := make([]string, len(snaps))
	for i, snap := range snaps {
		prefixed[i] = "file-" + snap
		body += "Content-Disposition: form-data; name=\"snap\"; filename=\"" + prefixed[i] + "\"\r\n" +
			"\r\n" +
			snap + "\r\n" +
			"----hello--\r\n"
	}

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")
	s.asUserAuth(c, req)
	rsp := s.asyncReq(c, req, s.authUser)

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()

	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Summary(), check.Equals, fmt.Sprintf(`Install snaps %s from files %s`, strutil.Quoted(snaps), strutil.Quoted(prefixed)))

	var data map[string][]string
	c.Assert(chg.Get("api-data", &data), check.IsNil)
	c.Check(data["snap-names"], check.DeepEquals, snaps)
}

func (s *sideloadSuite) TestSideloadManyFailInstallPathMany(c *check.C) {
	s.daemon(c)
	restore := daemon.MockSnapstateInstallPathMany(func(_ context.Context, s *state.State, infos []*snap.SideInfo, paths []string, userID int, flags *snapstate.Flags) ([]*state.TaskSet, error) {
		return nil, errors.New("expected")
	})
	defer restore()

	readRest := daemon.MockUnsafeReadSnapInfo(func(string) (*snap.Info, error) {
		return &snap.Info{SuggestedName: "name"}, nil
	})
	defer readRest()

	body := "----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	for _, snap := range []string{"one", "two"} {
		body += "Content-Disposition: form-data; name=\"snap\"; filename=\"file-" + snap + "\"\r\n" +
			"\r\n" +
			"xyzzy \r\n" +
			"----hello--\r\n"
	}

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")
	apiErr := s.errorReq(c, req, nil)

	c.Check(apiErr.JSON().Status, check.Equals, 500)
	c.Check(apiErr.Message, check.Equals, `cannot install snap files: expected`)
}

func (s *sideloadSuite) TestSideloadManyFailUnsafeReadInfo(c *check.C) {
	s.daemon(c)
	restore := daemon.MockUnsafeReadSnapInfo(func(string) (*snap.Info, error) {
		return nil, errors.New("expected")
	})
	defer restore()

	body := "----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"
	for _, snap := range []string{"one", "two"} {
		body += "Content-Disposition: form-data; name=\"snap\"; filename=\"file-" + snap + "\"\r\n" +
			"\r\n" +
			"xyzzy \r\n" +
			"----hello--\r\n"
	}

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")
	apiErr := s.errorReq(c, req, nil)

	c.Check(apiErr.JSON().Status, check.Equals, 400)
	c.Check(apiErr.Message, check.Equals, `cannot read snap file: expected`)
}

func (s *sideloadSuite) TestSideloadManySnapsDevmode(c *check.C) {
	body := "----hello--\r\n" +
		"Content-Disposition: form-data; name=\"devmode\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"

	s.errReadInfo(c, body)
}

func (s *sideloadSuite) TestSideloadManySnapsDangerous(c *check.C) {
	body := "----hello--\r\n" +
		"Content-Disposition: form-data; name=\"dangerous\"\r\n" +
		"\r\n" +
		"true\r\n" +
		"----hello--\r\n"

	s.errReadInfo(c, body)
}

func (s *sideloadSuite) errReadInfo(c *check.C, body string) {
	s.daemon(c)

	for _, snap := range []string{"one", "two"} {
		body += "Content-Disposition: form-data; name=\"snap\"; filename=\"" + snap + "\"\r\n" +
			"\r\n" +
			snap + "\r\n" +
			"----hello--\r\n"
	}

	req, err := http.NewRequest("POST", "/v2/snaps", bytes.NewBufferString(body))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")
	rsp := s.errorReq(c, req, nil)

	c.Assert(rsp.Status, check.Equals, 400)
	// gets as far as reading the file to get the SideInfo
	c.Assert(rsp.Message, check.Matches, "cannot read snap file:.*")
}

func (s *sideloadSuite) TestSideloadManySnapsAsserted(c *check.C) {
	d := s.daemonWithOverlordMockAndStore()
	s.markSeeded(d)
	st := d.Overlord().State()
	snaps := []string{"one", "two"}
	snapData := s.mockAssertions(c, st, snaps)

	expectedFlags := snapstate.Flags{RemoveSnapPath: true, Transaction: client.TransactionPerSnap}

	restore := daemon.MockSnapstateInstallPathMany(func(_ context.Context, s *state.State, infos []*snap.SideInfo, paths []string, userID int, flags *snapstate.Flags) ([]*state.TaskSet, error) {
		c.Check(*flags, check.DeepEquals, expectedFlags)

		var tss []*state.TaskSet
		for i, si := range infos {
			c.Check(*si, check.DeepEquals, snap.SideInfo{
				RealName: snaps[i],
				SnapID:   snaps[i] + "-id",
				Revision: snap.R(41),
			})

			ts := state.NewTaskSet(s.NewTask("fake-install-snap", fmt.Sprintf("Doing a fake install of %q", si.RealName)))
			tss = append(tss, ts)
		}

		return tss, nil
	})
	defer restore()

	bodyBuf := bytes.NewBufferString("----hello--\r\n")
	fileSnaps := make([]string, len(snaps))
	for i, snap := range snaps {
		fileSnaps[i] = "file-" + snap
		bodyBuf.WriteString("Content-Disposition: form-data; name=\"snap\"; filename=\"" + fileSnaps[i] + "\"\r\n\r\n")
		bodyBuf.Write(snapData[i])
		bodyBuf.WriteString("\r\n----hello--\r\n")
	}

	req, err := http.NewRequest("POST", "/v2/snaps", bodyBuf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")
	rsp := s.asyncReq(c, req, nil)

	c.Check(rsp.Status, check.Equals, 202)
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Summary(), check.Equals, fmt.Sprintf(`Install snaps %s from files %s`, strutil.Quoted(snaps), strutil.Quoted(fileSnaps)))

}

func (s *sideloadSuite) TestSideloadManySnapsOneNotAsserted(c *check.C) {
	d := s.daemonWithOverlordMockAndStore()
	s.markSeeded(d)
	st := d.Overlord().State()
	snaps := []string{"one", "two"}
	snapData := s.mockAssertions(c, st, []string{"one"})
	// unasserted snap
	twoSnap := snaptest.MakeTestSnapWithFiles(c, `name: two
version: 1`, nil)
	twoSnapData, err := os.ReadFile(twoSnap)
	c.Assert(err, check.IsNil)
	snapData = append(snapData, twoSnapData)

	bodyBuf := bytes.NewBufferString("----hello--\r\n")
	fileSnaps := make([]string, len(snaps))
	for i, snap := range snaps {
		fileSnaps[i] = "file-" + snap
		bodyBuf.WriteString("Content-Disposition: form-data; name=\"snap\"; filename=\"" + fileSnaps[i] + "\"\r\n\r\n")
		bodyBuf.Write(snapData[i])
		bodyBuf.WriteString("\r\n----hello--\r\n")
	}

	req, err := http.NewRequest("POST", "/v2/snaps", bodyBuf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "multipart/thing; boundary=--hello--")
	rsp := s.errorReq(c, req, nil)

	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Message, check.Matches, "cannot find signatures with metadata for snap \"file-two\"")
}

func (s *sideloadSuite) mockAssertions(c *check.C, st *state.State, snaps []string) (snapData [][]byte) {
	for _, snap := range snaps {
		thisSnap := snaptest.MakeTestSnapWithFiles(c, fmt.Sprintf(`name: %s
version: 1`, snap), nil)
		digest, size, err := asserts.SnapFileSHA3_384(thisSnap)
		c.Assert(err, check.IsNil)
		thisSnapData, err := os.ReadFile(thisSnap)
		c.Assert(err, check.IsNil)
		snapData = append(snapData, thisSnapData)

		dev1Acct := assertstest.NewAccount(s.StoreSigning, "devel1", nil, "")
		snapDecl, err := s.StoreSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
			"series":       "16",
			"snap-id":      snap + "-id",
			"snap-name":    snap,
			"publisher-id": dev1Acct.AccountID(),
			"timestamp":    time.Now().Format(time.RFC3339),
		}, nil, "")
		c.Assert(err, check.IsNil)
		snapRev, err := s.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
			"snap-sha3-384": digest,
			"snap-size":     fmt.Sprintf("%d", size),
			"snap-id":       snap + "-id",
			"snap-revision": "41",
			"developer-id":  dev1Acct.AccountID(),
			"timestamp":     time.Now().Format(time.RFC3339),
		}, nil, "")
		c.Assert(err, check.IsNil)

		st.Lock()
		assertstatetest.AddMany(st, s.StoreSigning.StoreAccountKey(""), dev1Acct, snapDecl, snapRev)
		st.Unlock()
	}

	return snapData
}

type trySuite struct {
	apiBaseSuite
}

func (s *trySuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})
}

func (s *trySuite) TestTrySnap(c *check.C) {
	d := s.daemonWithFakeSnapManager(c)

	var err error

	// mock a try dir
	tryDir := c.MkDir()
	snapYaml := filepath.Join(tryDir, "meta", "snap.yaml")
	err = os.MkdirAll(filepath.Dir(snapYaml), 0755)
	c.Assert(err, check.IsNil)
	err = os.WriteFile(snapYaml, []byte("name: foo\nversion: 1.0\n"), 0644)
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
		rsp := s.asyncReq(c, reqForFlags(t.flags), nil)
		st.Lock()
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
			"snap-name":  "foo",
			"snap-names": []interface{}{"foo"},
		}, check.Commentf(t.desc))

		c.Check(soon, check.Equals, 1, check.Commentf(t.desc))
	}
}

func (s *trySuite) TestTrySnapRelative(c *check.C) {
	d := s.daemon(c)
	st := d.Overlord().State()

	rspe := daemon.TrySnap(st, "relative-path", snapstate.Flags{}).(*daemon.APIError)
	c.Check(rspe.Message, testutil.Contains, "need an absolute path")
}

func (s *trySuite) TestTrySnapNotDir(c *check.C) {
	d := s.daemon(c)
	st := d.Overlord().State()

	rspe := daemon.TrySnap(st, "/does/not/exist", snapstate.Flags{}).(*daemon.APIError)
	c.Check(rspe.Message, testutil.Contains, "not a snap directory")
}

func (s *trySuite) TestTryChangeConflict(c *check.C) {
	d := s.daemonWithOverlordMockAndStore()
	st := d.Overlord().State()

	// mock a try dir
	tryDir := c.MkDir()

	defer daemon.MockUnsafeReadSnapInfo(func(path string) (*snap.Info, error) {
		return &snap.Info{SuggestedName: "foo"}, nil
	})()

	defer daemon.MockSnapstateTryPath(func(s *state.State, name, path string, flags snapstate.Flags) (*state.TaskSet, error) {
		return nil, &snapstate.ChangeConflictError{Snap: "foo"}
	})()

	rspe := daemon.TrySnap(st, tryDir, snapstate.Flags{}).(*daemon.APIError)
	c.Check(rspe.Kind, check.Equals, client.ErrorKindSnapChangeConflict)
}

func (s *sideloadSuite) TestSideloadSnapInvalidTransaction(c *check.C) {
	s.daemon(c)

	content := "" +
		"----hello--\r\n" +
		"Content-Disposition: form-data; name=\"transaction\"\r\n" +
		"\r\n" +
		"xyz\r\n" +
		"----hello--\r\n"
	head := map[string]string{"Content-Type": "multipart/thing; boundary=--hello--"}

	buf := bytes.NewBufferString(content)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	for k, v := range head {
		req.Header.Set(k, v)
	}

	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Message, check.Matches, `transaction must be either "per-snap" or "all-snaps"`)
}
