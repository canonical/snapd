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
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
)

// TODO:UC20 add a doc comment when this is stabilized
type bootChain struct {
	BrandID        string      `json:"brand-id"`
	Model          string      `json:"model"`
	Grade          string      `json:"grade"`
	ModelSignKeyID string      `json:"model-sign-key-id"`
	AssetChain     []bootAsset `json:"asset-chain"`
	Kernel         string      `json:"kernel"`
	// KernelRevision is the revision of the kernel snap. It is empty if
	// kernel is unasserted, in which case always reseal.
	KernelRevision string   `json:"kernel-revision"`
	KernelCmdlines []string `json:"kernel-cmdlines"`

	model          *asserts.Model
	kernelBootFile bootloader.BootFile
}

// TODO:UC20 add a doc comment when this is stabilized
type bootAsset struct {
	Role   bootloader.Role `json:"role"`
	Name   string          `json:"name"`
	Hashes []string        `json:"hashes"`
}

func bootAssetLess(b, other *bootAsset) bool {
	byRole := b.Role < other.Role
	byName := b.Name < other.Name
	// sort order: role -> name -> hash list (len -> lexical)
	if b.Role != other.Role {
		return byRole
	}
	if b.Name != other.Name {
		return byName
	}
	return stringListsLess(b.Hashes, other.Hashes)
}

func stringListsEqual(sl1, sl2 []string) bool {
	if len(sl1) != len(sl2) {
		return false
	}
	for i := range sl1 {
		if sl1[i] != sl2[i] {
			return false
		}
	}
	return true
}

func stringListsLess(sl1, sl2 []string) bool {
	if len(sl1) != len(sl2) {
		return len(sl1) < len(sl2)
	}
	for idx := range sl1 {
		if sl1[idx] < sl2[idx] {
			return true
		}
	}
	return false
}

func toPredictableBootAsset(b *bootAsset) *bootAsset {
	if b == nil {
		return nil
	}
	newB := *b
	if b.Hashes != nil {
		newB.Hashes = make([]string, len(b.Hashes))
		copy(newB.Hashes, b.Hashes)
		sort.Strings(newB.Hashes)
	}
	return &newB
}

func toPredictableBootChain(b *bootChain) *bootChain {
	if b == nil {
		return nil
	}
	newB := *b
	if b.AssetChain != nil {
		newB.AssetChain = make([]bootAsset, len(b.AssetChain))
		for i := range b.AssetChain {
			newB.AssetChain[i] = *toPredictableBootAsset(&b.AssetChain[i])
		}
	}
	if b.KernelCmdlines != nil {
		newB.KernelCmdlines = make([]string, len(b.KernelCmdlines))
		copy(newB.KernelCmdlines, b.KernelCmdlines)
		sort.Strings(newB.KernelCmdlines)
	}
	return &newB
}

func predictableBootAssetsEqual(b1, b2 []bootAsset) bool {
	b1JSON, err := json.Marshal(b1)
	if err != nil {
		return false
	}
	b2JSON, err := json.Marshal(b2)
	if err != nil {
		return false
	}
	return bytes.Equal(b1JSON, b2JSON)
}

func predictableBootAssetsLess(b1, b2 []bootAsset) bool {
	if len(b1) != len(b2) {
		return len(b1) < len(b2)
	}
	for i := range b1 {
		if bootAssetLess(&b1[i], &b2[i]) {
			return true
		}
	}
	return false
}

type byBootChainOrder []bootChain

func (b byBootChainOrder) Len() int      { return len(b) }
func (b byBootChainOrder) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byBootChainOrder) Less(i, j int) bool {
	// sort by model info
	if b[i].BrandID != b[j].BrandID {
		return b[i].BrandID < b[j].BrandID
	}
	if b[i].Model != b[j].Model {
		return b[i].Model < b[j].Model
	}
	if b[i].Grade != b[j].Grade {
		return b[i].Grade < b[j].Grade
	}
	if b[i].ModelSignKeyID != b[j].ModelSignKeyID {
		return b[i].ModelSignKeyID < b[j].ModelSignKeyID
	}
	// then boot assets
	if !predictableBootAssetsEqual(b[i].AssetChain, b[j].AssetChain) {
		return predictableBootAssetsLess(b[i].AssetChain, b[j].AssetChain)
	}
	// then kernel
	if b[i].Kernel != b[j].Kernel {
		return b[i].Kernel < b[j].Kernel
	}
	if b[i].KernelRevision != b[j].KernelRevision {
		return b[i].KernelRevision < b[j].KernelRevision
	}
	// and last kernel command lines
	if !stringListsEqual(b[i].KernelCmdlines, b[j].KernelCmdlines) {
		return stringListsLess(b[i].KernelCmdlines, b[j].KernelCmdlines)
	}
	return false
}

type predictableBootChains []bootChain

func toPredictableBootChains(chains []bootChain) predictableBootChains {
	if chains == nil {
		return nil
	}
	predictableChains := make([]bootChain, len(chains))
	for i := range chains {
		predictableChains[i] = *toPredictableBootChain(&chains[i])
	}
	sort.Sort(byBootChainOrder(predictableChains))
	return predictableChains
}

// predictableBootChainsEqualForReseal returns true when boot chains are
// equivalent for reseal.
func predictableBootChainsEqualForReseal(pb1, pb2 predictableBootChains) bool {
	pb1JSON, err := json.Marshal(pb1)
	if err != nil {
		return false
	}
	pb2JSON, err := json.Marshal(pb2)
	if err != nil {
		return false
	}
	// TODO:UC20: return false if either chains have unasserted kernels
	return bytes.Equal(pb1JSON, pb2JSON)
}

// bootAssetsToLoadChains generates a list of load chains covering given boot
// assets sequence. At the end of each chain, adds an entry for the kernel boot
// file.
func bootAssetsToLoadChains(assets []bootAsset, kernelBootFile bootloader.BootFile, roleToBlName map[bootloader.Role]string) ([]*secboot.LoadChain, error) {
	// kernel is added after all the assets
	addKernelBootFile := len(assets) == 0
	if addKernelBootFile {
		return []*secboot.LoadChain{secboot.NewLoadChain(kernelBootFile)}, nil
	}

	thisAsset := assets[0]
	blName := roleToBlName[thisAsset.Role]
	if blName == "" {
		return nil, fmt.Errorf("internal error: no bootloader name for boot asset role %q", thisAsset.Role)
	}
	var chains []*secboot.LoadChain
	for _, hash := range thisAsset.Hashes {
		var bf bootloader.BootFile
		var next []*secboot.LoadChain
		var err error

		p := filepath.Join(
			dirs.SnapBootAssetsDir,
			trustedAssetCacheRelPath(blName, thisAsset.Name, hash))
		if !osutil.FileExists(p) {
			return nil, fmt.Errorf("file %s not found in boot assets cache", p)
		}
		bf = bootloader.NewBootFile(
			"", // asset comes from the filesystem, not a snap
			p,
			thisAsset.Role,
		)
		next, err = bootAssetsToLoadChains(assets[1:], kernelBootFile, roleToBlName)
		if err != nil {
			return nil, err
		}
		chains = append(chains, secboot.NewLoadChain(bf, next...))
	}
	return chains, nil
}
