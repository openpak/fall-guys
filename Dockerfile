# syntax=docker/dockerfile:1
# Fall Guys backend: catapultd (login + gateway) and roundd (the round server, UDP 23533; run
# the same image with --entrypoint /roundd). Plain HTTP inside the pod network; Traefik
# terminates the console's TLS for the oncatapult.com names and forwards the stream.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/ ./cmd/catapultd ./cmd/roundd

FROM gcr.io/distroless/static
COPY --from=build /out/catapultd /out/roundd /
USER nonroot:nonroot
EXPOSE 23530 23533/udp
ENTRYPOINT ["/catapultd"]
