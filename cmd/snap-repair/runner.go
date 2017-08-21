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
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/strutil"
)

// Repair is a runnable repair.
type Repair struct {
	*asserts.Repair

	run      *Runner
	sequence int
}

// SetStatus sets the status of the repair in the state and saves the latter.
func (r *Repair) SetStatus(status RepairStatus) {
	brandID := r.BrandID()
	cur := *r.run.state.Sequences[brandID][r.sequence-1]
	cur.Status = status
	r.run.setRepairState(brandID, cur)
	r.run.SaveState()
}

// Run executes the repair script leaving execution trail files on disk.
func (r *Repair) Run() error {
	// write the script to disk
	rundir := filepath.Join(dirs.SnapRepairRunDir, r.BrandID(), r.RepairID())
	err := os.MkdirAll(rundir, 0775)
	if err != nil {
		return err
	}

	script := filepath.Join(rundir, fmt.Sprintf("script.r%d", r.Revision()))
	err = osutil.AtomicWriteFile(script, r.Body(), 0700, 0)
	if err != nil {
		return err
	}

	now := time.Now().Unix()
	logPath := filepath.Join(rundir, fmt.Sprintf("r%d.%v.output", r.Revision(), now))
	logf, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer logf.Close()

	// run things and captures output etc
	stR, stW, err := os.Pipe()
	if err != nil {
		return err
	}
	defer stR.Close()
	defer stW.Close()

	// run the script
	cmd := exec.Command(script)
	cmd.ExtraFiles = []*os.File{stW}
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "SNAP_REPAIR_STATUS_FD=3")
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	stW.Close()

	// stream output to logfile
	rr := io.MultiReader(stdoutPipe, stderrPipe)
	if _, err := io.Copy(logf, rr); err != nil {
		return err
	}
	// ignore error, we only care about what we got via the status pipe
	cmd.Wait()

	// check status, write stamp
	statusOutput, err := ioutil.ReadAll(stR)
	if err != nil {
		return err
	}

	var status RepairStatus
	switch strings.TrimSpace(string(statusOutput)) {
	case "done":
		status = DoneStatus
	case "skip":
		status = SkipStatus
	}
	statusPath := filepath.Join(rundir, fmt.Sprintf("r%d.%v.%s", r.Revision(), now, status))
	if err := osutil.AtomicWriteFile(statusPath, nil, 0600, 0); err != nil {
		return err
	}
	r.SetStatus(status)

	return nil
}

// Runner implements fetching, tracking and running repairs.
type Runner struct {
	BaseURL *url.URL
	cli     *http.Client

	state         state
	stateModified bool

	// sequenceNext keeps track of the next integer id in a brand sequence to considered in this run, see Next.
	sequenceNext map[string]int
}

// NewRunner returns a Runner.
func NewRunner() *Runner {
	// TODO: pass TLSConfig with lower-bounded time
	opts := httputil.ClientOpts{
		MayLogBody: false,
	}
	cli := httputil.NewHTTPClient(&opts)
	return &Runner{
		cli:          cli,
		sequenceNext: make(map[string]int),
	}
}

var (
	fetchRetryStrategy = retry.LimitCount(7, retry.LimitTime(90*time.Second,
		retry.Exponential{
			Initial: 500 * time.Millisecond,
			Factor:  2.5,
		},
	))

	peekRetryStrategy = retry.LimitCount(5, retry.LimitTime(44*time.Second,
		retry.Exponential{
			Initial: 300 * time.Millisecond,
			Factor:  2.5,
		},
	))
)

var (
	ErrRepairNotFound    = errors.New("repair not found")
	ErrRepairNotModified = errors.New("repair was not modified")
)

var (
	maxRepairScriptSize = 24 * 1024 * 1024
)

// Fetch retrieves a stream with the repair with the given ids and any
// auxiliary assertions. If revision>=0 the request will include an
// If-None-Match header with an ETag for the revision, and
// ErrRepairNotModified is returned if the revision is still current.
func (run *Runner) Fetch(brandID, repairID string, revision int) (repair *asserts.Repair, aux []asserts.Assertion, err error) {
	u, err := run.BaseURL.Parse(fmt.Sprintf("repairs/%s/%s", brandID, repairID))
	if err != nil {
		return nil, nil, err
	}

	var r []asserts.Assertion
	resp, err := httputil.RetryRequest(u.String(), func() (*http.Response, error) {
		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/x.ubuntu.assertion")
		if revision >= 0 {
			req.Header.Set("If-None-Match", fmt.Sprintf(`"%d"`, revision))
		}
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
		return nil, nil, err
	}

	switch resp.StatusCode {
	case 200:
		// ok
	case 304:
		// not modified
		return nil, nil, ErrRepairNotModified
	case 404:
		return nil, nil, ErrRepairNotFound
	default:
		return nil, nil, fmt.Errorf("cannot fetch repair, unexpected status %d", resp.StatusCode)
	}

	repair, aux, err = checkStream(brandID, repairID, r)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot fetch repair, %v", err)
	}
	return
}

