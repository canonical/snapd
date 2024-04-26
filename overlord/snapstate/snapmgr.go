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

package snapstate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/wrappers"
)

var (
	snapdTransitionDelayWithRandomess = 3*time.Hour + randutil.RandomDuration(4*time.Hour)
)

// SnapManager is responsible for the installation and removal of snaps.
type SnapManager struct {
	state   *state.State
	backend managerBackend

	autoRefresh    *autoRefresh
	refreshHints   *refreshHints
	catalogRefresh *catalogRefresh

	preseed bool

	ensuredMountsUpdated       bool
	ensuredDesktopFilesUpdated bool
	ensuredDownloadsCleaned    bool

	changeCallbackID int
}

// SnapSetup holds the necessary snap details to perform most snap manager tasks.
type SnapSetup struct {
	// FIXME: rename to RequestedChannel to convey the meaning better
	Channel string    `json:"channel,omitempty"`
	UserID  int       `json:"user-id,omitempty"`
	Base    string    `json:"base,omitempty"`
	Type    snap.Type `json:"type,omitempty"`
	// PlugsOnly indicates whether the relevant revisions for the
	// operation have only plugs (#plugs >= 0), and absolutely no
	// slots (#slots == 0).
	PlugsOnly bool `json:"plugs-only,omitempty"`

	// Version being installed/refreshed to.
	Version string `json:"version,omitempty"`

	CohortKey string `json:"cohort-key,omitempty"`

	// FIXME: implement rename of this as suggested in
	//  https://github.com/snapcore/snapd/pull/4103#discussion_r169569717
	//
	// Prereq is a list of snap-names that need to get installed
	// together with this snap. Typically used when installing
	// content-snaps with default-providers. Should be set along
	// with PrereqContentAttrs (and match its keys) for forward-compatibility.
	Prereq []string `json:"prereq,omitempty"`

	// PrereqContentAttrs maps default providers snap names to the content they provide.
	PrereqContentAttrs map[string][]string `json:"prereq-content-attrs,omitempty"`

	Flags

	SnapPath string `json:"snap-path,omitempty"`

	ExpectedProvenance string `json:"provenance,omitempty"`

	DownloadInfo *snap.DownloadInfo `json:"download-info,omitempty"`
	SideInfo     *snap.SideInfo     `json:"side-info,omitempty"`
	auxStoreInfo

	// InstanceKey is set by the user during installation and differs for
	// each instance of given snap
	InstanceKey string `json:"instance-key,omitempty"`

	// MigratedHidden is set if the user's snap dir has been migrated to
	// ~/.snap/data in the current change. So a 'false' value doesn't mean the
	// dir isn't hidden. This prevents us from always having to set it.
	MigratedHidden bool `json:"migrated-hidden,omitempty"`

	// UndidHiddenMigration is set if the migration to a hidden snap dir was undone in
	// the current change. A 'false' value doesn't mean the dir is hidden, just
	// that it wasn't exposed in this change.
	UndidHiddenMigration bool `json:"migrated-exposed,omitempty"`

	// MigratedToExposedHome is set if the ~/Snap dir was created and initialized in the
	// current change. A 'false' value doesn't that ~/Snap doesn't exist, just
	// that it wasn't create in the current change.
	MigratedToExposedHome bool `json:"migrated-exposed-home,omitempty"`

	// RemovedExposedHome is set if the ~/Snap sub directory was removed. This
	// should only happen when undoing the creation of that directory in the same
	// (failed) change. To disable usage of the exposed home in a change after it
	// was created, SnapSetup.DisableExposedHome should be used.
	RemovedExposedHome bool `json:"removed-exposed-home,omitempty"`

	// EnableExposedHome is set if the ~/Snap sub directory already exists and
	// should be used.
	EnableExposedHome bool `json:"enable-exposed-home,omitempty"`

	// DisabledExposedHome is set if ~/Snap should not be used as $HOME.
	DisableExposedHome bool `json:"disable-exposed-home,omitempty"`

	// DownloadBlobDir is the directory where the snap blob is downloaded to. If
	// empty, dir.SnapBlobDir is used.
	DownloadBlobDir string `json:"download-blob-dir,omitempty"`
}

func (snapsup *SnapSetup) InstanceName() string {
	return snap.InstanceName(snapsup.SnapName(), snapsup.InstanceKey)
}

func (snapsup *SnapSetup) SnapName() string {
	if snapsup.SideInfo.RealName == "" {
		panic("SnapSetup.SideInfo.RealName not set")
	}
	return snapsup.SideInfo.RealName
}

func (snapsup *SnapSetup) Revision() snap.Revision {
	return snapsup.SideInfo.Revision
}

func (snapsup *SnapSetup) containerInfo() snap.ContainerPlaceInfo {
	return snap.MinimalSnapContainerPlaceInfo(snapsup.InstanceName(), snapsup.Revision())
}

func (snapsup *SnapSetup) placeInfo() snap.PlaceInfo {
	return snap.MinimalPlaceInfo(snapsup.InstanceName(), snapsup.Revision())
}

// MountDir returns the path to the directory where this snap would be mounted.
func (snapsup *SnapSetup) MountDir() string {
	return snap.MountDir(snapsup.InstanceName(), snapsup.Revision())
}

// MountFile returns the path to the snap/squashfs file that is used to mount the snap.
func (snapsup *SnapSetup) MountFile() string {
	blobDir := snapsup.DownloadBlobDir
	if blobDir == "" {
		blobDir = dirs.SnapBlobDir
	}
	return snap.MountFileInDir(blobDir, snapsup.InstanceName(), snapsup.Revision())
}

