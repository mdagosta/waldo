# Lineage design B′ (#41, #42)

Draft 7, 2026-11-05. Seven drafts and twelve reviews, three of them run
against main's binary. Draft 7 fills in what an implementer would otherwise
decide, and makes the merge-base git's. The user's calls:

- nothing is ever "declared";
- merges on a path are always recomputed;
- local work in progress may have gaps, and the export is where history must
  be complete;
- identity and signing are deferred until we know how people coordinate;
- intermediate weights are not kept anywhere: a merge is recomputed when it
  is made, and later checked by its documents.

"History of this doc" at the end lists what each draft changed.

## Goals

1. **No new fields in records main hashes, so no maintainer decision.** Main
   reads every model the new binary makes, including merges.
2. **A model with no lineage works exactly as before.** That covers every
   model main made.
3. **Lineage is tamper-evident.** Every document in a history is exact bytes
   whose SHA-256 is pinned by a record main already hashes, or by a document
   pinned that way. Every link is verified or a gap; there is no third state.
4. **Lineage can't be removed silently.** A missing document is always a
   visible, structured gap, and merge refuses gaps on its path.

What hashes can't do: prove that training happened as a run BOM says.
Training can't be reproduced, so a run BOM is always its author's word. Hashes
prove a history unaltered and consistent. Merges are the exception, because
they are recomputed. Judging who to believe (identity, signatures) is deferred;
see "Deferred".

This is main's own guarantee, unchanged: "Hashes prove identity and
integrity. They do not prove legal rights, safe model behavior, truthful
declarations by an untrusted process, or regulatory compliance"
(docs/VISION.md), and WALDO "records attributable facts and verifies artifact
identity" (README.md). A release's history is a report by whoever exported it:
their own runs, and what their store held about the models before them.

## The compatibility rule

Main reads records with plain `json.Unmarshal` and checks them by re-hashing
its own struct. `hashJSON` is the SHA-256 of compact `json.Marshal`
(records.go:778, :794 on main). So a new field in a record main reads is
unusable:

- **Pinned records** (PLAN, ORIGIN-BOM, RUN-BOM): main drops the field, the
  hash no longer matches, and main refuses the model. Proven with main's
  binary: one such model also breaks main's `model list` and `lookaside` for
  the whole store, and main's `model rm` can't remove it.
- **Unpinned records** (MODEL.json; MODEL-BOM, compared with
  `reflect.DeepEqual`): main accepts the field and silently drops it on its
  next rewrite.

Safe, all proven against main's binary:

- new files beside the records, which every main command ignores;
- new values in existing string fields;
- new `source_artifacts` entries, which main never requires on disk.

`artifacts` entries are different: they change the origin ID and `Inspect`
requires them, so B′ adds none.

Main's contract says otherwise: "Readers of schema 1 must accept unknown
fields" (docs/OPENWALDO-BOM.md:95, ADR 0009). Main's re-hash breaks that, and
it is the real cause of the one-way upgrade in design A. The PR should say so.

## What main's records pin

| Pin (on main) | Names |
|---|---|
| model ID | the model's PLAN |
| `plan.parent.model_id`, `run_bom_sha256` | the parent's PLAN and the pinned RUN-BOM |
| `plan.parent.artifact.sha256` | the starting weights (a leaf) |
| `plan.origin_bom_sha256` | ORIGIN-BOM |
| `origin.source_artifacts[source-bom]` | the release `BOM.json`, raw bytes |
| `origin.source_artifacts[source-weights]` | the release's weights file, raw bytes |
| `plan.architecture_sha256` | the architecture (BPE: the full tokenizer) |
| `run_bom.corpus_bom_sha256` | the corpus BOM inside that run BOM |
| release `BOM.json` `model_id`, `source_bom_sha256` | the exporter's PLAN and normalized MODEL-BOM |

Not pinned by any hash on disk:

- **A local model's own run BOMs.** They are pinned only by `RunPin.BOMSHA256`
  in MODEL.json, which nothing hashes. A child pins one of them; a release's
  MODEL-BOM pins all of them.
