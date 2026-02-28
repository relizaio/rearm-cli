# DevOps Commands

Base Command: `devops`

## 15.1 Export Instance CycloneDX BOM

The `devops exportinst` command outputs the CycloneDX specification of your instance. It queries ReARM for the instance revision and returns the full CycloneDX BOM in JSON format.

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

Sample command with specific revision and namespace:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    devops exportinst \
    -i instance_api_id \
    -k instance_api_key \
    --instance "instance-uuid" \
    --revision 3 \
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
- **--revision** - Instance revision number (optional, defaults to latest revision if not specified)
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
- **--revision** - Instance revision for which to generate tags (optional)
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
    --artdigest "sha256:abc123..."
```

**Flags:**
- **--instance** - UUID of instance for which to generate (either this, or instanceuri must be provided).
- **--instanceuri** - URI of instance for which to generate (either this, or instanceuri must be provided).
- **--artdigest** - Digest or hash of the deliverable to resolve secrets for (required).
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