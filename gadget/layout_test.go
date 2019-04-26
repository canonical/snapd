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

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/gadget"
)

type layoutTestSuite struct {
	dir string
}

var _ = Suite(&layoutTestSuite{})

func (l *layoutTestSuite) SetUpTest(c *C) {
	l.dir = c.MkDir()
}

var defaultConstraints = gadget.PositioningConstraints{
	NonMBRStartOffset: 1 * gadget.SizeMiB,
	SectorSize:        512,
}

func (l *layoutTestSuite) TestVolumeSize(c *C) {
	vol := gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Size: 2 * gadget.SizeMiB},
		},
	}
	v, err := gadget.PositionVolume(l.dir, &vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Size: 3 * gadget.SizeMiB,
		Structures: []gadget.PositionedStructure{
			{VolumeStructure: &gadget.VolumeStructure{Size: 2 * gadget.SizeMiB}, StartOffset: 1 * gadget.SizeMiB},
		},
	})
}

func mustParseVolume(c *C, gadgetYaml, volume string) *gadget.Volume {
	var gi gadget.Info
	err := yaml.Unmarshal([]byte(gadgetYaml), &gi)
	c.Assert(err, IsNil)
	v, ok := gi.Volumes[volume]
	c.Assert(ok, Equals, true, Commentf("volume %q not found in gadget", volume))
	err = gadget.ValidateVolume("foo", &v)
	c.Assert(err, IsNil)
	return &v
}

func (l *layoutTestSuite) TestVolumeLayoutMinimal(c *C) {
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
	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Size: 501 * gadget.SizeMiB,
		Structures: []gadget.PositionedStructure{
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size: 400 * gadget.SizeMiB,
					Type: "00000000-0000-0000-0000-0000deadbeef",
				},
				StartOffset: 1 * gadget.SizeMiB,
				Index:       0,
			},
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size: 100 * gadget.SizeMiB,
					Type: "83,00000000-0000-0000-0000-0000feedface",
					Role: "system-data",
				},
				StartOffset: 401 * gadget.SizeMiB,
				Index:       1,
			},
		},
	})
}

func (l *layoutTestSuite) TestVolumeLayoutImplicitOrdering(c *C) {
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
	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Size: 1101 * gadget.SizeMiB,
		Structures: []gadget.PositionedStructure{
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size: 400 * gadget.SizeMiB,
					Type: "00000000-0000-0000-0000-dd00deadbeef",
				},
				StartOffset: 1 * gadget.SizeMiB,
				Index:       0,
			},
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size: 500 * gadget.SizeMiB,
					Type: "00000000-0000-0000-0000-cc00deadbeef",
					Role: "system-data",
				},
				StartOffset: 401 * gadget.SizeMiB,
				Index:       1,
			},
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size: 100 * gadget.SizeMiB,
					Type: "00000000-0000-0000-0000-bb00deadbeef",
				},
				StartOffset: 901 * gadget.SizeMiB,
				Index:       2,
			},
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size: 100 * gadget.SizeMiB,
					Type: "00000000-0000-0000-0000-aa00deadbeef",
				},
				StartOffset: 1001 * gadget.SizeMiB,
				Index:       3,
			},
		},
	})
}

func (l *layoutTestSuite) TestVolumeLayoutExplicitOrdering(c *C) {
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
	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Size: 1300 * gadget.SizeMiB,
		Structures: []gadget.PositionedStructure{
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:   100 * gadget.SizeMiB,
					Type:   "00000000-0000-0000-0000-aa00deadbeef",
					Offset: asSizePtr(1 * gadget.SizeMiB),
				},
				StartOffset: 1 * gadget.SizeMiB,
				Index:       3,
			},
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:   500 * gadget.SizeMiB,
					Type:   "00000000-0000-0000-0000-cc00deadbeef",
					Role:   "system-data",
					Offset: asSizePtr(200 * gadget.SizeMiB),
				},
				StartOffset: 200 * gadget.SizeMiB,
				Index:       1,
			},
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:   400 * gadget.SizeMiB,
					Type:   "00000000-0000-0000-0000-dd00deadbeef",
					Offset: asSizePtr(800 * gadget.SizeMiB),
				},
				StartOffset: 800 * gadget.SizeMiB,
				Index:       0,
			},
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:   100 * gadget.SizeMiB,
					Type:   "00000000-0000-0000-0000-bb00deadbeef",
					Offset: asSizePtr(1200 * gadget.SizeMiB),
				},
				StartOffset: 1200 * gadget.SizeMiB,
				Index:       2,
			},
		},
	})
}

