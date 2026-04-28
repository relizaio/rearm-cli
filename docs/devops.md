# DevOps Commands

Base Command: `devops`

## 15.1 Export Instance CycloneDX BOM

The `devops exportinst` command outputs the CycloneDX specification of your instance. It queries ReARM for the instance revision and returns the full CycloneDX BOM in JSON format.

The **--statetype** flag controls which state of the instance is exported:
- Default value is *PLAN* - outputs all product and component releases that are *approved* (expected) for the instance.
- Set to *ACTUAL* to output component releases *currently deployed* on the instance.

The **--revision** flag selects a specific historical revision and can be combined with `--statetype`:
- Default value is *-1*, which means the current state (latest approved or latest deployed, depending on `--statetype`).
- Set to a specific revision number obtained from ReARM to retrieve data for that exact revision.


Sample command using instance UUID:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    devops exportinst \
    -i instance_api_id \
    -k instance_api_key \
    --instance "instance-uuid"
```

Sample command using instance URI:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    devops exportinst \
    -i instance_api_id \
    -k instance_api_key \
    --instanceuri "instance-uri"
```

Sample command for ACTUAL (currently deployed) state:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    devops exportinst \
    -i instance_api_id \
    -k instance_api_key \
    --instance "instance-uuid" \
    --statetype ACTUAL
```

Sample command with specific revision and namespace:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    devops exportinst \
    -i instance_api_id \
    -k instance_api_key \
    --instance "instance-uuid" \
    --revision 3 \
    --statetype PLAN \
    --namespace "production"
```

Note: authentication can also be performed using instance-level or cluster-level API keys (prefixed with `INSTANCE__` or `CLUSTER__`), in which case `--instance` and `--instanceuri` flags are optional.

Sample command with instance-level API key:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    devops exportinst \
    -i INSTANCE__instance-api-id \
    -k instance-api-key
```

**Flags:**
- **--instance** - Instance UUID (required unless instanceuri is provided or an instance/cluster API key is used)
- **--instanceuri** - Instance URI, alternative to instance UUID (optional)
- **--revision** - Instance revision number (optional, default is -1 meaning current state)
- **--statetype** - Instance state type: `PLAN` (approved/expected, default) or `ACTUAL` (currently deployed). Can be combined with `--revision` (optional)
- **--namespace** - Namespace within the instance (optional)

**Output:**

On success, the command outputs the full CycloneDX JSON BOM for the instance revision:

```json
{
  "bomFormat": "CycloneDX",
  "specVersion": "1.6",
  "version": 1,
  "metadata": { ... },
  "components": [ ... ]
}
```

On failure, an error message is displayed.

## 15.2 Set Sealed Secret Certificate on Instance

The `devops setsecretcert` command sets the Bitnami Sealed Certificate property on an instance. This certificate is used to encrypt secrets for the instance. Only supports instance's own API Key.

Sample command:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    devops setsecretcert \
    -i instance_api_id \
    -k instance_api_key \
    --cert "sealed-certificate-value"
```

**Flags:**
- **--cert** - Sealed certificate used by the instance (required)

**Output:**

On success, the command outputs the JSON response from the mutation.

On failure, an error message is displayed.

## 15.3 Replace Tags on Deployment Templates for GitOps

The `devops replacetags` command replaces image tags in deployment templates (Helm values, Docker Compose, k8s manifests) with correct artifact versions. Tags can be sourced from an instance revision, a bundle version, an environment, or a local CycloneDX/text file.

### Using Instance and Revision

```bash
docker run --rm \
    -v /local/path/to/values.yaml:/values.yaml \
    -v /local/path/to/output_dir:/output_dir \
    registry.relizahub.com/library/rearm-cli \
    devops replacetags \
    -i api_id \
    -k api_key \
    --instanceuri <instance_uri> \
    --revision <revision_number> \
    --infile /values.yaml \
    --outfile /output_dir/output_values.yaml
```

### Using Product and Version

```bash
docker run --rm \
    -v /local/path/to/values.yaml:/values.yaml \
    -v /local/path/to/output_dir:/output_dir \
    registry.relizahub.com/library/rearm-cli \
    devops replacetags \
    -i api_id \
    -k api_key \
    --product <product_name> \
    --version <product_version> \
    --infile /values.yaml \
    --outfile /output_dir/output_values.yaml
```

### Using Environment