func checkStream(brandID, repairID string, r []asserts.Assertion) (repair *asserts.Repair, aux []asserts.Assertion, err error) {
	if len(r) == 0 {
		return nil, nil, fmt.Errorf("empty repair assertions stream")
	}
	var ok bool
	repair, ok = r[0].(*asserts.Repair)
	if !ok {
		return nil, nil, fmt.Errorf("unexpected first assertion %q", r[0].Type().Name)
	}

	if repair.BrandID() != brandID || repair.RepairID() != repairID {
		return nil, nil, fmt.Errorf("repair id mismatch %s/%s != %s/%s", repair.BrandID(), repair.RepairID(), brandID, repairID)
	}

	return repair, r[1:], nil
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
		return nil, fmt.Errorf("cannot peek repair headers, repair id mismatch %s/%s != %s/%s", headers["brand-id"], headers["repair-id"], brandID, repairID)
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
	SkipStatus
	DoneStatus
)

func (rs RepairStatus) String() string {
	switch rs {
	case RetryStatus:
		return "retry"
	case SkipStatus:
		return "skip"
	case DoneStatus:
		return "done"
	default:
		panic(fmt.Sprintf("unknown repair status %d", rs))
	}
}

// RepairState holds the current revision and status of a repair in a sequence of repairs.
type RepairState struct {
	Sequence int          `json:"sequence"`
	Revision int          `json:"revision"`
	Status   RepairStatus `json:"status"`
}

// state holds the atomically updated control state of the runner with sequences of repairs and their states.
type state struct {
	Device    deviceInfo                `json:"device"`
	Sequences map[string][]*RepairState `json:"sequences,omitempty"`
}

func (run *Runner) setRepairState(brandID string, state RepairState) {
	if run.state.Sequences == nil {
		run.state.Sequences = make(map[string][]*RepairState)
	}
	sequence := run.state.Sequences[brandID]
	if state.Sequence > len(sequence) {
		run.stateModified = true
		run.state.Sequences[brandID] = append(sequence, &state)
	} else if *sequence[state.Sequence-1] != state {
		run.stateModified = true
		sequence[state.Sequence-1] = &state
	}
}

func (run *Runner) readState() error {
	r, err := os.Open(dirs.SnapRepairStateFile)
	if err != nil {
		return err
	}
	defer r.Close()
	dec := json.NewDecoder(r)
	return dec.Decode(&run.state)
}

func (run *Runner) initState() error {
	if err := os.MkdirAll(dirs.SnapRepairDir, 0775); err != nil {
		return fmt.Errorf("cannot create repair state directory: %v", err)
	}
	// best-effort remove old
	os.Remove(dirs.SnapRepairStateFile)
	// TODO: init Device
	run.stateModified = true
	return run.SaveState()
}

// LoadState loads the repairs' state from disk, and (re)initializes it if it's missing or corrupted.
func (run *Runner) LoadState() error {
	err := run.readState()
	if err == nil {
		return nil
	}
	// error => initialize from scratch
	if !os.IsNotExist(err) {
		logger.Noticef("cannor read repair state: %v", err)
	}
	return run.initState()
}

// SaveState saves the repairs' state to disk.
func (run *Runner) SaveState() error {
	if !run.stateModified {
		return nil
	}
	m, err := json.Marshal(&run.state)
	if err != nil {
		return fmt.Errorf("cannot marshal repair state: %v", err)
	}
	err = osutil.AtomicWriteFile(dirs.SnapRepairStateFile, m, 0600, 0)
	if err != nil {
		return fmt.Errorf("cannot save repair state: %v", err)
	}
	run.stateModified = false
	return nil
}

