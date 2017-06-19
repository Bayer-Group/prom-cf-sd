FROM alpine:3.5

ENV BUILD_VERSION 1.0.0
ENV GOROOT=/usr/lib/go \
    GOPATH=/gopath \
    GOBIN=/gopath/bin \
    PATH=$PATH:$GOROOT/bin:$GOPATH/bin

WORKDIR /gopath/src/github.com/userid/prom-cf-sd
ADD . /gopath/src/github.com/userid/prom-cf-sd

RUN apk add -U git go build-base && \
  go get -v github.com/userid/prom-cf-sd && \
  go get -v github.com/cloudfoundry-community/go-cfclient && \
  go install github.com/userid/prom-cf-sd && \
  apk del git go build-base && \
  rm -rf /gopath/pkg && \
  rm -rf /gopath/src && \
  rm -rf /var/cache/apk/*

ENTRYPOINT /gopath/bin/prom-cf-sd 

EXPOSE 8080
