# Image for the resiliency tests, built from the minio and mc binaries
# in the repository root (make build; docs/resiliency/resiliency-tests.sh
# builds mc). Prebuilt minio/minio images are no longer published.
#
# The tests run commands in the containers with /bin/sh that rely on bash
# brace expansion and GNU coreutils options.
FROM debian:bookworm-slim

RUN ln -sf /bin/bash /bin/sh

COPY ./minio /usr/bin/minio
COPY ./mc /usr/bin/mc
COPY dockerscripts/docker-entrypoint.sh /usr/bin/docker-entrypoint.sh

ENTRYPOINT ["/usr/bin/docker-entrypoint.sh"]

VOLUME ["/data"]

CMD ["minio"]
