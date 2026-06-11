ARG GO_VERSION=1.26.3

FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src

RUN apk add --no-cache ca-certificates build-base pkgconf

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -tags musl -o /out/server ./cmd/server
RUN CGO_ENABLED=1 GOOS=linux go build -tags musl -o /out/worker ./cmd/worker

FROM alpine:3.22

WORKDIR /app

RUN apk add --no-cache ca-certificates libstdc++

COPY --from=build /out/server /app/server
COPY --from=build /out/worker /app/worker
COPY web/frontend/src/assets/default_avatar.png /app/default_avatar.png

ENV DEFAULT_AVATAR_PATH=/app/default_avatar.png

EXPOSE 8080

ENTRYPOINT ["/app/server"]