- **The run a run BOM started from.** `run_bom.initialization` names it by run
  ID and starting weights, with no run BOM hash (training/backend.go:351).

A model ID names a recipe (name, architecture, base), not an instance. Lookups
go by hash.

## Documents

A history is a set of documents, each held as bytes under its SHA-256.

- **Canonical bytes.** A JSON record is held as compact `json.Marshal` of its
  strict decode. The held bytes must equal that re-marshal and hash to the pin.
  Copying the file from disk is wrong in two ways:
  - files on disk are indented;
  - main accepts files carrying extra fields whose pin excludes them.

- **Raw bytes.** A release `BOM.json` is pinned as raw bytes and held that
  way. Before any link in it is followed, it is decoded strictly, and
  duplicate or case-variant keys are refused. `MERGE.json` is written
  canonical, so one merge has one encoding and one key.
- **Opaque.** A document whose bytes hash to its pin but which this binary
  can't decode strictly, or which decodes but isn't canonical, is held as
  opaque. Examples: a newer WALDO's document with a field this binary lacks,
  or a release `BOM.json` in UTF-16 or with a byte-order mark. Its own links
  are gaps. It is never an error.
- **Altered** when bytes don't hash to their pin, or a document's `kind` and
  `subject` don't match the type its link expects. An altered document is
  never followed. Merge refuses one on its path; everywhere else it is a gap
  with a warning (see the table under "Existing commands").
- **Shared parts held once.** A plan is held without its architecture, and a
  run BOM without its corpus BOM. Each part is held under its own pin and
  spliced back at the byte level. The part is the `"architecture":` or
  `"corpus_bom":` value in the compact bytes, which occurs exactly once, at
  the top level.
  - The held piece has that value replaced by the JSON string of the part's
    SHA-256. Main's value is always an object, so a string means stripped.
    The reader splices only a document reached by a plan or run BOM link
    whose held bytes don't hash to the pin.
  - Canonical checks apply to the reassembled whole, not to the stripped
    piece.
  - Splicing reproduced every pin on main-made models.

## Links

Followed, each checked against its pin:

- model ID → PLAN; parent `model_id` → PLAN; parent `run_bom_sha256` → RUN-BOM
- `origin_bom_sha256` → ORIGIN-BOM
- `source-bom` → release `BOM.json` → its `LINEAGE.json` artifact,
  `model_id` → PLAN, `source_bom_sha256` → MODEL-BOM → every run BOM
- `source-merge` → MERGE.json → each parent's RUN-BOM or ORIGIN-BOM (a run
  BOM's `model_id` then reaches the parent's PLAN)
- A run BOM's `initialization` is routed by `source_type` and `source_id`,
  as main writes it (build.go:1204, :1216, :1250):
  - `origin`: `source_id` is the ORIGIN-BOM hash; followed as above.
  - `run` with `source_id` equal to `source_run_id`: an earlier run of the
    same model (below).
  - `run` with `source_id` a model ID: the parent named by the plan's
    `parent` pin, which is already followed. Its weights must equal the
    pin's artifact.
- **Inside one model,** `initialization` names the earlier run by ID and
  weights only. A run BOM doesn't record its own output weights, so this link
  needs an anchor: the model's MODEL-BOM, which lists each run's ID, BOM hash
  and output weights.
  - While the model is local, the anchor is built from its MODEL.json.
  - When a child is made, it holds its parent's MODEL-BOM as a document.
  - In a release, the exporter's MODEL-BOM is pinned by `source_bom_sha256`.
    Its parents' MODEL-BOMs ride in the release `LINEAGE.json`, which
    `BOM.json` lists by hash.
  - Anchors are found through the `anchors` index in `LINEAGE.json` (model
    ID → MODEL-BOM hashes), since no pin names a parent's MODEL-BOM.

  Rules:
  - The anchor must list exactly one run with that ID, with its `role:
    weights` artifact equal to the initialization's weights (never a
    checkpoint). Weights match by SHA-256, never by path.
  - That run's BOM must name the same `model_id`, and an `ordinal` lower than
    the run it starts. This rules out cycles; the walker also keeps a set of
    visited documents and treats a revisit as a gap.
  - Several anchors for one model ID (snapshots taken before and after the
    model trained more) must agree on every run ID they share: the same BOM
    hash and weights. If they disagree, every run they disagree on is a gap.
  - A missing run, a duplicate, or a wrong ordinal is a gap. Dropping run
    pins from MODEL.json before an export therefore shows, rather than hiding
    history.

  A parent's anchor is not pinned by the parent. It is what the child's
  maker's store held about the parent: a report, as a run BOM is. It is
  consistent and unaltered, and `sources` records where it came from. Holding
  it is what lets a parent be deleted without losing its earlier runs. Who to
  believe is the deferred identity question.

