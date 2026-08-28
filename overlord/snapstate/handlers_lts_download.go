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

package snapstate

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	snapchannel "github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/ltstrack"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

// maybeRedirectSnapdToLTSTrack inspects the candidate snapd LTS track map.
// If this device's UC base must use a different channel than planned, it
// fetches that snap onto the revision-named blob path and rewrites snapsup
// in place so later tasks see the corrected setup without mutating the
// task graph. The caller persists snap-setup and then removes the discarded
// first download.
//
// ignoreChangeID is this download's change so an exclusive-downgrade check
// does not conflict with the change we are already running.
//
// Failure modes:
//   - candidate map unreadable: return error, fail the download task.
//     Do not link a snapd whose LTS policy could not be verified; that
//     snapd may mutate device state. A later snapd update that carries
//     a readable map can retry the change. An empty or missing map is
//     not unreadable: no mapping applies yet, so there is no redirect.
//   - store action to resolve the LTS revision fails: return error, fail the download task.
//   - LTS version is older than the installed snapd and other changes are
//     in progress: skip the jump, keep the latest candidate, log, return nil.
//   - download of that LTS revision fails: return error, fail the download task.
func maybeRedirectSnapdToLTSTrack(
	ctx context.Context,
	st *state.State,
	snapsup *SnapSetup,
	deviceCtx DeviceContext,
	theStore StoreService,
	user *auth.UserState,
	meter progress.Meter,
	dlOpts *store.DownloadOptions,
	perfTimings timings.Measurer,
	ignoreChangeID string,
) error {
	if !needsSnapdLTSTrackResolve(snapsup) {
		return nil
	}
	if deviceCtx == nil || deviceCtx.Model() == nil {
		return fmt.Errorf("cannot inspect snapd LTS tracks after download: no device model")
	}
	model := deviceCtx.Model()
	applies, err := snapdLTSPolicyApplies(model)
	if err != nil {
		return err
	}
	if !applies {
		return nil
	}

	targetChannel, err := inspectSnapdLTSAfterDownload(snapsup, model, snapsup.SnapPath)
	if err != nil {
		return err
	}
	if targetChannel == "" {
		return nil
	}
	fromChannel := snapsup.Channel

	sar, err := resolveSnapdLTSRevision(ctx, st, snapsup, targetChannel, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot resolve snapd LTS redirect to channel %q: %v",
			targetChannel, err)
	}

	st.Lock()
	skip, err := skipSnapdLTSRedirectForExclusiveDowngrade(st, sar, ignoreChangeID)
	st.Unlock()
	if err != nil {
		return err
	}
	if skip {
		logger.Noticef("snapd LTS redirect skipped: other changes in progress, staying on %q",
			fromChannel)
		return nil
	}

	meter.Notify(fmt.Sprintf("Switching snapd from channel %q to LTS track %q for this device",
		fromChannel, targetChannel))
	logger.Noticef("snapd LTS redirect: channel %q requires %q for this device, downloading LTS target",
		fromChannel, targetChannel)

	// Download the LTS target under the new revision's blob name so the
	// filename matches the revision later tasks and undo/cleanup will use.
	// The first download is removed by the caller after snap-setup is
	// persisted, so a crash cannot leave state pointing at a deleted blob.
	targetPath := snapSetupBlobPathForRevision(snapsup, sar.SideInfo.Revision)
	var dlErr error
	timings.Run(perfTimings, "download-lts-target",
		fmt.Sprintf("download snap %q on LTS track %q",
			snapsup.SnapName(), targetChannel),
		func(timings.Measurer) {
			dlErr = theStore.Download(ctx, snapsup.SnapName(), targetPath,
				&sar.DownloadInfo, meter, user, dlOpts)
		})
	if dlErr != nil {
		return fmt.Errorf("cannot download snapd LTS target on channel %q: %v",
			targetChannel, dlErr)
	}
	applySnapdLTSRedirectSetup(snapsup, sar, targetChannel, targetPath)

	meter.Notify(fmt.Sprintf("snapd redirected to LTS track %q (revision %s)",
		targetChannel, sar.SideInfo.Revision))
	logger.Noticef("snapd LTS redirect complete: rev %s on channel %q (was %q)",
		sar.SideInfo.Revision, targetChannel, fromChannel)
	return nil
}

