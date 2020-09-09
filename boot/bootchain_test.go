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

package boot_test

import (
	"encoding/json"
	"sort"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/testutil"
)

type bootchainSuite struct {
	testutil.BaseTest
}

var _ = Suite(&bootchainSuite{})

func (s *sealSuite) TestBootAssetsSort(c *C) {
	// by role
	d := []boot.BootAsset{
		{Role: "run", Name: "1ist", Hashes: []string{"b", "c"}},
		{Role: "recovery", Name: "1ist", Hashes: []string{"b", "c"}},
	}
	sort.Sort(boot.ByBootAssetOrder(d))
	c.Check(d, DeepEquals, []boot.BootAsset{
		{Role: "recovery", Name: "1ist", Hashes: []string{"b", "c"}},
		{Role: "run", Name: "1ist", Hashes: []string{"b", "c"}},
	})

	// by name
	d = []boot.BootAsset{
		{Role: "recovery", Name: "shim", Hashes: []string{"d", "e"}},
		{Role: "recovery", Name: "loader", Hashes: []string{"d", "e"}},
	}
	sort.Sort(boot.ByBootAssetOrder(d))
	c.Check(d, DeepEquals, []boot.BootAsset{
		{Role: "recovery", Name: "loader", Hashes: []string{"d", "e"}},
		{Role: "recovery", Name: "shim", Hashes: []string{"d", "e"}},
	})

	// by hash list length
	d = []boot.BootAsset{
		{Role: "run", Name: "1ist", Hashes: []string{"a", "f"}},
		{Role: "run", Name: "1ist", Hashes: []string{"d"}},
	}
	sort.Sort(boot.ByBootAssetOrder(d))
	c.Check(d, DeepEquals, []boot.BootAsset{
		{Role: "run", Name: "1ist", Hashes: []string{"d"}},
		{Role: "run", Name: "1ist", Hashes: []string{"a", "f"}},
	})

	// hash list entries
	d = []boot.BootAsset{
		{Role: "run", Name: "1ist", Hashes: []string{"b", "d"}},
		{Role: "run", Name: "1ist", Hashes: []string{"b", "c"}},
	}
	sort.Sort(boot.ByBootAssetOrder(d))
	c.Check(d, DeepEquals, []boot.BootAsset{
		{Role: "run", Name: "1ist", Hashes: []string{"b", "c"}},
		{Role: "run", Name: "1ist", Hashes: []string{"b", "d"}},
	})

	d = []boot.BootAsset{
		{Role: "run", Name: "loader", Hashes: []string{"z"}},
		{Role: "recovery", Name: "shim", Hashes: []string{"b"}},
		{Role: "run", Name: "loader", Hashes: []string{"c", "d"}},
		{Role: "run", Name: "1oader", Hashes: []string{"d", "e"}},
		{Role: "recovery", Name: "loader", Hashes: []string{"d", "e"}},
		{Role: "run", Name: "0oader", Hashes: []string{"x", "z"}},
	}
	sort.Sort(boot.ByBootAssetOrder(d))
	c.Check(d, DeepEquals, []boot.BootAsset{
		{Role: "recovery", Name: "loader", Hashes: []string{"d", "e"}},
		{Role: "recovery", Name: "shim", Hashes: []string{"b"}},
		{Role: "run", Name: "0oader", Hashes: []string{"x", "z"}},
		{Role: "run", Name: "1oader", Hashes: []string{"d", "e"}},
		{Role: "run", Name: "loader", Hashes: []string{"z"}},
		{Role: "run", Name: "loader", Hashes: []string{"c", "d"}},
	})

	// d is already sorted, sort it again
	sort.Sort(boot.ByBootAssetOrder(d))
	// still the same
	c.Check(d, DeepEquals, []boot.BootAsset{
		{Role: "recovery", Name: "loader", Hashes: []string{"d", "e"}},
		{Role: "recovery", Name: "shim", Hashes: []string{"b"}},
		{Role: "run", Name: "0oader", Hashes: []string{"x", "z"}},
		{Role: "run", Name: "1oader", Hashes: []string{"d", "e"}},
		{Role: "run", Name: "loader", Hashes: []string{"z"}},
		{Role: "run", Name: "loader", Hashes: []string{"c", "d"}},
	})

	// 2 identical entries
	d = []boot.BootAsset{
		{Role: "run", Name: "loader", Hashes: []string{"x", "z"}},
		{Role: "run", Name: "loader", Hashes: []string{"x", "z"}},
	}
	sort.Sort(boot.ByBootAssetOrder(d))
	c.Check(d, DeepEquals, []boot.BootAsset{
		{Role: "run", Name: "loader", Hashes: []string{"x", "z"}},
		{Role: "run", Name: "loader", Hashes: []string{"x", "z"}},
	})

}

