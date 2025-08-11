ARG IMAGE
ARG TAG
ARG PACKAGING_DIR
FROM ${IMAGE}:${TAG}

RUN zypper --gpg-auto-import-keys refresh && \
    zypper in -y --no-recommends --allow-unsigned-rpm rpm-build rpmdevtools go git

ARG PACKAGING_DIR
COPY packaging/${PACKAGING_DIR}/snapd.spec .

RUN zypper in -y --no-recommends --allow-unsigned-rpm $(rpmspec -q --buildrequires snapd.spec)