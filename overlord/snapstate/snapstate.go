// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
)

// control flags for doInstall
const (
	skipConfigure = 1 << iota
	noRestartBoundaries
)

// control flags for "Configure()"
const (
	IgnoreHookError = 1 << iota
	UseConfigDefaults
)

const (
	BeginEdge                        = state.TaskSetEdge("begin")
	BeforeHooksEdge                  = state.TaskSetEdge("before-hooks")
	HooksEdge                        = state.TaskSetEdge("hooks")
	BeforeMaybeRebootEdge            = state.TaskSetEdge("before-maybe-reboot")
	MaybeRebootEdge                  = state.TaskSetEdge("maybe-reboot")
	MaybeRebootWaitEdge              = state.TaskSetEdge("maybe-reboot-wait")
	AfterMaybeRebootWaitEdge         = state.TaskSetEdge("after-maybe-reboot-wait")
	LastBeforeLocalModificationsEdge = state.TaskSetEdge("last-before-local-modifications")
	EndEdge                          = state.TaskSetEdge("end")
)

const (
	firmwareUpdaterSnapID         = "EI0D1KHjP8XiwMZKqSjuh6W8zvcowUVP"
	snapdDesktopIntegrationSnapID = "IrwRHakqtzhFRHJOOPxKVPU0Kk7Erhcu"
)

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

type updateParamsFunc func(*snap.Info) (*RevisionOptions, Flags, *SnapState)

type readyUpdateInfo interface {
	minimalInstallInfo

	SnapSetupForUpdate(st *state.State, params updateParamsFunc, userID int, globalFlags *Flags, prqt PrereqTracker) (*SnapSetup, *SnapState, error)
}

// ByType supports sorting by snap type. The most important types come first.
type byType []minimalInstallInfo

func (r byType) Len() int      { return len(r) }
func (r byType) Swap(i, j int) { r[i], r[j] = r[j], r[i] }
func (r byType) Less(i, j int) bool {
	return r[i].Type().SortsBefore(r[j].Type())
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
	return getKeys(defaultProviderContentAttrs(st, ins.Info, prqt))
}

func (ins installSnapInfo) SnapSetupForUpdate(st *state.State, params updateParamsFunc, userID int, globalFlags *Flags, prqt PrereqTracker) (*SnapSetup, *SnapState, error) {
	update := ins.Info

	revnoOpts, flags, snapst := params(update)
	flags.IsAutoRefresh = globalFlags.IsAutoRefresh
	flags.IsContinuedAutoRefresh = globalFlags.IsContinuedAutoRefresh

	flags, err := earlyChecks(st, snapst, update, flags)
	if err != nil {
		return nil, nil, err
	}

	snapUserID, err := userIDForSnap(st, snapst, userID)
	if err != nil {
		return nil, nil, err
	}

	providerContentAttrs := defaultProviderContentAttrs(st, update, prqt)
	snapsup := SnapSetup{
		Base:               update.Base,
		Prereq:             getKeys(providerContentAttrs),
		PrereqContentAttrs: providerContentAttrs,
		Channel:            revnoOpts.Channel,
		CohortKey:          revnoOpts.CohortKey,
		UserID:             snapUserID,
		Flags:              flags.ForSnapSetup(),
		DownloadInfo:       &update.DownloadInfo,
		SideInfo:           &update.SideInfo,
		Type:               update.Type(),
		Version:            update.Version,
		PlugsOnly:          len(update.Slots) == 0,
		InstanceKey:        update.InstanceKey,
		auxStoreInfo: auxStoreInfo{
			Media: update.Media,
			// XXX we store this for the benefit of old snapd
			Website: update.Website(),
		},
		ExpectedProvenance: update.SnapProvenance,
	}
	snapsup.IgnoreRunning = globalFlags.IgnoreRunning
	return &snapsup, snapst, nil
}

// pathInfo holds information about a path install
type pathInfo struct {
	*snap.Info
	path     string
	sideInfo *snap.SideInfo
}

func (i pathInfo) DownloadSize() int64 {
	return i.Size
}

// SnapBase returns the base snap of the snap.
func (i pathInfo) SnapBase() string {
	return i.Base
}

func (i pathInfo) Prereq(st *state.State, prqt PrereqTracker) []string {
	return getKeys(defaultProviderContentAttrs(st, i.Info, prqt))
}

func (i pathInfo) SnapSetupForUpdate(st *state.State, params updateParamsFunc, _ int, _ *Flags, prqt PrereqTracker) (*SnapSetup, *SnapState, error) {
	update := i.Info

	_, flags, snapst := params(update)

	providerContentAttrs := defaultProviderContentAttrs(st, update, prqt)
	snapsup := SnapSetup{
		Base:               i.Base,
		Prereq:             getKeys(providerContentAttrs),
		PrereqContentAttrs: providerContentAttrs,
		SideInfo:           i.sideInfo,
		SnapPath:           i.path,
		Flags:              flags.ForSnapSetup(),
		Type:               i.Type(),
		Version:            i.Version,
		PlugsOnly:          len(i.Slots) == 0,
		InstanceKey:        i.InstanceKey,
	}
	return &snapsup, snapst, nil
}

// soundness check
var _ readyUpdateInfo = installSnapInfo{}

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

func isParallelInstallable(snapsup *SnapSetup) error {
	if snapsup.InstanceKey == "" {
		return nil
	}
	if snapsup.Type == snap.TypeApp {
		return nil
	}
	return fmt.Errorf("cannot install snap of type %v as %q", snapsup.Type, snapsup.InstanceName())
}

func optedIntoSnapdSnap(st *state.State) (bool, error) {
	tr := config.NewTransaction(st)
	experimentalAllowSnapd, err := features.Flag(tr, features.SnapdSnap)
	if err != nil && !config.IsNoOption(err) {
		return false, err
	}
	return experimentalAllowSnapd, nil
}

// refreshRetain returns refresh.retain value if set, or the default value (different for core and classic).
// It deals with potentially wrong type due to lax validation.
func refreshRetain(st *state.State) int {
	var val interface{}
	// due to lax validation of refresh.retain on set we might end up having a string representing a number here; handle it gracefully
	// for backwards compatibility.
	err := config.NewTransaction(st).Get("core", "refresh.retain", &val)
	var retain int
	if err == nil {
		switch v := val.(type) {
		// this is the expected value; confusingly, since we pass interface{} to Get(), we get json.Number type; if int reference was passed,
		// we would get an int instead of json.Number.
		case json.Number:
			retain, err = strconv.Atoi(string(v))
		// not really expected when requesting interface{}.
		case int:
			retain = v
		// we can get string here due to lax validation of refresh.retain on Set in older releases.
		case string:
			retain, err = strconv.Atoi(v)
		default:
			logger.Noticef("internal error: refresh.retain system option has unexpected type: %T", v)
		}
	}

	// this covers error from Get() and strconv above.
	if err != nil && !config.IsNoOption(err) {
		logger.Noticef("internal error: refresh.retain system option is not valid: %v", err)
	}

	// not set, use default value
	if retain == 0 {
		// on classic we only keep 2 copies by default
		if release.OnClassic {
			retain = 2
		} else {
			retain = 3
		}
	}
	return retain
}

var excludeFromRefreshAppAwareness = func(t snap.Type) bool {
	return t == snap.TypeSnapd || t == snap.TypeOS
}

func isDefaultConfigureAllowed(snapsup *SnapSetup) bool {
	return isConfigureAllowed(snapsup) && !isCoreSnap(snapsup.InstanceName())
}

func isConfigureAllowed(snapsup *SnapSetup) bool {
	// we do not support configuration for bases or the "snapd" snap yet
	return snapsup.Type != snap.TypeBase && snapsup.Type != snap.TypeSnapd
}

func configureSnapFlags(snapst *SnapState, snapsup *SnapSetup) int {
	confFlags := 0
	// config defaults cannot be retrieved without a snap ID
	hasSnapID := snapsup.SideInfo != nil && snapsup.SideInfo.SnapID != ""

	if !snapst.IsInstalled() && hasSnapID && !isCoreSnap(snapsup.InstanceName()) {
		// installation, run configure using the gadget defaults if available, system config defaults (attached to
		// "core") are consumed only during seeding, via an explicit configure step separate from installing
		confFlags |= UseConfigDefaults
	}
	return confFlags
}

func isCoreSnap(snapName string) bool {
	return snapName == defaultCoreSnapName
}

