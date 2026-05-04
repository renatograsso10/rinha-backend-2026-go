package index

import (
	"os"
	"path/filepath"
	"testing"
)

func sampleRefs() []Reference {
	return []Reference{
		{Vector: [Dims]float32{0.01, 0.08, 0.05, 0.82, 0.16, -1, -1, 0.04, 0.25, 0, 1, 0, 0.2, 0.04}, Fraud: false},
		{Vector: [Dims]float32{0.58, 0.91, 1, 0.04, 0, 0.01, 0.44, 0.46, 0.4, 1, 0, 1, 0.85, 0.003}, Fraud: true},
		{Vector: [Dims]float32{0.95, 0.83, 1, 0.21, 0.83, -1, -1, 0.95, 1, 0, 1, 1, 0.75, 0.005}, Fraud: true},
		{Vector: [Dims]float32{0.004, 0.16, 0.05, 0.78, 0.33, -1, -1, 0.03, 0.15, 0, 1, 0, 0.15, 0.006}, Fraud: false},
		{Vector: [Dims]float32{0.20, 0.10, 0.07, 0.70, 0.30, -1, -1, 0.10, 0.20, 0, 1, 0, 0.30, 0.02}, Fraud: false},
		{Vector: [Dims]float32{0.90, 0.90, 1, 0.25, 0.80, -1, -1, 0.90, 1, 0, 1, 1, 0.80, 0.01}, Fraud: true},
	}
}

func TestSaveLoadValidatesChecksum(t *testing.T) {
	idx, err := Build(sampleRefs())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	path := filepath.Join(t.TempDir(), "index.bin")
	if err := idx.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Count() != len(sampleRefs()) {
		t.Fatalf("count got %d want %d", loaded.Count(), len(sampleRefs()))
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	raw[len(raw)-1] ^= 0xff
	bad := filepath.Join(t.TempDir(), "bad.bin")
	if err := os.WriteFile(bad, raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(bad); err == nil {
		t.Fatal("Load corrupted index succeeded")
	}
}

func TestSearchMatchesBruteForceOnSample(t *testing.T) {
	refs := sampleRefs()
	idx, err := Build(refs)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	query := [Dims]float32{0.93, 0.82, 1, 0.20, 0.80, -1, -1, 0.94, 1, 0, 1, 1, 0.75, 0.005}

	got := idx.Search(query, 5, 64)
	want := BruteForce(refs, query, 5)
	if len(got) != len(want) {
		t.Fatalf("len got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Index != want[i].Index || got[i].Fraud != want[i].Fraud {
			t.Fatalf("result[%d] got %+v want %+v", i, got[i], want[i])
		}
	}
}
