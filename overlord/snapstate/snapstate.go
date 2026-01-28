// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2025 Canonical Ltd
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

// Package snapstate implements the manager and state aspects responsible for the installation and removal of snaps.
package snapstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
)

// control flags for "Configure()"
const (
	IgnoreHookError = 1 << iota
	UseConfigDefaults
)

const (
	BeginEdge                        = state.TaskSetEdge("begin")
	SnapSetupEdge                    = state.TaskSetEdge("snap-setup")
	BeforeHooksEdge                  = state.TaskSetEdge("before-hooks")
	HooksEdge                        = state.TaskSetEdge("hooks")
	MaybeRebootEdge                  = state.TaskSetEdge("maybe-reboot")
	MaybeRebootWaitEdge              = state.TaskSetEdge("maybe-reboot-wait")
	LastBeforeLocalModificationsEdge = state.TaskSetEdge("last-before-local-modifications")
	EndEdge                          = state.TaskSetEdge("end")
)

// userDaemonsOverrides lists by snap-id a set of well-known snaps for which we
// allow user-daemons directly until we make the feature generally available,
// and not experimental anymore.
//
// TODO: remove this once that is the case
var userDaemonsOverrides = []string{
	"EI0D1KHjP8XiwMZKqSjuh6W8zvcowUVP", // firmware-updater snap-id
	"IrwRHakqtzhFRHJOOPxKVPU0Kk7Erhcu", // snapd-desktop-integration snap-id
	"aoc5lfC8aUd2VL8VpvynUJJhGXp5K6Dj", // prompting-client snap-id
	"gjf3IPXoRiipCu9K0kVu52f0H56fIksg", // snap-store snap-id
}

var ErrNothingToDo = errors.New("nothing to do")

var osutilCheckFreeSpace = osutil.CheckFreeSpace

// TestingLeaveOutKernelUpdateGadgetAssets can be used to simulate an upgrade
// from a broken snapd that does not generate a "update-gadget-assets" task.
// See LP:#1940553
var TestingLeaveOutKernelUpdateGadgetAssets bool = false

type minimalInstallInfo interface {
	InstanceName() string
	Type() snap.Type
	SnapBase() string
	DownloadSize() int64
	Prereq(st *state.State, prqt PrereqTracker) []string
}

type installSnapInfo struct {
	*snap.Info
}

func (ins installSnapInfo) DownloadSize() int64 {
	return ins.DownloadInfo.Size
}

// SnapBase returns the base snap of the snap.
func (ins installSnapInfo) SnapBase() string {
	return ins.Base
}

func (ins installSnapInfo) Prereq(st *state.State, prqt PrereqTracker) []string {
	return keys(defaultProviderContentAttrs(st, ins.Info, prqt))
}

// InsufficientSpaceError represents an error where there is not enough disk
// space to perform an operation.
type InsufficientSpaceError struct {
	// Path is the filesystem path checked for available disk space
	Path string
	// Snaps affected by the failing operation
	Snaps []string
	// Kind of the change that failed
	ChangeKind string
	// Message is optional, otherwise one is composed from the other information
	Message string
}

func (e *InsufficientSpaceError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if len(e.Snaps) > 0 {
		snaps := strings.Join(e.Snaps, ", ")
		return fmt.Sprintf("insufficient space in %q to perform %q change for the following snaps: %s", e.Path, e.ChangeKind, snaps)
	}
	return fmt.Sprintf("insufficient space in %q", e.Path)
}

// Allows to know if snapd should send desktop notifications to the user.
// If there is a snap connected to the snap-refresh-observe slot, then
// no notification should be sent, delegating all the job to that snap.
func ShouldSendNotificationsToTheUser(st *state.State) (bool, error) {
	tr := config.NewTransaction(st)
	experimentalRefreshAppAwarenessUX, err := features.Flag(tr, features.RefreshAppAwarenessUX)
	if err != nil && !config.IsNoOption(err) {
		logger.Noticef("Cannot send notification about pending refresh: %v", err)
		return false, err
	}
	if experimentalRefreshAppAwarenessUX {
		// use notices + warnings fallback flow instead
		return false, nil
	}

	markerExists, err := HasActiveConnection(st, "snap-refresh-observe")
	if err != nil {
		logger.Noticef("Cannot send notification about pending refresh: %v", err)
		return false, err
	}
	if markerExists {
		// found snap with marker interface, skip notification
		return false, nil
	}
	return true, nil
}

// safetyMarginDiskSpace returns size plus a safety margin (5Mb)
func safetyMarginDiskSpace(size uint64) uint64 {
	return size + 5*1024*1024
}

// ConfigureSnap returns a set of tasks to configure snapName as done during installation/refresh.
func ConfigureSnap(st *state.State, snapName string, confFlags int) *state.TaskSet {
	// This is slightly ugly, ideally we would check the type instead
	// of hardcoding the name here. Unfortunately we do not have the
	// type until we actually run the change.
	if isCoreSnap(snapName) {
		confFlags |= IgnoreHookError
	}
	return Configure(st, snapName, nil, confFlags)
}

var Configure = func(st *state.State, snapName string, patch map[string]any, flags int) *state.TaskSet {
	panic("internal error: snapstate.Configure is unset")
}

var DefaultConfigure = func(st *state.State, snapName string) *state.TaskSet {
	panic("internal error: snapstate.DefaultConfigure is unset")
}

var SetupInstallHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupInstallHook is unset")
}

var SetupInstallComponentHook = func(st *state.State, snap, component string) *state.Task {
	panic("internal error: snapstate.SetupInstallComponentHook is unset")
}

var SetupPreRefreshComponentHook = func(st *state.State, snap, component string) *state.Task {
	panic("internal error: snapstate.SetupPreRefreshComponentHook is unset")
}

var SetupPostRefreshComponentHook = func(st *state.State, snap, component string) *state.Task {
	panic("internal error: snapstate.SetupPostRefreshComponentHook is unset")
}

var SetupRemoveComponentHook = func(st *state.State, snap, component string) *state.Task {
	panic("internal error: snapstate.SetupRemoveComponentHook is unset")
}

var SetupPreRefreshHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupPreRefreshHook is unset")
}

var SetupPostRefreshHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupPostRefreshHook is unset")
}

var SetupRemoveHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupRemoveHook is unset")
}

var CheckHealthHook = func(st *state.State, snapName string, rev snap.Revision) *state.Task {
	panic("internal error: snapstate.CheckHealthHook is unset")
}

var SetupGateAutoRefreshHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupAutoRefreshGatingHook is unset")
}

var AddSnapToQuotaGroup = func(st *state.State, snapName string, quotaGroup string) (*state.Task, error) {
	panic("internal error: snapstate.AddSnapToQuotaGroup is unset")
}

var HasActiveConnection = func(st *state.State, iface string) (bool, error) {
	panic("internal error: snapstate.HasActiveConnection is unset")
}

var generateSnapdWrappers = backend.GenerateSnapdWrappers

// isInvokedWithRevert returns true if the current process was invoked in the
// context of runtime failure handling, most likely by snap-failure.
func isInvokedWithRevert() bool {
	return os.Getenv("SNAPD_REVERT_TO_REV") != ""
}

// FinishRestartOptions are options for FinishRestart.
type FinishRestartOptions struct {
	// FinishRestartDefault sets the default behavior for FinishRestart in
	// case the "finish-restart" task variable is not found, that is, this
	// is the behavior for tasks created by older snapd. Tasks that call
	// FinishRestart set this value to what would have been the expected
	// behavior before the introduction of "finish-restart".
	FinishRestartDefault bool
}

// FinishRestart will return a Retry error if there is a pending restart
// and a real error if anything went wrong (like a rollback across
// restarts).
// For snapd snap updates this will also rerun wrappers generation to fully
// catch up with any change.
func FinishRestart(task *state.Task, snapsup *SnapSetup, opts FinishRestartOptions) (err error) {
	if snapdenv.Preseeding() {
		// nothing to do when preseeding
		return nil
	}
	// Check if the task really needs to call this
	needsFinishRestart := opts.FinishRestartDefault
	if err := task.Get("finish-restart", &needsFinishRestart); err != nil &&
		!errors.Is(err, state.ErrNoState) {
		return err
	}
	if !needsFinishRestart {
		return nil
	}

	if ok, _ := restart.Pending(task.State()); ok {
		// don't continue until we are in the restarted snapd
		task.Logf("Waiting for automatic snapd restart...")
		return &state.Retry{}
	}

	if snapsup.Type == snap.TypeSnapd {
		if isInvokedWithRevert() {
			return fmt.Errorf("there was a snapd rollback across the restart")
		}

		snapdInfo, err := snap.ReadCurrentInfo(snapsup.SnapName())
		if err != nil {
			return fmt.Errorf("cannot get current snapd snap info: %v", err)
		}

		// Old versions of snapd did not fill in the version field, unintentionally
		// triggering snapd downgrade detection logic. Fill in the version from the
		// snapd we are currently using.
		if snapsup.Version == "" {
			snapsup.Version = snapdInfo.Version
			if err = SetTaskSnapSetup(task, snapsup); err != nil {
				return err
			}
		}

		// if we have restarted and snapd was refreshed, then we need to generate
		// snapd wrappers again with current snapd, as the logic of generating
		// wrappers may have changed between previous and new snapd code.
		if !release.OnClassic {
			// TODO: if future changes to wrappers need one more snapd restart,
			// then it should be handled here as well.
			restart, err := generateSnapdWrappers(snapdInfo, nil)
			if err != nil {
				return err
			}
			if restart != nil {
				if err := restart.Restart(); err != nil {
					return err
				}
			}
		}
	}

	// consider kernel and base

	deviceCtx, err := DeviceCtx(task.State(), task, nil)
	if err != nil {
		return err
	}

	// Check if there was a rollback. A reboot can be triggered by:
	// - core (old core16 world, system-reboot)
	// - bootable base snap (new core18 world, system-reboot)
	// - kernel
	//
	// If no mode and in ephemeral run modes (like install, recover)
	// there can never be a rollback so we can skip the check there.
	// For bases we do not reboot in classic.
	//
	// TODO: Detect "snapd" snap daemon-restarts here that
	//       fallback into the old version (once we have
	//       better snapd rollback support in core18).
	//
	// Applies only to core-like boot, except if classic with modes for
	// base/core updates.
	if deviceCtx.RunMode() && boot.SnapTypeParticipatesInBoot(snapsup.Type, deviceCtx) {
		// get the name of the name relevant for booting
		// based on the given type
		model := deviceCtx.Model()
		var bootName string
		switch snapsup.Type {
		case snap.TypeKernel:
			bootName = model.Kernel()
		case snap.TypeOS, snap.TypeBase:
			bootName = "core"
			if model.Base() != "" {
				bootName = model.Base()
			}
		default:
			return nil
		}
		// if it is not a snap related to our booting we are not
		// interested
		if snapsup.InstanceName() != bootName {
			return nil
		}

		// compare what we think is "current" for snapd with what
		// actually booted. The bootloader may revert on a failed
		// boot from a bad os/base/kernel to a good one and in this
		// case we need to catch this and error accordingly
		current, err := boot.GetCurrentBoot(snapsup.Type, deviceCtx)
		if err == boot.ErrBootNameAndRevisionNotReady {
			return &state.Retry{After: 5 * time.Second}
		}
		if err != nil {
			return err
		}

		if snapsup.InstanceName() != current.SnapName() || snapsup.SideInfo.Revision != current.SnapRevision() {
			// TODO: make sure this revision gets ignored for
			//       automatic refreshes
			return fmt.Errorf("cannot finish %s installation, there was a rollback across reboot", snapsup.InstanceName())
		}
	}

	return nil
}

// FinishTaskWithRestart will finish a task that needs a restart, by
// setting its status and requesting a restart.
// It should usually be invoked returning its result immediately
// from the caller.
// It delegates the work to restart.FinishTaskWithRestart which decides
// on how the restart will be scheduled.
func FinishTaskWithRestart(task *state.Task, status state.Status, rt restart.RestartType, rebootInfo *boot.RebootInfo) error {
	var rebootRequiredSnap string
	// If system restart is requested, consider how the change the
	// task belongs to is configured (system-restart-immediate) to
	// choose whether request an immediate restart or not.
	if rt == restart.RestartSystem {
		snapsup, err := TaskSnapSetup(task)
		if err != nil {
			return fmt.Errorf("cannot get snap that triggered a reboot: %v", err)
		}
		rebootRequiredSnap = snapsup.InstanceName()

		chg := task.Change()
		var immediate bool
		if chg != nil {
			// ignore errors intentionally, to follow
			// RequestRestart itself which does not
			// return errors. If the state is corrupt
			// something else will error
			chg.Get("system-restart-immediate", &immediate)
		}
		if immediate {
			rt = restart.RestartSystemNow
		}
	}

	return restart.FinishTaskWithRestart(task, status, rt, rebootRequiredSnap, rebootInfo)
}

func isChangeRequestingSnapdRestart(chg *state.Change) bool {
	// during refresh of the snapd snap, after the services of new snapd
	// have been set up in link-snap, daemon restart is requested, link-snap
	// is marked as Done, and the auto-connect task is held off (in Do or
	// Doing states) until the restart completes
	// TODO: This may need additional handling for snapd restart along the
	// Undo path. For instance, 'link-snap' can request a restart in the undo
	// direction, making 'setup-profiles' wait for restart.
	var haveSnapd, linkDone, autoConnectWaiting bool
	for _, tsk := range chg.Tasks() {
		kind := tsk.Kind()
		switch kind {
		case "link-snap", "auto-connect":
			// we're only interested in link-snap and auto-connect
		default:
			continue
		}

		snapsup, err := TaskSnapSetup(tsk)
		if err != nil {
			// we're invoked in rollback scenario, things can be
			// wrong in a way we cannot anticipate, so let's only
			// log the error
			logger.Noticef("cannot obtain task snap-setup from %q: %v", tsk.ID(), err)
			continue
		}

		if snapsup.SnapName() != "snapd" {
			// not the snap we are looking for
			continue
		}

		haveSnapd = true

		status := tsk.Status()

		if kind == "link-snap" && status == state.DoneStatus {
			linkDone = true
		} else if kind == "auto-connect" && (status == state.DoStatus || status == state.DoingStatus) {
			autoConnectWaiting = true
		}
	}

	if haveSnapd && linkDone && autoConnectWaiting {
		// a snapd snap, for which we have a link-snap task that is
		// complete, and an auto-connect task that is waiting to
		// execute, this is a scenario which requests a restart of the
		// snapd daemon
		return true
	}

	return false
}

var ErrUnexpectedRuntimeRestart = errors.New("unexpected restart at runtime")

// CheckExpectedRestart check whether the current process state indicates that
// it may have been started as a response to an unexpected restart at runtime
// (most likely by snap-failure), and depending on the current changes state
// either returns ErrRecoveryFromUnexpectedRuntimeFailure to indicate that no
// failure handling is needed, or nil indicating that snapd should proceed with
// execution.
func CheckExpectedRestart(st *state.State) error {
	if !isInvokedWithRevert() {
		return nil
	}
	// we were invoked by snap-failure, there could be an ongoing refresh of
	// the snapd snap which has failed and a revert is pending, but it could
	// also be the case that the snapd process just failed at runtime, in
	// which case systemd may have triggered an on-failure handling, as such
	// proceed with inspecting the state to identify the scenario

	for _, chg := range st.Changes() {
		if chg.IsReady() {
			continue
		}

		if isChangeRequestingSnapdRestart(chg) {
			return nil
		}
	}

	return ErrUnexpectedRuntimeRestart
}

// IsErrAndNotWait returns true if err is not nil and neither state.Wait, it is
// useful for code using FinishTaskWithRestart to not undo work in the presence
// of a state.Wait return.
func IsErrAndNotWait(err error) bool {
	if _, ok := err.(*state.Wait); err == nil || ok {
		return false
	}
	return true
}

// defaultProviderContentAttrs takes a snap.Info and returns a map of
// default providers to the value of content attributes they should
// provide. Content attributes already provided by a snap in the system are omitted. What is returned depends on the behavior of the passed PrereqTracker.
func defaultProviderContentAttrs(st *state.State, info *snap.Info, prqt PrereqTracker) map[string][]string {
	if prqt == nil {
		prqt = snap.SimplePrereqTracker{}
	}
	repo := ifacerepo.Get(st)
	return prqt.MissingProviderContentTags(info, repo)
}

// validateFeatureFlags validates the given snap only uses experimental
// features that are enabled by the user.
func validateFeatureFlags(st *state.State, info *snap.Info) error {
	tr := config.NewTransaction(st)

	if len(info.Layout) > 0 {
		flag, err := features.Flag(tr, features.Layouts)
		if err != nil {
			return err
		}
		if !flag {
			return fmt.Errorf("experimental feature disabled - test it by setting 'experimental.layouts' to true")
		}
	}

	if info.InstanceKey != "" {
		flag, err := features.Flag(tr, features.ParallelInstances)
		if err != nil {
			return err
		}
		if !flag {
			return fmt.Errorf("experimental feature disabled - test it by setting 'experimental.parallel-instances' to true")
		}
	}

	var hasUserService, usesDbusActivation bool
	for _, app := range info.Apps {
		if app.IsService() && app.DaemonScope == snap.UserDaemon {
			hasUserService = true
		}
		if len(app.ActivatesOn) != 0 {
			usesDbusActivation = true
		}
	}

	if hasUserService {
		flag, err := features.Flag(tr, features.UserDaemons)
		if err != nil {
			return err
		}
		// Some well-known snaps are allowed to use user daemons,
		// irrespective of the feature flag state.
		//
		// TODO: remove the special case once
		// experimental.user-daemons is the default
		if !flag && !strutil.ListContains(userDaemonsOverrides, info.SnapID) {
			return fmt.Errorf("experimental feature disabled - test it by setting 'experimental.user-daemons' to true")
		}
		if !release.SystemctlSupportsUserUnits() {
			return fmt.Errorf("user session daemons are not supported on this release")
		}
	}

	if usesDbusActivation {
		flag, err := features.Flag(tr, features.DbusActivation)
		if err != nil {
			return err
		}
		if !flag {
			return fmt.Errorf("experimental feature disabled - test it by setting 'experimental.dbus-activation' to true")
		}
	}

	return nil
}

