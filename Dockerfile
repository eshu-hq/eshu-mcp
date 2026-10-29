# eshu-mcp pre-cutover image: health surface only, no tool dispatch yet.
FROM golang:1.26.10-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/eshu-mcp ./cmd/eshu-mcp

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/eshu-mcp /eshu-mcp
EXPOSE 8080
ENTRYPOINT ["/eshu-mcp"]
