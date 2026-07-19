# Multi-stage build for the Go lab server.
FROM golang:1.22-alpine AS builder

# Use a reachable module proxy inside the build environment.
ENV GOPROXY=https://goproxy.cn,https://goproxy.io,direct
ENV GOSUMDB=sum.golang.google.cn

WORKDIR /app

COPY go-server/go.mod go-server/go.sum ./
RUN go mod download

COPY go-server/ ./
RUN CGO_ENABLED=0 go build -o /lab-server .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /lab-server /lab-server

EXPOSE 8000

CMD ["/lab-server"]
