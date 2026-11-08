---
name: nvidia-jira-sync
description: Create or update the NVIDIA Jira hierarchy for a verification feature and its planned work when the user asks to track it.
---

# Sync verification work to Jira

Use the [NVIDIA feature verification and automation workflow](../../../docs/feature-verification-workflow.md),
feature definition, and [ticket breakdown](../nvidia-verification-breakdown/SKILL.md). Search the
NVIDIA project for existing related tickets before creating any. Inspect its
supported issue types and parent relationships; reuse an existing Feature or
Epic only when it fits the proposed work and parent chain. The project has
Feature, Epic, Story, Task, and Sub-task issues, with observed Feature → Epic →
Story/Task → Sub-task links. The breakdown describes work roles; it does not
require every level for every feature.

When the user asks to create or sync the tickets, use `jira -p NVIDIA` by
default, or the user's chosen interface (such as the Jira API), to create or
update the smallest needed hierarchy. Create the parent first, then
its children; verify their links and return the Jira keys and URLs. Each
ticket should carry its own definition of done. The feature ticket is complete
when its overall goal has been achieved.

The user supplies Jira access.

If Jira access or the issue hierarchy cannot be confirmed, present the
proposed tickets and state what remains uncreated.
