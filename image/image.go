// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/strutil"
)

var (
	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr
)

type Options struct {
	Classic         bool
	Snaps           []string
	RootDir         string
	Channel         string
	SnapChannels    map[string]string
	ModelFile       string
	GadgetUnpackDir string

	// Architecture to use if none is specified by the model,
	// useful only for classic mode. If set must match the model otherwise.
	Architecture string
}

type localInfos struct {
	// path to info for local snaps
	pathToInfo map[string]*snap.Info
	// name to path
	nameToPath map[string]string
}

func (li *localInfos) Name(pathOrName string) string {
	if info := li.pathToInfo[pathOrName]; info != nil {
		return info.InstanceName()
	}
	return pathOrName
}

func (li *localInfos) IsLocal(name string) bool {
	_, ok := li.nameToPath[name]
	return ok
}

func (li *localInfos) PreferLocal(name string) string {
	if path := li.Path(name); path != "" {
		return path
	}
	return name
}

func (li *localInfos) Path(name string) string {
	return li.nameToPath[name]
}

func (li *localInfos) Info(name string) *snap.Info {
	if p := li.nameToPath[name]; p != "" {
		return li.pathToInfo[p]
	}
	return nil
}

// hasName returns true if the given "snapName" is found within the
// list of snap names or paths.
func (li *localInfos) hasName(snaps []string, snapName string) bool {
	for _, snapNameOrPath := range snaps {
		if li.Name(snapNameOrPath) == snapName {
			return true
		}
	}
	return false
}

func localSnaps(tsto *ToolingStore, opts *Options) (*localInfos, error) {
	local := make(map[string]*snap.Info)
	nameToPath := make(map[string]string)
	for _, snapName := range opts.Snaps {
		if !strings.HasSuffix(snapName, ".snap") {
			continue
		}

		if !osutil.FileExists(snapName) {
			return nil, fmt.Errorf("local snap %s not found", snapName)
		}

		snapFile, err := snap.Open(snapName)
		if err != nil {
			return nil, err
		}
		info, err := snap.ReadInfoFromSnapFile(snapFile, nil)
		if err != nil {
			return nil, err
		}
		// local snap gets local revision
		info.Revision = snap.R(-1)
		nameToPath[info.InstanceName()] = snapName
		local[snapName] = info

		si, err := snapasserts.DeriveSideInfo(snapName, tsto)
		if err != nil && !asserts.IsNotFound(err) {
			return nil, err
		}
		if err == nil {
			info.SnapID = si.SnapID
			info.Revision = si.Revision
		}
	}
	return &localInfos{
		pathToInfo: local,
		nameToPath: nameToPath,
	}, nil
}

func validateSnapNames(snaps []string) error {
	for _, snapName := range snaps {
		if _, instanceKey := snap.SplitInstanceName(snapName); instanceKey != "" {
			// be specific about this error
			return fmt.Errorf("cannot use snap %q, parallel snap instances are unsupported", snapName)
		}
		if err := snap.ValidateName(snapName); err != nil {
			return err
		}
	}
	return nil
}

// validateNonLocalSnaps raises an error when snaps that would be pulled from
// the store use an instance key in their names
func validateNonLocalSnaps(snaps []string) error {
	nonLocalSnaps := make([]string, 0, len(snaps))
	for _, snapName := range snaps {
		if !strings.HasSuffix(snapName, ".snap") {
			nonLocalSnaps = append(nonLocalSnaps, snapName)
		}
	}
	return validateSnapNames(nonLocalSnaps)
}

// classicHasSnaps returns whether the model or options specify any snaps for the classic case
func classicHasSnaps(model *asserts.Model, opts *Options) bool {
	return model.Gadget() != "" || len(model.RequiredSnaps()) != 0 || len(opts.Snaps) != 0
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
		if opts.GadgetUnpackDir != "" {
			return fmt.Errorf("internal error: no gadget unpacking is performed for classic models but directory specified")
		}
		if model.Architecture() == "" && classicHasSnaps(model, opts) && opts.Architecture == "" {
			return fmt.Errorf("cannot have snaps for a classic image without an architecture in the model or from --arch")
		}
	}

	if err := validateNonLocalSnaps(opts.Snaps); err != nil {
		return err
	}
	if _, err := snap.ParseChannel(opts.Channel, ""); err != nil {
		return fmt.Errorf("cannot use channel: %v", err)
	}

	tsto, err := NewToolingStoreFromModel(model, opts.Architecture)
	if err != nil {
		return err
	}

	local, err := localSnaps(tsto, opts)
	if err != nil {
		return err
	}

	// FIXME: limitation until we can pass series parametrized much more
	if model.Series() != release.Series {
		return fmt.Errorf("model with series %q != %q unsupported", model.Series(), release.Series)
	}

	if !opts.Classic {
		// unpacking the gadget for core models
		if err := downloadUnpackGadget(tsto, model, opts, local); err != nil {
			return err
		}
	}

	return setupSeed(tsto, model, opts, local)
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

