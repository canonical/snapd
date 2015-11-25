#!/bin/sh
# set up classic ubuntu environment on snappy
# Author: Martin Pitt <martin.pitt@ubuntu.com>

set -eu

ROOT=/writable/classic
RELEASE=$(lsb_release -sc)
ARCH=$(dpkg --print-architecture)
ROOTFS_TAR=/writable/cache/ubuntu-rootfs-${ARCH}.tar.gz

# download URL $1 to file $2
# snappy doesn't have wget or curl installed
download() {
    python3 -c "from urllib.request import urlretrieve; urlretrieve('$1', '$2')"
}

# download URL $1 and print contents on stdout
download_stdout() {
    python3 -c "from urllib.request import urlopen; print(urlopen('$1').read().decode())"
}

if [ -d "$ROOT" ]; then
    echo "$ROOT already exists" >&2
    exit 1
fi

if [ $(id -u) -ne 0 ]; then
    echo "This script needs to be called as root" >&2
    exit 1
fi

# get root fs tarball (faster than debootstrap), and unpack it
# TODO: verify signature
if [ ! -e "$ROOTFS_TAR" ]; then
    echo "Getting LXC image index..."
    dir=$(download_stdout "https://images.linuxcontainers.org/meta/1.0/index-system" | grep "^ubuntu;$RELEASE;$ARCH;default" | cut -f6 -d';')
    url="https://images.linuxcontainers.org${dir}/rootfs.tar.xz"
    echo "Downloading $url ..."
    download "$url" "$ROOTFS_TAR"
fi
sudo mkdir -p "$ROOT"
echo "Unpacking $ROOTFS_TAR into $ROOT ..."
tar -C "$ROOT" -xpf "$ROOTFS_TAR"

# copy important config
for f in hostname hosts timezone localtime; do
    cp /etc/$f /writable/classic/etc/
done

# don't start services in the chroot on apt-get install
cat <<EOF > "$ROOT/usr/sbin/policy-rc.d"
#!/bin/sh
while true; do
    case "\$1" in
      -*) shift ;;
      makedev) exit 0;;
      x11-common) exit 0;;
      *) exit 101;;
    esac
done
EOF
chmod 755 "$ROOT/usr/sbin/policy-rc.d"

# remove ubuntu user, will come from snappy OS
chroot "$ROOT" deluser ubuntu

# install extra packages; make sure chroot can resolve DNS
mkdir -p "$ROOT"/run/resolvconf/
cp -a /run/resolvconf/resolv.conf "$ROOT"/run/resolvconf/
chroot "$ROOT" apt-get install -y libnss-extrausers

# enable libnss-extrausers
sed -i -r '/^(passwd|group|shadow):/ s/$/ extrausers/' "$ROOT/etc/nsswitch.conf"

# clean up cruft
rm -rf $ROOT/run/*
