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