func doInstall(st *state.State, snapst *SnapState, snapsup *SnapSetup, flags int, fromChange string, inUseCheck func(snap.Type) (boot.InUseFunc, error)) (*state.TaskSet, error) {
	tr := config.NewTransaction(st)
	experimentalRefreshAppAwareness, err := features.Flag(tr, features.RefreshAppAwareness)
	if err != nil && !config.IsNoOption(err) {
		return nil, err
	}
	experimentalGateAutoRefreshHook, err := features.Flag(tr, features.GateAutoRefreshHook)
	if err != nil && !config.IsNoOption(err) {
		return nil, err
	}

	if snapsup.InstanceName() == "system" {
		return nil, fmt.Errorf("cannot install reserved snap name 'system'")
	}
	if snapst.IsInstalled() && !snapst.Active {
		return nil, fmt.Errorf("cannot update disabled snap %q", snapsup.InstanceName())
	}
	if snapst.IsInstalled() && !snapsup.Flags.Revert {
		if inUseCheck == nil {
			return nil, fmt.Errorf("internal error: doInstall: inUseCheck not provided for refresh")
		}
	}

	if snapsup.Flags.Classic {
		if !release.OnClassic {
			return nil, fmt.Errorf("classic confinement is only supported on classic systems")
		} else if !dirs.SupportsClassicConfinement() {
			return nil, fmt.Errorf(i18n.G("classic confinement requires snaps under /snap or symlink from /snap to %s"), dirs.SnapMountDir)
		}
	}
	if !snapst.IsInstalled() { // install?
		// check that the snap command namespace doesn't conflict with an enabled alias
		if err := checkSnapAliasConflict(st, snapsup.InstanceName()); err != nil {
			return nil, err
		}
	}

	if err := isParallelInstallable(snapsup); err != nil {
		return nil, err
	}

	if err := checkChangeConflictIgnoringOneChange(st, snapsup.InstanceName(), snapst, fromChange); err != nil {
		return nil, err
	}

	targetRevision := snapsup.Revision()
	revisionStr := fmt.Sprintf(" (%s)", snapsup.Revision())

	if snapst.IsInstalled() {
		// consider also the current revision to set plugs-only hint
		info, err := snapst.CurrentInfo()
		if err != nil {
			return nil, err
		}
		snapsup.PlugsOnly = snapsup.PlugsOnly && (len(info.Slots) == 0)

		// When downgrading snapd we want to make sure that it's an exclusive change.
		if snapsup.SnapName() == "snapd" {
			res, err := strutil.VersionCompare(info.Version, snapsup.Version)
			if err != nil {
				return nil, fmt.Errorf("cannot compare versions of snapd [cur: %s, new: %s]: %v", info.Version, snapsup.Version, err)
			}
			// If snapsup.Version was smaller, 1 is returned.
			if res == 1 {
				if err := checkChangeConflictExclusiveKinds(st, "snapd downgrade", fromChange); err != nil {
					return nil, err
				}
			}
		}

		if experimentalRefreshAppAwareness && !excludeFromRefreshAppAwareness(snapsup.Type) && !snapsup.Flags.IgnoreRunning {
			// Note that because we are modifying the snap state inside
			// softCheckNothingRunningForRefresh, this block must be located
			// after the conflict check done above.
			if err := softCheckNothingRunningForRefresh(st, snapst, snapsup, info); err != nil {
				// snap is running; schedule its downloading before notifying to close
				var busyErr *timedBusySnapError
				if errors.As(err, &busyErr) && snapsup.IsAutoRefresh {
					tasks, err := findTasksMatchingKindAndSnap(st, "pre-download-snap", snapsup.InstanceName(), snapsup.Revision())
					if err != nil {
						return nil, err
					}

					for _, task := range tasks {
						switch task.Status() {
						case state.DoStatus, state.DoingStatus:
							// there's already a task for this snap/revision combination
							return nil, busyErr
						}
					}

					ts := state.NewTaskSet()
					preDownTask := st.NewTask("pre-download-snap", fmt.Sprintf(i18n.G("Pre-download snap %q%s from channel %q"), snapsup.InstanceName(), revisionStr, snapsup.Channel))
					preDownTask.Set("snap-setup", snapsup)
					preDownTask.Set("refresh-info", busyErr.PendingSnapRefreshInfo())

					ts.AddTask(preDownTask)
					return ts, busyErr
				}

				return nil, err
			}
		}

		if experimentalGateAutoRefreshHook {
			// If this snap was held, then remove it from snaps-hold.
			if err := resetGatingForRefreshed(st, snapsup.InstanceName()); err != nil {
				return nil, err
			}
		}
	}

	// check if we already have the revision locally (alters tasks)
	revisionIsLocal := snapst.LastIndex(targetRevision) >= 0

	prereq := st.NewTask("prerequisites", fmt.Sprintf(i18n.G("Ensure prerequisites for %q are available"), snapsup.InstanceName()))
	prereq.Set("snap-setup", snapsup)

	var prepare, prev *state.Task
	fromStore := false
	// if we have a local revision here we go back to that
	if snapsup.SnapPath != "" || revisionIsLocal {
		prepare = st.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q%s"), snapsup.SnapPath, revisionStr))
	} else {
		fromStore = true
		prepare = st.NewTask("download-snap", fmt.Sprintf(i18n.G("Download snap %q%s from channel %q"), snapsup.InstanceName(), revisionStr, snapsup.Channel))
	}
	prepare.Set("snap-setup", snapsup)
	prepare.WaitFor(prereq)

	tasks := []*state.Task{prereq, prepare}
	prev = prepare

	addTask := func(t *state.Task) {
		t.Set("snap-setup-task", prepare.ID())
		t.WaitFor(prev)
		tasks = append(tasks, t)
	}
	addTasksFromTaskSet := func(ts *state.TaskSet) {
		ts.WaitFor(prev)
		tasks = append(tasks, ts.Tasks()...)
		prev = tasks[len(tasks)-1]
	}

	var checkAsserts *state.Task
	if fromStore {
		// fetch and check assertions
		checkAsserts = st.NewTask("validate-snap", fmt.Sprintf(i18n.G("Fetch and check assertions for snap %q%s"), snapsup.InstanceName(), revisionStr))
		addTask(checkAsserts)
		prev = checkAsserts
	}

	// mount
	if !revisionIsLocal {
		mount := st.NewTask("mount-snap", fmt.Sprintf(i18n.G("Mount snap %q%s"), snapsup.InstanceName(), revisionStr))
		addTask(mount)
		prev = mount
	} else {
		if snapsup.Flags.RemoveSnapPath {
			// If the revision is local, we will not need the
			// temporary snap.  This can happen when
			// e.g. side-loading a local revision again.  The
			// SnapPath is only needed in the "mount-snap" handler
			// and that is skipped for local revisions.
			if err := os.Remove(snapsup.SnapPath); err != nil {
				return nil, err
			}
		}
	}

	// run refresh hooks when updating existing snap, otherwise run install hook further down.
	runRefreshHooks := snapst.IsInstalled() && !snapsup.Flags.Revert
	if runRefreshHooks {
		preRefreshHook := SetupPreRefreshHook(st, snapsup.InstanceName())
		addTask(preRefreshHook)
		prev = preRefreshHook
	}

	if snapst.IsInstalled() {
		// unlink-current-snap (will stop services for copy-data)
		stop := st.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap %q services"), snapsup.InstanceName()))
		stop.Set("stop-reason", snap.StopReasonRefresh)
		addTask(stop)
		prev = stop

		removeAliases := st.NewTask("remove-aliases", fmt.Sprintf(i18n.G("Remove aliases for snap %q"), snapsup.InstanceName()))
		removeAliases.Set("remove-reason", removeAliasesReasonRefresh)
		addTask(removeAliases)
		prev = removeAliases

		unlink := st.NewTask("unlink-current-snap", fmt.Sprintf(i18n.G("Make current revision for snap %q unavailable"), snapsup.InstanceName()))
		unlink.Set("unlink-reason", unlinkReasonRefresh)
		addTask(unlink)
		prev = unlink
	}

	// we need to know some of the characteristics of the device - it is
	// expected to always have a model/device context at this point.
	// TODO in a remodel this would use the old model, we need to fix this
	// as needsKernelSetup needs to know the new model for UC2{0,2} -> UC24
	// remodel case.
	deviceCtx, err := DeviceCtx(st, nil, nil)
	if err != nil {
		return nil, err
	}

	// This task is necessary only for UC20+ and hybrid
	if snapsup.Type == snap.TypeKernel && needsKernelSetup(deviceCtx) {
		setupKernel := st.NewTask("prepare-kernel-snap", fmt.Sprintf(i18n.G("Prepare kernel driver tree for %q%s"), snapsup.InstanceName(), revisionStr))
		addTask(setupKernel)
		prev = setupKernel
	}

	if deviceCtx.IsCoreBoot() && (snapsup.Type == snap.TypeGadget || (snapsup.Type == snap.TypeKernel && !TestingLeaveOutKernelUpdateGadgetAssets)) {
		// gadget update currently for core boot systems only
		gadgetUpdate := st.NewTask("update-gadget-assets", fmt.Sprintf(i18n.G("Update assets from %s %q%s"), snapsup.Type, snapsup.InstanceName(), revisionStr))
		addTask(gadgetUpdate)
		prev = gadgetUpdate
	}
	// kernel command line from gadget is for core boot systems only
	if deviceCtx.IsCoreBoot() && snapsup.Type == snap.TypeGadget {
		// make sure no other active changes are changing the kernel command line
		if err := CheckUpdateKernelCommandLineConflict(st, fromChange); err != nil {
			return nil, err
		}
		gadgetCmdline := st.NewTask("update-gadget-cmdline", fmt.Sprintf(i18n.G("Update kernel command line from gadget %q%s"), snapsup.InstanceName(), revisionStr))
		addTask(gadgetCmdline)
		prev = gadgetCmdline
	}

	// copy-data (needs stopped services by unlink)
	if !snapsup.Flags.Revert {
		copyData := st.NewTask("copy-snap-data", fmt.Sprintf(i18n.G("Copy snap %q data"), snapsup.InstanceName()))
		addTask(copyData)
		prev = copyData
	}

	// security
	setupSecurity := st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup snap %q%s security profiles"), snapsup.InstanceName(), revisionStr))
	addTask(setupSecurity)
	prev = setupSecurity

	// finalize (wrappers+current symlink)
	//
	// For essential snaps that require reboots, 'link-snap' is currently
	// marked as the edge of that reboot sequence. This means that we currently
	// expect 'link-snap' to request the reboot and be the last task to run
	// before the reboot takes place (for that lane/change). This task is
	// assigned the edge 'MaybeRebootEdge' to indicate this.
	//
	// 'link-snap' is the last task to run before a reboot for cases like the kernel
	// where we would like to try to make sure it boots correctly before we perform
	// additional tasks.
	linkSnap := st.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q%s available to the system"), snapsup.InstanceName(), revisionStr))
	addTask(linkSnap)
	prev = linkSnap

	// auto-connections
	//
	// For essential snaps that require reboots, 'auto-connect' is marked
	// as edge 'MaybeRebootWaitEdge' to indicate that this task is expected
	// to be the first to run after the reboot (for that lane/change). This
	// is noted here to make sure we consider any changes between 'link-snap'
	// and 'auto-connect', as that need the edges to be modified as well.
	//
	// 'auto-connect' is expected to run first after the reboot as it also
	// performs some reboot-verification code.
	autoConnect := st.NewTask("auto-connect", fmt.Sprintf(i18n.G("Automatically connect eligible plugs and slots of snap %q"), snapsup.InstanceName()))
	addTask(autoConnect)
	prev = autoConnect

	if snapsup.Type == snap.TypeKernel && needsKernelSetup(deviceCtx) {
		// This task needs to run after we're back and running the new
		// kernel after a reboot was requested in link-snap handler.
		setupKernel := st.NewTask("discard-old-kernel-snap-setup", fmt.Sprintf(i18n.G("Discard kernel driver tree for %q%s"), snapsup.InstanceName(), revisionStr))
		addTask(setupKernel)
		prev = setupKernel
	}

	// setup aliases
	setAutoAliases := st.NewTask("set-auto-aliases", fmt.Sprintf(i18n.G("Set automatic aliases for snap %q"), snapsup.InstanceName()))
	addTask(setAutoAliases)
	prev = setAutoAliases

	setupAliases := st.NewTask("setup-aliases", fmt.Sprintf(i18n.G("Setup snap %q aliases"), snapsup.InstanceName()))
	addTask(setupAliases)
	prev = setupAliases

	if snapsup.Flags.Prefer {
		prefer := st.NewTask("prefer-aliases", fmt.Sprintf(i18n.G("Prefer aliases for snap %q"), snapsup.InstanceName()))
		addTask(prefer)
		prev = prefer
	}

	if deviceCtx.IsCoreBoot() && snapsup.Type == snap.TypeSnapd {
		// make sure no other active changes are changing the kernel command line
		if err := CheckUpdateKernelCommandLineConflict(st, fromChange); err != nil {
			return nil, err
		}
		// only run for core devices and the snapd snap, run late enough
		// so that the task is executed by the new snapd
		bootConfigUpdate := st.NewTask("update-managed-boot-config", fmt.Sprintf(i18n.G("Update managed boot config assets from %q%s"), snapsup.InstanceName(), revisionStr))
		addTask(bootConfigUpdate)
		prev = bootConfigUpdate
	}

	if runRefreshHooks {
		postRefreshHook := SetupPostRefreshHook(st, snapsup.InstanceName())
		addTask(postRefreshHook)
		prev = postRefreshHook
	}

	var installHook *state.Task
	// only run install hook if installing the snap for the first time
	if !snapst.IsInstalled() {
		installHook = SetupInstallHook(st, snapsup.InstanceName())
		addTask(installHook)
		prev = installHook
	}

	if snapsup.QuotaGroupName != "" {
		quotaAddSnapTask, err := AddSnapToQuotaGroup(st, snapsup.InstanceName(), snapsup.QuotaGroupName)
		if err != nil {
			return nil, err
		}
		addTask(quotaAddSnapTask)
		prev = quotaAddSnapTask
	}

	// only run default-configure hook if installing the snap for the first time and
	// default-configure is allowed
	if !snapst.IsInstalled() && isDefaultConfigureAllowed(snapsup) {
		defaultConfigureSet := DefaultConfigure(st, snapsup.InstanceName())
		addTasksFromTaskSet(defaultConfigureSet)
	}

	// run new services
	startSnapServices := st.NewTask("start-snap-services", fmt.Sprintf(i18n.G("Start snap %q%s services"), snapsup.InstanceName(), revisionStr))
	addTask(startSnapServices)
	prev = startSnapServices

	// Do not do that if we are reverting to a local revision
	var cleanupTask *state.Task
	if snapst.IsInstalled() && !snapsup.Flags.Revert {
		retain := refreshRetain(st)

		// if we're not using an already present revision, account for the one being added
		if snapst.LastIndex(targetRevision) == -1 {
			retain-- //  we're adding one
		}

		seq := snapst.Sequence.Revisions
		currentIndex := snapst.LastIndex(snapst.Current)

		// discard everything after "current" (we may have reverted to
		// a previous versions earlier)
		for i := currentIndex + 1; i < len(seq); i++ {
			si := seq[i]
			if si.Snap.Revision == targetRevision {
				// but don't discard this one; its' the thing we're switching to!
				continue
			}
			ts := removeInactiveRevision(st, snapsup.InstanceName(), si.Snap.SnapID, si.Snap.Revision, snapsup.Type)
			addTasksFromTaskSet(ts)
		}

		// make sure we're not scheduling the removal of the target
		// revision in the case where the target revision is already in
		// the sequence.
		for i := 0; i < currentIndex; i++ {
			si := seq[i]
			if si.Snap.Revision == targetRevision {
				// we do *not* want to removeInactiveRevision of this one
				copy(seq[i:], seq[i+1:])
				seq = seq[:len(seq)-1]
				currentIndex--
			}
		}

		// normal garbage collect
		var inUse boot.InUseFunc
		for i := 0; i <= currentIndex-retain; i++ {
			if inUse == nil {
				var err error
				inUse, err = inUseCheck(snapsup.Type)
				if err != nil {
					return nil, err
				}
			}

			si := seq[i]
			if inUse(snapsup.InstanceName(), si.Snap.Revision) {
				continue
			}
			ts := removeInactiveRevision(st, snapsup.InstanceName(), si.Snap.SnapID, si.Snap.Revision, snapsup.Type)
			addTasksFromTaskSet(ts)
		}

		cleanupTask = st.NewTask("cleanup", fmt.Sprintf("Clean up %q%s install", snapsup.InstanceName(), revisionStr))
		addTask(cleanupTask)
	}

	installSet := state.NewTaskSet(tasks...)
	installSet.MarkEdge(prereq, BeginEdge)
	installSet.MarkEdge(setupAliases, BeforeHooksEdge)
	installSet.MarkEdge(setupSecurity, BeforeMaybeRebootEdge)
	installSet.MarkEdge(linkSnap, MaybeRebootEdge)
	installSet.MarkEdge(autoConnect, MaybeRebootWaitEdge)
	installSet.MarkEdge(setAutoAliases, AfterMaybeRebootWaitEdge)
	if installHook != nil {
		installSet.MarkEdge(installHook, HooksEdge)
	}
	// if snap is being installed from the store, then the last task before
	// any system modifications are done is check validate-snap, otherwise
	// it's the prepare-snap
	if checkAsserts != nil {
		installSet.MarkEdge(checkAsserts, LastBeforeLocalModificationsEdge)
	} else {
		installSet.MarkEdge(prepare, LastBeforeLocalModificationsEdge)
	}
	if flags&noRestartBoundaries == 0 {
		if err := SetEssentialSnapsRestartBoundaries(st, nil, []*state.TaskSet{installSet}); err != nil {
			return nil, err
		}
	}
	if flags&skipConfigure != 0 {
		if cleanupTask != nil {
			installSet.MarkEdge(cleanupTask, EndEdge)
		} else {
			installSet.MarkEdge(startSnapServices, EndEdge)
		}
		return installSet, nil
	}

	if isConfigureAllowed(snapsup) {
		confFlags := configureSnapFlags(snapst, snapsup)
		configSet := ConfigureSnap(st, snapsup.InstanceName(), confFlags)
		configSet.WaitAll(installSet)
		installSet.AddAll(configSet)
	}

	healthCheck := CheckHealthHook(st, snapsup.InstanceName(), snapsup.Revision())
	healthCheck.WaitAll(installSet)
	installSet.AddTask(healthCheck)
	installSet.MarkEdge(healthCheck, EndEdge)

	return installSet, nil
}

