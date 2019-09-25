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
package volmgr_test

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-install/volmgr"
	"github.com/snapcore/snapd/testutil"
)

func TestVolMgr(t *testing.T) { TestingT(t) }

type volmgrTestSuite struct {
}

var _ = Suite(&volmgrTestSuite{})

// FIXME: uncomment filesystem-label after new naming is enabled in gadget
const gadgetContent = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
        content:
          - image: pc-boot.img
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
      - name: Recovery
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        filesystem-label: ubuntu-seed
        size: 1200M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
      - name: Writable
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        #filesystem-label: ubuntu-data
        size: 1200M
`

const sfdiskMockScript = `echo '{
    "partitiontable": {
        "label": "gpt",
        "id": "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
        "device": "/dev/node",
        "unit": "sectors",
        "firstlba": 34,
        "lastlba": 8388574,
        "partitions": [
            {
                "node": "/dev/node1",
                "start": 2048,
                "size": 2048,
                "type": "21686148-6449-6E6F-744E-656564454649",
                "uuid": "2E59D969-52AB-430B-88AC-F83873519F6F",
                "name": "BIOS Boot"
            }
        ]
    }
}'`

const lsblkMockScript = `echo '{
    "blockdevices": [
        {"name": "node1", "fstype": null, "label": null, "uuid": null, "mountpoint": null}
    ]
}'`

func (s *volmgrTestSuite) TestVolumeManager(c *C) {
	gadgetRoot := c.MkDir()
	c.Assert(createGadget(gadgetRoot), IsNil)

	cmdSfdisk := testutil.MockCommand(c, "sfdisk", sfdiskMockScript)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", lsblkMockScript)
	defer cmdLsblk.Restore()

	cmdPartx := testutil.MockCommand(c, "partx", "exit 0")
	defer cmdPartx.Restore()

	cmdMkfsVFAT := testutil.MockCommand(c, "mkfs.vfat", "exit 0")
	defer cmdMkfsVFAT.Restore()

	cmdMke2fs := testutil.MockCommand(c, "mke2fs", "exit 0")
	defer cmdMke2fs.Restore()

	cmdCryptsetup := testutil.MockCommand(c, "cryptsetup", "exit 0")
	defer cmdCryptsetup.Restore()

	*volmgr.TempKeyFile = path.Join(gadgetRoot, "unlock.tmp")
	v, err := volmgr.NewVolumeManager(gadgetRoot, "/dev/node")
	c.Assert(err, IsNil)

	err = v.Run()
	c.Assert(err, IsNil)

	c.Assert(cmdPartx.Calls(), DeepEquals, [][]string{{"partx", "-u", "/dev/node"}})
	c.Assert(cmdMkfsVFAT.Calls(), DeepEquals, [][]string{{"mkfs.vfat", "-n", "ubuntu-seed", "/dev/node2"}})
	c.Assert(cmdCryptsetup.Calls(), DeepEquals, [][]string{
		{"cryptsetup", "-q", "luksFormat", "--type", "luks2", "--pbkdf-memory", "1000", "--master-key-file", *volmgr.TempKeyFile, "/dev/node3"},
		{"cryptsetup", "open", "--master-key-file", *volmgr.TempKeyFile, "/dev/node3", "ubuntu-data"},
	})

	// FIXME: gadget expects the system-data partition to be labeled as "writable". However, UC20
	//        changed it to "ubuntu-data", but older versions should keep the previous naming. Enable
	//        this test after we decide how to deal with legacy naming support.
	// c.Assert(cmdMke2fs.Calls(), DeepEquals, [][]string{{"mke2fs", "-t", "ext4", "-L", "ubuntu-data", "/dev/mapper/ubuntu-data"}})
}

func createGadget(gadgetRoot string) error {
	if err := os.Mkdir(path.Join(gadgetRoot, "meta"), 0755); err != nil {
		return err
	}
	if err := ioutil.WriteFile(path.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetContent), 0644); err != nil {
		return err
	}
	f, err := os.Create(path.Join(gadgetRoot, "pc-boot.img"))
	if err != nil {
		return err
	}
	f.Close()

	return nil
}
