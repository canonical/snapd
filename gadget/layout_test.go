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

package gadget_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/secboot"
)

type layoutTestSuite struct {
	dir string
}

var _ = Suite(&layoutTestSuite{})

func (p *layoutTestSuite) SetUpTest(c *C) {
	p.dir = c.MkDir()
}

func (p *layoutTestSuite) TestVolumeSize(c *C) {
	vol := gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Offset: asOffsetPtr(quantity.OffsetMiB), Size: 2 * quantity.SizeMiB, EnclosingVolume: &gadget.Volume{}},
		},
	}
	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(&vol, gadget.OnDiskStructsFromGadget(&vol), opts))


	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: &gadget.Volume{
			Structure: []gadget.VolumeStructure{
				{Offset: asOffsetPtr(quantity.OffsetMiB), Size: 2 * quantity.SizeMiB, EnclosingVolume: &gadget.Volume{}},
			},
		},
		LaidOutStructure: []gadget.LaidOutStructure{{
			OnDiskStructure: gadget.OnDiskStructure{
				Size:        2 * quantity.SizeMiB,
				StartOffset: 1 * quantity.OffsetMiB,
			},
			VolumeStructure: &gadget.VolumeStructure{
				Offset:          asOffsetPtr(quantity.OffsetMiB),
				Size:            2 * quantity.SizeMiB,
				EnclosingVolume: &gadget.Volume{},
			},
		}},
	})
}

func mustParseVolume(c *C, gadgetYaml, volume string) *gadget.Volume {
	gi := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(gadgetYaml), nil))

	v, ok := gi.Volumes[volume]
	c.Assert(ok, Equals, true, Commentf("volume %q not found in gadget", volume))
	return v
}

func (p *layoutTestSuite) TestLayoutVolumeMinimal(c *C) {
	gadgetYaml := `
volumes:
  first-image:
    bootloader: u-boot
    structure:
        - type: 00000000-0000-0000-0000-0000deadbeef
          size: 400M
        - type: 83,00000000-0000-0000-0000-0000feedface
          role: system-data
          size: 100M
`
	vol := mustParseVolume(c, gadgetYaml, "first-image")
	c.Assert(vol.Structure, HasLen, 2)

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))


	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-0000deadbeef",
					Size:        400 * quantity.SizeMiB,
					StartOffset: 1 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[0],
			},
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "83,00000000-0000-0000-0000-0000feedface",
					Size:        100 * quantity.SizeMiB,
					StartOffset: 401 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[1],
			},
		},
	})
}

func (p *layoutTestSuite) testLayoutVolumeWithDataPartitions(c *C, encType secboot.EncryptionType) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-dd00deadbeef
          filesystem: ext4
          filesystem-label: ubuntu-save
          role: system-save
          size: 1M
        - type: 00000000-0000-0000-0000-cc00deadbeef
          filesystem: ext4
          filesystem-label: ubuntu-data
          role: system-data
          size: 500M
