#!/bin/sh

if [ -d out/calico-kicker ]; then
   rm -Rf out/calico-kicker.tar
fi

mkdir -p out

echo "Building container image..."
docker build \
  --tag tkhq/calico-kicker \
  --progress=plain \
  --label "org.opencontainers.image.source=https://github.com/tkhq/calico-kicker" \
  --output "\
    type=oci,\
    rewrite-timestamp=true,\
    force-compression=true,\
    name=calico-kicker,\
    dest=out/calico-kicker.tar" \
  -f Containerfile \
  .

echo "Loading container image into container runtime..."
cat out/calico-kicker.tar | docker load
