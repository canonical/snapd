// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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

// Package store has support to use the Ubuntu Store for querying and downloading of snaps, and the related services.
package store

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/juju/ratelimit"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snapdtool"
)

var commandFromSystemSnap = snapdtool.CommandFromSystemSnap

var downloadRetryStrategy = retry.LimitCount(7, retry.LimitTime(90*time.Second,
	retry.Exponential{
		Initial: 500 * time.Millisecond,
		Factor:  2.5,
	},
))

var downloadSpeedMeasureWindow = 5 * time.Minute

// minimum average download speed (bytes/sec), measured over downloadSpeedMeasureWindow.
var downloadSpeedMin = float64(4096)

func init() {
	if v := os.Getenv("SNAPD_MIN_DOWNLOAD_SPEED"); v != "" {
		if speed, err := strconv.Atoi(v); err == nil {
			downloadSpeedMin = float64(speed)
		} else {
			logger.Noticef("Cannot parse SNAPD_MIN_DOWNLOAD_SPEED as number")
		}
	}
	if v := os.Getenv("SNAPD_DOWNLOAD_MEAS_WINDOW"); v != "" {
		if win, err := time.ParseDuration(v); err == nil {
			downloadSpeedMeasureWindow = win
		} else {
			logger.Noticef("Cannot parse SNAPD_DOWNLOAD_MEAS_WINDOW as time.Duration")
		}
	}
}

// Deltas enabled by default on classic, but allow opting in or out on both classic and core.
func (s *Store) useDeltas() (use bool) {
	s.xdeltaCheckLock.Lock()
	defer s.xdeltaCheckLock.Unlock()

	// check the cached value if available
	if s.shouldUseDeltas != nil {
		return *s.shouldUseDeltas
	}

	defer func() {
		// cache whatever value we return for next time
		s.shouldUseDeltas = &use
	}()

	// check if deltas were disabled by the environment
	if !osutil.GetenvBool("SNAPD_USE_DELTAS_EXPERIMENTAL", true) {
		// then the env var is explicitly false, we can't use deltas
		logger.Debugf("delta usage disabled by environment variable")
		return false
	}

	// TODO: have a per-format checker instead, we currently only support
	// xdelta3 as a format for deltas

	// check if the xdelta3 config command works from the system snap
	cmd, err := commandFromSystemSnap("/usr/bin/xdelta3", "config")
	if err == nil {
		// we have a xdelta3 from the system snap, make sure it works
		if runErr := cmd.Run(); runErr == nil {
			// success using the system snap provided one, setup the callback to
			// use the cmd we got from CommandFromSystemSnap, but with a small
			// tweak - this cmd to run xdelta3 from the system snap will likely
			// have other arguments and a different main exe usually, so
			// use it exactly as we got it from CommandFromSystemSnap,
			// but drop the last arg which we know is "config"
			exe := cmd.Path
			args := cmd.Args[:len(cmd.Args)-1]
			env := cmd.Env
			dir := cmd.Dir
			s.xdelta3CmdFunc = func(xDelta3args ...string) *exec.Cmd {
				return &exec.Cmd{
					Path: exe,
					Args: append(args, xDelta3args...),
					Env:  env,
					Dir:  dir,
				}
			}
			return true
		} else {
			logger.Noticef("unable to use system snap provided xdelta3, running config command failed: %v", runErr)
		}
	}

	// we didn't have one from a system snap or it didn't work, fallback to
	// trying xdelta3 from the system
	loc, err := exec.LookPath("xdelta3")
	if err != nil {
		// no xdelta3 in the env, so no deltas
		logger.Noticef("no host system xdelta3 available to use deltas")
		return false
	}

	if err := exec.Command(loc, "config").Run(); err != nil {
		// xdelta3 in the env failed to run, so no deltas
		logger.Noticef("unable to use host system xdelta3, running config command failed: %v", err)
		return false
	}

	// the xdelta3 in the env worked, so use that one
	s.xdelta3CmdFunc = func(args ...string) *exec.Cmd {
		return exec.Command(loc, args...)
	}
	return true
}