func needsKernelSetup(devCtx DeviceContext) bool {
	// Must be UC20+ or hybrid
	if !devCtx.HasModeenv() {
		return false
	}

	// Check that we have a snapd-generator that will create mount
	// units for the drivers tree, for both classic & UC
	if devCtx.Classic() {
		// We run the generator from the deb package, so check its version
		snapdInfoDir := filepath.Join(dirs.GlobalRootDir, dirs.CoreLibExecDir)
		debVersion, _, err := snapdtool.SnapdVersionFromInfoFile(snapdInfoDir)
		if err != nil {
			return false
		}

		res, err := strutil.VersionCompare(debVersion, "2.62")
		if err != nil {
			logger.Noticef("cannot compare %q to 2.62: %v", debVersion, err)
			return false
		}
		if res >= 0 {
			return true
		}
	} else {
		// We assume core24 onwards has the generator, for older boot bases
		// we return false.
		// TODO this won't work for a UC2{0,2} -> UC24+ remodel as we
		// need the context created from the new model. Get to this
		// ASAP after snapd 2.62 release.
		baseSn := devCtx.Model().BaseSnap()
		if baseSn == nil {
			logger.Noticef("internal error: no base in model")
			return false
		}
		// TODO in remodeling we are not getting the right answer,
		// how to fix that?
		switch baseSn.SnapName() {
		case "core20", "core22", "core22-desktop":
			return false
		default:
			return true
		}
	}

	return false
}

