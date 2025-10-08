#!/bin/sh

# This is a non-interactive version of the ypinit script included with
# the nis package

YPMAPDIR=/var/yp
YPBINDIR=/usr/lib/yp

if ! HOST=$($YPBINDIR/yphelper --hostname); then
        echo "Can't get local host's name.  Please check your path."
        exit 1
fi

if [ -z "$HOST" ]; then
        echo "The local host's name hasn't been set.  Please set it."
        exit 1
fi

if ! DOMAIN=$(domainname); then
        echo "Can't find domainname. Please fix your PATH"
        exit 1
fi
if [ "${DOMAIN}x" = "x" ] || [ "${DOMAIN}" = "(none)" ]; then
        echo "The local host's domain name hasn't been set.  Please set it."
        exit 1
fi

if [ ! -d "$YPMAPDIR" ] || [ -f "$YPMAPDIR" ]; then
        echo "The directory $YPMAPDIR doesn't exist."
        echo "Create it or run make install-* from the sourcen."
        exit 1
fi

mkdir -p $YPMAPDIR/"$DOMAIN"

rm -f $YPMAPDIR/"$DOMAIN"/*

echo "$HOST" >$YPMAPDIR/ypservers

echo "We need a few minutes to build the databases..."
echo "Building $YPMAPDIR/$DOMAIN/ypservers..."

# shellcheck disable=SC2002
if ! cat $YPMAPDIR/ypservers | awk '{print $0, $0}' | $YPBINDIR/makedbm - $YPMAPDIR/"$DOMAIN"/ypservers; then
  echo "Couldn't build yp data base $YPMAPDIR/$DOMAIN/ypservers."
  echo "Please fix it."
fi

echo "Running $YPMAPDIR/Makefile..."
if ! cd $YPMAPDIR && make NOPUSH=true; then
  echo "Error running Makefile."
  echo "Please try it by hand."
else
  echo ""
  echo "$HOST has been set up as a NIS server."
fi