// ComponentSetup holds the necessary component details to perform
// most component related tasks.
type ComponentSetup struct {
	// CompSideInfo for metadata not coming from the component
	CompSideInfo *snap.ComponentSideInfo `json:"comp-side-info,omitempty"`
	// CompType is needed as some types need special handling
	CompType snap.ComponentType
	// CompPath is the path to the file
	CompPath string `json:"comp-path,omitempty"`
}

func NewComponentSetup(csi *snap.ComponentSideInfo, compType snap.ComponentType, compPath string) *ComponentSetup {
	return &ComponentSetup{
		CompSideInfo: csi,
		CompType:     compType,
		CompPath:     compPath,
	}
}

// ComponentName returns the component name for compsu.
func (compsu *ComponentSetup) ComponentName() string {
	return compsu.CompSideInfo.Component.ComponentName
}

func (compsu *ComponentSetup) Revision() snap.Revision {
	return compsu.CompSideInfo.Revision
}

// RevertStatus is a status of a snap revert; anything other than DefaultStatus
// denotes a reverted snap revision that needs special handling in terms of
// refresh blocking.
type RevertStatus int

const (
	DefaultStatus RevertStatus = iota
	NotBlocked
)

// SnapState holds the state for a snap installed in the system.
type SnapState struct {
	SnapType string `json:"type"` // Use Type and SetType
	// Sequence contains installation side state for a snap revision and
	// related components.
	Sequence sequence.SnapSequence `json:"sequence"`

	// RevertStatus maps revisions to RevertStatus for revisions that
	// need special handling in Block().
	RevertStatus map[int]RevertStatus `json:"revert-status,omitempty"`
	Active       bool                 `json:"active,omitempty"`

	// LastActiveDisabledServices is a list of services that were disabled in
	// this snap when it was last active - i.e. when it was disabled, before
	// it was reverted, or before a refresh happens.
	// It is set during unlink-snap and unlink-current-snap and reset during
	// link-snap since it is only meant to be saved when snapd needs to remove
	// systemd units.
	// Note that to handle potential service renames, only services that exist
	// in the snap are removed from this list on link-snap, so that we can
	// remember services that were disabled in another revision and then renamed
	// or otherwise removed from the snap in a future refresh.
	LastActiveDisabledServices []string `json:"last-active-disabled-services,omitempty"`

	// tracking services enabled and disabled by hooks
	ServicesEnabledByHooks  []string `json:"services-enabled-by-hooks,omitempty"`
	ServicesDisabledByHooks []string `json:"services-disabled-by-hooks,omitempty"`

	// Current indicates the current active revision if Active is
	// true or the last active revision if Active is false
	// (usually while a snap is being operated on or disabled)
	Current         snap.Revision `json:"current"`
	TrackingChannel string        `json:"channel,omitempty"`
	Flags
	// aliases, see aliasesv2.go
	Aliases             map[string]*AliasTarget `json:"aliases,omitempty"`
	AutoAliasesDisabled bool                    `json:"auto-aliases-disabled,omitempty"`
	// AliasesPending when true indicates that aliases in internal state
	// and on disk might not match.
	AliasesPending bool `json:"aliases-pending,omitempty"`

	// UserID of the user requesting the install
	UserID int `json:"user-id,omitempty"`

	// InstanceKey is set by the user during installation and differs for
	// each instance of given snap
	InstanceKey string `json:"instance-key,omitempty"`
	CohortKey   string `json:"cohort-key,omitempty"`

	// RefreshInhibitedTime records the time when the refresh was first
	// attempted but inhibited because the snap was busy. This value is
	// reset on each successful refresh.
	RefreshInhibitedTime *time.Time `json:"refresh-inhibited-time,omitempty"`

	// LastRefreshTime records the time when the snap was last refreshed.
	LastRefreshTime *time.Time `json:"last-refresh-time,omitempty"`

	// LastRefreshTime is a map of component names to times that records
	// the time when a component was last refreshed.
	LastCompRefreshTime map[string]time.Time `json:"last-component-refresh-time,omitempty"`

	// MigratedHidden is set if the user's snap dir has been migrated
	// to ~/.snap/data.
	MigratedHidden bool `json:"migrated-hidden,omitempty"`

	// MigratedToExposedHome is set if ~/Snap was created and initialized. If set, ~/Snap
	// should be used as the snap's HOME.
	MigratedToExposedHome bool `json:"migrated-exposed-home,omitempty"`

	// PendingSecurity tracks information about snaps that have
	// their security profiles set up but are not active.
	// It is managed by ifacestate.
	PendingSecurity *PendingSecurityState `json:"pending-security,omitempty"`
}

// PendingSecurityState holds information about snaps that have
// their security profiles set up but are not active.
type PendingSecurityState struct {
	// SideInfo of the revision for which security profiles are or
	// should be set up if any.
	SideInfo *snap.SideInfo `json:"side-info,omitempty"`
}

func (snapst *SnapState) SetTrackingChannel(s string) error {
	s, err := channel.Full(s)
	if err != nil {
		return err
	}
	snapst.TrackingChannel = s
	return nil
}

// Type returns the type of the snap or an error.
// Should never error if Current is not nil.
func (snapst *SnapState) Type() (snap.Type, error) {
	if snapst.SnapType == "" {
		return snap.Type(""), fmt.Errorf("snap type unset")
	}
	return snap.Type(snapst.SnapType), nil
}

// SetType records the type of the snap.
func (snapst *SnapState) SetType(typ snap.Type) {
	snapst.SnapType = string(typ)
}

// IsInstalled returns whether the snap is installed, i.e. snapst represents an installed snap with Current revision set.
func (snapst *SnapState) IsInstalled() bool {
	if snapst.Current.Unset() {
		if len(snapst.Sequence.Revisions) > 0 {
			panic(fmt.Sprintf("snapst.Current and snapst.Sequence.Revisions out of sync: %#v %#v", snapst.Current, snapst.Sequence.Revisions))
		}

		return false
	}
	return true
}

