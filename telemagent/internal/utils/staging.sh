#!/bin/bash
# use this script in a fresh lxd container
set -ex

cd ~

sudo apt update
sudo snap install go --classic

export GOPATH=$(pwd)
export PATH=$PATH:$GOPATH/bin

echo "get snapd source"
git clone -b reverse-id https://github.com/Omar-Hatem-Canonical/snapd src/github.com/snapcore/snapd
cd src/github.com/snapcore/snapd
sudo apt -y build-dep ./

echo "build binaries with staging keys"
go build -tags withstagingkeys -o snap-stg github.com/snapcore/snapd/cmd/snap
go build -tags withstagingkeys -o snapd-stg github.com/snapcore/snapd/cmd/snapd

sudo systemctl stop snapd.service snapd.socket

sudo mv /usr/lib/snapd/snapd{,.orig}
sudo mv /usr/bin/snap{,.orig}
sudo cp -f snapd-stg /usr/lib/snapd/snapd
sudo cp -f snap-stg /usr/bin/snap

# just in case, remove all local snapd keys
sudo rm -rf /var/lib/snapd/assertions/asserts-v0
sudo rm -f /var/lib/snapd/state.json

sudo mv /usr/lib/snapd/snap-seccomp{,.orig}

cd ${GOPATH}/src/github.com/snapcore/snapd/cmd/snap-seccomp
go build -v
sudo cp snap-seccomp /usr/lib/snapd/
cd -

# customize environment
sudo su -c 'echo "SNAPPY_USE_STAGING_STORE=1" >> /etc/environment'
sudo su -c 'echo "SNAPD_DEBUG=1" >> /etc/environment'
sudo su -c 'echo "SNAPD_DEBUG_HTTP=7" >> /etc/environment'
sudo su -c 'echo "SNAPPY_TESTING=1" >> /etc/environment'

sudo systemctl start snapd.service snapd.socket

echo "done, you can use snap with staging now"