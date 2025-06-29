#!/bin/bash

# NFD Operator Must Gather Script for OpenShift
# This script collects debugging information for Node Feature Discovery operator issues

set -euo pipefail

MUST_GATHER_DIR="/home/tnewman/repos/midstream/nvidia-ci"
NFD_NAMESPACE="${NFD_NAMESPACE:-openshift-nfd}"
TIMESTAMP=$(date +"%Y%m%d-%H%M%S")
OUTPUT_DIR="${MUST_GATHER_DIR}/nfd-must-gather"

# Colors for output for debugging purposes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

create_output_dir() {
    log_info "Creating output directory: ${OUTPUT_DIR}"
    mkdir -p "${OUTPUT_DIR}"/{cluster,nodes,nfd-operator,nfd-resources,logs,events}
}

check_prerequisites() {
    log_info "Checking prerequisites"

    if ! command -v oc &> /dev/null; then
        log_error "oc command not found. Please ensure OpenShift CLI is installed."
        exit 1
    fi

    if ! oc whoami &> /dev/null; then
        log_error "Not logged into OpenShift cluster. Please login first."
        exit 1
    fi

    log_info "Prerequisites check passed"
}

collect_cluster_info() {
    log_info "Collecting cluster information"

    oc get clusterversion -o yaml > "${OUTPUT_DIR}/cluster/clusterversion.yaml" 2>/dev/null || log_warn "Failed to get cluster version"

    oc get clusteroperators -o yaml > "${OUTPUT_DIR}/cluster/clusteroperators.yaml" 2>/dev/null || log_warn "Failed to get cluster operators"

    oc get nodes -o wide > "${OUTPUT_DIR}/nodes/nodes-wide.txt" 2>/dev/null || log_warn "Failed to get nodes wide output"
    oc get nodes -o yaml > "${OUTPUT_DIR}/nodes/nodes.yaml" 2>/dev/null || log_warn "Failed to get nodes yaml"

    oc get machineconfigs -o yaml > "${OUTPUT_DIR}/cluster/machineconfigs.yaml" 2>/dev/null || log_warn "Failed to get machine configs"
    oc get machineconfigpools -o yaml > "${OUTPUT_DIR}/cluster/machineconfigpools.yaml" 2>/dev/null || log_warn "Failed to get machine config pools"
}

