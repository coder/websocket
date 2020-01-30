FROM golang:1

RUN apt-get update
RUN apt-get install -y chromium

ENV GOFLAGS="-mod=readonly"
ENV PAGER=cat
ENV CI=true
ENV MAKEFLAGS="--jobs=16 --output-sync=target"

RUN npm install -g prettier
RUN go get golang.org/x/tools/cmd/stringer
RUN go get golang.org/x/tools/cmd/goimports
RUN go get golang.org/x/lint/golint
RUN go get github.com/agnivade/wasmbrowsertest
RUN go get github.com/mattn/goveralls
