FROM golang:1.12

LABEL "com.github.actions.name"="go vet"
LABEL "com.github.actions.description"="Ensure code is somewhat semantically correct"
LABEL "com.github.actions.icon"="code"
LABEL "com.github.actions.color"="purple"

COPY entrypoint.sh /entrypoint.sh

CMD ["/entrypoint.sh"]
