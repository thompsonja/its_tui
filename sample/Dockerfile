FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY server/ .
ARG SKAFFOLD_GO_GCFLAGS
RUN CGO_ENABLED=0 GOOS=linux go build -gcflags="${SKAFFOLD_GO_GCFLAGS}" -o server .

FROM gcr.io/distroless/base-debian12
COPY --from=builder /build/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
