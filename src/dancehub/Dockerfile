FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum* ./
RUN --mount=type=cache,id=dancehub-go-mod,target=/go/pkg/mod go mod download && go mod verify
COPY . .
ARG SERVICE
RUN --mount=type=cache,id=dancehub-go-mod,target=/go/pkg/mod --mount=type=cache,id=dancehub-go-build,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/service ./cmd/${SERVICE}
RUN --mount=type=cache,id=dancehub-go-mod,target=/go/pkg/mod --mount=type=cache,id=dancehub-go-build,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/service /service
COPY --from=builder /out/healthcheck /healthcheck
USER 65532:65532
ENTRYPOINT ["/service"]
