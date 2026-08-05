package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/openwaldo/waldo-new/internal/acquire"
)

var fetchHuggingFaceFile = func(ctx context.Context, request acquire.HuggingFaceRequest) (acquire.Record, error) {
	return acquire.FetchHuggingFaceFile(ctx, request)
}

func runFetchHuggingFace(context Context, args []string, stdout, stderr io.Writer) error {
	request, err := parseFetchHuggingFace(args)
	if err != nil {
		return err
	}
	request.Token = os.Getenv("HF_TOKEN")
	request.Progress = func(progress acquire.Progress) {
		if context.JSON {
			_ = json.NewEncoder(stderr).Encode(progress)
			return
		}
		switch progress.Phase + "/" + progress.Status {
		case "metadata/resolving":
			fmt.Fprintf(stderr, "resolve  %s\n", progress.Message)
		case "artifact/resumed":
			fmt.Fprintf(stderr, "resume   %s (%s verified)\n", progress.Path, humanBytes(progress.Bytes))
		case "artifact/downloading":
			fmt.Fprintf(stderr, "download %s (%s)\n", progress.Path, humanBytes(progress.Total))
		case "artifact/verified":
			fmt.Fprintf(stderr, "verified %s (%s)\n", progress.Path, humanBytes(progress.Bytes))
		case "record/complete":
			fmt.Fprintf(stderr, "record   %s\n", progress.Path)
		case "record/resumed":
			fmt.Fprintf(stderr, "complete %s already verifies\n", progress.Path)
		}
	}
	record, err := fetchHuggingFaceFile(context.Execution, request)
	if err != nil {
		return err
	}
	if context.JSON {
		return writeJSON(stdout, record)
	}
	fmt.Fprintf(stdout, "acquired %s@%s\n", record.Source.Name, shortModelHash(record.Source.Version))
	fmt.Fprintf(stdout, "  directory     %s\n", request.Output)
	fmt.Fprintf(stdout, "  artifact      %s (%s)\n", record.Artifacts[0].Path, humanBytes(record.Artifacts[0].Bytes))
	fmt.Fprintf(stdout, "  sha256        %s\n", record.Artifacts[0].SHA256)
	fmt.Fprintf(stdout, "  evidence      %s\n", acquire.RecordName)
	fmt.Fprintln(stdout, "review the deposit, then pass its directory to `waldo index ingest`")
	return nil
}

func parseFetchHuggingFace(args []string) (acquire.HuggingFaceRequest, error) {
	var request acquire.HuggingFaceRequest
	var positional []string
	for i := 0; i < len(args); i++ {
		argument := args[i]
		switch {
		case argument == "--revision":
			value, next, err := optionValue(args, i, "--revision")
			if err != nil {
				return acquire.HuggingFaceRequest{}, err
			}
			request.Revision, i = value, next
		case strings.HasPrefix(argument, "--revision="):
			request.Revision = strings.TrimPrefix(argument, "--revision=")
		case strings.HasPrefix(argument, "-"):
			return acquire.HuggingFaceRequest{}, usageError{message: fmt.Sprintf("unknown fetch huggingface option %q", argument)}
		default:
			positional = append(positional, argument)
		}
	}
	if len(positional) != 3 {
		return acquire.HuggingFaceRequest{}, usageError{message: "usage: waldo fetch huggingface <owner/dataset> <file> <directory> [--revision <revision>]"}
	}
	request.Dataset, request.File, request.Output = positional[0], positional[1], positional[2]
	return request, nil
}