func (s *Store) cdnHeader() (string, error) {
	if s.noCDN {
		return "none", nil
	}

	// set Snap-CDN from cloud instance information
	// if available

	// TODO: do we want a more complex retry strategy
	// where we first to send this header and if the
	// operation fails that way to even get the connection
	// then we retry without sending this?
	return s.buildLocationString()
}

type HashError struct {
	name           string
	sha3_384       string
	targetSha3_384 string
}

func (e HashError) Error() string {
	return fmt.Sprintf("sha3-384 mismatch for %q: got %s but expected %s", e.name, e.sha3_384, e.targetSha3_384)
}

type DownloadOptions struct {
	RateLimit           int64
	Scheduled           bool
	LeavePartialOnError bool
}

// Download downloads the snap addressed by download info and returns its
// filename.
// The file is saved in temporary storage, and should be removed
// after use to prevent the disk from running out of space.
func (s *Store) Download(ctx context.Context, name string, targetPath string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState, dlOpts *DownloadOptions) error {
	// most other store network operations use s.endpointURL, which returns an
	// error if the store is offline. this doesn't, so we need to explicitly
	// check.
	if err := s.checkStoreOnline(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	if s.cacher.Get(downloadInfo.Sha3_384, targetPath) {
		logger.Debugf("Cache hit for SHA3_384 …%.5s.", downloadInfo.Sha3_384)
		return nil
	}

	if s.useDeltas() {
		logger.Debugf("Available deltas returned by store: %v", downloadInfo.Deltas)

		if len(downloadInfo.Deltas) == 1 {
			err := s.downloadAndApplyDelta(name, targetPath, downloadInfo, pbar, user, dlOpts)
			if err == nil {
				return nil
			}
			// We revert to normal downloads if there is any error.
			logger.Noticef("Cannot download or apply deltas for %s: %v", name, err)
		}
	}

	partialPath := targetPath + ".partial"
	w, err := os.OpenFile(partialPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	resume, err := w.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	defer func() {
		fi, _ := w.Stat()
		if cerr := w.Close(); cerr != nil && err == nil {
			err = cerr
		}
		if err == nil {
			return
		}
		if dlOpts == nil || !dlOpts.LeavePartialOnError || fi == nil || fi.Size() == 0 {
			os.Remove(w.Name())
		}
	}()
	if resume > 0 {
		logger.Debugf("Resuming download of %q at %d.", partialPath, resume)
	} else {
		logger.Debugf("Starting download of %q.", partialPath)
	}

	url := downloadInfo.DownloadURL
	if downloadInfo.Size == 0 || resume < downloadInfo.Size {
		err = download(ctx, name, downloadInfo.Sha3_384, url, user, s, w, resume, pbar, dlOpts)
		if err != nil {
			logger.Debugf("download of %q failed: %#v", url, err)
		}
	} else {
		// we're done! check the hash though
		h := crypto.SHA3_384.New()
		if _, err := w.Seek(0, os.SEEK_SET); err != nil {
			return err
		}
		if _, err := io.Copy(h, w); err != nil {
			return err
		}
		actualSha3 := fmt.Sprintf("%x", h.Sum(nil))
		if downloadInfo.Sha3_384 != actualSha3 {
			err = HashError{name, actualSha3, downloadInfo.Sha3_384}
		}
	}
	// If hashsum is incorrect retry once
	if _, ok := err.(HashError); ok {
		logger.Debugf("Hashsum error on download: %v", err.Error())
		logger.Debugf("Truncating and trying again from scratch.")
		err = w.Truncate(0)
		if err != nil {
			return err
		}
		_, err = w.Seek(0, io.SeekStart)
		if err != nil {
			return err
		}
		err = download(ctx, name, downloadInfo.Sha3_384, url, user, s, w, 0, pbar, nil)
		if err != nil {
			logger.Debugf("download of %q failed: %#v", url, err)
		}
	}

	if err != nil {
		return err
	}

	if err := os.Rename(w.Name(), targetPath); err != nil {
		return err
	}

	if err := w.Sync(); err != nil {
		return err
	}

	return s.cacher.Put(downloadInfo.Sha3_384, targetPath)
}

func downloadReqOpts(storeURL *url.URL, cdnHeader string, opts *DownloadOptions) *requestOptions {
	reqOptions := requestOptions{
		Method:       "GET",
		URL:          storeURL,
		ExtraHeaders: map[string]string{},
		// FIXME: use the new headers? with
		// APILevel: apiV2Endps,
	}
	if cdnHeader != "" {
		reqOptions.ExtraHeaders["Snap-CDN"] = cdnHeader
	}
	if opts != nil && opts.Scheduled {
		reqOptions.ExtraHeaders["Snap-Refresh-Reason"] = "scheduled"
	}

	return &reqOptions
}

type transferSpeedError struct {
	Speed float64
}

func (e *transferSpeedError) Error() string {
	return fmt.Sprintf("download too slow: %.2f bytes/sec", e.Speed)
}

// implements io.Writer interface
// XXX: move to osutil?
type TransferSpeedMonitoringWriter struct {
	mu sync.Mutex

	measureTimeWindow   time.Duration
	minDownloadSpeedBps float64

	ctx context.Context

	// internal state
	start   time.Time
	written int
	cancel  func()
	err     error

	// for testing
	measuredWindows int
}

// NewTransferSpeedMonitoringWriterAndContext returns an io.Writer that measures
// write speed in measureTimeWindow windows and cancels the operation if
// minDownloadSpeedBps is not achieved.
// Monitor() must be called to start actual measurement.
func NewTransferSpeedMonitoringWriterAndContext(origCtx context.Context, measureTimeWindow time.Duration, minDownloadSpeedBps float64) (*TransferSpeedMonitoringWriter, context.Context) {
	ctx, cancel := context.WithCancel(origCtx)
	w := &TransferSpeedMonitoringWriter{
		measureTimeWindow:   measureTimeWindow,
		minDownloadSpeedBps: minDownloadSpeedBps,
		ctx:                 ctx,
		cancel:              cancel,
	}
	return w, ctx
}

func (w *TransferSpeedMonitoringWriter) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.written = 0
	w.start = time.Now()
	w.measuredWindows++
}

// checkSpeed measures the transfer rate since last reset() call.
// The caller must call reset() over the desired time windows.
func (w *TransferSpeedMonitoringWriter) checkSpeed(min float64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	d := time.Since(w.start)
	// should never happen since checkSpeed is done after measureTimeWindow
	if d.Seconds() == 0 {
		return true
	}
	s := float64(w.written) / d.Seconds()
	ok := s >= min
	if !ok {
		w.err = &transferSpeedError{Speed: s}
	}
	return ok
}

// Monitor starts a new measurement for write operations and returns a quit
// channel that should be closed by the caller to finish the measurement.
func (w *TransferSpeedMonitoringWriter) Monitor() (quit chan bool) {
	quit = make(chan bool)
	w.reset()
	go func() {
		for {
			select {
			case <-time.After(w.measureTimeWindow):
				if !w.checkSpeed(w.minDownloadSpeedBps) {
					w.cancel()
					return
				}
				// reset the measurement every downloadSpeedMeasureWindow,
				// we want average speed per second over the mesure time window,
				// otherwise a large download with initial good download
				// speed could get stuck at the end of the download, and it
				// would take long time for overall average to "catch up".
				w.reset()
			case <-quit:
				return
			}
		}
	}()
	return quit
}

func (w *TransferSpeedMonitoringWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.written += len(p)
	return len(p), nil
}

// Err returns the transferSpeedError if encountered when measurement was run.
func (w *TransferSpeedMonitoringWriter) Err() error {
	return w.err
}

var ratelimitReader = ratelimit.Reader

var download = downloadImpl

// download writes an http.Request showing a progress.Meter
func downloadImpl(ctx context.Context, name, sha3_384, downloadURL string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *DownloadOptions) error {
	if dlOpts == nil {
		dlOpts = &DownloadOptions{}
	}

	storeURL, err := url.Parse(downloadURL)
	if err != nil {
		return err
	}

	cdnHeader, err := s.cdnHeader()
	if err != nil {
		return err
	}

	tc, downloadCtx := NewTransferSpeedMonitoringWriterAndContext(ctx, downloadSpeedMeasureWindow, downloadSpeedMin)

	var finalErr error
	var dlSize float64
	startTime := time.Now()
	for attempt := retry.Start(downloadRetryStrategy, nil); attempt.Next(); {
		reqOptions := downloadReqOpts(storeURL, cdnHeader, dlOpts)

		httputil.MaybeLogRetryAttempt(reqOptions.URL.String(), attempt, startTime)

		h := crypto.SHA3_384.New()

		if resume > 0 {
			reqOptions.ExtraHeaders["Range"] = fmt.Sprintf("bytes=%d-", resume)
			// seed the sha3 with the already local file
			if _, err := w.Seek(0, io.SeekStart); err != nil {
				return err
			}
			n, err := io.Copy(h, w)
			if err != nil {
				return err
			}
			if n != resume {
				return fmt.Errorf("resume offset wrong: %d != %d", resume, n)
			}
		}

		if cancelled(downloadCtx) {
			return fmt.Errorf("the download has been cancelled: %s", downloadCtx.Err())
		}
		var resp *http.Response
		cli := s.newHTTPClient(nil)
		oldCheckRedirect := cli.CheckRedirect
		if oldCheckRedirect == nil {
			panic("internal error: the httputil.NewHTTPClient-produced http.Client must have CheckRedirect defined")
		}
		cli.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			// remove user/device auth headers from being sent in "CDN" redirects
			// see also: https://bugs.launchpad.net/snapd/+bug/2027993
			// TODO: do we need to remove other identifying headers?
			dropAuthorization(req, &AuthorizeOptions{deviceAuth: true, apiLevel: reqOptions.APILevel})
			return oldCheckRedirect(req, via)
		}
		resp, finalErr = s.doRequest(downloadCtx, cli, reqOptions, user)
		if cancelled(downloadCtx) {
			return fmt.Errorf("the download has been cancelled: %s", downloadCtx.Err())
		}
		if finalErr != nil {
			if httputil.ShouldRetryAttempt(attempt, finalErr) {
				continue
			}
			break
		}
		if resume > 0 && resp.StatusCode != 206 {
			logger.Debugf("server does not support resume")
			if _, err := w.Seek(0, io.SeekStart); err != nil {
				return err
			}
			h = crypto.SHA3_384.New()
			resume = 0
		}
		if httputil.ShouldRetryHttpResponse(attempt, resp) {
			resp.Body.Close()
			continue
		}

		// XXX: we're inside retry loop, so this will be closed only on return.
		defer resp.Body.Close()

		switch resp.StatusCode {
		case 200, 206: // OK, Partial Content
		case 402: // Payment Required

			return fmt.Errorf("please buy %s before installing it", name)
		default:
			return &DownloadError{Code: resp.StatusCode, URL: resp.Request.URL}
		}

		if pbar == nil {
			pbar = progress.Null
		}
		dlSize = float64(resp.ContentLength)
		if resp.ContentLength == 0 {
			logger.Noticef("Unexpected Content-Length: 0 for %s", downloadURL)
		} else {
			logger.Debugf("Download size for %s: %d", downloadURL, resp.ContentLength)
		}
		pbar.Start(name, dlSize)
		mw := io.MultiWriter(w, h, pbar, tc)
		var limiter io.Reader
		limiter = resp.Body
		if limit := dlOpts.RateLimit; limit > 0 {
			bucket := ratelimit.NewBucketWithRate(float64(limit), 2*limit)
			limiter = ratelimitReader(resp.Body, bucket)
		}

		stopMonitorCh := tc.Monitor()
		_, finalErr = io.Copy(mw, limiter)
		close(stopMonitorCh)
		pbar.Finished()

		if err := tc.Err(); err != nil {
			return err
		}
		if cancelled(downloadCtx) {
			// cancelled for other reason that download timeout (which would
			// be caught by tc.Err() above).
			return fmt.Errorf("the download has been cancelled: %s", downloadCtx.Err())
		}

		if finalErr != nil {
			if httputil.ShouldRetryAttempt(attempt, finalErr) {
				// error while downloading should resume
				var seekerr error
				resume, seekerr = w.Seek(0, io.SeekEnd)
				if seekerr == nil {
					continue
				}
				// if seek failed, then don't retry end return the original error
			}
			break
		}

		actualSha3 := fmt.Sprintf("%x", h.Sum(nil))
		if sha3_384 != "" && sha3_384 != actualSha3 {
			finalErr = HashError{name, actualSha3, sha3_384}
		}
		break
	}
	if finalErr == nil {
		// not using quantity.FormatFoo as this is just for debug
		dt := time.Since(startTime)
		r := dlSize / dt.Seconds()
		var p rune
		for _, p = range " kMGTPEZY" {
			if r < 1000 {
				break
			}
			r /= 1000
		}

		logger.Debugf("Download succeeded in %.03fs (%.0f%cB/s).", dt.Seconds(), r, p)
	}
	return finalErr
}

