FROM opensuse/leap

RUN zypper --gpg-auto-import-keys refresh && \
    zypper in -y --no-recommends --allow-unsigned-rpm rpm-build rpmdevtools go git

COPY packaging/opensuse-15.6/snapd.spec .

RUN zypper in -y --no-recommends --allow-unsigned-rpm $(rpmspec -q --buildrequires snapd.spec)