`
	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 2)

	opts := &gadget.LayoutOptions{
		GadgetRootDir: p.dir,
		EncType:       encType,
	}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))


	saveFsLabel := "ubuntu-save"
	saveFsType := "ext4"
	dataFsLabel := "ubuntu-data"
	dataFsType := "ext4"
	if encType == secboot.EncryptionTypeLUKS {
		saveFsLabel = "ubuntu-save-enc"
		saveFsType = "crypto_LUKS"
		dataFsLabel = "ubuntu-data-enc"
		dataFsType = "crypto_LUKS"
	}
	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:             "00000000-0000-0000-0000-dd00deadbeef",
					Size:             1 * quantity.SizeMiB,
					StartOffset:      1 * quantity.OffsetMiB,
					PartitionFSType:  saveFsType,
					PartitionFSLabel: saveFsLabel,
				},
				VolumeStructure: &vol.Structure[0],
			},
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:             "00000000-0000-0000-0000-cc00deadbeef",
					Size:             500 * quantity.SizeMiB,
					StartOffset:      2 * quantity.OffsetMiB,
					PartitionFSType:  dataFsType,
					PartitionFSLabel: dataFsLabel,
				},
				VolumeStructure: &vol.Structure[1],
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumeWithDataPartitions(c *C) {
	p.testLayoutVolumeWithDataPartitions(c, secboot.EncryptionTypeNone)
}

func (p *layoutTestSuite) TestLayoutVolumeWithDataPartitionsEncrypted(c *C) {
	p.testLayoutVolumeWithDataPartitions(c, secboot.EncryptionTypeLUKS)
}

func (p *layoutTestSuite) TestLayoutVolumeImplicitOrdering(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-dd00deadbeef
          size: 400M
        - type: 00000000-0000-0000-0000-cc00deadbeef
          role: system-data
          size: 500M
        - type: 00000000-0000-0000-0000-bb00deadbeef
          size: 100M
        - type: 00000000-0000-0000-0000-aa00deadbeef
          size: 100M
`
	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 4)

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))


	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-dd00deadbeef",
					Size:        400 * quantity.SizeMiB,
					StartOffset: 1 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[0],
			},
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-cc00deadbeef",
					Size:        500 * quantity.SizeMiB,
					StartOffset: 401 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[1],
			},
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-bb00deadbeef",
					Size:        100 * quantity.SizeMiB,
					StartOffset: 901 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[2],
			},
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-aa00deadbeef",
					Size:        100 * quantity.SizeMiB,
					StartOffset: 1001 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[3],
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumeExplicitOrdering(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-dd00deadbeef
          size: 400M
          offset: 800M
        - type: 00000000-0000-0000-0000-cc00deadbeef
          role: system-data
          size: 500M
          offset: 200M
        - type: 00000000-0000-0000-0000-bb00deadbeef
          size: 100M
          offset: 1200M
        - type: 00000000-0000-0000-0000-aa00deadbeef
          size: 100M
          offset: 1M
`
	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 4)

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))


	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-aa00deadbeef",
					Size:        100 * quantity.SizeMiB,
					StartOffset: 1 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[0],
			},
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-cc00deadbeef",
					Size:        500 * quantity.SizeMiB,
					StartOffset: 200 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[1],
			},
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-dd00deadbeef",
					Size:        400 * quantity.SizeMiB,
					StartOffset: 800 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[2],
			},
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-bb00deadbeef",
					Size:        100 * quantity.SizeMiB,
					StartOffset: 1200 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[3],
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumeMixedOrdering(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-dd00deadbeef
          size: 400M
          offset: 800M
        - type: 00000000-0000-0000-0000-cc00deadbeef
          role: system-data
          size: 500M
          offset: 200M
        - type: 00000000-0000-0000-0000-bb00deadbeef
          size: 100M
        - type: 00000000-0000-0000-0000-aa00deadbeef
          size: 100M
          offset: 1M
`
	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 4)

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))


	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-aa00deadbeef",
					Size:        100 * quantity.SizeMiB,
					StartOffset: 1 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[0],
			},
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-cc00deadbeef",
					Size:        500 * quantity.SizeMiB,
					StartOffset: 200 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[1],
			},
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-bb00deadbeef",
					Size:        100 * quantity.SizeMiB,
					StartOffset: 700 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[2],
			},
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-dd00deadbeef",
					Size:        400 * quantity.SizeMiB,
					StartOffset: 800 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[3],
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumeErrorsContentNoSuchFile(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-dd00deadbeef
          size: 400M
          offset: 800M
          content:
              - image: foo.img
`
	vol := mustParseVolume(c, gadgetYaml, "first")
	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot lay out structure #0: content "foo.img":.*no such file or directory`)
}

func (p *layoutTestSuite) TestLayoutVolumeContentCanIgnoreContent(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-dd00deadbeef
          size: 400M
          offset: 800M
          content:
              - image: foo.img
`
	vol := mustParseVolume(c, gadgetYaml, "first")
	opts := &gadget.LayoutOptions{
		GadgetRootDir: p.dir,
	}
	// LayoutVolume fails with default options because the foo.img
	// file is missing
	_ := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))
	c.Assert(err, ErrorMatches, `cannot lay out structure #0: content "foo.img":.*no such file or directory`)

	// But LayoutVolume works with the IgnoreContent works
	opts.IgnoreContent = true
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))

	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-dd00deadbeef",
					Size:        400 * quantity.SizeMiB,
					StartOffset: 800 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[0],
			},
		},
	})
}

func makeSizedFile(c *C, path string, size quantity.Size, content []byte) {
	mylog.Check(os.MkdirAll(filepath.Dir(path), 0755))


	f := mylog.Check2(os.Create(path))

	defer f.Close()
	if size != 0 {
		mylog.Check(f.Truncate(int64(size)))

	}
	if content != nil {
		_ := mylog.Check2(io.Copy(f, bytes.NewReader(content)))

	}
}

func (p *layoutTestSuite) TestLayoutVolumeErrorsContentTooLargeSingle(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-dd00deadbeef
          size: 1M
          content:
              - image: foo.img
`
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), quantity.SizeMiB+1, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot lay out structure #0: content "foo.img" does not fit in the structure`)
}

