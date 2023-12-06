// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2024 Canonical Ltd
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

package bootloader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/bootloader/grubenv"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

// grub implements the required interfaces
var (
	_ Bootloader                        = (*grub)(nil)
	_ RecoveryAwareBootloader           = (*grub)(nil)
	_ ExtractedRunKernelImageBootloader = (*grub)(nil)
	_ TrustedAssetsBootloader           = (*grub)(nil)
)

type grub struct {
	rootdir string

	basedir string

	uefiRunKernelExtraction bool
	recovery                bool
	nativePartitionLayout   bool
	prepareImageTime        bool
}

// newGrub create a new Grub bootloader object
func newGrub(rootdir string, opts *Options) Bootloader {
	g := &grub{rootdir: rootdir}
	if opts != nil {
		// Set the flag to extract the run kernel, only
		// for UC20 run mode.
		// Both UC16/18 and the recovery mode of UC20 load
		// the kernel directly from snaps.
		g.uefiRunKernelExtraction = opts.Role == RoleRunMode
		g.recovery = opts.Role == RoleRecovery
		g.nativePartitionLayout = opts.NoSlashBoot || g.recovery
		g.prepareImageTime = opts.PrepareImageTime
	}
	if g.nativePartitionLayout {
		g.basedir = "EFI/ubuntu"
	} else {
		g.basedir = "boot/grub"
	}

	return g
}

func (g *grub) Name() string {
	return "grub"
}

func (g *grub) dir() string {
	if g.rootdir == "" {
		panic("internal error: unset rootdir")
	}
	return filepath.Join(g.rootdir, g.basedir)
}

func (g *grub) installManagedRecoveryBootConfig() error {
	assetName := g.Name() + "-recovery.cfg"
	systemFile := filepath.Join(g.rootdir, "/EFI/ubuntu/grub.cfg")
	return genericSetBootConfigFromAsset(systemFile, assetName)
}

func (g *grub) installManagedBootConfig() error {
	assetName := g.Name() + ".cfg"
	systemFile := filepath.Join(g.rootdir, "/EFI/ubuntu/grub.cfg")
	return genericSetBootConfigFromAsset(systemFile, assetName)
}

func (g *grub) InstallBootConfig(gadgetDir string, opts *Options) error {
	if opts != nil && opts.Role == RoleRecovery {
		// install managed config for the recovery partition
		return g.installManagedRecoveryBootConfig()
	}
	if opts != nil && opts.Role == RoleRunMode {
		// install managed boot config that can handle kernel.efi
		return g.installManagedBootConfig()
	}

	gadgetFile := filepath.Join(gadgetDir, g.Name()+".conf")
	systemFile := filepath.Join(g.rootdir, "/boot/grub/grub.cfg")
	return genericInstallBootConfig(gadgetFile, systemFile)
}

func (g *grub) SetRecoverySystemEnv(recoverySystemDir string, values map[string]string) error {
	if recoverySystemDir == "" {
		return fmt.Errorf("internal error: recoverySystemDir unset")
	}
	recoverySystemGrubEnv := filepath.Join(g.rootdir, recoverySystemDir, "grubenv")
	if err := os.MkdirAll(filepath.Dir(recoverySystemGrubEnv), 0755); err != nil {
		return err
	}
	genv := grubenv.NewEnv(recoverySystemGrubEnv)
	for k, v := range values {
		genv.Set(k, v)
	}
	return genv.Save()
}

func (g *grub) GetRecoverySystemEnv(recoverySystemDir string, key string) (string, error) {
	if recoverySystemDir == "" {
		return "", fmt.Errorf("internal error: recoverySystemDir unset")
	}
	recoverySystemGrubEnv := filepath.Join(g.rootdir, recoverySystemDir, "grubenv")
	genv := grubenv.NewEnv(recoverySystemGrubEnv)
	if err := genv.Load(); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return genv.Get(key), nil
}

