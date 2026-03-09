FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY server/ .
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

FROM alpine:latest
COPY --from=builder /build/server /server
EXPOSE 8080
CMD ["/server"]
