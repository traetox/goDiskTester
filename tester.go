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

	defaultSmartCtlBin  = `/usr/sbin/smartctl`
	SmartBinEnvOverride = `SMARTCTL_PATH`
)

type diskTestResult struct {
	TimeToRun       string
	Size            uint64
	HumanSize       string
	Written         uint64
	Read            uint64
	ContiguousWrite string
	ContiguousRead  string
	RandomWrite     string
	RandomRead      string
}

func testDisk(pth string) (res diskTestResult, err error) {
	var fio *os.File
	if fio, err = os.OpenFile(pth, os.O_RDWR, 0600); err != nil {
		return
	}

	//get the disk sizes
	var sz int64
	if sz, err = getBlockDeviceSize(fio); err != nil {
		return
	}
	blockAlignedSize := sz - (sz % blockSize)
	writeSize := (*wSize * GB) / 2 //because we do contiguous and random
	if writeSize > blockAlignedSize {
		writeSize = blockAlignedSize
	}

	ts := time.Now()

	var cw, cr, rw, rr string

	//do the big contiguous read/write
	if cw, cr, err = contiguousTest(fio, writeSize); err != nil {
		return
	}

	//do the random tests
	if rw, rr, err = randomTest(fio, writeSize, blockAlignedSize); err != nil {
		return
	}

	if err = fio.Close(); err != nil {
		return
	}
	res = diskTestResult{
		TimeToRun:       time.Since(ts).String(),
		Written:         uint64(writeSize * 2),
		Read:            uint64(writeSize * 2),
		Size:            uint64(sz),
		HumanSize:       ingest.HumanSize(uint64(sz)),
		ContiguousWrite: cw,
		ContiguousRead:  cr,
		RandomWrite:     rw,
		RandomRead:      rr,
	}
	return
}

func newBlock(mp map[int64]bool, rr *rand.Rand, maxBlock int64) (block int64) {
	for {
		block = rr.Int63n(maxBlock)
		if _, ok := mp[block]; !ok {
			mp[block] = true
			break
		}
	}
	return
}

func randomTest(fio *os.File, writeSize, totalSize int64) (w, r string, err error) {
	var n int
	blocksToWrite := writeSize / blockSize
	maxBlock := totalSize / blockSize

	blocks := make([]int64, 0, blocksToWrite)
	hw := newHashWriter(fio)
	randRdr := rand.New(rand.NewSource(time.Now().UnixNano()))

	ts := time.Now()
	mp := map[int64]bool{}
	for i := 0; i < int(blocksToWrite); i++ {
		block := make([]byte, blockSize)
		//pick a random block
		blockOffset := newBlock(mp, randRdr, maxBlock) * blockSize
		if n, err = randRdr.Read(block); err != nil || n != len(block) {
			err = fmt.Errorf("failed to create random data block [%d:%d} %w", n, len(block), err)
			return
		} else if n, err = hw.WriteAt(block, blockOffset); err != nil {
			return
		} else if err = fio.Sync(); err != nil {
			err = fmt.Errorf("Failed to sync random writes %w", err)
			return
		} else if n != len(block) {
			err = fmt.Errorf("failed to write complete datablock %d/%d", n, len(block))
			return
		}
		blocks = append(blocks, blockOffset)
	}
	w = ingest.HumanRate(uint64(writeSize), time.Since(ts))
	ts = time.Now()

	hsh := md5.New()
	for _, off := range blocks {
		block := make([]byte, blockSize)
		if n, err = fio.ReadAt(block, off); err != nil || n != len(block) {
			err = fmt.Errorf("failed to read block at 0x%x [%d:%d} %w", off, n, len(block), err)
			return
		} else if n, err = hsh.Write(block); err != nil || n != len(block) {
			err = fmt.Errorf("failed to copy block at 0x%x into hasher [%d:%d} %w", off, n, len(block), err)
			return
		}
	}
	r = ingest.HumanRate(uint64(writeSize), time.Since(ts))

	wSum := hw.Sum()
	rSum := hsh.Sum(nil)
	if v := bytes.Compare(wSum, rSum); v != 0 {
		err = fmt.Errorf("random write and read hashes do not match: %x != %x", rSum, wSum)
	}

	return
}

func contiguousTest(fio *os.File, writeSize int64) (w, r string, err error) {
	var written int64
	var read int64
	if _, err = fio.Seek(0, 0); err != nil {
		return
	}

	//write it out
	hw := newHashWriter(fio)
	randRdr := rand.New(rand.NewSource(time.Now().UnixNano()))
	ts := time.Now()
	if written, err = io.CopyN(hw, randRdr, writeSize); err != nil {
		return
	} else if written != writeSize {
		err = fmt.Errorf("Failed to write %d bytes, only %d written", writeSize, written)
		return
	} else if err = fio.Sync(); err != nil {
		err = fmt.Errorf("Failed to sync contiguous write %w", err)
		return
	}
	w = ingest.HumanRate(uint64(writeSize), time.Since(ts))
	ts = time.Now()
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
	r = ingest.HumanRate(uint64(writeSize), time.Since(ts))
	wSum := hw.Sum()
	rSum := hsh.Sum(nil)
	if v := bytes.Compare(wSum, rSum); v != 0 {
		err = fmt.Errorf("contiguous write and read hashes do not match: %x != %x", rSum, wSum)
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

func getBlockDeviceSize(fio *os.File) (sz int64, err error) {
	if sz, err = fio.Seek(0, io.SeekEnd); err == nil {
		_, err = fio.Seek(0, 0)
	}
	return
}

/*
func SmartTest(pth string) (res json.RawMessage) {
	//check
	binPath := defaultSmartCtlBin
	if override := os.GetEnv(SmartBinEnvOverride); override != `` {
		binPath = override
	}

	//check if the bin exists
	var fi os.FileInfo
	if fi, err = os.State(binPath); err != nil {
		return errJson(err)
	} else if fi.Mode().IsRegular() == false {
		return errJson(errors.New("smartctl not a regular file"))
	}
}

type errStruct struct {
	Error error
}

func errJson(err error) json.RawMessage {
	if err == nil {
		return []byte("{}")
	}
	bts, err := json.Marshal(errStruct{Error: err})
	if err != nil {
		return []byte("{}")
	}
	return json.RawMessage(bts)
}

*/
