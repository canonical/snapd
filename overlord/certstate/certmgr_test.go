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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/certstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/swfeats/swfeatstest"
	"github.com/snapcore/snapd/release"
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

	restore := release.MockOnClassic(false)
	s.AddCleanup(restore)

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
	s.state.Lock()
	c.Assert(err, IsNil)
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

func (s *certMgrTestSuite) TestEnsureSkipsOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	var called bool
	restore = certstate.MockRefreshCertificateDatabase(func() error {
		called = true
		return nil
	})
	defer restore()

	restoreBootID := certstate.MockOsutilBootID(func() (string, error) {
		return "", errors.New("boot id should not be queried on classic")
	})
	defer restoreBootID()

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)
	c.Assert(os.MkdirAll(dirs.SystemCertsDir, 0o755), IsNil)

	s.state.Unlock()
	defer s.state.Lock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)
	c.Check(called, Equals, false)
}

func (s *certMgrTestSuite) TestEnsureReconcilesWhenCertDbExists(c *C) {
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
	// Even when merged already points at a generation, Ensure reconciles the
	// store with the current inputs, because a base refresh planned by a
	// pre-certstate snapd schedules no update-cert-db task and would otherwise
	// leave the store rendered from the old base (SNAPDENG-38146).
	c.Check(called, Equals, true)
}

// TestEnsureReconcilesStaleGeneration exercises the real RefreshCertificateDatabase
// (not a mock) to prove that a generation rendered from an old base is
// re-rendered from the current base when Ensure runs.
func (s *certMgrTestSuite) TestEnsureReconcilesStaleGeneration(c *C) {
	certA, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	certB, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)

	// The base initially provides only certificate A.
	c.Assert(os.MkdirAll(dirs.SystemCertsDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SystemCertsDir, "a.crt"), certA, 0o644), IsNil)

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	// First Ensure renders the generation from A.
	c.Assert(s.mgr.Ensure(), IsNil)
	mergedBundle := filepath.Join(dirs.SnapdPKIV1Dir, "merged", "ca-certificates.crt")
	out, err := os.ReadFile(mergedBundle)
	c.Assert(err, IsNil)
	c.Check(bytes.Contains(out, certA), Equals, true)
	c.Check(bytes.Contains(out, certB), Equals, false)

	// The base is refreshed and now also provides certificate B, but no
	// update-cert-db task runs (as happens when the refresh was planned by a
	// pre-certstate snapd).
	c.Assert(os.WriteFile(filepath.Join(dirs.SystemCertsDir, "b.crt"), certB, 0o644), IsNil)

	// A fresh CertManager (as after a snapd restart) reconciles the store.
	s.mgr = certstate.Manager(s.state, s.o.TaskRunner())
	c.Assert(s.mgr.Ensure(), IsNil)

	out, err = os.ReadFile(mergedBundle)
	c.Assert(err, IsNil)
	c.Check(bytes.Contains(out, certA), Equals, true)
	c.Check(bytes.Contains(out, certB), Equals, true)
}

// TestEnsureSkipsReconcileWhileUpdateInProgress ensures Ensure does not render
// a new generation while an update-cert-db change is in flight, so the task's
// recorded previous generation and its undo stay coherent.
func (s *certMgrTestSuite) TestEnsureSkipsReconcileWhileUpdateInProgress(c *C) {
	var called bool
	restore := certstate.MockRefreshCertificateDatabase(func() error {
		called = true
		return nil
	})
	defer restore()

	// GC still runs on this path, so provide a boot ID.
	restoreBootID := certstate.MockOsutilBootID(func() (string, error) {
		return "boot-1", nil
	})
	defer restoreBootID()

	s.state.Lock()
	s.state.Set("seeded", true)
	c.Assert(os.MkdirAll(dirs.SystemCertsDir, 0o755), IsNil)
	seedCurrentPublishedGeneration(c, "existing", []byte("existing"))

	// An update-cert-db change is in flight.
	chg := s.state.NewChange("foo", "pending update")
	chg.AddTask(s.state.NewTask("update-cert-db", "pending update"))
	s.state.Unlock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)
	c.Check(called, Equals, false)
}

func (s *certMgrTestSuite) TestEnsureRegeneratesWhenMergedIsPlainDirectory(c *C) {
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

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	c.Assert(os.MkdirAll(mergedDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(mergedDir, "leftover"), []byte("stale"), 0o644), IsNil)

	s.state.Unlock()
	defer s.state.Lock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)
	c.Check(called, Equals, true)

	_, err = os.Stat(mergedDir)
	c.Check(os.IsNotExist(err), Equals, true)
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

