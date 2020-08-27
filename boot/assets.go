// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package boot

import (
	"crypto"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	_ "golang.org/x/crypto/sha3"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

type trustedAssetsCache struct {
	cacheDir string
	hash     crypto.Hash
}

func newTrustedAssetsCache(cacheDir string) *trustedAssetsCache {
	return &trustedAssetsCache{cacheDir: cacheDir, hash: crypto.SHA3_384}
}

func (c *trustedAssetsCache) assetKey(blName, assetName, assetHash string) string {
	return filepath.Join(blName, fmt.Sprintf("%s-%s", assetName, assetHash))
}

func (c *trustedAssetsCache) tempAssetKey(blName, assetName string) string {
	return filepath.Join(blName, assetName+".temp")
}

func (c *trustedAssetsCache) pathInCache(part string) string {
	return filepath.Join(c.cacheDir, part)
}

// fileHash calculates the hash of an arbitrary file using the same hash method
// as the cache.
func (c *trustedAssetsCache) fileHash(name string) (string, error) {
	digest, _, err := osutil.FileDigest(name, c.hash)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest), nil
}

// Add entry for a new named asset owned by a particular bootloader, with the
// binary content of the located at a given path. The cache ensures that only
// one entry for given tuple of (bootloader name, asset name, content-hash)
// exists in the cache.
func (c *trustedAssetsCache) Add(assetPath, blName, assetName string) (*trackedAsset, error) {
	if err := os.MkdirAll(c.pathInCache(blName), 0755); err != nil {
		return nil, fmt.Errorf("cannot create cache directory: %v", err)
	}

	// input
	inf, err := os.Open(assetPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open asset file: %v", err)
	}
	defer inf.Close()
	// temporary output
	tempPath := c.pathInCache(c.tempAssetKey(blName, assetName))
	outf, err := osutil.NewAtomicFile(tempPath, 0644, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return nil, fmt.Errorf("cannot create temporary cache file: %v", err)
	}
	defer outf.Cancel()

	// copy and hash at the same time
	h := c.hash.New()
	tr := io.TeeReader(inf, h)
	if _, err := io.Copy(outf, tr); err != nil {
		return nil, fmt.Errorf("cannot copy trusted asset to cache: %v", err)
	}
	hashStr := hex.EncodeToString(h.Sum(nil))
	cacheKey := c.assetKey(blName, assetName, hashStr)

	ta := &trackedAsset{
		blName: blName,
		name:   assetName,
		hash:   hashStr,
	}

	targetName := c.pathInCache(cacheKey)
	if osutil.FileExists(targetName) {
		// asset is already cached
		return ta, nil
	}
	// commit under a new name
	if err := outf.CommitAs(targetName); err != nil {
		return nil, fmt.Errorf("cannot commit file to assets cache: %v", err)
	}
	return ta, nil
}

