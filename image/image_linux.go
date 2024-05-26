// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2023 Canonical Ltd
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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/store/tooling"
	"github.com/snapcore/snapd/strutil"

	// to set sysconfig.ApplyFilesystemOnlyDefaults hook
	"github.com/snapcore/snapd/image/preseed"
	"github.com/snapcore/snapd/osutil"
	_ "github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/squashfs"
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

	if opts.Classic && opts.ModelFile == "" {
		// ubuntu-image has a use case for preseeding snaps in an arbitrary rootfs
		// using its --filesystem flag. This rootfs may or may not already have
		// snaps preseeded in it. In the case where the provided rootfs has no
		// snaps seeded image.Prepare will be called with no model assertion,
		// and we then use the GenericClassicModel.
		model = sysdb.GenericClassicModel()
	} else {
		model = mylog.Check2(decodeModelAssertion(opts))
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

	tsto := mylog.Check2(newToolingStoreFromModel(model, opts.Architecture))

	tsto.Stdout = Stdout

	// FIXME: limitation until we can pass series parametrized much more
	if model.Series() != release.Series {
		return fmt.Errorf("model with series %q != %q unsupported", model.Series(), release.Series)
	}
	mylog.Check(opts.Customizations.validate(model))
	mylog.Check(setupSeed(tsto, model, opts))

	if opts.Preseed {
		// TODO: support UC22
		if model.Classic() {
			return fmt.Errorf("cannot preseed the image for a classic model")
		}

		coreVersion := mylog.Check2(naming.CoreVersion(model.Base()))

		if coreVersion < 20 {
			return fmt.Errorf("cannot preseed the image for older base than core20")
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

	rawAssert := mylog.Check2(os.ReadFile(fn))

	ass := mylog.Check2(asserts.Decode(rawAssert))

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
	mylog.Check(os.MkdirAll(cloudDir, 0755))

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
		mylog.Check(os.MkdirAll(varCloudDir, 0755))
		mylog.Check(os.WriteFile(filepath.Join(varCloudDir, "meta-data"), []byte("instance-id: nocloud-static\n"), 0644))

		dst := filepath.Join(varCloudDir, "user-data")
		mylog.Check(osutil.CopyFile(custo.CloudInitUserData, dst, osutil.CopyFlagOverwrite))

	}

	if custo.ConsoleConf == "disabled" {
		// TODO: maybe share code with configcore somehow
		consoleConfDisabled := filepath.Join(defaultsDir, "/var/lib/console-conf/complete")
		mylog.Check(os.MkdirAll(filepath.Dir(consoleConfDisabled), 0755))
		mylog.Check(os.WriteFile(consoleConfDisabled, []byte("console-conf has been disabled by image customization\n"), 0644))

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

type imageSeeder struct {
	model *asserts.Model
	tsto  *tooling.ToolingStore

	classic        bool
	prepareDir     string
	wideCohortKey  string
	customizations *Customizations
	architecture   string

	hasModes    bool
	rootDir     string
	bootRootDir string
	seedDir     string
	label       string
	db          *asserts.Database
	w           *seedwriter.Writer
	f           seedwriter.SeedAssertionFetcher
}

func newImageSeeder(tsto *tooling.ToolingStore, model *asserts.Model, opts *Options) (*imageSeeder, error) {
	if model.Classic() != opts.Classic {
		return nil, fmt.Errorf("internal error: classic model but classic mode not set")
	}

	// Determine image seed paths, which can vary based on the type of image
	// we are generating.
	s := &imageSeeder{
		classic:       opts.Classic,
		prepareDir:    opts.PrepareDir,
		wideCohortKey: opts.WideCohortKey,
		// keep a pointer to the customization object in opts as the Validation
		// member might be defaulted if not set.
		customizations: &opts.Customizations,
		architecture:   determineImageArchitecture(model, opts),

		hasModes: model.Grade() != asserts.ModelGradeUnset,
		model:    model,
		tsto:     tsto,
	}

	if !s.hasModes {
		mylog.Check(s.setModelessDirs())
	} else {
		mylog.Check(s.setModesDirs())
	}

	// create directory for later unpacking the gadget in
	if !s.classic {
		gadgetUnpackDir := filepath.Join(s.prepareDir, "gadget")
		kernelUnpackDir := filepath.Join(s.prepareDir, "kernel")
		for _, unpackDir := range []string{gadgetUnpackDir, kernelUnpackDir} {
			mylog.Check(os.MkdirAll(unpackDir, 0755))
		}
	}

	wOpts := &seedwriter.Options{
		SeedDir:        s.seedDir,
		Label:          s.label,
		DefaultChannel: opts.Channel,
		Manifest:       opts.SeedManifest,
		ManifestPath:   opts.SeedManifestPath,

		TestSkipCopyUnverifiedModel: osutil.GetenvBool("UBUNTU_IMAGE_SKIP_COPY_UNVERIFIED_MODEL"),
	}
	w := mylog.Check2(seedwriter.New(model, wOpts))

	s.w = w
	return s, nil
}

func determineImageArchitecture(model *asserts.Model, opts *Options) string {
	// let the architecture supplied in opts take precedence
	if opts.Architecture != "" {
		// in theory we could check that this does not differ from the one
		// specified in the model, but this check is done somewhere else.
		return opts.Architecture
	} else if model.Architecture() != "" {
		return model.Architecture()
	} else {
		// if none had anything set, use the host architecture
		return arch.DpkgArchitecture()
	}
}

func (s *imageSeeder) setModelessDirs() error {
	if s.classic {
		// Classic, PrepareDir is the root dir itself
		s.rootDir = s.prepareDir
	} else {
		// Core 16/18,  writing for the writeable partition
		s.rootDir = filepath.Join(s.prepareDir, "image")
		s.bootRootDir = s.rootDir
	}
	s.seedDir = dirs.SnapSeedDirUnder(s.rootDir)

	// validity check target
	if osutil.FileExists(dirs.SnapStateFileUnder(s.rootDir)) {
		return fmt.Errorf("cannot prepare seed over existing system or an already booted image, detected state file %s", dirs.SnapStateFileUnder(s.rootDir))
	}
	if snaps, _ := filepath.Glob(filepath.Join(dirs.SnapBlobDirUnder(s.rootDir), "*.snap")); len(snaps) > 0 {
		return fmt.Errorf("expected empty snap dir in rootdir, got: %v", snaps)
	}
	return nil
}

func (s *imageSeeder) setModesDirs() error {
	// Core 20, writing for the system-seed partition
	s.seedDir = filepath.Join(s.prepareDir, "system-seed")
	s.label = makeLabel(time.Now())
	s.bootRootDir = s.seedDir

	// validity check target
	if systems, _ := filepath.Glob(filepath.Join(s.seedDir, "systems", "*")); len(systems) > 0 {
		return fmt.Errorf("expected empty systems dir in system-seed, got: %v", systems)
	}
	return nil
}

func (s *imageSeeder) start(optSnaps []*seedwriter.OptionsSnap) error {
	mylog.Check(s.w.SetOptionsSnaps(optSnaps))

	// TODO: developer database in home or use snapd (but need
	// a bit more API there, potential issues when crossing stores/series)
	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   trusted,
	}))

	newFetcher := func(save func(asserts.Assertion) error) asserts.Fetcher {
		return s.tsto.AssertionSequenceFormingFetcher(db, save)
	}
	s.db = db
	s.f = seedwriter.MakeSeedAssertionFetcher(newFetcher)
	return s.w.Start(db, s.f)
}

