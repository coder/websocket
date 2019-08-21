FROM archlinux/base

RUN pacman -Syu --noconfirm

RUN pacman -S --noconfirm go git shellcheck tar openssh gzip ca-certificates

RUN pacman -S --noconfirm gcc python2-pip && \
  pip2 install autobahntestsuite

RUN pacman -S --noconfirm npm && \
  npm install -g prettier

ENV GOFLAGS -mod=readonly