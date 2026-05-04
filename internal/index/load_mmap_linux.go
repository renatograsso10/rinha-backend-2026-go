package index

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"syscall"
)

func tryLoadMMap(path string) (*Index, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, true, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, true, err
	}
	if st.Size() < headerSize {
		return nil, true, errors.New("invalid index size")
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		return nil, false, nil
	}
	header := data[:headerSize]
	if string(header[:8]) != magic {
		syscall.Munmap(data)
		return nil, true, errors.New("invalid index header")
	}
	ver := binary.LittleEndian.Uint32(header[8:12])
	if ver != version {
		syscall.Munmap(data)
		return nil, true, errors.New("unsupported index version")
	}
	count := int(binary.LittleEndian.Uint32(header[12:16]))
	nodeCount := int(binary.LittleEndian.Uint32(header[16:20]))
	wantCRC := binary.LittleEndian.Uint32(header[20:24])
	labelBytes := (count + 7) / 8
	vectorBytes := count * Dims * 2
	nodeBytes := nodeCount * 16
	payloadBytes := labelBytes + vectorBytes + nodeBytes
	if len(data) != headerSize+payloadBytes {
		syscall.Munmap(data)
		return nil, true, errors.New("invalid index size")
	}
	if count > 0 && nodeCount != count {
		syscall.Munmap(data)
		return nil, true, errors.New("invalid node count")
	}
	payload := data[headerSize:]
	if crc32.ChecksumIEEE(payload) != wantCRC {
		syscall.Munmap(data)
		return nil, true, errors.New("index checksum mismatch")
	}
	labelStart := headerSize
	vectorStart := labelStart + labelBytes
	nodeStart := vectorStart + vectorBytes
	return &Index{
		count:       count,
		labels:      data[labelStart:vectorStart],
		vectorBytes: data[vectorStart:nodeStart],
		nodeBytes:   data[nodeStart:],
		mmap:        data,
	}, true, nil
}