func (p *layoutTestSuite) TestLayoutVolumeErrorsContentTooLargeMany(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-dd00deadbeef
          size: 2M
          content:
              - image: foo.img
              - image: bar.img
`
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), quantity.SizeMiB+1, nil)
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), quantity.SizeMiB+1, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot lay out structure #0: content "bar.img" does not fit in the structure`)
}

func (p *layoutTestSuite) TestLayoutVolumeErrorsContentTooLargeWithOffset(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-dd00deadbeef
          size: 1M
          content:
              - image: foo.img
                # 512kB
                offset: 524288
`
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), quantity.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot lay out structure #0: content "foo.img" does not fit in the structure`)
}

func (p *layoutTestSuite) TestLayoutVolumeErrorsContentLargerThanDeclared(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-dd00deadbeef
          size: 2M
          content:
              - image: foo.img
                size: 1M
`
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), quantity.SizeMiB+1, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot lay out structure #0: content "foo.img" size %v is larger than declared %v`, quantity.SizeMiB+1, quantity.SizeMiB))
}

func (p *layoutTestSuite) TestLayoutVolumeErrorsContentOverlap(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-dd00deadbeef
          size: 2M
          content:
              - image: foo.img
                size: 1M
                # 512kB
                offset: 524288
              - image: bar.img
                size: 1M
                offset: 0
`
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), quantity.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot lay out structure #0: content "foo.img" overlaps with preceding image "bar.img"`)
}

