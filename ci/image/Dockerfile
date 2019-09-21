FROM golang:1

ENV DEBIAN_FRONTEND=noninteractive
ENV GOPATH=/root/gopath
ENV PATH=$GOPATH/bin:$PATH
ENV GOFLAGS="-mod=readonly"
ENV PAGER=cat
ENV CI=true

RUN apt-get update && \
  apt-get install -y shellcheck npm chromium && \
  npm install -g prettier

# https://github.com/golang/go/wiki/WebAssembly#running-tests-in-the-browser
RUN go get github.com/agnivade/wasmbrowsertest && \
  mv $GOPATH/bin/wasmbrowsertest $GOPATH/bin/go_js_wasm_exec

RUN git config --global color.ui always

# Cache go modules and build cache.
COPY . /tmp/websocket
RUN cd /tmp/websocket && \
  CI= ./ci/run.sh && \
  rm -rf /tmp/websocket
