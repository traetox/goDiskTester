package main

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"hash"
	"io"
	"math/rand"
	"os"
	"time"

	"github.com/gravwell/gravwell/v3/ingest"
)

const (
	blockSize int64 = 1024 * 1024        //1MB
	GB        int64 = 1024 * 1024 * 1024 //1GB
)

type diskTestResult struct {
	TimeToRun time.Duration
	Size      uint64
	HumanSize string
	Written   uint64
	Read      uint64
}

func testDisk(pth string) (res diskTestResult, err error) {
	var fio *os.File
	if fio, err = os.OpenFile(pth, os.O_RDWR, 0600); err != nil {
		return
	}

	//get the disk sizes
	var fi os.FileInfo
	if fi, err = fio.Stat(); err != nil {
		return
	}
	sz := fi.Size()
	blockAlignedSize := sz - (sz % blockSize)
	writeSize := (*wSize * GB) / 2 //because we do contiguous and random
	if writeSize > blockAlignedSize {
		writeSize = blockAlignedSize
	}

	ts := time.Now()

	//do the big contiguous read/write
	if err = contiguousTest(fio, writeSize); err != nil {
		return
	}

	//do the random tests
	if err = randomTest(fio, writeSize, blockAlignedSize); err != nil {
		return
	}

	if err = fio.Close(); err != nil {
		return
	}
	res = diskTestResult{
		TimeToRun: time.Since(ts),
		Written:   uint64(writeSize * 2),
		Read:      uint64(writeSize * 2),
		Size:      uint64(sz),
		HumanSize: ingest.HumanSize(uint64(sz)),
	}
	return
}

func randomTest(fio *os.File, writeSize, totalSize int64) (err error) {
	var n int
	blocksToWrite := writeSize / blockSize
	maxBlock := totalSize / blockSize

	block := make([]byte, blockSize)
	blocks := make([]int64, 0, blocksToWrite)
	hw := newHashWriter(fio)
	randRdr := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := 0; i < int(blocksToWrite); i++ {
		//pick a random block
		blockOffset := randRdr.Int63n(maxBlock) * blockSize
		if n, err = randRdr.Read(block); err != nil || n != len(block) {
			err = fmt.Errorf("failed to create random data block [%d:%d} %w", n, len(block), err)
			return
		} else if n, err = hw.WriteAt(block, blockOffset); err != nil {
			return
		} else if n != len(block) {
			err = fmt.Errorf("failed to write complete datablock %d/%d", n, len(block))
			return
		}
		blocks = append(blocks, blockOffset)
	}

	hsh := md5.New()
	for _, off := range blocks {
		if n, err = fio.ReadAt(block, off); err != nil || n != len(block) {
			err = fmt.Errorf("failed to read block at 0x%x [%d:%d} %w", off, n, len(block), err)
			return
		} else if n, err = hsh.Write(block); err != nil || n != len(block) {
			err = fmt.Errorf("failed to copy block at 0x%x into hasher [%d:%d} %w", off, n, len(block), err)
			return
		}
	}

	wSum := hw.Sum()
	rSum := hsh.Sum(nil)
	if v := bytes.Compare(wSum, rSum); v != 0 {
		err = fmt.Errorf("write and read hashes do not match: %x != %x", rSum, wSum)
	}

	return
}

func contiguousTest(fio *os.File, writeSize int64) (err error) {
	var written int64
	var read int64
	if _, err = fio.Seek(0, 0); err != nil {
		return
	}

	//write it out
	hw := newHashWriter(fio)
	randRdr := rand.New(rand.NewSource(time.Now().UnixNano()))
	if written, err = io.CopyN(hw, randRdr, writeSize); err != nil {
		return
	} else if written != writeSize {
		err = fmt.Errorf("Failed to write %d bytes, only %d written", writeSize, written)
		return
	}
	//reset to beginning and copy the data back out into an md5 reader
	if _, err = fio.Seek(0, 0); err != nil {
		return
	}
	hsh := md5.New()
	if read, err = io.CopyN(hsh, fio, writeSize); err != nil {
		return
	} else if read != writeSize {
		err = fmt.Errorf("Failed to read %d bytes, only %d read", writeSize, read)
		return
	}
	wSum := hw.Sum()
	rSum := hsh.Sum(nil)
	if v := bytes.Compare(wSum, rSum); v != 0 {
		err = fmt.Errorf("write and read hashes do not match: %x != %x", rSum, wSum)
	}
	return //all good
}

func newHashWriter(fio *os.File) *hashWriter {
	return &hashWriter{
		fio: fio,
		hsh: md5.New(),
	}
}

type hashWriter struct {
	fio *os.File
	hsh hash.Hash
}

func (hw *hashWriter) Write(b []byte) (n int, err error) {
	if n, err = hw.fio.Write(b); err == nil {
		_, err = hw.hsh.Write(b[0:n])
	}
	return
}

func (hw *hashWriter) WriteAt(b []byte, off int64) (n int, err error) {
	if n, err = hw.fio.WriteAt(b, off); err == nil {
		_, err = hw.hsh.Write(b[0:n])
	}
	return
}

func (hw *hashWriter) Sum() []byte {
	return hw.hsh.Sum(nil)
}
