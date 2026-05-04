package vector

import (
	"math"
	"testing"
)

func assertClose(t *testing.T, got, want float32) {
	t.Helper()
	if math.Abs(float64(got-want)) > 0.0002 {
		t.Fatalf("got %.4f want %.4f", got, want)
	}
}

func TestVectorizeLegitDocExample(t *testing.T) {
	payload := Payload{
		ID: "tx-1329056812",
		Transaction: Transaction{
			Amount:       41.12,
			Installments: 2,
			RequestedAt:  "2026-03-11T18:45:53Z",
		},
		Customer: Customer{
			AvgAmount:      82.24,
			TxCount24h:     3,
			KnownMerchants: []string{"MERC-003", "MERC-016"},
		},
		Merchant:        Merchant{ID: "MERC-016", MCC: "5411", AvgAmount: 60.25},
		Terminal:        Terminal{IsOnline: false, CardPresent: true, KmFromHome: 29.23},
		LastTransaction: nil,
	}

	got, err := Vectorize(payload, DefaultNormalization(), DefaultMCCRisk())
	if err != nil {
		t.Fatalf("Vectorize: %v", err)
	}

	want := [Dims]float32{0.0041, 0.1667, 0.05, 0.7826, 0.3333, -1, -1, 0.0292, 0.15, 0, 1, 0, 0.15, 0.006}
	for i := range want {
		assertClose(t, got[i], want[i])
	}
}

func TestVectorizeFraudDocExampleClampsAndUnknownMerchant(t *testing.T) {
	payload := Payload{
		ID: "tx-3330991687",
		Transaction: Transaction{
			Amount:       9505.97,
			Installments: 10,
			RequestedAt:  "2026-03-14T05:15:12Z",
		},
		Customer: Customer{
			AvgAmount:      81.28,
			TxCount24h:     20,
			KnownMerchants: []string{"MERC-008", "MERC-007", "MERC-005"},
		},
		Merchant:        Merchant{ID: "MERC-068", MCC: "7802", AvgAmount: 54.86},
		Terminal:        Terminal{IsOnline: false, CardPresent: true, KmFromHome: 952.27},
		LastTransaction: nil,
	}

	got, err := Vectorize(payload, DefaultNormalization(), DefaultMCCRisk())
	if err != nil {
		t.Fatalf("Vectorize: %v", err)
	}

	want := [Dims]float32{0.9506, 0.8333, 1.0, 0.2174, 0.8333, -1, -1, 0.9523, 1.0, 0, 1, 1, 0.75, 0.0055}
	for i := range want {
		assertClose(t, got[i], want[i])
	}
}

func TestVectorizeLastTransactionAndDefaultMCC(t *testing.T) {
	payload := Payload{
		Transaction: Transaction{
			Amount:       200,
			Installments: 1,
			RequestedAt:  "2026-03-12T00:30:00Z",
		},
		Customer: Customer{
			AvgAmount:      100,
			TxCount24h:     50,
			KnownMerchants: []string{"MERC-001"},
		},
		Merchant: Merchant{ID: "MERC-002", MCC: "0000", AvgAmount: 20000},
		Terminal: Terminal{IsOnline: true, CardPresent: false, KmFromHome: 1500},
		LastTransaction: &LastTransaction{
			Timestamp:     "2026-03-11T23:00:00Z",
			KmFromCurrent: 2000,
		},
	}

	got, err := Vectorize(payload, DefaultNormalization(), DefaultMCCRisk())
	if err != nil {
		t.Fatalf("Vectorize: %v", err)
	}

	assertClose(t, got[5], 90.0/1440.0)
	assertClose(t, got[6], 1)
	assertClose(t, got[7], 1)
	assertClose(t, got[8], 1)
	assertClose(t, got[9], 1)
	assertClose(t, got[10], 0)
	assertClose(t, got[11], 1)
	assertClose(t, got[12], 0.5)
	assertClose(t, got[13], 1)
}

func TestDecisionThreshold(t *testing.T) {
	tests := []struct {
		frauds   int
		score    float32
		approved bool
	}{
		{0, 0.0, true},
		{2, 0.4, true},
		{3, 0.6, false},
		{5, 1.0, false},
	}

	for _, tt := range tests {
		score, approved := Decision(tt.frauds)
		assertClose(t, score, tt.score)
		if approved != tt.approved {
			t.Fatalf("frauds=%d approved got %v want %v", tt.frauds, approved, tt.approved)
		}
	}
}
