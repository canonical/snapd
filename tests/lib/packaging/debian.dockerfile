ARG SYSTEM
ARG TAG
FROM ${SYSTEM}:${TAG}

ARG SYSTEM
ARG TAG
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

COPY ./debian/control debian/control

RUN apt build-dep -y ./

RUN useradd test -m
