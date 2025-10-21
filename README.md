![Docker Image CI](https://github.com/relizaio/rearm-cli/actions/workflows/github_actions.yml/badge.svg?branch=main)
[![Go Report Card](https://goreportcard.com/badge/github.com/relizaio/rearm-cli)](https://goreportcard.com/report/github.com/relizaio/rearm-cli)
# Rearm CLI

This tool allows for command-line interactions with [ReARM](https://github.com/relizaio/rearm) (currently in Public Beta). ReARM is a system to manage software and (in the future) hardware releases with their Metadata, including SBOMs / xBOMs and other artifacts. We mainly support [CycloneDX](https://cyclonedx.org/) standard.

Community forum and support is available via [Discord](https://discord.com/invite/UTxjBf9juQ) - use #rearm channel.

Container image URI: registry.relizahub.com/library/rearm-cli.

## Download Rearm CLI

Below are the available downloads for the latest version of the Rearm CLI (25.10.1). Please download the proper package for your operating system and architecture.

The CLI is distributed as a single binary. Install by unzipping it and moving it to a directory included in your system's PATH.

[SHA256 checksums](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/sha256sums.txt)

macOS: [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/rearm-25.10.1-darwin-amd64.zip)

FreeBSD: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/rearm-25.10.1-freebsd-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/rearm-25.10.1-freebsd-amd64.zip) | [Arm](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/rearm-25.10.1-freebsd-arm.zip)

Linux: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/rearm-25.10.1-linux-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/rearm-25.10.1-linux-amd64.zip) | [Arm](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/rearm-25.10.1-linux-arm.zip) | [Arm64](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/rearm-25.10.1-linux-arm64.zip)

OpenBSD: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/rearm-25.10.1-openbsd-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/rearm-25.10.1-openbsd-amd64.zip)

Solaris: [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/rearm-25.10.1-solaris-amd64.zip)

Windows: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/rearm-25.10.1-windows-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-download/25.10.1/rearm-25.10.1-windows-amd64.zip)

It is possible to set authentication data via explicit flags, login command (see below) or following environment variables:

- REARM_APIKEYID - for API Key ID
- REARM_APIKEY - for API Key itself
- REARM_URI - for ReARM Uri

# Table of Contents - Use Cases
1. [Get Version Assignment From ReARM](#1-use-case-get-version-assignment-from-rearm)
2. [Send Release Metadata to ReARM](#2-use-case-send-release-metadata-to-rearm)
3. [Check If Artifact Hash Already Present In Some Release](#3-use-case-check-if-artifact-hash-already-present-in-some-release)
4. [Request Latest Release Per Component Or Product](#4-use-case-request-latest-release-per-component-or-product)
5. [Persist ReARM Credentials in a Config File](#5-use-case-persist-rearm-hub-credentials-in-a-config-file)
6. [Create New Component in ReARM](#6-use-case-create-new-component-in-rearm-hub)
7. [Synchronize Live Git Branches with ReARM](#7-use-case-synchronize-live-git-branches-with-rearm)
8. [Add Outbound Deliverables to Release](#8-use-case-add-outbound-deliverables-to-release)
9. [CycloneDX xBOM Utilities](#9-use-case-cyclonedx-xbom-utilities)
    1. [Fix incorrect OCI purl generated via cdxgen](#91-fix-incorrect-oci-purl-generated-via-cdxgen)
    2. [BOM supplier enrichment with BEAR](#92-bom-supplier-enrichment-with-bear)
    3. [Merge Multiple BOMs](#93-merge-multiple-boms)
10. [Finalize Release After CI Completion](#10-use-case-finalize-release-after-ci-completion)
11. [Transparency Exchange API (TEA) Discovery](#11-use-case-transparency-exchange-api-tea-discovery)
12. [Complete TEA Flow - Product and Component Details](#12-use-case-complete-tea-flow---product-and-component-details)

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

## 11. Use Case: Transparency Exchange API (TEA) Discovery

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

## 12. Use Case: Complete TEA Flow - Product and Component Details

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
