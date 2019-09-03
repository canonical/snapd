// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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

package seed

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
)

var ErrNoAssertions = errors.New("no seed assertions")

func LoadAssertions(assertsDir string, loaded func(*asserts.Ref) error) (*asserts.Batch, error) {
	batch := asserts.NewBatch(nil)

	dc, err := ioutil.ReadDir(assertsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoAssertions
		}
		return nil, fmt.Errorf("cannot read assertions dir: %s", err)
	}
	for _, fi := range dc {
		fn := filepath.Join(assertsDir, fi.Name())
		refs, err := readAsserts(batch, fn)
		if err != nil {
			return nil, fmt.Errorf("cannot read assertions: %s", err)
		}
		if loaded != nil {
			for _, ref := range refs {
				if err := loaded(ref); err != nil {
					return nil, err
				}
			}
		}
	}

	return batch, nil
}

func readAsserts(batch *asserts.Batch, fn string) ([]*asserts.Ref, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return batch.AddStream(f)
}