func ensureInstallPreconditions(st *state.State, info *snap.Info, flags Flags, snapst *SnapState) (Flags, error) {
	// if snap is allowed to be devmode via the dangerous model and it's
	// confinement is indeed devmode, promote the flags.DevMode to true
	if flags.ApplySnapDevMode && info.NeedsDevMode() {
		// TODO: what about jail-mode? will we also allow putting devmode
		// snaps (i.e. snaps with snap.yaml with confinement: devmode) into
		// strict confinement via the model assertion?
		flags.DevMode = true
	}

	// maintain the classic flag for already classic-confined snaps, assuming
	// we're not switching to jail-mode or devmode
	if !flags.JailMode && !flags.DevMode {
		flags.Classic = flags.Classic || snapst.Classic
	}

	if flags.Classic && !info.NeedsClassic() {
		// snap does not require classic confinement, silently drop the flag
		flags.Classic = false
	}

	// Implicitly set --unaliased flag for parallel installs to avoid
	// alias conflicts with the main snap
	if !snapst.IsInstalled() && !flags.Prefer && info.InstanceKey != "" {
		flags.Unaliased = true
	}

	if err := validateInfoAndFlags(info, snapst, flags); err != nil {
		return flags, err
	}
	if err := validateFeatureFlags(st, info); err != nil {
		return flags, fmt.Errorf("feature flag validation failed for snap %q: %w", info.InstanceName(), err)
	}
	// TODO: if we implement a --disabled flag for install we should skip the
	// dbus and desktop-file-ids checks below.
	if err := checkDBusServiceConflicts(st, info); err != nil {
		return flags, err
	}
	if err := checkDesktopFileIDsConflicts(st, info); err != nil {
		return flags, err
	}
	return flags, nil
}

// A PrereqTracker helps tracking snap prerequisites for one or across
// multiple snap operations. Depending of usage context implementations
// can be stateful or stateless.
// Functions taking a PrereqTracker accept nil and promise to call
// Add once for any target snap.
type PrereqTracker interface {
	// Add adds a snap for tracking.
	Add(*snap.Info)
	// MissingProviderContentTags returns a map keyed by the names of all
	// missing default-providers for the content plugs that the given
	// snap.Info needs. The map values are the corresponding content tags.
	// Different prerequisites trackers might decide in different
	// ways which providers are missing. Either making assumptions about
	// the snap operations that are being set up or considering
	// just the snap info and repo.
	// In the latter case if repo is not nil, any content tag provided by
	// an existing slot in it should be considered already available and
	// filtered out from the result. info might or might have not been
	// passed already to Add. snapstate uses the result to decide to
	// install providers automatically.
	MissingProviderContentTags(info *snap.Info, repo snap.InterfaceRepo) map[string][]string
}

// InstallPath returns a set of tasks for installing a snap from a file path
// and the snap.Info for the given snap.
//
// Note that the state must be locked by the caller.
// The provided SideInfo can contain just a name which results in a
// local revision and sideloading, or full metadata in which case it
// the snap will appear as installed from the store.
func InstallPath(st *state.State, si *snap.SideInfo, path, instanceName, channel string, flags Flags, prqt PrereqTracker) (*state.TaskSet, *snap.Info, error) {
	target := PathInstallGoal(PathSnap{
		InstanceName: instanceName,
		Path:         path,
		SideInfo:     si,
		RevOpts:      RevisionOptions{Channel: channel},
	})

	// TODO have caller pass a context
	info, ts, err := InstallOne(context.Background(), st, target, Options{
		Flags:         flags,
		PrereqTracker: prqt,
	})
	if err != nil {
		return nil, nil, err
	}
	return ts, info, nil
}

// TryPath returns a set of tasks for trying a snap from a file path.
// Note that the state must be locked by the caller.
func TryPath(st *state.State, name, path string, flags Flags) (*state.TaskSet, error) {
	flags.TryMode = true

	ts, _, err := InstallPath(st, &snap.SideInfo{RealName: name}, path, "", "", flags, nil)
	return ts, err
}

// Install returns a set of tasks for installing a snap.
// Note that the state must be locked by the caller.
//
// The returned TaskSet will contain a LastBeforeLocalModificationsEdge
// identifying the last task before the first task that introduces system
// modifications.
func Install(ctx context.Context, st *state.State, name string, opts *RevisionOptions, userID int, flags Flags) (*state.TaskSet, error) {
	return InstallWithDeviceContext(ctx, st, name, opts, userID, flags, nil, nil, "")
}

// InstallWithDeviceContext returns a set of tasks for installing a snap.
// It will query the store for the snap with the given deviceCtx.
// Note that the state must be locked by the caller.
//
// The returned TaskSet will contain a LastBeforeLocalModificationsEdge
// identifying the last task before the first task that introduces system
// modifications.
func InstallWithDeviceContext(ctx context.Context, st *state.State, name string, opts *RevisionOptions, userID int, flags Flags, prqt PrereqTracker, deviceCtx DeviceContext, fromChange string) (*state.TaskSet, error) {
	logger.Debugf("installing with device context %s", name)
	if opts == nil {
		opts = &RevisionOptions{}
	}

	target := StoreInstallGoal(StoreSnap{
		InstanceName: name,
		RevOpts:      *opts,
	})

	_, ts, err := InstallOne(ctx, st, target, Options{
		Flags:         flags,
		UserID:        userID,
		FromChange:    fromChange,
		PrereqTracker: prqt,
		DeviceCtx:     deviceCtx,
	})
	if err != nil {
		return nil, err
	}

	return ts, nil
}

// InstallPathWithDeviceContext returns a set of tasks for installing a local snap.
// Note that the state must be locked by the caller.
//
// The returned TaskSet will contain a LastBeforeLocalModificationsEdge
// identifying the last task before the first task that introduces system
// modifications.
func InstallPathWithDeviceContext(st *state.State, si *snap.SideInfo, path, name string,
	opts *RevisionOptions, userID int, flags Flags, prqt PrereqTracker,
	deviceCtx DeviceContext, fromChange string) (*state.TaskSet, error) {
	logger.Debugf("installing from local file with device context %s", name)

	if opts == nil {
		opts = &RevisionOptions{}
	}

	target := PathInstallGoal(PathSnap{
		InstanceName: name,
		Path:         path,
		SideInfo:     si,
		RevOpts:      *opts,
	})

	_, ts, err := InstallOne(context.Background(), st, target, Options{
		Flags:         flags,
		UserID:        userID,
		FromChange:    fromChange,
		PrereqTracker: prqt,
		DeviceCtx:     deviceCtx,
	})
	if err != nil {
		return nil, err
	}
	return ts, nil
}

// Download returns a set of tasks for downloading a snap and components into
// the given directory. The snap.Info for the snap that is downloaded is also
// returned. The tasks that are returned also download and validate the snap's
// and components' assertions. Prerequisites for the snap are not downloaded.
//
// TODO: this function will soon return an error if downloadDir ==
// dirs.SnapBlobDir.
func Download(
	ctx context.Context,
	st *state.State,
	name string,
	components []string,
	downloadDir string,
	revOpts RevisionOptions,
	opts Options,
) (*state.TaskSet, *snap.Info, error) {
	const skipSnapDownload = false
	return downloadTasks(ctx, st, name, components, downloadDir, skipSnapDownload, revOpts, opts)
}

// DownloadComponents returns a set of tasks for downloading the given snap
// components into the given directory. The tasks that are returned will also
// download and validate the components' assertions.
//
// TODO: this function will soon return an error if downloadDir ==
// dirs.SnapBlobDir.
func DownloadComponents(
	ctx context.Context,
	st *state.State,
	name string,
	components []string,
	downloadDir string,
	revOpts RevisionOptions,
	opts Options,
) (*state.TaskSet, error) {
	const skipSnapDownload = true
	ts, _, err := downloadTasks(ctx, st, name, components, downloadDir, skipSnapDownload, revOpts, opts)
	if err != nil {
		return nil, err
	}
	return ts, nil
}

func downloadTasks(
	ctx context.Context,
	st *state.State,
	name string,
	components []string,
	downloadDir string,
	skipSnapDownload bool,
	revOpts RevisionOptions,
	opts Options,
) (*state.TaskSet, *snap.Info, error) {
	if downloadDir == "" {
		return nil, nil, errors.New("internal error: must specify directory to download to")
	}

	if revOpts.CohortKey != "" && !revOpts.Revision.Unset() {
		return nil, nil, errors.New("internal error: cannot specify revision and cohort")
	}

	if revOpts.Channel == "" {
		revOpts.Channel = "stable"
	}

	if revOpts.ValidationSets == nil {
		revOpts.ValidationSets = snapasserts.NewValidationSets()
	}

	if err := snap.ValidateInstanceName(name); err != nil {
		return nil, nil, fmt.Errorf("invalid instance name: %v", err)
	}

	sar, err := sendOneDownloadAction(ctx, st, StoreSnap{
		InstanceName: name,
		Components:   components,
		RevOpts:      revOpts,
	}, opts)
	if err != nil {
		return nil, nil, err
	}

	info := sar.Info

	if opts.PrereqTracker != nil {
		opts.PrereqTracker.Add(info)
	}

	if opts.Flags.RequireTypeBase && info.Type() != snap.TypeBase && info.Type() != snap.TypeOS {
		return nil, nil, fmt.Errorf("unexpected snap type %q, instead of 'base'", info.Type())
	}

	snapsup := &SnapSetup{
		Channel:                     revOpts.Channel,
		Base:                        info.Base,
		UserID:                      opts.UserID,
		Flags:                       opts.Flags.ForSnapSetup(),
		DownloadInfo:                &info.DownloadInfo,
		SideInfo:                    &info.SideInfo,
		Type:                        info.Type(),
		Version:                     info.Version,
		InstanceKey:                 info.InstanceKey,
		CohortKey:                   revOpts.CohortKey,
		ExpectedProvenance:          info.SnapProvenance,
		DownloadBlobDir:             downloadDir,
		ComponentExclusiveOperation: skipSnapDownload,
	}

	if sar.RedirectChannel != "" {
		snapsup.Channel = sar.RedirectChannel
	}

	compsups, err := componentTargetsFromActionResult("download", sar, components)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot extract components from snap resources: %w", err)
	}

	for i := range compsups {
		compsups[i].DownloadBlobDir = downloadDir
	}

	if err := checkSnapAgainstValidationSets(sar.Info, compsups, "download", revOpts.ValidationSets); err != nil {
		return nil, nil, err
	}

	ts := state.NewTaskSet()
	var snapsupTask, prev *state.Task
	addTask := func(t *state.Task) {
		ts.AddTask(t)
		if prev == nil {
			t.Set("snap-setup", snapsup)
			snapsupTask = t
			ts.MarkEdge(t, BeginEdge)
			ts.MarkEdge(t, SnapSetupEdge)
		} else {
			t.WaitFor(prev)
			t.Set("snap-setup-task", snapsupTask.ID())
		}
		prev = t
	}

	if !skipSnapDownload {
		// TODO:COMPS: support checking for available space for components
		toDownloadTo := filepath.Dir(snapsup.BlobPath())
		if err := checkDiskSpaceDownload([]minimalInstallInfo{installSnapInfo{info}}, toDownloadTo); err != nil {
			return nil, nil, err
		}

		revisionStr := fmt.Sprintf(" (%s)", snapsup.Revision())

		download := st.NewTask("download-snap", fmt.Sprintf(i18n.G("Download snap %q%s from channel %q"), snapsup.InstanceName(), revisionStr, snapsup.Channel))
		addTask(download)

		validate := st.NewTask("validate-snap", fmt.Sprintf(i18n.G("Fetch and check assertions for snap %q%s"), snapsup.InstanceName(), revisionStr))
		addTask(validate)
	}

	compsupIDs := make([]string, 0, len(compsups))
	for _, c := range compsups {
		rev := fmt.Sprintf(" (%s)", c.CompSideInfo.Revision)

		download := st.NewTask("download-component", fmt.Sprintf(i18n.G("Download component %q%s"), c.ComponentName(), rev))
		download.Set("component-setup", c)
		addTask(download)
		compsupTaskID := download.ID()

		// even if the component itself is already installed, it might not have
		// been installed with the same snap revision. in that case,
		// validate-component will fetch new assertions from the store.
		validate := st.NewTask("validate-component", fmt.Sprintf(
			i18n.G("Fetch and check assertions for component %q%s"), c.ComponentName(), rev),
		)
		validate.Set("component-setup-task", compsupTaskID)
		addTask(validate)

		compsupIDs = append(compsupIDs, compsupTaskID)
	}

	snapsupTask.Set("component-setup-tasks", compsupIDs)

	// since nothing in this function does any "local" modifications, we just
	// set this edge on the last task in the chain
	ts.MarkEdge(prev, LastBeforeLocalModificationsEdge)

	return ts, info, nil
}

func validatedInfoFromPathAndSideInfo(instanceName string, path string, si *snap.SideInfo) (*snap.Info, error) {
	var info *snap.Info
	info, cont, err := backend.OpenSnapFile(path, si)
	if err != nil {
		return nil, fmt.Errorf("cannot open snap file: %v", err)
	}
	if err := validateContainer(cont, info, logger.Noticef); err != nil {
		return nil, err
	}

	snapName, instanceKey := snap.SplitInstanceName(instanceName)
	if info.SnapName() != snapName {
		return nil, fmt.Errorf("cannot install snap %q: instance name prefix does not match snap name: %s != %s", instanceName, snapName, info.SnapName())
	}
	info.InstanceKey = instanceKey

	return info, nil
}

// InstallPathMany returns a set of tasks for installing snaps from a file paths
// and snap.Infos.
//
// The state must be locked by the caller.
// The provided SideInfos can contain just a name which results in a
// local revision and sideloading, or full metadata in which case
// the snaps will appear as installed from the store.
func InstallPathMany(ctx context.Context, st *state.State, sideInfos []*snap.SideInfo, paths []string, userID int, flags *Flags) ([]*state.TaskSet, error) {
	if len(paths) != len(sideInfos) {
		return nil, fmt.Errorf("internal error: number of paths and side infos must match: %d != %d", len(paths), len(sideInfos))
	}

	if flags == nil {
		flags = &Flags{}
	}

	// this is to maintain backwards compatibility with the old behavior of
	// InstallPathMany
	if flags.Transaction == "" {
		flags.Transaction = client.TransactionPerSnap
	}

	// this is to maintain backwards compatibility with the old behavior of
	// InstallPathMany
	flags.NoReRefresh = true

	updates := make([]PathSnap, 0, len(sideInfos))
	for i, si := range sideInfos {
		updates = append(updates, PathSnap{
			Path:     paths[i],
			SideInfo: si,
		})
	}

	goal := PathUpdateGoal(updates...)
	_, uts, err := UpdateWithGoal(ctx, st, goal, nil, Options{
		Flags:     *flags,
		UserID:    userID,
		DeviceCtx: nil,
	})
	if err != nil {
		return nil, err
	}

	return uts.Refresh, nil
}

// InstallMany installs everything from the given list of names. When specifying
// revisions, the checks against enforced validation sets are bypassed.
// Note that the state must be locked by the caller.
func InstallMany(st *state.State, names []string, revOpts []*RevisionOptions, userID int, flags *Flags) ([]string, []*state.TaskSet, error) {
	if flags == nil {
		flags = &Flags{}
	}

	// this is to maintain backwards compatibility with the old behavior
	if flags.Transaction == "" {
		flags.Transaction = client.TransactionPerSnap
	}

	if len(revOpts) > 0 && len(revOpts) != len(names) {
		return nil, nil, fmt.Errorf("number of revisions does not match number of snaps")
	}

	snaps := make([]StoreSnap, 0, len(names))
	for i, name := range names {
		sn := StoreSnap{
			InstanceName:  name,
			SkipIfPresent: true,
		}
		if len(revOpts) > 0 && revOpts[i] != nil {
			sn.RevOpts = *revOpts[i]
		}
		snaps = append(snaps, sn)
	}

	target := StoreInstallGoal(snaps...)
	// TODO have caller pass a context
	infos, tss, err := InstallWithGoal(context.Background(), st, target, Options{
		Flags:  *flags,
		UserID: userID,
	})
	if err != nil {
		return nil, nil, err
	}

	installed := make([]string, 0, len(infos))
	for _, info := range infos {
		installed = append(installed, info.InstanceName())
	}

	return installed, tss, err
}

// RefreshCandidates gets a list of candidates for update
// Note that the state must be locked by the caller.
func RefreshCandidates(st *state.State, user *auth.UserState) ([]*snap.Info, error) {
	allSnaps, err := All(st)
	if err != nil {
		return nil, err
	}

	opts := Options{
		PrereqTracker: snap.SimplePrereqTracker{},
	}

	plan, err := storeUpdatePlan(context.TODO(), st, allSnaps, nil, user, nil, opts)
	if err != nil {
		return nil, err
	}

	return plan.revisionChanges(st, opts)
}

// ValidateRefreshes allows to hook validation into the handling of refresh candidates.
var ValidateRefreshes func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx DeviceContext) (validated []*snap.Info, err error)