Leaves: weights hashes, tokenizer corpus hashes, and other source-file hashes.

**Weights across export and pull.** Export renames tensors and rewrites the
safetensors metadata, and pull re-lays the payload. The tensors are
byte-identical throughout (measured on a v3 model). So:

- **pulled weights → release weights:** the pull's `source-weights` pin.
- **release weights → exporter's weights:** undoing export's renames and
  metadata rebuilds the exporter's file byte for byte (measured).
  - The rebuilt file must hash to the `role: weights` artifact of the run
    `BOM.json` names in `run_id`, which must be the MODEL-BOM's
    `current_run_id`. For an untrained origin, it must hash to the origin's
    weights. Any other run's weights, or a checkpoint, don't count.
  - Exporting the rebuilt file again must reproduce the release file's
    `source-weights` hash. So a header with duplicate or case-variant tensor
    names, which other readers could read differently, fails.
  - The safetensors metadata's `run_id`, which training writes and export
    keeps, must equal `BOM.json` `run_id`, and its `architecture_sha256` must
    equal the pinned MODEL-BOM's. `BOM.json` has no architecture field
    (modelexport/huggingface.go:56). For an untrained origin, including a
    merge, both keys must be absent.
  - A release with more than one weights file has no round trip and is a
    gap.
  - The pull runs this check, and keeps the release weights' header (8.4KB,
    unlisted) so it can be rerun later from the pulled file. The header is
    kept raw: the 8-byte length prefix and the JSON header with its padding.
  - Re-exporting a pulled model needs one reader rule: `normalized_by:
    openwaldo` restores `source_format: huggingface`.
  - Writers of weights (training, merge) must not set `source_format` or
    `export_format` metadata. If they do, the step is a gap.

## Gaps and ends

- **Gap:** a followed link whose document isn't held, an opaque document's
  links, an unresolved initialization, or a failed weights round trip.
  - Gaps are always derived by walking the history.
  - A stored list is only a cache, written as `{"gap": "<sha256>", "link":
    "run-bom"}`. `gap` is the hash the link expected; for an initialization
    or a round trip, the weights hash.
  - Link names: `plan`, `run-bom`, `origin-bom`, `architecture`,
    `corpus-bom`, `model-bom`, `release-bom`, `release-lineage`, `merge`,
    `initialization`, `weights-roundtrip`.
- **End:** a from-scratch model (no parent and no origin), or an origin with no
  `source-bom` pin. A non-WALDO release and a WALDO release re-uploaded
  without its `BOM.json` look the same.
- A release `BOM.json` that lists no `LINEAGE.json` is always a gap: without
  it the plan isn't held, so "from scratch" can't be checked. That includes
  every release made by main.

## Local history: `LINEAGE.json`

```
{"kind": "waldo-model-lineage", "schema": 1,
 "documents": {"<sha256>": "<base64 bytes>"},
 "anchors": {"<model id>": ["<sha256 of a MODEL-BOM>"]},
 "sources": {"<sha256>": [{"provider": "store"},
                          {"provider": "huggingface", "repository": "...",
                           "revision": "..."}]},
 "gaps": [{"gap": "<sha256>", "link": "run-bom"}]}
```

- **Encoding.** Compact `json.Marshal` of the struct: map keys sorted,
  standard padded base64. The same history held from the same sources gives
  the same file.