// IsComponentInCurrentSeq returns whether a given component is present for the
// snap represented by snapst in the active or last active revision.
func (snapst *SnapState) IsComponentInCurrentSeq(cref naming.ComponentRef) bool {
	if !snapst.IsInstalled() {
		return false
	}

	idx := snapst.LastIndex(snapst.Current)
	return snapst.Sequence.ComponentSideInfoForRev(idx, cref) != nil
}

// LocalRevision returns the "latest" local revision. Local revisions
// start at -1 and are counted down.
func (snapst *SnapState) LocalRevision() snap.Revision {
	var local snap.Revision
	for _, si := range snapst.Sequence.SideInfos() {
		if si.Revision.Local() && si.Revision.N < local.N {
			local = si.Revision
		}
	}
	return local
}

// CurrentSideInfo returns the side info for the revision indicated by snapst.Current in the snap revision sequence if there is one.
func (snapst *SnapState) CurrentSideInfo() *snap.SideInfo {
	if !snapst.IsInstalled() {
		return nil
	}
	if idx := snapst.LastIndex(snapst.Current); idx >= 0 {
		return snapst.Sequence.Revisions[idx].Snap
	}
	panic("cannot find snapst.Current in the snapst.Sequence.Revisions")
}

// CurrentComponentSideInfo returns the component side info for the revision indicated by
// snapst.Current in the snap revision sequence if there is one.
func (snapst *SnapState) CurrentComponentSideInfo(cref naming.ComponentRef) *snap.ComponentSideInfo {
	if !snapst.IsInstalled() {
		return nil
	}

	if idx := snapst.LastIndex(snapst.Current); idx >= 0 {
		return snapst.Sequence.ComponentSideInfoForRev(idx, cref)
	}

	// should not really happen as the method checks if the snap is installed
	panic("cannot find snapst.Current in snapst.Sequence.Revisions")
}

func (snapst *SnapState) previousSideInfo() *snap.SideInfo {
	n := len(snapst.Sequence.Revisions)
	if n < 2 {
		return nil
	}
	// find "current" and return the one before that
	currentIndex := snapst.LastIndex(snapst.Current)
	if currentIndex <= 0 {
		return nil
	}
	return snapst.Sequence.Revisions[currentIndex-1].Snap
}

// LastIndex returns the last index of the given revision in the
// snapst.Sequence.Revisions
func (snapst *SnapState) LastIndex(revision snap.Revision) int {
	return snapst.Sequence.LastIndex(revision)
}

// IsComponentRevPresent tells us if a given component revision is
// present in the system for this snap.
func (snapst *SnapState) IsComponentRevPresent(compSi *snap.ComponentSideInfo) bool {
	return snapst.Sequence.IsComponentRevPresent(compSi)
}

// Block returns revisions that should be blocked on refreshes,
// computed from Sequence[currentRevisionIndex+1:] and considering
// special casing resulting from snapst.RevertStatus map.
func (snapst *SnapState) Block() []snap.Revision {
	// return revisions from Sequence[currentIndex:], potentially excluding
	// some of them based on RevertStatus.
	currentIndex := snapst.LastIndex(snapst.Current)
	if currentIndex < 0 || currentIndex+1 == len(snapst.Sequence.Revisions) {
		return nil
	}
	out := []snap.Revision{}
	for _, si := range snapst.Sequence.Revisions[currentIndex+1:] {
		if status, ok := snapst.RevertStatus[si.Snap.Revision.N]; ok {
			if status == NotBlocked {
				continue
			}
		}
		out = append(out, si.Snap.Revision)
	}
	return out
}

var ErrNoCurrent = errors.New("snap has no current revision")

// Retrieval functions

const (
	errorOnBroken = 1 << iota
	withAuxStoreInfo
)

var snapReadInfo = snap.ReadInfo

// AutomaticSnapshot allows to hook snapshot manager's AutomaticSnapshot.
var AutomaticSnapshot func(st *state.State, instanceName string) (ts *state.TaskSet, err error)
var AutomaticSnapshotExpiration func(st *state.State) (time.Duration, error)
var EstimateSnapshotSize func(st *state.State, instanceName string, users []string) (uint64, error)

func readInfo(name string, si *snap.SideInfo, flags int) (*snap.Info, error) {
	info, err := snapReadInfo(name, si)
	if err != nil && flags&errorOnBroken != 0 {
		return nil, err
	}
	if err != nil {
		logger.Noticef("cannot read snap info of snap %q at revision %s: %s", name, si.Revision, err)
	}
	if bse, ok := err.(snap.BrokenSnapError); ok {
		_, instanceKey := snap.SplitInstanceName(name)
		info = &snap.Info{
			SuggestedName: name,
			Broken:        bse.Broken(),
			InstanceKey:   instanceKey,
		}
		info.Apps = snap.GuessAppsForBroken(info)
		if si != nil {
			info.SideInfo = *si
		}
		err = nil
	}
	if err == nil && flags&withAuxStoreInfo != 0 {
		if err := retrieveAuxStoreInfo(info); err != nil {
			logger.Debugf("cannot read auxiliary store info for snap %q: %v", name, err)
		}
	}
	return info, err
}

var revisionDate = revisionDateImpl

// revisionDate returns a good approximation of when a revision reached the system.
func revisionDateImpl(info *snap.Info) time.Time {
	fi, err := os.Lstat(info.MountFile())
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// CurrentInfo returns the information about the current active revision or the last active revision (if the snap is inactive). It returns the ErrNoCurrent error if snapst.Current is unset.
func (snapst *SnapState) CurrentInfo() (*snap.Info, error) {
	cur := snapst.CurrentSideInfo()
	if cur == nil {
		return nil, ErrNoCurrent
	}

	name := snap.InstanceName(cur.RealName, snapst.InstanceKey)
	return readInfo(name, cur, withAuxStoreInfo)
}

// CurrentComponentInfo returns the information about the current active
// revision or the last active revision (if the component is inactive). It
// returns the ErrNoCurrent error if the component is not found.
func (snapst *SnapState) CurrentComponentInfo(cref naming.ComponentRef) (*snap.ComponentInfo, error) {
	csi := snapst.CurrentComponentSideInfo(cref)
	if csi == nil {
		return nil, ErrNoCurrent
	}

	si, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	cpi := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
		csi.Revision, si.InstanceName())
	return readComponentInfo(cpi.MountDir(), si)
}

