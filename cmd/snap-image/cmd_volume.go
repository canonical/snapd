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
package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

type cmdPrepareVolume struct {
	Positional struct {
		PreparedRootDir string
		VolumeName      string
	} `positional-args:"yes" required:"yes"`
	WorkDir string `long:"work-dir"`
}

func init() {
	addCommand("prepare-volume",
		func() flags.Commander {
			return &cmdPrepareVolume{}
		},
		map[string]string{
			"work-dir": i18n.G("Work directory"),
		},
		[]argDesc{{
			name: "<image-root-dir>",
			desc: i18n.G("Prepared image root directory"),
		}, {
			name: "<volume-name>",
			desc: i18n.G("Volume name"),
		}})
}

func dumpVolumeInfo(pv *gadget.LaidOutVolume, rootfsForName map[string]string) {
	fmt.Fprintf(Stderr, "volume:\n")
	fmt.Fprintf(Stderr, "  size: %v\n", pv.Size)
	fmt.Fprintf(Stderr, "  schema: %v\n", pv.EffectiveSchema())
	fmt.Fprintf(Stderr, "  structures: \n")
	for _, ps := range pv.LaidOutStructure {
		fmt.Fprintf(Stderr, "     %v:\n", ps)
		fmt.Fprintf(Stderr, "       type: %v\n", ps.Type)
		fmt.Fprintf(Stderr, "       size: %v\n", ps.Size)
		fmt.Fprintf(Stderr, "       start-offset: %v\n", ps.StartOffset)
		erole := ps.EffectiveRole()
		if erole == "" {
			erole = "<none>"
		}
		fmt.Fprintf(Stderr, "       effective-role: %v\n", erole)
		fmt.Fprintf(Stderr, "       filesystem: %v\n", ps.Filesystem)
		fmt.Fprintf(Stderr, "       filesystem-label: %v\n", ps.EffectiveFilesystemLabel())
		fmt.Fprintf(Stderr, "       rootfs: %v\n", rootfsForName[ps.Name])
	}
}

func measureRootfsDu(where string) (int64, error) {
	cmd := exec.Command("du", "-s", "-B1", where)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("running du failed: %v", osutil.OutputErr(out, err))
	}
	fields := bytes.Fields(bytes.TrimSpace(out))
	if len(fields) != 2 {
		return 0, fmt.Errorf("unexpected output: %q", out)
	}

	val, err := strconv.ParseInt(string(fields[0]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse %q: %v", fields[0], err)
	}

	return val, nil
}

func alignSize(size, alignment int64) int64 {
	if size%alignment == 0 {
		return size
	}
	return size + alignment - size%alignment
}

func estimateSize(size, alignment int64) gadget.Size {
	// add some overhead
	size = int64(math.Ceil(float64(size) * 1.5))
	// adjust size to be multiple of sector size
	size = alignSize(size, alignment)
	// 8MB of extra padding for ext4
	size += 8 * 1024 * 1024
	return gadget.Size(size)
}

var measureRootfs = measureRootfsDu

func setupSystemData(vol *gadget.Volume, rootfsDir string, autoAdd bool) error {
	var systemData *gadget.VolumeStructure

	for i, vs := range vol.Structure {
		if vs.EffectiveRole() == gadget.SystemData {
			systemData = &vol.Structure[i]
			break
		}
	}

	size, err := measureRootfs(rootfsDir)
	if err != nil {
		return fmt.Errorf("cannot calculate the size of root filesystem: %v", err)
	}

	if systemData == nil {
		// system-data not defined for this volume

		if !autoAdd {
			// not adding one automatically
			return nil
		}

		// system data not defined, add it
		vol.Structure = append(vol.Structure, gadget.VolumeStructure{
			Name:       "writable",
			Size:       estimateSize(size, int64(defaultConstraints.SectorSize)),
			Role:       "system-data",
			Label:      "writable",
			Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			Filesystem: "ext4",
			Content: []gadget.VolumeContent{
				{Source: "/", Target: "/system-data/"},
			},
		})
		return nil
	}

	if systemData.Size < gadget.Size(size) {
		return fmt.Errorf("rootfs size %v is larger than declared system-data size %v", size, systemData.Size)
	}
	if systemData.Name == "" {
		systemData.Name = "writable"
	}
	if len(systemData.Content) == 0 {
		systemData.Content = []gadget.VolumeContent{
			{Source: "/", Target: "/system-data/"},
		}
	}
	return nil
}

var defaultConstraints = gadget.LayoutConstraints{
	SectorSize:        512,
	NonMBRStartOffset: 1 * gadget.SizeMiB,
}

