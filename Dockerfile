# Build
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/norviq-mcp ./cmd/norviq-mcp

# Runtime
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/norviq-mcp /norviq-mcp
EXPOSE 8087
USER nonroot:nonroot
ENTRYPOINT ["/norviq-mcp"]
