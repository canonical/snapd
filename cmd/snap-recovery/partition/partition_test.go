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
package partition_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
)

func TestPartition(t *testing.T) { TestingT(t) }

type partitionTestSuite struct{}

var _ = Suite(&partitionTestSuite{})

func positionedVolumeFromGadget(gadgetRoot string) (*gadget.LaidOutVolume, error) {
	info, err := gadget.ReadInfo(gadgetRoot, nil)
	if err != nil {
		return nil, err
	}

	constraints := gadget.LayoutConstraints{
		NonMBRStartOffset: 1 * gadget.SizeMiB,
		SectorSize:        512,
	}

	positionedVolume := map[string]*gadget.LaidOutVolume{}

	for name, vol := range info.Volumes {
		pvol, err := gadget.LayoutVolume(gadgetRoot, &vol, constraints)
		if err != nil {
			return nil, err
		}
		positionedVolume[name] = pvol
	}

	// Limit ourselves to just one volume for now.
	if len(positionedVolume) != 1 {
		return nil, fmt.Errorf("multiple volumes not supported")
	}
	var name string
	for k := range positionedVolume {
		name = k
	}
	return positionedVolume[name], nil
}

func makeMockGadget(gadgetRoot, gadgetContent string) error {
	if err := os.MkdirAll(filepath.Join(gadgetRoot, "meta"), 0755); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetContent), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "pc-boot.img"), nil, 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "grubx64.efi"), nil, 0644); err != nil {
		return err
	}

	return nil
}
