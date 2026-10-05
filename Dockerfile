# syntax=docker/dockerfile:1

# The map is built first: it changes more often than the API, and its
# output is the only thing the final image needs from Node.
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Only the Go sources are copied, so a README or web/ edit does not
# invalidate the module download or the build cache.
FROM golang:1.26-alpine AS api
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
ENV CGO_ENABLED=0
# -trimpath keeps build paths out of the binary; -s -w drop the symbol and
# DWARF tables, which this service has no use for in production.
RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=api /out/api /app/api
COPY --from=web /web/dist /app/web/dist
# A deployed image is production by construction: it logs JSON to
# stdout, and its trackers answer to ANALYTICS_ENABLED alone, off until
# that is "true". Railway injects PORT; see internal/config.listenAddr.
ENV APP_ENV=production \
    WEB_DIR=/app/web/dist
USER nobody
CMD ["/app/api"]
