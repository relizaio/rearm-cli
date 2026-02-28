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