func (g *grub) Present() (bool, error) {
	return osutil.FileExists(filepath.Join(g.dir(), "grub.cfg")), nil
}

func (g *grub) envFile() string {
	return filepath.Join(g.dir(), "grubenv")
}

func (g *grub) GetBootVars(names ...string) (map[string]string, error) {
	out := make(map[string]string)

	env := grubenv.NewEnv(g.envFile())
	if err := env.Load(); err != nil {
		return nil, err
	}

	for _, name := range names {
		out[name] = env.Get(name)
	}

	return out, nil
}

func (g *grub) SetBootVars(values map[string]string) error {
	env := grubenv.NewEnv(g.envFile())
	if err := env.Load(); err != nil && !os.IsNotExist(err) {
		return err
	}
	for k, v := range values {
		env.Set(k, v)
	}
	return env.Save()
}

func (g *grub) extractedKernelDir(prefix string, s snap.PlaceInfo) string {
	return filepath.Join(
		prefix,
		s.Filename(),
	)
}

func (g *grub) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	// default kernel assets are:
	// - kernel.img
	// - initrd.img
	// - dtbs/*
	var assets []string
	if g.uefiRunKernelExtraction {
		assets = []string{"kernel.efi"}
	} else {
		assets = []string{"kernel.img", "initrd.img", "dtbs/*"}
	}

	// extraction can be forced through either a special file in the kernel snap
	// or through an option in the bootloader
	_, err := snapf.ReadFile("meta/force-kernel-extraction")
	if g.uefiRunKernelExtraction || err == nil {
		return extractKernelAssetsToBootDir(
			g.extractedKernelDir(g.dir(), s),
			snapf,
			assets,
		)
	}
	return nil
}

func (g *grub) RemoveKernelAssets(s snap.PlaceInfo) error {
	return removeKernelAssetsFromBootDir(g.dir(), s)
}

// ExtractedRunKernelImageBootloader helper methods

func (g *grub) makeKernelEfiSymlink(s snap.PlaceInfo, name string) error {
	// use a relative symlink destination so that it resolves properly, if grub
	// is located at /run/mnt/ubuntu-boot or /boot/grub, etc.
	target := filepath.Join(
		s.Filename(),
		"kernel.efi",
	)

	// the location of the destination symlink as an absolute filepath
	source := filepath.Join(g.dir(), name)

	// check that the kernel snap has been extracted already so we don't
	// inadvertently create a dangling symlink
	// expand the relative symlink from g.dir()
	if !osutil.FileExists(filepath.Join(g.dir(), target)) {
		return fmt.Errorf(
			"cannot enable %s at %s: %v",
			name,
			target,
			os.ErrNotExist,
		)
	}

	// the symlink doesn't exist so just create it
	return osutil.AtomicSymlink(target, source)
}

// unlinkKernelEfiSymlink will remove the specified symlink if it exists. Note
// that if the symlink is "dangling", it will still remove the symlink without
// returning an error. This is useful for example to disable a try-kernel that
// was incorrectly created.
func (g *grub) unlinkKernelEfiSymlink(name string) error {
	symlink := filepath.Join(g.dir(), name)
	err := os.Remove(symlink)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (g *grub) readKernelSymlink(name string) (snap.PlaceInfo, error) {
	// read the symlink from <grub-dir>/<name> to
	// <grub-dir>/<snap-file-name>/<name> and parse the
	// directory (which is supposed to be the name of the snap) into the snap
	link := filepath.Join(g.dir(), name)

	// check that the symlink is not dangling before continuing
	if !osutil.FileExists(link) {
		return nil, fmt.Errorf("cannot read dangling symlink %s", name)
	}

	targetKernelEfi, err := os.Readlink(link)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s symlink: %v", link, err)
	}

	kernelSnapFileName := filepath.Base(filepath.Dir(targetKernelEfi))
	sn, err := snap.ParsePlaceInfoFromSnapFileName(kernelSnapFileName)
	if err != nil {
		return nil, fmt.Errorf(
			"cannot parse kernel snap file name from symlink target %q: %v",
			kernelSnapFileName,
			err,
		)
	}
	return sn, nil
}

