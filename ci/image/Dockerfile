FROM golang:1

RUN curl -sS https://dl.yarnpkg.com/debian/pubkey.gpg | apt-key add - && \
  echo "deb https://dl.yarnpkg.com/debian/ stable main" | tee /etc/apt/sources.list.d/yarn.list

RUN curl -sL https://deb.nodesource.com/setup_12.x | bash -
RUN apt-get install -y nodejs chromium yarn

COPY ./ci/image/gitignore /etc/git/ignore
RUN git config --system color.ui always
# Need to set set this explicitly for the system since github uses HOME=/home/github.
RUN git config --system core.excludesfile /etc/git/ignore

ENV GOPATH=/root/gopath
ENV PATH=$GOPATH/bin:$PATH
ENV GOFLAGS="-mod=readonly"
ENV PAGER=cat
ENV CI=true

# Cache go modules, build cache and yarn cache.
COPY . /tmp/websocket
RUN cd /tmp/websocket && \
  yarn && CI= yarn ci && \
  rm -rf /tmp/websocket
