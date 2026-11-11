# Transparency Exchange API (TEA) Commands

Base Command: `tea`

The `tea` commands are a client of the [Transparency Exchange API (TEA)](https://github.com/CycloneDX/transparency-exchange-api) version 1.0.0, built on the Go library [tea-client-go](https://github.com/oolong-project/tea-client-go). They resolve a Transparency Exchange Identifier (TEI) and read the product release it names. They print artifact URLs; they download nothing.

## Table of Contents

1. [Transparency Exchange API (TEA) Discovery](#111-transparency-exchange-api-tea-discovery)
2. [Complete TEA Flow - Product and Component Details](#112-complete-tea-flow---product-and-component-details)
3. [Authentication](#113-authentication)
4. [Exit codes](#114-exit-codes)
5. [Changes from earlier versions](#115-changes-from-earlier-versions)

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
| 7 | `full_tea_flow` printed the product release but at least one component could not be read |

Every failure is one line on stderr starting with `Error:`.

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