// TestEnsureRetriesReconcileAfterUpdateFinishes verifies that when an
// update-cert-db change is in flight during one Ensure, a later Ensure (after
// the change finishes) still reconciles the store, because the in-flight guard
// runs before the one-time-checks flag is set (SNAPDENG-38146 review feedback).
func (s *certMgrTestSuite) TestEnsureRetriesReconcileAfterUpdateFinishes(c *C) {
	var calls int
	restore := certstate.MockRefreshCertificateDatabase(func() error {
		calls++
		return nil
	})
	defer restore()

	s.state.Lock()
	s.state.Set("seeded", true)
	c.Assert(os.MkdirAll(dirs.SystemCertsDir, 0o755), IsNil)
	// An existing (possibly stale) generation is present.
	seedCurrentPublishedGeneration(c, "existing", []byte("existing"))

	// An update-cert-db change is in flight; the first Ensure must skip
	// reconciling without consuming the one-time run.
	chg := s.state.NewChange("foo", "pending update")
	task := s.state.NewTask("update-cert-db", "pending update")
	chg.AddTask(task)
	s.state.Unlock()

	c.Assert(s.mgr.Ensure(), IsNil)
	c.Check(calls, Equals, 0)

	// The change finishes; the next Ensure must reconcile now.
	s.state.Lock()
	chg.SetStatus(state.DoneStatus)
	task.SetStatus(state.DoneStatus)
	s.state.Unlock()

	c.Assert(s.mgr.Ensure(), IsNil)
	c.Check(calls, Equals, 1)
}