func (snapst *SnapState) InstanceName() string {
	cur := snapst.CurrentSideInfo()
	if cur == nil {
		return ""
	}
	return snap.InstanceName(cur.RealName, snapst.InstanceKey)
}

// RefreshInhibitProceedTime is the time after which a pending refresh is forced
// for a running snap in the next auto-refresh. Zero time indicates that there
// are no pending refreshes.
//
// The provided state must be locked by the caller.
func (snapst *SnapState) RefreshInhibitProceedTime(st *state.State) time.Time {
	if snapst.RefreshInhibitedTime == nil {
		// Zero time, no pending refreshes.
		return time.Time{}
	}
	// TODO: state is needed for when configurable max inhibition
	// is introduced (i.e. "core.refresh.max-inhibition-days").
	proceedTime := snapst.RefreshInhibitedTime.Add(maxInhibition)
	return proceedTime
}

func revisionInSequence(snapst *SnapState, needle snap.Revision) bool {
	for _, si := range snapst.Sequence.SideInfos() {
		if si.Revision == needle {
			return true
		}
	}
	return false
}

type cachedStoreKey struct{}

// ReplaceStore replaces the store used by the manager.
func ReplaceStore(state *state.State, store StoreService) {
	state.Cache(cachedStoreKey{}, store)
}

func cachedStore(st *state.State) StoreService {
	ubuntuStore := st.Cached(cachedStoreKey{})
	if ubuntuStore == nil {
		return nil
	}
	return ubuntuStore.(StoreService)
}

// the store implementation has the interface consumed here
var _ StoreService = (*store.Store)(nil)

// Store returns the store service provided by the optional device context or
// the one used by the snapstate package if the former has no
// override.
func Store(st *state.State, deviceCtx DeviceContext) StoreService {
	if deviceCtx != nil {
		sto := deviceCtx.Store()
		if sto != nil {
			return sto
		}
	}
	if cachedStore := cachedStore(st); cachedStore != nil {
		return cachedStore
	}
	panic("internal error: needing the store before managers have initialized it")
}

// Manager returns a new snap manager.
func Manager(st *state.State, runner *state.TaskRunner) (*SnapManager, error) {
	preseed := snapdenv.Preseeding()
	m := &SnapManager{
		state:                      st,
		autoRefresh:                newAutoRefresh(st),
		refreshHints:               newRefreshHints(st),
		catalogRefresh:             newCatalogRefresh(st),
		preseed:                    preseed,
		ensuredMountsUpdated:       false,
		ensuredDesktopFilesUpdated: false,
		ensuredDownloadsCleaned:    false,
	}
	if preseed {
		m.backend = backend.NewForPreseedMode()
	} else {
		m.backend = backend.Backend{}
	}

	if err := os.MkdirAll(dirs.SnapCookieDir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create directory %q: %v", dirs.SnapCookieDir, err)
	}

	if err := genRefreshRequestSalt(st); err != nil {
		return nil, fmt.Errorf("cannot generate request salt: %v", err)
	}

	// this handler does nothing
	runner.AddHandler("nop", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)

	// install/update related

	// TODO: no undo handler here, we may use the GC for this and just
	// remove anything that is not referenced anymore
	runner.AddHandler("prerequisites", m.doPrerequisites, nil)
	runner.AddHandler("prepare-snap", m.doPrepareSnap, m.undoPrepareSnap)
	runner.AddHandler("download-snap", m.doDownloadSnap, m.undoPrepareSnap)
	runner.AddHandler("mount-snap", m.doMountSnap, m.undoMountSnap)
	runner.AddHandler("unlink-current-snap", m.doUnlinkCurrentSnap, m.undoUnlinkCurrentSnap)
	runner.AddHandler("copy-snap-data", m.doCopySnapData, m.undoCopySnapData)
	runner.AddCleanup("copy-snap-data", m.cleanupCopySnapData)
	runner.AddHandler("link-snap", m.doLinkSnap, m.undoLinkSnap)
	runner.AddHandler("start-snap-services", m.startSnapServices, m.undoStartSnapServices)
	runner.AddHandler("switch-snap-channel", m.doSwitchSnapChannel, nil)
	runner.AddHandler("toggle-snap-flags", m.doToggleSnapFlags, nil)
	runner.AddHandler("check-rerefresh", m.doCheckReRefresh, nil)
	runner.AddHandler("conditional-auto-refresh", m.doConditionalAutoRefresh, nil)

	// specific set-up for the kernel snap
	runner.AddHandler("prepare-kernel-snap", m.doSetupKernelSnap, m.undoSetupKernelSnap)
	runner.AddHandler("discard-old-kernel-snap-setup", m.doCleanupOldKernelSnap, m.undoCleanupOldKernelSnap)

	// FIXME: drop the task entirely after a while
	// (having this wart here avoids yet-another-patch)
	runner.AddHandler("cleanup", func(*state.Task, *tomb.Tomb) error { return nil }, nil)

	// remove related
	runner.AddHandler("stop-snap-services", m.stopSnapServices, m.undoStopSnapServices)
	runner.AddHandler("unlink-snap", m.doUnlinkSnap, m.undoUnlinkSnap)
	runner.AddHandler("clear-snap", m.doClearSnapData, nil)
	runner.AddHandler("discard-snap", m.doDiscardSnap, nil)

	// alias related
	// FIXME: drop the task entirely after a while
	runner.AddHandler("clear-aliases", func(*state.Task, *tomb.Tomb) error { return nil }, nil)
	runner.AddHandler("set-auto-aliases", m.doSetAutoAliases, m.undoRefreshAliases)
	runner.AddHandler("setup-aliases", m.doSetupAliases, m.undoSetupAliases)
	runner.AddHandler("refresh-aliases", m.doRefreshAliases, m.undoRefreshAliases)
	runner.AddHandler("prune-auto-aliases", m.doPruneAutoAliases, m.undoRefreshAliases)
	runner.AddHandler("remove-aliases", m.doRemoveAliases, m.undoRemoveAliases)
	runner.AddHandler("alias", m.doAlias, m.undoRefreshAliases)
	runner.AddHandler("unalias", m.doUnalias, m.undoRefreshAliases)
	runner.AddHandler("disable-aliases", m.doDisableAliases, m.undoRefreshAliases)
	runner.AddHandler("prefer-aliases", m.doPreferAliases, m.undoRefreshAliases)

	// misc
	runner.AddHandler("switch-snap", m.doSwitchSnap, nil)
	runner.AddHandler("migrate-snap-home", m.doMigrateSnapHome, m.undoMigrateSnapHome)
	// no undo for now since it's last task in valset auto-resolution change
	runner.AddHandler("enforce-validation-sets", m.doEnforceValidationSets, nil)
	runner.AddHandler("pre-download-snap", m.doPreDownloadSnap, nil)

	// component tasks
	runner.AddHandler("prepare-component", m.doPrepareComponent, nil)
	runner.AddHandler("mount-component", m.doMountComponent, m.undoMountComponent)
	runner.AddHandler("unlink-current-component", m.doUnlinkCurrentComponent, m.undoUnlinkCurrentComponent)
	runner.AddHandler("link-component", m.doLinkComponent, m.undoLinkComponent)
	// We cannot undo much after a component file is removed. And it is the
	// last task anyway.
	runner.AddHandler("discard-component", m.doDiscardComponent, nil)
	runner.AddHandler("prepare-kernel-modules-components", m.doSetupKernelModules, m.doRemoveKernelModulesSetup)

	// control serialisation
	runner.AddBlocked(m.blockedTask)

	RegisterAffectedSnapsByKind("conditional-auto-refresh", conditionalAutoRefreshAffectedSnaps)

	return m, nil
}

