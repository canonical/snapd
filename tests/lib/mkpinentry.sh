#!/bin/sh

set -e

if gpg --version | MATCH "gpg \(GnuPG\) 1."; then
    echo "fake gpg pinentry not used for gpg v1"
else
    echo "setup fake gpg pinentry environment for gpg v2 or higher"
    case "$SPREAD_SYSTEM" in
        opensuse-*)
            mkdir -p ~/.gnupg
            echo pinentry-program "$TESTSLIB/pinentry-fake.sh" > ~/.gnupg/gpg-agent.conf
            ;;
        *)
            mkdir -p ~/.snap/gnupg/
            echo pinentry-program "$TESTSLIB/pinentry-fake.sh" > ~/.snap/gnupg/gpg-agent.conf
            chmod -R go-rwx ~/.snap
            ;;
    esac
fi