// snapChannel returns the channel to use for the given snap.
func snapChannel(name string, model *asserts.Model, opts *Options, local *localInfos) (string, error) {
	snapChannel := opts.SnapChannels[local.PreferLocal(name)]
	if snapChannel == "" {
		// fallback to default channel
		snapChannel = opts.Channel
	}
	// consider snap types that can be pinned to a track by the model
	var pinnedTrack string
	var kind string
	switch name {
	case model.Gadget():
		kind = "gadget"
		pinnedTrack = model.GadgetTrack()
	case model.Kernel():
		kind = "kernel"
		pinnedTrack = model.KernelTrack()
	}
	if pinnedTrack != "" {
		ch, err := makeChannelFromTrack(kind, pinnedTrack, snapChannel)
		if err != nil {
			return "", err
		}
		snapChannel = ch

	}
	return snapChannel, nil
}

func makeChannelFromTrack(what, track, snapChannel string) (string, error) {
	mch, err := snap.ParseChannel(track, "")
	if err != nil {
		return "", fmt.Errorf("cannot use track %q for %s from model assertion: %v", track, what, err)
	}
	if snapChannel != "" {
		ch, err := snap.ParseChannelVerbatim(snapChannel, "")
		if err != nil {
			return "", fmt.Errorf("cannot parse channel %q for %s", snapChannel, what)
		}
		if ch.Track != "" && ch.Track != mch.Track {
			return "", fmt.Errorf("channel %q for %s has a track incompatible with the track from model assertion: %s", snapChannel, what, track)
		}
		mch.Risk = ch.Risk
	}
	return mch.Clean().String(), nil
}

func downloadUnpackGadget(tsto *ToolingStore, model *asserts.Model, opts *Options, local *localInfos) error {
	if err := os.MkdirAll(opts.GadgetUnpackDir, 0755); err != nil {
		return fmt.Errorf("cannot create gadget unpack dir %q: %s", opts.GadgetUnpackDir, err)
	}

	gadgetName := model.Gadget()
	gadgetChannel, err := snapChannel(gadgetName, model, opts, local)
	if err != nil {
		return err
	}

	dlOpts := &DownloadOptions{
		TargetDir: opts.GadgetUnpackDir,
		Channel:   gadgetChannel,
	}
	snapFn, _, err := acquireSnap(tsto, gadgetName, dlOpts, local)
	if err != nil {
		return err
	}
	// FIXME: jumping through layers here, we need to make
	//        unpack part of the container interface (again)
	snap := squashfs.New(snapFn)
	return snap.Unpack("*", opts.GadgetUnpackDir)
}

func acquireSnap(tsto *ToolingStore, name string, dlOpts *DownloadOptions, local *localInfos) (downloadedSnap string, info *snap.Info, err error) {
	if info := local.Info(name); info != nil {
		// local snap to install (unasserted only for now)
		p := local.Path(name)
		dst, err := copyLocalSnapFile(p, dlOpts.TargetDir, info)
		if err != nil {
			return "", nil, err
		}
		return dst, info, nil
	}
	return tsto.DownloadSnap(name, *dlOpts)
}

type addingFetcher struct {
	asserts.Fetcher
	addedRefs []*asserts.Ref
}

func makeFetcher(tsto *ToolingStore, dlOpts *DownloadOptions, db *asserts.Database) *addingFetcher {
	var f addingFetcher
	save := func(a asserts.Assertion) error {
		f.addedRefs = append(f.addedRefs, a.Ref())
		return nil
	}
	f.Fetcher = tsto.AssertionFetcher(db, save)
	return &f

}