// StartUp implements StateStarterUp.Startup.
func (m *SnapManager) StartUp() error {
	writeSnapReadme()

	m.state.Lock()
	defer m.state.Unlock()
	if err := m.SyncCookies(m.state); err != nil {
		return fmt.Errorf("failed to generate cookies: %q", err)
	}

	// register handler that records a refresh-inhibit notice when
	// the set of inhibited snaps is changed.
	m.changeCallbackID = m.state.AddChangeStatusChangedHandler(processInhibitedAutoRefresh)

	return nil
}

// Stop implements StateStopper. It will unregister the change callback
// handler from state.
func (m *SnapManager) Stop() {
	st := m.state
	st.Lock()
	defer st.Unlock()

	st.RemoveChangeStatusChangedHandler(m.changeCallbackID)
}

func (m *SnapManager) CanStandby() bool {
	if n, err := NumSnaps(m.state); err == nil && n == 0 {
		return true
	}
	return false
}

func genRefreshRequestSalt(st *state.State) error {
	var refreshPrivacyKey string

	st.Lock()
	defer st.Unlock()

	if err := st.Get("refresh-privacy-key", &refreshPrivacyKey); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if refreshPrivacyKey != "" {
		// nothing to do
		return nil
	}

	refreshPrivacyKey = randutil.RandomString(16)
	st.Set("refresh-privacy-key", refreshPrivacyKey)

	return nil
}

func (m *SnapManager) blockedTask(cand *state.Task, running []*state.Task) bool {
	// Serialize "prerequisites", the state lock is not enough as
	// Install() inside doPrerequisites() will unlock to talk to
	// the store.
	if cand.Kind() == "prerequisites" {
		for _, t := range running {
			if t.Kind() == "prerequisites" {
				return true
			}
		}
	}

	return false
}

// NextRefresh returns the time the next update of the system's snaps
// will be attempted.
// The caller should be holding the state lock.
func (m *SnapManager) NextRefresh() time.Time {
	return m.autoRefresh.NextRefresh()
}

// EffectiveRefreshHold returns the time until to which refreshes are
// held if refresh.hold configuration is set.
// The caller should be holding the state lock.
func (m *SnapManager) EffectiveRefreshHold() (time.Time, error) {
	return m.autoRefresh.EffectiveRefreshHold()
}

// LastRefresh returns the time the last snap update.
// The caller should be holding the state lock.
func (m *SnapManager) LastRefresh() (time.Time, error) {
	return m.autoRefresh.LastRefresh()
}

// RefreshSchedule returns the current refresh schedule as a string suitable for
// display to a user and a flag indicating whether the schedule is a legacy one.
// The caller should be holding the state lock.
func (m *SnapManager) RefreshSchedule() (string, bool, error) {
	return m.autoRefresh.RefreshSchedule()
}

// EnsureAutoRefreshesAreDelayed will delay refreshes for the specified amount
// of time, as well as return any active auto-refresh changes that are currently
// not ready so that the client can wait for those.
func (m *SnapManager) EnsureAutoRefreshesAreDelayed(delay time.Duration) ([]*state.Change, error) {
	// always delay for at least the specified time, this ensures that even if
	// there are active refreshes right now, there won't be more auto-refreshes
	// that happen after the current set finish
	err := m.autoRefresh.ensureRefreshHoldAtLeast(delay)
	if err != nil {
		return nil, err
	}

	// look for auto refresh changes in progress
	autoRefreshChgsInFlight := []*state.Change{}
	for _, chg := range m.state.Changes() {
		if chg.Kind() == "auto-refresh" && !chg.IsReady() {
			autoRefreshChgsInFlight = append(autoRefreshChgsInFlight, chg)
		}
	}

	return autoRefreshChgsInFlight, nil
}

