# Build from the repository root: docker build -f transfer-frontend/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /transfer-frontend ./transfer-frontend

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /transfer-frontend /transfer-frontend
EXPOSE 8000/tcp
# Configuration (LISTEN, UPSTREAM, MAX_INFLIGHT, ...) comes from environment variables.
# TRANSPORT picks the file server version the gateway talks to; the default is UDP.
ENV TRANSPORT=udp GIN_MODE=release
ENTRYPOINT ["/transfer-frontend"]
