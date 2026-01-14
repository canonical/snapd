ARG IMAGE=amazonlinux
ARG TAG=2
FROM ${IMAGE}:${TAG}
ARG PACKAGING_DIR

RUN yum makecache && \
    yum update -y && \
    yum -y install rpm-build rpmdevtools go git

COPY packaging/${PACKAGING_DIR}/snapd.spec .

RUN yum -y install $(rpmspec -q --buildrequires snapd.spec)