// UpdateMany updates everything from the given list of names that the
// store says is updatable. If the list is empty, update everything.
// Note that the state must be locked by the caller.
func UpdateMany(ctx context.Context, st *state.State, names []string, revOpts []*RevisionOptions, userID int, flags *Flags) ([]string, []*state.TaskSet, error) {
	updated, tasksetGrp, err := updateManyFiltered(ctx, st, names, revOpts, userID, nil, flags, "")
	if err != nil {
		return nil, nil, err
	}
	return updated, tasksetGrp.Refresh, nil
}

func currentEssentialSnapNames(st *state.State) ([]string, error) {
	deviceCtx, err := DeviceCtxFromState(st, nil)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, sn := range deviceCtx.Model().EssentialSnaps() {
		names = append(names, sn.SnapName())
	}

	// some models have an implicit snapd, make sure that we account for it here
	if !strutil.ListContains(names, "snapd") {
		names = append(names, "snapd")
	}

	return names, nil
}

// ResolveValidationSetsEnforcementError installs and updates snaps in order to
// meet the validation set constraints reported in the ValidationSetsValidationError..
func ResolveValidationSetsEnforcementError(ctx context.Context, st *state.State, valErr *snapasserts.ValidationSetsValidationError, pinnedSeqs map[string]int, userID int) ([]*state.TaskSet, []string, error) {
	if len(valErr.InvalidSnaps) != 0 {
		invSnaps := make([]string, 0, len(valErr.InvalidSnaps))
		for invSnap := range valErr.InvalidSnaps {
			invSnaps = append(invSnaps, invSnap)
		}
		return nil, nil, fmt.Errorf("cannot auto-resolve validation set constraints that require removing snaps: %s", strutil.Quoted(invSnaps))
	}

	var invComps []string
	for snapName, cerr := range valErr.ComponentErrors {
		for compName := range cerr.InvalidComponents {
			invComps = append(invComps, naming.NewComponentRef(snapName, compName).String())
		}
	}
	if len(invComps) != 0 {
		return nil, nil, fmt.Errorf("cannot auto-resolve validation set constraints that require removing components: %s", strutil.Quoted(invComps))
	}

	vsets := snapasserts.NewValidationSets()
	for _, vs := range valErr.Sets {
		if err := vsets.Add(vs); err != nil {
			return nil, nil, err
		}
	}

	affected := make([]string, 0, len(valErr.MissingSnaps)+len(valErr.WrongRevisionSnaps))
	// use the same lane for installing and refreshing so everything is reversed
	lane := st.NewLane()

	// keep track of snaps that are being having their validation issues
	// resolved. we won't need to resolve any of their component errors
	// explicitly.
	resolved := make(map[string]bool)

	essential, err := currentEssentialSnapNames(st)
	if err != nil {
		return nil, nil, err
	}

	var wrongRevs, wrongRevsEssential []StoreUpdate
	for name := range valErr.WrongRevisionSnaps {
		resolved[name] = true

		var additionalComps []string
		if cerr, ok := valErr.ComponentErrors[name]; ok {
			additionalComps = keys(cerr.MissingComponents)
		}

		update := StoreUpdate{
			InstanceName: name,
			RevOpts: RevisionOptions{
				ValidationSets: vsets,
			},
			AdditionalComponents: additionalComps,
		}

		if strutil.ListContains(essential, name) {
			wrongRevsEssential = append(wrongRevsEssential, update)
		} else {
			wrongRevs = append(wrongRevs, update)
		}
	}

	var missing []StoreSnap
	for name := range valErr.MissingSnaps {
		var comps []string
		if cerr, ok := valErr.ComponentErrors[name]; ok {
			comps = keys(cerr.MissingComponents)
		}

		resolved[name] = true
		missing = append(missing, StoreSnap{
			InstanceName: name,
			RevOpts: RevisionOptions{
				ValidationSets: vsets,
			},
			Components: comps,
		})
	}

	var essentialTss []*state.TaskSet
	if len(wrongRevsEssential) > 0 {
		updated, uts, err := UpdateWithGoal(ctx, st, StoreUpdateGoal(wrongRevsEssential...), nil, Options{
			Flags: Flags{Transaction: client.TransactionAllSnaps, Lane: lane, NoReRefresh: true},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("cannot auto-resolve enforcement constraints: %w", err)
		}

		essentialTss = append(essentialTss, uts.Refresh...)
		affected = append(affected, updated...)
	}

	var updateTss []*state.TaskSet
	if len(wrongRevs) > 0 {
		updated, uts, err := UpdateWithGoal(ctx, st, StoreUpdateGoal(wrongRevs...), nil, Options{
			Flags: Flags{Transaction: client.TransactionAllSnaps, Lane: lane, NoReRefresh: true},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("cannot auto-resolve enforcement constraints: %w", err)
		}

		updateTss = append(updateTss, uts.Refresh...)
		affected = append(affected, updated...)
	}

	var installed []*snap.Info
	var installTss []*state.TaskSet
	if len(missing) > 0 {
		var err error
		installed, installTss, err = InstallWithGoal(ctx, st, StoreInstallGoal(missing...), Options{
			Flags: Flags{Transaction: client.TransactionAllSnaps, Lane: lane},
		})
		if err != nil {
			return nil, nil, fmt.Errorf("cannot auto-resolve enforcement constraints: %w", err)
		}
	}

	for snapName, cerr := range valErr.ComponentErrors {
		if resolved[snapName] {
			continue
		}

		comps := make([]string, 0, len(cerr.MissingComponents)+len(cerr.WrongRevisionComponents))
		comps = append(comps, keys(cerr.MissingComponents)...)
		comps = append(comps, keys(cerr.WrongRevisionComponents)...)

		info, err := CurrentInfo(st, snapName)
		if err != nil {
			return nil, nil, err
		}

		compTasks, err := InstallComponents(ctx, st, comps, info, vsets, Options{})
		if err != nil {
			return nil, nil, err
		}

		affected = append(affected, snapName)
		installTss = append(installTss, compTasks...)
	}

	for _, i := range installed {
		affected = append(affected, i.InstanceName())
	}

	// here we enforce some ordering constraints:
	//  * essential snaps are updated first; this ensures that all reboots are
	//    grouped together and subsequent updates/installs will see the new
	//    versions of the essential snaps
	//  * non-essential bases are installed next. this ensures that the
	//    following updates and installs have their bases available. without this
	//    ordering constraint, updates that bring in a new base will create a
	//    circular dependency on the update's "prerequisites" task.
	//  * the remaining updates and installs are done last

	// installTss is made up of the install tasks for snaps and components (in
	// that order). we only care about the snap ones, so we take a subslice.
	var baseInstalls, nonBaseInstalls []*state.TaskSet
	for i, ts := range installTss[:len(installed)] {
		if installed[i].Type() == snap.TypeBase {
			baseInstalls = append(baseInstalls, ts)
		} else {
			nonBaseInstalls = append(nonBaseInstalls, ts)
		}
	}

	nonBaseInstalls = append(nonBaseInstalls, installTss[len(installed):]...)

	// TODO: make use of EndEdges to make this chaining result in cleaner graphs
	tasksets := flattenAndWaitTaskSets(essentialTss, baseInstalls, updateTss, nonBaseInstalls)

	encodedAsserts := make(map[string][]byte, len(valErr.Sets))
	for vsStr, vs := range valErr.Sets {
		encodedAsserts[vsStr] = asserts.Encode(vs)
	}

	enforceTask := st.NewTask("enforce-validation-sets", "Enforce validation sets")
	enforceTask.Set("validation-sets", encodedAsserts)
	enforceTask.Set("pinned-sequence-numbers", pinnedSeqs)
	enforceTask.Set("userID", userID)

	for _, ts := range tasksets {
		enforceTask.WaitAll(ts) // TODO: make this not a WaitAll
	}
	ts := state.NewTaskSet(enforceTask)
	ts.JoinLane(lane)
	tasksets = append(tasksets, ts)

	return tasksets, affected, nil
}

// flattenAndWaitTaskSets merges a slice of [state.TaskSet] slices into one flat
// slice. It also enforces ordering so that every [state.Task] in chain[i] waits
// for each [state.Task] in chain[i-1].
//
// TODO: This implementation could be better if we relied on each task set to
// have an [EndEdge]. This isn't true at the moment.
func flattenAndWaitTaskSets(chain ...[]*state.TaskSet) []*state.TaskSet {
	if len(chain) == 0 {
		return nil
	}

	prev := chain[0]
	flattened := make([]*state.TaskSet, len(prev))
	copy(flattened, prev)

	for _, tss := range chain[1:] {
		// all of the tasks in tss must go after all of the tasks in prev
		// thus, all of the tasks in tss must wait on all of the tasks in prev
		for _, ts := range tss {
			for _, prevTs := range prev {
				ts.WaitAll(prevTs) // TODO: make this not a WaitAll
			}
		}
		flattened = append(flattened, tss...)
		prev = tss
	}

	return flattened
}

func keys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// updateFilter is the type of function that can be passed to
// updateManyFromChange so it filters the updates.
//
// If the filter returns true, the update for that snap proceeds. If
// it returns false, the snap is removed from the list of updates to
// consider.
type updateFilter = func(*snap.Info, *SnapState) bool

func updateManyFiltered(ctx context.Context, st *state.State, names []string, revOpts []*RevisionOptions, userID int, filter updateFilter, flags *Flags, fromChange string) ([]string, *UpdateTaskSets, error) {
	if flags == nil {
		flags = &Flags{}
	}

	// this is to maintain backwards compatibility with the old behavior
	if flags.Transaction == "" {
		flags.Transaction = client.TransactionPerSnap
	}

	updates := make([]StoreUpdate, 0, len(names))
	for i, name := range names {
		opts := RevisionOptions{}
		if len(revOpts) > 0 {
			opts = *revOpts[i]
		}

		updates = append(updates, StoreUpdate{
			InstanceName: name,
			RevOpts:      opts,
		})
	}

	goal := StoreUpdateGoal(updates...)
	return UpdateWithGoal(ctx, st, goal, filter, Options{
		Flags:      *flags,
		UserID:     userID,
		FromChange: fromChange,
		DeviceCtx:  nil,
	})
}

// canSplitRefresh returns whether the refresh is a standard refresh of a mix
// of essential and non-essential snaps on a hybrid system. If the refresh
// can be split, it also returns the two split update groups.
func canSplitRefresh(deviceCtx DeviceContext, updates []update) (essential, nonEssential []update, split bool) {
	if !deviceCtx.IsCoreBoot() || !release.OnClassic {
		return nil, nil, false
	}

	essential, nonEssential = splitEssentialUpdates(deviceCtx, updates)
	if len(essential) == 0 || len(nonEssential) == 0 {
		return nil, nil, false
	}

	return essential, nonEssential, true
}

// splitRefresh creates independent refresh task chains for the essential and
// non-essential snaps, so that the latter can refresh independently without
// waiting for the reboot that the essential snaps require. The only cross-set
// dependency is snapd which, if present, must refresh before all other snaps.
func splitRefresh(st *state.State, essential, nonEssential []update, userID int, flags *Flags, updateFunc func([]update) ([]string, bool, *UpdateTaskSets, error)) ([]string, *UpdateTaskSets, error) {
	// taskset with essential snaps (snapd, kernel, gadget and the model base)
	essentialUpdated, _, essentialTss, err := updateFunc(essential)
	if err != nil {
		return nil, nil, err
	}

	// taskset with non-essential snaps (apps and their bases)
	nonEssentialUpdated, nonEssentialReRefresh, nonEssentialTss, err := updateFunc(nonEssential)
	if err != nil {
		return nil, nil, err
	}

	allUpdated := append(essentialUpdated, nonEssentialUpdated...)

	// if snapd is in the essential snaps set, the non-essentials must wait for it
	if strutil.ListContains(allUpdated, "snapd") {
		snapdTss, err := maybeFindTasksetForSnap(essentialTss.Refresh, "snapd")
		if err != nil {
			return nil, nil, err
		}

		// make non-essential snaps also wait for snapd
		snapdEndTask := snapdTss.MaybeEdge(EndEdge)
		if snapdEndTask == nil {
			return nil, nil, fmt.Errorf("internal error: cannot find last task in snapd's update taskset")
		}

		for _, ts := range nonEssentialTss.Refresh {
			startTask := ts.MaybeEdge(BeginEdge)
			if startTask == nil {
				return nil, nil, fmt.Errorf("internal error: cannot find first task in snap's taskset")
			}
			startTask.WaitFor(snapdEndTask)
		}
	}

	// essential snaps don't use epochs at the moment so we can run a check-rerefresh
	// task with the non-essential set (before the reboot). Note that even if some
	// app depends on the model base, the prerequisites code will only wait for it
	// to link and therefore doesn't need to wait for the reboot
	if len(nonEssentialTss.Refresh) > 0 && !flags.NoReRefresh {
		var considerTasks []string
		for _, ts := range nonEssentialTss.Refresh {
			for _, t := range ts.Tasks() {
				considerTasks = append(considerTasks, t.ID())
			}
		}

		nonEssentialTss.Refresh = finalizeUpdate(st, nonEssentialTss.Refresh, nonEssentialReRefresh, nonEssentialUpdated, considerTasks, userID, flags)
	}

	return allUpdated, &UpdateTaskSets{
		// only non-essential snaps can trigger pre-downloads
		PreDownload: nonEssentialTss.PreDownload,
		Refresh:     append(essentialTss.Refresh, nonEssentialTss.Refresh...),
	}, nil
}

func maybeFindTasksetForSnap(tss []*state.TaskSet, name string) (*state.TaskSet, error) {
	for _, ts := range tss {
		for _, t := range ts.Tasks() {
			var snapsup SnapSetup
			err := t.Get("snap-setup", &snapsup)
			if err != nil {
				if errors.Is(err, state.ErrNoState) {
					continue
				}
				return nil, err
			}
			if snapsup.InstanceName() != name {
				break
			}
			return ts, nil
		}
	}

	return nil, nil
}

// UpdateTaskSets distinguishes tasksets for refreshes and pre-downloads since an
// auto-refresh can return both (even simultaneously).
type UpdateTaskSets struct {
	// PreDownload holds the pre-downloads tasksets created when there are busy
	// snaps that can't be refreshed during an auto-refresh.
	PreDownload []*state.TaskSet
	// Refresh holds the refresh tasksets.
	Refresh []*state.TaskSet
}

// update contains the state of a snap before it is updated on the system and
// the desired state of the snap.
type update struct {
	// SnapState contains the state of the snap on the system, before the snap is
	// updated.
	SnapState SnapState
	// Setup contains the desired state of the snap.
	Setup SnapSetup
	// Components contains the desired state of the components of the snap.
	Components []ComponentSetup
}

// revisionSatisfied returns true if the state of the snap on the system matches
// the state specified in the update. This method checks if the currently
// installed snap and components match what is specified in the update.
func (u *update) revisionSatisfied() (bool, error) {
	if u.Setup.AlwaysUpdate || !u.SnapState.IsInstalled() {
		return false, nil
	}

	if u.SnapState.Current != u.Setup.Revision() {
		return false, nil
	}

	comps, err := u.SnapState.CurrentComponentInfos()
	if err != nil {
		return false, err
	}

	currentCompRevs := make(map[string]snap.Revision, len(comps))
	for _, comp := range comps {
		currentCompRevs[comp.Component.ComponentName] = comp.Revision
	}

	for _, comp := range u.Components {
		if currentCompRevs[comp.CompSideInfo.Component.ComponentName] != comp.Revision() {
			return false, nil
		}
	}

	return true, nil
}

func doPotentiallySplitUpdate(st *state.State, requested []string, updates []update, opts Options) ([]string, *UpdateTaskSets, error) {
	// if we're on classic with a kernel/gadget, split refreshes with essential
	// snaps and apps so that the apps don't have to wait for a reboot
	if essential, nonEssential, ok := canSplitRefresh(opts.DeviceCtx, updates); ok {
		// if we're putting all of the snaps in the same lane, create the lane
		// now so that it can be shared between the essential and non-essential
		// sets
		if opts.Flags.Transaction == client.TransactionAllSnaps {
			opts.Flags.Lane = st.NewLane()
		}

		updateFunc := func(updates []update) ([]string, bool, *UpdateTaskSets, error) {
			// names are used to determine if the refresh is general, if it was
			// requested for a snap to update aliases and if it should be
			// reported so it's fine to pass them all into each call (extra are
			// ignored)
			return doUpdate(st, requested, updates, opts)
		}

		// splitRefresh already creates a check-rerefresh task as needed
		return splitRefresh(st, essential, nonEssential, opts.UserID, &opts.Flags, updateFunc)
	}

	updated, rerefresh, uts, err := doUpdate(st, requested, updates, opts)
	if err != nil {
		return nil, nil, err
	}

	// if there are only pre-downloads, don't add a check-rerefresh task
	if len(uts.Refresh) > 0 {
		uts.Refresh = finalizeUpdate(st, uts.Refresh, rerefresh, updated, nil, opts.UserID, &opts.Flags)
	}

	return updated, uts, nil
}

func doUpdate(st *state.State, requested []string, updates []update, opts Options) ([]string, bool, *UpdateTaskSets, error) {
	var installTasksets []*state.TaskSet
	var preDlTasksets []*state.TaskSet

	refreshAll := len(requested) == 0

	var nameSet map[string]bool
	if len(requested) != 0 {
		nameSet = make(map[string]bool, len(requested))
		for _, name := range requested {
			nameSet[name] = true
		}
	}

	newAutoAliases, mustPruneAutoAliases, transferTargets, err := autoAliasesUpdate(st, requested, updates)
	if err != nil {
		return nil, false, nil, err
	}

	reportUpdated := make(map[string]bool, len(updates))
	var pruningAutoAliasesTs *state.TaskSet

	if len(mustPruneAutoAliases) != 0 {
		var err error
		pruningAutoAliasesTs, err = applyAutoAliasesDelta(st, mustPruneAutoAliases, "prune", refreshAll, opts.FromChange, func(snapName string, _ *state.TaskSet) {
			if nameSet[snapName] {
				reportUpdated[snapName] = true
			}
		})
		if err != nil {
			return nil, false, nil, err
		}
		installTasksets = append(installTasksets, pruningAutoAliasesTs)
	}

	// wait for the auto-alias prune tasks as needed
	scheduleUpdate := func(snapName string, ts *state.TaskSet) {
		if pruningAutoAliasesTs != nil && (mustPruneAutoAliases[snapName] != nil || transferTargets[snapName]) {
			ts.WaitAll(pruningAutoAliasesTs)
		}
		reportUpdated[snapName] = true
	}

	// first snapd, core, kernel, bases, then rest
	sort.SliceStable(updates, func(i, j int) bool {
		return updates[i].Setup.Type.SortsBefore(updates[j].Setup.Type)
	})

	if opts.Flags.Transaction == client.TransactionAllSnaps && opts.Flags.Lane == 0 {
		opts.Flags.Lane = st.NewLane()
	}

	// some snaps might not have a revision change, we'll keep track of those
	// and check to see if we need to switch any metadata, like channel or
	// cohort
	var alreadySatisfied []update

	// keep track of any snaps that we requested to refresh actually got
	// refreshed. if any do, tell the caller that we need check for potential
	// re-refreshes
	needsRerefreshCheck := false

	// updates is sorted by kind so this will process first core
	// and bases and then other snaps
	for _, up := range updates {
		// if the update is already satisfied, then we can skip it
		ok, err := up.revisionSatisfied()
		if err != nil {
			return nil, false, nil, err
		}

		if ok {
			alreadySatisfied = append(alreadySatisfied, up)
			continue
		}

		if err := checkSnapRefreshFailures(st, &up.SnapState, up.Setup.Revision(), opts); err != nil {
			if errors.Is(err, errKnownBadRevision) {
				// revision known to fail during refresh and backoff delay has not passed
				continue
			}
			return nil, false, nil, err
		}

		// if any snaps actually get a revision change, we need to do a
		// re-refresh check
		needsRerefreshCheck = true

		// Do not set any default restart boundaries, we do it when we have access to all
		// the task-sets in preparation for single-reboot.
		ts, err := doInstallOrPreDownload(st, &up.SnapState, &up.Setup, up.Components, installContext{
			FromChange:          opts.FromChange,
			DeviceCtx:           opts.DeviceCtx,
			NoRestartBoundaries: true,
		})
		if err != nil {
			if errors.Is(err, &timedBusySnapError{}) && ts != nil {
				// snap is busy and pre-download tasks were made for it
				ts.JoinLane(st.NewLane())
				preDlTasksets = append(preDlTasksets, ts)
				continue
			}

			if refreshAll {
				logger.Noticef("cannot refresh snap %q: %v", up.Setup.InstanceName(), err)
				continue
			}
			return nil, false, nil, err
		}

		ts.JoinLane(generateLane(st, opts))

		scheduleUpdate(up.Setup.InstanceName(), ts)
		installTasksets = append(installTasksets, ts)
	}

	// Make sure each of them are marked with default restart-boundaries to maintain the previous
	// reboot-behaviour prior to new restart logic.
	if err := arrangeSnapTaskSetsLinkageAndRestart(st, nil, installTasksets); err != nil {
		return nil, false, nil, err
	}

	if len(newAutoAliases) != 0 {
		addAutoAliasesTs, err := applyAutoAliasesDelta(st, newAutoAliases, "refresh", refreshAll, opts.FromChange, scheduleUpdate)
		if err != nil {
			return nil, false, nil, err
		}
		installTasksets = append(installTasksets, addAutoAliasesTs)
	}

	for _, up := range alreadySatisfied {
		switchTs, err := maybeSwitchSnapMetadataTaskSet(st, up.Setup, up.SnapState, opts)
		if err != nil {
			return nil, false, nil, err
		}

		// nothing to do, we can skip this one
		if switchTs == nil {
			continue
		}

		if len(installTasksets) != 0 {
			switchTs.WaitAll(installTasksets[len(installTasksets)-1])
		}

		switchTs.JoinLane(generateLane(st, opts))
		installTasksets = append(installTasksets, switchTs)
		reportUpdated[up.Setup.InstanceName()] = true
	}

	updated := make([]string, 0, len(reportUpdated))
	for name := range reportUpdated {
		updated = append(updated, name)
	}

	updateTss := &UpdateTaskSets{
		Refresh:     installTasksets,
		PreDownload: preDlTasksets,
	}

	return updated, needsRerefreshCheck, updateTss, nil
}

func maybeSwitchSnapMetadataTaskSet(st *state.State, snapsup SnapSetup, snapst SnapState, opts Options) (*state.TaskSet, error) {
	switchChannel := snapst.TrackingChannel != snapsup.Channel
	switchCohortKey := snapst.CohortKey != snapsup.CohortKey

	// we only toggle validation set enforcement if we are refreshing exactly
	// one snap
	toggleIgnoreValidation := (snapst.IgnoreValidation != snapsup.IgnoreValidation) && opts.ExpectOneSnap

	// nothing to do, we can leave early
	if !switchChannel && !switchCohortKey && !toggleIgnoreValidation {
		return nil, nil
	}

	if err := checkChangeConflictIgnoringOneChange(st, snapst.InstanceName(), nil, opts.FromChange); err != nil {
		return nil, err
	}

	var snapsupTask *state.Task

	var tasks []*state.Task
	if switchChannel || switchCohortKey {
		summary := switchSummary(snapsup.InstanceName(), snapst.TrackingChannel, snapsup.Channel, snapst.CohortKey, snapsup.CohortKey)
		switchSnap := st.NewTask("switch-snap-channel", summary)
		switchSnap.Set("snap-setup", &snapsup)
		snapsupTask = switchSnap

		tasks = append(tasks, switchSnap)
	}

	if toggleIgnoreValidation {
		toggle := st.NewTask("toggle-snap-flags", fmt.Sprintf(i18n.G("Toggle snap %q flags"), snapsup.InstanceName()))
		if snapsupTask == nil {
			toggle.Set("snap-setup", &snapsup)
			snapsupTask = toggle
		} else {
			toggle.Set("snap-setup-task", snapsupTask.ID())
		}

		for _, tasks := range tasks {
			toggle.WaitFor(tasks)
		}

		tasks = append(tasks, toggle)
	}

	ts := state.NewTaskSet(tasks...)
	if snapsupTask != nil {
		ts.MarkEdge(snapsupTask, SnapSetupEdge)
	}

	return ts, nil
}

func splitEssentialUpdates(deviceCtx DeviceContext, updates []update) (essential, nonEssential []update) {
	snapdAndModelBase := make([]update, 0, 2)
	for _, up := range updates {
		switch up.Setup.Type {
		case snap.TypeSnapd:
			snapdAndModelBase = append(snapdAndModelBase, up)
		case snap.TypeBase:
			if up.Setup.InstanceName() == deviceCtx.Base() {
				snapdAndModelBase = append(snapdAndModelBase, up)
			} else {
				nonEssential = append(nonEssential, up)
			}
		case snap.TypeGadget, snap.TypeKernel:
			// snaps that require a reboot
			essential = append(essential, up)
		default:
			nonEssential = append(nonEssential, up)
		}
	}

	// if there's no other essential snaps, snapd and the model base can be
	// refreshed with the apps (order doesn't matter here, we sort later)
	if len(essential) > 0 {
		essential = append(essential, snapdAndModelBase...)
	} else {
		nonEssential = append(nonEssential, snapdAndModelBase...)
	}

	return essential, nonEssential
}

func finalizeUpdate(st *state.State, tasksets []*state.TaskSet, hasUpdates bool, updated, considerTasks []string, userID int, globalFlags *Flags) []*state.TaskSet {
	if hasUpdates && !globalFlags.NoReRefresh {
		// re-refresh will check the lanes to decide what to
		// _actually_ re-refresh, but it'll be a subset of updated
		// (and equal to updated if nothing goes wrong)
		sort.Strings(updated)
		rerefresh := st.NewTask("check-rerefresh", reRefreshSummary(updated, globalFlags))
		rerefresh.Set("rerefresh-setup", reRefreshSetup{
			UserID:  userID,
			Flags:   globalFlags,
			TaskIDs: considerTasks,
		})
		tasksets = append(tasksets, state.NewTaskSet(rerefresh))
	}
	return tasksets
}

func reRefreshSummary(updated []string, flags *Flags) string {
	var msg string
	n := len(updated)
	if n > 1 && !flags.IsAutoRefresh {
		n = 2
	}
	switch n {
	case 0:
		return ""
	case 1:
		msg = fmt.Sprintf(i18n.G("Monitoring snap %q to determine whether extra refresh steps are required"), updated[0])
	case 2, 3:
		quoted := strutil.Quoted(updated)
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		msg = fmt.Sprintf(i18n.G("Monitoring snaps %s to determine whether extra refresh steps are required"), quoted)
	default:
		msg = fmt.Sprintf(i18n.G("Monitoring %d snaps to determine whether extra refresh steps are required"), len(updated))
	}
	return msg
}

func applyAutoAliasesDelta(st *state.State, delta map[string][]string, op string, refreshAll bool, fromChange string, linkTs func(instanceName string, ts *state.TaskSet)) (*state.TaskSet, error) {
	applyTs := state.NewTaskSet()
	kind := "refresh-aliases"
	msg := i18n.G("Refresh aliases for snap %q")
	if op == "prune" {
		kind = "prune-auto-aliases"
		msg = i18n.G("Prune automatic aliases for snap %q")
	}
	for instanceName, aliases := range delta {
		if err := checkChangeConflictIgnoringOneChange(st, instanceName, nil, fromChange); err != nil {
			if refreshAll {
				// doing "refresh all", just skip this snap
				logger.Noticef("cannot %s automatic aliases for snap %q: %v", op, instanceName, err)
				continue
			}
			return nil, err
		}

		snapName, instanceKey := snap.SplitInstanceName(instanceName)
		snapsup := &SnapSetup{
			SideInfo:    &snap.SideInfo{RealName: snapName},
			InstanceKey: instanceKey,
		}
		alias := st.NewTask(kind, fmt.Sprintf(msg, snapsup.InstanceName()))
		alias.Set("snap-setup", &snapsup)
		if op == "prune" {
			alias.Set("aliases", aliases)
		}
		ts := state.NewTaskSet(alias)
		linkTs(instanceName, ts)
		applyTs.AddAll(ts)
	}
	return applyTs, nil
}

func autoAliasesUpdate(st *state.State, requested []string, updates []update) (changed map[string][]string, mustPrune map[string][]string, transferTargets map[string]bool, err error) {
	changed, dropped, err := autoAliasesDelta(st, nil)
	if err != nil {
		if len(requested) != 0 {
			// not "refresh all", error
			return nil, nil, nil, err
		}
		// log and continue
		logger.Noticef("cannot find the delta for automatic aliases for some snaps: %v", err)
	}

	refreshAll := len(requested) == 0

	// dropped alias -> snapName
	droppedAliases := make(map[string][]string, len(dropped))
	for instanceName, aliases := range dropped {
		for _, alias := range aliases {
			droppedAliases[alias] = append(droppedAliases[alias], instanceName)
		}
	}

	// filter changed considering only names if set:
	// we add auto-aliases only for mentioned snaps
	if !refreshAll && len(changed) != 0 {
		filteredChanged := make(map[string][]string, len(changed))
		for _, name := range requested {
			if changed[name] != nil {
				filteredChanged[name] = changed[name]
			}
		}
		changed = filteredChanged
	}

	// mark snaps that are sources or target of transfers
	transferSources := make(map[string]bool, len(dropped))
	transferTargets = make(map[string]bool, len(changed))
	for instanceName, aliases := range changed {
		for _, alias := range aliases {
			if sources := droppedAliases[alias]; len(sources) != 0 {
				transferTargets[instanceName] = true
				for _, source := range sources {
					transferSources[source] = true
				}
			}
		}
	}

	// snaps with updates
	updating := make(map[string]bool, len(updates))
	for _, up := range updates {
		ok, err := up.revisionSatisfied()
		if err != nil {
			return nil, nil, nil, err
		}

		updating[up.Setup.InstanceName()] = !ok
	}

	// add explicitly auto-aliases only for snaps that are not updated
	for instanceName := range changed {
		if updating[instanceName] {
			delete(changed, instanceName)
		}
	}

	// prune explicitly auto-aliases only for snaps that are mentioned
	// and not updated OR the source of transfers
	mustPrune = make(map[string][]string, len(dropped))
	for instanceName := range transferSources {
		mustPrune[instanceName] = dropped[instanceName]
	}
	if refreshAll {
		for instanceName, aliases := range dropped {
			if !updating[instanceName] {
				mustPrune[instanceName] = aliases
			}
		}
	} else {
		for _, name := range requested {
			if !updating[name] && dropped[name] != nil {
				mustPrune[name] = dropped[name]
			}
		}
	}

	return changed, mustPrune, transferTargets, nil
}

// resolveChannel returns the effective channel to use, based on the requested
// channel and constrains set by device model, or an error if switching to
// requested channel is forbidden.
func resolveChannel(snapName, oldChannel, newChannel string, deviceCtx DeviceContext) (effectiveChannel string, err error) {
	if newChannel == "" {
		return oldChannel, nil
	}

	// ensure we do not switch away from the kernel-track in the model
	model := deviceCtx.Model()

	var pinnedTrack, which string
	if snapName == model.Kernel() && model.KernelTrack() != "" {
		pinnedTrack, which = model.KernelTrack(), "kernel"
	}
	if snapName == model.Gadget() && model.GadgetTrack() != "" {
		pinnedTrack, which = model.GadgetTrack(), "gadget"
	}

	if pinnedTrack == "" {
		// no pinned track
		return channel.Resolve(oldChannel, newChannel)
	}

	// channel name is valid and consist of risk level or
	// risk/branch only, do the right thing and default to risk (or
	// risk/branch) within the pinned track
	resChannel, err := channel.ResolvePinned(pinnedTrack, newChannel)
	if err == channel.ErrPinnedTrackSwitch {
		// switching to a different track is not allowed
		return "", fmt.Errorf("cannot switch from %s track %q as specified for the (device) model to %q", which, pinnedTrack, newChannel)

	}
	if err != nil {
		return "", err
	}
	return resChannel, nil
}

var errRevisionSwitch = errors.New("cannot switch revision")

func switchSummary(snap, chanFrom, chanTo, cohFrom, cohTo string) string {
	if cohFrom != cohTo {
		if cohTo == "" {
			// leave cohort
			if chanFrom == chanTo {
				return fmt.Sprintf(i18n.G("Switch snap %q away from cohort %q"),
					snap, strutil.ElliptLeft(cohFrom, 10))
			}
			if chanFrom == "" {
				return fmt.Sprintf(i18n.G("Switch snap %q to channel %q and away from cohort %q"),
					snap, chanTo, strutil.ElliptLeft(cohFrom, 10),
				)
			}
			return fmt.Sprintf(i18n.G("Switch snap %q from channel %q to %q and away from cohort %q"),
				snap, chanFrom, chanTo, strutil.ElliptLeft(cohFrom, 10),
			)
		}
		if cohFrom == "" {
			// moving into a cohort
			if chanFrom == chanTo {
				return fmt.Sprintf(i18n.G("Switch snap %q from no cohort to %q"),
					snap, strutil.ElliptLeft(cohTo, 10))
			}
			if chanFrom == "" {
				return fmt.Sprintf(i18n.G("Switch snap %q to channel %q and from no cohort to %q"),
					snap, chanTo, strutil.ElliptLeft(cohTo, 10),
				)
			}
			// chanTo == "" is not interesting
			return fmt.Sprintf(i18n.G("Switch snap %q from channel %q to %q and from no cohort to %q"),
				snap, chanFrom, chanTo, strutil.ElliptLeft(cohTo, 10),
			)
		}
		if chanFrom == chanTo {
			return fmt.Sprintf(i18n.G("Switch snap %q from cohort %q to %q"),
				snap, strutil.ElliptLeft(cohFrom, 10), strutil.ElliptLeft(cohTo, 10))
		}
		if chanFrom == "" {
			return fmt.Sprintf(i18n.G("Switch snap %q to channel %q and from cohort %q to %q"),
				snap, chanTo, strutil.ElliptLeft(cohFrom, 10), strutil.ElliptLeft(cohTo, 10),
			)
		}
		return fmt.Sprintf(i18n.G("Switch snap %q from channel %q to %q and from cohort %q to %q"),
			snap, chanFrom, chanTo,
			strutil.ElliptLeft(cohFrom, 10), strutil.ElliptLeft(cohTo, 10),
		)
	}

	if chanFrom == "" {
		return fmt.Sprintf(i18n.G("Switch snap %q to channel %q"),
			snap, chanTo)
	}
	if chanFrom != chanTo {
		return fmt.Sprintf(i18n.G("Switch snap %q from channel %q to %q"),
			snap, chanFrom, chanTo)
	}
	// a no-change switch is accepted for idempotency
	return "No change switch (no-op)"
}

// Switch switches a snap to a new channel and/or cohort
func Switch(st *state.State, name string, opts *RevisionOptions, prqt PrereqTracker) (*state.TaskSet, error) {
	if opts == nil {
		opts = &RevisionOptions{}
	}
	if !opts.Revision.Unset() {
		return nil, errRevisionSwitch
	}

	if prqt == nil {
		prqt = snap.SimplePrereqTracker{}
	}

	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if !snapst.IsInstalled() {
		return nil, &snap.NotInstalledError{Snap: name}
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, err
	}

	deviceCtx, err := DeviceCtxFromState(st, nil)
	if err != nil {
		return nil, err
	}

	channel, err := resolveChannel(name, snapst.TrackingChannel, opts.Channel, deviceCtx)
	if err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo:    snapst.CurrentSideInfo(),
		InstanceKey: snapst.InstanceKey,
		// set the from state (i.e. no change), they are overridden from opts as needed below
		CohortKey: snapst.CohortKey,
		Channel:   channel,
		Type:      snap.Type(snapst.SnapType),
	}

	if opts.CohortKey != "" {
		snapsup.CohortKey = opts.CohortKey
	}
	if opts.LeaveCohort {
		snapsup.CohortKey = ""
	}

	current, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	// note, the prereq tracker doesn't use the channel given here. however, for
	// the sake of correctness, and if we ever do use the channel in the prereq
	// tracker, we update the channel to be what it is being switched to.
	current.SideInfo.Channel = snapsup.Channel
	prqt.Add(current)

	summary := switchSummary(snapsup.InstanceName(), snapst.TrackingChannel, snapsup.Channel, snapst.CohortKey, snapsup.CohortKey)
	switchSnap := st.NewTask("switch-snap", summary)
	switchSnap.Set("snap-setup", &snapsup)

	ts := state.NewTaskSet(switchSnap)
	ts.MarkEdge(switchSnap, SnapSetupEdge)

	return ts, nil
}

// RevisionOptions control the selection of a snap revision.
type RevisionOptions struct {
	Channel        string
	Revision       snap.Revision
	ValidationSets *snapasserts.ValidationSets
	CohortKey      string
	LeaveCohort    bool
}

func (r *RevisionOptions) setChannelIfUnset(channel string) {
	if r.Channel == "" {
		r.Channel = channel
	}
}

// resolveChannel conditionally resolves the channel for the given snap. If the
// the revision is set and the channel is empty, then we assume that the caller
// wants to install by revision and do not mutate the channel.
func (r *RevisionOptions) resolveChannel(instanceName string, fallback string, deviceCtx DeviceContext) error {
	// if the revision is set and the caller didn't provide a channel, then we
	// shouldn't mess with the channel. this is because we don't want the caller
	// to have to pick the right channel when refreshing/installing by revision.
	if !r.Revision.Unset() && r.Channel == "" {
		return nil
	}

	// otherwise, we know that the channel is either empty, or it is specified
	// along with the revision. in either case, we need to resolve the channel.

	resolved, err := resolveChannel(instanceName, fallback, r.Channel, deviceCtx)
	if err != nil {
		return err
	}
	r.Channel = resolved

	return nil
}

// initializeValidationSets ensures that r.ValidationSets is initialized with a
// value. If the caller has provided a value, it is used. If validation sets are
// explicitly ignored, we create a new empty validation set that has no rules.
// Otherwise, we use the enforced validation sets.
func (r *RevisionOptions) initializeValidationSets(vsets cachedValidationSets, opts Options) error {
	if r.ValidationSets != nil {
		return nil
	}

	if opts.Flags.IgnoreValidation {
		r.ValidationSets = snapasserts.NewValidationSets()
	} else {
		enforced, err := vsets()
		if err != nil {
			return err
		}
		r.ValidationSets = enforced
	}
	return nil
}

// Update initiates a change updating a snap.
// Note that the state must be locked by the caller.
//
// The returned TaskSet can contain a LastBeforeLocalModificationsEdge
// identifying the last task before the first task that introduces system
// modifications. If no such edge is set, then none of the tasks introduce
// system modifications.
func Update(st *state.State, name string, opts *RevisionOptions, userID int, flags Flags) (*state.TaskSet, error) {
	return UpdateWithDeviceContext(st, name, opts, userID, flags, nil, nil, "")
}

// UpdateWithDeviceContext initiates a change updating a snap.
// It will query the store for the snap with the given deviceCtx.
// Note that the state must be locked by the caller.
//
// The returned TaskSet can contain a LastBeforeLocalModificationsEdge
// identifying the last task before the first task that introduces system
// modifications. If no such edge is set, then none of the tasks introduce
// system modifications.
func UpdateWithDeviceContext(st *state.State, name string, opts *RevisionOptions, userID int, flags Flags, prqt PrereqTracker, deviceCtx DeviceContext, fromChange string) (*state.TaskSet, error) {
	if opts == nil {
		opts = &RevisionOptions{}
	}

	// this is to maintain backwards compatibility with the old behavior
	if flags.Transaction == "" {
		flags.Transaction = client.TransactionPerSnap
	}

	goal := StoreUpdateGoal(StoreUpdate{
		InstanceName: name,
		RevOpts:      *opts,
	})

	return UpdateOne(context.Background(), st, goal, nil, Options{
		Flags:         flags,
		UserID:        userID,
		DeviceCtx:     deviceCtx,
		FromChange:    fromChange,
		PrereqTracker: prqt,
	})
}

// UpdatePathWithDeviceContext initiates a change updating a snap from a local file.
// Note that the state must be locked by the caller.
//
// The returned TaskSet can contain a LastBeforeLocalModificationsEdge
// identifying the last task before the first task that introduces system
// modifications. If no such edge is set, then none of the tasks introduce
// system modifications.
func UpdatePathWithDeviceContext(st *state.State, si *snap.SideInfo, path, name string, opts *RevisionOptions, userID int, flags Flags, prqt PrereqTracker, deviceCtx DeviceContext, fromChange string) (*state.TaskSet, error) {
	if opts == nil {
		opts = &RevisionOptions{}
	}

	// this is to maintain backwards compatibility with the old behavior
	if flags.Transaction == "" {
		flags.Transaction = client.TransactionPerSnap
	}

	goal := PathUpdateGoal(PathSnap{
		Path:         path,
		SideInfo:     si,
		InstanceName: name,
		RevOpts:      *opts,
	})
	return UpdateOne(context.Background(), st, goal, nil, Options{
		Flags:         flags,
		UserID:        userID,
		DeviceCtx:     deviceCtx,
		PrereqTracker: prqt,
		FromChange:    fromChange,
	})
}

// AutoRefreshAssertions allows to hook fetching of important assertions
// into the Autorefresh function.
var AutoRefreshAssertions func(st *state.State, userID int) error

var AddCurrentTrackingToValidationSetsStack func(st *state.State) error

var RestoreValidationSetsTracking func(st *state.State) error

// AutoRefreshOptions are the options that can be passed to AutoRefresh
type AutoRefreshOptions struct {
	IsContinuedAutoRefresh bool
}

// AutoRefresh is the wrapper that will do a refresh of all the installed
// snaps on the system. In addition to that it will also refresh important
// assertions.
func AutoRefresh(ctx context.Context, st *state.State) ([]string, *UpdateTaskSets, error) {
	userID := 0

	if AutoRefreshAssertions != nil {
		// TODO: do something else if features.GateAutoRefreshHook is active
		// since some snaps may be held and not refreshed.
		if err := AutoRefreshAssertions(st, userID); err != nil {
			return nil, nil, err
		}
	}

	tr := config.NewTransaction(st)
	gateAutoRefreshHook, err := features.Flag(tr, features.GateAutoRefreshHook)
	if err != nil && !config.IsNoOption(err) {
		return nil, nil, err
	}
	if !gateAutoRefreshHook {
		// old-style refresh (gate-auto-refresh-hook feature disabled)
		return updateManyFiltered(ctx, st, nil, nil, userID, nil, &Flags{IsAutoRefresh: true}, "")
	}

	// TODO: rename to autoRefreshTasks when old auto refresh logic gets removed.
	// TODO2: pass "IsContinuedAutoRefresh" so that the SnapSetup of
	//        gate-auto-refresh contains this field (required so that
	//        the update-finished notifications work)
	updated, tss, err := autoRefreshPhase1(ctx, st, "")
	if err != nil {
		return nil, nil, err
	}

	return updated, &UpdateTaskSets{Refresh: tss}, nil
}

// autoRefreshPhase1 creates gate-auto-refresh hooks and conditional-auto-refresh
// task that initiates actual refresh. forGatingSnap is optional and limits auto-refresh
// to the snaps affecting the given snap only; it defaults to all snaps if nil.
// The state needs to be locked by the caller.
func autoRefreshPhase1(ctx context.Context, st *state.State, forGatingSnap string) ([]string, []*state.TaskSet, error) {
	user, err := userFromUserID(st, 0)
	if err != nil {
		return nil, nil, err
	}

	allSnaps, err := All(st)
	if err != nil {
		return nil, nil, err
	}

	refreshOpts := &store.RefreshOptions{Scheduled: true}
	// XXX: should we skip refreshCandidates if forGatingSnap isn't empty (meaning we're handling proceed from a snap)?
	plan, err := storeUpdatePlan(ctx, st, allSnaps, nil, user, refreshOpts, Options{})
	if err != nil {
		// XXX: should we reset "refresh-candidates" to nil in state for some types
		// of errors?
		return nil, nil, err
	}
	deviceCtx, err := DeviceCtxFromState(st, nil)
	if err != nil {
		return nil, nil, err
	}

	hints, err := refreshHintsFromUpdatePlan(st, plan, deviceCtx)
	if err != nil {
		return nil, nil, err
	}
	updateRefreshCandidates(st, hints, nil)

	// prune affecting snaps that are not in refresh candidates from hold state.
	if err := pruneGating(st, hints); err != nil {
		return nil, nil, err
	}

	updates := make([]string, 0, len(hints))

	// check conflicts
	fromChange := ""
	for _, t := range plan.targets {
		name := t.info.InstanceName()
		if _, ok := hints[name]; !ok {
			// filtered out by refreshHintsFromCandidates
			continue
		}

		if err := checkChangeConflictIgnoringOneChange(st, name, &t.snapst, fromChange); err != nil {
			logger.Noticef("cannot refresh snap %q: %v", name, err)
		} else {
			updates = append(updates, name)
		}
	}

	if forGatingSnap != "" {
		var gatingSnapHasUpdate bool
		for _, up := range updates {
			if up == forGatingSnap {
				gatingSnapHasUpdate = true
				break
			}
		}
		if !gatingSnapHasUpdate {
			return nil, nil, nil
		}
	}

	if len(updates) == 0 {
		return nil, nil, nil
	}

	// all snaps in updates are now considered to be operated on and should
	// provoke conflicts until updated or until we know (after running
	// gate-auto-refresh hooks) that some are not going to be updated
	// and can stop conflicting.

	affectedSnaps, err := affectedByRefresh(st, updates)
	if err != nil {
		return nil, nil, err
	}

	// only used if forGatingSnap != ""
	var snapsAffectingGatingSnap map[string]bool

	// if specific gating snap was given, drop other affected snaps unless
	// they are affected by same updates as forGatingSnap.
	if forGatingSnap != "" {
		snapsAffectingGatingSnap = affectedSnaps[forGatingSnap].AffectingSnaps

		// check if there is an intersection between affecting snaps of this
		// forGatingSnap and other gating snaps. If so, we need to run
		// their gate-auto-refresh hooks as well.
		for gatingSnap, affectedInfo := range affectedSnaps {
			if gatingSnap == forGatingSnap {
				continue
			}
			var found bool
			for affectingSnap := range affectedInfo.AffectingSnaps {
				if snapsAffectingGatingSnap[affectingSnap] {
					found = true
					break
				}
			}
			if !found {
				delete(affectedSnaps, gatingSnap)
			}
		}
	}

	var hooks *state.TaskSet
	if len(affectedSnaps) > 0 {
		affected := make([]string, 0, len(affectedSnaps))
		for snapName := range affectedSnaps {
			affected = append(affected, snapName)
		}
		sort.Strings(affected)
		hooks = createGateAutoRefreshHooks(st, affected)
	}

	// gate-auto-refresh hooks, followed by conditional-auto-refresh task waiting
	// for all hooks.
	ar := st.NewTask("conditional-auto-refresh", "Run auto-refresh for ready snaps")
	tss := []*state.TaskSet{state.NewTaskSet(ar)}
	if hooks != nil {
		ar.WaitAll(hooks)
		tss = append(tss, hooks)
	}

	// return all names as potentially getting updated even though some may be
	// held.
	names := make([]string, 0, len(updates))
	toUpdate := make(map[string]*refreshCandidate, len(updates))
	for _, up := range updates {
		// if specific gating snap was requested, filter out updates.
		if forGatingSnap != "" && forGatingSnap != up {
			if !snapsAffectingGatingSnap[up] {
				continue
			}
		}
		names = append(names, up)
		toUpdate[up] = hints[up]
	}

	// store the list of snaps to update on the conditional-auto-refresh task
	// (this may be a subset of refresh-candidates due to conflicts).
	ar.Set("snaps", toUpdate)

	// return all names as potentially getting updated even though some may be
	// held.
	sort.Strings(names)
	return names, tss, nil
}

// autoRefreshPhase2 creates tasks for refreshing snaps from updates.
func autoRefreshPhase2(st *state.State, candidates []*refreshCandidate, flags *Flags, fromChange string) (*UpdateTaskSets, error) {
	if flags == nil {
		flags = &Flags{}
	}

	// this should always be set during an auto-refresh
	flags.IsAutoRefresh = true
	// auto-refresh should always use a lane per snap
	flags.Transaction = client.TransactionPerSnap

	deviceCtx, err := DeviceCtx(st, nil, nil)
	if err != nil {
		return nil, err
	}

	updates := make([]update, 0, len(candidates))
	installInfos := make([]minimalInstallInfo, 0, len(candidates))
	for _, up := range candidates {
		snapsup, snapst, err := up.SnapSetupForUpdate(st, flags)
		if err != nil {
			logger.Noticef("cannot update %q: %v", up.InstanceName(), err)
			continue
		}

		updates = append(updates, update{
			Setup:      *snapsup,
			SnapState:  *snapst,
			Components: up.Components,
		})
		installInfos = append(installInfos, up)
	}

	if err := checkDiskSpace(st, "refresh", installInfos, 0, nil); err != nil {
		return nil, err
	}

	const userID = 0
	_, updateTss, err := doPotentiallySplitUpdate(st, nil, updates, Options{
		Flags:      *flags,
		UserID:     userID,
		FromChange: fromChange,
		DeviceCtx:  deviceCtx,
	})
	if err != nil {
		return nil, err
	}

	return updateTss, nil
}

func checkDiskSpaceDownload(infos []minimalInstallInfo, rootDir string) error {
	var totalSize uint64
	for _, info := range infos {
		totalSize += uint64(info.DownloadSize())
	}

	return checkForAvailableSpace(totalSize, infos, "download", rootDir)
}

// checkDiskSpace checks if there is enough space for the requested snaps and their prerequisites
func checkDiskSpace(st *state.State, changeKind string, infos []minimalInstallInfo, userID int, prqt PrereqTracker) error {
	var featFlag features.SnapdFeature

	switch changeKind {
	case "install":
		featFlag = features.CheckDiskSpaceInstall
	case "refresh":
		featFlag = features.CheckDiskSpaceRefresh
	default:
		return fmt.Errorf("cannot check disk space for invalid change kind %q", changeKind)
	}

	tr := config.NewTransaction(st)
	enabled, err := features.Flag(tr, featFlag)
	if err != nil && !config.IsNoOption(err) {
		return err
	}

	if !enabled {
		return nil
	}

	totalSize, err := installSize(st, infos, userID, prqt)
	if err != nil {
		return err
	}

	return checkForAvailableSpace(totalSize, infos, changeKind, dirs.SnapdStateDir(dirs.GlobalRootDir))
}

func checkForAvailableSpace(totalSize uint64, infos []minimalInstallInfo, changeKind string, rootDir string) error {
	requiredSpace := safetyMarginDiskSpace(totalSize)
	if err := osutilCheckFreeSpace(rootDir, requiredSpace); err != nil {
		snaps := make([]string, len(infos))
		for i, up := range infos {
			snaps[i] = up.InstanceName()
		}
		if _, ok := err.(*osutil.NotEnoughDiskSpaceError); ok {
			return &InsufficientSpaceError{
				Path:       rootDir,
				Snaps:      snaps,
				ChangeKind: changeKind,
			}
		}
		return err
	}
	return nil
}

// MigrateHome migrates a set of snaps to use a ~/Snap sub-directory as HOME.
// The state must be locked by the caller.
func MigrateHome(st *state.State, snaps []string) ([]*state.TaskSet, error) {
	tr := config.NewTransaction(st)
	moveDir, err := features.Flag(tr, features.MoveSnapHomeDir)
	if err != nil {
		return nil, err
	}

	if !moveDir {
		_, confName := features.MoveSnapHomeDir.ConfigOption()
		return nil, fmt.Errorf("cannot migrate to ~/Snap: flag %q is not set", confName)
	}

	allSnaps, err := All(st)
	if err != nil {
		return nil, err
	}

	for _, name := range snaps {
		if snapst, ok := allSnaps[name]; !ok {
			return nil, snap.NotInstalledError{Snap: name}
		} else if snapst.MigratedToExposedHome {
			return nil, fmt.Errorf("cannot migrate %q to ~/Snap: already migrated", name)
		}
	}

	var tss []*state.TaskSet
	for _, name := range snaps {
		si := allSnaps[name].CurrentSideInfo()
		snapsup := &SnapSetup{
			SideInfo: si,
		}

		var tasks []*state.Task
		prepare := st.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q (%s)"), name, si.Revision))
		prepare.Set("snap-setup", snapsup)
		tasks = append(tasks, prepare)

		prev := prepare
		addTask := func(t *state.Task) {
			t.Set("snap-setup-task", prepare.ID())
			t.WaitFor(prev)
			tasks = append(tasks, t)
		}

		stop := st.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap %q services"), name))
		stop.Set("stop-reason", "home-migration")
		addTask(stop)
		prev = stop

		unlink := st.NewTask("unlink-current-snap", fmt.Sprintf(i18n.G("Make current revision for snap %q unavailable"), name))
		unlink.Set("unlink-reason", unlinkReasonHomeMigration)
		addTask(unlink)
		prev = unlink

		migrate := st.NewTask("migrate-snap-home", fmt.Sprintf(i18n.G("Migrate %q to ~/Snap"), name))
		addTask(migrate)
		prev = migrate

		// finalize (wrappers+current symlink)
		linkSnap := st.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) available to the system"), name, si.Revision))
		addTask(linkSnap)
		prev = linkSnap

		// run new services
		startSnapServices := st.NewTask("start-snap-services", fmt.Sprintf(i18n.G("Start snap %q (%s) services"), name, si.Revision))
		addTask(startSnapServices)
		prev = startSnapServices

		var ts state.TaskSet
		for _, t := range tasks {
			ts.AddTask(t)
		}

		ts.JoinLane(st.NewLane())
		tss = append(tss, &ts)
	}

	return tss, nil
}