// actual ExtractedRunKernelImageBootloader methods

// EnableKernel will install a kernel.efi symlink in the bootloader partition,
// pointing to the referenced kernel snap. EnableKernel() will fail if the
// referenced kernel snap does not exist.
func (g *grub) EnableKernel(s snap.PlaceInfo) error {
	// add symlink from ubuntuBootPartition/kernel.efi to
	// <ubuntu-boot>/EFI/ubuntu/<snap-name>.snap/kernel.efi
	// so that we are consistent between uc16/uc18 and uc20 with where we
	// extract kernels
	return g.makeKernelEfiSymlink(s, "kernel.efi")
}

// EnableTryKernel will install a try-kernel.efi symlink in the bootloader
// partition, pointing towards the referenced kernel snap. EnableTryKernel()
// will fail if the referenced kernel snap does not exist.
func (g *grub) EnableTryKernel(s snap.PlaceInfo) error {
	// add symlink from ubuntuBootPartition/kernel.efi to
	// <ubuntu-boot>/EFI/ubuntu/<snap-name>.snap/kernel.efi
	// so that we are consistent between uc16/uc18 and uc20 with where we
	// extract kernels
	return g.makeKernelEfiSymlink(s, "try-kernel.efi")
}

// DisableTryKernel will remove the try-kernel.efi symlink if it exists. Note
// that when performing an update, you should probably first use EnableKernel(),
// then DisableTryKernel() for maximum safety.
func (g *grub) DisableTryKernel() error {
	return g.unlinkKernelEfiSymlink("try-kernel.efi")
}

// Kernel will return the kernel snap currently installed in the bootloader
// partition, pointed to by the kernel.efi symlink.
func (g *grub) Kernel() (snap.PlaceInfo, error) {
	return g.readKernelSymlink("kernel.efi")
}

// TryKernel will return the kernel snap currently being tried if it exists and
// false if there is not currently a try-kernel.efi symlink. Note if the symlink
// exists but does not point to an existing file an error will be returned.
func (g *grub) TryKernel() (snap.PlaceInfo, error) {
	// check that the _symlink_ exists, not that it points to something real
	// we check for whether it is a dangling symlink inside readKernelSymlink,
	// which returns an error when the symlink is dangling
	_, err := os.Lstat(filepath.Join(g.dir(), "try-kernel.efi"))
	if err == nil {
		p, err := g.readKernelSymlink("try-kernel.efi")
		// if we failed to read the symlink, then the try kernel isn't usable,
		// so return err because the symlink is there
		if err != nil {
			return nil, err
		}
		return p, nil
	}
	return nil, ErrNoTryKernelRef
}

// UpdateBootConfig updates the grub boot config only if it is already managed
// and has a lower edition.
//
// Implements TrustedAssetsBootloader for the grub bootloader.
func (g *grub) UpdateBootConfig() (bool, error) {
	// XXX: do we need to take opts here?
	bootScriptName := "grub.cfg"
	currentBootConfig := filepath.Join(g.dir(), "grub.cfg")
	if g.recovery {
		// use the recovery asset when asked to do so
		bootScriptName = "grub-recovery.cfg"
	}
	return genericUpdateBootConfigFromAssets(currentBootConfig, bootScriptName)
}

// ManagedAssets returns a list relative paths to boot assets inside the root
// directory of the filesystem.
//
// Implements TrustedAssetsBootloader for the grub bootloader.
func (g *grub) ManagedAssets() []string {
	return []string{
		filepath.Join(g.basedir, "grub.cfg"),
	}
}

