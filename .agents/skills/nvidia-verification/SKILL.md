---
name: nvidia-verification
description: Start here to coordinate NVIDIA feature verification and automation for externally developed features, including planning, Jira, testbeds, tests, setup, and CI jobs.
---

# NVIDIA feature verification and automation

Start here. This is the main entry point for the
[NVIDIA feature verification and automation workflow](../../../docs/feature-verification-workflow.md).

For new or unclear work, use
[nvidia-verification-feature](../nvidia-verification-feature/SKILL.md) to
define the functionality and Jira ticket's definition of done. Then use
[nvidia-verification-plan](../nvidia-verification-plan/SKILL.md) to review
existing work and plan checks, delivery, and environment needs.

When the work needs Jira tickets, use
[nvidia-verification-breakdown](../nvidia-verification-breakdown/SKILL.md) to
draft the hierarchy and [nvidia-jira-sync](../nvidia-jira-sync/SKILL.md) to
create or update it when requested.

When the plan calls for OpenShift, use
[nvidia-testbed](../nvidia-testbed/SKILL.md) to prepare one specific setup.
Manual verification may precede automation, or automation may be the first
goal. Certification is an optional purpose of either path.

Use [nvidia-implement](../nvidia-implement/SKILL.md) when a ticket calls for
test code, reusable setup automation, or CI configuration. Manual-only
verification follows the plan and ticket definition of done without an
implementation phase.
