# Transparency Exchange API (TEA) Commands

Base Command: `tea`

## 11.1 Transparency Exchange API (TEA) Discovery

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
