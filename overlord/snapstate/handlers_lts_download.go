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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	snapchannel "github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/ltschannel"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/timings"
)

type snapdLTSInspectResult struct {
	remapNeeded    bool
	currentChannel string
	targetChannel  string
}

// maybeRedirectSnapdToLTSChannel inspects the candidate snapd squashfs for
// the LTS track map. If the map says the device's UC base must land on a
// different channel than planned, it issues a second store action on the LTS
// channel, downloads the correct snap over the same blob path, and rewrites
// snapsup (SideInfo, DownloadInfo, Channel) in place so downstream tasks
// (validate-snap, mount-snap, link-snap) see the corrected setup without any
// task-graph mutation.
//
// Failure modes:
//   - candidate map missing or unreadable: log and pass through (v1 policy).
//   - second store action fails: return error, fail the download task.
//   - download of LTS target fails: return error, fail the download task.
func maybeRedirectSnapdToLTSChannel(
	ctx context.Context,
	st *state.State,
	snapsup *SnapSetup,
	model *asserts.Model,
	theStore StoreService,
	user *auth.UserState,
	meter progress.Meter,
	dlOpts *store.DownloadOptions,
	perfTimings timings.Measurer,
) error {
	if !needsSnapdLTSChannelResolve(snapsup, model) {
		return nil
	}

	inspected, err := inspectSnapdLTSAfterDownload(snapsup, model, snapsup.SnapPath)
	if err != nil {
		// Cannot read the LTS track map from the candidate snap (missing key,
		// parse failure, etc.). Log and pass through: channel validation at
		// planning time remains the only guard for this refresh cycle.
		logger.Noticef("cannot inspect snapd LTS tracks after download: %v", err)
		return nil
	}
	if !inspected.remapNeeded {
		return nil
	}

	// Honour validation sets so a pinned revision constraint is preserved on
	// the LTS track. If the pinned revision is not on the LTS track the store
	// will return an error, surfacing a "validation set conflicts with LTS
	// policy" condition to the operator.
	// TODO: This should be captured in the spec.
	st.Lock()
	vsets, err := EnforcedValidationSets(st)
	st.Unlock()
	if err != nil {
		return fmt.Errorf("cannot get validation sets for snapd LTS redirect: %v", err)
	}

	// Second store action on the LTS channel. Leave RevOpts.Revision empty to
	// select the latest revision on the LTS track. Drop the cohort key: it was
	// associated with the original channel and is not valid on the LTS channel.
	sar, err := sendOneInstallActionUnlocked(ctx, st, StoreSnap{
		InstanceName: snapsup.InstanceName(),
		RevOpts: RevisionOptions{
			Channel:        inspected.targetChannel,
			ValidationSets: vsets,
		},
	}, Options{})
	if err != nil {
		return fmt.Errorf("cannot resolve snapd LTS redirect to channel %q: %v",
			inspected.targetChannel, err)
	}

	meter.Notify(fmt.Sprintf("Switching snapd from channel %q to LTS channel %q for this device",
		inspected.currentChannel, inspected.targetChannel))
	logger.Noticef("snapd LTS redirect: channel %q requires %q for this device, downloading LTS target",
		inspected.currentChannel, inspected.targetChannel)

	// Download the LTS target to the same blob path, overwriting the first
	// download. doDownloadSnap already does a second store action + download
	// when DownloadInfo is missing from state (tasks written by older snapd
	// that predates storing DownloadInfo), so issuing a second action and
	// download here is an established pattern in this handler.
	var dlErr error
	timings.Run(perfTimings, "download-lts-target",
		fmt.Sprintf("download snap %q on LTS channel %q",
			snapsup.SnapName(), inspected.targetChannel),
		func(timings.Measurer) {
			dlErr = theStore.Download(ctx, snapsup.SnapName(), snapsup.SnapPath,
				&sar.DownloadInfo, meter, user, dlOpts)
		})
	if dlErr != nil {
		return fmt.Errorf("cannot download snapd LTS target on channel %q: %v",
			inspected.targetChannel, dlErr)
	}

	// Pre-check the LTS target's patch level before rewriting snap-setup so
	// that an incompatible frozen revision is refused here rather than after
	// a daemon restart via snap-failure revert.
	if err := checkSnapdLTSTargetPatchLevel(st, snapsup.SnapPath, inspected.targetChannel); err != nil {
		return err
	}

	// Rewrite snap-setup in place; the caller persists it.
	snapsup.SideInfo = &sar.SideInfo
	snapsup.DownloadInfo = &sar.DownloadInfo
	snapsup.Channel = inspected.targetChannel
	snapsup.ExpectedProvenance = sar.SnapProvenance

	meter.Notify(fmt.Sprintf("snapd redirected to LTS channel %q (revision %s)",
		inspected.targetChannel, sar.SideInfo.Revision))
	logger.Noticef("snapd LTS redirect complete: rev %s on channel %q (was %q)",
		sar.SideInfo.Revision, inspected.targetChannel, inspected.currentChannel)
	return nil
}

