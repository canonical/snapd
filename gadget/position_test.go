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

type positioningTestSuite struct {
	dir string
}

var _ = Suite(&positioningTestSuite{})

func (p *positioningTestSuite) SetUpTest(c *C) {
	p.dir = c.MkDir()
}

var defaultConstraints = gadget.PositioningConstraints{
	NonMBRStartOffset: 1 * gadget.SizeMiB,
	SectorSize:        512,
}

func (p *positioningTestSuite) TestVolumeSize(c *C) {
	vol := gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Size: 2 * gadget.SizeMiB},
		},
	}
	v, err := gadget.PositionVolume(p.dir, &vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Volume: &gadget.Volume{
			Structure: []gadget.VolumeStructure{
				{Size: 2 * gadget.SizeMiB},
			},
		},
		Size: 3 * gadget.SizeMiB,
		PositionedStructure: []gadget.PositionedStructure{
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

func (p *positioningTestSuite) TestVolumePositionMinimal(c *C) {
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

	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Volume: vol,
		Size:   501 * gadget.SizeMiB,
		PositionedStructure: []gadget.PositionedStructure{
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     1 * gadget.SizeMiB,
				Index:           0,
			},
			{
				VolumeStructure: &vol.Structure[1],
				StartOffset:     401 * gadget.SizeMiB,
				Index:           1,
			},
		},
	})
}

func (p *positioningTestSuite) TestVolumePositionImplicitOrdering(c *C) {
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

	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Volume: vol,
		Size:   1101 * gadget.SizeMiB,
		PositionedStructure: []gadget.PositionedStructure{
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     1 * gadget.SizeMiB,
				Index:           0,
			},
			{
				VolumeStructure: &vol.Structure[1],
				StartOffset:     401 * gadget.SizeMiB,
				Index:           1,
			},
			{
				VolumeStructure: &vol.Structure[2],
				StartOffset:     901 * gadget.SizeMiB,
				Index:           2,
			},
			{
				VolumeStructure: &vol.Structure[3],
				StartOffset:     1001 * gadget.SizeMiB,
				Index:           3,
			},
		},
	})
}

func (p *positioningTestSuite) TestVolumePositionExplicitOrdering(c *C) {
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

	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Volume: vol,
		Size:   1300 * gadget.SizeMiB,
		PositionedStructure: []gadget.PositionedStructure{
			{
				VolumeStructure: &vol.Structure[3],
				StartOffset:     1 * gadget.SizeMiB,
				Index:           3,
			},
			{
				VolumeStructure: &vol.Structure[1],
				StartOffset:     200 * gadget.SizeMiB,
				Index:           1,
			},
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     800 * gadget.SizeMiB,
				Index:           0,
			},
			{
				VolumeStructure: &vol.Structure[2],
				StartOffset:     1200 * gadget.SizeMiB,
				Index:           2,
			},
		},
	})
}

func (p *positioningTestSuite) TestVolumePositionMixedOrdering(c *C) {
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

	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Volume: vol,
		Size:   1200 * gadget.SizeMiB,
		PositionedStructure: []gadget.PositionedStructure{
			{
				VolumeStructure: &vol.Structure[3],
				StartOffset:     1 * gadget.SizeMiB,
				Index:           3,
			},
			{
				VolumeStructure: &vol.Structure[1],
				StartOffset:     200 * gadget.SizeMiB,
				Index:           1,
			},
			{
				VolumeStructure: &vol.Structure[2],
				StartOffset:     700 * gadget.SizeMiB,
				Index:           2,
			},
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     800 * gadget.SizeMiB,
				Index:           0,
			},
		},
	})
}

func (p *positioningTestSuite) TestVolumePositionErrorsContentNoSuchFile(c *C) {
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
	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
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

func (p *positioningTestSuite) TestVolumePositionErrorsContentTooLargeSingle(c *C) {
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB+1, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot position structure #0: content "foo.img" does not fit in the structure`)
}

func (p *positioningTestSuite) TestVolumePositionErrorsContentTooLargeMany(c *C) {
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB+1, nil)
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), gadget.SizeMiB+1, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	constraints := gadget.PositioningConstraints{
		NonMBRStartOffset: 1 * gadget.SizeMiB,
		SectorSize:        512,
	}
	v, err := gadget.PositionVolume(p.dir, vol, constraints)
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot position structure #0: content "bar.img" does not fit in the structure`)
}

func (p *positioningTestSuite) TestVolumePositionErrorsContentTooLargeWithOffset(c *C) {
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot position structure #0: content "foo.img" does not fit in the structure`)
}

func (p *positioningTestSuite) TestVolumePositionErrorsContentLargerThanDeclared(c *C) {
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB+1, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot position structure #0: content "foo.img" size %v is larger than declared %v`, gadget.SizeMiB+1, gadget.SizeMiB))
}

func (p *positioningTestSuite) TestVolumePositionErrorsContentOverlap(c *C) {
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), gadget.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot position structure #0: content "foo.img" overlaps with preceding image "bar.img"`)
}

