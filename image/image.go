// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/store"
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
		return info.Name()
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

func localSnaps(opts *Options) (*localInfos, error) {
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
			nameToPath[info.Name()] = snapName
			local[snapName] = info
		}
	}
	return &localInfos{
		pathToInfo: local,
		nameToPath: nameToPath,
	}, nil
}

func Prepare(opts *Options) error {
	model, err := decodeModelAssertion(opts)
	if err != nil {
		return err
	}

	local, err := localSnaps(opts)
	if err != nil {
		return err
	}

	// FIXME: limitation until we can pass series parametrized much more
	if model.Series() != release.Series {
		return fmt.Errorf("model with series %q != %q unsupported", model.Series(), release.Series)
	}
	sto := makeStore(model)

	if err := downloadUnpackGadget(sto, model, opts, local); err != nil {
		return err
	}

	return bootstrapToRootDir(sto, model, opts, local)
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

func downloadUnpackGadget(sto Store, model *asserts.Model, opts *Options, local *localInfos) error {
	if err := os.MkdirAll(opts.GadgetUnpackDir, 0755); err != nil {
		return fmt.Errorf("cannot create gadget unpack dir %q: %s", opts.GadgetUnpackDir, err)
	}

	dlOpts := &DownloadOptions{
		TargetDir: opts.GadgetUnpackDir,
		Channel:   opts.Channel,
	}
	snapFn, _, err := acquireSnap(sto, model.Gadget(), dlOpts, local)
	if err != nil {
		return err
	}
	// FIXME: jumping through layers here, we need to make
	//        unpack part of the container interface (again)
	snap := squashfs.New(snapFn)
	return snap.Unpack("*", opts.GadgetUnpackDir)
}

func acquireSnap(sto Store, name string, dlOpts *DownloadOptions, local *localInfos) (downloadedSnap string, info *snap.Info, err error) {
	if info := local.Info(name); info != nil {
		// local snap to install (unasserted only for now)
		p := local.Path(name)
		dst, err := copyLocalSnapFile(p, dlOpts.TargetDir, info)
		if err != nil {
			return "", nil, err
		}
		return dst, info, nil
	}
	return DownloadSnap(sto, name, snap.R(0), dlOpts)
}

type addingFetcher struct {
	asserts.Fetcher
	addedRefs []*asserts.Ref
}

func makeFetcher(sto Store, dlOpts *DownloadOptions, db *asserts.Database) *addingFetcher {
	var f addingFetcher
	save := func(a asserts.Assertion) error {
		f.addedRefs = append(f.addedRefs, a.Ref())
		return nil
	}
	f.Fetcher = StoreAssertionFetcher(sto, dlOpts, db, save)
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
	} else {
		dst := filepath.Join(cloudDir, "cloud-init.disabled")
		err = osutil.AtomicWriteFile(dst, nil, 0644, 0)
	}
	return err
}

const defaultCore = "core"

var trusted = sysdb.Trusted()

func MockTrusted(mockTrusted []asserts.Assertion) (restore func()) {
	prevTrusted := trusted
	trusted = mockTrusted
	return func() {
		trusted = prevTrusted
	}
}

