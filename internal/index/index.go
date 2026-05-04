package index

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"math"
	"os"
	"sort"

	json "github.com/goccy/go-json"
)

const (
	Dims       = 14
	magic      = "R26KNN01"
	version    = uint32(1)
	headerSize = 8 + 4 + 4 + 4 + 4
	scale      = float32(32767)
)

type Reference struct {
	Vector [Dims]float32 `json:"vector"`
	Fraud  bool          `json:"-"`
	Label  string        `json:"label,omitempty"`
}

type Neighbor struct {
	Index int
	Dist  float32
	Fraud bool
}

type node struct {
	left  int32
	right int32
	idx   int32
	axis  uint8
	_     [3]byte
}

type Index struct {
	count       int
	vectors     []int16
	labels      []byte
	nodes       []node
	vectorBytes []byte
	nodeBytes   []byte
	mmap        []byte
}

func Build(refs []Reference) (*Index, error) {
	if len(refs) == 0 {
		return nil, errors.New("empty references")
	}
	idx := &Index{
		count:   len(refs),
		vectors: make([]int16, len(refs)*Dims),
		labels:  make([]byte, (len(refs)+7)/8),
		nodes:   make([]node, 0, len(refs)),
	}
	points := make([]int, len(refs))
	for i, ref := range refs {
		points[i] = i
		for d, v := range ref.Vector {
			idx.vectors[i*Dims+d] = quant(v)
		}
		if ref.Fraud || ref.Label == "fraud" {
			idx.labels[i/8] |= 1 << uint(i%8)
		}
	}
	idx.buildNode(points, 0)
	return idx, nil
}

func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return idx.count
}

func (idx *Index) Search(query [Dims]float32, k, visitCap int) []Neighbor {
	if idx == nil || idx.Count() == 0 || k <= 0 {
		return nil
	}
	if k > idx.Count() {
		k = idx.Count()
	}
	if visitCap <= 0 {
		visitCap = 4096
	}
	var q [Dims]int16
	for i := range q {
		q[i] = quant(query[i])
	}
	if visitCap >= idx.Count() {
		best := make([]Neighbor, 0, k)
		for i := 0; i < idx.Count(); i++ {
			pushBest(&best, Neighbor{Index: i, Dist: idx.distQ(i, q), Fraud: idx.isFraud(i)}, k)
		}
		return best
	}
	best := make([]Neighbor, 0, k)
	visited := 0
	var walk func(int32)
	walk = func(ni int32) {
		if ni < 0 || visited >= visitCap {
			return
		}
		visited++
		n := idx.nodeAt(ni)
		dist := idx.distQ(int(n.idx), q)
		pushBest(&best, Neighbor{Index: int(n.idx), Dist: dist, Fraud: idx.isFraud(int(n.idx))}, k)
		axis := int(n.axis)
		delta := int(q[axis]) - int(idx.coord(int(n.idx), axis))
		first, second := n.left, n.right
		if delta > 0 {
			first, second = n.right, n.left
		}
		walk(first)
		worst := float32(math.MaxFloat32)
		if len(best) == k {
			worst = best[len(best)-1].Dist
		}
		axisDist := float32(int64(delta) * int64(delta))
		if axisDist <= worst || len(best) < k {
			walk(second)
		}
	}
	walk(0)
	return best
}

func BruteForce(refs []Reference, query [Dims]float32, k int) []Neighbor {
	if k > len(refs) {
		k = len(refs)
	}
	best := make([]Neighbor, 0, k)
	for i, ref := range refs {
		dist := float32(0)
		for d := 0; d < Dims; d++ {
			diff := query[d] - ref.Vector[d]
			dist += diff * diff
		}
		pushBest(&best, Neighbor{Index: i, Dist: dist, Fraud: ref.Fraud || ref.Label == "fraud"}, k)
	}
	return best
}