func (s *sealSuite) TestBootAssetsPredictable(c *C) {
	// by role
	ba := boot.BootAsset{
		Role: "run", Name: "list", Hashes: []string{"b", "a"},
	}
	pred := boot.ToPredictableBootAsset(&ba)
	c.Check(pred, DeepEquals, &boot.BootAsset{
		Role: "run", Name: "list", Hashes: []string{"a", "b"},
	})
	// original structure is not changed
	c.Check(ba, DeepEquals, boot.BootAsset{
		Role: "run", Name: "list", Hashes: []string{"b", "a"},
	})

	// try to make a predictable struct predictable once more
	predAgain := boot.ToPredictableBootAsset(pred)
	c.Check(predAgain, DeepEquals, pred)

	baNil := boot.ToPredictableBootAsset(nil)
	c.Check(baNil, IsNil)
}

func (s *sealSuite) TestBootChainMarshalOnlyAssets(c *C) {
	pbNil := boot.ToPredictableBootChain(nil)
	c.Check(pbNil, IsNil)

	bc := &boot.BootChain{
		AssetChain: []boot.BootAsset{
			{Role: "run", Name: "loader", Hashes: []string{"z"}},
			{Role: "recovery", Name: "shim", Hashes: []string{"b"}},
			{Role: "run", Name: "loader", Hashes: []string{"d", "c"}},
			{Role: "run", Name: "1oader", Hashes: []string{"e", "d"}},
			{Role: "recovery", Name: "loader", Hashes: []string{"e", "d"}},
			{Role: "run", Name: "0oader", Hashes: []string{"z", "x"}},
		},
	}

	predictableBc := boot.ToPredictableBootChain(bc)

	c.Check(predictableBc, DeepEquals, &boot.BootChain{
		// assets are sorted
		AssetChain: []boot.BootAsset{
			// hash lists are sorted
			{Role: "recovery", Name: "loader", Hashes: []string{"d", "e"}},
			{Role: "recovery", Name: "shim", Hashes: []string{"b"}},
			{Role: "run", Name: "0oader", Hashes: []string{"x", "z"}},
			{Role: "run", Name: "1oader", Hashes: []string{"d", "e"}},
			{Role: "run", Name: "loader", Hashes: []string{"z"}},
			{Role: "run", Name: "loader", Hashes: []string{"c", "d"}},
		},
	})

	d, err := json.Marshal(predictableBc)
	c.Assert(err, IsNil)
	c.Check(string(d), Equals, `{"brand-id":"","model":"","grade":"","model-sign-key-id":"","asset-chain":[{"role":"recovery","name":"loader","hashes":["d","e"]},{"role":"recovery","name":"shim","hashes":["b"]},{"role":"run","name":"0oader","hashes":["x","z"]},{"role":"run","name":"1oader","hashes":["d","e"]},{"role":"run","name":"loader","hashes":["z"]},{"role":"run","name":"loader","hashes":["c","d"]}],"kernel":"","kernel-revision":"","kernel-cmdlines":null}`)

	// already predictable, but try again
	alreadySortedBc := boot.ToPredictableBootChain(predictableBc)
	c.Check(alreadySortedBc, DeepEquals, predictableBc)

	// boot chain with 2 identical assets
	bcIdenticalAssets := &boot.BootChain{
		AssetChain: []boot.BootAsset{
			{Role: "run", Name: "loader", Hashes: []string{"z"}},
			{Role: "run", Name: "loader", Hashes: []string{"z"}},
		},
	}
	sortedBcIdentical := boot.ToPredictableBootChain(bcIdenticalAssets)
	c.Check(sortedBcIdentical, DeepEquals, bcIdenticalAssets)
}

