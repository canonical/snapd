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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/strutil"
)

// Runner implements fetching, tracking and running repairs.
type Runner struct {
	BaseURL *url.URL
	cli     *http.Client

	state state

	// nextIdx keeps track of the next internal idx in a brand sequence to considered in this run, see Next.
	nextIdx map[string]int
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
		cli:     cli,
		nextIdx: make(map[string]int),
	}
	// TODO: call LoadState implicitly?
}

var (
	fetchRetryStrategy = retry.LimitCount(10, retry.LimitTime(1*time.Minute,
		retry.Exponential{
			Initial: 100 * time.Millisecond,
			Factor:  2.5,
		},
	))

	peekRetryStrategy = retry.LimitCount(7, retry.LimitTime(30*time.Second,
		retry.Exponential{
			Initial: 100 * time.Millisecond,
			Factor:  2.5,
		},
	))
)

var ErrRepairNotFound = errors.New("repair not found")

var (
	maxRepairScriptSize = 24 * 1024 * 1024
)

// Fetch retrieves a stream with the repair with the given ids and any auxiliary assertions.
func (run *Runner) Fetch(brandID, repairID string) (r []asserts.Assertion, err error) {
	u, err := run.BaseURL.Parse(fmt.Sprintf("repairs/%s/%s", brandID, repairID))
	if err != nil {
		return nil, err
	}

	resp, err := httputil.RetryRequest(u.String(), func() (*http.Response, error) {
		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/x.ubuntu.assertion")
		return run.cli.Do(req)
	}, func(resp *http.Response) error {
		if resp.StatusCode == 200 {
			// decode assertions
			dec := asserts.NewDecoderWithTypeMaxBodySize(resp.Body, map[*asserts.AssertionType]int{
				asserts.RepairType: maxRepairScriptSize,
			})
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

	if err := checkStreamSanity(brandID, repairID, r); err != nil {
		return nil, fmt.Errorf("cannot fetch repair, %v", err)
	}

	return r, nil
}

func checkStreamSanity(brandID, repairID string, r []asserts.Assertion) error {
	if len(r) == 0 {
		return fmt.Errorf("empty repair assertions stream")
	}
	repair, ok := r[0].(*asserts.Repair)
	if !ok {
		return fmt.Errorf("unexpected first assertion %q", r[0].Type().Name)
	}

	if repair.BrandID() != brandID || repair.RepairID() != repairID {
		return fmt.Errorf("id mismatch %s/%s != %s/%s", repair.BrandID(), repair.RepairID(), brandID, repairID)
	}

	return nil
}

type peekResp struct {
	Headers map[string]interface{} `json:"headers"`
}

// Peek retrieves the headers for the repair with the given ids.
func (run *Runner) Peek(brandID, repairID string) (headers map[string]interface{}, err error) {
	u, err := run.BaseURL.Parse(fmt.Sprintf("repairs/%s/%s", brandID, repairID))
	if err != nil {
		return nil, err
	}

	var rsp peekResp

	resp, err := httputil.RetryRequest(u.String(), func() (*http.Response, error) {
		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		return run.cli.Do(req)
	}, func(resp *http.Response) error {
		rsp.Headers = nil
		if resp.StatusCode == 200 {
			dec := json.NewDecoder(resp.Body)
			return dec.Decode(&rsp)
		}
		return nil
	}, peekRetryStrategy)

	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case 200:
		// ok
	case 404:
		return nil, ErrRepairNotFound
	default:
		return nil, fmt.Errorf("cannot peek repair headers, unexpected status %d", resp.StatusCode)
	}

	headers = rsp.Headers
	if headers["brand-id"] != brandID || headers["repair-id"] != repairID {
		return nil, fmt.Errorf("cannot peek repair headers, id mismatch %s/%s != %s/%s", headers["brand-id"], headers["repair-id"], brandID, repairID)
	}

	return headers, nil
}

// deviceInfo captures information about the device.
type deviceInfo struct {
	Brand string `json:"brand"`
	Model string `json:"model"`
}

// RepairStatus represents the possible statuses of a repair.
type RepairStatus int

const (
	RetryStatus RepairStatus = iota
	NotApplicableStatus
	DoneStatus
)

// RepairState holds the current revision and status of a repair in a sequence of repairs.
type RepairState struct {
	Seq      int          `json:"seq"`
	Revision int          `json:"revision"`
	Status   RepairStatus `json:"status"`
}

// state holds the atomically updated control state of the runner with sequences of repairs and their states.
type state struct {
	Device    deviceInfo                `json:"device"`
	Sequences map[string][]*RepairState `json:"sequences,omitempty"`
}

func (run *Runner) readState() error {
	r, err := os.Open(dirs.SnapRepairStateFile)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(r)
	return dec.Decode(&run.state)
}

func (run *Runner) initState() error {
	if err := os.MkdirAll(dirs.SnapRepairDir, 0775); err != nil {
		panic(fmt.Sprintf("cannot create repair state directory: %v", err))
	}
	// best-effort remove old
	os.Remove(dirs.SnapRepairStateFile)
	// TODO: init Device
	run.SaveState()
	return nil
}

// LoadState loads the repairs' state from disk, and (re)initializes it if it's missing or corrupted.
func (run *Runner) LoadState() error {
	err := run.readState()
	if err == nil {
		return nil
	}
	// error => initialize from scratch
	// TODO: log error?
	return run.initState()
}

// SaveState saves the repairs' state to disk.
func (run *Runner) SaveState() {
	m, err := json.Marshal(&run.state)
	if err != nil {
		panic(fmt.Sprintf("cannot marshal repair state: %v", err))
	}
	err = osutil.AtomicWriteFile(dirs.SnapRepairStateFile, m, 0600, 0)
	if err != nil {
		panic(fmt.Sprintf("cannot save repair state: %v", err))
	}
}

func stringList(headers map[string]interface{}, name string) ([]string, error) {
	v, ok := headers[name]
	if !ok {
		return nil, nil
	}
	l, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid %q header", name)
	}
	r := make([]string, len(l))
	for i, v := range l {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("invalid %q header", name)
		}
		r[i] = s
	}
	return r, nil
}

