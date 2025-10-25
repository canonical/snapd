// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package assemblestate

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/asserts"
)

// AssertionDevices converts the data returned by an assembly session into the
// data structure used by the "devices" block of a cluster assertion.
func AssertionDevices(ids []Identity, routes Routes) ([]any, error) {
	addresses, err := addressesFromRoutes(routes)
	if err != nil {
		return nil, err
	}

	serials := make(map[DeviceToken]*asserts.Serial, len(ids))
	for _, identity := range ids {
		serial, err := serialFromBundle(identity.SerialBundle)
		if err != nil {
			return nil, fmt.Errorf("cannot parse serial bundle for device %q: %w", identity.RDT, err)
		}

		if _, ok := serials[identity.RDT]; ok {
			return nil, fmt.Errorf("duplicate device token found in identities: %q", identity.RDT)
		}

		serials[identity.RDT] = serial
	}

	// sort identities based on brand, model, then serial so that numeric id
	// assignment is consistent, even across multiple assemble sessions.
	sort.Slice(ids, func(i, j int) bool {
		left := serials[ids[i].RDT]
		right := serials[ids[j].RDT]

		if left.BrandID() != right.BrandID() {
			return left.BrandID() < right.BrandID()
		}
		if left.Model() != right.Model() {
			return left.Model() < right.Model()
		}

		return left.Serial() < right.Serial()
	})

	devices := make([]any, 0, len(ids))
	for i, identity := range ids {
		addrs := addresses[identity.RDT]
		if len(addrs) == 0 {
			return nil, fmt.Errorf("no addresses available for device %q", identity.RDT)
		}

		serial := serials[identity.RDT]
		devices = append(devices, map[string]any{
			"id":        strconv.Itoa(i + 1),
			"device":    serial.DeviceID().String(),
			"addresses": addrs,
		})
	}

	return devices, nil
}

func serialFromBundle(bundle string) (*asserts.Serial, error) {
	decoder := asserts.NewDecoder(strings.NewReader(bundle))
	for {
		assertion, err := decoder.Decode()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("cannot decode serial bundle: %w", err)
		}

		if assertion.Type() == asserts.SerialType {
			serial, ok := assertion.(*asserts.Serial)
			if !ok {
				return nil, fmt.Errorf("internal error: serial assertion has unexpected type %T", assertion)
			}
			return serial, nil
		}
	}

	return nil, errors.New("serial assertion not found in bundle")
}

func addressesFromRoutes(routes Routes) (map[DeviceToken][]any, error) {
	if len(routes.Routes)%3 != 0 {
		return nil, errors.New("routes array length must be multiple of 3")
	}

	addressSets := make(map[DeviceToken]map[string]bool)

	// TODO:GOVERSION: we repeat this iteration and validation construct a lot,
	// real iterators would be a good fit here
	for i := 0; i < len(routes.Routes); i += 3 {
		src := routes.Routes[i]
		dest := routes.Routes[i+1]
		addrIdx := routes.Routes[i+2]

		if src < 0 || src >= len(routes.Devices) {
			return nil, fmt.Errorf("invalid source device index %d in routes", src)
		}
		if dest < 0 || dest >= len(routes.Devices) {
			return nil, fmt.Errorf("invalid destination device index %d in routes", dest)
		}
		if addrIdx < 0 || addrIdx >= len(routes.Addresses) {
			return nil, fmt.Errorf("invalid address index %d in routes", addrIdx)
		}

		destRDT := routes.Devices[dest]
		addr := routes.Addresses[addrIdx]

		if addressSets[destRDT] == nil {
			addressSets[destRDT] = make(map[string]bool)
		}

		addressSets[destRDT][addr] = true
	}

	addresses := make(map[DeviceToken][]any, len(addressSets))
	for rdt, set := range addressSets {
		sorted := make([]string, 0, len(set))
		for addr := range set {
			sorted = append(sorted, addr)
		}
		sort.Strings(sorted)

		addrs := make([]any, 0, len(sorted))
		for _, addr := range sorted {
			addrs = append(addrs, addr)
		}

		addresses[rdt] = addrs
	}

	return addresses, nil
}
