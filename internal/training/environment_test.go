// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package training

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestEnvironmentResolverUsesMLXForDarwinAuto(t *testing.T) {
	called := false
	resolver := EnvironmentResolver{
		Preference: BackendAuto, OS: "darwin", Arch: "arm64",
		Resolvers: map[string]Resolver{BackendMLX: ResolverFunc(func(_ context.Context, _ ResolveRequest) (Selection, error) {
			called = true
			return testSelection("mlx"), nil
		})},
		Probe: func(context.Context, []string, string, string) (pythonPackage, error) {
			t.Fatal("macOS auto must not probe Linux frameworks")
			return pythonPackage{}, nil
		},
	}
	selection, err := resolver.Resolve(context.Background(), ResolveRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !called || selection.Execution.Framework != "mlx" {
		t.Fatalf("selection = %+v, called = %v", selection, called)
	}
}

func TestEnvironmentResolverPrefersInstalledTorchTitanOnLinux(t *testing.T) {
	var modules []string
	resolver := EnvironmentResolver{
		Preference: BackendAuto, OS: "linux", Arch: "amd64", Candidates: []string{"python3"},
		Resolvers: map[string]Resolver{
			BackendTorchTitan: ResolverFunc(func(_ context.Context, _ ResolveRequest) (Selection, error) {
				return testSelection("torchtitan"), nil
			}),
			BackendPyTorch: ResolverFunc(func(_ context.Context, _ ResolveRequest) (Selection, error) {
				t.Fatal("PyTorch must not be selected when TorchTitan is installed")
				return Selection{}, nil
			}),
		},
		Probe: func(_ context.Context, _ []string, _, module string) (pythonPackage, error) {
			modules = append(modules, module)
			return pythonPackage{Python: "python3", Version: "1.0"}, nil
		},
	}
	selection, err := resolver.Resolve(context.Background(), ResolveRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Execution.Framework != "torchtitan" || strings.Join(modules, ",") != "torchtitan" {
		t.Fatalf("selection = %+v, probes = %v", selection, modules)
	}
}

func TestEnvironmentResolverFallsBackToInstalledPyTorchOnLinux(t *testing.T) {
	resolver := EnvironmentResolver{
		Preference: BackendAuto, OS: "linux", Arch: "arm64", Candidates: []string{"python3"},
		Resolvers: map[string]Resolver{BackendPyTorch: ResolverFunc(func(_ context.Context, _ ResolveRequest) (Selection, error) {
			return testSelection("pytorch"), nil
		})},
		Probe: func(_ context.Context, _ []string, _, module string) (pythonPackage, error) {
			if module == "torchtitan" {
				return pythonPackage{}, errPythonPackageNotFound
			}
			return pythonPackage{Python: "python3", Version: "2.9"}, nil
		},
	}
	selection, err := resolver.Resolve(context.Background(), ResolveRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Execution.Framework != "pytorch" {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestEnvironmentResolverRejectsMultiNodeWithoutTorchTitan(t *testing.T) {
	cluster := Cluster{Nodes: 4, NodeRank: 0, Rendezvous: "primary:29500", RendezvousID: "run-A"}

	autoResolver := EnvironmentResolver{
		Preference: BackendAuto, OS: "linux", Arch: "arm64", Candidates: []string{"python3"}, Cluster: cluster,
		Resolvers: map[string]Resolver{BackendPyTorch: ResolverFunc(func(_ context.Context, _ ResolveRequest) (Selection, error) {
			return testSelection("pytorch"), nil
		})},
		Probe: func(_ context.Context, _ []string, _, module string) (pythonPackage, error) {
			if module == "torchtitan" {
				return pythonPackage{}, errPythonPackageNotFound
			}
			return pythonPackage{Python: "python3", Version: "2.9"}, nil
		},
	}
	_, err := autoResolver.Resolve(context.Background(), ResolveRequest{})
	if err == nil || !strings.Contains(err.Error(), "multi-node training") || !strings.Contains(err.Error(), "requires the TorchTitan backend") {
		t.Fatalf("auto multi-node error = %v", err)
	}

	fakeResolver := EnvironmentResolver{
		Preference: BackendFake, OS: "linux", Arch: "amd64", Cluster: cluster,
		Resolvers: map[string]Resolver{BackendFake: FakeResolver()},
	}
	if _, err := fakeResolver.Resolve(context.Background(), ResolveRequest{}); err == nil || !strings.Contains(err.Error(), "requires the TorchTitan backend") {
		t.Fatalf("explicit fake multi-node error = %v", err)
	}

	fakeResolver.Cluster = Cluster{}
	if _, err := fakeResolver.Resolve(context.Background(), ResolveRequest{}); err != nil {
		t.Fatalf("single-node fake resolve = %v", err)
	}
}

func TestEnvironmentResolverExplainsMissingBackends(t *testing.T) {
	missing := func(context.Context, []string, string, string) (pythonPackage, error) {
		return pythonPackage{}, errPythonPackageNotFound
	}
	resolver := EnvironmentResolver{Preference: BackendAuto, OS: "linux", Arch: "amd64", Candidates: []string{"python3"}, Probe: missing}
	_, err := resolver.Resolve(context.Background(), ResolveRequest{})
	if err == nil || !strings.Contains(err.Error(), "neither TorchTitan nor PyTorch") || !strings.Contains(err.Error(), "detected host:") || !strings.Contains(err.Error(), "pytorch.org/get-started/locally") {
		t.Fatalf("auto error = %v", err)
	}

	resolver.Preference = BackendPyTorch
	_, err = resolver.Resolve(context.Background(), ResolveRequest{})
	if err == nil || !strings.Contains(err.Error(), "`torch` Python package is not installed") || !strings.Contains(err.Error(), "CUDA, ROCm, or CPU") {
		t.Fatalf("explicit error = %v", err)
	}
}

func TestLinuxDistributionUsesPrettyName(t *testing.T) {
	got := linuxDistribution([]byte("NAME=Ubuntu\nVERSION_ID=24.04\nPRETTY_NAME=\"Ubuntu 24.04 LTS\"\n"))
	if got != "Ubuntu 24.04 LTS" {
		t.Fatalf("distribution = %q", got)
	}
}

func TestEnvironmentResolverDoesNotFallBackFromBrokenPreferredProbe(t *testing.T) {
	resolver := EnvironmentResolver{
		Preference: BackendAuto, OS: "linux", Arch: "amd64", Candidates: []string{"python3"},
		Probe: func(_ context.Context, _ []string, _, module string) (pythonPackage, error) {
			if module == "torchtitan" {
				return pythonPackage{}, errors.New("Python probe crashed")
			}
			t.Fatal("must not hide a broken preferred TorchTitan installation by falling back")
			return pythonPackage{}, nil
		},
	}
	_, err := resolver.Resolve(context.Background(), ResolveRequest{})
	if err == nil || !strings.Contains(err.Error(), "probe TorchTitan") {
		t.Fatalf("error = %v", err)
	}
}

func testSelection(framework string) Selection {
	backend := &testBackend{framework: framework}
	return Selection{Backend: backend, Execution: Execution{
		Backend: backend.Descriptor().Identity, Framework: framework,
		Host: Host{OS: "test", Architecture: "test"}, Nodes: 1, WorldSize: 1,
	}}
}

type testBackend struct{ framework string }

func (backend *testBackend) Descriptor() Descriptor {
	return Descriptor{Identity: Identity{Name: backend.framework, Revision: "test"}, Framework: backend.framework}
}

func (*testBackend) Run(context.Context, Request) (Observation, error) {
	return Observation{}, errors.New("not used")
}
