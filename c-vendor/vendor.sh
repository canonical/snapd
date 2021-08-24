#!/bin/sh

set -e

# XXX: provide a nice declarative format
# XXX2: reuse vendor.json and write some python3 code?
if [ ! -d ./squashfuse ]; then
    git clone https://github.com/snapcore/squashfuse
fi
(cd squashfuse && git checkout 319f6d41a0419465a55d9dcb848d2408b97764f9)


# XXX: also checkout squashfuse3 and build with that depending on
# host env
