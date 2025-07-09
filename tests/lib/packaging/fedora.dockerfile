FROM fedora:41

RUN dnf makecache && \
    dnf -y --refresh install --setopt=install_weak_deps=False rpm-build rpmdevtools

COPY packaging/fedora-42/snapd.spec snapd.spec

RUN dnf -y --refresh install --setopt=install_weak_deps=False $(rpmspec -q --buildrequires snapd.spec)