func (m *SnapManager) ensureVulnerableSnapRemoved(name string) error {
	// Do not do anything if we have already done this removal before on this
	// device. This is because if, after we have removed vulnerable snaps the
	// user decides to refresh to a vulnerable version of snapd, that is their
	// choice and furthermore, this removal is itself really just a last minute
	// circumvention for the issue where vulnerable snaps are left in place, we
	// do not intend to ever do this again and instead will unmount or remount
	// vulnerable old snaps as nosuid to prevent the suid snap-confine binaries
	// in them from being available to abuse for fixed vulnerabilies that are
	// not exploitable in the current versions of snapd/core snaps.
	var alreadyRemoved bool
	key := fmt.Sprintf("%s-snap-cve-2022-3328-vuln-removed", name)
	if err := m.state.Get(key, &alreadyRemoved); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if alreadyRemoved {
		return nil
	}
	var snapSt SnapState
	err := Get(m.state, name, &snapSt)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if errors.Is(err, state.ErrNoState) {
		// not installed, nothing to do
		return nil
	}

	// check if the installed, active version is fixed
	fixedVersionInstalled := false
	inactiveVulnRevisions := []snap.Revision{}
	for _, si := range snapSt.Sequence.SideInfos() {
		// check this version
		s := snap.Info{SideInfo: *si}
		ver, _, err := snapdtool.SnapdVersionFromInfoFile(filepath.Join(s.MountDir(), dirs.CoreLibExecDir))
		if err != nil {
			return err
		}
		// res is < 0 if "ver" is lower than "2.57.6"
		res, err := strutil.VersionCompare(ver, "2.57.6")
		if err != nil {
			return err
		}
		revIsVulnerable := (res < 0)
		switch {
		case !revIsVulnerable && si.Revision == snapSt.Current:
			fixedVersionInstalled = true
		case revIsVulnerable && si.Revision == snapSt.Current:
			// The active installed revision is not fixed, we can break out
			// early since we know we won't be able to remove old revisions.
			// Note that we do not attempt to refresh the snap right now, partly
			// because it may not work due to validations on the core/snapd snap
			// on some devices, but also because doing so out of band from
			// normal, controllable refresh schedules introduces non-trivial
			// load on store services and ignores user settings around refresh
			// schedules which we ought to obey as best we can.
			return nil
		case revIsVulnerable && si.Revision != snapSt.Current:
			// si revision is not fixed, but is not active, so it is a candidate
			// for removal
			inactiveVulnRevisions = append(inactiveVulnRevisions, si.Revision)
		default:
			// si revision is not active, but it is fixed, so just ignore it
		}
	}

	if !fixedVersionInstalled {
		return nil
	}

	// remove all the inactive vulnerable revisions
	for _, rev := range inactiveVulnRevisions {
		tss, err := Remove(m.state, name, rev, nil)

		if err != nil {
			// in case of conflict, just trigger another ensure in a little
			// bit and try again later
			if _, ok := err.(*ChangeConflictError); ok {
				m.state.EnsureBefore(time.Minute)
				return nil
			}
			return fmt.Errorf("cannot make task set for removing %s snap: %v", name, err)
		}

		msg := fmt.Sprintf(i18n.G("Remove inactive vulnerable %q snap (%v)"), name, rev)

		chg := m.state.NewChange("remove-snap", msg)
		chg.AddAll(tss)
		chg.Set("snap-names", []string{name})
	}

	// TODO: is it okay to set state here as done or should we do this
	// elsewhere after the change is done somehow?

	// mark state as done
	m.state.Set(key, true)

	// not strictly necessary, but does not hurt to ensure anyways
	m.state.EnsureBefore(0)

	return nil
}

func (m *SnapManager) ensureVulnerableSnapConfineVersionsRemovedOnClassic() error {
	// only remove snaps on classic
	if !release.OnClassic {
		return nil
	}

	m.state.Lock()
	defer m.state.Unlock()

	// we have to remove vulnerable versions of both the core and snapd snaps
	// only when we now have fixed versions installed / active
	// the fixed version is 2.57.6, so if the version of the current core/snapd
	// snap is that or higher, then we proceed (if we didn't already do this)

	if err := m.ensureVulnerableSnapRemoved("snapd"); err != nil {
		return err
	}

	if err := m.ensureVulnerableSnapRemoved("core"); err != nil {
		return err
	}

	return nil
}

// ensureForceDevmodeDropsDevmodeFromState undoes the forced devmode
// in snapstate for forced devmode distros.
func (m *SnapManager) ensureForceDevmodeDropsDevmodeFromState() error {
	if !sandbox.ForceDevMode() {
		return nil
	}

	m.state.Lock()
	defer m.state.Unlock()

	// int because we might want to come back and do a second pass at cleanup
	var fixed int
	if err := m.state.Get("fix-forced-devmode", &fixed); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	if fixed > 0 {
		return nil
	}

	for _, name := range []string{"core", "ubuntu-core"} {
		var snapst SnapState
		if err := Get(m.state, name, &snapst); errors.Is(err, state.ErrNoState) {
			// nothing to see here
			continue
		} else if err != nil {
			// bad
			return err
		}
		if info := snapst.CurrentSideInfo(); info == nil || info.SnapID == "" {
			continue
		}
		snapst.DevMode = false
		Set(m.state, name, &snapst)
	}
	m.state.Set("fix-forced-devmode", 1)

	return nil
}

