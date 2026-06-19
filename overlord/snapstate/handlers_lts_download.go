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
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	snapchannel "github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/ltschannel"
	"github.com/snapcore/snapd/snap/squashfs"
)

type snapdLTSInspectResult struct {
	remapNeeded    bool
	currentChannel string
	targetChannel  string
}

// needsSnapdLTSChannelResolve reports whether LTS channel resolution should run
// after a snapd store download. Only operational gating lives here.
func needsSnapdLTSChannelResolve(snapsup *SnapSetup, model *asserts.Model) bool {
	if snapsup == nil || snapsup.Type != snap.TypeSnapd {
		return false
	}
	if snapsup.SideInfo == nil || snapsup.SideInfo.SnapID == "" {
		return false
	}
	return model != nil
}

func inspectSnapdLTSAfterDownload(snapsup *SnapSetup, model *asserts.Model, blobPath string) (snapdLTSInspectResult, error) {
	currentChannel, err := cleanedSnapChannel(snapsup.Channel)
	if err != nil {
		return snapdLTSInspectResult{}, fmt.Errorf("cannot parse download channel: %v", err)
	}

	targetChannel, err := ltschannel.SnapdLTSChannel(model, snapsup.Channel, squashfs.New(blobPath))
	if err != nil {
		return snapdLTSInspectResult{}, fmt.Errorf("cannot resolve LTS channel: %v", err)
	}

	result := snapdLTSInspectResult{
		currentChannel: currentChannel,
		targetChannel:  targetChannel,
	}
	result.remapNeeded = snapChannelsDiffer(targetChannel, currentChannel)
	return result, nil
}

func maybeInspectSnapdLTSAfterDownload(snapsup *SnapSetup, model *asserts.Model, blobPath string) {
	if !needsSnapdLTSChannelResolve(snapsup, model) {
		return
	}

	result, err := inspectSnapdLTSAfterDownload(snapsup, model, blobPath)
	if err != nil {
		logger.Noticef("cannot inspect snapd LTS tracks after download: %v", err)
		return
	}
	if result.remapNeeded {
		logger.Noticef("snapd LTS channel remap needed after download: have %q, require %q",
			result.currentChannel, result.targetChannel)
	}
}

func cleanedSnapChannel(channel string) (string, error) {
	parsed, err := snapchannel.ParseVerbatim(channel, "-")
	if err != nil {
		return "", err
	}
	return parsed.Clean().String(), nil
}

func snapChannelsDiffer(a, b string) bool {
	cleanA, err := cleanedSnapChannel(a)
	if err != nil {
		return true
	}
	cleanB, err := cleanedSnapChannel(b)
	if err != nil {
		return true
	}
	return cleanA != cleanB
}
