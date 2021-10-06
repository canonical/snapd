// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"

	// to set sysconfig.ApplyFilesystemOnlyDefaults hook
	_ "github.com/snapcore/snapd/overlord/configstate/configcore"
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

func (custo *Customizations) validate(model *asserts.Model) error {
	core20 := model.Grade() != asserts.ModelGradeUnset
	var unsupported []string
	unsupportedConsoleConfDisable := func() {
		if custo.ConsoleConf == "disabled" {
			unsupported = append(unsupported, "console-conf disable")
		}
	}
	unsupportedBootFlags := func() {
		if len(custo.BootFlags) != 0 {
			unsupported = append(unsupported, fmt.Sprintf("boot flags (%s)", strings.Join(custo.BootFlags, " ")))
		}
	}

	kind := "UC16/18"
	switch {
	case core20:
		kind = "UC20"
		// TODO:UC20: consider supporting these with grade dangerous?
		unsupportedConsoleConfDisable()
		if custo.CloudInitUserData != "" {
			unsupported = append(unsupported, "cloud-init user-data")
		}
	case model.Classic():
		kind = "classic"
		unsupportedConsoleConfDisable()
		unsupportedBootFlags()
	default:
		// UC16/18
		unsupportedBootFlags()
	}
	if len(unsupported) != 0 {
		return fmt.Errorf("cannot support with %s model requested customizations: %s", kind, strings.Join(unsupported, ", "))
	}
	return nil
}

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

	if err := opts.Customizations.validate(model); err != nil {
		return err
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

func unpackSnap(gadgetFname, gadgetUnpackDir string) error {
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

func customizeImage(rootDir, defaultsDir string, custo *Customizations) error {
	// customize with cloud-init user-data
	if custo.CloudInitUserData != "" {
		// See
		// https://cloudinit.readthedocs.io/en/latest/topics/dir_layout.html
		// https://cloudinit.readthedocs.io/en/latest/topics/datasources/nocloud.html
		varCloudDir := filepath.Join(rootDir, "/var/lib/cloud/seed/nocloud-net")
		if err := os.MkdirAll(varCloudDir, 0755); err != nil {
			return err
		}
		if err := ioutil.WriteFile(filepath.Join(varCloudDir, "meta-data"), []byte("instance-id: nocloud-static\n"), 0644); err != nil {
			return err
		}
		dst := filepath.Join(varCloudDir, "user-data")
		if err := osutil.CopyFile(custo.CloudInitUserData, dst, osutil.CopyFlagOverwrite); err != nil {
			return err
		}
	}

	if custo.ConsoleConf == "disabled" {
		// TODO: maybe share code with configcore somehow
		consoleConfDisabled := filepath.Join(defaultsDir, "/var/lib/console-conf/complete")
		if err := os.MkdirAll(filepath.Dir(consoleConfDisabled), 0755); err != nil {
			return err
		}
		if err := ioutil.WriteFile(consoleConfDisabled, []byte("console-conf has been disabled by image customization\n"), 0644); err != nil {
			return err
		}
	}

	return nil
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

	var gadgetUnpackDir, kernelUnpackDir string
	// create directory for later unpacking the gadget in
	if !opts.Classic {
		gadgetUnpackDir = filepath.Join(opts.PrepareDir, "gadget")
		kernelUnpackDir = filepath.Join(opts.PrepareDir, "kernel")
		for _, unpackDir := range []string{gadgetUnpackDir, kernelUnpackDir} {
			if err := os.MkdirAll(unpackDir, 0755); err != nil {
				return fmt.Errorf("cannot create unpack dir %q: %s", unpackDir, err)
			}
		}
	}

	newFetcher := func(save func(asserts.Assertion) error) asserts.Fetcher {
		return tsto.AssertionFetcher(db, save)
	}
	f, err := w.Start(db, newFetcher)
	if err != nil {
		return err
	}

	if opts.Customizations.Validation == "" && !opts.Classic {
		fmt.Fprintf(Stderr, "WARNING: proceeding to download snaps ignoring validations, this default will change in the future. For now use --validation=enforce for validations to be taken into account, pass instead --validation=ignore to preserve current behavior going forward\n")
	}
	if opts.Customizations.Validation == "" {
		opts.Customizations.Validation = "ignore"
	}

	localSnaps, err := w.LocalSnaps()
	if err != nil {
		return err
	}

	var curSnaps []*CurrentSnap
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

		if info.ID() != "" {
			curSnaps = append(curSnaps, &CurrentSnap{
				SnapName: info.SnapName(),
				SnapID:   info.ID(),
				Revision: info.Revision,
				Epoch:    info.Epoch,
			})
		}
	}

	if err := w.InfoDerived(); err != nil {
		return err
	}

	for {
		toDownload, err := w.SnapsToDownload()
		if err != nil {
			return err
		}

		byName := make(map[string]*seedwriter.SeedSnap, len(toDownload))
		beforeDownload := func(info *snap.Info) (string, error) {
			sn := byName[info.SnapName()]
			if sn == nil {
				return "", fmt.Errorf("internal error: downloading unexpected snap %q", info.SnapName())
			}
			fmt.Fprintf(Stdout, "Fetching %s\n", sn.SnapName())
			if err := w.SetInfo(sn, info); err != nil {
				return "", err
			}
			return sn.Path, nil
		}
		snapToDownloadOptions := make([]SnapToDownload, len(toDownload))
		for i, sn := range toDownload {
			byName[sn.SnapName()] = sn
			snapToDownloadOptions[i].Snap = sn
			snapToDownloadOptions[i].Channel = sn.Channel
			snapToDownloadOptions[i].CohortKey = opts.WideCohortKey
		}
		downloadedSnaps, err := tsto.DownloadMany(snapToDownloadOptions, curSnaps, DownloadManyOptions{
			BeforeDownloadFunc: beforeDownload,
			EnforceValidation:  opts.Customizations.Validation == "enforce",
		})
		if err != nil {
			return err
		}

		for _, sn := range toDownload {
			dlsn := downloadedSnaps[sn.SnapName()]

			if err := w.SetRedirectChannel(sn, dlsn.RedirectChannel); err != nil {
				return err
			}

			// fetch snap assertions
			prev := len(f.Refs())
			if _, err = FetchAndCheckSnapAssertions(dlsn.Path, dlsn.Info, f, db); err != nil {
				return err
			}
			aRefs := f.Refs()[prev:]
			sn.ARefs = aRefs

			curSnaps = append(curSnaps, &CurrentSnap{
				SnapName: sn.Info.SnapName(),
				SnapID:   sn.Info.ID(),
				Revision: sn.Info.Revision,
				Epoch:    sn.Info.Epoch,
				Channel:  sn.Channel,
			})
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
	kernelFname := ""
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
			kernelFname = sn.Path
		}
	}

	// unpacking the gadget for core models
	if err := unpackSnap(gadgetFname, gadgetUnpackDir); err != nil {
		return err
	}
	if err := unpackSnap(kernelFname, kernelUnpackDir); err != nil {
		return err
	}

	if err := boot.MakeBootableImage(model, bootRootDir, bootWith, opts.Customizations.BootFlags); err != nil {
		return err
	}

	gadgetInfo, err := gadget.ReadInfoAndValidate(gadgetUnpackDir, model, nil)
	if err != nil {
		return err
	}
	// validate content against the kernel as well
	if err := gadget.ValidateContent(gadgetInfo, gadgetUnpackDir, kernelUnpackDir); err != nil {
		return err
	}

	// write resolved content to structure root
	if err := writeResolvedContent(opts.PrepareDir, gadgetInfo, gadgetUnpackDir, kernelUnpackDir); err != nil {
		return err
	}

	// early config & cloud-init config (done at install for Core 20)
	if !core20 {
		// and the cloud-init things
		if err := installCloudConfig(rootDir, gadgetUnpackDir); err != nil {
			return err
		}

		defaultsDir := sysconfig.WritableDefaultsDir(rootDir)
		defaults := gadget.SystemDefaults(gadgetInfo.Defaults)
		if len(defaults) > 0 {
			if err := os.MkdirAll(sysconfig.WritableDefaultsDir(rootDir, "/etc"), 0755); err != nil {
				return err
			}
			return sysconfig.ApplyFilesystemOnlyDefaults(model, defaultsDir, defaults)
		}

		customizeImage(rootDir, defaultsDir, &opts.Customizations)
	}

	return nil
}