// LinkNewBaseOrKernel creates a new task set that enables swapping to a base or
// kernel snap that is already installed on the system. The primary use case for
// this function is remodeling.
//
// For bases, we create prepare-snap and link-snap tasks. Technically this would
// create link-component tasks for any installed components, but bases do not
// currently use components.
//
// For kernels, we create prepare-snap, an update-gadget-assets task (if
// needed), link-snap, and link-component tasks for any installed components.
//
// Note that this function previously created a prepare-kernel-snap task, but
// this was not needed. Since this function is only used if the snap is
// installed already installed, then it is expected that the drivers tree is
// present. Thus, the prepare-kernel-snap task would be redundant.
func LinkNewBaseOrKernel(st *state.State, name string, fromChange string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if errors.Is(err, state.ErrNoState) {
		return nil, &snap.NotInstalledError{Snap: name}
	}
	if err != nil {
		return nil, err
	}

	if err := checkChangeConflictIgnoringOneChange(st, name, nil, fromChange); err != nil {
		return nil, err
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	switch info.Type() {
	case snap.TypeOS, snap.TypeBase, snap.TypeKernel:
		// good
	default:
		// bad
		return nil, fmt.Errorf("internal error: cannot link type %v", info.Type())
	}

	snapsup := &SnapSetup{
		SideInfo:    snapst.CurrentSideInfo(),
		Flags:       snapst.Flags.ForSnapSetup(),
		Type:        info.Type(),
		Version:     info.Version,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}

	// note that prepare-snap doesn't actually do anything here, and is mostly
	// used as a task to carry the snap-setup information.
	prepareSnap := st.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q (%s) for remodel"), snapsup.InstanceName(), snapst.Current))
	prepareSnap.Set("snap-setup", &snapsup)

	ts := state.NewTaskSet(prepareSnap)
	ts.MarkEdge(prepareSnap, LastBeforeLocalModificationsEdge)
	ts.MarkEdge(prepareSnap, SnapSetupEdge)

	if err := addLinkNewBaseOrKernelTasks(st, snapst, ts, prepareSnap); err != nil {
		return nil, err
	}

	return ts, nil
}

func addLinkNewBaseOrKernelTasks(st *state.State, snapst SnapState, ts *state.TaskSet, snapsupTask *state.Task) error {
	tasks := ts.Tasks()
	if len(tasks) == 0 {
		return errors.New("internal error: task set must be seeded with at least one task")
	}

	prev := tasks[len(tasks)-1]
	add := func(t *state.Task) {
		t.Set("snap-setup-task", snapsupTask.ID())
		t.WaitFor(prev)
		ts.AddTask(t)
		prev = t
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	// preserve the same order as during the update
	if info.Type() == snap.TypeKernel {
		// this previously created a prepare-kernel-snap task. however, this
		// isn't needed since we're only using this function to swap to already
		// installed kernels. thus, the drivers tree should be present.

		// kernel snaps can carry boot assets
		gadgetUpdate := st.NewTask("update-gadget-assets", fmt.Sprintf(i18n.G("Update assets from %s %q (%s) for remodel"), info.Type(), info.InstanceName(), snapst.Current))
		add(gadgetUpdate)
	}

	linkSnap := st.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) available to the system during remodel"), info.InstanceName(), snapst.Current))
	add(linkSnap)
	ts.MarkEdge(linkSnap, MaybeRebootEdge)

	components := snapst.Sequence.ComponentsForRevision(snapst.Current)
	compsupTasks := make([]string, 0, len(components))
	for _, cs := range components {
		compsup := ComponentSetup{
			CompSideInfo: cs.SideInfo,
			CompType:     cs.CompType,
		}

		cref := compsup.CompSideInfo.Component
		compRev := compsup.CompSideInfo.Revision

		link := st.NewTask("link-component", fmt.Sprintf(i18n.G("Make component %q (%s) available to the system during remodel"), cref, compRev))
		link.Set("component-setup", compsup)
		add(link)

		compsupTasks = append(compsupTasks, link.ID())
	}

	snapsupTask.Set("component-setup-tasks", compsupTasks)

	return nil
}

func findSnapSetupTask(tasks []*state.Task) (*state.Task, *SnapSetup, error) {
	var snapsup SnapSetup
	for _, tsk := range tasks {
		if tsk.Has("snap-setup") {
			if err := tsk.Get("snap-setup", &snapsup); err != nil {
				return nil, nil, err
			}
			return tsk, &snapsup, nil
		}
	}
	return nil, nil, nil
}

// AddLinkNewBaseOrKernel appends tasks to a given task set. This enables
// swapping to a base or kernel snap that is already installed on the system.
// The primary use case for this function is remodeling.
//
// It is expected that the given task set contains a snap setup task.
// Additionally, it should not perform any modifications to the local system.
//
// For bases, we create a link-snap task. Technically this would create
// link-component tasks for any installed components, but bases do not currently
// use components.
//
// For kernels, we create an update-gadget-assets task (if needed), link-snap,
// and link-component tasks for any installed components.
//
// Note that this function previously created a prepare-kernel-snap task, but
// this was not needed. Since this function is only used if the snap is
// installed already installed, then it is expected that the drivers tree is
// present. Thus, the prepare-kernel-snap task would be redundant.
func AddLinkNewBaseOrKernel(st *state.State, ts *state.TaskSet) (*state.TaskSet, error) {
	if ts.MaybeEdge(LastBeforeLocalModificationsEdge) != nil {
		return nil, errors.New("internal error: cannot add tasks to link new base or kernel to task set that introduces local modifications")
	}

	allTasks := ts.Tasks()
	snapSetupTask, snapsup, err := findSnapSetupTask(allTasks)
	if err != nil {
		return nil, err
	}
	if snapSetupTask == nil {
		return nil, errors.New("internal error: cannot identify task with snap-setup")
	}

	var snapst SnapState
	if err := Get(st, snapsup.InstanceName(), &snapst); err != nil {
		return nil, err
	}

	if snapst.Current != snapsup.Revision() {
		return nil, errors.New("internal error: cannot add tasks to link new base or kernel to task set that changes the snap revision")
	}

	// no task in the task set is marked as last before system modifications are
	// introduced, so we need to mark the last task in the original set, as
	// tasks introduced here modify system state
	ts.MarkEdge(allTasks[len(allTasks)-1], LastBeforeLocalModificationsEdge)

	if err := addLinkNewBaseOrKernelTasks(st, snapst, ts, snapSetupTask); err != nil {
		return nil, err
	}

	return ts, nil
}

// SwitchToNewGadget creates a new task set with
// prepare/update-gadget-assets/update-gadget-cmdline tasks for the gadget snap,
// for remodel.
func SwitchToNewGadget(st *state.State, name string, fromChange string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if errors.Is(err, state.ErrNoState) {
		return nil, &snap.NotInstalledError{Snap: name}
	}
	if err != nil {
		return nil, err
	}

	if err := checkChangeConflictIgnoringOneChange(st, name, nil, fromChange); err != nil {
		return nil, err
	}

	// make sure no other active changes are changing the kernel command line
	if err := CheckUpdateKernelCommandLineConflict(st, fromChange); err != nil {
		return nil, err
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	if info.Type() != snap.TypeGadget {
		return nil, fmt.Errorf("internal error: cannot link type %v", info.Type())
	}

	snapsup := &SnapSetup{
		SideInfo:    snapst.CurrentSideInfo(),
		Flags:       snapst.Flags.ForSnapSetup(),
		Type:        info.Type(),
		Version:     info.Version,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}

	prepareSnap := st.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q (%s) for remodel"), snapsup.InstanceName(), snapst.Current))
	prepareSnap.Set("snap-setup", &snapsup)

	gadgetUpdate := st.NewTask("update-gadget-assets", fmt.Sprintf(i18n.G("Update assets from %s %q (%s) for remodel"), snapsup.Type, snapsup.InstanceName(), snapst.Current))
	gadgetUpdate.WaitFor(prepareSnap)
	gadgetUpdate.Set("snap-setup-task", prepareSnap.ID())
	gadgetCmdline := st.NewTask("update-gadget-cmdline", fmt.Sprintf(i18n.G("Update kernel command line from %s %q (%s) for remodel"), snapsup.Type, snapsup.InstanceName(), snapst.Current))
	gadgetCmdline.WaitFor(gadgetUpdate)
	gadgetCmdline.Set("snap-setup-task", prepareSnap.ID())

	ts := state.NewTaskSet(prepareSnap, gadgetUpdate, gadgetCmdline)
	// prepare-snap is the last task that carries no system modifications
	ts.MarkEdge(prepareSnap, LastBeforeLocalModificationsEdge)
	ts.MarkEdge(prepareSnap, SnapSetupEdge)
	return ts, nil
}

// AddGadgetAssetsTasks creates the same tasks as SwitchToNewGadget but adds
// them to the provided task set.
func AddGadgetAssetsTasks(st *state.State, ts *state.TaskSet) (*state.TaskSet, error) {
	allTasks := ts.Tasks()
	snapSetupTask, snapsup, err := findSnapSetupTask(allTasks)
	if err != nil {
		return nil, err
	}
	if snapSetupTask == nil {
		return nil, fmt.Errorf("internal error: cannot identify task with snap-setup")
	}

	gadgetUpdate := st.NewTask("update-gadget-assets", fmt.Sprintf(i18n.G("Update assets from %s %q (%s) for remodel"), snapsup.Type, snapsup.InstanceName(), snapsup.Revision()))
	gadgetUpdate.Set("snap-setup-task", snapSetupTask.ID())
	// wait for the last task in existing set
	gadgetUpdate.WaitFor(allTasks[len(allTasks)-1])
	ts.AddTask(gadgetUpdate)
	// gadget snaps can carry kernel command line fragments
	gadgetCmdline := st.NewTask("update-gadget-cmdline", fmt.Sprintf(i18n.G("Update kernel command line from %s %q (%s) for remodel"), snapsup.Type, snapsup.InstanceName(), snapsup.Revision()))
	gadgetCmdline.Set("snap-setup-task", snapSetupTask.ID())
	gadgetCmdline.WaitFor(gadgetUpdate)
	ts.AddTask(gadgetCmdline)
	// make sure that remodel can identify which tasks introduce actual
	// changes to the system and order them correctly
	if edgeTask := ts.MaybeEdge(LastBeforeLocalModificationsEdge); edgeTask == nil {
		// no task in the task set is marked as last before system
		// modifications are introduced, so we need to mark the last
		// task in the set, as tasks introduced here modify system state
		ts.MarkEdge(allTasks[len(allTasks)-1], LastBeforeLocalModificationsEdge)
	}
	return ts, nil
}

// Enable sets a snap to the active state
func Enable(st *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if errors.Is(err, state.ErrNoState) {
		return nil, &snap.NotInstalledError{Snap: name}
	}
	if err != nil {
		return nil, err
	}

	if snapst.Active {
		return nil, fmt.Errorf("snap %q already enabled", name)
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, err
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo:    snapst.CurrentSideInfo(),
		Flags:       snapst.Flags.ForSnapSetup(),
		Type:        info.Type(),
		Version:     info.Version,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}

	prepareSnap := st.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q (%s)"), snapsup.InstanceName(), snapst.Current))
	prepareSnap.Set("snap-setup", &snapsup)

	setupProfiles := st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup snap %q (%s) security profiles"), snapsup.InstanceName(), snapst.Current))
	setupProfiles.Set("snap-setup-task", prepareSnap.ID())
	setupProfiles.WaitFor(prepareSnap)

	linkSnap := st.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) available to the system"), snapsup.InstanceName(), snapst.Current))
	linkSnap.Set("snap-setup-task", prepareSnap.ID())
	linkSnap.WaitFor(setupProfiles)

	// setup aliases
	setupAliases := st.NewTask("setup-aliases", fmt.Sprintf(i18n.G("Setup snap %q aliases"), snapsup.InstanceName()))
	setupAliases.Set("snap-setup-task", prepareSnap.ID())
	setupAliases.WaitFor(linkSnap)

	startSnapServices := st.NewTask("start-snap-services", fmt.Sprintf(i18n.G("Start snap %q (%s) services"), snapsup.InstanceName(), snapst.Current))
	startSnapServices.Set("snap-setup-task", prepareSnap.ID())
	startSnapServices.WaitFor(setupAliases)

	return state.NewTaskSet(prepareSnap, setupProfiles, linkSnap, setupAliases, startSnapServices), nil
}

// Disable sets a snap to the inactive state
func Disable(st *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if errors.Is(err, state.ErrNoState) {
		return nil, &snap.NotInstalledError{Snap: name}
	}
	if err != nil {
		return nil, err
	}
	if !snapst.Active {
		return nil, fmt.Errorf("snap %q already disabled", name)
	}

	info, err := Info(st, name, snapst.Current)
	if err != nil {
		return nil, err
	}
	if !canDisable(info) {
		return nil, fmt.Errorf("snap %q cannot be disabled", name)
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snap.InstanceSnap(name),
			Revision: snapst.Current,
		},
		Type:        info.Type(),
		Version:     info.Version,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}

	stopSnapServices := st.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap %q (%s) services"), snapsup.InstanceName(), snapst.Current))
	stopSnapServices.Set("snap-setup", &snapsup)
	stopSnapServices.Set("stop-reason", snap.StopReasonDisable)

	removeAliases := st.NewTask("remove-aliases", fmt.Sprintf(i18n.G("Remove aliases for snap %q"), snapsup.InstanceName()))
	removeAliases.Set("snap-setup-task", stopSnapServices.ID())
	removeAliases.Set("remove-reason", removeAliasesReasonDisable)
	removeAliases.WaitFor(stopSnapServices)

	unlinkSnap := st.NewTask("unlink-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) unavailable to the system"), snapsup.InstanceName(), snapst.Current))
	unlinkSnap.Set("snap-setup-task", stopSnapServices.ID())
	unlinkSnap.WaitFor(removeAliases)

	removeProfiles := st.NewTask("remove-profiles", fmt.Sprintf(i18n.G("Remove security profiles of snap %q"), snapsup.InstanceName()))
	removeProfiles.Set("snap-setup-task", stopSnapServices.ID())
	removeProfiles.WaitFor(unlinkSnap)

	return state.NewTaskSet(stopSnapServices, removeAliases, unlinkSnap, removeProfiles), nil
}