func (s *imageSeeder) snapSupportsImageArch(sn *seedwriter.SeedSnap) bool {
	for _, a := range sn.Info.Architectures {
		if a == "all" || a == s.architecture {
			return true
		}
	}
	return false
}

func (s *imageSeeder) validateSnapArchs(snaps []*seedwriter.SeedSnap) error {
	for _, sn := range snaps {
		if !s.snapSupportsImageArch(sn) {
			return fmt.Errorf("snap %q supported architectures (%s) are incompatible with the model architecture (%s)",
				sn.Info.SnapName(), strings.Join(sn.Info.Architectures, ", "), s.architecture)
		}
	}
	return nil
}

type localSnapRefs map[*seedwriter.SeedSnap][]*asserts.Ref

func (s *imageSeeder) deriveInfoForLocalSnaps(f seedwriter.SeedAssertionFetcher, db *asserts.Database) (localSnapRefs, error) {
	localSnaps := mylog.Check2(s.w.LocalSnaps())

	snaps := make(map[*seedwriter.SeedSnap][]*asserts.Ref)
	for _, sn := range localSnaps {
		si, aRefs := mylog.Check3(seedwriter.DeriveSideInfo(sn.Path, s.model, f, db))
		if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
			return nil, err
		}

		snapFile := mylog.Check2(snapfile.Open(sn.Path))

		info := mylog.Check2(snap.ReadInfoFromSnapFile(snapFile, si))
		mylog.Check(s.w.SetInfo(sn, info))

		snaps[sn] = aRefs
	}
	mylog.Check(

		// derive info first before verifying the arch
		s.validateSnapArchs(localSnaps))

	return snaps, s.w.InfoDerived()
}

