FROM golang:1

ENV DEBIAN_FRONTEND=noninteractive
ENV GOPATH=/root/gopath
ENV GOFLAGS="-mod=readonly"
ENV PAGER=cat

RUN apt-get update && \
  apt-get install -y shellcheck npm && \
  npm install -g prettier

RUN git config --global color.ui always