- **No type label.** The link that reaches a document decides its type.
  `anchors` is an index for finding anchors, not a type.
- **Sources** never hold a local path: `store` for this store, otherwise the
  pinned release address (directory pulls: runs repository, record path and
  commit; see "Before code"). A document reached two ways lists both.
- **Only reachable documents.** Only documents reachable from the model's own
  pins, and the anchors of models those reach, are kept.
- **Sources are facts, not tiers.** They are for the later trust rule, and
  they live here rather than in `MERGE.json`, so identical merges stay
  identical.
- **Nothing points at it.** No record names the file, so deleting it loses only
  what isn't held elsewhere, and the loss shows as gaps.

The new binary fills it when it makes a model. It reads the base model's
records and kept files, and any model in the store that holds a needed
document by hash.

- **Written once, atomically.** It is written in `initializeModel`'s temp
  directory before the rename (its `prepare` hook), and in the pull's temp
  directory. A compose resumed after an interruption, or one main started,
  writes it if it is missing. It is never rewritten otherwise, so the gaps
  cache is written once and never trusted.
- **Finding documents in the store.** Index model directories and `.merges`
  by reading records with `readJSON` and `hashJSON`, not `Inspect`, and their
  `LINEAGE.json`. Skip any model that fails to read. Look up by hash and
  model ID, never by a model's local name.

Main makes a model from another in two ways, and both are covered:

- **Parent pin** (the base has completed runs): `plan.parent` is set. The
  child holds:
  - the parent's PLAN and pinned RUN-BOM;
  - the parent's MODEL-BOM as the anchor, and the earlier runs it resolves;
  - everything behind them.
- **Origin copy** (the base has no runs: a fresh pull, a merge, `.merges/<key>`,
  and every join hop). Main copies ORIGIN-BOM verbatim and only the listed
  artifacts.
  - The new binary also copies `MERGE.json`, the kept release `BOM.json` with
    its `LINEAGE.json` and weights header, and writes its own `LINEAGE.json`
    from the base's.
  - A child that main makes of such a base has gaps there. They stay visible
    because the `source-bom` and `source-merge` pins still name the documents.

Local work in progress may have gaps. gmk's existing models are untouched;
their history is whatever is still on disk.

## Exports: where history is completed

A release ships `LINEAGE.json` as a file listed with its SHA-256 in `BOM.json`
`artifacts`, role `lineage`, in every format (Hugging Face, MLX, GGUF,
Ollama). `BOM.json` gets no new field and stays about 3KB however deep the
history is. Export never changes the source model.

The release lineage holds:

- the exporter's PLAN, normalized MODEL-BOM, and ORIGIN-BOM or run BOMs;
- everything behind them, flat and deduplicated, including the `BOM.json` and
  weights header of every release it was pulled through.

At export, WALDO closes every gap it can from what's on disk, finding
documents as compose does (see "Finding documents in the store"). A parent's
earlier runs come from the parent's records if the parent is still present.
Once in the release, they are pinned by the release itself.

Gaps that remain are listed in the release and printed as a warning. Export
doesn't refuse them, because main's models must still export. Merge refuses
them on its path.

A WALDO-format export copies the directory, and writes the copy's
`LINEAGE.json` with gaps closed the same way. Nothing pins that file, so
rewriting it in the copy is safe.

## Pulls

A pull keeps, unlisted in the origin's `artifacts`, in `origin/release/`:

- `BOM.json`;
- `LINEAGE.json`;
- `weights-header.bin`, the release weights' header.

Main's `Pull` is one function (pull.go:105-260). #41 moves the release keep,
the round trip and the lineage check into one helper, so join-local's
`PullDirectory` can call it.

`LINEAGE.json` is fetched only when `BOM.json` lists it, outside the loop
that builds `source_artifacts`: adding it there would change the origin ID.
`BOM.json` is kept before the source directory is removed.