// canDisable verifies that a snap can be deactivated.
func canDisable(si *snap.Info) bool {
	for _, importantSnapType := range []snap.Type{snap.TypeGadget, snap.TypeKernel, snap.TypeOS, snap.TypeBase} {
		if importantSnapType == si.Type() {
			return false
		}
	}

	return true
}

// canRemove verifies that a snap can be removed.
func canRemove(st *state.State, si *snap.Info, snapst *SnapState, removeAll bool, deviceCtx DeviceContext) error {
	rev := snap.Revision{}
	if !removeAll {
		rev = si.Revision
	}

	if err := PolicyFor(si.Type(), deviceCtx.Model()).CanRemove(st, snapst, rev, deviceCtx); err != nil {
		return err
	}

	// check if this snap is required by any validation set in enforcing mode
	enforcedSets, err := EnforcedValidationSets(st)
	if err != nil {
		return err
	}
	if enforcedSets == nil {
		return nil
	}
	pres, err := enforcedSets.Presence(si)
	if err != nil {
		return err
	}
	if pres.Presence != asserts.PresenceRequired {
		return nil
	}
	// removeAll is set if we're removing the snap completely
	if removeAll {
		if pres.Revision.Unset() {
			return fmt.Errorf("snap %q is required by validation sets: %s", si.InstanceName(), pres.Sets.CommaSeparated())
		}
		return fmt.Errorf("snap %q at revision %s is required by validation sets: %s", si.InstanceName(), pres.Revision, pres.Sets.CommaSeparated())
	}

	// rev is set at this point (otherwise we would hit removeAll case)
	if pres.Revision == rev {
		return fmt.Errorf("snap %q at revision %s is required by validation sets: %s", si.InstanceName(), rev, pres.Sets.CommaSeparated())
	} // else - it's ok to remove a revision different than the required
	return nil
}

