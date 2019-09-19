FROM golang:1

ENV DEBIAN_FRONTEND=noninteractive
ENV GOPATH=/root/gopath
ENV GOFLAGS="-mod=readonly"
ENV PAGER=cat
ENV CI=true

RUN apt-get update && \
  apt-get install -y shellcheck npm && \
  npm install -g prettier

RUN git config --global color.ui always

# Cache go modules.
COPY . /tmp/websocket
RUN cd /tmp/websocket && \
  go mod download && \
  rm -rf /tmp/websocket
