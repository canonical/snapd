// -*- Mode: Go; indent-tabs-mode: t -*-

package gadget

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/systemd"
)

func GenerateRepartConfig(gadgetRoot string, vol *Volume, encrypt bool, observer ContentObserver) error {
	outputdir := "/run/repart.d"
	os.MkdirAll(outputdir, 0o777)
	for idx, struc := range vol.Structure {
		if struc.Type == "mbr" {
			logger.Noticef("Ignoring MBR")
			continue
		}
		if struc.OffsetWrite != nil {
			logger.Noticef("Ignoring offset-write")
		}
		if struc.Offset != nil {
			logger.Noticef("Ignoring offset")
		}

		path := filepath.Join(outputdir, fmt.Sprintf("%02d-%v.conf", idx, systemd.EscapeUnitNamePath(struc.Name)))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o666)
		if err != nil {
			return err
		}
		defer f.Close()
		fmt.Fprintf(f, "[Partition]\n")
		fmt.Fprintf(f, "Type=%v\n", strings.Split(struc.Type, ",")[1])
		if struc.Label != "" {
			if encrypt && (struc.Role == "system-data" || struc.Role == "system-save") {
				fmt.Fprintf(f, "Label=%v-enc\n", struc.Label)
			} else {
				fmt.Fprintf(f, "Label=%v\n", struc.Label)
			}
		} else if struc.Name != "" {
			if encrypt && (struc.Role == "system-data" || struc.Role == "system-save") {
				fmt.Fprintf(f, "Label=%v-enc\n", struc.Name)
			} else {
				fmt.Fprintf(f, "Label=%v\n", struc.Name)
			}
		}
		if struc.Filesystem != "" {
			fmt.Fprintf(f, "Format=%v\n", struc.Filesystem)
		}
		fullSize := struc.Size
		if encrypt {
			fullSize = struc.Size + 16*quantity.SizeMiB
		}
		fmt.Fprintf(f, "SizeMinBytes=%v\n", fullSize)
		if struc.Role != "system-data" {
			fmt.Fprintf(f, "SizeMaxBytes=%v\n", fullSize)
		}
		if struc.Role == "system-data" || struc.Role == "system-save" || struc.Role == "system-boot" {
			fmt.Fprintf(f, "FactoryReset=true\n")
		}
		if encrypt && (struc.Role == "system-data" || struc.Role == "system-save") {
			fmt.Fprintf(f, "Encrypt=key-file\n")
		}
		for _, content := range struc.Content {
			if content.Offset != nil {
				logger.Noticef("Offset set for image. Ignoring content.")
				continue
			}
			if content.OffsetWrite != nil {
				logger.Noticef("Ignoring offset-write")
			}

			if struc.Filesystem == "none" || struc.Filesystem == "" {
				// TODO: What about content.Size?
				fmt.Fprintf(f, "CopyBlocks=%v\n", filepath.Join(gadgetRoot, content.Image))
			} else {
				// TODO: What about content.Unpack?

				source := filepath.Join(gadgetRoot, content.UnresolvedSource)
				// TODO: Make the observer more generic, it should not need a LaidOutStructure
				fake := &LaidOutStructure{
					VolumeStructure: &struc,
					StartOffset: 0,
				}
			 	apply, err := observer.Observe(ContentWrite, fake, "/", content.Target, &ContentChange{"", source})
				if err != nil {
					return err
				}
				if apply != ChangeApply {
					return fmt.Errorf("Unexpected return: %v", apply)
				}
				fmt.Fprintf(f, "CopyFiles=%v:%v\n", source, filepath.Join("/", content.Target))
			}
		}
	}

	return nil
}
