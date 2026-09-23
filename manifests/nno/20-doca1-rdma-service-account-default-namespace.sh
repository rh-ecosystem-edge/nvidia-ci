#!/usr/bin/env bash
#
# Post-apply hook for 20-doca1-rdma-service-account-default-namespace.yaml.
# Grants the "privileged" SCC to the "rdma" ServiceAccount that manifest creates,
# so RDMA workload pods running under it can request privileged access.

set -e

oc -n default adm policy add-scc-to-user privileged -z rdma