func (s *imageSeeder) validationSetKeysAndRevisionForSnap(snapName string) ([]snapasserts.ValidationSetKey, snap.Revision, error) {
	vsas := mylog.Check2(s.db.FindMany(asserts.ValidationSetType, nil))
	if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
		return nil, snap.Revision{}, err
	}

	allVss := snapasserts.NewValidationSets()
	for _, a := range vsas {
		mylog.Check(allVss.Add(a.(*asserts.ValidationSet)))
	}
	mylog.Check(

		// Just for a good measure, perform a conflict check once we have the
		// list of all validation-sets for the image seed.
		allVss.Conflict())

	// TODO: It's pointed out that here and some of the others uses of this
	// may miss logic for optional snaps which have required revisions. This
	// is not covered by the below check, and we may or may not have multiple places
	// with a similar issue.
	snapVsKeys, snapRev := mylog.Check3(allVss.CheckPresenceRequired(naming.Snap(snapName)))

	if len(snapVsKeys) > 0 {
		return snapVsKeys, snapRev, nil
	}
	return nil, s.w.Manifest().AllowedSnapRevision(snapName), nil
}

func (s *imageSeeder) downloadSnaps(snapsToDownload []*seedwriter.SeedSnap, curSnaps []*tooling.CurrentSnap) (downloadedSnaps map[string]*tooling.DownloadedSnap, err error) {
	byName := make(map[string]*seedwriter.SeedSnap, len(snapsToDownload))
	revisions := make(map[string]snap.Revision)
	beforeDownload := func(info *snap.Info) (string, error) {
		sn := byName[info.SnapName()]
		if sn == nil {
			return "", fmt.Errorf("internal error: downloading unexpected snap %q", info.SnapName())
		}
		rev := revisions[info.SnapName()]
		if rev.Unset() {
			rev = info.Revision
		}
		fmt.Fprintf(Stdout, "Fetching %s (%s)\n", sn.SnapName(), rev)
		mylog.Check(s.w.SetInfo(sn, info))
		mylog.Check(s.validateSnapArchs([]*seedwriter.SeedSnap{sn}))

		return sn.Path, nil
	}
	snapToDownloadOptions := make([]tooling.SnapToDownload, len(snapsToDownload))
	for i, sn := range snapsToDownload {
		vss, rev := mylog.Check3(s.validationSetKeysAndRevisionForSnap(sn.SnapName()))

		byName[sn.SnapName()] = sn
		revisions[sn.SnapName()] = rev
		snapToDownloadOptions[i].Snap = sn
		snapToDownloadOptions[i].Channel = sn.Channel
		snapToDownloadOptions[i].Revision = rev
		snapToDownloadOptions[i].CohortKey = s.wideCohortKey
		snapToDownloadOptions[i].ValidationSets = vss
	}

	// sort the curSnaps slice for test consistency
	sort.Slice(curSnaps, func(i, j int) bool {
		return curSnaps[i].SnapName < curSnaps[j].SnapName
	})
	downloadedSnaps = mylog.Check2(s.tsto.DownloadMany(snapToDownloadOptions, curSnaps, tooling.DownloadManyOptions{
		BeforeDownloadFunc: beforeDownload,
		EnforceValidation:  s.customizations.Validation == "enforce",
	}))

	return downloadedSnaps, nil
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

func (s *imageSeeder) downloadAllSnaps(localSnaps localSnapRefs, fetchAsserts seedwriter.AssertsFetchFunc) error {
	curSnaps := localSnapsWithID(localSnaps)
	for {
		toDownload := mylog.Check2(s.w.SnapsToDownload())

		downloadedSnaps := mylog.Check2(s.downloadSnaps(toDownload, curSnaps))

		for _, sn := range toDownload {
			dlsn := downloadedSnaps[sn.SnapName()]
			mylog.Check(s.w.SetRedirectChannel(sn, dlsn.RedirectChannel))

			curSnaps = append(curSnaps, &tooling.CurrentSnap{
				SnapName: sn.Info.SnapName(),
				SnapID:   sn.Info.ID(),
				Revision: sn.Info.Revision,
				Epoch:    sn.Info.Epoch,
				Channel:  sn.Channel,
			})
		}

		complete := mylog.Check2(s.w.Downloaded(fetchAsserts))

		if complete {
			break
		}
	}
	return nil
}

func (s *imageSeeder) finishSeedClassic() error {
	var fpath string
	if s.hasModes {
		fpath = filepath.Join(s.seedDir, "systems")
	} else {
		fpath = filepath.Join(s.seedDir, "seed.yaml")
	}
	// warn about ownership if not root:root
	fi := mylog.Check2(os.Stat(fpath))

	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		if st.Uid != 0 || st.Gid != 0 {
			fmt.Fprintf(Stderr, "WARNING: ensure that the contents under %s are owned by root:root in the (final) image\n", s.seedDir)
		}
	}
	// done already
	return nil
}

