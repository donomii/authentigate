# syntax=docker/dockerfile:1
FROM golang:1.24-alpine AS build

WORKDIR /src
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /out/authentigate .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /out/menu ./modules/menu
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -o /out/cameraview ./modules/cameraview

FROM alpine:3.20

RUN apk add --no-cache ca-certificates libcap

WORKDIR /app

COPY --from=build /src/files /app/files
COPY --from=build /src/modules /app/modules
COPY --from=build /src/config.json /app/config.json
COPY --from=build /src/provider_secrets.json.example /app/provider_secrets.json.example
COPY --from=build /src/favicon.ico /app/favicon.ico
COPY --from=build /src/docker/entrypoint.sh /app/entrypoint.sh

COPY --from=build /out/authentigate /app/authentigate
COPY --from=build /out/menu /app/modules/menu/menu
COPY --from=build /out/cameraview /app/modules/cameraview/cameraview

RUN addgroup -S app \
    && adduser -S -G app app \
    && mkdir -p /app/.certs \
    && chown -R app:app /app \
    && chmod +x /app/entrypoint.sh \
    && setcap 'cap_net_bind_service=+ep' /app/authentigate

USER app

EXPOSE 443 8081

ENTRYPOINT ["/app/entrypoint.sh"]
