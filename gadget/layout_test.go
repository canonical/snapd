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

func (p *layoutTestSuite) SetUpTest(c *C) {
	p.dir = c.MkDir()
}

var defaultConstraints = gadget.LayoutConstraints{
	NonMBRStartOffset: 1 * gadget.SizeMiB,
	SectorSize:        512,
}

func (p *layoutTestSuite) TestVolumeSize(c *C) {
	vol := gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Size: 2 * gadget.SizeMiB},
		},
	}
	v, err := gadget.LayoutVolume(p.dir, &vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume: &gadget.Volume{
			Structure: []gadget.VolumeStructure{
				{Size: 2 * gadget.SizeMiB},
			},
		},
		Size:       3 * gadget.SizeMiB,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
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

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       501 * gadget.SizeMiB,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
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

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       1101 * gadget.SizeMiB,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
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

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       1300 * gadget.SizeMiB,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
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

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)

	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       1200 * gadget.SizeMiB,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
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
	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, `cannot lay out structure #0: content "foo.img":.*no such file or directory`)
}

func makeSizedFile(c *C, path string, size gadget.Size, content []byte) {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	c.Assert(err, IsNil)

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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB+1, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB+1, nil)
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), gadget.SizeMiB+1, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	constraints := gadget.LayoutConstraints{
		NonMBRStartOffset: 1 * gadget.SizeMiB,
		SectorSize:        512,
	}
	v, err := gadget.LayoutVolume(p.dir, vol, constraints)
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB+1, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(v, IsNil)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot lay out structure #0: content "foo.img" size %v is larger than declared %v`, gadget.SizeMiB+1, gadget.SizeMiB))
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), gadget.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), gadget.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 1)
	c.Assert(vol.Structure[0].Content, HasLen, 2)

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       3 * gadget.SizeMiB,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     1 * gadget.SizeMiB,
				LaidOutContent: []gadget.LaidOutContent{
					{
						VolumeContent: &vol.Structure[0].Content[1],
						StartOffset:   1 * gadget.SizeMiB,
						Size:          gadget.SizeMiB,
						Index:         1,
					},
					{
						VolumeContent: &vol.Structure[0].Content[0],
						StartOffset:   2 * gadget.SizeMiB,
						Size:          gadget.SizeMiB,
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), gadget.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), gadget.SizeMiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 1)
	c.Assert(vol.Structure[0].Content, HasLen, 2)

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       3 * gadget.SizeMiB,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     1 * gadget.SizeMiB,
				LaidOutContent: []gadget.LaidOutContent{
					{
						VolumeContent: &vol.Structure[0].Content[0],
						StartOffset:   1 * gadget.SizeMiB,
						Size:          gadget.SizeMiB,
						Index:         0,
					},
					{
						VolumeContent: &vol.Structure[0].Content[1],
						StartOffset:   2 * gadget.SizeMiB,
						Size:          gadget.SizeMiB,
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
	size1_5MiB := gadget.SizeMiB + gadget.SizeMiB/2
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), size1_5MiB, nil)

	vol := mustParseVolume(c, gadgetYaml, "first")
	c.Assert(vol.Structure, HasLen, 1)
	c.Assert(vol.Structure[0].Content, HasLen, 1)

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       3 * gadget.SizeMiB,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     1 * gadget.SizeMiB,
				LaidOutContent: []gadget.LaidOutContent{
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

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       3 * gadget.SizeMiB,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				VolumeStructure: &vol.Structure[0],
				StartOffset:     1 * gadget.SizeMiB,
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumeConstraintsChange(c *C) {
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

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       3 * gadget.SizeMiB,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
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
	constraints := gadget.LayoutConstraints{
		// 512kiB
		NonMBRStartOffset: 512 * gadget.SizeKiB,
		SectorSize:        512,
	}
	v, err = gadget.LayoutVolume(p.dir, vol, constraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       2*gadget.SizeMiB + 512*gadget.SizeKiB,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
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
	constraintsBad := gadget.LayoutConstraints{
		NonMBRStartOffset: 400,
		SectorSize:        512,
	}
	v, err = gadget.LayoutVolume(p.dir, vol, constraintsBad)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       2*gadget.SizeMiB + 446,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
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

	// sector size is properly recorded
	constraintsSector := gadget.LayoutConstraints{
		NonMBRStartOffset: 1 * gadget.SizeMiB,
		SectorSize:        1024,
	}
	v, err = gadget.LayoutVolume(p.dir, vol, constraintsSector)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       3 * gadget.SizeMiB,
		SectorSize: 1024,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				VolumeStructure: &vol.Structure[0],
				Index:           0,
			},
			{
				VolumeStructure: &vol.Structure[1],
				StartOffset:     1 * gadget.SizeMiB,
				Index:           1,
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumeConstraintsSectorSize(c *C) {
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

	constraintsBadSectorSize := gadget.LayoutConstraints{
		NonMBRStartOffset: 1 * gadget.SizeMiB,
		SectorSize:        384,
	}
	_, err := gadget.LayoutVolume(p.dir, vol, constraintsBadSectorSize)
	c.Assert(err, ErrorMatches, "cannot lay out volume, structure #1 size is not a multiple of sector size 384")
}

func (p *layoutTestSuite) TestLayoutVolumeConstraintsNeedsSectorSize(c *C) {
	constraintsBadSectorSize := gadget.LayoutConstraints{
		NonMBRStartOffset: 1 * gadget.SizeMiB,
		// SectorSize left unspecified
	}
	_, err := gadget.LayoutVolume(p.dir, &gadget.Volume{}, constraintsBadSectorSize)
	c.Assert(err, ErrorMatches, "cannot lay out volume, invalid constraints: sector size cannot be 0")
}

func (p *layoutTestSuite) TestLayoutVolumeMBRImplicitConstraints(c *C) {
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

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       2 * gadget.SizeMiB,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				// MBR
				VolumeStructure: &vol.Structure[0],
				StartOffset:     0,
				Index:           0,
			}, {
				VolumeStructure: &vol.Structure[1],
				StartOffset:     1 * gadget.SizeMiB,
				Index:           1,
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumeOffsetWriteAll(c *C) {
	var gadgetYaml = `
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), 200*gadget.SizeKiB, []byte(""))
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), 150*gadget.SizeKiB, []byte(""))

	vol := mustParseVolume(c, gadgetYaml, "pc")
	c.Assert(vol.Structure, HasLen, 3)

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, &gadget.LaidOutVolume{
		Volume:     vol,
		Size:       3 * gadget.SizeMiB,
		SectorSize: 512,
		RootDir:    p.dir,
		LaidOutStructure: []gadget.LaidOutStructure{
			{
				// mbr
				VolumeStructure: &vol.Structure[0],
				StartOffset:     0,
				Index:           0,
			}, {
				// foo
				VolumeStructure: &vol.Structure[1],
				StartOffset:     1 * gadget.SizeMiB,
				Index:           1,
				// break for gofmt < 1.11
				AbsoluteOffsetWrite: asSizePtr(92),
				LaidOutContent: []gadget.LaidOutContent{
					{
						VolumeContent: &vol.Structure[1].Content[0],
						Size:          200 * gadget.SizeKiB,
						StartOffset:   1 * gadget.SizeMiB,
						// offset-write: bar+10
						AbsoluteOffsetWrite: asSizePtr(2*gadget.SizeMiB + 10),
					},
				},
			}, {
				// bar
				VolumeStructure: &vol.Structure[2],
				StartOffset:     2 * gadget.SizeMiB,
				Index:           2,
				// break for gofmt < 1.11
				AbsoluteOffsetWrite: asSizePtr(600),
				LaidOutContent: []gadget.LaidOutContent{
					{
						VolumeContent: &vol.Structure[2].Content[0],
						Size:          150 * gadget.SizeKiB,
						StartOffset:   2 * gadget.SizeMiB,
						// offset-write: bar+10
						AbsoluteOffsetWrite: asSizePtr(450),
					},
				},
			},
		},
	})
}

func (p *layoutTestSuite) TestLayoutVolumeOffsetWriteBadRelativeTo(c *C) {
	// define volumes explicitly as those would not pass validation
	volBadStructure := gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{
				Name: "foo",
				Type: "DA,21686148-6449-6E6F-744E-656564454649",
				Size: 1 * gadget.SizeMiB,
				OffsetWrite: &gadget.RelativeOffset{
					RelativeTo: "bar",
					Offset:     10,
				},
			},
		},
	}
	volBadContent := gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{
				Name: "foo",
				Type: "DA,21686148-6449-6E6F-744E-656564454649",
				Size: 1 * gadget.SizeMiB,
				Content: []gadget.VolumeContent{
					{
						Image: "foo.img",
						OffsetWrite: &gadget.RelativeOffset{
							RelativeTo: "bar",
							Offset:     10,
						},
					},
				},
			},
		},
	}

	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), 200*gadget.SizeKiB, []byte(""))

	v, err := gadget.LayoutVolume(p.dir, &volBadStructure, defaultConstraints)
	c.Check(v, IsNil)
	c.Check(err, ErrorMatches, `cannot resolve offset-write of structure #0 \("foo"\): refers to an unknown structure "bar"`)

	v, err = gadget.LayoutVolume(p.dir, &volBadContent, defaultConstraints)
	c.Check(v, IsNil)
	c.Check(err, ErrorMatches, `cannot resolve offset-write of structure #0 \("foo"\) content "foo.img": refers to an unknown structure "bar"`)
}

func (p *layoutTestSuite) TestLayoutVolumeOffsetWriteEnlargesVolume(c *C) {
	var gadgetYamlStructure = `
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
        # 1GB
        offset-write: mbr+1073741824

`
	vol := mustParseVolume(c, gadgetYamlStructure, "pc")

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	// offset-write is at 1GB
	c.Check(v.Size, Equals, 1*gadget.SizeGiB+gadget.SizeLBA48Pointer)

	var gadgetYamlContent = `
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
        content:
          - image: foo.img
            # 2GB
            offset-write: mbr+2147483648
          - image: bar.img
            # 1GB
            offset-write: mbr+1073741824
          - image: baz.img
            # 3GB
            offset-write: mbr+3221225472

`
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), 200*gadget.SizeKiB, []byte(""))
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), 150*gadget.SizeKiB, []byte(""))
	makeSizedFile(c, filepath.Join(p.dir, "baz.img"), 100*gadget.SizeKiB, []byte(""))

	vol = mustParseVolume(c, gadgetYamlContent, "pc")

	v, err = gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	// foo.img offset-write is at 3GB
	c.Check(v.Size, Equals, 3*gadget.SizeGiB+gadget.SizeLBA48Pointer)
}

