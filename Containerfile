FROM stagex/pallet-go:sx2025.06.1@sha256:6f0ac820e46e2fc43633f2118c5e34bbdda2f0d3671fae0eb8a100188d70d6a8 AS build

ARG CGO_ENABLED="0"
ARG GO_BUILDFLAGS="-x -v -trimpath -buildvcs=false"
ARG GO_LDFLAGS="-s -w -buildid= -extldflags='-static'"
ARG GOFLAGS=${GO_BUILDFLAGS} -ldflags="${GO_LDFLAGS}"

ADD . .

RUN go mod download
RUN --network=none go build ${GOFLAGS} -o /rootfs/calico-kicker

FROM stagex/core-ca-certificates:sx2025.08.0@sha256:85780a2557bae5c7ed1c7af3d20d66c77e10284da3e538b90755e18cc5ffbe33

COPY --from=build /rootfs/calico-kicker /calico-kicker
