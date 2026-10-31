# syntax=docker/dockerfile:1
# Arth API + worker image. Build context: apps/api
#   docker build -t arth-api:dev apps/api
#   docker run --rm -p 8080:8080 --env-file apps/api/.env arth-api:dev            # serve
#   docker run --rm --env-file apps/api/.env arth-api:dev migrate up               # migrations
#   docker run --rm --env-file apps/api/.env --entrypoint /usr/local/bin/worker arth-api:dev

FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "-s -w" -o /out/api ./cmd/api && \
    go build -ldflags "-s -w" -o /out/worker ./cmd/worker

# distroless static: CA certificates + /etc/passwd with a nonroot user, no shell.
# Timezone data is compiled into the binaries (import _ "time/tzdata").
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /usr/local/bin/api
COPY --from=build /out/worker /usr/local/bin/worker
USER nonroot:nonroot
ENV APP_ENV=production PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]
