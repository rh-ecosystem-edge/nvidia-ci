FROM quay.io/openshift/origin-cli:4.20 as oc-cli
FROM registry.access.redhat.com/ubi9/go-toolset:1.21

LABEL org.opencontainers.image.authors="Red Hat Ecosystem Engineering"

USER root
# Copying oc binary
COPY --from=oc-cli /usr/bin/oc /usr/bin/oc

# Install dependencies: `operator-sdk`
ARG OPERATOR_SDK_VERSION=v1.6.2
RUN ARCH=$(case $(uname -m) in x86_64) echo -n amd64 ;; aarch64) echo -n arm64 ;; *) echo -n $(uname -m) ;; esac) && \
    OS=$(uname | awk '{print tolower($0)}') && \
    OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/${OPERATOR_SDK_VERSION} && \
    curl -fLO ${OPERATOR_SDK_DL_URL}/operator-sdk_${OS}_${ARCH} && \
    chmod +x operator-sdk_${OS}_${ARCH} && \
    mv operator-sdk_${OS}_${ARCH} /usr/local/bin/operator-sdk

# Get the source code in there
WORKDIR /root/nvidia-ci

ENV GOCACHE=/root/nvidia-ci/tmp/
ENV PATH="${PATH}:/opt/app-root/src/go/bin"

# Defaults we want the image to run with, can be overridden
ARG ARTIFACT_DIR=/root/nvidia-ci/test-results
ENV ARTIFACT_DIR="${ARTIFACT_DIR}"
ENV TEST_TRACE=true
ENV VERBOSE_LEVEL=100
ENV DUMP_FAILED_TESTS=true

COPY . .

RUN make install-ginkgo
RUN mkdir -p "${ARTIFACT_DIR}" && chmod 777 "${ARTIFACT_DIR}"
RUN mkdir -p "${GOCACHE}" && chmod 777 "${GOCACHE}"
RUN chmod 777 /root/nvidia-ci -R
ARG GPU_OPERATOR_VERSION=v23.9.1
RUN curl -SsL -o gpu-operator-must-gather.sh -L https://raw.githubusercontent.com/NVIDIA/gpu-operator/${GPU_OPERATOR_VERSION}/hack/must-gather.sh && \
    chmod +x gpu-operator-must-gather.sh

ENTRYPOINT ["bash"]