func (s *imageSeeder) finishSeedCore() error {
	gadgetUnpackDir := filepath.Join(s.prepareDir, "gadget")
	kernelUnpackDir := filepath.Join(s.prepareDir, "kernel")

	bootSnaps := mylog.Check2(s.w.BootSnaps())

	bootWith := &boot.BootableSet{
		UnpackedGadgetDir: gadgetUnpackDir,
		Recovery:          s.hasModes,
	}
	if s.label != "" {
		bootWith.RecoverySystemDir = filepath.Join("/systems/", s.label)
		bootWith.RecoverySystemLabel = s.label
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
	mylog.Check(

		// unpacking the gadget for core models
		unpackSnap(bootWith.GadgetPath, gadgetUnpackDir))
	mylog.Check(unpackSnap(kernelFname, kernelUnpackDir))

	gadgetInfo := mylog.Check2(gadget.ReadInfoAndValidate(gadgetUnpackDir, s.model, nil))
	mylog.Check(

		// validate content against the kernel as well
		gadget.ValidateContent(gadgetInfo, gadgetUnpackDir, kernelUnpackDir))
	mylog.Check(

		// write resolved content to structure root
		writeResolvedContent(s.prepareDir, gadgetInfo, gadgetUnpackDir, kernelUnpackDir))
	mylog.Check(boot.MakeBootableImage(s.model, s.bootRootDir, bootWith, s.customizations.BootFlags))

	// early config & cloud-init config (done at install for Core 20)
	if !s.hasModes {
		mylog.Check(
			// and the cloud-init things
			installCloudConfig(s.rootDir, gadgetUnpackDir))

		defaultsDir := sysconfig.WritableDefaultsDir(s.rootDir)
		defaults := gadget.SystemDefaults(gadgetInfo.Defaults)
		if len(defaults) > 0 {
			mylog.Check(os.MkdirAll(sysconfig.WritableDefaultsDir(s.rootDir, "/etc"), 0755))
			mylog.Check(sysconfig.ApplyFilesystemOnlyDefaults(s.model, defaultsDir, defaults))

		}

		customizeImage(s.rootDir, defaultsDir, s.customizations)
	}
	return nil
}

func (s *imageSeeder) warnOnUnassertedSnaps() error {
	unassertedSnaps := mylog.Check2(s.w.UnassertedSnaps())

	if len(unassertedSnaps) > 0 {
		locals := make([]string, len(unassertedSnaps))
		for i, sn := range unassertedSnaps {
			locals[i] = sn.SnapName()
		}
		fmt.Fprintf(Stderr, "WARNING: %s installed from local snaps disconnected from a store cannot be refreshed subsequently!\n", strutil.Quoted(locals))
	}
	return nil
}

func (s *imageSeeder) finish() error {
	// print any warnings that occurred during the download phase
	for _, warn := range s.w.Warnings() {
		fmt.Fprintf(Stderr, "WARNING: %s\n", warn)
	}
	mylog.Check(

		// print warnings on unasserted snaps
		s.warnOnUnassertedSnaps())

	// run validation-set checks, this is also done by store but
	// we double-check for the seed.
	if s.customizations.Validation != "ignore" {
		mylog.Check(s.w.CheckValidationSets())
	}

	copySnap := func(name, src, dst string) error {
		fmt.Fprintf(Stdout, "Copying %q (%s)\n", src, name)
		return osutil.CopyFile(src, dst, 0)
	}
	mylog.Check(s.w.SeedSnaps(copySnap))
	mylog.Check(s.w.WriteMeta())

	// TODO: There will be classic UC20+ model based systems
	//       that will have a bootable  ubuntu-seed partition.
	//       This will need to be handled here eventually too.
	if s.classic {
		return s.finishSeedClassic()
	}
	return s.finishSeedCore()
}

func optionSnaps(opts *Options) []*seedwriter.OptionsSnap {
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

func selectAssertionMaxFormats(tsto *tooling.ToolingStore, model *asserts.Model, sysSn, kernSn *seedwriter.SeedSnap) error {
	if sysSn == nil {
		// nothing to do
		return nil
	}
	snapf := mylog.Check2(snapfile.Open(sysSn.Path))

	maxFormats, _ := mylog.Check3(snap.SnapdAssertionMaxFormatsFromSnapFile(snapf))

	if model.Grade() != asserts.ModelGradeUnset && kernSn != nil {
		// take also kernel into account
		kf := mylog.Check2(snapfile.Open(kernSn.Path))

		kMaxFormats, _ := mylog.Check3(snap.SnapdAssertionMaxFormatsFromSnapFile(kf))

		if kMaxFormats == nil {
			fmt.Fprintf(Stderr, "WARNING: the kernel for the specified UC20+ model does not carry assertion max formats information, assuming possibly incorrectly the kernel revision can use the same formats as snapd\n")
		} else {
			for name, maxFormat := range maxFormats {
				// pick the lowest format
				if kMaxFormats[name] < maxFormat {
					maxFormats[name] = kMaxFormats[name]
				}
			}
		}
	}
	tsto.SetAssertionMaxFormats(maxFormats)
	return nil
}

var setupSeed = func(tsto *tooling.ToolingStore, model *asserts.Model, opts *Options) error {
	s := mylog.Check2(newImageSeeder(tsto, model, opts))
	mylog.Check(s.start(optionSnaps(opts)))

	// We need to use seedwriter.DeriveSideInfo earlier than
	// we might possibly know the system and kernel snaps to
	// know the correct assertion max format to use.
	// Fetch assertions tentatively into a temporary database
	// and later either copy them or fetch more appropriate ones.
	tmpDb := s.db.WithStackedBackstore(asserts.NewMemoryBackstore())
	tmpFetcher := seedwriter.MakeSeedAssertionFetcher(func(save func(asserts.Assertion) error) asserts.Fetcher {
		return tsto.AssertionFetcher(tmpDb, save)
	})

	localSnaps := mylog.Check2(s.deriveInfoForLocalSnaps(tmpFetcher, tmpDb))

	if opts.Customizations.Validation == "" {
		if !opts.Classic {
			fmt.Fprintf(Stderr, "WARNING: proceeding to download snaps ignoring validations, this default will change in the future. For now use --validation=enforce for validations to be taken into account, pass instead --validation=ignore to preserve current behavior going forward\n")
		}
		opts.Customizations.Validation = "ignore"
	}

	assertMaxFormatsSelected := false
	var assertMaxFormats map[string]int

	copyOrRefetchIfFormatTooNewIntoDb := func(aRefs []*asserts.Ref) error {
		// copy or re-fetch assertions to replace if the format is too
		// new; as the replacing is based on the primary key previous
		// cross check on provenance will still be valid or db
		// consistency checks will fail
		for _, aRef := range aRefs {
			a := mylog.Check2(aRef.Resolve(tmpDb.Find))

			if assertMaxFormats != nil && a.Format() > assertMaxFormats[aRef.Type.Name] {
				mylog.Check(
					// format was too new, re-fetch to replace
					s.f.Fetch(aRef))
			} else {
				mylog.Check(
					// copy
					s.f.Save(a))
			}
		}
		return nil
	}

	fetchAsserts := func(sn, sysSn, kernSn *seedwriter.SeedSnap) ([]*asserts.Ref, error) {
		if !assertMaxFormatsSelected {
			mylog.Check(selectAssertionMaxFormats(tsto, model, sysSn, kernSn))

			assertMaxFormatsSelected = true
			assertMaxFormats = tsto.AssertionMaxFormats()
		}
		prev := len(s.f.Refs())
		if aRefs, ok := localSnaps[sn]; ok {
			mylog.Check(copyOrRefetchIfFormatTooNewIntoDb(aRefs))
		} else {
			mylog.Check2(
				// fetch snap assertions
				FetchAndCheckSnapAssertions(sn.Path, sn.Info, model, s.f, s.db))
		}
		return s.f.Refs()[prev:], nil
	}
	mylog.Check(s.downloadAllSnaps(localSnaps, fetchAsserts))

	return s.finish()
}