collect_nfd_operator() {
    log_info "Collecting NFD operator information"

    # Find all namespaces that contain NFD-related resources
    local nfd_namespaces=()

    # Check default NFD namespace
    if oc get namespace "${NFD_NAMESPACE}" &> /dev/null; then
        nfd_namespaces+=("${NFD_NAMESPACE}")
    fi

    # Look for NFD resources in all namespaces
    log_info "Searching for NFD resources across all namespaces"

    # Find namespaces with NFD pods
    while IFS= read -r ns; do
        if [[ -n "$ns" && "$ns" != "NAMESPACE" ]]; then
            if [[ ! " ${nfd_namespaces[*]} " =~ " ${ns} " ]]; then
                nfd_namespaces+=("$ns")
            fi
        fi
    done < <(oc get pods --all-namespaces --selector=app=nfd --no-headers 2>/dev/null | awk '{print $1}' | sort -u)

    # Find namespaces with deployments containing 'nfd' in name
    while IFS= read -r ns; do
        if [[ -n "$ns" && "$ns" != "NAMESPACE" ]]; then
            if [[ ! " ${nfd_namespaces[*]} " =~ " ${ns} " ]]; then
                nfd_namespaces+=("$ns")
            fi
        fi
    done < <(oc get deployments --all-namespaces --no-headers 2>/dev/null | grep -i nfd | awk '{print $1}' | sort -u)

    # Find namespaces with daemonsets containing 'nfd' in name
    while IFS= read -r ns; do
        if [[ -n "$ns" && "$ns" != "NAMESPACE" ]]; then
            if [[ ! " ${nfd_namespaces[*]} " =~ " ${ns} " ]]; then
                nfd_namespaces+=("$ns")
            fi
        fi
    done < <(oc get daemonsets --all-namespaces --no-headers 2>/dev/null | grep -i nfd | awk '{print $1}' | sort -u)

    # If no NFD namespaces found, fall back to common locations
    if [[ ${#nfd_namespaces[@]} -eq 0 ]]; then
        log_warn "No NFD resources found. Checking common namespaces"
        for ns in openshift-operators default kube-system; do
            if oc get namespace "$ns" &> /dev/null; then
                nfd_namespaces+=("$ns")
            fi
        done
    fi

    # Saving list of NFD namespaces found
    {
        echo "NFD-related namespaces found:"
        printf '%s\n' "${nfd_namespaces[@]}"
    } > "${OUTPUT_DIR}/nfd-operator/nfd-namespaces.txt"

    log_info "Found NFD resources in namespaces: ${nfd_namespaces[*]}"

    # Collect resources from each NFD namespace
    for ns in "${nfd_namespaces[@]}"; do
        log_info "Collecting NFD operator resources from namespace: $ns"

        # Create namespace-specific subdirectory
        mkdir -p "${OUTPUT_DIR}/nfd-operator/${ns}"

        # NFD Operator deployment and pods
        oc get deployment -n "$ns" -o yaml > "${OUTPUT_DIR}/nfd-operator/${ns}/deployments.yaml" 2>/dev/null || log_warn "Failed to get deployments in $ns"
        oc get pods -n "$ns" -o wide > "${OUTPUT_DIR}/nfd-operator/${ns}/pods-wide.txt" 2>/dev/null || log_warn "Failed to get pods wide output in $ns"
        oc get pods -n "$ns" -o yaml > "${OUTPUT_DIR}/nfd-operator/${ns}/pods.yaml" 2>/dev/null || log_warn "Failed to get pods yaml in $ns"

        oc get replicasets -n "$ns" -o yaml > "${OUTPUT_DIR}/nfd-operator/${ns}/replicasets.yaml" 2>/dev/null || log_warn "Failed to get replicasets in $ns"
        oc get daemonsets -n "$ns" -o yaml > "${OUTPUT_DIR}/nfd-operator/${ns}/daemonsets.yaml" 2>/dev/null || log_warn "Failed to get daemonsets in $ns"

        oc get services -n "$ns" -o yaml > "${OUTPUT_DIR}/nfd-operator/${ns}/services.yaml" 2>/dev/null || log_warn "Failed to get services in $ns"
        oc get configmaps -n "$ns" -o yaml > "${OUTPUT_DIR}/nfd-operator/${ns}/configmaps.yaml" 2>/dev/null || log_warn "Failed to get configmaps in $ns"
        oc get secrets -n "$ns" -o yaml > "${OUTPUT_DIR}/nfd-operator/${ns}/secrets.yaml" 2>/dev/null || log_warn "Failed to get secrets in $ns"

        oc get serviceaccounts -n "$ns" -o yaml > "${OUTPUT_DIR}/nfd-operator/${ns}/serviceaccounts.yaml" 2>/dev/null || log_warn "Failed to get service accounts in $ns"
        oc get rolebindings -n "$ns" -o yaml > "${OUTPUT_DIR}/nfd-operator/${ns}/rolebindings.yaml" 2>/dev/null || log_warn "Failed to get role bindings in $ns"

        # Collect logs for this namespace
        collect_logs_for_namespace "$ns"
    done

    oc get clusterrolebindings -o yaml | grep -A 10 -B 10 nfd > "${OUTPUT_DIR}/nfd-operator/clusterrolebindings.yaml" 2>/dev/null || log_warn "Failed to get NFD cluster role bindings"
    oc get clusterroles -o yaml | grep -A 20 -B 5 nfd > "${OUTPUT_DIR}/nfd-operator/clusterroles.yaml" 2>/dev/null || log_warn "Failed to get NFD cluster roles"
}

collect_nfd_resources() {
    log_info "Collecting NFD custom resources"

    oc get crd | grep nfd > "${OUTPUT_DIR}/nfd-resources/nfd-crds.txt" 2>/dev/null || log_warn "No NFD CRDs found"

    # Get NFD CRDs in detail
    for crd in $(oc get crd -o name | grep nfd 2>/dev/null); do
        crd_name=$(basename "$crd")
        oc get "$crd" -o yaml > "${OUTPUT_DIR}/nfd-resources/crd-${crd_name}.yaml" 2>/dev/null || log_warn "Failed to get CRD: $crd_name"
    done

    # NFD instances/custom resources - collect from ALL namespaces
    log_info "Collecting NodeFeatureRules from all namespaces"
    oc get nodefeaturerules --all-namespaces -o yaml > "${OUTPUT_DIR}/nfd-resources/nodefeaturerules-all-namespaces.yaml" 2>/dev/null || log_warn "Failed to get NodeFeatureRules from all namespaces"
    oc get nodefeaturerules --all-namespaces -o wide > "${OUTPUT_DIR}/nfd-resources/nodefeaturerules-all-namespaces-wide.txt" 2>/dev/null || log_warn "Failed to get NodeFeatureRules wide output from all namespaces"

    log_info "Collecting NodeFeatures from all namespaces"
    oc get nodefeatures --all-namespaces -o yaml > "${OUTPUT_DIR}/nfd-resources/nodefeatures-all-namespaces.yaml" 2>/dev/null || log_warn "Failed to get NodeFeatures from all namespaces"
    oc get nodefeatures --all-namespaces -o wide > "${OUTPUT_DIR}/nfd-resources/nodefeatures-all-namespaces-wide.txt" 2>/dev/null || log_warn "Failed to get NodeFeatures wide output from all namespaces"

    log_info "Collecting NodeFeatureDiscovery from all namespaces"
    oc get nodefeaturediscovery --all-namespaces -o yaml > "${OUTPUT_DIR}/nfd-resources/nodefeaturediscovery-all-namespaces.yaml" 2>/dev/null || log_warn "Failed to get NodeFeatureDiscovery from all namespaces"
    oc get nodefeaturediscovery --all-namespaces -o wide > "${OUTPUT_DIR}/nfd-resources/nodefeaturediscovery-all-namespaces-wide.txt" 2>/dev/null || log_warn "Failed to get NodeFeatureDiscovery wide output from all namespaces"

    # Also collect from specific namespace if it exists (for backward compatibility)
    if oc get namespace "${NFD_NAMESPACE}" &> /dev/null; then
        log_info "Collecting NFD resources from namespace: ${NFD_NAMESPACE}"
        oc get nodefeaturerules -n "${NFD_NAMESPACE}" -o yaml > "${OUTPUT_DIR}/nfd-resources/nodefeaturerules-${NFD_NAMESPACE}.yaml" 2>/dev/null || log_warn "Failed to get NodeFeatureRules from ${NFD_NAMESPACE}"
        oc get nodefeatures -n "${NFD_NAMESPACE}" -o yaml > "${OUTPUT_DIR}/nfd-resources/nodefeatures-${NFD_NAMESPACE}.yaml" 2>/dev/null || log_warn "Failed to get NodeFeatures from ${NFD_NAMESPACE}"
        oc get nodefeaturediscovery -n "${NFD_NAMESPACE}" -o yaml > "${OUTPUT_DIR}/nfd-resources/nodefeaturediscovery-${NFD_NAMESPACE}.yaml" 2>/dev/null || log_warn "Failed to get NodeFeatureDiscovery from ${NFD_NAMESPACE}"
    fi

    log_info "Identifying namespaces with NFD resources"
    {
        echo "=== Namespaces containing NodeFeatureRules ==="
        oc get nodefeaturerules --all-namespaces --no-headers 2>/dev/null | awk '{print $1}' | sort -u || echo "None found"
        echo ""
        echo "=== Namespaces containing NodeFeatures ==="
        oc get nodefeatures --all-namespaces --no-headers 2>/dev/null | awk '{print $1}' | sort -u || echo "None found"
        echo ""
        echo "=== Namespaces containing NodeFeatureDiscovery ==="
        oc get nodefeaturediscovery --all-namespaces --no-headers 2>/dev/null | awk '{print $1}' | sort -u || echo "None found"
    } > "${OUTPUT_DIR}/nfd-resources/nfd-namespaces.txt"
}

collect_logs_for_namespace() {
    local namespace="$1"
    log_info "Collecting logs for namespace: $namespace"

    # Create namespace-specific logs directory
    mkdir -p "${OUTPUT_DIR}/logs/${namespace}"

    # Get all pods in the namespace
    local pods
    pods=$(oc get pods -n "$namespace" -o name 2>/dev/null) || {
        log_warn "Failed to get pods in namespace $namespace"
        return
    }

    while IFS= read -r pod; do
        if [[ -n "$pod" ]]; then
            pod_name=$(basename "$pod")
            log_info "Collecting logs for pod: $pod_name in namespace: $namespace"

            # Current logs
            oc logs -n "$namespace" "$pod" > "${OUTPUT_DIR}/logs/${namespace}/${pod_name}.log" 2>/dev/null || log_warn "Failed to get current logs for $pod_name in $namespace"

            # Container-specific logs for multi-container pods
            local containers
            containers=$(oc get "$pod" -n "$namespace" -o jsonpath='{.spec.containers[*].name}' 2>/dev/null)
            if [[ $(echo "$containers" | wc -w) -gt 1 ]]; then
                for container in $containers; do
                    oc logs -n "$namespace" "$pod" -c "$container" > "${OUTPUT_DIR}/logs/${namespace}/${pod_name}-${container}-current.log" 2>/dev/null || log_warn "Failed to get logs for container $container in $pod_name"
                    oc logs -n "$namespace" "$pod" -c "$container" --previous > "${OUTPUT_DIR}/logs/${namespace}/${pod_name}-${container}-previous.log" 2>/dev/null || log_warn "No previous logs for container $container in $pod_name"
                done
            fi
        fi
    done <<< "$pods"
}

collect_logs() {
    log_info "Collecting logs from all NFD-related namespaces"

    # This function is now called from collect_nfd_operator for each namespace
    # But we'll also collect from the default NFD namespace if it wasn't already covered
    if oc get namespace "${NFD_NAMESPACE}" &> /dev/null; then
        collect_logs_for_namespace "${NFD_NAMESPACE}"
    fi

    # Collect any NFD-related pods from other namespaces that might have been missed
    log_info "Scanning all namespaces for additional NFD pods"
    while IFS= read -r line; do
        if [[ -n "$line" && "$line" != "NAMESPACE"* ]]; then
            local ns=$(echo "$line" | awk '{print $1}')
            local pod=$(echo "$line" | awk '{print $2}')

            if [[ -n "$ns" && -n "$pod" ]]; then
                mkdir -p "${OUTPUT_DIR}/logs/${ns}"

                log_info "Found additional NFD pod: $pod in namespace: $ns"

                # Collect logs for this pod
                oc logs -n "$ns" "$pod" > "${OUTPUT_DIR}/logs/${ns}/${pod}-current.log" 2>/dev/null || log_warn "Failed to get current logs for $pod in $ns"
                oc logs -n "$ns" "$pod" --previous > "${OUTPUT_DIR}/logs/${ns}/${pod}-previous.log" 2>/dev/null || log_warn "No previous logs for $pod in $ns"
            fi
        fi
    done < <(oc get pods --all-namespaces --no-headers 2>/dev/null | grep -i nfd)
}

collect_events() {
    log_info "Collecting events"

    # Events from all NFD-related namespaces
    log_info "Collecting events from all NFD-related namespaces"

    # Get list of namespaces that have NFD resources
    local nfd_namespaces=()
    while IFS= read -r ns; do
        if [[ -n "$ns" && "$ns" != "NAMESPACE" ]]; then
            nfd_namespaces+=("$ns")
        fi
    done < <(oc get pods --all-namespaces --no-headers 2>/dev/null | grep -i nfd | awk '{print $1}' | sort -u)

    # Add the default NFD namespace if it exists
    if oc get namespace "${NFD_NAMESPACE}" &> /dev/null; then
        if [[ ! " ${nfd_namespaces[*]} " =~ " ${NFD_NAMESPACE} " ]]; then
            nfd_namespaces+=("${NFD_NAMESPACE}")
        fi
    fi

    # Collect events from each NFD namespace
    for ns in "${nfd_namespaces[@]}"; do
        log_info "Collecting events from namespace: $ns"
        oc get events -n "$ns" --sort-by='.lastTimestamp' > "${OUTPUT_DIR}/events/events-${ns}.txt" 2>/dev/null || log_warn "Failed to get events in namespace $ns"
    done

    # Cluster-wide events related to NFD
    oc get events --all-namespaces --sort-by='.lastTimestamp' | grep -i nfd > "${OUTPUT_DIR}/events/cluster-nfd-events.txt" 2>/dev/null || log_warn "No NFD-related cluster events found"

    # Node events (might be relevant for NFD issues)
    oc get events --all-namespaces --field-selector involvedObject.kind=Node --sort-by='.lastTimestamp' > "${OUTPUT_DIR}/events/node-events.txt" 2>/dev/null || log_warn "Failed to get node events"

    # Events related to NFD custom resources
    oc get events --all-namespaces --sort-by='.lastTimestamp' | grep -E "(NodeFeature|NodeFeatureRule|NodeFeatureDiscovery)" > "${OUTPUT_DIR}/events/nfd-resource-events.txt" 2>/dev/null || log_warn "No NFD custom resource events found"
}

collect_node_features() {
    log_info "Collecting node feature information"

    # Node labels (NFD adds labels to nodes)
    oc get nodes --show-labels > "${OUTPUT_DIR}/nodes/node-labels.txt" 2>/dev/null || log_warn "Failed to get node labels"

    # Detailed node descriptions
    local nodes
    nodes=$(oc get nodes -o name 2>/dev/null) || {
        log_warn "Failed to get node list"
        return
    }

    while IFS= read -r node; do
        if [[ -n "$node" ]]; then
            node_name=$(basename "$node")
            oc describe "$node" > "${OUTPUT_DIR}/nodes/describe-${node_name}.txt" 2>/dev/null || log_warn "Failed to describe node $node_name"
        fi
    done <<< "$nodes"
}

create_summary() {
    log_info "Creating summary report"

    cat > "${OUTPUT_DIR}/SUMMARY.txt" << EOF
NFD Operator Must Gather Summary
Generated: $(date)
Cluster: $(oc whoami --show-server 2>/dev/null || echo "Unknown")
User: $(oc whoami 2>/dev/null || echo "Unknown")
NFD Namespace: ${NFD_NAMESPACE}

Directory Structure:
├── cluster/           - Cluster-wide information
├── nodes/            - Node information and features
├── nfd-operator/     - NFD operator deployments, pods, configs
├── nfd-resources/    - NFD custom resources and CRDs
├── logs/             - Pod logs (current and previous)
├── events/           - Events related to NFD
└── SUMMARY.txt       - This summary file

Key Files:
- cluster/clusterversion.yaml - Cluster version information
- nodes/node-labels.txt - Node labels (including NFD labels)
- nfd-operator/pods.yaml - NFD operator pods
- nfd-resources/nodefeaturerules.yaml - NFD rules configuration
- logs/ - All NFD operator logs

Troubleshooting Tips:
1. Check pod status in nfd-operator/pods.yaml
2. Review logs in logs/ directory for errors
3. Verify NFD rules in nfd-resources/nodefeaturerules.yaml
4. Check node labels in nodes/node-labels.txt for expected features
5. Review events in events/ directory for issues

EOF

    log_info "Summary report created"
}

create_archive() {
    log_info "Creating archive"

    cd "${OUTPUT_DIR}"
    tar -czf "nfd-must-gather-${TIMESTAMP}.tar.gz" "dump"

    log_info "Archive created: ${OUTPUT_DIR}/nfd-must-gather-${TIMESTAMP}.tar.gz"
}

main() {
    log_info "Starting NFD Operator Must Gather collection"

    check_prerequisites
    create_output_dir

    collect_cluster_info
    collect_nfd_operator
    collect_nfd_resources
    collect_logs
    collect_events
    collect_node_features

    create_summary
    create_archive

    log_info "NFD Must Gather collection completed successfully!"
    log_info "Output directory: ${OUTPUT_DIR}"
    log_info "Archive: ${MUST_GATHER_DIR}/nfd-must-gather-${TIMESTAMP}.tar.gz"
}

# Handle script interruption
trap 'log_error "Script interrupted"; exit 1' INT TERM

main "$@"
