#!/bin/sh

if [ gpg --version | MATCH "gpg (GnuPG) 1." ]; then
    echo "fake gpg pinentry not used for gpg v1"
else
    echo "setup fake gpg pinentry environment for gpg v2 or higher"
    mkdir -p ~/.gnupg
    echo pinentry-program "$TESTSLIB/pinentry-fake.sh" > ~/.gnupg/gpg-agent.conf
fi