func (g *grub) commandLineForEdition(edition uint, pieces CommandLineComponents) (string, error) {
	if err := pieces.Validate(); err != nil {
		return "", err
	}

	var nonSnapdCmdline string
	if pieces.FullArgs == "" {
		staticCmdline := g.defaultCommandLineForEdition(edition)

		keepDefaultArgs := kcmdline.RemoveMatchingFilter(staticCmdline, pieces.RemoveArgs)

		nonSnapdCmdline = strutil.JoinNonEmpty(append(keepDefaultArgs, pieces.ExtraArgs), " ")
	} else {
		nonSnapdCmdline = pieces.FullArgs
	}
	args, err := kcmdline.Split(nonSnapdCmdline)
	if err != nil {
		return "", fmt.Errorf("cannot use badly formatted kernel command line: %v", err)
	}
	// join all argument with a single space, see
	// grub-core/lib/cmdline.c:grub_create_loader_cmdline() for reference,
	// arguments are separated by a single space, the space after last is
	// replaced with terminating NULL
	snapdArgs := make([]string, 0, 2)
	if pieces.ModeArg != "" {
		snapdArgs = append(snapdArgs, pieces.ModeArg)
	}
	if pieces.SystemArg != "" {
		snapdArgs = append(snapdArgs, pieces.SystemArg)
	}
	return strings.Join(append(snapdArgs, args...), " "), nil
}

func (g *grub) assetName() string {
	if g.recovery {
		return "grub-recovery.cfg"
	}

	return "grub.cfg"
}

func (g *grub) defaultCommandLineForEdition(edition uint) string {
	return staticCommandLineForGrubAssetEdition(g.assetName(), edition)
}

func editionFromDiskConfigAssetFallback(bootConfig string) (uint, error) {
	edition, err := editionFromDiskConfigAsset(bootConfig)
	if err != nil {
		if err != errNoEdition {
			return 0, err
		}
		// we were called using the TrustedAssetsBootloader interface
		// meaning the caller expects to us to use the managed assets,
		// since one on disk is not managed, use the initial edition of
		// the internal boot asset which is compatible with grub.cfg
		// used before we started writing out the files ourselves
		edition = 1
	}

	return edition, nil
}

// CommandLine returns the kernel command line composed of mode and
// system arguments, followed by either a built-in bootloader specific
// static arguments corresponding to the on-disk boot asset edition, and
// any extra arguments or a separate set of arguments provided in the
// components. The command line may be different when using a recovery
// bootloader.
//
// Implements TrustedAssetsBootloader for the grub bootloader.
func (g *grub) CommandLine(pieces CommandLineComponents) (string, error) {
	currentBootConfig := filepath.Join(g.dir(), "grub.cfg")

	edition, err := editionFromDiskConfigAssetFallback(currentBootConfig)
	if err != nil {
		return "", fmt.Errorf("cannot obtain edition number of current boot config: %v", err)
	}

	return g.commandLineForEdition(edition, pieces)
}

// CandidateCommandLine is similar to CommandLine, but uses the current
// edition of managed built-in boot assets as reference.
//
// Implements TrustedAssetsBootloader for the grub bootloader.
func (g *grub) CandidateCommandLine(pieces CommandLineComponents) (string, error) {
	edition, err := editionFromInternalConfigAsset(g.assetName())
	if err != nil {
		return "", err
	}
	return g.commandLineForEdition(edition, pieces)
}

// DefaultCommandLine returns the default kernel command-line used by
// the bootloader excluding the recovery mode and system parameters.
func (g *grub) DefaultCommandLine(candidate bool) (string, error) {
	var edition uint

	// if "candidate", we look for the managed boot assets
	// (current snapd) rather than the ones currently installed on
	// the boot/seed disk. This is needed to know the default
	// command line before candidate boot assets are installed
	if candidate {
		var err error
		edition, err = editionFromInternalConfigAsset(g.assetName())
		if err != nil {
			return "", err
		}
	} else {
		currentBootConfig := filepath.Join(g.dir(), "grub.cfg")

		var err error
		edition, err = editionFromDiskConfigAssetFallback(currentBootConfig)
		if err != nil {
			return "", fmt.Errorf("cannot obtain edition number of current boot config: %v", err)
		}
	}

	return g.defaultCommandLineForEdition(edition), nil
}