func (s *sealSuite) TestBootChainMarshalFull(c *C) {
	bc := &boot.BootChain{
		BrandID:        "mybrand",
		Model:          "foo",
		Grade:          "dangerous",
		ModelSignKeyID: "my-key-id",
		// asset chain will get sorted when marshaling
		AssetChain: []boot.BootAsset{
			{Role: "run", Name: "loader", Hashes: []string{"c", "d"}},
			// hash list will get sorted
			{Role: "recovery", Name: "shim", Hashes: []string{"b", "a"}},
			{Role: "recovery", Name: "loader", Hashes: []string{"d"}},
		},
		Kernel:         "pc-kernel",
		KernelRevision: "1234",
		KernelCmdlines: []string{`foo=bar baz=0x123`, `a=1`},
	}

	uc20model := makeMockUC20Model()
	bc.SetModel(uc20model)
	kernelBootFile := bootloader.NewBootFile("pc-kernel", "/foo", "recovery")
	bc.SetKernelBootFile(kernelBootFile)

	expectedPredictableBc := &boot.BootChain{
		BrandID:        "mybrand",
		Model:          "foo",
		Grade:          "dangerous",
		ModelSignKeyID: "my-key-id",
		// assets are sorted
		AssetChain: []boot.BootAsset{
			{Role: "recovery", Name: "loader", Hashes: []string{"d"}},
			// hash lists are sorted
			{Role: "recovery", Name: "shim", Hashes: []string{"a", "b"}},
			{Role: "run", Name: "loader", Hashes: []string{"c", "d"}},
		},
		Kernel:         "pc-kernel",
		KernelRevision: "1234",
		KernelCmdlines: []string{`a=1`, `foo=bar baz=0x123`},
	}
	// those can't be set directly, but are copied as well
	expectedPredictableBc.SetModel(uc20model)
	expectedPredictableBc.SetKernelBootFile(kernelBootFile)

	predictableBc := boot.ToPredictableBootChain(bc)
	c.Check(predictableBc, DeepEquals, expectedPredictableBc)

	d, err := json.Marshal(predictableBc)
	c.Assert(err, IsNil)
	c.Check(string(d), Equals, `{"brand-id":"mybrand","model":"foo","grade":"dangerous","model-sign-key-id":"my-key-id","asset-chain":[{"role":"recovery","name":"loader","hashes":["d"]},{"role":"recovery","name":"shim","hashes":["a","b"]},{"role":"run","name":"loader","hashes":["c","d"]}],"kernel":"pc-kernel","kernel-revision":"1234","kernel-cmdlines":["a=1","foo=bar baz=0x123"]}`)

	expectedOriginal := &boot.BootChain{
		BrandID:        "mybrand",
		Model:          "foo",
		Grade:          "dangerous",
		ModelSignKeyID: "my-key-id",
		// asset chain will get sorted when marshaling
		AssetChain: []boot.BootAsset{
			{Role: "run", Name: "loader", Hashes: []string{"c", "d"}},
			// hash list will get sorted
			{Role: "recovery", Name: "shim", Hashes: []string{"b", "a"}},
			{Role: "recovery", Name: "loader", Hashes: []string{"d"}},
		},
		Kernel:         "pc-kernel",
		KernelRevision: "1234",
		KernelCmdlines: []string{`foo=bar baz=0x123`, `a=1`},
	}
	expectedOriginal.SetModel(uc20model)
	expectedOriginal.SetKernelBootFile(kernelBootFile)
	// original structure has not been modified
	c.Check(bc, DeepEquals, expectedOriginal)
}

