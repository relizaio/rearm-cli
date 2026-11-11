# Transparency Exchange API (TEA) Commands

Base Command: `tea`

The `tea` commands are a client of the [Transparency Exchange API (TEA)](https://github.com/CycloneDX/transparency-exchange-api) version 1.0.0, built on the Go library [tea-client-go](https://github.com/oolong-project/tea-client-go). The interactive commands (`tea discovery`, `tea full_tea_flow`) resolve a Transparency Exchange Identifier (TEI) and read the product release it names for a person: they print text and artifact URLs, and download nothing. The machine mode (`tea machine <verb>`, 11.6) is for a service: it answers in JSON, bounds what it reads, downloads verified artifacts into a directory and refuses to connect to the host it runs on.

## Table of Contents

1. [Transparency Exchange API (TEA) Discovery](#111-transparency-exchange-api-tea-discovery)
2. [Complete TEA Flow - Product and Component Details](#112-complete-tea-flow---product-and-component-details)
3. [Authentication](#113-authentication)
4. [Exit codes](#114-exit-codes)
5. [Changes from earlier versions](#115-changes-from-earlier-versions)
6. [Machine mode](#116-machine-mode)

---

## 11.1 Transparency Exchange API (TEA) Discovery

The `tea discovery` command resolves a TEI to a product release UUID with TEA 1.0.0 discovery and prints the UUID.

**TEI format:**

```
tei://<domain>/<type>/<id>
```

- **domain** - the DNS name that serves the TEA well-known document (for example `demo.rearmhq.com`). No port, no IP address.
- **type** - one of the TEI types of TEA 1.0.0: `uuid`, `purl`, `hash`, `eanupc`, `gtin`, `asin`, `udi`.
- **id** - the identifier. For `purl` it is the Package URL encoded as unpadded Base64URL.

The legacy form `urn:tei:<type>:<domain>:<id>` is not accepted (see 11.5).

**The discovery process** (done by the library, as the TEA specification defines it):

1. Fetch `https://<domain>/.well-known/tea`.
2. Choose the endpoint with the greatest supported version (1.0.0), then the highest priority.
3. Call `<endpoint>/v1.0.0/discovery?tei=<TEI>`. A failed attempt (connection, TLS, timeout, server error) is retried with backoff and the next endpoint is tried, at most 4 requests in all. A TEA "not found" answer ends discovery.
4. Print the product release UUID.

When the TEI resolves to several product releases, the command lists them on stderr and asks which one to print. It asks only when standard input is a terminal; otherwise it lists them and exits with code 4 without reading standard input.

Sample command (the public ReARM demo):

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    tea discovery \
    --tei "tei://demo.rearmhq.com/uuid/b9f115fb-6688-4a3f-a262-3acc04e1fc9e"
```

Sample command that may have to ask for a choice (`-it` gives the container a terminal):

```bash
docker run --rm -it registry.relizahub.com/library/rearm-cli \
    tea discovery \
    --tei "tei://products.example.com/purl/cGtnOnB5cGkvY3ljbG9uZWR4LXB5dGhvbi1saWJAOC40LjA"
```

Sample command against a local TEA server over plain http, for local testing only (plain http is not compliant with the TEA specification):

```bash
docker run --rm --network host registry.relizahub.com/library/rearm-cli \
    tea discovery \
    --tei "tei://localhost/uuid/d4d9f54a-abcf-41ee-ac79-1a52914d44b0" \
    --usehttp \
    --useport 8086
```

**Flags:**
- **--tei** - the TEI to resolve (required)
- **--usehttp** - allow plain http on one origin only, `http://<domain>:<useport>` (or `http://<domain>` on port 80 without `--useport`). Every other URL must be https: an endpoint, a server or a redirect on another plain-http host or port is refused (exit code 5).
- **--useport** - the port of the well-known resource (default 443 over https)
- **--credential-stdin** - read the TEA credential from standard input (see 11.3)

**Output:**

On success, the product release UUID on stdout:
```
b9f115fb-6688-4a3f-a262-3acc04e1fc9e
```

With several results, the list and the prompt on stderr (stdout carries only the UUID):
```
Multiple discovery results found (2 results).
  1) Example Product 1.2.3 (product release 2a9e5c10-3d0b-4c6e-9d5e-0f1a2b3c4d5e, server https://products.example.com/tea)
  2) Example Product 1.2.3-hotfix (product release 7b1f6d21-4e1c-4d7f-8e6f-1a2b3c4d5e6f, server https://products.example.com/tea)
Enter your choice (number): 
```

On failure, one line on stderr and a non-zero exit code (11.4):
```
Error: TEI not resolved: tea: identifier not resolved by https://products.example.com/tea/v1.0.0: OBJECT_UNKNOWN
```

**Debug Mode:**

`-d true` prints, on stderr, the TEI, the kind of credential and one line per request and response: scheme, host, port and path only, never a query string, a header or a credential.

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    tea discovery \
    -d true \
    --tei "tei://demo.rearmhq.com/uuid/b9f115fb-6688-4a3f-a262-3acc04e1fc9e"
```

```
Resolving TEI: tei://demo.rearmhq.com/uuid/b9f115fb-6688-4a3f-a262-3acc04e1fc9e
TEA credential: none
TEA request [well-known]: GET https://demo.rearmhq.com/.well-known/tea
TEA response [well-known]: 200 https://demo.rearmhq.com/.well-known/tea
TEA request [api-discovery]: GET https://demo.rearmhq.com/tea/v1.0.0/discovery
TEA response [api-discovery]: 200 https://demo.rearmhq.com/tea/v1.0.0/discovery
```

## 11.2 Complete TEA Flow - Product and Component Details

The `tea full_tea_flow` command discovers a product release from a TEI, as `tea discovery` does, and reads it from the server discovery names.

**The flow:**
1. Discovery (11.1), with the same choice when the TEI resolves to several product releases.
2. The product release: product name, version, lifecycle events (CLE) and the artifacts of its latest collection.
3. For each component of the product release, in order:
   - a pinned component: that component release;
   - an unpinned component: its newest release by creation date, with a note;
   - the component release: name, version, lifecycle events and the artifacts of its latest collection.

Each artifact format shows its description, media type, URL and signature URL. A format without an external URL shows the TEA server's download endpoint for it. A URL shown is the server's claim; the command fetches none of them.

Everything is printed after it has been read. A component that cannot be read is reported on stderr as a `Warning:` line and the command exits with code 7. Text a server supplies is printed with control and format characters escaped (for example `\x1b`, `\n`, `\u202e`) and cut at 2048 characters.

Sample command:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    tea full_tea_flow \
    --tei "tei://demo.rearmhq.com/uuid/b9f115fb-6688-4a3f-a262-3acc04e1fc9e"
```

Sample command with debug output:

```bash
docker run --rm registry.relizahub.com/library/rearm-cli \
    tea full_tea_flow \
    -d true \
    --tei "tei://demo.rearmhq.com/uuid/b9f115fb-6688-4a3f-a262-3acc04e1fc9e"
```

**Flags:**
- **--tei**, **--usehttp**, **--useport**, **--credential-stdin** - as in 11.1
- **-d** - debug output on stderr (11.1)

**Sample Output:**

```

=== Product Information ===
Product Name: Sample Product
Version: 1.0.0
  Lifecycle Events (CLE):
    [2] type=released effective=2026-01-15T00:00:00Z published=2026-01-15T00:00:00Z version=1.0.0 license=Apache-2.0 description="First \"GA\" release" references=https://example.com/notes,https://example.com/more
    [1] type=supersededBy effective=2026-01-10T00:00:00Z published=2026-01-11T12:30:00.5Z versions=0.9.0,vers:generic/<0.9.0 supportId=standard supersededBy=1.0.0 reason="replaced"

  Artifact: Product SBOM (BOM)
    - Description: CycloneDX JSON
      Media Type: application/vnd.cyclonedx+json
      URL: https://cdn.example.com/p.json


--- Component: Core Lib ---
Version: 2.0.0
  Lifecycle Events (CLE):
    [1] type=endOfLife effective=2027-01-01T00:00:00Z published=2026-01-15T00:00:00Z

  Artifact: Build SBOM (BOM)
    - Description: CycloneDX JSON
      Media Type: application/vnd.cyclonedx+json
      URL: https://cdn.example.com/c.json
      Signature URL: https://cdn.example.com/c.json.sig
    - Description: 
      Media Type: application/vnd.cyclonedx+xml
      URL: https://tea.example.com/tea/v1.0.0/artifact/66666666-6666-4666-8666-666666666662/2/download?mediaType=application%2Fvnd.cyclonedx%2Bxml

  Artifact: LICENSE
    - Description: License
      Media Type: text/plain
      URL: https://tea.example.com/tea/v1.0.0/artifact/66666666-6666-4666-8666-666666666662/1/download?mediaType=text%2Fplain
Note: Component 'Free Part' does not have a pinned release. Selecting latest available release.

--- Component: Free Part ---
Version: 3.1.0
  No artifacts in the latest collection of this component release.
```

Lifecycle timestamps are printed as UTC instants (RFC 3339).

## 11.3 Authentication

A TEA server may require a credential for some or all of its content. The credential comes from environment variables, or from standard input; never from a flag value and never from `~/.rearm.yaml`.

| Variable | Meaning |
| --- | --- |
| `REARM_TEA_AUTH_ROOT` | the API root the credential belongs to, exactly as the server advertises it, for example `https://tea.example.com/tea`. Required with any credential. Not a secret. |
| `REARM_TEA_TOKEN` | a bearer token |
| `REARM_TEA_APIKEYID`, `REARM_TEA_APIKEY` | an API key id and secret. The command exchanges them for a token once, at `<root>/v1.0.0/token`, before its first request. |

`--credential-stdin` reads the secret from standard input instead of the variable: the key secret when `REARM_TEA_APIKEYID` is set, else the token. On a terminal it is read without echo; otherwise the first line is taken (at most 16 KiB).

Rules:

- **The credential is sent only under the root you state.** Every request under `REARM_TEA_AUTH_ROOT` (same scheme, host, port, and a path at or below the root) carries `Authorization: Bearer <token>`; no other request does, redirects included. The root is not taken from anything a server sends, so a TEI from a third party cannot send your token elsewhere. A request outside the root is made without the credential; when the server answers 401 or 403, the error line says where the request went.
- **The ReARM API key is never sent to a TEA server.** `-k`, `-i`, `REARM_APIKEY`, `REARM_API_KEY`, `REARM_APIKEYID`, `REARM_API_ID` and the login session belong to your ReARM instance; the TEA commands do not read them.
- Contradictory settings are refused before any request (exit code 2): a token together with a key; a key id without its secret or the reverse; a credential without `REARM_TEA_AUTH_ROOT`; `--credential-stdin` together with the variable it replaces; an empty secret; a root with a user name or password, a query, a fragment or a trailing slash.
- **Plain http is for a local server only.** An `http` root is accepted only on the origin `--usehttp` and `--useport` allow, and the credential then travels in clear text.
- `REARM_TEA_AUTH_ROOT` alone, without a credential, is ignored.

**Finding the root:** it is the `url` of an endpoint in `https://<domain>/.well-known/tea`, or the part before `/v1.0.0` of the request lines `-d true` prints. A ReARM server's root is `https://<host>/tea/<organization TEA id>`, or `https://<host>/tea` on a custom host.

Sample command with an API key:

```bash
export REARM_TEA_AUTH_ROOT=https://tea.example.com/tea
export REARM_TEA_APIKEYID=<key id>
read -rs REARM_TEA_APIKEY && export REARM_TEA_APIKEY
docker run --rm -e REARM_TEA_AUTH_ROOT -e REARM_TEA_APIKEYID -e REARM_TEA_APIKEY \
    registry.relizahub.com/library/rearm-cli \
    tea full_tea_flow \
    --tei "tei://tea.example.com/uuid/d4d9f54a-abcf-41ee-ac79-1a52914d44b0"
```

Sample command with a token on standard input:

```bash
printf '%s\n' "$TOKEN" | docker run --rm -i -e REARM_TEA_AUTH_ROOT \
    registry.relizahub.com/library/rearm-cli \
    tea discovery --credential-stdin \
    --tei "tei://tea.example.com/uuid/d4d9f54a-abcf-41ee-ac79-1a52914d44b0"
```

Error lines (exit code 6), which never contain the token, the key id or the secret:

```
Error: the TEA server requires a credential (HTTP 401): ...; set REARM_TEA_AUTH_ROOT with REARM_TEA_TOKEN, or with REARM_TEA_APIKEYID and REARM_TEA_APIKEY
Error: the TEA server refused the request (HTTP 401): ...; the credential is sent only under <root>, the refused request went to <url>
Error: the TEA token endpoint refused the API key: requesting token: HTTP 401: invalid_client
Error: the TEA server at <root> has no token endpoint (HTTP 404): unset REARM_TEA_APIKEYID and REARM_TEA_APIKEY to call it without a credential
Error: no TEA access token: ...
```

## 11.4 Exit codes

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | any other failure: connection, TLS, DNS, a time bound, a server error, an invalid or oversized answer, discovery attempts used up, an interrupt; also a flag the command does not know or a missing `--tei` |
| 2 | usage: an invalid TEI (the `urn:tei:` form included), an invalid `--useport`, invalid or contradictory credential variables |
| 3 | not resolved: the server does not know the TEI, or the product release discovery named |
| 4 | several product releases and none chosen (standard input is not a terminal, or the answer was not a number in range) |
| 5 | a URL was refused by the URL policy: plain http outside the origin `--usehttp` and `--useport` allow, a user name or password in a URL, too many redirects |
| 6 | authentication: HTTP 401 or 403, or the token endpoint refused the API key or gave no usable token |
| 7 | `full_tea_flow` printed the product release but at least one component could not be read; in the machine mode, `download` wrote its manifest and at least one selected file is not verified |
| 8 | machine mode only: a connection to an address was refused (loopback, this host, link-local, unspecified, cloud metadata; 11.6) |
| 9 | machine mode only: a limit was hit; the error object names it |
| 10 | machine mode only: a connection could not be made (name resolution, refused, unreachable, timed out) |

The interactive commands never return 8, 9 or 10: they keep 1 for a time bound or an oversized answer. Every failure of an interactive command is one line on stderr starting with `Error:`; the machine mode writes a JSON error object instead (11.6).

## 11.5 Changes from earlier versions

1. TEA 1.0.0 only. A server that offers only the 0.4.0 beta is not supported.
2. Only `tei://<domain>/<type>/<id>` is accepted; the `urn:tei:...` form is refused (exit code 2).
3. Plain http is used only with `--usehttp`, and only on that one origin: an endpoint, a server root or a redirect on another http host or port is refused. Without `--usehttp` nothing is fetched over http.
4. `--usehttp` without `--useport` uses port 80 (it used 443).
5. An unpinned component shows its newest release by creation date (earlier versions showed the oldest).
6. Several discovery results: one numbered list of the product releases and one choice, on stderr, instead of questions about component versions on stdout. When standard input is not a terminal the command lists them and exits with code 4; it never reads a piped answer.
7. Errors are one line; the exit code tells the kind (it was always 1). `full_tea_flow` exits with code 7 when a component could not be read (it exited 0).
8. Debug output is on stderr, one line per request without the query string. The separate DNS step and its lines are gone.
9. Text a server supplies is escaped (control and format characters) and cut at 2048 characters when printed.
10. Lifecycle timestamps print as UTC RFC 3339.
11. The product release's own artifacts are printed. A format without an external URL prints the TEA download endpoint. An empty collection prints `No artifacts in the latest collection of this component release.`
12. The endpoint is chosen as the specification says (the greatest supported version, then priority), with retries and failover; after discovery the API is called at the server discovery names, not at the well-known endpoint.
13. A lifecycle (CLE) request that fails, other than "not implemented" or "unknown", is reported: for the product release the command fails, for a component it is a warning. It was ignored.
14. `full_tea_flow` prints after everything was read, not while reading.
15. `HTTPS_PROXY` and `NO_PROXY` are honoured for https as well.
16. The printed product release UUID is in canonical lowercase form.
17. New: credentials (11.3).

## 11.6 Machine mode

`rearm tea machine <verb>` is the TEA client for a service, such as ReARM's rebom fetching from supplier TEA servers. Its rules:

1. **Every setting is on the command line.** The machine mode reads no configuration file (`~/.rearm.*`, `--config`) and no `REARM_<FLAG>` variable, so nothing in the environment can switch `--allow-this-host` or `--usehttp` on. The only variables read are the four TEA credential variables (11.3). The ReARM API key and login session are not read.
2. **It never prompts.** Standard input is read only with `--credential-stdin`, and then only its first line (at most 16 KiB); with `--credential-stdin` and a terminal on standard input the run is a usage error.
3. **stdout is one JSON document followed by a line feed, or nothing.** It carries a document only with exit code 0 or 7. `--help` prints cobra's help.
4. **stderr is empty on exit 0 and 7, and one JSON error object line otherwise.** With `-d true`, trace lines come before it; the error object is then the last line and the only one starting with `{`.
5. Flag errors, a missing or unknown verb and a missing required flag are usage errors: the error object with reason `USAGE`, exit code 2.
6. `SIGINT` and `SIGTERM` end the run: reason `INTERRUPTED`, exit code 1, stdout empty.

### Verbs

| Verb | Answers | Required flags | Document |
| --- | --- | --- | --- |
| `resolve` | which product release(s) a TEI names, and on which servers (discovery only) | `--tei` | `TEA_RESOLUTION` |
| `product` | name and identifiers of one product | `--api-root`, `--uuid` | `TEA_PRODUCT` |
| `component` | name and identifiers of one component | `--api-root`, `--uuid` | `TEA_COMPONENT` |
| `releases` | the releases of a product or a component, paged | `--api-root`, `--product` or `--component` | `TEA_RELEASE_LIST` |
| `release` | one release with its components, its latest collection and its lifecycle (CLE) | `--api-root`, `--product-release` or `--component-release` | `TEA_RELEASE` |
| `download` | the artifacts of one collection version, verified, into a new directory | `--api-root`, `--product-release` or `--component-release`, `--collection-version`, `--dir` | `TEA_DOWNLOAD_MANIFEST` |

Flags of every verb: `--credential-stdin`, `--usehttp`, `--allow-this-host`, `--connect-timeout`, `--deadline`, `--max-document-bytes`, `--max-redirects`, `--max-string-bytes`, `--max-output-bytes`, and the global `-d true`. `resolve` adds `--useport`. `releases` adds `--page-token`, `--page-size`, `--max-pages`. `download` adds `--artifact-type`, `--exclude-artifact-type`, `--media-type`, `--max-artifact-bytes`, `--max-total-bytes`, `--max-files`.

`--api-root` is the TEA API root of the server: the `url` of a well-known endpoint, or a `rootUrl` from a `resolve` result. It must not carry a user name, a password, a query, a fragment or a trailing slash (a refused value is never repeated in the error). The API version is 1.0.0. Plain http needs `--usehttp`, and then applies to exactly that root; for `resolve`, `--usehttp` keeps its meaning of 11.1. Uuid flags are sent in canonical form.

```bash
rearm tea machine resolve --tei "tei://demo.rearmhq.com/uuid/b9f115fb-6688-4a3f-a262-3acc04e1fc9e"
rearm tea machine release --api-root https://demo.rearmhq.com/tea --product-release b9f115fb-6688-4a3f-a262-3acc04e1fc9e
rearm tea machine releases --api-root https://demo.rearmhq.com/tea --product <uuid> --max-pages 10
rearm tea machine download --api-root https://demo.rearmhq.com/tea --component-release <uuid> \
    --collection-version 1 --artifact-type BOM --dir /tmp/intake/run-1
```

### The JSON contract (format version 1)

- One object on one line; `formatVersion` (the number 1) and `kind` come first.
- **Every key is always present.** An optional value the server did not send is `null`; an empty list is `[]`.
- **Evolution.** Within format version 1 a CLI release may add keys and add values to the enumerations marked open; it changes or removes nothing. Ignore keys you do not know and treat an unknown value of an open enumeration as its generic case. Anything else is format version 2. Check that `formatVersion` is 1.
- **Server strings are data, exact**: not trimmed, normalised, cut or interpreted. A string longer than `--max-string-bytes` fails the run (reason `STRING_BYTES`); nothing is truncated.
- **The bytes are printable ASCII**: JSON escaping, and every character above 0x7E (and 0x7F) as `\uXXXX`, so no escape sequence, bidirectional control or line break reaches a log or a terminal. A JSON parser gets the strings back unchanged.
- **Dates are UTC instants**, RFC 3339 with `Z`.
- `preRelease` is `YES`, `NO` or `UNSTATED` (the server did not say).
- Uuids are lowercase. Values the server controls that are enumerations in TEA (`type`, `idType`, `algType`, `belongsTo`, update reason `type`, lifecycle event `type`) are passed through and are open.
- The `server` of every document but `TEA_RESOLUTION` is `{"rootUrl": <--api-root>, "apiVersion": "1.0.0"}`.

`TEA_RESOLUTION`: several results are the answer, in the server's order (ambiguity is data, exit code 0); a TEA 404 is `NOT_FOUND` (exit code 3); more than 32 results is `DISCOVERY_RESULTS`.

```json
{"formatVersion":1,"kind":"TEA_RESOLUTION","tei":"tei://tea.example.com/uuid/2a9e5c10-3d0b-4c6e-9d5e-0f1a2b3c4d5e","results":[{"productReleaseUuid":"2a9e5c10-3d0b-4c6e-9d5e-0f1a2b3c4d5e","discoveredAt":{"rootUrl":"https://tea.example.com/tea","apiVersion":"1.0.0"},"servers":[{"rootUrl":"https://tea.example.com/tea","versions":["1.0.0"],"priority":1}]}]}
```

`TEA_PRODUCT` (`TEA_COMPONENT` has the key `component` with the same shape):

```json
{"formatVersion":1,"kind":"TEA_PRODUCT","server":{"rootUrl":"https://tea.example.com/tea","apiVersion":"1.0.0"},"product":{"uuid":"11111111-1111-4111-8111-111111111111","name":"Example Product","identifiers":[{"idType":"PURL","idValue":"pkg:generic/example/product"}]}}
```

`TEA_RELEASE_LIST`, here a run with `--max-pages 1`:

```json
{"formatVersion":1,"kind":"TEA_RELEASE_LIST","server":{"rootUrl":"https://tea.example.com/tea","apiVersion":"1.0.0"},"parent":{"kind":"PRODUCT","uuid":"11111111-1111-4111-8111-111111111111"},"releases":[{"uuid":"2a9e5c10-3d0b-4c6e-9d5e-0f1a2b3c4d5e","version":"1.2.3","name":"Example Product","createdDate":"2026-01-10T08:00:00Z","releaseDate":"2026-01-15T00:00:00Z","preRelease":"NO","identifiers":[]}],"page":{"stoppedBy":"MAX_PAGES","nextPageToken":"b3BhcXVl","pagesRead":1}}
```

- `page.stoppedBy` is `END` (no further page; `nextPageToken` is `null`), `MAX_PAGES` or `OUTPUT_BYTES`. With the last two, run the verb again with `--page-token=<nextPageToken>` (always the `=` form: a token may start with `-`); the whole list is the concatenation of the runs until `END`. `pagesRead` is the number of pages in this document.
- A page is added whole or not at all: when it would take the document past `--max-output-bytes` it is dropped and `nextPageToken` fetches it again. When the first page of a run alone is too large the run fails (`OUTPUT_BYTES`).
- `--page-size` 1 to 100 (default 100), `--max-pages` 1 to 1000 (default 50). An empty `--page-token` is a usage error. A server token that could not be passed back (a control character) is `INVALID_RESPONSE`.

`TEA_RELEASE`: two requests, the release with its latest collection and its lifecycle.

```json
{"formatVersion":1,"kind":"TEA_RELEASE","server":{"rootUrl":"https://tea.example.com/tea","apiVersion":"1.0.0"},"release":{"kind":"PRODUCT_RELEASE","uuid":"2a9e5c10-3d0b-4c6e-9d5e-0f1a2b3c4d5e","parentUuid":"11111111-1111-4111-8111-111111111111","name":"Example Product","version":"1.2.3","createdDate":"2026-01-10T08:00:00Z","releaseDate":"2026-01-15T00:00:00Z","preRelease":"NO","identifiers":[],"components":[{"componentUuid":"33333333-3333-4333-8333-333333333333","releaseUuid":"44444444-4444-4444-8444-444444444444"},{"componentUuid":"55555555-5555-4555-8555-555555555555","releaseUuid":null}]},"collection":{"uuid":"2a9e5c10-3d0b-4c6e-9d5e-0f1a2b3c4d5e","version":2,"createdDate":"2026-01-16T10:00:00Z","belongsTo":"PRODUCT_RELEASE","updateReason":{"type":"VEX_UPDATED","comment":"new statement"},"artifacts":[{"uuid":"66666666-6666-4666-8666-666666666661","version":1,"name":"Product SBOM","type":"BOM","createdDate":"2026-01-10T08:05:00Z","formats":[{"mediaType":"application/vnd.cyclonedx+json","description":"CycloneDX JSON","externalUrl":null,"signatureUrl":null,"checksums":[{"algType":"SHA-256","algValue":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}]}]}]},"cle":{"status":"READ","events":[],"supportDefinitions":[]}}
```

- `release.kind` is `PRODUCT_RELEASE` or `COMPONENT_RELEASE`; `components` are a product release's refs in order (`releaseUuid` is `null` for an unpinned ref; `[]` for a component release). Refs are not followed.
- `externalUrl` is the format's `url` (`null` when the TEA server serves the bytes itself); `signatureUrl` is data, nothing downloads it.
- `cle.status` is `READ`, or `NOT_AVAILABLE` when the server answers the lifecycle request with `NOT_IMPLEMENTED` or `OBJECT_UNKNOWN`; any other lifecycle failure fails the run. Events carry every field of a TEA lifecycle event; `versions` entries are `{"version", "range"}`, `supportDefinitions` entries `{"id", "description", "url"}`.

### download

`--collection-version` is required: the caller reads the release, chooses a version, and gets exactly that immutable version. The command:

1. checks the flags: the parent of `--dir` must be an existing directory and nothing may exist at `--dir` (a symbolic link counts);
2. reads the collection version (unknown: `NOT_FOUND`, exit code 3);
3. lists one row per format of every artifact, numbered from 1, and applies the selection; more selected rows than `--max-files` (`FILE_COUNT`), or a manifest that could exceed `--max-output-bytes` (`OUTPUT_BYTES`), fails the run before anything is fetched or created;
4. creates `--dir` with mode 0700 (it never writes into a directory it did not create);
5. fetches the selected rows one at a time into `<number>.bin.part` (exclusive create, mode 0600), verifying while streaming, and renames a verified file to `<number>.bin`; a failed file is removed;
6. writes the manifest: exit code 0 when every selected row is verified (also when nothing was selected), else 7.

**File names are the CLI's** (`000001.bin`, the row number); nothing a server sends (artifact name, media type, URL, `Content-Disposition`) takes part in a path.

**Selection.** `--artifact-type T` (repeatable): only these TEA types. `--exclude-artifact-type T` (repeatable): everything else, including types the specification does not know. The two cannot be combined, and a value must be one of the eleven TEA 1.0.0 artifact types. `--media-type M` (repeatable): of those artifacts, only the formats with this type/subtype (case-insensitive, parameters ignored).

**What VERIFIED means.** The bytes in `fileName` matched every checksum the server listed for that format that the client implements (SHA-256, SHA-384, SHA-512, SHA3, BLAKE2b, and SHA-1 beside them), at least one of them stronger than SHA-1; `verifiedAlgorithms` names them. There is no "downloaded, not verified" state: a format with no checksum, SHA-1 only, or only BLAKE3 or unknown algorithms is `NO_USABLE_CHECKSUM` and is not requested; a malformed checksum value is `INVALID_RESPONSE`, not requested; a mismatch is `CHECKSUM_MISMATCH` and the bytes are removed. Verification is integrity against the same server's checksums, not authenticity.

**Credentials.** A server-hosted format is requested at `<root>/v1.0.0/artifact/<uuid>/<version>/download?mediaType=...` and carries the credential when that URL is under `REARM_TEA_AUTH_ROOT`. A format with an external `url` is requested without a credential on every hop, and a redirect that leaves the root loses the credential.

```json
{"formatVersion":1,"kind":"TEA_DOWNLOAD_MANIFEST","server":{"rootUrl":"https://tea.example.com/tea","apiVersion":"1.0.0"},"release":{"kind":"COMPONENT_RELEASE","uuid":"44444444-4444-4444-8444-444444444444"},"collection":{"uuid":"44444444-4444-4444-8444-444444444444","version":2,"createdDate":"2026-01-16T10:00:00Z","belongsTo":"COMPONENT_RELEASE","updateReason":{"type":"ARTIFACT_ADDED","comment":null}},"files":[{"number":1,"artifactUuid":"66666666-6666-4666-8666-666666666662","artifactVersion":2,"artifactName":"Build SBOM","artifactType":"BOM","artifactCreatedDate":"2026-01-10T08:05:00Z","mediaType":"application/vnd.cyclonedx+json","description":"CycloneDX JSON","source":"EXTERNAL","sourceUrl":"https://cdn.example.com/c.json","signatureUrl":null,"checksums":[{"algType":"SHA-256","algValue":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}],"status":"VERIFIED","fileName":"000001.bin","sizeBytes":4,"responseMediaType":"application/json","verifiedAlgorithms":["SHA-256"],"reason":null,"message":null,"address":null,"addressClass":null},{"number":2,"artifactUuid":"66666666-6666-4666-8666-666666666662","artifactVersion":2,"artifactName":"Build SBOM","artifactType":"BOM","artifactCreatedDate":"2026-01-10T08:05:00Z","mediaType":"application/vnd.cyclonedx+xml","description":null,"source":"SERVER","sourceUrl":"https://tea.example.com/tea/v1.0.0/artifact/66666666-6666-4666-8666-666666666662/2/download?mediaType=application%2Fvnd.cyclonedx%2Bxml","signatureUrl":null,"checksums":[],"status":"FAILED","fileName":null,"sizeBytes":null,"responseMediaType":null,"verifiedAlgorithms":[],"reason":"NO_USABLE_CHECKSUM","message":"downloading artifact 66666666-6666-4666-8666-666666666662 version 2: tea: no usable checksum: ...","address":null,"addressClass":null}],"summary":{"verified":1,"failed":1,"notAttempted":0,"notSelected":0,"bytesReceived":4},"stoppedBy":"END"}
```

- `files[].status` is `VERIFIED`, `FAILED`, `NOT_ATTEMPTED` (selected, but the run stopped before it) or `NOT_SELECTED`. `fileName`, `sizeBytes`, `responseMediaType` and `verifiedAlgorithms` are set only for `VERIFIED`.
- `source` is `SERVER` or `EXTERNAL`; `sourceUrl` is the URL requested first, as the server wrote it, query string included (it may be the supplier's signed link: it is data).
- `reason` is a reason of the table below and `message` its text (ASCII, at most 1024 bytes); `address` and `addressClass` are set for `ADDRESS_REFUSED`.
- `stoppedBy` is `END`, `TOTAL_BYTES` or `DEADLINE`. A run-wide bound hit while downloading keeps the verified files: the row being fetched is `FAILED` with that reason, the rest are `NOT_ATTEMPTED` with it, and the exit code is 7.
- `summary.bytesReceived` counts artifact bytes received, kept or not.
- A local file system failure (`LOCAL_IO`) or an interrupt ends the run with exit code 1 and no manifest.

**What the caller does:** create the parent of `--dir`, remove `--dir` after every run, and treat a run without a manifest on stdout as nothing downloaded (discard the directory whole). Exit code 7 means: read the manifest.

### Limits

Every limit is a flag with a default; none can be switched off (no range includes 0); a value outside its range is a usage error.

| Flag | Default | Range | Reason when hit |
| --- | --- | --- | --- |
| `--max-document-bytes` | 16 MiB | 1 KiB to 256 MiB | `DOCUMENT_BYTES` (one JSON answer; the well-known document 64 KiB and the discovery answer 1 MiB are fixed) |
| `--max-redirects` | 5 | 1 to 20 | `REDIRECT_LIMIT` (exit code 5) |
| `--page-size`, `--max-pages` | 100, 50 | 1 to 100, 1 to 1000 | not an error: `page.stoppedBy` is `MAX_PAGES` |
| `--max-string-bytes` | 8192 | 256 to 1 MiB | `STRING_BYTES` (the message names the field, never its value) |
| `--max-output-bytes` | 32 MiB | 64 KiB to 1 GiB | `OUTPUT_BYTES` (for `releases`: `page.stoppedBy`) |
| `--max-artifact-bytes` | 100 MiB | 1 to 16 GiB | row `ARTIFACT_BYTES` |
| `--max-total-bytes` | 1 GiB | 1 to 64 GiB | rows `TOTAL_BYTES`, `stoppedBy`; it bounds the bytes received, kept or not; when nothing of it is left the next file is not requested |
| `--max-files` | 200 | 1 to 100000 | `FILE_COUNT`, before anything is fetched |
| `--connect-timeout` | 5s | 1s to 60s | `CONNECT_FAILED` (exit code 10); covers name resolution and every address tried |
| `--deadline` | 10m | 1s to 24h | `DEADLINE`; the whole run, the token exchange included |
| (fixed) | 15 s per discovery attempt, 30 s per call, 30 s without a byte on a download | | `REQUEST_TIMEOUT` |

A limit counts decoded bytes: a gzip answer cannot exceed it, and any other content coding on a download is `INVALID_RESPONSE`.

### Exit codes and the error object

| Code | Reasons |
| --- | --- |
| 0 | success |
| 1 | `FAILED`, `SERVER_ERROR`, `INVALID_RESPONSE`, `TLS_CERTIFICATE`, `LOCAL_IO`, `INTERRUPTED` |
| 2 | `USAGE` |
| 3 | `NOT_FOUND` |
| 5 | `URL_REFUSED`, `REDIRECT_LIMIT` |
| 6 | `AUTH` |
| 7 | `download` only: the manifest is on stdout and a selected file is not verified |
| 8 | `ADDRESS_REFUSED` |
| 9 | `DEADLINE`, `REQUEST_TIMEOUT`, `DOCUMENT_BYTES`, `STRING_BYTES`, `OUTPUT_BYTES`, `FILE_COUNT`, `DISCOVERY_RESULTS` |
| 10 | `CONNECT_FAILED` |

Code 4 is never returned: several results are data. `reason` is an open enumeration; a manifest row can also carry `NO_USABLE_CHECKSUM`, `CHECKSUM_MISMATCH`, `NO_ACCEPTABLE_FORMAT`, `ARTIFACT_BYTES` and `TOTAL_BYTES`.

```json
{"formatVersion":1,"kind":"TEA_ERROR","exitCode":8,"reason":"ADDRESS_REFUSED","message":"refused to connect to 127.0.0.1 (loopback) for https://tea.example.com/tea/v1.0.0/product/11111111-1111-4111-8111-111111111111","url":"https://tea.example.com/tea/v1.0.0/product/11111111-1111-4111-8111-111111111111","httpStatus":null,"address":"127.0.0.1","addressClass":"LOOPBACK","phase":null,"hint":"this host is refused as a target by default; --allow-this-host lifts it for local testing","ignoredProxyVariables":[]}
```

- `message` is printable and at most 2048 characters; `url` is the failing request without query or user name; `httpStatus` the status of an HTTP error.
- `address` and `addressClass` for `ADDRESS_REFUSED`; `phase` (`RESOLVE` or `CONNECT`) for `CONNECT_FAILED`.
- `CONNECT_FAILED` covers name resolution failures, refused and unreachable connections and connect timeouts alike: a connection blocked by a network policy looks like either. Its `hint` is always `the connection could not be made; the network policy of the deployment may block this address`, and `ignoredProxyVariables` names the proxy variables that are set and were not used.
- The object never carries a credential, a key id, a query string or a response body.

### The address refusal

The machine mode refuses, at connect time and on the resolved address of every connection (the well-known document, discovery, API calls, the token request, every redirect, server-hosted and external artifact URLs), these targets. The address is taken without its zone and with an IPv4-mapped IPv6 form turned into its IPv4 address; the first matching class wins:

| Class | Addresses | `--allow-this-host` |
| --- | --- | --- |
| `CLOUD_METADATA` | `169.254.169.254`, `100.100.100.200`, `168.63.129.16`, `fd00:ec2::254`, `fd00:ec2::23`, `fd20:ce::254`, `fd00:c1::a9fe:a9fe` | still refused |
| `LINK_LOCAL` | `169.254.0.0/16`, `fe80::/10` | still refused |
| `UNSPECIFIED` | `0.0.0.0/8`, `::` | still refused |
| `LOOPBACK` | `127.0.0.0/8`, `::1` | allowed |
| `OWN_ADDRESS` | every address of a network interface of this host, read at every connection | allowed |

- A name is resolved once per connection, every address is classified, a refused address is never dialled, and the connection is made to the classified address itself; when every address is refused the run fails with `ADDRESS_REFUSED` (exit code 8). A socket-level check refuses a refused address once more just before connecting. When the interfaces of the host cannot be listed, no connection is made.
- **`--allow-this-host`** (command line only) is for local testing: it allows loopback and this host's own addresses, nothing else. Production callers do not pass it.
- Everything else is allowed: public addresses, and private addresses that are not this host's. Restricting private ranges is the job of the deployment's network policy (the Helm charts' egress rules), not of the CLI.
- NAT64, 6to4, Teredo and IPv4-compatible forms that embed a refused IPv4 address are not decoded.
- The interactive commands (11.1, 11.2) are not restricted.

### Proxy and TLS

- `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY` (and lowercase) are **ignored** by the machine mode: through a proxy the address check would not see the real target. A cluster whose only way out is an HTTP proxy cannot use the machine mode.
- TLS uses the system trust store (`SSL_CERT_FILE` and `SSL_CERT_DIR` add a private CA); verification cannot be disabled. HTTP/1.1 only; response headers are bounded to 64 KiB.

### Trace

`-d true` writes trace lines on stderr: `TEA request [<stage>]: <method> <url>`, `TEA response [<stage>]: <status> <url>`, `TEA credential: <kind> for <root>`, `TEA address refused: <address> <CLASS> for <host>`, and discovery attempts and backoffs. URLs carry scheme, host, port and path only; no header, query string, token or key is printed.
