# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /out/jobfinder .

# ---- runtime stage ----
FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates && \
    addgroup -S jobfinder && \
    adduser -S -G jobfinder -H -D jobfinder

COPY --from=build /out/jobfinder /jobfinder

USER jobfinder

ENTRYPOINT ["/jobfinder"]