// RemoveFlags are used to pass additional flags to the Remove operation.
type RemoveFlags struct {
	// Remove the snap without creating snapshot data
	Purge bool
	// Kill running snap apps and services
	Terminate bool
}

// Remove returns a set of tasks for removing snap.
// Note that the state must be locked by the caller.
func Remove(st *state.State, name string, revision snap.Revision, flags *RemoveFlags) (*state.TaskSet, error) {
	if flags == nil {
		flags = &RemoveFlags{}
	}

	if err := checkSnapDirsInNFSMount(st, flags); err != nil {
		return nil, err
	}

	ts, snapshotSize, err := removeTasks(st, name, revision, flags)
	// removeTasks() checks check-disk-space-remove feature flag, so snapshotSize
	// will only be greater than 0 if the feature is enabled.
	if snapshotSize > 0 {
		requiredSpace := safetyMarginDiskSpace(snapshotSize)
		path := dirs.SnapdStateDir(dirs.GlobalRootDir)
		if err := osutilCheckFreeSpace(path, requiredSpace); err != nil {
			if _, ok := err.(*osutil.NotEnoughDiskSpaceError); ok {
				return nil, &InsufficientSpaceError{
					Path:       path,
					Snaps:      []string{name},
					ChangeKind: "remove",
					Message:    fmt.Sprintf("cannot create automatic snapshot when removing last revision of the snap: %v", err)}
			}
			return nil, err
		}
	}
	return ts, err
}