Origin IDs match main's pull of the same release from the same hub address,
proven byte for byte (`source.url` includes the hub endpoint). Main's pull
keeps none of these files and never fetches `LINEAGE.json`. If a kept file is
deleted, the pins name it, and the pinned revision makes it fetchable again.
Directory pulls are fetchable again only once `PullDirectory` records a
runs-repository commit (see "Before code").

The pull runs the weights round trip before re-laying the payload, and checks
every document in the release lineage against its pin.

A release never fails a pull. Documents that fail their pin are dropped with a
warning, and they and a failed round trip become gaps. The pull keeps only
documents that verified, so a later "present and fails" means something
changed on disk.

## Merges (#42)

A merged model is an origin model with no new fields:

| Field | Value |
|---|---|
| `source.provider` | `merge` |
| `source.repository` | empty (hidden origins are content-addressed, ADR 0051) |
| `source.revision`, `requested_revision` | SHA-256 of `MERGE.json` |
| `source.url`, `license` | empty |
| `source_artifacts` | `MERGE.json`, role `source-merge` |
| `artifacts` | the runtime files, as today |

Main reads merged models as ordinary origin models; every command passed on
main's binary. Main's EU export of a merge reports the honest required gap "a
modified model requires the original model public-summary URL", which
`--allow-incomplete` passes. The parents are in the release lineage. Main's
EU-BOM covers only a model's own runs, as for every fine-tune on main.

`MERGE.json` is canonical JSON holding:

- the method and its weights (shares), written as `json.Marshal` writes
  float64;
- for each parent: run BOM hash or origin BOM hash, weights hash, and
  license. No model ID: it includes the local model name, so two people who
  pulled the same parents under different names would write different bytes.
  The run BOM already names its model.
- parents sorted by weights hash, then by BOM hash; `mean-f32-v1` sums them
  in that order, since float addition depends on order.

Nothing else: no locations and no hop sources, so every closer computing the
same merge from the same parents writes the same bytes. The `.merges` key is
the file's hash.

Merge rules:

- **At least two distinct parents** (by weights hash), or the merge is
  refused.
- **Merge-base, as in git:** the set of lowest common ancestors. A common
  ancestor is weights that every parent's history reaches, through the links
  above, including export and pull. It is lowest when it is not an ancestor
  of another common ancestor. The set is the same for every closer, so no
  tie-break is needed. Criss-cross histories (two merges of the same parents
  with different shares, then merged) have two; both count. A member may be
  an end: two fine-tunes of one Hugging Face model, or join round 1 over a
  from-scratch bootstrap. A parent that is an ancestor of another parent is
  itself the merge-base.
- **Strict path:** every parent's path from its tip down to the merge-base
  set, excluding the members themselves, must have no gaps and no ends. Gaps
  and ends below it are recorded, not refused, so one old gap doesn't poison
  every later merge. A gap above every common ancestor hides where histories
  meet; it is on the path, so the merge is refused.
- **A new merge is recomputed when it is made.** Its parents' weights are on
  disk at that moment by definition. `mean-f32-v1` is deterministic across
  platforms (`float32(value * share)` prevents fused multiply-add on arm64); a
  three-parent v3 merge reads 150MB in under a second. In join, every closer
  computes the same merge independently and they must agree.
- **An earlier merge on the path is checked by its documents:** `MERGE.json`
  and each parent's run BOM or ORIGIN-BOM, and weights hash, all matching
  their pins. If the parents' weights happen to be on disk, it is recomputed
  too, and a result that differs is refused. Intermediate weights aren't kept
  anywhere, so later this is the merger's word, like a run BOM.
- **Every parent of an earlier merge on the path must reach a member of the
  merge-base,** through a complete path of its own. WALDO models grow from the
  ground up, so a family never merges in a model from outside it: every merge,
  new or earlier, has parents that share an ancestor. Starting a family from a
  pulled model is still fine; that model is an end the family shares.
- **A merge-base member that is itself a merge** is not on the path, so it
  is checked like any document, not recomputed.
- `MERGE.json`'s place on disk (`origin/MERGE.json`) and its field names are
  settled in #42.

