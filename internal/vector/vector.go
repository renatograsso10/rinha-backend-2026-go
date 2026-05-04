package vector

import (
	"errors"
	"time"
)

const Dims = 14

type Payload struct {
	ID              string           `json:"id"`
	Transaction     Transaction      `json:"transaction"`
	Customer        Customer         `json:"customer"`
	Merchant        Merchant         `json:"merchant"`
	Terminal        Terminal         `json:"terminal"`
	LastTransaction *LastTransaction `json:"last_transaction"`
}

type Transaction struct {
	Amount       float64 `json:"amount"`
	Installments int     `json:"installments"`
	RequestedAt  string  `json:"requested_at"`
}

type Customer struct {
	AvgAmount      float64  `json:"avg_amount"`
	TxCount24h     int      `json:"tx_count_24h"`
	KnownMerchants []string `json:"known_merchants"`
}

type Merchant struct {
	ID        string  `json:"id"`
	MCC       string  `json:"mcc"`
	AvgAmount float64 `json:"avg_amount"`
}

type Terminal struct {
	IsOnline    bool    `json:"is_online"`
	CardPresent bool    `json:"card_present"`
	KmFromHome  float64 `json:"km_from_home"`
}

type LastTransaction struct {
	Timestamp     string  `json:"timestamp"`
	KmFromCurrent float64 `json:"km_from_current"`
}

type Normalization struct {
	MaxAmount            float64 `json:"max_amount"`
	MaxInstallments      float64 `json:"max_installments"`
	AmountVsAvgRatio     float64 `json:"amount_vs_avg_ratio"`
	MaxMinutes           float64 `json:"max_minutes"`
	MaxKM                float64 `json:"max_km"`
	MaxTxCount24h        float64 `json:"max_tx_count_24h"`
	MaxMerchantAvgAmount float64 `json:"max_merchant_avg_amount"`
}

func DefaultNormalization() Normalization {
	return Normalization{
		MaxAmount:            10000,
		MaxInstallments:      12,
		AmountVsAvgRatio:     10,
		MaxMinutes:           1440,
		MaxKM:                1000,
		MaxTxCount24h:        20,
		MaxMerchantAvgAmount: 10000,
	}
}

func DefaultMCCRisk() map[string]float32 {
	return map[string]float32{
		"5411": 0.15,
		"5812": 0.30,
		"5912": 0.20,
		"5944": 0.45,
		"7801": 0.80,
		"7802": 0.75,
		"7995": 0.85,
		"4511": 0.35,
		"5311": 0.25,
		"5999": 0.50,
	}
}

func Vectorize(p Payload, n Normalization, mcc map[string]float32) ([Dims]float32, error) {
	var out [Dims]float32
	reqAt, err := time.Parse(time.RFC3339, p.Transaction.RequestedAt)
	if err != nil {
		return out, err
	}
	reqAt = reqAt.UTC()

	out[0] = clamp32(p.Transaction.Amount / n.MaxAmount)
	out[1] = clamp32(float64(p.Transaction.Installments) / n.MaxInstallments)
	if p.Customer.AvgAmount > 0 {
		out[2] = clamp32((p.Transaction.Amount / p.Customer.AvgAmount) / n.AmountVsAvgRatio)
	}
	out[3] = float32(reqAt.Hour()) / 23
	out[4] = float32((int(reqAt.Weekday())+6)%7) / 6

	if p.LastTransaction == nil {
		out[5], out[6] = -1, -1
	} else {
		lastAt, err := time.Parse(time.RFC3339, p.LastTransaction.Timestamp)
		if err != nil {
			return out, err
		}
		mins := reqAt.Sub(lastAt.UTC()).Minutes()
		if mins < 0 {
			return out, errors.New("last_transaction.timestamp after transaction.requested_at")
		}
		out[5] = clamp32(mins / n.MaxMinutes)
		out[6] = clamp32(p.LastTransaction.KmFromCurrent / n.MaxKM)
	}

	out[7] = clamp32(p.Terminal.KmFromHome / n.MaxKM)
	out[8] = clamp32(float64(p.Customer.TxCount24h) / n.MaxTxCount24h)
	if p.Terminal.IsOnline {
		out[9] = 1
	}
	if p.Terminal.CardPresent {
		out[10] = 1
	}
	out[11] = 1
	for _, known := range p.Customer.KnownMerchants {
		if known == p.Merchant.ID {
			out[11] = 0
			break
		}
	}
	if risk, ok := mcc[p.Merchant.MCC]; ok {
		out[12] = risk
	} else {
		out[12] = 0.5
	}
	out[13] = clamp32(p.Merchant.AvgAmount / n.MaxMerchantAvgAmount)
	return out, nil
}

func Decision(frauds int) (float32, bool) {
	score := float32(frauds) / 5
	return score, score < 0.6
}

func clamp32(v float64) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return float32(v)
}
