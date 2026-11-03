# Compatibility with the Node.js version

This program is the native version of the `smstunnel` command on npm (the Node.js version: its `src/` and tests are the
reference, described in its `CLI-SPEC.md`). For the same version number it has the same commands,
arguments, options, environment variables, output (human and JSON, byte for byte), exit codes, stdin handling and saved
login. This file says how that is done, how it is checked, and where the two differ.

## How it is built

- `internal/cli`: the commands, ported function by function from the Node.js version's `src/`.
- `internal/js`: the JavaScript behaviour that the output depends on, reproduced in Go:
  - strings as JavaScript sees them: UTF-16 lengths (table widths, the 300-character limit on error bodies),
    `trim()`, `\s`, `toLowerCase()` (with `İ` → `i̇` and the final `ς`), lone surrogates from JSON, and the WHATWG
    UTF-8 decoder (one U+FFFD per bad sequence) for stdin, files and answers;
  - `JSON.parse` and `JSON.stringify` (member order with array-index keys first, `Number#toString`, escapes);
  - `Date.parse`: a port of V8's date parser, so `--since 2026-11-03 08:00` (and every other form V8 takes) means what
    it means in Node.js, plus ICU's rule for local times in a daylight-saving gap or overlap;
  - the WHATWG URL rules behind `--api-base` (IPv4 forms such as `127.1`, IPv6, `https:host`, dot segments,
    percent-encoding), and `URLSearchParams` for the query.
- HTTP goes through [smstunnel-go](https://github.com/narcisbodea/smstunnel-go) 1.0.0's `Client`. The Node.js version
  prints SMSTunnel's answers as they came and decides from their raw JSON, and sends the country code as typed, so the
  CLI gives the library its own `http.Client` (`WithHTTPClient`): the transport sends exactly the request the Node.js
  version sends (URL and query order, JSON body, headers) and keeps each answer as it came. It also connects the way
  Node.js does: HTTP/1.1, no proxy, no redirects, 30 seconds of silence is a timeout, answers over 5 MiB are dropped,
  and it records whether the request was written completely (for "the text may have gone out").

## How it is checked

| Check | What | Result (3 Nov 2026) |
|---|---|---|
| `TestGolden` | `test/parity/golden.mjs` runs the Node.js version's own functions over a corpus and stores the answers in `internal/cli/testdata/golden`; the Go functions must answer the same, in two time zones | 2,657 cases × 2 zones, identical |
| `go test ./...` | the Node.js version's unit and mock end-to-end tests, ported; real Ctrl+C on the built binary | all pass on Linux and Windows |
| `test/parity/parity.mjs` | the same scenarios through both programs, against the Node.js version's mock API (exit code, stdout, stderr and the requests the API received) and against a local SMSTunnel API; one login used by the other program (file store, and Windows Credential Manager) | Windows 11: 184 of 184 identical (160 mock, 20 local API, 4 shared logins); Linux (Debian, Node.js 22.13.1): 182 of 182 |

`parity.mjs` normalises only ids, times, ports and temporary paths (and, against the shared local API, the usage
counts that other keys of the same account change). Every run gets `SMSTUNNEL_API_BASE` pointing at a closed local
port unless a scenario sets its own address, so no scenario can reach smstunnel.io.

## Where the specification and the Node.js code disagree

The Node.js code wins; this version does what the code does.

1. A 2xx answer whose body is a JSON array is accepted (the spec says "not a JSON object" is an invalid answer).
2. `config.json` holding a JSON array is kept as it is, which reads like `{}` (the spec says "not an object is {}").
3. `--since` with a span so long that the date is invalid (`999999999999999d`): the spec says "now minus that"; the
   code ends with exit 1, `Unexpected error: RangeError: Invalid time value` when it builds the request (after the key
   and `--phone` checks). This version does the same (without Node.js's stack trace).
4. `--since` dates: the spec describes ISO dates; the code takes everything V8's `Date.parse` takes after the
   `YYYY-MM-DD` start (`2026-11-03 08:00`, `2026-11-03 8:00 PM`, `2026-11-03 EST`, and `2026-11-03junk`, which V8 reads
   as 11 June: "jun"). The V8 parser is ported as it is.
5. `logout` when `config.json` names a system store and `SMSTUNNEL_KEYCHAIN_SERVICE` is invalid: the spec says errors
   are ignored; the code ends with exit 2 (the service name is checked before anything is removed).
6. Ctrl+C during a status request of `--wait`: the spec says the next sleep returns "interrupted"; the code first
   checks the deadline, so when the time is up it ends "still-waiting" (exit 9).
7. `Idempotent-Replayed` sent twice: Node.js joins the values (`true, true`), which is not `true`.
8. A key in `SMSTUNNEL_API_KEY` with characters Node.js can't put in a header: no request is made, exit 7
   "Could not reach SMSTunnel … (ERR_INVALID_CHAR)".

## Differences that remain

- Bugs of the Node.js version that are not reproduced: commands named like JavaScript object properties
  (`smstunnel constructor`, `smstunnel help toString`) are unknown commands here; `--interval` above 24.8 days (setTimeout
  overflow) waits the interval here instead of polling every millisecond.
- Errors that are bugs ("Unexpected error"): Node.js prints a stack trace, this version one line. Answers of an
  unexpected shape (a `devices` that is not a list, say) end with exit 1 in both, with other words.
- Certificates: Node.js trusts its own copy of the Mozilla roots, this version the system's store (on Linux,
  `ca-certificates`, which the packages depend on). A company root installed in Windows or macOS is trusted here and
  not by Node.js.
- Messages for file errors (`Could not read <file>: …`) use Node.js's form for the common cases (EACCES, ENOENT,
  EISDIR, ENOTDIR) only.