```bash
docker run --rm \
    -v /local/path/to/values.yaml:/values.yaml \
    -v /local/path/to/output_dir:/output_dir \
    registry.relizahub.com/library/rearm-cli \
    devops replacetags \
    -i api_id \
    -k api_key \
    --env <environment_name> \
    --infile /values.yaml \
    --outfile /output_dir/output_values.yaml
```

**Flags:**
- **--infile** - Input file to parse, such as helm values file or docker compose file
- **--outfile** - Output file with parsed values (optional, if not supplied - outputs to stdout)
- **--indirectory** - Path to directory of input files to parse (either infile or indirectory is required)
- **--outdirectory** - Path to directory of output files (required if indirectory is used)
- **--tagsource** - Source file with tags (optional, specify either source file or instance/product/environment)
- **--env** - Environment for which to generate tags (optional)
- **--instance** - Instance UUID for which to generate tags (optional)
- **--instanceuri** - Instance URI for which to generate tags (optional)
- **--revision** - Instance revision for which to generate tags (optional, default is -1 meaning current state)
- **--statetype** - Instance state type: `PLAN` (approved/expected, default) or `ACTUAL` (currently deployed). Can be combined with `--revision` (optional)
- **--namespace** - Specific namespace for replace tagging (optional)
- **--product** - UUID or Name of product for which to generate tags (optional)
- **--version** - Product version for which to generate tags (optional, required when using product)
- **--defsource** - Source file for definitions, e.g. output of helm template command (optional)
- **--type** - Type of source tags file: cyclonedx (default) or text
- **--provenance** - Enable/disable adding provenance metadata to beginning of outfile (optional, default true)
- **--parsemode** - Parse mode: extended (default), simple (only image tags), or strict (fail if artifact not found)
- **--fordiff** - Resolve secrets by timestamp instead of sealed value; disables provenance (optional, default false)
- **--resolveprops** - Resolve instance properties and secrets from ReARM (optional, default false)
- **--usenamespaceproduct** - Use namespace and product for prop resolution (optional, default false)

**Property and Secret Resolution:**

To resolve secrets and properties from instances, set `--resolveprops` to true. In the templated file, properties are defined as `$RELIZA{PROPERTY.property_key}` (with optional default: `$RELIZA{PROPERTY.property_key:default_value}`), and secrets as `$RELIZA{SECRET.secret_key}` or `$RELIZA{PLAINSECRET.secret_key}`.

## 15.4 Retrieve Instance Properties and Secrets

The `devops instprops` command retrieves specific properties and secrets for an instance from ReARM. Secrets are only returned if allowed to be read by the instance, if the instance has a sealed certificate set, and in encrypted form.

Sample command:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    devops instprops \
    -i instance_api_id \
    -k instance_api_key \
    --instance "instance-uuid" \
    --property "FQDN" \
    --secret "DB_PASSWORD"
```

**Flags:**
- **--instanceuri** - URI of the instance (optional, either instanceuri or instance flag or instance API key must be used).
- **--revision** - Revision number for the instance to use as a source for properties (optional, defaults to -1 which represents latest).
- **--namespace** - Specific namespace of the instance to use to retrieve sealed secrets - as secrets are returned sealed with namespace scope (optional, default to "default").
- **--product** - UUID or name of specific product (optional)
- **--usenamespaceproduct** - Use namespace and product for prop resolution (optional, default false)
- **--property** - Specifies name of the property to retrieve. For multiple properties, use multiple --property flags.
- **--secret** - Specifies name of the secret to retrieve. For multiple secrets, use multiple --secret flags.

## 15.5 Override and Get Merged Helm Chart Values

The `devops helmvalues` command lets you do a helm style override of the default helm chart values and outputs merged helm values.

Sample command:

```bash
docker run --rm \
    -v /local/path/to/chart:/chart \
    registry.relizahub.com/library/rearm-cli \
    devops helmvalues /chart \
    -f values-override-1.yaml \
    -f values-override-2.yaml \
    -o /chart/output-values.yaml
```

**Flags:**
- **--outfile | -o** - Output file with merged values (optional, if not supplied - outputs to stdout).
- **--values | -f** - Specify override values YAML file. Indicate file name only here, path would be resolved according to path to the chart in the command. Can specify multiple value files - in that case and if different values files define same properties, properties in the files that appear later in the command will take precedence - just like helm works.

## 15.6 Get Deliverable Download Secrets

The `devops delsecrets` command retrieves secrets needed to download a specific deliverable. The deliverable must belong to the organization.

Sample command:

```bash
docker run --rm \
    registry.relizahub.com/library/rearm-cli \
    devops delsecrets \
    -i api_id \
    -k api_key \
    --instance "instance-uuid" \
    --deldigest "sha256:abc123..."