func (s *sealSuite) TestBootChainEqualForResealComplex(c *C) {
	bc := []boot.BootChain{
		{
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				{Role: "run", Name: "loader", Hashes: []string{"c", "d"}},
				// hash list will get sorted
				{Role: "recovery", Name: "shim", Hashes: []string{"b", "a"}},
				{Role: "recovery", Name: "loader", Hashes: []string{"d"}},
			},
			Kernel:         "pc-kernel",
			KernelRevision: "1234",
			KernelCmdlines: []string{`foo=bar baz=0x123`},
		},
	}
	pb := boot.ToPredictableBootChains(bc)
	// sorted variant
	pbOther := boot.PredictableBootChains{
		{
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				{Role: "recovery", Name: "loader", Hashes: []string{"d"}},
				{Role: "recovery", Name: "shim", Hashes: []string{"a", "b"}},
				{Role: "run", Name: "loader", Hashes: []string{"c", "d"}},
			},
			Kernel:         "pc-kernel",
			KernelRevision: "1234",
			KernelCmdlines: []string{`foo=bar baz=0x123`},
		},
	}
	eq := boot.PredictableBootChainsEqualForReseal(pb, pbOther)
	c.Check(eq, Equals, true, Commentf("not equal\none: %v\nother: %v", pb, pbOther))
}

func (s *sealSuite) TestPredictableBootChainsEqualForResealSimple(c *C) {
	var pbNil boot.PredictableBootChains

	c.Check(boot.PredictableBootChainsEqualForReseal(pbNil, pbNil), Equals, true)

	bcJustOne := []boot.BootChain{
		{
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				{Role: "run", Name: "loader", Hashes: []string{"c", "d"}},
			},
			Kernel:         "pc-kernel-other",
			KernelRevision: "1234",
			KernelCmdlines: []string{`foo`},
		},
	}
	pbJustOne := boot.ToPredictableBootChains(bcJustOne)
	// equal with self
	c.Check(boot.PredictableBootChainsEqualForReseal(pbJustOne, pbJustOne), Equals, true)

	// equal with nil?
	c.Check(boot.PredictableBootChainsEqualForReseal(pbJustOne, pbNil), Equals, false)

	bcMoreAssets := []boot.BootChain{
		{
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				{Role: "run", Name: "loader", Hashes: []string{"c", "d"}},
			},
			Kernel:         "pc-kernel-other",
			KernelRevision: "1234",
			KernelCmdlines: []string{`foo`},
		}, {
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				{Role: "run", Name: "loader", Hashes: []string{"d", "e"}},
			},
			Kernel:         "pc-kernel-other",
			KernelRevision: "1234",
			KernelCmdlines: []string{`foo`},
		},
	}

	pbMoreAssets := boot.ToPredictableBootChains(bcMoreAssets)

	c.Check(boot.PredictableBootChainsEqualForReseal(pbMoreAssets, pbJustOne), Equals, false)
	// with self
	c.Check(boot.PredictableBootChainsEqualForReseal(pbMoreAssets, pbMoreAssets), Equals, true)
}