// removeTasks provides the task set to remove snap name after taking a snapshot
// if flags.Purge is not true, it also computes an estimate of the latter size.
func removeTasks(st *state.State, name string, revision snap.Revision, flags *RemoveFlags) (removeTs *state.TaskSet, snapshotSize uint64, err error) {
	var snapst SnapState
	err = Get(st, name, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, 0, err
	}

	if !snapst.IsInstalled() {
		return nil, 0, &snap.NotInstalledError{Snap: name, Rev: snap.R(0)}
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, 0, err
	}

	deviceCtx, err := DeviceCtxFromState(st, nil)
	if err != nil {
		return nil, 0, err
	}

	active := snapst.Active
	var removeAll bool
	if revision.Unset() {
		revision = snapst.Current
		removeAll = true
	} else {
		if active && len(snapst.Sequence.Revisions) > 1 {
			if revision == snapst.Current {
				msg := "cannot remove active revision %s of snap %q"
				if len(snapst.Sequence.Revisions) > 1 {
					msg += " (revert first?)"
				}
				return nil, 0, fmt.Errorf(msg, revision, name)
			}
			active = false
		}

		if !revisionInSequence(&snapst, revision) {
			return nil, 0, &snap.NotInstalledError{Snap: name, Rev: revision}
		}

		removeAll = len(snapst.Sequence.Revisions) == 1
	}

	info, err := Info(st, name, revision)
	if err != nil {
		return nil, 0, err
	}

	// check if this is something that can be removed
	if err := canRemove(st, info, &snapst, removeAll, deviceCtx); err != nil {
		return nil, 0, fmt.Errorf("snap %q is not removable: %v", name, err)
	}

	// main/current SnapSetup
	snapsup := SnapSetup{
		SideInfo: &snap.SideInfo{
			SnapID:   info.SnapID,
			RealName: snap.InstanceSnap(name),
			Revision: revision,
		},
		Type: info.Type(),
		// no version info needed
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}

	// trigger remove

	removeTs = state.NewTaskSet()
	var chain *state.TaskSet

	addNext := func(ts *state.TaskSet) {
		if chain != nil {
			ts.WaitAll(chain)
		}
		removeTs.AddAll(ts)
		chain = ts
	}

	var prev *state.Task
	var stopSnapServices *state.Task
	if active {
		stopSnapServices = st.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap %q services"), name))
		stopSnapServices.Set("snap-setup", snapsup)
		stopSnapServices.Set("stop-reason", snap.StopReasonRemove)
		addNext(state.NewTaskSet(stopSnapServices))
		prev = stopSnapServices
	}

	// only run remove hook if uninstalling the snap completely
	if removeAll {
		for _, comp := range snapst.Sequence.ComponentsForRevision(snapst.Current) {
			removeCompHook := SetupRemoveComponentHook(st, snapsup.InstanceName(), comp.SideInfo.Component.ComponentName)
			addNext(state.NewTaskSet(removeCompHook))
			prev = removeCompHook
		}

		removeHook := SetupRemoveHook(st, snapsup.InstanceName())
		addNext(state.NewTaskSet(removeHook))
		if prev != nil {
			removeHook.WaitFor(prev)
		}
		prev = removeHook

		// run disconnect hooks
		disconnect := st.NewTask("auto-disconnect", fmt.Sprintf(i18n.G("Disconnect interfaces of snap %q"), snapsup.InstanceName()))
		disconnect.Set("snap-setup", snapsup)
		if prev != nil {
			disconnect.WaitFor(prev)
		}
		addNext(state.NewTaskSet(disconnect))
		prev = disconnect
	}

	if flags.Terminate {
		// This check is needed to avoid having the snap stuck in inhibition since
		// "kill-snap-apps" inhibits the snap from running and "discard-snap" only
		// removes the inhibition file when removing last revision.
		if !removeAll {
			return nil, 0, fmt.Errorf("cannot terminate running apps unless all revisions are removed")
		}
		stopSnapApps := st.NewTask("kill-snap-apps", fmt.Sprintf(i18n.G("Kill running snap %q apps"), name))
		stopSnapApps.Set("snap-setup", snapsup)
		stopSnapApps.Set("kill-reason", snap.KillReasonRemove)
		if prev != nil {
			stopSnapApps.WaitFor(prev)
		}
		addNext(state.NewTaskSet(stopSnapApps))
		prev = stopSnapApps
	}

	// 'purge' flag disables automatic snapshot for given remove op
	if !flags.Purge {
		if tp, _ := snapst.Type(); tp == snap.TypeApp && removeAll {
			ts, err := AutomaticSnapshot(st, name)
			if err == nil {
				tr := config.NewTransaction(st)
				checkDiskSpaceRemove, err := features.Flag(tr, features.CheckDiskSpaceRemove)
				if err != nil && !config.IsNoOption(err) {
					return nil, 0, err
				}
				if checkDiskSpaceRemove {
					snapshotSize, err = EstimateSnapshotSize(st, name, nil)
					if err != nil {
						return nil, 0, err
					}
				}
				addNext(ts)
			} else {
				if err != ErrNothingToDo {
					return nil, 0, err
				}
			}
		}
	}

	if active { // unlink
		var tasks []*state.Task

		removeAliases := st.NewTask("remove-aliases", fmt.Sprintf(i18n.G("Remove aliases for snap %q"), name))
		removeAliases.WaitFor(prev) // prev is not needed beyond here
		removeAliases.Set("snap-setup-task", stopSnapServices.ID())
		removeAliases.Set("remove-reason", removeAliasesReasonRemove)

		unlink := st.NewTask("unlink-snap", fmt.Sprintf(i18n.G("Make snap %q unavailable to the system"), name))
		unlink.Set("snap-setup-task", stopSnapServices.ID())
		unlink.WaitFor(removeAliases)

		removeSecurity := st.NewTask("remove-profiles", fmt.Sprintf(i18n.G("Remove security profile for snap %q (%s)"), name, revision))
		removeSecurity.WaitFor(unlink)
		removeSecurity.Set("snap-setup-task", stopSnapServices.ID())

		tasks = append(tasks, removeAliases, unlink, removeSecurity)
		addNext(state.NewTaskSet(tasks...))
	}

	if removeAll {
		si := snapst.Sequence.SideInfos()
		currentIndex := snapst.LastIndex(snapst.Current)
		for i := len(si) - 1; i >= 0; i-- {
			if i != currentIndex {
				si := si[i]
				ts, err := removeInactiveRevision(st, &snapst, name,
					info.SnapID, si.Revision, snapsup.Type)
				if err != nil {
					return nil, 0, err
				}
				addNext(ts)
			}
		}
		// add tasks for removing the current revision last,
		// this is then also when common data will be removed
		if currentIndex >= 0 {
			ts, err := removeInactiveRevision(st, &snapst, name,
				info.SnapID, si[currentIndex].Revision, snapsup.Type)
			if err != nil {
				return nil, 0, err
			}
			addNext(ts)
		}
	} else {
		ts, err := removeInactiveRevision(st, &snapst, name, info.SnapID, revision,
			snapsup.Type)
		if err != nil {
			return nil, 0, err
		}
		addNext(ts)
	}

	return removeTs, snapshotSize, nil
}

func removeInactiveRevision(st *state.State, snapst *SnapState, name, snapID string, revision snap.Revision, typ snap.Type) (*state.TaskSet, error) {
	var tasks []*state.Task

	snapName, instanceKey := snap.SplitInstanceName(name)
	snapsup := SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
			SnapID:   snapID,
			Revision: revision,
		},
		InstanceKey: instanceKey,
		Type:        typ,
		// no version info needed
	}

	clearData := st.NewTask("clear-snap",
		fmt.Sprintf(i18n.G("Remove data for snap %q (%s)"), name, revision))
	clearData.Set("snap-setup", snapsup)
	tasks = append(tasks, clearData)

	// Discard components first
	cinfos, err := snapst.ComponentInfosForRevision(revision)
	if err != nil {
		return nil, err
	}
	discardCompTasks := make([]*state.Task, len(cinfos))
	for i, cinfo := range cinfos {
		compsup := ComponentSetup{
			CompSideInfo: &cinfo.ComponentSideInfo,
			CompType:     cinfo.Type,
			ComponentInstallFlags: ComponentInstallFlags{
				MultiComponentInstall: true,
			},
		}

		unlinkComp := st.NewTask("unlink-component", fmt.Sprintf(i18n.G(
			"Unlink component %q for snap revision %s"),
			compsup.CompSideInfo.Component, revision.String()))
		unlinkComp.Set("snap-setup-task", clearData.ID())
		unlinkComp.Set("component-setup", compsup)
		unlinkComp.WaitFor(clearData)

		discardComp := st.NewTask("discard-component", fmt.Sprintf(i18n.G(
			"Discard revision for component %q"),
			compsup.CompSideInfo.Component))
		discardComp.Set("snap-setup-task", clearData.ID())
		discardComp.Set("component-setup-task", unlinkComp.ID())
		discardComp.WaitFor(unlinkComp)
		discardCompTasks[i] = discardComp

		tasks = append(tasks, unlinkComp, discardComp)
	}

	discardSnap := st.NewTask("discard-snap",
		fmt.Sprintf(i18n.G("Remove snap %q (%s) from the system"), name, revision))
	discardSnap.Set("snap-setup-task", clearData.ID())
	if len(discardCompTasks) > 0 {
		discardSnap.WaitAll(state.NewTaskSet(discardCompTasks...))
	} else {
		discardSnap.WaitFor(clearData)
	}
	tasks = append(tasks, discardSnap)

	return state.NewTaskSet(tasks...), nil
}

func checkSnapDirsInNFSMount(st *state.State, flags *RemoveFlags) error {
	nfsMount, err := osutil.SnapDirsUnderNFSMounts()
	if err != nil {
		logger.Noticef("cannot check if any snap dirs are under remote mouts: %v", err)
		return nil
	}

	if !nfsMount {
		return nil
	}

	if !flags.Purge {
		return fmt.Errorf("cannot snapshot user data directories in NFS mounts: use --purge to skip taking a snapshot")
	}

	st.AddWarning("May not be able to remove user data under NFS mounted snap directory", nil)
	return nil
}

// RemoveMany removes everything from the given list of names.
// Note that the state must be locked by the caller.
func RemoveMany(st *state.State, names []string, flags *RemoveFlags) ([]string, []*state.TaskSet, error) {
	if flags == nil {
		flags = &RemoveFlags{}
	}

	if err := checkSnapDirsInNFSMount(st, flags); err != nil {
		return nil, nil, err
	}

	names = strutil.Deduplicate(names)
	if err := validateSnapNames(names); err != nil {
		return nil, nil, err
	}

	removed := make([]string, 0, len(names))
	tasksets := make([]*state.TaskSet, 0, len(names))

	var totalSnapshotsSize uint64
	path := dirs.SnapdStateDir(dirs.GlobalRootDir)

	for _, name := range names {
		ts, snapshotSize, err := removeTasks(st, name, snap.R(0), flags)
		// FIXME: is this expected behavior?
		if _, ok := err.(*snap.NotInstalledError); ok {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		totalSnapshotsSize += snapshotSize
		removed = append(removed, name)
		ts.JoinLane(st.NewLane())
		tasksets = append(tasksets, ts)
	}

	// removeTasks() checks check-disk-space-remove feature flag, so totalSnapshotsSize
	// will only be greater than 0 if the feature is enabled.
	if totalSnapshotsSize > 0 {
		requiredSpace := safetyMarginDiskSpace(totalSnapshotsSize)
		if err := osutilCheckFreeSpace(path, requiredSpace); err != nil {
			if _, ok := err.(*osutil.NotEnoughDiskSpaceError); ok {
				return nil, nil, &InsufficientSpaceError{
					Path:       path,
					Snaps:      names,
					ChangeKind: "remove",
				}
			}
			return nil, nil, err
		}
	}

	return removed, tasksets, nil
}

func validateSnapNames(names []string) error {
	var invalidNames []string

	for _, name := range names {
		if err := snap.ValidateInstanceName(name); err != nil {
			invalidNames = append(invalidNames, name)
		}
	}

	if len(invalidNames) > 0 {
		return fmt.Errorf("cannot remove invalid snap names: %v", strings.Join(invalidNames, ", "))
	}

	return nil
}

// Revert returns a set of tasks for reverting to the previous version of the snap.
// Note that the state must be locked by the caller.
func Revert(st *state.State, name string, flags Flags, fromChange string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}

	pi := snapst.previousSideInfo()
	if pi == nil {
		return nil, fmt.Errorf("no revision to revert to")
	}

	return RevertToRevision(st, name, pi.Revision, flags, fromChange)
}

func RevertToRevision(st *state.State, name string, rev snap.Revision, flags Flags, fromChange string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}

	if snapst.Current == rev {
		return nil, fmt.Errorf("already on requested revision")
	}

	if !snapst.Active {
		return nil, fmt.Errorf("cannot revert inactive snaps")
	}
	i := snapst.LastIndex(rev)
	if i < 0 {
		return nil, fmt.Errorf("cannot find revision %s for snap %q", rev, name)
	}

	flags.Revert = true
	// TODO: make flags be per revision to avoid this logic (that
	//       leaves corner cases all over the place)
	if !(flags.JailMode || flags.DevMode || flags.Classic) {
		if snapst.Flags.DevMode {
			flags.DevMode = true
		}
		if snapst.Flags.JailMode {
			flags.JailMode = true
		}
		if snapst.Flags.Classic {
			flags.Classic = true
		}
	}

	info, err := Info(st, name, rev)
	if err != nil {
		return nil, err
	}

	snapsup := SnapSetup{
		Base:        info.Base,
		SideInfo:    snapst.Sequence.SideInfos()[i],
		Flags:       flags.ForSnapSetup(),
		Type:        info.Type(),
		Version:     info.Version,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}

	components := snapst.Sequence.ComponentsForRevision(rev)
	compsups := make([]ComponentSetup, 0, len(components))
	for _, comp := range components {
		compsups = append(compsups, ComponentSetup{
			CompSideInfo: comp.SideInfo,
			CompType:     comp.CompType,
			ComponentInstallFlags: ComponentInstallFlags{
				MultiComponentInstall: true,
			},
		})
	}

	return doInstallOrPreDownload(st, &snapst, &snapsup, compsups, installContext{FromChange: fromChange})
}

// TransitionCore transitions from an old core snap name to a new core
// snap name. It is used for the ubuntu-core -> core transition (that
// is not just a rename because the two snaps have different snapIDs)
//
// Note that this function makes some assumptions like:
// - no aliases setup for both snaps
// - no data needs to be copied
// - all interfaces are absolutely identical on both new and old
// Do not use this as a general way to transition from snap A to snap B.
func TransitionCore(st *state.State, oldName, newName string) ([]*state.TaskSet, error) {
	var oldSnapst, newSnapst SnapState
	err := Get(st, oldName, &oldSnapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if !oldSnapst.IsInstalled() {
		return nil, fmt.Errorf("cannot transition snap %q: not installed", oldName)
	}

	var all []*state.TaskSet
	// install new core (if not already installed)
	err = Get(st, newName, &newSnapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if !newSnapst.IsInstalled() {
		enforced, err := EnforcedValidationSets(st)
		if err != nil {
			return nil, err
		}

		result, err := sendOneInstallAction(context.TODO(), st, StoreSnap{
			InstanceName: newName,
			RevOpts: RevisionOptions{
				Channel:        oldSnapst.TrackingChannel,
				ValidationSets: enforced,
			},
		}, Options{})
		if err != nil {
			return nil, err
		}

		newInfo := result.Info

		// start by installing the new snap
		tsInst, err := doInstallOrPreDownload(st, &newSnapst, &SnapSetup{
			Channel:      oldSnapst.TrackingChannel,
			DownloadInfo: &newInfo.DownloadInfo,
			SideInfo:     &newInfo.SideInfo,
			Type:         newInfo.Type(),
			Version:      newInfo.Version,
		}, nil, installContext{})
		if err != nil {
			return nil, err
		}
		all = append(all, tsInst)
	}

	// then transition the interface connections over
	transIf := st.NewTask("transition-ubuntu-core", fmt.Sprintf(i18n.G("Transition security profiles from %q to %q"), oldName, newName))
	transIf.Set("old-name", oldName)
	transIf.Set("new-name", newName)
	if len(all) > 0 {
		transIf.WaitAll(all[0])
	}
	tsTrans := state.NewTaskSet(transIf)
	all = append(all, tsTrans)

	// FIXME: this is just here for the tests
	transIf.Set("snap-setup", &SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: oldName,
		},
	})

	// then remove the old snap
	tsRm, err := Remove(st, oldName, snap.R(0), nil)
	if err != nil {
		return nil, err
	}
	tsRm.WaitFor(transIf)
	all = append(all, tsRm)

	return all, nil
}

