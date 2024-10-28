![Docker Image CI](https://github.com/relizaio/rearm-cli/actions/workflows/dockerimage.yml/badge.svg?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/relizaio/rearm-cli)](https://goreportcard.com/report/github.com/relizaio/rearm-cli)
# Rearm CLI

This tool allows for command-line interactions with [ReARM at relizahub.com](https://relizahub.com) (currently in public preview mode). Particularly, Rearm CLI can stream metadata about instances, releases, artifacts, resolve bundles based on ReARM data. Available as either a Docker image or binary.

Video tutorial about key functionality of ReARM is available on [YouTube](https://www.youtube.com/watch?v=yDlf5fMBGuI).

Argo CD GitOps Integration using Kustomize [tutorial](https://itnext.io/building-kubernetes-cicd-pipeline-with-github-actions-argocd-and-rearm-hub-e7120b9be870).

Community forum and support is available at [r/Reliza](https://reddit.com/r/Reliza).

Docker image is available at [relizaio/rearm-cli](https://hub.docker.com/r/relizaio/rearm-cli)

## Download Rearm CLI

Below are the available downloads for the latest version of the Rearm CLI (2024.07.0). Please download the proper package for your operating system and architecture.

The CLI is distributed as a single binary. Install by unzipping it and moving it to a directory included in your system's PATH.

[SHA256 checksums](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/sha256sums.txt)

macOS: [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/rearm-cli-2024.07.0-darwin-amd64.zip)

FreeBSD: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/rearm-cli-2024.07.0-freebsd-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/rearm-cli-2024.07.0-freebsd-amd64.zip) | [Arm](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/rearm-cli-2024.07.0-freebsd-arm.zip)

Linux: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/rearm-cli-2024.07.0-linux-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/rearm-cli-2024.07.0-linux-amd64.zip) | [Arm](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/rearm-cli-2024.07.0-linux-arm.zip) | [Arm64](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/rearm-cli-2024.07.0-linux-arm64.zip)

OpenBSD: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/rearm-cli-2024.07.0-openbsd-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/rearm-cli-2024.07.0-openbsd-amd64.zip)

Solaris: [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/rearm-cli-2024.07.0-solaris-amd64.zip)

Windows: [32-bit](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/rearm-cli-2024.07.0-windows-386.zip) | [64-bit](https://d7ge14utcyki8.cloudfront.net/rearm-cli-download/2024.07.0/rearm-cli-2024.07.0-windows-amd64.zip)

It is possible to set authentication data via explicit flags, login command (see below) or following environment variables:

- APIKEYID - for API Key ID
- APIKEY - for API Key itself
- URI - for ReARM Uri (if not set, default at https://app.relizahub.com is used)

# Table of Contents - Use Cases
1. [Get Version Assignment From ReARM](#1-use-case-get-version-assignment-from-rearm-hub)
2. [Send Release Metadata to ReARM](#2-use-case-send-release-metadata-to-rearm-hub)
3. [Check If Artifact Hash Already Present In Some Release](#3-use-case-check-if-artifact-hash-already-present-in-some-release)
4. [Request Latest Release Per Component Or Bundle](#6-use-case-request-latest-release-per-component-or-bundle)
6. [Persist ReARM Credentials in a Config File](#10-use-case-persist-rearm-hub-credentials-in-a-config-file)
8. [Create New Component in ReARM](#12-use-case-create-new-component-in-rearm-hub)
9. [Add new artifacts to release in ReARM](#14-use-case-add-new-artifacts-to-release-in-rearm-hub)
12. [Send Pull Request Data to ReARM](#19-use-case-send-pull-request-data-to-rearm-hub)
13. [Attach a downloadable artifact to a Release on ReARM](#20-use-case-attach-a-downloadable-artifact-to-a-release-on-rearm-hub)
## 1. Use Case: Get Version Assignment From ReARM

This use case requests Version from ReARM for our component. Note that component schema must be preset on ReARM prior to using this API. API key must also be generated for the component from ReARM.

Sample command for semver version schema:

```bash
docker run --rm relizaio/rearm-cli    \
    getversion    \
    -i component_or_organization_wide_rw_api_id    \
    -k component_or_organization_wide_rw_api_key    \
    -b master    \
    --pin 1.2.patch
```

Sample command with commit details for a git commit:

```bash
docker run --rm relizaio/rearm-cli    \
    getversion    \
    -i component_or_organization_wide_rw_api_id    \
    -k component_or_organization_wide_rw_api_key    \
    -b master    \
    --vcstype git \
    --commit $CI_COMMIT_SHA \
    --commitmessage $CI_COMMIT_MESSAGE \
    --vcsuri $CI_PROJECT_URL \
    --date $(git log -1 --date=iso-strict --pretty='%ad')
```

Sample command to obtain only version info and skip creating the release:

```bash
docker run --rm relizaio/rearm-cli    \
    getversion    \
    -i component_or_organization_wide_rw_api_id    \
    -k component_or_organization_wide_rw_api_key    \
    -b master    \
    --onlyversion
```

Flags stand for:

- **getversion** - command that denotes we are obtaining the next available release version for the branch. Note that if the call succeeds, the version assignment will be recorded and will not be given again by ReARM, even if it is not consumed. It will create the release in pending status.
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
- **--manual** - flag to indicate a manual release (optional). Sets status as "draft", otherwise "pending" status is used.
- **--onlyversion** - boolean flag to skip creation of the release (optional). Default is false.

## 2. Use Case: Send Release Metadata to ReARM

This use case is commonly used in the CI workflow to stream Release metadata to ReARM. As in previous case, API key must be generated for the component on ReARM prior to sending release details.

Sample command to send release details:

```bash
docker run --rm relizaio/rearm-cli    \
    addrelease    \
    -i component_or_organization_wide_rw_api_id    \
    -k component_or_organization_wide_rw_api_key    \
    -b master    \
    -v 20.02.3    \
    --vcsuri github.com/relizaio/rearm-cli    \
    --vcstype git    \
    --commit 7bfc5ce7b0da277d139f7993f90761223fa54442    \
    --vcstag 20.02.3    \
    --artid relizaio/rearm-cli    \
    --artbuildid 1    \
    --artcimeta Github Actions    \
    --arttype Docker    \
    --artdigests sha256:4e8b31b19ef16731a6f82410f9fb929da692aa97b71faeb1596c55fbf663dcdd    \
    --tagkey key1
    --tagval val1
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
- **date** - flag to denote date time with timezone when commit was made, iso strict formatting with timezone is required, i.e. for git use git log --date=iso-strict (optional).
- **vcstag** - flag to denote vcs tag (optional). This is needed to include vcs tag into commit, if present.
- **status** - flag to denote release status (optional). Supply "rejected" for failed releases, otherwise "complete" is used.
- **artid** - flag to denote artifact identifier (optional). This is required to add artifact metadata into release.
- **artbuildid** - flag to denote artifact build id (optional). This flag is optional and may be used to indicate build system id of the release (i.e., this could be circleci build number).
- **artbuilduri** - flag to denote artifact build uri (optional). This flag is optional and is used to denote the uri for where the build takes place.
- **artcimeta** - flag to denote artifact CI metadata (optional). This flag is optional and like artbuildid may be used to indicate build system metadata in free form.
- **arttype** - flag to denote artifact type (optional). This flag is used to denote artifact type. Types are based on [CycloneDX](https://cyclonedx.org/) spec. Supported values: Docker, File, Image, Font, Library, Application, Framework, OS, Device, Firmware.
- **datestart** - flag to denote artifact build start date and time, must conform to ISO strict date (in bash, use *date -Iseconds*, if used there must be one datestart flag entry per artifact, optional).
- **dateend** - flag to denote artifact build end date and time, must conform to ISO strict date (in bash, use *date -Iseconds*, if used there must be one datestart flag entry per artifact, optional).
- **artpublisher** - flag to denote artifact publisher (if used there must be one publisher flag entry per artifact, optional).
- **artversion** - flag to denote artifact version if different from release version (if used there must be one publisher flag entry per artifact, optional).
- **artpackage** - flag to denote artifact package type according to CycloneDX spec: MAVEN, NPM, NUGET, GEM, PYPI, DOCKER (if used there must be one publisher flag entry per artifact, optional).
- **artgroup** - flag to denote artifact group (if used there must be one group flag entry per artifact, optional).
- **artdigests** - flag to denote artifact digests (optional). This flag is used to indicate artifact digests. By convention, digests must be prefixed with type followed by colon and then actual digest hash, i.e. *sha256:4e8b31b19ef16731a6f82410f9fb929da692aa97b71faeb1596c55fbf663dcdd* - here type is *sha256* and digest is *4e8b31b19ef16731a6f82410f9fb929da692aa97b71faeb1596c55fbf663dcdd*. Multiple digests are supported and must be comma separated. I.e.:

```bash
--artdigests sha256:4e8b31b19ef16731a6f82410f9fb929da692aa97b71faeb1596c55fbf663dcdd,sha1:fe4165996a41501715ea0662b6a906b55e34a2a1
```

- **tagkey** - flag to denote keys of artifact tags (optional, but every tag key must have corresponding tag value). Multiple tag keys per artifact are supported and must be comma separated. I.e.:

```bash
--tagkey key1,key2
```

- **tagval** - flag to denote values of artifact tags (optional, but every tag value must have corresponding tag key). Multiple tag values per artifact are supported and must be comma separated. I.e.:

```bash
--tagval val1,val2
```

Note that multiple artifacts per release are supported. In which case artifact specific flags (artid, arbuildid, artbuilduri, artcimeta, arttype, artdigests, tagkey and tagval must be repeated for each artifact).

For sample of how to use workflow in CI, refer to the GitHub Actions build yaml of this component [here](https://github.com/relizaio/rearm-cli/blob/master/.github/workflows/dockerimage.yml).

## 3. Use Case: Check If Artifact Hash Already Present In Some Release

This is particularly useful for monorepos to see if there was a change in sub-component or not. We are using this technique in our sample [playground component](https://github.com/relizaio/rearm-hub-playground). We supply an artifact hash to ReARM - and if it's present already, we get release details; if not - we get an empty json response {}. Search space is scoped to a single component which is defined by API Id and API Key.

Sample command:

```bash
docker run --rm relizaio/rearm-cli    \
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

## 6. Use Case: Request Latest Release Per Component Or Bundle

This use case is when ReARM is queried either by CI or CD environment or by integration instance to check latest release version available per specific Component or Bundle. Only releases with *COMPLETE* status may be returned.

Sample command:

```bash
docker run --rm relizaio/rearm-cli    \
    getlatestrelease    \
    -i api_id    \
    -k api_key    \
    --component b4534a29-3309-4074-8a3a-34c92e1a168b    \
    --branch master    \
    --env TEST
```

Flags stand for:

- **getlatestrelease** - command that denotes we are requesting latest release data for Component or Bundle from ReARM
- **-i** - flag for api id which can be either api id for this component or organization-wide read API (required).
- **-k** - flag for api key which can be either api key for this component or organization-wide read API (required).
- **--component** - flag to denote UUID of specific Component or Bundle, UUID must be obtained from [ReARM](https://relizahub.com) (optional if component api key is used, otherwise required).
- **--bundle** - flag to denote UUID of Bundle which packages Component or Bundle for which we inquiry about its version via --component flag, UUID must be obtained from [ReARM](https://relizahub.com) (optional).
- **--branch** - flag to denote required branch of chosen Component or Bundle (optional, if not supplied settings from ReARM UI are used).
- **--env** - flag to denote environment to which release approvals should match. Environment can be one of: DEV, BUILD, TEST, SIT, UAT, PAT, STAGING, PRODUCTION. If not supplied, latest release will be returned regardless of approvals (optional).
- **--tagkey** - flag to denote tag key to use as a selector for artifact (optional, if provided tagval flag must also be supplied). Note that currently only single tag is supported.
- **--tagval** - flag to denote tag value to use as a selector for artifact (optional, if provided tagkey flag must also be supplied).
- **--instance** - flag to denote specific instance for which release should match (optional, if supplied namespace flag is also used and env flag gets overrided by instance's environment).
- **--namespace** - flag to denote specific namespace within instance, if instance is supplied (optional).
- **--status** - Status of the last known release to return, default is complete (optional, can be - [complete, pending or rejected]). If set to "pending", will return either pending or complete release. If set to "rejected", will return either pending or complete or rejected releases.

Here is a full example how we can use the getlatestrelease command leveraging jq to obtain the latest docker image with sha256 that we need to use for integration (don't forget to change api_id, api_key, component, branch and env to proper values as needed):

```bash
rlzclientout=$(docker run --rm relizaio/rearm-cli    \
    getlatestrelease    \
    -i api_id    \
    -k api_key    \
    --component b4534a29-3309-4074-8a3a-34c92e1a168b    \
    --branch master    \
    --env TEST);    \
    echo $(echo $rlzclientout | jq -r .artifactDetails[0].identifier)@$(echo $rlzclientout | jq -r .artifactDetails[0].digests[] | grep sha256)
```

## 10. Use Case: Persist ReARM Credentials in a Config File

This use case is for the case when we want to persist ReARM API Credentials and URL in a config file.

The `login` command saves `API ID`, `API KEY` and `URI` as specified by flags in a config file `.rearm.env` in the home directory for the executing user.

Sample Command:

```bash
docker run --rm \
    -v ~:/home/apprunner \
    relizaio/rearm-cli \
    login \
    -i api_id \
    -k api_key \
    -u reliza_hub_uri
```

Flags stand for:

- **-i** - flag for api id.
- **-k** - flag for api key.
- **-u** - flag for rearm hub uri.

## 12. Use Case: Create New Component in ReARM

This use case creates a new component for our organization. API key must be generated prior to using.

Sample command to create component:

```bash
docker run --rm relizaio/rearm-cli    \
    createcomponent    \
    -i org_api_id    \
    -k org_api_key    \
    --name componentname
    --type component
    --versionschema semver
    --featurebranchversioning Branch.Micro
    --vcsuri github.com/relizaio/rearm-cli
    --includeapi
```

Flags stand for:

- **createcomponent** - command that denotes we are creating a new component for our organization. Note that a vcs repository must either already exist or be created during this call.
- **-i** - flag for org api id (required).
- **-k** - flag for org api key (required).
- **name** - flag to denote component name (required).
- **type** - flag to denote component type (required). Supported values: component, bundle.
- **defaultbranch** - flag to denote default branch name (optional, if not set "main" will be used). Available names are either main or master.
- **versionschema** - flag to denote version schema (optional, if not set "semver" will be used). [Available version schemas](https://github.com/relizaio/versioning).
- **featurebranchversioning** - flag to denote feature branch version schema (optional, if not set "Branch.Micro will be used).
- **vcsuuid** - flag to denote uuid of vcs repository for the component (for existing repositories, either this flag or vcsuri are required).
- **vcsuri** - flag to denote uri of vcs repository for the component, if existing repository with uri does not exist and vcsname and vcstype are not set, ReARM will attempt to autoparse github, gitlab, and bitbucket uri's.
- **vcsname** - flag to denote name of vcs repository to create for component (required if ReARM cannot parse uri).
- **vcstype** - flag to denote type of vcs to create for component. Supported values: git, svn, mercurial (required if ReARM cannot parse uri).
- **includeapi** - boolean flag to return component api key and id of newly created component (optional). Default is false.

## 14. Use Case: Add new artifacts to release in ReARM

This use case adds 1 or more artifacts to an existing release. API key must be generated prior to using.

Sample command to add artifact:

```bash
docker run --rm relizaio/rearm-cli    \
    addartifact    \
    -i component_or_organization_wide_rw_api_id    \
    -k component_or_organization_wide_rw_api_key    \
    -v 20.02.3    \
    --artid relizaio/rearm-cli    \
    --artbuildid 1    \
    --artcimeta Github Actions    \
    --arttype Docker    \
    --artdigests sha256:4e8b31b19ef16731a6f82410f9fb929da692aa97b71faeb1596c55fbf663dcdd    \
    --tagkey key1
    --tagval val1
```

Flags stand for:

- **addartifact** - command that denotes we are adding artifact(s) to a release.
- **-i** - flag for component api id or organization-wide read-write api id (required).
- **-k** - flag for component api key or organization-wide read-write api key (required).
- **releaseid** - flag to specify release uuid, which can be obtained from the release view or programmatically (either this flag or component and version are required).
- **component** - flag to denote component uuid (optional). Required if organization-wide read-write key is used and releaseid isn't, ignored if component specific api key is used.
- **version** - version (either this flag and component or releaseid are required)
- **artid** - flag to denote artifact identifier (optional). This is required to add artifact metadata into release.
- **artbuildid** - flag to denote artifact build id (optional). This flag is optional and may be used to indicate build system id of the release (i.e., this could be circleci build number).
- **artbuilduri** - flag to denote artifact build uri (optional). This flag is optional and is used to denote the uri for where the build takes place.
- **artcimeta** - flag to denote artifact CI metadata (optional). This flag is optional and like artbuildid may be used to indicate build system metadata in free form.
- **arttype** - flag to denote artifact type (optional). This flag is used to denote artifact type. Types are based on [CycloneDX](https://cyclonedx.org/) spec. Supported values: Docker, File, Image, Font, Library, Application, Framework, OS, Device, Firmware.
- **datestart** - flag to denote artifact build start date and time, must conform to ISO strict date (in bash, use *date -Iseconds*, if used there must be one datestart flag entry per artifact, optional).
- **dateend** - flag to denote artifact build end date and time, must conform to ISO strict date (in bash, use *date -Iseconds*, if used there must be one datestart flag entry per artifact, optional).
- **artpublisher** - flag to denote artifact publisher (if used there must be one publisher flag entry per artifact, optional).
- **artversion** - flag to denote artifact version if different from release version (if used there must be one publisher flag entry per artifact, optional).
- **artpackage** - flag to denote artifact package type according to CycloneDX spec: MAVEN, NPM, NUGET, GEM, PYPI, DOCKER (if used there must be one publisher flag entry per artifact, optional).
- **artgroup** - flag to denote artifact group (if used there must be one group flag entry per artifact, optional).
- **artdigests** - flag to denote artifact digests (optional). This flag is used to indicate artifact digests. By convention, digests must be prefixed with type followed by colon and then actual digest hash, i.e. *sha256:4e8b31b19ef16731a6f82410f9fb929da692aa97b71faeb1596c55fbf663dcdd* - here type is *sha256* and digest is *4e8b31b19ef16731a6f82410f9fb929da692aa97b71faeb1596c55fbf663dcdd*. Multiple digests are supported and must be comma separated. I.e.:

```bash
--artdigests sha256:4e8b31b19ef16731a6f82410f9fb929da692aa97b71faeb1596c55fbf663dcdd,sha1:fe4165996a41501715ea0662b6a906b55e34a2a1
```

- **tagkey** - flag to denote keys of artifact tags (optional, but every tag key must have corresponding tag value). Multiple tag keys per artifact are supported and must be comma separated. I.e.:

```bash
--tagkey key1,key2
```

- **tagval** - flag to denote values of artifact tags (optional, but every tag value must have corresponding tag key). Multiple tag values per artifact are supported and must be comma separated. I.e.:

```bash
--tagval val1,val2
```

Notes:
1. Multiple artifacts per release are supported. In which case artifact specific flags (artid, arbuildid, artbuilduri, artcimeta, arttype, artdigests, tagkey and tagval must be repeated for each artifact).
2. Artifacts may be added to Complete or Rejected releases (this can be used for adding for example test reports), however a special tag would be placed on those artifacts by ReARM.

## 18. Use Case: Override and get merged helm chart values

This use case lets you do a helm style override of the default helm chart values and outputs merged helm values.

Sample command:
```bash
docker run --rm relizaio/rearm-cli    \
    helmvalues <Absolute or Relative Path to the Chart>   \
    -f <values-override-1.yaml>    \
    -f <values-override-2.yaml>    \
    -o <output-values.yaml>
```

Flags stand for:

- **--outfile | -o** - Output file with merge values (optional, if not supplied - outputs to stdout).
- **--values | -f** - Specify override values YAML file. Indicate file name only here, path would be resolved according to path to the chart in the command. Can specify multiple value file - in that case and if different values files define same properties, properties in the files that appear later in the command will take precedence - just like helm works.

## 19. Use Case: Send Pull Request Data to ReARM

This use case is used in the CI workflow to stream Pull Request metadata to ReARM.

Sample command to send Pull Request details:

Sample command:
```bash
docker run --rm relizaio/rearm-cli    \
    prdata \
    -i component_or_organization_wide_api_id    \
    -k component_or_organization_wide_api_key    \
    -b <base branch name> \
    -s <pull request state - OPEN | CLOSED | MERGED> \
    -t <target branch name> \
    --endpoint <pull request endpoint> \
    --title <title> \
    --createdDate <ISO 8601 date > \
    --number <pull request number> \
    --commits <comma separated list of commit shas>
```

Flags stand for:

- **--branch | -b** - Name of the base branch for the pull request.
- **--state** - State of the pull request
- **--targetBranch | t** - Name of the target branch for the pull request.
- **--endpoint** - HTML endpoint of the pull request.
- **--title** - Title of the pull request.
- **--number** - Number of the pull request.
- **--commits** - Comma seprated commit shas on this pull request.
- **--commits** - SHA of current commit on the Pull Request (will be merged with existing list)
- **--createdDate** - Datetime when the pull request was created.
- **--closedDate** - Datetime when the pull request was closed.
- **--mergedDate** - Datetime when the pull request was merged.
- **--endpoint** - Title of the pull request.
- **--component** - Component UUID if org-wide key is used.

## 20. Use Case: Attach a downloadable artifact to a Release on ReARM

This use case is to attach a downloadable artifact to a Release on ReARM. For example, to add a report obtained by automated tests for a release.

Sample command:

```bash
docker run --rm relizaio/rearm-cli    \
    addDownloadableArtifact \
    -i api_id \ 
    -k api_key    \
    --releaseid release_uuid    \
    --artifactType TEST_REPORT \
    --file <path_to_the_report>
```

Flags stand for:
- **--file | -f** - flag to specify path to the artifact file.
- **--releaseid** - flag to specify release uuid, which can be obtained from the release view or programmatically (either this flag or component id and release version or component id and instance are required).
- **--component** - flag to specify component uuid, which can be obtained from the component settings on ReARM UI (either this flag and release version or releaseid must be provided).
- **--releaseversion** - flag to specify release string version with the component flag above (either this flag and component or releaseid must be provided).
- **--artifactType** - flag to specify type of the artifact - can be (TEST_REPORT, SECURITY_SCAN, DOCUMENTATION, GENERIC) or some user defined value .

# Development of Reliza-CLI

## Adding dependencies to Reliza-CLI

Dependencies are handled using go modules and imports file is automatically generated. If importing a github repository use this command first:

```bash
go get github.com/xxxxxx/xxxxxx
```

You then should be able to add what you need as an import to your files. Once they've been imported call this command to generate the imports file:

```bash
go generate ./internal/imports
```
