FROM golang:1-alpine

RUN apk add --update bash git

# Autobahn Test suite.
RUN apk add py2-pip python2-dev gcc libffi-dev libc-dev openssl-dev && \
  pip install autobahntestsuite

# prettier.
RUN apk add npm && npm install -g prettier

# shellcheck
RUN apk add xz curl && \
  curl -o /tmp/shellcheck.tar.xz -L https://storage.googleapis.com/shellcheck/shellcheck-stable.linux.x86_64.tar.xz && \
  tar -xf /tmp/shellcheck.tar.xz && \
  ln -s "${PWD}/shellcheck-stable/shellcheck" /usr/local/bin/shellcheck