---
name: nvidia-testbed
description: Make one OpenShift test environment ready for a planned NVIDIA verification, including special or loaned hardware.
---

# Prepare a specific testbed

This is phase 4 of the [NVIDIA feature verification and automation workflow](../../../docs/feature-verification-workflow.md).
Use the chosen testbed's tools unless the user selects another interface.

Use the plan's target versions, configuration, and hardware to find a suitable
existing OpenShift cluster or make one available. The `nvidia-ci` tests access
that cluster through `KUBECONFIG`; provisioning may happen in a CI job or
separately.

Use the planned lab, NVIDIA lab via VPN, or suitable cloud environment (AWS,
GCP, Azure). Bare-metal installations commonly use the OpenShift Assisted
Installer and a BMC, operated manually or using existing Redfish automation.
Establish hardware availability and any loan or VPN access before preparing it.

The outcome is a usable environment for this verification. Record its actual
versions, hardware, and setup so manual and automated checks can share it.

If required access or hardware is unavailable, report which scenarios remain
blocked and what can proceed without it.

When the ticket calls for new reusable setup code, hand off to
[nvidia-implement](../nvidia-implement/SKILL.md) for that code deliverable.
