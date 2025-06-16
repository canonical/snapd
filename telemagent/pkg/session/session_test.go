// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package session_test

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"os"
	"testing"

	"github.com/snapcore/snapd/telemagent/pkg/session"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type sessionSuite struct {
	sess    session.Session
	snapPub session.SnapPub
	logger  *slog.Logger
}

var _ = Suite(&sessionSuite{})

func (ss *sessionSuite) SetUpSuite(c *C) {
	ss.sess = session.Session{ID: "1214", Username: "ubuntu_user", Password: []byte("pass")}
	ss.snapPub = session.SnapPub{Publisher: "canonical", Name: "multipass"}

	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	ss.logger = slog.New(logHandler)
}

func (ss *sessionSuite) TestNewContext(c *C) {
	ctx := session.NewContext(context.Background(), &ss.sess)

	c.Check(ctx, NotNil)
}

func (ss *sessionSuite) TestFromContextFail(c *C) {
	sess, ok := session.FromContext(context.Background())

	c.Check(sess, IsNil)
	c.Check(ok, Equals, false)
}

func (ss *sessionSuite) TestFromContextSuccess(c *C) {
	ctx := session.NewContext(context.Background(), &ss.sess)

	retrievedSess, ok := session.FromContext(ctx)

	c.Check(retrievedSess, NotNil)
	c.Check(ok, Equals, true)
	c.Check(retrievedSess.ID, Equals, "1214")
	c.Check(retrievedSess.Username, Equals, "ubuntu_user")
	c.Check(string(retrievedSess.Password), Equals, "pass")
}

func (ss *sessionSuite) TestFromSnapContextFail(c *C) {
	snapPub, snapName, ok := session.GetSnapFromContext(context.Background())

	c.Check(snapName, Equals, "")
	c.Check(snapPub, Equals, "")
	c.Check(ok, Equals, false)
}

func (ss *sessionSuite) TestFromProcContextSuccess(c *C) {
	ctx := session.AddSnapToContext(context.Background(), ss.snapPub.Publisher, ss.snapPub.Name)

	retrievedPub, retrievedSnap, ok := session.GetSnapFromContext(ctx)

	c.Check(retrievedPub, NotNil)
	c.Check(retrievedSnap, NotNil)
	c.Check(ok, Equals, true)
	c.Check(retrievedPub, Equals, "canonical")
	c.Check(retrievedSnap, Equals, "multipass")
}

func (ss *sessionSuite) TestLogActionFail(c *C) {
	err := session.LogAction(context.Background(), "log", nil, nil, nil, *ss.logger)

	c.Check(err, NotNil)
}

func (ss *sessionSuite) TestLogActionWithCert(c *C) {
	cert := x509.Certificate{Subject: pkix.Name{CommonName: "Hello"}}
	sessWithCert := session.Session{ID: "1214", Username: "ubuntu_user", Password: []byte("pass"), Cert: cert}

	ctx := session.NewContext(context.Background(), &sessWithCert)
	err := session.LogAction(ctx, "log", nil, nil, nil, *ss.logger)

	c.Check(err, IsNil)
}