```

**Flags:**
- **--instance** - UUID of instance for which to generate (either this, or instanceuri must be provided).
- **--instanceuri** - URI of instance for which to generate (either this, or instanceuri must be provided).
- **--deldigest** - Digest or hash of the deliverable to resolve secrets for (required).
- **--namespace** - Namespace to use for secrets (optional, defaults to default namespace).

## 15.7 Check if Instance Has Sealed Secret Certificate

The `devops iscertinit` command checks whether an instance has a Bitnami Sealed Certificate property configured. This property is used to encrypt secrets for the instance.

Sample command:

```bash
docker run --rm \
    registry.relizahub.com/library/rearm-cli \
    devops iscertinit \
    -i api_id \
    -k api_key \
    --instance "instance-uuid"
```

**Flags:**
- **--instance** - UUID of instance for which to check (optional, either this or instanceuri must be provided).
- **--instanceuri** - URI of instance for which to check (optional, either this or instance must be provided).

## 15.8 Send Deployment Metadata From Instance

The `devops instdata` command sends digests of active deployments from an instance to ReARM. The API key must be generated for the instance from ReARM.

Sample command:

```bash
docker run --rm \
    registry.relizahub.com/library/rearm-cli \
    devops instdata \
    -i instance_api_id \
    -k instance_api_key \
    --images "sha256:c10779b369c6f2638e4c7483a3ab06f13b3f57497154b092c87e1b15088027a5 sha256:e6c2bcd817beeb94f05eaca2ca2fce5c9a24dc29bde89fbf839b652824304703" \
    --namespace default \
    --sender sender1
```

**Flags:**
- **--images** - Whitespace-separated sha256 digests of images sent from the instance (optional, either images or imagefile must be provided). Sending full docker image URIs with digests is also accepted, e.g. `relizaio/reliza-cli:latest@sha256:ebe68a0427bf88d748a4cad0a419392c75c867a216b70d4cd9ef68e8031fe7af`.
- **--imagefile** - Absolute path to file with image string or image k8s json (optional, either images or imagefile must be provided). Default value: `/resources/images`. Use `kubectl get po -o json | jq "[.items[] | {namespace:.metadata.namespace, pod:.metadata.name, status:.status.containerStatuses[]}]"` to obtain k8s json.
- **--imagestyle** - Set to "k8s" for k8s json image format (optional).
- **--namespace** - Namespace where images are being sent (optional, defaults to "default"). Namespaces are useful to separate different products deployed on the same instance.
- **--sender** - Unique sender within a single namespace (optional). Useful when different nodes stream only part of application deployment data — nodes should use the same namespace but different senders so their data does not overwrite each other.

## 15.9 List Product Feature Sets on an Instance Plan

The `devops listfeaturesets` command returns the per-product feature-set inventory of an instance's plan. For every product currently deployed on the plan, the response includes the product (uuid + name), the feature set currently mapped to it, and the full list of active feature sets the caller could switch that deployment to.

This is the discovery side of the integration with [versionfeatureset](#1511-version-feature-set-on-a-product) and [switchfeatureset](#1510-switch-product-feature-set-on-an-instance) — call `listfeaturesets` first to learn the product UUIDs and current feature-set UUIDs, then use them to drive a versioning or switch.

**FREEFORM-only**: requires a FREEFORM API key with `DEVOPS_READ` on the instance (or its parent cluster, via the cluster→child permission cascade).

Sample command using instance URI:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    devops listfeaturesets \
    -i freeform_api_id \
    -k freeform_api_key \
    --instanceuri "https://my.sandbox.example.com" \
    --namespace "production"
```

Sample command using instance UUID:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    devops listfeaturesets \
    -i freeform_api_id \
    -k freeform_api_key \
    --instance "instance-uuid" \
    --namespace "production"