func (s *sealSuite) TestPredictableBootChainsFullMarshal(c *C) {
	// chains will be sorted
	chains := []boot.BootChain{
		{
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "signed",
			ModelSignKeyID: "my-key-id",
			// assets will be sorted
			AssetChain: []boot.BootAsset{
				// hashes will be sorted
				{Role: "recovery", Name: "shim", Hashes: []string{"x", "y"}},
				{Role: "recovery", Name: "loader", Hashes: []string{"c", "d"}},
				{Role: "run", Name: "loader", Hashes: []string{"z", "x"}},
			},
			Kernel:         "pc-kernel-other",
			KernelRevision: "2345",
			KernelCmdlines: []string{`snapd_recovery_mode=run foo`},
		}, {
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			// assets will be sorted
			AssetChain: []boot.BootAsset{
				// hashes will be sorted
				{Role: "recovery", Name: "shim", Hashes: []string{"y", "x"}},
				{Role: "recovery", Name: "loader", Hashes: []string{"c", "d"}},
				{Role: "run", Name: "loader", Hashes: []string{"b", "a"}},
			},
			Kernel:         "pc-kernel-other",
			KernelRevision: "1234",
			KernelCmdlines: []string{`snapd_recovery_mode=run foo`},
		}, {
			// recovery system
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			// will be sorted
			AssetChain: []boot.BootAsset{
				{Role: "recovery", Name: "shim", Hashes: []string{"y", "x"}},
				{Role: "recovery", Name: "loader", Hashes: []string{"c", "d"}},
			},
			Kernel:         "pc-kernel-other",
			KernelRevision: "12",
			KernelCmdlines: []string{
				// will be sorted
				`snapd_recovery_mode=recover snapd_recovery_system=23 foo`,
				`snapd_recovery_mode=recover snapd_recovery_system=12 foo`,
			},
		},
	}

	predictableChains := boot.ToPredictableBootChains(chains)
	d, err := json.Marshal(predictableChains)
	c.Assert(err, IsNil)

	var data []map[string]interface{}
	err = json.Unmarshal(d, &data)
	c.Assert(err, IsNil)
	c.Check(data, DeepEquals, []map[string]interface{}{
		{
			"model":             "foo",
			"brand-id":          "mybrand",
			"grade":             "dangerous",
			"model-sign-key-id": "my-key-id",
			"kernel":            "pc-kernel-other",
			"kernel-revision":   "12",
			"kernel-cmdlines": []interface{}{
				`snapd_recovery_mode=recover snapd_recovery_system=12 foo`,
				`snapd_recovery_mode=recover snapd_recovery_system=23 foo`,
			},
			"asset-chain": []interface{}{
				map[string]interface{}{"role": "recovery", "name": "loader", "hashes": []interface{}{"c", "d"}},
				map[string]interface{}{"role": "recovery", "name": "shim", "hashes": []interface{}{"x", "y"}},
			},
		}, {
			"model":             "foo",
			"brand-id":          "mybrand",
			"grade":             "dangerous",
			"model-sign-key-id": "my-key-id",
			"kernel":            "pc-kernel-other",
			"kernel-revision":   "1234",
			"kernel-cmdlines":   []interface{}{"snapd_recovery_mode=run foo"},
			"asset-chain": []interface{}{
				map[string]interface{}{"role": "recovery", "name": "loader", "hashes": []interface{}{"c", "d"}},
				map[string]interface{}{"role": "recovery", "name": "shim", "hashes": []interface{}{"x", "y"}},
				map[string]interface{}{"role": "run", "name": "loader", "hashes": []interface{}{"a", "b"}},
			},
		}, {
			"model":             "foo",
			"brand-id":          "mybrand",
			"grade":             "signed",
			"model-sign-key-id": "my-key-id",
			"kernel":            "pc-kernel-other",
			"kernel-revision":   "2345",
			"kernel-cmdlines":   []interface{}{"snapd_recovery_mode=run foo"},
			"asset-chain": []interface{}{
				map[string]interface{}{"role": "recovery", "name": "loader", "hashes": []interface{}{"c", "d"}},
				map[string]interface{}{"role": "recovery", "name": "shim", "hashes": []interface{}{"x", "y"}},
				map[string]interface{}{"role": "run", "name": "loader", "hashes": []interface{}{"x", "z"}},
			},
		},
	})
}

