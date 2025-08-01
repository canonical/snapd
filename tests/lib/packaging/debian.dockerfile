ARG SYSTEM=ubuntu
ARG TAG=24.04
ARG GOLANG_VERSION=1.18
FROM ${SYSTEM}:${TAG}

ARG SYSTEM
ARG TAG
ARG GOLANG_VERSION
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get upgrade -y

RUN if [ "$SYSTEM" = "ubuntu" ] && [ "$TAG" = "16.04" ]; then \
        apt install -y software-properties-common && \
        add-apt-repository ppa:snappy-dev/image && \
        apt update; \
    fi

RUN apt-get install -y \
    sbuild \
    devscripts \
    git

RUN if [ "$GOLANG_VERSION" = "1.21" ]; then \
        apt-get install -y golang-${GOLANG_VERSION}; \
    fi

COPY ./debian/control debian/control

RUN apt build-dep -y ./

RUN if [ -z "$(command -v go)" ]; then \
        ln -s "/usr/lib/go-${GOLANG_VERSION}/bin/go" /usr/bin/go; \
    fi

RUN useradd test -m