func (x *cmdPrepareVolume) Execute(args []string) error {
	gadgetRootDir := filepath.Join(x.Positional.PreparedRootDir, "gadget")
	rootfsDir := filepath.Join(x.Positional.PreparedRootDir, "image")

	if x.WorkDir != "" && !osutil.IsDirectory(x.WorkDir) {
		return fmt.Errorf("work directory %q does not exist", x.WorkDir)
	}

	gi, err := gadget.ReadInfo(gadgetRootDir, false)
	if err != nil {
		return err
	}

	vol, ok := gi.Volumes[x.Positional.VolumeName]
	if !ok {
		return fmt.Errorf("volume %q not defined", x.Positional.VolumeName)
	}

	autoAdd := len(gi.Volumes) == 1
	if err := setupSystemData(&vol, rootfsDir, autoAdd); err != nil {
		return err
	}

	rootfsForStruct := make(map[string]string, len(vol.Structure))
	for _, s := range vol.Structure {
		switch s.EffectiveRole() {
		case "system-boot", "mbr":
			rootfsForStruct[s.Name] = gadgetRootDir
		case "system-data":
			rootfsForStruct[s.Name] = rootfsDir
		case "":
			// fallback to gadget rootfs
			rootfsForStruct[s.Name] = gadgetRootDir
		}
	}

	pv, err := gadget.LayoutVolume(gadgetRootDir, &vol, defaultConstraints)
	if err != nil {
		return fmt.Errorf("cannot lay out volume %q: %v", x.Positional.VolumeName, err)
	}

	// GPT schema uses a GPT header at the start of the image and a backup
	// header at the end. The backup header size is 34 * LBA size (512B)
	if pv.EffectiveSchema() == gadget.GPT {
		pv.Size += 34 * pv.SectorSize
	}

	dumpVolumeInfo(pv, rootfsForStruct)

	workDir := x.WorkDir
	if workDir == "" {
		tmp, err := ioutil.TempDir("", "snap-image-")
		if err != nil {
			return fmt.Errorf("cannot prepare temporary work directory: %v", err)
		}
		workDir = tmp
	}

	imageData := ImageData{
		PreparedImageDir: x.Positional.PreparedRootDir,
		StructRootfs:     rootfsForStruct,
	}
	ib := NewImageBuilder(workDir, pv)
	logger.Noticef("work directory: %v", workDir)

	output, err := ib.BuildVolume(imageData, rootfsForStruct)
	if err != nil {
		return err
	}
	fmt.Fprintf(Stdout, "image written to: %v\n", output)
	return nil
}

type ImageData struct {
	PreparedImageDir string
	StructRootfs     map[string]string
}

func (id *ImageData) RootfsForStruct(ps *gadget.LaidOutStructure) (string, error) {
	rootfs, ok := id.StructRootfs[ps.Name]
	if !ok {
		return "", fmt.Errorf("no rootfs for structure %v", ps)
	}
	return rootfs, nil
}

type ImageBuilder struct {
	workDir string
	vol     *gadget.LaidOutVolume
}

func NewImageBuilder(workDir string, vol *gadget.LaidOutVolume) *ImageBuilder {
	return &ImageBuilder{
		workDir: workDir,
		vol:     vol,
	}
}

func (ib *ImageBuilder) fileForStructure(ps *gadget.LaidOutStructure) string {
	return filepath.Join(ib.workDir, fmt.Sprintf("part-%04d.img", ps.Index))
}

func (ib *ImageBuilder) fileInWork(name string) string {
	return filepath.Join(ib.workDir, name)
}

func (ib *ImageBuilder) BuildVolume(data ImageData, rootForName map[string]string) (output string, err error) {
	for _, vs := range ib.vol.LaidOutStructure {
		simg := ib.fileForStructure(&vs)
		if err := ib.prepareStructure(data, simg, &vs); err != nil {
			return "", fmt.Errorf("cannot write structure %v: %v", vs, err)
		}
	}

	imgName := ib.fileInWork("output.img")
	logger.Noticef("writing image to %v", imgName)

	out, err := makeSizedFile(imgName, ib.vol.Size)
	if err != nil {
		return "", fmt.Errorf("cannot prepare image file: %v", err)
	}
	defer out.Close()

	if err := gadget.Partition(out.Name(), ib.vol); err != nil {
		return "", fmt.Errorf("cannot partition volume: %v", err)
	}

	for _, vs := range ib.vol.LaidOutStructure {
		simg := ib.fileForStructure(&vs)
		if err := writeStructureToVolume(out, simg, &vs); err != nil {
			return "", fmt.Errorf("cannot write structure %v to volume: %v", vs, err)
		}
	}

	for _, vs := range ib.vol.LaidOutStructure {
		ow, err := gadget.NewOffsetWriter(&vs, ib.vol.SectorSize)
		if err != nil {
			return "", fmt.Errorf("cannot create offset writer: %v", err)
		}
		if err := ow.Write(out); err != nil {
			return "", fmt.Errorf("cannot populate offset-write for %v: %v", vs, err)
		}
	}

	return imgName, nil
}

