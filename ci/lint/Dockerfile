FROM golang:1.12

LABEL "com.github.actions.name"="lint"
LABEL "com.github.actions.description"=""
LABEL "com.github.actions.icon"="code"
LABEL "com.github.actions.color"="purple"

RUN apt update && apt install -y shellcheck

COPY entrypoint.sh /entrypoint.sh

CMD ["/entrypoint.sh"]
