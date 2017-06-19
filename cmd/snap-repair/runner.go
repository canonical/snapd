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

package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/httputil"
)

// Runner implements fetching, tracking and running repairs.
type Runner struct {
	URL string
	cli *http.Client
}

// NewRunner returns a Runner.
func NewRunner() *Runner {
	// TODO: pass TLSConfig with lower-bounded time
	opts := httputil.ClientOpts{
		Timeout:    15 * time.Second,
		MayLogBody: false,
	}
	cli := httputil.NewHTTPClient(&opts)
	return &Runner{
		cli: cli,
	}
}

var (
	fetchRetryStrategy = retry.LimitCount(10, retry.LimitTime(1*time.Minute,
		retry.Exponential{
			Initial: 100 * time.Millisecond,
			Factor:  2.5,
		},
	))
)

var ErrRepairNotFound = errors.New("repair not found")

// Fetch retrieves a stream with the repair with the given ids and any auxiliary assertions.
func (run *Runner) Fetch(brandID, repairID string) (r []asserts.Assertion, err error) {
	// TODO: BaseURL
	url := fmt.Sprintf(run.URL, brandID, repairID)

	resp, err := httputil.RetryRequest(url, func() (*http.Response, error) {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/x.ubuntu.assertion")
		return run.cli.Do(req)
	}, func(resp *http.Response) error {
		if resp.StatusCode == 200 {
			// decode assertions
			// TODO: use a decoder that can accept large repair bodies
			dec := asserts.NewDecoder(resp.Body)
			for {
				a, err := dec.Decode()
				if err == io.EOF {
					break
				}
				if err != nil {
					return err
				}
				r = append(r, a)
			}
			if len(r) == 0 {
				return io.ErrUnexpectedEOF
			}
		}
		return nil
	}, fetchRetryStrategy)

	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case 200:
		// ok
	case 404:
		return nil, ErrRepairNotFound
	default:
		return nil, fmt.Errorf("cannot fetch repair, unexpected status %d", resp.StatusCode)
	}

	repair, ok := r[0].(*asserts.Repair)
	if !ok {
		return nil, fmt.Errorf("cannot fetch repair, unexpected first assertion %q", r[0].Type().Name)
	}

	if repair.BrandID() != brandID || repair.RepairID() != repairID {
		return nil, fmt.Errorf("cannot fetch repair, id mismatch %s/%s != %s/%s", repair.BrandID(), repair.RepairID(), brandID, repairID)
	}

	return r, nil
}
