# Freechains CLI Assumptions and Compatibility

The Freechains Go client ([`freechains/`](../freechains/)) invokes the upstream `freechains` v0.20 CLI via `os/exec`. Because several CLI output details are not formally defined in the upstream documentation, the client relies on the assumptions documented below.

---

## Assumptions Matrix

| Assumption | Expected CLI Format | Source Location |
| :--- | :--- | :--- |
| Global flag ordering | Global options `--root=<dir>` and `--now=<secs>` must precede subcommands. | [`freechains/args.go`](../freechains/args.go) (`prependGlobals`) |
| Action ID output format | Commands `post`, `like`, `dislike`, `revoke`, `unrevoke` emit the 40-character hex ID on the final non-empty line of stdout. | [`freechains/parse.go`](../freechains/parse.go) (`ParseActionID`) |
| Chain ID output format | Commands `chains add <alias> init\|clone` emit the chain identifier (`#<40hex>`) on the last non-empty line of stdout. | [`freechains/parse.go`](../freechains/parse.go) (`ParseChainID`) |
| Consensus order listing | Command `chain <alias> list order` emits one 40-character hex ID per line; revoked actions are wrapped in tildes (`~<id>~`). | [`freechains/parse.go`](../freechains/parse.go) (`ParseOrder`) |
| Action metadata format | Command `chain <alias> get metadata <id>` prints action ID on line 1 followed by `<key> <value>` lines (`action`, `sign`, `author`, `n`, etc.). | [`freechains/parse.go`](../freechains/parse.go) (`ParseMetadata`) |
| Reputation scalar values | Commands `reps author <pub>` and `reps action <id>` emit a single base-10 integer on the first line of stdout. | [`freechains/parse.go`](../freechains/parse.go) (`ParseInt64`) |
| Reputation table listings | Commands `reps authors`, `reps actions`, `reps revokes` emit space-delimited records: `<pub> <reps>`, `<id> <reps>`, `<id> <author_reps> <others_reps>`. | [`freechains/parse.go`](../freechains/parse.go) (`ParseRepsAuthors`, `ParseRepsActions`, `ParseRepsRevokes`) |
| Error detection | Substring rules in stderr identify common failure modes (`"insufficient reputation"`, `"unknown post"`, `"revoked payload"`). | [`freechains/errors.go`](../freechains/errors.go) (`DefaultErrorRules`) |

---

## Updating Parsers for New CLI Versions

If upstream Freechains CLI output formats change:

1. **Capture CLI outputs**: Record actual command outputs from the target `freechains` binary and add them to the test fixtures under [`freechains/testdata/`](../freechains/testdata/).
2. **Update parsers**: Adjust string parsing in [`freechains/parse.go`](../freechains/parse.go) or error patterns in [`freechains/errors.go`](../freechains/errors.go).
3. **Execute unit tests**: Verify parser compliance by running the test suite:
   ```bash
   go test -v ./freechains
   ```
