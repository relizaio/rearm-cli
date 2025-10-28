![Docker Image CI](https://github.com/relizaio/rearm-cli/actions/workflows/github_actions.yml/badge.svg?branch=main)
[![Go Report Card](https://goreportcard.com/badge/github.com/relizaio/rearm-cli)](https://goreportcard.com/report/github.com/relizaio/rearm-cli)
# Rearm CLI

This tool allows for command-line interactions with [ReARM](https://github.com/relizaio/rearm) (currently in Public Beta). ReARM is a system to manage software and (in the future) hardware releases with their Metadata, including SBOMs / xBOMs and other artifacts. We mainly support [CycloneDX](https://cyclonedx.org/) standard.

Community forum and support is available via [Discord](https://discord.com/invite/UTxjBf9juQ) - use #rearm channel.

Container image URI: registry.relizahub.com/library/rearm-cli.

## Download Rearm CLI

Below are the available downloads for the latest version of the Rearm CLI (25.10.2). Please download the proper package for your operating system and architecture.

The CLI is distributed as a single binary. Install by unzipping it and moving it to a directory included in your system's PATH.

[SHA256 checksums](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/sha256sums.txt)

macOS: [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/rearm-25.10.2-darwin-amd64.zip)

FreeBSD: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/rearm-25.10.2-freebsd-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/rearm-25.10.2-freebsd-amd64.zip) | [Arm](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/rearm-25.10.2-freebsd-arm.zip)

Linux: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/rearm-25.10.2-linux-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/rearm-25.10.2-linux-amd64.zip) | [Arm](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/rearm-25.10.2-linux-arm.zip) | [Arm64](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/rearm-25.10.2-linux-arm64.zip)

OpenBSD: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/rearm-25.10.2-openbsd-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/rearm-25.10.2-openbsd-amd64.zip)

Solaris: [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/rearm-25.10.2-solaris-amd64.zip)

Windows: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/rearm-25.10.2-windows-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.2/rearm-25.10.2-windows-amd64.zip)

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
9. [CycloneDX xBOM Utilities](#9-use-case-cyclonedx-xbom-utilities)
    1. [Fix incorrect OCI purl generated via cdxgen](#91-fix-incorrect-oci-purl-generated-via-cdxgen)
    2. [BOM supplier enrichment with BEAR](#92-bom-supplier-enrichment-with-bear)
    3. [Convert SPDX to CycloneDX](#93-convert-spdx-to-cyclonedx)
    4. [Merge Multiple BOMs](#94-merge-multiple-boms)
10. [Finalize Release After CI Completion](#10-use-case-finalize-release-after-ci-completion)
11. [Transparency Exchange API (TEA) Commands](#11-use-case-transparency-exchange-api-tea-commands)
    1. [Transparency Exchange API (TEA) Discovery](#111-transparency-exchange-api-tea-discovery)
    2. [Complete TEA Flow - Product and Component Details](#112-complete-tea-flow---product-and-component-details)
12. [Oolong TEA Server Content Management Commands](#12-use-case-oolong-tea-server-content-management-commands)
    1. [Add Product](#121-add-product)
    2. [Add Component](#122-add-component)
    3. [Add Component Release](#123-add-component-release)
    4. [Add Product Release](#124-add-product-release)
    5. [Add Artifact](#125-add-artifact)

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
- **--stripbom** - flag to toggle stripping of bom metadata for hash comparison (optional). Default is true. Supported values: true|false.

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
    --name componentname
    --type component
    --versionschema semver
    --featurebranchversioning Branch.Micro
    --vcsuri github.com/registry.relizahub.com/library/rearm-cli
    --includeapi
```

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

## 7. Use Case: Synchronize Live Git Branches with ReARM

Sends a list of Live Git branches to ReARM. Non-live branches present on ReARM will be archived.

Sample command:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    syncbranches \
    -i api_id \ 
    -k api_key    \
    --component component_uuid    \
    --livebranches $(git branch -r --format="%(refname)" | base64 -w 0)
```

Flags stand for:
- **--component** - flag to specify component uuid, which can be obtained from the component settings on ReARM UI (either this flag or component's API key must be used).
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
- **--identifiers** - Deliverable Identifiers (i.e. PURL) IdentifierType-Value Pairs (multiple allowed, separate several IdentifierType-Value pairs for one Deliverable with commas, and seprate IdentifierType-Value in a pair with colon, e.g. --identifiers "PURL:somepurl,TEI:sometei")
- **--odelartsjson** -Deliverable Artifacts json array (optional, multiple allowed, use a json array for each deliverable, similar to add release use case)
- **--odelbuildid** - Deliverable Build ID (optional, multiple allowed)
- **--odelbuilduri** - Deliverable Build URI (multiple allowed)
- **--odelcimeta** - Deliverable CI Meta (multiple allowed)
- **--odeldigests** - Deliverable Digests (multiple allowed, separate several digests for one Deliverable with commas)
- **--odelid** - Deliverable Primary Identifier (multiple allowed)
- **--odelpackage** - Deliverable package type (i.e. Maven) (multiple allowed)
- **--odelpublisher** - Deliverable publisher (multiple allowed)
- **--odelversion** -  Deliverable version, if different from release (multiple allowed)
- **--osarr** - Deliverable supported OS array (multiple allowed, use comma seprated values for each deliverable)
- **--releaseid** - UUID of release to add deliverable to (either releaseid or component, branch, and version must be set)
- **--tagsarr** - Deliverable Tag Key-Value Pairs (multiple allowed, separate several tag key-value pairs for one Deliverable with commas, and seprate key-value in a pair with colon)
- **--version** - Release version (either releaseid or component, branch, and version must be set)
- **--branch** - Release branch (either releaseid or component, branch, and version must be set)
- **--stripbom** - flag to toggle stripping of bom metadata for hash comparison (optional - can). Default is true. Supported values: true|false.

## 9. Use Case: CycloneDX xBOM Utilities
Base Command: `bomutils`

### 9.1 Fix incorrect OCI purl generated via cdxgen
purl generated for an oci artifact via cdxgen contains incorrect purls; as per the [spec](https://github.com/package-url/purl-spec/blob/main/PURL-TYPES.rst#oci) `oci` purls must be registry agnostic by default.

use this utility command to fix such purls in an SBOM.

e.g. 

Sample command:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    bomutils fixpurl \
    --ociimage image_with_SHA_DIGEST \ 
    -f input-bom.json \
    -o output-bom.json
```

Read from stdin and write to stdout


```bash
cat bom_samples/rebom-ui-oci.json | docker run --rm -i registry.relizahub.com/library/rearm-cli    \
    bomutils fixpurl \
    --ociimage image_with_SHA_DIGEST
```

Flags stand for:
- **--ociimage** - flag to specify oci image with digest
- **--infile (-f)** - input cyclonedx sbom json file. (Optional - reades from stdin when not specified)
- **--outfile (-o)** - output file path to write bom json. (Optional - writes to stdout when not specified)
- **--newpurl** - new purl to be set, will attempt to autogenerate if missing (but will autogenerate for oci only)


### 9.2 BOM Supplier Enrichment with BEAR

BOMs generated by many tools are missing supplier data. Use this command to enrich with supplier data using [Project BEAR](https://github.com/relizaio/bear).

Sample command:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    bomutils enrichsupplier \
    -f input-bom.json \
    -o output-bom.json
```

Flags stand for:
- **--bearUri** - URI of BEAR service to use, default to https://beardemo.rearmhq.com (currently, publicly available, data currently available under Apache 2.0 license)
- **--infile (-f)** - input cyclonedx sbom json file. (Optional - reades from stdin when not specified)
- **--outfile (-o)** - output file path to write bom json. (Optional - writes to stdout when not specified)

### 9.3 Convert SPDX to CycloneDX

The `convert-spdx` command converts SPDX 2.2/2.3 JSON BOMs to CycloneDX 1.6 JSON format with high fidelity preservation of metadata, licenses, PURLs, and dependencies.

Sample command:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    bomutils convert-spdx \
    --infile input-spdx.json \
    --outfile output-cyclonedx.json \
    --validate
```

Flags stand for:
- **--infile** - Input SPDX JSON file path (required)
- **--outfile** - Output CycloneDX JSON file path (required)  
- **--validate** - Validate the generated CycloneDX BOM (optional)

### 9.4 Merge Multiple BOMs

The `merge-boms` command allows you to merge multiple CycloneDX BOMs into a single consolidated BOM. This is useful when you have multiple BOMs from different components or services that need to be combined into a unified view.

Currently only works with CycloneDX 1.6.

Sample command:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    bomutils merge-boms \
    --input-files bom1.json,bom2.json,bom3.json \
    --name "merged-application" \
    --version "1.0.0" \
    --group "com.example" \
    --structure FLAT \
    --outfile merged-bom.json
```

Sample command with hierarchical structure:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli    \
    bomutils merge-boms \
    --input-files frontend-bom.json,backend-bom.json \
    --name "full-stack-app" \
    --version "2.1.0" \
    --structure HIERARCHICAL \
    --root-component-merge-mode PRESERVE_UNDER_NEW_ROOT \
    --purl "pkg:generic/full-stack-app@2.1.0"
```

Flags stand for:
- **--input-files** - Comma-separated list of input BOM file paths to merge (required)
- **--name** - Name for the new root component of the merged BOM (optional)
- **--version** - Version for the new root component of the merged BOM (optional)
- **--group** - Group for the new root component of the merged BOM (optional)
- **--structure** - Structure of the merged BOM: FLAT or HIERARCHICAL (default: FLAT)
- **--root-component-merge-mode** - How to handle root components: PRESERVE_UNDER_NEW_ROOT or FLATTEN_UNDER_NEW_ROOT (default: PRESERVE_UNDER_NEW_ROOT)
- **--purl** - Set bom-ref and purl for the root merged component (optional)
- **--outfile** - Output file path to write merged BOM (optional - writes to stdout when not specified)

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

### 11.1 Transparency Exchange API (TEA) Discovery

The `tea discovery` command resolves a Transparency Exchange Identifier (TEI) to a product release UUID by following the TEA discovery flow as defined in the [CycloneDX TEA specification](https://github.com/CycloneDX/transparency-exchange-api).

The discovery process:
1. Parses the TEI to extract the domain name
2. Resolves DNS records for the domain (A, AAAA, CNAME)
3. Queries the `.well-known/tea` endpoint to discover available TEA servers
4. Calls the discovery API endpoint with the TEI
5. Returns the product release UUID

Sample command:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    tea discovery \
    --tei "urn:tei:uuid:products.example.com:d4d9f54a-abcf-11ee-ac79-1a52914d44b"
```

Sample command with different domain and port:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    tea discovery \
    --tei "urn:tei:purl:cyclonedx.org:pkg:pypi/cyclonedx-python-lib@8.4.0"
```

Sample command with HTTP (non-HTTPS) connection (note that this is not compliant with TEA standard, should be used for local testing only):

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    tea discovery \
    --tei "urn:tei:hash:localhost:SHA256:fd44efd601f651c8865acf0dfeacb0df19a2b50ec69ead0262096fd2f67197b9" \
    --usehttp true \
    --useport 3000
```

**TEI Format:**

TEI (Transparency Exchange Identifier) follows the format:
```
urn:tei:<type>:<domain-name>:<unique-identifier>
```

Where:
- **type** - The type of unique identifier (uuid, purl, hash, or swid)
- **domain-name** - The domain hosting the TEA endpoint (e.g., products.example.com, localhost)
- **unique-identifier** - The identifier value, format depends on type:
  - **uuid** - A UUID (e.g., d4d9f54a-abcf-11ee-ac79-1a52914d44b)
  - **purl** - Package URL in canonical form (e.g., pkg:pypi/cyclonedx-python-lib@8.4.0)
  - **hash** - Hash with algorithm prefix (e.g., SHA256:fd44efd601f651c8865acf0dfeacb0df19a2b50ec69ead0262096fd2f67197b9)
  - **swid** - Software Identification Tag

**Flags:**
- **--tei** - Transparency Exchange Identifier to resolve (required)
- **--usehttp** - Use HTTP instead of HTTPS for connections (default: false)
- **--useport** - Port to use for resolving well-known resource (default: 443)

**Output:**

On success, the command outputs the product release UUID:
```
d4d9f54a-abcf-11ee-ac79-1a52914d44b
```

On failure, an error message is displayed:
```
Error: Failed to retrieve product release UUID: <error details>
```

**Debug Mode:**

Use the `-d true` flag to enable debug output showing the discovery process:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    tea discovery \
    -d true \
    --tei "urn:tei:uuid:products.example.com:d4d9f54a-abcf-11ee-ac79-1a52914d44b"
```

## 11.2 Complete TEA Flow - Product and Component Details

The `tea full_tea_flow` command performs a complete TEA discovery and data retrieval flow. It discovers a product release from a TEI and retrieves comprehensive information about the product and all its components, including artifacts and their formats.

**The flow:**
1. Performs TEI discovery to get the product release UUID
2. Retrieves product release details (product name and version)
3. For each component in the product:
   - If the component has a pinned release, uses that release
   - If the component only has a UUID (no pinned release), fetches the latest available release and displays a notification
   - Retrieves component release details (component name and version)
   - Lists all artifacts in the latest collection with their formats, including:
     - Artifact type
     - Format description
     - MIME type
     - Download URL
     - Signature URL (if available)

Sample command:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    tea full_tea_flow \
    --tei "urn:tei:uuid:demo.rearmhq.com:62c5cdb4-b462-4cda-9b07-37b7b1c61c65"
```

Sample command with debug output:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    tea full_tea_flow \
    -d true \
    --tei "urn:tei:uuid:demo.rearmhq.com:62c5cdb4-b462-4cda-9b07-37b7b1c61c65"
```

**Flags:**
- **--tei** - Transparency Exchange Identifier to resolve (required)
- **--usehttp** - Use HTTP instead of HTTPS for connections (default: false)
- **--useport** - Port to use for resolving well-known resource (default: 443)
- **-d** - Enable debug mode to see detailed API calls and processing steps (optional)

**Sample Output:**

```
=== Product Information ===
Product Name: Example Product
Version: 1.2.3

Note: Component 'example-component' does not have a pinned release. Selecting latest available release.

--- Component: example-component ---
Version: 2.0.1

  Artifact Type: BOM
    - Description: CycloneDX SBOM in JSON format
      Media Type: application/vnd.cyclonedx+json
      URL: https://example.com/artifacts/sbom.json
      Signature URL: https://example.com/artifacts/sbom.json.sig

  Artifact Type: OTHER
    - Description: SIGNATURE Raw Artifact as Uploaded to ReARM
      Media Type: text/plain; charset=utf-8
      URL: https://registry.example.com/image:2.0.1
```

## 12. Use Case: Oolong TEA Server Content Management Commands
Base Command: `oolong`

The `oolong` command provides tools for managing content in an Oolong TEA Server, including products, components, releases, and artifacts. These commands create and update YAML files in a structured content directory that can be used by the Oolong TEA Server.

**Global Flag:**
- **--contentdir** - Path to the content directory (required for all subcommands)

### 12.1 Add Product

The `oolong add_product` command creates a new product in the content directory. It generates a product directory with a `product.yaml` file containing the product metadata.

**Process:**
1. Converts the product name to lowercase snake_case for the directory name
2. Creates the directory structure: `<contentdir>/products/<snake_case_name>/`
3. Generates a UUID if not provided
4. Creates `product.yaml` with product metadata
5. Checks for existing products to prevent duplicates

Sample command:

```bash
rearm oolong add_product \
    --contentdir ./content \
    --name "My Product"
```

Sample command with custom UUID:

```bash
rearm oolong add_product \
    --contentdir ./content \
    --name "My Product" \
    --uuid "8ab3c557-7f36-4ebd-a593-026c28337630"
```

**Flags:**
- **--contentdir** - Content directory path (required, global flag)
- **--name** - Product name (required)
- **--uuid** - Product UUID (optional, auto-generated if not provided)

**Output:**

```
Successfully created/updated product: My Product
  Directory: ./content/products/my_product
  UUID: 8ab3c557-7f36-4ebd-a593-026c28337630
```

**Generated File Structure:**

```
content/
└── products/
    └── my_product/
        └── product.yaml
```

**product.yaml Format:**

```yaml
uuid: 8ab3c557-7f36-4ebd-a593-026c28337630
name: My Product
identifiers: []
```

### 12.2 Add Component

The `oolong add_component` command creates a new component in the content directory. It generates a component directory with a `component.yaml` file containing the component metadata.

**Process:**
1. Converts the component name to lowercase snake_case for the directory name
2. Creates the directory structure: `<contentdir>/components/<snake_case_name>/`
3. Generates a UUID if not provided
4. Creates `component.yaml` with component metadata
5. Checks for existing components to prevent duplicates

Sample command:

```bash
rearm oolong add_component \
    --contentdir ./content \
    --name "Database Component"
```

Sample command with custom UUID:

```bash
rearm oolong add_component \
    --contentdir ./content \
    --name "Database Component" \
    --uuid "adc0909a-3039-47eb-82ba-7686767c0d52"
```

**Flags:**
- **--contentdir** - Content directory path (required, global flag)
- **--name** - Component name (required)
- **--uuid** - Component UUID (optional, auto-generated if not provided)

**Output:**

```
Successfully created/updated component: Database Component
  Directory: ./content/components/database_component
  UUID: adc0909a-3039-47eb-82ba-7686767c0d52
```

**Generated File Structure:**

```
content/
└── components/
    └── database_component/
        └── component.yaml
```

**component.yaml Format:**

```yaml
uuid: adc0909a-3039-47eb-82ba-7686767c0d52
name: Database Component
identifiers: []
```

### 12.3 Add Component Release

The `oolong add_component_release` command creates a new release for an existing component. It generates a release directory with `release.yaml` and an initial collection.

**Process:**
1. Resolves the component by name or UUID
2. Creates the release directory: `<component_dir>/releases/<version>/`
3. Generates a UUID if not provided
4. Sets timestamps to current UTC time if not provided
5. Creates `release.yaml` with release metadata and identifiers
6. Creates an initial collection at `collections/1.yaml`
7. Checks for existing release versions to prevent duplicates

Sample command with component name:

```bash
rearm oolong add_component_release \
    --contentdir ./content \
    --component "Database Component" \
    --version "1.0.0"
```

Sample command with component UUID:

```bash
rearm oolong add_component_release \
    --contentdir ./content \
    --component "adc0909a-3039-47eb-82ba-7686767c0d52" \
    --version "1.0.0"
```

Sample command with identifiers:

```bash
rearm oolong add_component_release \
    --contentdir ./content \
    --component "Database Component" \
    --version "1.0.0" \
    --tei "urn:tei:uuid:demo.rearmhq.com:7a7fa4da-bf9b-478f-b934-2fe9e0fc317c" \
    --purl "pkg:generic/database-component@1.0.0"
```

Sample command with all options:

```bash
rearm oolong add_component_release \
    --contentdir ./content \
    --component "Database Component" \
    --version "1.0.0-beta" \
    --uuid "7a7fa4da-bf9b-478f-b934-2fe9e0fc317c" \
    --createddate "2025-10-16T19:07:55Z" \
    --releasedate "2025-10-16T19:07:55Z" \
    --prerelease \
    --tei "urn:tei:uuid:demo.rearmhq.com:7a7fa4da-bf9b-478f-b934-2fe9e0fc317c" \
    --tei "urn:tei:purl:demo.rearmhq.com:pkg:generic/database-component@1.0.0-beta" \
    --purl "pkg:generic/database-component@1.0.0-beta"
```

**Flags:**
- **--contentdir** - Content directory path (required, global flag)
- **--component** - Component name or UUID (required)
- **--version** - Release version string (required)
- **--uuid** - Release UUID (optional, auto-generated if not provided)
- **--createddate** - Created date in RFC3339 format (optional, defaults to current UTC time)
- **--releasedate** - Release date in RFC3339 format (optional, defaults to current UTC time)
- **--prerelease** - Mark as pre-release (optional, defaults to false)
- **--tei** - TEI identifier (optional, can be specified multiple times)
- **--purl** - PURL identifier (optional, can be specified multiple times)

**Output:**

```
Successfully created component release: 1.0.0
  Component: Database Component
  Directory: ./content/components/database_component/releases/1.0.0
  UUID: 7a7fa4da-bf9b-478f-b934-2fe9e0fc317c
  Created initial collection: collections/1.yaml
```

**Generated File Structure:**

```
content/
└── components/
    └── database_component/
        ├── component.yaml
        └── releases/
            └── 1.0.0/
                ├── release.yaml
                └── collections/
                    └── 1.yaml
```

**release.yaml Format:**

```yaml
uuid: 7a7fa4da-bf9b-478f-b934-2fe9e0fc317c
version: 1.0.0
createdDate: "2025-10-16T19:07:55Z"
releaseDate: "2025-10-16T19:07:55Z"
preRelease: false
identifiers:
- idType: TEI
  idValue: urn:tei:uuid:demo.rearmhq.com:7a7fa4da-bf9b-478f-b934-2fe9e0fc317c
- idType: PURL
  idValue: pkg:generic/database-component@1.0.0
distributions: []
```

**collections/1.yaml Format:**

```yaml
version: 1
date: "2025-10-16T19:07:56Z"
updateReason:
  type: INITIAL_RELEASE
  comment: ""
artifacts: []
```

**Component Resolution:**

The `--component` flag accepts either:
- **Component Name** - Exact match of the component name (e.g., "Database Component")
- **Component UUID** - Full UUID of the component (e.g., "adc0909a-3039-47eb-82ba-7686767c0d52")

The command will search through all components in the content directory and match by either name or UUID.

**Date Format:**

Dates must be in RFC3339 format with UTC timezone:
```
2025-10-16T19:07:55Z
```

If not provided, the current UTC timestamp will be used automatically.

### 12.4 Add Product Release

The `oolong add_product_release` command creates a new release for an existing product. It generates a release directory with `release.yaml` and an initial collection, and optionally links component releases to the product release.

**Process:**
1. Resolves the product by name or UUID
2. Validates that component and component_release flags are properly paired
3. Creates the release directory: `<product_dir>/releases/<version>/`
4. Generates a UUID if not provided
5. Sets timestamps to current UTC time if not provided
6. For each component pair:
   - Resolves the component by name or UUID
   - Resolves the component release by version or UUID
   - Creates a component reference with both UUIDs
7. Creates `release.yaml` with release metadata, identifiers, and component references
8. Creates an initial collection at `collections/1.yaml`
9. Checks for existing release versions to prevent duplicates

Sample command with product name:

```bash
rearm oolong add_product_release \
    --contentdir ./content \
    --product "My Product" \
    --version "1.0.0"
```

Sample command with product UUID:

```bash
rearm oolong add_product_release \
    --contentdir ./content \
    --product "8ab3c557-7f36-4ebd-a593-026c28337630" \
    --version "1.0.0"
```

Sample command with linked components:

```bash
rearm oolong add_product_release \
    --contentdir ./content \
    --product "My Product" \
    --version "1.0.0" \
    --component "Database Component" \
    --component_release "1.0.0" \
    --component "API Component" \
    --component_release "2.1.0"
```

Sample command with identifiers and all options:

```bash
rearm oolong add_product_release \
    --contentdir ./content \
    --product "My Product" \
    --version "1.0.0-beta" \
    --uuid "9485fbc9-aa7d-4c26-95c9-d5a8ccb1c073" \
    --createddate "2025-10-16T19:26:57Z" \
    --releasedate "2025-10-16T19:26:57Z" \
    --prerelease \
    --tei "urn:tei:uuid:demo.rearmhq.com:9485fbc9-aa7d-4c26-95c9-d5a8ccb1c073" \
    --purl "pkg:generic/my-product@1.0.0-beta" \
    --component "adc0909a-3039-47eb-82ba-7686767c0d52" \
    --component_release "7a7fa4da-bf9b-478f-b934-2fe9e0fc317c"
```

**Flags:**
- **--contentdir** - Content directory path (required, global flag)
- **--product** - Product name or UUID (required)
- **--version** - Release version string (required)
- **--uuid** - Release UUID (optional, auto-generated if not provided)
- **--createddate** - Created date in RFC3339 format (optional, defaults to current UTC time)
- **--releasedate** - Release date in RFC3339 format (optional, defaults to current UTC time)
- **--prerelease** - Mark as pre-release (optional, defaults to false)
- **--tei** - TEI identifier (optional, can be specified multiple times)
- **--purl** - PURL identifier (optional, can be specified multiple times)
- **--component** - Component name or UUID to link (optional, can be specified multiple times, must be paired with `--component_release`)
- **--component_release** - Component release version or UUID to link (optional, can be specified multiple times, must be paired with `--component`)

**Output:**

```
Successfully created product release: 1.0.0
  Product: My Product
  Directory: ./content/products/my_product/releases/1.0.0
  UUID: 9485fbc9-aa7d-4c26-95c9-d5a8ccb1c073
  Linked components: 2
  Created initial collection: collections/1.yaml
```

**Generated File Structure:**

```
content/
└── products/
    └── my_product/
        ├── product.yaml
        └── releases/
            └── 1.0.0/
                ├── release.yaml
                └── collections/
                    └── 1.yaml
```

**release.yaml Format:**

```yaml
uuid: 9485fbc9-aa7d-4c26-95c9-d5a8ccb1c073
version: 1.0.0
createdDate: "2025-10-16T19:26:57Z"
releaseDate: "2025-10-16T19:26:57Z"
preRelease: false
identifiers:
- idType: TEI
  idValue: urn:tei:uuid:demo.rearmhq.com:9485fbc9-aa7d-4c26-95c9-d5a8ccb1c073
- idType: PURL
  idValue: pkg:generic/my-product@1.0.0
components:
- uuid: adc0909a-3039-47eb-82ba-7686767c0d52
  release: 7a7fa4da-bf9b-478f-b934-2fe9e0fc317c
- uuid: 35218a8a-7e08-4502-8165-fa2b7a4a0b8a
  release: fd327965-a46b-42a1-85ad-a31468100e2b
```

**collections/1.yaml Format:**

```yaml
version: 1
date: "2025-10-16T19:07:56Z"
updateReason:
  type: INITIAL_RELEASE
  comment: ""
artifacts: []
```

**Component Linking:**

The `--component` and `--component_release` flags work in pairs:
- Each `--component` flag must have a corresponding `--component_release` flag
- The order matters: the first `--component` is paired with the first `--component_release`, and so on
- Both flags accept either names or UUIDs
- The command will:
  1. Find the component by name or UUID
  2. Find the component release within that component by version or UUID
  3. Store both the component UUID and component release UUID in the product release

**Product Resolution:**

The `--product` flag accepts either:
- **Product Name** - Exact match of the product name (e.g., "My Product")
- **Product UUID** - Full UUID of the product (e.g., "8ab3c557-7f36-4ebd-a593-026c28337630")

The command will search through all products in the content directory and match by either name or UUID.

**Date Format:**

Dates must be in RFC3339 format with UTC timezone:
```
2025-10-16T19:26:57Z
```

If not provided, the current UTC timestamp will be used automatically.

### 12.5 Add Artifact

The `oolong add_artifact` command creates a new artifact in the content directory. Artifacts represent external resources like SBOMs, attestations, licenses, and other release-related files.

**Process:**
1. Validates the artifact type against allowed TEA artifact types
2. Generates a UUID if not provided
3. Creates the artifacts directory if it doesn't exist
4. Creates a YAML file named `<uuid>.yaml` in the artifacts directory
5. Parses hash values and normalizes algorithm names
6. Creates a single format entry with the provided metadata
7. Checks for existing artifact UUIDs to prevent duplicates

Sample command:

```bash
rearm oolong add_artifact \
    --contentdir ./content \
    --name "Product SBOM" \
    --type BOM \
    --mediatype "application/vnd.cyclonedx+json" \
    --url "https://example.com/artifacts/sbom.json"
```

Sample command with signature and checksums:

```bash
rearm oolong add_artifact \
    --contentdir ./content \
    --name "Product SBOM" \
    --type BOM \
    --mediatype "application/vnd.cyclonedx+json" \
    --url "https://example.com/artifacts/sbom.json" \
    --signatureurl "https://example.com/artifacts/sbom.json.sig" \
    --description "CycloneDX SBOM in JSON format" \
    --hash "sha256=05ca5f89a206f5863ae3327d52daed8b760a91c3ce465708447bd3499c4492fe"
```

Sample command with multiple hashes:

```bash
rearm oolong add_artifact \
    --contentdir ./content \
    --name "Release Attestation" \
    --type ATTESTATION \
    --mediatype "application/vnd.in-toto+json" \
    --url "https://example.com/attestation.json" \
    --hash "sha256=abc123def456" \
    --hash "sha512=fedcba654321"
```

Sample command with custom UUID:

```bash
rearm oolong add_artifact \
    --contentdir ./content \
    --name "License File" \
    --type LICENSE \
    --mediatype "text/plain" \
    --url "https://example.com/LICENSE" \
    --uuid "173cedd7-fabb-4d3a-9315-7d7465d236b6"
```

**Flags:**
- **--contentdir** - Content directory path (required, global flag)
- **--name** - Artifact name (required)
- **--type** - Artifact type (required), must be one of:
  - `ATTESTATION` - attestation (i.e., build or release attestation)
  - `BOM` - Bill of Materials (SBOM, HBOM, OBOM, AIBOM, etc or a mix)
  - `BUILD_META` - Build metadata
  - `CERTIFICATION` - Certifications and compliance documents
  - `FORMULATION` - Build formulation or recipe
  - `LICENSE` - License files
  - `RELEASE_NOTES` - Release notes and changelogs
  - `SECURITY_TXT` - Security contact information
  - `THREAT_MODEL` - Threat model documentation
  - `VULNERABILITIES` - Vulnerability reports (VEX, etc.)
  - `OTHER` - Other artifact types
- **--mediatype** - media type (required)
- **--url** - Artifact URL (required)
- **--uuid** - Artifact UUID (optional, auto-generated if not provided)
- **--signatureurl** - Signature URL (optional)
- **--description** - Artifact description (optional, defaults to empty)
- **--hash** - Hash in format `algorithm=value` (optional, can be specified multiple times)

**Output:**

```
Successfully created artifact: Product SBOM
  Type: BOM
  File: ./content/artifacts/173cedd7-fabb-4d3a-9315-7d7465d236b6.yaml
  UUID: 173cedd7-fabb-4d3a-9315-7d7465d236b6
```

**Generated File Structure:**

```
content/
└── artifacts/
    └── 173cedd7-fabb-4d3a-9315-7d7465d236b6.yaml
```

**Artifact YAML Format:**

```yaml
name: Product SBOM
type: BOM
version: 1
distributionTypes: []
formats:
- mimeType: application/vnd.cyclonedx+json
  description: CycloneDX SBOM in JSON format
  url: https://example.com/artifacts/sbom.json
  signatureUrl: https://example.com/artifacts/sbom.json.sig
  checksums:
  - algType: SHA-256
    algValue: 05ca5f89a206f5863ae3327d52daed8b760a91c3ce465708447bd3499c4492fe
```

**Note:** If `--description` is not provided, the `description` field will be empty in the YAML output.

**Hash Format:**

Hashes must be specified in the format `algorithm=value`:
- `sha256=abc123def456`
- `sha512=fedcba654321`
- `blake3=123abc456def`

**Supported Hash Algorithms:**
- `MD5`
- `SHA-1` (or `sha1`)
- `SHA-256` (or `sha256`)
- `SHA-384` (or `sha384`)
- `SHA-512` (or `sha512`)
- `SHA3-256` (or `sha3256`)
- `SHA3-384` (or `sha3384`)
- `SHA3-512` (or `sha3512`)
- `BLAKE2b-256` (or `blake2b256`)
- `BLAKE2b-384` (or `blake2b384`)
- `BLAKE2b-512` (or `blake2b512`)
- `BLAKE3`

Algorithm names are case-insensitive and will be automatically normalized to the standard format.

Multiple hashes can be provided by using the `--hash` flag multiple times.

**Artifact Types:**

The artifact type must match one of the TEA (Transparency Exchange API) standard artifact types. These types help categorize and identify the purpose of each artifact in the transparency exchange ecosystem.

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