// State/info accessors

// Installing returns whether there's an in-progress installation.
func Installing(st *state.State) bool {
	for _, task := range st.Tasks() {
		k := task.Kind()
		chg := task.Change()
		if k == "mount-snap" && chg != nil && !chg.IsReady() {
			return true
		}
	}
	return false
}

// Info returns the information about the snap with given name and revision.
// Works also for a mounted candidate snap in the process of being installed.
func Info(st *state.State, name string, revision snap.Revision) (*snap.Info, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if errors.Is(err, state.ErrNoState) {
		return nil, &snap.NotInstalledError{Snap: name}
	}
	if err != nil {
		return nil, err
	}

	sis := snapst.Sequence.SideInfos()
	for i := len(sis) - 1; i >= 0; i-- {
		if si := sis[i]; si.Revision == revision {
			return readInfo(name, si, 0)
		}
	}

	return nil, fmt.Errorf("cannot find snap %q at revision %s", name, revision.String())
}

// CurrentInfo returns the information about the current revision of a snap with the given name.
func CurrentInfo(st *state.State, name string) (*snap.Info, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	info, err := snapst.CurrentInfo()
	if err == ErrNoCurrent {
		return nil, &snap.NotInstalledError{Snap: name}
	}
	return info, err
}

// Get retrieves the SnapState of the given snap.
func Get(st *state.State, name string, snapst *SnapState) error {
	if snapst == nil {
		return fmt.Errorf("internal error: snapst is nil")
	}
	// SnapState is (un-)marshalled from/to JSON, fields having omitempty
	// tag will not appear in the output (if empty) and subsequently will
	// not be unmarshalled to (or cleared); if the caller reuses the same
	// struct though subsequent calls, it is possible that they end up with
	// garbage inside, clear the destination struct so that we always
	// unmarshal to a clean state
	*snapst = SnapState{}

	var snaps map[string]*json.RawMessage
	err := st.Get("snaps", &snaps)
	if err != nil {
		return err
	}
	raw, ok := snaps[name]
	if !ok {
		return state.ErrNoState
	}

	// XXX: &snapst pointer isn't needed here but it is likely historical
	// (a bug in old JSON marshaling probably).
	err = json.Unmarshal([]byte(*raw), &snapst)
	if err != nil {
		return fmt.Errorf("cannot unmarshal snap state: %v", err)
	}
	return nil
}

// All retrieves return a map from name to SnapState for all current snaps in the system state.
func All(st *state.State) (map[string]*SnapState, error) {
	// XXX: result is a map because sideloaded snaps carry no name
	// atm in their sideinfos
	var stateMap map[string]*SnapState
	if err := st.Get("snaps", &stateMap); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	curStates := make(map[string]*SnapState, len(stateMap))
	for instanceName, snapst := range stateMap {
		curStates[instanceName] = snapst
	}
	return curStates, nil
}

// InstalledSnaps returns the list of all installed snaps suitable for
// ValidationSets checks.
func InstalledSnaps(st *state.State) (snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool, err error) {
	all, err := All(st)
	if err != nil {
		return nil, nil, err
	}
	ignoreValidation = make(map[string]bool)
	for _, snapState := range all {
		cur, err := snapState.CurrentInfo()
		if err != nil {
			return nil, nil, err
		}

		var comps []snapasserts.InstalledComponent
		for _, comp := range snapState.Sequence.Revisions[snapState.LastIndex(cur.Revision)].Components {
			comps = append(comps, snapasserts.InstalledComponent{
				ComponentRef: comp.SideInfo.Component,
				Revision:     comp.SideInfo.Revision,
			})
		}

		snaps = append(snaps, snapasserts.NewInstalledSnap(
			snapState.InstanceName(),
			snapState.CurrentSideInfo().SnapID,
			cur.Revision,
			comps,
		))

		if snapState.IgnoreValidation {
			ignoreValidation[snapState.InstanceName()] = true
		}
	}
	return snaps, ignoreValidation, nil
}

// NumSnaps returns the number of installed snaps.
func NumSnaps(st *state.State) (int, error) {
	var snaps map[string]*json.RawMessage
	if err := st.Get("snaps", &snaps); err != nil && !errors.Is(err, state.ErrNoState) {
		return -1, err
	}
	return len(snaps), nil
}

// Set sets the SnapState of the given snap, overwriting any earlier state.
// Note that a SnapState with an empty Sequence will be treated as if snapst was
// nil and name will be deleted from the state.
func Set(st *state.State, name string, snapst *SnapState) {
	var snaps map[string]*json.RawMessage
	err := st.Get("snaps", &snaps)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		panic("internal error: cannot unmarshal snaps state: " + err.Error())
	}
	if snaps == nil {
		snaps = make(map[string]*json.RawMessage)
	}
	if snapst == nil || (len(snapst.Sequence.Revisions) == 0) {
		delete(snaps, name)
	} else {
		data, err := json.Marshal(snapst)
		if err != nil {
			panic("internal error: cannot marshal snap state: " + err.Error())
		}
		raw := json.RawMessage(data)
		snaps[name] = &raw
	}
	st.Set("snaps", snaps)
}

// ActiveInfos returns information about all active snaps.
func ActiveInfos(st *state.State) ([]*snap.Info, error) {
	var stateMap map[string]*SnapState
	var infos []*snap.Info
	if err := st.Get("snaps", &stateMap); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	for instanceName, snapst := range stateMap {
		if !snapst.Active {
			continue
		}
		snapInfo, err := snapst.CurrentInfo()
		if err != nil {
			logger.Noticef("cannot retrieve info for snap %q: %s", instanceName, err)
			continue
		}
		infos = append(infos, snapInfo)
	}
	return infos, nil
}

func HasSnapOfType(st *state.State, snapType snap.Type) (bool, error) {
	var stateMap map[string]*SnapState
	if err := st.Get("snaps", &stateMap); err != nil && !errors.Is(err, state.ErrNoState) {
		return false, err
	}

	for _, snapst := range stateMap {
		typ, err := snapst.Type()
		if err != nil {
			return false, err
		}
		if typ == snapType {
			return true, nil
		}
	}

	return false, nil
}

func infosForType(st *state.State, snapType snap.Type) ([]*snap.Info, error) {
	var stateMap map[string]*SnapState
	if err := st.Get("snaps", &stateMap); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}

	var res []*snap.Info
	for _, snapst := range stateMap {
		if !snapst.IsInstalled() {
			continue
		}
		typ, err := snapst.Type()
		if err != nil {
			return nil, err
		}
		if typ != snapType {
			continue
		}
		si, err := snapst.CurrentInfo()
		if err != nil {
			return nil, err
		}
		res = append(res, si)
	}

	if len(res) == 0 {
		return nil, state.ErrNoState
	}

	return res, nil
}

func infoForDeviceSnap(st *state.State, deviceCtx DeviceContext, whichName func(*asserts.Model) string) (*snap.Info, error) {
	if deviceCtx == nil {
		return nil, fmt.Errorf("internal error: unset deviceCtx")
	}
	model := deviceCtx.Model()
	snapName := whichName(model)
	if snapName == "" {
		return nil, state.ErrNoState
	}
	var snapst SnapState
	err := Get(st, snapName, &snapst)
	if err != nil {
		return nil, err
	}
	return snapst.CurrentInfo()
}

// GadgetInfo finds the gadget snap's info for the given device context.
func GadgetInfo(st *state.State, deviceCtx DeviceContext) (*snap.Info, error) {
	return infoForDeviceSnap(st, deviceCtx, (*asserts.Model).Gadget)
}

// KernelInfo finds the kernel snap's info for the given device context.
func KernelInfo(st *state.State, deviceCtx DeviceContext) (*snap.Info, error) {
	return infoForDeviceSnap(st, deviceCtx, (*asserts.Model).Kernel)
}

// BootBaseInfo finds the boot base snap's info for the given device context.
func BootBaseInfo(st *state.State, deviceCtx DeviceContext) (*snap.Info, error) {
	baseName := func(mod *asserts.Model) string {
		base := mod.Base()
		if base == "" {
			return "core"
		}
		return base
	}
	return infoForDeviceSnap(st, deviceCtx, baseName)
}

// TODO: reintroduce a KernelInfo(state.State, DeviceContext) if needed
// KernelInfo finds the current kernel snap's info.

// coreInfo finds the current OS snap's info. If both
// "core" and "ubuntu-core" is installed then "core"
// is preferred. Different core names are not supported
// currently and will result in an error.
func coreInfo(st *state.State) (*snap.Info, error) {
	res, err := infosForType(st, snap.TypeOS)
	if err != nil {
		return nil, err
	}

	// a single core: just return it
	if len(res) == 1 {
		return res[0], nil
	}

	// some systems have two cores: ubuntu-core/core
	// we always return "core" in this case
	if len(res) == 2 {
		if res[0].InstanceName() == defaultCoreSnapName && res[1].InstanceName() == "ubuntu-core" {
			return res[0], nil
		}
		if res[0].InstanceName() == "ubuntu-core" && res[1].InstanceName() == defaultCoreSnapName {
			return res[1], nil
		}
		return nil, fmt.Errorf("unexpected cores %q and %q", res[0].InstanceName(), res[1].InstanceName())
	}

	return nil, fmt.Errorf("unexpected number of cores, got %d", len(res))
}

// ConfigDefaults returns the configuration defaults for the snap as
// specified in the gadget for the given device context.
// If gadget is absent or the snap has no snap-id it returns ErrNoState.
func ConfigDefaults(st *state.State, deviceCtx DeviceContext, snapName string) (map[string]any, error) {
	info, err := GadgetInfo(st, deviceCtx)
	if err != nil {
		return nil, err
	}

	// system configuration is kept under "core" so apply its defaults when
	// configuring "core"
	isSystemDefaults := snapName == defaultCoreSnapName
	var snapst SnapState
	if err := Get(st, snapName, &snapst); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}

	var snapID string
	if snapst.IsInstalled() {
		snapID = snapst.CurrentSideInfo().SnapID
	}
	// system snaps (core and snapd) snaps can be addressed even without a
	// snap-id via the special "system" value in the config; first-boot
	// always configures the core snap with UseConfigDefaults
	if snapID == "" && !isSystemDefaults {
		return nil, state.ErrNoState
	}

	// no constraints enforced: those should have been checked before already
	gadgetInfo, err := gadget.ReadInfo(info.MountDir(), nil)
	if err != nil {
		return nil, err
	}

	// we support setting core defaults via "system"
	if isSystemDefaults {
		if defaults, ok := gadgetInfo.Defaults["system"]; ok {
			if _, ok := gadgetInfo.Defaults[snapID]; ok && snapID != "" {
				logger.Noticef("core snap configuration defaults found under both 'system' key and core-snap-id, preferring 'system'")
			}

			return defaults, nil
		}
	}

	defaults, ok := gadgetInfo.Defaults[snapID]
	if !ok {
		return nil, state.ErrNoState
	}

	return defaults, nil
}

// GadgetConnections returns the interface connection instructions
// specified in the gadget for the given device context.
// If gadget is absent it returns ErrNoState.
func GadgetConnections(st *state.State, deviceCtx DeviceContext) ([]gadget.Connection, error) {
	info, err := GadgetInfo(st, deviceCtx)
	if err != nil {
		return nil, err
	}

	// no constraints enforced: those should have been checked before already
	gadgetInfo, err := gadget.ReadInfo(info.MountDir(), nil)
	if err != nil {
		return nil, err
	}

	return gadgetInfo.Connections, nil
}

// downloadsToKeep returns a map of download file names that need to be kept
// for all current snaps in the system state.
//
// A downloaded file is only kept if any of the following are true:
//  1. The revision is in SnapState.Sequence
//  2. The revision is in saved in refresh-candidates
//  3. The revision is pointed to by a download task in an ongoing change
//
// It is the caller's responsibility to lock state before calling this function.
func downloadsToKeep(st *state.State) (map[string]bool, error) {
	snapStates, err := All(st)
	if err != nil {
		return nil, err
	}

	var refreshHints map[string]*refreshCandidate
	if err := st.Get("refresh-candidates", &refreshHints); err != nil && !errors.Is(err, &state.NoStateError{}) {
		return nil, fmt.Errorf("cannot get refresh-candidates: %v", err)
	}

	var downloadsToKeep map[string]bool
	keep := func(name string, rev snap.Revision) {
		if downloadsToKeep == nil {
			downloadsToKeep = make(map[string]bool)
		}
		downloadsToKeep[fmt.Sprintf("%s_%s.snap", name, rev)] = true
	}

	// keep revisions in snap's sequence
	for snapName, snapst := range snapStates {
		for _, si := range snapst.Sequence.SideInfos() {
			keep(snapName, si.Revision)
		}
	}

	// keep revisions in refresh hints
	for snapName, hint := range refreshHints {
		keep(snapName, hint.Revision())
	}

	// keep revisions pointed to by a download task in an ongoing change
	for _, chg := range st.Changes() {
		if chg.IsReady() {
			continue
		}
		for _, t := range chg.Tasks() {
			if t.Kind() != "download-snap" {
				continue
			}
			snapsup, err := TaskSnapSetup(t)
			if err != nil {
				return nil, err
			}
			keep(snapsup.InstanceName(), snapsup.Revision())
		}
	}

	return downloadsToKeep, nil
}

var maxUnusedDownloadRetention = 24 * time.Hour

func maybeRemoveSnapDownload(file string) error {
	now := time.Now()

	fi, err := os.Stat(file)
	if err != nil {
		return err
	}
	// skip deleting new downloads
	if fi.ModTime().Add(maxUnusedDownloadRetention).After(now) {
		return nil
	}

	return os.Remove(file)
}

// cleanDownloads removes downloads that are no longer needed for all snaps.
//
// It is the caller's responsibility to lock state before calling this function.
var cleanDownloads = func(st *state.State) error {
	keep, err := downloadsToKeep(st)
	if err != nil {
		return err
	}

	matches, err := filepath.Glob(filepath.Join(dirs.SnapBlobDir, "*.snap"))
	if err != nil {
		return err
	}
	for _, file := range matches {
		if keep[filepath.Base(file)] {
			continue
		}
		if rmErr := maybeRemoveSnapDownload(file); rmErr != nil {
			// continue deletion, report error in the end
			err = rmErr
		}
	}

	return err
}

// cleanSnapDownloads removes downloads that are no longer needed for the given snap.
//
// It is the caller's responsibility to lock state before calling this function.
var cleanSnapDownloads = func(st *state.State, snapName string) error {
	keep, err := downloadsToKeep(st)
	if err != nil {
		return err
	}

	regex := regexp.MustCompile(fmt.Sprintf("^%s_x?[0-9]+\\.snap$", snapName))

	matches, err := filepath.Glob(filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_*.snap", snapName)))
	if err != nil {
		return err
	}
	for _, file := range matches {
		if !regex.MatchString(filepath.Base(file)) {
			continue
		}
		if keep[filepath.Base(file)] {
			continue
		}
		if rmErr := maybeRemoveSnapDownload(file); rmErr != nil {
			// continue deletion, report error in the end
			err = rmErr
		}
	}

	return err
}

// IconInstallFilename returns the path at which the cached icon would be
// located for the snap with the given ID, if it exists. This function always
// returns the path (for non-empty snap ID), and does not check whether the
// snap is installed or the icon is actually present.
func IconInstallFilename(snapID string) string {
	return backend.IconInstallFilename(snapID)
}

func MockOsutilCheckFreeSpace(mock func(path string, minSize uint64) error) (restore func()) {
	old := osutilCheckFreeSpace
	osutilCheckFreeSpace = mock
	return func() { osutilCheckFreeSpace = old }
}

// UnmountAllSnaps unmounts all of the snaps and components in the system state.
// The primary use case for this is to unmount all snaps that were installed in
// the chroot environment that is used when creating a preseeded image.
func UnmountAllSnaps(st *state.State) error {
	all, err := All(st)
	if err != nil {
		return err
	}

	for _, snapst := range all {
		if err := unmountSnap(snapst); err != nil {
			return err
		}
	}
	return nil
}

func unmountSnap(snapst *SnapState) error {
	unmountedComps := make(map[string]bool)
	for _, rev := range snapst.Sequence.Revisions {
		for _, c := range rev.Components {
			compName := c.SideInfo.Component.ComponentName
			cpi := snap.MinimalComponentContainerPlaceInfo(
				compName,
				c.SideInfo.Revision,
				snapst.InstanceName(),
			)

			mountDir := cpi.MountDir()

			// components might be shared between snap revisions, so make sure
			// we only unmount them once
			if unmountedComps[mountDir] {
				continue
			}
			unmountedComps[mountDir] = true

			logger.Debugf("unmounting component %s at %s", compName, mountDir)
			if _, err := exec.Command("umount", "-d", "-l", mountDir).CombinedOutput(); err != nil {
				return err
			}
		}

		mountDir := snap.MountDir(snapst.InstanceName(), rev.Snap.Revision)
		logger.Debugf("unmounting snap %s at %s", snapst.InstanceName(), mountDir)
		if _, err := exec.Command("umount", "-d", "-l", mountDir).CombinedOutput(); err != nil {
			return err
		}
	}

	return nil
}
