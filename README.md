# git-repo-template

GitHub repo template setup for all git-repos

## Included CI Checks

The default GitHub Actions workflow in `.github/workflows/ci.yml` runs:

- YAML validation for `.yml` and `.yaml` files (via PyYAML)
- Basic secret-pattern scanning across YAML/JSON sources while excluding common generated/vendor paths
