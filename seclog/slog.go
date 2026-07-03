// -*- Mode: Go; indent-tabs-mode: t -*-

// go1.21 is required for log/slog which was added in Go 1.21.
// See https://go.dev/doc/go1.21#slog
// The noslog tag allows excluding the slog-based logger entirely.
//go:build go1.21 && !noslog

/*
 * Copyright (C) 2026 Canonical Ltd
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

package seclog

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/snapcore/snapd/logger"
)

// NewSlogLogger creates a new security logger backed by log/slog that
// writes structured JSON to writer. Events at or above minLevel are
// emitted; lower-level events are silently discarded.
func NewSlogLogger(writer io.Writer, appID string, minLevel Level) SecurityLogger {
	var handler slog.Handler = newJsonHandler(writer, slog.Level(minLevel))

	logger := &slogLogger{
		// always include app_id and type
		logger: slog.New(handler).With(
			slog.String("app_id", appID),
			slog.String("type", "security"),
		),
	}
	return logger
}

// Ensure [slogLogger] implements [SecurityLogger].
var _ SecurityLogger = (*slogLogger)(nil)

// slogLogger implements [SecurityLogger] and is constructed by
// [NewSlogLogger]. It wraps a [slog.Logger] and provides the required
// methods. The logger emits structured JSON with a predefined schema for
// built-in attributes.
type slogLogger struct {
	logger *slog.Logger
}

// LogEvent implements [SecurityLogger.LogEvent].
func (l *slogLogger) LogEvent(event Event, description string, attrs ...Attr) {
	slogAttrs := make([]slog.Attr, 0, len(attrs)+2)
	slogAttrs = append(slogAttrs,
		slog.Attr{Key: "category", Value: slog.StringValue(event.Category)},
		slog.Attr{Key: "event", Value: slog.StringValue(event.Name)},
	)
	for _, a := range attrs {
		slogAttrs = append(slogAttrs, slog.Any(a.Key, a.Value))
	}
	l.logger.LogAttrs(
		context.TODO(),
		slog.Level(event.Level),
		description,
		slogAttrs...,
	)
}

// newJsonHandler returns a slog JSON handler configured for security logs.
//
// It writes newline-delimited JSON to writer and enforces a schema for the
// built-in attributes:
//   - time:     key "datetime", formatted in UTC using [time.RFC3339Nano]
//   - level:    rendered as a string via [Level.String]
//   - message:  key "description"
//
// [NewSlogLogger] adds additional built-in attributes to the logger context:
//   - app_id:   always included with the value provided to [NewSlogLogger]
//   - type:     always included with the value "security"
//
// Additional attributes are preserved verbatim, including nested groups. The
// handler logs at or above the minLevel threshold. It does not close or sync
// writer.
func newJsonHandler(writer io.Writer, minLevel slog.Leveler) slog.Handler {
	options := &slog.HandlerOptions{
		Level: minLevel,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			switch attr.Key {
			case slog.TimeKey:
				// use "datetime" instead of default "time"
				attr.Key = "datetime"
				if t, ok := attr.Value.Any().(time.Time); ok {
					// convert to formatted string
					attr.Value = slog.StringValue(t.UTC().Format(time.RFC3339Nano))
				}
			case slog.LevelKey:
				if l, ok := attr.Value.Any().(slog.Level); ok {
					attr.Value = slog.StringValue(Level(l).String())
				}
			case slog.MessageKey:
				// use "description" instead of default "msg"
				attr.Key = "description"
			}
			return attr
		},
	}

	return &errorAwareHandler{inner: slog.NewJSONHandler(writer, options)}
}

// writeFailureThreshold is the number of consecutive write failures
// after which error reporting is suppressed.
const writeFailureThreshold = 3

// errorAwareHandler wraps a [slog.Handler] and reports write failures
// via the snapd logger. After [writeFailureThreshold] consecutive
// failures, further error messages are suppressed until a write
// succeeds.
type errorAwareHandler struct {
	inner            slog.Handler
	consecutiveFails atomic.Int32
}

func (h *errorAwareHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *errorAwareHandler) Handle(ctx context.Context, r slog.Record) error {
	if err := h.inner.Handle(ctx, r); err != nil {
		n := h.consecutiveFails.Add(1)
		if n < writeFailureThreshold {
			logger.Noticef("WARNING: security log write failed: %v", err)
		}
		if n == writeFailureThreshold {
			logger.Noticef("WARNING: security log write failed %d times, further failures will not be reported", writeFailureThreshold)
		}
		return err
	}
	if n := h.consecutiveFails.Swap(0); n >= writeFailureThreshold {
		logger.Noticef("security log write recovered following %d failures", n)
	}
	return nil
}

func (h *errorAwareHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &errorAwareHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *errorAwareHandler) WithGroup(name string) slog.Handler {
	return &errorAwareHandler{inner: h.inner.WithGroup(name)}
}

// LogValue implements [slog.LogValuer], allowing [Reason] to be
// used directly as a structured log attribute value.
func (r Reason) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("code", r.Code),
		slog.String("kind", r.Kind),
		slog.String("message", r.Message),
	)
}

// LogValue implements [slog.LogValuer], allowing [SnapdUser] to be
// used directly as a structured log attribute value.
func (u SnapdUser) LogValue() slog.Value {
	expiration := "never"
	if !u.Expiration.IsZero() {
		expiration = u.Expiration.UTC().Format(time.RFC3339Nano)
	}
	return slog.GroupValue(
		slog.Int64("snapd_user_id", u.ID),
		slog.String("store_user_name", u.StoreUserName),
		slog.String("store_user_email", u.StoreUserEmail),
		slog.String("expiration", expiration),
	)
}

// LogValue implements [slog.LogValuer], allowing [Endpoint] to be
// used directly as a structured log attribute value.
func (e Endpoint) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("method", e.Method),
		slog.String("path", e.Path),
		slog.String("action", e.Action),
		slog.String("access_checker", e.AccessChecker),
		slog.String("access_level", e.AccessLevel),
	)
}

// LogValue implements [slog.LogValuer], allowing [Peer] to be used
// directly as a structured log attribute value.
func (p Peer) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("socket", p.Socket),
		slog.Int64("uid", int64(p.UID)),
		slog.Int64("pid", int64(p.PID)),
	)
}

// LogValue implements [slog.LogValuer], allowing [Ref] to be used directly
// as a structured log attribute value.
func (r Ref) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Any("primary_key", r.PrimaryKey),
		slog.Int("revision", r.Revision),
	)
}

// LogValue implements [slog.LogValuer], allowing [AddOptions] to be
// used directly as a structured log attribute value.
func (o AddOptions) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("real_user_name", o.RealUserName),
		slog.Bool("sudoer", o.Sudoer),
		slog.Bool("extra_users", o.ExtraUsers),
		slog.Bool("force_password_change", o.ForcePasswordChange),
		slog.Bool("known", o.Known),
	}
	if o.Assertion != nil {
		attrs = append(attrs, slog.Any("assertion", o.Assertion))
	}
	return slog.GroupValue(attrs...)
}

// LogValue implements [slog.LogValuer], allowing [RemoveOptions] to be
// used directly as a structured log attribute value.
func (o RemoveOptions) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("force", o.Force),
	)
}

// LogValue implements [slog.LogValuer], allowing [AuthzChecks] to be
// used directly as a structured log attribute value.
func (a AuthzChecks) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("access_options", string(a.AccessOptions)),
		slog.String("peer_credentials", string(a.PeerCreds)),
		slog.String("socket", string(a.Socket)),
		slog.String("interface_requirements", string(a.Interface)),
		slog.String("open_access", string(a.OpenAccess)),
		slog.String("user_authentication", string(a.UserAuth)),
		slog.String("root", string(a.Root)),
		slog.String("polkit", string(a.Polkit)),
	)
}
