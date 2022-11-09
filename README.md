# go-alfred-prs

A Golang port of [GitHub PR workflow][1] for [Alfred][2].

**[DOWNLOAD][3]**

This workflow is powered by **[AwGo][4]** and **[go-github][5]**.

The original GitHub PR workflow was developed in Python 2 which reached its end-of-life on Jan 1st, 2020.
Also, Apple no longer bundles Python 2 in macOS since macOS 12.3 Monterey.

### Features
* shows you all relevant pull requests (the ones you want to see anyway)
* optionally displays ✅ or ❌ for each pull request that was reviewed
* securely stores your GitHub API token in the system keychain
* works with GitHub and GitHub Enterprise
* no extra runtime dependencies - just what you'd expect from a Go application

## Commands
* **`ghpr`** - display your pull requests
* **`ghpr-update`** - manually refresh the list of PRs
* **`ghpr-host`** - set a custom GitHub URL
* **`ghpr-auth`** - set your GitHub API token

## Workflow Environment Variables
- **`CACHE_AGE_SECONDS`** - TTL for internal cache of pull requests (default is `600`)
- **`GIT_BASE_URL`** - url of the GitHub instance (default is `github.com`)
- **`QUERY_BY_ROLES`** - filter for the displayed pull requests (default is `-assignee,-author,+involves,-mentions,-review-requested`)
- **`SHOW_REVIEWS`** - flag to enable displaying PR reviews (default is `false`)

## Release process
A new release is automatically published by GitHub Actions when the change to the workflow [version](version) is detected.

At the moment, the workflow binary is built for `amd64` architecture only.

<details>
<summary>Why `amd64` only?</summary>

While it is possible to compile the workflow for `amd64` and `arm64`, and merge the two into a universal binary, - doing so would double the size of the executable.

And anyway, Mac computers with Apple silicon can run `amd64` executables seamlessly using [Rosetta][6].

To install Rosetta for the first time on a Mac with Apple silicon, run the command below:

    $ softwareupdate --install-rosetta

</details>

## Other GitHub workflows for Alfred
* https://github.com/gharlan/alfred-github-workflow (all batteries included, requires php runtime, stores API token in a SQLite db)
* https://github.com/edgarjs/alfred-github-repos (requires ruby, stores API token in workflow env)


[1]: https://github.com/renuo/alfred-pr-workflow
[2]: https://alfredapp.com
[3]: https://github.com/AndreyBozhko/go-alfred-prs/releases
[4]: https://github.com/deanishe/awgo
[5]: https://github.com/google/go-github
[6]: https://en.wikipedia.org/wiki/Rosetta_(software)
