FROM golang:1.12

LABEL "com.github.actions.name"="vet"
LABEL "com.github.actions.description"="Run go vet"
LABEL "com.github.actions.icon"="code"
LABEL "com.github.actions.color"="purple"

COPY entrypoint.sh /entrypoint.sh

RUN apt update && apt install -y shellcheck

CMD ["/entrypoint.sh"]