// Applicable returns whether a repair with the given headers is applicable to the device.
func (run *Runner) Applicable(headers map[string]interface{}) bool {
	series, err := stringList(headers, "series")
	if err != nil {
		return false
	}
	if len(series) != 0 && !strutil.ListContains(series, release.Series) {
		return false
	}
	// TODO: architecture, model filtering
	return true
}

var errNotToRun = errors.New("repair is not to run")

func (run *Runner) fetch(brandID string, seq int) (r []asserts.Assertion, err error) {
	repairID := strconv.Itoa(seq)
	headers, err := run.Peek(brandID, repairID)
	if err != nil {
		return nil, err
	}
	if !run.Applicable(headers) {
		return nil, errNotToRun
	}
	return run.Fetch(brandID, repairID)
}

var errReuse = errors.New("reuse repair on disk")

func (run *Runner) refetch(brandID string, seq, revision int) (r []asserts.Assertion, err error) {
	// TODO: the endpoint should support revision based E-Tags and then we
	// can use them here instead of two requests
	repairID := strconv.Itoa(seq)
	headers, err := run.Peek(brandID, repairID)
	if err != nil {
		return nil, err
	}
	refetchRevision, ok := headers["revision"]
	if !ok {
		refetchRevision = "0"
	}
	if refetchRevision == strconv.Itoa(revision) {
		return nil, errReuse
	}
	return run.Fetch(brandID, repairID)
}