- `--api-base` with a domain outside ASCII uses a light IDNA mapping (lower case, full-width forms, Punycode), not the
  full UTS 46 tables.
- Local times of dates before the current Windows time zone rules (Windows only: Go uses the registry's rules, ICU has
  the full history).
- `Array#sort` with unreadable `receivedAt` values (NaN): both sorts are stable, but V8's TimSort may order such texts
  differently.
- Windows: Ctrl+Break while waiting is taken as Ctrl+C here; Node.js ends.
- The hidden key prompt when the terminal closes before Enter: Node.js waits forever, this version takes what was
  typed.
- Not user-visible: a fresh connection per request (Node.js keeps them alive); the `User-Agent` is
  `smstunnel-cli/<version> (go<version>; <os>) smstunnel-go/<version>`.

## What smstunnel-go would need so the CLI could rely on it alone

1. The raw answer of every call (`ListPhones` and `ListReceivedTexts` have no `Raw`), and the headers of successful
   answers.
2. A base URL that is the API root, used as given (it always inserts `api/v1/`, so `--api-base https://host/custom`
   can't be expressed).
3. The country code sent as given, or a documented normalisation: `WithCountryCode("40")` sends `"+40"`, a different
   body (and, with an Idempotency-Key, a 422 for a retry made by another tool).
4. A way to set the whole User-Agent (only a suffix after `smstunnel-go/…` now).
5. On `ConnectionError`: whether the request was written completely, and a category (DNS, refused, TLS, reset,
   timeout, too large), so a caller can tell "could not reach" from "may have gone out".
6. `APIError.Message` as SMSTunnel said it: plain-text bodies are stripped of tags, and the fallback is
   "SMSTunnel returned error: 404 Not Found" where the CLI needs `HTTP 404`; `success: false` in a 2xx answer can't be
   told from other errors.
7. `RetryAfter` as SMSTunnel sent it (seconds as an integer; HTTP dates in every form): the library caps it at 24 h
   and takes decimals.
8. No refusal before sending of what the API itself would judge: keys with spaces or non-ASCII characters, texts that
   are blank only by Go's `unicode.IsSpace` (U+0085).
9. Lenient decoding: one field in an unexpected form (`lastSeenAt` not RFC 3339, `isOnline` not a boolean) fails the
   whole call.
10. `GetMessageStatus` without a `data` member decodes the whole answer as the status (the CLI must say "SMSTunnel sent
    back no status"); `SendResult.MessageID` prefers `data.messageId` (Node.js: the top-level one first).
11. The received-texts query in the API's order, and `since` for years outside 0–9999 (cosmetic).

The CLI works around each of these in `internal/cli/api.go`. `ReceivedTextsCursor` is not used: `--watch` keeps the
Node.js version's loop (one page per poll, a 5-minute overlap, up to 5000 ids remembered).