// staticCommandLineForGrubAssetEdition fetches a static command line for given
// grub asset edition
func staticCommandLineForGrubAssetEdition(asset string, edition uint) string {
	cmdline := assets.SnippetForEdition(fmt.Sprintf("%s:static-cmdline", asset), edition)
	if cmdline == nil {
		return ""
	}
	return string(cmdline)
}

type taggedPath struct {
	tag  string
	path string
}

func (t taggedPath) Id() string {
	basename := filepath.Base(t.path)
	if t.tag == "" {
		return basename
	}
	return fmt.Sprintf("%s:%s", t.tag, basename)
}

// grubBootAssetPath contains the paths for assets in the boot chain.
type grubBootAssetPath struct {
	defaultShimBinary taggedPath
	defaultGrubBinary taggedPath
	fallbackBinary    taggedPath
	shimBinary        taggedPath
	grubBinary        taggedPath
}

// grubBootAssetsForArch contains the paths for assets for different
// architectures in a map.
// For backward compliance, we do not have tags
// for asset paths that used to exist before usage of tags.
var grubBootAssetsForArch = map[string]grubBootAssetPath{
	"amd64": {
		defaultShimBinary: taggedPath{
			path: filepath.Join("EFI/boot/", "bootx64.efi"),
		},
		defaultGrubBinary: taggedPath{
			path: filepath.Join("EFI/boot/", "grubx64.efi"),
		},
		fallbackBinary: taggedPath{
			tag:  "boot",
			path: filepath.Join("EFI/boot/", "fbx64.efi"),
		},
		shimBinary: taggedPath{
			tag:  "ubuntu",
			path: filepath.Join("EFI/ubuntu/", "shimx64.efi"),
		},
		grubBinary: taggedPath{
			tag:  "ubuntu",
			path: filepath.Join("EFI/ubuntu/", "grubx64.efi"),
		},
	},
	"arm64": {
		defaultShimBinary: taggedPath{
			path: filepath.Join("EFI/boot/", "bootaa64.efi"),
		},
		defaultGrubBinary: taggedPath{
			path: filepath.Join("EFI/boot/", "grubaa64.efi"),
		},
		fallbackBinary: taggedPath{
			tag:  "boot",
			path: filepath.Join("EFI/boot/", "fbaa64.efi"),
		},
		shimBinary: taggedPath{
			tag:  "ubuntu",
			path: filepath.Join("EFI/ubuntu/", "shimaa64.efi"),
		},
		grubBinary: taggedPath{
			tag:  "ubuntu",
			path: filepath.Join("EFI/ubuntu/", "grubaa64.efi"),
		},
	},
}

func (g *grub) getGrubBootAssetsForArch() (*grubBootAssetPath, error) {
	if g.prepareImageTime {
		return nil, fmt.Errorf("internal error: retrieving boot assets at prepare image time")
	}
	archi := arch.DpkgArchitecture()
	assets, ok := grubBootAssetsForArch[archi]
	if !ok {
		return nil, fmt.Errorf("cannot find grub assets for %q", archi)
	}
	return &assets, nil
}

// getGrubRecoveryModeTrustedAssets returns the list of ordered asset
// chain for recovery mode, which are shim and grub from the seed
// partition.
func (g *grub) getGrubRecoveryModeTrustedAssets() ([][]taggedPath, error) {
	assets, err := g.getGrubBootAssetsForArch()
	if err != nil {
		return nil, err
	}
	return [][]taggedPath{{assets.shimBinary, assets.grubBinary}, {assets.defaultShimBinary, assets.defaultGrubBinary}}, nil
}

