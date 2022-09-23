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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
)

// LayoutMultiVolumeFromYaml returns all LaidOutVolumes for the given
// gadget.yaml string and works for either single or multiple volume
// gadget.yaml's. An empty directory to use to create a gadget.yaml file should
// be provided, such as c.MkDir() in tests.
func LayoutMultiVolumeFromYaml(newDir, kernelDir, gadgetYaml string, model gadget.Model) (map[string]*gadget.LaidOutVolume, error) {
	gadgetRoot, err := WriteGadgetYaml(newDir, gadgetYaml)
	if err != nil {
		return nil, err
	}

	_, allVolumes, err := gadget.LaidOutVolumesFromGadget(gadgetRoot, kernelDir, model)
	if err != nil {
		return nil, fmt.Errorf("cannot layout volumes: %v", err)
	}

	return allVolumes, nil
}

func WriteGadgetYaml(newDir, gadgetYaml string) (string, error) {
	gadgetRoot := filepath.Join(newDir, "gadget")
	if err := os.MkdirAll(filepath.Join(gadgetRoot, "meta"), 0755); err != nil {
		return "", err
	}

	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetYaml), 0644); err != nil {
		return "", err
	}

	return gadgetRoot, nil
}

// LayoutFromYaml returns a LaidOutVolume for the given gadget.yaml string. It
// currently only supports gadget.yaml's with a single volume in them. An empty
// directory to use to create a gadget.yaml file should be provided, such as
// c.MkDir() in tests.
func LayoutFromYaml(newDir, gadgetYaml string, model gadget.Model) (*gadget.LaidOutVolume, error) {
	gadgetRoot, err := WriteGadgetYaml(newDir, gadgetYaml)
	if err != nil {
		return nil, err
	}

	return MustLayOutSingleVolumeFromGadget(gadgetRoot, "", model)
}

// MustLayOutSingleVolumeFromGadget takes a gadget rootdir and lays out the
// partitions as specified. This function does not handle multiple volumes and
// is meant for test helpers only. For runtime users, with multiple volumes
// handled by choosing the ubuntu-* role volume, see LaidOutVolumesFromGadget
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
		opts := &gadget.LayoutOptions{
			GadgetRootDir: gadgetRoot,
			KernelRootDir: kernelRoot,
		}
		// we know info.Volumes map has size 1 so we can return here
		return gadget.LayoutVolume(vol, constraints, opts)
	}

	// this is impossible to reach, we already checked that info.Volumes has a
	// length of 1
	panic("impossible logic error")
}

type ModelCharacteristics struct {
	IsClassic bool
	HasModes  bool
}

func (m *ModelCharacteristics) Classic() bool {
	return m.IsClassic
}

func (m *ModelCharacteristics) Grade() asserts.ModelGrade {
	if m.HasModes {
		return asserts.ModelSigned
	}
	return asserts.ModelGradeUnset
}