func (l *layoutTestSuite) TestVolumeLayoutMixedOrdering(c *C) {
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
	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Size: 1200 * gadget.SizeMiB,
		Structures: []gadget.PositionedStructure{
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:   100 * gadget.SizeMiB,
					Type:   "00000000-0000-0000-0000-aa00deadbeef",
					Offset: asSizePtr(1 * gadget.SizeMiB),
				},
				StartOffset: 1 * gadget.SizeMiB,
				Index:       3,
			},
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:   500 * gadget.SizeMiB,
					Type:   "00000000-0000-0000-0000-cc00deadbeef",
					Role:   "system-data",
					Offset: asSizePtr(200 * gadget.SizeMiB),
				},
				StartOffset: 200 * gadget.SizeMiB,
				Index:       1,
			},
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size: 100 * gadget.SizeMiB,
					Type: "00000000-0000-0000-0000-bb00deadbeef",
				},
				StartOffset: 700 * gadget.SizeMiB,
				Index:       2,
			},
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:   400 * gadget.SizeMiB,
					Type:   "00000000-0000-0000-0000-dd00deadbeef",
					Offset: asSizePtr(800 * gadget.SizeMiB),
				},
				StartOffset: 800 * gadget.SizeMiB,
				Index:       0,
			},
		},
	})
}

func (l *layoutTestSuite) TestVolumeLayoutErrorsContentNoSuchFile(c *C) {
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
	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot position structure #0: content "foo.img":.*no such file or directory`)
}

func makeSizedFile(c *C, path string, size gadget.Size, content []byte) {
	f, err := os.Create(path)
	c.Assert(err, IsNil)
	defer f.Close()
	if size != 0 {
		err = f.Truncate(int64(size))
		c.Assert(err, IsNil)
	}
	if content != nil {
		_, err := io.Copy(f, bytes.NewReader(content))
		c.Assert(err, IsNil)
	}
}

