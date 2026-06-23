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

type checkpointBackend struct {
	checkpoints [][]byte
}

func (b *checkpointBackend) Checkpoint(data []byte) error {
	b.checkpoints = append(b.checkpoints, append([]byte(nil), data...))
	return nil
}

func (b *checkpointBackend) EnsureBefore(time.Duration) {}

func (s *certMgrTestSuite) settle(c *C) {
	s.state.Unlock()
	err := s.o.Settle(30 * time.Second)
	c.Assert(err, IsNil)
	s.state.Lock()
}

func seedCurrentPublishedGeneration(c *C, generation string, bundle []byte) string {
	target := filepath.Join("published", generation)
	generationDir := filepath.Join(dirs.SnapdPKIV1Dir, target)
	c.Assert(os.MkdirAll(generationDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(generationDir, "ca-certificates.crt"), bundle, 0o644), IsNil)
	c.Assert(os.Symlink(target, filepath.Join(dirs.SnapdPKIV1Dir, "merged")), IsNil)
	return target
}

func setMergedTarget(c *C, target string) {
	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	c.Assert(os.RemoveAll(mergedDir), IsNil)
	c.Assert(os.Symlink(target, mergedDir), IsNil)
}

func (s *certMgrTestSuite) TestEnsureCallsUpdateCertificateDatabase(c *C) {
	var called bool
	restore := certstate.MockRefreshCertificateDatabase(func() error {
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
	restore := certstate.MockRefreshCertificateDatabase(func() error {
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
	restore := certstate.MockRefreshCertificateDatabase(func() error {
		called = true
		return nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)
	c.Assert(os.MkdirAll(dirs.SystemCertsDir, 0o755), IsNil)

	seedCurrentPublishedGeneration(c, "existing", []byte("existing"))

	s.state.Unlock()
	defer s.state.Lock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)
	c.Check(called, Equals, false)
}

func (s *certMgrTestSuite) TestEnsureSkipsWhenNoBaseCertsDir(c *C) {
	var called bool
	restore := certstate.MockRefreshCertificateDatabase(func() error {
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
	restore := certstate.MockRefreshCertificateDatabase(func() error {
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
	restore := certstate.MockRefreshCertificateDatabase(func() error {
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

func (s *certMgrTestSuite) TestEnsureGarbageCollectionSkipsWhileUpdateInProgress(c *C) {
	restoreBootID := certstate.MockOsutilBootID(func() (string, error) {
		return "", errors.New("boot id should not be queried while update-cert-db is in flight")
	})
	defer restoreBootID()

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)

	currentTarget := seedCurrentPublishedGeneration(c, "current", []byte("current"))
	currentDir := filepath.Join(dirs.SnapdPKIV1Dir, currentTarget)
	taskPreviousTarget := filepath.Join("published", "task-prev")
	taskPreviousDir := filepath.Join(dirs.SnapdPKIV1Dir, taskPreviousTarget)
	c.Assert(os.MkdirAll(taskPreviousDir, 0o755), IsNil)

	staleTarget := filepath.Join("published", "stale")
	staleDir := filepath.Join(dirs.SnapdPKIV1Dir, staleTarget)
	c.Assert(os.MkdirAll(staleDir, 0o755), IsNil)

	for _, dir := range []string{currentDir, taskPreviousDir, staleDir} {
		c.Assert(os.WriteFile(filepath.Join(dir, ".snapd-inactive"), []byte("boot-1"), 0o644), IsNil)
	}

	s.state.Lock()
	chg := s.state.NewChange("foo", "pending update keeps gc from running")
	task := s.state.NewTask("update-cert-db", "pending update")
	task.Set(certstate.PreviousGenerationTaskKey, taskPreviousTarget)
	chg.AddTask(task)
	s.state.Unlock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	for _, path := range []string{
		currentDir,
		taskPreviousDir,
		staleDir,
	} {
		_, err = os.Stat(path)
		c.Assert(err, IsNil)
		c.Assert(filepath.Join(path, ".snapd-inactive"), testutil.FileEquals, "boot-1")
	}
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

	backupPath := filepath.Join(dirs.SnapdPKIV1Dir, "merged.staged")
	_, err = os.Stat(backupPath)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *certMgrTestSuite) TestDoUpdateCertificateDatabaseRetryAfterSwapPreservesUndoBackup(c *C) {
	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)

	oldBundle := []byte("old-ca-bundle")
	newPEM, _, err := makeTestCertPEM("new")
	c.Assert(err, IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseCertsDir, "new.crt"), newPEM, 0o644), IsNil)
	seedCurrentPublishedGeneration(c, "old", oldBundle)

	s.state.Lock()
	task := s.state.NewTask("update-cert-db", "retrying update after swap")
	s.state.Unlock()

	err = s.mgr.DoUpdateCertificateDatabase(task, nil)
	c.Assert(err, IsNil)

	firstRefreshBundle, err := os.ReadFile(filepath.Join(mergedDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(bytes.Contains(firstRefreshBundle, newPEM), Equals, true)

	err = s.mgr.DoUpdateCertificateDatabase(task, nil)
	c.Assert(err, IsNil)

	out, err := os.ReadFile(filepath.Join(mergedDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(out, DeepEquals, firstRefreshBundle)

	err = s.mgr.UndoUpdateCertificateDatabase(task, nil)
	c.Assert(err, IsNil)

	out, err = os.ReadFile(filepath.Join(mergedDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(out, DeepEquals, oldBundle)
}

func (s *certMgrTestSuite) TestDoUpdateCertificateDatabaseCheckpointsPreviousGenerationBeforeRefresh(c *C) {
	backend := &checkpointBackend{}
	st := state.New(backend)
	runner := state.NewTaskRunner(st)
	mgr := certstate.Manager(st, runner)

	oldTarget := seedCurrentPublishedGeneration(c, "old", []byte("old-ca-bundle"))

	st.Lock()
	chg := st.NewChange("foo", "checkpoint previous generation")
	task := st.NewTask("update-cert-db", "running handler")
	chg.AddTask(task)
	st.Unlock()
	backend.checkpoints = nil

	restore := certstate.MockRefreshCertificateDatabase(func() error {
		c.Assert(backend.checkpoints, HasLen, 1)
		restoredState, err := state.ReadState(nil, bytes.NewReader(backend.checkpoints[0]))
		c.Assert(err, IsNil)
		restoredState.Lock()
		defer restoredState.Unlock()

		restoredTask := restoredState.Task(task.ID())
		c.Assert(restoredTask, NotNil)

		var previousTarget string
		err = restoredTask.Get(certstate.PreviousGenerationTaskKey, &previousTarget)
		c.Assert(err, IsNil)
		c.Check(previousTarget, Equals, oldTarget)
		return nil
	})
	defer restore()

	err := mgr.DoUpdateCertificateDatabase(task, nil)
	c.Assert(err, IsNil)
}

func (s *certMgrTestSuite) TestDoUpdateCertificateDatabasePropagatesCheckpointReadError(c *C) {
	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)

	var called bool
	restore := certstate.MockRefreshCertificateDatabase(func() error {
		called = true
		return nil
	})
	defer restore()

	s.state.Lock()
	task := s.state.NewTask("update-cert-db", "checkpoint read error")
	task.Set(certstate.PreviousGenerationTaskKey, 42)
	s.state.Unlock()

	err := s.mgr.DoUpdateCertificateDatabase(task, nil)
	c.Assert(err, NotNil)
	c.Check(called, Equals, false)
}

func (s *certMgrTestSuite) TestUndoUpdateCertificateDatabaseRestoresBackup(c *C) {
	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")

	current := []byte("current-ca-bundle")
	seedCurrentPublishedGeneration(c, "current", current)

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

	mergedInfo, err := os.Lstat(mergedDir)
	c.Assert(err, IsNil)
	c.Check(mergedInfo.Mode()&os.ModeSymlink != 0, Equals, true)

	_, err = os.Stat(mergedDir + ".staged")
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *certMgrTestSuite) TestUndoUpdateCertificateDatabaseWithoutPriorMergedRemovesMerged(c *C) {
	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)
	certA, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseCertsDir, "a.crt"), certA, 0o644), IsNil)

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	_, err = os.Stat(mergedDir)
	c.Assert(os.IsNotExist(err), Equals, true)

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

	_, err = os.Stat(mergedDir)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *certMgrTestSuite) TestUndoUpdateCertificateDatabaseIsIdempotent(c *C) {
	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)

	oldBundle := []byte("restored-ca-bundle")
	newPEM, _, err := makeTestCertPEM("new")
	c.Assert(err, IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseCertsDir, "new.crt"), newPEM, 0o644), IsNil)
	seedCurrentPublishedGeneration(c, "restored", oldBundle)

	s.state.Lock()
	task := s.state.NewTask("update-cert-db", "retrying undo after swap")
	s.state.Unlock()

	err = s.mgr.DoUpdateCertificateDatabase(task, nil)
	c.Assert(err, IsNil)

	err = s.mgr.UndoUpdateCertificateDatabase(task, nil)
	c.Assert(err, IsNil)

	out, err := os.ReadFile(filepath.Join(mergedDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(out, DeepEquals, oldBundle)

	err = s.mgr.UndoUpdateCertificateDatabase(task, nil)
	c.Assert(err, IsNil)

	out, err = os.ReadFile(filepath.Join(mergedDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(out, DeepEquals, oldBundle)

	_, err = os.Stat(filepath.Join(dirs.SnapdPKIV1Dir, "merged.staged"))
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *certMgrTestSuite) TestUndoUpdateCertificateDatabaseRetryAfterRebootKeepsRestoredMerged(c *C) {
	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)

	oldBundle := []byte("old-ca-bundle")
	newPEM, _, err := makeTestCertPEM("new")
	c.Assert(err, IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseCertsDir, "new.crt"), newPEM, 0o644), IsNil)
	oldTarget := seedCurrentPublishedGeneration(c, "old", oldBundle)

	err = certstate.RefreshCertificateDatabase()
	c.Assert(err, IsNil)

	s.state.Lock()
	task := s.state.NewTask("update-cert-db", "retrying undo after reboot")
	task.Set(certstate.PreviousGenerationTaskKey, oldTarget)
	s.state.Unlock()

	setMergedTarget(c, oldTarget)

	err = s.mgr.UndoUpdateCertificateDatabase(task, nil)
	c.Assert(err, IsNil)

	currentTarget, err := os.Readlink(filepath.Join(dirs.SnapdPKIV1Dir, "merged"))
	c.Assert(err, IsNil)
	c.Check(currentTarget, Equals, oldTarget)
}

func (s *certMgrTestSuite) TestUndoUpdateCertificateDatabaseWithoutPriorMergedRetryAfterRebootRemovesMerged(c *C) {
	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)

	newPEM, _, err := makeTestCertPEM("new")
	c.Assert(err, IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseCertsDir, "new.crt"), newPEM, 0o644), IsNil)

	err = certstate.RefreshCertificateDatabase()
	c.Assert(err, IsNil)

	s.state.Lock()
	task := s.state.NewTask("update-cert-db", "retrying undo without previous generation")
	task.Set(certstate.PreviousGenerationTaskKey, "")
	s.state.Unlock()

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	c.Assert(os.RemoveAll(mergedDir), IsNil)
	c.Assert(os.MkdirAll(mergedDir, 0o755), IsNil)

	err = s.mgr.UndoUpdateCertificateDatabase(task, nil)
	c.Assert(err, IsNil)

	_, err = os.Stat(mergedDir)
	c.Check(os.IsNotExist(err), Equals, true)
}
