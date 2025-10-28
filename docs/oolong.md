# Oolong TEA Server Content Management Commands

Base Command: `oolong`

The `oolong` command provides tools for managing content in an Oolong TEA Server, including products, components, releases, and artifacts. These commands create and update YAML files in a structured content directory that can be used by the Oolong TEA Server.

**Global Flag:**
- **--contentdir** - Path to the content directory (required for all subcommands)

## Subcommands

- [12.1 Add Product](#121-add-product)
- [12.2 Add Component](#122-add-component)
- [12.3 Add Component Release](#123-add-component-release)
- [12.4 Add Product Release](#124-add-product-release)
- [12.5 Add Artifact](#125-add-artifact)
- [12.6 Add Artifact to Releases](#126-add-artifact-to-releases)
- [12.7 Add Distribution to Component Release](#127-add-distribution-to-component-release)

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

Sample command with artifacts:

```bash
rearm oolong add_component_release \
    --contentdir ./content \
    --component "Database Component" \
    --version "1.0.0" \
    --artifact "173cedd7-fabb-4d3a-9315-7d7465d236b6" \
    --artifact "abc12345-1234-5678-9abc-def012345678"
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
    --purl "pkg:generic/database-component@1.0.0-beta" \
    --artifact "173cedd7-fabb-4d3a-9315-7d7465d236b6"
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
- **--artifact** - Artifact UUID to add to initial collection (optional, can be specified multiple times)

**Output:**

```
Successfully created component release: 1.0.0
  Component: Database Component
  Directory: ./content/components/database_component/releases/1.0.0
  UUID: 7a7fa4da-bf9b-478f-b934-2fe9e0fc317c
  Created initial collection: collections/1.yaml
```

**Output with artifacts:**

```
Successfully created component release: 1.0.0
  Component: Database Component
  Directory: ./content/components/database_component/releases/1.0.0
  UUID: 7a7fa4da-bf9b-478f-b934-2fe9e0fc317c
  Artifacts added: 2
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

Sample command with artifacts:

```bash
rearm oolong add_product_release \
    --contentdir ./content \
    --product "My Product" \
    --version "1.0.0" \
    --artifact "173cedd7-fabb-4d3a-9315-7d7465d236b6" \
    --component "Database Component" \
    --component_release "1.0.0"
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
    --component_release "7a7fa4da-bf9b-478f-b934-2fe9e0fc317c" \
    --artifact "173cedd7-fabb-4d3a-9315-7d7465d236b6"
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
- **--artifact** - Artifact UUID to add to initial collection (optional, can be specified multiple times)

**Output:**

```
Successfully created product release: 1.0.0
  Product: My Product
  Directory: ./content/products/my_product/releases/1.0.0
  UUID: 9485fbc9-aa7d-4c26-95c9-d5a8ccb1c073
  Linked components: 2
  Created initial collection: collections/1.yaml
```

**Output with artifacts:**

```
Successfully created product release: 1.0.0
  Product: My Product
  Directory: ./content/products/my_product/releases/1.0.0
  UUID: 9485fbc9-aa7d-4c26-95c9-d5a8ccb1c073
  Linked components: 1
  Artifacts added: 1
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
8. Optionally links the artifact to specified component and/or product releases by:
   - Validating that component/componentrelease and product/productrelease flags are properly paired
   - Finding the latest collection version for each release
   - Creating a new collection version with the artifact added
   - Setting the update reason to `ARTIFACT_ADDED`

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

Sample command with release linking:

```bash
rearm oolong add_artifact \
    --contentdir ./content \
    --name "Product SBOM" \
    --type BOM \
    --mediatype "application/vnd.cyclonedx+json" \
    --url "https://example.com/artifacts/sbom.json" \
    --hash "sha256=abc123def456" \
    --component "Database Component" \
    --componentrelease "1.0.0" \
    --product "My Product" \
    --productrelease "1.0.0"
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
- **--component** - Component name or UUID to link (optional, can be specified multiple times, must be paired with `--componentrelease`)
- **--componentrelease** - Component release version or UUID to link (optional, can be specified multiple times, must be paired with `--component`)
- **--product** - Product name or UUID to link (optional, can be specified multiple times, must be paired with `--productrelease`)
- **--productrelease** - Product release version or UUID to link (optional, can be specified multiple times, must be paired with `--product`)

**Output:**

```
Successfully created artifact: Product SBOM
  Type: BOM
  File: ./content/artifacts/173cedd7-fabb-4d3a-9315-7d7465d236b6.yaml
  UUID: 173cedd7-fabb-4d3a-9315-7d7465d236b6
```

**Output with release linking:**

```
Successfully created artifact: Product SBOM
  Type: BOM
  File: ./content/artifacts/173cedd7-fabb-4d3a-9315-7d7465d236b6.yaml
  UUID: 173cedd7-fabb-4d3a-9315-7d7465d236b6

Linking artifact to releases...
Added artifact 173cedd7-fabb-4d3a-9315-7d7465d236b6 to component 'Database Component' release '1.0.0' (collection version 2)
Added artifact 173cedd7-fabb-4d3a-9315-7d7465d236b6 to product 'My Product' release '1.0.0' (collection version 2)

Successfully processed 2 release(s)
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

**Notes:**
- If `--description` is not provided, the `description` field will be empty in the YAML output.
- When linking to releases, the artifact is added to a new collection version with update reason `ARTIFACT_ADDED`. This is the same behavior as the `add_artifact_to_releases` command.
- You can link the artifact to multiple component and product releases in a single command by specifying the flags multiple times.

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

### 12.6 Add Artifact to Releases

The `oolong add_artifact_to_releases` command links an existing artifact to one or more component releases and/or product releases. For each release, it creates a new collection version with the artifact added.

**Process:**
1. Validates that component/componentrelease and product/productrelease flags are properly paired
2. Validates that at least one release is specified
3. Resolves all components and products by name or UUID (fails if any not found)
4. Resolves all component releases and product releases by version or UUID (fails if any not found)
5. For each release:
   - Finds the latest collection version
   - Checks if the artifact UUID already exists in that collection
   - If exists: logs message and skips
   - If not exists: creates new collection with incremented version, copies all data, adds artifact UUID, sets updateReason to ARTIFACT_ADDED
6. No changes are made if any validation fails (atomic operation)

Sample command with component release:

```bash
rearm oolong add_artifact_to_releases \
    --contentdir ./content \
    --artifactuuid "173cedd7-fabb-4d3a-9315-7d7465d236b6" \
    --component "Kauf Bulb Hardware" \
    --componentrelease "BLF10"
```

Sample command with product release:

```bash
rearm oolong add_artifact_to_releases \
    --contentdir ./content \
    --artifactuuid "173cedd7-fabb-4d3a-9315-7d7465d236b6" \
    --product "Kauf Bulb" \
    --productrelease "BLF10-1m-1.95"
```

Sample command with multiple component releases:

```bash
rearm oolong add_artifact_to_releases \
    --contentdir ./content \
    --artifactuuid "173cedd7-fabb-4d3a-9315-7d7465d236b6" \
    --component "Database Component" \
    --componentrelease "1.0.0" \
    --component "API Component" \
    --componentrelease "2.1.0"
```

Sample command with both component and product releases:

```bash
rearm oolong add_artifact_to_releases \
    --contentdir ./content \
    --artifactuuid "173cedd7-fabb-4d3a-9315-7d7465d236b6" \
    --component "adc0909a-3039-47eb-82ba-7686767c0d52" \
    --componentrelease "BLF10" \
    --product "8ab3c557-7f36-4ebd-a593-026c28337630" \
    --productrelease "BLF10-1m-1.95"
```

**Flags:**
- **--contentdir** - Content directory path (required, global flag)
- **--artifactuuid** - Artifact UUID to add (required)
- **--component** - Component name or UUID (optional, can be specified multiple times, must be paired with `--componentrelease`)
- **--componentrelease** - Component release version or UUID (optional, can be specified multiple times, must be paired with `--component`)
- **--product** - Product name or UUID (optional, can be specified multiple times, must be paired with `--productrelease`)
- **--productrelease** - Product release version or UUID (optional, can be specified multiple times, must be paired with `--product`)

**Output when artifact is added:**

```
Added artifact 173cedd7-fabb-4d3a-9315-7d7465d236b6 to component 'Kauf Bulb Hardware' release 'BLF10' (collection version 2)
Added artifact 173cedd7-fabb-4d3a-9315-7d7465d236b6 to product 'Kauf Bulb' release 'BLF10-1m-1.95' (collection version 2)

Successfully processed 2 release(s)
```

**Output when artifact already exists:**

```
Artifact 173cedd7-fabb-4d3a-9315-7d7465d236b6 already added to component 'Kauf Bulb Hardware' release 'BLF10'
Artifact 173cedd7-fabb-4d3a-9315-7d7465d236b6 already added to product 'Kauf Bulb' release 'BLF10-1m-1.95'

Successfully processed 2 release(s)
```

**Created Collection Format:**

When a new collection is created, it increments the version number and adds the artifact:

```yaml
version: 2
date: "2025-10-28T16:26:48Z"
updateReason:
  type: ARTIFACT_ADDED
  comment: Added artifact 173cedd7-fabb-4d3a-9315-7d7465d236b6
artifacts:
- 173cedd7-fabb-4d3a-9315-7d7465d236b6
```

If the previous collection already had artifacts, they are preserved:

```yaml
version: 3
date: "2025-10-28T16:30:00Z"
updateReason:
  type: ARTIFACT_ADDED
  comment: Added artifact 173cedd7-fabb-4d3a-9315-7d7465d236b6
artifacts:
- abc12345-1234-5678-9abc-def012345678
- 173cedd7-fabb-4d3a-9315-7d7465d236b6
```

**Pairing Rules:**

The `--component` and `--componentrelease` flags work in pairs:
- Each `--component` flag must have a corresponding `--componentrelease` flag
- The order matters: the first `--component` is paired with the first `--componentrelease`, and so on
- Both flags accept either names or UUIDs

The same pairing rules apply to `--product` and `--productrelease` flags.

**Atomic Operation:**

The command validates ALL releases before making ANY changes:
- If any component is not found, the command exits with an error
- If any component release is not found, the command exits with an error
- If any product is not found, the command exits with an error
- If any product release is not found, the command exits with an error
- Only if all validations pass will the command proceed to update collections

**Idempotent Behavior:**

Running the command multiple times with the same artifact and releases is safe:
- If the artifact is already in the latest collection, no new collection is created
- A message is logged indicating the artifact is already present
- The command succeeds without making changes

### 12.7 Add Distribution to Component Release

The `oolong add_distribution_to_component_release` command adds a distribution entry to an existing component release. Distributions represent different ways a component can be distributed (e.g., different binary formats, update files, etc.).

**Process:**
1. Resolves the component by name or UUID
2. Resolves the component release by version or UUID
3. Reads the existing release.yaml
4. Creates a new distribution entry with the provided metadata
5. Appends the distribution to the release's distributions array
6. Writes the updated release.yaml

Sample command:

```bash
rearm oolong add_distribution_to_component_release \
    --contentdir ./content \
    --component "Kauf Bulb Firmware" \
    --componentrelease "1.96" \
    --url "https://github.com/KaufHA/kauf-rgbww-bulbs/releases/download/v1.96/kauf-bulb-v1.96-update-1m.bin" \
    --hash "sha256=5db2debf05bf9d7dcf7397cf2d780acf6079589dfbd474183cf4da80d8564e29"
```

Sample command with all options:

```bash
rearm oolong add_distribution_to_component_release \
    --contentdir ./content \
    --component "Kauf Bulb Firmware" \
    --componentrelease "1.96" \
    --url "https://github.com/KaufHA/kauf-rgbww-bulbs/releases/download/v1.96/kauf-bulb-v1.96-update-4m.bin" \
    --signatureurl "https://github.com/KaufHA/kauf-rgbww-bulbs/releases/download/v1.96/kauf-bulb-v1.96-update-4m.bin.sig" \
    --description "4MB firmware update binary" \
    --distributiontype "FIRMWARE_UPDATE" \
    --hash "sha256=7429339b03f418ccc89408b3392388b3181c373d45f2279395811e38829d43e9" \
    --purl "pkg:generic/kauf-bulb-firmware@1.96?variant=4m" \
    --tei "urn:tei:hash:kaufha.com:SHA256:7429339b03f418ccc89408b3392388b3181c373d45f2279395811e38829d43e9"
```

Sample command with multiple hashes:

```bash
rearm oolong add_distribution_to_component_release \
    --contentdir ./content \
    --component "35218a8a-7e08-4502-8165-fa2b7a4a0b8a" \
    --componentrelease "ff742766-4925-4b97-871b-e3285b7f932a" \
    --url "https://example.com/firmware/v2.0.bin" \
    --hash "sha256=abc123def456" \
    --hash "sha512=fedcba654321"
```

**Flags:**
- **--contentdir** - Content directory path (required, global flag)
- **--component** - Component name or UUID (required)
- **--componentrelease** - Component release version or UUID (required)
- **--url** - Distribution URL (required)
- **--signatureurl** - Signature URL (optional)
- **--description** - Distribution description (optional)
- **--distributiontype** - Distribution type (optional)
- **--hash** - Hash in format `algorithm=value` (optional, can be specified multiple times)
- **--purl** - PURL identifier (optional, can be specified multiple times)
- **--tei** - TEI identifier (optional, can be specified multiple times)

**Output:**

```
Successfully added distribution to component release
  Component: Kauf Bulb Firmware
  Release: 1.96
  Distribution URL: https://github.com/KaufHA/kauf-rgbww-bulbs/releases/download/v1.96/kauf-bulb-v1.96-update-1m.bin
```

**Updated release.yaml Format:**

After adding distributions, the release.yaml will include a distributions array:

```yaml
uuid: ff742766-4925-4b97-871b-e3285b7f932a
version: 1.96
createdDate: "2025-10-16T18:03:53Z"
releaseDate: "2025-10-16T18:03:53Z"
preRelease: false
identifiers: []
distributions:
- distributionType: ""
  description: ""
  identifiers: []
  url: https://github.com/KaufHA/kauf-rgbww-bulbs/releases/download/v1.96/kauf-bulb-v1.96-update-1m.bin
  signatureUrl: ""
  checksums:
  - algType: SHA-256
    algValue: 5db2debf05bf9d7dcf7397cf2d780acf6079589dfbd474183cf4da80d8564e29
- distributionType: ""
  description: ""
  identifiers: []
  url: https://github.com/KaufHA/kauf-rgbww-bulbs/releases/download/v1.96/kauf-bulb-v1.96-update-4m.bin
  signatureUrl: ""
  checksums:
  - algType: SHA-256
    algValue: 7429339b03f418ccc89408b3392388b3181c373d45f2279395811e38829d43e9
```

**Component and Release Resolution:**

Both `--component` and `--componentrelease` flags accept either:
- **Name/Version** - Exact match of the component name or release version
- **UUID** - Full UUID of the component or release

The command will search through the content directory and match by either identifier type.

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

**Use Cases:**

This command is useful for:
- Adding multiple distribution formats for the same release (e.g., different binary variants)
- Adding firmware update files with checksums
- Providing different download options for different platforms or configurations
- Associating identifiers (PURLs, TEIs) with specific distributions
