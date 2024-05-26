// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2024 Canonical Ltd
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

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/jsonutil"
)

func unixDialer(socketPath string) func(string, string) (net.Conn, error) {
	if socketPath == "" {
		socketPath = dirs.SnapdSocket
	}
	return func(_, _ string) (net.Conn, error) {
		return net.Dial("unix", socketPath)
	}
}

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Config allows to customize client behavior.
type Config struct {
	// BaseURL contains the base URL where snappy daemon is expected to be.
	// It can be empty for a default behavior of talking over a unix socket.
	BaseURL string

	// DisableAuth controls whether the client should send an
	// Authorization header from reading the auth.json data.
	DisableAuth bool

	// Interactive controls whether the client runs in interactive mode.
	// At present, this only affects whether interactive polkit
	// authorisation is requested.
	Interactive bool

	// Socket is the path to the unix socket to use
	Socket string

	// DisableKeepAlive indicates whether the connections should not be kept
	// alive for later reuse
	DisableKeepAlive bool

	// User-Agent to sent to the snapd daemon
	UserAgent string
}

// A Client knows how to talk to the snappy daemon.
type Client struct {
	baseURL url.URL
	doer    doer

	disableAuth bool
	interactive bool

	maintenance error

	warningCount     int
	warningTimestamp time.Time

	userAgent string

	// SetMayLogBody controls whether a request or response's body may be logged
	// if the appropriate environment variable is set
	SetMayLogBody func(bool)
}

// New returns a new instance of Client
func New(config *Config) *Client {
	if config == nil {
		config = &Config{}
	}

	var baseURL *url.URL
	var dial func(network, addr string) (net.Conn, error)

	// By default talk over an UNIX socket.
	if config.BaseURL == "" {
		dial = unixDialer(config.Socket)
		baseURL = &url.URL{
			Scheme: "http",
			Host:   "localhost",
		}
	} else {
		baseURL = mylog.Check2(url.Parse(config.BaseURL))
	}

	transport := &httputil.LoggedTransport{
		Transport: &http.Transport{
			Dial:              dial,
			DisableKeepAlives: config.DisableKeepAlive,
		},
		Key:        "SNAP_CLIENT_DEBUG_HTTP",
		MayLogBody: true,
	}
	return &Client{
		baseURL:     *baseURL,
		doer:        &http.Client{Transport: transport},
		disableAuth: config.DisableAuth,
		interactive: config.Interactive,
		userAgent:   config.UserAgent,
		SetMayLogBody: func(logBody bool) {
			transport.MayLogBody = logBody
		},
	}
}

// Maintenance returns an error reflecting the daemon maintenance status or nil.
func (client *Client) Maintenance() error {
	return client.maintenance
}

// WarningsSummary returns the number of warnings that are ready to be shown to
// the user, and the timestamp of the most recently added warning (useful for
// silencing the warning alerts, and OKing the returned warnings).
func (client *Client) WarningsSummary() (count int, timestamp time.Time) {
	return client.warningCount, client.warningTimestamp
}

func (client *Client) WhoAmI() (string, error) {
	user := mylog.Check2(readAuthData())
	if os.IsNotExist(err) {
		return "", nil
	}

	return user.Email, nil
}

func (client *Client) setAuthorization(req *http.Request) error {
	user := mylog.Check2(readAuthData())
	if os.IsNotExist(err) {
		return nil
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, `Macaroon root="%s"`, user.Macaroon)
	for _, discharge := range user.Discharges {
		fmt.Fprintf(&buf, `, discharge="%s"`, discharge)
	}
	req.Header.Set("Authorization", buf.String())
	return nil
}

type RequestError struct{ error }

func (e RequestError) Error() string {
	return fmt.Sprintf("cannot build request: %v", e.error)
}

type AuthorizationError struct{ Err error }

func (e AuthorizationError) Error() string {
	return fmt.Sprintf("cannot add authorization: %v", e.Err)
}

func (e AuthorizationError) Is(target error) bool {
	_, ok := target.(AuthorizationError)
	return ok
}

type ConnectionError struct{ Err error }

func (e ConnectionError) Error() string {
	var errStr string
	switch e.Err {
	case context.DeadlineExceeded:
		errStr = "timeout exceeded while waiting for response"
	case context.Canceled:
		errStr = "request canceled"
	default:
		errStr = e.Err.Error()
	}
	return fmt.Sprintf("cannot communicate with server: %s", errStr)
}