func (p *layoutTestSuite) TestLayoutVolumeContentExplicitOrder(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-0000deadbeef
          size: 2M
          content:
              - image: foo.img
                size: 1M
                offset: 1M
              - image: bar.img
                size: 1M
                offset: 0
`
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), quantity.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 1)
	c.Assert(vol.Structure[0].Content, HasLen, 2)

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))

	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-0000deadbeef",
					Size:        2 * quantity.SizeMiB,
					StartOffset: 1 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[0],
				LaidOutContent: []gadget.LaidOutContent{
					{
						VolumeContent: &vol.Structure[0].Content[1],
						StartOffset:   1 * quantity.OffsetMiB,
						Size:          quantity.SizeMiB,
						Index:         1,
					},
					{
						VolumeContent: &vol.Structure[0].Content[0],
						StartOffset:   2 * quantity.OffsetMiB,
						Size:          quantity.SizeMiB,
						Index:         0,
					},
				},
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumeContentImplicitOrder(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-0000deadbeef
          size: 2M
          content:
              - image: foo.img
                size: 1M
              - image: bar.img
                size: 1M
`
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), quantity.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 1)
	c.Assert(vol.Structure[0].Content, HasLen, 2)

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))

	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-0000deadbeef",
					Size:        2 * quantity.SizeMiB,
					StartOffset: 1 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[0],
				LaidOutContent: []gadget.LaidOutContent{
					{
						VolumeContent: &vol.Structure[0].Content[0],
						StartOffset:   1 * quantity.OffsetMiB,
						Size:          quantity.SizeMiB,
						Index:         0,
					},
					{
						VolumeContent: &vol.Structure[0].Content[1],
						StartOffset:   2 * quantity.OffsetMiB,
						Size:          quantity.SizeMiB,
						Index:         1,
					},
				},
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumeContentImplicitSize(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-0000deadbeef
          size: 2M
          content:
              - image: foo.img
`
	size1_5MiB := quantity.SizeMiB + quantity.SizeMiB/2
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), size1_5MiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 1)
	c.Assert(vol.Structure[0].Content, HasLen, 1)

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))

	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-0000deadbeef",
					Size:        2 * quantity.SizeMiB,
					StartOffset: 1 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[0],
				LaidOutContent: []gadget.LaidOutContent{
					{
						VolumeContent: &vol.Structure[0].Content[0],
						StartOffset:   1 * quantity.OffsetMiB,
						Size:          size1_5MiB,
					},
				},
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumeContentNonBare(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-0000deadbeef
          filesystem: ext4
          size: 2M
          content:
              - source: foo.txt
                target: /boot
`
	makeSizedFile(c, filepath.Join(p.dir, "foo.txt"), 0, []byte("foobar\n"))

	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 1)
	c.Assert(vol.Structure[0].Content, HasLen, 1)

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))

	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:            "00000000-0000-0000-0000-0000deadbeef",
					PartitionFSType: "ext4",
					Size:            2 * quantity.SizeMiB,
					StartOffset:     1 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[0],
				ResolvedContent: []gadget.ResolvedContent{
					{
						VolumeContent: &gadget.VolumeContent{
							UnresolvedSource: "foo.txt",
							Target:           "/boot",
						},
						ResolvedSource: filepath.Join(p.dir, "foo.txt"),
					},
				},
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumeMBR(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - name: mbr
          type: bare
          role: mbr
          size: 446
        - name: other
          type: 00000000-0000-0000-0000-0000deadbeef
          size: 1M
`
	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 2)

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))

	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				// MBR
				OnDiskStructure: gadget.OnDiskStructure{
					Name:        "mbr",
					Type:        "bare",
					Size:        quantity.Size(446),
					StartOffset: 0,
				},
				VolumeStructure: &vol.Structure[0],
			}, {
				OnDiskStructure: gadget.OnDiskStructure{
					Name:        "other",
					Type:        "00000000-0000-0000-0000-0000deadbeef",
					Size:        quantity.SizeMiB,
					StartOffset: 1 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[1],
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumeOffsetWriteAll(c *C) {
	gadgetYaml := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
        content:
          - image: foo.img
            offset-write: bar+10
      - name: bar
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset-write: 600
        content:
          - image: bar.img
            offset-write: 450
`
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), 200*quantity.SizeKiB, []byte(""))
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), 150*quantity.SizeKiB, []byte(""))

	vol := mustParseVolume(c, gadgetYaml, "pc")
	c.Assert(vol.Structure, HasLen, 3)

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))

	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				// mbr
				OnDiskStructure: gadget.OnDiskStructure{
					Name:        "mbr",
					Type:        "mbr",
					Size:        quantity.Size(440),
					StartOffset: 0,
				},
				VolumeStructure: &vol.Structure[0],
			}, {
				// foo
				OnDiskStructure: gadget.OnDiskStructure{
					Name:        "foo",
					Type:        "DA,21686148-6449-6E6F-744E-656564454649",
					Size:        quantity.SizeMiB,
					StartOffset: 1 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[1],
				LaidOutContent: []gadget.LaidOutContent{
					{
						VolumeContent: &vol.Structure[1].Content[0],
						Size:          200 * quantity.SizeKiB,
						StartOffset:   1 * quantity.OffsetMiB,
					},
				},
			}, {
				// bar
				OnDiskStructure: gadget.OnDiskStructure{
					Name:        "bar",
					Type:        "DA,21686148-6449-6E6F-744E-656564454649",
					Size:        quantity.SizeMiB,
					StartOffset: 2 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[2],
				LaidOutContent: []gadget.LaidOutContent{
					{
						VolumeContent: &vol.Structure[2].Content[0],
						Size:          150 * quantity.SizeKiB,
						StartOffset:   2 * quantity.OffsetMiB,
					},
				},
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumePartialNoSuchFile(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - type: 00000000-0000-0000-0000-dd00deadbeef
          size: 400M
          offset: 800M
          content:
              - image: foo.img
`
	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 1)

	v := mylog.Check2(gadget.LayoutVolumePartially(vol, gadget.OnDiskStructsFromGadget(vol)))
	c.Assert(v, DeepEquals, &gadget.PartiallyLaidOutVolume{
		Volume: vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				OnDiskStructure: gadget.OnDiskStructure{
					Type:        "00000000-0000-0000-0000-dd00deadbeef",
					Size:        400 * quantity.SizeMiB,
					StartOffset: 800 * quantity.OffsetMiB,
				},
				VolumeStructure: &vol.Structure[0],
			},
		},
	})

}

func (p *layoutTestSuite) TestLaidOutStructureShift(c *C) {
	gadgetYamlContent := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        content:
          - image: foo.img
          - image: bar.img
            # 300KB
            offset: 307200

`
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), 200*quantity.SizeKiB, []byte(""))
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), 150*quantity.SizeKiB, []byte(""))

	vol := mustParseVolume(c, gadgetYamlContent, "pc")

	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))

	c.Assert(v.LaidOutStructure, HasLen, 1)
	c.Assert(v.LaidOutStructure[0].LaidOutContent, HasLen, 2)

	ps := v.LaidOutStructure[0]

	c.Assert(ps, DeepEquals, gadget.LaidOutStructure{
		// foo
		OnDiskStructure: gadget.OnDiskStructure{
			Name:        "foo",
			Type:        "DA,21686148-6449-6E6F-744E-656564454649",
			Size:        quantity.SizeMiB,
			StartOffset: 1 * quantity.OffsetMiB,
		},
		VolumeStructure: &vol.Structure[0],
		LaidOutContent: []gadget.LaidOutContent{
			{
				VolumeContent: &vol.Structure[0].Content[0],
				Size:          200 * quantity.SizeKiB,
				StartOffset:   1 * quantity.OffsetMiB,
				Index:         0,
			}, {
				VolumeContent: &vol.Structure[0].Content[1],
				Size:          150 * quantity.SizeKiB,
				StartOffset:   1*quantity.OffsetMiB + 300*quantity.OffsetKiB,
				Index:         1,
			},
		},
	})

	shiftedTo0 := gadget.ShiftStructureTo(ps, 0)
	c.Assert(shiftedTo0, DeepEquals, gadget.LaidOutStructure{
		// foo
		OnDiskStructure: gadget.OnDiskStructure{
			Name:        "foo",
			Type:        "DA,21686148-6449-6E6F-744E-656564454649",
			Size:        quantity.SizeMiB,
			StartOffset: 0,
		},
		VolumeStructure: &vol.Structure[0],
		LaidOutContent: []gadget.LaidOutContent{
			{
				VolumeContent: &vol.Structure[0].Content[0],
				Size:          200 * quantity.SizeKiB,
				StartOffset:   0,
				Index:         0,
			}, {
				VolumeContent: &vol.Structure[0].Content[1],
				Size:          150 * quantity.SizeKiB,
				StartOffset:   300 * quantity.OffsetKiB,
				Index:         1,
			},
		},
	})

	shiftedTo2M := gadget.ShiftStructureTo(ps, 2*quantity.OffsetMiB)
	c.Assert(shiftedTo2M, DeepEquals, gadget.LaidOutStructure{
		// foo
		OnDiskStructure: gadget.OnDiskStructure{
			Name:        "foo",
			Type:        "DA,21686148-6449-6E6F-744E-656564454649",
			Size:        quantity.SizeMiB,
			StartOffset: 2 * quantity.OffsetMiB,
		},
		VolumeStructure: &vol.Structure[0],
		LaidOutContent: []gadget.LaidOutContent{
			{
				VolumeContent: &vol.Structure[0].Content[0],
				Size:          200 * quantity.SizeKiB,
				StartOffset:   2 * quantity.OffsetMiB,
				Index:         0,
			}, {
				VolumeContent: &vol.Structure[0].Content[1],
				Size:          150 * quantity.SizeKiB,
				StartOffset:   2*quantity.OffsetMiB + 300*quantity.OffsetKiB,
				Index:         1,
			},
		},
	})
}

