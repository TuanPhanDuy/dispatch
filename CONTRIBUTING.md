# Contributing

## Branching model (Git Flow)

| Branch | Purpose | Branches from | Merges into |
|---|---|---|---|
| `main` | Released code only. Every merge is a release and gets a `vX.Y.Z` tag. | — | — |
| `develop` | Integration branch; the default branch on GitHub. Always builds and passes tests. | `main` (once) | — |
| `feature/<topic>` | One feature, fix, docs or CI change. | `develop` | `develop` |
| `release/<X.Y.Z>` | Freezes a release: version bumps, last fixes, release notes. No new features. | `develop` | `main`, then back into `develop` |
| `hotfix/<topic>` | Urgent fix to a released version. | `main` | `main`, then back into `develop` |

Never commit directly to `main` or `develop`; everything lands through a pull request.

Name branches after the topic in kebab-case: `feature/tui-panes`, `feature/fix-ci-lint`, `hotfix/daemon-crash-on-resize`.

## Working on a feature

```sh
git switch develop && git pull
git switch -c feature/<topic>
# ...commit...
go vet ./... && go test ./... -race     # must be green before opening a PR
git push -u origin feature/<topic>
gh pr create --base develop
```

Keep each branch to one topic. If you notice an unrelated problem along the way, fix it on its own branch.

Feature PRs are **squash-merged** into `develop`, so the PR title becomes the commit message — write it as a short imperative summary ("Tile sessions in resizable panes"). Delete the branch after merging.

## Cutting a release

```sh
git switch develop && git pull
git switch -c release/X.Y.Z
# bump versions if needed, update docs, fix only release blockers
git push -u origin release/X.Y.Z
gh pr create --base main --title "Release vX.Y.Z"
```

After the release PR is merged into `main` (**merge commit, not squash**, so `main` and `develop` share history):

```sh
git switch main && git pull
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z               # triggers the Release workflow
git switch develop && git pull
git merge --no-ff main               # bring release fixes back into develop
git push
```

Pushing the tag runs `.github/workflows/release.yml`, which publishes the CLI/TUI archives (GoReleaser) and the macOS DMGs to the GitHub release.

## Hotfixes

```sh
git switch main && git pull
git switch -c hotfix/<topic>
# fix + regression test
gh pr create --base main
```

Once merged, tag the patch release from `main` as above, then merge `main` back into `develop`.

## Versioning

[Semantic Versioning](https://semver.org): `MAJOR.MINOR.PATCH`. New features bump MINOR, fixes bump PATCH. While the project is `0.x`, breaking changes bump MINOR.

## Checks

CI (`.github/workflows/ci.yml`) runs vet, tests on macOS and Linux, and lint on every pull request and on every push to `main` and `develop`. Don't merge a red PR.

For GUI changes, also run the app (`cd gui && npm start`) and exercise what you changed; for packaging changes, build the DMG locally with `cd gui && npm run dist`.