// getGrubRunModeTrustedAssets returns the list of ordered asset
// chains for run mode, which is grub from the boot partition.
func (g *grub) getGrubRunModeTrustedAssets() ([][]taggedPath, error) {
	assets, err := g.getGrubBootAssetsForArch()
	if err != nil {
		return nil, err
	}
	return [][]taggedPath{{assets.defaultGrubBinary}}, nil
}

// TrustedAssets returns the map of relative paths to asset
// identifers. The relative paths are relative to the bootloader's
// rootdir. The asset identifiers correspond to the backward
// compatible names recorded in the modeenv (CurrentTrustedBootAssets
// and CurrentTrustedRecoveryBootAssets).
func (g *grub) TrustedAssets() (map[string]string, error) {
	if !g.nativePartitionLayout {
		return nil, fmt.Errorf("internal error: trusted assets called without native host-partition layout")
	}
	ret := make(map[string]string)
	var chains [][]taggedPath
	var err error
	if g.recovery {
		chains, err = g.getGrubRecoveryModeTrustedAssets()
	} else {
		chains, err = g.getGrubRunModeTrustedAssets()
	}
	if err != nil {
		return nil, err
	}
	for _, chain := range chains {
		for _, asset := range chain {
			ret[asset.path] = asset.Id()
		}
	}
	return ret, nil
}

// RecoveryBootChains returns the list of load chains for recovery modes.
// It should be called on a RoleRecovery bootloader.
func (g *grub) RecoveryBootChains(kernelPath string) ([][]BootFile, error) {
	if !g.recovery {
		return nil, fmt.Errorf("not a recovery bootloader")
	}

	// add trusted assets to the recovery chain
	assetsSet, err := g.getGrubRecoveryModeTrustedAssets()
	if err != nil {
		return nil, err
	}
	chains := make([][]BootFile, 0, len(assetsSet))
	for _, assets := range assetsSet {
		chain := make([]BootFile, 0, len(assets)+1)
		for _, ta := range assets {
			chain = append(chain, NewBootFile("", ta.path, RoleRecovery))
		}
		// add recovery kernel to the recovery chain
		chain = append(chain, NewBootFile(kernelPath, "kernel.efi", RoleRecovery))
		chains = append(chains, chain)
	}

	return chains, nil
}

// BootChains returns the list of load chains for run mode.
// It should be called on a RoleRecovery bootloader passing the
// RoleRunMode bootloader.
func (g *grub) BootChains(runBl Bootloader, kernelPath string) ([][]BootFile, error) {
	if !g.recovery {
		return nil, fmt.Errorf("not a recovery bootloader")
	}
	if runBl.Name() != "grub" {
		return nil, fmt.Errorf("run mode bootloader must be grub")
	}

	// add trusted assets to the recovery chain
	recoveryModeAssetsSet, err := g.getGrubRecoveryModeTrustedAssets()
	if err != nil {
		return nil, err
	}
	runModeAssetsSet, err := g.getGrubRunModeTrustedAssets()
	if err != nil {
		return nil, err
	}
	chains := make([][]BootFile, 0, len(recoveryModeAssetsSet)*len(runModeAssetsSet))
	for _, recoveryModeAssets := range recoveryModeAssetsSet {
		for _, runModeAssets := range runModeAssetsSet {
			chain := make([]BootFile, 0, len(recoveryModeAssets)+len(runModeAssets)+1)
			for _, ta := range recoveryModeAssets {
				chain = append(chain, NewBootFile("", ta.path, RoleRecovery))
			}
			for _, ta := range runModeAssets {
				chain = append(chain, NewBootFile("", ta.path, RoleRunMode))
			}
			// add kernel to the boot chain
			chain = append(chain, NewBootFile(kernelPath, "kernel.efi", RoleRunMode))
			chains = append(chains, chain)
		}
	}

	return chains, nil
}
