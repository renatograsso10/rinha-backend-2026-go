package vector

import "testing"

func TestVectorizeJSONMatchesStructVectorize(t *testing.T) {
	body := []byte(`{"id":"tx-3576980410","transaction":{"amount":384.88,"installments":3,"requested_at":"2026-03-11T20:23:35Z"},"customer":{"avg_amount":769.76,"tx_count_24h":3,"known_merchants":["MERC-009","MERC-009","MERC-001","MERC-001"]},"merchant":{"id":"MERC-001","mcc":"5912","avg_amount":298.95},"terminal":{"is_online":false,"card_present":true,"km_from_home":13.7090520965},"last_transaction":{"timestamp":"2026-03-11T14:58:35Z","km_from_current":18.8626479774}}`)
	p := Payload{
		Transaction: Transaction{Amount: 384.88, Installments: 3, RequestedAt: "2026-03-11T20:23:35Z"},
		Customer:    Customer{AvgAmount: 769.76, TxCount24h: 3, KnownMerchants: []string{"MERC-009", "MERC-009", "MERC-001", "MERC-001"}},
		Merchant:    Merchant{ID: "MERC-001", MCC: "5912", AvgAmount: 298.95},
		Terminal:    Terminal{IsOnline: false, CardPresent: true, KmFromHome: 13.7090520965},
		LastTransaction: &LastTransaction{
			Timestamp:     "2026-03-11T14:58:35Z",
			KmFromCurrent: 18.8626479774,
		},
	}
	want, err := Vectorize(p, DefaultNormalization(), DefaultMCCRisk())
	if err != nil {
		t.Fatal(err)
	}
	got, ok := VectorizeJSON(body, DefaultNormalization(), DefaultMCCRisk())
	if !ok {
		t.Fatal("fast vectorize failed")
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dim %d got %v want %v", i, got[i], want[i])
		}
	}
}

func TestVectorizeJSONNullLastTransaction(t *testing.T) {
	body := []byte(`{"id":"tx-1329056812","transaction":{"amount":41.12,"installments":2,"requested_at":"2026-03-11T18:45:53Z"},"customer":{"avg_amount":82.24,"tx_count_24h":3,"known_merchants":["MERC-003","MERC-016"]},"merchant":{"id":"MERC-016","mcc":"5411","avg_amount":60.25},"terminal":{"is_online":false,"card_present":true,"km_from_home":29.23},"last_transaction":null}`)
	got, ok := VectorizeJSON(body, DefaultNormalization(), DefaultMCCRisk())
	if !ok {
		t.Fatal("fast vectorize failed")
	}
	if got[5] != -1 || got[6] != -1 || got[11] != 0 {
		t.Fatalf("unexpected dims: last=(%v,%v), unknown=%v", got[5], got[6], got[11])
	}
}
