// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package gadget

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

func splitType(maybeHybridType string) (mbrTypeID, gptTypeID string) {
	switch {
	case validTypeID.MatchString(maybeHybridType):
		// only MBR type
		return maybeHybridType, ""
	case validGUUID.MatchString(maybeHybridType):
		// only GPT type
		return "", maybeHybridType
	}
	idx := strings.IndexRune(maybeHybridType, ',')
	if idx == -1 {
		return "", ""
	}
	// mbr,GUUID
	return maybeHybridType[:idx], maybeHybridType[idx+1:]
}

func Partition(image string, pv *LaidOutVolume) error {
	if image == "" {
		return fmt.Errorf("internal error: image path is unset")
	}
	if pv.SectorSize != 512 {
		// check for unsupported sector size
		return fmt.Errorf("cannot use sector size %v", pv.SectorSize)
	}

	asSector := func(v Size) Size {
		return v / pv.SectorSize
	}

	script := &bytes.Buffer{}
	// only sector unit is supported
	fmt.Fprintf(script, "unit: sectors\n")
	switch pv.EffectiveSchema() {
	case GPT:
		fmt.Fprintf(script, "label: gpt\n")
		fmt.Fprintf(script, "first-lba: 34\n")
	case MBR:
		fmt.Fprintf(script, "label: dos\n")
	}
	if pv.ID != "" {
		fmt.Fprintf(script, "label-id: %v\n", pv.ID)
	}
	fmt.Fprintf(script, "\n")

	for _, ps := range pv.LaidOutStructure {
		if ps.Type == "bare" || ps.Type == "mbr" {
			continue
		}

		start := asSector(ps.StartOffset)
		size := asSector(ps.Size)
		fmt.Fprintf(script, "start=%v, size=%v", start, size)

		mbrType, gptType := splitType(ps.Type)
		pType := mbrType
		if pv.EffectiveSchema() == GPT {
			pType = gptType
		}
		if pType != "" {
			fmt.Fprintf(script, ", type=%v", pType)
		}

		if pv.EffectiveSchema() == GPT && ps.Name != "" {
			fmt.Fprintf(script, ", name=%q", ps.Name)
		}
		if pv.EffectiveSchema() == MBR && ps.EffectiveRole() == SystemBoot {
			fmt.Fprintf(script, ", bootable")
		}

		fmt.Fprintf(script, "\n")
	}
	return runSfdisk(image, script.String())
}

func runSfdisk(image string, script string) error {
	cmd := exec.Command("sfdisk", image)
	cmd.Stdin = bytes.NewBufferString(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Noticef("failed sfdisk script:\n%v", script)
		return fmt.Errorf("cannot partition image using sfdisk: %v", osutil.OutputErr(out, err))
	}
	return nil
}

// PositionedVolumeFromGadget takes a gadget rootdir and positions the
// partitions as specified.
func PositionedVolumeFromGadget(gadgetRoot string) (*LaidOutVolume, error) {
	info, err := ReadInfo(gadgetRoot, nil)
	if err != nil {
		return nil, err
	}
	// Limit ourselves to just one volume for now.
	if len(info.Volumes) != 1 {
		return nil, fmt.Errorf("cannot position multiple volumes yet")
	}

	constraints := LayoutConstraints{
		NonMBRStartOffset: 1 * SizeMiB,
		SectorSize:        512,
	}

	for _, vol := range info.Volumes {
		pvol, err := LayoutVolume(gadgetRoot, &vol, constraints)
		if err != nil {
			return nil, err
		}
		// we know  info.Volumes map has size 1 so we can return here
		return pvol, nil
	}
	return nil, fmt.Errorf("internal error in PositionedVolumeFromGadget: this line cannot be reached")
}
