![Docker Image CI](https://github.com/relizaio/rearm-cli/actions/workflows/github_actions.yml/badge.svg?branch=main)
[![Go Report Card](https://goreportcard.com/badge/github.com/relizaio/rearm-cli)](https://goreportcard.com/report/github.com/relizaio/rearm-cli)
# Rearm CLI

This tool allows for command-line interactions with [ReARM](https://github.com/relizaio/rearm) (currently in Public Beta). ReARM is a system to manage software and (in the future) hardware releases with their Metadata, including SBOMs / xBOMs and other artifacts. We mainly support [CycloneDX](https://cyclonedx.org/) standard.

Community forum and support is available via [Discord](https://discord.com/invite/UTxjBf9juQ) - use #rearm channel.

Container image URI: registry.relizahub.com/library/rearm-cli.

## Download Rearm CLI

Below are the available downloads for the latest version of the Rearm CLI (25.12.1). Please download the proper package for your operating system and architecture.

The CLI is distributed as a single binary. Install by unzipping it and moving it to a directory included in your system's PATH.

[SHA256 checksums](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/sha256sums.txt)

macOS: [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/rearm-25.12.1-darwin-amd64.zip)

FreeBSD: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/rearm-25.12.1-freebsd-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/rearm-25.12.1-freebsd-amd64.zip) | [Arm](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/rearm-25.12.1-freebsd-arm.zip)

Linux: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/rearm-25.12.1-linux-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/rearm-25.12.1-linux-amd64.zip) | [Arm](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/rearm-25.12.1-linux-arm.zip) | [Arm64](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/rearm-25.12.1-linux-arm64.zip)

OpenBSD: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/rearm-25.12.1-openbsd-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/rearm-25.12.1-openbsd-amd64.zip)

Solaris: [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/rearm-25.12.1-solaris-amd64.zip)

Windows: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/rearm-25.12.1-windows-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.12.1/rearm-25.12.1-windows-amd64.zip)

It is possible to set authentication data via explicit flags, login command (see below) or following environment variables:

- REARM_APIKEYID - for API Key ID
- REARM_APIKEY - for API Key itself
- REARM_URI - for ReARM Uri

# Table of Contents - Use Cases
1. [Get Version Assignment From ReARM](#1-use-case-get-version-assignment-from-rearm)
2. [Send Release Metadata to ReARM](#2-use-case-send-release-metadata-to-rearm)
3. [Check If Artifact Hash Already Present In Some Release](#3-use-case-check-if-artifact-hash-already-present-in-some-release)
4. [Request Latest Release Per Component Or Product](#4-use-case-request-latest-release-per-component-or-product)
5. [Persist ReARM Credentials in a Config File](#5-use-case-persist-rearm-credentials-in-a-config-file)
6. [Create New Component in ReARM](#6-use-case-create-new-component-in-rearm)
7. [Synchronize Live Git Branches with ReARM](#7-use-case-synchronize-live-git-branches-with-rearm)
8. [Add Outbound Deliverables to Release](#8-use-case-add-outbound-deliverables-to-release)
9. [xBOM Utilities](docs/bomutils.md)
    1. [Fix incorrect OCI purl generated via cdxgen](docs/bomutils.md#91-fix-incorrect-oci-purl-generated-via-cdxgen)
    2. [BOM supplier enrichment with BEAR](docs/bomutils.md#92-bom-supplier-enrichment-with-bear)
    3. [Convert SPDX to CycloneDX](docs/bomutils.md#93-convert-spdx-to-cyclonedx)
    4. [Merge Multiple BOMs](docs/bomutils.md#94-merge-multiple-boms)
10. [Finalize Release After CI Completion](#10-use-case-finalize-release-after-ci-completion)
11. [Transparency Exchange API (TEA) Commands](docs/tea.md)
    1. [Transparency Exchange API (TEA) Discovery](docs/tea.md#111-transparency-exchange-api-tea-discovery)
    2. [Complete TEA Flow - Product and Component Details](docs/tea.md#112-complete-tea-flow---product-and-component-details)
12. [Oolong TEA Server Content Management Commands](docs/oolong.md)
    1. [Add Product](docs/oolong.md#121-add-product)
    2. [Add Component](docs/oolong.md#122-add-component)
    3. [Add Component Release](docs/oolong.md#123-add-component-release)
    4. [Add Product Release](docs/oolong.md#124-add-product-release)
    5. [Add Artifact](docs/oolong.md#125-add-artifact)
    6. [Add Artifact to Releases](docs/oolong.md#126-add-artifact-to-releases)
13. [VCS-based Component Resolution](#13-use-case-vcs-based-component-resolution)
    1. [Creating Components](#131-creating-components)
    2. [Getting Versions](#132-getting-versions)
    3. [Adding Releases](#133-adding-releases)
    4. [Monorepo Support](#134-monorepo-support)

## 1. Use Case: Get Version Assignment From ReARM

This use case requests Version from ReARM for our component. Note that component schema must be preset on ReARM prior to using this API. API key must also be generated for the component from ReARM.

Sample command for semver version schema:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    getversion    \
    -i component_or_organization_wide_rw_api_id    \
    -k component_or_organization_wide_rw_api_key    \
    -b main    \
    --pin 1.2.patch
```

Sample command with commit details for a git commit:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    getversion    \
    -i component_or_organization_wide_rw_api_id    \
    -k component_or_organization_wide_rw_api_key    \
    -b main    \
    --vcstype git \
    --commit $CI_COMMIT_SHA \
    --commitmessage $CI_COMMIT_MESSAGE \
    --vcsuri $CI_PROJECT_URL \
    --date $(git log -1 --date=iso-strict --pretty='%ad')
```

Sample command to obtain only version info and skip creating the release:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    getversion    \
    -i component_or_organization_wide_rw_api_id    \
    -k component_or_organization_wide_rw_api_key    \
    -b main    \
    --onlyversion
```

Sample command using VCS-based component resolution:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    getversion    \
    -i organization_wide_rw_api_id    \
    -k organization_wide_rw_api_key    \
    --vcsuri github.com/myorg/myapp    \
    -b main
```

This approach identifies the component by VCS URI instead of component UUID, simplifying CI/CD integration. For monorepos with multiple components in one repository, add `--repo-path` to specify the subdirectory (e.g., `--repo-path frontend`).

Flags stand for:

- **getversion** - command that denotes we are obtaining the next available release version for the branch. Note that if the call succeeds, the version assignment will be recorded and will not be given again by ReARM, even if it is not consumed. It will create the release with 'PENDING' lifecycle.
- **-i** - flag for component api id (required).
- **-k** - flag for component api key (required).
- **-b** - flag to denote branch (required). If the branch is not recorded yet, ReARM will attempt to create it.
- **component** - flag to denote component uuid (optional). Required if organization-wide read-write key is used, ignored if component specific api key is used.
- **--pin** - flag to denote branch pin (optional for existing branches, required for new branches). If supplied for an existing branch and pin is different from current, it will override current pin.
- **--vcsuri** - flag to denote vcs uri (optional). This flag is needed if we want to set a commit for the release. However, soon it will be needed only if the vcs uri is not yet set for the component.
- **--vcstype** - flag to denote vcs type (optional). Supported values: git, svn, mercurial. As with vcsuri, this flag is needed if we want to set a commit for the release. However, soon it will be needed only if the vcs uri is not yet set for the component.
- **--commit** - flag to denote vcs commit id or hash (optional). This is needed to provide source code entry metadata into the release.
- **--commitmessage** - flag to denote vcs commit message (optional). Alongside *commit* flag this would be used to provide source code entry metadata into the release.
- **--commits** - flag to provide base64-encoded list of commits in the format *git log --date=iso-strict --pretty='%H|||%ad|||%s|||%an|||%ae' | base64 -w 0* (optional). If *commit* flag is not set, top commit will be used as commit bound to release.
- **--date** - flag to denote date time with timezone when commit was made, iso strict formatting with timezone is required, i.e. for git use git log --date=iso-strict (optional).
- **--vcstag** - flag to denote vcs tag (optional). This is needed to include vcs tag into commit, if present.
- **--metadata** - flag to set version metadata (optional). This may be semver metadata or custom version schema metadata.
- **--modifier** - flag to set version modifier (optional). This may be semver modifier or custom version schema metadata.
- **--manual** - flag to indicate a manual release (optional). Sets lifecycle as 'DRAFT', otherwise 'PENDING' lifecycle is set.
- **--onlyversion** - boolean flag to skip creation of the release (optional). Default is false.
- **--repo-path** - Repository path for monorepo components (optional).
- **--action** - Bump action name: bump | bumppatch | bumpminor | bumpmajor | bumpdate (optional).

## 2. Use Case: Send Release Metadata to ReARM

This use case is commonly used in the CI workflow to stream Release metadata to ReARM. As in previous case, API key must be generated for the component on ReARM prior to sending release details.

Sample command to send release details:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    addrelease    \
    -i component_or_organization_wide_rw_api_id    \
    -k component_or_organization_wide_rw_api_key    \
    -b master    \
    -v 20.02.3    \
    --vcsuri github.com/registry.relizahub.com/library/rearm-cli    \
    --vcstype git    \
    --commit 7bfc5ce7b0da277d139f7993f90761223fa54442    \
    --vcstag 20.02.3    \
    --odelid registry.relizahub.com/library/rearm-cli    \
    --odelbuildid 1    \
    --odelcimeta Github Actions    \
    --odeltype CONTAINER    \
    --odeldigests sha256:4e8b31b19ef16731a6f82410f9fb929da692aa97b71faeb1596c55fbf663dcdd
```

Sample command with all three artifact types (Source Code Entry, Release, and Deliverable artifacts):

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    addrelease    \
    -i component_or_organization_wide_rw_api_id    \
    -k component_or_organization_wide_rw_api_key    \
    -b main    \
    -v 1.0.0    \
    --vcsuri github.com/myorg/myapp    \
    --vcstype git    \
    --commit abc123def456    \
    --vcstag v1.0.0    \
    --date 2025-11-20T15:30:00Z    \
    --scearts '[{"displayIdentifier":"build-log","type":"BUILD_META","storedIn":"REARM","filePath":"./build.log"}]'    \
    --releasearts '[{"displayIdentifier":"release-notes-v1.0.0","type":"RELEASE_NOTES","storedIn":"EXTERNALLY","downloadLinks":[{"uri":"https://docs.example.com/releases/v1.0.0","content":"Release Notes"}]},{"displayIdentifier":"security-report","type":"USER_DOCUMENT","storedIn":"REARM","filePath":"./security-report.pdf"}]'    \
    --odelid myapp-docker-image    \
    --odeltype CONTAINER    \
    --odeldigests sha256:abc123def456    \
    --odelartsjson '[{"displayIdentifier":"myapp-sbom","type":"BOM","bomFormat":"CYCLONEDX","storedIn":"REARM","inventoryTypes":["SOFTWARE"],"filePath":"./sbom.json"},{"displayIdentifier":"myapp-attestation","type":"ATTESTATION","storedIn":"REARM","filePath":"./attestation.json"}]'
```

**Understanding Artifact Types:**

The addrelease command supports three types of artifacts, each serving a different purpose:

1. **Source Code Entry Artifacts (`--scearts`)**: Artifacts attached to the commit/source code entry
   - Examples: Source Code SBOM
   - Use when: The artifact is specific to the source code at that commit

2. **Release Artifacts (`--releasearts`)**: Artifacts attached directly to the release
   - Examples: Release notes, security reports, user documentation, certifications
   - Use when: The artifact describes or documents the release as a whole

3. **Deliverable Artifacts (`--odelartsjson`)**: Artifacts attached to specific deliverables
   - Examples: SBOMs, attestations, signatures, VEX documents
   - Use when: The artifact is specific to a deliverable (container, binary, package)
   - Note: Must have one `--odelartsjson` entry per `--odelid` deliverable

**Using Tags on Artifacts:**

Tags can be added to artifacts in `--releasearts`, `--scearts`, and `--odelartsjson` using the `tags` field. Tags are key-value pairs useful for categorization, filtering, and metadata.

Example with tags on release artifacts:
```bash
--releasearts '[{"displayIdentifier":"security-report","type":"USER_DOCUMENT","storedIn":"REARM","filePath":"./security-report.pdf","tags":[{"key":"category","value":"security"},{"key":"reviewed","value":"true"}]}]'
```

Example with tags on SCE artifacts:
```bash
--scearts '[{"displayIdentifier":"source-sbom","type":"BOM","bomFormat":"CYCLONEDX","storedIn":"REARM","filePath":"./source-sbom.json","tags":[{"key":"generator","value":"cdxgen"},{"key":"scope","value":"source"}]}]'
```

Example with tags on deliverable artifacts:
```bash
--odelartsjson '[{"displayIdentifier":"container-sbom","type":"BOM","bomFormat":"CYCLONEDX","storedIn":"REARM","inventoryTypes":["SOFTWARE"],"filePath":"./sbom.json","tags":[{"key":"format","value":"cyclonedx"},{"key":"version","value":"1.5"}]}]'
```

Example with nested artifacts (artifacts within artifacts) with tags:
```bash
--odelartsjson '[{"displayIdentifier":"main-sbom","type":"BOM","bomFormat":"CYCLONEDX","storedIn":"REARM","filePath":"./main-sbom.json","tags":[{"key":"primary","value":"true"}],"artifacts":[{"displayIdentifier":"nested-vex","type":"VEX","storedIn":"REARM","filePath":"./vex.json","tags":[{"key":"type","value":"openvex"}]}]}]'
```

Sample command using VCS-based component resolution:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    addrelease    \
    -i organization_wide_rw_api_id    \
    -k organization_wide_rw_api_key    \
    --vcsuri github.com/myorg/myapp    \
    -b main    \
    -v 1.0.0    \
    --vcstype git    \
    --commit abc123def456
```

This approach identifies the component by VCS URI instead of component UUID, eliminating the need to track UUIDs in CI/CD workflows. For monorepos with multiple components in one repository, add `--repo-path` to specify the subdirectory (e.g., `--repo-path frontend`).

Flags stand for:

- **addrelease** - command that denotes we are sending Release Metadata of a Component to ReARM.
- **-i** - flag for component api id or organization-wide read-write api id (required).
- **-k** - flag for component api key or organization-wide read-write api key (required).
- **-b** - flag to denote branch (required). If branch is not recorded yet, ReARM will attempt to create it.
- **-v** - version (required). Note that ReARM will reject the call if a release with this exact version is already present for this component.
- **endpoint** - flag to denote test endpoint URI (optional). This would be useful for systems where every release gets test URI.
- **component** - flag to denote component uuid (optional). Required if organization-wide read-write key is used, ignored if component specific api key is used.
- **vcsuri** - flag to denote vcs uri (optional). Currently this flag is needed if we want to set a commit for the release. However, soon it will be needed only if the vcs uri is not yet set for the component.
- **vcstype** - flag to denote vcs type (optional). Supported values: git, svn, mercurial. As with vcsuri, this flag is needed if we want to set a commit for the release. However, soon it will be needed only if the vcs uri is not yet set for the component.
- **commit** - flag to denote vcs commit id or hash (optional). This is needed to provide source code entry metadata into the release.
- **commitmessage** - flag to denote vcs commit subject (optional). Alongside *commit* flag this would be used to provide source code entry metadata into the release.
- **commits** - flag to provide base64-encoded list of commits in the format *git log --date=iso-strict --pretty='%H|||%ad|||%s|||%an|||%ae' | base64 -w 0* (optional). If *commit* flag is not set, top commit will be used as commit bound to release.
- **scearts** - flag to denote metadata Artifacts set on Source Code Entry - or commit (optional). Expects JSON Array representation, with Keys for each object: type, bomFormat, filePath. Sample entry:
```json
[{"bomFormat": "CYCLONEDX","type": "BOM","filePath": "./fs.cdx.bom.json"}]
```
- **releasearts** - flag to denote metadata Artifacts attached directly to the release (optional). Expects JSON Array representation, with Keys for each object: displayIdentifier, type, storedIn, filePath (or downloadLinks for external storage). Sample entry for internal storage:
```json
[{"displayIdentifier": "release-notes-v1.0.0","type": "RELEASE_NOTES","storedIn": "REARM","filePath": "./release-notes.md"}]
```
Sample entry for external storage:
```json
[{"displayIdentifier": "release-notes-v1.0.0","type": "RELEASE_NOTES","storedIn": "EXTERNALLY","downloadLinks": [{"uri": "https://docs.example.com/releases/v1.0.0","content": "Release Notes"}]}]
```
- **date** - flag to denote date time with timezone when commit was made, iso strict formatting with timezone is required, i.e. for git use git log --date=iso-strict (optional).
- **vcstag** - flag to denote vcs tag (optional). This is needed to include vcs tag into commit, if present.
- **lifecycle** - flag to denote release lifecycle (optional). Set to 'REJECTED' for failed releases, otherwise 'DRAFT' is used, may be also set to 'ASSEMBLED'.
- **odelid** - flag to denote output deliverable identifier (optional). This is required to add output deliverable metadata into release.
- **odelbuildid** - flag to denote output deliverable build id (optional). This flag is optional and may be used to indicate build system id of the release (i.e., this could be circleci build number).
- **odelbuilduri** - flag to denote output deliverable build uri (optional). This flag is optional and is used to denote the uri for where the build takes place.
- **odelcimeta** - flag to denote output deliverable CI metadata (optional). This flag is optional and like odelbuildid may be used to indicate build system metadata in free form.
- **odeltype** - flag to denote output deliverable type (optional). Types are based on [CycloneDX 1.6 spec](https://github.com/CycloneDX/specification/blob/master/schema/bom-1.6.schema.json) - refer to lines 836-850 in the spec. Supported values (case-insensitive): Application, Framework, Library, Container, Platform, Operatine-system, Device, Device-driver, Firmware, File, Machine-learning-model, Data, Cryptographic-asset.
- **--odelidentifiers** - Deliverable Identifiers (i.e. PURL) IdentifierType-Value Pairs (multiple allowed, separate several IdentifierType-Value pairs for one Deliverable with commas, and seprate IdentifierType-Value in a pair with colon, e.g. --odelidentifiers "PURL:somepurl,TEI:sometei")
- **datestart** - flag to denote output deliverable build start date and time, must conform to ISO strict date (in bash, use *date -Iseconds*, if used there must be one datestart flag entry per deliverable, optional).
- **dateend** - flag to denote output deliverable build end date and time, must conform to ISO strict date (in bash, use *date -Iseconds*, if used there must be one datestart flag entry per deliverable, optional).
- **odelpublisher** - flag to denote output deliverable publisher (if used there must be one publisher flag entry per deliverable, optional).
- **odeldigests** - flag to denote output deliverable digests (optional). By convention, digests must be prefixed with type followed by colon and then actual digest hash, i.e. *sha256:4e8b31b19ef16731a6f82410f9fb929da692aa97b71faeb1596c55fbf663dcdd* - here type is *sha256* and digest is *4e8b31b19ef16731a6f82410f9fb929da692aa97b71faeb1596c55fbf663dcdd*. Multiple digests are supported and must be comma separated. I.e.:

```bash
--odeldigests sha256:4e8b31b19ef16731a6f82410f9fb929da692aa97b71faeb1596c55fbf663dcdd,sha1:fe4165996a41501715ea0662b6a906b55e34a2a1
```
- **odelartsjson** - flag to denote metadata Artifacts set on Output Deliverable(optional). Format is similar to *scearts* - expects JSON Array representation, with Keys for each object: type, bomFormat, filePath
- **--repo-path** - Repository path for monorepo components (optional).
- **--releasearts** - Release Artifacts json array (optional).
- **--odelgroup** - Deliverable group (multiple allowed, optional).
- **--odelpackage** - Deliverable package type (multiple allowed, optional).
- **--osarr** - Deliverable supported OS array (multiple allowed, use comma seprated values for each deliverable, optional).
- **--cpuarr** - Deliverable supported CPU array (multiple allowed, use comma seprated values for each deliverable, optional).
- **--odelversion** - Deliverable version, if different from release (multiple allowed, optional).
- **--tagsarr** - Deliverable Tag Key-Value Pairs (multiple allowed, separate several tag key-value pairs for one Deliverable with commas, and seprate key-value in a pair with colon, optional).
- **--stripbom** - flag to toggle stripping of bom metadata for hash comparison (optional). Default is true. Supported values: true|false.

Note that multiple deliverables per release are supported. In which case deliverable specific flags (odelid, odelbuildid, odelbuilduri, odelcimeta, odeltype, odeldigests, odelartsjson must be repeated for each deliverable).

For sample of how to use workflow in CI, refer to the ReARM Add Release GitHub Action[here](https://github.com/relizaio/rearm-add-release).

## 3. Use Case: Check If Artifact Hash Already Present In Some Release

This is particularly useful for monorepos to see if there was a change in sub-component or not. We supply an artifact hash to ReARM - and if it's present already, we get release details; if not - we get an empty json response {}. Search space is scoped to a single component which is defined by API Id and API Key.

Sample command:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    checkhash    \
    -i component_or_org_wide_api_id    \
    -k component_or_org_wide_api_key    \
    --hash sha256:hash
```

Flags stand for:

- **checkhash** - command that denotes we are checking artifact hash.
- **-i** - flag for component api id (required).
- **-k** - flag for component api key (required).
- **--hash** - flag to denote actual hash (required). By convention, hash must include hashing algorithm as its first part, i.e. sha256: or sha512:
- **--component** - flag to denote UUID of specific Component, UUID must be obtained from ReARM (optional, required if org-wide or user api key is used).

## 4. Use Case: Request Latest Release Per Component Or Product

This use case is when ReARM is queried either by CI or CD environment or by integration instance to check latest release version available per specific Component or Product.

Sample command:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    getlatestrelease    \
    -i api_id    \
    -k api_key    \
    --component b4534a29-3309-4074-8a3a-34c92e1a168b    \
    --branch main
```

Flags stand for:

- **getlatestrelease** - command that denotes we are requesting latest release data for Component or Product from ReARM
- **-i** - flag for api id which can be either api id for this component or organization-wide read API (required).
- **-k** - flag for api key which can be either api key for this component or organization-wide read API (required).
- **--component** - flag to denote UUID of specific Component or Product, UUID must be obtained from [ReARM](https://relizahub.com) (optional if component api key is used, otherwise required).
  - **--product** - flag to denote UUID of Product which packages Component or Product for which we inquiry about its version via --component flag, UUID must be obtained from [ReARM](https://relizahub.com) (optional).
  - **--branch** - flag to denote required branch of chosen Component or Product (optional, if not supplied settings from ReARM UI are used).
  - **--lifecycle** - Lifecycle of the last known release to return, default is 'ASSEMBLED' (optional, can be - [CANCELLED, REJECTED, PENDING, DRAFT, ASSEMBLED, GENERAL_AVAILABILITY, END_OF_SUPPORT]). Will include all higher level lifecycles, i.e. if set to CANCELLED, will return releases in any lifecycle.
  - **--operator** - Match operator for a list of approvals, 'AND' or 'OR', default is 'AND' (optional).
  - **--approvalentry** - Approval entry names or IDs (optional, multiple allowed).
  - **--approvalstate** - Approval states corresponding to approval entries, can be 'APPROVED', 'DISAPPROVED' or 'UNSET' (optional, multiple allowed, required if approval entries are present).
  - **--env** - Environment to obtain approvals details from (optional).
  - **--instance** - Instance ID for which to check release (optional).
  - **--namespace** - Namespace within instance for which to check release, only matters if instance is supplied (optional).
  - **--tagkey** - Tag key to use for picking artifact (optional).
  - **--tagval** - Tag value to use for picking artifact (optional).

## 5. Use Case: Persist ReARM Credentials in a Config File

This use case is for the case when we want to persist ReARM API Credentials and URL in a config file.

The `login` command saves `API ID`, `API KEY` and `URI` as specified by flags in a config file `.rearm.env` in the home directory for the executing user.

Sample Command:

```bash
docker run --rm \
    -v ~:/home/apprunner \
    registry.relizahub.com/library/rearm-cli \
    login \
    -i api_id \
    -k api_key \
    -u rearm_server_uri
```

Flags stand for:

- **-i** - flag for api id.
- **-k** - flag for api key.
- **-u** - flag for rearm hub uri.

## 6. Use Case: Create New Component in ReARM

This use case creates a new component for our organization. API key must be generated prior to using.

Sample command to create component:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    createcomponent    \
    -i org_api_id    \
    -k org_api_key    \
    --name componentname    \
    --type component    \
    --versionschema semver    \
    --featurebranchversioning Branch.Micro    \
    --vcsuri github.com/registry.relizahub.com/library/rearm-cli
```

Sample command for monorepo component:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    createcomponent    \
    -i org_api_id    \
    -k org_api_key    \
    --name myapp-frontend    \
    --type component    \
    --versionschema semver    \
    --vcsuri github.com/myorg/myrepo    \
    --repo-path frontend
```

**⚠️ Important: Version Schema Requirement**

The `--versionschema` flag is **critical** for components created via API. Without it:
- `getversion` command will fail with "missing version schema configuration" error
- Version generation will not work
- Component will need manual update via UI

**Always include**: `--versionschema semver` when creating components.

Flags stand for:

- **createcomponent** - command that denotes we are creating a new component for our organization. Note that a vcs repository must either already exist or be created during this call.
- **-i** - flag for org api id (required).
- **-k** - flag for org api key (required).
- **name** - flag to denote component name (required).
- **type** - flag to denote component type (required). Supported values: component, product.
- **defaultbranch** - flag to denote default branch name (optional, if not set "main" will be used). Available names are either main or master.
- **versionschema** - flag to denote version schema (optional, if not set "semver" will be used). [Available version schemas](https://github.com/relizaio/versioning).
- **featurebranchversioning** - flag to denote feature branch version schema (optional, if not set "Branch.Micro will be used).
- **vcsuuid** - flag to denote uuid of vcs repository for the component (for existing repositories, either this flag or vcsuri are required).
- **vcsuri** - flag to denote uri of vcs repository for the component, if existing repository with uri does not exist and vcsname and vcstype are not set, ReARM will attempt to autoparse github, gitlab, and bitbucket uri's.
- **vcsname** - flag to denote name of vcs repository to create for component (required if ReARM cannot parse uri).
- **vcstype** - flag to denote type of vcs to create for component. Supported values: git, svn, mercurial (required if ReARM cannot parse uri).
- **includeapi** - boolean flag to return component api key and id of newly created component (optional). Default is false.
- **--repo-path** - Repository path for monorepo components (optional).

## 7. Use Case: Synchronize Live Git Branches with ReARM

Sends a list of Live Git branches to ReARM. Non-live branches present on ReARM will be archived.

Sample command using component UUID:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    syncbranches \
    -i api_id \ 
    -k api_key    \
    --component component_uuid    \
    --livebranches $(git branch -r --format="%(refname)" | base64 -w 0)
```

Sample command using VCS-based component resolution:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    syncbranches \
    -i organization_wide_rw_api_id    \
    -k organization_wide_rw_api_key    \
    --vcsuri github.com/myorg/myrepo    \
    --repo-path frontend    \
    --livebranches $(git branch -r --format="%(refname)" | base64 -w 0)
```

Flags stand for:
- **--component** - flag to specify component uuid, which can be obtained from the component settings on ReARM UI (either this flag, vcsuri, or component's API key must be used).
- **--vcsuri** - URI of VCS repository for VCS-based component resolution (optional, alternative to component UUID).
- **--repo-path** - Repository path for monorepo components (optional, use with vcsuri when multiple components share one repository).
- **--livebranches** - base64'd list of git branches, for local branches use `git branch --format=\"%(refname)\" | base64 -w 0` to obtain, for remote branches use `git branch -r --format=\"%(refname)\" | base64 -w 0`. Choose between local and remote branches based on your CI context.


## 8. Use Case: Add Outbound Deliverables to Release

This use case adds outbound deliverables to a ReARM Release. Release must be in Pending or Draft lifecycle.

Sample command:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    addodeliverable \
    -i api_id \ 
    -k api_key    \
    --odelid registry.relizahub.com/library/rearm-cli    \
    --odelbuildid 1    \
    --odelcimeta Github Actions    \
    --odeltype CONTAINER    \
    --odeldigests sha256:4e8b31b19ef16731a6f82410f9fb929da692aa97b71faeb1596c55fbf663dcdd
    --releaseid 22a98c21-ab90-4a17-94f5-2dd81be5647b
```

Flags stand for:
- **--component** - Component UUID for this release, required if organization wide API Key is used
- **--cpuarr** - Array of CPU architectures supported by this Deliverable (optional, multiple allowed)
- **--dateend** - Deliverable Build End date and time (optional, multiple allowed)
- **--datestart** - Deliverable Build Start date and time (optional, multiple allowed)
- **--odelidentifiers** - Deliverable Identifiers (i.e. PURL) IdentifierType-Value Pairs (multiple allowed, separate several IdentifierType-Value pairs for one Deliverable with commas, and seprate IdentifierType-Value in a pair with colon, e.g. --odelidentifiers "PURL:somepurl,TEI:sometei")
- **--odelartsjson** -Deliverable Artifacts json array (optional, multiple allowed, use a json array for each deliverable, similar to add release use case)
- **--odelbuildid** - Deliverable Build ID (optional, multiple allowed)
- **--odelbuilduri** - Deliverable Build URI (multiple allowed)
- **--odelcimeta** - Deliverable CI Meta (multiple allowed)
- **--odeldigests** - Deliverable Digests (multiple allowed, separate several digests for one Deliverable with commas)
- **--odelid** - Deliverable Primary Identifier (multiple allowed)
- **--odelpackage** - Deliverable package type (i.e. Maven) (multiple allowed)
- **--odelpublisher** - Deliverable publisher (multiple allowed)
- **--odelgroup** - Deliverable group (multiple allowed)
- **--odelversion** -  Deliverable version, if different from release (multiple allowed)
- **--osarr** - Deliverable supported OS array (multiple allowed, use comma seprated values for each deliverable)
- **--releaseid** - UUID of release to add deliverable to (either releaseid or component, branch, and version must be set)
- **--tagsarr** - Deliverable Tag Key-Value Pairs (multiple allowed, separate several tag key-value pairs for one Deliverable with commas, and seprate key-value in a pair with colon)
- **--version** - Release version (either releaseid or component, branch, and version must be set)
- **--branch** - Release branch (either releaseid or component, branch, and version must be set)
- **--stripbom** - flag to toggle stripping of bom metadata for hash comparison (optional - can). Default is true. Supported values: true|false.

## 9. Use Case: xBOM Utilities
See [bomutils documentation](docs/bomutils.md)

### 10. Use Case: Finalize Release After CI Completion

The `releasefinalizer` command is used to run a release finalizer, which can be executed after submitting a release or after adding a new deliverable to a release. This command signals the completion of the CI process for a release in ReARM, ensuring all post-release or post-deliverable actions are triggered.

Typical workflow:

Submit a release or add a new deliverable to a release
Run the release finalizer to complete the CI process

Sample command (Docker):
```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    releasefinalizer \
    -i <component_or_org_wide_api_id> \
    -k <component_or_org_wide_api_key> \
    --releaseid <release_uuid>
```

Flags stand for :
- **--releaseid** - UUID of the release to finalize (required)

This command can be integrated into CI/CD workflows to signal the end of the release process, ensuring that all finalization hooks and actions are called in ReARM.

## 11. Use Case: Transparency Exchange API (TEA) Commands
Base Command: `tea`
See [tea documentation](docs/tea.md)

## 12. Use Case: Oolong TEA Server Content Management Commands
Base Command: `oolong`
See [oolong documentation](docs/oolong.md)

## 13. Use Case: VCS-based Component Resolution

ReARM supports identifying components by VCS repository URI in addition to component UUID:

**Traditional (UUID-based):**
```bash
getversion --component "4b272da8-2fea-4f13-a6a4-8e6e746c6e86" -b "main"
```

**VCS-based (Recommended for CI/CD):**
```bash
getversion --vcsuri "github.com/myorg/myapp" -b "main"
```

**Benefits**: Eliminates UUID management, uses repository context already available in CI/CD pipelines.

**For Monorepos**: Add `--repo-path` when multiple components share one repository:
```bash
getversion --vcsuri "github.com/myorg/myrepo" --repo-path "frontend" -b "main"
```

---

### 13.1: Creating Components

**Single Repository:**
```bash
createcomponent --name "myapp" --type "component" \
  --vcsuri "github.com/myorg/myapp" --versionschema "semver"
```

**Monorepo (add --repo-path):**
```bash
createcomponent --name "myapp-frontend" --type "component" \
  --vcsuri "github.com/myorg/myrepo" --repo-path "frontend" --versionschema "semver"
```

**Key Flags:**
- `--vcsuri` - Repository URI for VCS-based resolution
- `--versionschema "semver"` - **Required** for version generation
- `--repo-path` - Optional, only for monorepos

---

### 13.2: Getting Versions

**Single Repository:**
```bash
getversion --vcsuri "github.com/myorg/myapp" -b "main"
```

**Monorepo:**
```bash
getversion --vcsuri "github.com/myorg/myrepo" --repo-path "frontend" -b "main"
```

---

### 13.3: Adding Releases

**Single Repository:**
```bash
addrelease --vcsuri "github.com/myorg/myapp" -b "main" -v "1.0.0"
```

**Monorepo:**
```bash
addrelease --vcsuri "github.com/myorg/myrepo" --repo-path "frontend" -b "main" -v "1.0.0"
```

---

### 13.4: Monorepo Support

For repositories with multiple components, use `--repo-path` to specify the subdirectory:

```bash
--vcsuri "github.com/myorg/myrepo" --repo-path "frontend"
--vcsuri "github.com/myorg/myrepo" --repo-path "backend"
--vcsuri "github.com/myorg/myrepo" --repo-path "services/auth"
```

---

# Development of ReARM CLI

## Adding dependencies to ReARM CLI

Dependencies are handled using go modules and imports file is automatically generated. If importing a github repository use this command first:

```bash
go get github.com/xxxxxx/xxxxxx
```

You then should be able to add what you need as an import to your files. Once they've been imported call this command to generate the imports file:

```bash
go generate ./internal/imports
```