// skipSnapdLTSRedirectForExclusiveDowngrade reports whether a latest→older-LTS
// jump must be skipped because other changes are in progress. ignoreChangeID
// is this download's change so the check does not conflict with itself.
// The state lock must be held.
func skipSnapdLTSRedirectForExclusiveDowngrade(st *state.State, sar store.SnapActionResult, ignoreChangeID string) (bool, error) {
	var snapst SnapState
	err := Get(st, "snapd", &snapst)
	if errors.Is(err, state.ErrNoState) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !snapst.IsInstalled() {
		return false, nil
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return false, fmt.Errorf("cannot retrieve snap info for current snapd: %v", err)
	}

	// Empty LTS version is treated as a downgrade, matching
	// changeIsSnapdDowngrade when Version was not persisted.
	if sar.Version != "" {
		res, err := strutil.VersionCompare(info.Version, sar.Version)
		if err != nil {
			return false, fmt.Errorf("cannot compare versions of snapd [cur: %s, new: %s]: %v", info.Version, sar.Version, err)
		}
		if res != 1 {
			return false, nil
		}
	}

	err = checkChangeConflictExclusiveKinds(st, "snapd downgrade", ignoreChangeID)
	if err == nil {
		return false, nil
	}
	if errors.Is(err, &ChangeConflictError{}) {
		return true, nil
	}
	return false, err
}

// applySnapdLTSRedirectSetup re-plans snapsup for the LTS revision after that
// blob has been downloaded. It copies the SnapSetup fields that
// target.setups() would have set from an LTS store result, so later tasks
// see the same revision metadata as if that revision had been planned.
//
// Copied from the LTS SnapActionResult / new blob path (same sources as
// setups()): SideInfo, DownloadInfo, Channel, ExpectedProvenance
// (Info.SnapProvenance, not Info.Provenance()), SnapPath, Version,
// IntegrityDataInfo, Base, AuxStoreInfo (Media, StoreURL, Website()).
//
// Left as planned: PlugsOnly (store yaml has no slots; implicit slots are
// not on the SAR), PluggedConfdbIDs (snapd does not plug confdb; implicit
// confdb is a slot), Flags, Type, UserID, CohortKey, IsExplicitChannel /
// IsExplicitRevision, ValidationSets, InstanceKey, DownloadBlobDir, Prereq*,
// home-migration flags, kernel/component fields. The icon file is SnapID-keyed
// and already downloaded; it is not re-fetched. The dm-verity sidecar is not
// downloaded here (doDownloadSnap does not fetch it either).
func applySnapdLTSRedirectSetup(snapsup *SnapSetup, sar store.SnapActionResult, targetChannel, targetPath string) {
	snapsup.SideInfo = &sar.SideInfo
	snapsup.DownloadInfo = &sar.DownloadInfo
	snapsup.Channel = targetChannel
	snapsup.ExpectedProvenance = sar.SnapProvenance
	snapsup.SnapPath = targetPath
	snapsup.Version = sar.Version
	snapsup.IntegrityDataInfo = sar.IntegrityData
	snapsup.Base = sar.Base
	snapsup.AuxStoreInfo = backend.AuxStoreInfo{
		Media:    sar.Media,
		StoreURL: sar.StoreURL,
		Website:  sar.Website(),
	}
}

// resolveSnapdLTSRevision asks the store for the current revision on
// targetChannel, honouring the cohort key and this operation's validation sets.
// deviceCtx selects the store (remodel new-store vs cached default).
func resolveSnapdLTSRevision(ctx context.Context, st *state.State, snapsup *SnapSetup, targetChannel string, deviceCtx DeviceContext) (store.SnapActionResult, error) {
	// Honour this operation's validation sets unless it ignored
	// validation. Monitor and forgotten sets are not consulted: the same
	// gate as install/refresh. Planned keys on snap-setup are the
	// operation snapshot (remodel, explicit RevOpts, plan-time enforced
	// sets). Missing keys (old in-flight tasks) fall back to currently
	// enforced sets.
	var vsets *snapasserts.ValidationSets
	action := "install"
	if snapsup.IgnoreValidation {
		vsets = snapasserts.NewValidationSets()
	} else {
		st.Lock()
		var snapst SnapState
		if err := Get(st, snapsup.InstanceName(), &snapst); err != nil && !errors.Is(err, state.ErrNoState) {
			st.Unlock()
			return store.SnapActionResult{}, err
		}
		if snapst.IsInstalled() {
			action = "refresh"
		}
		var err error
		switch {
		case snapsup.ValidationSets == nil:
			vsets, err = EnforcedValidationSets(st)
		case len(snapsup.ValidationSets) == 0:
			vsets = snapasserts.NewValidationSets()
		default:
			vsets, err = ValidationSetsFromKeys(st, snapsup.ValidationSets)
		}
		st.Unlock()
		if err != nil {
			return store.SnapActionResult{}, err
		}
	}

	pres, err := vsets.Presence(naming.Snap(snapsup.SnapName()))
	if err != nil {
		return store.SnapActionResult{}, err
	}
	if err := checkSnapAgainstConstraints(snapsup.InstanceName(), snap.Revision{}, pres, action); err != nil {
		return store.SnapActionResult{}, err
	}

	storeVsets := vsets
	if !pres.Revision.Unset() {
		// A pin would make completeStoreAction drop the channel and fetch
		// that blob without asking whether it is on the LTS track. Resolve
		// the track without the pin, then require an exact match.
		storeVsets = snapasserts.NewValidationSets()
	}

	// Leave revision empty so the store selects one on that channel. Keep
	// the cohort key: the store returns the cohort-frozen revision or errors.
	sar, err := sendOneInstallActionUnlocked(ctx, st, StoreSnap{
		InstanceName: snapsup.InstanceName(),
		RevOpts: RevisionOptions{
			Channel:        targetChannel,
			CohortKey:      snapsup.CohortKey,
			ValidationSets: storeVsets,
		},
	}, Options{
		Flags:     Flags{IgnoreValidation: snapsup.IgnoreValidation},
		DeviceCtx: deviceCtx,
	})
	if err != nil {
		return store.SnapActionResult{}, err
	}
	if !pres.Revision.Unset() && sar.SideInfo.Revision != pres.Revision {
		return store.SnapActionResult{}, fmt.Errorf("cannot get revision %s required by validation sets from that track (got %s)",
			pres.Revision, sar.SideInfo.Revision)
	}
	return sar, nil
}

