## Troubleshooting

If the failure is related to authentication or permissions, check the `MAINTAINER_PERSONAL_ACCESS_TOKEN` secret:

1. Go to **Repository Settings > Secrets and variables > Actions**
2. Verify `MAINTAINER_PERSONAL_ACCESS_TOKEN` exists and is not expired
3. If expired, create a new fine-grained PAT:
   - GitHub Settings > Developer settings > Personal access tokens > Fine-grained tokens
   - Name: `nvidia-ci-automated-tests`
   - Resource owner: `rh-ecosystem-edge`
   - Repository: `nvidia-ci` only
   - Permissions: Contents (R/W), Pull requests (R/W)
   - Request org admin approval (see [GitHub docs](https://docs.github.com/en/organizations/managing-programmatic-access-to-your-organization/managing-requests-for-personal-access-tokens-in-your-organization))
   - Update the repository secret with the new token