// DownloadStream will copy the snap from the request to the io.Reader
func (s *Store) DownloadStream(ctx context.Context, name string, downloadInfo *snap.DownloadInfo, resume int64, user *auth.UserState) (io.ReadCloser, int, error) {
	// most other store network operations use s.endpointURL, which returns an
	// error if the store is offline. this doesn't, so we need to explicitly
	// check.
	if err := s.checkStoreOnline(); err != nil {
		return nil, 0, err
	}

	// XXX: coverage of this is rather poor
	if path := s.cacher.GetPath(downloadInfo.Sha3_384); path != "" {
		logger.Debugf("Cache hit for SHA3_384 …%.5s.", downloadInfo.Sha3_384)
		file, err := os.OpenFile(path, os.O_RDONLY, 0600)
		if err != nil {
			return nil, 0, err
		}
		if resume == 0 {
			return file, 200, nil
		}
		_, err = file.Seek(resume, io.SeekStart)
		if err != nil {
			return nil, 0, err
		}
		return file, 206, nil
	}

	storeURL, err := url.Parse(downloadInfo.DownloadURL)
	if err != nil {
		return nil, 0, err
	}

	cdnHeader, err := s.cdnHeader()
	if err != nil {
		return nil, 0, err
	}

	resp, err := doDownloadReq(ctx, storeURL, cdnHeader, resume, s, user)
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, resp.StatusCode, nil
}

