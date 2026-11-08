# NVIDIA feature verification and automation workflow

This repository tests NVIDIA features developed elsewhere. Use this workflow to
plan and deliver manual verification, automated tests, reusable setup, or CI
jobs. The work may begin with manual verification or go directly to
automation. Either path may serve certification.
Most features need OpenShift on the target hardware. `nvidia-ci` contains the
tests; the [OpenShift CI configuration](https://github.com/openshift/release/tree/main/ci-operator/config/rh-ecosystem-edge/nvidia-ci)
defines the jobs that run them.

## Entry point and repository guidance

Use the `nvidia-verification` skill as the default entry point for NVIDIA
feature verification and automation. Invoke a phase skill directly only when
the user has already defined the phase and scope.

Before implementation, follow the owning repository's agent instructions:

- In `nvidia-ci`, read `AGENTS.md` for repository commands, layout, and test
  mechanics. `CLAUDE.md` imports that file rather than maintaining a second copy.
- For NVIDIA CI jobs in `openshift/release`, read that repository's root
  `AGENTS.md`. Normally, implement and validate automation in `nvidia-ci`
  first, then add or update a job there to invoke that automation.

## Recommended agent tools

These are defaults for agent interaction. Use the user's chosen interface when
specified, such as the Jira API instead of `jira`. The user supplies credentials
and access, including `KUBECONFIG`, VPN, and target hardware access.

| System | Recommended tool or interface |
|---|---|
| NVIDIA Jira | `jira -p NVIDIA` |
| GitHub repositories, pull requests, and CI job definitions | `gh`; `git` for local checkouts |
| OpenShift clusters | `oc` with `KUBECONFIG` |
| Cloud testbeds | The provider CLI: `aws`, `gcloud`, or `az` |

## 1. Define the feature

Use [nvidia-verification-feature](../.agents/skills/nvidia-verification-feature/SKILL.md)
with the source feature description and any Jira ticket. State the exact
functionality to verify or automate and a definition of done for the ticket.
The definition of done should say what deliverable or result means the goal is
achieved; closing the ticket represents that outcome.

For manual verification, the deliverable is a result for **[named behavior]**
on **[target setup]**. Automation may deliver test code covering that
behavior, repeatable setup code, or a CI job, as defined by the ticket.

## 2. Plan the delivery

Use [nvidia-verification-plan](../.agents/skills/nvidia-verification-plan/SKILL.md).
Review feature documents, related work in the
[NVIDIA Jira project](https://redhat.atlassian.net/browse/NVIDIA), existing
code and tests, and current CI job definitions. Identify what already exists,
what is missing, and what can be reused. Then choose scenarios, manual or
automated methods, the testbed, and the work needed to meet the definition of
done. Include certification criteria only when certification is the goal.

## 3. Track the work in Jira when useful

Use [nvidia-verification-breakdown](../.agents/skills/nvidia-verification-breakdown/SKILL.md)
to propose the smallest useful ticket hierarchy. The parent represents the
feature goal. The NVIDIA project currently uses a Feature → Epic → Story/Task →
Sub-task pattern; use existing applicable parents and only the levels needed.
Children may track planning, the testbed, manual verification, test automation,
CI job changes, or certification when these are separate pieces of work. Each
ticket has its own definition of done.

Use [nvidia-jira-sync](../.agents/skills/nvidia-jira-sync/SKILL.md) when you
want the proposed hierarchy created or updated in the NVIDIA Jira project.
Confirm the actual parent links before creating tickets. The feature and a
planning ticket can be tracked before the full plan; delivery tickets can be
added after planning.

## 4. Prepare the testbed for this verification

When the plan calls for OpenShift, use
[nvidia-testbed](../.agents/skills/nvidia-testbed/SKILL.md) to make one suitable
environment available on the target hardware. It may be an
existing cluster or one built in a lab, NVIDIA's lab through VPN, or a suitable
cloud (AWS, GCP, Azure). Bare-metal setups commonly use the OpenShift Assisted
Installer and BMC access. The outcome is a cluster the planned checks can use
through `KUBECONFIG`.

## 5. Implement automation when needed

For a manual-only ticket, an engineer follows the plan on the testbed and
records the results. The ticket closes when its definition of done is met.

For a ticket that includes test code, reusable testbed setup code, or CI job
configuration, use [nvidia-implement](../.agents/skills/nvidia-implement/SKILL.md)
as an optional coding phase. It helps the engineer inspect existing work,
make the scoped changes in the owning repository, validate them, and identify
any remaining definition-of-done gaps. The deliverable may be Ginkgo code in
`nvidia-ci`, setup automation, a job in `openshift/release`, or a combination.
Normally, automation is implemented and validated in `nvidia-ci` before a job
is added or updated to invoke it. Steps 4 and 5 can overlap: running new setup
automation may create the specific cluster needed for verification.

The [nvidia-verification](../.agents/skills/nvidia-verification/SKILL.md)
skill can coordinate the phases. Each phase can also be used directly when the
feature or testbed is already defined.

## Example requests

- “Use `$nvidia-verification-feature` to define verification work for
  **[feature link]** and draft its definition of done.”
- “Use `$nvidia-verification-plan` to plan delivery for **[Jira ticket]**,
  starting with existing documentation, tests, and CI jobs.”
- “Use `$nvidia-verification-breakdown` to propose Jira tickets for the plan,
  then `$nvidia-jira-sync` to create them.”
- “Use `$nvidia-implement` for **[automation ticket]** to add the test or setup
  automation described by its definition of done.”
- “Use `$nvidia-verification` for **[Jira ticket]** to refine its goal, plan
  verification, and follow any applicable implementation work.”
