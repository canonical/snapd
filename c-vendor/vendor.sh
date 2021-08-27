#!/bin/sh

set -e

# XXX: provide a nice declarative format
# XXX2: reuse vendor.json and write some python3 code?
if [ ! -d ./squashfuse ]; then
    git clone https://github.com/vasi/squashfuse
fi
(cd squashfuse && git checkout 74f4fe86ebd47a2fb7df5cb60d452354f977c72e)

