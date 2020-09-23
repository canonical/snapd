// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package image

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	// to set sysconfig.ApplyFilesystemOnlyDefaults hook
	_ "github.com/snapcore/snapd/overlord/configstate/configcore"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/sysconfig"
)

var (
	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr
)

// classicHasSnaps returns whether the model or options specify any snaps for the classic case
func classicHasSnaps(model *asserts.Model, opts *Options) bool {
	return model.Gadget() != "" || len(model.RequiredNoEssentialSnaps()) != 0 || len(opts.Snaps) != 0
}

func Prepare(opts *Options) error {
	model, err := decodeModelAssertion(opts)
	if err != nil {
		return err
	}

	if model.Architecture() != "" && opts.Architecture != "" && model.Architecture() != opts.Architecture {
		return fmt.Errorf("cannot override model architecture: %s", model.Architecture())
	}

	if !opts.Classic {
		if model.Classic() {
			return fmt.Errorf("--classic mode is required to prepare the image for a classic model")
		}
	} else {
		if !model.Classic() {
			return fmt.Errorf("cannot prepare the image for a core model with --classic mode specified")
		}
		if model.Architecture() == "" && classicHasSnaps(model, opts) && opts.Architecture == "" {
			return fmt.Errorf("cannot have snaps for a classic image without an architecture in the model or from --arch")
		}
	}

	tsto, err := NewToolingStoreFromModel(model, opts.Architecture)
	if err != nil {
		return err
	}

	// FIXME: limitation until we can pass series parametrized much more
	if model.Series() != release.Series {
		return fmt.Errorf("model with series %q != %q unsupported", model.Series(), release.Series)
	}

	return setupSeed(tsto, model, opts)
}

// these are postponed, not implemented or abandoned, not finalized,
// don't let them sneak in into a used model assertion
var reserved = []string{"core", "os", "class", "allowed-modes"}

func decodeModelAssertion(opts *Options) (*asserts.Model, error) {
	fn := opts.ModelFile

	rawAssert, err := ioutil.ReadFile(fn)
	if err != nil {
		return nil, fmt.Errorf("cannot read model assertion: %s", err)
	}

	ass, err := asserts.Decode(rawAssert)
	if err != nil {
		return nil, fmt.Errorf("cannot decode model assertion %q: %s", fn, err)
	}
	modela, ok := ass.(*asserts.Model)
	if !ok {
		return nil, fmt.Errorf("assertion in %q is not a model assertion", fn)
	}

	for _, rsvd := range reserved {
		if modela.Header(rsvd) != nil {
			return nil, fmt.Errorf("model assertion cannot have reserved/unsupported header %q set", rsvd)
		}
	}

	return modela, nil
}

func unpackGadget(gadgetFname, gadgetUnpackDir string) error {
	// FIXME: jumping through layers here, we need to make
	//        unpack part of the container interface (again)
	snap := squashfs.New(gadgetFname)
	return snap.Unpack("*", gadgetUnpackDir)
}

func installCloudConfig(rootDir, gadgetDir string) error {
	cloudConfig := filepath.Join(gadgetDir, "cloud.conf")
	if !osutil.FileExists(cloudConfig) {
		return nil
	}

	cloudDir := filepath.Join(rootDir, "/etc/cloud")
	if err := os.MkdirAll(cloudDir, 0755); err != nil {
		return err
	}
	dst := filepath.Join(cloudDir, "cloud.cfg")
	return osutil.CopyFile(cloudConfig, dst, osutil.CopyFlagOverwrite)
}

var trusted = sysdb.Trusted()

func MockTrusted(mockTrusted []asserts.Assertion) (restore func()) {
	prevTrusted := trusted
	trusted = mockTrusted
	return func() {
		trusted = prevTrusted
	}
}

func makeLabel(now time.Time) string {
	return now.UTC().Format("20060102")
}

