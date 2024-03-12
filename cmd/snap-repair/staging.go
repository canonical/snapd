// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build withtestkeys || withstagingkeys

/*
 * Copyright (C) 2017-2020 Canonical Ltd
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

package main

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snapdenv"
)

const (
	encodedStagingRepairRootAccountKey = `type: account-key
authority-id: canonical
public-key-sha3-384: 0GgXgD-RtfU0HJFBmaaiUQaNUFATl1oOlzJ44Bi4MbQAwcu8ektQLGBkKQ4JuA_O
account-id: canonical
name: repair-root
since: 2017-07-07T00:00:00.0Z
body-length: 1406
sign-key-sha3-384: e2r8on4LgdxQSaW5T8mBD5oC2fSTktTVxOYa5w3kIP4_nNF6L7mt6fOJShdOGkKu

AcbDTQRWhcGAASAA0D+AqdqBtgiaABSZ++NdDKcvFtpmEphDfpEAAo4MAWucmE5yVN9mHFRXvwU0
ZkLQwPuiF5Vp5EP/kHyKgmmF9nKUBXnuZXfuH4vzH9ZuEfdxGc0On+XK4KwyPUzOXj2n1Rsxj0P5
06wJ6QFghi6nORx3Hz4pxZH7SRANgZudwWE53+whbkJyU/psv4RXfxPnu3YGo0qPk/wGCfV8kkkH
UFmeqJJEk14EGI+Kv9WlIVAqctryqf9mSXkgnhp4lzdGpsRCmRcw7boVjdOieFCJv+gVs7i52Yxu
YyiA09XF8j85DSMl4s/TyN4bm9649beBCpTJNyb3Ep6lydGiZck8ChJRLDXGP2XZtRKXsBMNIfwP
5qnLtX1/Sm1znag0grGbd3AUqL6ySAr42x8QIxZfqzk5DvQbF3xOiu2xzxTt1eB3069MlnFw99ui
fLwlec7imbCiX3bryutCRhKgkJz4MbNsyHiW51k1l1IDbABey+gfVHpeBq/2aHK5qSy98dRBSYSj
Ki++j8zR1ODequWy+OrF4cu6IQ35eunHQ2mRsJIE0xFGAjG3vCPJwzVoNS5m5R0ffncIUYdKxt/R
W2mLo43qX0fquW5LvHyI14d3B3LYfKz05FmASJaE4A+/GQhM7kMCnmykro0MM6MU0sd2OruOZVVo
z6GQ37Hyo/TGToyCr7qD/+tSq3dKYGyezl/Y4I589eqnc1DaMHL2ssiXDsbpSRHpnNqMUq6UNg4M
NsUiDLaGsNJj1ft6A7jz+yoqJ74m3hlaQK1Rot8FXBkuJGoRKBahHbh/bfkGWChDvzY9ZpoUExY2
rp7tNYEP/LEAI/RQd03sBnqd3V8YhggT2n6QCC4ikLKvUTE3RY0qn5aAa7KC1wVi+7SfdeVl3RBF
Jyb9GCYfRUv/bfFH/TCZ9WVN1v/GIMcjBFGJf7H3cz/deela53XSaecYHuvFRpVfzmx28UcR8UY4
5WqHfxnVQlY6DPv+kjzMzIEJGwgSAFc0d4wlSwS/Y1T0ednFRUyjMAxEUvE8tOLibtXw4q/srIFt
OIgpd/xErcyi5Ddgt7EQoYo+rtVZ8x5EwR0+i7VAV+a3bnGSJW2LFEjt2RZUiMjohVZ4oOVuoDd2
VQzMFv41flbyqjgHhtJSCIOKDg9uI2FHbQ5vrX9qBooS68YkBALwCq+P7nSxDxFuS0CgrzSH35FX
VneOl68U74pxRgdlPJ0HI92oilrbTH8Ft0m5SzNsy+9ZZZtIDFQW+lx/ApixyifARFnZ3C3Gdx59
FlFNbE75+X28joGtul2mPjJ1eI1dCwiFCF3R/rwfRmw3Wpv76re+EzVR1MJVCcTgC1lUoCJpKl1J
n3PQLcR8J0iqswARAQAB

AcLBXAQAAQoABgUCWYM7bQAKCRAHKljtl9kuLtCFD/4miBm0HyLE8GdboeUtWw+oOlH0AgabRqYi
a1TpEJeYQIjnwDuCCPYtJxL1Rc+5dSNnbY9L+34NuaSyYMJY/FMuSS5iaNomGnj7YiAOds1+1/6h
Z1bTm3ttZnphg5DxckYZLaKoYgRaOzAbiRM8l+2bDbXlq3KRxZ7o7D1V/xpPis8SWK57gQ7VppHI
fcw5jnzWokWSowaKShimjJNCXMaeGdGJBLU1wcJC/XRf3tXSZecwMfL9CN/G8b17HvIFN/Pe3oS9
QxYMQ0p3J3PF3F19Iow0VHi78hPKtVmJb5igwzBlGYFW7zZ3R35nJ7Iv6VW58G2HDDGMdBfZp930
FbLb3mj8Yw3S5fcMZ09vpT7PK0tjFoVJtDFBOkrjvxVMEPRa0IJNcfl/hgPdp1/IFXWpZhfvk8a8
qgzffxN+Ro/J4Jt9QrHM4sNwiEOjVvHY4cQ9GOfns9UqocmxYPDxElBNraCFOCSudZgXiyF7zUYF
OnYqTDR4ChiZtmUqIiZr6rXgZTm1raGlqR7nsbDlkJtru7tzkgMRw8xFRolaQIKiyAwTewF7vLho
imwYTRuYRMzft1q5EeRWR4XwtlIuqsXg3FCGTNIG4HiAFKrrNV7AOvVjIUSgpOcWv2leSiRQjgpY
I9oD82ii+5rKvebnGIa0o+sWhYNFoviP/49DnDNJWA==
`
)

func init() {
	repairRootAccountKey, err := asserts.Decode([]byte(encodedStagingRepairRootAccountKey))
	if err != nil {
		panic(fmt.Sprintf("cannot decode trusted account-key: %v", err))
	}
	if snapdenv.UseStagingStore() {
		trustedRepairRootKeys = append(trustedRepairRootKeys, repairRootAccountKey.(*asserts.AccountKey))
	}
}