func stringList(headers map[string]interface{}, name string) ([]string, error) {
	v, ok := headers[name]
	if !ok {
		return nil, nil
	}
	l, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("header %q is not a list", name)
	}
	r := make([]string, len(l))
	for i, v := range l {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("header %q contains non-string elements", name)
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

var errSkip = errors.New("repair unnecessary on this system")

func (run *Runner) fetch(brandID string, seq int) (repair *asserts.Repair, aux []asserts.Assertion, err error) {
	repairID := strconv.Itoa(seq)
	headers, err := run.Peek(brandID, repairID)
	if err != nil {
		return nil, nil, err
	}
	if !run.Applicable(headers) {
		return nil, nil, errSkip
	}
	return run.Fetch(brandID, repairID, -1)
}

func (run *Runner) refetch(brandID string, seq, revision int) (repair *asserts.Repair, aux []asserts.Assertion, err error) {
	repairID := strconv.Itoa(seq)
	return run.Fetch(brandID, repairID, revision)
}

func (run *Runner) saveStream(brandID string, seq int, repair *asserts.Repair, aux []asserts.Assertion) error {
	repairID := strconv.Itoa(seq)
	d := filepath.Join(dirs.SnapRepairAssertsDir, brandID, repairID)
	err := os.MkdirAll(d, 0775)
	if err != nil {
		return err
	}
	buf := &bytes.Buffer{}
	enc := asserts.NewEncoder(buf)
	r := append([]asserts.Assertion{repair}, aux...)
	for _, a := range r {
		if err := enc.Encode(a); err != nil {
			return fmt.Errorf("cannot encode repair assertions %q-%s for saving: %v", brandID, repairID, err)
		}
	}
	p := filepath.Join(d, fmt.Sprintf("repair.r%d", r[0].Revision()))
	return osutil.AtomicWriteFile(p, buf.Bytes(), 0600, 0)
}

func (run *Runner) readSavedStream(brandID string, seq, revision int) (repair *asserts.Repair, aux []asserts.Assertion, err error) {
	repairID := strconv.Itoa(seq)
	d := filepath.Join(dirs.SnapRepairAssertsDir, brandID, repairID)
	p := filepath.Join(d, fmt.Sprintf("repair.r%d", revision))
	f, err := os.Open(p)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	dec := asserts.NewDecoder(f)
	var r []asserts.Assertion
	for {
		a, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("cannot decode repair assertions %q-%s from disk: %v", brandID, repairID, err)
		}
		r = append(r, a)
	}
	return checkStream(brandID, repairID, r)
}

func (run *Runner) makeReady(brandID string, sequenceNext int) (repair *asserts.Repair, err error) {
	sequence := run.state.Sequences[brandID]
	var aux []asserts.Assertion
	var state RepairState
	if sequenceNext <= len(sequence) {
		// consider retries
		state = *sequence[sequenceNext-1]
		if state.Status != RetryStatus {
			return nil, errSkip
		}
		var err error
		repair, aux, err = run.refetch(brandID, state.Sequence, state.Revision)
		if err != nil {
			logger.Noticef("cannot refetch repair %q-%d, will retry what is on disk: %v", brandID, sequenceNext, err)
			// try to use what we have already on disk
			repair, aux, err = run.readSavedStream(brandID, state.Sequence, state.Revision)
			if err != nil {
				return nil, err
			}
		}
	} else {
		// fetch the next repair in the sequence
		// assumes no gaps, each repair id is present so far,
		// possibly skipped
		var err error
		repair, aux, err = run.fetch(brandID, sequenceNext)
		if err != nil && err != errSkip {
			return nil, err
		}
		state = RepairState{
			Sequence: sequenceNext,
		}
		if err == errSkip {
			// TODO: store headers to justify decision
			state.Status = SkipStatus
			run.setRepairState(brandID, state)
			return nil, errSkip
		}
	}
	// TODO: verify with signatures
	if err := run.saveStream(brandID, state.Sequence, repair, aux); err != nil {
		return nil, err
	}
	state.Revision = repair.Revision()
	if !run.Applicable(repair.Headers()) {
		state.Status = SkipStatus
		run.setRepairState(brandID, state)
		return nil, errSkip
	}
	run.setRepairState(brandID, state)
	return repair, nil
}

// Next returns the next repair for the brand id sequence to run/retry or ErrRepairNotFound if there is none atm. It updates the state as required.
func (run *Runner) Next(brandID string) (*Repair, error) {
	sequenceNext := run.sequenceNext[brandID]
	if sequenceNext == 0 {
		sequenceNext = 1
	}
	for {
		repair, err := run.makeReady(brandID, sequenceNext)
		// SaveState is a no-op unless makeReady modified the state
		stateErr := run.SaveState()
		if err != nil && err != errSkip && err != ErrRepairNotFound {
			// err is a non trivial error, just log the SaveState error and report err
			if stateErr != nil {
				logger.Noticef("%v", stateErr)
			}
			return nil, err
		}
		if stateErr != nil {
			return nil, stateErr
		}
		if err == ErrRepairNotFound {
			return nil, ErrRepairNotFound
		}

		sequenceNext += 1
		run.sequenceNext[brandID] = sequenceNext
		if err == errSkip {
			continue
		}

		return &Repair{
			Repair:   repair,
			run:      run,
			sequence: sequenceNext - 1,
		}, nil
	}
}
