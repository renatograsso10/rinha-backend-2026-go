package main

import (
	"net"
	"os"
	"strconv"

	json "github.com/goccy/go-json"
	"github.com/renat/rinha-backend-2026-go/internal/index"
	"github.com/renat/rinha-backend-2026-go/internal/vector"
	"github.com/valyala/fasthttp"
)

type app struct {
	idx       *index.Index
	norm      vector.Normalization
	mcc       map[string]float32
	visitCap  int
	k         int
	threshold float32
}

type response struct {
	Approved   bool    `json:"approved"`
	FraudScore float32 `json:"fraud_score"`
}

func main() {
	indexPath := getenv("INDEX_PATH", "/app/index.bin")
	idx, err := index.Load(indexPath)
	if err != nil {
		panic(err)
	}
	visitCap, _ := strconv.Atoi(getenv("VISIT_CAP", "8192"))
	k, _ := strconv.Atoi(getenv("K_NEIGHBORS", "5"))
	if k <= 0 {
		k = 5
	}
	thr, _ := strconv.ParseFloat(getenv("FRAUD_THRESHOLD", "0.6"), 32)
	if thr <= 0 || thr > 1 {
		thr = 0.6
	}
	a := &app{
		idx:       idx,
		norm:      vector.DefaultNormalization(),
		mcc:       vector.DefaultMCCRisk(),
		visitCap:  visitCap,
		k:         k,
		threshold: float32(thr),
	}
	if getenv("SERVER_MODE", "fasthttp") == "raw" {
		if socketPath := os.Getenv("SOCKET_PATH"); socketPath != "" {
			_ = os.Remove(socketPath)
			ln, err := net.Listen("unix", socketPath)
			if err != nil {
				panic(err)
			}
			_ = os.Chmod(socketPath, 0o777)
			if err := a.serveRaw(ln); err != nil {
				panic(err)
			}
			return
		}
		ln, err := net.Listen("tcp", ":"+getenv("PORT", "8080"))
		if err != nil {
			panic(err)
		}
		if err := a.serveRaw(ln); err != nil {
			panic(err)
		}
		return
	}
	if socketPath := os.Getenv("SOCKET_PATH"); socketPath != "" {
		_ = os.Remove(socketPath)
		ln, err := net.Listen("unix", socketPath)
		if err != nil {
			panic(err)
		}
		_ = os.Chmod(socketPath, 0o777)
		if err := fasthttp.Serve(ln, a.handle); err != nil {
			panic(err)
		}
		return
	}
	if err := fasthttp.ListenAndServe(":"+getenv("PORT", "8080"), a.handle); err != nil {
		panic(err)
	}
}

func (a *app) handle(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())
	switch {
	case ctx.IsGet() && path == "/ready":
		ctx.SetStatusCode(fasthttp.StatusNoContent)
	case ctx.IsPost() && path == "/fraud-score":
		a.fraudScore(ctx)
	default:
		ctx.SetStatusCode(fasthttp.StatusNotFound)
	}
}

func (a *app) fraudScore(ctx *fasthttp.RequestCtx) {
	ctx.Response.Header.SetContentType("application/json")
	q, ok := vector.VectorizeJSON(ctx.PostBody(), a.norm, a.mcc)
	if !ok {
		var p vector.Payload
		if err := json.Unmarshal(ctx.PostBody(), &p); err != nil {
			ctx.SetStatusCode(fasthttp.StatusOK)
			writeDecision(ctx, 0, true)
			return
		}
		var err error
		q, err = vector.Vectorize(p, a.norm, a.mcc)
		if err != nil {
			ctx.SetStatusCode(fasthttp.StatusOK)
			writeDecision(ctx, 0, true)
			return
		}
	}
	score, approved := a.decisionForVector(q)
	ctx.SetStatusCode(fasthttp.StatusOK)
	writeDecision(ctx, score, approved)
}

func (a *app) decisionForVector(q [vector.Dims]float32) (float32, bool) {
	neighbors := a.idx.Search(q, a.k, a.visitCap)
	frauds := 0
	for _, n := range neighbors {
		if n.Fraud {
			frauds++
		}
	}
	return vector.DecisionWith(frauds, a.k, a.threshold)
}

func writeDecision(ctx *fasthttp.RequestCtx, score float32, approved bool) {
	switch {
	case approved && score == 0:
		ctx.SetBodyString(`{"approved":true,"fraud_score":0}`)
	case approved && score < 0.3:
		ctx.SetBodyString(`{"approved":true,"fraud_score":0.2}`)
	case approved:
		ctx.SetBodyString(`{"approved":true,"fraud_score":0.4}`)
	case score < 0.7:
		ctx.SetBodyString(`{"approved":false,"fraud_score":0.6}`)
	case score < 0.9:
		ctx.SetBodyString(`{"approved":false,"fraud_score":0.8}`)
	default:
		ctx.SetBodyString(`{"approved":false,"fraud_score":1}`)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