func mockKernel(c *C, kernelYaml string, filesWithContent map[string]string) string {
	// precondition
	_ := mylog.Check2(kernel.InfoFromKernelYaml([]byte(kernelYaml)))


	kernelRootDir := c.MkDir()
	mylog.Check(os.MkdirAll(filepath.Join(kernelRootDir, "meta"), 0755))

	mylog.Check(os.WriteFile(filepath.Join(kernelRootDir, "meta/kernel.yaml"), []byte(kernelYaml), 0644))


	for fname, content := range filesWithContent {
		p := filepath.Join(kernelRootDir, fname)
		mylog.Check(os.MkdirAll(filepath.Dir(p), 0755))

		mylog.Check(os.WriteFile(p, []byte(content), 0644))

	}
	mylog.

		// ensure we have valid kernel.yaml in our tests
		Check(kernel.Validate(kernelRootDir))


	return kernelRootDir
}

var gadgetYamlWithKernelRef = `
 volumes:
  pi:
    bootloader: u-boot
    structure:
      - type: 00000000-0000-0000-0000-dd00deadbeef
        filesystem: vfat
        filesystem-label: system-boot
        size: 128M
        content:
          - source: $kernel:dtbs/boot-assets/
            target: /
          - source: $kernel:dtbs/some-file
            target: /
          - source: file-from-gadget
            target: /
          - source: dir-from-gadget/
            target: /
`

func (p *layoutTestSuite) TestResolveContentPathsNotInWantedAssets(c *C) {
	vol := mustParseVolume(c, gadgetYamlWithKernelRef, "pi")
	c.Assert(vol.Structure, HasLen, 1)

	kernelSnapDir := c.MkDir()
	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir, KernelRootDir: kernelSnapDir}
	_ := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))
	c.Assert(err, ErrorMatches, `cannot resolve content for structure #0 \(""\) at index 0: cannot find "dtbs" in kernel info from "/.*"`)
}

