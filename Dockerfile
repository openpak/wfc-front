FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/wfc-front .
FROM scratch
LABEL org.opencontainers.image.source="https://github.com/openpak/wfc-front"
COPY --from=build /out/wfc-front /wfc-front
USER 65532:65532
ENTRYPOINT ["/wfc-front"]
