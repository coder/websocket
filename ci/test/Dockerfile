FROM golang:1.12

LABEL "com.github.actions.name"="test"
LABEL "com.github.actions.description"=""
LABEL "com.github.actions.icon"="code"
LABEL "com.github.actions.color"="green"

RUN apt update && \
	apt install -y shellcheck python-pip && \
	pip install autobahntestsuite

COPY entrypoint.sh /entrypoint.sh

CMD ["/entrypoint.sh"]