func installCloudConfig(gadgetDir string) error {
	cloudConfig := filepath.Join(gadgetDir, "cloud.conf")
	if !osutil.FileExists(cloudConfig) {
		return nil
	}

	cloudDir := filepath.Join(dirs.GlobalRootDir, "/etc/cloud")
	if err := os.MkdirAll(cloudDir, 0755); err != nil {
		return err
	}
	dst := filepath.Join(cloudDir, "cloud.cfg")
	return osutil.CopyFile(cloudConfig, dst, osutil.CopyFlagOverwrite)
}

// defaultCore is used if no base is specified by the model
const defaultCore = "core"

var trusted = sysdb.Trusted()

func MockTrusted(mockTrusted []asserts.Assertion) (restore func()) {
	prevTrusted := trusted
	trusted = mockTrusted
	return func() {
		trusted = prevTrusted
	}
}

// neededDefaultProviders returns the names of all default-providers for
// the content plugs that the given snap.Info needs.
func neededDefaultProviders(info *snap.Info) (cps []string) {
	for _, plug := range info.Plugs {
		if plug.Interface == "content" {
			var dprovider string
			if err := plug.Attr("default-provider", &dprovider); err == nil && dprovider != "" {
				cps = append(cps, dprovider)
			}
		}
	}
	return cps
}

// hasBase checks if the given snap has a base in the given localInfos and
// snaps. If not an error is returned.
func hasBase(snap *snap.Info, local *localInfos, snaps []string) error {
	// snap needs no base (or it simply needs core which is never listed explicitly): nothing to do
	if snap.Base == "" {
		return nil
	}

	// snap explicitly listed as not needing a base snap (e.g. a content-only snap)
	if snap.Base == "none" {
		return nil
	}

	// core provides everything that core16 needs
	if snap.Base == "core16" && local.hasName(snaps, "core") {
		return nil
	}
	if local.hasName(snaps, snap.Base) {
		return nil
	}
	return fmt.Errorf("cannot add snap %q without also adding its base %q explicitly", snap.InstanceName(), snap.Base)
}