func (p *layoutTestSuite) TestLaidOutStructureShift(c *C) {
	var gadgetYamlContent = `
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
	makeSizedFile(c, filepath.Join(p.dir, "foo.img"), 200*gadget.SizeKiB, []byte(""))
	makeSizedFile(c, filepath.Join(p.dir, "bar.img"), 150*gadget.SizeKiB, []byte(""))

	vol := mustParseVolume(c, gadgetYamlContent, "pc")

	v, err := gadget.LayoutVolume(p.dir, vol, defaultConstraints)
	c.Assert(err, IsNil)
	c.Assert(v.LaidOutStructure, HasLen, 1)
	c.Assert(v.LaidOutStructure[0].LaidOutContent, HasLen, 2)

	ps := v.LaidOutStructure[0]

	c.Assert(ps, DeepEquals, gadget.LaidOutStructure{
		// foo
		VolumeStructure: &vol.Structure[0],
		StartOffset:     1 * gadget.SizeMiB,
		Index:           0,
		LaidOutContent: []gadget.LaidOutContent{
			{
				VolumeContent: &vol.Structure[0].Content[0],
				Size:          200 * gadget.SizeKiB,
				StartOffset:   1 * gadget.SizeMiB,
				Index:         0,
			}, {
				VolumeContent: &vol.Structure[0].Content[1],
				Size:          150 * gadget.SizeKiB,
				StartOffset:   1*gadget.SizeMiB + 300*gadget.SizeKiB,
				Index:         1,
			},
		},
	})

	shiftedTo0 := gadget.ShiftStructureTo(ps, 0)
	c.Assert(shiftedTo0, DeepEquals, gadget.LaidOutStructure{
		// foo
		VolumeStructure: &vol.Structure[0],
		StartOffset:     0,
		Index:           0,
		LaidOutContent: []gadget.LaidOutContent{
			{
				VolumeContent: &vol.Structure[0].Content[0],
				Size:          200 * gadget.SizeKiB,
				StartOffset:   0,
				Index:         0,
			}, {
				VolumeContent: &vol.Structure[0].Content[1],
				Size:          150 * gadget.SizeKiB,
				StartOffset:   300 * gadget.SizeKiB,
				Index:         1,
			},
		},
	})

	shiftedTo2M := gadget.ShiftStructureTo(ps, 2*gadget.SizeMiB)
	c.Assert(shiftedTo2M, DeepEquals, gadget.LaidOutStructure{
		// foo
		VolumeStructure: &vol.Structure[0],
		StartOffset:     2 * gadget.SizeMiB,
		Index:           0,
		LaidOutContent: []gadget.LaidOutContent{
			{
				VolumeContent: &vol.Structure[0].Content[0],
				Size:          200 * gadget.SizeKiB,
				StartOffset:   2 * gadget.SizeMiB,
				Index:         0,
			}, {
				VolumeContent: &vol.Structure[0].Content[1],
				Size:          150 * gadget.SizeKiB,
				StartOffset:   2*gadget.SizeMiB + 300*gadget.SizeKiB,
				Index:         1,
			},
		},
	})
}
