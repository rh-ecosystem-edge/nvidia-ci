---
name: nvidia-implement
description: Implement NVIDIA verification automation when a ticket calls for test code, reusable testbed setup code, or CI job configuration.
---

# Implement verification automation

This is phase 5 of the [NVIDIA feature verification and automation workflow](../../../docs/feature-verification-workflow.md).

Use this skill when the ticket's definition of done includes code or
configuration. Manual-only verification follows the plan without this phase.

Read the ticket and verification plan, then inspect existing tests, setup, and
CI jobs. Identify the deliverable: Ginkgo test code in `nvidia-ci`, automation
code that can repeatedly create or prepare an OpenShift testbed, a CI job in
`openshift/release`, or a combination.

Follow the owning repository's agent guidance. In `nvidia-ci`, use its root
`AGENTS.md` for commands and package guidance. For NVIDIA CI jobs in
`openshift/release`, use that repository's root `AGENTS.md`. Normally, implement
and validate the automation in `nvidia-ci` first, then update the job
configuration to invoke it. Reuse existing work where practical.

Produce the scoped code or configuration changes. Run applicable checks and the
relevant OpenShift test when its planned testbed is available.
Running new setup automation may also produce the specific testbed needed for
verification. Report the delivered files, validation results, and any
definition-of-done item that remains unverified.
