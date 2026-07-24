# Stage 1: build frontend
FROM node:20-alpine AS frontend-builder

WORKDIR /web-ui
COPY web-ui/package.json web-ui/package-lock.json ./
RUN npm ci
COPY web-ui/ ./
RUN npm run build

# Stage 2: build Go server
FROM golang:1.22-alpine AS builder

ENV GOPROXY=https://goproxy.cn,https://goproxy.io,direct
ENV GOSUMDB=sum.golang.google.cn

WORKDIR /app

COPY go-server/go.mod go-server/go.sum ./
RUN go mod download

COPY go-server/ ./
COPY --from=frontend-builder /web-ui/dist ./static/
RUN CGO_ENABLED=0 go build -o /lab-server .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /lab-server /lab-server

EXPOSE 8000

CMD ["/lab-server"]
