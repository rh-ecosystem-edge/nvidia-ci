FROM registry.access.redhat.com/ubi9/ubi

MAINTAINER Red Hat Ecosystem Engineering

# Install dependencies
RUN yum install -y \
    glibc-langpack-en \
    go \
    git \
    make \
    jq \
    vim \
    wget \
    rsync \
    time && \
    yum clean all && \
    rm -rf $HOME/.cache && \
    rm -rf /var/cache/yum

# Install dependencies: `oc`
ARG OCP_CLI_VERSION=latest
ARG OCP_CLI_URL=https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/${OCP_CLI_VERSION}/openshift-client-linux.tar.gz
RUN curl ${OCP_CLI_URL} | tar xfz - -C /usr/local/bin oc

# Install dependencies: `operator-sdk`
ARG OPERATOR_SDK_VERSION=v1.6.2
RUN ARCH=$(case $(uname -m) in x86_64) echo -n amd64 ;; aarch64) echo -n arm64 ;; *) echo -n $(uname -m) ;; esac) && \
    OS=$(uname | awk '{print tolower($0)}') && \
    OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/${OPERATOR_SDK_VERSION} && \
    curl -LO ${OPERATOR_SDK_DL_URL}/operator-sdk_${OS}_${ARCH} && \
    chmod +x operator-sdk_${OS}_${ARCH} && \
    mv operator-sdk_${OS}_${ARCH} /usr/local/bin/operator-sdk

# Get the source code in there
WORKDIR /root/nvidia-ci

COPY . .

ENTRYPOINT ["bash"]
