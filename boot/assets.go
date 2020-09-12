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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/strutil"
)

type trustedAssetsCache struct {
	cacheDir string
	hash     crypto.Hash
}

func newTrustedAssetsCache(cacheDir string) *trustedAssetsCache {
	return &trustedAssetsCache{cacheDir: cacheDir, hash: crypto.SHA3_384}
}

func (c *trustedAssetsCache) tempAssetRelPath(blName, assetName string) string {
	return filepath.Join(blName, assetName+".temp")
}

func (c *trustedAssetsCache) pathInCache(part string) string {
	return filepath.Join(c.cacheDir, part)
}

func trustedAssetCacheRelPath(blName, assetName, assetHash string) string {
	return filepath.Join(blName, fmt.Sprintf("%s-%s", assetName, assetHash))
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
	tempPath := c.pathInCache(c.tempAssetRelPath(blName, assetName))
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
	cacheKey := trustedAssetCacheRelPath(blName, assetName, hashStr)

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
	cacheKey := trustedAssetCacheRelPath(blName, assetName, hashStr)
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
	encryptionKey         secboot.EncryptionKey
}

// Observe observes the operation related to the content of a given gadget
// structure. In particular, the TrustedAssetsInstallObserver tracks writing of
// managed boot assets, such as the bootloader binary which is measured as part
// of the secure boot.
//
// Implements gadget.ContentObserver.
func (o *TrustedAssetsInstallObserver) Observe(op gadget.ContentOperation, affectedStruct *gadget.LaidOutStructure, root, relativeTarget string, data *gadget.ContentChange) (bool, error) {
	if affectedStruct.Role != gadget.SystemBoot {
		// only care about system-boot
		return true, nil
	}

	if o.blName == "" {
		// we have no information about the bootloader yet
		bl, err := bootloader.ForGadget(o.gadgetDir, root, &bootloader.Options{Role: bootloader.RoleRunMode, NoSlashBoot: true})
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
	ta, err := o.cache.Add(data.After, o.blName, filepath.Base(relativeTarget))
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
		Role: bootloader.RoleRecovery,
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

func (o *TrustedAssetsInstallObserver) ChosenEncryptionKey(key secboot.EncryptionKey) {
	o.encryptionKey = key
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
	// there is no need to track assets if we did not seal any keys
	if !hasSealedKeys(dirs.GlobalRootDir) {
		return nil, ErrObserverNotApplicable
	}

	return &TrustedAssetsUpdateObserver{
		cache: newTrustedAssetsCache(dirs.SnapBootAssetsDir),
		model: model,
	}, nil
}

// TrustedAssetsUpdateObserver tracks the updates of trusted boot assets and
// attempts to reseal when needed.
type TrustedAssetsUpdateObserver struct {
	cache *trustedAssetsCache
	model *asserts.Model

	bootBootloader    bootloader.Bootloader
	bootTrustedAssets []string
	changedAssets     []*trackedAsset

	seedBootloader    bootloader.Bootloader
	seedTrustedAssets []string
	seedChangedAssets []*trackedAsset

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
func (o *TrustedAssetsUpdateObserver) Observe(op gadget.ContentOperation, affectedStruct *gadget.LaidOutStructure, root, relativeTarget string, data *gadget.ContentChange) (bool, error) {
	var whichBootloader bootloader.Bootloader
	var whichAssets []string
	var err error
	var isRecovery bool

	switch affectedStruct.Role {
	case gadget.SystemBoot:
		if o.bootBootloader == nil {
			o.bootBootloader, o.bootTrustedAssets, err = findMaybeTrustedAssetsBootloader(root, &bootloader.Options{
				Role:        bootloader.RoleRunMode,
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
				Role: bootloader.RoleRecovery,
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
		return o.observeUpdate(whichBootloader, isRecovery, root, relativeTarget, data)
	case gadget.ContentRollback:
		return o.observeRollback(whichBootloader, isRecovery, root, relativeTarget, data)
	default:
		// we only care about update and rollback actions
		return false, nil
	}
}

func (o *TrustedAssetsUpdateObserver) observeUpdate(bl bootloader.Bootloader, recovery bool, root, relativeTarget string, change *gadget.ContentChange) (bool, error) {
	modeenvBefore, err := o.modeenv.Copy()
	if err != nil {
		return false, fmt.Errorf("cannot copy modeenv: %v", err)
	}

	// we may be running after a mid-update reboot, where a successful boot
	// would have trimmed the tracked assets hash lists to contain only the
	// asset we booted with

	var taBefore *trackedAsset
	if change.Before != "" {
		// make sure that the original copy is present in the cache if
		// it existed
		taBefore, err = o.cache.Add(change.Before, bl.Name(), filepath.Base(relativeTarget))
		if err != nil {
			return false, err
		}
	}

	ta, err := o.cache.Add(change.After, bl.Name(), filepath.Base(relativeTarget))
	if err != nil {
		return false, err
	}

	trustedAssets := &o.modeenv.CurrentTrustedBootAssets
	changedAssets := &o.changedAssets
	if recovery {
		trustedAssets = &o.modeenv.CurrentTrustedRecoveryBootAssets
		changedAssets = &o.seedChangedAssets
	}
	// keep track of the change for cancellation purpose
	*changedAssets = append(*changedAssets, ta)

	if *trustedAssets == nil {
		*trustedAssets = bootAssetsMap{}
	}

	if taBefore != nil && !isAssetAlreadyTracked(*trustedAssets, taBefore) {
		// make sure that the boot asset that was was in the filesystem
		// before the update, is properly tracked until either a
		// successful boot or the update is canceled
		// the original asset hash is listed first
		(*trustedAssets)[taBefore.name] = append([]string{taBefore.hash}, (*trustedAssets)[taBefore.name]...)
	}

	if !isAssetAlreadyTracked(*trustedAssets, ta) {
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
	if err := o.modeenv.Write(); err != nil {
		return false, fmt.Errorf("cannot write modeeenv: %v", err)
	}
	return true, nil
}

func (o *TrustedAssetsUpdateObserver) observeRollback(bl bootloader.Bootloader, recovery bool, root, relativeTarget string, data *gadget.ContentChange) (bool, error) {
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

	if err := o.modeenv.Write(); err != nil {
		return false, fmt.Errorf("cannot write modeeenv: %v", err)
	}

	return false, nil
}

// BeforeWrite is called when the update process has been staged for execution.
func (o *TrustedAssetsUpdateObserver) BeforeWrite() error {
	if o.modeenv == nil {
		// modeenv wasn't even loaded yet, meaning none of the trusted
		// boot assets was updated
		return nil
	}
	if err := resealKeyToModeenv(dirs.GlobalRootDir, o.model, o.modeenv); err != nil {
		return err
	}
	return nil
}

func (o *TrustedAssetsUpdateObserver) canceledUpdate(recovery bool) {
	trustedAssets := &o.modeenv.CurrentTrustedBootAssets
	otherTrustedAssets := o.modeenv.CurrentTrustedRecoveryBootAssets
	changedAssets := o.changedAssets
	if recovery {
		trustedAssets = &o.modeenv.CurrentTrustedRecoveryBootAssets
		otherTrustedAssets = o.modeenv.CurrentTrustedBootAssets
		changedAssets = o.seedChangedAssets
	}

	if len(*trustedAssets) == 0 {
		return
	}

	for _, changed := range changedAssets {
		hashList, ok := (*trustedAssets)[changed.name]
		if !ok || len(hashList) == 0 {
			// not tracked already, nothing to do
			continue
		}
		if len(hashList) == 1 {
			currentAssetHash := hashList[0]
			if currentAssetHash != changed.hash {
				// assets list has already been trimmed, nothing
				// to do
				continue
			} else {
				// asset was newly added
				delete(*trustedAssets, changed.name)
			}
		} else {
			// asset updates were appended to the list
			(*trustedAssets)[changed.name] = hashList[:1]
		}
		if !isAssetHashTrackedInMap(otherTrustedAssets, changed.name, changed.hash) {
			// asset revision is not used used elsewhere, we can remove it from the cache
			if err := o.cache.Remove(changed.blName, changed.name, changed.hash); err != nil {
				logger.Noticef("cannot remove unused boot asset %v:%v: %v", changed.name, changed.hash, err)
			}
		}
	}
}

// Canceled is called when the update has been canceled, or if changes
// were written and the update has been reverted.
func (o *TrustedAssetsUpdateObserver) Canceled() error {
	if o.modeenv == nil {
		// modeenv wasn't even loaded yet, meaning none of the boot
		// assets was updated
		return nil
	}
	for _, isRecovery := range []bool{false, true} {
		o.canceledUpdate(isRecovery)
	}

	if err := o.modeenv.Write(); err != nil {
		return fmt.Errorf("cannot write modeeenv: %v", err)
	}

	if err := resealKeyToModeenv(dirs.GlobalRootDir, o.model, o.modeenv); err != nil {
		return fmt.Errorf("while canceling gadget update: %v", err)
	}
	return nil
}

func observeSuccessfulBootAssetsForBootloader(m *Modeenv, root string, opts *bootloader.Options) (drop []*trackedAsset, err error) {
	trustedAssetsMap := &m.CurrentTrustedBootAssets
	otherTrustedAssetsMap := m.CurrentTrustedRecoveryBootAssets
	whichBootloader := "run mode"
	if opts != nil && opts.Role == bootloader.RoleRecovery {
		trustedAssetsMap = &m.CurrentTrustedRecoveryBootAssets
		otherTrustedAssetsMap = m.CurrentTrustedBootAssets
		whichBootloader = "recovery"
	}

	if len(*trustedAssetsMap) == 0 {
		// bootloader may have trusted assets, but we are not tracking
		// any for the boot process
		return nil, nil
	}

	// let's find the bootloader first
	bl, trustedAssets, err := findMaybeTrustedAssetsBootloader(root, opts)
	if err != nil {
		return nil, err
	}
	if len(trustedAssets) == 0 {
		// not a trusted assets bootloader, nothing to do
		return nil, nil
	}

	cache := newTrustedAssetsCache(dirs.SnapBootAssetsDir)
	for _, trustedAsset := range trustedAssets {
		assetName := filepath.Base(trustedAsset)

		// find the hash of the file on disk
		assetHash, err := cache.fileHash(filepath.Join(root, trustedAsset))
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("cannot calculate the digest of existing trusted asset: %v", err)
		}
		if assetHash == "" {
			// no trusted asset on disk, but we booted nonetheless,
			// at least log something
			logger.Noticef("system booted without %v bootloader trusted asset %q", whichBootloader, trustedAsset)
			// given that asset names cannot be reused, clear the
			// boot assets map for the current bootloader
			delete(*trustedAssetsMap, assetName)
			continue
		}

		// this is what we booted with
		bootedWith := []string{assetHash}
		// one of these was expected during boot
		hashList := (*trustedAssetsMap)[assetName]

		assetFound := false
		// find out if anything needs to be dropped
		for _, hash := range hashList {
			if hash == assetHash {
				assetFound = true
				continue
			}
			if !isAssetHashTrackedInMap(otherTrustedAssetsMap, assetName, hash) {
				// asset can be dropped
				drop = append(drop, &trackedAsset{
					blName: bl.Name(),
					name:   assetName,
					hash:   hash,
				})
			}
		}

		if !assetFound {
			// unexpected, we have booted with an asset whose hash
			// is not listed among the ones we expect

			// TODO:UC20: try to restore the asset from cache
			return nil, fmt.Errorf("system booted with unexpected %v bootloader asset %q hash %v", whichBootloader, trustedAsset, assetHash)
		}

		// update the list of what we booted with
		(*trustedAssetsMap)[assetName] = bootedWith

	}
	return drop, nil
}

// observeSuccessfulBootAssets observes the state of the trusted boot assets
// after a successful boot. Returns a modified modeenv reflecting a new state,
// and a list of assets that can be dropped from the cache.
func observeSuccessfulBootAssets(m *Modeenv) (newM *Modeenv, drop []*trackedAsset, err error) {
	newM, err = m.Copy()
	if err != nil {
		return nil, nil, err
	}

	for _, bl := range []struct {
		root string
		opts *bootloader.Options
	}{
		{
			// ubuntu-boot bootloader
			root: InitramfsUbuntuBootDir,
			opts: &bootloader.Options{Role: bootloader.RoleRunMode, NoSlashBoot: true},
		}, {
			// ubuntu-seed bootloader
			root: InitramfsUbuntuSeedDir,
			opts: &bootloader.Options{Role: bootloader.RoleRecovery, NoSlashBoot: true},
		},
	} {
		dropForBootloader, err := observeSuccessfulBootAssetsForBootloader(newM, bl.root, bl.opts)
		if err != nil {
			return nil, nil, err
		}
		drop = append(drop, dropForBootloader...)
	}
	return newM, drop, nil
}
