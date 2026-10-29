# eshu-mcp pre-cutover image: full read-path dispatch over core HTTP contracts.
# Both stages pin by digest (verified 2026-10-29; re-verify with a registry
# lookup when bumping the tag). The build stage is the multi-arch
# golang:1.26.10-bookworm list; the runtime stage is the multi-arch
# distroless static nonroot OCI index. Refresh the build digest with:
#   TOKEN=$(curl -s "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/golang:pull" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
#   curl -s -D - -o /dev/null -H "Authorization: Bearer $TOKEN" \
#     -H "Accept: application/vnd.oci.image.index.v1+json" \
#     https://registry-1.docker.io/v2/library/golang/manifests/1.26.10-bookworm | grep -i docker-content-digest
# Runtime base refresh:
#   TOKEN=$(curl -s "https://gcr.io/v2/token?service=gcr.io&scope=repository:distroless/static:pull" | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
#   curl -s -D - -o /dev/null -H "Authorization: Bearer $TOKEN" \
#     -H "Accept: application/vnd.oci.image.index.v1+json" \
#     https://gcr.io/v2/distroless/static/manifests/nonroot | grep -i docker-content-digest
FROM golang:1.26.10-bookworm@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb AS build
# VERSION is the release tag (release.yml sets it; local builds report dev).
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=${VERSION}" -o /out/eshu-mcp ./cmd/eshu-mcp

FROM gcr.io/distroless/static:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
COPY --from=build /out/eshu-mcp /eshu-mcp
EXPOSE 8080
ENTRYPOINT ["/eshu-mcp"]
