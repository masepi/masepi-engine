FROM golang:1.23-alpine AS build

ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/masepi ./cmd/masepi

FROM alpine:3.22
RUN apk add --no-cache ca-certificates git openssh-client tzdata
COPY server/ssh/github_known_hosts /etc/ssh/ssh_known_hosts
ENV GIT_TERMINAL_PROMPT=0 \
    GIT_SSH_COMMAND="ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=/dev/null -o GlobalKnownHostsFile=/etc/ssh/ssh_known_hosts"
COPY --from=build /out/masepi /usr/local/bin/masepi
ENTRYPOINT ["masepi"]
CMD ["publisher"]
