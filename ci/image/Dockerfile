FROM golang:1

ENV DEBIAN_FRONTEND=noninteractive
ENV GOPATH /root/gopath
ENV GOFLAGS -mod=readonly

RUN apt-get update && \
  apt-get install -y shellcheck python-pip npm && \
  pip2 install autobahntestsuite && \
  npm install -g prettier

