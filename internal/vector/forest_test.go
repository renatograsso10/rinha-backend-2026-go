package vector

import "testing"

func TestForestDecisionSeparatesDocExamples(t *testing.T) {
	legit := [Dims]float32{0.0041, 0.1667, 0.05, 0.7826, 0.3333, -1, -1, 0.0292, 0.15, 0, 1, 0, 0.15, 0.006}
	fraud := [Dims]float32{0.9506, 0.8333, 1.0, 0.2174, 0.8333, -1, -1, 0.9523, 1.0, 0, 1, 1, 0.75, 0.0055}

	if ForestApproved(legit) != true {
		t.Fatal("doc legit example should be approved")
	}
	if ForestApproved(fraud) != false {
		t.Fatal("doc fraud example should be denied")
	}
}