func (c *trustedAssetsCache) Remove(blName, assetName, hashStr string) error {
	cacheKey := c.assetKey(blName, assetName, hashStr)
	if err := os.Remove(c.pathInCache(cacheKey)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// CopyBootAssetsCacheToRoot copies the boot assets cache to a corresponding
// location under a new root directory.
func CopyBootAssetsCacheToRoot(dstRoot string) error {
	if !osutil.IsDirectory(dirs.SnapBootAssetsDir) {
		// nothing to copy
		return nil
	}

	newCacheRoot := dirs.SnapBootAssetsDirUnder(dstRoot)
	if err := os.MkdirAll(newCacheRoot, 0755); err != nil {
		return fmt.Errorf("cannot create cache directory under new root: %v", err)
	}
	err := filepath.Walk(dirs.SnapBootAssetsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(dirs.SnapBootAssetsDir, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := os.MkdirAll(filepath.Join(newCacheRoot, relPath), info.Mode()); err != nil {
				return fmt.Errorf("cannot recreate cache directory %q: %v", relPath, err)
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported non-file entry %q mode %v", relPath, info.Mode())
		}
		if err := osutil.CopyFile(path, filepath.Join(newCacheRoot, relPath), osutil.CopyFlagPreserveAll); err != nil {
			return fmt.Errorf("cannot copy boot asset cache file %q: %v", relPath, err)
		}
		return nil
	})
	return err
}

// ErrObserverNotApplicable indicates that observer is not applicable for use
// with the model.
var ErrObserverNotApplicable = errors.New("observer not applicable")

// TrustedAssetsInstallObserverForModel returns a new trusted assets observer
// for use during installation of the run mode system, provided the device model
// supports secure boot. Otherwise, nil and ErrObserverNotApplicable is
// returned.
func TrustedAssetsInstallObserverForModel(model *asserts.Model, gadgetDir string) (*TrustedAssetsInstallObserver, error) {
	if model.Grade() == asserts.ModelGradeUnset {
		// no need to observe updates when assets are not managed
		return nil, ErrObserverNotApplicable
	}
	if gadgetDir == "" {
		return nil, fmt.Errorf("internal error: gadget dir not provided")
	}

	return &TrustedAssetsInstallObserver{
		model:     model,
		cache:     newTrustedAssetsCache(dirs.SnapBootAssetsDir),
		gadgetDir: gadgetDir,
	}, nil
}

type trackedAsset struct {
	blName, name, hash string
}

func (t *trackedAsset) equal(other *trackedAsset) bool {
	return t.blName == other.blName &&
		t.name == other.name &&
		t.hash == other.hash
}

func isAssetAlreadyTracked(bam bootAssetsMap, newAsset *trackedAsset) bool {
	return isAssetHashTrackedInMap(bam, newAsset.name, newAsset.hash)
}

func isAssetHashTrackedInMap(bam bootAssetsMap, assetName, assetHash string) bool {
	if bam == nil {
		return false
	}
	hashes, ok := bam[assetName]
	if !ok {
		return false
	}
	return strutil.ListContains(hashes, assetHash)
}

// TrustedAssetsInstallObserver tracks the installation of trusted boot assets.
type TrustedAssetsInstallObserver struct {
	model                 *asserts.Model
	gadgetDir             string
	cache                 *trustedAssetsCache
	blName                string
	trustedAssets         []string
	trackedAssets         bootAssetsMap
	trackedRecoveryAssets bootAssetsMap
}

// Observe observes the operation related to the content of a given gadget
// structure. In particular, the TrustedAssetsInstallObserver tracks writing of
// managed boot assets, such as the bootloader binary which is measured as part
// of the secure boot.
//
// Implements gadget.ContentObserver.
func (o *TrustedAssetsInstallObserver) Observe(op gadget.ContentOperation, affectedStruct *gadget.LaidOutStructure, root, realSource, relativeTarget string) (bool, error) {
	if affectedStruct.Role != gadget.SystemBoot {
		// only care about system-boot
		return true, nil
	}

	if o.blName == "" {
		// we have no information about the bootloader yet
		bl, err := bootloader.ForGadget(o.gadgetDir, root, &bootloader.Options{NoSlashBoot: true})
		if err != nil {
			return false, fmt.Errorf("cannot find bootloader: %v", err)
		}
		o.blName = bl.Name()
		tbl, ok := bl.(bootloader.TrustedAssetsBootloader)
		if !ok {
			return true, nil
		}
		trustedAssets, err := tbl.TrustedAssets()
		if err != nil {
			return false, fmt.Errorf("cannot list %q bootloader trusted assets: %v", bl.Name(), err)
		}
		o.trustedAssets = trustedAssets
	}
	if len(o.trustedAssets) == 0 || !strutil.ListContains(o.trustedAssets, relativeTarget) {
		// not one of the trusted assets
		return true, nil
	}
	ta, err := o.cache.Add(realSource, o.blName, filepath.Base(relativeTarget))
	if err != nil {
		return false, err
	}
	// during installation, modeenv is written out later, at this point we
	// only care that the same file may appear multiple times in gadget
	// structure content, so make sure we are not tracking it yet
	if !isAssetAlreadyTracked(o.trackedAssets, ta) {
		if o.trackedAssets == nil {
			o.trackedAssets = bootAssetsMap{}
		}
		if len(o.trackedAssets[ta.name]) > 0 {
			return false, fmt.Errorf("cannot reuse asset name %q", ta.name)
		}
		o.trackedAssets[ta.name] = append(o.trackedAssets[ta.name], ta.hash)
	}
	return true, nil
}

// ObserveExistingTrustedRecoveryAssets observes existing trusted assets of a
// recovery bootloader located inside a given root directory.
func (o *TrustedAssetsInstallObserver) ObserveExistingTrustedRecoveryAssets(recoveryRootDir string) error {
	bl, err := bootloader.Find(recoveryRootDir, &bootloader.Options{
		NoSlashBoot: true,
		Recovery:    true,
	})
	if err != nil {
		return fmt.Errorf("cannot identify recovery system bootloader: %v", err)
	}
	tbl, ok := bl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		// not a trusted assets bootloader
		return nil
	}
	trustedAssets, err := tbl.TrustedAssets()
	if err != nil {
		return fmt.Errorf("cannot list %q recovery bootloader trusted assets: %v", bl.Name(), err)
	}
	for _, trustedAsset := range trustedAssets {
		ta, err := o.cache.Add(filepath.Join(recoveryRootDir, trustedAsset), bl.Name(), filepath.Base(trustedAsset))
		if err != nil {
			return err
		}
		if !isAssetAlreadyTracked(o.trackedRecoveryAssets, ta) {
			if o.trackedRecoveryAssets == nil {
				o.trackedRecoveryAssets = bootAssetsMap{}
			}
			if len(o.trackedRecoveryAssets[ta.name]) > 0 {
				return fmt.Errorf("cannot reuse recovery asset name %q", ta.name)
			}
			o.trackedRecoveryAssets[ta.name] = append(o.trackedRecoveryAssets[ta.name], ta.hash)
		}
	}
	return nil
}

func (o *TrustedAssetsInstallObserver) currentTrustedBootAssetsMap() bootAssetsMap {
	return o.trackedAssets
}

func (o *TrustedAssetsInstallObserver) currentTrustedRecoveryBootAssetsMap() bootAssetsMap {
	return o.trackedRecoveryAssets
}

// TrustedAssetsUpdateObserverForModel returns a new trusted assets observer for
// tracking changes to the measured boot assets during gadget updates, provided
// the device model supports secure boot. Otherwise, nil and ErrObserverNotApplicable is
// returned.
func TrustedAssetsUpdateObserverForModel(model *asserts.Model) (*TrustedAssetsUpdateObserver, error) {
	if model.Grade() == asserts.ModelGradeUnset {
		// no need to observe updates when assets are not managed
		return nil, ErrObserverNotApplicable
	}

	return &TrustedAssetsUpdateObserver{
		cache: newTrustedAssetsCache(dirs.SnapBootAssetsDir),
	}, nil
}

// TrustedAssetsUpdateObserver tracks the updates of trusted boot assets and
// attempts to reseal when needed.
type TrustedAssetsUpdateObserver struct {
	cache *trustedAssetsCache

	bootBootloader    bootloader.Bootloader
	bootTrustedAssets []string

	seedBootloader    bootloader.Bootloader
	seedTrustedAssets []string

	modeenv *Modeenv
}

func findMaybeTrustedAssetsBootloader(root string, opts *bootloader.Options) (foundBl bootloader.Bootloader, trustedAssets []string, err error) {
	foundBl, err = bootloader.Find(root, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot find bootloader: %v", err)
	}
	tbl, ok := foundBl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return foundBl, nil, nil
	}
	trustedAssets, err = tbl.TrustedAssets()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot list %q bootloader trusted assets: %v", foundBl.Name(), err)
	}
	return foundBl, trustedAssets, nil
}

