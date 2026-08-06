# Architectural decision records

ADRs record decisions that constrain future implementation. They describe why
a choice was made and what would justify revisiting it; they are not progress
logs.

- [0001: One binary with bounded domains](0001-single-binary.md)
- [0002: OpenWALDO BOMs are the model boundary](0002-openwaldo-bom-boundary.md)
- [0003: Preserve the public index format](0003-index-compatibility.md)
- [0004: Persist the training run state machine](0004-training-run-state.md)
- [0005: Keep fetchers external, bounded, and local-first](0005-external-fetchers.md)
- [0006: Use an index-centered CLI](0006-index-centered-cli.md)
- [0007: Verify objects before admission and export](0007-verified-lookaside.md)
- [0008: Expand submanifest trees before materialization](0008-verified-submanifests.md)
- [0009: Stabilize the corpus export BOM](0009-stable-corpus-bom.md)
- [0010: Canonical text uses record schema 1 Parquet](0010-canonical-text-parquet.md)
- [0011: Separate immutable model plans, run BOMs, and observed state](0011-model-plan-and-boms.md)
- [0012: Store interactive S3 credentials in the OS keychain](0012-s3-credentials-in-os-keychain.md)
- [0013: Remove lookaside objects only by explicit name](0013-explicit-lookaside-removal.md)
- [0014: Execute external fetchers only through explicit ingest recipe](0014-explicit-ingest-recipe.md)
- [0015: Keep ingest manifests compact and token counts referential](0015-compact-ingest-manifests.md)
- [0016: Forecast against a versioned accelerator catalog](0016-versioned-hardware-forecast.md)
- [0017: Resolve training backends outside portable model composes](0017-portable-training-backend-contract.md)
- [0018: Resolve training profiles and stream a versioned worker protocol](0018-training-profile-and-worker-protocol.md)
- [0019: Fail closed into a real MLX backend on Apple Silicon](0019-real-mlx-backend.md)
- [0020: Select training backends from host policy](0020-host-backend-policy.md)
- [0021: Keep inference ephemeral and artifact-bound](0021-ephemeral-inference.md)
- [0022: Export separate signed model release formats](0022-model-release-exports.md)
- [0023: Reset unreleased schemas to version 1](0023-reset-unreleased-schemas.md)
- [0024: Execute Linux training through the shared PyTorch worker contract](0024-pytorch-training-adapter.md)
- [0025: Use TorchTitan for single-node distributed training](0025-torchtitan-distributed-adapter.md)
- [0026: Normalize pinned open weights behind an origin BOM](0026-pinned-model-origin.md)
- [0027: Quantize with upstream tools and bounded index calibration](0027-bounded-index-calibration.md)
- [0028: Make index metadata YAML-primary with JSON compatibility](0028-yaml-primary-index-metadata.md)
