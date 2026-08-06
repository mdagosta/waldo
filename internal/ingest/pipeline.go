package ingest

import (
	"context"
	"fmt"
)

// StreamCanonicalTextBatches routes each planned input through its accepted
// adapter in stable plan order. The adapter choice is never re-detected during
// execution.
func StreamCanonicalTextBatches(ctx context.Context, plan Plan, consume func(TextBatch) error) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if consume == nil {
		return fmt.Errorf("text batch consumer is required")
	}
	for _, input := range plan.Inputs {
		emitProgress(ctx, ProgressEvent{Phase: "convert", Status: "started", Input: input.Artifact.Path, Adapter: input.Adapter, TotalBytes: input.Artifact.Bytes})
		inputPlan := plan
		inputPlan.Inputs = []PlanInput{input}
		var err error
		if input.Profile.recordProfile() {
			err = StreamMappedRecordBatches(ctx, inputPlan, consume)
			if err != nil {
				return err
			}
			emitProgress(ctx, ProgressEvent{Phase: "convert", Status: "completed", Input: input.Artifact.Path, Adapter: input.Adapter, Bytes: input.Artifact.Bytes, TotalBytes: input.Artifact.Bytes})
			continue
		}
		if input.Adapter == ProfileBoundedText || input.Adapter == ProfileXMLRecord {
			err = StreamProfiledFileBatches(ctx, inputPlan, consume)
			if err != nil {
				return err
			}
			emitProgress(ctx, ProgressEvent{Phase: "convert", Status: "completed", Input: input.Artifact.Path, Adapter: input.Adapter, Bytes: input.Artifact.Bytes, TotalBytes: input.Artifact.Bytes})
			continue
		}
		switch input.Adapter {
		case "text", "markdown":
			err = StreamTextBatches(ctx, inputPlan, consume)
		case "parquet":
			err = StreamParquetTextBatches(ctx, inputPlan, consume)
		case "jsonl":
			err = StreamJSONLTextBatches(ctx, inputPlan, consume)
		default:
			err = fmt.Errorf("unsupported accepted adapter %q", input.Adapter)
		}
		if err != nil {
			return err
		}
		emitProgress(ctx, ProgressEvent{Phase: "convert", Status: "completed", Input: input.Artifact.Path, Adapter: input.Adapter, Bytes: input.Artifact.Bytes, TotalBytes: input.Artifact.Bytes})
	}
	return nil
}