func (p *layoutTestSuite) TestResolveContentPathsSkipResolveContent(c *C) {
	vol := mustParseVolume(c, gadgetYamlWithKernelRef, "pi")
	c.Assert(vol.Structure, HasLen, 1)

	kernelSnapDir := c.MkDir()
	defaultOpts := &gadget.LayoutOptions{GadgetRootDir: p.dir, KernelRootDir: kernelSnapDir}

	opts := defaultOpts
	_ := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))
	c.Assert(err, ErrorMatches, `cannot resolve content for structure #0 \(""\) at index 0: cannot find "dtbs" in kernel info from "/.*"`)

	// SkipResolveContent will allow to layout the volume even if
	// files are missing
	opts = defaultOpts
	opts.SkipResolveContent = true
	v := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))

	c.Assert(v.Structure, HasLen, 1)

	// As does IgnoreContent will allow to layout the volume even if
	// files are missing
	opts = defaultOpts
	opts.IgnoreContent = true
	v = mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))

	c.Assert(v.Structure, HasLen, 1)
}

func (p *layoutTestSuite) TestResolveContentPathsErrorInKernelRef(c *C) {
	// create invalid kernel ref
	s := strings.Replace(gadgetYamlWithKernelRef, "$kernel:dtbs", "$kernel:-invalid-kernel-ref", -1)
	// Note that mustParseVolume does not call ValidateContent() which
	// would be needed to validate "$kernel:" refs.
	vol := mustParseVolume(c, s, "pi")
	c.Assert(vol.Structure, HasLen, 1)

	kernelSnapDir := c.MkDir()
	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir, KernelRootDir: kernelSnapDir}
	_ := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))
	c.Assert(err, ErrorMatches, `cannot resolve content for structure #0 \(""\) at index 0: cannot parse kernel ref: invalid asset name in kernel ref "\$kernel:-invalid-kernel-ref/boot-assets/"`)
}

func (p *layoutTestSuite) TestResolveContentPathsNotInWantedeContent(c *C) {
	kernelYaml := `
assets:
  dtbs:
    update: true
    content:
      - dtbs/
`

	vol := mustParseVolume(c, gadgetYamlWithKernelRef, "pi")
	c.Assert(vol.Structure, HasLen, 1)

	kernelSnapDir := mockKernel(c, kernelYaml, map[string]string{
		"dtbs/foo.dtb": "foo.dtb content",
	})
	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir, KernelRootDir: kernelSnapDir}
	_ := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))
	c.Assert(err, ErrorMatches, `cannot resolve content for structure #0 \(""\) at index 0: cannot find wanted kernel content "boot-assets/" in "/.*"`)
}

func (p *layoutTestSuite) TestResolveContentPaths(c *C) {
	kernelYaml := `
assets:
  dtbs:
    update: true
    content:
      - boot-assets/
      - some-file
`
	vol := mustParseVolume(c, gadgetYamlWithKernelRef, "pi")
	c.Assert(vol.Structure, HasLen, 1)

	kernelSnapFiles := map[string]string{
		"boot-assets/foo": "foo-content",
		"some-file":       "some-file content",
	}
	kernelSnapDir := mockKernel(c, kernelYaml, kernelSnapFiles)
	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir, KernelRootDir: kernelSnapDir}
	lv := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))

	// Volume.Content is unchanged
	c.Assert(lv.Structure, HasLen, 1)
	c.Check(lv.Structure[0].Content, DeepEquals, []gadget.VolumeContent{
		{
			UnresolvedSource: "$kernel:dtbs/boot-assets/",
			Target:           "/",
		},
		{
			UnresolvedSource: "$kernel:dtbs/some-file",
			Target:           "/",
		},
		{
			UnresolvedSource: "file-from-gadget",
			Target:           "/",
		},
		{
			UnresolvedSource: "dir-from-gadget/",
			Target:           "/",
		},
	})
	// and the LaidOutSturctures ResolvedContent has the correct paths
	c.Assert(lv.LaidOutStructure, HasLen, 1)
	c.Check(lv.LaidOutStructure[0].ResolvedContent, DeepEquals, []gadget.ResolvedContent{
		{
			VolumeContent:  &lv.Structure[0].Content[0],
			ResolvedSource: filepath.Join(kernelSnapDir, "boot-assets/") + "/",
			KernelUpdate:   true,
		},
		{
			VolumeContent:  &lv.Structure[0].Content[1],
			ResolvedSource: filepath.Join(kernelSnapDir, "some-file"),
			KernelUpdate:   true,
		},
		{
			VolumeContent:  &lv.Structure[0].Content[2],
			ResolvedSource: filepath.Join(p.dir, "file-from-gadget"),
		},
		{
			VolumeContent:  &lv.Structure[0].Content[3],
			ResolvedSource: filepath.Join(p.dir, "dir-from-gadget") + "/",
		},
	})
}

