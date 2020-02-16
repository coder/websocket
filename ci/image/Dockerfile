FROM golang:1

RUN apt-get update
RUN apt-get install -y chromium npm shellcheck

ARG SHFMT_URL=https://github.com/mvdan/sh/releases/download/v3.0.1/shfmt_v3.0.1_linux_amd64
RUN curl -L $SHFMT_URL > /usr/local/bin/shfmt && chmod +x /usr/local/bin/shfmt

ENV GOFLAGS="-mod=readonly"
ENV CI=true
ENV MAKEFLAGS="--jobs=16 --output-sync=target"

RUN npm install -g prettier
RUN go get golang.org/x/tools/cmd/stringer
RUN go get golang.org/x/tools/cmd/goimports
RUN go get golang.org/x/lint/golint
RUN go get github.com/agnivade/wasmbrowsertest
RUN go get github.com/mattn/goveralls
