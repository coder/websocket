FROM golang:1

RUN apt-get update
RUN apt-get install -y chromium
RUN apt-get install -y npm
RUN apt-get install -y shellcheck

ENV GOPATH=/root/gopath
ENV PATH=$GOPATH/bin:$PATH
ENV GOFLAGS="-mod=readonly"
ENV PAGER=cat
ENV CI=true
ENV MAKEFLAGS="-j --output-sync target"

COPY ./ci/image/gitignore /root/.config/git/ignore
RUN git config --system color.ui always

RUN npm install -g prettier
RUN go get golang.org/x/tools/cmd/stringer
RUN go get golang.org/x/tools/cmd/goimports
RUN go get mvdan.cc/sh/cmd/shfmt
RUN go get golang.org/x/lint/golint
RUN go get github.com/agnivade/wasmbrowsertest

# Cache go modules and build cache.
COPY . /tmp/websocket
RUN cd /tmp/websocket && \
  CI= make && \
  rm -rf /tmp/websocket

# GitHub actions tries to override HOME to /github/home and then
# mounts a temp directory into there. We do not want this behaviour.
# I assume it is so that $HOME is preserved between steps in a job.
ENTRYPOINT ["env", "HOME=/root"]
