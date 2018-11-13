#!/bin/bash

if gpg --version | MATCH 'gpg \(GnuPG\) 1.'; then
    echo "fake gpg pinentry not used for gpg v1"
else
    echo "setup fake gpg pinentry environment for gpg v2 or higher"
    gpgdir=~/.snap/gnupg
    if gpg --version | MATCH 'gpg \(GnuPG\) 2.0'; then
        gpgdir=~/.gnupg
    fi
    mkdir -p "$gpgdir"
    # It is just created once by gpg-agent, then if this dir does not exist gpg will
    # fail to create the key
    mkdir -p "$gpgdir/private-keys-v1.d"
    echo pinentry-program "$TESTSLIB/pinentry-fake.sh" > "$gpgdir/gpg-agent.conf"
    chmod -R go-rwx "$gpgdir"
    if [ -d ~/.snap ]; then
        chmod -R go-rwx ~/.snap
    fi
fi

