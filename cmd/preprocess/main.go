package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/renat/rinha-backend-2026-go/internal/index"
)

func main() {
	in := flag.String("in", "resources/references.json.gz", "input references json/json.gz")
	out := flag.String("out", "index.bin", "output binary index")
	flag.Parse()

	var (
		refs []index.Reference
		err  error
	)
	if strings.EqualFold(filepath.Ext(*in), ".gz") {
		refs, err = index.LoadJSONGZ(*in)
	} else {
		refs, err = index.LoadJSON(*in)
	}
	if err != nil {
		panic(err)
	}
	idx, err := index.Build(refs)
	if err != nil {
		panic(err)
	}
	if err := idx.Save(*out); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %s refs=%d\n", *out, idx.Count())
}
