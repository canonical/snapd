#!/bin/sh

echo "setup fake gpg pinentry environment"
mkdir -p ~/.snap/gnupg/
echo pinentry-program "$TESTSLIB/pinentry-fake.sh" > ~/.snap/gnupg/gpg-agent.conf
chmod -R go-rwx ~/.snap
