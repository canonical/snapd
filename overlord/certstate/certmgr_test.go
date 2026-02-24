// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2026 Canonical Ltd
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

package certstate_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/certstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

type certMgrTestSuite struct {
	testutil.BaseTest

	o     *overlord.Overlord
	state *state.State
	se    *overlord.StateEngine
	mgr   *certstate.CertManager
}

func (s *certMgrTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())

	s.o = overlord.Mock()
	s.state = s.o.State()
	s.se = s.o.StateEngine()
	s.mgr = certstate.Manager(s.state, s.o.TaskRunner())

	// For triggering errors
	erroringHandler := func(_ *state.Task, _ *tomb.Tomb) error {
		return errors.New("error out")
	}
	s.o.TaskRunner().AddHandler("error-trigger", erroringHandler, nil)

	s.o.AddManager(s.mgr)
	s.o.AddManager(s.o.TaskRunner())
}

func (s *certMgrTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

var _ = Suite(&certMgrTestSuite{})

func (s *certMgrTestSuite) settle(c *C) {
	s.state.Unlock()
	err := s.o.Settle(30 * time.Second)
	c.Assert(err, IsNil)
	s.state.Lock()
}

func (s *certMgrTestSuite) TestEnsureCallsUpdateCertificateDatabase(c *C) {
	var called bool
	restore := certstate.MockGenerateCertificateDatabase(func() error {
		called = true
		return nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// Mark the system as seeded to trigger certificate generation.
	s.state.Set("seeded", true)

	c.Assert(os.MkdirAll(dirs.SystemCertsDir, 0o755), IsNil)

	s.state.Unlock()
	defer s.state.Lock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)
	c.Check(called, Equals, true)
}

func (s *certMgrTestSuite) TestEnsureDoesNothingWhenNotSeeded(c *C) {
	var called bool
	restore := certstate.MockGenerateCertificateDatabase(func() error {
		called = true
		return nil
	})
	defer restore()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)
	c.Check(called, Equals, false)
}

func (s *certMgrTestSuite) TestEnsureSkipsWhenCertDbExists(c *C) {
	var called bool
	restore := certstate.MockGenerateCertificateDatabase(func() error {
		called = true
		return nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	c.Assert(os.MkdirAll(mergedDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(mergedDir, "ca-certificates.crt"), []byte("existing"), 0o644), IsNil)

	s.state.Unlock()
	defer s.state.Lock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)
	c.Check(called, Equals, false)
}

func (s *certMgrTestSuite) TestEnsureSkipsWhenNoBaseCertsDir(c *C) {
	var called bool
	restore := certstate.MockGenerateCertificateDatabase(func() error {
		called = true
		return nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)

	s.state.Unlock()
	defer s.state.Lock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)
	c.Check(called, Equals, false)
}

func (s *certMgrTestSuite) TestEnsureRunsOnlyOnce(c *C) {
	var calls int
	restore := certstate.MockGenerateCertificateDatabase(func() error {
		calls++
		return nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)

	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)

	s.state.Unlock()
	defer s.state.Lock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	c.Check(calls, Equals, 1)
}

func (s *certMgrTestSuite) TestEnsurePropagatesGenerateError(c *C) {
	restore := certstate.MockGenerateCertificateDatabase(func() error {
		return errors.New("boom")
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)

	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)

	s.state.Unlock()
	defer s.state.Lock()

	err := s.mgr.Ensure()
	c.Check(err, ErrorMatches, ".*boom.*")
}

func (s *certMgrTestSuite) TestDoUpdateCertificateDatabaseGeneratesMerged(c *C) {
	certA, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	certB, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)

	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseCertsDir, "a.crt"), certA, 0o644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseCertsDir, "b.crt"), certB, 0o644), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("foo", "test change")
	chg.AddTask(s.state.NewTask("update-cert-db", "running handler"))
	s.settle(c)

	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	mergedPath := filepath.Join(dirs.SnapdPKIV1Dir, "merged", "ca-certificates.crt")
	out, err := os.ReadFile(mergedPath)
	c.Assert(err, IsNil)
	c.Check(bytes.Contains(out, certA), Equals, true)
	c.Check(bytes.Contains(out, certB), Equals, true)
}

func (s *certMgrTestSuite) TestUndoUpdateCertificateDatabaseRestoresBackup(c *C) {
	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	c.Assert(os.MkdirAll(mergedDir, 0o755), IsNil)

	current := []byte("current-ca-bundle")
	c.Assert(os.WriteFile(filepath.Join(mergedDir, "ca-certificates.crt"), current, 0o644), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("foo", "test change")
	chg.AddTask(s.state.NewTask("update-cert-db", "running handler"))
	chg.AddTask(s.state.NewTask("error-trigger", "triggering error for rollback"))
	s.settle(c)

	c.Check(chg.Err(), NotNil)
	c.Check(chg.Status(), Equals, state.ErrorStatus)

	// verify the status of the tasks
	tasks := chg.Tasks()
	c.Check(len(tasks), Equals, 2)
	c.Check(tasks[0].Kind(), Equals, "update-cert-db")
	c.Check(tasks[0].Status(), Equals, state.UndoneStatus)
	c.Check(tasks[1].Kind(), Equals, "error-trigger")
	c.Check(tasks[1].Status(), Equals, state.ErrorStatus)

	out, err := os.ReadFile(filepath.Join(mergedDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(out, DeepEquals, current)

	_, err = os.Stat(filepath.Join(mergedDir, "ca-certificates.crt.old"))
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *certMgrTestSuite) TestUndoUpdateCertificateDatabaseMissingBackupNoError(c *C) {
	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	c.Assert(os.MkdirAll(mergedDir, 0o755), IsNil)

	current := []byte("current-ca-bundle")
	c.Assert(os.WriteFile(filepath.Join(mergedDir, "ca-certificates.crt"), current, 0o644), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("foo", "test change")
	chg.AddTask(s.state.NewTask("update-cert-db", "running handler"))
	chg.AddTask(s.state.NewTask("error-trigger", "triggering error for rollback"))
	s.settle(c)

	c.Check(chg.Err(), NotNil)
	c.Check(chg.Status(), Equals, state.ErrorStatus)

	// verify the status of the tasks
	tasks := chg.Tasks()
	c.Check(len(tasks), Equals, 2)
	c.Check(tasks[0].Kind(), Equals, "update-cert-db")
	c.Check(tasks[0].Status(), Equals, state.UndoneStatus)
	c.Check(tasks[1].Kind(), Equals, "error-trigger")
	c.Check(tasks[1].Status(), Equals, state.ErrorStatus)

	out, err := os.ReadFile(filepath.Join(mergedDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(out, DeepEquals, current)
}