func (l *layoutTestSuite) TestVolumeLayoutErrorsContentTooLargeSingle(c *C) {
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
	makeSizedFile(c, filepath.Join(l.dir, "foo.img"), gadget.SizeMiB+1, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot position structure #0: content "foo.img" does not fit in the structure`)
}

func (l *layoutTestSuite) TestVolumeLayoutErrorsContentTooLargeMany(c *C) {
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
	makeSizedFile(c, filepath.Join(l.dir, "foo.img"), gadget.SizeMiB+1, nil)
	makeSizedFile(c, filepath.Join(l.dir, "bar.img"), gadget.SizeMiB+1, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	constraints := gadget.PositioningConstraints{
		NonMBRStartOffset: 1 * gadget.SizeMiB,
		SectorSize:        512,
	}
	v, err := gadget.PositionVolume(l.dir, vol, constraints)
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot position structure #0: content "bar.img" does not fit in the structure`)
}

func (l *layoutTestSuite) TestVolumeLayoutErrorsContentTooLargeWithOffset(c *C) {
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
	makeSizedFile(c, filepath.Join(l.dir, "foo.img"), gadget.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot position structure #0: content "foo.img" does not fit in the structure`)
}

func (l *layoutTestSuite) TestVolumeLayoutErrorsContentLargerThanDeclared(c *C) {
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
	makeSizedFile(c, filepath.Join(l.dir, "foo.img"), gadget.SizeMiB+1, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot position structure #0: content "foo.img" size %v is larger than declared %v`, gadget.SizeMiB+1, gadget.SizeMiB))
}

func (l *layoutTestSuite) TestVolumeLayoutErrorsContentOverlap(c *C) {
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
	makeSizedFile(c, filepath.Join(l.dir, "foo.img"), gadget.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(l.dir, "bar.img"), gadget.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot position structure #0: content "foo.img" overlaps with preceding image "bar.img"`)
}

func (l *layoutTestSuite) TestVolumeLayoutContentExplicitOrder(c *C) {
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
	makeSizedFile(c, filepath.Join(l.dir, "foo.img"), gadget.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(l.dir, "bar.img"), gadget.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Size: 3 * gadget.SizeMiB,
		Structures: []gadget.PositionedStructure{
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size: 2 * gadget.SizeMiB,
					Type: "00000000-0000-0000-0000-0000deadbeef",
					Content: []gadget.VolumeContent{
						{Image: "foo.img", Size: gadget.SizeMiB, Offset: asSizePtr(gadget.SizeMiB)},
						{Image: "bar.img", Size: gadget.SizeMiB, Offset: asSizePtr(0)},
					},
				},
				StartOffset: 1 * gadget.SizeMiB,
				PositionedContent: []gadget.PositionedContent{
					{
						VolumeContent: &gadget.VolumeContent{Image: "bar.img", Size: gadget.SizeMiB, Offset: asSizePtr(0)},
						StartOffset:   1 * gadget.SizeMiB,
						Size:          gadget.SizeMiB,
					},
					{
						VolumeContent: &gadget.VolumeContent{Image: "foo.img", Size: gadget.SizeMiB, Offset: asSizePtr(gadget.SizeMiB)},
						StartOffset:   2 * gadget.SizeMiB,
						Size:          gadget.SizeMiB,
					},
				},
			},
		},
	})
}

func (l *layoutTestSuite) TestVolumeLayoutContentImplicitOrder(c *C) {
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
	makeSizedFile(c, filepath.Join(l.dir, "foo.img"), gadget.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(l.dir, "bar.img"), gadget.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Size: 3 * gadget.SizeMiB,
		Structures: []gadget.PositionedStructure{
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size: 2 * gadget.SizeMiB,
					Type: "00000000-0000-0000-0000-0000deadbeef",
					Content: []gadget.VolumeContent{
						{Image: "foo.img", Size: gadget.SizeMiB},
						{Image: "bar.img", Size: gadget.SizeMiB},
					},
				},
				StartOffset: 1 * gadget.SizeMiB,
				PositionedContent: []gadget.PositionedContent{
					{
						VolumeContent: &gadget.VolumeContent{Image: "foo.img", Size: gadget.SizeMiB},
						StartOffset:   1 * gadget.SizeMiB,
						Size:          gadget.SizeMiB,
					},
					{
						VolumeContent: &gadget.VolumeContent{Image: "bar.img", Size: gadget.SizeMiB},
						StartOffset:   2 * gadget.SizeMiB,
						Size:          gadget.SizeMiB,
					},
				},
			},
		},
	})
}

func (l *layoutTestSuite) TestVolumeLayoutContentImplicitSize(c *C) {
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
	size1_5MiB := gadget.SizeMiB + gadget.SizeMiB/2
	makeSizedFile(c, filepath.Join(l.dir, "foo.img"), size1_5MiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Size: 3 * gadget.SizeMiB,
		Structures: []gadget.PositionedStructure{
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:    2 * gadget.SizeMiB,
					Type:    "00000000-0000-0000-0000-0000deadbeef",
					Content: []gadget.VolumeContent{{Image: "foo.img"}},
				},
				StartOffset: 1 * gadget.SizeMiB,
				PositionedContent: []gadget.PositionedContent{
					{
						VolumeContent: &gadget.VolumeContent{Image: "foo.img"},
						StartOffset:   1 * gadget.SizeMiB,
						Size:          size1_5MiB,
					},
				},
			},
		},
	})
}

func (l *layoutTestSuite) TestVolumeLayoutContentNonBare(c *C) {
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
	makeSizedFile(c, filepath.Join(l.dir, "foo.txt"), 0, []byte("foobar\n"))

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Size: 3 * gadget.SizeMiB,
		Structures: []gadget.PositionedStructure{
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:       2 * gadget.SizeMiB,
					Type:       "00000000-0000-0000-0000-0000deadbeef",
					Filesystem: "ext4",
					Content:    []gadget.VolumeContent{{Source: "foo.txt", Target: "/boot"}},
				},
				StartOffset: 1 * gadget.SizeMiB,
			},
		},
	})
}

