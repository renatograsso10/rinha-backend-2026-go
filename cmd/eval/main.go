package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"
	"github.com/renat/rinha-backend-2026-go/internal/index"
	"github.com/renat/rinha-backend-2026-go/internal/vector"
)

type testData struct {
	Entries []entry `json:"entries"`
}

type entry struct {
	Request          json.RawMessage `json:"request"`
	ExpectedApproved bool            `json:"expected_approved"`
}

func main() {
	indexPath := flag.String("index", "index.bin", "index path")
	dataPath := flag.String("data", "", "test-data.json path")
	capsArg := flag.String("caps", "64,128,256,512,1024", "comma-separated visit caps")
	limit := flag.Int("limit", 0, "max entries")
	fast := flag.Bool("fast", false, "use fast JSON vectorizer")
	marginsArg := flag.String("margins", "", "comma-separated classifier margins for hybrid evaluation")
	flag.Parse()
	if *dataPath == "" {
		fmt.Fprintln(os.Stderr, "-data is required")
		os.Exit(2)
	}
	idx, err := index.Load(*indexPath)
	must(err)
	raw, err := os.ReadFile(*dataPath)
	must(err)
	var data testData
	must(json.Unmarshal(raw, &data))
	if *limit > 0 && *limit < len(data.Entries) {
		data.Entries = data.Entries[:*limit]
	}
	caps := parseCaps(*capsArg)
	margins := parseFloatCaps(*marginsArg)
	norm := vector.DefaultNormalization()
	mcc := vector.DefaultMCCRisk()
	queries := make([][vector.Dims]float32, len(data.Entries))
	for i, e := range data.Entries {
		var q [vector.Dims]float32
		if *fast {
			var ok bool
			q, ok = vector.VectorizeJSON(e.Request, norm, mcc)
			if !ok {
				panic("fast vectorize failed")
			}
		} else {
			var p vector.Payload
			must(json.Unmarshal(e.Request, &p))
			var err error
			q, err = vector.Vectorize(p, norm, mcc)
			must(err)
		}
		queries[i] = q
	}
	fmt.Printf("entries=%d refs=%d\n", len(data.Entries), idx.Count())
	evalClassifier("linear", data.Entries, queries, vector.LinearApproved)
	evalClassifier("forest", data.Entries, queries, vector.ForestApproved)
	evalScoredClassifier("forest-best", data.Entries, queries, vector.ForestScore)
	for _, cap := range caps {
		start := time.Now()
		fp, fn := 0, 0
		for i, q := range queries {
			frauds := 0
			for _, n := range idx.Search(q, 5, cap) {
				if n.Fraud {
					frauds++
				}
			}
			_, approved := vector.Decision(frauds)
			if approved != data.Entries[i].ExpectedApproved {
				if approved {
					fn++
				} else {
					fp++
				}
			}
		}
		elapsed := time.Since(start)
		failRate := float64(fp+fn) / float64(len(data.Entries)) * 100
		fmt.Printf("cap=%d fp=%d fn=%d fail=%.2f%% avg=%s total=%s\n",
			cap, fp, fn, failRate, elapsed/time.Duration(len(data.Entries)), elapsed)
		for _, margin := range margins {
			start = time.Now()
			fp, fn, ann := 0, 0, 0
			for i, q := range queries {
				risk := vector.LinearRisk(q)
				approved := risk < vector.LinearThreshold
				if risk >= vector.LinearThreshold-margin && risk <= vector.LinearThreshold+margin {
					ann++
					frauds := 0
					for _, n := range idx.Search(q, 5, cap) {
						if n.Fraud {
							frauds++
						}
					}
					_, approved = vector.Decision(frauds)
				}
				if approved != data.Entries[i].ExpectedApproved {
					if approved {
						fn++
					} else {
						fp++
					}
				}
			}
			elapsed = time.Since(start)
			failRate = float64(fp+fn) / float64(len(data.Entries)) * 100
			fmt.Printf("hybrid cap=%d margin=%.3f ann=%d fp=%d fn=%d weighted=%d fail=%.2f%% avg=%s total=%s\n",
				cap, margin, ann, fp, fn, fp+3*fn, failRate, elapsed/time.Duration(len(data.Entries)), elapsed)
		}
	}
}

type scoredDecision struct {
	score            float32
	expectedApproved bool
}

func evalScoredClassifier(name string, entries []entry, queries [][vector.Dims]float32, score func([vector.Dims]float32) float32) {
	decisions := make([]scoredDecision, len(entries))
	for i, q := range queries {
		decisions[i] = scoredDecision{score: score(q), expectedApproved: entries[i].ExpectedApproved}
	}
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].score < decisions[j].score })
	fp, fn := 0, 0
	for _, d := range decisions {
		if d.expectedApproved {
			fp++
		}
	}
	bestWeighted, bestThreshold, bestFP, bestFN := fp+3*fn, decisions[0].score, fp, fn
	for i := 0; i < len(decisions); {
		j := i + 1
		for j < len(decisions) && decisions[j].score == decisions[i].score {
			j++
		}
		for _, d := range decisions[i:j] {
			if d.expectedApproved {
				fp--
			} else {
				fn++
			}
		}
		threshold := decisions[i].score + 1
		if j < len(decisions) {
			threshold = decisions[j].score
		}
		weighted := fp + 3*fn
		if weighted < bestWeighted {
			bestWeighted, bestThreshold, bestFP, bestFN = weighted, threshold, fp, fn
		}
		i = j
	}
	failRate := float64(bestFP+bestFN) / float64(len(entries)) * 100
	fmt.Printf("%s threshold=%.9g fp=%d fn=%d weighted=%d fail=%.2f%%\n",
		name, bestThreshold, bestFP, bestFN, bestWeighted, failRate)
}

func evalClassifier(name string, entries []entry, queries [][vector.Dims]float32, approve func([vector.Dims]float32) bool) {
	start := time.Now()
	fp, fn := 0, 0
	for i, q := range queries {
		approved := approve(q)
		if approved != entries[i].ExpectedApproved {
			if approved {
				fn++
			} else {
				fp++
			}
		}
	}
	elapsed := time.Since(start)
	failRate := float64(fp+fn) / float64(len(entries)) * 100
	fmt.Printf("%s fp=%d fn=%d weighted=%d fail=%.2f%% avg=%s total=%s\n",
		name, fp, fn, fp+3*fn, failRate, elapsed/time.Duration(len(entries)), elapsed)
}

func parseCaps(s string) []int {
	parts := strings.Split(s, ",")
	caps := make([]int, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		must(err)
		caps = append(caps, v)
	}
	return caps
}

func parseFloatCaps(s string) []float32 {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		must(err)
		out = append(out, float32(v))
	}
	return out
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
