// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !cgo

package osutil

// The builtin os/user functions only look at /etc/passwd and
// /etc/group when building without cgo.
//
// So if something extra is configured via nsswitch.conf, like
// extrausers those are not searched with the standard user.Lookup()
// which is used in find{Uid,Gid}NoGetenvFallback.
//
// To fix this behavior we use find{Uid,Gid}WithGetentFallback() that
// perform a 'getent <database> <name>' automatically if no local user
// is found and getent will use the libc nss functions to search all
// configured data sources.

var (
	findUid = findUidWithGetentFallback
	findGid = findGidWithGetentFallback
)