func (e ConnectionError) Unwrap() error {
	return e.Err
}

type InternalClientError struct{ Err error }

func (e InternalClientError) Error() string {
	return fmt.Sprintf("internal error: %s", e.Err.Error())
}

func (e InternalClientError) Is(target error) bool {
	_, ok := target.(InternalClientError)
	return ok
}

// AllowInteractionHeader is the HTTP request header used to indicate
// that the client is willing to allow interaction.
const AllowInteractionHeader = "X-Allow-Interaction"

// raw performs a request and returns the resulting http.Response and
// error. You usually only need to call this directly if you expect the
// response to not be JSON, otherwise you'd call Do(...) instead.
func (client *Client) raw(ctx context.Context, method, urlpath string, query url.Values, headers map[string]string, body io.Reader) (*http.Response, error) {
	// fake a url to keep http.Client happy
	u := client.baseURL
	u.Path = path.Join(client.baseURL.Path, urlpath)
	u.RawQuery = query.Encode()
	req := mylog.Check2(http.NewRequest(method, u.String(), body))

	if client.userAgent != "" {
		req.Header.Set("User-Agent", client.userAgent)
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}
	// Content-length headers are special and need to be set
	// directly to the request. Just setting it to the header
	// will be ignored by go http.
	if clStr := req.Header.Get("Content-Length"); clStr != "" {
		cl := mylog.Check2(strconv.ParseInt(clStr, 10, 64))

		req.ContentLength = cl
	}

	if !client.disableAuth {
		mylog.
			// set Authorization header if there are user's credentials
			Check(client.setAuthorization(req))
	}

	if client.interactive {
		req.Header.Set(AllowInteractionHeader, "true")
	}

	if ctx != nil {
		req = req.WithContext(ctx)
	}

	rsp := mylog.Check2(client.doer.Do(req))

	return rsp, nil
}

