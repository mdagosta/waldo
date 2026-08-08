// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package corpus

import (
	"fmt"

	"github.com/openwaldo/waldo/internal/shard"
)

// AttachShardAttestations records already-verified local shard evidence in a
// corpus BOM. Object identity remains the ShardPin SHA-256; the nested digest
// independently pins the embedded builder BOM.
func AttachShardAttestations(bom *BOM, objects []MaterializedObject) error {
	if bom == nil {
		return fmt.Errorf("corpus BOM is required")
	}
	for _, object := range objects {
		if err := AttachShardAttestation(bom, object); err != nil {
			return err
		}
	}
	for _, pin := range bom.Shards {
		if pin.Attestation == nil {
			return fmt.Errorf("shard %s has no materialized attestation evidence", pin.SHA256[:12])
		}
	}
	return bom.Validate()
}

func AttachShardAttestation(bom *BOM, object MaterializedObject) error {
	attestation, err := shard.InspectAttestation(object.Path)
	if err != nil {
		return fmt.Errorf("inspect shard %s attestation: %w", object.Shard.SHA256[:12], err)
	}
	if attestation.Status == "unattested" {
		attestation.Status = "deep-validated"
	}
	found := false
	for position := range bom.Shards {
		if bom.Shards[position].SHA256 == object.Shard.SHA256 {
			copy := attestation
			bom.Shards[position].Attestation = &copy
			found = true
		}
	}
	if !found {
		return fmt.Errorf("materialized shard %s is absent from the corpus BOM", object.Shard.SHA256[:12])
	}
	return nil
}