In join, each parent's path is: pulled unit, release weights, the unit's
trained weights, and the start of that run, which is the previous round's merge
as pulled. That merge is the merge-base. Round 0 must stay a single bootstrap
model: several from-scratch units share no ancestor to merge on.

## Existing commands

- `list`, `summary`, `chat`, `train`, `continue`, `rm` and `lookaside` behave
  exactly as on main.
- Fine-tuning and export carry history forward and print gaps as a warning.
- Only merging refuses gaps.

| Found | Pull | Compose, fine-tune, export | Merge |
|---|---|---|---|
| Release document fails its pin, or wrong `kind`/`subject` | dropped, warning, gap | — | — |
| Held document fails its key or pin, or wrong `kind`/`subject` | — | warning naming the file, gap | refused if on the path |
| `LINEAGE.json` doesn't parse, or bad base64 | dropped, warning, gaps | warning naming the file, gaps | refused if on the path |
| Opaque document; missing document; anchors that disagree; failed round trip | gap | warning, gap | refused if on the path |

Held files are written atomically and verified when written, so a held file
that fails means the store was changed, and the warning says so. Nothing but
merge refuses on lineage: pull and export never do, and local work may have
gaps.

## Deferred: identity and signing

The adversary showed that a consistent release can still claim a false
starting point: a run BOM saying "started from your weights". It would then
appear to share your history. No hash catches that. Only knowing who said it
does.

Deferred until we know how people coordinate:

- signing `BOM.json` and keeping `BOM.sigstore.json`;
- checking publishers against named identities;
- trust lists, and `--trust`.

Nothing in B′ blocks adding these later. Each history already records which
release every hop came from, so a later trust rule can be applied to existing
merges. Join's round records, signed by identities' SSH keys, are the first
place it will apply.

## Against the goals