// rawWithTimeout is like raw(), but sets a timeout based on opts for
// the whole of request and response (including rsp.Body() read) round
// trip. If opts is nil the default doTimeout is used.
// The caller is responsible for canceling the internal context
// to release the resources associated with the request by calling the
// returned cancel function.
func (client *Client) rawWithTimeout(ctx context.Context, method, urlpath string, query url.Values, headers map[string]string, body io.Reader, opts *doOptions) (*http.Response, context.CancelFunc, error) {
	opts = ensureDoOpts(opts)
	if opts.Timeout <= 0 {
		return nil, nil, InternalClientError{fmt.Errorf("timeout not set in options for rawWithTimeout")}
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	rsp := mylog.Check2(client.raw(ctx, method, urlpath, query, headers, body))
	if err != nil && ctx.Err() != nil {
		cancel()
		return nil, nil, ConnectionError{ctx.Err()}
	}

	return rsp, cancel, err
}

var (
	doRetry = 250 * time.Millisecond
	// snapd may need to reach out to the store, where it uses a fixed 10s
	// timeout for the whole of a single request to complete, requests are
	// retried for up to 38s in total, make sure that the client timeout is
	// not shorter than that
	doTimeout = 120 * time.Second
)

// MockDoTimings mocks the delay used by the do retry loop and request timeout.
func MockDoTimings(retry, timeout time.Duration) (restore func()) {
	oldRetry := doRetry
	oldTimeout := doTimeout
	doRetry = retry
	doTimeout = timeout
	return func() {
		doRetry = oldRetry
		doTimeout = oldTimeout
	}
}

type hijacked struct {
	do func(*http.Request) (*http.Response, error)
}

func (h hijacked) Do(req *http.Request) (*http.Response, error) {
	return h.do(req)
}

// Hijack lets the caller take over the raw http request
func (client *Client) Hijack(f func(*http.Request) (*http.Response, error)) {
	client.doer = hijacked{f}
}

type doOptions struct {
	// Timeout is the overall request timeout
	Timeout time.Duration
	// Retry interval.
	// Note for a request with a Timeout but without a retry, Retry should just
	// be set to something larger than the Timeout.
	Retry time.Duration
}

func ensureDoOpts(opts *doOptions) *doOptions {
	if opts == nil {
		// defaults
		opts = &doOptions{
			Timeout: doTimeout,
			Retry:   doRetry,
		}
	}
	return opts
}

// doNoTimeoutAndRetry can be passed to the do family to not have timeout
// nor retries.
var doNoTimeoutAndRetry = &doOptions{
	Timeout: time.Duration(-1),
}

// do performs a request and decodes the resulting json into the given
// value. It's low-level, for testing/experimenting only; you should
// usually use a higher level interface that builds on this.
func (client *Client) do(method, path string, query url.Values, headers map[string]string, body io.Reader, v interface{}, opts *doOptions) (statusCode int, err error) {
	opts = ensureDoOpts(opts)

	client.checkMaintenanceJSON()

	var rsp *http.Response
	ctx := context.Background()
	if opts.Timeout <= 0 {
		// no timeout and retries
		rsp = mylog.Check2(client.raw(ctx, method, path, query, headers, body))
	} else {
		if opts.Retry <= 0 {
			return 0, InternalClientError{fmt.Errorf("retry setting %s invalid", opts.Retry)}
		}
		retry := time.NewTicker(opts.Retry)
		defer retry.Stop()
		timeout := time.NewTimer(opts.Timeout)
		defer timeout.Stop()

		for {
			var cancel context.CancelFunc
			// use the same timeout as for the whole of the retry
			// loop to error out the whole do() call when a single
			// request exceeds the deadline
			rsp, cancel = mylog.Check3(client.rawWithTimeout(ctx, method, path, query, headers, body, opts))
			if err == nil {
				defer cancel()
			}
			if err == nil || shouldNotRetryError(err) || method != "GET" {
				break
			}
			select {
			case <-retry.C:
				continue
			case <-timeout.C:
			}
			break
		}
	}

	defer rsp.Body.Close()

	if v != nil {
		mylog.Check(decodeInto(rsp.Body, v))
	}

	return rsp.StatusCode, nil
}

func shouldNotRetryError(err error) bool {
	return errors.Is(err, AuthorizationError{}) ||
		errors.Is(err, InternalClientError{})
}

func decodeInto(reader io.Reader, v interface{}) error {
	dec := json.NewDecoder(reader)
	mylog.Check(dec.Decode(v))

	return nil
}

// doSync performs a request to the given path using the specified HTTP method.
// It expects a "sync" response from the API and on success decodes the JSON
// response payload into the given value using the "UseNumber" json decoding
// which produces json.Numbers instead of float64 types for numbers.
func (client *Client) doSync(method, path string, query url.Values, headers map[string]string, body io.Reader, v interface{}) (*ResultInfo, error) {
	return client.doSyncWithOpts(method, path, query, headers, body, v, nil)
}

// checkMaintenanceJSON checks if there is a maintenance.json file written by
// snapd the daemon that positively identifies snapd as being unavailable due to
// maintenance, either for snapd restarting itself to update, or rebooting the
// system to update the kernel or base snap, etc. If there is ongoing
// maintenance, then the maintenance object on the client is set appropriately.
// note that currently checkMaintenanceJSON does not return errors, such that
// if the file is missing or corrupt or empty, nothing will happen and it will
// be silently ignored
func (client *Client) checkMaintenanceJSON() {
	f := mylog.Check2(os.Open(dirs.SnapdMaintenanceFile))
	// just continue if we can't read the maintenance file

	defer f.Close()

	// we have a maintenance file, try to read it
	maintenance := &Error{}
	mylog.Check(json.NewDecoder(f).Decode(&maintenance))
	// if the json is malformed, just ignore it for now, we only use it for
	// positive identification of snapd down for maintenance

	if maintenance != nil {
		switch maintenance.Kind {
		case ErrorKindDaemonRestart:
			client.maintenance = maintenance
		case ErrorKindSystemRestart:
			client.maintenance = maintenance
		}
		// don't set maintenance for other kinds, as we don't know what it
		// is yet

		// this also means an empty json object in maintenance.json doesn't get
		// treated as a real maintenance downtime for example
	}
}

func (client *Client) doSyncWithOpts(method, path string, query url.Values, headers map[string]string, body io.Reader, v interface{}, opts *doOptions) (*ResultInfo, error) {
	// first check maintenance.json to see if snapd is down for a restart, and
	// set cli.maintenance as appropriate, then perform the request
	// TODO: it would be a nice thing to skip the request if we know that snapd
	// won't respond and return a specific error, but that's a big behavior
	// change we probably shouldn't make right now, not to mention it probably
	// requires adjustments in other areas too
	client.checkMaintenanceJSON()

	var rsp response
	statusCode := mylog.Check2(client.do(method, path, query, headers, body, &rsp, opts))
	mylog.Check(rsp.err(client, statusCode))

	if rsp.Type != "sync" {
		return nil, fmt.Errorf("expected sync response, got %q", rsp.Type)
	}

	if v != nil {
		mylog.Check(jsonutil.DecodeWithNumber(bytes.NewReader(rsp.Result), v))
	}

	client.warningCount = rsp.WarningCount
	client.warningTimestamp = rsp.WarningTimestamp

	return &rsp.ResultInfo, nil
}

func (client *Client) doAsync(method, path string, query url.Values, headers map[string]string, body io.Reader) (changeID string, err error) {
	_, changeID = mylog.Check3(client.doAsyncFull(method, path, query, headers, body, nil))
	return
}

func (client *Client) doAsyncFull(method, path string, query url.Values, headers map[string]string, body io.Reader, opts *doOptions) (result json.RawMessage, changeID string, err error) {
	var rsp response
	statusCode := mylog.Check2(client.do(method, path, query, headers, body, &rsp, opts))
	mylog.Check(rsp.err(client, statusCode))

	if rsp.Type != "async" {
		return nil, "", fmt.Errorf("expected async response for %q on %q, got %q", method, path, rsp.Type)
	}
	if statusCode != 202 {
		return nil, "", fmt.Errorf("operation not accepted")
	}
	if rsp.Change == "" {
		return nil, "", fmt.Errorf("async response without change reference")
	}

	return rsp.Result, rsp.Change, nil
}

type ServerVersion struct {
	Version     string
	Series      string
	OSID        string
	OSVersionID string
	OnClassic   bool

	KernelVersion  string
	Architecture   string
	Virtualization string
}

func (client *Client) ServerVersion() (*ServerVersion, error) {
	sysInfo := mylog.Check2(client.SysInfo())

	return &ServerVersion{
		Version:     sysInfo.Version,
		Series:      sysInfo.Series,
		OSID:        sysInfo.OSRelease.ID,
		OSVersionID: sysInfo.OSRelease.VersionID,
		OnClassic:   sysInfo.OnClassic,

		KernelVersion:  sysInfo.KernelVersion,
		Architecture:   sysInfo.Architecture,
		Virtualization: sysInfo.Virtualization,
	}, nil
}

// A response produced by the REST API will usually fit in this
// (exceptions are the icons/ endpoints obvs)
type response struct {
	Result json.RawMessage `json:"result"`
	Type   string          `json:"type"`
	Change string          `json:"change"`

	WarningCount     int       `json:"warning-count"`
	WarningTimestamp time.Time `json:"warning-timestamp"`

	ResultInfo

	Maintenance *Error `json:"maintenance"`
}

// Error is the real value of response.Result when an error occurs.
type Error struct {
	Kind    ErrorKind   `json:"kind"`
	Value   interface{} `json:"value"`
	Message string      `json:"message"`

	StatusCode int
}

func (e *Error) Error() string {
	return e.Message
}

// IsRetryable returns true if the given error is an error
// that can be retried later.
func IsRetryable(err error) bool {
	switch e := err.(type) {
	case *Error:
		return e.Kind == ErrorKindSnapChangeConflict
	}
	return false
}

// IsTwoFactorError returns whether the given error is due to problems
// in two-factor authentication.
func IsTwoFactorError(err error) bool {
	e, ok := err.(*Error)
	if !ok || e == nil {
		return false
	}

	return e.Kind == ErrorKindTwoFactorFailed || e.Kind == ErrorKindTwoFactorRequired
}

// IsInterfacesUnchangedError returns whether the given error means the requested
// change to interfaces was not made, because there was nothing to do.
func IsInterfacesUnchangedError(err error) bool {
	e, ok := err.(*Error)
	if !ok || e == nil {
		return false
	}
	return e.Kind == ErrorKindInterfacesUnchanged
}

// IsAssertionNotFoundError returns whether the given error means that the
// assertion wasn't found and thus the device isn't ready/seeded.
func IsAssertionNotFoundError(err error) bool {
	e, ok := err.(*Error)
	if !ok || e == nil {
		return false
	}

	return e.Kind == ErrorKindAssertionNotFound
}

// OSRelease contains information about the system extracted from /etc/os-release.
type OSRelease struct {
	ID        string `json:"id"`
	VersionID string `json:"version-id,omitempty"`
}

// RefreshInfo contains information about refreshes.
type RefreshInfo struct {
	// Timer contains the refresh.timer setting.
	Timer string `json:"timer,omitempty"`
	// Schedule contains the legacy refresh.schedule setting.
	Schedule string `json:"schedule,omitempty"`
	Last     string `json:"last,omitempty"`
	Hold     string `json:"hold,omitempty"`
	Next     string `json:"next,omitempty"`
}

// SysInfo holds system information
type SysInfo struct {
	Series    string    `json:"series,omitempty"`
	Version   string    `json:"version,omitempty"`
	BuildID   string    `json:"build-id"`
	OSRelease OSRelease `json:"os-release"`
	OnClassic bool      `json:"on-classic"`
	Managed   bool      `json:"managed"`

	KernelVersion  string `json:"kernel-version,omitempty"`
	Architecture   string `json:"architecture,omitempty"`
	Virtualization string `json:"virtualization,omitempty"`

	Refresh         RefreshInfo         `json:"refresh,omitempty"`
	Confinement     string              `json:"confinement"`
	SandboxFeatures map[string][]string `json:"sandbox-features,omitempty"`

	Features map[string]features.FeatureInfo `json:"features,omitempty"`
}

func (rsp *response) err(cli *Client, statusCode int) error {
	if cli != nil {
		maintErr := rsp.Maintenance
		// avoid setting to (*client.Error)(nil)
		if maintErr != nil {
			cli.maintenance = maintErr
		} else {
			cli.maintenance = nil
		}
	}
	if rsp.Type != "error" {
		return nil
	}
	var resultErr Error
	mylog.Check(json.Unmarshal(rsp.Result, &resultErr))
	if err != nil || resultErr.Message == "" {
		return fmt.Errorf("server error: %q", http.StatusText(statusCode))
	}
	resultErr.StatusCode = statusCode

	return &resultErr
}

func parseError(r *http.Response) error {
	var rsp response
	if r.Header.Get("Content-Type") != "application/json" {
		return fmt.Errorf("server error: %q", r.Status)
	}

	dec := json.NewDecoder(r.Body)
	mylog.Check(dec.Decode(&rsp))
	mylog.Check(rsp.err(nil, r.StatusCode))
	if err == nil {
		return fmt.Errorf("server error: %q", r.Status)
	}
	return err
}

// SysInfo gets system information from the REST API.
func (client *Client) SysInfo() (*SysInfo, error) {
	var sysInfo SysInfo

	opts := &doOptions{
		Timeout: 25 * time.Second,
		Retry:   doRetry,
	}
	mylog.Check2(client.doSyncWithOpts("GET", "/v2/system-info", nil, nil, nil, &sysInfo, opts))

	return &sysInfo, nil
}

type debugAction struct {
	Action string      `json:"action"`
	Params interface{} `json:"params,omitempty"`
}

// Debug is only useful when writing test code, it will trigger
// an internal action with the given parameters.
func (client *Client) Debug(action string, params interface{}, result interface{}) error {
	body := mylog.Check2(json.Marshal(debugAction{
		Action: action,
		Params: params,
	}))

	_ = mylog.Check2(client.doSync("POST", "/v2/debug", nil, nil, bytes.NewReader(body), result))
	return err
}

func (client *Client) DebugGet(aspect string, result interface{}, params map[string]string) error {
	urlParams := url.Values{"aspect": []string{aspect}}
	for k, v := range params {
		urlParams.Set(k, v)
	}
	_ := mylog.Check2(client.doSync("GET", "/v2/debug", urlParams, nil, nil, &result))
	return err
}

type SystemRecoveryKeysResponse struct {
	RecoveryKey  string `json:"recovery-key"`
	ReinstallKey string `json:"reinstall-key,omitempty"`
}

func (client *Client) SystemRecoveryKeys(result interface{}) error {
	_ := mylog.Check2(client.doSync("GET", "/v2/system-recovery-keys", nil, nil, nil, &result))
	return err
}

func (c *Client) MigrateSnapHome(snaps []string) (changeID string, err error) {
	body := mylog.Check2(json.Marshal(struct {
		Action string   `json:"action"`
		Snaps  []string `json:"snaps"`
	}{
		Action: "migrate-home",
		Snaps:  snaps,
	}))

	return c.doAsync("POST", "/v2/debug", nil, nil, bytes.NewReader(body))
}
