#!/bin/sh
if [ "$(command -v python3)" != "" ]; then
	exec python3 ./mount.py "$@";
elif [ "$(command -v python2)" != "" ]; then
	exec python2 ./mount.py "$@";
else
	echo "cannot mount: Python 2 or 3 required"
	exit 1
fi
