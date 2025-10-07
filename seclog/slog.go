// -*- Mode: Go; indent-tabs-mode: t -*-
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
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/snapcore/snapd/osutil"
)

// slogProvider implements [provider].
type slogProvider struct{}

// Ensure [slogProvider] implements [provider].
var _ provider = slogProvider{}

// New constructs an slog based [securityLogger] that emits structured JSON to the
// provided [io.Writer]. The returned logger enables dynamic level control via
// an internal [slog.LevelVar].
func (slogProvider) New(writer io.Writer, appID string, minLevel Level) securityLogger {
	return newSlogLogger(writer, appID, minLevel)
}

// Impl returns the implementation.
func (slogProvider) Impl() Impl {
	return ImplSlog
}

func init() {
	register(slogProvider{})
}

func newSlogLogger(writer io.Writer, appID string, minLevel Level) securityLogger {
	levelVar := new(slog.LevelVar)
	levelVar.Set(slog.Level(minLevel))
	var handler slog.Handler = newJsonHandler(writer, levelVar)
	if lw, ok := writer.(levelWriter); ok {
		handler = newLevelHandler(handler, lw)
	}
	logger := &slogLogger{
		// enable dynamic level adjustment
		levelVar: levelVar,
		// always include app_id and type
		logger: slog.New(handler).With(
			slog.String("app_id", appID),
			slog.String("type", "security"),
		),
	}
	return logger
}

// slogLogger implements [securityLogger] and is constructed by the
// [slogProvider]. It wraps a [slog.Logger] and provides the required
// methods. The logger emits structured JSON with a predefined schema for
// built-in attributes and supports dynamic log level control via an internal
// [slog.LevelVar]. When used with a [levelWriter] sink, it ensures that
// each message is written with the correct severity level.
type slogLogger struct {
	logger   *slog.Logger
	levelVar *slog.LevelVar
}

// Ensure [slogLogger] implements [securityLogger].
var _ securityLogger = (*slogLogger)(nil)

// SlogLogger is a test only helper to retrieve a pointer to the underlying
// [slog.Logger].
func (l *slogLogger) SlogLogger() *slog.Logger {
	osutil.MustBeTestBinary("SlogLogger() is for testing only")
	return l.logger
}

// LogLoggingEnabled implements [securityLogger.LogLoggingEnabled].
func (l *slogLogger) LogLoggingEnabled() {
	l.logger.LogAttrs(
		context.Background(),
		slog.Level(LevelInfo),
		"Security auditing enabled",
		slog.Attr{Key: "category", Value: slog.StringValue("SYS")},
		slog.Attr{Key: "event", Value: slog.StringValue("sys_logging_enabled")},
	)
}

// LogLoggingDisabled implements [securityLogger.LogLoggingDisabled].
func (l *slogLogger) LogLoggingDisabled() {
	l.logger.LogAttrs(
		context.Background(),
		slog.Level(LevelCritical),
		"Security auditing disabled",
		slog.Attr{Key: "category", Value: slog.StringValue("SYS")},
		slog.Attr{Key: "event", Value: slog.StringValue("sys_logging_disabled")},
	)
}

// LogLoginSuccess implements [securityLogger.LogLoginSuccess].
func (l *slogLogger) LogLoginSuccess(user SnapdUser) {
	l.logger.LogAttrs(
		context.Background(),
		slog.Level(LevelInfo),
		fmt.Sprintf("User %s login success", user.String()),
		slog.Attr{Key: "category", Value: slog.StringValue("AUTHN")},
		slog.Attr{Key: "event", Value: slog.StringValue("authn_login_success")},
		slog.Any("user", user),
	)
}

// LogLoginFailure implements [securityLogger.LogLoginFailure].
func (l *slogLogger) LogLoginFailure(user SnapdUser) {
	l.logger.LogAttrs(
		context.Background(),
		slog.Level(LevelWarn),
		fmt.Sprintf("User %s login failure", user.String()),
		slog.Attr{Key: "category", Value: slog.StringValue("AUTHN")},
		slog.Attr{Key: "event", Value: slog.StringValue("authn_login_failure")},
		slog.Any("user", user),
	)
}

// LogValue implements [slog.LogValuer], allowing SnapdUser to be
// used directly as a structured log attribute value.
func (u SnapdUser) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int64("snapd-user-id", u.ID),
		slog.String("system-user-name", u.SystemUserName),
		slog.String("store-user-email", u.StoreUserEmail),
		slog.String("expiration", u.Expiration.UTC().Format(time.RFC3339Nano)),
	)
}

// newJsonHandler returns a slog JSON handler configured for security logs.
//
// It writes newline-delimited JSON to writer and enforces a schema for the
// built-in attributes:
//   - time:     key "datetime", formatted in UTC using [time.RFC3339Nano]
//   - level:    rendered as a string via [Level.String]
//   - message:  key "description"
//   - app_id:   always included with the value provided to newSlogLogger
//   - type:     always included with the value "security"
//
// Additional attributes are preserved verbatim, including nested groups. The
// handler logs at or above the minLevel threshold. It does not
// close or sync writer.
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

	return slog.NewJSONHandler(writer, options)
}

// levelWriter extends [io.Writer] with per-message level control. Writers
// that implement this interface allow log handlers to set the severity for
// each message before writing.
type levelWriter interface {
	io.Writer
	SetLevel(Level)
}

// levelHandler is a [slog.Handler] wrapper that sets the level on a
// [levelWriter] before each message is handled. This ensures that the
// written output carries the correct per-message priority.
//
// All derived handlers returned by WithAttrs and WithGroup share the same
// [levelWriter] and mutex, since they write to the same sink.
type levelHandler struct {
	inner slog.Handler
	lw    levelWriter
	mu    *sync.Mutex
}

func newLevelHandler(inner slog.Handler, lw levelWriter) slog.Handler {
	return &levelHandler{inner: inner, lw: lw, mu: &sync.Mutex{}}
}

func (h *levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *levelHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lw.SetLevel(Level(r.Level))
	return h.inner.Handle(ctx, r)
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{inner: h.inner.WithAttrs(attrs), lw: h.lw, mu: h.mu}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{inner: h.inner.WithGroup(name), lw: h.lw, mu: h.mu}
}
