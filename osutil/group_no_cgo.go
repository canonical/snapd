// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !cgo

package osutil

// The builtin os/user functions only look at /etc/passwd and
// /etc/group when building without cgo.
//
// So nothing configured via nsswitch.conf, like extrausers. findUid()
// and findGid() is searched. To fix this behavior we use
// find{Uid,Gid}WithGetentFallback() that perform a 'getent
// <database> <name>' automatically if no local user is found.

var (
	findUid = findUidWithGetentFallback
	findGid = findGidWithGetentFallback
)
