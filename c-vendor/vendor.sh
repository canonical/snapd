#!/bin/sh

set -e

# XXX: provide a nice declarative format
# XXX2: reuse vendor.json and write some python3 code?
if [ ! -d ./squashfuse ]; then
    git clone https://github.com/vasi/squashfuse
fi

# This is just tip/master as of Aug 30th 2021, there is no other
# specific reason to use this. It works with both "libfuse-dev" and
# "libfuse3-dev" which is important as 16.04 only have libfuse-dev
# and 21.10 only has libfuse3-dev
if [ -d ./squashfuse/.git ]; then
    (cd squashfuse && git checkout 74f4fe86ebd47a2fb7df5cb60d452354f977c72e)
fi

