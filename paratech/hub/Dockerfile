# Hub image. The build context is paratech/ because the hub module needs ../shared (see the replace in hub/go.mod):
#   docker build -f hub/Dockerfile -t paratech-hub .      (run from paratech/)
# Configuration comes from the environment at run time (HUB_DATABASE_URL is required); nothing secret is baked in.

FROM golang:1.26.9-alpine AS build
WORKDIR /src
# GOWORK=off: the image has no go.work (it would also list ./desktop); hub/go.mod alone resolves shared.
ENV CGO_ENABLED=0 GOWORK=off GOFLAGS=-trimpath

COPY shared/go.mod shared/go.sum shared/
COPY hub/go.mod hub/go.sum hub/
RUN cd hub && go mod download

COPY shared/ shared/
COPY hub/ hub/
RUN cd hub && go build -ldflags="-s -w" -o /out/hub ./cmd/hub

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hub /hub
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/hub"]