var doDownloadReq = doDownloadReqImpl

func doDownloadReqImpl(ctx context.Context, storeURL *url.URL, cdnHeader string, resume int64, s *Store, user *auth.UserState) (*http.Response, error) {
	reqOptions := downloadReqOpts(storeURL, cdnHeader, nil)
	if resume > 0 {
		reqOptions.ExtraHeaders["Range"] = fmt.Sprintf("bytes=%d-", resume)
	}
	cli := s.newHTTPClient(nil)
	return s.doRequest(ctx, cli, reqOptions, user)
}

// downloadDelta downloads the delta for the preferred format, returning the path.
func (s *Store) downloadDelta(deltaName string, downloadInfo *snap.DownloadInfo, w io.ReadWriteSeeker, pbar progress.Meter, user *auth.UserState, dlOpts *DownloadOptions) error {

	if len(downloadInfo.Deltas) != 1 {
		return errors.New("store returned more than one download delta")
	}

	deltaInfo := downloadInfo.Deltas[0]

	if deltaInfo.Format != s.deltaFormat {
		return fmt.Errorf("store returned unsupported delta format %q (only xdelta3 currently)", deltaInfo.Format)
	}

	url := deltaInfo.DownloadURL

	return download(context.TODO(), deltaName, deltaInfo.Sha3_384, url, user, s, w, 0, pbar, dlOpts)
}

