// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build snapdfips

/*
 * Copyright (C) 2024 Canonical Ltd
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
package snapdtool

// The sole purpose of this file is to include the fipsonly package, so that the
// set of supported TLS algorithms is limited to what is permitted by the FIPS
// spec. Unfortunately Microsoft Go FIPS toolchain < 1.22 does not set up the
// relevant runtime configuration dynamically, see
// https://github.com/microsoft/go/blob/66dd0ab88969dff30f3180c41a1a77f592090d68/eng/doc/fips/README.md#configuration-overview.
// Note that this will only build if the crypto/tls/fipsonly package itself is
// enabled through relevant build tags.

import _ "crypto/tls/fipsonly"