func writeStructureToVolume(out io.WriteSeeker, from string, vs *gadget.LaidOutStructure) error {
	in, err := os.Open(from)
	if err != nil {
		return fmt.Errorf("cannot open image file: %v", err)
	}
	defer in.Close()

	if _, err := out.Seek(int64(vs.StartOffset), io.SeekStart); err != nil {
		return fmt.Errorf("cannot seek to position %v: %v", vs.StartOffset, err)
	}

	if _, err := io.CopyN(out, in, int64(vs.Size)); err != nil {
		return fmt.Errorf("cannot copy source image to destination: %v", err)
	}
	return nil
}

func makeSizedFile(name string, size gadget.Size) (*os.File, error) {
	out, err := os.Create(name)
	if err != nil {
		return nil, fmt.Errorf("cannot create file: %v", err)
	}

	if err := out.Truncate(int64(size)); err != nil {
		out.Close()
		return nil, fmt.Errorf("cannot resize file to %v bytes: %v", size, err)
	}
	return out, nil
}

func (ib *ImageBuilder) prepareStructure(data ImageData, img string, vs *gadget.LaidOutStructure) error {
	rootDir, err := data.RootfsForStruct(vs)
	if err != nil {
		return fmt.Errorf("cannot find data directory for %v: %v", vs, err)
	}

	logger.Noticef("structure %v:\n  image file: %v\n  rootfs: %v", vs, img, rootDir)

	out, err := makeSizedFile(img, vs.Size)
	if err != nil {
		return fmt.Errorf("cannot prepare structure image file: %v", err)
	}

	defer out.Close()

	if vs.IsBare() {
		return ib.prepareRawStructure(data, out, rootDir, vs)
	} else {
		return ib.prepareFilesystemStructure(data, out, rootDir, vs)
	}
}

func (ib *ImageBuilder) prepareRawStructure(_ ImageData, out *os.File, rootDir string, vs *gadget.LaidOutStructure) error {
	// each structure is written to a partition file, thus we need to apply
	// bias to have them start at 0 offset of the output file
	shifted := gadget.ShiftStructureTo(*vs, 0)
	raw, err := gadget.NewRawStructureWriter(rootDir, &shifted)
	if err != nil {
		return fmt.Errorf("cannot prepare image writer: %v", err)
	}

	if err := raw.Write(out); err != nil {
		return fmt.Errorf("cannot write image: %v", err)
	}

	return nil
}

func copyTree(src, dst string) error {
	fis, err := ioutil.ReadDir(src)
	if err != nil {
		return fmt.Errorf("cannot list directory entries: %v", err)
	}

	for _, fi := range fis {
		pSrc := filepath.Join(src, fi.Name())
		pDst := filepath.Join(dst, fi.Name())
		if fi.IsDir() {
			if err := os.MkdirAll(pDst, 0755); err != nil {
				return fmt.Errorf("cannot create directory prefix: %v", err)
			}
			if err := copyTree(pSrc, pDst); err != nil {
				return err
			}
		} else {
			// overwrite & sync by default
			copyFlags := osutil.CopyFlagOverwrite | osutil.CopyFlagSync
			if err := osutil.CopyFile(pSrc, pDst, copyFlags); err != nil {
				return err
			}
		}
	}
	return nil
}

func (ib *ImageBuilder) fixupSystemBoot(data ImageData) gadget.PostStageFunc {
	return func(where string, vs *gadget.LaidOutStructure) error {
		logger.Noticef("%v role? %v", vs, vs.EffectiveRole())
		if vs.EffectiveRole() != "system-boot" {
			return nil
		}

		var fromDir, toDir string

		switch ib.vol.Bootloader {
		case "grub":
			fromDir = filepath.Join(data.PreparedImageDir, "image", "boot", "grub")
			toDir = filepath.Join(where, "EFI", "ubuntu")
		case "u-boot":
			fromDir = filepath.Join(data.PreparedImageDir, "image", "boot", "uboot")
			toDir = where
		default:
			return fmt.Errorf("unsupported bootloader %q", ib.vol.Bootloader)
		}

		logger.Noticef("populate system boot env files in %v from %v ", toDir, fromDir)

		// TODO: error out when bootloader bits are not present?
		if osutil.IsDirectory(fromDir) {
			if err := copyTree(fromDir, toDir); err != nil {
				return err
			}
		}

		return nil
	}
}

func (ib *ImageBuilder) prepareFilesystemStructure(data ImageData, out *os.File, rootDir string, vs *gadget.LaidOutStructure) error {
	fname := out.Name()

	logger.Noticef("root dir for %v: %v", vs, rootDir)
	fs, err := gadget.NewFilesystemImageWriter(rootDir, vs, ib.workDir)
	if err != nil {
		return fmt.Errorf("cannot create filesystem image writer: %v", err)
	}

	var postStage gadget.PostStageFunc
	if vs.EffectiveRole() == "system-boot" {
		// boot filesystem gets extra steps
		postStage = ib.fixupSystemBoot(data)
	}

	if err := fs.Write(fname, postStage); err != nil {
		return fmt.Errorf("cannot create filesystem image: %v", err)
	}

	return nil
}