func (l *layoutTestSuite) TestVolumeLayoutConstraintsChange(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - role: mbr
          type: bare
          size: 446
          offset: 0
        - type: 00000000-0000-0000-0000-0000deadbeef
          filesystem: ext4
          size: 2M
          content:
              - source: foo.txt
                target: /boot
`
	makeSizedFile(c, filepath.Join(l.dir, "foo.txt"), 0, []byte("foobar\n"))

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.PositionVolume(l.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Size: 3 * gadget.SizeMiB,
		Structures: []gadget.PositionedStructure{
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:   446,
					Type:   "bare",
					Role:   gadget.MBR,
					Offset: asSizePtr(0),
				},
				StartOffset: 0,
				Index:       0,
			},
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:       2 * gadget.SizeMiB,
					Type:       "00000000-0000-0000-0000-0000deadbeef",
					Filesystem: "ext4",
					Content:    []gadget.VolumeContent{{Source: "foo.txt", Target: "/boot"}},
				},
				StartOffset: 1 * gadget.SizeMiB,
				Index:       1,
			},
		},
	})

	// still valid
	constraints := gadget.PositioningConstraints{
		// 512kiB
		NonMBRStartOffset: 512 * gadget.SizeKiB,
		SectorSize:        512,
	}
	v, err = gadget.PositionVolume(l.dir, vol, constraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Size: 2*gadget.SizeMiB + 512*gadget.SizeKiB,
		Structures: []gadget.PositionedStructure{
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:   446,
					Type:   "bare",
					Role:   gadget.MBR,
					Offset: asSizePtr(0),
				},
				StartOffset: 0,
				Index:       0,
			},
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:       2 * gadget.SizeMiB,
					Type:       "00000000-0000-0000-0000-0000deadbeef",
					Filesystem: "ext4",
					Content:    []gadget.VolumeContent{{Source: "foo.txt", Target: "/boot"}},
				},
				StartOffset: 512 * gadget.SizeKiB,
				Index:       1,
			},
		},
	})

	// constraints would make a non MBR structure overlap with MBR, but
	// structures start one after another unless offset is specified
	// explicitly
	constraintsBad := gadget.PositioningConstraints{
		NonMBRStartOffset: 400,
		SectorSize:        512,
	}
	v, err = gadget.PositionVolume(l.dir, vol, constraintsBad)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Size: 2*gadget.SizeMiB + 446,
		Structures: []gadget.PositionedStructure{
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:   446,
					Type:   "bare",
					Role:   gadget.MBR,
					Offset: asSizePtr(0),
				},
				Index: 0,
			},
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size:       2 * gadget.SizeMiB,
					Type:       "00000000-0000-0000-0000-0000deadbeef",
					Filesystem: "ext4",
					Content:    []gadget.VolumeContent{{Source: "foo.txt", Target: "/boot"}},
				},
				StartOffset: 446,
				Index:       1,
			},
		},
	})
}

func (l *layoutTestSuite) TestVolumeLayoutConstraintsSectorSize(c *C) {
	gadgetYaml := `
volumes:
  first:
    schema: gpt
    bootloader: grub
    structure:
        - role: mbr
          type: bare
          size: 446
          offset: 0
        - type: 00000000-0000-0000-0000-0000deadbeef
          filesystem: ext4
          size: 2M
          content:
              - source: foo.txt
                target: /boot
`
	makeSizedFile(c, filepath.Join(l.dir, "foo.txt"), 0, []byte("foobar\n"))

	vol := mustParseVolume(c, gadgetYaml, "first")

	constraintsBadSectorSize := gadget.PositioningConstraints{
		NonMBRStartOffset: 1 * gadget.SizeMiB,
		SectorSize:        384,
	}
	_, err := gadget.PositionVolume(l.dir, vol, constraintsBadSectorSize)
	c.Assert(err, ErrorMatches, "cannot position volume, structure #1 size is not a multiple of sector size 384")
}

func (l *layoutTestSuite) TestVolumeLayoutConstraintsNeedsSectorSize(c *C) {
	constraintsBadSectorSize := gadget.PositioningConstraints{
		NonMBRStartOffset: 1 * gadget.SizeMiB,
		// SectorSize left unspecified
	}
	_, err := gadget.PositionVolume(l.dir, &gadget.Volume{}, constraintsBadSectorSize)
	c.Assert(err, ErrorMatches, "cannot position volume, invalid constraints: sector size cannot be 0")
}
