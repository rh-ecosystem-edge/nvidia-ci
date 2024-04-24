FROM registry.access.redhat.com/ubi9/go-toolset:1.20

LABEL org.opencontainers.image.authors="Red Hat Ecosystem Engineering"

USER root
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

ENV GOCACHE=/tmp/
ENV PATH="${PATH}:/opt/app-root/src/go/bin"

COPY . .

RUN make install-ginkgo
RUN chmod 777 /root/nvidia-ci -R

ENTRYPOINT ["bash"]
