// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/store/tooling"

	// to set sysconfig.ApplyFilesystemOnlyDefaults hook
	"github.com/snapcore/snapd/image/preseed"
	"github.com/snapcore/snapd/osutil"
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

	preseedCore20 = preseed.Core20
)

func (custo *Customizations) validate(model *asserts.Model) error {
	hasModes := model.Grade() != asserts.ModelGradeUnset
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
	case hasModes:
		kind = "UC20+"
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

var newToolingStoreFromModel = tooling.NewToolingStoreFromModel

func Prepare(opts *Options) error {
	var model *asserts.Model
	var err error
	if opts.Classic && opts.ModelFile == "" {
		// ubuntu-image has a use case for preseeding snaps in an arbitrary rootfs
		// using its --filesystem flag. This rootfs may or may not already have
		// snaps preseeded in it. In the case where the provided rootfs has no
		// snaps seeded image.Prepare will be called with no model assertion,
		// and we then use the GenericClassicModel.
		model = sysdb.GenericClassicModel()
	} else {
		model, err = decodeModelAssertion(opts)
		if err != nil {
			return err
		}
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

	tsto, err := newToolingStoreFromModel(model, opts.Architecture)
	if err != nil {
		return err
	}
	tsto.Stdout = Stdout

	// FIXME: limitation until we can pass series parametrized much more
	if model.Series() != release.Series {
		return fmt.Errorf("model with series %q != %q unsupported", model.Series(), release.Series)
	}

	if err := opts.Customizations.validate(model); err != nil {
		return err
	}

	if err := setupSeed(tsto, model, opts); err != nil {
		return err
	}

	if opts.Preseed {
		// TODO: support UC22
		if model.Classic() {
			return fmt.Errorf("cannot preseed the image for a classic model")
		}
		if model.Base() != "core20" {
			return fmt.Errorf("cannot preseed the image for a model other than core20")
		}
		coreOpts := &preseed.CoreOptions{
			PrepareImageDir:           opts.PrepareDir,
			PreseedSignKey:            opts.PreseedSignKey,
			AppArmorKernelFeaturesDir: opts.AppArmorKernelFeaturesDir,
			SysfsOverlay:              opts.SysfsOverlay,
		}
		return preseedCore20(coreOpts)
	}

	return nil
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

type seedOptions struct {
	rootDir     string
	bootRootDir string
	seedDir     string
	label       string
}

func determineSeedOptions(hasModes bool, opts *Options) (*seedOptions, error) {
	seedOpts := &seedOptions{}
	if !hasModes {
		if opts.Classic {
			// Classic, PrepareDir is the root dir itself
			seedOpts.rootDir = opts.PrepareDir
		} else {
			// Core 16/18,  writing for the writeable partition
			seedOpts.rootDir = filepath.Join(opts.PrepareDir, "image")
			seedOpts.bootRootDir = seedOpts.rootDir
		}
		seedOpts.seedDir = dirs.SnapSeedDirUnder(seedOpts.rootDir)

		// validity check target
		if osutil.FileExists(dirs.SnapStateFileUnder(seedOpts.rootDir)) {
			return nil, fmt.Errorf("cannot prepare seed over existing system or an already booted image, detected state file %s", dirs.SnapStateFileUnder(seedOpts.rootDir))
		}
		if snaps, _ := filepath.Glob(filepath.Join(dirs.SnapBlobDirUnder(seedOpts.rootDir), "*.snap")); len(snaps) > 0 {
			return nil, fmt.Errorf("expected empty snap dir in rootdir, got: %v", snaps)
		}

	} else {
		// Core 20, writing for the system-seed partition
		seedOpts.seedDir = filepath.Join(opts.PrepareDir, "system-seed")
		seedOpts.label = makeLabel(time.Now())
		seedOpts.bootRootDir = seedOpts.seedDir

		// validity check target
		if systems, _ := filepath.Glob(filepath.Join(seedOpts.seedDir, "systems", "*")); len(systems) > 0 {
			return nil, fmt.Errorf("expected empty systems dir in system-seed, got: %v", systems)
		}
	}
	return seedOpts, nil
}

func optionalSnaps(opts *Options) []*seedwriter.OptionsSnap {
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
	return optSnaps
}

type localSnapRefs map[*seedwriter.SeedSnap][]*asserts.Ref

func deriveInfoForLocalSnaps(w *seedwriter.Writer, model *asserts.Model, f seedwriter.RefAssertsFetcher, db *asserts.Database) (localSnapRefs, error) {
	localSnaps, err := w.LocalSnaps()
	if err != nil {
		return nil, err
	}

	snaps := make(map[*seedwriter.SeedSnap][]*asserts.Ref)
	for _, sn := range localSnaps {
		si, aRefs, err := seedwriter.DeriveSideInfo(sn.Path, model, f, db)
		if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
			return nil, err
		}

		snapFile, err := snapfile.Open(sn.Path)
		if err != nil {
			return nil, err
		}
		info, err := snap.ReadInfoFromSnapFile(snapFile, si)
		if err != nil {
			return nil, err
		}

		if err := w.SetInfo(sn, info); err != nil {
			return nil, err
		}
		snaps[sn] = aRefs
	}
	return snaps, w.InfoDerived()
}

func manifestFromLocalSnaps(snaps localSnapRefs, opts *Options) (map[string]snap.Revision, error) {
	// Build a map of snaps for the manifest file
	imageManifest := make(map[string]snap.Revision)

	// Check local snaps again, but now after InfoDerived has been called. InfoDerived
	// fills out the snap revisions for the local snaps, and we need this to verify against
	// expected revisions.
	for sn := range snaps {
		// Its a bit more tricky to deal with local snaps, as we only have that specific revision
		// available. Therefore the revision in the local snap must be exactly the revision specified
		// in the manifest. If it's not, we fail.
		specifiedRevision := opts.Revisions[sn.Info.SnapName()]
		if !specifiedRevision.Unset() && specifiedRevision != sn.Info.Revision {
			return nil, fmt.Errorf("cannot use snap %s for image, unknown/local revision does not match the value specified by revisions file (%s != %s)",
				sn.Path, sn.Info.Revision, specifiedRevision)
		}
		if !sn.Info.Revision.Unset() {
			imageManifest[sn.Info.SnapName()] = sn.Info.Revision
		}
	}
	return imageManifest, nil
}

func localSnapsWithID(snaps localSnapRefs) []*tooling.CurrentSnap {
	var localSnaps []*tooling.CurrentSnap
	for sn := range snaps {
		if sn.Info.ID() == "" {
			continue
		}
		localSnaps = append(localSnaps, &tooling.CurrentSnap{
			SnapName: sn.Info.SnapName(),
			SnapID:   sn.Info.ID(),
			Revision: sn.Info.Revision,
			Epoch:    sn.Info.Epoch,
		})
	}
	return localSnaps
}

func downloadSnaps(snapsToDownload []*seedwriter.SeedSnap, curSnaps []*tooling.CurrentSnap, w *seedwriter.Writer, tsto *tooling.ToolingStore, opts *Options) (downloadedSnaps map[string]*tooling.DownloadedSnap, err error) {
	byName := make(map[string]*seedwriter.SeedSnap, len(snapsToDownload))
	beforeDownload := func(info *snap.Info) (string, error) {
		sn := byName[info.SnapName()]
		if sn == nil {
			return "", fmt.Errorf("internal error: downloading unexpected snap %q", info.SnapName())
		}
		rev := opts.Revisions[sn.SnapName()]
		if !rev.Unset() {
			fmt.Fprintf(Stdout, "Fetching %s (%d)\n", sn.SnapName(), rev)
		} else {
			fmt.Fprintf(Stdout, "Fetching %s (%d)\n", sn.SnapName(), info.Revision)
		}
		if err := w.SetInfo(sn, info); err != nil {
			return "", err
		}
		return sn.Path, nil
	}
	snapToDownloadOptions := make([]tooling.SnapToDownload, len(snapsToDownload))
	for i, sn := range snapsToDownload {
		byName[sn.SnapName()] = sn
		snapToDownloadOptions[i].Snap = sn
		snapToDownloadOptions[i].Channel = sn.Channel
		snapToDownloadOptions[i].Revision = opts.Revisions[sn.SnapName()]
		snapToDownloadOptions[i].CohortKey = opts.WideCohortKey
	}

	// sort the curSnaps slice for test consistency
	sort.Slice(curSnaps, func(i, j int) bool {
		return curSnaps[i].SnapName < curSnaps[j].SnapName
	})
	downloadedSnaps, err = tsto.DownloadMany(snapToDownloadOptions, curSnaps, tooling.DownloadManyOptions{
		BeforeDownloadFunc: beforeDownload,
		EnforceValidation:  opts.Customizations.Validation == "enforce",
	})
	if err != nil {
		return nil, err
	}
	return downloadedSnaps, nil
}

func downloadAllSnaps(localSnaps localSnapRefs, w *seedwriter.Writer, tsto *tooling.ToolingStore, fetchAsserts func(sn, sysSn, kSn *seedwriter.SeedSnap) ([]*asserts.Ref, error), opts *Options) error {
	curSnaps := localSnapsWithID(localSnaps)
	for {
		toDownload, err := w.SnapsToDownload()
		if err != nil {
			return err
		}

		downloadedSnaps, err := downloadSnaps(toDownload, curSnaps, w, tsto, opts)
		if err != nil {
			return err
		}

		for _, sn := range toDownload {
			dlsn := downloadedSnaps[sn.SnapName()]
			if err := w.SetRedirectChannel(sn, dlsn.RedirectChannel); err != nil {
				return err
			}

			curSnaps = append(curSnaps, &tooling.CurrentSnap{
				SnapName: sn.Info.SnapName(),
				SnapID:   sn.Info.ID(),
				Revision: sn.Info.Revision,
				Epoch:    sn.Info.Epoch,
				Channel:  sn.Channel,
			})
		}

		complete, err := w.Downloaded(fetchAsserts)
		if err != nil {
			return err
		}
		if complete {
			break
		}
	}
	return nil
}

func selectAssertionMaxFormats(tsto *tooling.ToolingStore, sysSn *seedwriter.SeedSnap) error {
	if sysSn == nil {
		// nothing to do
		return nil
	}
	snapf, err := snapfile.Open(sysSn.Path)
	if err != nil {
		return err
	}
	// XXX take also kernel into account
	// XXX warning logic
	maxFormats, _, err := snap.SnapdAssertionMaxFormatsFromSnapFile(snapf)
	if err != nil {
		return err
	}
	tsto.SetAssertionMaxFormats(maxFormats)
	return nil
}

func seedClassicImage(hasModes bool, seedOpts *seedOptions) error {
	var fpath string
	if hasModes {
		fpath = filepath.Join(seedOpts.seedDir, "systems")
	} else {
		fpath = filepath.Join(seedOpts.seedDir, "seed.yaml")
	}
	// warn about ownership if not root:root
	fi, err := os.Stat(fpath)
	if err != nil {
		return fmt.Errorf("cannot stat %q: %s", fpath, err)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		if st.Uid != 0 || st.Gid != 0 {
			fmt.Fprintf(Stderr, "WARNING: ensure that the contents under %s are owned by root:root in the (final) image\n", seedOpts.seedDir)
		}
	}
	// done already
	return nil
}

func seedCoreImage(w *seedwriter.Writer, model *asserts.Model, hasModes bool, opts *Options, seedOpts *seedOptions) error {
	gadgetUnpackDir := filepath.Join(opts.PrepareDir, "gadget")
	kernelUnpackDir := filepath.Join(opts.PrepareDir, "kernel")

	bootSnaps, err := w.BootSnaps()
	if err != nil {
		return err
	}

	bootWith := &boot.BootableSet{
		UnpackedGadgetDir: gadgetUnpackDir,
		Recovery:          hasModes,
	}
	if seedOpts.label != "" {
		bootWith.RecoverySystemDir = filepath.Join("/systems/", seedOpts.label)
		bootWith.RecoverySystemLabel = seedOpts.label
	}

	// find the snap.Info/path for kernel/os/base/gadget so
	// that boot.MakeBootable can DTRT
	kernelFname := ""
	for _, sn := range bootSnaps {
		switch sn.Info.Type() {
		case snap.TypeGadget:
			bootWith.Gadget = sn.Info
			bootWith.GadgetPath = sn.Path
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
	if err := unpackSnap(bootWith.GadgetPath, gadgetUnpackDir); err != nil {
		return err
	}
	if err := unpackSnap(kernelFname, kernelUnpackDir); err != nil {
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

	if err := boot.MakeBootableImage(model, seedOpts.bootRootDir, bootWith, opts.Customizations.BootFlags); err != nil {
		return err
	}

	// early config & cloud-init config (done at install for Core 20)
	if !hasModes {
		// and the cloud-init things
		if err := installCloudConfig(seedOpts.rootDir, gadgetUnpackDir); err != nil {
			return err
		}

		defaultsDir := sysconfig.WritableDefaultsDir(seedOpts.rootDir)
		defaults := gadget.SystemDefaults(gadgetInfo.Defaults)
		if len(defaults) > 0 {
			if err := os.MkdirAll(sysconfig.WritableDefaultsDir(seedOpts.rootDir, "/etc"), 0755); err != nil {
				return err
			}
			if err := sysconfig.ApplyFilesystemOnlyDefaults(model, defaultsDir, defaults); err != nil {
				return err
			}
		}

		customizeImage(seedOpts.rootDir, defaultsDir, &opts.Customizations)
	}
	return nil
}

var setupSeed = func(tsto *tooling.ToolingStore, model *asserts.Model, opts *Options) error {
	if model.Classic() != opts.Classic {
		return fmt.Errorf("internal error: classic model but classic mode not set")
	}

	// Determine image seed paths, which can vary based on the type of image
	// we are generating.
	hasModes := model.Grade() != asserts.ModelGradeUnset
	seedOpts, err := determineSeedOptions(hasModes, opts)
	if err != nil {
		return err
	}

	wOpts := &seedwriter.Options{
		SeedDir:        seedOpts.seedDir,
		Label:          seedOpts.label,
		DefaultChannel: opts.Channel,

		TestSkipCopyUnverifiedModel: osutil.GetenvBool("UBUNTU_IMAGE_SKIP_COPY_UNVERIFIED_MODEL"),
	}
	w, err := seedwriter.New(model, wOpts)
	if err != nil {
		return err
	}

	optSnaps := optionalSnaps(opts)
	if err := w.SetOptionsSnaps(optSnaps); err != nil {
		return err
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
	newFetcher := func(save func(asserts.Assertion) error) asserts.Fetcher {
		return tsto.AssertionFetcher(db, save)
	}

	// create directory for later unpacking the gadget in
	if !opts.Classic {
		gadgetUnpackDir := filepath.Join(opts.PrepareDir, "gadget")
		kernelUnpackDir := filepath.Join(opts.PrepareDir, "kernel")
		for _, unpackDir := range []string{gadgetUnpackDir, kernelUnpackDir} {
			if err := os.MkdirAll(unpackDir, 0755); err != nil {
				return fmt.Errorf("cannot create unpack dir %q: %s", unpackDir, err)
			}
		}
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

	// We need to use seedwriter.DeriveSideInfo earlier than
	// we might possibly know the system and kernel snaps to
	// know the correct assertion max format to use.
	// Fetch assertions tentatively into a temporary database
	// and later either copy them or fetch more appropriate ones.
	tmpDb := db.WithStackedBackstore(asserts.NewMemoryBackstore())
	tmpFetcher := seedwriter.MakeRefAssertsFetcher(func(save func(asserts.Assertion) error) asserts.Fetcher {
		return tsto.AssertionFetcher(tmpDb, save)
	})

	localSnaps, err := deriveInfoForLocalSnaps(w, model, tmpFetcher, tmpDb)
	if err != nil {
		return err
	}

	// Build a map of snaps for the manifest file, but now after
	// InfoDerived has been called. InfoDerived fills out the snap
	// revisions for the local snaps, and we need this to verify against
	// expected revisions.
	imageManifest, err := manifestFromLocalSnaps(localSnaps, opts)
	if err != nil {
		return err
	}

	assertMaxFormatsSelected := false
	var assertMaxFormats map[string]int

	copyOrRefetchIfFormatTooNewIntoDb := func(aRefs []*asserts.Ref) error {
		// copy or re-fetch assertions to replace if the format is too
		// new; as the replacing is based on the primary key previous
		// cross check on provenance will still be valid or db
		// consistency checks will fail
		for _, aRef := range aRefs {
			a, err := aRef.Resolve(tmpDb.Find)
			if err != nil {
				return fmt.Errorf("internal error: lost saved assertion")
			}
			if assertMaxFormats != nil && a.Format() > assertMaxFormats[aRef.Type.Name] {
				// format was too new, re-fetch to replace
				if err := f.Fetch(aRef); err != nil {
					return err
				}
			} else {
				// copy
				if err := f.Save(a); err != nil {
					return err
				}
			}
		}
		return nil
	}

	fetchAsserts := func(sn, sysSn, kSn *seedwriter.SeedSnap) ([]*asserts.Ref, error) {
		if !assertMaxFormatsSelected {
			if err := selectAssertionMaxFormats(tsto, sysSn); err != nil {
				return nil, err
			}
			assertMaxFormatsSelected = true
			assertMaxFormats = tsto.AssertionMaxFormats()
		}
		prev := len(f.Refs())
		if aRefs, ok := localSnaps[sn]; ok {
			if err := copyOrRefetchIfFormatTooNewIntoDb(aRefs); err != nil {
				return nil, err
			}
		} else {
			// fetch snap assertions
			if _, err = FetchAndCheckSnapAssertions(sn.Path, sn.Info, model, f, db); err != nil {
				return nil, err
			}
			if !sn.Info.Revision.Unset() {
				imageManifest[sn.Info.SnapName()] = sn.Info.Revision
			}
		}
		return f.Refs()[prev:], nil
	}

	if err := downloadAllSnaps(localSnaps, w, tsto, fetchAsserts, opts); err != nil {
		return err
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

	// last thing is to generate the image seed manifest file
	if opts.SeedManifestPath != "" {
		if err := WriteSeedManifest(opts.SeedManifestPath, imageManifest); err != nil {
			return err
		}
	}

	// TODO: There will be classic UC20+ model based systems
	//       that will have a bootable  ubuntu-seed partition.
	//       This will need to be handled here eventually too.
	if opts.Classic {
		return seedClassicImage(hasModes, seedOpts)
	}
	return seedCoreImage(w, model, hasModes, opts, seedOpts)
}