func (run *Runner) saveStream(brandID string, seq int, r []asserts.Assertion) error {
	repairID := strconv.Itoa(seq)
	d := filepath.Join(dirs.SnapRepairAssertsDir, brandID, repairID)
	err := os.MkdirAll(d, 0775)
	if err != nil {
		return err
	}
	buf := &bytes.Buffer{}
	enc := asserts.NewEncoder(buf)
	for _, a := range r {
		if err := enc.Encode(a); err != nil {
			return fmt.Errorf("cannot encode repair assertions %q-%s for saving: %v", brandID, repairID, err)
		}
	}
	p := filepath.Join(d, fmt.Sprintf("repair.r%d", r[0].Revision()))
	return osutil.AtomicWriteFile(p, buf.Bytes(), 0600, 0)
}

func (run *Runner) readSavedStream(brandID string, seq, revision int) (r []asserts.Assertion, err error) {
	repairID := strconv.Itoa(seq)
	d := filepath.Join(dirs.SnapRepairAssertsDir, brandID, repairID)
	p := filepath.Join(d, fmt.Sprintf("repair.r%d", revision))
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	dec := asserts.NewDecoder(f)
	for {
		a, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("cannot decode repair assertions %q-%s from disk: %v", brandID, repairID, err)
		}
		r = append(r, a)
	}
	if err := checkStreamSanity(brandID, repairID, r); err != nil {
		return nil, err
	}
	return r, nil
}

func (run *Runner) makeReady(brandID string, idx int) (state *RepairState, repair *asserts.Repair, err error) {
	sequence := run.state.Sequences[brandID]
	var a []asserts.Assertion
	if idx < len(sequence) {
		// consider retries
		state = sequence[idx]
		if state.Status != RetryStatus {
			return nil, nil, errNotToRun
		}
		var err error
		a, err = run.refetch(brandID, state.Seq, state.Revision)
		if err != nil {
			// TODO: log error
			// try to use what we have already on disk
			a, err = run.readSavedStream(brandID, state.Seq, state.Revision)
			if err != nil {
				return nil, nil, err
			}
		}
	} else {
		// fetch the next repair in the sequence
		nextSeq := 0
		if n := len(sequence); n != 0 {
			nextSeq = sequence[n-1].Seq
		}
		nextSeq += 1
		var err error
		a, err = run.fetch(brandID, nextSeq)
		if err != nil && err != errNotToRun {
			return nil, nil, err
		}
		state = &RepairState{
			Seq: nextSeq,
		}
		if err == errNotToRun {
			// TODO: store headers to justify decision
			state.Status = NotApplicableStatus
			return state, nil, errNotToRun
		}
	}
	// TODO: verify with signatures
	if err := run.saveStream(brandID, state.Seq, a); err != nil {
		return nil, nil, err
	}
	repair = a[0].(*asserts.Repair)
	state.Revision = repair.Revision()
	if !run.Applicable(repair.Headers()) {
		state.Status = NotApplicableStatus
		return state, nil, errNotToRun
	}
	return state, repair, nil
}

// Next returns the next repair for the brand id sequence to run/retry or ErrRepairNotFound if there is none atm. It updates the state as required.
func (run *Runner) Next(brandID string) (*asserts.Repair, error) {
	nextIdx, _ := run.nextIdx[brandID]
	for {
		state, repair, err := run.makeReady(brandID, nextIdx)
		if err == ErrRepairNotFound {
			run.SaveState()
			return nil, ErrRepairNotFound
		}
		if err != nil && err != errNotToRun {
			return nil, err
		}
		// append state for new repair in sequence
		if nextIdx == len(run.state.Sequences[brandID]) {
			if run.state.Sequences == nil {
				run.state.Sequences = make(map[string][]*RepairState)
			}
			run.state.Sequences[brandID] = append(run.state.Sequences[brandID], state)
		}
		nextIdx += 1
		run.nextIdx[brandID] = nextIdx
		if err == errNotToRun {
			continue
		}
		run.SaveState()
		return repair, nil
	}
}
