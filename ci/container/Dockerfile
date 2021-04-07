FROM golang

RUN apt-get update
RUN apt-get install -y npm shellcheck chromium

ENV GO111MODULE=on
RUN go get golang.org/x/tools/cmd/goimports
RUN go get mvdan.cc/sh/v3/cmd/shfmt
RUN go get golang.org/x/tools/cmd/stringer
RUN go get golang.org/x/lint/golint
RUN go get github.com/agnivade/wasmbrowsertest

RUN npm --unsafe-perm=true install -g prettier
RUN npm --unsafe-perm=true install -g netlify-cli
