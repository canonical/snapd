#!/bin/sh

if autoreconf --version > /dev/null 2>&1; then : ; else
  echo "Missing autoconf"
  exit 1
fi

if aclocal --version > /dev/null 2>&1; then : ; else
  echo "Missing automake"
  exit 1
fi

if libtoolize --version > /dev/null 2>&1; then : ; else
  if glibtoolize --version > /dev/null 2>&1; then : ; else
    echo "Missing libtool"
    exit 1
  fi
fi

if pkg-config --version > /dev/null 2>&1; then : ; else
  echo "Missing pkg-config"
  exit 1
fi

exec autoreconf -i
