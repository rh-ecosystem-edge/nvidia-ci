# Use /opt/app-root for OpenShift compatibility
ARG OC_VERSION=4.21
ARG OPERATOR_SDK_VERSION=v1.42.0
ARG GO_TOOLSET_VERSION=1.25.5

FROM quay.io/openshift/origin-cli:${OC_VERSION} as oc-cli

FROM quay.io/operator-framework/operator-sdk:${OPERATOR_SDK_VERSION} as operator-sdk

FROM registry.access.redhat.com/ubi9/go-toolset:${GO_TOOLSET_VERSION} as ginkgo-builder

USER 1001

ARG APP_ROOT=/opt/app-root
ARG GOPATH=${APP_ROOT}/src/go

ENV GOCACHE=/tmp/go-cache/
ENV GOPATH="${GOPATH}"

RUN echo "GOPATH: ${GOPATH}"

WORKDIR ${APP_ROOT}

COPY --chown=1001:0 vendor/ ./vendor/
COPY --chown=1001:0 go.mod go.sum ./
COPY --chown=1001:0 Makefile ./
COPY --chown=1001:0 scripts/install-ginkgo.sh ./scripts/install-ginkgo.sh

RUN make install-ginkgo

FROM registry.access.redhat.com/ubi9/go-toolset:${GO_TOOLSET_VERSION}

LABEL org.opencontainers.image.authors="Red Hat Ecosystem Engineering"

ARG APP_ROOT=/opt/app-root
ARG GOPATH=${APP_ROOT}/src/go
ARG WORKDIR=${APP_ROOT}/nvidia-ci
ARG ARTIFACT_DIR=${WORKDIR}/test-results

USER root

# Copying binaries
COPY --from=oc-cli /usr/bin/oc /usr/bin/oc
COPY --from=operator-sdk /usr/local/bin/operator-sdk /usr/local/bin/operator-sdk

# Install dependencies combined into single layer to reduce image size
RUN dnf install -y jq && \
    dnf clean all && \
    rm -rf /var/cache/dnf /var/cache/yum

# Switch to non-root user (default user in base image)
# All subsequent operations run as this user, following OpenShift best practices
USER 1001

ENV GOCACHE=/tmp/go-cache/
ENV PATH="${PATH}:${GOPATH}/bin"

# Defaults we want the image to run with, can be overridden
ENV ARTIFACT_DIR="${ARTIFACT_DIR}"
ENV TEST_TRACE=true
ENV VERBOSE_LEVEL=100
ENV DUMP_FAILED_TESTS=true

WORKDIR ${WORKDIR}

RUN mkdir -p "${ARTIFACT_DIR}" && \
    chmod g=u "${WORKDIR}" && \
    chmod -R g=u "${ARTIFACT_DIR}"

COPY --from=ginkgo-builder --chmod=755 --chown=1001:0 "${GOPATH}/bin/ginkgo" "${GOPATH}/bin/ginkgo"

# Cherry-pick artifacts to reduce image size
COPY --chown=1001:0 vendor/ ./vendor/
COPY --chown=1001:0 go.mod go.sum ./
COPY --chown=1001:0 Makefile ./
COPY --chown=1001:0 internal/ ./internal/
COPY --chown=1001:0 scripts/ ./scripts/
COPY --chown=1001:0 pkg/ ./pkg/
COPY --chown=1001:0 --chmod=775 tests/ ./tests/

RUN make get-gpu-operator-must-gather && \
    make get-nfd-must-gather

ENTRYPOINT ["bash"]
