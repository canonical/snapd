// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package gadgettest

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
)

// LayoutFromYaml returns a LaidOutVolume for the given gadget.yaml string. It
// currently only supports gadget.yaml's with a single volume in them. An empty
// directory to use to create a gadget.yaml file should be provided, such as
// c.MkDir() in tests.
func LayoutFromYaml(newDir, gadgetYaml string, model gadget.Model) (*gadget.LaidOutVolume, error) {
	gadgetRoot := filepath.Join(newDir, "gadget")
	if err := os.MkdirAll(filepath.Join(gadgetRoot, "meta"), 0755); err != nil {
		return nil, err
	}

	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetYaml), 0644); err != nil {
		return nil, err
	}
	return MustLayOutSingleVolumeFromGadget(gadgetRoot, "", model)
}

// MustLayOutSingleVolumeFromGadget takes a gadget rootdir and lays out the
// partitions as specified. This function does not handle multiple volumes and
// is meant for test helpers only. For runtime users, with multiple volumes
// handled by choosing the ubuntu-* role volume, see LaidOutSystemVolumeFromGadget
func MustLayOutSingleVolumeFromGadget(gadgetRoot, kernelRoot string, model gadget.Model) (*gadget.LaidOutVolume, error) {
	info, err := gadget.ReadInfo(gadgetRoot, model)
	if err != nil {
		return nil, err
	}

	if len(info.Volumes) != 1 {
		return nil, fmt.Errorf("only single volumes supported in test helper")
	}

	constraints := gadget.LayoutConstraints{
		NonMBRStartOffset: 1 * quantity.OffsetMiB,
	}

	for _, vol := range info.Volumes {
		// we know info.Volumes map has size 1 so we can return here
		return gadget.LayoutVolume(gadgetRoot, kernelRoot, vol, constraints)
	}

	// this is impossible to reach, we already checked that info.Volumes has a
	// length of 1
	panic("impossible logic error")
}

// MockLsblkCommand returns a string suitable for use with MockCommand for lsblk
// with the expected input/output pairing. The input keys are expected to be a
// full string of the lsblk arguments and options, while the output values are
// expected to be a string of the bash quoted JSON to be output for that input.
func MockLsblkCommand(expIO map[string]string) string {
	templ := `
case "$*" in 
	%s
	*)
		echo "unexpected args $*"
		exit 1
		;;
esac`

	insert := ""
	for inArgs, outJSON := range expIO {
		insert += fmt.Sprintf(`
	%q)
		echo %s
		;;
`, inArgs, outJSON)
	}
	return fmt.Sprintf(templ, insert)
}
