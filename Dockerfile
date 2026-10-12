# Optional container image for an always-on server on your own network.
#   docker build -t canute-qr .
#   docker run -p 127.0.0.1:8080:8080 -v canute-qr-data:/data canute-qr
# Publish on 127.0.0.1 unless other devices should reach it; the UI has no
# password. Add the host names you will browse to with --allowed-hosts.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /canute-qr ./cmd/canute-qr

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /canute-qr /canute-qr
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/canute-qr"]
CMD ["serve", "--addr", "0.0.0.0:8080", "--data-dir", "/data", "--allowed-hosts", "localhost:8080,127.0.0.1:8080"]