func setupSeed(tsto *ToolingStore, model *asserts.Model, opts *Options) error {
	if model.Classic() != opts.Classic {
		return fmt.Errorf("internal error: classic model but classic mode not set")
	}

	core20 := model.Grade() != asserts.ModelGradeUnset
	var rootDir string
	var bootRootDir string
	var seedDir string
	var label string
	if !core20 {
		if opts.Classic {
			// Classic, PrepareDir is the root dir itself
			rootDir = opts.PrepareDir
		} else {
			// Core 16/18,  writing for the writeable partition
			rootDir = filepath.Join(opts.PrepareDir, "image")
			bootRootDir = rootDir
		}
		seedDir = dirs.SnapSeedDirUnder(rootDir)

		// sanity check target
		if osutil.FileExists(dirs.SnapStateFileUnder(rootDir)) {
			return fmt.Errorf("cannot prepare seed over existing system or an already booted image, detected state file %s", dirs.SnapStateFileUnder(rootDir))
		}
		if snaps, _ := filepath.Glob(filepath.Join(dirs.SnapBlobDirUnder(rootDir), "*.snap")); len(snaps) > 0 {
			return fmt.Errorf("expected empty snap dir in rootdir, got: %v", snaps)
		}

	} else {
		// Core 20, writing for the system-seed partition
		seedDir = filepath.Join(opts.PrepareDir, "system-seed")
		label = makeLabel(time.Now())
		bootRootDir = seedDir

		// sanity check target
		if systems, _ := filepath.Glob(filepath.Join(seedDir, "systems", "*")); len(systems) > 0 {
			return fmt.Errorf("expected empty systems dir in system-seed, got: %v", systems)
		}
	}

	// TODO: developer database in home or use snapd (but need
	// a bit more API there, potential issues when crossing stores/series)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   trusted,
	})
	if err != nil {
		return err
	}

	wOpts := &seedwriter.Options{
		SeedDir:        seedDir,
		Label:          label,
		DefaultChannel: opts.Channel,

		TestSkipCopyUnverifiedModel: osutil.GetenvBool("UBUNTU_IMAGE_SKIP_COPY_UNVERIFIED_MODEL"),
	}

	w, err := seedwriter.New(model, wOpts)
	if err != nil {
		return err
	}

	optSnaps := make([]*seedwriter.OptionsSnap, 0, len(opts.Snaps))
	for _, snapName := range opts.Snaps {
		var optSnap seedwriter.OptionsSnap
		if strings.HasSuffix(snapName, ".snap") {
			// local
			optSnap.Path = snapName
		} else {
			optSnap.Name = snapName
		}
		optSnap.Channel = opts.SnapChannels[snapName]
		optSnaps = append(optSnaps, &optSnap)
	}

	if err := w.SetOptionsSnaps(optSnaps); err != nil {
		return err
	}

	var gadgetUnpackDir string
	// create directory for later unpacking the gadget in
	if !opts.Classic {
		gadgetUnpackDir = filepath.Join(opts.PrepareDir, "gadget")
		if err := os.MkdirAll(gadgetUnpackDir, 0755); err != nil {
			return fmt.Errorf("cannot create gadget unpack dir %q: %s", gadgetUnpackDir, err)
		}
	}

	newFetcher := func(save func(asserts.Assertion) error) asserts.Fetcher {
		return tsto.AssertionFetcher(db, save)
	}
	f, err := w.Start(db, newFetcher)
	if err != nil {
		return err
	}

	localSnaps, err := w.LocalSnaps()
	if err != nil {
		return err
	}

	for _, sn := range localSnaps {
		si, aRefs, err := seedwriter.DeriveSideInfo(sn.Path, f, db)
		if err != nil && !asserts.IsNotFound(err) {
			return err
		}

		snapFile, err := snapfile.Open(sn.Path)
		if err != nil {
			return err
		}
		info, err := snap.ReadInfoFromSnapFile(snapFile, si)
		if err != nil {
			return err
		}

		if err := w.SetInfo(sn, info); err != nil {
			return err
		}
		sn.ARefs = aRefs
	}

	if err := w.InfoDerived(); err != nil {
		return err
	}

	for {
		toDownload, err := w.SnapsToDownload()
		if err != nil {
			return err
		}

		for _, sn := range toDownload {
			fmt.Fprintf(Stdout, "Fetching %s\n", sn.SnapName())

			targetPathFunc := func(info *snap.Info) (string, error) {
				if err := w.SetInfo(sn, info); err != nil {
					return "", err
				}
				return sn.Path, nil
			}

			dlOpts := DownloadOptions{
				TargetPathFunc: targetPathFunc,
				Channel:        sn.Channel,
				CohortKey:      opts.WideCohortKey,
			}
			fn, info, redirectChannel, err := tsto.DownloadSnap(sn.SnapName(), dlOpts) // TODO|XXX make this take the SnapRef really
			if err != nil {
				return err
			}
			if err := w.SetRedirectChannel(sn, redirectChannel); err != nil {
				return err
			}

			// fetch snap assertions
			prev := len(f.Refs())
			if _, err = FetchAndCheckSnapAssertions(fn, info, f, db); err != nil {
				return err
			}
			aRefs := f.Refs()[prev:]
			sn.ARefs = aRefs
		}

		complete, err := w.Downloaded()
		if err != nil {
			return err
		}
		if complete {
			break
		}
	}

	for _, warn := range w.Warnings() {
		fmt.Fprintf(Stderr, "WARNING: %s\n", warn)
	}

	unassertedSnaps, err := w.UnassertedSnaps()
	if err != nil {
		return err
	}
	if len(unassertedSnaps) > 0 {
		locals := make([]string, len(unassertedSnaps))
		for i, sn := range unassertedSnaps {
			locals[i] = sn.SnapName()
		}
		fmt.Fprintf(Stderr, "WARNING: %s installed from local snaps disconnected from a store cannot be refreshed subsequently!\n", strutil.Quoted(locals))
	}

	copySnap := func(name, src, dst string) error {
		fmt.Fprintf(Stdout, "Copying %q (%s)\n", src, name)
		return osutil.CopyFile(src, dst, 0)
	}
	if err := w.SeedSnaps(copySnap); err != nil {
		return err
	}

	if err := w.WriteMeta(); err != nil {
		return err
	}

	if opts.Classic {
		// TODO:UC20: consider Core 20 extended models vs classic
		seedFn := filepath.Join(seedDir, "seed.yaml")
		// warn about ownership if not root:root
		fi, err := os.Stat(seedFn)
		if err != nil {
			return fmt.Errorf("cannot stat seed.yaml: %s", err)
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			if st.Uid != 0 || st.Gid != 0 {
				fmt.Fprintf(Stderr, "WARNING: ensure that the contents under %s are owned by root:root in the (final) image", seedDir)
			}
		}
		// done already
		return nil
	}

	bootSnaps, err := w.BootSnaps()
	if err != nil {
		return err
	}

	bootWith := &boot.BootableSet{
		UnpackedGadgetDir: gadgetUnpackDir,
		Recovery:          core20,
	}
	if label != "" {
		bootWith.RecoverySystemDir = filepath.Join("/systems/", label)
		bootWith.RecoverySystemLabel = label
	}

	// find the gadget file
	// find the snap.Info/path for kernel/os/base so
	// that boot.MakeBootable can DTRT
	gadgetFname := ""
	for _, sn := range bootSnaps {
		switch sn.Info.Type() {
		case snap.TypeGadget:
			gadgetFname = sn.Path
		case snap.TypeOS, snap.TypeBase:
			bootWith.Base = sn.Info
			bootWith.BasePath = sn.Path
		case snap.TypeKernel:
			bootWith.Kernel = sn.Info
			bootWith.KernelPath = sn.Path
		}
	}

	// unpacking the gadget for core models
	if err := unpackGadget(gadgetFname, gadgetUnpackDir); err != nil {
		return err
	}

	if err := boot.MakeBootable(model, bootRootDir, bootWith, nil); err != nil {
		return err
	}

	// early config & cloud-init config (done at install for Core 20)
	if !core20 {
		// and the cloud-init things
		if err := installCloudConfig(rootDir, gadgetUnpackDir); err != nil {
			return err
		}

		gadgetInfo, err := gadget.ReadInfo(gadgetUnpackDir, model)
		if err != nil {
			return err
		}

		defaultsDir := sysconfig.WritableDefaultsDir(rootDir)
		defaults := gadget.SystemDefaults(gadgetInfo.Defaults)
		if len(defaults) > 0 {
			if err := os.MkdirAll(sysconfig.WritableDefaultsDir(rootDir, "/etc"), 0755); err != nil {
				return err
			}
			applyOpts := &sysconfig.FilesystemOnlyApplyOptions{Classic: opts.Classic}
			return sysconfig.ApplyFilesystemOnlyDefaults(defaultsDir, defaults, applyOpts)
		}
	}

	return nil
}
