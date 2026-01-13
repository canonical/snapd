ARG IMAGE=amazonlinux
ARG TAG=2
ARG PACKAGING_DIR
FROM ${IMAGE}:${TAG}

RUN yum makecache && \
    yum update -y && \
    yum -y install rpm-build rpmdevtools go git

ARG PACKAGING_DIR
COPY packaging/${PACKAGING_DIR}/snapd.spec .

RUN yum -y install $(rpmspec -q --buildrequires snapd.spec)
