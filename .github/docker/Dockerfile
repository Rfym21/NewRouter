# Release Linux binaries already include the frontend; this image only adds
# runtime dependencies and the binary for the requested architecture.
FROM debian:bookworm-slim@sha256:f06537653ac770703bc45b4b113475bd402f451e85223f0f2837acbf89ab020a

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata libasan8 wget \
    && rm -rf /var/lib/apt/lists/* \
    && update-ca-certificates

ARG TARGETARCH
COPY --chmod=755 ${TARGETARCH}/new-router /new-router
COPY licenses/ /licenses/
EXPOSE 3000
WORKDIR /data
ENTRYPOINT ["/new-router"]