// needsSnapdLTSTrackResolve reports whether LTS track resolution should run
// after a snapd store download. Only operational gating lives here.
func needsSnapdLTSTrackResolve(snapsup *SnapSetup) bool {
	if snapsup == nil || snapsup.Type != snap.TypeSnapd {
		return false
	}
	// Explicit --channel= / --revision= win over LTS redirect.
	if snapsup.IsExplicitChannel || snapsup.IsExplicitRevision {
		return false
	}
	// Empty SnapID means unasserted snap (sideloaded or from a local path);
	// LTS track redirect only applies to store-fetched snaps.
	if snapsup.SideInfo == nil || snapsup.SideInfo.SnapID == "" {
		return false
	}
	// The redirect rewrites snap-setup after prerequisites has already run
	// against the planned revision's metadata. This is only safe when snapd
	// has no prerequisites — TypeSnapd is an unconditional no-op in
	// doPrerequisites today. If that ever changes (snapd gains a base or
	// content plug), the planned prereqs may not match the LTS-target's
	// prereqs. Skip the redirect rather than link snapd with unsatisfied
	// prerequisites.
	if len(snapsup.Prereq) > 0 || len(snapsup.PrereqContentAttrs) > 0 {
		return false
	}
	return true
}

// snapdLTSPolicyApplies reports whether this model can use snapd LTS track
// policy. Classic and UC16 cannot: there is no separate snapd snap to map.
// Callers must still fail closed on a nil model.
func snapdLTSPolicyApplies(model *asserts.Model) (bool, error) {
	if model.Classic() {
		return false, nil
	}
	bootBase, err := model.CoreVersion()
	if err != nil {
		return false, err
	}
	if bootBase == 16 {
		return false, nil
	}
	return true, nil
}

// inspectSnapdLTSAfterDownload returns the LTS channel the candidate requires,
// or empty if no redirect is needed. An unreadable candidate map is an error.
// ErrNoTrack, ErrNotAllowed, and ErrBootBaseNotManaged (including an empty
// or missing map) are not: no redirect.
func inspectSnapdLTSAfterDownload(snapsup *SnapSetup, model *asserts.Model, blobPath string) (string, error) {
	// An empty channel is not a missing map: there is no planned track to
	// rewrite. Skipping here avoids failing downloads whose setup never
	// resolved a channel (refresh-all with empty tracking).
	if snapsup.Channel == "" {
		return "", nil
	}

	parsed, err := snapchannel.ParseVerbatim(snapsup.Channel, "-")
	if err != nil {
		return "", fmt.Errorf("cannot parse download channel: %v", err)
	}

	candidate := squashfs.New(blobPath)
	targetChannel, err := ltstrack.Resolve(model, snapsup.Channel, candidate)
	if err != nil {
		if errors.Is(err, ltstrack.ErrNoTrack) || errors.Is(err, ltstrack.ErrNotAllowed) || errors.Is(err, ltstrack.ErrBootBaseNotManaged) {
			return "", nil
		}
		return "", err
	}

	if targetChannel == parsed.Clean().String() {
		return "", nil
	}
	return targetChannel, nil
}

// snapSetupBlobPathForRevision is BlobPath for a revision other than the one
// currently in snapsup. SideInfo is left unchanged.
func snapSetupBlobPathForRevision(snapsup *SnapSetup, rev snap.Revision) string {
	blobDir := snapsup.DownloadBlobDir
	if blobDir == "" {
		blobDir = dirs.SnapBlobDir
	}
	return snap.MountFileInDir(blobDir, snapsup.InstanceName(), rev)
}

// removeDiscardedSnapdDownload removes the first download after a successful
// LTS redirect. Same-revision targets keep the file: the LTS download wrote
// onto that path.
func removeDiscardedSnapdDownload(discardPath, targetPath string) {
	if discardPath == "" || discardPath == targetPath {
		return
	}
	if err := os.Remove(discardPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Noticef("cannot remove discarded snapd download %q after LTS redirect: %v", discardPath, err)
	}
}
