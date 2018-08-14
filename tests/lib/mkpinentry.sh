#!/bin/bash

if gpg --version | MATCH 'gpg \(GnuPG\) 1.'; then
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
            # It is just created once by gpg-agent, then if this dir does not exist gpg will
            # fail to create the key
            mkdir -p ~/.snap/gnupg/private-keys-v1.d
            echo pinentry-program "$TESTSLIB/pinentry-fake.sh" > ~/.snap/gnupg/gpg-agent.conf
            chmod -R go-rwx ~/.snap
            ;;
    esac
fi
