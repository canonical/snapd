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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"
)

func TestPartition(t *testing.T) { TestingT(t) }

type partitionTestSuite struct{}

var _ = Suite(&partitionTestSuite{})

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