```

**Flags:**
- **--instance** - Instance UUID (either this or `--instanceuri` is required).
- **--instanceuri** - Instance URI; resolved against the FREEFORM key's org (either this or `--instance` is required).
- **--namespace** - Namespace whose deployments to inspect (required for STANDALONE_INSTANCE and CLUSTER; ignored for CLUSTER_INSTANCE — the server pins the namespace to the instance's own namespace and any value passed is ignored).

**Output:** JSON array; one entry per product deployed in the namespace. Each entry has `product { uuid, name }`, `currentFeatureSet { uuid, name }`, and `availableFeatureSets [{ uuid, name }]`.

## 15.10 Switch Product Feature Set on an Instance

The `devops switchfeatureset` command changes which feature set is deployed for a given product on an instance plan. The new feature set must be a branch on the same product. ReARM CD picks the change up on its next reconcile, so the sandbox / instance rolls to whatever release is current on the new feature set.

**FREEFORM-only**: requires a FREEFORM API key with `DEVOPS_WRITE` on the instance (or its parent cluster).

Sample command using instance URI:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    devops switchfeatureset \
    -i freeform_api_id \
    -k freeform_api_key \
    --instanceuri "https://my.sandbox.example.com" \
    --product "product-uuid" \
    --featureset "feature-set-uuid" \
    --namespace "production"
```

**Flags:**
- **--instance** - Instance UUID (either this or `--instanceuri` is required).
- **--instanceuri** - Instance URI; resolved against the FREEFORM key's org (either this or `--instance` is required).
- **--product** - UUID of the PRODUCT component whose deployment to switch (required).
- **--featureset** - UUID of the new feature set (branch on the same product) to switch the deployment to (required).
- **--namespace** - Namespace of the deployment to switch (required for STANDALONE_INSTANCE / CLUSTER; ignored for CLUSTER_INSTANCE).

**Output:** JSON of the updated instance.

## 15.11 Version Feature Set on a Product

The `devops versionfeatureset` command spins up a new feature set on a PRODUCT, copying the BASE feature set's dependency configuration and re-pointing the listed dependencies to the supplied branches. The new feature set is named after the first override branch, gets `autoIntegrate=ENABLED`, and an auto-integrate run is triggered before the call returns — so the new feature set picks up the latest releases from the overridden branches automatically.

A typical flow is: a developer pushes a feature branch on a constituent component (e.g. `backend`) to CI; once the build runs the ReARM `getversion` step the branch is registered. Calling `versionfeatureset` with that component+branch then creates a new product feature set wired to that branch, and `switchfeatureset` points an instance at it for testing.

Each override identifies its component either by `componentUuid` (UUID) or by `(vcsUri, repoPath)` — the latter reuses the same component-by-VCS lookup other commands use. The override `branch` is the **name** of the branch on the resolved component; the branch must already exist (no auto-creation).

**FREEFORM-only**: requires a FREEFORM API key with `VERSION_FEATURESET` permission function granted at scope `COMPONENT` on the product. A `READ_ONLY`-or-stronger key with the function is sufficient; the function is the gate.

Sample command using component UUIDs:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    devops versionfeatureset \
    -i freeform_api_id \
    -k freeform_api_key \
    --product "product-uuid" \
    --overrides '[{"componentUuid":"backend-component-uuid","branch":"my-feature-branch"}]'
```

Sample command using VCS URI + repo path:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    devops versionfeatureset \
    -i freeform_api_id \
    -k freeform_api_key \
    --product "product-uuid" \
    --overrides '[{"vcsUri":"https://github.com/org/repo","repoPath":"backend","branch":"my-feature-branch"}]'
```

Multiple overrides in the same call:

```bash
--overrides '[
  {"componentUuid":"backend-uuid","branch":"my-feature-branch"},
  {"componentUuid":"rebom-uuid","branch":"my-feature-branch"}
]'
```

When multiple overrides are supplied with different branch names, the new feature set is named after the first override's branch arbitrarily.

**Flags:**
- **--product** - UUID of the PRODUCT component to version (required, always by UUID).
- **--overrides** - JSON array of dependency-branch overrides (required). Each entry: `{"componentUuid":"...","branch":"..."}` or `{"vcsUri":"...","repoPath":"...","branch":"..."}`.

**Validation:**
- The product must exist and be of type PRODUCT.
- Each override must resolve a component (by UUID or `(vcsUri, repoPath)`) and a branch (by name on that component).
- Every override component must already be a dependency of the product's BASE feature set — overrides are pure branch tweaks, not new dependencies.
- A feature set with the chosen new name must not already exist on the product.

**Output:** JSON of the newly-created `Branch` (the new feature set), including its UUID for use as the input to a subsequent `switchfeatureset` call.