FROM golang:1.12

LABEL "com.github.actions.name"="ensure go mod"
LABEL "com.github.actions.description"="Ensure go.mod and go.sum are up to date"
LABEL "com.github.actions.icon"="code"
LABEL "com.github.actions.color"="purple"

COPY entrypoint.sh /entrypoint.sh

CMD ["/entrypoint.sh"]
