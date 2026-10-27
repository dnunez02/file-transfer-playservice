# Build from the repository root: docker build -f transfer-server/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /transfer-server ./transfer-server

# distroless/static includes the CA certificates the AWS SDK needs to talk to S3 over HTTPS
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /transfer-server /transfer-server
EXPOSE 8080/udp 8080/tcp 8081/tcp
# Configuration (BUCKET, PORT, HEALTH_PORT, WORKERS, ...) comes from environment variables.
# The UDP server is the default; run the TCP server with the command "tcp" instead.
ENTRYPOINT ["/transfer-server"]
CMD ["udp"]
