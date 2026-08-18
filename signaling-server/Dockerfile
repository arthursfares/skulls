# build stage
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /signaling-server .

# runtime stage
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && \
	adduser -D -u 10001 skulls && \
	mkdir -p /signaling-server/data && \
	chown -R skulls:skulls /signaling-server
WORKDIR /signaling-server
COPY --from=build /signaling-server /signaling-server/signaling-server
USER skulls
EXPOSE 8080
# confirm the listener is accepting connections
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
	CMD nc -z 127.0.0.1 8080 || exit 1
ENTRYPOINT ["/signaling-server/signaling-server"]