| Case | Design A (current #41) | B′ |
|---|---|---|
| Main reads new fine-tunes | no | yes |
| Main reads new pulls | yes | yes, same origin IDs |
| Main reads merges and their descendants | no; one breaks main's whole store | yes; main's descendants show gaps |
| gmk's existing models | work, no history | work, with history still on disk |
| Altered document | refused | never followed; a warning and a gap; merge refuses it on its path |
| Truncated or deleted `LINEAGE.json` | truncation accepted (bug); deletion refused | gaps; merge refuses on its path |
| Deleted kept release `BOM.json` | silently a root | a gap, fetchable again |
| Training data of each hop | hashes only | held (run BOMs, corpus BOMs once) |
| Earlier runs of a parent | per-run nodes | held by the child as a report (the anchor); the parent can be deleted |
| Merges in a history | trusted as recorded | recomputed when made; checked by documents later |
| Weights across export and pull | not connected | checked by round trip |
| Release history size | linear | linear; `BOM.json` stays about 3KB |
| Newer WALDO's documents | refused | held as opaque, gaps beyond |
| A forged starting point in a release | marked declared | recorded by source; judged when identity lands |
| Maintainer decision | needed (one-way) | none |

Projected sizes, measured documents, k=3, corpus BOMs held once:
- join release lineage: about 1MB at round 1, 2MB at round 16;
- BPE: +0.4MB once for the tokenizer;
- about 0.2GB per identity over 16 rounds.

## Before code

- **Join-local, `PullDirectory`:** it writes `url: file://<absolute path>` and
  the local directory name into a hashed ORIGIN-BOM. That publishes each
  joiner's home path, and makes every closer's merge record differ for the
  same weights. Give it an identity-independent source (runs repository URL
  and record path) in those existing string fields, and the runs-repository
  commit as its revision, so a deleted kept file can be fetched again.
- **Join-local, TRAINING-CLOSURE.json:** replace it, and the closers'
  agreement on it (`decision()`, state.go:226), with the new lineage.
- **#42 merge writer:** checked. It writes `{"format":"pt","merged_by":
  "openwaldo"}`, so the round trip holds.
- **#42:** split out fb3b1a0, the hostfile worker acknowledgement. #42's
  lineage.go (558 lines) is built on design A and is reworked.

## Implementation (#41)

In order, with rough sizes:

1. Remove design A: `LineageNode`, `LineageLink`, `lineage_sha256` and the
   lineage fields (about −1,600 lines with their tests). Keep
   `initializeModel`'s `prepare` hook, `releaseBOMPath`, `exactJSON`,
   `readRegularFile` and `writeReleaseBOM`.
2. Document store: canonical, raw and opaque bytes, splicing (about 250).
3. Walker: links, initialization routing, anchors (about 400).
4. Weights round trip (about 250).
5. Pull: split out the import helper, keep the release files (about 200).
6. `LINEAGE.json` at compose, both cases, and on resume (about 100).
7. Export closes gaps, all formats and WALDO-format (about 150).
8. Tests (about 900): origin ID equal to main's pull; canonical, opaque and
   altered bytes; splice round trip; anchors (duplicate, checkpoint, wrong
   model ID, wrong ordinal, disagreeing snapshots, dropped pins); a cycle;
   truncated and altered `LINEAGE.json`; round trip for MLX and PyTorch
   weights, an untrained origin, a multi-file release; export closing a gap
   from a parent on disk; pull keeping only verified documents; a resumed
   compose.
9. ADR 0084 rewritten, MODEL-EXPORTS and the PR body.

## Open questions

None left. Decided:

- Nothing fetches automatically. A missing kept file is a gap whose warning
  names where it lives (for example, re-pull `<repo>@<revision>`); merge
  refuses it on its path, and refuses a missing parent's weights, with the
  same message. Join may fetch on its own later.
- The release `LINEAGE.json` file is an ordinary documented addition (ADR,
  MODEL-EXPORTS), not a maintainer decision.
- python-byte-v3 is retired after the restack; a fresh run with shorter rounds
  follows.

## History of this doc

- **Draft 2 (B′):** no new record fields; merges as plain origins; a document
  cache.
- **Draft 3:**
  - release lineage moved out of `BOM.json` (nesting grew about 6× per join
    round);
  - origin-copy children added;
  - the unpinned initialization link no longer followed;
  - canonical bytes;
  - release `model_id` and `source_bom_sha256` followed;
  - merge strictness limited to the path;
  - corpus BOMs held once.
- **Draft 4:** no declared state; merges on the path recomputed.
- **Draft 7:**
  - merge-base is git's set of lowest common ancestors (criss-cross
    histories work);
  - a parent's anchor is stated as a report, found by an `anchors` index;
    disagreeing anchors are gaps;
  - initialization routed by `source_type`; earlier runs need a lower
    ordinal (no cycles);
  - `MERGE.json` parents sorted, summed in order, without model IDs; at
    least two; a failed recompute refuses;
  - round trip checks `architecture_sha256` against the MODEL-BOM;
  - `LINEAGE.json` encoding, `sources`, written once atomically and on
    resume;
  - a table of warning and gap; only merge refuses on lineage, so export
    never does;
  - splice encoding, gap link names, release role `lineage`, how export
    finds documents, WALDO-format export closes gaps;
  - pull helper split out of `Pull`; directory pulls fetchable by commit;
  - implementation order and sizes.
- **Draft 6:**
  - earlier runs anchored by a held MODEL-BOM, with one run per ID and its
    weights artifact;
  - round trip pinned to the current run's weights, re-exported to match,
    and checked against metadata;
  - non-canonical documents are opaque, not refused;
  - merge-base defined, deterministic, and allowed to be an end;
  - earlier merges checked by documents (intermediate weights aren't kept);
  - `MERGE.json` holds no location or hop source;
  - pull never fails on lineage;
  - join-local's `PullDirectory` source and closure listed.
- **Draft 5:**
  - weights round trip, measured;
  - initialization followed inside one model through its own records (fixes a
    regression on a parent's earlier runs, and dropped run pins);
  - exports close gaps;
  - every parent of a recomputed merge reaches the merge-base;
  - merge parents carry a location;
  - merge records each hop's source;
  - identity and signing deferred;
  - raw documents decoded strictly before following;
  - leftover "declared" wording removed.
