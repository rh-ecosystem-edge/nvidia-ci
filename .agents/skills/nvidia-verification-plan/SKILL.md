---
name: nvidia-verification-plan
description: Analyze existing NVIDIA CI work and plan a feature's manual or automated verification, with a ticket-specific definition of done.
---

# Plan feature verification and automation

This is phase 2 of the [NVIDIA feature verification and automation workflow](../../../docs/feature-verification-workflow.md).
Review external sources when applicable.

First review the feature documents and related tickets in the
[NVIDIA Jira project](https://redhat.atlassian.net/browse/NVIDIA), the relevant
tests and helpers in this repository, and the
[NVIDIA CI job definitions](https://github.com/openshift/release/tree/main/ci-operator/config/rh-ecosystem-edge/nvidia-ci).
Summarize existing coverage, reusable work, and gaps. The test code lives here;
CI job definitions can provision or use an OpenShift cluster and run the tests.
State when a source cannot be accessed; separate known requirements from open
questions. When CI job changes look likely, inspect the NVIDIA CI configs and
plan the job that will invoke the automation. Follow the `openshift/release`
root `AGENTS.md` when the plan reaches job changes.

Write a brief plan covering:

1. **Goal and definition of done:** Name the Jira ticket when available.
   Check that its definition of done names the functionality to verify or
   automate and the deliverable that means the goal is achieved. Automation
   may deliver test code, setup automation, or a CI job. Clarify the goal when
   needed.
   Manual work may come first; automation may also start first.
2. **Existing state:** Note relevant documents, related Jira work, existing
   tests and helpers, and CI jobs. Identify what is already covered or usable.
3. **Scenarios:** For each behavior, state the action, observable expected
   result, chosen method, and evidence to collect. Note gaps rather than
   assuming an unverified behavior passes.
4. **Testbed:** Normally plan for OpenShift on the target hardware. Decide
   whether to use an existing setup or build one in a lab, NVIDIA's lab via
   VPN, or a suitable cloud (AWS, GCP, Azure). For bare metal, consider the
   Assisted Installer with BMC access, manually or through Redfish automation.
   Identify required versions, configuration, hardware, and loan or access
   constraints. Distinguish the needed environment from any reusable setup
   code the ticket must deliver.
5. **Reuse:** Identify setup, workloads, checks, or evidence collection that
   can serve both manual and automated paths. Point to existing repository
   helpers when applicable.
6. **Delivery:** Outline work needed for manual checks, test code, automated
   setup, or a CI job, as applicable. Include how the deliverable will be
   validated and any dependency that blocks the definition of done.
   If certification is the goal, include the supplied criteria and required
   evidence; ask for missing criteria instead of inventing them.
7. **Jira breakdown:** Identify which planning or delivery activities need
   their own tickets. Use
   [nvidia-verification-breakdown](../nvidia-verification-breakdown/SKILL.md)
   when a hierarchy would help track the feature's definition of done.

Use a compact scenario table and short sections. Save the plan where the user
requests, or present it in the conversation when no location is specified.
