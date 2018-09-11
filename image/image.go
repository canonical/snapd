// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2017 Canonical Ltd
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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/partition"
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
	Snaps           []string
	RootDir         string
	Channel         string
	ModelFile       string
	GadgetUnpackDir string
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
		if strings.HasSuffix(snapName, ".snap") && osutil.FileExists(snapName) {
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
	}
	return &localInfos{
		pathToInfo: local,
		nameToPath: nameToPath,
	}, nil
}

func validateNoParallelSnapInstances(snaps []string) error {
	for _, snapName := range snaps {
		_, instanceKey := snap.SplitInstanceName(snapName)
		if instanceKey != "" {
			return fmt.Errorf("cannot use snap %q, parallel snap instances are unsupported", snapName)
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
	return validateNoParallelSnapInstances(nonLocalSnaps)
}

func Prepare(opts *Options) error {
	model, err := decodeModelAssertion(opts)
	if err != nil {
		return err
	}

	if err := validateNonLocalSnaps(opts.Snaps); err != nil {
		return err
	}
	if _, err := snap.ParseChannel(opts.Channel, ""); err != nil {
		return fmt.Errorf("cannot use channel: %v", err)
	}

	// TODO: might make sense to support this later
	if model.Classic() {
		return fmt.Errorf("cannot prepare image of a classic model")
	}

	tsto, err := NewToolingStoreFromModel(model)
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

	if err := downloadUnpackGadget(tsto, model, opts, local); err != nil {
		return err
	}

	return bootstrapToRootDir(tsto, model, opts, local)
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

	modelSnaps := modela.RequiredSnaps()
	modelSnaps = append(modelSnaps, modela.Kernel(), modela.Gadget(), modela.Base())
	if err := validateNoParallelSnapInstances(modelSnaps); err != nil {
		return nil, err
	}

	return modela, nil
}

func downloadUnpackGadget(tsto *ToolingStore, model *asserts.Model, opts *Options, local *localInfos) error {
	if err := os.MkdirAll(opts.GadgetUnpackDir, 0755); err != nil {
		return fmt.Errorf("cannot create gadget unpack dir %q: %s", opts.GadgetUnpackDir, err)
	}

	dlOpts := &DownloadOptions{
		TargetDir: opts.GadgetUnpackDir,
		Channel:   opts.Channel,
	}
	if model.GadgetTrack() != "" {
		gch, err := makeChannelFromTrack("gadget", model.GadgetTrack(), opts.Channel)
		if err != nil {
			return err
		}
		dlOpts.Channel = gch
	}
	snapFn, _, err := acquireSnap(tsto, model.Gadget(), dlOpts, local)
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
	return tsto.DownloadSnap(name, snap.R(0), dlOpts)
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
	var err error

	cloudDir := filepath.Join(dirs.GlobalRootDir, "/etc/cloud")
	if err := os.MkdirAll(cloudDir, 0755); err != nil {
		return err
	}

	cloudConfig := filepath.Join(gadgetDir, "cloud.conf")
	if osutil.FileExists(cloudConfig) {
		dst := filepath.Join(cloudDir, "cloud.cfg")
		err = osutil.CopyFile(cloudConfig, dst, osutil.CopyFlagOverwrite)
	}
	return err
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

func makeChannelFromTrack(what, track, defaultChannel string) (string, error) {
	errPrefix := fmt.Sprintf("cannot use track %q for %s from model assertion", track, what)
	mch, err := snap.ParseChannel(track, "")
	if err != nil {
		return "", fmt.Errorf("%s: %v", errPrefix, err)
	}
	if defaultChannel != "" {
		dch, err := snap.ParseChannel(defaultChannel, "")
		if err != nil {
			return "", fmt.Errorf("internal error: cannot parse channel %q", defaultChannel)
		}
		mch.Risk = dch.Risk
	}
	return mch.Clean().String(), nil
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

func bootstrapToRootDir(tsto *ToolingStore, model *asserts.Model, opts *Options, local *localInfos) error {
	// FIXME: try to avoid doing this
	if opts.RootDir != "" {
		dirs.SetRootDir(opts.RootDir)
		defer dirs.SetRootDir("/")
	}

	// sanity check target
	if osutil.FileExists(dirs.SnapStateFile) {
		return fmt.Errorf("cannot bootstrap over existing system")
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
	if err := os.MkdirAll(dirs.SnapBlobDir, 0755); err != nil {
		return err
	}

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

	snaps := []string{}
	// always add an implicit snapd first when a base is used
	if model.Base() != "" {
		snaps = append(snaps, "snapd")
		// TODO: once we order snaps by what they need this
		//       can go aways
		// Here we ensure that "core" is seeded very early
		// when bases are in use. This fixes the issue
		// that when people use model assertions with
		// required snaps like bluez which at this point
		// still requires core will hang forever in seeding.
		if strutil.ListContains(opts.Snaps, "core") {
			snaps = append(snaps, "core")
		}
	}
	// core/base,kernel,gadget first
	snaps = append(snaps, local.PreferLocal(baseName))
	snaps = append(snaps, local.PreferLocal(model.Kernel()))
	snaps = append(snaps, local.PreferLocal(model.Gadget()))
	// then required and the user requested stuff
	for _, snapName := range model.RequiredSnaps() {
		snaps = append(snaps, local.PreferLocal(snapName))
	}
	snaps = append(snaps, opts.Snaps...)

	seen := make(map[string]bool)
	var locals []string
	downloadedSnapsInfoForBootConfig := map[string]*snap.Info{}
	var seedYaml snap.Seed
	for _, snapName := range snaps {
		name := local.Name(snapName)
		if seen[name] {
			fmt.Fprintf(Stdout, "%s already prepared, skipping\n", name)
			continue
		}

		if name != snapName {
			fmt.Fprintf(Stdout, "Copying %q (%s)\n", snapName, name)
		} else {
			fmt.Fprintf(Stdout, "Fetching %s\n", snapName)
		}

		snapChannel := opts.Channel
		if name == model.Kernel() && model.KernelTrack() != "" {
			kch, err := makeChannelFromTrack("kernel", model.KernelTrack(), opts.Channel)
			if err != nil {
				return err
			}
			snapChannel = kch
		}
		if name == model.Gadget() && model.GadgetTrack() != "" {
			gch, err := makeChannelFromTrack("gadget", model.GadgetTrack(), opts.Channel)
			if err != nil {
				return err
			}
			snapChannel = gch
		}
		dlOpts := &DownloadOptions{
			TargetDir: snapSeedDir,
			Channel:   snapChannel,
		}

		fn, info, err := acquireSnap(tsto, name, dlOpts, local)
		if err != nil {
			return err
		}
		// Sanity check, note that we could support this case
		// if we have a use-case but it requires changes in the
		// devicestate/firstboot.go ordering code.
		if info.Type == snap.TypeGadget && info.Base != model.Base() {
			return fmt.Errorf("cannot use gadget snap because its base %q is different from model base %q", info.Base, model.Base())
		}
		if info.Base != "" && !local.hasName(snaps, info.Base) {
			return fmt.Errorf("cannot add snap %q without also adding its base %q explicitly", name, info.Base)
		}
		// warn about missing default providers
		dps := neededDefaultProviders(info)
		for _, dp := range dps {
			if !local.hasName(snaps, dp) {
				fmt.Fprintf(Stderr, "WARNING: the %q default content provider %q is not getting installed.", info.InstanceName(), dp)
			}
		}

		seen[name] = true
		typ := info.Type

		// if it comes from the store fetch the snap assertions too
		if info.SnapID != "" {
			snapDecl, err := FetchAndCheckSnapAssertions(fn, info, f, db)
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
			locals = append(locals, name)
			// local snaps have no channel
			snapChannel = ""
		}

		// kernel/os/model.base are required for booting
		if typ == snap.TypeKernel || local.Name(snapName) == baseName {
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
			downloadedSnapsInfoForBootConfig[dst] = info
		}

		// set seed.yaml
		seedYaml.Snaps = append(seedYaml.Snaps, &snap.SeedSnap{
			Name:    info.InstanceName(),
			SnapID:  info.SnapID, // cross-ref
			Channel: snapChannel,
			File:    filepath.Base(fn),
			DevMode: info.NeedsDevMode(),
			Contact: info.Contact,
			// no assertions for this snap were put in the seed
			Unasserted: info.SnapID == "",
		})
	}
	if len(locals) > 0 {
		fmt.Fprintf(Stderr, "WARNING: %s were installed from local snaps disconnected from a store and cannot be refreshed subsequently!\n", strutil.Quoted(locals))
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

	// TODO: add the refs as an assertions list of maps section to seed.yaml

	seedFn := filepath.Join(dirs.SnapSeedDir, "seed.yaml")
	if err := seedYaml.Write(seedFn); err != nil {
		return fmt.Errorf("cannot write seed.yaml: %s", err)
	}

	// now do the bootloader stuff
	if err := partition.InstallBootConfig(opts.GadgetUnpackDir); err != nil {
		return err
	}

	if err := setBootvars(downloadedSnapsInfoForBootConfig, model); err != nil {
		return err
	}

	// and the cloud-init things
	if err := installCloudConfig(opts.GadgetUnpackDir); err != nil {
		return err
	}

	return nil
}

func setBootvars(downloadedSnapsInfoForBootConfig map[string]*snap.Info, model *asserts.Model) error {
	if len(downloadedSnapsInfoForBootConfig) != 2 {
		return fmt.Errorf("setBootvars can only be called with exactly one kernel and exactly one core/base boot info: %v", downloadedSnapsInfoForBootConfig)
	}

	// Set bootvars for kernel/core snaps so the system boots and
	// does the first-time initialization. There is also no
	// mounted kernel/core/base snap, but just the blobs.
	bootloader, err := partition.FindBootloader()
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
		switch info.Type {
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
	if err := bootloader.SetBootVars(m); err != nil {
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