// applyDelta generates a target snap from a previously downloaded snap and a downloaded delta.
var applyDelta = func(s *Store, name string, deltaPath string, deltaInfo *snap.DeltaInfo, targetPath string, targetSha3_384 string) error {
	return s.applyDeltaImpl(name, deltaPath, deltaInfo, targetPath, targetSha3_384)
}

func (s *Store) applyDeltaImpl(name string, deltaPath string, deltaInfo *snap.DeltaInfo, targetPath string, targetSha3_384 string) error {
	snapBase := fmt.Sprintf("%s_%d.snap", name, deltaInfo.FromRevision)
	snapPath := filepath.Join(dirs.SnapBlobDir, snapBase)

	if !osutil.FileExists(snapPath) {
		return fmt.Errorf("snap %q revision %d not found at %s", name, deltaInfo.FromRevision, snapPath)
	}

	if deltaInfo.Format != "xdelta3" {
		return fmt.Errorf("cannot apply unsupported delta format %q (only xdelta3 currently)", deltaInfo.Format)
	}

	partialTargetPath := targetPath + ".partial"

	xdelta3Args := []string{"-d", "-s", snapPath, deltaPath, partialTargetPath}

	// validity check that deltas are available and that the path for the xdelta3
	// command is set
	if ok := s.useDeltas(); !ok {
		return fmt.Errorf("internal error: applyDelta used when deltas are not available")
	}

	// run the xdelta3 command, cleaning up if we fail and logging about it
	if runErr := s.xdelta3CmdFunc(xdelta3Args...).Run(); runErr != nil {
		logger.Noticef("encountered error applying delta: %v", runErr)
		if err := os.Remove(partialTargetPath); err != nil {
			logger.Noticef("error cleaning up partial delta target %q: %s", partialTargetPath, err)
		}
		return runErr
	}

	if err := os.Chmod(partialTargetPath, 0600); err != nil {
		return err
	}

	bsha3_384, _, err := osutil.FileDigest(partialTargetPath, crypto.SHA3_384)
	if err != nil {
		return err
	}
	sha3_384 := fmt.Sprintf("%x", bsha3_384)
	if targetSha3_384 != "" && sha3_384 != targetSha3_384 {
		if err := os.Remove(partialTargetPath); err != nil {
			logger.Noticef("failed to remove partial delta target %q: %s", partialTargetPath, err)
		}
		return HashError{name, sha3_384, targetSha3_384}
	}

	if err := os.Rename(partialTargetPath, targetPath); err != nil {
		return osutil.CopyFile(partialTargetPath, targetPath, 0)
	}

	return nil
}