func bootstrapToRootDir(sto Store, model *asserts.Model, opts *Options, local *localInfos) error {
	// FIXME: try to avoid doing this
	if opts.RootDir != "" {
		dirs.SetRootDir(opts.RootDir)
		defer dirs.SetRootDir("/")
	}

	// sanity check target
	if osutil.FileExists(dirs.SnapStateFile) {
		return fmt.Errorf("cannot bootstrap over existing system")
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
	f := makeFetcher(sto, &DownloadOptions{}, db)

	if err := f.Save(model); err != nil {
		if !osutil.GetenvBool("UBUNTU_IMAGE_SKIP_COPY_UNVERIFIED_MODEL") {
			return fmt.Errorf("cannot fetch and check prerequisites for the model assertion: %v", err)
		} else {
			logger.Noticef("Cannot fetch and check prerequisites for the model assertion, it will not be copied into the image: %v", err)
			f.addedRefs = nil
		}
	}

	// put snaps in place
	if err := os.MkdirAll(dirs.SnapBlobDir, 0755); err != nil {
		return err
	}

	snapSeedDir := filepath.Join(dirs.SnapSeedDir, "snaps")
	assertSeedDir := filepath.Join(dirs.SnapSeedDir, "assertions")
	dlOpts := &DownloadOptions{
		TargetDir: snapSeedDir,
		Channel:   opts.Channel,
		DevMode:   false, // XXX: should this be true?
	}

	for _, d := range []string{snapSeedDir, assertSeedDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}

	snaps := []string{}
	// core,kernel,gadget first
	snaps = append(snaps, local.PreferLocal(defaultCore))
	snaps = append(snaps, local.PreferLocal(model.Kernel()))
	snaps = append(snaps, local.PreferLocal(model.Gadget()))
	// then required and the user requested stuff
	for _, snapName := range model.RequiredSnaps() {
		snaps = append(snaps, local.PreferLocal(snapName))
	}
	snaps = append(snaps, opts.Snaps...)

	seen := make(map[string]bool)
	var locals []string
	downloadedSnapsInfo := map[string]*snap.Info{}
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

		fn, info, err := acquireSnap(sto, name, dlOpts, local)
		if err != nil {
			return err
		}

		seen[name] = true
		typ := info.Type

		// if it comes from the store fetch the snap assertions too
		// TODO: support somehow including available assertions
		// also for local snaps
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
				publisher := snapDecl.PublisherID()
				if publisher != model.BrandID() && publisher != "canonical" {
					return fmt.Errorf("cannot use %s %q published by %q for model by %q", kind, name, publisher, model.BrandID())
				}
			}
		} else {
			locals = append(locals, name)
		}

		// kernel/os are required for booting
		if typ == snap.TypeKernel || typ == snap.TypeOS {
			dst := filepath.Join(dirs.SnapBlobDir, filepath.Base(fn))
			if err := osutil.CopyFile(fn, dst, 0); err != nil {
				return err
			}
			// store the snap.Info for kernel/os so
			// that the bootload can DTRT
			downloadedSnapsInfo[dst] = info
		}

		// set seed.yaml
		seedYaml.Snaps = append(seedYaml.Snaps, &snap.SeedSnap{
			Name:    info.Name(),
			SnapID:  info.SnapID, // cross-ref
			Channel: info.Channel,
			File:    filepath.Base(fn),
			DevMode: info.NeedsDevMode(),
			// no assertions for this snap were put in the seed
			Unasserted: info.SnapID == "",
		})
	}
	if len(locals) > 0 {
		fmt.Fprintf(Stderr, "WARNING: %s were installed from local snaps disconnected from a store and cannot be refreshed subsequently!", strutil.Quoted(locals))
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

	if err := setBootvars(downloadedSnapsInfo); err != nil {
		return err
	}

	// and the cloud-init things
	if err := installCloudConfig(opts.GadgetUnpackDir); err != nil {
		return err
	}

	return nil
}

func setBootvars(downloadedSnapsInfo map[string]*snap.Info) error {
	// Set bootvars for kernel/core snaps so the system boots and
	// does the first-time initialization. There is also no
	// mounted kernel/core snap, but just the blobs.
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
	for _, fn := range snaps {
		bootvar := ""

		info := downloadedSnapsInfo[fn]
		switch info.Type {
		case snap.TypeOS:
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

func runCommand(cmdStr ...string) error {
	cmd := exec.Command(cmdStr[0], cmdStr[1:]...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cannot run %v: %s", cmdStr, osutil.OutputErr(output, err))
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

func makeStore(model *asserts.Model) Store {
	cfg := store.DefaultConfig()
	cfg.Architecture = model.Architecture()
	cfg.StoreID = model.Store()
	return store.New(cfg, nil)
}