// changeInFlight returns true if there is any change in the state
// in non-ready state.
func changeInFlight(st *state.State) bool {
	for _, chg := range st.Changes() {
		if !chg.IsReady() {
			// another change already in motion
			return true
		}
	}
	return false
}

// ensureSnapdSnapTransition will migrate systems to use the "snapd" snap
func (m *SnapManager) ensureSnapdSnapTransition() error {
	m.state.Lock()
	defer m.state.Unlock()

	// we only auto-transition people on classic systems, for core we
	// will need to do a proper re-model
	if !release.OnClassic {
		return nil
	}

	// Wait for the system to be seeded before transtioning
	var seeded bool
	err := m.state.Get("seeded", &seeded)
	if err != nil {
		if !errors.Is(err, state.ErrNoState) {
			// already seeded or other error
			return err
		}
		return nil
	}
	if !seeded {
		return nil
	}

	// check if snapd snap is installed
	var snapst SnapState
	err = Get(m.state, "snapd", &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	// nothing to do
	if snapst.IsInstalled() {
		return nil
	}

	// check if the user opts into the snapd snap
	optedIntoSnapdTransition, err := optedIntoSnapdSnap(m.state)
	if err != nil {
		return err
	}
	// nothing to do: the user does not want the snapd snap yet
	if !optedIntoSnapdTransition {
		return nil
	}

	// ensure we only transition systems that have snaps already
	installedSnaps, err := NumSnaps(m.state)
	if err != nil {
		return err
	}
	// no installed snaps (yet): do nothing (fresh classic install)
	if installedSnaps == 0 {
		return nil
	}

	// get current core snap and use same channel/user for the snapd snap
	err = Get(m.state, "core", &snapst)
	// Note that state.ErrNoState should never happen in practise. However
	// if it *does* happen we still want to fix those systems by installing
	// the snapd snap.
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	coreChannel := snapst.TrackingChannel
	// snapd/core are never blocked on auth so we don't need to copy
	// the userID from the snapst here
	userID := 0

	if changeInFlight(m.state) {
		// check that there is no change in flight already, this is a
		// precaution to ensure the snapd transition is safe
		return nil
	}

	// ensure we limit the retries in case something goes wrong
	var lastSnapdTransitionAttempt time.Time
	err = m.state.Get("snapd-transition-last-retry-time", &lastSnapdTransitionAttempt)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	now := time.Now()
	if !lastSnapdTransitionAttempt.IsZero() && lastSnapdTransitionAttempt.Add(snapdTransitionDelayWithRandomess).After(now) {
		return nil
	}
	m.state.Set("snapd-transition-last-retry-time", now)

	var retryCount int
	err = m.state.Get("snapd-transition-retry", &retryCount)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	m.state.Set("snapd-transition-retry", retryCount+1)

	ts, err := Install(context.Background(), m.state, "snapd", &RevisionOptions{Channel: coreChannel}, userID, Flags{})
	if err != nil {
		return err
	}

	msg := i18n.G("Transition to the snapd snap")
	chg := m.state.NewChange("transition-to-snapd-snap", msg)
	chg.AddAll(ts)

	return nil
}

// ensureUbuntuCoreTransition will migrate systems that use "ubuntu-core"
// to the new "core" snap
func (m *SnapManager) ensureUbuntuCoreTransition() error {
	m.state.Lock()
	defer m.state.Unlock()

	var snapst SnapState
	err := Get(m.state, "ubuntu-core", &snapst)
	if errors.Is(err, state.ErrNoState) {
		return nil
	}
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	// Wait for the system to be seeded before transtioning
	var seeded bool
	err = m.state.Get("seeded", &seeded)
	if err != nil {
		if !errors.Is(err, state.ErrNoState) {
			// already seeded or other error
			return err
		}
		return nil
	}
	if !seeded {
		return nil
	}

	// check that there is no change in flight already, this is a
	// precaution to ensure the core transition is safe
	if changeInFlight(m.state) {
		// another change already in motion
		return nil
	}

	// ensure we limit the retries in case something goes wrong
	var lastUbuntuCoreTransitionAttempt time.Time
	err = m.state.Get("ubuntu-core-transition-last-retry-time", &lastUbuntuCoreTransitionAttempt)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	now := time.Now()
	if !lastUbuntuCoreTransitionAttempt.IsZero() && lastUbuntuCoreTransitionAttempt.Add(6*time.Hour).After(now) {
		return nil
	}

	tss, trErr := TransitionCore(m.state, "ubuntu-core", "core")
	if _, ok := trErr.(*ChangeConflictError); ok {
		// likely just too early, retry at next Ensure
		return nil
	}

	m.state.Set("ubuntu-core-transition-last-retry-time", now)

	var retryCount int
	err = m.state.Get("ubuntu-core-transition-retry", &retryCount)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	m.state.Set("ubuntu-core-transition-retry", retryCount+1)

	if trErr != nil {
		return trErr
	}

	msg := i18n.G("Transition ubuntu-core to core")
	chg := m.state.NewChange("transition-ubuntu-core", msg)
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	return nil
}

// atSeed implements at seeding policy for refreshes.
func (m *SnapManager) atSeed() error {
	m.state.Lock()
	defer m.state.Unlock()
	var seeded bool
	err := m.state.Get("seeded", &seeded)
	if !errors.Is(err, state.ErrNoState) {
		// already seeded or other error
		return err
	}
	if err := m.autoRefresh.AtSeed(); err != nil {
		return err
	}
	if err := m.refreshHints.AtSeed(); err != nil {
		return err
	}
	return nil
}

var (
	localInstallCleanupWait = time.Duration(24 * time.Hour)
	localInstallLastCleanup time.Time
)

// localInstallCleanup removes files that might've been left behind by an
// old aborted local install.
//
// They're usually cleaned up, but if they're created and then snapd
// stops before writing the change to disk (killed, light cut, etc)
// it'll be left behind.
//
// The code that creates the files is in daemon/api.go's postSnaps
func (m *SnapManager) localInstallCleanup() error {
	m.state.Lock()
	defer m.state.Unlock()

	now := timeNow()
	cutoff := now.Add(-localInstallCleanupWait)
	if localInstallLastCleanup.After(cutoff) {
		return nil
	}
	localInstallLastCleanup = now

	d, err := os.Open(dirs.SnapBlobDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer d.Close()

	var filenames []string
	var fis []os.FileInfo
	for err == nil {
		// TODO: if we had fstatat we could avoid a bunch of stats
		fis, err = d.Readdir(100)
		// fis is nil if err isn't
		for _, fi := range fis {
			name := fi.Name()
			if !strings.HasPrefix(name, dirs.LocalInstallBlobTempPrefix) {
				continue
			}
			if fi.ModTime().After(cutoff) {
				continue
			}
			filenames = append(filenames, name)
		}
	}
	if err != io.EOF {
		return err
	}
	return osutil.UnlinkManyAt(d, filenames)
}

func MockEnsuredMountsUpdated(m *SnapManager, ensured bool) (restore func()) {
	osutil.MustBeTestBinary("ensured snap mounts can only be mocked from tests")
	old := m.ensuredMountsUpdated
	m.ensuredMountsUpdated = ensured
	return func() {
		m.ensuredMountsUpdated = old
	}
}

func getSystemD() systemd.Systemd {
	if snapdenv.Preseeding() {
		return systemd.NewEmulationMode(dirs.GlobalRootDir)
	} else {
		return systemd.New(systemd.SystemMode, nil)
	}
}

func (m *SnapManager) ensureMountsUpdated() error {
	m.state.Lock()
	defer m.state.Unlock()

	if m.ensuredMountsUpdated {
		return nil
	}

	// only run after we are seeded
	var seeded bool
	err := m.state.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !seeded {
		return nil
	}

	allStates, err := All(m.state)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	if len(allStates) != 0 {
		sysd := getSystemD()

		for _, snapSt := range allStates {
			info, err := snapSt.CurrentInfo()
			if err != nil {
				return err
			}
			dev, err := DeviceCtx(m.state, nil, nil)
			// Ignore error if model assertion not yet known
			if err != nil && !errors.Is(err, state.ErrNoState) {
				return err
			}
			squashfsPath := dirs.StripRootDir(info.MountFile())
			whereDir := dirs.StripRootDir(info.MountDir())
			// Ensure mount files, but do not restart mount units
			// of snap files if the units are modified as services
			// in the snap have a Requires= on them. Otherwise the
			// services would be restarted.
			//   This is especially relevant for the snapd snap as if
			// this happens, it would end up in a bad state after
			// an update.
			// TODO Ensure mounts of snap components as well
			// TODO refactor so the check for kernel type is not repeated
			// in the installation case
			snapType, _ := snapSt.Type()
			// We cannot ensure for this type yet as the mount unit
			// flags depend on the model in this case.
			if snapType == snap.TypeKernel && dev == nil {
				continue
			}
			if _, err = sysd.EnsureMountUnitFile(info.MountDescription(),
				squashfsPath, whereDir, "squashfs",
				systemd.EnsureMountUnitFlags{
					PreventRestartIfModified: true,
					// We need early mounts only for UC20+/hybrid, also 16.04
					// systemd seems to be buggy if we enable this.
					StartBeforeDriversLoad: snapType == snap.TypeKernel &&
						dev.HasModeenv()}); err != nil {
				return err
			}
		}
	}

	m.ensuredMountsUpdated = true

	return nil
}

func (m *SnapManager) ensureDesktopFilesUpdated() error {
	m.state.Lock()
	defer m.state.Unlock()

	if m.ensuredDesktopFilesUpdated {
		return nil
	}

	// only run after we are seeded
	var seeded bool
	err := m.state.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !seeded {
		return nil
	}

	allStates, err := All(m.state)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	var snaps []*snap.Info
	for _, snapSt := range allStates {
		info, err := snapSt.CurrentInfo()
		if err != nil {
			return err
		}
		snaps = append(snaps, info)
	}
	if err := wrappers.EnsureSnapDesktopFiles(snaps); err != nil {
		return err
	}

	m.ensuredDesktopFilesUpdated = true

	return nil
}

func (m *SnapManager) ensureDownloadsCleaned() error {
	m.state.Lock()
	defer m.state.Unlock()

	if m.ensuredDownloadsCleaned {
		return nil
	}

	// only run after we are seeded
	var seeded bool
	err := m.state.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !seeded {
		return nil
	}

	if err := cleanDownloads(m.state); err != nil {
		return err
	}

	m.ensuredDownloadsCleaned = true

	return nil
}

// Ensure implements StateManager.Ensure.
func (m *SnapManager) Ensure() error {
	if m.preseed {
		return nil
	}

	// do not exit right away on error
	errs := []error{
		m.atSeed(),
		m.ensureAliasesV2(),
		m.ensureForceDevmodeDropsDevmodeFromState(),
		m.ensureUbuntuCoreTransition(),
		m.ensureSnapdSnapTransition(),
		// we should check for full regular refreshes before
		// considering issuing a hint only refresh request
		m.autoRefresh.Ensure(),
		m.refreshHints.Ensure(),
		m.catalogRefresh.Ensure(),
		m.localInstallCleanup(),
		m.ensureVulnerableSnapConfineVersionsRemovedOnClassic(),
		m.ensureMountsUpdated(),
		m.ensureDesktopFilesUpdated(),
		m.ensureDownloadsCleaned(),
	}

	//FIXME: use firstErr helper
	for _, e := range errs {
		if e != nil {
			return e
		}
	}

	return nil
}