func (idx *Index) Save(path string) error {
	payload := bytes.Buffer{}
	if err := binary.Write(&payload, binary.LittleEndian, idx.labels); err != nil {
		return err
	}
	if err := binary.Write(&payload, binary.LittleEndian, idx.vectors); err != nil {
		return err
	}
	buf := make([]byte, 16)
	for _, n := range idx.nodes {
		binary.LittleEndian.PutUint32(buf[0:4], uint32(n.left))
		binary.LittleEndian.PutUint32(buf[4:8], uint32(n.right))
		binary.LittleEndian.PutUint32(buf[8:12], uint32(n.idx))
		buf[12] = n.axis
		clear(buf[13:16])
		payload.Write(buf)
	}
	checksum := crc32.ChecksumIEEE(payload.Bytes())

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	if _, err := w.WriteString(magic); err != nil {
		return err
	}
	for _, v := range []uint32{version, uint32(idx.Count()), uint32(len(idx.nodes)), checksum} {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	if _, err := w.Write(payload.Bytes()); err != nil {
		return err
	}
	return w.Flush()
}

func Load(path string) (*Index, error) {
	if idx, ok, err := tryLoadMMap(path); ok || err != nil {
		return idx, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, err
	}
	if string(header[:8]) != magic {
		return nil, errors.New("invalid index header")
	}
	ver := binary.LittleEndian.Uint32(header[8:12])
	if ver != version {
		return nil, errors.New("unsupported index version")
	}
	count := int(binary.LittleEndian.Uint32(header[12:16]))
	nodeCount := int(binary.LittleEndian.Uint32(header[16:20]))
	wantCRC := binary.LittleEndian.Uint32(header[20:24])
	labelBytes := (count + 7) / 8
	vectorBytes := count * Dims * 2
	nodeBytes := nodeCount * 16
	payloadBytes := labelBytes + vectorBytes + nodeBytes
	crc := crc32.NewIEEE()
	r := io.TeeReader(io.LimitReader(f, int64(payloadBytes)), crc)
	idx := &Index{
		count:   count,
		labels:  make([]byte, labelBytes),
		vectors: make([]int16, count*Dims),
		nodes:   make([]node, nodeCount),
	}
	if _, err := io.ReadFull(r, idx.labels); err != nil {
		return nil, err
	}
	buf := make([]byte, 1<<20)
	pos := 0
	for remaining := vectorBytes; remaining > 0; {
		n := len(buf)
		if n > remaining {
			n = remaining
		}
		if _, err := io.ReadFull(r, buf[:n]); err != nil {
			return nil, err
		}
		for off := 0; off < n; off += 2 {
			idx.vectors[pos] = int16(binary.LittleEndian.Uint16(buf[off : off+2]))
			pos++
		}
		remaining -= n
	}
	buf = make([]byte, 1<<20)
	pos = 0
	for remaining := nodeBytes; remaining > 0; {
		n := len(buf)
		if n > remaining {
			n = remaining
		}
		n -= n % 16
		if _, err := io.ReadFull(r, buf[:n]); err != nil {
			return nil, err
		}
		for off := 0; off < n; off += 16 {
			idx.nodes[pos].left = int32(binary.LittleEndian.Uint32(buf[off : off+4]))
			idx.nodes[pos].right = int32(binary.LittleEndian.Uint32(buf[off+4 : off+8]))
			idx.nodes[pos].idx = int32(binary.LittleEndian.Uint32(buf[off+8 : off+12]))
			idx.nodes[pos].axis = buf[off+12]
			pos++
		}
		remaining -= n
	}
	if crc.Sum32() != wantCRC {
		return nil, errors.New("index checksum mismatch")
	}
	var extra [1]byte
	if n, _ := f.Read(extra[:]); n != 0 {
		return nil, errors.New("invalid index size")
	}
	if count > 0 && nodeCount != count {
		return nil, errors.New("invalid node count")
	}
	return idx, nil
}

func LoadJSONGZ(path string) ([]Reference, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	return decodeReferences(gz)
}

func LoadJSON(path string) ([]Reference, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeReferences(f)
}

func decodeReferences(r io.Reader) ([]Reference, error) {
	dec := json.NewDecoder(bufio.NewReaderSize(r, 1<<20))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return nil, errors.New("references must be array")
	}
	refs := make([]Reference, 0, 1024)
	for dec.More() {
		var ref Reference
		if err := dec.Decode(&ref); err != nil {
			return nil, err
		}
		ref.Fraud = ref.Label == "fraud"
		refs = append(refs, ref)
	}
	return refs, nil
}

func (idx *Index) buildNode(points []int, depth int) int32 {
	if len(points) == 0 {
		return -1
	}
	axis := uint8(depth % Dims)
	sort.Slice(points, func(i, j int) bool {
		return idx.coord(points[i], int(axis)) < idx.coord(points[j], int(axis))
	})
	mid := len(points) / 2
	ni := int32(len(idx.nodes))
	idx.nodes = append(idx.nodes, node{idx: int32(points[mid]), axis: axis, left: -1, right: -1})
	idx.nodes[ni].left = idx.buildNode(points[:mid], depth+1)
	idx.nodes[ni].right = idx.buildNode(points[mid+1:], depth+1)
	return ni
}

func (idx *Index) distQ(point int, q [Dims]int16) float32 {
	sum := int64(0)
	for d := 0; d < Dims; d++ {
		diff := int64(q[d]) - int64(idx.coord(point, d))
		sum += diff * diff
	}
	return float32(sum)
}

func (idx *Index) isFraud(i int) bool {
	return idx.labels[i/8]&(1<<uint(i%8)) != 0
}

func (idx *Index) coord(point, dim int) int16 {
	if idx.vectorBytes != nil {
		off := (point*Dims + dim) * 2
		return int16(binary.LittleEndian.Uint16(idx.vectorBytes[off : off+2]))
	}
	return idx.vectors[point*Dims+dim]
}

func (idx *Index) nodeAt(ni int32) node {
	if idx.nodeBytes != nil {
		off := int(ni) * 16
		return node{
			left:  int32(binary.LittleEndian.Uint32(idx.nodeBytes[off : off+4])),
			right: int32(binary.LittleEndian.Uint32(idx.nodeBytes[off+4 : off+8])),
			idx:   int32(binary.LittleEndian.Uint32(idx.nodeBytes[off+8 : off+12])),
			axis:  idx.nodeBytes[off+12],
		}
	}
	return idx.nodes[ni]
}

func quant(v float32) int16 {
	if v < -1 {
		v = -1
	}
	if v > 1 {
		v = 1
	}
	return int16(math.Round(float64(v * scale)))
}

func pushBest(best *[]Neighbor, n Neighbor, k int) {
	items := *best
	pos := sort.Search(len(items), func(i int) bool {
		if items[i].Dist == n.Dist {
			return items[i].Index > n.Index
		}
		return items[i].Dist > n.Dist
	})
	items = append(items, Neighbor{})
	copy(items[pos+1:], items[pos:])
	items[pos] = n
	if len(items) > k {
		items = items[:k]
	}
	*best = items
}
