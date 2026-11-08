---
name: nvidia-verification-breakdown
description: Turn an NVIDIA feature verification plan into a small Jira-ready hierarchy of planning and delivery work.
---

# Break down verification work

This is phase 3 of the [NVIDIA feature verification and automation workflow](../../../docs/feature-verification-workflow.md).
Check related Jira work when applicable.

Use the feature's definition of done and any available verification plan. Treat
the verification feature as the overall outcome. The NVIDIA project currently
uses Feature → Epic → Story/Task → Sub-task links; reuse an applicable existing
parent or epic and add only the levels the work needs.

Propose separate tickets only for work that benefits from independent
tracking: planning, testbed preparation, manual verification, test automation,
CI job changes, or certification, as applicable. These are possibilities, not
a required checklist.

For each ticket, give a short goal, definition of done, proposed parent, and
any existing Jira key. Show how the child work contributes to the feature's
definition of done. Development here means test code, setup, or CI work; the
externally developed product feature is outside this workflow unless the user
explicitly includes it.

For automation tickets, name the test code, setup automation, or CI job in
scope as the deliverable.

Return a compact hierarchy. It may start with the feature and a planning
ticket, then gain delivery tickets after the plan is clearer.

## Next step

Recommend [nvidia-jira-sync](../nvidia-jira-sync/SKILL.md) when the hierarchy
is ready to create or update in Jira. If the breakdown exposes an unresolved
feature or testbed decision, return to
[nvidia-verification-plan](../nvidia-verification-plan/SKILL.md) first.
