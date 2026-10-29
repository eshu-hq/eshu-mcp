# eshu-mcp pre-cutover image: full read-path dispatch over core HTTP contracts.
# Runtime base is digest-pinned for reproducible rebuilds; refresh with:
#   TOKEN=$(curl -s "https://gcr.io/v2/token?service=gcr.io&scope=repository:distroless/static:pull" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
#   curl -s -D - -o /dev/null -H "Authorization: Bearer $TOKEN" \
#     -H "Accept: application/vnd.oci.image.index.v1+json" \
#     https://gcr.io/v2/distroless/static/manifests/nonroot | grep -i docker-content-digest
FROM golang:1.26.10-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/eshu-mcp ./cmd/eshu-mcp

FROM gcr.io/distroless/static:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
COPY --from=build /out/eshu-mcp /eshu-mcp
EXPOSE 8080
ENTRYPOINT ["/eshu-mcp"]