// downloadAndApplyDelta downloads and then applies the delta to the current snap.
func (s *Store) downloadAndApplyDelta(name, targetPath string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState, dlOpts *DownloadOptions) error {
	deltaInfo := &downloadInfo.Deltas[0]

	deltaPath := fmt.Sprintf("%s.%s-%d-to-%d.partial", targetPath, deltaInfo.Format, deltaInfo.FromRevision, deltaInfo.ToRevision)
	deltaName := fmt.Sprintf(i18n.G("%s (delta)"), name)

	w, err := os.OpenFile(deltaPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := w.Close(); cerr != nil && err == nil {
			err = cerr
		}
		os.Remove(deltaPath)
	}()

	err = s.downloadDelta(deltaName, downloadInfo, w, pbar, user, dlOpts)
	if err != nil {
		return err
	}

	logger.Debugf("Successfully downloaded delta for %q at %s", name, deltaPath)
	if err := applyDelta(s, name, deltaPath, deltaInfo, targetPath, downloadInfo.Sha3_384); err != nil {
		return err
	}

	logger.Debugf("Successfully applied delta for %q at %s, saving %d bytes.", name, deltaPath, downloadInfo.Size-deltaInfo.Size)
	return nil
}

func (s *Store) CacheDownloads() int {
	return s.cfg.CacheDownloads
}

func (s *Store) SetCacheDownloads(fileCount int) {
	s.cfg.CacheDownloads = fileCount
	if fileCount > 0 {
		s.cacher = NewCacheManager(dirs.SnapDownloadCacheDir, fileCount)
	} else {
		s.cacher = &nullCache{}
	}
}