// needsSnapdLTSChannelResolve reports whether LTS channel resolution should run
// after a snapd store download. Only operational gating lives here.
func needsSnapdLTSChannelResolve(snapsup *SnapSetup, model *asserts.Model) bool {
	if snapsup == nil || snapsup.Type != snap.TypeSnapd {
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
	return model != nil
}

func inspectSnapdLTSAfterDownload(snapsup *SnapSetup, model *asserts.Model, blobPath string) (snapdLTSInspectResult, error) {
	parsed, err := snapchannel.ParseVerbatim(snapsup.Channel, "-")
	if err != nil {
		return snapdLTSInspectResult{}, fmt.Errorf("cannot parse download channel: %v", err)
	}
	currentChannel := parsed.Clean().String()

	targetChannel, err := ltschannel.SnapdLTSChannel(model, snapsup.Channel, squashfs.New(blobPath))
	if err != nil {
		if errors.Is(err, ltschannel.ErrLTSBaseNotManaged) {
			// Base has no LTS policy yet; no redirect needed.
			return snapdLTSInspectResult{remapNeeded: false}, nil
		}
		return snapdLTSInspectResult{}, fmt.Errorf("cannot resolve LTS channel: %v", err)
	}

	// Both currentChannel and targetChannel are already canonicalised via
	// Clean().String(), so direct string comparison is sufficient.
	return snapdLTSInspectResult{
		currentChannel: currentChannel,
		targetChannel:  targetChannel,
		remapNeeded:    targetChannel != currentChannel,
	}, nil
}

// checkSnapdLTSTargetPatchLevel reads the patch level from the candidate snapd
// blob and compares it against the state's current patch level. If the state
// is ahead of the LTS target (i.e. the target would be refused at daemon
// start), the error is returned here so the download task fails early without
// a daemon restart attempt.
//
// If the blob carries no SNAPD_PATCH_LEVEL key (older snap without the key),
// the check is skipped and the existing daemon-start safety net applies.
func checkSnapdLTSTargetPatchLevel(st *state.State, blobPath, targetChannel string) error {
	targetLevel, targetVersion, err := snap.SnapdPatchLevelFromSnapFile(squashfs.New(blobPath))
	if err != nil {
		// Cannot parse patch level; proceed and let the daemon-start check handle it.
		logger.Noticef("snapd LTS redirect: cannot read patch level from target blob, proceeding: %v", err)
		return nil
	}
	if targetLevel == 0 {
		// Key absent in older snap; cannot check.
		return nil
	}

	st.Lock()
	var stateLevel int
	stateErr := st.Get("patch-level", &stateLevel)
	st.Unlock()
	if errors.Is(stateErr, state.ErrNoState) || stateErr != nil {
		// No patch level recorded in state yet; nothing to compare against.
		return nil
	}

	if stateLevel > targetLevel {
		return fmt.Errorf("cannot redirect snapd to LTS channel %q: "+
			"target version %s patch level %d is incompatible with "+
			"current state patch level %d",
			targetChannel, targetVersion, targetLevel, stateLevel)
	}
	return nil
}