// TestEnsureGCErrorDoesNotBlockReconcile verifies that a garbage-collection
// failure (e.g. a failed boot-ID read or a full disk) is logged but does not
// prevent the store from being reconciled — reconciliation is the self-heal
// this path exists to provide (SNAPDENG-38146 review feedback).
func (s *certMgrTestSuite) TestEnsureGCErrorDoesNotBlockReconcile(c *C) {
	var rendered bool
	restore := certstate.MockRefreshCertificateDatabase(func() error {
		rendered = true
		return nil
	})
	defer restore()

	// Force garbage collection to fail.
	restoreBootID := certstate.MockOsutilBootID(func() (string, error) {
		return "", errors.New("cannot read boot id")
	})
	defer restoreBootID()

	s.state.Lock()
	s.state.Set("seeded", true)
	c.Assert(os.MkdirAll(dirs.SystemCertsDir, 0o755), IsNil)
	seedCurrentPublishedGeneration(c, "existing", []byte("existing"))
	s.state.Unlock()

	// Ensure succeeds and the store is reconciled despite the GC failure.
	c.Assert(s.mgr.Ensure(), IsNil)
	c.Check(rendered, Equals, true)
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

func (s *certMgrTestSuite) TestEnsureLoopLogging(c *C) {
	swfeatstest.CheckEnsureLoopLogging("certmgr.go", c, true)
}

// TestEnsureClearsStaleInactiveMarkerForDisplacedGeneration pins the
// GC-before-render ordering. The currently-active generation carries a stale
// inactivity marker from a previous boot. Because GC runs before the render, the
// marker is cleared while the generation is still current; the render then
// displaces it (new content => new generation), and because the marker was
// cleared, the displaced generation is not treated as pending deletion on this
// boot. If GC ran after the render instead, the generation would already be
// non-current when GC looked at it, and the stale marker from the old boot would
// cause GC to delete it immediately. (SNAPDENG-38146 review feedback)
func (s *certMgrTestSuite) TestEnsureClearsStaleInactiveMarkerForDisplacedGeneration(c *C) {
	restoreBootID := certstate.MockOsutilBootID(func() (string, error) {
		return "boot-2", nil
	})
	defer restoreBootID()

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	// The base starts out providing certificate A.
	certA, _, err := makeTestCertPEM("A")
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(dirs.SystemCertsDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SystemCertsDir, "a.crt"), certA, 0o644), IsNil)

	// Render the generation for A via the real code so it is the active one.
	s.mgr = certstate.Manager(s.state, s.o.TaskRunner())
	c.Assert(s.mgr.Ensure(), IsNil)
	targetA, err := filepath.EvalSymlinks(filepath.Join(dirs.SnapdPKIV1Dir, "merged"))
	c.Assert(err, IsNil)
	genDirA := targetA
	// Resolve to the published/<gen> dir (targetA is absolute after EvalSymlinks).
	c.Assert(genDirA, Not(Equals), "")

	// Mark the active generation as if it were displaced on a previous boot
	// (stale marker with an old boot ID).
	inactiveFile := filepath.Join(genDirA, ".snapd-inactive")
	c.Assert(os.WriteFile(inactiveFile, []byte("boot-1"), 0o644), IsNil)

	// The base now also provides certificate B, so reconciliation will publish a
	// new generation and displace A.
	certB, _, err := makeTestCertPEM("B")
	c.Assert(err, IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SystemCertsDir, "b.crt"), certB, 0o644), IsNil)

	// A fresh CertManager (as after a snapd restart) reconciles. GC must run
	// before the render: it sees A still current with a stale marker and clears
	// the marker, then the render displaces A.
	s.mgr = certstate.Manager(s.state, s.o.TaskRunner())
	c.Assert(s.mgr.Ensure(), IsNil)

	// The displaced generation A survived this boot: GC ran before the render and
	// cleared A's stale marker while A was still current, so when the render then
	// displaced A, there was no old-boot marker left to trigger deletion. The
	// marker stays cleared until a later GC pass re-marks the now-inactive A with
	// the then-current boot ID.
	_, err = os.Stat(genDirA)
	c.Check(err, IsNil, Commentf("displaced generation should survive this boot"))
	c.Check(inactiveFile, testutil.FileAbsent)

	// The active generation now contains B.
	out, err := os.ReadFile(filepath.Join(dirs.SnapdPKIV1Dir, "merged", "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(bytes.Contains(out, certA), Equals, true)
	c.Check(bytes.Contains(out, certB), Equals, true)
}

// TestEnsureKeepsLegacyMergedDirWhenUpdateInProgress covers a legacy plain
// merged directory existing while an update-cert-db change is in flight. The
// store must not be removed on a path that skips rendering, otherwise a change
// that aborts before its update task leaves the store deleted until the next
// snapd restart (SNAPDENG-38146 review feedback).
func (s *certMgrTestSuite) TestEnsureKeepsLegacyMergedDirWhenUpdateInProgress(c *C) {
	// With an update-cert-db change in flight, Ensure returns before the
	// legacy-dir removal, garbage collection, and reconciliation, so no boot ID
	// is needed and nothing is rendered.
	var rendered bool
	restore := certstate.MockRefreshCertificateDatabase(func() error {
		rendered = true
		return nil
	})
	defer restore()

	s.state.Lock()
	s.state.Set("seeded", true)
	c.Assert(os.MkdirAll(dirs.SystemCertsDir, 0o755), IsNil)

	// merged is a plain directory (legacy layout), not a symlink.
	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	c.Assert(os.MkdirAll(mergedDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(mergedDir, "leftover"), []byte("stale"), 0o644), IsNil)

	// An update-cert-db change is in flight.
	chg := s.state.NewChange("foo", "pending update")
	chg.AddTask(s.state.NewTask("update-cert-db", "pending update"))
	s.state.Unlock()

	c.Assert(s.mgr.Ensure(), IsNil)

	// The render was skipped and the legacy directory was left in place, so the
	// store is still present for the in-flight change to reconcile.
	c.Check(rendered, Equals, false)
	c.Check(osutil.IsSymlink(mergedDir), Equals, false)
	_, err := os.Stat(filepath.Join(mergedDir, "leftover"))
	c.Check(err, IsNil)
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

// TestDoUpdateCertificateDatabaseRemovesOldPlainMergedDir ensures the update
// handler removes the old-style merged directory (a plain directory from before
// generations were published) before recording the previous generation, so the
// rollback target is a real generation and not the leftover directory.
func (s *certMgrTestSuite) TestDoUpdateCertificateDatabaseRemovesOldPlainMergedDir(c *C) {
	newPEM, _, err := makeTestCertPEM("new")
	c.Assert(err, IsNil)

	baseCertsDir := dirs.SystemCertsDir
	c.Assert(os.MkdirAll(baseCertsDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseCertsDir, "new.crt"), newPEM, 0o644), IsNil)

	// merged is a plain directory (old layout), not a symlink.
	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	c.Assert(os.MkdirAll(mergedDir, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(mergedDir, "leftover"), []byte("stale"), 0o644), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("foo", "test change")
	chg.AddTask(s.state.NewTask("update-cert-db", "running handler"))
	s.settle(c)

	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	// The plain directory is gone and merged is now a symlink to a generation.
	c.Check(osutil.IsSymlink(mergedDir), Equals, true)
	out, err := os.ReadFile(filepath.Join(mergedDir, "ca-certificates.crt"))
	c.Assert(err, IsNil)
	c.Check(bytes.Contains(out, newPEM), Equals, true)
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
