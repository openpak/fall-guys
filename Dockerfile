# syntax=docker/dockerfile:1
# Fall Guys backend: catapultd (login + gateway). Plain HTTP inside the pod network; Traefik
# terminates the console's TLS for the oncatapult.com names and forwards the stream.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/catapultd ./cmd/catapultd

FROM gcr.io/distroless/static
COPY --from=build /out/catapultd /catapultd
USER nonroot:nonroot
EXPOSE 23530
ENTRYPOINT ["/catapultd"]