// Observe observes the operation related to the update or rollback of the
// content of a given gadget structure. In particular, the
// TrustedAssetsUpdateObserver tracks updates of managed boot assets, such as
// the bootloader binary which is measured as part of the secure boot.
//
// Implements gadget.ContentUpdateObserver.
func (o *TrustedAssetsUpdateObserver) Observe(op gadget.ContentOperation, affectedStruct *gadget.LaidOutStructure, root, realSource, relativeTarget string) (bool, error) {
	var whichBootloader bootloader.Bootloader
	var whichAssets []string
	var err error
	var isRecovery bool

	switch affectedStruct.Role {
	case gadget.SystemBoot:
		if o.bootBootloader == nil {
			o.bootBootloader, o.bootTrustedAssets, err = findMaybeTrustedAssetsBootloader(root, &bootloader.Options{
				NoSlashBoot: true,
			})
			if err != nil {
				return false, err
			}
		}
		whichBootloader = o.bootBootloader
		whichAssets = o.bootTrustedAssets
	case gadget.SystemSeed:
		if o.seedBootloader == nil {
			o.seedBootloader, o.seedTrustedAssets, err = findMaybeTrustedAssetsBootloader(root, &bootloader.Options{
				NoSlashBoot: true,
				Recovery:    true,
			})
			if err != nil {
				return false, err
			}
		}
		whichBootloader = o.seedBootloader
		whichAssets = o.seedTrustedAssets
		isRecovery = true
	default:
		// only system-seed and system-boot are of interest
		return true, nil
	}
	if len(whichAssets) == 0 || !strutil.ListContains(whichAssets, relativeTarget) {
		// not one of the trusted assets
		return true, nil
	}
	if o.modeenv == nil {
		// we've hit a trusted asset, so a modeenv is needed now too
		o.modeenv, err = ReadModeenv("")
		if err != nil {
			return false, fmt.Errorf("cannot load modeenv: %v", err)
		}
	}
	switch op {
	case gadget.ContentUpdate:
		return o.observeUpdate(whichBootloader, isRecovery, root, realSource, relativeTarget)
	case gadget.ContentRollback:
		return o.observeRollback(whichBootloader, isRecovery, root, realSource, relativeTarget)
	default:
		// we only care about update and rollback actions
		return false, nil
	}
}

