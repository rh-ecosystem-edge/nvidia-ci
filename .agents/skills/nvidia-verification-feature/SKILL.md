---
name: nvidia-verification-feature
description: Define a Jira work item for verifying or automating an externally developed NVIDIA feature, including its definition of done.
---

# Define a verification feature

This is phase 1 of the [NVIDIA feature verification and automation workflow](../../../docs/feature-verification-workflow.md).
Inspect existing Jira work when applicable.

Use the feature description and related documents or Jira tickets to state the
functionality in scope. Identify whether the goal is manual verification,
automated testing, or both, and whether it serves certification.

Write a concise definition of done: which functionality must be checked, on
which target platform or hardware when known, and what deliverable or observed
result means the Jira ticket's goal has been achieved. Leave unknowns for
planning rather than filling them with assumptions.

For an automation goal, name the code or configuration to deliver: a test,
repeatable setup, or CI job, as applicable. A run can validate the deliverable.

If a ticket exists, use its wording and propose any needed clarification. If
there is no ticket yet, draft text suitable for one. Use
[nvidia-jira-sync](../nvidia-jira-sync/SKILL.md) when the user wants the feature
tracked in Jira; delivery tickets can be added after planning.