func (p *layoutTestSuite) TestResolveContentPathsLp1907056(c *C) {
	gadgetYamlWithKernelRef := `
 volumes:
  pi:
    schema: mbr
    bootloader: u-boot
    structure:
      - name: ubuntu-seed
        role: system-seed
        filesystem: vfat
        type: 0C
        size: 1200M
        content:
          - source: $kernel:pidtbs/dtbs/broadcom/
            target: /
          - source: $kernel:pidtbs/dtbs/overlays/
            target: /overlays
          - source: boot-assets/
            target: /
`

	kernelYaml := `
assets:
  pidtbs:
    update: true
    content:
      - dtbs/
`
	vol := mustParseVolume(c, gadgetYamlWithKernelRef, "pi")
	c.Assert(vol.Structure, HasLen, 1)

	kernelSnapDir := mockKernel(c, kernelYaml, map[string]string{
		"dtbs/foo.dtb": "foo.dtb content",
	})
	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir, KernelRootDir: kernelSnapDir}
	lv := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))

	// Volume.Content is unchanged
	c.Assert(lv.Structure, HasLen, 1)
	c.Check(lv.Structure[0].Content, DeepEquals, []gadget.VolumeContent{
		{
			UnresolvedSource: "$kernel:pidtbs/dtbs/broadcom/",
			Target:           "/",
		},
		{
			UnresolvedSource: "$kernel:pidtbs/dtbs/overlays/",
			Target:           "/overlays",
		},
		{
			UnresolvedSource: "boot-assets/",
			Target:           "/",
		},
	})
	// and the LaidOutSturctures ResolvedContent has the correct paths
	c.Assert(lv.LaidOutStructure, HasLen, 1)
	c.Check(lv.LaidOutStructure[0].ResolvedContent, DeepEquals, []gadget.ResolvedContent{
		{
			VolumeContent:  &lv.Structure[0].Content[0],
			ResolvedSource: filepath.Join(kernelSnapDir, "dtbs/broadcom/") + "/",
			KernelUpdate:   true,
		},
		{
			VolumeContent:  &lv.Structure[0].Content[1],
			ResolvedSource: filepath.Join(kernelSnapDir, "dtbs/overlays") + "/",
			KernelUpdate:   true,
		},
		{
			VolumeContent:  &lv.Structure[0].Content[2],
			ResolvedSource: filepath.Join(p.dir, "boot-assets") + "/",
		},
	})
}

func (p *layoutTestSuite) TestResolveContentSamePrefixErrors(c *C) {
	gadgetYamlWithKernelRef := `
 volumes:
  pi:
    schema: mbr
    bootloader: u-boot
    structure:
      - name: ubuntu-seed
        role: system-seed
        filesystem: vfat
        type: 0C
        size: 1200M
        content:
          - source: $kernel:dtbs/a
            target: /
          - source: $kernel:dtbs/ab
            target: /
`
	kernelYaml := `
assets:
  dtbs:
    update: true
    content:
      - a
`
	vol := mustParseVolume(c, gadgetYamlWithKernelRef, "pi")
	c.Assert(vol.Structure, HasLen, 1)

	kernelSnapDir := mockKernel(c, kernelYaml, map[string]string{
		"a/foo.dtb": "foo.dtb content",
	})
	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir, KernelRootDir: kernelSnapDir}
	_ := mylog.Check2(gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts))
	c.Assert(err, ErrorMatches, `.*: cannot find wanted kernel content "ab" in.*`)
}

