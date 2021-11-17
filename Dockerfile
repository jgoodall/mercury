FROM golang:1.17 AS build-env
RUN mkdir -p /code && chmod 0777 /code
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
    protobuf-compiler libprotobuf-dev \
    libpcap0.8 libpcap-dev \
    && rm -rf /var/lib/apt/lists/* \
    && curl -sL https://github.com/magefile/mage/releases/download/v1.10.0/mage_1.10.0_Linux-64bit.tar.gz | tar xzf - -C /usr/local/bin
WORKDIR /code
COPY ./go.mod ./go.sum ./
RUN go mod download
COPY . .
RUN mage -v build

FROM debian:bullseye
RUN apt-get update && apt-get install -y --no-install-recommends libpcap0.8
COPY --from=build-env /code/bin/mercury-linux-amd64 /mercury
WORKDIR /
EXPOSE 7123
EXPOSE 8123
ENTRYPOINT ["/mercury"]
CMD ["help"]
