#!/bin/sh
if [ "$(command -v python3)" != "" ]; then
	exec ./mount-py3.py "$@";
else
	exec ./mount-py2.py "$@";
fi