func (p *positioningTestSuite) TestVolumePositionContentExplicitOrder(c *C) {
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), gadget.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 1)
	c.Assert(vol.Structure[0].Content, HasLen, 2)

	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Volume: vol,
		Size:   3 * gadget.SizeMiB,
		PositionedStructure: []gadget.PositionedStructure{
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     1 * gadget.SizeMiB,
				PositionedContent: []gadget.PositionedContent{
					{
						VolumeContent: &vol.Structure[0].Content[1],
						StartOffset:   1 * gadget.SizeMiB,
						Size:          gadget.SizeMiB,
					},
					{
						VolumeContent: &vol.Structure[0].Content[0],
						StartOffset:   2 * gadget.SizeMiB,
						Size:          gadget.SizeMiB,
					},
				},
			},
		},
	})
}

func (p *positioningTestSuite) TestVolumePositionContentImplicitOrder(c *C) {
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), gadget.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 1)
	c.Assert(vol.Structure[0].Content, HasLen, 2)

	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Volume: vol,
		Size:   3 * gadget.SizeMiB,
		PositionedStructure: []gadget.PositionedStructure{
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     1 * gadget.SizeMiB,
				PositionedContent: []gadget.PositionedContent{
					{
						VolumeContent: &vol.Structure[0].Content[0],
						StartOffset:   1 * gadget.SizeMiB,
						Size:          gadget.SizeMiB,
					},
					{
						VolumeContent: &vol.Structure[0].Content[1],
						StartOffset:   2 * gadget.SizeMiB,
						Size:          gadget.SizeMiB,
					},
				},
			},
		},
	})
}

func (p *positioningTestSuite) TestVolumePositionContentImplicitSize(c *C) {
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), size1_5MiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 1)
	c.Assert(vol.Structure[0].Content, HasLen, 1)

	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Volume: vol,
		Size:   3 * gadget.SizeMiB,
		PositionedStructure: []gadget.PositionedStructure{
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     1 * gadget.SizeMiB,
				PositionedContent: []gadget.PositionedContent{
					{
						VolumeContent: &vol.Structure[0].Content[0],
						StartOffset:   1 * gadget.SizeMiB,
						Size:          size1_5MiB,
					},
				},
			},
		},
	})
}

func (p *positioningTestSuite) TestVolumePositionContentNonBare(c *C) {
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

	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Volume: vol,
		Size:   3 * gadget.SizeMiB,
		PositionedStructure: []gadget.PositionedStructure{
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     1 * gadget.SizeMiB,
			},
		},
	})
}

func (p *positioningTestSuite) TestVolumePositionConstraintsChange(c *C) {
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.txt"), 0, []byte("foobar\n"))

	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 2)
	c.Assert(vol.Structure[1].Content, HasLen, 1)

	v, err := gadget.PositionVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Volume: vol,
		Size:   3 * gadget.SizeMiB,
		PositionedStructure: []gadget.PositionedStructure{
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     0,
				Index:           0,
			},
			{
				VolumeStructure: &vol.Structure[1],
				StartOffset:     1 * gadget.SizeMiB,
				Index:           1,
			},
		},
	})

	// still valid
	constraints := gadget.PositioningConstraints{
		// 512kiB
		NonMBRStartOffset: 512 * gadget.SizeKiB,
		SectorSize:        512,
	}
	v, err = gadget.PositionVolume(p.dir, vol, constraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Volume: vol,
		Size:   2*gadget.SizeMiB + 512*gadget.SizeKiB,
		PositionedStructure: []gadget.PositionedStructure{
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     0,
				Index:           0,
			},
			{
				VolumeStructure: &vol.Structure[1],
				StartOffset:     512 * gadget.SizeKiB,
				Index:           1,
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
	v, err = gadget.PositionVolume(p.dir, vol, constraintsBad)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.PositionedVolume{
		Volume: vol,
		Size:   2*gadget.SizeMiB + 446,
		PositionedStructure: []gadget.PositionedStructure{
			{
				VolumeStructure: &vol.Structure[0],
				Index:           0,
			},
			{
				VolumeStructure: &vol.Structure[1],
				StartOffset:     446,
				Index:           1,
			},
		},
	})
}

func (p *positioningTestSuite) TestVolumePositionConstraintsSectorSize(c *C) {
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.txt"), 0, []byte("foobar\n"))

	vol := mustParseVolume(c, gadgetYaml, "first")

	constraintsBadSectorSize := gadget.PositioningConstraints{
		NonMBRStartOffset: 1 * gadget.SizeMiB,
		SectorSize:        384,
	}
	_, err := gadget.PositionVolume(p.dir, vol, constraintsBadSectorSize)
	c.Assert(err, ErrorMatches, "cannot position volume, structure #1 size is not a multiple of sector size 384")
}

func (p *positioningTestSuite) TestVolumePositionConstraintsNeedsSectorSize(c *C) {
	constraintsBadSectorSize := gadget.PositioningConstraints{
		NonMBRStartOffset: 1 * gadget.SizeMiB,
		// SectorSize left unspecified
	}
	_, err := gadget.PositionVolume(p.dir, &gadget.Volume{}, constraintsBadSectorSize)
	c.Assert(err, ErrorMatches, "cannot position volume, invalid constraints: sector size cannot be 0")
}
