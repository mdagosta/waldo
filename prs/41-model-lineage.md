A model records only one step of its history, so deleting an intermediate model loses everything before it and breaks the provenance that shows how, and on what data, a model was trained. This keeps that history with every model made from another, and with its releases. Model merging (#42) builds on it to find where two models' weights came from.

**Stacked:** these PRs merge in order: #41, #42, #46, #47, #49, then #48. This is the bottom: it sits on main, and #42 builds on it.

## What changed

When WALDO makes a model from another, by compose or pull, it saves the documents behind it in `LINEAGE.json` (`waldo-model-lineage`, schema 1): plans, run BOMs, origin BOMs, model BOMs and release `BOM.json` files, each as its bytes under its SHA-256. No record changes.

- **Parents can be deleted.** A fine-tune of a fine-tune keeps the whole chain.
- **Links follow existing pins.** A document counts only when its hash matches and it names what its pin names: a run BOM names the pinned model and run, a model BOM names its model, and a parent pin's weights are the ones its parent's run made, when that run's model BOM is held.
- **Gaps warn, never fail.** A document that's missing or doesn't match is a gap. Composes, pulls and exports warn and carry on, and `LINEAGE.json` lists its gaps.
- **Releases.** Hugging Face, MLX, GGUF and Ollama exports ship `LINEAGE.json`, listed in `BOM.json` with role `lineage`. A pull links a release to the run that made it only when the release lists the pulled weights by hash, and keeps the file out of the origin, so a model's ID doesn't depend on its history.
- **Identity and integrity.** As in VISION, hashes prove what a history contains and that it's intact, not that it's true: its documents say what their makers said, and signing says who said it.

Derived releases now carry their ancestors' plans and run BOMs, with their corpus BOMs; the native WALDO package already carried the model's own.

## Verification

`gofmt` and every `./testing/all.sh` step pass on macOS arm64 at 670904c, including the real MLX lifecycle; the Linux-only PyTorch/TorchTitan suites and the calibrated GGUF export skip. In `testing/e2e/model-mlx.sh`, the Hugging Face export lists `LINEAGE.json` in its `BOM.json`, and pulling it back keeps its history with no gaps. Unit tests cover fine-tune chains with their parents deleted, a pulled fine-tune fine-tuned again, damaged and missing histories as gaps, pins whose documents name another model, run or weights as gaps, releases that never fail a pull and link only their own weights, origin IDs that don't depend on lineage, and a resumed compose writing its missing history.

## Contracts

ADR 0084 (it collides with #38's WIP numbering; happy to renumber). New file: `LINEAGE.json` in a model made from another and in derived release packages, role `lineage` in their `BOM.json`. No record fields change.

## DCO

- [x] Every commit includes a `Signed-off-by` trailer (`git commit --signoff`).