func setupSeed(tsto *ToolingStore, model *asserts.Model, opts *Options, local *localInfos) error {
	if model.Classic() != opts.Classic {
		return fmt.Errorf("internal error: classic model but classic mode not set")
	}

	// FIXME: try to avoid doing this
	if opts.RootDir != "" {
		dirs.SetRootDir(opts.RootDir)
		defer dirs.SetRootDir("/")
	}

	// sanity check target
	if osutil.FileExists(dirs.SnapStateFile) {
		return fmt.Errorf("cannot prepare seed over existing system or an already booted image, detected state file %s", dirs.SnapStateFile)
	}
	if snaps, _ := filepath.Glob(filepath.Join(dirs.SnapBlobDir, "*.snap")); len(snaps) > 0 {
		return fmt.Errorf("need an empty snap dir in rootdir, got: %v", snaps)
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
	f := makeFetcher(tsto, &DownloadOptions{}, db)

	if err := f.Save(model); err != nil {
		if !osutil.GetenvBool("UBUNTU_IMAGE_SKIP_COPY_UNVERIFIED_MODEL") {
			return fmt.Errorf("cannot fetch and check prerequisites for the model assertion: %v", err)
		} else {
			fmt.Fprintf(Stderr, "WARNING: Cannot fetch and check prerequisites for the model assertion, it will not be copied into the image making it unusable (unless this is a test): %v\n", err)
			f.addedRefs = nil
		}
	}

	// put snaps in place
	snapSeedDir := filepath.Join(dirs.SnapSeedDir, "snaps")
	assertSeedDir := filepath.Join(dirs.SnapSeedDir, "assertions")
	for _, d := range []string{snapSeedDir, assertSeedDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}

	baseName := defaultCore
	if model.Base() != "" {
		baseName = model.Base()
	}

	if !opts.Classic {
		if err := os.MkdirAll(dirs.SnapBlobDir, 0755); err != nil {
			return err
		}
	}

	basesAndApps := []string{}
	basesAndApps = append(basesAndApps, baseName)
	basesAndApps = append(basesAndApps, model.RequiredSnaps()...)
	basesAndApps = append(basesAndApps, opts.Snaps...)
	// TODO: required snaps should get their base from required
	// snaps (mentioned in the model); additional snaps could
	// fetch their base, providers instead if not already present
	// Be careful about core vs core16

	seed := &seed{
		model:        model,
		baseName:     baseName,
		opts:         opts,
		local:        local,
		tsto:         tsto,
		basesAndApps: basesAndApps,
		snapSeedDir:  snapSeedDir,
		f:            f,
		db:           db,
		seen:         make(map[string]bool),
		downloadedSnapsInfoForBootConfig: make(map[string]*snap.Info),
	}

	snaps := []string{}
	// always add an implicit snapd first when a base is used
	if model.Base() != "" {
		snaps = append(snaps, "snapd")
	}

	if !opts.Classic {
		// core/base,kernel,gadget first
		snaps = append(snaps, baseName)
		snaps = append(snaps, model.Kernel())
		snaps = append(snaps, model.Gadget())
	} else {
		// classic image case: first core as needed and gadget
		if classicHasSnaps(model, opts) {
			// TODO: later use snapd+core16 or core18 if specified
			snaps = append(snaps, "core")
		}
		if model.Gadget() != "" {
			snaps = append(snaps, model.Gadget())
		}
	}

	// then required and the user requested stuff
	snaps = append(snaps, model.RequiredSnaps()...)
	snaps = append(snaps, opts.Snaps...)

	for _, snapName := range snaps {
		if err := seed.add(snapName); err != nil {
			return err
		}
	}

	if len(seed.locals) > 0 {
		fmt.Fprintf(Stderr, "WARNING: %s were installed from local snaps disconnected from a store and cannot be refreshed subsequently!\n", strutil.Quoted(seed.locals))
	}

	// fetch device store assertion (and prereqs) if available
	if model.Store() != "" {
		err := snapasserts.FetchStore(f, model.Store())
		if err != nil {
			if nfe, ok := err.(*asserts.NotFoundError); !ok || nfe.Type != asserts.StoreType {
				return err
			}
		}
	}

	for _, aRef := range f.addedRefs {
		var afn string
		// the names don't matter in practice as long as they don't conflict
		if aRef.Type == asserts.ModelType {
			afn = "model"
		} else {
			afn = fmt.Sprintf("%s.%s", strings.Join(aRef.PrimaryKey, ","), aRef.Type.Name)
		}
		a, err := aRef.Resolve(db.Find)
		if err != nil {
			return fmt.Errorf("internal error: lost saved assertion")
		}
		err = ioutil.WriteFile(filepath.Join(assertSeedDir, afn), asserts.Encode(a), 0644)
		if err != nil {
			return err
		}
	}

	seedYaml := seed.seedYaml()
	seedFn := filepath.Join(dirs.SnapSeedDir, "seed.yaml")
	if err := seedYaml.Write(seedFn); err != nil {
		return fmt.Errorf("cannot write seed.yaml: %s", err)
	}

	if opts.Classic {
		// warn about ownership if not root:root
		fi, err := os.Stat(seedFn)
		if err != nil {
			return fmt.Errorf("cannot stat seed.yaml: %s", err)
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			if st.Uid != 0 || st.Gid != 0 {
				fmt.Fprintf(Stderr, "WARNING: ensure that the contents under %s are owned by root:root in the (final) image", dirs.SnapSeedDir)
			}
		}
	}

	if !opts.Classic {
		// now do the bootloader stuff
		if err := bootloader.InstallBootConfig(opts.GadgetUnpackDir); err != nil {
			return err
		}

		if err := setBootvars(seed.downloadedSnapsInfoForBootConfig, model); err != nil {
			return err
		}

		// and the cloud-init things
		if err := installCloudConfig(opts.GadgetUnpackDir); err != nil {
			return err
		}
	}

	return nil
}

type seedEntry struct {
	snap     *snap.SeedSnap
	snapType snap.Type
}

type seedEntriesByType []seedEntry

func (e seedEntriesByType) Len() int      { return len(e) }
func (e seedEntriesByType) Swap(i, j int) { e[i], e[j] = e[j], e[i] }
func (e seedEntriesByType) Less(i, j int) bool {
	return e[i].snapType.SortsBefore(e[j].snapType)
}

type seed struct {
	model    *asserts.Model
	baseName string

	opts  *Options
	local *localInfos

	tsto *ToolingStore

	basesAndApps []string

	snapSeedDir string
	f           asserts.Fetcher
	db          *asserts.Database

	entries seedEntriesByType

	seen                             map[string]bool
	locals                           []string
	downloadedSnapsInfoForBootConfig map[string]*snap.Info
}

func (s *seed) add(snapName string) error {
	model := s.model
	opts := s.opts
	local := s.local

	name := local.Name(snapName)
	if s.seen[name] {
		fmt.Fprintf(Stdout, "%s already prepared, skipping\n", name)
		return nil
	}

	if local.IsLocal(name) {
		fmt.Fprintf(Stdout, "Copying %q (%s)\n", local.Path(name), name)
	} else {
		fmt.Fprintf(Stdout, "Fetching %s\n", name)
	}

	snapChannel, err := snapChannel(name, model, opts, local)
	if err != nil {
		return err
	}

	dlOpts := &DownloadOptions{
		TargetDir: s.snapSeedDir,
		Channel:   snapChannel,
	}
	fn, info, err := acquireSnap(s.tsto, name, dlOpts, local)
	if err != nil {
		return err
	}

	// Sanity check, note that we could support this case
	// if we have a use-case but it requires changes in the
	// devicestate/firstboot.go ordering code.
	if info.GetType() == snap.TypeGadget && info.Base != model.Base() {
		return fmt.Errorf("cannot use gadget snap because its base %q is different from model base %q", info.Base, model.Base())
	}
	if err := hasBase(info, local, s.basesAndApps); err != nil {
		return err
	}
	// warn about missing default providers
	for _, dp := range neededDefaultProviders(info) {
		if !local.hasName(s.basesAndApps, dp) {
			// TODO: have a way to ignore this issue on a snap by snap basis?
			return fmt.Errorf("cannot use snap %q without its default content provider %q being added explicitly", info.InstanceName(), dp)
		}
	}

	s.seen[name] = true
	typ := info.GetType()

	needsClassic := info.NeedsClassic()
	if needsClassic && !opts.Classic {
		return fmt.Errorf("cannot use classic snap %q in a core system", info.InstanceName())
	}

	// if it comes from the store fetch the snap assertions too
	if info.SnapID != "" {
		snapDecl, err := FetchAndCheckSnapAssertions(fn, info, s.f, s.db)
		if err != nil {
			return err
		}
		var kind string
		switch typ {
		case snap.TypeKernel:
			kind = "kernel"
		case snap.TypeGadget:
			kind = "gadget"
		}
		if kind != "" { // kernel or gadget
			// TODO: share helpers with devicestate if the policy becomes much more complicated
			publisher := snapDecl.PublisherID()
			if publisher != model.BrandID() && publisher != "canonical" {
				return fmt.Errorf("cannot use %s %q published by %q for model by %q", kind, name, publisher, model.BrandID())
			}
		}
	} else {
		s.locals = append(s.locals, name)
		// local snaps have no channel
		snapChannel = ""
	}

	// kernel/os/model.base are required for booting on core
	if !opts.Classic && (typ == snap.TypeKernel || name == s.baseName) {
		dst := filepath.Join(dirs.SnapBlobDir, filepath.Base(fn))
		// construct a relative symlink from the blob dir
		// to the seed file
		relSymlink, err := filepath.Rel(dirs.SnapBlobDir, fn)
		if err != nil {
			return fmt.Errorf("cannot build symlink: %v", err)
		}
		if err := os.Symlink(relSymlink, dst); err != nil {
			return err
		}
		// store the snap.Info for kernel/os/base so
		// that the bootload can DTRT
		s.downloadedSnapsInfoForBootConfig[dst] = info
	}

	s.entries = append(s.entries, seedEntry{
		snap: &snap.SeedSnap{
			Name:    info.InstanceName(),
			SnapID:  info.SnapID, // cross-ref
			Channel: snapChannel,
			File:    filepath.Base(fn),
			DevMode: info.NeedsDevMode(),
			Classic: needsClassic,
			Contact: info.Contact,
			// no assertions for this snap were put in the seed
			Unasserted: info.SnapID == "",
		},
		snapType: typ,
	})

	return nil
}

func (s *seed) seedYaml() *snap.Seed {
	var seedYaml snap.Seed

	sort.Stable(s.entries)

	seedYaml.Snaps = make([]*snap.SeedSnap, len(s.entries))
	for i, e := range s.entries {
		seedYaml.Snaps[i] = e.snap
	}

	return &seedYaml
}

func setBootvars(downloadedSnapsInfoForBootConfig map[string]*snap.Info, model *asserts.Model) error {
	if len(downloadedSnapsInfoForBootConfig) != 2 {
		return fmt.Errorf("setBootvars can only be called with exactly one kernel and exactly one core/base boot info: %v", downloadedSnapsInfoForBootConfig)
	}

	// Set bootvars for kernel/core snaps so the system boots and
	// does the first-time initialization. There is also no
	// mounted kernel/core/base snap, but just the blobs.
	loader, err := bootloader.Find()
	if err != nil {
		return fmt.Errorf("cannot set kernel/core boot variables: %s", err)
	}

	snaps, err := filepath.Glob(filepath.Join(dirs.SnapBlobDir, "*.snap"))
	if len(snaps) == 0 || err != nil {
		return fmt.Errorf("internal error: cannot find core/kernel snap")
	}

	m := map[string]string{
		"snap_mode":       "",
		"snap_try_core":   "",
		"snap_try_kernel": "",
	}
	if model.DisplayName() != "" {
		m["snap_menuentry"] = model.DisplayName()
	}

	for _, fn := range snaps {
		bootvar := ""

		info := downloadedSnapsInfoForBootConfig[fn]
		if info == nil {
			// this should never happen, if it does print some
			// debug info
			keys := make([]string, 0, len(downloadedSnapsInfoForBootConfig))
			for k := range downloadedSnapsInfoForBootConfig {
				keys = append(keys, k)
			}
			return fmt.Errorf("cannot get download info for snap %s, available infos: %v", fn, keys)
		}
		switch info.GetType() {
		case snap.TypeOS, snap.TypeBase:
			bootvar = "snap_core"
		case snap.TypeKernel:
			bootvar = "snap_kernel"
			if err := extractKernelAssets(fn, info); err != nil {
				return err
			}
		}

		if bootvar != "" {
			name := filepath.Base(fn)
			m[bootvar] = name
		}
	}
	if err := loader.SetBootVars(m); err != nil {
		return err
	}

	return nil
}

func extractKernelAssets(snapPath string, info *snap.Info) error {
	snapf, err := snap.Open(snapPath)
	if err != nil {
		return err
	}

	if err := boot.ExtractKernelAssets(info, snapf); err != nil {
		return err
	}
	return nil
}

func copyLocalSnapFile(snapPath, targetDir string, info *snap.Info) (dstPath string, err error) {
	dst := filepath.Join(targetDir, filepath.Base(info.MountFile()))
	return dst, osutil.CopyFile(snapPath, dst, 0)
}

func ValidateSeed(seedFile string) error {
	seed, err := snap.ReadSeedYaml(seedFile)
	if err != nil {
		return err
	}

	var errs []error
	// read the snaps info
	snapInfos := make(map[string]*snap.Info)
	for _, seedSnap := range seed.Snaps {
		fn := filepath.Join(filepath.Dir(seedFile), "snaps", seedSnap.File)
		snapf, err := snap.Open(fn)
		if err != nil {
			errs = append(errs, err)
		} else {
			info, err := snap.ReadInfoFromSnapFile(snapf, nil)
			if err != nil {
				errs = append(errs, fmt.Errorf("cannot use snap %s: %v", fn, err))
			} else {
				snapInfos[info.InstanceName()] = info
			}
		}
	}

	// ensure we have either "core" or "snapd"
	_, haveCore := snapInfos["core"]
	_, haveSnapd := snapInfos["snapd"]
	if !(haveCore || haveSnapd) {
		errs = append(errs, fmt.Errorf("the core or snapd snap must be part of the seed"))
	}

	// check that all bases/default-providers are part of the seed
	for _, info := range snapInfos {
		// ensure base is available
		if info.Base != "" && info.Base != "none" {
			if _, ok := snapInfos[info.Base]; !ok {
				errs = append(errs, fmt.Errorf("cannot use snap %q: base %q is missing", info.InstanceName(), info.Base))
			}
		}
		// ensure core is available
		if info.Base == "" && info.SnapType == snap.TypeApp && info.InstanceName() != "snapd" {
			if _, ok := snapInfos["core"]; !ok {
				errs = append(errs, fmt.Errorf(`cannot use snap %q: required snap "core" missing`, info.InstanceName()))
			}
		}
		// ensure default-providers are available
		for _, dp := range neededDefaultProviders(info) {
			if _, ok := snapInfos[dp]; !ok {
				errs = append(errs, fmt.Errorf("cannot use snap %q: default provider %q is missing", info.InstanceName(), dp))
			}
		}
	}

	if errs != nil {
		var buf bytes.Buffer
		for _, err := range errs {
			fmt.Fprintf(&buf, "\n- %s", err)
		}
		return fmt.Errorf("cannot validate seed:%s", buf.Bytes())
	}

	return nil
}