func (s *sealSuite) TestPredictableBootChainsFields(c *C) {
	chainsNil := boot.ToPredictableBootChains(nil)
	c.Check(chainsNil, IsNil)

	justOne := []boot.BootChain{
		{
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "signed",
			ModelSignKeyID: "my-key-id",
			Kernel:         "pc-kernel-other",
			KernelRevision: "2345",
			KernelCmdlines: []string{`foo`},
		},
	}
	predictableJustOne := boot.ToPredictableBootChains(justOne)
	c.Check(predictableJustOne, DeepEquals, boot.PredictableBootChains(justOne))

	chainsGrade := []boot.BootChain{
		{
			Grade: "signed",
		}, {
			Grade: "dangerous",
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsGrade), DeepEquals, boot.PredictableBootChains{
		{
			Grade: "dangerous",
		}, {
			Grade: "signed",
		},
	})

	chainsKernel := []boot.BootChain{
		{
			Grade:  "dangerous",
			Kernel: "foo",
		}, {
			Grade:  "dangerous",
			Kernel: "bar",
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsKernel), DeepEquals, boot.PredictableBootChains{
		{
			Grade:  "dangerous",
			Kernel: "bar",
		}, {
			Grade:  "dangerous",
			Kernel: "foo",
		},
	})

	chainsCmdline := []boot.BootChain{
		{
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`a`},
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsCmdline), DeepEquals, boot.PredictableBootChains{
		{
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`a`},
		}, {
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	})

	chainsModel := []boot.BootChain{
		{
			Model:          "fridge",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsModel), DeepEquals, boot.PredictableBootChains{
		{
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			Model:          "fridge",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	})

	chainsBrand := []boot.BootChain{
		{
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			BrandID:        "acme",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsBrand), DeepEquals, boot.PredictableBootChains{
		{
			BrandID:        "acme",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	})

	chainsKeyID := []boot.BootChain{
		{
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
			ModelSignKeyID: "key-2",
		}, {
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
			ModelSignKeyID: "key-1",
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsKeyID), DeepEquals, boot.PredictableBootChains{
		{
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
			ModelSignKeyID: "key-1",
		}, {
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
			ModelSignKeyID: "key-2",
		},
	})

	chainsAssets := []boot.BootChain{
		{
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			ModelSignKeyID: "key-1",
			AssetChain: []boot.BootAsset{
				// will be sorted
				{Hashes: []string{"b", "a"}},
			},
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			ModelSignKeyID: "key-1",
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"b"}},
			},
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsAssets), DeepEquals, boot.PredictableBootChains{
		{
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			ModelSignKeyID: "key-1",
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"b"}},
			},
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			ModelSignKeyID: "key-1",
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"a", "b"}},
			},
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	})

	chainsFewerAssets := []boot.BootChain{
		{
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"b", "a"}},
				{Hashes: []string{"c", "d"}},
			},
		}, {
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"b"}},
			},
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsFewerAssets), DeepEquals, boot.PredictableBootChains{
		{
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"b"}},
			},
		}, {
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"a", "b"}},
				{Hashes: []string{"c", "d"}},
			},
		},
	})

	// not confused if 2 chains are identical
	chainsIdenticalAssets := []boot.BootChain{
		{
			BrandID:        "foo",
			Model:          "box",
			ModelSignKeyID: "key-1",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"a", "b"}},
				{Name: "asset", Hashes: []string{"a", "b"}},
			},
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			ModelSignKeyID: "key-1",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"a", "b"}},
				{Name: "asset", Hashes: []string{"a", "b"}},
			},
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsIdenticalAssets), DeepEquals, boot.PredictableBootChains(chainsIdenticalAssets))
}

func (s *sealSuite) TestPredictableBootChainsSortOrder(c *C) {
	// check that sort order is model info, assets, kernel, kernel cmdline

	chains := []boot.BootChain{
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1", "cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1", "cm=2"},
		},
	}
	predictable := boot.ToPredictableBootChains(chains)
	c.Check(predictable, DeepEquals, boot.PredictableBootChains{
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1", "cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1", "cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
	})
}
