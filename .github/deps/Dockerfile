FROM golang:1.12

LABEL "com.github.actions.name"="deps"
LABEL "com.github.actions.description"="Ensure synced go.mod and go.sum"
LABEL "com.github.actions.icon"="code"
LABEL "com.github.actions.color"="purple"

COPY entrypoint.sh /entrypoint.sh

CMD ["/entrypoint.sh"]