func (p *layoutTestSuite) TestLayoutWithMinSize(c *C) {
	vol := gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{
				Offset:          asOffsetPtr(quantity.OffsetMiB),
				MinSize:         quantity.SizeMiB,
				Size:            2 * quantity.SizeMiB,
				YamlIndex:       0,
				EnclosingVolume: &gadget.Volume{},
			},
			{
				MinSize:         quantity.SizeMiB,
				Offset:          asOffsetPtr(3 * quantity.OffsetMiB),
				Size:            2 * quantity.SizeMiB,
				YamlIndex:       1,
				EnclosingVolume: &gadget.Volume{},
			},
		},
	}
	v := mylog.Check2(gadget.LayoutVolume(&vol, gadget.OnDiskStructsFromGadget(&vol), nil))


	// Check StartOffset and Size is well defined even if using min-size
	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: &vol,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				OnDiskStructure: gadget.OnDiskStructure{
					StartOffset: 1 * quantity.OffsetMiB,
					Size:        2 * quantity.SizeMiB,
				},
				VolumeStructure: &vol.Structure[0],
			},
			{
				OnDiskStructure: gadget.OnDiskStructure{
					StartOffset: 3 * quantity.OffsetMiB,
					Size:        2 * quantity.SizeMiB,
				},
				VolumeStructure: &vol.Structure[1],
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutWithDiskData(c *C) {
	vol := gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Offset: asOffsetPtr(quantity.OffsetMiB), Size: 2 * quantity.SizeMiB, EnclosingVolume: &gadget.Volume{}},
		},
	}
	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}

	gadgetToDiskStruct := map[int]*gadget.OnDiskStructure{
		0: {StartOffset: quantity.OffsetMiB, Size: 2 * quantity.SizeMiB},
	}

	v := mylog.Check2(gadget.LayoutVolume(&vol, gadgetToDiskStruct, opts))


	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: &gadget.Volume{
			Structure: []gadget.VolumeStructure{
				{Offset: asOffsetPtr(quantity.OffsetMiB), Size: 2 * quantity.SizeMiB, EnclosingVolume: &gadget.Volume{}},
			},
		},
		LaidOutStructure: []gadget.LaidOutStructure{{
			OnDiskStructure: gadget.OnDiskStructure{
				Size:        2 * quantity.SizeMiB,
				StartOffset: 1 * quantity.OffsetMiB,
			},
			VolumeStructure: &gadget.VolumeStructure{
				Offset:          asOffsetPtr(quantity.OffsetMiB),
				Size:            2 * quantity.SizeMiB,
				EnclosingVolume: &gadget.Volume{},
			},
		}},
	})
}

func (p *layoutTestSuite) TestLayoutWithDiskDataFails(c *C) {
	vol := gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{
				Name: "first", YamlIndex: 0,
				Offset:          asOffsetPtr(quantity.OffsetMiB),
				Size:            2 * quantity.SizeMiB,
				EnclosingVolume: &gadget.Volume{},
			},
			{
				Name: "second", YamlIndex: 1,
				Offset:          asOffsetPtr(3 * quantity.OffsetMiB),
				Size:            2 * quantity.SizeMiB,
				EnclosingVolume: &gadget.Volume{},
			},
		},
	}
	opts := &gadget.LayoutOptions{GadgetRootDir: p.dir}

	gadgetToDiskStruct := map[int]*gadget.OnDiskStructure{
		0: {Name: "first", StartOffset: quantity.OffsetMiB, Size: 2 * quantity.SizeMiB},
	}

	v := mylog.Check2(gadget.LayoutVolume(&vol, gadgetToDiskStruct, opts))
	c.Assert(v, IsNil)
	c.Assert(err.Error(), Equals, `internal error: partition "second" not in disk map`)
}

func (p *layoutTestSuite) TestLayoutVolumeStructure(c *C) {
	type test struct {
		dgpair gadget.OnDiskAndGadgetStructurePair
		err    string
	}
	for _, tc := range []test{
		{
			dgpair: gadget.OnDiskAndGadgetStructurePair{
				DiskStructure: &gadget.OnDiskStructure{Name: "part1"},
				GadgetStructure: &gadget.VolumeStructure{
					Name:            "part1",
					EnclosingVolume: &gadget.Volume{},
				},
			},
		},
		{
			dgpair: gadget.OnDiskAndGadgetStructurePair{
				DiskStructure: &gadget.OnDiskStructure{Name: "part1"},
				GadgetStructure: &gadget.VolumeStructure{
					Name: "part1",
					Content: []gadget.VolumeContent{
						{
							// single file in target directory
							UnresolvedSource: "foo",
							Target:           "/foo-dir/",
						},
					},
					EnclosingVolume: &gadget.Volume{},
				},
			},
			err: `cannot lay out structure #0 ("part1"): content "": stat : no such file or directory`,
		},
	} {
		los := mylog.Check2(gadget.LayoutVolumeStructure(&tc.dgpair, nil, &gadget.LayoutOptions{}))
		if tc.err == "" {
			c.Check(err, IsNil)
			c.Check(los, DeepEquals, &gadget.LaidOutStructure{
				OnDiskStructure: *tc.dgpair.DiskStructure,
				VolumeStructure: tc.dgpair.GadgetStructure,
			})
		} else {
			c.Check(err.Error(), Equals, tc.err)
			c.Check(los, IsNil)
		}
	}
}