func findTasksMatchingKindAndSnap(st *state.State, kind string, snapName string, revision snap.Revision) ([]*state.Task, error) {
	var tasks []*state.Task
	for _, t := range st.Tasks() {
		if t.Kind() != kind {
			continue
		}

		snapsup, _, err := snapSetupAndState(t)
		if err != nil {
			return nil, err
		}

		if snapsup.InstanceName() == snapName && snapsup.Revision() == revision {
			tasks = append(tasks, t)
		}
	}

	return tasks, nil
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

var Configure = func(st *state.State, snapName string, patch map[string]interface{}, flags int) *state.TaskSet {
	panic("internal error: snapstate.Configure is unset")
}

var DefaultConfigure = func(st *state.State, snapName string) *state.TaskSet {
	panic("internal error: snapstate.DefaultConfigure is unset")
}

var SetupInstallHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupInstallHook is unset")
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

// FinishRestart will return a Retry error if there is a pending restart
// and a real error if anything went wrong (like a rollback across
// restarts).
// For snapd snap updates this will also rerun wrappers generation to fully
// catch up with any change.
func FinishRestart(task *state.Task, snapsup *SnapSetup) (err error) {
	if snapdenv.Preseeding() {
		// nothing to do when preseeding
		return nil
	}
	if ok, _ := restart.Pending(task.State()); ok {
		// don't continue until we are in the restarted snapd
		task.Logf("Waiting for automatic snapd restart...")
		return &state.Retry{}
	}

	if snapsup.Type == snap.TypeSnapd {
		if os.Getenv("SNAPD_REVERT_TO_REV") != "" {
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

func getKeys(kv map[string][]string) []string {
	keys := make([]string, 0, len(kv))

	for key := range kv {
		keys = append(keys, key)
	}

	return keys
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
		// The firmware-updater and snapd-desktop-integration
		// snaps are allowed to use user daemons, irrespective
		// of the feature flag state.
		//
		// TODO: remove the special case once
		// experimental.user-daemons is the default
		if !flag && info.SnapID != firmwareUpdaterSnapID && info.SnapID != snapdDesktopIntegrationSnapID {
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
		return flags, err
	}
	if err := checkDBusServiceConflicts(st, info); err != nil {
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

// addPrereq adds the given prerequisite snap to the tracker, if the tracker is
// not nil
func addPrereq(prqt PrereqTracker, info *snap.Info) {
	if prqt != nil {
		prqt.Add(info)
	}
}

// InstallPath returns a set of tasks for installing a snap from a file path
// and the snap.Info for the given snap.
//
// Note that the state must be locked by the caller.
// The provided SideInfo can contain just a name which results in a
// local revision and sideloading, or full metadata in which case it
// the snap will appear as installed from the store.
func InstallPath(st *state.State, si *snap.SideInfo, path, instanceName, channel string, flags Flags, prqt PrereqTracker) (*state.TaskSet, *snap.Info, error) {
	if si.RealName == "" {
		return nil, nil, fmt.Errorf("internal error: snap name to install %q not provided", path)
	}

	if flags.Lane != 0 {
		return nil, nil, fmt.Errorf("transaction lane is unsupported in InstallPath")
	}

	if instanceName == "" {
		instanceName = si.RealName
	}

	deviceCtx, err := DeviceCtxFromState(st, nil)
	if err != nil {
		return nil, nil, err
	}

	var snapst SnapState
	err = Get(st, instanceName, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, nil, err
	}

	if si.SnapID != "" {
		if si.Revision.Unset() {
			return nil, nil, fmt.Errorf("internal error: snap id set to install %q but revision is unset", path)
		}
	}

	channel, err = resolveChannel(instanceName, snapst.TrackingChannel, channel, deviceCtx)
	if err != nil {
		return nil, nil, err
	}

	var instFlags int
	if flags.SkipConfigure {
		// extract it as a doInstall flag, this is not passed
		// into SnapSetup
		instFlags |= skipConfigure
	}

	// It is ok do open the snap file here because we either
	// have side info or the user passed --dangerous
	info, err := validatedInfoFromPathAndSideInfo(instanceName, path, si)
	if err != nil {
		return nil, nil, err
	}

	snapName, instanceKey := snap.SplitInstanceName(instanceName)
	if info.SnapName() != snapName {
		return nil, nil, fmt.Errorf("cannot install snap %q, the name does not match the metadata %q", instanceName, info.SnapName())
	}
	info.InstanceKey = instanceKey

	flags, err = ensureInstallPreconditions(st, info, flags, &snapst)
	if err != nil {
		return nil, nil, err
	}
	// this might be a refresh; check the epoch before proceeding
	if err := earlyEpochCheck(info, &snapst); err != nil {
		return nil, nil, err
	}

	addPrereq(prqt, info)
	providerContentAttrs := defaultProviderContentAttrs(st, info, prqt)
	snapsup := &SnapSetup{
		Base:               info.Base,
		Prereq:             getKeys(providerContentAttrs),
		PrereqContentAttrs: providerContentAttrs,
		SideInfo:           si,
		SnapPath:           path,
		Channel:            channel,
		Flags:              flags.ForSnapSetup(),
		Type:               info.Type(),
		Version:            info.Version,
		PlugsOnly:          len(info.Slots) == 0,
		InstanceKey:        info.InstanceKey,
	}

	ts, err := doInstall(st, &snapst, snapsup, instFlags, "", inUseFor(deviceCtx))
	return ts, info, err
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

type snapInfoForInstall func(DeviceContext, *RevisionOptions) (si *snap.Info, snapPath, redirectChannel string, e error)

// InstallWithDeviceContext returns a set of tasks for installing a snap.
// It will query the store for the snap with the given deviceCtx.
// Note that the state must be locked by the caller.
//
// The returned TaskSet will contain a LastBeforeLocalModificationsEdge
// identifying the last task before the first task that introduces system
// modifications.
func InstallWithDeviceContext(ctx context.Context, st *state.State, name string, opts *RevisionOptions, userID int, flags Flags, prqt PrereqTracker, deviceCtx DeviceContext, fromChange string) (*state.TaskSet, error) {
	logger.Debugf("installing with device context %s", name)
	snapInstallInfo := func(dc DeviceContext, ro *RevisionOptions) (si *snap.Info, snapPath, redirectChannel string, e error) {
		sar, err := installInfo(ctx, st, name, ro, userID, flags, dc)
		if err != nil {
			return nil, "", "", err
		}
		addPrereq(prqt, sar.Info)
		return sar.Info, "", sar.RedirectChannel, nil
	}
	return installWithDeviceContext(st, name, opts, userID, flags, prqt, deviceCtx, fromChange, snapInstallInfo)
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

	if !opts.Revision.Unset() && si.Revision != opts.Revision {
		return nil, fmt.Errorf("cannot install local snap %q: %v != %v (revision mismatch)", name, opts.Revision, si.Revision)
	}

	snapInstallInfo := func(DeviceContext, *RevisionOptions) (info *snap.Info, snapPath, redirectChannel string, e error) {
		info, err := validatedInfoFromPathAndSideInfo(name, path, si)
		if err != nil {
			return nil, "", "", err
		}
		addPrereq(prqt, info)
		return info, path, "", nil
	}
	return installWithDeviceContext(st, name, opts, userID, flags, prqt, deviceCtx, fromChange, snapInstallInfo)
}

func installWithDeviceContext(st *state.State, name string, opts *RevisionOptions, userID int, flags Flags, prqt PrereqTracker, deviceCtx DeviceContext, fromChange string, snapInstallInfo snapInfoForInstall) (*state.TaskSet, error) {
	if opts == nil {
		opts = &RevisionOptions{}
	}
	if opts.CohortKey != "" && !opts.Revision.Unset() {
		return nil, errors.New("cannot specify revision and cohort")
	}

	if flags.Lane != 0 {
		return nil, fmt.Errorf("transaction lane is unsupported in InstallWithDeviceContext")
	}

	if opts.Channel == "" {
		opts.Channel = "stable"
	}

	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if snapst.IsInstalled() {
		return nil, &snap.AlreadyInstalledError{Snap: name}
	}

	if err := snap.ValidateInstanceName(name); err != nil {
		return nil, fmt.Errorf("invalid instance name: %v", err)
	}

	// make sure to have a model set
	devPastSeedCtx, err := DevicePastSeeding(st, deviceCtx)
	if err != nil {
		return nil, err
	}

	info, snapPath, redirectChannel, err := snapInstallInfo(devPastSeedCtx, opts)
	if err != nil {
		return nil, err
	}

	if flags.RequireTypeBase && info.Type() != snap.TypeBase && info.Type() != snap.TypeOS {
		return nil, fmt.Errorf("unexpected snap type %q, instead of 'base'", info.Type())
	}

	flags, err = ensureInstallPreconditions(st, info, flags, &snapst)
	if err != nil {
		return nil, err
	}

	if err := checkDiskSpace(st, "install", []minimalInstallInfo{installSnapInfo{info}}, userID, prqt); err != nil {
		return nil, err
	}

	providerContentAttrs := defaultProviderContentAttrs(st, info, prqt)
	snapsup := &SnapSetup{
		Channel:            opts.Channel,
		Base:               info.Base,
		Prereq:             getKeys(providerContentAttrs),
		PrereqContentAttrs: providerContentAttrs,
		UserID:             userID,
		Flags:              flags.ForSnapSetup(),
		DownloadInfo:       &info.DownloadInfo,
		SideInfo:           &info.SideInfo,
		Type:               info.Type(),
		Version:            info.Version,
		PlugsOnly:          len(info.Slots) == 0,
		InstanceKey:        info.InstanceKey,
		auxStoreInfo: auxStoreInfo{
			Media: info.Media,
			// XXX we store this for the benefit of old snapd
			Website: info.Website(),
		},
		CohortKey:          opts.CohortKey,
		ExpectedProvenance: info.SnapProvenance,
	}

	// If we don't have a local snap we need to download it.
	if snapPath != "" {
		snapsup.SnapPath = snapPath
	} else {
		snapsup.DownloadInfo = &info.DownloadInfo
	}

	if redirectChannel != "" {
		snapsup.Channel = redirectChannel
	}

	return doInstall(st, &snapst, snapsup, 0, fromChange, nil)
}

// Download returns a set of tasks for downloading a snap into the given
// blobDirectory. If blobDirectory is empty, then dirs.SnapBlobDir is used. The
// snap.Info for the snap that is downloaded is also returned. The tasks that
// are returned will also download and validate the snap's assertion.
// Prerequisites for the snap are not downloaded.
func Download(ctx context.Context, st *state.State, name string, blobDirectory string, opts *RevisionOptions, userID int, flags Flags, deviceCtx DeviceContext) (*state.TaskSet, *snap.Info, error) {
	if opts == nil {
		opts = &RevisionOptions{}
	}

	if opts.CohortKey != "" && !opts.Revision.Unset() {
		return nil, nil, errors.New("cannot specify revision and cohort")
	}

	if opts.Channel == "" {
		opts.Channel = "stable"
	}

	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, nil, err
	}

	if err := snap.ValidateInstanceName(name); err != nil {
		return nil, nil, fmt.Errorf("invalid instance name: %v", err)
	}

	sar, err := downloadInfo(ctx, st, name, opts, userID, deviceCtx)
	if err != nil {
		return nil, nil, err
	}

	info := sar.Info

	// if we are going to use the default download dir, and the same snap
	// revision is already installed, then we should not overwrite the snap that
	// is already in the dir.
	if (blobDirectory == "" || blobDirectory == dirs.SnapBlobDir) && info.Revision == snapst.Current {
		return nil, nil, &snap.AlreadyInstalledError{Snap: name}
	}

	if flags.RequireTypeBase && info.Type() != snap.TypeBase && info.Type() != snap.TypeOS {
		return nil, nil, fmt.Errorf("unexpected snap type %q, instead of 'base'", info.Type())
	}

	snapsup := &SnapSetup{
		Channel:            opts.Channel,
		Base:               info.Base,
		UserID:             userID,
		Flags:              flags.ForSnapSetup(),
		DownloadInfo:       &info.DownloadInfo,
		SideInfo:           &info.SideInfo,
		Type:               info.Type(),
		Version:            info.Version,
		InstanceKey:        info.InstanceKey,
		CohortKey:          opts.CohortKey,
		ExpectedProvenance: info.SnapProvenance,
		DownloadBlobDir:    blobDirectory,
	}

	if sar.RedirectChannel != "" {
		snapsup.Channel = sar.RedirectChannel
	}

	toDownloadTo := filepath.Dir(snapsup.MountFile())
	if err := checkDiskSpaceDownload([]minimalInstallInfo{installSnapInfo{info}}, toDownloadTo); err != nil {
		return nil, nil, err
	}

	revisionStr := fmt.Sprintf(" (%s)", snapsup.Revision())

	download := st.NewTask("download-snap", fmt.Sprintf(i18n.G("Download snap %q%s from channel %q"), snapsup.InstanceName(), revisionStr, snapsup.Channel))
	download.Set("snap-setup", snapsup)

	checkAsserts := st.NewTask("validate-snap", fmt.Sprintf(i18n.G("Fetch and check assertions for snap %q%s"), snapsup.InstanceName(), revisionStr))
	checkAsserts.Set("snap-setup-task", download.ID())
	checkAsserts.WaitFor(download)

	installSet := state.NewTaskSet(download, checkAsserts)
	installSet.MarkEdge(download, BeginEdge)
	installSet.MarkEdge(checkAsserts, LastBeforeLocalModificationsEdge)

	return installSet, info, nil
}

func validatedInfoFromPathAndSideInfo(snapName, path string, si *snap.SideInfo) (*snap.Info, error) {
	var info *snap.Info
	info, cont, err := backend.OpenSnapFile(path, si)
	if err != nil {
		return nil, fmt.Errorf("cannot open snap file: %v", err)
	}
	if err := validateContainer(cont, info, logger.Noticef); err != nil {
		return nil, err
	}
	if err := snap.ValidateInstanceName(snapName); err != nil {
		return nil, fmt.Errorf("invalid instance name: %v", err)
	}
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
	if flags == nil {
		flags = &Flags{}
	}

	deviceCtx, err := DevicePastSeeding(st, nil)
	if err != nil {
		return nil, err
	}

	var updates []minimalInstallInfo
	var names []string
	stateByInstanceName := make(map[string]*SnapState, len(sideInfos))
	flagsByInstanceName := make(map[string]Flags, len(sideInfos))

	for i, si := range sideInfos {
		name := si.RealName

		info, err := validatedInfoFromPathAndSideInfo(name, paths[i], si)
		if err != nil {
			return nil, err
		}

		var snapst SnapState
		if err = Get(st, name, &snapst); err != nil && !errors.Is(err, state.ErrNoState) {
			return nil, err
		}

		flags, err := earlyChecks(st, &snapst, info, *flags)
		if err != nil {
			return nil, err
		}

		if !(flags.JailMode || flags.DevMode) {
			flags.Classic = flags.Classic || snapst.Flags.Classic
		}

		updates = append(updates, pathInfo{Info: info, path: paths[i], sideInfo: si})
		names = append(names, name)
		stateByInstanceName[name] = &snapst
		flagsByInstanceName[name] = flags
	}

	if err := checkDiskSpace(st, "install", updates, userID, nil); err != nil {
		return nil, err
	}

	params := func(update *snap.Info) (*RevisionOptions, Flags, *SnapState) {
		name := update.InstanceName()
		return nil, flagsByInstanceName[name], stateByInstanceName[name]
	}

	_, updateTss, err := doUpdate(ctx, st, names, updates, params, userID, flags, nil, deviceCtx, "")
	if err != nil {
		return nil, err
	}

	return updateTss.Refresh, nil
}

// InstallMany installs everything from the given list of names. When specifying
// revisions, the checks against enforced validation sets are bypassed.
// Note that the state must be locked by the caller.
func InstallMany(st *state.State, names []string, revOpts []*RevisionOptions, userID int, flags *Flags) ([]string, []*state.TaskSet, error) {
	if flags == nil {
		flags = &Flags{}
	}

	// need to have a model set before trying to talk the store
	deviceCtx, err := DevicePastSeeding(st, nil)
	if err != nil {
		return nil, nil, err
	}

	names = strutil.Deduplicate(names)

	toInstall := make([]string, 0, len(names))
	for _, name := range names {
		var snapst SnapState
		err := Get(st, name, &snapst)
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return nil, nil, err
		}
		if snapst.IsInstalled() {
			continue
		}

		if err := snap.ValidateInstanceName(name); err != nil {
			return nil, nil, fmt.Errorf("invalid instance name: %v", err)
		}

		toInstall = append(toInstall, name)
	}

	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, nil, err
	}

	installs, err := installCandidates(st, toInstall, revOpts, "stable", user)
	if err != nil {
		return nil, nil, err
	}

	snapInfos := make([]minimalInstallInfo, len(installs))
	for i, sar := range installs {
		snapInfos[i] = installSnapInfo{sar.Info}
	}

	if err = checkDiskSpace(st, "install", snapInfos, userID, nil); err != nil {
		return nil, nil, err
	}

	// can only specify a lane when running multiple operations transactionally
	if flags.Transaction != client.TransactionAllSnaps && flags.Lane != 0 {
		return nil, nil, errors.New("cannot specify a lane without setting transaction to \"all-snaps\"")
	}

	var transactionLane int
	if flags.Transaction == client.TransactionAllSnaps {
		if flags.Lane != 0 {
			transactionLane = flags.Lane
		} else {
			transactionLane = st.NewLane()
		}
	}

	tasksets := make([]*state.TaskSet, 0, len(installs))
	for _, sar := range installs {
		info := sar.Info
		var snapst SnapState

		validatedFlags, err := ensureInstallPreconditions(st, info, *flags, &snapst)
		if err != nil {
			return nil, nil, err
		}

		channel := "stable"
		if sar.RedirectChannel != "" {
			channel = sar.RedirectChannel
		}

		providerContentAttrs := defaultProviderContentAttrs(st, info, nil)
		snapsup := &SnapSetup{
			Channel:            channel,
			Base:               info.Base,
			Prereq:             getKeys(providerContentAttrs),
			PrereqContentAttrs: providerContentAttrs,
			UserID:             userID,
			Flags:              validatedFlags.ForSnapSetup(),
			DownloadInfo:       &info.DownloadInfo,
			SideInfo:           &info.SideInfo,
			Type:               info.Type(),
			Version:            info.Version,
			PlugsOnly:          len(info.Slots) == 0,
			InstanceKey:        info.InstanceKey,
			ExpectedProvenance: info.SnapProvenance,
		}

		ts, err := doInstall(st, &snapst, snapsup, 0, "", inUseFor(deviceCtx))
		if err != nil {
			return nil, nil, err
		}

		// If transactional, use a single lane for all snaps, so when
		// one fails the changes for all affected snaps will be
		// undone. Otherwise, have different lanes per snap so failures
		// only affect the culprit snap.
		if flags.Transaction == client.TransactionAllSnaps {
			ts.JoinLane(transactionLane)
		} else {
			ts.JoinLane(st.NewLane())
		}
		tasksets = append(tasksets, ts)
	}

	return toInstall, tasksets, nil
}

// RefreshCandidates gets a list of candidates for update
// Note that the state must be locked by the caller.
func RefreshCandidates(st *state.State, user *auth.UserState) ([]*snap.Info, error) {
	updates, _, _, err := refreshCandidates(context.TODO(), st, nil, nil, user, nil)
	return updates, err
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

	affected := make([]string, 0, len(valErr.MissingSnaps)+len(valErr.WrongRevisionSnaps))
	var tasksets []*state.TaskSet
	// use the same lane for installing and refreshing so everything is reversed
	lane := st.NewLane()

	collectRevOpts := func(snapToRevToVss map[string]map[snap.Revision][]string) ([]string, []*RevisionOptions) {
		var names []string
		var revOpts []*RevisionOptions

		for snapName, revAndVs := range snapToRevToVss {
			for rev, valsets := range revAndVs {
				vsKeys := make([]snapasserts.ValidationSetKey, 0, len(valsets))
				for _, vs := range valsets {
					vsKey := snapasserts.NewValidationSetKey(valErr.Sets[vs])
					vsKeys = append(vsKeys, vsKey)
				}

				revOpts = append(revOpts, &RevisionOptions{Revision: rev, ValidationSets: vsKeys})
			}
			names = append(names, snapName)
		}

		return names, revOpts
	}

	if len(valErr.WrongRevisionSnaps) > 0 {
		names, revOpts := collectRevOpts(valErr.WrongRevisionSnaps)
		// we're targeting precise revisions so re-refreshes don't make sense. Refreshes
		// between epochs should managed by through  the validation sets
		flags := &Flags{Transaction: client.TransactionAllSnaps, Lane: lane, NoReRefresh: true}

		updated, tss, err := UpdateMany(ctx, st, names, revOpts, userID, flags)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot auto-resolve enforcement constraints: %w", err)
		}

		tasksets = append(tasksets, tss...)
		affected = append(affected, updated...)
	}

	if len(valErr.MissingSnaps) > 0 {
		names, revOpts := collectRevOpts(valErr.MissingSnaps)
		flags := &Flags{Transaction: client.TransactionAllSnaps, Lane: lane}

		installed, tss, err := InstallMany(st, names, revOpts, userID, flags)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot auto-resolve enforcement constraints: %w", err)
		}

		// updates should be done before the installs
		for _, ts := range tss {
			for _, prevTs := range tasksets {
				ts.WaitAll(prevTs)
			}
		}
		tasksets = append(tasksets, tss...)
		affected = append(affected, installed...)
	}

	encodedAsserts := make(map[string][]byte, len(valErr.Sets))
	for vsStr, vs := range valErr.Sets {
		encodedAsserts[vsStr] = asserts.Encode(vs)
	}

	enforceTask := st.NewTask("enforce-validation-sets", "Enforce validation sets")
	enforceTask.Set("validation-sets", encodedAsserts)
	enforceTask.Set("pinned-sequence-numbers", pinnedSeqs)
	enforceTask.Set("userID", userID)

	for _, ts := range tasksets {
		enforceTask.WaitAll(ts)
	}
	ts := state.NewTaskSet(enforceTask)
	ts.JoinLane(lane)
	tasksets = append(tasksets, ts)

	return tasksets, affected, nil
}

// updateFilter is the type of function that can be passed to
// updateManyFromChange so it filters the updates.
//
// If the filter returns true, the update for that snap proceeds. If
// it returns false, the snap is removed from the list of updates to
// consider.
type updateFilter func(*snap.Info, *SnapState) bool

func updateManyFiltered(ctx context.Context, st *state.State, names []string, revOpts []*RevisionOptions, userID int, filter updateFilter, flags *Flags, fromChange string) ([]string, *UpdateTaskSets, error) {
	if flags == nil {
		flags = &Flags{}
	}
	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, nil, err
	}

	// need to have a model set before trying to talk the store
	deviceCtx, err := DevicePastSeeding(st, nil)
	if err != nil {
		return nil, nil, err
	}

	names = strutil.Deduplicate(names)

	refreshOpts := &store.RefreshOptions{Scheduled: flags.IsAutoRefresh}
	updates, stateByInstanceName, ignoreValidation, err := refreshCandidates(ctx, st, names, revOpts, user, refreshOpts)
	if err != nil {
		return nil, nil, err
	}

	// save the candidates so the auto-refresh can be continued if it's inhibited
	// by a running snap.
	if flags.IsAutoRefresh {
		hints, err := refreshHintsFromCandidates(st, updates, ignoreValidation, deviceCtx)
		if err != nil {
			return nil, nil, err
		}

		updateRefreshCandidates(st, hints, names)
	}

	if filter != nil {
		actual := updates[:0]
		for _, update := range updates {
			if filter(update, stateByInstanceName[update.InstanceName()]) {
				actual = append(actual, update)
			}
		}
		updates = actual
	}

	if ValidateRefreshes != nil && len(updates) != 0 {
		updates, err = ValidateRefreshes(st, updates, ignoreValidation, userID, deviceCtx)
		if err != nil {
			// not doing "refresh all" report the error
			if len(names) != 0 {
				return nil, nil, err
			}
			// doing "refresh all", log the problems
			logger.Noticef("cannot refresh some snaps: %v", err)
		}
	}

	params := func(update *snap.Info) (*RevisionOptions, Flags, *SnapState) {
		snapst := stateByInstanceName[update.InstanceName()]
		// setting options to what's in state as multi-refresh doesn't let you change these
		opts := &RevisionOptions{
			Channel:   snapst.TrackingChannel,
			CohortKey: snapst.CohortKey,
		}
		return opts, snapst.Flags, snapst
	}

	toUpdate := make([]minimalInstallInfo, len(updates))
	for i, up := range updates {
		toUpdate[i] = installSnapInfo{up}
	}

	// don't refresh held snaps in a general refresh
	if len(names) == 0 {
		toUpdate, err = filterHeldSnaps(st, toUpdate, flags)
		if err != nil {
			return nil, nil, err
		}
	}

	if err = checkDiskSpace(st, "refresh", toUpdate, userID, nil); err != nil {
		return nil, nil, err
	}

	var updated []string
	var updateTss *UpdateTaskSets
	if essential, nonEssential, ok := canSplitRefresh(deviceCtx, toUpdate, flags); ok {
		// if we're on classic with a kernel/gadget, split refreshes with essential
		// snaps and apps so that the apps don't have to wait for a reboot
		updateFunc := func(updates []minimalInstallInfo) ([]string, *UpdateTaskSets, error) {
			// names are used to determine if the refresh is general, if it was
			// requested for a snap to update aliases and if it should be reported
			// so it's fine to pass them all into each call (extra are ignored)
			return doUpdate(ctx, st, names, updates, params, userID, flags, nil, deviceCtx, fromChange)
		}
		updated, updateTss, err = splitRefresh(essential, nonEssential, updateFunc)
	} else {
		updated, updateTss, err = doUpdate(ctx, st, names, toUpdate, params, userID, flags, nil, deviceCtx, fromChange)
	}
	if err != nil {
		return nil, nil, err
	}

	// if there are only pre-downloads, don't add a check-rerefresh task
	if len(updateTss.Refresh) > 0 {
		updateTss.Refresh = finalizeUpdate(st, updateTss.Refresh, len(updates) > 0, updated, userID, flags)
	}
	return updated, updateTss, nil
}

// canSplitRefresh returns whether the refresh is a standard refresh of a mix
// of essential and non-essential snaps on a hybrid system. If the refresh
// can be split, it also returns the two split update groups.
func canSplitRefresh(deviceCtx DeviceContext, infos []minimalInstallInfo, flags *Flags) (essential, nonEssential []minimalInstallInfo, split bool) {
	// TODO: support split refresh for auto-refresh as well
	if !deviceCtx.IsCoreBoot() || !release.OnClassic || flags.IsAutoRefresh {
		return nil, nil, false
	}

	essential, nonEssential = splitEssentialUpdates(deviceCtx, infos)
	if len(essential) == 0 || len(nonEssential) == 0 {
		return nil, nil, false
	}

	return essential, nonEssential, true
}

// splitRefresh creates two separate tasksets for the essential and non-essential
// snap refresh groups, creating dependencies between specific tasks when
// appropriate (e.g., snapd is present and should go first or an app uses the
// model base as its base and must wait for its update).
func splitRefresh(essential, nonEssential []minimalInstallInfo, updateFunc func([]minimalInstallInfo) ([]string, *UpdateTaskSets, error)) ([]string, *UpdateTaskSets, error) {
	// taskset with essential snaps (snapd, kernel, gadget and the model base)
	essentialUpdated, essentialTss, err := updateFunc(essential)
	if err != nil {
		return nil, nil, err
	}

	// taskset with non-essential snaps (apps and their bases)
	nonEssentialUpdated, nonEssentialTss, err := updateFunc(nonEssential)
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

	// add dependencies between apps and the boot base, if required
	for _, base := range essential {
		if base.Type() != snap.TypeBase {
			continue
		}

		for _, app := range nonEssential {
			if app.SnapBase() != base.InstanceName() {
				continue
			}

			baseTaskset, err := maybeFindTasksetForSnap(essentialTss.Refresh, base.InstanceName())
			if err != nil {
				return nil, nil, err
			}

			appTaskset, err := maybeFindTasksetForSnap(nonEssentialTss.Refresh, app.InstanceName())
			if err != nil {
				return nil, nil, err
			}

			if baseTaskset == nil || appTaskset == nil {
				// one of the snaps is not being updated
				continue
			}

			// make the app wait for the base
			baseEndTask := baseTaskset.MaybeEdge(EndEdge)
			if baseEndTask == nil {
				return nil, nil, fmt.Errorf("internal error: cannot find last task in base's update taskset")
			}
			appStartTask := appTaskset.MaybeEdge(BeginEdge)
			if appStartTask == nil {
				return nil, nil, fmt.Errorf("internal error: cannot find first task in snap's taskset")
			}

			appStartTask.WaitFor(baseEndTask)
		}
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

// filterHeldSnaps filters held snaps from being updated in a general refresh.
func filterHeldSnaps(st *state.State, updates []minimalInstallInfo, flags *Flags) ([]minimalInstallInfo, error) {
	holdLevel := HoldGeneral
	if flags.IsAutoRefresh {
		holdLevel = HoldAutoRefresh
	}
	heldSnaps, err := HeldSnaps(st, holdLevel)
	if err != nil {
		return nil, err
	}

	filteredUpdates := make([]minimalInstallInfo, 0, len(updates))
	for _, update := range updates {
		if _, ok := heldSnaps[update.InstanceName()]; !ok {
			filteredUpdates = append(filteredUpdates, update)
		}
	}

	return filteredUpdates, nil
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

func doUpdate(ctx context.Context, st *state.State, names []string, updates []minimalInstallInfo, params updateParamsFunc, userID int, globalFlags *Flags, prqt PrereqTracker, deviceCtx DeviceContext, fromChange string) ([]string, *UpdateTaskSets, error) {
	if globalFlags == nil {
		globalFlags = &Flags{}
	}

	var installTasksets []*state.TaskSet
	var preDlTasksets []*state.TaskSet

	refreshAll := len(names) == 0
	var nameSet map[string]bool
	if len(names) != 0 {
		nameSet = make(map[string]bool, len(names))
		for _, name := range names {
			nameSet[name] = true
		}
	}

	newAutoAliases, mustPruneAutoAliases, transferTargets, err := autoAliasesUpdate(st, names, updates)
	if err != nil {
		return nil, nil, err
	}

	reportUpdated := make(map[string]bool, len(updates))
	var pruningAutoAliasesTs *state.TaskSet

	if len(mustPruneAutoAliases) != 0 {
		var err error
		pruningAutoAliasesTs, err = applyAutoAliasesDelta(st, mustPruneAutoAliases, "prune", refreshAll, fromChange, func(snapName string, _ *state.TaskSet) {
			if nameSet[snapName] {
				reportUpdated[snapName] = true
			}
		})
		if err != nil {
			return nil, nil, err
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
	sort.Stable(byType(updates))

	// can only specify a lane when running multiple operations transactionally
	if globalFlags.Transaction != client.TransactionAllSnaps && globalFlags.Lane != 0 {
		return nil, nil, errors.New("cannot specify a lane without setting transaction to \"all-snaps\"")
	}

	// updates is sorted by kind so this will process first core
	// and bases and then other snaps
	var transactionLane int
	if globalFlags.Transaction == client.TransactionAllSnaps {
		if globalFlags.Lane != 0 {
			transactionLane = globalFlags.Lane
		} else {
			transactionLane = st.NewLane()
			// keep this in case doUpdate is called again (e.g., in a split refresh)
			globalFlags.Lane = transactionLane
		}
	}

	for _, update := range updates {
		snapsup, snapst, err := update.(readyUpdateInfo).SnapSetupForUpdate(st, params, userID, globalFlags, prqt)
		if err != nil {
			if refreshAll {
				logger.Noticef("cannot update %q: %v", update.InstanceName(), err)
				continue
			}
			return nil, nil, err
		}

		// Do not set any default restart boundaries, we do it when we have access to all
		// the task-sets in preparation for single-reboot.
		ts, err := doInstall(st, snapst, snapsup, noRestartBoundaries, fromChange, inUseFor(deviceCtx))
		if err != nil {
			if errors.Is(err, &timedBusySnapError{}) && ts != nil {
				// snap is busy and pre-download tasks were made for it
				ts.JoinLane(st.NewLane())
				preDlTasksets = append(preDlTasksets, ts)
				continue
			}

			if refreshAll {
				// doing "refresh all", just skip this snap
				logger.Noticef("cannot refresh snap %q: %v", update.InstanceName(), err)
				continue
			}
			return nil, nil, err
		}
		// If transactional, use a single lane for all snaps, so when
		// one fails the changes for all affected snaps will be
		// undone. Otherwise, have different lanes per snap so failures
		// only affect the culprit snap.
		if globalFlags.Transaction == client.TransactionAllSnaps {
			ts.JoinLane(transactionLane)
		} else {
			ts.JoinLane(st.NewLane())
		}

		scheduleUpdate(update.InstanceName(), ts)
		installTasksets = append(installTasksets, ts)
	}

	// Make sure each of them are marked with default restart-boundaries to maintain the previous
	// reboot-behaviour prior to new restart logic.
	if err := arrangeSnapTaskSetsLinkageAndRestart(st, nil, installTasksets); err != nil {
		return nil, nil, err
	}

	if len(newAutoAliases) != 0 {
		addAutoAliasesTs, err := applyAutoAliasesDelta(st, newAutoAliases, "refresh", refreshAll, fromChange, scheduleUpdate)
		if err != nil {
			return nil, nil, err
		}
		installTasksets = append(installTasksets, addAutoAliasesTs)
	}

	updated := make([]string, 0, len(reportUpdated))
	for name := range reportUpdated {
		updated = append(updated, name)
	}

	updateTss := &UpdateTaskSets{
		Refresh:     installTasksets,
		PreDownload: preDlTasksets,
	}
	return updated, updateTss, nil
}

func splitEssentialUpdates(deviceCtx DeviceContext, updates []minimalInstallInfo) (essential, nonEssential []minimalInstallInfo) {
	var snapd minimalInstallInfo
	var modelBase minimalInstallInfo

	for _, update := range updates {
		switch update.Type() {
		case snap.TypeSnapd:
			snapd = update
		case snap.TypeBase:
			if update.InstanceName() == deviceCtx.Base() {
				modelBase = update
			} else {
				nonEssential = append(nonEssential, update)
			}
		case snap.TypeGadget, snap.TypeKernel:
			// snaps that require a reboot
			essential = append(essential, update)
		default:
			nonEssential = append(nonEssential, update)
		}
	}

	// if there's no other essential snaps, snapd and the model base can be
	// refreshed with the apps (order doesn't matter here, we sort later)
	for _, info := range []minimalInstallInfo{snapd, modelBase} {
		if info != nil {
			if len(essential) > 0 {
				essential = append(essential, info)
			} else {
				nonEssential = append(nonEssential, info)
			}
		}
	}

	return essential, nonEssential
}

func finalizeUpdate(st *state.State, tasksets []*state.TaskSet, hasUpdates bool, updated []string, userID int, globalFlags *Flags) []*state.TaskSet {
	if hasUpdates && !globalFlags.NoReRefresh {
		// re-refresh will check the lanes to decide what to
		// _actually_ re-refresh, but it'll be a subset of updated
		// (and equal to updated if nothing goes wrong)
		sort.Strings(updated)
		rerefresh := st.NewTask("check-rerefresh", reRefreshSummary(updated, globalFlags))
		rerefresh.Set("rerefresh-setup", reRefreshSetup{
			UserID: userID,
			Flags:  globalFlags,
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

func autoAliasesUpdate(st *state.State, names []string, updates []minimalInstallInfo) (changed map[string][]string, mustPrune map[string][]string, transferTargets map[string]bool, err error) {
	changed, dropped, err := autoAliasesDelta(st, nil)
	if err != nil {
		if len(names) != 0 {
			// not "refresh all", error
			return nil, nil, nil, err
		}
		// log and continue
		logger.Noticef("cannot find the delta for automatic aliases for some snaps: %v", err)
	}

	refreshAll := len(names) == 0

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
		for _, name := range names {
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
	for _, info := range updates {
		updating[info.InstanceName()] = true
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
		for _, name := range names {
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
		return "", nil
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
func Switch(st *state.State, name string, opts *RevisionOptions) (*state.TaskSet, error) {
	if opts == nil {
		opts = &RevisionOptions{}
	}
	if !opts.Revision.Unset() {
		return nil, errRevisionSwitch
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

	opts.Channel, err = resolveChannel(name, snapst.TrackingChannel, opts.Channel, deviceCtx)
	if err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo:    snapst.CurrentSideInfo(),
		InstanceKey: snapst.InstanceKey,
		// set the from state (i.e. no change), they are overridden from opts as needed below
		CohortKey: snapst.CohortKey,
		Channel:   snapst.TrackingChannel,
		Type:      snap.Type(snapst.SnapType),
	}

	if opts.Channel != "" {
		snapsup.Channel = opts.Channel
	}
	if opts.CohortKey != "" {
		snapsup.CohortKey = opts.CohortKey
	}
	if opts.LeaveCohort {
		snapsup.CohortKey = ""
	}

	summary := switchSummary(snapsup.InstanceName(), snapst.TrackingChannel, snapsup.Channel, snapst.CohortKey, snapsup.CohortKey)
	switchSnap := st.NewTask("switch-snap", summary)
	switchSnap.Set("snap-setup", &snapsup)

	return state.NewTaskSet(switchSnap), nil
}

// RevisionOptions control the selection of a snap revision.
type RevisionOptions struct {
	Channel        string
	Revision       snap.Revision
	ValidationSets []snapasserts.ValidationSetKey
	CohortKey      string
	LeaveCohort    bool
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

type snapInfoForUpdate func(dc DeviceContext, ro *RevisionOptions, fl Flags, snapst *SnapState) ([]minimalInstallInfo, error)

// UpdateWithDeviceContext initiates a change updating a snap.
// It will query the store for the snap with the given deviceCtx.
// Note that the state must be locked by the caller.
//
// The returned TaskSet can contain a LastBeforeLocalModificationsEdge
// identifying the last task before the first task that introduces system
// modifications. If no such edge is set, then none of the tasks introduce
// system modifications.
func UpdateWithDeviceContext(st *state.State, name string, opts *RevisionOptions, userID int, flags Flags, prqt PrereqTracker, deviceCtx DeviceContext, fromChange string) (*state.TaskSet, error) {
	snapUpdateInfo := func(dc DeviceContext, ro *RevisionOptions, fl Flags, snapst *SnapState) ([]minimalInstallInfo, error) {
		toUpdate := []minimalInstallInfo{}
		info, infoErr := infoForUpdate(st, snapst, name, ro, userID, fl, dc)
		switch infoErr {
		case nil:
			addPrereq(prqt, info)
			toUpdate = append(toUpdate, installSnapInfo{info})
		case store.ErrNoUpdateAvailable:
			// there may be some new auto-aliases
			return toUpdate, infoErr
		default:
			return nil, infoErr
		}
		return toUpdate, infoErr
	}

	return updateWithDeviceContext(st, name, opts, userID, flags, prqt, deviceCtx, fromChange, snapUpdateInfo)
}

// UpdatePathWithDeviceContext initiates a change updating a snap from a local file.
// Note that the state must be locked by the caller.
//
// The returned TaskSet can contain a LastBeforeLocalModificationsEdge
// identifying the last task before the first task that introduces system
// modifications. If no such edge is set, then none of the tasks introduce
// system modifications.
func UpdatePathWithDeviceContext(st *state.State, si *snap.SideInfo, path, name string, opts *RevisionOptions, userID int, flags Flags, prqt PrereqTracker, deviceCtx DeviceContext, fromChange string) (*state.TaskSet, error) {
	if !opts.Revision.Unset() && si.Revision != opts.Revision {
		return nil, fmt.Errorf("cannot install local snap %q: %v != %v (revision mismatch)", name, opts.Revision, si.Revision)
	}
	snapUpdateInfo := func(dc DeviceContext, ro *RevisionOptions, fl Flags, snapst *SnapState) ([]minimalInstallInfo, error) {
		toUpdate := []minimalInstallInfo{}
		info, err := validatedInfoFromPathAndSideInfo(name, path, si)
		if err != nil {
			return nil, err
		}
		// Trying to update to the same revision that is already installed.
		// We abuse here store.ErrNoUpdateAvailable to keep behavior
		// consistent with when we try to update from the store.
		if snapst.CurrentSideInfo().Revision == info.Revision {
			return toUpdate, store.ErrNoUpdateAvailable
		}
		addPrereq(prqt, info)
		installInfo := pathInfo{Info: info, path: path, sideInfo: si}
		toUpdate = append(toUpdate, installInfo)
		return toUpdate, nil
	}

	return updateWithDeviceContext(st, name, opts, userID, flags, prqt, deviceCtx, fromChange, snapUpdateInfo)
}

func updateWithDeviceContext(st *state.State, name string, opts *RevisionOptions, userID int, flags Flags, prqt PrereqTracker, deviceCtx DeviceContext, fromChange string, snapUpdateInfo snapInfoForUpdate) (*state.TaskSet, error) {
	if opts == nil {
		opts = &RevisionOptions{}
	}
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if !snapst.IsInstalled() {
		return nil, &snap.NotInstalledError{Snap: name}
	}

	// FIXME: snaps that are not active are skipped for now
	//        until we know what we want to do
	if !snapst.Active {
		return nil, fmt.Errorf("refreshing disabled snap %q not supported", name)
	}

	// make sure we have a model set
	deviceCtx, err = DevicePastSeeding(st, deviceCtx)
	if err != nil {
		return nil, err
	}

	opts.Channel, err = resolveChannel(name, snapst.TrackingChannel, opts.Channel, deviceCtx)
	if err != nil {
		return nil, err
	}

	if opts.Channel == "" {
		// default to tracking the same channel
		opts.Channel = snapst.TrackingChannel
	}
	if opts.CohortKey == "" {
		// default to being in the same cohort
		opts.CohortKey = snapst.CohortKey
	}
	if opts.LeaveCohort {
		opts.CohortKey = ""
	}

	// TODO: make flags be per revision to avoid this logic (that
	//       leaves corner cases all over the place)
	if !(flags.JailMode || flags.DevMode) {
		flags.Classic = flags.Classic || snapst.Flags.Classic
	}

	toUpdate, infoErr := snapUpdateInfo(deviceCtx, opts, flags, &snapst)
	if infoErr != nil && infoErr != store.ErrNoUpdateAvailable {
		return nil, infoErr
	}

	if err = checkDiskSpace(st, "refresh", toUpdate, userID, prqt); err != nil {
		return nil, err
	}

	params := func(update *snap.Info) (*RevisionOptions, Flags, *SnapState) {
		return opts, flags, &snapst
	}

	_, updateTss, err := doUpdate(context.TODO(), st, []string{name}, toUpdate, params, userID, &flags, prqt, deviceCtx, fromChange)
	if err != nil {
		return nil, err
	}

	// only auto-refreshes can generate pre-download tasks so we don't need to check them
	tts := updateTss.Refresh

	// see if we need to switch the channel or cohort, or toggle ignore-validation
	switchChannel := snapst.TrackingChannel != opts.Channel
	switchCohortKey := snapst.CohortKey != opts.CohortKey
	toggleIgnoreValidation := snapst.IgnoreValidation != flags.IgnoreValidation
	if infoErr == store.ErrNoUpdateAvailable && (switchChannel || switchCohortKey || toggleIgnoreValidation) {
		if err := checkChangeConflictIgnoringOneChange(st, name, nil, fromChange); err != nil {
			return nil, err
		}

		snapsup := &SnapSetup{
			SideInfo:    snapst.CurrentSideInfo(),
			Flags:       snapst.Flags.ForSnapSetup(),
			InstanceKey: snapst.InstanceKey,
			Type:        snap.Type(snapst.SnapType),
			// no version info needed
			CohortKey: opts.CohortKey,
		}

		if switchChannel || switchCohortKey {
			// update the tracked channel and cohort
			snapsup.Channel = opts.Channel
			snapsup.CohortKey = opts.CohortKey
			// Update the current snap channel as well. This ensures that
			// the UI displays the right values.
			snapsup.SideInfo.Channel = opts.Channel

			summary := switchSummary(snapsup.InstanceName(), snapst.TrackingChannel, opts.Channel, snapst.CohortKey, opts.CohortKey)
			switchSnap := st.NewTask("switch-snap-channel", summary)
			switchSnap.Set("snap-setup", &snapsup)

			switchSnapTs := state.NewTaskSet(switchSnap)
			for _, ts := range tts {
				switchSnapTs.WaitAll(ts)
			}
			tts = append(tts, switchSnapTs)
		}

		if toggleIgnoreValidation {
			snapsup.IgnoreValidation = flags.IgnoreValidation
			toggle := st.NewTask("toggle-snap-flags", fmt.Sprintf(i18n.G("Toggle snap %q flags"), snapsup.InstanceName()))
			toggle.Set("snap-setup", &snapsup)

			toggleTs := state.NewTaskSet(toggle)
			for _, ts := range tts {
				toggleTs.WaitAll(ts)
			}
			tts = append(tts, toggleTs)
		}

		currentInfo, err := snapst.CurrentInfo()
		if err != nil {
			return nil, err
		}

		// if there isn't an update available, then we should still add the
		// current info to the prereq tracker. this is because we will not
		// return an error from this function, and the caller will assume
		// everything worked.
		addPrereq(prqt, currentInfo)
	}

	if len(tts) == 0 && len(toUpdate) == 0 {
		// really nothing to do, return the original no-update-available error
		return nil, infoErr
	}

	tts = finalizeUpdate(st, tts, len(toUpdate) > 0, []string{name}, userID, &flags)

	flat := state.NewTaskSet()
	for _, ts := range tts {
		// The tasksets we get from "doUpdate" contain important
		// "TaskEdge" information that is needed for "Remodel".
		// To preserve those we need to use "AddAllWithEdges()".
		if err := flat.AddAllWithEdges(ts); err != nil {
			return nil, err
		}
	}
	return flat, nil
}

func infoForUpdate(st *state.State, snapst *SnapState, name string, opts *RevisionOptions, userID int, flags Flags, deviceCtx DeviceContext) (*snap.Info, error) {
	if opts.Revision.Unset() {
		// good ol' refresh
		info, err := updateInfo(st, snapst, opts, userID, flags, deviceCtx)
		if err != nil {
			return nil, err
		}
		if ValidateRefreshes != nil && !flags.IgnoreValidation {
			_, err := ValidateRefreshes(st, []*snap.Info{info}, nil, userID, deviceCtx)
			if err != nil {
				return nil, err
			}
		}
		return info, nil
	}
	var sideInfo *snap.SideInfo
	for _, si := range snapst.Sequence.SideInfos() {
		if si.Revision == opts.Revision {
			sideInfo = si
			break
		}
	}
	if sideInfo == nil {
		// refresh from given revision from store
		return updateToRevisionInfo(st, snapst, opts, userID, flags, deviceCtx)
	}

	// refresh-to-local, this assumes the snap revision is mounted
	return readInfo(name, sideInfo, errorOnBroken)
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

	refreshOpts := &store.RefreshOptions{Scheduled: true}
	// XXX: should we skip refreshCandidates if forGatingSnap isn't empty (meaning we're handling proceed from a snap)?
	candidates, snapstateByInstance, ignoreValidationByInstanceName, err := refreshCandidates(ctx, st, nil, nil, user, refreshOpts)
	if err != nil {
		// XXX: should we reset "refresh-candidates" to nil in state for some types
		// of errors?
		return nil, nil, err
	}
	deviceCtx, err := DeviceCtxFromState(st, nil)
	if err != nil {
		return nil, nil, err
	}
	hints, err := refreshHintsFromCandidates(st, candidates, ignoreValidationByInstanceName, deviceCtx)
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
	for _, up := range candidates {
		if _, ok := hints[up.InstanceName()]; !ok {
			// filtered out by refreshHintsFromCandidates
			continue
		}

		snapst := snapstateByInstance[up.InstanceName()]
		if err := checkChangeConflictIgnoringOneChange(st, up.InstanceName(), snapst, fromChange); err != nil {
			logger.Noticef("cannot refresh snap %q: %v", up.InstanceName(), err)
		} else {
			updates = append(updates, up.InstanceName())
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
func autoRefreshPhase2(ctx context.Context, st *state.State, updates []*refreshCandidate, flags *Flags, fromChange string) (*UpdateTaskSets, error) {
	if flags == nil {
		flags = &Flags{IsAutoRefresh: true}
	}
	userID := 0

	deviceCtx, err := DeviceCtx(st, nil, nil)
	if err != nil {
		return nil, err
	}

	toUpdate := make([]minimalInstallInfo, len(updates))
	for i, up := range updates {
		toUpdate[i] = up
	}

	if err := checkDiskSpace(st, "refresh", toUpdate, 0, nil); err != nil {
		return nil, err
	}

	updated, updateTss, err := doUpdate(ctx, st, nil, toUpdate, nil, userID, flags, nil, deviceCtx, fromChange)
	if err != nil {
		return nil, err
	}

	// only auto-refreshes can generate pre-download tasks so we don't need to check them
	if len(updateTss.Refresh) > 0 {
		updateTss.Refresh = finalizeUpdate(st, updateTss.Refresh, len(updates) > 0, updated, userID, flags)
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

// LinkNewBaseOrKernel creates a new task set with prepare/link-snap, and
// additionally update-gadget-assets for the kernel snap, tasks for a remodel.
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

	prepareSnap := st.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q (%s) for remodel"), snapsup.InstanceName(), snapst.Current))
	prepareSnap.Set("snap-setup", &snapsup)
	prev := prepareSnap
	ts := state.NewTaskSet(prepareSnap)
	// preserve the same order as during the update
	if info.Type() == snap.TypeKernel {
		// TODO in a remodel this would use the old model, we need to fix this
		// as needsKernelSetup needs to know the new model for UC2{0,2} -> UC24
		// remodel case.
		deviceCtx, err := DeviceCtx(st, nil, nil)
		if err != nil {
			return nil, err
		}
		if needsKernelSetup(deviceCtx) {
			setupKernel := st.NewTask("prepare-kernel-snap", fmt.Sprintf(i18n.G("Prepare kernel driver tree for %q (%s) for remodel"), snapsup.InstanceName(), snapst.Current))
			ts.AddTask(setupKernel)
			setupKernel.Set("snap-setup-task", prepareSnap.ID())
			setupKernel.WaitFor(prev)
			prev = setupKernel
		}

		// kernel snaps can carry boot assets
		gadgetUpdate := st.NewTask("update-gadget-assets", fmt.Sprintf(i18n.G("Update assets from %s %q (%s) for remodel"), snapsup.Type, snapsup.InstanceName(), snapst.Current))
		gadgetUpdate.Set("snap-setup-task", prepareSnap.ID())
		gadgetUpdate.WaitFor(prev)
		ts.AddTask(gadgetUpdate)
		prev = gadgetUpdate
	}
	linkSnap := st.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) available to the system during remodel"), snapsup.InstanceName(), snapst.Current))
	linkSnap.Set("snap-setup-task", prepareSnap.ID())
	linkSnap.WaitFor(prev)
	ts.AddTask(linkSnap)
	// prepare-snap is the last task that carries no system modifications
	ts.MarkEdge(prepareSnap, LastBeforeLocalModificationsEdge)
	return ts, nil
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

// AddLinkNewBaseOrKernel creates the same tasks as LinkNewBaseOrKernel but adds
// them to the provided task set.
func AddLinkNewBaseOrKernel(st *state.State, ts *state.TaskSet) (*state.TaskSet, error) {
	allTasks := ts.Tasks()
	snapSetupTask, snapsup, err := findSnapSetupTask(allTasks)
	if err != nil {
		return nil, err
	}
	if snapSetupTask == nil {
		return nil, fmt.Errorf("internal error: cannot identify task with snap-setup")
	}
	// the first task added here waits for the last task in the existing set
	prev := allTasks[len(allTasks)-1]
	// preserve the same order as during the update
	if snapsup.Type == snap.TypeKernel {
		// TODO in a remodel this would use the old model, we need to fix this
		// as needsKernelSetup needs to know the new model for UC2{0,2} -> UC24
		// remodel case.
		deviceCtx, err := DeviceCtx(st, nil, nil)
		if err != nil {
			return nil, err
		}
		if needsKernelSetup(deviceCtx) {
			setupKernel := st.NewTask("prepare-kernel-snap", fmt.Sprintf(i18n.G("Prepare kernel driver tree for %q (%s) for remodel"), snapsup.InstanceName(), snapsup.Revision()))
			setupKernel.Set("snap-setup-task", snapSetupTask.ID())
			setupKernel.WaitFor(prev)
			ts.AddTask(setupKernel)
			prev = setupKernel
		}

		// kernel snaps can carry boot assets
		gadgetUpdate := st.NewTask("update-gadget-assets", fmt.Sprintf(i18n.G("Update assets from %s %q (%s) for remodel"), snapsup.Type, snapsup.InstanceName(), snapsup.Revision()))
		gadgetUpdate.Set("snap-setup-task", snapSetupTask.ID())
		// wait for the last task in existing set
		gadgetUpdate.WaitFor(prev)
		ts.AddTask(gadgetUpdate)
		prev = gadgetUpdate
	}
	linkSnap := st.NewTask("link-snap",
		fmt.Sprintf(i18n.G("Make snap %q (%s) available to the system during remodel"), snapsup.InstanceName(), snapsup.SideInfo.Revision))
	linkSnap.Set("snap-setup-task", snapSetupTask.ID())
	linkSnap.WaitFor(prev)
	ts.AddTask(linkSnap)
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
	for _, importantSnapType := range []snap.Type{snap.TypeGadget, snap.TypeKernel, snap.TypeOS} {
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
	requiredValsets, requiredRevision, err := enforcedSets.CheckPresenceRequired(si)
	if err != nil {
		if _, ok := err.(*snapasserts.PresenceConstraintError); !ok {
			return err
		}
		// else - presence is invalid, nothing to do (not really possible since
		// it shouldn't be allowed to get installed in the first place).
		return nil
	}
	if len(requiredValsets) == 0 {
		// not required by any validation set (or is optional)
		return nil
	}
	// removeAll is set if we're removing the snap completely
	if removeAll {
		if requiredRevision.Unset() {
			return fmt.Errorf("snap %q is required by validation sets: %s", si.InstanceName(), snapasserts.ValidationSetKeySlice(requiredValsets).CommaSeparated())
		}
		return fmt.Errorf("snap %q at revision %s is required by validation sets: %s", si.InstanceName(), requiredRevision, snapasserts.ValidationSetKeySlice(requiredValsets).CommaSeparated())
	}

	// rev is set at this point (otherwise we would hit removeAll case)
	if requiredRevision.N == rev.N {
		return fmt.Errorf("snap %q at revision %s is required by validation sets: %s", si.InstanceName(), rev, snapasserts.ValidationSetKeySlice(requiredValsets).CommaSeparated())
	} // else - it's ok to remove a revision different than the required
	return nil
}

// RemoveFlags are used to pass additional flags to the Remove operation.
type RemoveFlags struct {
	// Remove the snap without creating snapshot data
	Purge bool
}

// Remove returns a set of tasks for removing snap.
// Note that the state must be locked by the caller.
func Remove(st *state.State, name string, revision snap.Revision, flags *RemoveFlags) (*state.TaskSet, error) {
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
		if active {
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
		removeHook := SetupRemoveHook(st, snapsup.InstanceName())
		addNext(state.NewTaskSet(removeHook))
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

	// 'purge' flag disables automatic snapshot for given remove op
	if flags == nil || !flags.Purge {
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
				addNext(removeInactiveRevision(st, name, info.SnapID, si.Revision, snapsup.Type))
			}
		}
		// add tasks for removing the current revision last,
		// this is then also when common data will be removed
		if currentIndex >= 0 {
			addNext(removeInactiveRevision(st, name, info.SnapID, si[currentIndex].Revision, snapsup.Type))
		}
	} else {
		addNext(removeInactiveRevision(st, name, info.SnapID, revision, snapsup.Type))
	}

	return removeTs, snapshotSize, nil
}

func removeInactiveRevision(st *state.State, name, snapID string, revision snap.Revision, typ snap.Type) *state.TaskSet {
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

	clearData := st.NewTask("clear-snap", fmt.Sprintf(i18n.G("Remove data for snap %q (%s)"), name, revision))
	clearData.Set("snap-setup", snapsup)

	discardSnap := st.NewTask("discard-snap", fmt.Sprintf(i18n.G("Remove snap %q (%s) from the system"), name, revision))
	discardSnap.WaitFor(clearData)
	discardSnap.Set("snap-setup-task", clearData.ID())

	return state.NewTaskSet(clearData, discardSnap)
}

// RemoveMany removes everything from the given list of names.
// Note that the state must be locked by the caller.
func RemoveMany(st *state.State, names []string, flags *RemoveFlags) ([]string, []*state.TaskSet, error) {
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

	snapsup := &SnapSetup{
		Base:        info.Base,
		SideInfo:    snapst.Sequence.SideInfos()[i],
		Flags:       flags.ForSnapSetup(),
		Type:        info.Type(),
		Version:     info.Version,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}
	return doInstall(st, &snapst, snapsup, 0, fromChange, nil)
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
		var userID int
		newInfo, err := installInfo(context.TODO(), st, newName, &RevisionOptions{Channel: oldSnapst.TrackingChannel}, userID, Flags{}, nil)
		if err != nil {
			return nil, err
		}

		// start by installing the new snap
		tsInst, err := doInstall(st, &newSnapst, &SnapSetup{
			Channel:      oldSnapst.TrackingChannel,
			DownloadInfo: &newInfo.DownloadInfo,
			SideInfo:     &newInfo.SideInfo,
			Type:         newInfo.Type(),
			Version:      newInfo.Version,
		}, 0, "", nil)
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
		snaps = append(snaps, snapasserts.NewInstalledSnap(snapState.InstanceName(),
			snapState.CurrentSideInfo().SnapID, cur.Revision))
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
func ConfigDefaults(st *state.State, deviceCtx DeviceContext, snapName string) (map[string]interface{}, error) {
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

func MockOsutilCheckFreeSpace(mock func(path string, minSize uint64) error) (restore func()) {
	old := osutilCheckFreeSpace
	osutilCheckFreeSpace = mock
	return func() { osutilCheckFreeSpace = old }
}