func (o *TrustedAssetsUpdateObserver) observeUpdate(bl bootloader.Bootloader, recovery bool, root, realSource, relativeTarget string) (bool, error) {
	modeenvBefore, err := o.modeenv.Copy()
	if err != nil {
		return false, fmt.Errorf("cannot copy modeenv: %v", err)
	}

	ta, err := o.cache.Add(realSource, bl.Name(), filepath.Base(relativeTarget))
	if err != nil {
		return false, err
	}

	trustedAssets := &o.modeenv.CurrentTrustedBootAssets
	if recovery {
		trustedAssets = &o.modeenv.CurrentTrustedRecoveryBootAssets
	}
	if !isAssetAlreadyTracked(*trustedAssets, ta) {
		if *trustedAssets == nil {
			*trustedAssets = bootAssetsMap{}
		}
		if len((*trustedAssets)[ta.name]) > 1 {
			// we expect at most 2 different blobs for a given asset
			// name, the current one and one that will be installed
			// during an update; more entries indicates that the
			// same asset name is used multiple times with different
			// content
			return false, fmt.Errorf("cannot reuse asset name %q", ta.name)
		}
		(*trustedAssets)[ta.name] = append((*trustedAssets)[ta.name], ta.hash)
	}

	if o.modeenv.deepEqual(modeenvBefore) {
		return true, nil
	}
	if err := o.modeenv.WriteTo(""); err != nil {
		return false, fmt.Errorf("cannot write modeeenv: %v", err)
	}
	return true, nil
}

func (o *TrustedAssetsUpdateObserver) observeRollback(bl bootloader.Bootloader, recovery bool, root, realSource, relativeTarget string) (bool, error) {
	trustedAssets := &o.modeenv.CurrentTrustedBootAssets
	otherTrustedAssets := o.modeenv.CurrentTrustedRecoveryBootAssets
	if recovery {
		trustedAssets = &o.modeenv.CurrentTrustedRecoveryBootAssets
		otherTrustedAssets = o.modeenv.CurrentTrustedBootAssets
	}

	assetName := filepath.Base(relativeTarget)
	hashList, ok := (*trustedAssets)[assetName]
	if !ok || len(hashList) == 0 {
		// asset not tracked in modeenv
		return true, nil
	}

	// new assets are appended to the list
	expectedOldHash := hashList[0]
	// sanity check, make sure that the current file is what we expect
	newlyAdded := false
	ondiskHash, err := o.cache.fileHash(filepath.Join(root, relativeTarget))
	if err != nil {
		// file may not exist if it was added by the update, that's ok
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("cannot calculate the digest of current asset: %v", err)
		}
		newlyAdded = true
		if len(hashList) > 1 {
			// we have more than 1 hash of the asset, so we expected
			// a previous revision to be restored, but got nothing
			// instead
			return false, fmt.Errorf("tracked asset %q is unexpectedly missing from disk",
				assetName)
		}
	} else {
		if ondiskHash != expectedOldHash {
			// this is unexpected, a different file exists on disk?
			return false, fmt.Errorf("unexpected content of existing asset %q", relativeTarget)
		}
	}

	// XXX: do we need to support using the same asset name multiple times
	// for a given bootloader?
	newHash := ""
	if len(hashList) == 1 {
		if newlyAdded {
			newHash = hashList[0]
		}
	} else {
		newHash = hashList[1]
	}
	if newHash != "" && !isAssetHashTrackedInMap(otherTrustedAssets, assetName, newHash) {
		// asset revision is not used used elsewhere, we can remove it from the cache
		if err := o.cache.Remove(bl.Name(), assetName, newHash); err != nil {
			// XXX: should this be a log instead?
			return false, fmt.Errorf("cannot remove unused boot asset %v:%v: %v", assetName, newHash, err)
		}
	}

	// update modeenv content
	if !newlyAdded {
		(*trustedAssets)[assetName] = hashList[:1]
	} else {
		delete(*trustedAssets, assetName)
	}

	if err := o.modeenv.WriteTo(""); err != nil {
		return false, fmt.Errorf("cannot write modeeenv: %v", err)
	}

	return false, nil
}

// BeforeWrite is called when the update process has been staged for execution.
func (o *TrustedAssetsUpdateObserver) BeforeWrite() error {
	// TODO:UC20:
	// - reseal with a given state of modeenv
	return nil
}

// Canceled is called when the update has been canceled, or if changes
// were written and the update has been reverted.
func (o *TrustedAssetsUpdateObserver) Canceled() error {
	// TODO:UC20:
	// - drop unused assets and update modeenv if needed
	// - reseal with a given state of modeenv
	return nil
}
