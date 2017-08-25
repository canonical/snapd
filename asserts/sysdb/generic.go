// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package sysdb

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/osutil"
)

const (
	encodedGenericAccount = `type: account
authority-id: canonical
account-id: generic
display-name: Generic
timestamp: 2017-07-27T00:00:00.0Z
username: generic
validation: certified
sign-key-sha3-384: -CvQKAwRQ5h3Ffn10FILJoEZUXOv6km9FwA80-Rcj-f-6jadQ89VRswHNiEB9Lxk

AcLDXAQAAQoABgUCWYuVIgAKCRDUpVvql9g3II66IACcoxSoX8+PQLa9TNuNBUs3bdTW6V5ZOdE8
vnziIg+yqu3qYfWHcRf1qu7K9Igv5lH3uM5jh2AHlndaoX4Qg1Rm9rOZCkRr1dDUmdRDBXN2pdTA
oydd0Ivpeai4ATbSZs11h50/vN/mxBwM6TzdGHqRNt6lvygAPe7VtfchSW/J0NsSIHr9SUeuIHkJ
C79DV27B+9/m8pnpKJo/Fv8nKGs4sMduKVjrj9Po3UhpZEQWf3I3SeDI5IE4TgoDe+O7neGUtT6W
D9wnMWLphC+rHbJguxXG/fmnUYiM2U8o4WVrs/fjF0zDRH7rY3tbLPbFXf2OD4qfOvS//VLQWeCK
KAgKhwz0d5CqaHyKSplywSvwO/dxlrqOjt39k3EjYxVuNS5UQk/BzPoDZD5maisCFm9JZwqBlWHP
6XTj8rhHSkNAPXezs2ZpVSsdtNYmpLLzWIFsAviuoMjYYDyL6jZrD4RBNrNOvSNQGLezB+eyI5DW
9vr2ppCw8zr49epPvJ4uqj/AILgr52zworl7v/27X67BOSoRMmE4AOnvjSJ8cN6Yt83AuEI4aZbP
DlF2Znqp8o/srtmJ3ZMpsjIsAqVhCeTU6eWXbYfNUlIMSmC6CDwQQzsukU4M6NEwUQbWddiM3iNL
FdeFsBscXg4Qm/0Y3PULriDoct+VpBUhzwVXG+Lj6rjtcX7n1C/7u9i/+WIBJ7jU4FBjwOdgpSCQ
DSCb0PgTM2PfbScFpn3KVYs0kT/Jc40Lpw6CUG9iUIdz5qlJzhbRiuhU8yjEg9q/5lWizAuxcP+P
anNhmNXsme46IJh7WnlzPAVMsToz8bWY01LC3t33pPGlRJo109PMbNK7reMIb4KFiL4Hy7gVmTj9
uydReVBUTZuMLRq1ShAJNScZ+HTpWruLoiC87Rf1++1KakahmtWYCdlJv/JSOyjSh8D9h0GEmqON
lKmzrNgQS8QhLh5uBcITN2Kt1UFGu2o9I8l0TgD5Uh9fG/R/A536fpcvIzOA/lhVttO6P9POwUVv
RIBZ3TpVOSzQ+ADpDexRUouPLPkgUwVBSctcgafMaj/w8DsULvlOYd3Sqq9a+zg6bZr9oPBKutUn
YkIUWmLW1OsdQ2eBS9dFqzJTAOELxNOUq37UGnIrMbk3Vn8hLK+S/+W9XL6WVxzmL1PT9FJZZ41p
KdaFV+mvrTfyoxuzXxkWbMwQkc56Ifn+IojbDwMI4FcTcl4dOeUrlnqwBJmTTwEhLVkYDvzYsVV9
4joFUWhp10JMm3lO+3596m0kYWMhyvGfYnH7QcQ3GtMAz82yRHc1X+seeWyD/aIjlHYNYfaJ5Ogs
VC76lXi7swMtA9jV5FJIGmQufLo9f93NSYxqwpa8
`
)

var (
	genericAssertions        []asserts.Assertion
	genericStagingAssertions []asserts.Assertion
	genericExtraAssertions   []asserts.Assertion
)

func init() {
	genericAccount, err := asserts.Decode([]byte(encodedGenericAccount))
	if err != nil {
		panic(fmt.Sprintf("cannot decode 'generic' account: %v", err))
	}
	genericAssertions = []asserts.Assertion{genericAccount}
}

// Generic returns a copy of the current set of predefined assertions for the 'generic' authority as used by Open.
func Generic() []asserts.Assertion {
	generic := []asserts.Assertion(nil)
	if !osutil.GetenvBool("SNAPPY_USE_STAGING_STORE") {
		generic = append(generic, genericAssertions...)
	} else {
		generic = append(generic, genericStagingAssertions...)
	}
	generic = append(generic, genericExtraAssertions...)
	return generic
}

// InjectGeneric injects further predefined assertions into the set used Open.
// Returns a restore function to reinstate the previous set. Useful
// for tests or called globally without worrying about restoring.
func InjectGeneric(extra []asserts.Assertion) (restore func()) {
	prev := genericExtraAssertions
	genericExtraAssertions = make([]asserts.Assertion, len(prev)+len(extra))
	copy(genericExtraAssertions, prev)
	copy(genericExtraAssertions[len(prev):], extra)
	return func() {
		genericExtraAssertions = prev
	}
}
