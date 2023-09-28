FROM golang

RUN apt-get update
RUN apt-get install -y npm shellcheck chromium

ENV GO111MODULE=on
RUN go install golang.org/x/tools/cmd/goimports@latest
RUN go install mvdan.cc/sh/v3/cmd/shfmt@latest
RUN go install golang.org/x/tools/cmd/stringer@latest
RUN go install golang.org/x/lint/golint@latest
RUN go install github.com/agnivade/wasmbrowsertest@latest

RUN npm --unsafe-perm=true install -g prettier
RUN npm --unsafe-perm=true install -g netlify-cli
