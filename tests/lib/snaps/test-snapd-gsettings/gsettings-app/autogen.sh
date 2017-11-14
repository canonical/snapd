#!/bin/sh

set -e

test -n "$srcdir" || srcdir=`dirname "$0"`
test -n "$srcdir" || srcdir=.

olddir=`pwd`
cd "$srcdir"

# This will run autoconf, automake, etc. for us
autoreconf --force --install

cd "$olddir"

if test -z "$NOCONFIGURE"; then
  echo "Running: $srcdir/configure" "$@"
  "$srcdir"/configure "$@"
fi